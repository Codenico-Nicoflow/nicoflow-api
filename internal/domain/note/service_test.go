package note_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/internal/domain/note"
	"github.com/nicoflow/nicoflow-api/internal/domain/notelink"
)

// ── mocks ─────────────────────────────────────────────────────────────────────

type mockRepo struct {
	listByProject  func(ctx context.Context, userID, projectID string, f note.ListNotesFilter) ([]note.Note, string, error)
	getByID        func(ctx context.Context, userID, id string) (note.Note, error)
	create         func(ctx context.Context, n note.Note) (note.Note, error)
	update         func(ctx context.Context, p note.UpdateParams) (note.Note, bool, error)
	deleteFn       func(ctx context.Context, userID, id string) (bool, error)
	existsForUser  func(ctx context.Context, userID, noteID string) (bool, error)
	existsByID     func(ctx context.Context, id string) (bool, error)
	searchMentions func(ctx context.Context, userID, term, excludeID string, limit int) ([]note.MentionResult, error)
}

func (m *mockRepo) ListByProject(ctx context.Context, userID, projectID string, f note.ListNotesFilter) ([]note.Note, string, error) {
	return m.listByProject(ctx, userID, projectID, f)
}
func (m *mockRepo) GetByID(ctx context.Context, userID, id string) (note.Note, error) {
	return m.getByID(ctx, userID, id)
}
func (m *mockRepo) Create(ctx context.Context, n note.Note) (note.Note, error) {
	return m.create(ctx, n)
}
func (m *mockRepo) Update(ctx context.Context, p note.UpdateParams) (note.Note, bool, error) {
	return m.update(ctx, p)
}
func (m *mockRepo) Delete(ctx context.Context, userID, id string) (bool, error) {
	return m.deleteFn(ctx, userID, id)
}
func (m *mockRepo) ExistsForUser(ctx context.Context, userID, noteID string) (bool, error) {
	if m.existsForUser == nil {
		return true, nil
	}
	return m.existsForUser(ctx, userID, noteID)
}
func (m *mockRepo) ExistsByID(ctx context.Context, id string) (bool, error) {
	if m.existsByID == nil {
		return true, nil
	}
	return m.existsByID(ctx, id)
}
func (m *mockRepo) SearchMentions(ctx context.Context, userID, term, excludeID string, limit int) ([]note.MentionResult, error) {
	if m.searchMentions == nil {
		return nil, nil
	}
	return m.searchMentions(ctx, userID, term, excludeID, limit)
}

type mockProjects struct {
	verify func(ctx context.Context, userID, projectID string) error
}

func (m *mockProjects) VerifyProjectOwner(ctx context.Context, userID, projectID string) error {
	if m.verify == nil {
		return nil
	}
	return m.verify(ctx, userID, projectID)
}

// ── helpers ───────────────────────────────────────────────────────────────────

const (
	testUser    = "u_1"
	testProject = "p_1"
	testNoteID  = "n_1"
)

var notFound = apperror.New(http.StatusNotFound, apperror.ErrResourceNotFound, "resource not found")

// echoCreate returns the row as the database would: version 1, timestamps set.
func echoCreate(ctx context.Context, n note.Note) (note.Note, error) {
	n.Version = 1
	n.CreatedAt = time.Now()
	n.UpdatedAt = time.Now()
	return n, nil
}

func storedNote(version int) note.Note {
	pid := testProject
	return note.Note{
		ID: testNoteID, UserID: testUser, ProjectID: &pid,
		Title: "stored", Content: json.RawMessage(note.EmptyDoc), ContentText: "stored text",
		Version: version, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
}

// assertAppErr checks an error carries the expected code and status.
func assertAppErr(t *testing.T, err error, wantCode string, wantStatus int) {
	t.Helper()
	if err == nil {
		t.Fatalf("err = nil, want %s", wantCode)
	}
	var ae *apperror.AppError
	if !errors.As(err, &ae) {
		t.Fatalf("err = %v, want *apperror.AppError", err)
	}
	if ae.Code != wantCode {
		t.Errorf("code = %q, want %q", ae.Code, wantCode)
	}
	if ae.Status != wantStatus {
		t.Errorf("status = %d, want %d", ae.Status, wantStatus)
	}
}

// ── create ────────────────────────────────────────────────────────────────────

// AC1 — create happy path.
func TestServiceCreate(t *testing.T) {
	var saved note.Note
	repo := &mockRepo{create: func(ctx context.Context, n note.Note) (note.Note, error) {
		saved = n
		return echoCreate(ctx, n)
	}}
	svc := note.NewService(repo, &mockProjects{}, nil, nil)

	content := json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"GTD thread"}]}]}`)
	view, err := svc.Create(context.Background(), testUser, note.CreateNoteRequest{
		ProjectID: testProject, Title: "GTD structure thread", Content: content,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if view.Version != 1 {
		t.Errorf("Version = %d, want 1", view.Version)
	}
	if view.Title != "GTD structure thread" {
		t.Errorf("Title = %q", view.Title)
	}
	if string(view.Content) != string(content) {
		t.Errorf("Content = %s, want the submitted document", view.Content)
	}
	// The mirror is server-derived, never client-supplied.
	if saved.ContentText != "GTD thread" {
		t.Errorf("ContentText = %q, want it derived by flattenDoc", saved.ContentText)
	}
	if saved.UserID != testUser {
		t.Errorf("UserID = %q, want the caller", saved.UserID)
	}
}

func TestCreateDefaultsContentToEmptyDoc(t *testing.T) {
	var saved note.Note
	repo := &mockRepo{create: func(ctx context.Context, n note.Note) (note.Note, error) {
		saved = n
		return echoCreate(ctx, n)
	}}
	svc := note.NewService(repo, &mockProjects{}, nil, nil)

	if _, err := svc.Create(context.Background(), testUser, note.CreateNoteRequest{
		ProjectID: testProject, Title: "no body",
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	var doc struct {
		Type    string            `json:"type"`
		Content []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(saved.Content, &doc); err != nil {
		t.Fatalf("stored content is not valid JSON: %v", err)
	}
	if doc.Type != "doc" || len(doc.Content) != 0 {
		t.Errorf("content = %s, want the empty doc", saved.Content)
	}
	if saved.ContentText != "" {
		t.Errorf("ContentText = %q, want empty", saved.ContentText)
	}
}

// AC3 — a project the caller doesn't own is 404, never 403, and nothing is written.
func TestCreateForeignProjectDoesNotLeak(t *testing.T) {
	created := false
	repo := &mockRepo{create: func(ctx context.Context, n note.Note) (note.Note, error) {
		created = true
		return echoCreate(ctx, n)
	}}
	svc := note.NewService(repo, &mockProjects{
		verify: func(ctx context.Context, userID, projectID string) error { return notFound },
	}, nil, nil)

	_, err := svc.Create(context.Background(), testUser, note.CreateNoteRequest{
		ProjectID: "p_someone_else", Title: "trespass",
	})

	assertAppErr(t, err, apperror.ErrResourceNotFound, http.StatusNotFound)
	if created {
		t.Error("a note was created for a project the caller does not own")
	}
}

// AC8 — validation.
func TestCreateValidation(t *testing.T) {
	tests := []struct {
		name string
		req  note.CreateNoteRequest
	}{
		{name: "missing projectId", req: note.CreateNoteRequest{Title: "t"}},
		{name: "blank projectId", req: note.CreateNoteRequest{ProjectID: "   ", Title: "t"}},
		{name: "empty title", req: note.CreateNoteRequest{ProjectID: testProject, Title: ""}},
		{name: "whitespace-only title", req: note.CreateNoteRequest{ProjectID: testProject, Title: "   "}},
		{
			name: "title over 255",
			req:  note.CreateNoteRequest{ProjectID: testProject, Title: strings.Repeat("a", 256)},
		},
		{
			name: "content is an array",
			req:  note.CreateNoteRequest{ProjectID: testProject, Title: "t", Content: json.RawMessage(`[]`)},
		},
		{
			name: "content is a bare string",
			req:  note.CreateNoteRequest{ProjectID: testProject, Title: "t", Content: json.RawMessage(`"x"`)},
		},
		{
			name: "content is malformed",
			req:  note.CreateNoteRequest{ProjectID: testProject, Title: "t", Content: json.RawMessage(`{`)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			created := false
			repo := &mockRepo{create: func(ctx context.Context, n note.Note) (note.Note, error) {
				created = true
				return echoCreate(ctx, n)
			}}
			svc := note.NewService(repo, &mockProjects{}, nil, nil)

			_, err := svc.Create(context.Background(), testUser, tt.req)

			assertAppErr(t, err, apperror.ErrInvalidInput, http.StatusUnprocessableEntity)
			if created {
				t.Error("an invalid request reached the repository")
			}
		})
	}
}

func TestCreateTitleAtLimitIsAccepted(t *testing.T) {
	repo := &mockRepo{create: echoCreate}
	svc := note.NewService(repo, &mockProjects{}, nil, nil)

	// 255 multi-byte runes: within the column, but 510 bytes — a byte-based
	// length check would wrongly reject this.
	title := strings.Repeat("ש", 255)
	if _, err := svc.Create(context.Background(), testUser, note.CreateNoteRequest{
		ProjectID: testProject, Title: title,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
}

// AC2 — notes are Free and unlimited: no count, no cap, no plan argument.
func TestCreateIsNotPlanGated(t *testing.T) {
	count := 0
	repo := &mockRepo{create: func(ctx context.Context, n note.Note) (note.Note, error) {
		count++
		return echoCreate(ctx, n)
	}}
	svc := note.NewService(repo, &mockProjects{}, nil, nil)

	for i := range 50 {
		if _, err := svc.Create(context.Background(), testUser, note.CreateNoteRequest{
			ProjectID: testProject, Title: fmt.Sprintf("note %d", i),
		}); err != nil {
			t.Fatalf("note %d was refused: %v", i, err)
		}
	}
	if count != 50 {
		t.Errorf("created %d notes, want 50", count)
	}
}

// ── update ────────────────────────────────────────────────────────────────────

// AC5 — a successful update bumps the version.
func TestUpdateBumpsVersion(t *testing.T) {
	var got note.UpdateParams
	repo := &mockRepo{
		getByID: func(ctx context.Context, userID, id string) (note.Note, error) {
			return storedNote(7), nil
		},
		update: func(ctx context.Context, p note.UpdateParams) (note.Note, bool, error) {
			got = p
			n := storedNote(8)
			n.Title, n.Content, n.ContentText = p.Title, p.Content, p.ContentText
			return n, true, nil
		},
	}
	svc := note.NewService(repo, &mockProjects{}, nil, nil)

	title := "renamed"
	content := json.RawMessage(`{"type":"doc","content":[{"type":"text","text":"new body"}]}`)
	view, err := svc.Update(context.Background(), testUser, testNoteID, note.UpdateNoteRequest{
		Title: &title, Content: &content, Version: intPtr(7),
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if view.Version != 8 {
		t.Errorf("Version = %d, want 8", view.Version)
	}
	if got.Version != 7 {
		t.Errorf("guarded on version %d, want the client's 7", got.Version)
	}
	if got.ContentText != "new body" {
		t.Errorf("ContentText = %q, want it re-derived by flattenDoc", got.ContentText)
	}
}

// AC4 — a stale version is a 409, and the document is not touched.
func TestUpdateStaleVersionConflicts(t *testing.T) {
	repo := &mockRepo{
		getByID: func(ctx context.Context, userID, id string) (note.Note, error) {
			return storedNote(7), nil
		},
		// 0 rows: the guarded UPDATE matched nothing because version moved on.
		update: func(ctx context.Context, p note.UpdateParams) (note.Note, bool, error) {
			return note.Note{}, false, nil
		},
	}
	svc := note.NewService(repo, &mockProjects{}, nil, nil)

	title := "stale write"
	_, err := svc.Update(context.Background(), testUser, testNoteID, note.UpdateNoteRequest{
		Title: &title, Version: intPtr(6),
	})

	assertAppErr(t, err, apperror.ErrConflict, http.StatusConflict)
}

// A note the caller doesn't own is 404, not 409 — the conflict path must never
// become an existence oracle.
func TestUpdateForeignNoteIsNotFound(t *testing.T) {
	repo := &mockRepo{
		getByID: func(ctx context.Context, userID, id string) (note.Note, error) {
			return note.Note{}, notFound
		},
	}
	svc := note.NewService(repo, &mockProjects{}, nil, nil)

	title := "hijack"
	_, err := svc.Update(context.Background(), testUser, testNoteID, note.UpdateNoteRequest{
		Title: &title, Version: intPtr(1),
	})

	assertAppErr(t, err, apperror.ErrResourceNotFound, http.StatusNotFound)
}

func TestUpdateVersionValidation(t *testing.T) {
	tests := []struct {
		name string
		req  note.UpdateNoteRequest
	}{
		{name: "version omitted", req: note.UpdateNoteRequest{}},
		{name: "version zero", req: note.UpdateNoteRequest{Version: intPtr(0)}},
		{name: "version negative", req: note.UpdateNoteRequest{Version: intPtr(-1)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reached := false
			repo := &mockRepo{
				getByID: func(ctx context.Context, userID, id string) (note.Note, error) {
					reached = true
					return storedNote(1), nil
				},
			}
			svc := note.NewService(repo, &mockProjects{}, nil, nil)

			_, err := svc.Update(context.Background(), testUser, testNoteID, tt.req)

			assertAppErr(t, err, apperror.ErrInvalidInput, http.StatusUnprocessableEntity)
			if reached {
				t.Error("an invalid version reached the repository")
			}
		})
	}
}

// PATCH is partial: an omitted field keeps its stored value rather than blanking.
func TestUpdatePreservesOmittedFields(t *testing.T) {
	stored := storedNote(3)
	var got note.UpdateParams
	repo := &mockRepo{
		getByID: func(ctx context.Context, userID, id string) (note.Note, error) { return stored, nil },
		update: func(ctx context.Context, p note.UpdateParams) (note.Note, bool, error) {
			got = p
			return stored, true, nil
		},
	}
	svc := note.NewService(repo, &mockProjects{}, nil, nil)

	title := "only the title changes"
	if _, err := svc.Update(context.Background(), testUser, testNoteID, note.UpdateNoteRequest{
		Title: &title, Version: intPtr(3),
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	if got.Title != title {
		t.Errorf("Title = %q, want %q", got.Title, title)
	}
	if string(got.Content) != string(stored.Content) {
		t.Errorf("Content = %s, want the stored document untouched", got.Content)
	}
	if got.ContentText != stored.ContentText {
		t.Errorf("ContentText = %q, want the stored mirror untouched", got.ContentText)
	}
}

func TestUpdateValidatesTitleAndContent(t *testing.T) {
	long := strings.Repeat("a", 256)
	blank := "  "
	badContent := json.RawMessage(`[]`)

	tests := []struct {
		name string
		req  note.UpdateNoteRequest
	}{
		{name: "empty title", req: note.UpdateNoteRequest{Title: &blank, Version: intPtr(1)}},
		{name: "title over 255", req: note.UpdateNoteRequest{Title: &long, Version: intPtr(1)}},
		{name: "content not an object", req: note.UpdateNoteRequest{Content: &badContent, Version: intPtr(1)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			written := false
			repo := &mockRepo{
				getByID: func(ctx context.Context, userID, id string) (note.Note, error) {
					return storedNote(1), nil
				},
				update: func(ctx context.Context, p note.UpdateParams) (note.Note, bool, error) {
					written = true
					return storedNote(2), true, nil
				},
			}
			svc := note.NewService(repo, &mockProjects{}, nil, nil)

			_, err := svc.Update(context.Background(), testUser, testNoteID, tt.req)

			assertAppErr(t, err, apperror.ErrInvalidInput, http.StatusUnprocessableEntity)
			if written {
				t.Error("an invalid update reached the repository")
			}
		})
	}
}

// ── list ──────────────────────────────────────────────────────────────────────

// AC6 — the list carries excerpts and no body.
func TestListReturnsExcerptsOnly(t *testing.T) {
	long := strings.Repeat("x", 500)
	repo := &mockRepo{listByProject: func(ctx context.Context, userID, projectID string, f note.ListNotesFilter) ([]note.Note, string, error) {
		pid := testProject
		return []note.Note{
			{ID: "n_1", ProjectID: &pid, Title: "big", ContentText: long, Version: 2},
			{ID: "n_2", ProjectID: &pid, Title: "small", ContentText: "short text", Version: 1},
		}, "", nil
	}}
	svc := note.NewService(repo, &mockProjects{}, nil, nil)

	result, err := svc.ListByProject(context.Background(), testUser, testProject, note.ListNotesFilter{})
	if err != nil {
		t.Fatalf("ListByProject: %v", err)
	}
	list := result.Items
	if len(list) != 2 {
		t.Fatalf("len = %d, want 2", len(list))
	}

	if got := len([]rune(list[0].Excerpt)); got != note.ExcerptLen {
		t.Errorf("excerpt length = %d, want %d", got, note.ExcerptLen)
	}
	if list[1].Excerpt != "short text" {
		t.Errorf("excerpt = %q, want the full short text", list[1].Excerpt)
	}

	// The list shape has no Content field at all — assert on the marshalled JSON
	// so a future field addition can't silently start shipping bodies.
	raw, err := json.Marshal(list[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, present := fields["content"]; present {
		t.Errorf("list item carries a content field: %s", raw)
	}
	if _, present := fields["excerpt"]; !present {
		t.Errorf("list item has no excerpt field: %s", raw)
	}
}

func TestListRequiresProjectID(t *testing.T) {
	svc := note.NewService(&mockRepo{}, &mockProjects{}, nil, nil)

	_, err := svc.ListByProject(context.Background(), testUser, "  ", note.ListNotesFilter{})

	assertAppErr(t, err, apperror.ErrInvalidInput, http.StatusUnprocessableEntity)
}

// A guessed project id must 404 rather than return an empty list — an empty
// list would still confirm the project is reachable.
func TestListForeignProjectIsNotFound(t *testing.T) {
	listed := false
	repo := &mockRepo{listByProject: func(ctx context.Context, userID, projectID string, f note.ListNotesFilter) ([]note.Note, string, error) {
		listed = true
		return nil, "", nil
	}}
	svc := note.NewService(repo, &mockProjects{
		verify: func(ctx context.Context, userID, projectID string) error { return notFound },
	}, nil, nil)

	_, err := svc.ListByProject(context.Background(), testUser, "p_someone_else", note.ListNotesFilter{})

	assertAppErr(t, err, apperror.ErrResourceNotFound, http.StatusNotFound)
	if listed {
		t.Error("notes were listed for a project the caller does not own")
	}
}

func TestListEmptyProjectReturnsEmptySlice(t *testing.T) {
	repo := &mockRepo{listByProject: func(ctx context.Context, userID, projectID string, f note.ListNotesFilter) ([]note.Note, string, error) {
		return nil, "", nil
	}}
	svc := note.NewService(repo, &mockProjects{}, nil, nil)

	result, err := svc.ListByProject(context.Background(), testUser, testProject, note.ListNotesFilter{})
	if err != nil {
		t.Fatalf("ListByProject: %v", err)
	}
	// Non-nil so the envelope carries [] rather than null.
	if result.Items == nil {
		t.Fatal("items is nil, want an empty slice")
	}
	if len(result.Items) != 0 {
		t.Errorf("len = %d, want 0", len(result.Items))
	}
}

// ── get / delete ──────────────────────────────────────────────────────────────

func TestGetReturnsFullDocument(t *testing.T) {
	stored := storedNote(4)
	repo := &mockRepo{getByID: func(ctx context.Context, userID, id string) (note.Note, error) {
		return stored, nil
	}}
	svc := note.NewService(repo, &mockProjects{}, nil, nil)

	view, err := svc.Get(context.Background(), testUser, testNoteID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(view.Content) != string(stored.Content) {
		t.Errorf("Content = %s, want the full document", view.Content)
	}
	if view.Version != 4 {
		t.Errorf("Version = %d, want 4", view.Version)
	}
}

func TestGetMissingNoteIsNotFound(t *testing.T) {
	repo := &mockRepo{getByID: func(ctx context.Context, userID, id string) (note.Note, error) {
		return note.Note{}, notFound
	}}
	svc := note.NewService(repo, &mockProjects{}, nil, nil)

	_, err := svc.Get(context.Background(), testUser, "n_missing")

	assertAppErr(t, err, apperror.ErrResourceNotFound, http.StatusNotFound)
}

func TestServiceDelete(t *testing.T) {
	repo := &mockRepo{deleteFn: func(ctx context.Context, userID, id string) (bool, error) {
		return true, nil
	}}
	svc := note.NewService(repo, &mockProjects{}, nil, nil)

	if err := svc.Delete(context.Background(), testUser, testNoteID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

func TestServiceDeleteMissingNoteIsNotFound(t *testing.T) {
	repo := &mockRepo{deleteFn: func(ctx context.Context, userID, id string) (bool, error) {
		return false, nil
	}}
	svc := note.NewService(repo, &mockProjects{}, nil, nil)

	err := svc.Delete(context.Background(), testUser, "n_missing")

	assertAppErr(t, err, apperror.ErrResourceNotFound, http.StatusNotFound)
}

func intPtr(v int) *int { return &v }

// ── real-time events + attachment cleanup (E-053 / NIC-1897) ─────────────────

type spyBroadcaster struct{ events []note.Event }

func (s *spyBroadcaster) Broadcast(_ string, ev note.Event) { s.events = append(s.events, ev) }

type spyCleaner struct {
	calls []string // "ownerType:ownerID"
	err   error
}

func (s *spyCleaner) DeleteAllForOwner(_ context.Context, _, ownerType, ownerID string) error {
	s.calls = append(s.calls, ownerType+":"+ownerID)
	return s.err
}

// AC2 — note.created carries the full list-shaped view.
func TestCreateBroadcastsCreated(t *testing.T) {
	bc := &spyBroadcaster{}
	svc := note.NewService(&mockRepo{create: echoCreate}, &mockProjects{}, nil, bc)

	if _, err := svc.Create(context.Background(), testUser, note.CreateNoteRequest{
		ProjectID: testProject, Title: "t",
		Content: json.RawMessage(`{"type":"doc","content":[{"type":"text","text":"body"}]}`),
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if len(bc.events) != 1 {
		t.Fatalf("emitted %d events, want 1", len(bc.events))
	}
	if bc.events[0].Type != note.EventCreated {
		t.Errorf("type = %q, want %q", bc.events[0].Type, note.EventCreated)
	}
	view, ok := bc.events[0].Payload.(note.NoteView)
	if !ok {
		t.Fatalf("payload = %T, want note.NoteView", bc.events[0].Payload)
	}
	if view.Excerpt != "body" {
		t.Errorf("excerpt = %q, want the flattened text", view.Excerpt)
	}
}

// AC1 — note.updated must never carry the document body. Autosave fires every
// ~1–2s, so a full rich-text payload per save would be wasteful and racy.
func TestUpdateBroadcastsWithoutContent(t *testing.T) {
	bc := &spyBroadcaster{}
	repo := &mockRepo{
		getByID: func(ctx context.Context, userID, id string) (note.Note, error) { return storedNote(1), nil },
		update: func(ctx context.Context, p note.UpdateParams) (note.Note, bool, error) {
			n := storedNote(2)
			n.ContentText = p.ContentText
			return n, true, nil
		},
	}
	svc := note.NewService(repo, &mockProjects{}, nil, bc)

	content := json.RawMessage(`{"type":"doc","content":[{"type":"text","text":"fresh"}]}`)
	if _, err := svc.Update(context.Background(), testUser, testNoteID, note.UpdateNoteRequest{
		Content: &content, Version: intPtr(1),
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	if len(bc.events) != 1 || bc.events[0].Type != note.EventUpdated {
		t.Fatalf("events = %+v, want one note.updated", bc.events)
	}

	// Marshal the payload: the guarantee is about the wire shape, so assert there
	// rather than on the Go type alone.
	raw, err := json.Marshal(bc.events[0].Payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, present := fields["content"]; present {
		t.Errorf("note.updated carries content: %s", raw)
	}
	if _, present := fields["excerpt"]; !present {
		t.Errorf("note.updated has no excerpt: %s", raw)
	}
}

// AC2 — note.deleted carries only { id }.
func TestDeleteBroadcastsID(t *testing.T) {
	bc := &spyBroadcaster{}
	repo := &mockRepo{deleteFn: func(ctx context.Context, userID, id string) (bool, error) { return true, nil }}
	svc := note.NewService(repo, &mockProjects{}, nil, bc)

	if err := svc.Delete(context.Background(), testUser, testNoteID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if len(bc.events) != 1 || bc.events[0].Type != note.EventDeleted {
		t.Fatalf("events = %+v, want one note.deleted", bc.events)
	}
	payload, ok := bc.events[0].Payload.(note.DeletedPayload)
	if !ok {
		t.Fatalf("payload = %T, want note.DeletedPayload", bc.events[0].Payload)
	}
	if payload.ID != testNoteID {
		t.Errorf("id = %q, want %q", payload.ID, testNoteID)
	}
}

// A failed mutation must not announce itself.
func TestFailedMutationsDoNotBroadcast(t *testing.T) {
	t.Run("stale update", func(t *testing.T) {
		bc := &spyBroadcaster{}
		repo := &mockRepo{
			getByID: func(ctx context.Context, userID, id string) (note.Note, error) { return storedNote(7), nil },
			update: func(ctx context.Context, p note.UpdateParams) (note.Note, bool, error) {
				return note.Note{}, false, nil
			},
		}
		svc := note.NewService(repo, &mockProjects{}, nil, bc)

		title := "stale"
		if _, err := svc.Update(context.Background(), testUser, testNoteID, note.UpdateNoteRequest{
			Title: &title, Version: intPtr(6),
		}); err == nil {
			t.Fatal("expected a conflict")
		}
		if len(bc.events) != 0 {
			t.Errorf("a rejected save broadcast %+v", bc.events)
		}
	})

	t.Run("missing delete", func(t *testing.T) {
		bc := &spyBroadcaster{}
		repo := &mockRepo{deleteFn: func(ctx context.Context, userID, id string) (bool, error) { return false, nil }}
		svc := note.NewService(repo, &mockProjects{}, nil, bc)

		if err := svc.Delete(context.Background(), testUser, "n_missing"); err == nil {
			t.Fatal("expected a not-found")
		}
		if len(bc.events) != 0 {
			t.Errorf("a no-op delete broadcast %+v", bc.events)
		}
	})
}

// A nil broadcaster is a valid no-op seam, not a panic.
func TestNilBroadcasterIsSafe(t *testing.T) {
	svc := note.NewService(&mockRepo{create: echoCreate}, &mockProjects{}, nil, nil)

	if _, err := svc.Create(context.Background(), testUser, note.CreateNoteRequest{
		ProjectID: testProject, Title: "t",
	}); err != nil {
		t.Fatalf("Create with a nil broadcaster: %v", err)
	}
}

func TestDeleteCleansAttachments(t *testing.T) {
	cleaner := &spyCleaner{}
	repo := &mockRepo{deleteFn: func(ctx context.Context, userID, id string) (bool, error) { return true, nil }}
	svc := note.NewService(repo, &mockProjects{}, nil, nil).WithCleaner(cleaner)

	if err := svc.Delete(context.Background(), testUser, testNoteID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if len(cleaner.calls) != 1 || cleaner.calls[0] != "note:"+testNoteID {
		t.Errorf("cleaner calls = %v, want one note:%s", cleaner.calls, testNoteID)
	}
}

// AC7 — cleanup is best-effort: a failure never fails the delete that already
// committed, so the caller still sees 204.
func TestDeleteSucceedsWhenCleanupFails(t *testing.T) {
	cleaner := &spyCleaner{err: errors.New("s3 unreachable")}
	bc := &spyBroadcaster{}
	repo := &mockRepo{deleteFn: func(ctx context.Context, userID, id string) (bool, error) { return true, nil }}
	svc := note.NewService(repo, &mockProjects{}, nil, bc).WithCleaner(cleaner)

	if err := svc.Delete(context.Background(), testUser, testNoteID); err != nil {
		t.Fatalf("a failed cleanup blocked the delete: %v", err)
	}
	if len(bc.events) != 1 {
		t.Errorf("the delete did not broadcast: %+v", bc.events)
	}
}

// A note with no attachment feature wired must still delete.
func TestDeleteWithNilCleaner(t *testing.T) {
	repo := &mockRepo{deleteFn: func(ctx context.Context, userID, id string) (bool, error) { return true, nil }}
	svc := note.NewService(repo, &mockProjects{}, nil, nil)

	if err := svc.Delete(context.Background(), testUser, testNoteID); err != nil {
		t.Fatalf("Delete with a nil cleaner: %v", err)
	}
}

// A missing note must not trigger cleanup — there is no owner to clean.
func TestMissingDeleteSkipsCleanup(t *testing.T) {
	cleaner := &spyCleaner{}
	repo := &mockRepo{deleteFn: func(ctx context.Context, userID, id string) (bool, error) { return false, nil }}
	svc := note.NewService(repo, &mockProjects{}, nil, nil).WithCleaner(cleaner)

	if err := svc.Delete(context.Background(), testUser, "n_missing"); err == nil {
		t.Fatal("expected a not-found")
	}
	if len(cleaner.calls) != 0 {
		t.Errorf("cleanup ran for a missing note: %v", cleaner.calls)
	}
}

// ── search mentions ──────────────────────────────────────────────────────────

// AC1 — typeahead returns matches, scoped to the requesting user via the repo call.
func TestSearchMentionsHappyPath(t *testing.T) {
	var gotUser, gotTerm, gotExclude string
	repo := &mockRepo{searchMentions: func(ctx context.Context, userID, term, excludeID string, limit int) ([]note.MentionResult, error) {
		gotUser, gotTerm, gotExclude = userID, term, excludeID
		return []note.MentionResult{
			{ID: "n_1", Title: "Q3 Roadmap"},
			{ID: "n_2", Title: "Q3 Retro"},
		}, nil
	}}
	svc := note.NewService(repo, &mockProjects{}, nil, nil)

	results, err := svc.SearchMentions(context.Background(), testUser, "Q3", "")
	if err != nil {
		t.Fatalf("SearchMentions: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	if gotUser != testUser {
		t.Errorf("repo called with userID = %q, want %q", gotUser, testUser)
	}
	if gotTerm != "Q3" {
		t.Errorf("repo called with term = %q, want %q", gotTerm, "Q3")
	}
	if gotExclude != "" {
		t.Errorf("gotExclude = %q, want empty", gotExclude)
	}
}

// AC2 — self-exclusion: excludeId is passed through to the repository, which
// enforces it in SQL. This test verifies the service does not drop or alter it.
func TestSearchMentionsPassesExcludeID(t *testing.T) {
	var gotExclude string
	repo := &mockRepo{searchMentions: func(ctx context.Context, userID, term, excludeID string, limit int) ([]note.MentionResult, error) {
		gotExclude = excludeID
		return []note.MentionResult{}, nil
	}}
	svc := note.NewService(repo, &mockProjects{}, nil, nil)

	if _, err := svc.SearchMentions(context.Background(), testUser, "Q3", testNoteID); err != nil {
		t.Fatalf("SearchMentions: %v", err)
	}
	if gotExclude != testNoteID {
		t.Errorf("gotExclude = %q, want %q", gotExclude, testNoteID)
	}
}

// Empty term short-circuits before the repository is ever called.
func TestSearchMentionsEmptyTermSkipsRepo(t *testing.T) {
	called := false
	repo := &mockRepo{searchMentions: func(ctx context.Context, userID, term, excludeID string, limit int) ([]note.MentionResult, error) {
		called = true
		return nil, nil
	}}
	svc := note.NewService(repo, &mockProjects{}, nil, nil)

	results, err := svc.SearchMentions(context.Background(), testUser, "   ", "")
	if err != nil {
		t.Fatalf("SearchMentions: %v", err)
	}
	if results == nil {
		t.Error("results = nil, want an empty (non-nil) slice")
	}
	if called {
		t.Error("repo was called for a blank term")
	}
}

// A nil repo result is normalised to an empty slice, never null in the envelope.
func TestSearchMentionsNilResultsNormalised(t *testing.T) {
	repo := &mockRepo{searchMentions: func(ctx context.Context, userID, term, excludeID string, limit int) ([]note.MentionResult, error) {
		return nil, nil
	}}
	svc := note.NewService(repo, &mockProjects{}, nil, nil)

	results, err := svc.SearchMentions(context.Background(), testUser, "Q3", "")
	if err != nil {
		t.Fatalf("SearchMentions: %v", err)
	}
	if results == nil {
		t.Error("results = nil, want an empty (non-nil) slice")
	}
}

// ── backlinks ─────────────────────────────────────────────────────────────────

type mockLinks struct {
	getBacklinks func(ctx context.Context, noteID string) ([]notelink.BacklinkNote, error)
}

func (m *mockLinks) GetBacklinks(ctx context.Context, noteID string) ([]notelink.BacklinkNote, error) {
	return m.getBacklinks(ctx, noteID)
}

// AC1 — happy path: backlinks come back in NoteView shape, no content.
func TestGetBacklinksHappyPath(t *testing.T) {
	repo := &mockRepo{
		getByID: func(ctx context.Context, userID, id string) (note.Note, error) {
			if id == testNoteID {
				n := storedNote(1)
				n.ID = testNoteID
				return n, nil
			}
			n := storedNote(1)
			n.ID = id
			n.Title = "linker " + id
			return n, nil
		},
	}
	links := &mockLinks{getBacklinks: func(ctx context.Context, noteID string) ([]notelink.BacklinkNote, error) {
		return []notelink.BacklinkNote{{ID: "n_a", Title: "A"}, {ID: "n_c", Title: "C"}}, nil
	}}
	svc := note.NewService(repo, &mockProjects{}, links, nil)

	views, err := svc.GetBacklinks(context.Background(), testUser, testNoteID)
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("len(views) = %d, want 2", len(views))
	}
	for _, v := range views {
		if v.Title == "" {
			t.Error("view has no title")
		}
	}
}

// AC2 — no backlinks yields an empty slice, never nil.
func TestGetBacklinksEmpty(t *testing.T) {
	repo := &mockRepo{getByID: func(ctx context.Context, userID, id string) (note.Note, error) {
		return storedNote(1), nil
	}}
	links := &mockLinks{getBacklinks: func(ctx context.Context, noteID string) ([]notelink.BacklinkNote, error) {
		return nil, nil
	}}
	svc := note.NewService(repo, &mockProjects{}, links, nil)

	views, err := svc.GetBacklinks(context.Background(), testUser, testNoteID)
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if views == nil {
		t.Error("views = nil, want an empty (non-nil) slice")
	}
	if len(views) != 0 {
		t.Errorf("len(views) = %d, want 0", len(views))
	}
}

// AC3 — a missing or foreign note is 404, and the link store is never queried.
func TestGetBacklinksNotFound(t *testing.T) {
	queried := false
	repo := &mockRepo{getByID: func(ctx context.Context, userID, id string) (note.Note, error) {
		return note.Note{}, notFound
	}}
	links := &mockLinks{getBacklinks: func(ctx context.Context, noteID string) ([]notelink.BacklinkNote, error) {
		queried = true
		return nil, nil
	}}
	svc := note.NewService(repo, &mockProjects{}, links, nil)

	_, err := svc.GetBacklinks(context.Background(), testUser, "n_foreign")

	assertAppErr(t, err, apperror.ErrResourceNotFound, http.StatusNotFound)
	if queried {
		t.Error("the link store was queried for a note the caller does not own")
	}
}

// A source note that vanished between the link query and the read is skipped,
// not surfaced as an error — the rest of the backlinks must still return.
func TestGetBacklinksSkipsVanishedSource(t *testing.T) {
	repo := &mockRepo{getByID: func(ctx context.Context, userID, id string) (note.Note, error) {
		if id == "n_gone" {
			return note.Note{}, notFound
		}
		n := storedNote(1)
		n.ID = id
		return n, nil
	}}
	links := &mockLinks{getBacklinks: func(ctx context.Context, noteID string) ([]notelink.BacklinkNote, error) {
		return []notelink.BacklinkNote{{ID: "n_gone", Title: "gone"}, {ID: "n_ok", Title: "ok"}}, nil
	}}
	svc := note.NewService(repo, &mockProjects{}, links, nil)

	views, err := svc.GetBacklinks(context.Background(), testUser, testNoteID)
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(views) != 1 {
		t.Fatalf("len(views) = %d, want 1 (the vanished source skipped)", len(views))
	}
}
