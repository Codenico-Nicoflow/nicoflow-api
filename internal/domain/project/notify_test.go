package project_test

import (
	"context"
	"testing"

	"github.com/nicoflow/nicoflow-api/internal/domain/notification"
	"github.com/nicoflow/nicoflow-api/internal/domain/project"
)

// fakeNotifier records every Create call, to prove project_completed fires
// only on the explicit status transition, never on task-count inference.
type fakeNotifier struct {
	calls []notification.Notification
}

func (f *fakeNotifier) Create(_ context.Context, n notification.Notification) (notification.NotificationView, bool, error) {
	f.calls = append(f.calls, n)
	return notification.NotificationView{}, true, nil
}

func updateStatusRepo(fromStatus, toStatus string) *mockProjectRepo {
	return &mockProjectRepo{
		getByID: func(_ context.Context, _, id string) (*project.Project, error) {
			return &project.Project{ID: id, UserID: "u1", Name: "P", Status: fromStatus}, nil
		},
		update: func(_ context.Context, _, id string, _ project.UpdateProjectRequest) (project.Project, error) {
			return project.Project{ID: id, UserID: "u1", Name: "P", Status: toStatus}, nil
		},
	}
}

func TestUpdate_ProjectCompletedOnExplicitStatusTransition(t *testing.T) {
	tests := []struct {
		name       string
		from, to   string
		wantNotify bool
	}{
		{"active->completed: fires", "active", "completed", true},
		{"completed->completed: no edge, nothing fires", "completed", "completed", false},
		{"completed->active (reopen): nothing fires", "completed", "active", false},
		{"active->archived: not completed, nothing fires", "active", "archived", false},
		{"active->active (reopen then re-complete later): nothing fires here", "active", "active", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fn := &fakeNotifier{}
			svc := project.NewService(updateStatusRepo(tt.from, tt.to), nil, fn)
			status := tt.to
			if _, err := svc.Update(context.Background(), "u1", "p1", project.UpdateProjectRequest{Status: &status}); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantNotify && len(fn.calls) != 1 {
				t.Fatalf("calls = %d, want 1", len(fn.calls))
			}
			if !tt.wantNotify && len(fn.calls) != 0 {
				t.Fatalf("calls = %d, want 0", len(fn.calls))
			}
			if tt.wantNotify && fn.calls[0].Type != notification.TypeProjectCompleted {
				t.Errorf("type = %s, want %s", fn.calls[0].Type, notification.TypeProjectCompleted)
			}
		})
	}
}

func TestUpdate_ProjectCompletedNotFiredWhenStatusUntouched(t *testing.T) {
	fn := &fakeNotifier{}
	repo := &mockProjectRepo{
		update: func(_ context.Context, _, id string, _ project.UpdateProjectRequest) (project.Project, error) {
			return project.Project{ID: id, UserID: "u1", Name: "P", Status: "active"}, nil
		},
	}
	svc := project.NewService(repo, nil, fn)
	name := "renamed"
	if _, err := svc.Update(context.Background(), "u1", "p1", project.UpdateProjectRequest{Name: &name}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fn.calls) != 0 {
		t.Fatalf("calls = %d, want 0 (status field untouched)", len(fn.calls))
	}
}

// A nil notifier is a safe no-op (notifications disabled).
func TestUpdate_NilNotifierIsNoop(t *testing.T) {
	svc := project.NewService(updateStatusRepo("active", "completed"), nil, nil)
	status := "completed"
	if _, err := svc.Update(context.Background(), "u1", "p1", project.UpdateProjectRequest{Status: &status}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
