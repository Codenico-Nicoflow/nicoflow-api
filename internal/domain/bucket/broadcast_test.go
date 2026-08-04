package bucket_test

import (
	"context"
	"errors"
	"testing"

	"github.com/nicoflow/nicoflow-api/internal/domain/bucket"
	"github.com/nicoflow/nicoflow-api/internal/domain/task"
)

type fakeBroadcaster struct{ events []bucket.Event }

func (f *fakeBroadcaster) Broadcast(_ string, ev bucket.Event) { f.events = append(f.events, ev) }

func (f *fakeBroadcaster) types() []string {
	out := make([]string, len(f.events))
	for i, ev := range f.events {
		out[i] = ev.Type
	}
	return out
}

func processReq() bucket.ProcessBucketRequest {
	return bucket.ProcessBucketRequest{
		ProcessingResult: bucket.ResultTask,
		ProjectID:        ptr("p1"),
		TaskDetails:      &bucket.ProcessTaskDetails{Title: "from inbox"},
	}
}

// Process→task must fire bucket.processed AND task.created together, only after
// both writes succeed.
func TestBroadcast_ProcessFiresBothEvents(t *testing.T) {
	repo := &mockRepo{
		getByID: func(_ context.Context, _, id string) (bucket.Bucket, error) { return unprocessed(id), nil },
		markProcessed: func(_ context.Context, _, id, _ string, _ bucket.ProcessedRefs) (bucket.Bucket, error) {
			return unprocessed(id), nil
		},
	}
	tc := &mockTaskCreator{create: func(_ context.Context, _, _, _ string, _ task.CreateTaskRequest) (task.TaskView, error) {
		return task.TaskView{ID: "t-new"}, nil
	}}
	fb := &fakeBroadcaster{}
	svc := bucket.NewService(repo, tc, nil, fb)

	if _, err := svc.Process(context.Background(), "u1", "b1", "free", processReq()); err != nil {
		t.Fatalf("process: %v", err)
	}

	want := []string{bucket.EventProcessed, bucket.EventTaskCreated}
	got := fb.types()
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("events = %v, want %v", got, want)
	}
	view, ok := fb.events[1].Payload.(task.TaskView)
	if !ok || view.ID != "t-new" {
		t.Errorf("task.created payload = %#v, want the created TaskView", fb.events[1].Payload)
	}
}

// A mid-operation failure (task created, mark fails) must emit nothing.
func TestBroadcast_ProcessMidFailureEmitsNothing(t *testing.T) {
	repo := &mockRepo{
		getByID: func(_ context.Context, _, id string) (bucket.Bucket, error) { return unprocessed(id), nil },
		markProcessed: func(_ context.Context, _, _, _ string, _ bucket.ProcessedRefs) (bucket.Bucket, error) {
			return bucket.Bucket{}, errors.New("mark failed")
		},
	}
	tc := &mockTaskCreator{create: func(_ context.Context, _, _, _ string, _ task.CreateTaskRequest) (task.TaskView, error) {
		return task.TaskView{ID: "t-new"}, nil
	}}
	fb := &fakeBroadcaster{}
	svc := bucket.NewService(repo, tc, nil, fb)

	if _, err := svc.Process(context.Background(), "u1", "b1", "free", processReq()); err == nil {
		t.Fatal("expected process error")
	}
	if len(fb.events) != 0 {
		t.Errorf("events = %v, want none on mid-op failure", fb.types())
	}
}

func TestBroadcast_CreateDeleteAndTrashEmit(t *testing.T) {
	repo := &mockRepo{
		create: func(_ context.Context, b bucket.Bucket) (bucket.Bucket, error) { return b, nil },
		delete: func(context.Context, string, string) error { return nil },
		getByID: func(_ context.Context, _, id string) (bucket.Bucket, error) {
			return unprocessed(id), nil
		},
		markProcessed: func(_ context.Context, _, id, _ string, _ bucket.ProcessedRefs) (bucket.Bucket, error) {
			return unprocessed(id), nil
		},
	}
	fb := &fakeBroadcaster{}
	svc := bucket.NewService(repo, &mockTaskCreator{}, nil, fb)

	if _, err := svc.Create(context.Background(), "u1", "note"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := svc.Delete(context.Background(), "u1", "b1", "free"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := svc.Process(context.Background(), "u1", "b2", "free",
		bucket.ProcessBucketRequest{ProcessingResult: bucket.ResultTrash}); err != nil {
		t.Fatalf("trash: %v", err)
	}

	want := []string{bucket.EventCreated, bucket.EventDeleted, bucket.EventProcessed}
	got := fb.types()
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("events = %v, want %v", got, want)
	}
}
