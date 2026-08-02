//go:build integration

package googlecal_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nicoflow/nicoflow-api/internal/domain/googlecal"
	"github.com/nicoflow/nicoflow-api/internal/testutil"
)

func TestStateRepository_CreateAndConsume(t *testing.T) {
	pool := testutil.NewTestDB(t)
	repo := googlecal.NewStateRepository(pool)
	ctx := context.Background()
	userID := seedUser(t, pool)

	raw, fingerprint, err := googlecal.GenerateState()
	if err != nil {
		t.Fatalf("GenerateState() error = %v", err)
	}
	if raw == fingerprint {
		t.Fatal("raw state equals its fingerprint; the raw value must not be stored")
	}

	if err := repo.Create(ctx, userID, fingerprint, "/calendar", time.Now().Add(googlecal.StateTTL)); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	gotUser, gotPath, err := repo.Consume(ctx, fingerprint)
	if err != nil {
		t.Fatalf("Consume() error = %v", err)
	}
	if gotUser != userID {
		t.Errorf("userID = %q, want %q", gotUser, userID)
	}
	if gotPath != "/calendar" {
		t.Errorf("redirectPath = %q, want /calendar", gotPath)
	}
}

// The raw value must never be recoverable from the database — only its
// fingerprint is stored, so a dump yields nothing replayable.
func TestStateRepository_StoresOnlyFingerprint(t *testing.T) {
	pool := testutil.NewTestDB(t)
	repo := googlecal.NewStateRepository(pool)
	ctx := context.Background()
	userID := seedUser(t, pool)

	raw, fingerprint, err := googlecal.GenerateState()
	if err != nil {
		t.Fatalf("GenerateState() error = %v", err)
	}
	if err := repo.Create(ctx, userID, fingerprint, "", time.Now().Add(googlecal.StateTTL)); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM google_oauth_states WHERE state_fingerprint = $1`, raw,
	).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Error("the raw state value is present in the database")
	}
}

func TestStateRepository_ConsumeRejects(t *testing.T) {
	pool := testutil.NewTestDB(t)
	repo := googlecal.NewStateRepository(pool)
	ctx := context.Background()
	userID := seedUser(t, pool)

	tests := []struct {
		name  string
		setup func() string // returns the fingerprint to consume
	}{
		{
			name:  "unknown state",
			setup: func() string { return googlecal.StateFingerprint("never-issued") },
		},
		{
			name: "expired state",
			setup: func() string {
				_, fp, _ := googlecal.GenerateState()
				if err := repo.Create(ctx, userID, fp, "", time.Now().Add(-time.Minute)); err != nil {
					t.Fatalf("Create() error = %v", err)
				}
				return fp
			},
		},
		{
			name: "already consumed state",
			setup: func() string {
				_, fp, _ := googlecal.GenerateState()
				if err := repo.Create(ctx, userID, fp, "", time.Now().Add(googlecal.StateTTL)); err != nil {
					t.Fatalf("Create() error = %v", err)
				}
				if _, _, err := repo.Consume(ctx, fp); err != nil {
					t.Fatalf("first Consume() error = %v", err)
				}
				return fp
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := repo.Consume(ctx, tt.setup())
			// All three are deliberately indistinguishable — distinguishing them
			// would be an oracle for probing valid values.
			if !errors.Is(err, googlecal.ErrStateInvalid) {
				t.Errorf("Consume() error = %v, want ErrStateInvalid", err)
			}
		})
	}
}

// Two callbacks racing on one state must not both succeed — verification and
// consumption are a single atomic UPDATE for exactly this reason.
func TestStateRepository_ConcurrentConsumeYieldsOneWinner(t *testing.T) {
	pool := testutil.NewTestDB(t)
	repo := googlecal.NewStateRepository(pool)
	ctx := context.Background()
	userID := seedUser(t, pool)

	_, fingerprint, err := googlecal.GenerateState()
	if err != nil {
		t.Fatalf("GenerateState() error = %v", err)
	}
	if err := repo.Create(ctx, userID, fingerprint, "", time.Now().Add(googlecal.StateTTL)); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	const racers = 8
	var wg sync.WaitGroup
	results := make([]error, racers)

	wg.Add(racers)
	for i := range racers {
		go func() {
			defer wg.Done()
			_, _, results[i] = repo.Consume(ctx, fingerprint)
		}()
	}
	wg.Wait()

	wins := 0
	for _, err := range results {
		if err == nil {
			wins++
		}
	}
	if wins != 1 {
		t.Errorf("successful consumes = %d, want exactly 1", wins)
	}
}

// Restarting the flow supersedes the abandoned attempt — otherwise a stale
// state stays valid for the full TTL alongside the real one.
func TestStateRepository_CreateSupersedesUnusedStates(t *testing.T) {
	pool := testutil.NewTestDB(t)
	repo := googlecal.NewStateRepository(pool)
	ctx := context.Background()
	userID := seedUser(t, pool)

	_, first, _ := googlecal.GenerateState()
	if err := repo.Create(ctx, userID, first, "", time.Now().Add(googlecal.StateTTL)); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	_, second, _ := googlecal.GenerateState()
	if err := repo.Create(ctx, userID, second, "", time.Now().Add(googlecal.StateTTL)); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if _, _, err := repo.Consume(ctx, first); !errors.Is(err, googlecal.ErrStateInvalid) {
		t.Error("the superseded state is still consumable")
	}
	if _, _, err := repo.Consume(ctx, second); err != nil {
		t.Errorf("the current state was not consumable: %v", err)
	}
}

func TestStateRepository_DeleteExpired(t *testing.T) {
	pool := testutil.NewTestDB(t)
	repo := googlecal.NewStateRepository(pool)
	ctx := context.Background()
	userID := seedUser(t, pool)

	_, expired, _ := googlecal.GenerateState()
	if err := repo.Create(ctx, userID, expired, "", time.Now().Add(-time.Hour)); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	deleted, err := repo.DeleteExpired(ctx)
	if err != nil {
		t.Fatalf("DeleteExpired() error = %v", err)
	}
	if deleted < 1 {
		t.Errorf("deleted = %d, want at least 1", deleted)
	}
}

// A deleted account must not leave its pending consent states behind.
func TestStateRepository_UserDeleteCascades(t *testing.T) {
	pool := testutil.NewTestDB(t)
	repo := googlecal.NewStateRepository(pool)
	ctx := context.Background()
	userID := seedUser(t, pool)

	_, fingerprint, _ := googlecal.GenerateState()
	if err := repo.Create(ctx, userID, fingerprint, "", time.Now().Add(googlecal.StateTTL)); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM google_oauth_states WHERE state_fingerprint = $1`, fingerprint,
	).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("state rows after user delete = %d, want 0", count)
	}
}
