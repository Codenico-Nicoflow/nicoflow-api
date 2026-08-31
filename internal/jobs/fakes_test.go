package jobs

import (
	"context"

	"github.com/nicoflow/nicoflow-api/internal/domain/notification"
)

// fakeRepo is a fully-scriptable Repository fake shared by the digest sweep
// tests. Each count/list is keyed by userID; a missing key returns the zero
// value (no error) unless the matching *Err map says otherwise.
type fakeRepo struct {
	users []RemindableUser

	scheduled   map[string]int
	overdue     map[string]int
	unprocessed map[string]int
	openTasks   map[string]int
	completed   map[string]int
	streakDates map[string][]string

	scheduledErr   map[string]error
	overdueErr     map[string]error
	unprocessedErr map[string]error
	openTasksErr   map[string]error
	completedErr   map[string]error
}

func (f *fakeRepo) ListRemindableUsers(_ context.Context) ([]RemindableUser, error) {
	return f.users, nil
}

func (f *fakeRepo) CountScheduledOn(_ context.Context, userID, _ string) (int, error) {
	if err := f.scheduledErr[userID]; err != nil {
		return 0, err
	}
	return f.scheduled[userID], nil
}

func (f *fakeRepo) CountOverdue(_ context.Context, userID, _ string) (int, error) {
	if err := f.overdueErr[userID]; err != nil {
		return 0, err
	}
	return f.overdue[userID], nil
}

func (f *fakeRepo) CountUnprocessedInbox(_ context.Context, userID string) (int, error) {
	if err := f.unprocessedErr[userID]; err != nil {
		return 0, err
	}
	return f.unprocessed[userID], nil
}

func (f *fakeRepo) CountOpenTasks(_ context.Context, userID string) (int, error) {
	if err := f.openTasksErr[userID]; err != nil {
		return 0, err
	}
	return f.openTasks[userID], nil
}

func (f *fakeRepo) CountCompletedOn(_ context.Context, userID, _, _ string) (int, error) {
	if err := f.completedErr[userID]; err != nil {
		return 0, err
	}
	return f.completed[userID], nil
}

func (f *fakeRepo) RecentCompletionDates(_ context.Context, userID, _, _ string, _ int) ([]string, error) {
	return f.streakDates[userID], nil
}

// fakeCreator records every Create call. inserted controls the return value
// (false simulates a dedupe-held row: the caller still attempted the call).
type fakeCreator struct {
	calls    []notification.Notification
	inserted bool
}

func (f *fakeCreator) Create(_ context.Context, n notification.Notification) (notification.NotificationView, bool, error) {
	f.calls = append(f.calls, n)
	return notification.NotificationView{}, f.inserted, nil
}
