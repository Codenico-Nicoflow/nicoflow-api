package notification

import (
	"context"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// Broadcaster pushes a notification event to a user's live connections. It is a
// WS-ready seam: nil in this epic (poll-only), satisfied by the real ws.Hub in
// E-022 without changing this service.
type Broadcaster interface {
	Broadcast(userID string, event Event)
}

// Service defines the notification business logic interface.
type Service interface {
	List(ctx context.Context, userID string, f ListNotificationsFilter) (ListNotificationsResponse, error)
	UnreadCount(ctx context.Context, userID string) (UnreadCountResponse, error)
	MarkRead(ctx context.Context, userID, id string) (NotificationView, error)
	MarkAllRead(ctx context.Context, userID string) (CountResponse, error)
	Delete(ctx context.Context, userID, id string) error
	// Create is the single funnel every producer (cron, future announcements) goes
	// through. It inserts idempotently (by dedupe_key) and, when a row is actually
	// created and a broadcaster is wired, emits the full-payload event.
	Create(ctx context.Context, n Notification) (NotificationView, bool, error)

	// GetPreferences returns the user's notification preferences (defaults when absent).
	GetPreferences(ctx context.Context, userID string) (PreferencesView, error)
	// UpdatePreferences validates and lazily upserts the user's preferences.
	UpdatePreferences(ctx context.Context, userID string, u UpdatePreferences) (PreferencesView, error)

	// WithEmailSender / WithPushSender attach the out-of-app channel senders at
	// wire-up. Nil senders leave the corresponding channel a no-op.
	WithEmailSender(e EmailSender) Service
	WithPushSender(p PushSender) Service

	// Subscribe stores a web-push subscription for the user. Pro-gated: a free-plan
	// caller gets a PLAN_LIMIT_EXCEEDED error and nothing is stored.
	Subscribe(ctx context.Context, userID, plan string, req SubscribeRequest) error
	// Unsubscribe removes the user's subscription for the given endpoint (idempotent).
	Unsubscribe(ctx context.Context, userID, endpoint string) error
}

type service struct {
	repo        Repository
	broadcaster Broadcaster // nil in MVP (poll-only); injected in E-022.
	emailSender EmailSender // nil = email is a no-op (per-notification path).
	pushSender  PushSender  // nil = web push is a no-op; wired by NIC-1580.
}

// NewService creates a notification Service. Pass a nil broadcaster until the
// WebSocket hub exists (E-022). Out-of-app channel senders default to nil (no-op)
// and are attached with WithEmailSender / WithPushSender.
func NewService(repo Repository, broadcaster Broadcaster) Service {
	return &service{repo: repo, broadcaster: broadcaster}
}

// WithEmailSender attaches the per-notification email sender. Returns the same
// Service for chaining at wire-up. A nil sender leaves email a no-op.
func (s *service) WithEmailSender(e EmailSender) Service {
	s.emailSender = e
	return s
}

// WithPushSender attaches the web-push sender (NIC-1580). A nil sender leaves web
// push a no-op (safe when VAPID is unconfigured).
func (s *service) WithPushSender(p PushSender) Service {
	s.pushSender = p
	return s
}

func (s *service) List(ctx context.Context, userID string, f ListNotificationsFilter) (ListNotificationsResponse, error) {
	items, next, err := s.repo.List(ctx, userID, f)
	if err != nil {
		return ListNotificationsResponse{}, err
	}
	views := make([]NotificationView, len(items))
	for i, n := range items {
		views[i] = notificationToView(n)
	}
	return ListNotificationsResponse{Items: views, NextCursor: next}, nil
}

func (s *service) UnreadCount(ctx context.Context, userID string) (UnreadCountResponse, error) {
	count, err := s.repo.CountUnread(ctx, userID)
	if err != nil {
		return UnreadCountResponse{}, err
	}
	return UnreadCountResponse{Count: count}, nil
}

func (s *service) MarkRead(ctx context.Context, userID, id string) (NotificationView, error) {
	n, err := s.repo.MarkRead(ctx, userID, id)
	if err != nil {
		return NotificationView{}, err
	}
	return notificationToView(n), nil
}

func (s *service) MarkAllRead(ctx context.Context, userID string) (CountResponse, error) {
	count, err := s.repo.MarkAllRead(ctx, userID)
	if err != nil {
		return CountResponse{}, err
	}
	return CountResponse{Count: count}, nil
}

func (s *service) Delete(ctx context.Context, userID, id string) error {
	return s.repo.Delete(ctx, userID, id)
}

func (s *service) Create(ctx context.Context, n Notification) (NotificationView, bool, error) {
	if n.ID == "" {
		n.ID = uuid.New().String()
	}
	created, inserted, err := s.repo.InsertIfAbsent(ctx, n)
	if err != nil {
		return NotificationView{}, false, err
	}
	if !inserted {
		return NotificationView{}, false, nil
	}
	view := notificationToView(created)
	// In-app + WS delivery is free for every plan — never gated.
	if s.broadcaster != nil {
		s.broadcaster.Broadcast(created.UserID, Event{Type: "notification.created", Payload: view})
	}
	// Out-of-app channels (email, web push) go through the single dispatch policy,
	// gated on plan + prefs + subscription. Skip the recipient lookup entirely when
	// no channel sender is wired (MVP / local dev with no email or VAPID).
	if s.emailSender != nil || s.pushSender != nil {
		if rec, err := s.repo.GetRecipient(ctx, created.UserID); err != nil {
			log.Error().Err(err).Str("user_id", created.UserID).Msg("dispatch: load recipient failed")
		} else {
			s.dispatchChannels(ctx, rec, view)
		}
	}
	return view, true, nil
}
