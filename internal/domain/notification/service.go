package notification

import (
	"context"

	"github.com/google/uuid"
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
}

type service struct {
	repo        Repository
	broadcaster Broadcaster // nil in MVP (poll-only); injected in E-022.
}

// NewService creates a notification Service. Pass a nil broadcaster until the
// WebSocket hub exists (E-022).
func NewService(repo Repository, broadcaster Broadcaster) Service {
	return &service{repo: repo, broadcaster: broadcaster}
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
	if s.broadcaster != nil {
		s.broadcaster.Broadcast(created.UserID, Event{Type: "notification.created", Payload: view})
	}
	return view, true, nil
}
