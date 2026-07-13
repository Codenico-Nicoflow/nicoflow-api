package search_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/internal/domain/search"
)

// ── mock repository ───────────────────────────────────────────────────────────

type mockRepo struct {
	searchTasks    func(ctx context.Context, userID, term string, limit int) ([]search.TaskResult, error)
	searchProjects func(ctx context.Context, userID, term string, limit int) ([]search.ProjectResult, error)
	searchAreas    func(ctx context.Context, userID, term string, limit int) ([]search.AreaResult, error)
}

func (m *mockRepo) SearchTasks(ctx context.Context, userID, term string, limit int) ([]search.TaskResult, error) {
	return m.searchTasks(ctx, userID, term, limit)
}
func (m *mockRepo) SearchProjects(ctx context.Context, userID, term string, limit int) ([]search.ProjectResult, error) {
	return m.searchProjects(ctx, userID, term, limit)
}
func (m *mockRepo) SearchAreas(ctx context.Context, userID, term string, limit int) ([]search.AreaResult, error) {
	return m.searchAreas(ctx, userID, term, limit)
}

func appErr(err error) *apperror.AppError {
	var ae *apperror.AppError
	if errors.As(err, &ae) {
		return ae
	}
	return nil
}

// ── Validate ──────────────────────────────────────────────────────────────────

func TestValidate(t *testing.T) {
	tests := []struct {
		name      string
		term      string
		types     []string
		limit     int
		wantErr   bool
		wantLimit int
		wantTypes []string
	}{
		{name: "too short", term: "a", wantErr: true},
		{name: "too short after trim", term: "  x  ", wantErr: true},
		{name: "too long (101 chars)", term: strings.Repeat("a", 101), wantErr: true},
		{name: "max length ok (100 chars)", term: strings.Repeat("a", 100), wantLimit: 10},
		{name: "min length ok (2 chars)", term: "go", wantLimit: 10},
		{name: "unknown type rejected", term: "bucket", types: []string{"task", "widget"}, wantErr: true},
		{name: "valid types normalised", term: "bucket", types: []string{"Task", " AREA "}, wantLimit: 10, wantTypes: []string{"task", "area"}},
		{name: "limit default when zero", term: "bucket", limit: 0, wantLimit: 10},
		{name: "limit capped at 50", term: "bucket", limit: 999, wantLimit: 50},
		{name: "negative limit → default", term: "bucket", limit: -5, wantLimit: 10},
		{name: "explicit limit kept", term: "bucket", limit: 25, wantLimit: 25},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := search.Validate(tt.term, tt.types, tt.limit)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("want error, got nil")
				}
				if ae := appErr(err); ae == nil || ae.Code != apperror.ErrInvalidInput {
					t.Fatalf("want INVALID_INPUT, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if q.Limit != tt.wantLimit {
				t.Errorf("limit: want %d, got %d", tt.wantLimit, q.Limit)
			}
			if tt.wantTypes != nil {
				if len(q.Types) != len(tt.wantTypes) {
					t.Fatalf("types: want %v, got %v", tt.wantTypes, q.Types)
				}
				for i := range tt.wantTypes {
					if q.Types[i] != tt.wantTypes[i] {
						t.Errorf("types[%d]: want %q, got %q", i, tt.wantTypes[i], q.Types[i])
					}
				}
			}
		})
	}
}

// ── Search ────────────────────────────────────────────────────────────────────

func TestService_Search(t *testing.T) {
	sampleTasks := []search.TaskResult{{ID: "t1", Title: "Ship the bucket page", ProjectID: "p1", ProjectName: "Bucket cleanup"}}
	sampleProjects := []search.ProjectResult{{ID: "p1", Name: "Bucket cleanup", AreaName: "Work"}}
	sampleAreas := []search.AreaResult{{ID: "a1", Name: "Bucket area"}}

	tests := []struct {
		name         string
		types        []string
		wantTasks    int
		wantProjects int
		wantAreas    int
	}{
		{name: "all groups when types omitted", types: nil, wantTasks: 1, wantProjects: 1, wantAreas: 1},
		{name: "only tasks requested", types: []string{"task"}, wantTasks: 1, wantProjects: 0, wantAreas: 0},
		{name: "tasks and areas requested", types: []string{"task", "area"}, wantTasks: 1, wantProjects: 0, wantAreas: 1},
		{name: "only projects requested", types: []string{"project"}, wantTasks: 0, wantProjects: 1, wantAreas: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tasksCalled, projectsCalled, areasCalled bool
			repo := &mockRepo{
				searchTasks: func(_ context.Context, _, _ string, _ int) ([]search.TaskResult, error) {
					tasksCalled = true
					return sampleTasks, nil
				},
				searchProjects: func(_ context.Context, _, _ string, _ int) ([]search.ProjectResult, error) {
					projectsCalled = true
					return sampleProjects, nil
				},
				searchAreas: func(_ context.Context, _, _ string, _ int) ([]search.AreaResult, error) {
					areasCalled = true
					return sampleAreas, nil
				},
			}
			svc := search.NewService(repo)

			q, err := search.Validate("bucket", tt.types, 10)
			if err != nil {
				t.Fatalf("validate: %v", err)
			}
			resp, err := svc.Search(context.Background(), "user-1", q)
			if err != nil {
				t.Fatalf("search: %v", err)
			}

			if len(resp.Tasks) != tt.wantTasks {
				t.Errorf("tasks: want %d, got %d", tt.wantTasks, len(resp.Tasks))
			}
			if len(resp.Projects) != tt.wantProjects {
				t.Errorf("projects: want %d, got %d", tt.wantProjects, len(resp.Projects))
			}
			if len(resp.Areas) != tt.wantAreas {
				t.Errorf("areas: want %d, got %d", tt.wantAreas, len(resp.Areas))
			}
			// Unrequested groups must not hit the repository (no wasted queries).
			if tt.wantTasks == 0 && tasksCalled {
				t.Error("SearchTasks called for unrequested group")
			}
			if tt.wantProjects == 0 && projectsCalled {
				t.Error("SearchProjects called for unrequested group")
			}
			if tt.wantAreas == 0 && areasCalled {
				t.Error("SearchAreas called for unrequested group")
			}
		})
	}
}

func TestService_Search_PerTypeLimitPropagated(t *testing.T) {
	var gotLimit int
	repo := &mockRepo{
		searchTasks: func(_ context.Context, _, _ string, limit int) ([]search.TaskResult, error) {
			gotLimit = limit
			return nil, nil
		},
		searchProjects: func(_ context.Context, _, _ string, _ int) ([]search.ProjectResult, error) { return nil, nil },
		searchAreas:    func(_ context.Context, _, _ string, _ int) ([]search.AreaResult, error) { return nil, nil },
	}
	svc := search.NewService(repo)

	q, err := search.Validate("bucket", []string{"task"}, 5)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if _, err := svc.Search(context.Background(), "user-1", q); err != nil {
		t.Fatalf("search: %v", err)
	}
	if gotLimit != 5 {
		t.Errorf("per-type limit: want 5 propagated to repo, got %d", gotLimit)
	}
}

func TestService_Search_EmptyResult(t *testing.T) {
	repo := &mockRepo{
		searchTasks:    func(_ context.Context, _, _ string, _ int) ([]search.TaskResult, error) { return nil, nil },
		searchProjects: func(_ context.Context, _, _ string, _ int) ([]search.ProjectResult, error) { return nil, nil },
		searchAreas:    func(_ context.Context, _, _ string, _ int) ([]search.AreaResult, error) { return nil, nil },
	}
	svc := search.NewService(repo)

	q, err := search.Validate("nothingmatches", nil, 10)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	resp, err := svc.Search(context.Background(), "user-1", q)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	// Empty results must serialise as [] (non-nil), never null.
	if resp.Tasks == nil || resp.Projects == nil || resp.Areas == nil {
		t.Fatalf("expected non-nil empty slices, got %+v", resp)
	}
	if len(resp.Tasks)+len(resp.Projects)+len(resp.Areas) != 0 {
		t.Errorf("expected all-empty, got %+v", resp)
	}
}

func TestService_Search_RepoErrorPropagates(t *testing.T) {
	wantErr := apperror.New(500, apperror.ErrDatabaseError, "boom")
	repo := &mockRepo{
		searchTasks: func(_ context.Context, _, _ string, _ int) ([]search.TaskResult, error) {
			return nil, wantErr
		},
		searchProjects: func(_ context.Context, _, _ string, _ int) ([]search.ProjectResult, error) { return nil, nil },
		searchAreas:    func(_ context.Context, _, _ string, _ int) ([]search.AreaResult, error) { return nil, nil },
	}
	svc := search.NewService(repo)

	q, _ := search.Validate("bucket", nil, 10)
	_, err := svc.Search(context.Background(), "user-1", q)
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if ae := appErr(err); ae == nil || ae.Code != apperror.ErrDatabaseError {
		t.Fatalf("want DATABASE_ERROR, got %v", err)
	}
}
