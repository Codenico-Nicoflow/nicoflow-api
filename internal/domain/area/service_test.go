package area_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/internal/domain/area"
)

// ── mock repository ───────────────────────────────────────────────────────────

type mockAreaRepo struct {
	countByUser func(ctx context.Context, userID string) (int, error)
	create      func(ctx context.Context, a area.Area) (area.Area, error)
	list        func(ctx context.Context, userID string, f area.ListAreasFilter) ([]area.Area, string, error)
	listWP      func(ctx context.Context, userID string) ([]area.AreaWithProjects, error)
	getByID     func(ctx context.Context, userID, id string) (*area.Area, error)
	update      func(ctx context.Context, userID, id string, req area.UpdateAreaRequest) (area.Area, error)
	delete      func(ctx context.Context, userID, id string) error
	reorder     func(ctx context.Context, userID string, items []area.ReorderItem) (int, error)
}

func (m *mockAreaRepo) CountByUser(ctx context.Context, userID string) (int, error) {
	return m.countByUser(ctx, userID)
}
func (m *mockAreaRepo) Create(ctx context.Context, a area.Area) (area.Area, error) {
	return m.create(ctx, a)
}
func (m *mockAreaRepo) List(ctx context.Context, userID string, f area.ListAreasFilter) ([]area.Area, string, error) {
	return m.list(ctx, userID, f)
}
func (m *mockAreaRepo) ListWithProjects(ctx context.Context, userID string) ([]area.AreaWithProjects, error) {
	return m.listWP(ctx, userID)
}
func (m *mockAreaRepo) GetByID(ctx context.Context, userID, id string) (*area.Area, error) {
	return m.getByID(ctx, userID, id)
}
func (m *mockAreaRepo) Update(ctx context.Context, userID, id string, req area.UpdateAreaRequest) (area.Area, error) {
	return m.update(ctx, userID, id, req)
}
func (m *mockAreaRepo) Delete(ctx context.Context, userID, id string) error {
	return m.delete(ctx, userID, id)
}
func (m *mockAreaRepo) Reorder(ctx context.Context, userID string, items []area.ReorderItem) (int, error) {
	return m.reorder(ctx, userID, items)
}

// ── helpers ───────────────────────────────────────────────────────────────────

func appErr(err error) *apperror.AppError {
	var ae *apperror.AppError
	if errors.As(err, &ae) {
		return ae
	}
	return nil
}

// ── Create tests ─────────────────────────────────────────────────────────────

func TestAreaService_Create(t *testing.T) {
	tests := []struct {
		name       string
		plan       string
		req        area.CreateAreaRequest
		repoCount  int
		repoErr    error
		createArea area.Area
		createErr  error
		wantCode   string
		wantStatus int
		wantName   string
	}{
		{
			name:       "happy path — pro plan",
			plan:       "pro",
			req:        area.CreateAreaRequest{Name: "Work", Color: "#3B82F6", Icon: "folder"},
			createArea: area.Area{ID: "abc", Name: "Work", Color: "#3B82F6", Icon: "folder"},
			wantName:   "Work",
		},
		{
			name:       "happy path — free plan under limit",
			plan:       "free",
			req:        area.CreateAreaRequest{Name: "Work"},
			repoCount:  2,
			createArea: area.Area{ID: "abc", Name: "Work", Color: "#3B82F6", Icon: "folder"},
			wantName:   "Work",
		},
		{
			name:       "applies default color and icon when omitted",
			plan:       "pro",
			req:        area.CreateAreaRequest{Name: "Home"},
			createArea: area.Area{ID: "xyz", Name: "Home", Color: "#3B82F6", Icon: "folder"},
			wantName:   "Home",
		},
		{
			name:       "free plan at limit",
			plan:       "free",
			req:        area.CreateAreaRequest{Name: "Extra"},
			repoCount:  3,
			wantCode:   apperror.ErrPlanLimitExceeded,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "empty name",
			plan:       "pro",
			req:        area.CreateAreaRequest{Name: "   "},
			wantCode:   apperror.ErrInvalidInput,
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "name too long",
			plan:       "pro",
			req:        area.CreateAreaRequest{Name: string(make([]byte, 256))},
			wantCode:   apperror.ErrInvalidInput,
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "invalid hex color",
			plan:       "pro",
			req:        area.CreateAreaRequest{Name: "X", Color: "red"},
			wantCode:   apperror.ErrInvalidInput,
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "invalid icon",
			plan:       "pro",
			req:        area.CreateAreaRequest{Name: "X", Icon: "not-a-real-icon"},
			wantCode:   apperror.ErrInvalidInput,
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "duplicate name from repo",
			plan:       "pro",
			req:        area.CreateAreaRequest{Name: "Work"},
			createErr:  apperror.New(http.StatusConflict, apperror.ErrDuplicateName, "duplicate"),
			wantCode:   apperror.ErrDuplicateName,
			wantStatus: http.StatusConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mockAreaRepo{
				countByUser: func(_ context.Context, _ string) (int, error) {
					return tt.repoCount, tt.repoErr
				},
				create: func(_ context.Context, a area.Area) (area.Area, error) {
					if tt.createErr != nil {
						return area.Area{}, tt.createErr
					}
					return tt.createArea, nil
				},
			}
			svc := area.NewService(repo)
			view, err := svc.Create(context.Background(), "user1", tt.plan, tt.req)

			if tt.wantCode != "" {
				ae := appErr(err)
				if ae == nil {
					t.Fatalf("expected AppError with code %q, got nil error", tt.wantCode)
				}
				if ae.Code != tt.wantCode {
					t.Errorf("code: got %q, want %q", ae.Code, tt.wantCode)
				}
				if ae.Status != tt.wantStatus {
					t.Errorf("status: got %d, want %d", ae.Status, tt.wantStatus)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if view.Name != tt.wantName {
				t.Errorf("name: got %q, want %q", view.Name, tt.wantName)
			}
		})
	}
}

// ── Update tests ─────────────────────────────────────────────────────────────

func TestAreaService_Update(t *testing.T) {
	namePtr := func(s string) *string { return &s }

	tests := []struct {
		name       string
		req        area.UpdateAreaRequest
		repoArea   area.Area
		repoErr    error
		wantCode   string
		wantStatus int
	}{
		{
			name:     "happy path",
			req:      area.UpdateAreaRequest{Name: namePtr("Updated")},
			repoArea: area.Area{ID: "1", Name: "Updated"},
		},
		{
			name:       "not found",
			req:        area.UpdateAreaRequest{Name: namePtr("X")},
			repoErr:    apperror.New(http.StatusNotFound, apperror.ErrAreaNotFound, "not found"),
			wantCode:   apperror.ErrAreaNotFound,
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "empty name",
			req:        area.UpdateAreaRequest{Name: namePtr("  ")},
			wantCode:   apperror.ErrInvalidInput,
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "invalid color",
			req:        area.UpdateAreaRequest{Color: namePtr("blue")},
			wantCode:   apperror.ErrInvalidInput,
			wantStatus: http.StatusUnprocessableEntity,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mockAreaRepo{
				update: func(_ context.Context, _, _ string, _ area.UpdateAreaRequest) (area.Area, error) {
					return tt.repoArea, tt.repoErr
				},
			}
			svc := area.NewService(repo)
			_, err := svc.Update(context.Background(), "user1", "id1", tt.req)

			if tt.wantCode != "" {
				ae := appErr(err)
				if ae == nil {
					t.Fatalf("expected AppError %q, got nil", tt.wantCode)
				}
				if ae.Code != tt.wantCode {
					t.Errorf("code: got %q, want %q", ae.Code, tt.wantCode)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// ── Reorder tests ─────────────────────────────────────────────────────────────

func TestAreaService_Reorder(t *testing.T) {
	tests := []struct {
		name       string
		req        area.ReorderRequest
		repoResult int
		repoErr    error
		wantCode   string
		wantCount  int
	}{
		{
			name:       "happy path",
			req:        area.ReorderRequest{Items: []area.ReorderItem{{ID: "1", DisplayOrder: 0}, {ID: "2", DisplayOrder: 1}}},
			repoResult: 2,
			wantCount:  2,
		},
		{
			name:     "empty items",
			req:      area.ReorderRequest{Items: []area.ReorderItem{}},
			wantCode: apperror.ErrInvalidInput,
		},
		{
			name:     "id not owned",
			req:      area.ReorderRequest{Items: []area.ReorderItem{{ID: "unknown", DisplayOrder: 0}}},
			repoErr:  apperror.New(http.StatusNotFound, apperror.ErrAreaNotFound, "not found"),
			wantCode: apperror.ErrAreaNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mockAreaRepo{
				reorder: func(_ context.Context, _ string, _ []area.ReorderItem) (int, error) {
					return tt.repoResult, tt.repoErr
				},
			}
			svc := area.NewService(repo)
			n, err := svc.Reorder(context.Background(), "user1", tt.req)

			if tt.wantCode != "" {
				ae := appErr(err)
				if ae == nil {
					t.Fatalf("expected AppError %q, got nil", tt.wantCode)
				}
				if ae.Code != tt.wantCode {
					t.Errorf("code: got %q, want %q", ae.Code, tt.wantCode)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if n != tt.wantCount {
				t.Errorf("count: got %d, want %d", n, tt.wantCount)
			}
		})
	}
}
