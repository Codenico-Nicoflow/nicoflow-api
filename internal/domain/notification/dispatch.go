package notification

import (
	"context"

	"github.com/rs/zerolog/log"
)

// planPro is the plan value that unlocks Pro channels (email digest, web push).
const planPro = "pro"

// Recipient is the per-user context the dispatch policy needs to decide which
// out-of-app channels a notification may go to. In-app + WS are always free and
// are NOT gated here.
type Recipient struct {
	UserID string
	Email  string
	Plan   string
}

// PushSender delivers a web push to a user's active subscriptions. It is the seam
// the web-push subsystem (NIC-1580) plugs into; nil here means "no push configured"
// and every push is a silent no-op (safe local/dev). Defined in the consumer.
type PushSender interface {
	// Send pushes the notification to the user's subscriptions. It returns nil on a
	// best-effort basis; a dead subscription is pruned by the implementation.
	Send(ctx context.Context, userID string, view NotificationView) error
}

// EmailSender delivers a single-notification email. Like PushSender it is a seam —
// nil means email is a no-op. (The batched Pro due digest still lives in the sweep;
// this is the per-notification path, gated by the same policy.)
type EmailSender interface {
	Send(ctx context.Context, to string, view NotificationView) error
}

// ShouldSendEmail is the single source of truth for the email-channel gate: Pro
// plan with the email-digest preference on. Every producer and the dispatcher call
// this — none reimplement the plan/pref check.
func ShouldSendEmail(plan string, emailDigest bool) bool {
	return plan == planPro && emailDigest
}

// ShouldSendPush is the single source of truth for the web-push gate: Pro plan,
// push preference on, and at least one active subscription.
func ShouldSendPush(plan string, pushEnabled, hasSubscription bool) bool {
	return plan == planPro && pushEnabled && hasSubscription
}

// dispatchChannels sends the out-of-app channels (email, web push) for a freshly
// created notification, gated by plan + preferences + subscription presence. In-app
// persistence and the WS broadcast happen in Create and are NOT gated here — they
// are free for every plan. Channel failures are logged, never propagated: a push or
// email problem must not fail notification creation.
func (s *service) dispatchChannels(ctx context.Context, r Recipient, view NotificationView) {
	prefs, err := s.repo.GetPreferences(ctx, r.UserID)
	if err != nil {
		log.Error().Err(err).Str("user_id", r.UserID).Msg("dispatch: load prefs failed")
		return
	}

	if s.emailSender != nil && ShouldSendEmail(r.Plan, prefs.EmailDigest) {
		if err := s.emailSender.Send(ctx, r.Email, view); err != nil {
			log.Error().Err(err).Str("user_id", r.UserID).Msg("dispatch: email send failed")
		}
	}

	// Push gating consults the subscription store via the sender itself (it prunes
	// dead endpoints), so the presence check lives inside PushSender.Send. The policy
	// gate here is plan + pushEnabled; an empty subscription set is a no-op there.
	if s.pushSender != nil && r.Plan == planPro && prefs.PushEnabled {
		if err := s.pushSender.Send(ctx, r.UserID, view); err != nil {
			log.Error().Err(err).Str("user_id", r.UserID).Msg("dispatch: push send failed")
		}
	}
}
