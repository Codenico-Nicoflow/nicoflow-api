package project_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/internal/domain/project"
)

// ── mock repository ───────────────────────────────────────────────────────────

type mockProjectRepo struct {
	countByUser func(ctx context.Context, userID string) (int, error)
	create      func(ctx context.Context, p project.Project) (project.Project, error)
	list        func(ctx context.Context, userID string, f project.ListProjectsFilter) ([]project.Project, string, error)
	listByArea  func(ctx context.Context, userID, areaID string, f project.ListProjectsFilter) ([]project.Project, string, error)
	getByID     func(ctx context.Context, userID, id string) (*project.Project, error)
	update      func(ctx context.Context, userID, id string, req project.UpdateProjectRequest) (project.Project, error)
	delete      func(ctx context.Context, userID, id string) error
	reorder     func(ctx context.Context, userID string, items []project.ReorderItem) (int, error)
}

func (m *mockProjectRepo) CountByUser(ctx context.Context, userID string) (int, error) {
	return m.countByUser(ctx, userID)
}
func (m *mockProjectRepo) Create(ctx context.Context, p project.Project) (project.Project, error) {
	return m.create(ctx, p)
}
func (m *mockProjectRepo) List(ctx context.Context, userID string, f project.ListProjectsFilter) ([]project.Project, string, error) {
	return m.list(ctx, userID, f)
}
func (m *mockProjectRepo) ListByArea(ctx context.Context, userID, areaID string, f project.ListProjectsFilter) ([]project.Project, string, error) {
	return m.listByArea(ctx, userID, areaID, f)
}
func (m *mockProjectRepo) GetByID(ctx context.Context, userID, id string) (*project.Project, error) {
	return m.getByID(ctx, userID, id)
}
func (m *mockProjectRepo) Update(ctx context.Context, userID, id string, req project.UpdateProjectRequest) (project.Project, error) {
	return m.update(ctx, userID, id, req)
}
func (m *mockProjectRepo) Delete(ctx context.Context, userID, id string) error {
	return m.delete(ctx, userID, id)
}
func (m *mockProjectRepo) Reorder(ctx context.Context, userID string, items []project.ReorderItem) (int, error) {
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

func strPtr(s string) *string { return &s }

// ── Create tests ─────────────────────────────────────────────────────────────

func TestProjectService_Create(t *testing.T) {
	tests := []struct {
		name          string
		plan          string
		req           project.CreateProjectRequest
		repoCount     int
		repoCountErr  error
		createProject project.Project
		createErr     error
		wantCode      string
		wantStatus    int
		wantName      string
	}{
		{
			name:          "happy path — pro plan",
			plan:          "pro",
			req:           project.CreateProjectRequest{Name: "Alpha"},
			createProject: project.Project{ID: "p1", Name: "Alpha", Status: "active", FolderIcon: "folder"},
			wantName:      "Alpha",
		},
		{
			name:          "happy path — free plan under limit",
			plan:          "free",
			req:           project.CreateProjectRequest{Name: "Beta"},
			repoCount:     4,
			createProject: project.Project{ID: "p2", Name: "Beta", Status: "active", FolderIcon: "folder"},
			wantName:      "Beta",
		},
		{
			// boundary: 4 existing projects (one below the limit of 5) is allowed.
			name:          "free plan boundary — N-1 allowed",
			plan:          "free",
			req:           project.CreateProjectRequest{Name: "Fifth"},
			repoCount:     4,
			createProject: project.Project{ID: "p5", Name: "Fifth", Status: "active", FolderIcon: "folder"},
			wantName:      "Fifth",
		},
		{
			// boundary: 5 existing projects (at the limit) blocks the 6th.
			name:       "free plan at limit",
			plan:       "free",
			req:        project.CreateProjectRequest{Name: "Sixth"},
			repoCount:  5,
			wantCode:   apperror.ErrPlanLimitExceeded,
			wantStatus: http.StatusForbidden,
		},
		{
			// boundary: already over the limit (graceful-downgrade state) stays blocked.
			name:       "free plan over limit — N+1 blocked",
			plan:       "free",
			req:        project.CreateProjectRequest{Name: "Seventh"},
			repoCount:  6,
			wantCode:   apperror.ErrPlanLimitExceeded,
			wantStatus: http.StatusForbidden,
		},
		{
			// pro plan is never gated regardless of count.
			name:          "pro plan ignores limit at high count",
			plan:          "pro",
			req:           project.CreateProjectRequest{Name: "Unlimited"},
			repoCount:     999,
			createProject: project.Project{ID: "pX", Name: "Unlimited", Status: "active", FolderIcon: "folder"},
			wantName:      "Unlimited",
		},
		{
			name:       "empty name",
			plan:       "pro",
			req:        project.CreateProjectRequest{Name: "   "},
			wantCode:   apperror.ErrInvalidInput,
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "invalid status",
			plan:       "pro",
			req:        project.CreateProjectRequest{Name: "X", Status: "pending"},
			wantCode:   apperror.ErrInvalidStatus,
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "invalid folderIcon",
			plan:       "pro",
			req:        project.CreateProjectRequest{Name: "X", FolderIcon: "not-a-real-icon"},
			wantCode:   apperror.ErrInvalidInput,
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "description too long",
			plan:       "pro",
			req:        project.CreateProjectRequest{Name: "X", Description: strPtr(string(make([]byte, 2001)))},
			wantCode:   apperror.ErrInvalidInput,
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "duplicate name from repo",
			plan:       "pro",
			req:        project.CreateProjectRequest{Name: "Alpha"},
			createErr:  apperror.New(http.StatusConflict, apperror.ErrDuplicateName, "duplicate"),
			wantCode:   apperror.ErrDuplicateName,
			wantStatus: http.StatusConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mockProjectRepo{
				countByUser: func(_ context.Context, _ string) (int, error) {
					return tt.repoCount, tt.repoCountErr
				},
				create: func(_ context.Context, p project.Project) (project.Project, error) {
					if tt.createErr != nil {
						return project.Project{}, tt.createErr
					}
					return tt.createProject, nil
				},
			}
			svc := project.NewService(repo, nil)
			view, err := svc.Create(context.Background(), "user1", "area1", tt.plan, tt.req)

			if tt.wantCode != "" {
				ae := appErr(err)
				if ae == nil {
					t.Fatalf("expected AppError %q, got nil", tt.wantCode)
				}
				if ae.Code != tt.wantCode {
					t.Errorf("code: got %q, want %q", ae.Code, tt.wantCode)
				}
				if tt.wantStatus != 0 && ae.Status != tt.wantStatus {
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

func TestProjectService_Update(t *testing.T) {
	tests := []struct {
		name       string
		req        project.UpdateProjectRequest
		repoProj   project.Project
		repoErr    error
		wantCode   string
		wantStatus int
	}{
		{
			name:     "happy path — rename",
			req:      project.UpdateProjectRequest{Name: strPtr("Renamed")},
			repoProj: project.Project{ID: "p1", Name: "Renamed"},
		},
		{
			name:       "empty areaId rejected — a project must keep an area",
			req:        project.UpdateProjectRequest{AreaID: strPtr("")},
			wantCode:   apperror.ErrInvalidInput,
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:     "move to another area",
			req:      project.UpdateProjectRequest{AreaID: strPtr("a2")},
			repoProj: project.Project{ID: "p1", AreaID: "a2"},
		},
		{
			name:       "not found",
			req:        project.UpdateProjectRequest{Name: strPtr("X")},
			repoErr:    apperror.New(http.StatusNotFound, apperror.ErrProjectNotFound, "not found"),
			wantCode:   apperror.ErrProjectNotFound,
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "empty name",
			req:        project.UpdateProjectRequest{Name: strPtr("  ")},
			wantCode:   apperror.ErrInvalidInput,
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "invalid status",
			req:        project.UpdateProjectRequest{Status: strPtr("pending")},
			wantCode:   apperror.ErrInvalidStatus,
			wantStatus: http.StatusUnprocessableEntity,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mockProjectRepo{
				update: func(_ context.Context, _, _ string, _ project.UpdateProjectRequest) (project.Project, error) {
					return tt.repoProj, tt.repoErr
				},
			}
			svc := project.NewService(repo, nil)
			_, err := svc.Update(context.Background(), "user1", "p1", tt.req)

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

func TestProjectService_Reorder(t *testing.T) {
	tests := []struct {
		name       string
		req        project.ReorderRequest
		repoResult int
		repoErr    error
		wantCode   string
		wantCount  int
	}{
		{
			name:       "happy path",
			req:        project.ReorderRequest{Items: []project.ReorderItem{{ID: "p1", DisplayOrder: 0}, {ID: "p2", DisplayOrder: 1}}},
			repoResult: 2,
			wantCount:  2,
		},
		{
			name:     "empty items",
			req:      project.ReorderRequest{Items: []project.ReorderItem{}},
			wantCode: apperror.ErrInvalidInput,
		},
		{
			name:     "id not owned",
			req:      project.ReorderRequest{Items: []project.ReorderItem{{ID: "unknown"}}},
			repoErr:  apperror.New(http.StatusNotFound, apperror.ErrProjectNotFound, "not found"),
			wantCode: apperror.ErrProjectNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mockProjectRepo{
				reorder: func(_ context.Context, _ string, _ []project.ReorderItem) (int, error) {
					return tt.repoResult, tt.repoErr
				},
			}
			svc := project.NewService(repo, nil)
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
