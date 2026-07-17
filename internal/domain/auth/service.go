package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"regexp"
	"sync"
	"time"
	"unicode"

	"github.com/rs/zerolog/log"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/internal/config"
	"github.com/nicoflow/nicoflow-api/pkg/emailutil"
	"github.com/nicoflow/nicoflow-api/pkg/hashutil"
	"github.com/nicoflow/nicoflow-api/pkg/jwtutil"
)

var usernameRE = regexp.MustCompile(`^[a-zA-Z0-9]{3,20}$`)

// dummyHash is computed once at first login attempt against a nonexistent email.
// Running bcrypt on the not-found path makes response time uniform, preventing timing-based
// email enumeration (an attacker cannot distinguish "no such user" from "wrong password").
var (
	dummyHashOnce sync.Once
	dummyHash     string
)

func getDummyHash() string {
	dummyHashOnce.Do(func() {
		dummyHash, _ = hashutil.Hash("dummy-constant-for-timing-safety")
	})
	return dummyHash
}

// Service defines auth and user management business logic.
type Service interface {
	Register(ctx context.Context, req RegisterRequest) (AuthResponse, error)
	Login(ctx context.Context, req LoginRequest) (AuthResponse, error)
	Logout(ctx context.Context, userID, rawRefreshToken string) error
	LogoutAll(ctx context.Context, userID string) error
	RefreshToken(ctx context.Context, rawRefreshToken string) (AuthResponse, error)
	ForgotPassword(ctx context.Context, email string) error
	ResetPassword(ctx context.Context, req ResetPasswordRequest) error
	ChangePassword(ctx context.Context, userID string, req ChangePasswordRequest) (AuthResponse, error)
	VerifyEmail(ctx context.Context, req VerifyEmailRequest) error
	ResendVerification(ctx context.Context, email string) error
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

	// Send the verification email. Register does NOT log the user in — they must
	// verify, then sign in. So we return the user only, no tokens/cookie.
	s.sendVerificationEmail(ctx, user)

	return AuthResponse{User: userToView(user)}, nil
}

// sendVerificationEmail issues a fresh email-verification token and dispatches
// the verification link. The token is stored synchronously (so it exists the
// moment registration returns), but the SMTP send is fired in a detached
// goroutine: it must not block the HTTP response, and the request ctx is
// cancelled once the response is written. Failures are logged, never returned —
// verification is best-effort and the user can always resend.
func (s *service) sendVerificationEmail(ctx context.Context, user User) {
	rawToken, err := generateRawToken()
	if err != nil {
		log.Error().Err(err).Str("user_id", user.ID).Msg("verify-email: generate token failed")
		return
	}
	tokenHash, err := hashutil.Hash(rawToken)
	if err != nil {
		log.Error().Err(err).Str("user_id", user.ID).Msg("verify-email: hash failed")
		return
	}
	fp := fingerprint(rawToken)
	expiresAt := time.Now().Add(24 * time.Hour)
	if err := s.repo.StoreEmailVerificationToken(ctx, user.ID, tokenHash, fp, expiresAt); err != nil {
		log.Error().Err(err).Str("user_id", user.ID).Msg("verify-email: store token failed")
		return
	}
	if s.cfg.SMTPDsn == "" {
		return
	}
	verifyURL := s.cfg.AppBaseURL + "/verify-email?token=" + rawToken
	go func() {
		if err := emailutil.SendVerificationEmail(user.Email, verifyURL, s.cfg.SMTPDsn); err != nil {
			log.Error().Err(err).Str("user_id", user.ID).Msg("verify-email: send failed")
		}
	}()
}

func (s *service) Login(ctx context.Context, req LoginRequest) (AuthResponse, error) {
	identifier := req.LoginIdentifier()
	if identifier == "" || req.Password == "" {
		return AuthResponse{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "identifier and password are required")
	}

	// Look up by email or username. The response is intentionally identical for
	// "no such user" and "wrong password" to prevent account enumeration.
	user, err := s.repo.GetUserByIdentifier(ctx, identifier)
	if err != nil {
		var ae *apperror.AppError
		if errors.As(err, &ae) && ae.Code == apperror.ErrUserNotFound {
			// Constant-time: run bcrypt so response time matches the "wrong password" path.
			hashutil.Compare(getDummyHash(), req.Password)
			return AuthResponse{}, apperror.New(http.StatusUnauthorized, apperror.ErrUnauthorized, "invalid credentials")
		}
		return AuthResponse{}, err
	}

	// Account lockout check.
	if user.LockedUntil != nil && time.Now().Before(*user.LockedUntil) {
		return AuthResponse{}, apperror.New(http.StatusTooManyRequests, apperror.ErrRateLimited, "account temporarily locked due to too many failed attempts")
	}

	if !hashutil.Compare(user.PasswordHash, req.Password) {
		_ = s.repo.IncrementFailedLogin(ctx, user.ID)
		return AuthResponse{}, apperror.New(http.StatusUnauthorized, apperror.ErrUnauthorized, "invalid credentials")
	}

	// Successful login — reset counter.
	_ = s.repo.ResetFailedLogin(ctx, user.ID)

	// Gate unverified accounts (after the password check, so we don't leak
	// verification state to someone who doesn't know the password).
	if s.cfg.RequireEmailVerification && !user.EmailVerified {
		return AuthResponse{}, apperror.New(http.StatusForbidden, apperror.ErrEmailNotVerified, "email not verified")
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

	// Atomic DELETE RETURNING — eliminates the race window between a prior SELECT and DELETE.
	// If the token was already consumed (concurrent retry), the row is gone and we get ErrInvalidToken.
	rt, err := s.repo.DeleteRefreshTokenReturning(ctx, fp)
	if err != nil {
		return AuthResponse{}, err
	}

	// bcrypt-verify the returned hash to detect tampering.
	if !hashutil.Compare(rt.TokenHash, rawRefreshToken) {
		_ = s.repo.DeleteAllRefreshTokens(ctx, rt.UserID)
		return AuthResponse{}, apperror.New(http.StatusUnauthorized, apperror.ErrInvalidToken, "invalid refresh token")
	}

	// Reject an expired token. The DELETE RETURNING above already consumed the row,
	// so an expired token is now gone either way — but we must not mint a new access
	// token from it. Background GC only sweeps hourly, so without this check an
	// expired-but-not-yet-purged token would still refresh. Mirrors the expiry check
	// in ResetPassword/VerifyEmail.
	if time.Now().After(rt.ExpiresAt) {
		return AuthResponse{}, apperror.New(http.StatusUnauthorized, apperror.ErrInvalidToken, "refresh token expired")
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
	if s.cfg.SMTPDsn != "" {
		// Detached send: the SMTP exchange must not block the response, and the
		// request ctx is cancelled once we return. The reset row is already
		// persisted, so the link works regardless of send latency.
		userID := user.ID
		go func() {
			if err := emailutil.SendPasswordReset(email, resetURL, s.cfg.SMTPDsn); err != nil {
				log.Error().Err(err).Str("user_id", userID).Msg("failed to send password reset email")
			}
		}()
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

// ChangePassword changes the password for an already-authenticated user.
// It re-verifies currentPassword via bcrypt (mismatch → 401, no enumeration
// concern since the caller is authenticated), enforces the register password
// policy on newPassword server-side, then revokes ALL of the user's refresh
// tokens and issues a fresh pair to the caller — every other device is signed
// out while the changer stays logged in. A "password changed" email is fired
// best-effort as a defense-in-depth signal.
func (s *service) ChangePassword(ctx context.Context, userID string, req ChangePasswordRequest) (AuthResponse, error) {
	if req.NewPassword != req.ConfirmPassword {
		return AuthResponse{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "passwords do not match")
	}
	if err := validatePassword(req.NewPassword); err != nil {
		return AuthResponse{}, err
	}

	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return AuthResponse{}, err
	}

	if !hashutil.Compare(user.PasswordHash, req.CurrentPassword) {
		return AuthResponse{}, apperror.New(http.StatusUnauthorized, apperror.ErrUnauthorized, "current password is incorrect")
	}

	newHash, err := hashutil.Hash(req.NewPassword)
	if err != nil {
		return AuthResponse{}, fmt.Errorf("auth.ChangePassword hash: %w", err)
	}
	if err := s.repo.UpdatePassword(ctx, userID, newHash); err != nil {
		return AuthResponse{}, err
	}

	// Revoke every refresh token (kicks all devices, including the caller's old
	// one), then issue a fresh pair so the changer stays signed in.
	if err := s.repo.DeleteAllRefreshTokens(ctx, userID); err != nil {
		return AuthResponse{}, err
	}

	// Best-effort defense-in-depth email; must not block or fail the change.
	if s.cfg.SMTPDsn != "" {
		email := user.Email
		userIDCopy := user.ID
		go func() {
			if err := emailutil.SendPasswordChanged(email, s.cfg.SMTPDsn); err != nil {
				log.Error().Err(err).Str("user_id", userIDCopy).Msg("password-changed: send failed")
			}
		}()
	}

	return s.issueAuthResponse(ctx, user, s.cfg.RefreshTokenExpiry)
}

// VerifyEmail consumes a verification token and marks the user's email verified.
// Single-use, expiry-checked, dual-hash verified (mirrors ResetPassword).
func (s *service) VerifyEmail(ctx context.Context, req VerifyEmailRequest) error {
	if req.Token == "" {
		return apperror.New(http.StatusUnauthorized, apperror.ErrInvalidToken, "verification token required")
	}

	fp := fingerprint(req.Token)
	evt, err := s.repo.GetEmailVerificationTokenByFingerprint(ctx, fp)
	if err != nil {
		return err
	}
	if evt.UsedAt != nil {
		return apperror.New(http.StatusUnauthorized, apperror.ErrInvalidToken, "verification token already used")
	}
	if time.Now().After(evt.ExpiresAt) {
		return apperror.New(http.StatusUnauthorized, apperror.ErrInvalidToken, "verification token expired")
	}
	if !hashutil.Compare(evt.TokenHash, req.Token) {
		return apperror.New(http.StatusUnauthorized, apperror.ErrInvalidToken, "invalid verification token")
	}

	if err := s.repo.MarkEmailVerified(ctx, evt.UserID); err != nil {
		return err
	}
	return s.repo.MarkEmailVerificationTokenUsed(ctx, fp)
}

// ResendVerification re-issues a verification email. Returns success regardless
// of whether the email exists, to avoid user enumeration.
func (s *service) ResendVerification(ctx context.Context, email string) error {
	if err := validateEmail(email); err != nil {
		return nil
	}
	user, err := s.repo.GetUserByEmail(ctx, email)
	if err != nil {
		return nil
	}
	s.sendVerificationEmail(ctx, user)
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
	if req.Language != nil {
		if err := validateLanguage(*req.Language); err != nil {
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
	return apperror.New(http.StatusNotImplemented, apperror.ErrInternalServerError, "push notifications are not yet available")
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
	if _, err := mail.ParseAddress(email); err != nil {
		return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidEmail, "invalid email address")
	}
	return nil
}

// supportedLanguages is the allowlist for the user's UI language preference.
// Kept in sync with the frontend SUPPORTED_LANGUAGES and SPEC §10. Validated in
// the service layer (no DB CHECK) so adding a language is a code-only change.
var supportedLanguages = map[string]bool{"en": true, "he": true, "ru": true}

func validateLanguage(language string) error {
	if !supportedLanguages[language] {
		return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "unsupported language; must be one of en, he, ru")
	}
	return nil
}

func validatePassword(password string) error {
	// Policy: 8–72 chars (bcrypt truncates beyond 72 bytes), with at least one
	// uppercase and one lowercase letter. Keep this in sync with the frontend
	// passwordSchema and SPEC §3.
	if len(password) < 8 {
		return apperror.New(http.StatusBadRequest, apperror.ErrWeakPassword, "password must be at least 8 characters with an uppercase and a lowercase letter")
	}
	if len(password) > 72 {
		return apperror.New(http.StatusBadRequest, apperror.ErrWeakPassword, "password must be at most 72 characters")
	}
	var hasUpper, hasLower bool
	for _, r := range password {
		switch {
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsLower(r):
			hasLower = true
		}
	}
	if !hasUpper || !hasLower {
		return apperror.New(http.StatusBadRequest, apperror.ErrWeakPassword, "password must contain at least one uppercase and one lowercase letter")
	}
	return nil
}
