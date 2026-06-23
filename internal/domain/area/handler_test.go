package area_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/internal/domain/area"
	mw "github.com/nicoflow/nicoflow-api/internal/middleware"
)

// mockService implements area.Service for handler-level tests. Each method
// delegates to an optional func field; nil fields fall back to a benign default
// so a test only wires the behaviour it cares about.
type mockService struct {
	listFn    func(ctx context.Context, userID string, f area.ListAreasFilter) (area.ListAreasResponse, error)
	listWPFn  func(ctx context.Context, userID string) ([]area.AreaWithProjectsView, error)
	getFn     func(ctx context.Context, userID, id string) (area.AreaView, error)
	createFn  func(ctx context.Context, userID, plan string, req area.CreateAreaRequest) (area.AreaView, error)
	updateFn  func(ctx context.Context, userID, id string, req area.UpdateAreaRequest) (area.AreaView, error)
	deleteFn  func(ctx context.Context, userID, id string) error
	reorderFn func(ctx context.Context, userID string, req area.ReorderRequest) (int, error)
}

func (m *mockService) List(ctx context.Context, userID string, f area.ListAreasFilter) (area.ListAreasResponse, error) {
	if m.listFn != nil {
		return m.listFn(ctx, userID, f)
	}
	return area.ListAreasResponse{}, nil
}
func (m *mockService) ListWithProjects(ctx context.Context, userID string) ([]area.AreaWithProjectsView, error) {
	if m.listWPFn != nil {
		return m.listWPFn(ctx, userID)
	}
	return nil, nil
}
func (m *mockService) Get(ctx context.Context, userID, id string) (area.AreaView, error) {
	if m.getFn != nil {
		return m.getFn(ctx, userID, id)
	}
	return area.AreaView{}, nil
}
func (m *mockService) Create(ctx context.Context, userID, plan string, req area.CreateAreaRequest) (area.AreaView, error) {
	if m.createFn != nil {
		return m.createFn(ctx, userID, plan, req)
	}
	return area.AreaView{}, nil
}
func (m *mockService) Update(ctx context.Context, userID, id string, req area.UpdateAreaRequest) (area.AreaView, error) {
	if m.updateFn != nil {
		return m.updateFn(ctx, userID, id, req)
	}
	return area.AreaView{}, nil
}
func (m *mockService) Delete(ctx context.Context, userID, id string) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, userID, id)
	}
	return nil
}
func (m *mockService) Reorder(ctx context.Context, userID string, req area.ReorderRequest) (int, error) {
	if m.reorderFn != nil {
		return m.reorderFn(ctx, userID, req)
	}
	return 0, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

// envelope mirrors the pkg/respond response shape so tests can decode either branch.
type envelope struct {
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// authReq builds a request whose context carries an authenticated user + plan,
// mirroring what the Auth middleware injects in production.
func authReq(method, target, plan string, body []byte) *http.Request {
	var r *http.Request
	if body != nil {
		r = httptest.NewRequest(method, target, bytes.NewReader(body))
	} else {
		r = httptest.NewRequest(method, target, nil)
	}
	r = r.WithContext(mw.WithAuth(r.Context(), "user1", plan))
	return r
}

// withURLParam attaches a chi route param to the request (handlers read path
// params via chi.URLParam).
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
	if string(e.Data) != "null" && len(e.Data) != 0 {
		t.Errorf("error response must have null data, got %s", e.Data)
	}
}

// ── Create ────────────────────────────────────────────────────────────────────

func TestHandler_Create(t *testing.T) {
	t.Run("malformed JSON body → 400 INVALID_INPUT", func(t *testing.T) {
		h := area.NewHandler(&mockService{})
		r := authReq(http.MethodPost, "/v1/areas", "free", []byte("{not json"))
		w := httptest.NewRecorder()

		h.Create(w, r)

		assertErr(t, w, http.StatusBadRequest, apperror.ErrInvalidInput)
	})

	t.Run("success → 201 with area view, error null", func(t *testing.T) {
		h := area.NewHandler(&mockService{
			createFn: func(_ context.Context, userID, plan string, req area.CreateAreaRequest) (area.AreaView, error) {
				if userID != "user1" || plan != "free" {
					t.Errorf("ctx not propagated: userID=%q plan=%q", userID, plan)
				}
				return area.AreaView{ID: "a1", Name: req.Name, Color: "#10B981", Icon: "folder"}, nil
			},
		})
		body, _ := json.Marshal(area.CreateAreaRequest{Name: "Health", Color: "#10B981", Icon: "folder"})
		r := authReq(http.MethodPost, "/v1/areas", "free", body)
		w := httptest.NewRecorder()

		h.Create(w, r)

		if w.Code != http.StatusCreated {
			t.Fatalf("status: got %d, want 201 (body %s)", w.Code, w.Body.String())
		}
		e := decode(t, w)
		if e.Error != nil {
			t.Errorf("error must be null on success, got %+v", e.Error)
		}
		var view area.AreaView
		if err := json.Unmarshal(e.Data, &view); err != nil {
			t.Fatalf("decode data: %v", err)
		}
		if view.ID != "a1" || view.Name != "Health" {
			t.Errorf("unexpected view: %+v", view)
		}
	})

	t.Run("service plan-limit error → 403 PLAN_LIMIT_EXCEEDED", func(t *testing.T) {
		h := area.NewHandler(&mockService{
			createFn: func(_ context.Context, _, _ string, _ area.CreateAreaRequest) (area.AreaView, error) {
				return area.AreaView{}, apperror.New(http.StatusForbidden, apperror.ErrPlanLimitExceeded, "free plan allows up to 3 areas")
			},
		})
		body, _ := json.Marshal(area.CreateAreaRequest{Name: "Extra"})
		r := authReq(http.MethodPost, "/v1/areas", "free", body)
		w := httptest.NewRecorder()

		h.Create(w, r)

		assertErr(t, w, http.StatusForbidden, apperror.ErrPlanLimitExceeded)
	})

	t.Run("unexpected (non-AppError) → 500 INTERNAL_SERVER_ERROR", func(t *testing.T) {
		h := area.NewHandler(&mockService{
			createFn: func(_ context.Context, _, _ string, _ area.CreateAreaRequest) (area.AreaView, error) {
				return area.AreaView{}, context.DeadlineExceeded
			},
		})
		body, _ := json.Marshal(area.CreateAreaRequest{Name: "X"})
		r := authReq(http.MethodPost, "/v1/areas", "pro", body)
		w := httptest.NewRecorder()

		h.Create(w, r)

		assertErr(t, w, http.StatusInternalServerError, apperror.ErrInternalServerError)
	})
}

// ── Get / Update / Delete (path-param handlers) ────────────────────────────────

func TestHandler_Get_CrossUserNotFound(t *testing.T) {
	h := area.NewHandler(&mockService{
		getFn: func(_ context.Context, _, _ string) (area.AreaView, error) {
			return area.AreaView{}, apperror.New(http.StatusNotFound, apperror.ErrAreaNotFound, "not found")
		},
	})
	r := withURLParam(authReq(http.MethodGet, "/v1/areas/other", "pro", nil), "id", "other")
	w := httptest.NewRecorder()

	h.Get(w, r)

	assertErr(t, w, http.StatusNotFound, apperror.ErrAreaNotFound)
}

func TestHandler_Update(t *testing.T) {
	t.Run("malformed body → 400 INVALID_INPUT", func(t *testing.T) {
		h := area.NewHandler(&mockService{})
		r := withURLParam(authReq(http.MethodPatch, "/v1/areas/a1", "pro", []byte("nope")), "id", "a1")
		w := httptest.NewRecorder()

		h.Update(w, r)

		assertErr(t, w, http.StatusBadRequest, apperror.ErrInvalidInput)
	})

	t.Run("success → 200 with updated view", func(t *testing.T) {
		h := area.NewHandler(&mockService{
			updateFn: func(_ context.Context, _, id string, _ area.UpdateAreaRequest) (area.AreaView, error) {
				return area.AreaView{ID: id, Name: "Renamed"}, nil
			},
		})
		body, _ := json.Marshal(area.UpdateAreaRequest{})
		r := withURLParam(authReq(http.MethodPatch, "/v1/areas/a1", "pro", body), "id", "a1")
		w := httptest.NewRecorder()

		h.Update(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("status: got %d, want 200", w.Code)
		}
		var view area.AreaView
		json.Unmarshal(decode(t, w).Data, &view)
		if view.Name != "Renamed" {
			t.Errorf("name: got %q", view.Name)
		}
	})
}

func TestHandler_Delete_NoContent(t *testing.T) {
	h := area.NewHandler(&mockService{
		deleteFn: func(_ context.Context, _, _ string) error { return nil },
	})
	r := withURLParam(authReq(http.MethodDelete, "/v1/areas/a1", "pro", nil), "id", "a1")
	w := httptest.NewRecorder()

	h.Delete(w, r)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status: got %d, want 204", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Errorf("204 must have empty body, got %q", w.Body.String())
	}
}

// ── List (query-param validation) ──────────────────────────────────────────────

func TestHandler_List_BadLimit(t *testing.T) {
	h := area.NewHandler(&mockService{})
	r := authReq(http.MethodGet, "/v1/areas?limit=999", "pro", nil)
	w := httptest.NewRecorder()

	h.List(w, r)

	assertErr(t, w, http.StatusBadRequest, apperror.ErrInvalidInput)
}

// ── Reorder ────────────────────────────────────────────────────────────────────

func TestHandler_Reorder(t *testing.T) {
	t.Run("malformed body → 400 INVALID_INPUT", func(t *testing.T) {
		h := area.NewHandler(&mockService{})
		r := authReq(http.MethodPatch, "/v1/areas/reorder", "pro", []byte("["))
		w := httptest.NewRecorder()

		h.Reorder(w, r)

		assertErr(t, w, http.StatusBadRequest, apperror.ErrInvalidInput)
	})

	t.Run("success → 200 with updated count", func(t *testing.T) {
		h := area.NewHandler(&mockService{
			reorderFn: func(_ context.Context, _ string, req area.ReorderRequest) (int, error) {
				return len(req.Items), nil
			},
		})
		body, _ := json.Marshal(area.ReorderRequest{Items: []area.ReorderItem{{ID: "1", DisplayOrder: 0}, {ID: "2", DisplayOrder: 1}}})
		r := authReq(http.MethodPatch, "/v1/areas/reorder", "pro", body)
		w := httptest.NewRecorder()

		h.Reorder(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("status: got %d, want 200", w.Code)
		}
		var data map[string]int
		json.Unmarshal(decode(t, w).Data, &data)
		if data["updated"] != 2 {
			t.Errorf("updated: got %d, want 2", data["updated"])
		}
	})
}
