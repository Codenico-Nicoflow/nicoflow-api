//go:build integration

package notelink_test

import (
	"context"
	"sort"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nicoflow/nicoflow-api/internal/domain/notelink"
	"github.com/nicoflow/nicoflow-api/internal/testutil"
)

const testEmailSuffix = "@notelink.integration.test"

func cleanTestData(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	queries := []string{
		`DELETE FROM note_links WHERE source_note_id IN (SELECT id FROM notes WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%' || $1))`,
		`DELETE FROM notes    WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%' || $1)`,
		`DELETE FROM projects WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%' || $1)`,
		`DELETE FROM areas    WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%' || $1)`,
		`DELETE FROM users    WHERE email LIKE '%' || $1`,
	}
	for _, q := range queries {
		if _, err := pool.Exec(context.Background(), q, testEmailSuffix); err != nil {
			t.Fatalf("cleanTestData: %v", err)
		}
	}
}

func newRepo(t *testing.T) (notelink.Repository, *pgxpool.Pool) {
	t.Helper()
	pool := testutil.NewTestDB(t)
	cleanTestData(t, pool)
	t.Cleanup(func() { cleanTestData(t, pool) })
	return notelink.NewRepository(pool), pool
}

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
	return id
}

func seedProject(t *testing.T, pool *pgxpool.Pool, userID string) string {
	t.Helper()
	ctx := context.Background()
	areaID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO areas (id, user_id, name) VALUES ($1, $2, $3)`,
		areaID, userID, "area "+areaID[:8],
	); err != nil {
		t.Fatalf("seedProject area: %v", err)
	}
	projectID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO projects (id, user_id, area_id, name) VALUES ($1, $2, $3, $4)`,
		projectID, userID, areaID, "project "+projectID[:8],
	); err != nil {
		t.Fatalf("seedProject project: %v", err)
	}
	return projectID
}

func seedNote(t *testing.T, pool *pgxpool.Pool, userID, projectID, title string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO notes (id, user_id, project_id, title) VALUES ($1, $2, $3, $4)`,
		id, userID, projectID, title,
	)
	if err != nil {
		t.Fatalf("seedNote: %v", err)
	}
	return id
}

func linkExists(t *testing.T, pool *pgxpool.Pool, sourceID, targetID string) bool {
	t.Helper()
	var exists bool
	err := pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM note_links WHERE source_note_id = $1 AND target_note_id = $2)`,
		sourceID, targetID,
	).Scan(&exists)
	if err != nil {
		t.Fatalf("linkExists: %v", err)
	}
	return exists
}

// AC1 — link created on save.
func TestCreateLink(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)
	projectID := seedProject(t, pool, userID)
	a := seedNote(t, pool, userID, projectID, "note A")
	b := seedNote(t, pool, userID, projectID, "note B")

	if err := repo.CreateLink(ctx, a, b); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
	if !linkExists(t, pool, a, b) {
		t.Error("expected note_links row (A, B) to exist")
	}
}

// Unique constraint prevents duplicate links — CreateLink is idempotent.
func TestCreateLink_Duplicate(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)
	projectID := seedProject(t, pool, userID)
	a := seedNote(t, pool, userID, projectID, "note A")
	b := seedNote(t, pool, userID, projectID, "note B")

	if err := repo.CreateLink(ctx, a, b); err != nil {
		t.Fatalf("CreateLink #1: %v", err)
	}
	if err := repo.CreateLink(ctx, a, b); err != nil {
		t.Fatalf("CreateLink #2 (duplicate) should be a no-op, got: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM note_links WHERE source_note_id = $1 AND target_note_id = $2`,
		a, b,
	).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1 (unique constraint should collapse duplicates)", count)
	}
}

func TestDeleteLink(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)
	projectID := seedProject(t, pool, userID)
	a := seedNote(t, pool, userID, projectID, "note A")
	b := seedNote(t, pool, userID, projectID, "note B")

	if err := repo.CreateLink(ctx, a, b); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
	if err := repo.DeleteLink(ctx, a, b); err != nil {
		t.Fatalf("DeleteLink: %v", err)
	}
	if linkExists(t, pool, a, b) {
		t.Error("expected note_links row (A, B) to be gone")
	}

	// Deleting an absent link is a no-op, not an error.
	if err := repo.DeleteLink(ctx, a, b); err != nil {
		t.Errorf("DeleteLink on absent row should not error, got: %v", err)
	}
}

// AC2 — link removed when mention removed, other links for the same source
// left alone.
func TestReplaceLinksForNote(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)
	projectID := seedProject(t, pool, userID)
	a := seedNote(t, pool, userID, projectID, "note A")
	b := seedNote(t, pool, userID, projectID, "note B")
	c := seedNote(t, pool, userID, projectID, "note C")
	d := seedNote(t, pool, userID, projectID, "note D")

	// A starts by mentioning B and C.
	if err := repo.ReplaceLinksForNote(ctx, a, []string{b, c}); err != nil {
		t.Fatalf("ReplaceLinksForNote (seed): %v", err)
	}
	if !linkExists(t, pool, a, b) || !linkExists(t, pool, a, c) {
		t.Fatal("expected A to link to both B and C after seed")
	}

	// A's content is resaved: mention of B removed, mention of D added, C kept.
	if err := repo.ReplaceLinksForNote(ctx, a, []string{c, d}); err != nil {
		t.Fatalf("ReplaceLinksForNote (resync): %v", err)
	}
	if linkExists(t, pool, a, b) {
		t.Error("expected A -> B to be removed after resync")
	}
	if !linkExists(t, pool, a, c) {
		t.Error("expected A -> C to survive the resync")
	}
	if !linkExists(t, pool, a, d) {
		t.Error("expected A -> D to be added by the resync")
	}

	// Resync to no mentions at all clears every outbound link.
	if err := repo.ReplaceLinksForNote(ctx, a, nil); err != nil {
		t.Fatalf("ReplaceLinksForNote (clear): %v", err)
	}
	if linkExists(t, pool, a, c) || linkExists(t, pool, a, d) {
		t.Error("expected all outbound links from A to be cleared")
	}
}

// AC3 — cascade on source delete.
func TestCascade_SourceDeleted(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)
	projectID := seedProject(t, pool, userID)
	a := seedNote(t, pool, userID, projectID, "note A")
	b := seedNote(t, pool, userID, projectID, "note B")

	if err := repo.CreateLink(ctx, a, b); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM notes WHERE id = $1`, a); err != nil {
		t.Fatalf("delete source note: %v", err)
	}

	if linkExists(t, pool, a, b) {
		t.Error("expected note_links row to cascade-delete when the source note is deleted")
	}
}

// AC4 — cascade on target delete, no hard-block.
func TestCascade_TargetDeleted(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)
	projectID := seedProject(t, pool, userID)
	a := seedNote(t, pool, userID, projectID, "note A")
	b := seedNote(t, pool, userID, projectID, "note B")

	if err := repo.CreateLink(ctx, a, b); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM notes WHERE id = $1`, b); err != nil {
		t.Fatalf("delete target note should succeed, no hard-block: %v", err)
	}

	if linkExists(t, pool, a, b) {
		t.Error("expected note_links row to cascade-delete when the target note is deleted")
	}
}

// Backlinks query returns the correct set.
func TestGetBacklinks(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)
	projectID := seedProject(t, pool, userID)
	target := seedNote(t, pool, userID, projectID, "target note")
	linkerOne := seedNote(t, pool, userID, projectID, "linker one")
	linkerTwo := seedNote(t, pool, userID, projectID, "linker two")

	if err := repo.CreateLink(ctx, linkerOne, target); err != nil {
		t.Fatalf("CreateLink linkerOne: %v", err)
	}
	if err := repo.CreateLink(ctx, linkerTwo, target); err != nil {
		t.Fatalf("CreateLink linkerTwo: %v", err)
	}

	backlinks, err := repo.GetBacklinks(ctx, target)
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(backlinks) != 2 {
		t.Fatalf("len(backlinks) = %d, want 2", len(backlinks))
	}

	got := []string{backlinks[0].ID, backlinks[1].ID}
	sort.Strings(got)
	want := []string{linkerOne, linkerTwo}
	sort.Strings(want)
	if got[0] != want[0] || got[1] != want[1] {
		t.Errorf("backlink IDs = %v, want %v", got, want)
	}

	empty, err := repo.GetBacklinks(ctx, linkerOne)
	if err != nil {
		t.Fatalf("GetBacklinks (no inbound): %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("expected no backlinks for a note nothing links to, got %d", len(empty))
	}
}
