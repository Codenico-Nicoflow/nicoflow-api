package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nicoflow/nicoflow-api/internal/model"
	pgrepo "github.com/nicoflow/nicoflow-api/internal/repository/postgres"
	"github.com/nicoflow/nicoflow-api/internal/testutil"
)

// insertTestUser inserts a minimal user row so FK constraints on refresh_tokens pass.
func insertTestUser(t *testing.T, pool *pgxpool.Pool, userID string) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO users (id, email, password_hash, timezone, created_at, updated_at)
		 VALUES ($1, $2, 'hash', 'UTC', NOW(), NOW())`,
		userID, userID+"@test.com",
	)
	if err != nil {
		t.Fatalf("insertTestUser: %v", err)
	}
}

// setupRepo connects to the test DB, registers cleanup, and returns a ready repo and pool
func setupRepo(t *testing.T) (*pgrepo.RefreshTokenRepo, *pgxpool.Pool) {
	t.Helper()
	pool := testutil.NewTestDB(t)
	t.Cleanup(func() {
		testutil.CleanTables(t, pool, "refresh_tokens", "users")
	})
	return pgrepo.NewRefreshTokenRepo(pool), pool
}

func TestRefreshTokenRepo_Create(t *testing.T) {
	repo, pool := setupRepo(t)

	userID := uuid.NewString()
	insertTestUser(t, pool, userID)

	tok := &model.RefreshToken{
		ID:        uuid.NewString(),
		UserID:    userID,
		TokenHash: "rt_test-hash_" + uuid.NewString(),
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
		CreatedAt: time.Now(),
	}

	if err := repo.Create(context.Background(), tok); err != nil {
		t.Fatalf("Create: %v", err)
	}

	token, err := repo.FindByTokenHash(context.Background(), tok.TokenHash)

	if token == nil || err != nil {
		t.Fatalf("FindByTokenHash: %v", err)
	}
	if token.ID != tok.ID {
		t.Errorf("FindByTokenHash: expected token ID %s, got %s", tok.ID, token.ID)
	}
	if token.UserID != tok.UserID {
		t.Errorf("FindByTokenHash: expected user ID %s, got %s", tok.UserID, token.UserID)
	}

}

func TestRefreshTokenRepo_FindByTokenHash_NotFound(t *testing.T) {
	repo, pool := setupRepo(t)

	userID := uuid.NewString()
	insertTestUser(t, pool, userID)

	token, err := repo.FindByTokenHash(context.Background(), "random_hash")
	if err != nil {
		t.Fatalf("FindByTokenHash: %v", err)
	}

	if token != nil {
		t.Errorf("FindByTokenHash: expected nil, got %v", token)
	}
}

func TestRefreshTokenRepo_DeleteByTokenHash(t *testing.T) {
	repo, pool := setupRepo(t)

	userID := uuid.NewString()
	insertTestUser(t, pool, userID)

	tok := &model.RefreshToken{
		ID:        uuid.NewString(),
		UserID:    userID,
		TokenHash: "rt_test_hash_" + uuid.NewString(),
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
		CreatedAt: time.Now(),
	}

	if err := repo.Create(context.Background(), tok); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := repo.DeleteByTokenHash(context.Background(), tok.TokenHash); err != nil {
		t.Fatalf("DeleteByTokenHash: %v", err)
	}

	token, err := repo.FindByTokenHash(context.Background(), tok.TokenHash)
	if err != nil {
		t.Fatalf("FindByTokenHash: %v", err)
	}

	if token != nil {
		t.Errorf("FindByTokenHash: expected nil, got %v", token)
	}
}

func TestRefreshTokenRepo_DeleteAllByUserID(t *testing.T) {
	repo, pool := setupRepo(t)

	userID := uuid.NewString()
	insertTestUser(t, pool, userID)

	tokOne := &model.RefreshToken{
		ID:        uuid.NewString(),
		UserID:    userID,
		TokenHash: "rt_test_hash_" + uuid.NewString(),
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
		CreatedAt: time.Now(),
	}
	tokTwo := &model.RefreshToken{
		ID:        uuid.NewString(),
		UserID:    userID,
		TokenHash: "rt_test_hash_" + uuid.NewString(),
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
		CreatedAt: time.Now(),
	}

	if err := repo.Create(context.Background(), tokOne); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Create(context.Background(), tokTwo); err != nil {
		t.Fatalf("Create: %v", err)
	}

	err := repo.DeleteAllByUserID(context.Background(), userID)

	if err != nil {
		t.Fatalf("DeleteAllByUserID: %v", err)
	}

	tOne, err := repo.FindByTokenHash(context.Background(), tokOne.TokenHash)
	if err != nil {
		t.Fatalf("FindByTokenHash: %v", err)
	}
	if tOne != nil {
		t.Errorf("FindByTokenHash: expected nil, got %v", tOne)
	}

	tTwo, err := repo.FindByTokenHash(context.Background(), tokTwo.TokenHash)
	if err != nil {
		t.Fatalf("FindByTokenHash: %v", err)
	}
	if tTwo != nil {
		t.Errorf("FindByTokenHash: expected nil, got %v", tTwo)
	}
}
