package jobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nicoflow/nicoflow-api/pkg/emailutil"
)

// sentDigest records one captured digest send.
type sentDigest struct {
	to    string
	tasks []emailutil.DigestTask
	dsn   string
}

// digestSpy records sends and can be primed to fail for specific recipients.
type digestSpy struct {
	sent    []sentDigest
	failFor map[string]bool // recipient → return an error
}

func (d *digestSpy) send(to string, tasks []emailutil.DigestTask, dsn string) error {
	d.sent = append(d.sent, sentDigest{to: to, tasks: tasks, dsn: dsn})
	if d.failFor[to] {
		return errors.New("smtp boom")
	}
	return nil
}

// at0800 is a clock pinned to a UTC hour that is local 08:00 for UTC users.
func at0800() time.Time { return time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC) }

func newNotifierWithDigest(t *testing.T, users []RemindableUser, tasks map[string][]DueTask, spy *digestSpy, dsn string) *DueDateNotifier {
	t.Helper()
	repo := &fakeRepo{users: users, tasksByID: tasks}
	n := NewDueDateNotifier(repo, &fakeCreator{inserted: true}, dsn)
	n.now = at0800
	SetDigestSender(n, spy.send)
	return n
}

func TestDigest_ProWithDigestOnSends(t *testing.T) {
	spy := &digestSpy{}
	users := []RemindableUser{
		{UserID: "u1", Email: "pro@x.test", Plan: "pro", Timezone: "UTC", BeforeDueMinutes: 1440, EmailDigest: true},
	}
	tasks := map[string][]DueTask{"u1": {{ID: "t1", Title: "A"}, {ID: "t2", Title: "B"}}}
	n := newNotifierWithDigest(t, users, tasks, spy, "smtp://x")

	if _, err := n.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(spy.sent) != 1 {
		t.Fatalf("sends = %d, want 1", len(spy.sent))
	}
	if spy.sent[0].to != "pro@x.test" || len(spy.sent[0].tasks) != 2 {
		t.Fatalf("send = %+v, want pro@x.test with 2 tasks", spy.sent[0])
	}
}

func TestDigest_FreeUserSkipped(t *testing.T) {
	spy := &digestSpy{}
	users := []RemindableUser{
		{UserID: "u1", Email: "free@x.test", Plan: "free", Timezone: "UTC", BeforeDueMinutes: 1440, EmailDigest: true},
	}
	tasks := map[string][]DueTask{"u1": {{ID: "t1", Title: "A"}}}
	n := newNotifierWithDigest(t, users, tasks, spy, "smtp://x")

	if _, err := n.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(spy.sent) != 0 {
		t.Fatalf("sends = %d, want 0 (free user)", len(spy.sent))
	}
}

func TestDigest_ProOptedOutSkipped(t *testing.T) {
	spy := &digestSpy{}
	users := []RemindableUser{
		{UserID: "u1", Email: "pro@x.test", Plan: "pro", Timezone: "UTC", BeforeDueMinutes: 1440, EmailDigest: false},
	}
	tasks := map[string][]DueTask{"u1": {{ID: "t1", Title: "A"}}}
	n := newNotifierWithDigest(t, users, tasks, spy, "smtp://x")

	if _, err := n.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(spy.sent) != 0 {
		t.Fatalf("sends = %d, want 0 (opted out)", len(spy.sent))
	}
}

func TestDigest_NoTasksNoSend(t *testing.T) {
	spy := &digestSpy{}
	users := []RemindableUser{
		{UserID: "u1", Email: "pro@x.test", Plan: "pro", Timezone: "UTC", BeforeDueMinutes: 1440, EmailDigest: true},
	}
	// No tasks scheduled → nothing to summarise.
	n := newNotifierWithDigest(t, users, map[string][]DueTask{}, spy, "smtp://x")

	if _, err := n.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(spy.sent) != 0 {
		t.Fatalf("sends = %d, want 0 (no tasks)", len(spy.sent))
	}
}

func TestDigest_BatchIsolation(t *testing.T) {
	// First Pro user's send errors; the second must still be attempted.
	spy := &digestSpy{failFor: map[string]bool{"first@x.test": true}}
	users := []RemindableUser{
		{UserID: "u1", Email: "first@x.test", Plan: "pro", Timezone: "UTC", BeforeDueMinutes: 1440, EmailDigest: true},
		{UserID: "u2", Email: "second@x.test", Plan: "pro", Timezone: "UTC", BeforeDueMinutes: 1440, EmailDigest: true},
	}
	tasks := map[string][]DueTask{
		"u1": {{ID: "t1", Title: "A"}},
		"u2": {{ID: "t2", Title: "B"}},
	}
	n := newNotifierWithDigest(t, users, tasks, spy, "smtp://x")

	if _, err := n.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(spy.sent) != 2 {
		t.Fatalf("sends = %d, want 2 (first errored, second still sent)", len(spy.sent))
	}
	if spy.sent[1].to != "second@x.test" {
		t.Fatalf("second send to %q, want second@x.test", spy.sent[1].to)
	}
}
