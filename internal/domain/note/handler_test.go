package note_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/internal/domain/note"
	mw "github.com/nicoflow/nicoflow-api/internal/middleware"
)

// envelope mirrors the project-wide response shape. error is a structured object,
// never a bare string — the client keys off error.code.
type envelope struct {
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// mockService lets the handler tests drive every branch without a database.
type mockService struct {
	listByProject  func(ctx context.Context, userID, projectID string, f note.ListNotesFilter) (note.NoteListView, error)
	get            func(ctx context.Context, userID, id string) (note.NoteDetailView, error)
	create         func(ctx context.Context, userID string, req note.CreateNoteRequest) (note.NoteDetailView, error)
	update         func(ctx context.Context, userID, id string, req note.UpdateNoteRequest) (note.NoteDetailView, error)
	deleteFn       func(ctx context.Context, userID, id string) error
	searchMentions func(ctx context.Context, userID, term, excludeID string) ([]note.MentionResult, error)
	getBacklinks   func(ctx context.Context, userID, id string) ([]note.NoteView, error)
}

func (m *mockService) ListByProject(ctx context.Context, userID, projectID string, f note.ListNotesFilter) (note.NoteListView, error) {
	return m.listByProject(ctx, userID, projectID, f)
}
func (m *mockService) Get(ctx context.Context, userID, id string) (note.NoteDetailView, error) {
	return m.get(ctx, userID, id)
}
func (m *mockService) Create(ctx context.Context, userID string, req note.CreateNoteRequest) (note.NoteDetailView, error) {
	return m.create(ctx, userID, req)
}
func (m *mockService) Update(ctx context.Context, userID, id string, req note.UpdateNoteRequest) (note.NoteDetailView, error) {
	return m.update(ctx, userID, id, req)
}
func (m *mockService) Delete(ctx context.Context, userID, id string) error {
	return m.deleteFn(ctx, userID, id)
}

// WithCleaner is wire-up only; the handler never calls it.
func (m *mockService) WithCleaner(note.AttachmentCleaner) note.Service { return m }

func (m *mockService) SearchMentions(ctx context.Context, userID, term, excludeID string) ([]note.MentionResult, error) {
	return m.searchMentions(ctx, userID, term, excludeID)
}

func (m *mockService) GetBacklinks(ctx context.Context, userID, id string) ([]note.NoteView, error) {
	return m.getBacklinks(ctx, userID, id)
}

// serve routes the request through a chi mux so {id} URL params resolve exactly
// as they do in the real router.
func serve(t *testing.T, svc note.Service, method, target string, body string) *httptest.ResponseRecorder {
	t.Helper()
	h := note.NewHandler(svc)

	r := chi.NewRouter()
	r.Get("/notes", h.List)
	r.Get("/notes/search", h.SearchMentions)
	r.Post("/notes", h.Create)
	r.Get("/notes/{id}", h.Get)
	r.Patch("/notes/{id}", h.Update)
	r.Delete("/notes/{id}", h.Delete)
	r.Get("/notes/{id}/backlinks", h.GetBacklinks)

	var reader *bytes.Reader
	if body == "" {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader([]byte(body))
	}
	req := httptest.NewRequest(method, target, reader)
	req = req.WithContext(mw.WithAuth(req.Context(), testUser, "free"))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func decodeEnvelope(t *testing.T, rec *httptest.ResponseRecorder) envelope {
	t.Helper()
	var env envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope from %q: %v", rec.Body.String(), err)
	}
	return env
}

func detailView() note.NoteDetailView {
	pid := testProject
	return note.NoteDetailView{
		ID: testNoteID, ProjectID: &pid, Title: "t",
		Content: json.RawMessage(note.EmptyDoc), Version: 1,
	}
}

func TestHandlerCreateReturns201(t *testing.T) {
	svc := &mockService{create: func(ctx context.Context, userID string, req note.CreateNoteRequest) (note.NoteDetailView, error) {
		return detailView(), nil
	}}

	rec := serve(t, svc, http.MethodPost, "/notes", `{"projectId":"p_1","title":"t"}`)

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", rec.Code)
	}
	env := decodeEnvelope(t, rec)
	if env.Error != nil {
		t.Errorf("error = %+v, want null", env.Error)
	}
	var view note.NoteDetailView
	if err := json.Unmarshal(env.Data, &view); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if view.ID != testNoteID {
		t.Errorf("id = %q, want %q", view.ID, testNoteID)
	}
}

// AC7 — a body over the cap is refused as 422 and never reaches the service.
func TestHandlerRejectsOversizedBody(t *testing.T) {
	reached := false
	svc := &mockService{create: func(ctx context.Context, userID string, req note.CreateNoteRequest) (note.NoteDetailView, error) {
		reached = true
		return detailView(), nil
	}}

	// One byte of text past the cap, wrapped in valid JSON.
	huge := `{"projectId":"p_1","title":"t","content":{"type":"doc","text":"` +
		strings.Repeat("a", int(note.MaxRequestBytes)) + `"}}`

	rec := serve(t, svc, http.MethodPost, "/notes", huge)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", rec.Code)
	}
	env := decodeEnvelope(t, rec)
	if env.Error == nil || env.Error.Code != apperror.ErrInvalidInput {
		t.Errorf("error = %+v, want %s", env.Error, apperror.ErrInvalidInput)
	}
	if reached {
		t.Error("an oversized body reached the service")
	}
}

func TestHandlerAcceptsBodyUnderCap(t *testing.T) {
	svc := &mockService{create: func(ctx context.Context, userID string, req note.CreateNoteRequest) (note.NoteDetailView, error) {
		return detailView(), nil
	}}

	// Comfortably inside the cap — proves the limit doesn't reject normal notes.
	body := `{"projectId":"p_1","title":"t","content":{"type":"doc","text":"` +
		strings.Repeat("a", 1000) + `"}}`

	if rec := serve(t, svc, http.MethodPost, "/notes", body); rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", rec.Code)
	}
}

func TestHandlerMalformedBodyIs422(t *testing.T) {
	svc := &mockService{create: func(ctx context.Context, userID string, req note.CreateNoteRequest) (note.NoteDetailView, error) {
		return detailView(), nil
	}}

	rec := serve(t, svc, http.MethodPost, "/notes", `{"projectId":`)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", rec.Code)
	}
	env := decodeEnvelope(t, rec)
	if env.Error == nil || env.Error.Code != apperror.ErrInvalidInput {
		t.Errorf("error = %+v, want %s", env.Error, apperror.ErrInvalidInput)
	}
}

// An unknown field is rejected rather than silently dropped — this is what stops
// a client's contentText from being quietly accepted and then ignored.
func TestHandlerRejectsUnknownFields(t *testing.T) {
	svc := &mockService{create: func(ctx context.Context, userID string, req note.CreateNoteRequest) (note.NoteDetailView, error) {
		return detailView(), nil
	}}

	rec := serve(t, svc, http.MethodPost, "/notes", `{"projectId":"p_1","title":"t","contentText":"injected"}`)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422 for a client-sent contentText", rec.Code)
	}
}

// AC4 at the HTTP boundary — a stale version surfaces as 409 CONFLICT.
func TestHandlerStaleUpdateIs409(t *testing.T) {
	svc := &mockService{update: func(ctx context.Context, userID, id string, req note.UpdateNoteRequest) (note.NoteDetailView, error) {
		return note.NoteDetailView{}, apperror.New(http.StatusConflict, apperror.ErrConflict, "note was modified by another session")
	}}

	rec := serve(t, svc, http.MethodPatch, "/notes/n_1", `{"title":"x","version":6}`)

	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", rec.Code)
	}
	env := decodeEnvelope(t, rec)
	if env.Error == nil || env.Error.Code != apperror.ErrConflict {
		t.Errorf("error = %+v, want %s", env.Error, apperror.ErrConflict)
	}
}

func TestHandlerNotFoundIs404(t *testing.T) {
	svc := &mockService{get: func(ctx context.Context, userID, id string) (note.NoteDetailView, error) {
		return note.NoteDetailView{}, notFound
	}}

	rec := serve(t, svc, http.MethodGet, "/notes/n_missing", "")

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	env := decodeEnvelope(t, rec)
	if env.Error == nil || env.Error.Code != apperror.ErrResourceNotFound {
		t.Errorf("error = %+v, want %s", env.Error, apperror.ErrResourceNotFound)
	}
}

func TestHandlerDeleteReturns204(t *testing.T) {
	svc := &mockService{deleteFn: func(ctx context.Context, userID, id string) error { return nil }}

	rec := serve(t, svc, http.MethodDelete, "/notes/n_1", "")

	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body = %q, want empty", rec.Body.String())
	}
}

// The projectId query param is forwarded verbatim; the service owns validation.
func TestHandlerListPassesProjectID(t *testing.T) {
	var got string
	svc := &mockService{listByProject: func(ctx context.Context, userID, projectID string, f note.ListNotesFilter) (note.NoteListView, error) {
		got = projectID
		return note.NoteListView{Items: []note.NoteView{}}, nil
	}}

	rec := serve(t, svc, http.MethodGet, "/notes?projectId=p_abc", "")

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if got != "p_abc" {
		t.Errorf("projectId = %q, want %q", got, "p_abc")
	}
}

// An empty list must serialize as [] rather than null, so the client can render
// it without a nil check.
func TestHandlerEmptyListIsArray(t *testing.T) {
	svc := &mockService{listByProject: func(ctx context.Context, userID, projectID string, f note.ListNotesFilter) (note.NoteListView, error) {
		return note.NoteListView{Items: []note.NoteView{}}, nil
	}}

	rec := serve(t, svc, http.MethodGet, "/notes?projectId=p_1", "")

	env := decodeEnvelope(t, rec)
	// data is now a NoteListView object; items must be [] not null.
	var listView struct {
		Items json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(env.Data, &listView); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if string(listView.Items) != "[]" {
		t.Errorf("items = %s, want []", listView.Items)
	}
}

func TestHandlerInternalErrorIs500(t *testing.T) {
	svc := &mockService{get: func(ctx context.Context, userID, id string) (note.NoteDetailView, error) {
		return note.NoteDetailView{}, context.DeadlineExceeded
	}}

	rec := serve(t, svc, http.MethodGet, "/notes/n_1", "")

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	env := decodeEnvelope(t, rec)
	if env.Error == nil || env.Error.Code != apperror.ErrInternalServerError {
		t.Errorf("error = %+v, want %s", env.Error, apperror.ErrInternalServerError)
	}
	// A raw Go error must never reach the client.
	if strings.Contains(env.Error.Message, "deadline") {
		t.Errorf("message leaks the internal error: %q", env.Error.Message)
	}
}

func TestHandlerSearchMentions(t *testing.T) {
	var gotTerm, gotExclude string
	svc := &mockService{searchMentions: func(ctx context.Context, userID, term, excludeID string) ([]note.MentionResult, error) {
		gotTerm, gotExclude = term, excludeID
		return []note.MentionResult{{ID: "n_2", Title: "Q3 Retro"}}, nil
	}}

	rec := serve(t, svc, http.MethodGet, "/notes/search?q=Q3&excludeId=n_1", "")

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if gotTerm != "Q3" {
		t.Errorf("q passed to service = %q, want %q", gotTerm, "Q3")
	}
	if gotExclude != "n_1" {
		t.Errorf("excludeId passed to service = %q, want %q", gotExclude, "n_1")
	}

	env := decodeEnvelope(t, rec)
	var results []note.MentionResult
	if err := json.Unmarshal(env.Data, &results); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if len(results) != 1 || results[0].ID != "n_2" {
		t.Errorf("results = %+v, want one match with id n_2", results)
	}
}

func TestHandlerSearchMentionsEmptyResultsAreEmptyArray(t *testing.T) {
	svc := &mockService{searchMentions: func(ctx context.Context, userID, term, excludeID string) ([]note.MentionResult, error) {
		return []note.MentionResult{}, nil
	}}

	rec := serve(t, svc, http.MethodGet, "/notes/search?q=zz", "")

	env := decodeEnvelope(t, rec)
	if string(env.Data) != "[]" {
		t.Errorf("data = %s, want []", env.Data)
	}
}

func TestHandlerGetBacklinks(t *testing.T) {
	svc := &mockService{getBacklinks: func(ctx context.Context, userID, id string) ([]note.NoteView, error) {
		return []note.NoteView{{ID: "n_a", Title: "A"}, {ID: "n_c", Title: "C"}}, nil
	}}

	rec := serve(t, svc, http.MethodGet, "/notes/n_b/backlinks", "")

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	env := decodeEnvelope(t, rec)
	var views []note.NoteView
	if err := json.Unmarshal(env.Data, &views); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if len(views) != 2 {
		t.Errorf("len(views) = %d, want 2", len(views))
	}
}

func TestHandlerGetBacklinksEmptyIsEmptyArray(t *testing.T) {
	svc := &mockService{getBacklinks: func(ctx context.Context, userID, id string) ([]note.NoteView, error) {
		return []note.NoteView{}, nil
	}}

	rec := serve(t, svc, http.MethodGet, "/notes/n_b/backlinks", "")

	env := decodeEnvelope(t, rec)
	if string(env.Data) != "[]" {
		t.Errorf("data = %s, want []", env.Data)
	}
}

func TestHandlerGetBacklinksNotFoundIs404(t *testing.T) {
	svc := &mockService{getBacklinks: func(ctx context.Context, userID, id string) ([]note.NoteView, error) {
		return nil, apperror.New(http.StatusNotFound, apperror.ErrResourceNotFound, "note not found")
	}}

	rec := serve(t, svc, http.MethodGet, "/notes/n_missing/backlinks", "")

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	env := decodeEnvelope(t, rec)
	if env.Error == nil || env.Error.Code != apperror.ErrResourceNotFound {
		t.Errorf("error = %+v, want %s", env.Error, apperror.ErrResourceNotFound)
	}
}
