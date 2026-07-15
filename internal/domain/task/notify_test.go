package task

import (
	"context"
	"errors"
	"testing"

	"github.com/nicoflow/nicoflow-api/internal/domain/notification"
)

// fakeNotifier records every Create call and can be forced to error, to prove
// the fire-and-forget guarantee (a notify error must not fail the mutation).
type fakeNotifier struct {
	calls   []notification.Notification
	failErr error
}

func (f *fakeNotifier) Create(_ context.Context, n notification.Notification) (notification.NotificationView, bool, error) {
	f.calls = append(f.calls, n)
	if f.failErr != nil {
		return notification.NotificationView{}, false, f.failErr
	}
	return notification.NotificationView{}, true, nil
}

// updateRepo builds a mockRepo whose GetByID returns a task in fromStatus and
// whose Update returns the same task moved to toStatus, with a configurable
// remaining non-terminal count for the project_completed check.
func updateRepo(fromStatus, toStatus string, remaining int) *mockRepo {
	return &mockRepo{
		getByID: func(_ context.Context, _, id string) (*Task, error) {
			return &Task{ID: id, UserID: "u1", ProjectID: "p1", Title: "T", Status: fromStatus}, nil
		},
		update: func(_ context.Context, _, id string, _ UpdateTaskRequest, _ completedAtChange) (Task, error) {
			return Task{ID: id, UserID: "u1", ProjectID: "p1", Title: "T", Status: toStatus}, nil
		},
		countNonTerminal: func(_ context.Context, _, _ string) (int, error) { return remaining, nil },
	}
}

func typesFired(calls []notification.Notification) []string {
	out := make([]string, len(calls))
	for i, c := range calls {
		out[i] = c.Type
	}
	return out
}

func TestUpdate_EmitsOnDoneEdge(t *testing.T) {
	tests := []struct {
		name      string
		from, to  string
		remaining int
		want      []string
	}{
		{"active->done, tasks remain: task_completed only", "active", "done", 2,
			[]string{notification.TypeTaskCompleted}},
		{"active->done, last one: task+project completed", "active", "done", 0,
			[]string{notification.TypeTaskCompleted, notification.TypeProjectCompleted}},
		{"done->done: no edge, nothing fires", "done", "done", 0, nil},
		{"active->someday: not done, nothing fires", "active", "someday", 0, nil},
		{"done->active (reopen): nothing fires", "done", "active", 5, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fn := &fakeNotifier{}
			svc := NewService(updateRepo(tt.from, tt.to, tt.remaining), fn)
			status := tt.to
			if _, err := svc.Update(context.Background(), "u1", "t1", "pro", UpdateTaskRequest{Status: &status}); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got := typesFired(fn.calls)
			if len(got) != len(tt.want) {
				t.Fatalf("fired %v, want %v", got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("fired %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestUpdate_TaskCompletedMetadataAndDedupe(t *testing.T) {
	fn := &fakeNotifier{}
	svc := NewService(updateRepo("active", "done", 3), fn)
	status := "done"
	if _, err := svc.Update(context.Background(), "u1", "t1", "pro", UpdateTaskRequest{Status: &status}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fn.calls) != 1 {
		t.Fatalf("want 1 call, got %d", len(fn.calls))
	}
	n := fn.calls[0]
	if n.DedupeKey == nil || *n.DedupeKey != "task_completed:t1" {
		t.Errorf("dedupeKey = %v, want task_completed:t1", n.DedupeKey)
	}
	if string(n.Metadata) != `{"taskId":"t1"}` {
		t.Errorf("metadata = %s, want {\"taskId\":\"t1\"}", n.Metadata)
	}
}

// AC2: a notifier error must never fail the mutation.
func TestUpdate_NotifyErrorDoesNotFailMutation(t *testing.T) {
	fn := &fakeNotifier{failErr: errors.New("boom")}
	svc := NewService(updateRepo("active", "done", 0), fn)
	status := "done"
	view, err := svc.Update(context.Background(), "u1", "t1", "pro", UpdateTaskRequest{Status: &status})
	if err != nil {
		t.Fatalf("notify error leaked into mutation: %v", err)
	}
	if view.Status != "done" {
		t.Errorf("view.Status = %q, want done", view.Status)
	}
}

// A nil notifier is a safe no-op (notifications disabled).
func TestUpdate_NilNotifierIsNoop(t *testing.T) {
	svc := NewService(updateRepo("active", "done", 0), nil)
	status := "done"
	if _, err := svc.Update(context.Background(), "u1", "t1", "pro", UpdateTaskRequest{Status: &status}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
