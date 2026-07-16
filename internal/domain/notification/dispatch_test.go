package notification_test

import (
	"context"
	"testing"

	"github.com/nicoflow/nicoflow-api/internal/domain/notification"
)

func TestShouldSendEmail(t *testing.T) {
	tests := []struct {
		name   string
		plan   string
		digest bool
		want   bool
	}{
		{"pro + digest on", "pro", true, true},
		{"pro + digest off", "pro", false, false},
		{"free + digest on", "free", true, false},
		{"free + digest off", "free", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := notification.ShouldSendEmail(tt.plan, tt.digest); got != tt.want {
				t.Fatalf("ShouldSendEmail(%q,%v) = %v, want %v", tt.plan, tt.digest, got, tt.want)
			}
		})
	}
}

func TestShouldSendPush(t *testing.T) {
	tests := []struct {
		name    string
		plan    string
		enabled bool
		hasSub  bool
		want    bool
	}{
		{"pro + on + sub", "pro", true, true, true},
		{"pro + on + no sub", "pro", true, false, false},
		{"pro + off + sub", "pro", false, true, false},
		{"free + on + sub", "free", true, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := notification.ShouldSendPush(tt.plan, tt.enabled, tt.hasSub); got != tt.want {
				t.Fatalf("ShouldSendPush = %v, want %v", got, tt.want)
			}
		})
	}
}

// ── channel senders + recipient/prefs fakes ─────────────────────────────────

type fakeEmailSender struct{ calls int }

func (f *fakeEmailSender) Send(_ context.Context, _ string, _ notification.NotificationView) error {
	f.calls++
	return nil
}

type fakePushSender struct{ calls int }

func (f *fakePushSender) Send(_ context.Context, _ string, _ notification.NotificationView) error {
	f.calls++
	return nil
}

// dispatchRepo is a minimal repo that inserts everything and serves a fixed
// recipient + preferences, so Create → dispatchChannels runs end to end.
type dispatchRepo struct {
	*mockRepo
	recipient notification.Recipient
	prefs     notification.Preferences
}

func (d *dispatchRepo) GetRecipient(_ context.Context, _ string) (notification.Recipient, error) {
	return d.recipient, nil
}
func (d *dispatchRepo) GetPreferences(_ context.Context, _ string) (notification.Preferences, error) {
	return d.prefs, nil
}

func newDispatchService(t *testing.T, rec notification.Recipient, prefs notification.Preferences) (notification.Service, *fakeEmailSender, *fakePushSender) {
	t.Helper()
	repo := &dispatchRepo{
		mockRepo: &mockRepo{
			insertIfAbsent: func(_ context.Context, n notification.Notification) (notification.Notification, bool, error) {
				return n, true, nil // always a fresh insert
			},
		},
		recipient: rec,
		prefs:     prefs,
	}
	email := &fakeEmailSender{}
	push := &fakePushSender{}
	svc := notification.NewService(repo, nil).WithEmailSender(email).WithPushSender(push)
	return svc, email, push
}

// AC1: free user → no email, no push (in-app row still created — Create returns inserted).
func TestDispatch_FreeUserChannelsSuppressed(t *testing.T) {
	svc, email, push := newDispatchService(t,
		notification.Recipient{UserID: "u1", Email: "u1@x.test", Plan: "free"},
		notification.Preferences{EmailDigest: true, PushEnabled: true},
	)

	_, inserted, err := svc.Create(context.Background(), notification.Notification{UserID: "u1", Type: notification.TypeTaskCompleted})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !inserted {
		t.Fatal("in-app notification must still be created for a free user")
	}
	if email.calls != 0 || push.calls != 0 {
		t.Fatalf("free user got channels: email=%d push=%d, want 0/0", email.calls, push.calls)
	}
}

// AC2: Pro user with email digest on + push on → both channels fire.
func TestDispatch_ProChannelsFire(t *testing.T) {
	svc, email, push := newDispatchService(t,
		notification.Recipient{UserID: "u1", Email: "u1@x.test", Plan: "pro"},
		notification.Preferences{EmailDigest: true, PushEnabled: true},
	)

	if _, _, err := svc.Create(context.Background(), notification.Notification{UserID: "u1", Type: notification.TypeTaskCompleted}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if email.calls != 1 || push.calls != 1 {
		t.Fatalf("pro channels: email=%d push=%d, want 1/1", email.calls, push.calls)
	}
}

// Pro user with prefs off → no channels (gate respects preferences).
func TestDispatch_ProPrefsOffSuppressed(t *testing.T) {
	svc, email, push := newDispatchService(t,
		notification.Recipient{UserID: "u1", Email: "u1@x.test", Plan: "pro"},
		notification.Preferences{EmailDigest: false, PushEnabled: false},
	)

	if _, _, err := svc.Create(context.Background(), notification.Notification{UserID: "u1", Type: notification.TypeTaskCompleted}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if email.calls != 0 || push.calls != 0 {
		t.Fatalf("prefs off: email=%d push=%d, want 0/0", email.calls, push.calls)
	}
}

// A duplicate (dedupe-held) notification does not dispatch channels.
func TestDispatch_DuplicateNoChannels(t *testing.T) {
	repo := &dispatchRepo{
		mockRepo: &mockRepo{
			insertIfAbsent: func(_ context.Context, _ notification.Notification) (notification.Notification, bool, error) {
				return notification.Notification{}, false, nil // dedupe held
			},
		},
		recipient: notification.Recipient{UserID: "u1", Plan: "pro"},
		prefs:     notification.Preferences{EmailDigest: true, PushEnabled: true},
	}
	email := &fakeEmailSender{}
	push := &fakePushSender{}
	svc := notification.NewService(repo, nil).WithEmailSender(email).WithPushSender(push)

	if _, inserted, err := svc.Create(context.Background(), notification.Notification{UserID: "u1", Type: notification.TypeTaskCompleted}); err != nil || inserted {
		t.Fatalf("Create dup: inserted=%v err=%v, want false/nil", inserted, err)
	}
	if email.calls != 0 || push.calls != 0 {
		t.Fatalf("duplicate dispatched channels: email=%d push=%d, want 0/0", email.calls, push.calls)
	}
}
