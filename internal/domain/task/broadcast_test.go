package task

import (
	"context"
	"errors"
	"testing"
)

// fakeBroadcaster records every emitted event, so tests can assert exactly which
// events fired and in what order.
type fakeBroadcaster struct{ events []Event }

func (f *fakeBroadcaster) Broadcast(_ string, ev Event) { f.events = append(f.events, ev) }

func (f *fakeBroadcaster) types() []string {
	out := make([]string, len(f.events))
	for i, ev := range f.events {
		out[i] = ev.Type
	}
	return out
}

func happyRepo(t Task) *mockRepo {
	return &mockRepo{
		projectOwned:     func(context.Context, string, string) (bool, error) { return true, nil },
		nextDisplayOrder: func(context.Context, string, string) (int, error) { return 0, nil },
		create:           func(_ context.Context, in Task) (Task, error) { return in, nil },
		getByID:          func(context.Context, string, string) (*Task, error) { return &t, nil },
		update: func(_ context.Context, _, _ string, _ UpdateTaskRequest, _ completedAtChange) (Task, error) {
			return t, nil
		},
		deleteFn:       func(context.Context, string, string) error { return nil },
		updateSchedule: func(context.Context, string, string, *string, *bool) (Task, error) { return t, nil },
		repack:         func(context.Context, string, string, int) (Task, error) { return t, nil },
	}
}

func TestBroadcast_EventPerMutation(t *testing.T) {
	base := Task{ID: "t1", UserID: "u1", ProjectID: "p1", Title: "x", Status: "active"}

	tests := []struct {
		name      string
		call      func(svc Service) error
		wantTypes []string
	}{
		{
			name: "Create emits task.created",
			call: func(svc Service) error {
				_, err := svc.Create(context.Background(), "u1", "p1", "pro", CreateTaskRequest{Title: "x"})
				return err
			},
			wantTypes: []string{EventCreated},
		},
		{
			name: "CreateWithoutEvent emits nothing",
			call: func(svc Service) error {
				_, err := svc.CreateWithoutEvent(context.Background(), "u1", "p1", "pro", CreateTaskRequest{Title: "x"})
				return err
			},
			wantTypes: nil,
		},
		{
			name: "Update emits task.updated only",
			call: func(svc Service) error {
				title := "y"
				_, err := svc.Update(context.Background(), "u1", "t1", "pro", UpdateTaskRequest{Title: &title})
				return err
			},
			wantTypes: []string{EventUpdated},
		},
		{
			name: "SetStatus emits task.status_changed only",
			call: func(svc Service) error {
				_, err := svc.SetStatus(context.Background(), "u1", "t1", "pro", "done")
				return err
			},
			wantTypes: []string{EventStatusChanged},
		},
		{
			name: "Delete emits task.deleted",
			call: func(svc Service) error {
				return svc.Delete(context.Background(), "u1", "t1")
			},
			wantTypes: []string{EventDeleted},
		},
		{
			name: "Schedule emits task.updated",
			call: func(svc Service) error {
				_, err := svc.Schedule(context.Background(), "u1", "t1", ScheduleRequest{})
				return err
			},
			wantTypes: []string{EventUpdated},
		},
		{
			name: "ReorderOne emits task.updated",
			call: func(svc Service) error {
				_, err := svc.ReorderOne(context.Background(), "u1", "t1", 2)
				return err
			},
			wantTypes: []string{EventUpdated},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fb := &fakeBroadcaster{}
			svc := NewService(happyRepo(base), nil, fb)
			if err := tt.call(svc); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got := fb.types()
			if len(got) != len(tt.wantTypes) {
				t.Fatalf("events = %v, want %v", got, tt.wantTypes)
			}
			for i := range got {
				if got[i] != tt.wantTypes[i] {
					t.Errorf("event[%d] = %q, want %q", i, got[i], tt.wantTypes[i])
				}
			}
		})
	}
}

func TestBroadcast_NoEventOnFailure(t *testing.T) {
	boom := errors.New("db down")
	repo := happyRepo(Task{})
	repo.create = func(context.Context, Task) (Task, error) { return Task{}, boom }
	repo.deleteFn = func(context.Context, string, string) error { return boom }

	fb := &fakeBroadcaster{}
	svc := NewService(repo, nil, fb)

	if _, err := svc.Create(context.Background(), "u1", "p1", "pro", CreateTaskRequest{Title: "x"}); err == nil {
		t.Fatal("expected create error")
	}
	if err := svc.Delete(context.Background(), "u1", "t1"); err == nil {
		t.Fatal("expected delete error")
	}
	if len(fb.events) != 0 {
		t.Errorf("events = %v, want none on failed mutations", fb.types())
	}
}

func TestBroadcast_SubtaskMutationsEmitParentUpdated(t *testing.T) {
	repo := &mockSubtaskRepo{
		taskOwned:    func(context.Context, string, string) (bool, error) { return true, nil },
		nextPosition: func(context.Context, string) (int, error) { return 0, nil },
		create:       func(_ context.Context, st Subtask) (Subtask, error) { return st, nil },
		update: func(_ context.Context, _, _, id string, _ UpdateSubtaskRequest) (Subtask, error) {
			return Subtask{ID: id, TaskID: "t1"}, nil
		},
		deleteFn: func(context.Context, string, string, string) error { return nil },
	}
	fb := &fakeBroadcaster{}
	svc := NewSubtaskService(repo, fb)

	if _, err := svc.Create(context.Background(), "u1", "t1", CreateSubtaskRequest{Title: "s"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	title := "s2"
	if _, err := svc.Update(context.Background(), "u1", "t1", "s1", UpdateSubtaskRequest{Title: &title}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := svc.Delete(context.Background(), "u1", "t1", "s1"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	want := []string{EventUpdated, EventUpdated, EventUpdated}
	got := fb.types()
	if len(got) != len(want) {
		t.Fatalf("events = %v, want three task.updated", got)
	}
	for i, ev := range fb.events {
		if ev.Type != EventUpdated {
			t.Errorf("event[%d].Type = %q, want %q", i, ev.Type, EventUpdated)
		}
		ref, ok := ev.Payload.(Ref)
		if !ok || ref.ID != "t1" {
			t.Errorf("event[%d].Payload = %#v, want Ref{ID: t1}", i, ev.Payload)
		}
	}
}
