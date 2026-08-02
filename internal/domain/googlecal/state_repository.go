package googlecal

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// StateTTL bounds how long a consent round-trip may take. Long enough for a
// user to read the consent screen and pick an account, short enough that a
// state value harvested from a browser history or a proxy log is dead by the
// time anyone tries it.
const StateTTL = 10 * time.Minute

// stateBytes is the entropy of a raw state value. 32 bytes matches the refresh
// and password-reset tokens; guessing one is not a threat model we need to
// reason further about.
const stateBytes = 32

// StateRepository stores single-use OAuth `state` values.
//
// Consume is the security-critical method: it must atomically verify AND mark
// used, or two concurrent callbacks could both pass. The interface deliberately
// has no "check" method separate from "consume" — offering one would invite a
// caller to write the racy version.
type StateRepository interface {
	Create(ctx context.Context, userID, fingerprint, redirectPath string, expiresAt time.Time) error
	// Consume atomically marks a state used and returns the owning user and
	// redirect path. A value that is unknown, expired, or already used is
	// indistinguishable to the caller — all three return ErrStateInvalid.
	Consume(ctx context.Context, fingerprint string) (userID string, redirectPath string, err error)
	DeleteExpired(ctx context.Context) (int64, error)
}

// GenerateState returns a raw state value and its storage fingerprint. Only the
// fingerprint is persisted, so a database dump yields nothing replayable.
func GenerateState() (raw string, fingerprint string, err error) {
	buf := make([]byte, stateBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("googlecal: generate state: %w", err)
	}
	raw = hex.EncodeToString(buf)
	return raw, StateFingerprint(raw), nil
}

// StateFingerprint is the stored form of a state value.
func StateFingerprint(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

type pgStateRepo struct{ db *pgxpool.Pool }

// NewStateRepository creates a postgres-backed OAuth state repository.
func NewStateRepository(db *pgxpool.Pool) StateRepository { return &pgStateRepo{db: db} }

func (r *pgStateRepo) Create(ctx context.Context, userID, fingerprint, redirectPath string, expiresAt time.Time) error {
	// A user restarting the flow supersedes their unused states rather than
	// accumulating them — otherwise an abandoned attempt stays valid for the
	// full TTL alongside the real one.
	if _, err := r.db.Exec(ctx,
		`DELETE FROM google_oauth_states WHERE user_id = $1 AND used_at IS NULL`,
		userID,
	); err != nil {
		return fmt.Errorf("googlecal.Create state: clear prior: %w", err)
	}

	if _, err := r.db.Exec(ctx,
		`INSERT INTO google_oauth_states (state_fingerprint, user_id, redirect_path, expires_at)
		 VALUES ($1, $2, NULLIF($3, ''), $4)`,
		fingerprint, userID, redirectPath, expiresAt,
	); err != nil {
		return fmt.Errorf("googlecal.Create state: %w", err)
	}
	return nil
}

// Consume marks the state used and returns its owner.
//
// The UPDATE's WHERE clause carries the whole check — unused AND unexpired —
// so verification and consumption are one atomic statement. A replayed value
// matches zero rows on the second attempt because used_at is no longer NULL.
func (r *pgStateRepo) Consume(ctx context.Context, fingerprint string) (string, string, error) {
	var userID string
	var redirectPath *string

	err := r.db.QueryRow(ctx,
		`UPDATE google_oauth_states
			SET used_at = NOW()
		 WHERE state_fingerprint = $1
		   AND used_at IS NULL
		   AND expires_at > NOW()
		 RETURNING user_id, redirect_path`,
		fingerprint,
	).Scan(&userID, &redirectPath)

	if err != nil {
		// Unknown, expired and replayed all land here and are deliberately not
		// distinguished — telling a caller which one it was is an oracle.
		return "", "", ErrStateInvalid
	}

	if redirectPath == nil {
		return userID, "", nil
	}
	return userID, *redirectPath, nil
}

// DeleteExpired drops consumed and expired rows. Run from the existing cron
// sweep; nothing depends on it for correctness, since Consume already rejects
// stale values.
func (r *pgStateRepo) DeleteExpired(ctx context.Context) (int64, error) {
	tag, err := r.db.Exec(ctx,
		`DELETE FROM google_oauth_states WHERE expires_at < NOW() OR used_at IS NOT NULL`,
	)
	if err != nil {
		return 0, fmt.Errorf("googlecal.DeleteExpired states: %w", err)
	}
	return tag.RowsAffected(), nil
}
