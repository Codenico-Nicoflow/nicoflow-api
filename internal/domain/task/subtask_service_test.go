package task

import (
	"context"
	"testing"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
)

type mockSubtaskRepo struct {
	taskOwned    func(ctx context.Context, userID, taskID string) (bool, error)
	listByTask   func(ctx context.Context, taskID string) ([]Subtask, error)
	create       func(ctx context.Context, s Subtask) (Subtask, error)
	update       func(ctx context.Context, userID, taskID, id string, req UpdateSubtaskRequest) (Subtask, error)
	deleteFn     func(ctx context.Context, userID, taskID, id string) error
	nextPosition func(ctx context.Context, taskID string) (int, error)
}

func (m *mockSubtaskRepo) TaskOwned(ctx context.Context, userID, taskID string) (bool, error) {
	return m.taskOwned(ctx, userID, taskID)
}
func (m *mockSubtaskRepo) ListByTask(ctx context.Context, taskID string) ([]Subtask, error) {
	return m.listByTask(ctx, taskID)
}
func (m *mockSubtaskRepo) Create(ctx context.Context, s Subtask) (Subtask, error) {
	return m.create(ctx, s)
}
func (m *mockSubtaskRepo) Update(ctx context.Context, userID, taskID, id string, req UpdateSubtaskRequest) (Subtask, error) {
	return m.update(ctx, userID, taskID, id, req)
}
func (m *mockSubtaskRepo) Delete(ctx context.Context, userID, taskID, id string) error {
	return m.deleteFn(ctx, userID, taskID, id)
}
func (m *mockSubtaskRepo) NextPosition(ctx context.Context, taskID string) (int, error) {
	return m.nextPosition(ctx, taskID)
}

func TestSubtaskService_Create(t *testing.T) {
	tests := []struct {
		name     string
		req      CreateSubtaskRequest
		owned    bool
		wantErr  bool
		wantCode string
		wantPos  int
	}{
		{
			name:    "appends at next position",
			req:     CreateSubtaskRequest{Title: "Step 1"},
			owned:   true,
			wantPos: 4,
		},
		{
			name:    "explicit position honoured",
			req:     CreateSubtaskRequest{Title: "Step 1", Position: ptr(0)},
			owned:   true,
			wantPos: 0,
		},
		{
			name:     "empty title rejected",
			req:      CreateSubtaskRequest{Title: "  "},
			owned:    true,
			wantErr:  true,
			wantCode: apperror.ErrInvalidInput,
		},
		{
			name:     "task not owned -> 404",
			req:      CreateSubtaskRequest{Title: "x"},
			owned:    false,
			wantErr:  true,
			wantCode: apperror.ErrResourceNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var persisted Subtask
			repo := &mockSubtaskRepo{
				taskOwned:    func(_ context.Context, _, _ string) (bool, error) { return tt.owned, nil },
				nextPosition: func(_ context.Context, _ string) (int, error) { return 4, nil },
				create: func(_ context.Context, s Subtask) (Subtask, error) {
					persisted = s
					return s, nil
				},
			}
			_, err := NewSubtaskService(repo).Create(context.Background(), "u1", "t1", tt.req)
			if tt.wantErr {
				if ae := appErr(err); ae == nil || ae.Code != tt.wantCode {
					t.Fatalf("want %s, got %+v", tt.wantCode, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if persisted.Position != tt.wantPos {
				t.Errorf("position = %d, want %d", persisted.Position, tt.wantPos)
			}
			if persisted.TaskID != "t1" {
				t.Errorf("taskID = %q", persisted.TaskID)
			}
		})
	}
}

func TestSubtaskService_List_NotOwned(t *testing.T) {
	repo := &mockSubtaskRepo{
		taskOwned: func(_ context.Context, _, _ string) (bool, error) { return false, nil },
	}
	_, err := NewSubtaskService(repo).List(context.Background(), "u1", "t1")
	if ae := appErr(err); ae == nil || ae.Code != apperror.ErrResourceNotFound {
		t.Fatalf("want RESOURCE_NOT_FOUND, got %+v", err)
	}
}

func TestSubtaskService_Update_EmptyTitleRejected(t *testing.T) {
	repo := &mockSubtaskRepo{}
	empty := "   "
	_, err := NewSubtaskService(repo).Update(context.Background(), "u1", "t1", "s1", UpdateSubtaskRequest{Title: &empty})
	if ae := appErr(err); ae == nil || ae.Code != apperror.ErrInvalidInput {
		t.Fatalf("want INVALID_INPUT, got %+v", err)
	}
}
