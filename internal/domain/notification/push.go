package notification

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/rs/zerolog/log"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/pkg/pushutil"
)

// PushSubscription is a stored browser push subscription (one row per user+endpoint).
type PushSubscription struct {
	Endpoint  string
	P256dhKey string
	AuthKey   string
	UserAgent string
}

// SubscribeRequest is the body of POST /v1/notifications/push/subscribe — the shape
// a browser's PushSubscription.toJSON() produces, flattened for our API.
type SubscribeRequest struct {
	Endpoint  string `json:"endpoint"`
	P256dhKey string `json:"p256dhKey"`
	AuthKey   string `json:"authKey"`
	UserAgent string `json:"userAgent"`
}

// Subscribe stores a web-push subscription for the user. Pro-gated per SPEC §5:
// free-plan callers get PLAN_LIMIT_EXCEEDED and nothing is stored. Upserts on
// endpoint so a repeat subscribe from the same browser refreshes, not duplicates.
func (s *service) Subscribe(ctx context.Context, userID, plan string, req SubscribeRequest) error {
	if plan != planPro {
		return apperror.New(http.StatusForbidden, apperror.ErrPlanLimitExceeded, "web push is a Pro feature")
	}
	if req.Endpoint == "" || req.P256dhKey == "" || req.AuthKey == "" {
		return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "endpoint, p256dhKey and authKey are required")
	}
	return s.repo.UpsertPushSubscription(ctx, userID, PushSubscription(req))
}

// Unsubscribe removes the user's subscription for the endpoint. Idempotent — no
// plan gate (a downgraded user must still be able to remove a subscription).
func (s *service) Unsubscribe(ctx context.Context, userID, endpoint string) error {
	if endpoint == "" {
		return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "endpoint is required")
	}
	return s.repo.DeletePushSubscription(ctx, userID, endpoint)
}

// pushRepo is the subset of Repository the push sender needs. Kept narrow so the
// sender can be tested with a small fake.
type pushRepo interface {
	ListPushSubscriptions(ctx context.Context, userID string) ([]PushSubscription, error)
	DeletePushSubscription(ctx context.Context, userID, endpoint string) error
}

// pushDispatcher satisfies the PushSender seam (dispatch.go): it fans a created
// notification out to all of a user's subscriptions via pushutil, pruning any that
// the push service reports as gone (404/410). Best-effort — a per-subscription
// failure is logged and never propagated.
type pushDispatcher struct {
	repo   pushRepo
	sender pushutil.Sender
}

// NewPushSender builds the PushSender wired into notification.Service via
// WithPushSender. Pass the configured pushutil.Sender (a no-op when VAPID is unset,
// which makes every send a silent no-op).
func NewPushSender(repo Repository, sender pushutil.Sender) PushSender {
	return &pushDispatcher{repo: repo, sender: sender}
}

// Send delivers the notification to every active subscription of the user, pruning
// expired ones. An empty subscription set is a no-op.
func (d *pushDispatcher) Send(ctx context.Context, userID string, view NotificationView) error {
	subs, err := d.repo.ListPushSubscriptions(ctx, userID)
	if err != nil {
		return err
	}
	if len(subs) == 0 {
		return nil
	}

	payload, err := json.Marshal(struct {
		Title    string          `json:"title"`
		Body     string          `json:"body"`
		Type     string          `json:"type"`
		Metadata json.RawMessage `json:"metadata,omitempty"`
	}{Title: view.Title, Body: view.Body, Type: view.Type, Metadata: view.Metadata})
	if err != nil {
		return err
	}

	for _, s := range subs {
		res, sendErr := d.sender.Send(ctx, pushutil.Subscription{
			Endpoint:  s.Endpoint,
			P256dhKey: s.P256dhKey,
			AuthKey:   s.AuthKey,
		}, payload)
		if res.Expired {
			// Dead endpoint → prune so we stop trying it.
			if delErr := d.repo.DeletePushSubscription(ctx, userID, s.Endpoint); delErr != nil {
				log.Error().Err(delErr).Str("user_id", userID).Msg("push: prune expired subscription failed")
			}
			continue
		}
		if sendErr != nil {
			log.Error().Err(sendErr).Str("user_id", userID).Msg("push: send failed")
		}
	}
	return nil
}
