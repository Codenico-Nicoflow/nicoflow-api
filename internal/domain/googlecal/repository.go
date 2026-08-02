package googlecal

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/pkg/cryptoutil"
)

// Repository persists the Google connection. The refresh token crosses this
// interface as plaintext and is encrypted on the way in / decrypted on the way
// out, so no caller can accidentally write an unencrypted token: there is no
// method that accepts ciphertext.
type Repository interface {
	Get(ctx context.Context, userID string) (Connection, error)
	Upsert(ctx context.Context, c Connection) (Connection, error)
	UpdateSelectedCalendars(ctx context.Context, userID string, calendarIDs []string) (Connection, error)
	SetError(ctx context.Context, userID string, message *string) error
	Delete(ctx context.Context, userID string) error
}

type pgRepo struct {
	db     *pgxpool.Pool
	cipher *cryptoutil.Cipher
}

// NewRepository creates a postgres-backed connection repository. The cipher is
// required: a disabled one makes every write fail loudly rather than silently
// storing a plaintext token.
func NewRepository(db *pgxpool.Pool, c *cryptoutil.Cipher) Repository {
	return &pgRepo{db: db, cipher: c}
}

const selectCols = ` user_id, refresh_token_encrypted, google_account_email,
	selected_calendar_ids, scopes, connected_at, last_sync_at, last_error `

// scan reads a row and decrypts the token in one step, so a Connection can never
// exist holding ciphertext in its plaintext field.
func (r *pgRepo) scan(row pgx.Row) (Connection, error) {
	var c Connection
	var sealed []byte

	if err := row.Scan(
		&c.UserID, &sealed, &c.GoogleAccountEmail,
		&c.SelectedCalendarIDs, &c.Scopes, &c.ConnectedAt, &c.LastSyncAt, &c.LastError,
	); err != nil {
		return Connection{}, err
	}

	token, err := r.cipher.DecryptString(sealed)
	if err != nil {
		// A stored token we cannot decrypt is unrecoverable — the key rotated or
		// the row was tampered with. Surface it as a connection failure so the
		// user is prompted to reconnect, rather than as a 500.
		return Connection{}, apperror.New(
			http.StatusBadGateway,
			apperror.ErrGoogleAuthFailed,
			"stored Google credentials could not be read; reconnect required",
		)
	}
	c.RefreshToken = Secret(token)

	return c, nil
}

func (r *pgRepo) Get(ctx context.Context, userID string) (Connection, error) {
	row := r.db.QueryRow(ctx,
		`SELECT`+selectCols+`FROM google_calendar_connections WHERE user_id = $1`,
		userID,
	)

	c, err := r.scan(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Connection{}, apperror.New(
			http.StatusConflict,
			apperror.ErrGoogleNotConnected,
			"no Google Calendar connection for this user",
		)
	}
	if err != nil {
		var appErr *apperror.AppError
		if errors.As(err, &appErr) {
			return Connection{}, appErr
		}
		return Connection{}, fmt.Errorf("googlecal.Get: %w", err)
	}
	return c, nil
}

// Upsert creates or replaces the user's connection. Re-consenting overwrites the
// stored token rather than adding a row — a user has exactly one Google link, and
// the PK enforces it.
func (r *pgRepo) Upsert(ctx context.Context, c Connection) (Connection, error) {
	// The one place plaintext leaves Secret: straight into the cipher, never
	// through an intermediate variable that something could log.
	sealed, err := r.cipher.EncryptString(c.RefreshToken.Reveal())
	if err != nil {
		return Connection{}, fmt.Errorf("googlecal.Upsert encrypt: %w", err)
	}

	// selected_calendar_ids is deliberately NOT overwritten on conflict: a user
	// re-authorising should keep the calendars they picked, not silently revert
	// to the default. last_error clears, since a successful connect means the
	// previous failure is resolved.
	row := r.db.QueryRow(ctx,
		`INSERT INTO google_calendar_connections
			(user_id, refresh_token_encrypted, google_account_email, selected_calendar_ids, scopes)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (user_id) DO UPDATE SET
			refresh_token_encrypted = EXCLUDED.refresh_token_encrypted,
			google_account_email    = EXCLUDED.google_account_email,
			scopes                  = EXCLUDED.scopes,
			connected_at            = NOW(),
			last_error              = NULL
		 RETURNING`+selectCols,
		c.UserID, sealed, c.GoogleAccountEmail, c.SelectedCalendarIDs, c.Scopes,
	)

	out, err := r.scan(row)
	if err != nil {
		var appErr *apperror.AppError
		if errors.As(err, &appErr) {
			return Connection{}, appErr
		}
		return Connection{}, fmt.Errorf("googlecal.Upsert: %w", err)
	}
	return out, nil
}

func (r *pgRepo) UpdateSelectedCalendars(ctx context.Context, userID string, calendarIDs []string) (Connection, error) {
	row := r.db.QueryRow(ctx,
		`UPDATE google_calendar_connections
			SET selected_calendar_ids = $2
		 WHERE user_id = $1
		 RETURNING`+selectCols,
		userID, calendarIDs,
	)

	c, err := r.scan(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Connection{}, apperror.New(
			http.StatusConflict,
			apperror.ErrGoogleNotConnected,
			"no Google Calendar connection for this user",
		)
	}
	if err != nil {
		var appErr *apperror.AppError
		if errors.As(err, &appErr) {
			return Connection{}, appErr
		}
		return Connection{}, fmt.Errorf("googlecal.UpdateSelectedCalendars: %w", err)
	}
	return c, nil
}

// SetError records or clears the last failure. Callers must pass a message that
// carries no token material — it is shown to the user.
func (r *pgRepo) SetError(ctx context.Context, userID string, message *string) error {
	if _, err := r.db.Exec(ctx,
		`UPDATE google_calendar_connections SET last_error = $2 WHERE user_id = $1`,
		userID, message,
	); err != nil {
		return fmt.Errorf("googlecal.SetError: %w", err)
	}
	return nil
}

// Delete removes the connection. Idempotent: disconnecting twice is not an error,
// because the caller's intent (no connection) is already satisfied.
func (r *pgRepo) Delete(ctx context.Context, userID string) error {
	if _, err := r.db.Exec(ctx,
		`DELETE FROM google_calendar_connections WHERE user_id = $1`,
		userID,
	); err != nil {
		return fmt.Errorf("googlecal.Delete: %w", err)
	}
	return nil
}
