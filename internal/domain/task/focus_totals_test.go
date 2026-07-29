package task

import (
	"context"
	"testing"
	"time"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
)

// ── fake focus totals ─────────────────────────────────────────────────────────

type fakeFocusTotals struct {
	scalar      func(ctx context.Context, userID, taskID string) (int64, error)
	batch       func(ctx context.Context, userID string, taskIDs []string) (map[string]int64, error)
	scalarCalls int
	batchCalls  int
}

func (f *fakeFocusTotals) SumClosedSecondsByTask(ctx context.Context, userID, taskID string) (int64, error) {
	f.scalarCalls++
	if f.scalar == nil {
		return 0, nil
	}
	return f.scalar(ctx, userID, taskID)
}

func (f *fakeFocusTotals) SumClosedSecondsByTaskBatch(ctx context.Context, userID string, taskIDs []string) (map[string]int64, error) {
	f.batchCalls++
	if f.batch == nil {
		return map[string]int64{}, nil
	}
	return f.batch(ctx, userID, taskIDs)
}

// ── Focus enrichment (batch) ──────────────────────────────────────────────────

var focusTotalsNow = func() time.Time { return time.Date(2026, 6, 15, 9, 0, 0, 0, time.UTC) }

func focusTotalsRepo() *mockRepo {
	candidates := []Task{
		{ID: "a", Energy: "low", Priority: "low", Status: "active"},
		{ID: "b", Energy: "low", Priority: "low", Status: "active"},
		{ID: "c", Energy: "low", Priority: "low", Status: "inbox"},
	}
	return &mockRepo{
		listActiveInbox: func(_ context.Context, _ string) ([]Task, error) { return candidates, nil },
	}
}

func TestService_Focus_TotalFocusSeconds(t *testing.T) {
	now, repo := focusTotalsNow, focusTotalsRepo()

	t.Run("carries summed seconds per task in one batch query", func(t *testing.T) {
		var gotUser string
		var gotIDs []string
		totals := &fakeFocusTotals{
			batch: func(_ context.Context, userID string, taskIDs []string) (map[string]int64, error) {
				gotUser = userID
				gotIDs = taskIDs
				return map[string]int64{"a": 120, "b": 30}, nil
			},
		}
		svc := NewServiceWithClock(repo, now).WithFocusTotals(totals)

		resp, err := svc.Focus(context.Background(), "u1", FocusParams{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if totals.batchCalls != 1 {
			t.Errorf("batch calls = %d, want exactly 1", totals.batchCalls)
		}
		if totals.scalarCalls != 0 {
			t.Errorf("scalar calls = %d, want 0 — the list must use the batch path", totals.scalarCalls)
		}
		if gotUser != "u1" {
			t.Errorf("batch scoped to user %q, want u1", gotUser)
		}
		if len(gotIDs) != len(resp.Items) {
			t.Errorf("batch asked for %d ids, want %d", len(gotIDs), len(resp.Items))
		}
		want := map[string]int64{"a": 120, "b": 30, "c": 0}
		for _, item := range resp.Items {
			if item.TotalFocusSeconds != want[item.ID] {
				t.Errorf("task %s totalFocusSeconds = %d, want %d", item.ID, item.TotalFocusSeconds, want[item.ID])
			}
		}
	})

	t.Run("no closed segments reads as zero", func(t *testing.T) {
		totals := &fakeFocusTotals{} // batch returns an empty map — every id is a miss
		svc := NewServiceWithClock(repo, now).WithFocusTotals(totals)

		resp, err := svc.Focus(context.Background(), "u1", FocusParams{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, item := range resp.Items {
			if item.TotalFocusSeconds != 0 {
				t.Errorf("task %s totalFocusSeconds = %d, want 0", item.ID, item.TotalFocusSeconds)
			}
		}
	})

}

func TestService_Focus_TotalFocusSeconds_Edges(t *testing.T) {
	now, repo := focusTotalsNow, focusTotalsRepo()

	t.Run("empty focus list never queries totals", func(t *testing.T) {
		emptyRepo := &mockRepo{
			listActiveInbox: func(_ context.Context, _ string) ([]Task, error) { return nil, nil },
		}
		totals := &fakeFocusTotals{}
		svc := NewServiceWithClock(emptyRepo, now).WithFocusTotals(totals)

		if _, err := svc.Focus(context.Background(), "u1", FocusParams{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if totals.batchCalls != 0 {
			t.Errorf("batch calls = %d, want 0 for an empty list", totals.batchCalls)
		}
	})

	t.Run("batch failure fails the request", func(t *testing.T) {
		totals := &fakeFocusTotals{
			batch: func(_ context.Context, _ string, _ []string) (map[string]int64, error) {
				return nil, apperror.New(500, apperror.ErrInternalServerError, "boom")
			},
		}
		svc := NewServiceWithClock(repo, now).WithFocusTotals(totals)

		if _, err := svc.Focus(context.Background(), "u1", FocusParams{}); err == nil {
			t.Fatal("want error when the totals batch fails, got nil")
		}
	})

	t.Run("nil totals seam keeps the zero default", func(t *testing.T) {
		svc := NewServiceWithClock(repo, now)
		resp, err := svc.Focus(context.Background(), "u1", FocusParams{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, item := range resp.Items {
			if item.TotalFocusSeconds != 0 {
				t.Errorf("task %s totalFocusSeconds = %d, want 0 with no seam wired", item.ID, item.TotalFocusSeconds)
			}
		}
	})
}

// ── GetTask enrichment (scalar) ───────────────────────────────────────────────

func TestService_Get_TotalFocusSeconds(t *testing.T) {
	repo := &mockRepo{
		getByID: func(_ context.Context, _, id string) (*Task, error) {
			return &Task{ID: id, Status: "active"}, nil
		},
	}

	tests := []struct {
		name  string
		total int64
	}{
		{name: "carries the summed seconds", total: 45},
		{name: "no closed segments reads as zero", total: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotUser, gotTask string
			totals := &fakeFocusTotals{
				scalar: func(_ context.Context, userID, taskID string) (int64, error) {
					gotUser, gotTask = userID, taskID
					return tt.total, nil
				},
			}
			svc := NewService(repo, nil, nil).WithFocusTotals(totals)

			view, err := svc.Get(context.Background(), "u1", "t1")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if view.TotalFocusSeconds != tt.total {
				t.Errorf("totalFocusSeconds = %d, want %d", view.TotalFocusSeconds, tt.total)
			}
			if totals.scalarCalls != 1 {
				t.Errorf("scalar calls = %d, want 1", totals.scalarCalls)
			}
			if gotUser != "u1" || gotTask != "t1" {
				t.Errorf("scalar scoped to (%q, %q), want (u1, t1)", gotUser, gotTask)
			}
		})
	}

	t.Run("scalar failure fails the request", func(t *testing.T) {
		totals := &fakeFocusTotals{
			scalar: func(_ context.Context, _, _ string) (int64, error) {
				return 0, apperror.New(500, apperror.ErrInternalServerError, "boom")
			},
		}
		svc := NewService(repo, nil, nil).WithFocusTotals(totals)

		if _, err := svc.Get(context.Background(), "u1", "t1"); err == nil {
			t.Fatal("want error when the totals query fails, got nil")
		}
	})
}

// ── project task-list stays unenriched ────────────────────────────────────────

func TestService_ListByProject_NoFocusTotalsQuery(t *testing.T) {
	repo := &mockRepo{
		projectOwned: func(_ context.Context, _, _ string) (bool, error) { return true, nil },
		listByProject: func(_ context.Context, _, _ string, _ ListTasksFilter) ([]Task, error) {
			return []Task{{ID: "a", Status: "active"}}, nil
		},
	}
	totals := &fakeFocusTotals{}
	svc := NewService(repo, nil, nil).WithFocusTotals(totals)

	resp, err := svc.ListByProject(context.Background(), "u1", "p1", ListTasksFilter{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if totals.scalarCalls != 0 || totals.batchCalls != 0 {
		t.Errorf("list view ran %d scalar + %d batch totals queries, want none",
			totals.scalarCalls, totals.batchCalls)
	}
	if resp.Items[0].TotalFocusSeconds != 0 {
		t.Errorf("list view totalFocusSeconds = %d, want the zero default", resp.Items[0].TotalFocusSeconds)
	}
}
