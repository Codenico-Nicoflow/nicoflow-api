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
	searchNotes    func(ctx context.Context, userID, term string, limit int) ([]search.NoteResult, error)
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
func (m *mockRepo) SearchNotes(ctx context.Context, userID, term string, limit int) ([]search.NoteResult, error) {
	if m.searchNotes == nil {
		return nil, nil
	}
	return m.searchNotes(ctx, userID, term, limit)
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

// assertGroup checks one result group's size and that the repository was only
// consulted when the group was requested.
func assertGroup(t *testing.T, name string, got, want int, called bool) {
	t.Helper()
	if got != want {
		t.Errorf("%s: want %d, got %d", name, want, got)
	}
	if want == 0 && called {
		t.Errorf("%s: repository queried for an unrequested group", name)
	}
}

// ── Search ────────────────────────────────────────────────────────────────────

func TestService_Search(t *testing.T) {
	sampleTasks := []search.TaskResult{{ID: "t1", Title: "Ship the bucket page", ProjectID: "p1", ProjectName: "Bucket cleanup"}}
	sampleProjects := []search.ProjectResult{{ID: "p1", Name: "Bucket cleanup", AreaName: "Work"}}
	sampleAreas := []search.AreaResult{{ID: "a1", Name: "Bucket area"}}
	sampleNotes := []search.NoteResult{{ID: "n1", Title: "Bucket notes", ProjectID: "p1", ProjectName: "Bucket cleanup"}}

	tests := []struct {
		name         string
		types        []string
		wantTasks    int
		wantProjects int
		wantAreas    int
		wantNotes    int
	}{
		{name: "all groups when types omitted", types: nil, wantTasks: 1, wantProjects: 1, wantAreas: 1, wantNotes: 1},
		{name: "only tasks requested", types: []string{"task"}, wantTasks: 1, wantProjects: 0, wantAreas: 0, wantNotes: 0},
		{name: "tasks and areas requested", types: []string{"task", "area"}, wantTasks: 1, wantProjects: 0, wantAreas: 1, wantNotes: 0},
		{name: "only projects requested", types: []string{"project"}, wantTasks: 0, wantProjects: 1, wantAreas: 0, wantNotes: 0},
		// AC4 — types=note narrows the response to the notes group alone.
		{name: "only notes requested", types: []string{"note"}, wantTasks: 0, wantProjects: 0, wantAreas: 0, wantNotes: 1},
		{name: "notes alongside tasks", types: []string{"task", "note"}, wantTasks: 1, wantProjects: 0, wantAreas: 0, wantNotes: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tasksCalled, projectsCalled, areasCalled, notesCalled bool
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
				searchNotes: func(_ context.Context, _, _ string, _ int) ([]search.NoteResult, error) {
					notesCalled = true
					return sampleNotes, nil
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

			// An unrequested group must be empty AND must not have hit the
			// repository — a populated group is a filter bug, a wasted query is a
			// performance one.
			assertGroup(t, "tasks", len(resp.Tasks), tt.wantTasks, tasksCalled)
			assertGroup(t, "projects", len(resp.Projects), tt.wantProjects, projectsCalled)
			assertGroup(t, "areas", len(resp.Areas), tt.wantAreas, areasCalled)
			assertGroup(t, "notes", len(resp.Notes), tt.wantNotes, notesCalled)
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
