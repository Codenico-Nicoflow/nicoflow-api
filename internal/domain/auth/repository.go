package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
)

// Repository is the data-access interface for the auth domain.
// Defined here (at the consumer boundary) per architecture conventions.
type Repository interface {
	CreateUser(ctx context.Context, email, username, passwordHash string) (User, error)
	GetUserByEmail(ctx context.Context, email string) (User, error)
	GetUserByIdentifier(ctx context.Context, identifier string) (User, error)
	GetUserByID(ctx context.Context, userID string) (User, error)
	UpdateUser(ctx context.Context, userID string, req UpdateMeRequest) (User, error)
	SoftDeleteUser(ctx context.Context, userID string) error

	StoreRefreshToken(ctx context.Context, userID, tokenHash, tokenFingerprint string, expiresAt time.Time) error
	GetRefreshTokenByFingerprint(ctx context.Context, fingerprint string) (RefreshToken, error)
	DeleteRefreshToken(ctx context.Context, fingerprint string) (int64, error)
	DeleteRefreshTokenReturning(ctx context.Context, fingerprint string) (RefreshToken, error)
	DeleteAllRefreshTokens(ctx context.Context, userID string) error

	IncrementFailedLogin(ctx context.Context, userID string) error
	ResetFailedLogin(ctx context.Context, userID string) error

	StorePasswordResetToken(ctx context.Context, userID, tokenHash, tokenFingerprint string, expiresAt time.Time) error
	GetPasswordResetTokenByFingerprint(ctx context.Context, fingerprint string) (PasswordResetToken, error)
	MarkPasswordResetTokenUsed(ctx context.Context, fingerprint string) error
	UpdatePassword(ctx context.Context, userID, passwordHash string) error

	StoreEmailVerificationToken(ctx context.Context, userID, tokenHash, tokenFingerprint string, expiresAt time.Time) error
	GetEmailVerificationTokenByFingerprint(ctx context.Context, fingerprint string) (EmailVerificationToken, error)
	MarkEmailVerificationTokenUsed(ctx context.Context, fingerprint string) error
	MarkEmailVerified(ctx context.Context, userID string) error
}

type pgRepo struct {
	db *pgxpool.Pool
}

// NewRepository returns a postgres-backed Repository.
func NewRepository(db *pgxpool.Pool) Repository {
	return &pgRepo{db: db}
}

func (r *pgRepo) CreateUser(ctx context.Context, email, username, passwordHash string) (User, error) {
	id := uuid.New().String()
	var u User
	err := r.db.QueryRow(ctx, `
		INSERT INTO users (id, email, username, password_hash, theme, status, plan, timezone)
		VALUES (@id, @email, @username, @passwordHash, 'light', 'regular', 'free', 'UTC')
		RETURNING id, email, COALESCE(username,''), password_hash,
		          COALESCE(first_name,''), COALESCE(last_name,''),
		          theme, COALESCE(image_url,''), status, plan, timezone,
		          created_at, updated_at`,
		pgx.NamedArgs{
			"id":           id,
			"email":        email,
			"username":     username,
			"passwordHash": passwordHash,
		},
	).Scan(
		&u.ID, &u.Email, &u.Username, &u.PasswordHash,
		&u.FirstName, &u.LastName,
		&u.Theme, &u.ImageURL, &u.Status, &u.Plan, &u.Timezone,
		&u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		if name := uniqueViolationField(err); name != "" {
			// Match on the field in the constraint/index name rather than an exact
			// name, so renames stay correct: username is users_username_key (pre-021)
			// or users_username_active_uniq (021+); email is users_email_active_uniq.
			switch {
			case strings.Contains(name, "username"):
				return User{}, apperror.New(http.StatusConflict, apperror.ErrUsernameAlreadyExists, "username already taken")
			case strings.Contains(name, "email"):
				return User{}, apperror.New(http.StatusConflict, apperror.ErrEmailAlreadyExists, "email already in use")
			default:
				return User{}, apperror.New(http.StatusConflict, apperror.ErrConflict, "resource already exists")
			}
		}
		return User{}, fmt.Errorf("auth.CreateUser: %w", err)
	}
	return u, nil
}

// GetUserByIdentifier looks a user up by email when the identifier parses as an
// email address, otherwise by username. Used by login to accept either form.
func (r *pgRepo) GetUserByIdentifier(ctx context.Context, identifier string) (User, error) {
	if _, err := mail.ParseAddress(identifier); err == nil {
		return r.GetUserByEmail(ctx, identifier)
	}
	return r.getUserByUsername(ctx, identifier)
}

func (r *pgRepo) getUserByUsername(ctx context.Context, username string) (User, error) {
	var u User
	err := r.db.QueryRow(ctx, `
		SELECT id, email, COALESCE(username,''), password_hash,
		       COALESCE(first_name,''), COALESCE(last_name,''),
		       theme, COALESCE(image_url,''), status, plan, timezone,
		       email_verified, failed_login_count, locked_until,
		       created_at, updated_at
		FROM users
		WHERE username = @username AND deleted_at IS NULL`,
		pgx.NamedArgs{"username": username},
	).Scan(
		&u.ID, &u.Email, &u.Username, &u.PasswordHash,
		&u.FirstName, &u.LastName,
		&u.Theme, &u.ImageURL, &u.Status, &u.Plan, &u.Timezone,
		&u.EmailVerified, &u.FailedLoginCount, &u.LockedUntil,
		&u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, apperror.New(http.StatusNotFound, apperror.ErrUserNotFound, "user not found")
		}
		return User{}, fmt.Errorf("auth.getUserByUsername: %w", err)
	}
	return u, nil
}

func (r *pgRepo) GetUserByEmail(ctx context.Context, email string) (User, error) {
	var u User
	err := r.db.QueryRow(ctx, `
		SELECT id, email, COALESCE(username,''), password_hash,
		       COALESCE(first_name,''), COALESCE(last_name,''),
		       theme, COALESCE(image_url,''), status, plan, timezone,
		       email_verified, failed_login_count, locked_until,
		       created_at, updated_at
		FROM users
		WHERE email = @email AND deleted_at IS NULL`,
		pgx.NamedArgs{"email": email},
	).Scan(
		&u.ID, &u.Email, &u.Username, &u.PasswordHash,
		&u.FirstName, &u.LastName,
		&u.Theme, &u.ImageURL, &u.Status, &u.Plan, &u.Timezone,
		&u.EmailVerified, &u.FailedLoginCount, &u.LockedUntil,
		&u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, apperror.New(http.StatusNotFound, apperror.ErrUserNotFound, "user not found")
		}
		return User{}, fmt.Errorf("auth.GetUserByEmail: %w", err)
	}
	return u, nil
}

func (r *pgRepo) GetUserByID(ctx context.Context, userID string) (User, error) {
	var u User
	err := r.db.QueryRow(ctx, `
		SELECT id, email, COALESCE(username,''), password_hash,
		       COALESCE(first_name,''), COALESCE(last_name,''),
		       theme, COALESCE(image_url,''), status, plan, timezone,
		       email_verified,
		       created_at, updated_at
		FROM users
		WHERE id = @userID AND deleted_at IS NULL`,
		pgx.NamedArgs{"userID": userID},
	).Scan(
		&u.ID, &u.Email, &u.Username, &u.PasswordHash,
		&u.FirstName, &u.LastName,
		&u.Theme, &u.ImageURL, &u.Status, &u.Plan, &u.Timezone,
		&u.EmailVerified,
		&u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, apperror.New(http.StatusNotFound, apperror.ErrUserNotFound, "user not found")
		}
		return User{}, fmt.Errorf("auth.GetUserByID: %w", err)
	}
	return u, nil
}

func (r *pgRepo) UpdateUser(ctx context.Context, userID string, req UpdateMeRequest) (User, error) {
	var u User
	err := r.db.QueryRow(ctx, `
		UPDATE users SET
		  first_name  = COALESCE(@firstName,  first_name),
		  last_name   = COALESCE(@lastName,   last_name),
		  email       = COALESCE(@email,       email),
		  timezone    = COALESCE(@timezone,    timezone),
		  theme       = COALESCE(@theme,       theme),
		  updated_at  = NOW()
		WHERE id = @userID AND deleted_at IS NULL
		RETURNING id, email, COALESCE(username,''), password_hash,
		          COALESCE(first_name,''), COALESCE(last_name,''),
		          theme, COALESCE(image_url,''), status, plan, timezone,
		          created_at, updated_at`,
		pgx.NamedArgs{
			"firstName": req.FirstName,
			"lastName":  req.LastName,
			"email":     req.Email,
			"timezone":  req.Timezone,
			"theme":     req.Theme,
			"userID":    userID,
		},
	).Scan(
		&u.ID, &u.Email, &u.Username, &u.PasswordHash,
		&u.FirstName, &u.LastName,
		&u.Theme, &u.ImageURL, &u.Status, &u.Plan, &u.Timezone,
		&u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, apperror.New(http.StatusNotFound, apperror.ErrUserNotFound, "user not found")
		}
		if isUniqueViolation(err) {
			return User{}, apperror.New(http.StatusConflict, apperror.ErrConflict, "email already in use")
		}
		return User{}, fmt.Errorf("auth.UpdateUser: %w", err)
	}
	return u, nil
}

func (r *pgRepo) SoftDeleteUser(ctx context.Context, userID string) error {
	tag, err := r.db.Exec(ctx,
		`UPDATE users SET deleted_at = NOW() WHERE id = @userID AND deleted_at IS NULL`,
		pgx.NamedArgs{"userID": userID},
	)
	if err != nil {
		return fmt.Errorf("auth.SoftDeleteUser: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return apperror.New(http.StatusNotFound, apperror.ErrUserNotFound, "user not found")
	}
	return nil
}

func (r *pgRepo) StoreRefreshToken(ctx context.Context, userID, tokenHash, tokenFingerprint string, expiresAt time.Time) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO refresh_tokens (id, user_id, token_hash, token_fingerprint, expires_at)
		VALUES (@id, @userID, @tokenHash, @tokenFingerprint, @expiresAt)`,
		pgx.NamedArgs{
			"id":               uuid.New().String(),
			"userID":           userID,
			"tokenHash":        tokenHash,
			"tokenFingerprint": tokenFingerprint,
			"expiresAt":        expiresAt,
		},
	)
	if err != nil {
		return fmt.Errorf("auth.StoreRefreshToken: %w", err)
	}
	return nil
}

func (r *pgRepo) GetRefreshTokenByFingerprint(ctx context.Context, fingerprint string) (RefreshToken, error) {
	var rt RefreshToken
	err := r.db.QueryRow(ctx, `
		SELECT id, user_id, token_hash, token_fingerprint, expires_at, created_at
		FROM refresh_tokens
		WHERE token_fingerprint = @fingerprint AND expires_at > NOW()`,
		pgx.NamedArgs{"fingerprint": fingerprint},
	).Scan(&rt.ID, &rt.UserID, &rt.TokenHash, &rt.TokenFingerprint, &rt.ExpiresAt, &rt.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return RefreshToken{}, apperror.New(http.StatusUnauthorized, apperror.ErrInvalidToken, "invalid or expired refresh token")
		}
		return RefreshToken{}, fmt.Errorf("auth.GetRefreshTokenByFingerprint: %w", err)
	}
	return rt, nil
}

// DeleteRefreshToken deletes a single token by fingerprint and returns rows affected.
// Caller checks 0 rows to detect token reuse.
func (r *pgRepo) DeleteRefreshToken(ctx context.Context, fingerprint string) (int64, error) {
	tag, err := r.db.Exec(ctx,
		`DELETE FROM refresh_tokens WHERE token_fingerprint = @fingerprint`,
		pgx.NamedArgs{"fingerprint": fingerprint},
	)
	if err != nil {
		return 0, fmt.Errorf("auth.DeleteRefreshToken: %w", err)
	}
	return tag.RowsAffected(), nil
}

// DeleteRefreshTokenReturning atomically deletes the token and returns its data in one statement.
// If no row matches (already consumed or never existed) it returns ErrInvalidToken.
// This prevents a race where two concurrent retries both pass a prior SELECT+bcrypt check.
func (r *pgRepo) DeleteRefreshTokenReturning(ctx context.Context, fingerprint string) (RefreshToken, error) {
	var rt RefreshToken
	err := r.db.QueryRow(ctx, `
		DELETE FROM refresh_tokens
		WHERE token_fingerprint = @fingerprint
		RETURNING user_id, token_hash, expires_at`,
		pgx.NamedArgs{"fingerprint": fingerprint},
	).Scan(&rt.UserID, &rt.TokenHash, &rt.ExpiresAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return RefreshToken{}, apperror.New(http.StatusUnauthorized, apperror.ErrInvalidToken, "invalid or expired refresh token")
		}
		return RefreshToken{}, fmt.Errorf("auth.DeleteRefreshTokenReturning: %w", err)
	}
	return rt, nil
}

func (r *pgRepo) DeleteAllRefreshTokens(ctx context.Context, userID string) error {
	_, err := r.db.Exec(ctx,
		`DELETE FROM refresh_tokens WHERE user_id = @userID`,
		pgx.NamedArgs{"userID": userID},
	)
	if err != nil {
		return fmt.Errorf("auth.DeleteAllRefreshTokens: %w", err)
	}
	return nil
}

func (r *pgRepo) StorePasswordResetToken(ctx context.Context, userID, tokenHash, tokenFingerprint string, expiresAt time.Time) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("auth.StorePasswordResetToken begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// One active reset token per user at a time.
	_, err = tx.Exec(ctx,
		`DELETE FROM password_reset_tokens WHERE user_id = @userID AND used_at IS NULL`,
		pgx.NamedArgs{"userID": userID},
	)
	if err != nil {
		return fmt.Errorf("auth.StorePasswordResetToken cleanup: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO password_reset_tokens (id, user_id, token_hash, token_fingerprint, expires_at)
		VALUES (@id, @userID, @tokenHash, @tokenFingerprint, @expiresAt)`,
		pgx.NamedArgs{
			"id":               uuid.New().String(),
			"userID":           userID,
			"tokenHash":        tokenHash,
			"tokenFingerprint": tokenFingerprint,
			"expiresAt":        expiresAt,
		},
	)
	if err != nil {
		return fmt.Errorf("auth.StorePasswordResetToken: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("auth.StorePasswordResetToken commit: %w", err)
	}
	return nil
}

func (r *pgRepo) GetPasswordResetTokenByFingerprint(ctx context.Context, fingerprint string) (PasswordResetToken, error) {
	var prt PasswordResetToken
	err := r.db.QueryRow(ctx, `
		SELECT id, user_id, token_hash, token_fingerprint, expires_at, used_at, created_at
		FROM password_reset_tokens
		WHERE token_fingerprint = @fingerprint
		  AND expires_at > NOW()
		  AND used_at IS NULL`,
		pgx.NamedArgs{"fingerprint": fingerprint},
	).Scan(&prt.ID, &prt.UserID, &prt.TokenHash, &prt.TokenFingerprint, &prt.ExpiresAt, &prt.UsedAt, &prt.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return PasswordResetToken{}, apperror.New(http.StatusUnauthorized, apperror.ErrInvalidToken, "invalid or expired reset token")
		}
		return PasswordResetToken{}, fmt.Errorf("auth.GetPasswordResetToken: %w", err)
	}
	return prt, nil
}

func (r *pgRepo) MarkPasswordResetTokenUsed(ctx context.Context, fingerprint string) error {
	_, err := r.db.Exec(ctx,
		`UPDATE password_reset_tokens SET used_at = NOW() WHERE token_fingerprint = @fingerprint`,
		pgx.NamedArgs{"fingerprint": fingerprint},
	)
	if err != nil {
		return fmt.Errorf("auth.MarkPasswordResetTokenUsed: %w", err)
	}
	return nil
}

func (r *pgRepo) UpdatePassword(ctx context.Context, userID, passwordHash string) error {
	tag, err := r.db.Exec(ctx, `
		UPDATE users SET password_hash = @passwordHash, updated_at = NOW()
		WHERE id = @userID AND deleted_at IS NULL`,
		pgx.NamedArgs{"passwordHash": passwordHash, "userID": userID},
	)
	if err != nil {
		return fmt.Errorf("auth.UpdatePassword: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return apperror.New(http.StatusNotFound, apperror.ErrUserNotFound, "user not found")
	}
	return nil
}

func (r *pgRepo) StoreEmailVerificationToken(ctx context.Context, userID, tokenHash, tokenFingerprint string, expiresAt time.Time) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("auth.StoreEmailVerificationToken begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// One active verification token per user at a time.
	_, err = tx.Exec(ctx,
		`DELETE FROM email_verification_tokens WHERE user_id = @userID AND used_at IS NULL`,
		pgx.NamedArgs{"userID": userID},
	)
	if err != nil {
		return fmt.Errorf("auth.StoreEmailVerificationToken cleanup: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO email_verification_tokens (id, user_id, token_hash, token_fingerprint, expires_at)
		VALUES (@id, @userID, @tokenHash, @tokenFingerprint, @expiresAt)`,
		pgx.NamedArgs{
			"id":               uuid.New().String(),
			"userID":           userID,
			"tokenHash":        tokenHash,
			"tokenFingerprint": tokenFingerprint,
			"expiresAt":        expiresAt,
		},
	)
	if err != nil {
		return fmt.Errorf("auth.StoreEmailVerificationToken: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("auth.StoreEmailVerificationToken commit: %w", err)
	}
	return nil
}

func (r *pgRepo) GetEmailVerificationTokenByFingerprint(ctx context.Context, fingerprint string) (EmailVerificationToken, error) {
	var evt EmailVerificationToken
	err := r.db.QueryRow(ctx, `
		SELECT id, user_id, token_hash, token_fingerprint, expires_at, used_at, created_at
		FROM email_verification_tokens
		WHERE token_fingerprint = @fingerprint
		  AND expires_at > NOW()
		  AND used_at IS NULL`,
		pgx.NamedArgs{"fingerprint": fingerprint},
	).Scan(&evt.ID, &evt.UserID, &evt.TokenHash, &evt.TokenFingerprint, &evt.ExpiresAt, &evt.UsedAt, &evt.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return EmailVerificationToken{}, apperror.New(http.StatusUnauthorized, apperror.ErrInvalidToken, "invalid or expired verification token")
		}
		return EmailVerificationToken{}, fmt.Errorf("auth.GetEmailVerificationToken: %w", err)
	}
	return evt, nil
}

func (r *pgRepo) MarkEmailVerificationTokenUsed(ctx context.Context, fingerprint string) error {
	_, err := r.db.Exec(ctx,
		`UPDATE email_verification_tokens SET used_at = NOW() WHERE token_fingerprint = @fingerprint`,
		pgx.NamedArgs{"fingerprint": fingerprint},
	)
	if err != nil {
		return fmt.Errorf("auth.MarkEmailVerificationTokenUsed: %w", err)
	}
	return nil
}

func (r *pgRepo) MarkEmailVerified(ctx context.Context, userID string) error {
	tag, err := r.db.Exec(ctx,
		`UPDATE users SET email_verified = true, updated_at = NOW() WHERE id = @userID AND deleted_at IS NULL`,
		pgx.NamedArgs{"userID": userID},
	)
	if err != nil {
		return fmt.Errorf("auth.MarkEmailVerified: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return apperror.New(http.StatusNotFound, apperror.ErrUserNotFound, "user not found")
	}
	return nil
}

// IncrementFailedLogin bumps the failed_login_count and sets locked_until using
// exponential back-off: lock duration = 2^(count-1) minutes, capped at 60 minutes.
func (r *pgRepo) IncrementFailedLogin(ctx context.Context, userID string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE users
		SET failed_login_count = failed_login_count + 1,
		    locked_until = CASE
		        WHEN failed_login_count + 1 >= 5
		        THEN NOW() + (LEAST(POWER(2, failed_login_count - 3), 60) || ' minutes')::INTERVAL
		        ELSE NULL
		    END
		WHERE id = @userID AND deleted_at IS NULL`,
		pgx.NamedArgs{"userID": userID},
	)
	if err != nil {
		return fmt.Errorf("auth.IncrementFailedLogin: %w", err)
	}
	return nil
}

// ResetFailedLogin clears the failed login counter and lock after a successful login.
func (r *pgRepo) ResetFailedLogin(ctx context.Context, userID string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE users SET failed_login_count = 0, locked_until = NULL
		WHERE id = @userID AND deleted_at IS NULL`,
		pgx.NamedArgs{"userID": userID},
	)
	if err != nil {
		return fmt.Errorf("auth.ResetFailedLogin: %w", err)
	}
	return nil
}

// StartTokenGC launches a background goroutine that deletes expired refresh tokens
// and expired/used password reset tokens once per hour.
// Call it once at server startup; it runs until the process exits.
func StartTokenGC(ctx context.Context, db *pgxpool.Pool) {
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if _, err := db.Exec(ctx, `DELETE FROM refresh_tokens WHERE expires_at < NOW()`); err != nil {
					log.Error().Err(err).Msg("token GC: failed to purge expired refresh tokens")
				}
				if _, err := db.Exec(ctx, `DELETE FROM password_reset_tokens WHERE expires_at < NOW() OR used_at IS NOT NULL`); err != nil {
					log.Error().Err(err).Msg("token GC: failed to purge expired reset tokens")
				}
				if _, err := db.Exec(ctx, `DELETE FROM email_verification_tokens WHERE expires_at < NOW() OR used_at IS NOT NULL`); err != nil {
					log.Error().Err(err).Msg("token GC: failed to purge expired verification tokens")
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

// isUniqueViolation checks for PostgreSQL unique constraint violation (error code 23505).
func isUniqueViolation(err error) bool {
	type pgErr interface{ SQLState() string }
	var pg pgErr
	return errors.As(err, &pg) && pg.SQLState() == "23505"
}

// uniqueViolationField returns the constraint/index name of a PostgreSQL unique
// violation (23505), or "" if err is not a unique violation. Used to tell apart
// a duplicate email (users_email_active_uniq) from a duplicate username
// (users_username_key) so callers can return a specific error code.
func uniqueViolationField(err error) string {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return pg.ConstraintName
	}
	return ""
}
