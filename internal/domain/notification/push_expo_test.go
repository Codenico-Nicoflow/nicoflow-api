package notification_test

import (
	"context"
	"errors"
	"testing"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/internal/domain/notification"
	"github.com/nicoflow/nicoflow-api/pkg/expopush"
)

// fakeExpoSender records batches and can force a per-token outcome.
type fakeExpoSender struct {
	batches      [][]expopush.Message
	unregistered map[string]bool
	failWith     error
}

func (f *fakeExpoSender) Send(_ context.Context, msgs []expopush.Message) ([]expopush.Result, error) {
	f.batches = append(f.batches, msgs)
	if f.failWith != nil {
		return nil, f.failWith
	}
	out := make([]expopush.Result, len(msgs))
	for i, m := range msgs {
		out[i] = expopush.Result{Token: m.To, Unregistered: f.unregistered[m.To]}
	}
	return out, nil
}

func codeOf(t *testing.T, err error) string {
	t.Helper()
	var appErr *apperror.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("want *apperror.AppError, got %T (%v)", err, err)
	}
	return appErr.Code
}

func TestSubscribe_Platform(t *testing.T) {
	tests := []struct {
		name     string
		plan     string
		req      notification.SubscribeRequest
		wantCode string // "" ⇒ success
		wantSub  notification.PushSubscription
	}{
		{
			name:    "web shape stores a web row",
			plan:    "pro",
			req:     notification.SubscribeRequest{Endpoint: "https://push/1", P256dhKey: "p", AuthKey: "a"},
			wantSub: notification.PushSubscription{Platform: "web", Endpoint: "https://push/1", P256dhKey: "p", AuthKey: "a"},
		},
		{
			// Web clients predating mobile never send `platform`; an omitted
			// value must keep working exactly as before.
			name:    "omitted platform defaults to web",
			plan:    "pro",
			req:     notification.SubscribeRequest{Platform: "", Endpoint: "https://push/2", P256dhKey: "p", AuthKey: "a"},
			wantSub: notification.PushSubscription{Platform: "web", Endpoint: "https://push/2", P256dhKey: "p", AuthKey: "a"},
		},
		{
			name:    "expo shape stores an expo row",
			plan:    "pro",
			req:     notification.SubscribeRequest{Platform: "expo", ExpoPushToken: "ExponentPushToken[abc]", DeviceID: "device-1"},
			wantSub: notification.PushSubscription{Platform: "expo", ExpoPushToken: "ExponentPushToken[abc]", DeviceID: "device-1"},
		},
		{
			name:     "expo without a token is invalid",
			plan:     "pro",
			req:      notification.SubscribeRequest{Platform: "expo"},
			wantCode: apperror.ErrInvalidInput,
		},
		{
			name:     "web without keys is invalid",
			plan:     "pro",
			req:      notification.SubscribeRequest{Endpoint: "https://push/1"},
			wantCode: apperror.ErrInvalidInput,
		},
		{
			name:     "unknown platform is rejected",
			plan:     "pro",
			req:      notification.SubscribeRequest{Platform: "fcm", ExpoPushToken: "t"},
			wantCode: apperror.ErrInvalidInput,
		},
		{
			name:     "free plan is gated on web",
			plan:     "free",
			req:      notification.SubscribeRequest{Endpoint: "https://push/1", P256dhKey: "p", AuthKey: "a"},
			wantCode: apperror.ErrPlanLimitExceeded,
		},
		{
			// The Pro gate must not be bypassable by switching platform.
			name:     "free plan is gated on expo",
			plan:     "free",
			req:      notification.SubscribeRequest{Platform: "expo", ExpoPushToken: "ExponentPushToken[abc]"},
			wantCode: apperror.ErrPlanLimitExceeded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo := newSubService()

			err := svc.Subscribe(context.Background(), "u1", tt.plan, tt.req)

			if tt.wantCode != "" {
				if got := codeOf(t, err); got != tt.wantCode {
					t.Fatalf("want code %s, got %s", tt.wantCode, got)
				}
				if len(repo.upserted) != 0 {
					t.Fatalf("a rejected subscribe must store nothing, got %v", repo.upserted)
				}
				return
			}

			if err != nil {
				t.Fatalf("Subscribe: %v", err)
			}
			if len(repo.upserted) != 1 {
				t.Fatalf("want 1 stored subscription, got %d", len(repo.upserted))
			}
			if got := repo.upserted[0]; got != tt.wantSub {
				t.Fatalf("stored %+v, want %+v", got, tt.wantSub)
			}
		})
	}
}

func TestUnsubscribe_ByEitherIdentifier(t *testing.T) {
	tests := []struct {
		name     string
		req      notification.SubscribeRequest
		wantGone string
		wantCode string
	}{
		{
			name:     "web unsubscribes by endpoint",
			req:      notification.SubscribeRequest{Endpoint: "https://push/1"},
			wantGone: "https://push/1",
		},
		{
			name:     "mobile unsubscribes by expo token",
			req:      notification.SubscribeRequest{ExpoPushToken: "ExponentPushToken[abc]"},
			wantGone: "ExponentPushToken[abc]",
		},
		{
			name:     "neither identifier is invalid",
			req:      notification.SubscribeRequest{},
			wantCode: apperror.ErrInvalidInput,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo := newSubService()

			err := svc.Unsubscribe(context.Background(), "u1", tt.req)

			if tt.wantCode != "" {
				if got := codeOf(t, err); got != tt.wantCode {
					t.Fatalf("want code %s, got %s", tt.wantCode, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Unsubscribe: %v", err)
			}
			if len(repo.deleted) != 1 || repo.deleted[0] != tt.wantGone {
				t.Fatalf("want %q deleted, got %v", tt.wantGone, repo.deleted)
			}
		})
	}
}

// The fanout must route each subscription to its own transport — a web row must
// never reach Expo, and an expo row must never be handed to the VAPID sender
// (it has no endpoint or keys).
func TestPushSender_RoutesByPlatform(t *testing.T) {
	repo := &pushSenderRepo{mockRepo: &mockRepo{}, subs: []notification.PushSubscription{
		{Platform: "web", Endpoint: "https://push/1", P256dhKey: "p", AuthKey: "a"},
		{Platform: "expo", ExpoPushToken: "ExponentPushToken[abc]"},
	}}
	web := &fakeSender{}
	expo := &fakeExpoSender{}
	ps := notification.NewPushSender(repo, web, expo)

	if err := ps.Send(context.Background(), "u1", notification.NotificationView{Title: "T", Body: "B"}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if len(web.sent) != 1 || web.sent[0] != "https://push/1" {
		t.Fatalf("web sender got %v, want the web endpoint only", web.sent)
	}
	if len(expo.batches) != 1 || len(expo.batches[0]) != 1 {
		t.Fatalf("want one expo batch of one, got %v", expo.batches)
	}
	if got := expo.batches[0][0]; got.To != "ExponentPushToken[abc]" || got.Title != "T" || got.Body != "B" {
		t.Fatalf("expo message %+v does not carry the notification", got)
	}
}

// DeviceNotRegistered is Expo's analogue of Web Push's 410 — the token is dead
// and must be pruned so it is never retried.
func TestPushSender_PrunesUnregisteredExpoToken(t *testing.T) {
	repo := &pushSenderRepo{mockRepo: &mockRepo{}, subs: []notification.PushSubscription{
		{Platform: "expo", ExpoPushToken: "dead"},
		{Platform: "expo", ExpoPushToken: "live"},
	}}
	expo := &fakeExpoSender{unregistered: map[string]bool{"dead": true}}
	ps := notification.NewPushSender(repo, &fakeSender{}, expo)

	if err := ps.Send(context.Background(), "u1", notification.NotificationView{Title: "T"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(repo.expoDeleted) != 1 || repo.expoDeleted[0] != "dead" {
		t.Fatalf("want only the dead token pruned, got %v", repo.expoDeleted)
	}
}

// A failing Expo batch must not fail the send — push is best-effort and must
// never break the notification write that triggered it, nor prune live tokens.
func TestPushSender_ExpoBatchFailureIsBestEffort(t *testing.T) {
	repo := &pushSenderRepo{mockRepo: &mockRepo{}, subs: []notification.PushSubscription{
		{Platform: "expo", ExpoPushToken: "t1"},
	}}
	expo := &fakeExpoSender{failWith: errors.New("expo down")}
	ps := notification.NewPushSender(repo, &fakeSender{}, expo)

	if err := ps.Send(context.Background(), "u1", notification.NotificationView{Title: "T"}); err != nil {
		t.Fatalf("a failed expo batch must not fail Send, got %v", err)
	}
	if len(repo.expoDeleted) != 0 {
		t.Fatalf("a transport failure must not prune tokens, got %v", repo.expoDeleted)
	}
}
