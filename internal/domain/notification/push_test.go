package notification_test

import (
	"context"
	"errors"
	"testing"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/internal/domain/notification"
	"github.com/nicoflow/nicoflow-api/pkg/pushutil"
)

// ── Subscribe / Unsubscribe (service, Pro gate) ─────────────────────────────

// subRepo records subscription writes so the service tests can assert them.
type subRepo struct {
	*mockRepo
	upserted []notification.PushSubscription
	deleted  []string
}

func (r *subRepo) UpsertPushSubscription(_ context.Context, _ string, sub notification.PushSubscription) error {
	r.upserted = append(r.upserted, sub)
	return nil
}
func (r *subRepo) DeletePushSubscription(_ context.Context, _, endpoint string) error {
	r.deleted = append(r.deleted, endpoint)
	return nil
}

func newSubService() (notification.Service, *subRepo) {
	repo := &subRepo{mockRepo: &mockRepo{}}
	return notification.NewService(repo, nil), repo
}

// AC1: a Pro user subscribing stores the row.
func TestSubscribe_ProStores(t *testing.T) {
	svc, repo := newSubService()
	err := svc.Subscribe(context.Background(), "u1", "pro", notification.SubscribeRequest{
		Endpoint: "https://push/1", P256dhKey: "p", AuthKey: "a",
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if len(repo.upserted) != 1 || repo.upserted[0].Endpoint != "https://push/1" {
		t.Fatalf("want one upsert of the endpoint, got %+v", repo.upserted)
	}
}

// AC2: a free user is gated with PLAN_LIMIT_EXCEEDED and nothing is stored.
func TestSubscribe_FreeGated(t *testing.T) {
	svc, repo := newSubService()
	err := svc.Subscribe(context.Background(), "u1", "free", notification.SubscribeRequest{
		Endpoint: "https://push/1", P256dhKey: "p", AuthKey: "a",
	})
	var ae *apperror.AppError
	if !errors.As(err, &ae) || ae.Code != apperror.ErrPlanLimitExceeded {
		t.Fatalf("want PLAN_LIMIT_EXCEEDED, got %v", err)
	}
	if len(repo.upserted) != 0 {
		t.Fatalf("free user must not store a subscription, got %+v", repo.upserted)
	}
}

// Missing keys → INVALID_INPUT, nothing stored.
func TestSubscribe_MissingFields(t *testing.T) {
	svc, repo := newSubService()
	err := svc.Subscribe(context.Background(), "u1", "pro", notification.SubscribeRequest{Endpoint: "https://push/1"})
	var ae *apperror.AppError
	if !errors.As(err, &ae) || ae.Code != apperror.ErrInvalidInput {
		t.Fatalf("want INVALID_INPUT, got %v", err)
	}
	if len(repo.upserted) != 0 {
		t.Fatalf("invalid subscribe must store nothing, got %+v", repo.upserted)
	}
}

func TestUnsubscribe_Deletes(t *testing.T) {
	svc, repo := newSubService()
	if err := svc.Unsubscribe(context.Background(), "u1", "https://push/1"); err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}
	if len(repo.deleted) != 1 || repo.deleted[0] != "https://push/1" {
		t.Fatalf("want one delete of the endpoint, got %+v", repo.deleted)
	}
}

// ── PushSender (fan-out + prune) ────────────────────────────────────────────

// pushSenderRepo serves a fixed subscription list and records prunes.
type pushSenderRepo struct {
	*mockRepo
	subs    []notification.PushSubscription
	deleted []string
}

func (r *pushSenderRepo) ListPushSubscriptions(_ context.Context, _ string) ([]notification.PushSubscription, error) {
	return r.subs, nil
}
func (r *pushSenderRepo) DeletePushSubscription(_ context.Context, _, endpoint string) error {
	r.deleted = append(r.deleted, endpoint)
	return nil
}

// fakeSender returns a per-endpoint result so tests can force an expired response.
type fakeSender struct {
	expired map[string]bool
	sent    []string
}

func (f *fakeSender) Send(_ context.Context, sub pushutil.Subscription, _ []byte) (pushutil.Result, error) {
	f.sent = append(f.sent, sub.Endpoint)
	if f.expired[sub.Endpoint] {
		return pushutil.Result{Expired: true}, errors.New("410 gone")
	}
	return pushutil.Result{}, nil
}

// AC3: a Pro user with an active subscription → push is sent.
func TestPushSender_Sends(t *testing.T) {
	repo := &pushSenderRepo{mockRepo: &mockRepo{}, subs: []notification.PushSubscription{
		{Endpoint: "https://push/1", P256dhKey: "p", AuthKey: "a"},
	}}
	sender := &fakeSender{}
	ps := notification.NewPushSender(repo, sender)

	if err := ps.Send(context.Background(), "u1", notification.NotificationView{Title: "T", Body: "B"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(sender.sent) != 1 || len(repo.deleted) != 0 {
		t.Fatalf("want 1 send / 0 prune, got sent=%v deleted=%v", sender.sent, repo.deleted)
	}
}

// AC3: a 410 Gone response prunes the subscription.
func TestPushSender_PrunesExpired(t *testing.T) {
	repo := &pushSenderRepo{mockRepo: &mockRepo{}, subs: []notification.PushSubscription{
		{Endpoint: "https://push/dead", P256dhKey: "p", AuthKey: "a"},
		{Endpoint: "https://push/live", P256dhKey: "p", AuthKey: "a"},
	}}
	sender := &fakeSender{expired: map[string]bool{"https://push/dead": true}}
	ps := notification.NewPushSender(repo, sender)

	if err := ps.Send(context.Background(), "u1", notification.NotificationView{Title: "T"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(repo.deleted) != 1 || repo.deleted[0] != "https://push/dead" {
		t.Fatalf("want the dead endpoint pruned, got %v", repo.deleted)
	}
}

// Empty subscription set is a no-op.
func TestPushSender_NoSubsNoOp(t *testing.T) {
	repo := &pushSenderRepo{mockRepo: &mockRepo{}}
	sender := &fakeSender{}
	ps := notification.NewPushSender(repo, sender)

	if err := ps.Send(context.Background(), "u1", notification.NotificationView{}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(sender.sent) != 0 {
		t.Fatalf("no subscriptions → no send, got %v", sender.sent)
	}
}
