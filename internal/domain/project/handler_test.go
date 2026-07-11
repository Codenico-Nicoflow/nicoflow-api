package project_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/internal/domain/project"
	mw "github.com/nicoflow/nicoflow-api/internal/middleware"
)

// mockService implements project.Service for handler-level tests. Each method
// delegates to an optional func field; nil fields fall back to a benign default.
type mockService struct {
	listFn       func(ctx context.Context, userID string, f project.ListProjectsFilter) (project.ListProjectsResponse, error)
	listByAreaFn func(ctx context.Context, userID, areaID string, f project.ListProjectsFilter) (project.ListProjectsResponse, error)
	getFn        func(ctx context.Context, userID, id string) (project.ProjectView, error)
	createFn     func(ctx context.Context, userID, areaID, plan string, req project.CreateProjectRequest) (project.ProjectView, error)
	updateFn     func(ctx context.Context, userID, id string, req project.UpdateProjectRequest) (project.ProjectView, error)
	deleteFn     func(ctx context.Context, userID, id string) error
	reorderFn    func(ctx context.Context, userID string, req project.ReorderRequest) (int, error)
}

func (m *mockService) List(ctx context.Context, userID string, f project.ListProjectsFilter) (project.ListProjectsResponse, error) {
	if m.listFn != nil {
		return m.listFn(ctx, userID, f)
	}
	return project.ListProjectsResponse{}, nil
}
func (m *mockService) ListByArea(ctx context.Context, userID, areaID string, f project.ListProjectsFilter) (project.ListProjectsResponse, error) {
	if m.listByAreaFn != nil {
		return m.listByAreaFn(ctx, userID, areaID, f)
	}
	return project.ListProjectsResponse{}, nil
}
func (m *mockService) Get(ctx context.Context, userID, id string) (project.ProjectView, error) {
	if m.getFn != nil {
		return m.getFn(ctx, userID, id)
	}
	return project.ProjectView{}, nil
}
func (m *mockService) Create(ctx context.Context, userID, areaID, plan string, req project.CreateProjectRequest) (project.ProjectView, error) {
	if m.createFn != nil {
		return m.createFn(ctx, userID, areaID, plan, req)
	}
	return project.ProjectView{}, nil
}
func (m *mockService) Update(ctx context.Context, userID, id string, req project.UpdateProjectRequest) (project.ProjectView, error) {
	if m.updateFn != nil {
		return m.updateFn(ctx, userID, id, req)
	}
	return project.ProjectView{}, nil
}
func (m *mockService) Delete(ctx context.Context, userID, id string) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, userID, id)
	}
	return nil
}
func (m *mockService) Reorder(ctx context.Context, userID string, req project.ReorderRequest) (int, error) {
	if m.reorderFn != nil {
		return m.reorderFn(ctx, userID, req)
	}
	return 0, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

type envelope struct {
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func authReq(method, target, plan string, body []byte) *http.Request {
	var r *http.Request
	if body != nil {
		r = httptest.NewRequest(method, target, bytes.NewReader(body))
	} else {
		r = httptest.NewRequest(method, target, nil)
	}
	return r.WithContext(mw.WithAuth(r.Context(), "user1", plan))
}

func withURLParam(r *http.Request, key, val string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, val)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func decode(t *testing.T, w *httptest.ResponseRecorder) envelope {
	t.Helper()
	var e envelope
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatalf("decode body %q: %v", w.Body.String(), err)
	}
	return e
}

func assertErr(t *testing.T, w *httptest.ResponseRecorder, wantStatus int, wantCode string) {
	t.Helper()
	if w.Code != wantStatus {
		t.Errorf("status: got %d, want %d (body %s)", w.Code, wantStatus, w.Body.String())
	}
	e := decode(t, w)
	if e.Error == nil {
		t.Fatalf("expected error envelope, got %s", w.Body.String())
	}
	if e.Error.Code != wantCode {
		t.Errorf("error.code: got %q, want %q", e.Error.Code, wantCode)
	}
}

// ── Create ────────────────────────────────────────────────────────────────────

func TestHandler_Create(t *testing.T) {
	t.Run("malformed JSON body → 400 INVALID_INPUT", func(t *testing.T) {
		h := project.NewHandler(&mockService{})
		r := withURLParam(authReq(http.MethodPost, "/v1/areas/a1/projects", "free", []byte("{bad")), "areaId", "a1")
		w := httptest.NewRecorder()

		h.Create(w, r)

		assertErr(t, w, http.StatusBadRequest, apperror.ErrInvalidInput)
	})

	t.Run("success → 201, areaId propagated, full IProject shape", func(t *testing.T) {
		h := project.NewHandler(&mockService{
			createFn: func(_ context.Context, userID, areaID, plan string, req project.CreateProjectRequest) (project.ProjectView, error) {
				if areaID != "a1" {
					t.Errorf("areaId path param not propagated: got %q", areaID)
				}
				if userID != "user1" || plan != "free" {
					t.Errorf("ctx not propagated: userID=%q plan=%q", userID, plan)
				}
				return project.ProjectView{ID: "p1", AreaID: areaID, Name: req.Name, Status: "active", FolderIcon: "folder"}, nil
			},
		})
		body, _ := json.Marshal(project.CreateProjectRequest{Name: "Q3 Launch", Status: "active"})
		r := withURLParam(authReq(http.MethodPost, "/v1/areas/a1/projects", "free", body), "areaId", "a1")
		w := httptest.NewRecorder()

		h.Create(w, r)

		if w.Code != http.StatusCreated {
			t.Fatalf("status: got %d, want 201 (body %s)", w.Code, w.Body.String())
		}
		e := decode(t, w)
		if e.Error != nil {
			t.Errorf("error must be null, got %+v", e.Error)
		}
		var view project.ProjectView
		if err := json.Unmarshal(e.Data, &view); err != nil {
			t.Fatalf("decode data: %v", err)
		}
		if view.ID != "p1" || view.AreaID != "a1" {
			t.Errorf("unexpected view: %+v", view)
		}
	})

	t.Run("plan-limit error → 403 PLAN_LIMIT_EXCEEDED", func(t *testing.T) {
		h := project.NewHandler(&mockService{
			createFn: func(_ context.Context, _, _, _ string, _ project.CreateProjectRequest) (project.ProjectView, error) {
				return project.ProjectView{}, apperror.New(http.StatusForbidden, apperror.ErrPlanLimitExceeded, "free plan allows up to 5 projects")
			},
		})
		body, _ := json.Marshal(project.CreateProjectRequest{Name: "Sixth"})
		r := withURLParam(authReq(http.MethodPost, "/v1/areas/a1/projects", "free", body), "areaId", "a1")
		w := httptest.NewRecorder()

		h.Create(w, r)

		assertErr(t, w, http.StatusForbidden, apperror.ErrPlanLimitExceeded)
	})

	t.Run("cross-user areaId → 404 PROJECT_NOT_FOUND", func(t *testing.T) {
		h := project.NewHandler(&mockService{
			createFn: func(_ context.Context, _, _, _ string, _ project.CreateProjectRequest) (project.ProjectView, error) {
				return project.ProjectView{}, apperror.New(http.StatusNotFound, apperror.ErrProjectNotFound, "not found")
			},
		})
		body, _ := json.Marshal(project.CreateProjectRequest{Name: "X"})
		r := withURLParam(authReq(http.MethodPost, "/v1/areas/other/projects", "pro", body), "areaId", "other")
		w := httptest.NewRecorder()

		h.Create(w, r)

		assertErr(t, w, http.StatusNotFound, apperror.ErrProjectNotFound)
	})
}

// ── List filter validation ─────────────────────────────────────────────────────

func TestHandler_List_InvalidStatus(t *testing.T) {
	h := project.NewHandler(&mockService{})
	r := authReq(http.MethodGet, "/v1/projects?status=unknown", "pro", nil)
	w := httptest.NewRecorder()

	h.List(w, r)

	assertErr(t, w, http.StatusBadRequest, apperror.ErrInvalidStatus)
}

func TestHandler_List_BadFavorite(t *testing.T) {
	h := project.NewHandler(&mockService{})
	r := authReq(http.MethodGet, "/v1/projects?isFavorite=maybe", "pro", nil)
	w := httptest.NewRecorder()

	h.List(w, r)

	assertErr(t, w, http.StatusBadRequest, apperror.ErrInvalidInput)
}

// ── Get / Update / Delete ──────────────────────────────────────────────────────

func TestHandler_Get_CrossUserNotFound(t *testing.T) {
	h := project.NewHandler(&mockService{
		getFn: func(_ context.Context, _, _ string) (project.ProjectView, error) {
			return project.ProjectView{}, apperror.New(http.StatusNotFound, apperror.ErrProjectNotFound, "not found")
		},
	})
	r := withURLParam(authReq(http.MethodGet, "/v1/projects/other", "pro", nil), "id", "other")
	w := httptest.NewRecorder()

	h.Get(w, r)

	assertErr(t, w, http.StatusNotFound, apperror.ErrProjectNotFound)
}

func TestHandler_Update_MoveArea(t *testing.T) {
	h := project.NewHandler(&mockService{
		updateFn: func(_ context.Context, _, id string, req project.UpdateProjectRequest) (project.ProjectView, error) {
			// A project always belongs to an area; areaId carries the target area to move into.
			if req.AreaID == nil {
				t.Fatal("areaId should have decoded to a non-nil pointer")
			}
			return project.ProjectView{ID: id, AreaID: *req.AreaID, Name: "Moved"}, nil
		},
	})
	r := withURLParam(authReq(http.MethodPatch, "/v1/projects/p1", "pro", []byte(`{"areaId":"a2"}`)), "id", "p1")
	w := httptest.NewRecorder()

	h.Update(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var view project.ProjectView
	json.Unmarshal(decode(t, w).Data, &view)
	if view.AreaID != "a2" {
		t.Errorf("areaId: got %q, want a2", view.AreaID)
	}
}

func TestHandler_Delete_NoContent(t *testing.T) {
	h := project.NewHandler(&mockService{})
	r := withURLParam(authReq(http.MethodDelete, "/v1/projects/p1", "pro", nil), "id", "p1")
	w := httptest.NewRecorder()

	h.Delete(w, r)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status: got %d, want 204", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Errorf("204 must have empty body, got %q", w.Body.String())
	}
}

// ── Reorder ────────────────────────────────────────────────────────────────────

func TestHandler_Reorder_Success(t *testing.T) {
	h := project.NewHandler(&mockService{
		reorderFn: func(_ context.Context, _ string, req project.ReorderRequest) (int, error) {
			return len(req.Items), nil
		},
	})
	body, _ := json.Marshal(project.ReorderRequest{Items: []project.ReorderItem{{ID: "1", DisplayOrder: 0}, {ID: "2", DisplayOrder: 1}, {ID: "3", DisplayOrder: 2}}})
	r := authReq(http.MethodPatch, "/v1/projects/reorder", "pro", body)
	w := httptest.NewRecorder()

	h.Reorder(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var data map[string]int
	json.Unmarshal(decode(t, w).Data, &data)
	if data["updated"] != 3 {
		t.Errorf("updated: got %d, want 3", data["updated"])
	}
}
