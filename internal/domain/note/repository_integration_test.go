//go:build integration

package note_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/internal/domain/note"
	"github.com/nicoflow/nicoflow-api/internal/testutil"
)

const testEmailSuffix = "@note.integration.test"

func cleanTestData(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	// Notes and projects cascade from users, but delete leaf-first anyway so a
	// partial failure can't leave orphans behind for the next run.
	queries := []string{
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

func newRepo(t *testing.T) (note.Repository, *pgxpool.Pool) {
	t.Helper()
	pool := testutil.NewTestDB(t)
	cleanTestData(t, pool)
	t.Cleanup(func() { cleanTestData(t, pool) })
	return note.NewRepository(pool), pool
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

// seedProject creates the area a project requires (migration 027 made area_id
// NOT NULL) and returns the project id.
func seedProject(t *testing.T, pool *pgxpool.Pool, userID string) string {
	t.Helper()
	ctx := context.Background()
	// Names are unique per user, so derive them from the id — a test that seeds
	// two projects for one user would otherwise collide.
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

func newNote(userID, projectID, title, text string) note.Note {
	return note.Note{
		ID:          uuid.NewString(),
		UserID:      userID,
		ProjectID:   &projectID,
		Title:       title,
		Content:     json.RawMessage(note.EmptyDoc),
		ContentText: text,
	}
}

func mustCreate(t *testing.T, repo note.Repository, n note.Note) note.Note {
	t.Helper()
	out, err := repo.Create(context.Background(), n)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return out
}

func TestCreateAndGetByID(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)
	projectID := seedProject(t, pool, userID)

	content := json.RawMessage(`{"type":"doc","content":[{"type":"paragraph"}]}`)
	in := newNote(userID, projectID, "first note", "hello world")
	in.Content = content

	created := mustCreate(t, repo, in)

	if created.Version != 1 {
		t.Errorf("Version = %d, want 1 from the column default", created.Version)
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Error("timestamps were not stamped by the database")
	}

	got, err := repo.GetByID(ctx, userID, created.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Title != "first note" || got.ContentText != "hello world" {
		t.Errorf("got %+v, want the stored title and text", got)
	}
	if got.ProjectID == nil || *got.ProjectID != projectID {
		t.Errorf("ProjectID = %v, want %q", got.ProjectID, projectID)
	}
	// Compare semantically: JSONB normalises whitespace and key order on storage,
	// so the bytes that come back are not the bytes that went in.
	if !sameJSON(t, got.Content, content) {
		t.Errorf("Content = %s, want %s", got.Content, content)
	}
}

// sameJSON reports whether two JSON documents are structurally equal.
func sameJSON(t *testing.T, a, b json.RawMessage) bool {
	t.Helper()
	var av, bv interface{}
	if err := json.Unmarshal(a, &av); err != nil {
		t.Fatalf("sameJSON: %s: %v", a, err)
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		t.Fatalf("sameJSON: %s: %v", b, err)
	}
	return reflect.DeepEqual(av, bv)
}

// AC3 — another user's note is invisible, never forbidden, so the service can
// surface 404 without leaking that the row exists.
func TestRowIsolation(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	owner := seedUser(t, pool)
	intruder := seedUser(t, pool)
	projectID := seedProject(t, pool, owner)

	n := mustCreate(t, repo, newNote(owner, projectID, "private", "secret text"))

	t.Run("GetByID returns not-found", func(t *testing.T) {
		_, err := repo.GetByID(ctx, intruder, n.ID)
		var appErr *apperror.AppError
		if !errors.As(err, &appErr) {
			t.Fatalf("err = %v, want *apperror.AppError", err)
		}
		if appErr.Code != apperror.ErrResourceNotFound {
			t.Errorf("code = %q, want %q", appErr.Code, apperror.ErrResourceNotFound)
		}
	})

	t.Run("Delete affects no row", func(t *testing.T) {
		ok, err := repo.Delete(ctx, intruder, n.ID)
		if err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if ok {
			t.Error("ok = true, want false — a non-owner must not delete")
		}
		if _, err := repo.GetByID(ctx, owner, n.ID); err != nil {
			t.Errorf("owner lost the note: %v", err)
		}
	})

	t.Run("Update affects no row", func(t *testing.T) {
		_, ok, err := repo.Update(ctx, note.UpdateParams{
			ID: n.ID, UserID: intruder, Version: n.Version,
			Title: "hijacked", Content: json.RawMessage(note.EmptyDoc), ContentText: "hijacked",
		})
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
		if ok {
			t.Error("ok = true, want false — a non-owner must not update")
		}
		after, err := repo.GetByID(ctx, owner, n.ID)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if after.Title != "private" || after.Version != 1 {
			t.Errorf("note was mutated: title=%q version=%d", after.Title, after.Version)
		}
	})

	t.Run("ExistsForUser is false", func(t *testing.T) {
		exists, err := repo.ExistsForUser(ctx, intruder, n.ID)
		if err != nil {
			t.Fatalf("ExistsForUser: %v", err)
		}
		if exists {
			t.Error("exists = true, want false for a non-owner")
		}
	})
}

// AC4 — a stale version is rejected by the statement itself.
func TestUpdateVersionGuard(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)
	projectID := seedProject(t, pool, userID)

	n := mustCreate(t, repo, newNote(userID, projectID, "draft", "v1 text"))

	updated, ok, err := repo.Update(ctx, note.UpdateParams{
		ID: n.ID, UserID: userID, Version: 1,
		Title: "draft v2", Content: json.RawMessage(note.EmptyDoc), ContentText: "v2 text",
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !ok {
		t.Fatal("ok = false, want true for a current version")
	}
	if updated.Version != 2 {
		t.Errorf("Version = %d, want 2 after a guarded save", updated.Version)
	}
	if !updated.UpdatedAt.After(n.UpdatedAt) && !updated.UpdatedAt.Equal(n.UpdatedAt) {
		t.Error("updated_at went backwards")
	}

	// Replaying the same base version must lose — the row has moved on.
	_, ok, err = repo.Update(ctx, note.UpdateParams{
		ID: n.ID, UserID: userID, Version: 1,
		Title: "stale write", Content: json.RawMessage(note.EmptyDoc), ContentText: "stale text",
	})
	if err != nil {
		t.Fatalf("stale Update: %v", err)
	}
	if ok {
		t.Error("ok = true, want false for a stale version")
	}

	after, err := repo.GetByID(ctx, userID, n.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if after.Version != 2 || after.Title != "draft v2" || after.ContentText != "v2 text" {
		t.Errorf("stale save leaked through: %+v", after)
	}
}

func TestUpdateMissingNote(t *testing.T) {
	repo, pool := newRepo(t)
	userID := seedUser(t, pool)

	_, ok, err := repo.Update(context.Background(), note.UpdateParams{
		ID: uuid.NewString(), UserID: userID, Version: 1,
		Title: "ghost", Content: json.RawMessage(note.EmptyDoc), ContentText: "",
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if ok {
		t.Error("ok = true, want false for a missing note")
	}
}

// AC6 — the list is ordered by recency and never pulls the document body.
func TestListByProject(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)
	projectID := seedProject(t, pool, userID)
	otherProject := seedProject(t, pool, userID)

	body := json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"big"}]}]}`)
	for i, title := range []string{"oldest", "middle", "newest"} {
		n := newNote(userID, projectID, title, title+" text")
		n.Content = body
		created := mustCreate(t, repo, n)
		// Distinct updated_at values — successive NOW() calls can land on the same
		// instant and make the ordering assertion meaningless.
		if _, err := pool.Exec(ctx,
			`UPDATE notes SET updated_at = NOW() + $2::interval WHERE id = $1`,
			created.ID, fmt.Sprintf("%d seconds", i+1),
		); err != nil {
			t.Fatalf("stagger updated_at: %v", err)
		}
	}
	mustCreate(t, repo, newNote(userID, otherProject, "elsewhere", "other project"))

	list, _, err := repo.ListByProject(ctx, userID, projectID, note.ListNotesFilter{})
	if err != nil {
		t.Fatalf("ListByProject: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("len = %d, want 3 — the other project's note must not appear", len(list))
	}

	want := []string{"newest", "middle", "oldest"}
	for i, w := range want {
		if list[i].Title != w {
			t.Errorf("list[%d].Title = %q, want %q (updated_at DESC)", i, list[i].Title, w)
		}
	}
	for _, n := range list {
		if len(n.Content) != 0 {
			t.Errorf("list item %q carries content %s, want it omitted", n.Title, n.Content)
		}
		if n.ContentText == "" {
			t.Errorf("list item %q has no content_text for the excerpt", n.Title)
		}
	}
}

func TestListByProjectIsolatedByUser(t *testing.T) {
	repo, pool := newRepo(t)
	owner := seedUser(t, pool)
	intruder := seedUser(t, pool)
	projectID := seedProject(t, pool, owner)

	mustCreate(t, repo, newNote(owner, projectID, "private", "secret"))

	list, _, err := repo.ListByProject(context.Background(), intruder, projectID, note.ListNotesFilter{})
	if err != nil {
		t.Fatalf("ListByProject: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("len = %d, want 0 — a guessed project id must list nothing", len(list))
	}
}

func TestExistsForUser(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)
	projectID := seedProject(t, pool, userID)

	n := mustCreate(t, repo, newNote(userID, projectID, "owned", "text"))

	exists, err := repo.ExistsForUser(ctx, userID, n.ID)
	if err != nil {
		t.Fatalf("ExistsForUser: %v", err)
	}
	if !exists {
		t.Error("exists = false, want true for the owner")
	}

	exists, err = repo.ExistsForUser(ctx, userID, uuid.NewString())
	if err != nil {
		t.Fatalf("ExistsForUser missing: %v", err)
	}
	if exists {
		t.Error("exists = true, want false for a missing note")
	}
}

func TestDelete(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)
	projectID := seedProject(t, pool, userID)

	n := mustCreate(t, repo, newNote(userID, projectID, "doomed", "text"))

	ok, err := repo.Delete(ctx, userID, n.ID)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !ok {
		t.Fatal("ok = false, want true for the owner")
	}

	// A repeated delete is a miss, not an error — the caller maps it to 404.
	ok, err = repo.Delete(ctx, userID, n.ID)
	if err != nil {
		t.Fatalf("second Delete: %v", err)
	}
	if ok {
		t.Error("ok = true, want false on a repeated delete")
	}
}

// AC5 — deleting a project orphans its notes rather than destroying them.
func TestProjectDeleteOrphansNotes(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)
	projectID := seedProject(t, pool, userID)

	var ids []string
	for _, title := range []string{"a", "b", "c"} {
		ids = append(ids, mustCreate(t, repo, newNote(userID, projectID, title, title)).ID)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM projects WHERE id = $1`, projectID); err != nil {
		t.Fatalf("delete project: %v", err)
	}

	for _, id := range ids {
		got, err := repo.GetByID(ctx, userID, id)
		if err != nil {
			t.Fatalf("note %s was destroyed with its project: %v", id, err)
		}
		if got.ProjectID != nil {
			t.Errorf("ProjectID = %v, want nil after the project was deleted", *got.ProjectID)
		}
	}
}

// AC2 — the generated column indexes both fields and regenerates on update.
func TestSearchVector(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)
	projectID := seedProject(t, pool, userID)

	n := mustCreate(t, repo, newNote(userID, projectID, "quarterly roadmap", "shipping the notes editor"))

	matches := func(query string) bool {
		t.Helper()
		var found bool
		err := pool.QueryRow(ctx,
			`SELECT EXISTS (
			     SELECT 1 FROM notes
			      WHERE id = $1 AND search_vector @@ to_tsquery('simple', $2)
			 )`,
			n.ID, query,
		).Scan(&found)
		if err != nil {
			t.Fatalf("search %q: %v", query, err)
		}
		return found
	}

	if !matches("roadmap") {
		t.Error("title lexemes are missing from search_vector")
	}
	if !matches("editor") {
		t.Error("content_text lexemes are missing from search_vector")
	}
	// 'simple' does not stem, so a prefix query must still match.
	if !matches("roadma:*") {
		t.Error("prefix query did not match — the 'simple' config is expected")
	}

	if _, _, err := repo.Update(ctx, note.UpdateParams{
		ID: n.ID, UserID: userID, Version: n.Version,
		Title: "quarterly roadmap", Content: json.RawMessage(note.EmptyDoc), ContentText: "migrated to postgres",
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	if matches("editor") {
		t.Error("search_vector still holds the replaced text — it did not regenerate")
	}
	if !matches("postgres") {
		t.Error("search_vector did not pick up the new text")
	}
}

func TestContentDefaultsToEmptyDoc(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)
	projectID := seedProject(t, pool, userID)

	id := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO notes (id, user_id, project_id, title) VALUES ($1, $2, $3, 'defaulted')`,
		id, userID, projectID,
	); err != nil {
		t.Fatalf("insert: %v", err)
	}

	got, err := repo.GetByID(ctx, userID, id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}

	var doc struct {
		Type    string            `json:"type"`
		Content []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(got.Content, &doc); err != nil {
		t.Fatalf("content is not valid JSON: %v", err)
	}
	if doc.Type != "doc" || len(doc.Content) != 0 {
		t.Errorf("content = %s, want an empty doc", got.Content)
	}
	if got.ContentText != "" {
		t.Errorf("ContentText = %q, want empty", got.ContentText)
	}
}

// A hard-deleted user takes their notes with them (ON DELETE CASCADE), unlike a
// deleted project.
func TestUserDeleteCascades(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)
	projectID := seedProject(t, pool, userID)

	n := mustCreate(t, repo, newNote(userID, projectID, "gone", "text"))

	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM notes WHERE id = $1`, n.ID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0 — notes must cascade with their user", count)
	}
}

// A note referenced by a processed inbox item survives that item's FK: the
// column is ON DELETE SET NULL in the other direction.
func TestBucketCreatedNoteIDLink(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)
	projectID := seedProject(t, pool, userID)

	n := mustCreate(t, repo, newNote(userID, projectID, "from inbox", "captured"))

	bucketID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO bucket (id, user_id, content, processing_result, created_note_id, processed_at)
		 VALUES ($1, $2, 'a thought', 'note', $3, $4)`,
		bucketID, userID, n.ID, time.Now(),
	); err != nil {
		t.Fatalf("insert bucket item: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM bucket WHERE id = $1`, bucketID); err != nil {
			t.Errorf("cleanup bucket: %v", err)
		}
	})

	if ok, err := repo.Delete(ctx, userID, n.ID); err != nil || !ok {
		t.Fatalf("Delete: ok=%v err=%v", ok, err)
	}

	var linked *string
	if err := pool.QueryRow(ctx, `SELECT created_note_id FROM bucket WHERE id = $1`, bucketID).Scan(&linked); err != nil {
		t.Fatalf("read bucket item: %v", err)
	}
	if linked != nil {
		t.Errorf("created_note_id = %q, want NULL after the note was deleted", *linked)
	}
}
