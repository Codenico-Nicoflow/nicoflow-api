package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"time"
	"unicode"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/internal/config"
	"github.com/nicoflow/nicoflow-api/pkg/emailutil"
	"github.com/nicoflow/nicoflow-api/pkg/hashutil"
	"github.com/nicoflow/nicoflow-api/pkg/jwtutil"
)

var emailRE = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)
var usernameRE = regexp.MustCompile(`^[a-zA-Z0-9]{3,20}$`)

// Service defines auth and user management business logic.
type Service interface {
	Register(ctx context.Context, req RegisterRequest) (AuthResponse, error)
	Login(ctx context.Context, req LoginRequest) (AuthResponse, error)
	Logout(ctx context.Context, userID, rawRefreshToken string) error
	LogoutAll(ctx context.Context, userID string) error
	RefreshToken(ctx context.Context, rawRefreshToken string) (AuthResponse, error)
	ForgotPassword(ctx context.Context, email string) error
	ResetPassword(ctx context.Context, req ResetPasswordRequest) error
	GetProfile(ctx context.Context, userID string) (UserView, error)
	UpdateMe(ctx context.Context, userID string, req UpdateMeRequest) (UserView, error)
	DeleteMe(ctx context.Context, userID string) error
	RegisterPushToken(ctx context.Context, userID string, req RegisterPushTokenRequest) error
}

type service struct {
	repo Repository
	cfg  config.Config
}

// NewService creates a new auth service.
func NewService(repo Repository, cfg config.Config) Service {
	return &service{repo: repo, cfg: cfg}
}

func (s *service) Register(ctx context.Context, req RegisterRequest) (AuthResponse, error) {
	if err := validateEmail(req.Email); err != nil {
		return AuthResponse{}, err
	}
	if err := validatePassword(req.Password); err != nil {
		return AuthResponse{}, err
	}
	if !usernameRE.MatchString(req.Username) {
		return AuthResponse{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "username must be 3-20 alphanumeric characters")
	}

	hash, err := hashutil.Hash(req.Password)
	if err != nil {
		return AuthResponse{}, fmt.Errorf("auth.Register hash: %w", err)
	}

	user, err := s.repo.CreateUser(ctx, req.Email, req.Username, hash)
	if err != nil {
		return AuthResponse{}, err
	}

	return s.issueAuthResponse(ctx, user, s.cfg.RefreshTokenExpiry)
}

func (s *service) Login(ctx context.Context, req LoginRequest) (AuthResponse, error) {
	if req.Email == "" || req.Password == "" {
		return AuthResponse{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "email and password are required")
	}

	user, err := s.repo.GetUserByEmail(ctx, req.Email)
	if err != nil {
		// Return 401 regardless of whether user exists (no enumeration).
		var ae *apperror.AppError
		if errors.As(err, &ae) && ae.Code == apperror.ErrUserNotFound {
			return AuthResponse{}, apperror.New(http.StatusUnauthorized, apperror.ErrUnauthorized, "invalid credentials")
		}
		return AuthResponse{}, err
	}

	if !hashutil.Compare(user.PasswordHash, req.Password) {
		return AuthResponse{}, apperror.New(http.StatusUnauthorized, apperror.ErrUnauthorized, "invalid credentials")
	}

	tokenTTL := s.cfg.RefreshTokenExpiry
	if !req.Remember {
		tokenTTL = 24 * time.Hour
	}
	return s.issueAuthResponse(ctx, user, tokenTTL)
}

func (s *service) Logout(ctx context.Context, _ string, rawRefreshToken string) error {
	if rawRefreshToken == "" {
		return nil
	}
	fingerprint := fingerprint(rawRefreshToken)
	_, err := s.repo.DeleteRefreshToken(ctx, fingerprint)
	return err
}

func (s *service) LogoutAll(ctx context.Context, userID string) error {
	return s.repo.DeleteAllRefreshTokens(ctx, userID)
}

func (s *service) RefreshToken(ctx context.Context, rawRefreshToken string) (AuthResponse, error) {
	if rawRefreshToken == "" {
		return AuthResponse{}, apperror.New(http.StatusUnauthorized, apperror.ErrInvalidToken, "refresh token required")
	}

	fp := fingerprint(rawRefreshToken)

	rt, err := s.repo.GetRefreshTokenByFingerprint(ctx, fp)
	if err != nil {
		return AuthResponse{}, err
	}

	if !hashutil.Compare(rt.TokenHash, rawRefreshToken) {
		// Token tampered — revoke all sessions for the user.
		_ = s.repo.DeleteAllRefreshTokens(ctx, rt.UserID)
		return AuthResponse{}, apperror.New(http.StatusUnauthorized, apperror.ErrInvalidToken, "invalid refresh token")
	}

	// Atomic rotation: delete old token; 0 rows = already consumed (reuse attack).
	rows, err := s.repo.DeleteRefreshToken(ctx, fp)
	if err != nil {
		return AuthResponse{}, err
	}
	if rows == 0 {
		_ = s.repo.DeleteAllRefreshTokens(ctx, rt.UserID)
		return AuthResponse{}, apperror.New(http.StatusUnauthorized, apperror.ErrInvalidToken, "refresh token already used")
	}

	user, err := s.repo.GetUserByID(ctx, rt.UserID)
	if err != nil {
		return AuthResponse{}, err
	}

	return s.issueAuthResponse(ctx, user, s.cfg.RefreshTokenExpiry)
}

func (s *service) ForgotPassword(ctx context.Context, email string) error {
	if err := validateEmail(email); err != nil {
		// Return nil — never reveal whether the email exists.
		return nil
	}

	user, err := s.repo.GetUserByEmail(ctx, email)
	if err != nil {
		// Always return success to prevent user enumeration.
		return nil
	}

	rawToken, err := generateRawToken()
	if err != nil {
		return fmt.Errorf("auth.ForgotPassword generate token: %w", err)
	}

	tokenHash, err := hashutil.Hash(rawToken)
	if err != nil {
		return fmt.Errorf("auth.ForgotPassword hash: %w", err)
	}

	fp := fingerprint(rawToken)
	expiresAt := time.Now().Add(time.Hour)

	if err := s.repo.StorePasswordResetToken(ctx, user.ID, tokenHash, fp, expiresAt); err != nil {
		return err
	}

	resetURL := s.cfg.AppBaseURL + "/reset-password?token=" + rawToken
	// Best-effort email send — log failure but don't surface to caller.
	if s.cfg.SMTPDsn != "" {
		_ = emailutil.SendPasswordReset(email, resetURL, s.cfg.SMTPDsn)
	}

	return nil
}

func (s *service) ResetPassword(ctx context.Context, req ResetPasswordRequest) error {
	if req.NewPassword != req.ConfirmPassword {
		return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "passwords do not match")
	}
	if err := validatePassword(req.NewPassword); err != nil {
		return err
	}
	if req.Token == "" {
		return apperror.New(http.StatusUnauthorized, apperror.ErrInvalidToken, "reset token required")
	}

	fp := fingerprint(req.Token)
	prt, err := s.repo.GetPasswordResetTokenByFingerprint(ctx, fp)
	if err != nil {
		return err
	}

	if prt.UsedAt != nil {
		return apperror.New(http.StatusUnauthorized, apperror.ErrInvalidToken, "reset token already used")
	}
	if time.Now().After(prt.ExpiresAt) {
		return apperror.New(http.StatusUnauthorized, apperror.ErrInvalidToken, "reset token expired")
	}
	if !hashutil.Compare(prt.TokenHash, req.Token) {
		return apperror.New(http.StatusUnauthorized, apperror.ErrInvalidToken, "invalid reset token")
	}

	newHash, err := hashutil.Hash(req.NewPassword)
	if err != nil {
		return fmt.Errorf("auth.ResetPassword hash: %w", err)
	}

	if err := s.repo.UpdatePassword(ctx, prt.UserID, newHash); err != nil {
		return err
	}
	if err := s.repo.MarkPasswordResetTokenUsed(ctx, fp); err != nil {
		return err
	}
	// Invalidate all refresh tokens so existing sessions must re-login.
	_ = s.repo.DeleteAllRefreshTokens(ctx, prt.UserID)

	return nil
}

func (s *service) GetProfile(ctx context.Context, userID string) (UserView, error) {
	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return UserView{}, err
	}
	return userToView(user), nil
}

func (s *service) UpdateMe(ctx context.Context, userID string, req UpdateMeRequest) (UserView, error) {
	if req.Email != nil {
		if err := validateEmail(*req.Email); err != nil {
			return UserView{}, err
		}
	}
	user, err := s.repo.UpdateUser(ctx, userID, req)
	if err != nil {
		return UserView{}, err
	}
	return userToView(user), nil
}

func (s *service) DeleteMe(ctx context.Context, userID string) error {
	if err := s.repo.SoftDeleteUser(ctx, userID); err != nil {
		return err
	}
	_ = s.repo.DeleteAllRefreshTokens(ctx, userID)
	return nil
}

func (s *service) RegisterPushToken(_ context.Context, _ string, _ RegisterPushTokenRequest) error {
	// Push notification tokens are stored in a future story (E-push).
	// Accept the request and return 201 without error.
	return nil
}

// issueAuthResponse generates a fresh refresh token + JWT and returns the AuthResponse.
// tokenTTL controls both the DB expiry and the cookie Max-Age sent to the client.
func (s *service) issueAuthResponse(ctx context.Context, user User, tokenTTL time.Duration) (AuthResponse, error) {
	rawToken, err := generateRawToken()
	if err != nil {
		return AuthResponse{}, fmt.Errorf("auth: generate refresh token: %w", err)
	}

	tokenHash, err := hashutil.Hash(rawToken)
	if err != nil {
		return AuthResponse{}, fmt.Errorf("auth: hash refresh token: %w", err)
	}

	fp := fingerprint(rawToken)
	expiresAt := time.Now().Add(tokenTTL)
	if err := s.repo.StoreRefreshToken(ctx, user.ID, tokenHash, fp, expiresAt); err != nil {
		return AuthResponse{}, err
	}

	jwt, err := jwtutil.Issue(user.ID, user.Email, user.Plan, s.cfg.JWTSecret, s.cfg.JWTExpiry)
	if err != nil {
		return AuthResponse{}, err
	}

	return AuthResponse{
		Token:        jwt,
		RefreshToken: rawToken,
		User:         userToView(user),
		CookieMaxAge: int(tokenTTL.Seconds()),
	}, nil
}

// generateRawToken returns a 32-byte cryptographically random hex string (64 chars).
func generateRawToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generateRawToken: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// fingerprint returns the SHA-256 hex of the raw token for O(1) DB lookup.
func fingerprint(rawToken string) string {
	sum := sha256.Sum256([]byte(rawToken))
	return hex.EncodeToString(sum[:])
}

func validateEmail(email string) error {
	if !emailRE.MatchString(email) {
		return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidEmail, "invalid email address")
	}
	return nil
}

func validatePassword(password string) error {
	if len(password) < 8 || len(password) > 20 {
		return apperror.New(http.StatusBadRequest, apperror.ErrWeakPassword, "password must be 8-20 characters")
	}
	var hasUpper, hasLower, hasDigit bool
	for _, c := range password {
		switch {
		case unicode.IsUpper(c):
			hasUpper = true
		case unicode.IsLower(c):
			hasLower = true
		case unicode.IsDigit(c):
			hasDigit = true
		}
	}
	if !hasUpper || !hasLower || !hasDigit {
		return apperror.New(http.StatusBadRequest, apperror.ErrWeakPassword, "password must contain at least 1 uppercase letter, 1 lowercase letter, and 1 digit")
	}
	return nil
}
