package notification

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/rs/zerolog/log"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/pkg/expopush"
	"github.com/nicoflow/nicoflow-api/pkg/pushutil"
)

// Push transports. Web is the historical (and implicit) platform, so a request
// that omits the field is treated as web — existing web clients are unchanged.
const (
	PlatformWeb  = "web"
	PlatformExpo = "expo"
)

// PushSubscription is a stored push subscription. Which fields are populated
// depends on Platform: web rows carry Endpoint/P256dhKey/AuthKey, expo rows carry
// ExpoPushToken (and DeviceID when the client supplies one). The DB enforces the
// same shape rule — see migration 053.
type PushSubscription struct {
	Platform      string
	Endpoint      string
	P256dhKey     string
	AuthKey       string
	UserAgent     string
	ExpoPushToken string
	DeviceID      string
}

// SubscribeRequest is the body of POST /v1/notifications/push/subscribe. It is a
// discriminated union on `platform`:
//
//	web  (default) — { endpoint, p256dhKey, authKey, userAgent }
//	expo           — { platform: "expo", expoPushToken, deviceId? }
//
// Platform is omitted by web clients that predate mobile, so an empty value means
// web rather than an error.
type SubscribeRequest struct {
	Platform      string `json:"platform"`
	Endpoint      string `json:"endpoint"`
	P256dhKey     string `json:"p256dhKey"`
	AuthKey       string `json:"authKey"`
	UserAgent     string `json:"userAgent"`
	ExpoPushToken string `json:"expoPushToken"`
	DeviceID      string `json:"deviceId"`
}

// Subscribe stores a push subscription for the user. Pro-gated per SPEC §5:
// free-plan callers get PLAN_LIMIT_EXCEEDED and nothing is stored, on either
// platform. Upserts so a repeat subscribe from the same browser or device
// refreshes rather than duplicating.
func (s *service) Subscribe(ctx context.Context, userID, plan string, req SubscribeRequest) error {
	if plan != planPro {
		return apperror.New(http.StatusForbidden, apperror.ErrPlanLimitExceeded, "push is a Pro feature")
	}

	platform := req.Platform
	if platform == "" {
		platform = PlatformWeb
	}

	switch platform {
	case PlatformWeb:
		if req.Endpoint == "" || req.P256dhKey == "" || req.AuthKey == "" {
			return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "endpoint, p256dhKey and authKey are required")
		}
	case PlatformExpo:
		if req.ExpoPushToken == "" {
			return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "expoPushToken is required")
		}
	default:
		return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "platform must be web or expo")
	}

	return s.repo.UpsertPushSubscription(ctx, userID, PushSubscription{
		Platform:      platform,
		Endpoint:      req.Endpoint,
		P256dhKey:     req.P256dhKey,
		AuthKey:       req.AuthKey,
		UserAgent:     req.UserAgent,
		ExpoPushToken: req.ExpoPushToken,
		DeviceID:      req.DeviceID,
	})
}

// Unsubscribe removes a subscription. Idempotent — no plan gate (a downgraded
// user must still be able to remove one). Accepts either identifier: web clients
// send the endpoint, mobile sends the Expo token.
func (s *service) Unsubscribe(ctx context.Context, userID string, req SubscribeRequest) error {
	if req.ExpoPushToken != "" {
		return s.repo.DeleteExpoPushSubscription(ctx, userID, req.ExpoPushToken)
	}
	if req.Endpoint == "" {
		return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "endpoint or expoPushToken is required")
	}
	return s.repo.DeletePushSubscription(ctx, userID, req.Endpoint)
}

// pushRepo is the subset of Repository the push sender needs. Kept narrow so the
// sender can be tested with a small fake.
type pushRepo interface {
	ListPushSubscriptions(ctx context.Context, userID string) ([]PushSubscription, error)
	DeletePushSubscription(ctx context.Context, userID, endpoint string) error
	DeleteExpoPushSubscription(ctx context.Context, userID, token string) error
}

// pushDispatcher satisfies the PushSender seam (dispatch.go): it fans a created
// notification out to all of a user's subscriptions, routing each by platform —
// web via pushutil (VAPID), expo via the Expo Push Service — and pruning any the
// respective service reports as gone. Best-effort: a per-subscription failure is
// logged and never propagated.
type pushDispatcher struct {
	repo   pushRepo
	sender pushutil.Sender
	expo   expopush.Sender
}

// NewPushSender builds the PushSender wired into notification.Service via
// WithPushSender. Both senders are no-ops when their transport is unconfigured
// (VAPID unset / Expo disabled), which makes every send a silent no-op.
func NewPushSender(repo Repository, sender pushutil.Sender, expo expopush.Sender) PushSender {
	return &pushDispatcher{repo: repo, sender: sender, expo: expo}
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

	var expoMsgs []expopush.Message
	for _, s := range subs {
		if s.Platform == PlatformExpo {
			// Expo sends in batches, so collect and dispatch after the loop.
			expoMsgs = append(expoMsgs, expopush.Message{
				To:    s.ExpoPushToken,
				Title: view.Title,
				Body:  view.Body,
				Data:  map[string]any{"type": view.Type, "id": view.ID},
			})
			continue
		}

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

	d.sendExpo(ctx, userID, expoMsgs)
	return nil
}

// sendExpo delivers the Expo half of the fanout in batches, pruning tokens Expo
// reports as DeviceNotRegistered. A whole-batch failure is logged and dropped —
// push is best-effort and must never fail the notification write that triggered it.
func (d *pushDispatcher) sendExpo(ctx context.Context, userID string, msgs []expopush.Message) {
	for start := 0; start < len(msgs); start += expopush.MaxBatchSize {
		end := min(start+expopush.MaxBatchSize, len(msgs))

		results, err := d.expo.Send(ctx, msgs[start:end])
		if err != nil {
			log.Error().Err(err).Str("user_id", userID).Msg("push: expo batch failed")
			continue
		}
		for _, res := range results {
			if res.Unregistered {
				if delErr := d.repo.DeleteExpoPushSubscription(ctx, userID, res.Token); delErr != nil {
					log.Error().Err(delErr).Str("user_id", userID).Msg("push: prune expo token failed")
				}
				continue
			}
			if res.Err != nil {
				log.Error().Err(res.Err).Str("user_id", userID).Msg("push: expo send failed")
			}
		}
	}
}
