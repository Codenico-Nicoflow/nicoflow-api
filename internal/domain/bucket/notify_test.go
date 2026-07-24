package bucket_test

import (
	"context"
	"errors"
	"testing"

	"github.com/nicoflow/nicoflow-api/internal/domain/bucket"
	"github.com/nicoflow/nicoflow-api/internal/domain/notification"
)

// fakeNotifier records Create calls and can be forced to error. It structurally
// satisfies the unexported bucket.notifier param of NewService.
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

// trashSvc builds a bucket service whose trash-process marks succeed and whose
// unprocessed count returns `remaining`, wired to the given notifier.
func trashSvc(remaining int, fn *fakeNotifier) bucket.Service {
	repo := &mockRepo{
		markProcessed: func(_ context.Context, _, id, result string, _, _ *string) (bucket.Bucket, error) {
			b := unprocessed(id)
			b.ProcessingResult = &result
			return b, nil
		},
		countUnprocessed: func(context.Context, string) (int, error) { return remaining, nil },
	}
	return bucket.NewService(repo, &mockTaskCreator{}, fn, nil)
}

func processTrash(t *testing.T, svc bucket.Service, plan string) {
	t.Helper()
	if _, err := svc.Process(context.Background(), "u1", "b1", plan,
		bucket.ProcessBucketRequest{ProcessingResult: bucket.ResultTrash}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInboxZero_ProGatedAndCountGated(t *testing.T) {
	tests := []struct {
		name      string
		plan      string
		remaining int
		wantFire  bool
	}{
		{"pro, count hits 0 -> fires", "pro", 0, true},
		{"pro, items remain -> no fire", "pro", 3, false},
		{"free, count hits 0 -> no fire (Pro-only)", "free", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fn := &fakeNotifier{}
			processTrash(t, trashSvc(tt.remaining, fn), tt.plan)
			fired := len(fn.calls) == 1 && fn.calls[0].Type == notification.TypeInboxZero
			if fired != tt.wantFire {
				t.Fatalf("fired=%v, want %v (calls=%d)", fired, tt.wantFire, len(fn.calls))
			}
			if tt.wantFire {
				k := fn.calls[0].DedupeKey
				if k == nil || len(*k) < len("inbox_zero:u1:") || (*k)[:len("inbox_zero:u1:")] != "inbox_zero:u1:" {
					t.Errorf("dedupeKey = %v, want inbox_zero:u1:<date>", k)
				}
			}
		})
	}
}

// Deleting an unprocessed item that clears the inbox fires inbox_zero for Pro.
func TestInboxZero_OnDelete(t *testing.T) {
	fn := &fakeNotifier{}
	repo := &mockRepo{
		delete:           func(context.Context, string, string) error { return nil },
		countUnprocessed: func(context.Context, string) (int, error) { return 0, nil },
	}
	svc := bucket.NewService(repo, &mockTaskCreator{}, fn, nil)
	if err := svc.Delete(context.Background(), "u1", "b1", "pro"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fn.calls) != 1 || fn.calls[0].Type != notification.TypeInboxZero {
		t.Fatalf("want one inbox_zero, got %d calls", len(fn.calls))
	}
}

// AC: a notifier error must never fail the mutation.
func TestInboxZero_NotifyErrorDoesNotFailMutation(t *testing.T) {
	fn := &fakeNotifier{failErr: errors.New("boom")}
	if _, err := trashSvc(0, fn).Process(context.Background(), "u1", "b1", "pro",
		bucket.ProcessBucketRequest{ProcessingResult: bucket.ResultTrash}); err != nil {
		t.Fatalf("notify error leaked into mutation: %v", err)
	}
}

// A CountUnprocessed failure is swallowed: no notification, mutation succeeds.
func TestInboxZero_CountErrorSwallowed(t *testing.T) {
	fn := &fakeNotifier{}
	repo := &mockRepo{
		markProcessed: func(_ context.Context, _, id, result string, _, _ *string) (bucket.Bucket, error) {
			b := unprocessed(id)
			b.ProcessingResult = &result
			return b, nil
		},
		countUnprocessed: func(context.Context, string) (int, error) { return 0, errors.New("db down") },
	}
	svc := bucket.NewService(repo, &mockTaskCreator{}, fn, nil)
	if _, err := svc.Process(context.Background(), "u1", "b1", "pro",
		bucket.ProcessBucketRequest{ProcessingResult: bucket.ResultTrash}); err != nil {
		t.Fatalf("count error leaked into mutation: %v", err)
	}
	if len(fn.calls) != 0 {
		t.Fatalf("no notification expected when count fails, got %d", len(fn.calls))
	}
}

// A Delete repo error is returned and skips the inbox_zero emission.
func TestInboxZero_DeleteErrorReturned(t *testing.T) {
	fn := &fakeNotifier{}
	repo := &mockRepo{
		delete:           func(context.Context, string, string) error { return errors.New("not found") },
		countUnprocessed: func(context.Context, string) (int, error) { return 0, nil },
	}
	svc := bucket.NewService(repo, &mockTaskCreator{}, fn, nil)
	if err := svc.Delete(context.Background(), "u1", "b1", "pro"); err == nil {
		t.Fatal("expected delete error to propagate")
	}
	if len(fn.calls) != 0 {
		t.Fatalf("no emission when delete fails, got %d", len(fn.calls))
	}
}
