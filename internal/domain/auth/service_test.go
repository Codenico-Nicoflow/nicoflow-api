package auth_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/internal/config"
	"github.com/nicoflow/nicoflow-api/internal/domain/auth"
	"github.com/nicoflow/nicoflow-api/pkg/hashutil"
)

// mockRepo implements auth.Repository for testing.
type mockRepo struct {
	createUserFn                     func(ctx context.Context, email, username, passwordHash string) (auth.User, error)
	getUserByEmailFn                 func(ctx context.Context, email string) (auth.User, error)
	getUserByIDFn                    func(ctx context.Context, userID string) (auth.User, error)
	updateUserFn                     func(ctx context.Context, userID string, req auth.UpdateMeRequest) (auth.User, error)
	softDeleteUserFn                 func(ctx context.Context, userID string) error
	storeRefreshTokenFn              func(ctx context.Context, userID, tokenHash, fp string, expiresAt time.Time) error
	getRefreshTokenByFingerprintFn   func(ctx context.Context, fingerprint string) (auth.RefreshToken, error)
	deleteRefreshTokenFn             func(ctx context.Context, fingerprint string) (int64, error)
	deleteRefreshTokenReturningFn    func(ctx context.Context, fingerprint string) (auth.RefreshToken, error)
	deleteAllRefreshTokensFn         func(ctx context.Context, userID string) error
	incrementFailedLoginFn           func(ctx context.Context, userID string) error
	resetFailedLoginFn               func(ctx context.Context, userID string) error
	storePasswordResetTokenFn        func(ctx context.Context, userID, tokenHash, fp string, expiresAt time.Time) error
	getPasswordResetTokenByFpFn      func(ctx context.Context, fingerprint string) (auth.PasswordResetToken, error)
	markPasswordResetTokenUsedFn     func(ctx context.Context, fingerprint string) error
	updatePasswordFn                 func(ctx context.Context, userID, passwordHash string) error
	getUserByIdentifierFn            func(ctx context.Context, identifier string) (auth.User, error)
	storeEmailVerificationTokenFn    func(ctx context.Context, userID, tokenHash, fp string, expiresAt time.Time) error
	getEmailVerificationTokenByFpFn  func(ctx context.Context, fingerprint string) (auth.EmailVerificationToken, error)
	markEmailVerificationTokenUsedFn func(ctx context.Context, fingerprint string) error
	markEmailVerifiedFn              func(ctx context.Context, userID string) error
}

func (m *mockRepo) CreateUser(ctx context.Context, email, username, passwordHash string) (auth.User, error) {
	return m.createUserFn(ctx, email, username, passwordHash)
}
func (m *mockRepo) GetUserByEmail(ctx context.Context, email string) (auth.User, error) {
	return m.getUserByEmailFn(ctx, email)
}
func (m *mockRepo) GetUserByID(ctx context.Context, userID string) (auth.User, error) {
	return m.getUserByIDFn(ctx, userID)
}
func (m *mockRepo) UpdateUser(ctx context.Context, userID string, req auth.UpdateMeRequest) (auth.User, error) {
	return m.updateUserFn(ctx, userID, req)
}
func (m *mockRepo) SoftDeleteUser(ctx context.Context, userID string) error {
	return m.softDeleteUserFn(ctx, userID)
}
func (m *mockRepo) StoreRefreshToken(ctx context.Context, userID, tokenHash, fp string, expiresAt time.Time) error {
	return m.storeRefreshTokenFn(ctx, userID, tokenHash, fp, expiresAt)
}
func (m *mockRepo) GetRefreshTokenByFingerprint(ctx context.Context, fingerprint string) (auth.RefreshToken, error) {
	return m.getRefreshTokenByFingerprintFn(ctx, fingerprint)
}
func (m *mockRepo) DeleteRefreshToken(ctx context.Context, fingerprint string) (int64, error) {
	return m.deleteRefreshTokenFn(ctx, fingerprint)
}
func (m *mockRepo) DeleteRefreshTokenReturning(ctx context.Context, fingerprint string) (auth.RefreshToken, error) {
	return m.deleteRefreshTokenReturningFn(ctx, fingerprint)
}
func (m *mockRepo) DeleteAllRefreshTokens(ctx context.Context, userID string) error {
	return m.deleteAllRefreshTokensFn(ctx, userID)
}
func (m *mockRepo) IncrementFailedLogin(ctx context.Context, userID string) error {
	return m.incrementFailedLoginFn(ctx, userID)
}
func (m *mockRepo) ResetFailedLogin(ctx context.Context, userID string) error {
	return m.resetFailedLoginFn(ctx, userID)
}
func (m *mockRepo) StorePasswordResetToken(ctx context.Context, userID, tokenHash, fp string, expiresAt time.Time) error {
	return m.storePasswordResetTokenFn(ctx, userID, tokenHash, fp, expiresAt)
}
func (m *mockRepo) GetPasswordResetTokenByFingerprint(ctx context.Context, fingerprint string) (auth.PasswordResetToken, error) {
	return m.getPasswordResetTokenByFpFn(ctx, fingerprint)
}
func (m *mockRepo) MarkPasswordResetTokenUsed(ctx context.Context, fingerprint string) error {
	return m.markPasswordResetTokenUsedFn(ctx, fingerprint)
}
func (m *mockRepo) UpdatePassword(ctx context.Context, userID, passwordHash string) error {
	return m.updatePasswordFn(ctx, userID, passwordHash)
}
func (m *mockRepo) GetUserByIdentifier(ctx context.Context, identifier string) (auth.User, error) {
	if m.getUserByIdentifierFn != nil {
		return m.getUserByIdentifierFn(ctx, identifier)
	}
	// Default: behave like email lookup so existing login tests keep working.
	return m.getUserByEmailFn(ctx, identifier)
}
func (m *mockRepo) StoreEmailVerificationToken(ctx context.Context, userID, tokenHash, fp string, expiresAt time.Time) error {
	if m.storeEmailVerificationTokenFn != nil {
		return m.storeEmailVerificationTokenFn(ctx, userID, tokenHash, fp, expiresAt)
	}
	return nil
}
func (m *mockRepo) GetEmailVerificationTokenByFingerprint(ctx context.Context, fingerprint string) (auth.EmailVerificationToken, error) {
	if m.getEmailVerificationTokenByFpFn != nil {
		return m.getEmailVerificationTokenByFpFn(ctx, fingerprint)
	}
	return auth.EmailVerificationToken{}, nil
}
func (m *mockRepo) MarkEmailVerificationTokenUsed(ctx context.Context, fingerprint string) error {
	if m.markEmailVerificationTokenUsedFn != nil {
		return m.markEmailVerificationTokenUsedFn(ctx, fingerprint)
	}
	return nil
}
func (m *mockRepo) MarkEmailVerified(ctx context.Context, userID string) error {
	if m.markEmailVerifiedFn != nil {
		return m.markEmailVerifiedFn(ctx, userID)
	}
	return nil
}

func testCfg() config.Config {
	return config.Config{
		JWTSecret:          "test-secret-minimum-32-bytes-here!!",
		JWTExpiry:          15 * time.Minute,
		RefreshTokenExpiry: 7 * 24 * time.Hour,
	}
}

func fixedUser() auth.User {
	return auth.User{
		ID:            "usr_abc123",
		Email:         "user@example.com",
		Username:      "johndoe",
		Plan:          "free",
		Theme:         "light",
		Language:      "en",
		Status:        "regular",
		Timezone:      "UTC",
		EmailVerified: true,
	}
}

func happyRepo() *mockRepo {
	return &mockRepo{
		createUserFn: func(_ context.Context, _, _, _ string) (auth.User, error) {
			return fixedUser(), nil
		},
		getUserByEmailFn: func(_ context.Context, _ string) (auth.User, error) {
			u := fixedUser()
			hash, _ := hashutil.Hash("Secret123")
			u.PasswordHash = hash
			return u, nil
		},
		getUserByIDFn: func(_ context.Context, _ string) (auth.User, error) {
			return fixedUser(), nil
		},
		updateUserFn: func(_ context.Context, _ string, _ auth.UpdateMeRequest) (auth.User, error) {
			return fixedUser(), nil
		},
		softDeleteUserFn:               func(_ context.Context, _ string) error { return nil },
		storeRefreshTokenFn:            func(_ context.Context, _, _, _ string, _ time.Time) error { return nil },
		getRefreshTokenByFingerprintFn: func(_ context.Context, _ string) (auth.RefreshToken, error) { return auth.RefreshToken{}, nil },
		deleteRefreshTokenFn:           func(_ context.Context, _ string) (int64, error) { return 1, nil },
		deleteRefreshTokenReturningFn: func(_ context.Context, _ string) (auth.RefreshToken, error) {
			hash, _ := hashutil.Hash("validrawtoken")
			return auth.RefreshToken{UserID: "usr_abc123", TokenHash: hash, ExpiresAt: time.Now().Add(time.Hour)}, nil
		},
		deleteAllRefreshTokensFn:  func(_ context.Context, _ string) error { return nil },
		incrementFailedLoginFn:    func(_ context.Context, _ string) error { return nil },
		resetFailedLoginFn:        func(_ context.Context, _ string) error { return nil },
		storePasswordResetTokenFn: func(_ context.Context, _, _, _ string, _ time.Time) error { return nil },
		getPasswordResetTokenByFpFn: func(_ context.Context, _ string) (auth.PasswordResetToken, error) {
			return auth.PasswordResetToken{}, nil
		},
		markPasswordResetTokenUsedFn: func(_ context.Context, _ string) error { return nil },
		updatePasswordFn:             func(_ context.Context, _, _ string) error { return nil },
	}
}

func TestRegister(t *testing.T) {
	tests := []struct {
		name    string
		req     auth.RegisterRequest
		repoFn  func(*mockRepo)
		wantErr string
	}{
		{
			name: "success",
			req:  auth.RegisterRequest{Email: "user@example.com", Password: "Secret123", Username: "johndoe"},
		},
		{
			name:    "invalid email",
			req:     auth.RegisterRequest{Email: "notanemail", Password: "Secret123", Username: "johndoe"},
			wantErr: apperror.ErrInvalidEmail,
		},
		{
			name:    "weak password - too short",
			req:     auth.RegisterRequest{Email: "user@example.com", Password: "Ab1", Username: "johndoe"},
			wantErr: apperror.ErrWeakPassword,
		},
		{
			name:    "weak password - too long",
			req:     auth.RegisterRequest{Email: "user@example.com", Password: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Username: "johndoe"},
			wantErr: apperror.ErrWeakPassword,
		},
		{
			name:    "weak password - no uppercase",
			req:     auth.RegisterRequest{Email: "user@example.com", Password: "secret123", Username: "johndoe"},
			wantErr: apperror.ErrWeakPassword,
		},
		{
			name:    "weak password - no lowercase",
			req:     auth.RegisterRequest{Email: "user@example.com", Password: "SECRET123", Username: "johndoe"},
			wantErr: apperror.ErrWeakPassword,
		},
		{
			name:    "invalid username",
			req:     auth.RegisterRequest{Email: "user@example.com", Password: "Secret123", Username: "x"},
			wantErr: apperror.ErrInvalidInput,
		},
		{
			name: "duplicate email",
			req:  auth.RegisterRequest{Email: "user@example.com", Password: "Secret123", Username: "johndoe"},
			repoFn: func(m *mockRepo) {
				m.createUserFn = func(_ context.Context, _, _, _ string) (auth.User, error) {
					return auth.User{}, apperror.New(http.StatusConflict, apperror.ErrEmailAlreadyExists, "email already in use")
				}
			},
			wantErr: apperror.ErrEmailAlreadyExists,
		},
		{
			name: "duplicate username",
			req:  auth.RegisterRequest{Email: "user@example.com", Password: "Secret123", Username: "johndoe"},
			repoFn: func(m *mockRepo) {
				m.createUserFn = func(_ context.Context, _, _, _ string) (auth.User, error) {
					return auth.User{}, apperror.New(http.StatusConflict, apperror.ErrUsernameAlreadyExists, "username already taken")
				}
			},
			wantErr: apperror.ErrUsernameAlreadyExists,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := happyRepo()
			if tt.repoFn != nil {
				tt.repoFn(repo)
			}
			svc := auth.NewService(repo, testCfg())

			resp, err := svc.Register(context.Background(), tt.req)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error %q, got nil", tt.wantErr)
				}
				var ae *apperror.AppError
				if !errors.As(err, &ae) || ae.Code != tt.wantErr {
					t.Fatalf("expected error code %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			// Register does not auto-login — it returns the user, no tokens.
			if resp.User.Email == "" {
				t.Error("expected user in register response")
			}
			if resp.Token != "" || resp.RefreshToken != "" {
				t.Error("expected no tokens on register (verification required before login)")
			}
		})
	}
}

func TestLogin(t *testing.T) {
	tests := []struct {
		name    string
		req     auth.LoginRequest
		repoFn  func(*mockRepo)
		wantErr string
	}{
		{
			name: "success by email identifier",
			req:  auth.LoginRequest{Identifier: "user@example.com", Password: "Secret123"},
		},
		{
			name: "success by username identifier",
			req:  auth.LoginRequest{Identifier: "johndoe", Password: "Secret123"},
			repoFn: func(m *mockRepo) {
				m.getUserByIdentifierFn = func(_ context.Context, _ string) (auth.User, error) {
					u := fixedUser()
					hash, _ := hashutil.Hash("Secret123")
					u.PasswordHash = hash
					return u, nil
				}
			},
		},
		{
			name: "success via legacy email field",
			req:  auth.LoginRequest{Email: "user@example.com", Password: "Secret123"},
		},
		{
			name:    "wrong password",
			req:     auth.LoginRequest{Identifier: "user@example.com", Password: "WrongPass1"},
			wantErr: apperror.ErrUnauthorized,
		},
		{
			name: "user not found",
			req:  auth.LoginRequest{Identifier: "nobody@example.com", Password: "Secret123"},
			repoFn: func(m *mockRepo) {
				m.getUserByIdentifierFn = func(_ context.Context, _ string) (auth.User, error) {
					return auth.User{}, apperror.New(http.StatusNotFound, apperror.ErrUserNotFound, "user not found")
				}
			},
			wantErr: apperror.ErrUnauthorized,
		},
		{
			name:    "missing credentials",
			req:     auth.LoginRequest{},
			wantErr: apperror.ErrInvalidInput,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := happyRepo()
			if tt.repoFn != nil {
				tt.repoFn(repo)
			}
			svc := auth.NewService(repo, testCfg())

			_, err := svc.Login(context.Background(), tt.req)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error %q, got nil", tt.wantErr)
				}
				var ae *apperror.AppError
				if !errors.As(err, &ae) || ae.Code != tt.wantErr {
					t.Fatalf("expected error code %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestLogin_RememberMe(t *testing.T) {
	cfg := testCfg() // RefreshTokenExpiry = 7 * 24h

	tests := []struct {
		name             string
		remember         bool
		wantCookieMaxAge int
		wantMinTTL       time.Duration
		wantMaxTTL       time.Duration
	}{
		{
			name:             "remember=true uses full RefreshTokenExpiry",
			remember:         true,
			wantCookieMaxAge: int(cfg.RefreshTokenExpiry.Seconds()), // 604800
			wantMinTTL:       cfg.RefreshTokenExpiry - time.Second,
			wantMaxTTL:       cfg.RefreshTokenExpiry + time.Second,
		},
		{
			name:             "remember=false uses 24h session TTL and zero cookie max-age",
			remember:         false,
			wantCookieMaxAge: int((24 * time.Hour).Seconds()), // 86400
			wantMinTTL:       24*time.Hour - time.Second,
			wantMaxTTL:       24*time.Hour + time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var storedExpiresAt time.Time
			repo := happyRepo()
			repo.storeRefreshTokenFn = func(_ context.Context, _, _, _ string, expiresAt time.Time) error {
				storedExpiresAt = expiresAt
				return nil
			}

			svc := auth.NewService(repo, cfg)
			resp, err := svc.Login(context.Background(), auth.LoginRequest{
				Email:    "user@example.com",
				Password: "Secret123",
				Remember: tt.remember,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if resp.CookieMaxAge != tt.wantCookieMaxAge {
				t.Errorf("CookieMaxAge = %d, want %d", resp.CookieMaxAge, tt.wantCookieMaxAge)
			}

			actualTTL := time.Until(storedExpiresAt)
			if actualTTL < tt.wantMinTTL || actualTTL > tt.wantMaxTTL {
				t.Errorf("stored token TTL = %v, want between %v and %v", actualTTL, tt.wantMinTTL, tt.wantMaxTTL)
			}
		})
	}
}

func TestLogin_EmailVerificationGate(t *testing.T) {
	tests := []struct {
		name     string
		require  bool
		verified bool
		wantErr  string
	}{
		{name: "flag on + unverified is rejected", require: true, verified: false, wantErr: apperror.ErrEmailNotVerified},
		{name: "flag on + verified succeeds", require: true, verified: true},
		{name: "flag off + unverified succeeds (dev path)", require: false, verified: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := happyRepo()
			repo.getUserByIdentifierFn = func(_ context.Context, _ string) (auth.User, error) {
				u := fixedUser()
				u.EmailVerified = tt.verified
				hash, _ := hashutil.Hash("Secret123")
				u.PasswordHash = hash
				return u, nil
			}

			cfg := testCfg()
			cfg.RequireEmailVerification = tt.require
			svc := auth.NewService(repo, cfg)

			_, err := svc.Login(context.Background(), auth.LoginRequest{Identifier: "user@example.com", Password: "Secret123"})
			if tt.wantErr != "" {
				var ae *apperror.AppError
				if !errors.As(err, &ae) || ae.Code != tt.wantErr {
					t.Fatalf("expected error code %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestLogin_WrongPassword_DoesNotLeakVerificationState(t *testing.T) {
	// With the gate on and an unverified user, a wrong password must still return
	// UNAUTHORIZED (not EMAIL_NOT_VERIFIED) — the gate is after the password check.
	repo := happyRepo()
	repo.getUserByIdentifierFn = func(_ context.Context, _ string) (auth.User, error) {
		u := fixedUser()
		u.EmailVerified = false
		hash, _ := hashutil.Hash("Secret123")
		u.PasswordHash = hash
		return u, nil
	}
	cfg := testCfg()
	cfg.RequireEmailVerification = true
	svc := auth.NewService(repo, cfg)

	_, err := svc.Login(context.Background(), auth.LoginRequest{Identifier: "user@example.com", Password: "WrongPass1"})
	var ae *apperror.AppError
	if !errors.As(err, &ae) || ae.Code != apperror.ErrUnauthorized {
		t.Fatalf("expected UNAUTHORIZED, got %v", err)
	}
}

func TestRegister_DoesNotAutoLogin(t *testing.T) {
	var storedVerificationToken bool
	repo := happyRepo()
	repo.storeEmailVerificationTokenFn = func(_ context.Context, _, _, _ string, _ time.Time) error {
		storedVerificationToken = true
		return nil
	}

	svc := auth.NewService(repo, testCfg())
	resp, err := svc.Register(context.Background(), auth.RegisterRequest{
		Email: "new@example.com", Password: "Secret123", Username: "newuser",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	// No session is established on register — tokens must be empty.
	if resp.Token != "" {
		t.Error("expected empty access token (no auto-login on register)")
	}
	if resp.RefreshToken != "" {
		t.Error("expected empty refresh token (no auto-login on register)")
	}
	// But the user is returned and a verification email/token is issued.
	if resp.User.Email != "user@example.com" {
		t.Errorf("expected user in response, got %+v", resp.User)
	}
	if !storedVerificationToken {
		t.Error("expected a verification token to be stored on register")
	}
}

func TestRefreshToken_ReuseDetection(t *testing.T) {
	var revokedAll bool
	repo := happyRepo()
	// Simulate token already consumed — atomic delete finds no row.
	repo.deleteRefreshTokenReturningFn = func(_ context.Context, _ string) (auth.RefreshToken, error) {
		return auth.RefreshToken{}, apperror.New(http.StatusUnauthorized, apperror.ErrInvalidToken, "invalid or expired refresh token")
	}
	repo.deleteAllRefreshTokensFn = func(_ context.Context, _ string) error {
		revokedAll = true
		return nil
	}

	svc := auth.NewService(repo, testCfg())
	_, err := svc.RefreshToken(context.Background(), "validrawtoken")

	if err == nil {
		t.Fatal("expected error for reused token, got nil")
	}
	var ae *apperror.AppError
	if !errors.As(err, &ae) || ae.Code != apperror.ErrInvalidToken {
		t.Fatalf("expected INVALID_TOKEN, got %v", err)
	}
	// With atomic rotation, when the DELETE finds no row we return early — no separate revoke-all.
	if revokedAll {
		t.Error("revoke-all should not be called when token is simply not found")
	}
}

func TestRefreshToken_TamperedToken(t *testing.T) {
	var revokedAll bool
	repo := happyRepo()
	// Return a valid row but with a hash that doesn't match "tampered".
	repo.deleteRefreshTokenReturningFn = func(_ context.Context, _ string) (auth.RefreshToken, error) {
		hash, _ := hashutil.Hash("original-token")
		return auth.RefreshToken{UserID: "usr_abc123", TokenHash: hash, ExpiresAt: time.Now().Add(time.Hour)}, nil
	}
	repo.deleteAllRefreshTokensFn = func(_ context.Context, _ string) error {
		revokedAll = true
		return nil
	}

	svc := auth.NewService(repo, testCfg())
	_, err := svc.RefreshToken(context.Background(), "tampered-token")

	if err == nil {
		t.Fatal("expected error for tampered token, got nil")
	}
	var ae *apperror.AppError
	if !errors.As(err, &ae) || ae.Code != apperror.ErrInvalidToken {
		t.Fatalf("expected INVALID_TOKEN, got %v", err)
	}
	if !revokedAll {
		t.Error("expected all tokens to be revoked when token is tampered")
	}
}

func TestForgotPassword_AlwaysSucceeds(t *testing.T) {
	repo := happyRepo()
	// Unknown email — should still return nil.
	repo.getUserByEmailFn = func(_ context.Context, _ string) (auth.User, error) {
		return auth.User{}, apperror.New(http.StatusNotFound, apperror.ErrUserNotFound, "not found")
	}

	svc := auth.NewService(repo, testCfg())
	err := svc.ForgotPassword(context.Background(), "nobody@example.com")
	if err != nil {
		t.Fatalf("ForgotPassword() should always return nil, got %v", err)
	}
}

func TestResetPassword(t *testing.T) {
	usedAt := time.Now().Add(-time.Minute)
	expiredAt := time.Now().Add(-time.Hour)

	tests := []struct {
		name    string
		req     auth.ResetPasswordRequest
		repoFn  func(*mockRepo)
		wantErr string
	}{
		{
			name:    "passwords mismatch",
			req:     auth.ResetPasswordRequest{Token: "tok", NewPassword: "Secret123", ConfirmPassword: "Different1"},
			wantErr: apperror.ErrInvalidInput,
		},
		{
			name:    "weak new password",
			req:     auth.ResetPasswordRequest{Token: "tok", NewPassword: "weak", ConfirmPassword: "weak"},
			wantErr: apperror.ErrWeakPassword,
		},
		{
			name: "token already used",
			req:  auth.ResetPasswordRequest{Token: "tok", NewPassword: "NewPass123", ConfirmPassword: "NewPass123"},
			repoFn: func(m *mockRepo) {
				m.getPasswordResetTokenByFpFn = func(_ context.Context, _ string) (auth.PasswordResetToken, error) {
					hash, _ := hashutil.Hash("tok")
					return auth.PasswordResetToken{
						UserID:    "usr_abc",
						TokenHash: hash,
						ExpiresAt: time.Now().Add(time.Hour),
						UsedAt:    &usedAt,
					}, nil
				}
			},
			wantErr: apperror.ErrInvalidToken,
		},
		{
			name: "token expired",
			req:  auth.ResetPasswordRequest{Token: "tok", NewPassword: "NewPass123", ConfirmPassword: "NewPass123"},
			repoFn: func(m *mockRepo) {
				m.getPasswordResetTokenByFpFn = func(_ context.Context, _ string) (auth.PasswordResetToken, error) {
					hash, _ := hashutil.Hash("tok")
					return auth.PasswordResetToken{
						UserID:    "usr_abc",
						TokenHash: hash,
						ExpiresAt: expiredAt,
					}, nil
				}
			},
			wantErr: apperror.ErrInvalidToken,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := happyRepo()
			if tt.repoFn != nil {
				tt.repoFn(repo)
			}
			svc := auth.NewService(repo, testCfg())

			err := svc.ResetPassword(context.Background(), tt.req)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error %q, got nil", tt.wantErr)
				}
				var ae *apperror.AppError
				if !errors.As(err, &ae) || ae.Code != tt.wantErr {
					t.Fatalf("expected error code %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// secret123Hash is the bcrypt hash of "Secret123", computed once for the whole
// package. bcrypt at cost 12 is deliberately slow, so hashing per-subtest would
// add seconds to the already bcrypt-heavy auth suite (which runs under -race in
// CI). Compute it a single time and reuse.
var secret123Hash = func() string {
	h, err := hashutil.Hash("Secret123")
	if err != nil {
		panic(err)
	}
	return h
}()

// userWithPasswordRepo returns a happyRepo whose GetUserByID yields a user whose
// PasswordHash is bcrypt("Secret123") — so ChangePassword's current-password
// check can succeed.
func userWithPasswordRepo() *mockRepo {
	repo := happyRepo()
	repo.getUserByIDFn = func(_ context.Context, _ string) (auth.User, error) {
		u := fixedUser()
		u.PasswordHash = secret123Hash
		return u, nil
	}
	return repo
}

func TestChangePassword(t *testing.T) {
	tests := []struct {
		name    string
		req     auth.ChangePasswordRequest
		wantErr string
	}{
		{
			name: "success",
			req:  auth.ChangePasswordRequest{CurrentPassword: "Secret123", NewPassword: "NewPass123", ConfirmPassword: "NewPass123"},
		},
		{
			name:    "wrong current password",
			req:     auth.ChangePasswordRequest{CurrentPassword: "WrongPass1", NewPassword: "NewPass123", ConfirmPassword: "NewPass123"},
			wantErr: apperror.ErrUnauthorized,
		},
		{
			name:    "confirm mismatch",
			req:     auth.ChangePasswordRequest{CurrentPassword: "Secret123", NewPassword: "NewPass123", ConfirmPassword: "Different1"},
			wantErr: apperror.ErrInvalidInput,
		},
		{
			name:    "weak new password — too short",
			req:     auth.ChangePasswordRequest{CurrentPassword: "Secret123", NewPassword: "Aa1", ConfirmPassword: "Aa1"},
			wantErr: apperror.ErrWeakPassword,
		},
		{
			name:    "weak new password — no uppercase",
			req:     auth.ChangePasswordRequest{CurrentPassword: "Secret123", NewPassword: "lowercase1", ConfirmPassword: "lowercase1"},
			wantErr: apperror.ErrWeakPassword,
		},
		{
			name:    "weak new password — no lowercase",
			req:     auth.ChangePasswordRequest{CurrentPassword: "Secret123", NewPassword: "UPPERCASE1", ConfirmPassword: "UPPERCASE1"},
			wantErr: apperror.ErrWeakPassword,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := userWithPasswordRepo()
			svc := auth.NewService(repo, testCfg())

			resp, err := svc.ChangePassword(context.Background(), "usr_abc123", tt.req)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error %q, got nil", tt.wantErr)
				}
				var ae *apperror.AppError
				if !errors.As(err, &ae) || ae.Code != tt.wantErr {
					t.Fatalf("expected error code %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			// Success must issue a fresh token pair to the caller.
			if resp.Token == "" || resp.RefreshToken == "" {
				t.Fatalf("expected a fresh token pair, got token=%q refresh=%q", resp.Token, resp.RefreshToken)
			}
		})
	}
}

// TestChangePassword_Success_RevokesAllTokens asserts the change revokes every
// refresh token (kicking other devices) before issuing the caller's new pair.
func TestChangePassword_Success_RevokesAllTokens(t *testing.T) {
	repo := userWithPasswordRepo()
	var revokedFor string
	repo.deleteAllRefreshTokensFn = func(_ context.Context, userID string) error {
		revokedFor = userID
		return nil
	}
	svc := auth.NewService(repo, testCfg())

	_, err := svc.ChangePassword(context.Background(), "usr_abc123", auth.ChangePasswordRequest{
		CurrentPassword: "Secret123",
		NewPassword:     "NewPass123",
		ConfirmPassword: "NewPass123",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if revokedFor != "usr_abc123" {
		t.Fatalf("expected all refresh tokens revoked for usr_abc123, got %q", revokedFor)
	}
}

func TestLogoutAll(t *testing.T) {
	var deletedAll bool
	repo := happyRepo()
	repo.deleteAllRefreshTokensFn = func(_ context.Context, _ string) error {
		deletedAll = true
		return nil
	}

	svc := auth.NewService(repo, testCfg())
	err := svc.LogoutAll(context.Background(), "usr_abc")
	if err != nil {
		t.Fatalf("LogoutAll() error = %v", err)
	}
	if !deletedAll {
		t.Error("expected all tokens to be deleted")
	}
}

func TestLogout_Success(t *testing.T) {
	var deletedFP string
	repo := happyRepo()
	repo.deleteRefreshTokenFn = func(_ context.Context, fp string) (int64, error) {
		deletedFP = fp
		return 1, nil
	}

	svc := auth.NewService(repo, testCfg())
	if err := svc.Logout(context.Background(), "usr_abc", "somerawtoken"); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
	if deletedFP == "" {
		t.Error("expected DeleteRefreshToken to be called with a fingerprint")
	}
}

func TestLogout_EmptyToken_NoOp(t *testing.T) {
	called := false
	repo := happyRepo()
	repo.deleteRefreshTokenFn = func(_ context.Context, _ string) (int64, error) {
		called = true
		return 0, nil
	}

	svc := auth.NewService(repo, testCfg())
	if err := svc.Logout(context.Background(), "usr_abc", ""); err != nil {
		t.Fatalf("Logout() with empty token should return nil, got %v", err)
	}
	if called {
		t.Error("expected no DB call when refresh token is empty")
	}
}

func TestGetProfile_Success(t *testing.T) {
	repo := happyRepo()
	svc := auth.NewService(repo, testCfg())

	view, err := svc.GetProfile(context.Background(), "usr_abc123")
	if err != nil {
		t.Fatalf("GetProfile() error = %v", err)
	}
	if view.ID != "usr_abc123" {
		t.Errorf("ID = %q, want %q", view.ID, "usr_abc123")
	}
	if view.Email != "user@example.com" {
		t.Errorf("Email = %q, want %q", view.Email, "user@example.com")
	}
}

func TestGetProfile_UserNotFound(t *testing.T) {
	repo := happyRepo()
	repo.getUserByIDFn = func(_ context.Context, _ string) (auth.User, error) {
		return auth.User{}, apperror.New(http.StatusNotFound, apperror.ErrUserNotFound, "not found")
	}

	svc := auth.NewService(repo, testCfg())
	_, err := svc.GetProfile(context.Background(), "usr_missing")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var ae *apperror.AppError
	if !errors.As(err, &ae) || ae.Code != apperror.ErrUserNotFound {
		t.Fatalf("expected USER_NOT_FOUND, got %v", err)
	}
}

func TestUpdateMe_Success(t *testing.T) {
	firstName := "Jane"
	repo := happyRepo()
	repo.updateUserFn = func(_ context.Context, _ string, req auth.UpdateMeRequest) (auth.User, error) {
		u := fixedUser()
		if req.FirstName != nil {
			u.FirstName = *req.FirstName
		}
		return u, nil
	}

	svc := auth.NewService(repo, testCfg())
	view, err := svc.UpdateMe(context.Background(), "usr_abc123", auth.UpdateMeRequest{
		FirstName: &firstName,
	})
	if err != nil {
		t.Fatalf("UpdateMe() error = %v", err)
	}
	if view.FirstName != "Jane" {
		t.Errorf("FirstName = %q, want %q", view.FirstName, "Jane")
	}
}

func TestUpdateMe_InvalidEmail(t *testing.T) {
	bad := "notanemail"
	svc := auth.NewService(happyRepo(), testCfg())

	_, err := svc.UpdateMe(context.Background(), "usr_abc123", auth.UpdateMeRequest{
		Email: &bad,
	})
	if err == nil {
		t.Fatal("expected error for invalid email, got nil")
	}
	var ae *apperror.AppError
	if !errors.As(err, &ae) || ae.Code != apperror.ErrInvalidEmail {
		t.Fatalf("expected INVALID_EMAIL, got %v", err)
	}
}

func TestUpdateMe_Language(t *testing.T) {
	tests := []struct {
		name      string
		language  string
		wantError bool
	}{
		{name: "english", language: "en", wantError: false},
		{name: "hebrew", language: "he", wantError: false},
		{name: "russian", language: "ru", wantError: false},
		{name: "unsupported", language: "de", wantError: true},
		{name: "empty", language: "", wantError: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := happyRepo()
			repo.updateUserFn = func(_ context.Context, _ string, req auth.UpdateMeRequest) (auth.User, error) {
				u := fixedUser()
				if req.Language != nil {
					u.Language = *req.Language
				}
				return u, nil
			}
			svc := auth.NewService(repo, testCfg())

			lang := tc.language
			view, err := svc.UpdateMe(context.Background(), "usr_abc123", auth.UpdateMeRequest{Language: &lang})

			if tc.wantError {
				if err == nil {
					t.Fatalf("expected error for language %q, got nil", tc.language)
				}
				var ae *apperror.AppError
				if !errors.As(err, &ae) || ae.Code != apperror.ErrInvalidInput {
					t.Fatalf("expected INVALID_INPUT, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("UpdateMe() error = %v", err)
			}
			if view.Language != tc.language {
				t.Errorf("Language = %q, want %q", view.Language, tc.language)
			}
		})
	}
}

func TestDeleteMe_RevokesAllTokens(t *testing.T) {
	var softDeleted, revokedAll bool
	repo := happyRepo()
	repo.softDeleteUserFn = func(_ context.Context, _ string) error {
		softDeleted = true
		return nil
	}
	repo.deleteAllRefreshTokensFn = func(_ context.Context, _ string) error {
		revokedAll = true
		return nil
	}

	svc := auth.NewService(repo, testCfg())
	if err := svc.DeleteMe(context.Background(), "usr_abc123"); err != nil {
		t.Fatalf("DeleteMe() error = %v", err)
	}
	if !softDeleted {
		t.Error("expected SoftDeleteUser to be called")
	}
	if !revokedAll {
		t.Error("expected DeleteAllRefreshTokens to be called")
	}
}

func TestRefreshToken_Success(t *testing.T) {
	const rawToken = "validrawtoken"
	repo := happyRepo()
	repo.getRefreshTokenByFingerprintFn = func(_ context.Context, _ string) (auth.RefreshToken, error) {
		hash, _ := hashutil.Hash(rawToken)
		return auth.RefreshToken{
			UserID:    "usr_abc123",
			TokenHash: hash,
			ExpiresAt: time.Now().Add(time.Hour),
		}, nil
	}
	repo.deleteRefreshTokenFn = func(_ context.Context, _ string) (int64, error) {
		return 1, nil
	}

	svc := auth.NewService(repo, testCfg())
	resp, err := svc.RefreshToken(context.Background(), rawToken)
	if err != nil {
		t.Fatalf("RefreshToken() error = %v", err)
	}
	if resp.Token == "" {
		t.Error("expected non-empty JWT after refresh")
	}
	if resp.RefreshToken == "" {
		t.Error("expected new refresh token after rotation")
	}
}

func TestRefreshToken_EmptyToken(t *testing.T) {
	svc := auth.NewService(happyRepo(), testCfg())
	_, err := svc.RefreshToken(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty refresh token, got nil")
	}
	var ae *apperror.AppError
	if !errors.As(err, &ae) || ae.Code != apperror.ErrInvalidToken {
		t.Fatalf("expected INVALID_TOKEN, got %v", err)
	}
}

// TestRefreshToken_Expired guards the window where the hourly GC hasn't yet
// purged an expired refresh token: the row still exists and its hash matches,
// but its ExpiresAt is in the past, so refresh must reject it rather than mint a
// fresh access token from a stale session.
func TestRefreshToken_Expired(t *testing.T) {
	const rawToken = "validrawtoken"
	repo := happyRepo()
	repo.deleteRefreshTokenReturningFn = func(_ context.Context, _ string) (auth.RefreshToken, error) {
		hash, _ := hashutil.Hash(rawToken)
		return auth.RefreshToken{
			UserID:    "usr_abc123",
			TokenHash: hash,
			ExpiresAt: time.Now().Add(-time.Minute), // expired
		}, nil
	}

	svc := auth.NewService(repo, testCfg())
	_, err := svc.RefreshToken(context.Background(), rawToken)
	if err == nil {
		t.Fatal("expected error for expired refresh token, got nil")
	}
	var ae *apperror.AppError
	if !errors.As(err, &ae) || ae.Code != apperror.ErrInvalidToken {
		t.Fatalf("expected INVALID_TOKEN, got %v", err)
	}
}

func TestVerifyEmail_EmptyToken(t *testing.T) {
	svc := auth.NewService(happyRepo(), testCfg())
	err := svc.VerifyEmail(context.Background(), auth.VerifyEmailRequest{Token: ""})
	var ae *apperror.AppError
	if !errors.As(err, &ae) || ae.Code != apperror.ErrInvalidToken {
		t.Fatalf("expected INVALID_TOKEN, got %v", err)
	}
}

func TestVerifyEmail_Success(t *testing.T) {
	repo := happyRepo()
	rawToken := "verify-raw-token"
	hash, _ := hashutil.Hash(rawToken)
	var markedVerified, markedUsed bool
	repo.getEmailVerificationTokenByFpFn = func(_ context.Context, _ string) (auth.EmailVerificationToken, error) {
		return auth.EmailVerificationToken{UserID: "usr_abc123", TokenHash: hash, ExpiresAt: time.Now().Add(time.Hour)}, nil
	}
	repo.markEmailVerifiedFn = func(_ context.Context, _ string) error { markedVerified = true; return nil }
	repo.markEmailVerificationTokenUsedFn = func(_ context.Context, _ string) error { markedUsed = true; return nil }

	svc := auth.NewService(repo, testCfg())
	if err := svc.VerifyEmail(context.Background(), auth.VerifyEmailRequest{Token: rawToken}); err != nil {
		t.Fatalf("VerifyEmail() error = %v", err)
	}
	if !markedVerified {
		t.Error("expected MarkEmailVerified to be called")
	}
	if !markedUsed {
		t.Error("expected MarkEmailVerificationTokenUsed to be called")
	}
}

func TestVerifyEmail_TamperedToken(t *testing.T) {
	repo := happyRepo()
	// Stored hash is for a different token → bcrypt compare fails.
	otherHash, _ := hashutil.Hash("a-different-token")
	repo.getEmailVerificationTokenByFpFn = func(_ context.Context, _ string) (auth.EmailVerificationToken, error) {
		return auth.EmailVerificationToken{UserID: "usr_abc123", TokenHash: otherHash, ExpiresAt: time.Now().Add(time.Hour)}, nil
	}

	svc := auth.NewService(repo, testCfg())
	err := svc.VerifyEmail(context.Background(), auth.VerifyEmailRequest{Token: "verify-raw-token"})
	var ae *apperror.AppError
	if !errors.As(err, &ae) || ae.Code != apperror.ErrInvalidToken {
		t.Fatalf("expected INVALID_TOKEN, got %v", err)
	}
}
