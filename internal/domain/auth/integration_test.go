//go:build integration

package auth_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/internal/config"
	"github.com/nicoflow/nicoflow-api/internal/domain/auth"
	"github.com/nicoflow/nicoflow-api/internal/testutil"
	"github.com/nicoflow/nicoflow-api/pkg/hashutil"
	"github.com/nicoflow/nicoflow-api/pkg/jwtutil"
)

// authTestEmailDomain scopes every cleanup: all auth integration users use
// @example.com emails. Deleting by this domain (never a blanket DELETE FROM
// users) keeps the suite from wiping rows other packages seed in the shared
// test DB while running in parallel — see NIC-1608.
const authTestEmailDomain = "@example.com"

// cleanAuthTestData removes only auth's own users and their child rows,
// children first to respect FK constraints. Scoped by email domain so it never
// touches another package's data.
func cleanAuthTestData(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	const userScope = ` WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%` + authTestEmailDomain + `')`
	queries := []string{
		`DELETE FROM email_verification_tokens` + userScope,
		`DELETE FROM password_reset_tokens` + userScope,
		`DELETE FROM refresh_tokens` + userScope,
		`DELETE FROM users WHERE email LIKE '%` + authTestEmailDomain + `'`,
	}
	for _, q := range queries {
		if _, err := pool.Exec(context.Background(), q); err != nil {
			t.Fatalf("cleanAuthTestData: %v", err)
		}
	}
}

// integrationSvc returns a real Service backed by the test DB and a cleanup func.
func integrationSvc(t *testing.T) (auth.Service, *pgxpool.Pool) {
	t.Helper()
	pool := testutil.NewTestDB(t)
	cleanAuthTestData(t, pool)
	t.Cleanup(func() { cleanAuthTestData(t, pool) })
	cfg := config.Config{
		JWTSecret:          "integration-test-secret-32-bytes!!",
		JWTExpiry:          15 * time.Minute,
		RefreshTokenExpiry: 7 * 24 * time.Hour,
		// SMTPDsn intentionally empty — email send is skipped in tests
	}
	return auth.NewService(auth.NewRepository(pool), cfg), pool
}

// mustRegister registers a user and then logs in to obtain a session. Register
// no longer issues tokens (verification-before-login), so callers that need a
// refresh/access token go through login here. Verification is not required by
// the default integration config, so an unverified user can still log in.
func mustRegister(t *testing.T, svc auth.Service) auth.AuthResponse {
	t.Helper()
	if _, err := svc.Register(context.Background(), auth.RegisterRequest{
		Email:    "integration@example.com",
		Password: "Integrate1",
		Username: "integuser",
	}); err != nil {
		t.Fatalf("mustRegister: register: %v", err)
	}
	resp, err := svc.Login(context.Background(), auth.LoginRequest{
		Identifier: "integration@example.com",
		Password:   "Integrate1",
		Remember:   true,
	})
	if err != nil {
		t.Fatalf("mustRegister: login: %v", err)
	}
	return resp
}

// assertErrCode fatals if err is nil or does not carry the expected apperror code.
func assertErrCode(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error with code %q, got nil", code)
	}
	var ae *apperror.AppError
	if !errors.As(err, &ae) {
		t.Fatalf("expected *apperror.AppError, got %T: %v", err, err)
	}
	if ae.Code != code {
		t.Fatalf("expected error code %q, got %q (message: %s)", code, ae.Code, ae.Message)
	}
}

// insertResetToken writes a valid reset token directly into the DB and returns the raw token.
// This lets us test the full reset flow without needing a live SMTP server.
func insertResetToken(t *testing.T, pool *pgxpool.Pool, email string) string {
	t.Helper()

	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("insertResetToken: rand: %v", err)
	}
	rawToken := hex.EncodeToString(b)

	tokenHash, err := hashutil.Hash(rawToken)
	if err != nil {
		t.Fatalf("insertResetToken: hash: %v", err)
	}

	sum := sha256.Sum256([]byte(rawToken))
	fp := hex.EncodeToString(sum[:])
	expiresAt := time.Now().Add(time.Hour)

	_, err = pool.Exec(context.Background(), `
		INSERT INTO password_reset_tokens (id, user_id, token_hash, token_fingerprint, expires_at)
		SELECT gen_random_uuid()::text, u.id, $1, $2, $3
		FROM users u WHERE u.email = $4 AND u.deleted_at IS NULL`,
		tokenHash, fp, expiresAt, email,
	)
	if err != nil {
		t.Fatalf("insertResetToken: exec: %v", err)
	}
	return rawToken
}

// ── Register ──────────────────────────────────────────────────────────────────

func TestIntegration_Register(t *testing.T) {
	svc, _ := integrationSvc(t)

	resp, err := svc.Register(context.Background(), auth.RegisterRequest{
		Email:    "integration@example.com",
		Password: "Integrate1",
		Username: "integuser",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	// Register does not auto-login — user is returned, but no tokens.
	if resp.Token != "" || resp.RefreshToken != "" {
		t.Error("expected no tokens on register (verification required before login)")
	}
	if resp.User.Email != "integration@example.com" {
		t.Errorf("User.Email = %q, want %q", resp.User.Email, "integration@example.com")
	}

	// The user persists and can then log in (default config does not require
	// verification), yielding a valid JWT.
	login, err := svc.Login(context.Background(), auth.LoginRequest{
		Identifier: "integration@example.com",
		Password:   "Integrate1",
	})
	if err != nil {
		t.Fatalf("Login() after register error = %v", err)
	}
	claims, err := jwtutil.Parse(login.Token, "integration-test-secret-32-bytes!!")
	if err != nil {
		t.Fatalf("JWT parse error: %v", err)
	}
	if claims.Subject == "" {
		t.Error("expected non-empty JWT subject (userID)")
	}
}

func TestIntegration_Login_EmailNotVerified(t *testing.T) {
	pool := testutil.NewTestDB(t)
	cleanAuthTestData(t, pool)
	t.Cleanup(func() { cleanAuthTestData(t, pool) })
	cfg := config.Config{
		JWTSecret:                "integration-test-secret-32-bytes!!",
		JWTExpiry:                15 * time.Minute,
		RefreshTokenExpiry:       7 * 24 * time.Hour,
		RequireEmailVerification: true,
	}
	svc := auth.NewService(auth.NewRepository(pool), cfg)

	if _, err := svc.Register(context.Background(), auth.RegisterRequest{
		Email:    "unverified@example.com",
		Password: "Integrate1",
		Username: "unverifieduser",
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	// Unverified user is blocked from logging in when the gate is on.
	_, err := svc.Login(context.Background(), auth.LoginRequest{
		Identifier: "unverified@example.com",
		Password:   "Integrate1",
	})
	assertErrCode(t, err, apperror.ErrEmailNotVerified)

	// Mark verified, then login succeeds.
	if _, err := pool.Exec(context.Background(),
		`UPDATE users SET email_verified = true WHERE email = $1`, "unverified@example.com"); err != nil {
		t.Fatalf("mark verified: %v", err)
	}
	if _, err := svc.Login(context.Background(), auth.LoginRequest{
		Identifier: "unverified@example.com",
		Password:   "Integrate1",
	}); err != nil {
		t.Fatalf("Login() after verification error = %v", err)
	}
}

func TestIntegration_Register_DuplicateEmail(t *testing.T) {
	svc, _ := integrationSvc(t)
	mustRegister(t, svc)

	_, err := svc.Register(context.Background(), auth.RegisterRequest{
		Email:    "integration@example.com",
		Password: "Integrate1",
		Username: "otheruser",
	})
	assertErrCode(t, err, apperror.ErrEmailAlreadyExists)
}

func TestIntegration_Register_DuplicateUsername(t *testing.T) {
	svc, _ := integrationSvc(t)
	mustRegister(t, svc)

	// Same username, different email → must surface USERNAME_ALREADY_EXISTS,
	// not the email conflict code.
	_, err := svc.Register(context.Background(), auth.RegisterRequest{
		Email:    "different@example.com",
		Password: "Integrate1",
		Username: "integuser",
	})
	assertErrCode(t, err, apperror.ErrUsernameAlreadyExists)
}

func TestIntegration_Register_WeakPassword_NoUppercase(t *testing.T) {
	svc, _ := integrationSvc(t)

	_, err := svc.Register(context.Background(), auth.RegisterRequest{
		Email:    "integration@example.com",
		Password: "integrate1", // no uppercase
		Username: "integuser",
	})
	assertErrCode(t, err, apperror.ErrWeakPassword)
}

// ── Login ─────────────────────────────────────────────────────────────────────

func TestIntegration_Login_ByEmail(t *testing.T) {
	svc, _ := integrationSvc(t)
	mustRegister(t, svc)

	resp, err := svc.Login(context.Background(), auth.LoginRequest{
		Identifier: "integration@example.com",
		Password:   "Integrate1",
	})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if resp.Token == "" {
		t.Error("expected non-empty JWT")
	}
}

func TestIntegration_Login_ByUsername(t *testing.T) {
	svc, _ := integrationSvc(t)
	mustRegister(t, svc)

	resp, err := svc.Login(context.Background(), auth.LoginRequest{
		Identifier: "integuser",
		Password:   "Integrate1",
	})
	if err != nil {
		t.Fatalf("Login() by username error = %v", err)
	}
	if resp.Token == "" {
		t.Error("expected non-empty JWT")
	}
	if resp.User.Email != "integration@example.com" {
		t.Errorf("User.Email = %q, want %q", resp.User.Email, "integration@example.com")
	}
}

func TestIntegration_Login_LegacyEmailField(t *testing.T) {
	svc, _ := integrationSvc(t)
	mustRegister(t, svc)

	// Older clients still send `email` instead of `identifier`.
	resp, err := svc.Login(context.Background(), auth.LoginRequest{
		Email:    "integration@example.com",
		Password: "Integrate1",
	})
	if err != nil {
		t.Fatalf("Login() legacy email field error = %v", err)
	}
	if resp.Token == "" {
		t.Error("expected non-empty JWT")
	}
}

func TestIntegration_Login_WrongPassword(t *testing.T) {
	svc, _ := integrationSvc(t)
	mustRegister(t, svc)

	_, err := svc.Login(context.Background(), auth.LoginRequest{
		Identifier: "integration@example.com",
		Password:   "WrongPass1",
	})
	assertErrCode(t, err, apperror.ErrUnauthorized)
}

func TestIntegration_Login_NonExistentIdentifier(t *testing.T) {
	svc, _ := integrationSvc(t)

	_, err := svc.Login(context.Background(), auth.LoginRequest{
		Identifier: "nobody@example.com",
		Password:   "Integrate1",
	})
	// Must return same error code as wrong password — no enumeration.
	assertErrCode(t, err, apperror.ErrUnauthorized)
}

// ── Refresh token ─────────────────────────────────────────────────────────────

func TestIntegration_RefreshToken_Rotation(t *testing.T) {
	svc, _ := integrationSvc(t)
	reg := mustRegister(t, svc)

	resp, err := svc.RefreshToken(context.Background(), reg.RefreshToken)
	if err != nil {
		t.Fatalf("RefreshToken() error = %v", err)
	}
	if resp.Token == "" {
		t.Error("expected new JWT after refresh")
	}
	if resp.RefreshToken == reg.RefreshToken {
		t.Error("refresh token must rotate on each use")
	}

	// Old token must now be rejected.
	_, err = svc.RefreshToken(context.Background(), reg.RefreshToken)
	if err == nil {
		t.Fatal("expected error reusing old refresh token, got nil")
	}
}

func TestIntegration_RefreshToken_ConsumedTokenRejected(t *testing.T) {
	svc, _ := integrationSvc(t)
	reg := mustRegister(t, svc)

	// Rotate once — original token is consumed.
	_, err := svc.RefreshToken(context.Background(), reg.RefreshToken)
	if err != nil {
		t.Fatalf("first refresh: %v", err)
	}

	// Presenting the consumed original token must be rejected.
	_, err = svc.RefreshToken(context.Background(), reg.RefreshToken)
	assertErrCode(t, err, apperror.ErrInvalidToken)
}

// ── Forgot password ───────────────────────────────────────────────────────────

func TestIntegration_ForgotPassword_StoresToken(t *testing.T) {
	svc, pool := integrationSvc(t)
	mustRegister(t, svc)

	// SMTPDsn is empty so email is skipped — token must still be written to DB.
	if err := svc.ForgotPassword(context.Background(), "integration@example.com"); err != nil {
		t.Fatalf("ForgotPassword() error = %v", err)
	}

	var count int
	err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM password_reset_tokens WHERE used_at IS NULL`,
	).Scan(&count)
	if err != nil {
		t.Fatalf("count reset tokens: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 active reset token in DB, got %d", count)
	}
}

func TestIntegration_ForgotPassword_UnknownEmail(t *testing.T) {
	svc, _ := integrationSvc(t)

	// Must always return nil — no enumeration.
	if err := svc.ForgotPassword(context.Background(), "ghost@example.com"); err != nil {
		t.Fatalf("ForgotPassword() with unknown email must return nil, got %v", err)
	}
}

func TestIntegration_ForgotPassword_ReplacesExistingToken(t *testing.T) {
	svc, pool := integrationSvc(t)
	mustRegister(t, svc)

	// Request twice — only one active token should exist.
	_ = svc.ForgotPassword(context.Background(), "integration@example.com")
	_ = svc.ForgotPassword(context.Background(), "integration@example.com")

	var count int
	err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM password_reset_tokens WHERE used_at IS NULL`,
	).Scan(&count)
	if err != nil {
		t.Fatalf("count reset tokens: %v", err)
	}
	if count != 1 {
		t.Errorf("expected exactly 1 active reset token after double request, got %d", count)
	}
}

// ── Reset password ────────────────────────────────────────────────────────────

func TestIntegration_ResetPassword(t *testing.T) {
	svc, pool := integrationSvc(t)
	mustRegister(t, svc)

	rawToken := insertResetToken(t, pool, "integration@example.com")

	if err := svc.ResetPassword(context.Background(), auth.ResetPasswordRequest{
		Token:           rawToken,
		NewPassword:     "NewIntegrate1",
		ConfirmPassword: "NewIntegrate1",
	}); err != nil {
		t.Fatalf("ResetPassword() error = %v", err)
	}

	// Old password must be rejected.
	_, err := svc.Login(context.Background(), auth.LoginRequest{
		Identifier: "integration@example.com",
		Password:   "Integrate1",
	})
	assertErrCode(t, err, apperror.ErrUnauthorized)

	// New password must work.
	_, err = svc.Login(context.Background(), auth.LoginRequest{
		Identifier: "integration@example.com",
		Password:   "NewIntegrate1",
	})
	if err != nil {
		t.Fatalf("login with new password failed: %v", err)
	}
}

func TestIntegration_ResetPassword_TokenAlreadyUsed(t *testing.T) {
	svc, pool := integrationSvc(t)
	mustRegister(t, svc)

	rawToken := insertResetToken(t, pool, "integration@example.com")
	req := auth.ResetPasswordRequest{
		Token:           rawToken,
		NewPassword:     "NewIntegrate1",
		ConfirmPassword: "NewIntegrate1",
	}

	if err := svc.ResetPassword(context.Background(), req); err != nil {
		t.Fatalf("first reset: %v", err)
	}

	// Second attempt with same token must fail.
	assertErrCode(t, svc.ResetPassword(context.Background(), req), apperror.ErrInvalidToken)
}

func TestIntegration_ResetPassword_RevokesAllSessions(t *testing.T) {
	svc, pool := integrationSvc(t)
	reg := mustRegister(t, svc)

	rawToken := insertResetToken(t, pool, "integration@example.com")

	if err := svc.ResetPassword(context.Background(), auth.ResetPasswordRequest{
		Token:           rawToken,
		NewPassword:     "NewIntegrate1",
		ConfirmPassword: "NewIntegrate1",
	}); err != nil {
		t.Fatalf("ResetPassword() error = %v", err)
	}

	// Pre-existing refresh token must be revoked.
	_, err := svc.RefreshToken(context.Background(), reg.RefreshToken)
	if err == nil {
		t.Fatal("expected pre-reset refresh token to be revoked")
	}
}

// ── Logout ────────────────────────────────────────────────────────────────────

func TestIntegration_Logout(t *testing.T) {
	svc, _ := integrationSvc(t)
	reg := mustRegister(t, svc)

	if err := svc.Logout(context.Background(), reg.User.ID, reg.RefreshToken); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}

	_, err := svc.RefreshToken(context.Background(), reg.RefreshToken)
	if err == nil {
		t.Fatal("expected refresh to fail after logout")
	}
}

func TestIntegration_LogoutAll(t *testing.T) {
	svc, _ := integrationSvc(t)
	reg := mustRegister(t, svc)

	// Create a second session.
	reg2, err := svc.Login(context.Background(), auth.LoginRequest{
		Identifier: "integration@example.com",
		Password:   "Integrate1",
	})
	if err != nil {
		t.Fatalf("second login: %v", err)
	}

	if err := svc.LogoutAll(context.Background(), reg.User.ID); err != nil {
		t.Fatalf("LogoutAll() error = %v", err)
	}

	_, err = svc.RefreshToken(context.Background(), reg.RefreshToken)
	if err == nil {
		t.Fatal("session 1 should be revoked after LogoutAll")
	}
	_, err = svc.RefreshToken(context.Background(), reg2.RefreshToken)
	if err == nil {
		t.Fatal("session 2 should be revoked after LogoutAll")
	}
}

// ── Soft delete ───────────────────────────────────────────────────────────────

func TestIntegration_DeleteMe(t *testing.T) {
	svc, _ := integrationSvc(t)
	reg := mustRegister(t, svc)

	if err := svc.DeleteMe(context.Background(), reg.User.ID); err != nil {
		t.Fatalf("DeleteMe() error = %v", err)
	}

	// Refresh token must be revoked.
	_, err := svc.RefreshToken(context.Background(), reg.RefreshToken)
	if err == nil {
		t.Fatal("expected refresh to fail after soft delete")
	}

	// Login must fail (deleted_at IS NULL filter).
	_, err = svc.Login(context.Background(), auth.LoginRequest{
		Identifier: "integration@example.com",
		Password:   "Integrate1",
	})
	assertErrCode(t, err, apperror.ErrUnauthorized)
}

// ── Email verification ──────────────────────────────────────────────────────

// insertVerificationToken writes a valid email-verification token directly into
// the DB and returns the raw token (no SMTP needed).
func insertVerificationToken(t *testing.T, pool *pgxpool.Pool, email string) string {
	t.Helper()

	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("insertVerificationToken: rand: %v", err)
	}
	rawToken := hex.EncodeToString(b)

	tokenHash, err := hashutil.Hash(rawToken)
	if err != nil {
		t.Fatalf("insertVerificationToken: hash: %v", err)
	}

	sum := sha256.Sum256([]byte(rawToken))
	fp := hex.EncodeToString(sum[:])
	expiresAt := time.Now().Add(24 * time.Hour)

	_, err = pool.Exec(context.Background(), `
		INSERT INTO email_verification_tokens (id, user_id, token_hash, token_fingerprint, expires_at)
		SELECT gen_random_uuid()::text, u.id, $1, $2, $3
		FROM users u WHERE u.email = $4 AND u.deleted_at IS NULL`,
		tokenHash, fp, expiresAt, email,
	)
	if err != nil {
		t.Fatalf("insertVerificationToken: exec: %v", err)
	}
	return rawToken
}

func TestIntegration_VerifyEmail(t *testing.T) {
	svc, pool := integrationSvc(t)
	mustRegister(t, svc)

	rawToken := insertVerificationToken(t, pool, "integration@example.com")

	if err := svc.VerifyEmail(context.Background(), auth.VerifyEmailRequest{Token: rawToken}); err != nil {
		t.Fatalf("VerifyEmail() error = %v", err)
	}

	// email_verified must now be true.
	var verified bool
	if err := pool.QueryRow(context.Background(),
		`SELECT email_verified FROM users WHERE email = $1`, "integration@example.com",
	).Scan(&verified); err != nil {
		t.Fatalf("query email_verified: %v", err)
	}
	if !verified {
		t.Error("expected email_verified = true after VerifyEmail")
	}

	// Token is single-use — second use must fail.
	err := svc.VerifyEmail(context.Background(), auth.VerifyEmailRequest{Token: rawToken})
	assertErrCode(t, err, apperror.ErrInvalidToken)
}

func TestIntegration_VerifyEmail_InvalidToken(t *testing.T) {
	svc, _ := integrationSvc(t)
	mustRegister(t, svc)

	err := svc.VerifyEmail(context.Background(), auth.VerifyEmailRequest{Token: "not-a-real-token"})
	assertErrCode(t, err, apperror.ErrInvalidToken)
}
