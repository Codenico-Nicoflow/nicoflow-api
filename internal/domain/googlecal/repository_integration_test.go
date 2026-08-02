//go:build integration

package googlecal_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/internal/domain/googlecal"
	"github.com/nicoflow/nicoflow-api/internal/testutil"
	"github.com/nicoflow/nicoflow-api/pkg/cryptoutil"
)

const (
	testEmailSuffix  = "@googlecal-test.local"
	testRefreshToken = "1//0-integration-refresh-token"
	readonlyScope    = "https://www.googleapis.com/auth/calendar.readonly"
)

func seedUser(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	id := uuid.NewString()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO users (id, email, username, password_hash, plan)
		 VALUES ($1, $2, $3, 'x', 'free')`,
		id, id+testEmailSuffix, "u_"+id[:8],
	)
	if err != nil {
		t.Fatalf("seedUser: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, id)
	})
	return id
}

func newRepo(t *testing.T, pool *pgxpool.Pool) googlecal.Repository {
	t.Helper()
	key := make([]byte, cryptoutil.KeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	c, err := cryptoutil.NewCipher(base64.StdEncoding.EncodeToString(key))
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	return googlecal.NewRepository(pool, c)
}

func newConnection(userID string) googlecal.Connection {
	return googlecal.Connection{
		UserID:              userID,
		RefreshToken:        googlecal.Secret(testRefreshToken),
		GoogleAccountEmail:  "user@example.com",
		SelectedCalendarIDs: []string{"primary"},
		Scopes:              []string{readonlyScope},
	}
}

func TestRepository_UpsertAndGet(t *testing.T) {
	pool := testutil.NewTestDB(t)
	repo := newRepo(t, pool)
	ctx := context.Background()
	userID := seedUser(t, pool)

	if _, err := repo.Upsert(ctx, newConnection(userID)); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	got, err := repo.Get(ctx, userID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if got.RefreshToken.Reveal() != testRefreshToken {
		t.Errorf("RefreshToken = %q, want the original token back", got.RefreshToken.Reveal())
	}
	if got.GoogleAccountEmail != "user@example.com" {
		t.Errorf("GoogleAccountEmail = %q", got.GoogleAccountEmail)
	}
	if len(got.Scopes) != 1 || got.Scopes[0] != readonlyScope {
		t.Errorf("Scopes = %v, want only the readonly scope", got.Scopes)
	}
	if got.LastError != nil {
		t.Errorf("LastError = %v, want nil on a fresh connection", *got.LastError)
	}
}

// AC2: the token must be provably ciphertext in the database, not readable
// plaintext — inspected directly, bypassing the repository that would decrypt it.
func TestRepository_TokenStoredAsCiphertext(t *testing.T) {
	pool := testutil.NewTestDB(t)
	repo := newRepo(t, pool)
	ctx := context.Background()
	userID := seedUser(t, pool)

	if _, err := repo.Upsert(ctx, newConnection(userID)); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	var stored []byte
	if err := pool.QueryRow(ctx,
		`SELECT refresh_token_encrypted FROM google_calendar_connections WHERE user_id = $1`,
		userID,
	).Scan(&stored); err != nil {
		t.Fatalf("raw select: %v", err)
	}

	if len(stored) == 0 {
		t.Fatal("stored token is empty")
	}
	if bytes.Contains(stored, []byte(testRefreshToken)) {
		t.Error("stored value contains the plaintext token")
	}
	if string(stored) == testRefreshToken {
		t.Error("stored value IS the plaintext token")
	}
}

// A token encrypted under one key must not be readable by a repository holding
// another — the ciphertext is bound to the key, not merely obfuscated.
func TestRepository_WrongKeyFailsToRead(t *testing.T) {
	pool := testutil.NewTestDB(t)
	ctx := context.Background()
	userID := seedUser(t, pool)

	if _, err := newRepo(t, pool).Upsert(ctx, newConnection(userID)); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	// A second repo with a freshly generated (different) key.
	_, err := newRepo(t, pool).Get(ctx, userID)
	if err == nil {
		t.Fatal("Get() with the wrong key succeeded; want a typed failure")
	}

	appErr, ok := err.(*apperror.AppError)
	if !ok {
		t.Fatalf("Get() error = %T, want *apperror.AppError", err)
	}
	if appErr.Code != apperror.ErrGoogleAuthFailed {
		t.Errorf("error code = %q, want %q", appErr.Code, apperror.ErrGoogleAuthFailed)
	}
}

func TestRepository_GetMissingIsTyped(t *testing.T) {
	pool := testutil.NewTestDB(t)
	repo := newRepo(t, pool)
	userID := seedUser(t, pool)

	_, err := repo.Get(context.Background(), userID)
	if err == nil {
		t.Fatal("Get() on a user with no connection succeeded")
	}

	appErr, ok := err.(*apperror.AppError)
	if !ok {
		t.Fatalf("Get() error = %T, want *apperror.AppError", err)
	}
	if appErr.Code != apperror.ErrGoogleNotConnected {
		t.Errorf("error code = %q, want %q", appErr.Code, apperror.ErrGoogleNotConnected)
	}
}

// Re-consenting replaces the token and keeps the user's calendar picks — losing
// them on every re-auth would silently undo a deliberate choice.
func TestRepository_UpsertReplacesTokenKeepsSelection(t *testing.T) {
	pool := testutil.NewTestDB(t)
	repo := newRepo(t, pool)
	ctx := context.Background()
	userID := seedUser(t, pool)

	if _, err := repo.Upsert(ctx, newConnection(userID)); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	if _, err := repo.UpdateSelectedCalendars(ctx, userID, []string{"work", "personal"}); err != nil {
		t.Fatalf("UpdateSelectedCalendars() error = %v", err)
	}

	reconnected := newConnection(userID)
	reconnected.RefreshToken = googlecal.Secret("1//0-rotated-token")
	reconnected.SelectedCalendarIDs = []string{"primary"}
	got, err := repo.Upsert(ctx, reconnected)
	if err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	if got.RefreshToken.Reveal() != "1//0-rotated-token" {
		t.Errorf("RefreshToken = %q, want the rotated token", got.RefreshToken.Reveal())
	}
	if len(got.SelectedCalendarIDs) != 2 {
		t.Errorf("SelectedCalendarIDs = %v, want the prior selection preserved", got.SelectedCalendarIDs)
	}
}

func TestRepository_SetErrorAndClear(t *testing.T) {
	pool := testutil.NewTestDB(t)
	repo := newRepo(t, pool)
	ctx := context.Background()
	userID := seedUser(t, pool)

	if _, err := repo.Upsert(ctx, newConnection(userID)); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	message := "invalid_grant"
	if err := repo.SetError(ctx, userID, &message); err != nil {
		t.Fatalf("SetError() error = %v", err)
	}
	got, err := repo.Get(ctx, userID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.LastError == nil || *got.LastError != message {
		t.Errorf("LastError = %v, want %q", got.LastError, message)
	}

	// A successful reconnect clears the recorded failure.
	if _, err := repo.Upsert(ctx, newConnection(userID)); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	got, err = repo.Get(ctx, userID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.LastError != nil {
		t.Errorf("LastError = %q, want nil after a successful reconnect", *got.LastError)
	}
}

// Disconnecting twice is not an error — the caller's intent is already satisfied.
func TestRepository_DeleteIsIdempotent(t *testing.T) {
	pool := testutil.NewTestDB(t)
	repo := newRepo(t, pool)
	ctx := context.Background()
	userID := seedUser(t, pool)

	if _, err := repo.Upsert(ctx, newConnection(userID)); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	for i := range 2 {
		if err := repo.Delete(ctx, userID); err != nil {
			t.Fatalf("Delete() call %d error = %v", i+1, err)
		}
	}

	if _, err := repo.Get(ctx, userID); err == nil {
		t.Error("Get() succeeded after Delete()")
	}
}

// AC5-adjacent: deleting the user must take the Google connection with it, so a
// deleted account leaves no live third-party credential behind.
func TestRepository_UserDeleteCascades(t *testing.T) {
	pool := testutil.NewTestDB(t)
	repo := newRepo(t, pool)
	ctx := context.Background()
	userID := seedUser(t, pool)

	if _, err := repo.Upsert(ctx, newConnection(userID)); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM google_calendar_connections WHERE user_id = $1`, userID,
	).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("connection rows after user delete = %d, want 0", count)
	}
}
