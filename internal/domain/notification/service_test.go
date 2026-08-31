package notification_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/internal/domain/notification"
)

// ── mock repository ───────────────────────────────────────────────────────────

type mockRepo struct {
	list           func(ctx context.Context, userID string, f notification.ListNotificationsFilter) ([]notification.Notification, string, error)
	countUnread    func(ctx context.Context, userID string) (int, error)
	markRead       func(ctx context.Context, userID, id string) (notification.Notification, error)
	markAllRead    func(ctx context.Context, userID string) (int, error)
	del            func(ctx context.Context, userID, id string) error
	insertIfAbsent func(ctx context.Context, n notification.Notification) (notification.Notification, bool, error)
	getRecipient   func(ctx context.Context, userID string) (notification.Recipient, error)
	getPreferences func(ctx context.Context, userID string) (notification.Preferences, error)
	upsertPrefs    func(ctx context.Context, userID string, u notification.UpdatePreferences) (notification.Preferences, error)
}

func (m *mockRepo) List(ctx context.Context, userID string, f notification.ListNotificationsFilter) ([]notification.Notification, string, error) {
	return m.list(ctx, userID, f)
}
func (m *mockRepo) CountUnread(ctx context.Context, userID string) (int, error) {
	return m.countUnread(ctx, userID)
}
func (m *mockRepo) MarkRead(ctx context.Context, userID, id string) (notification.Notification, error) {
	return m.markRead(ctx, userID, id)
}
func (m *mockRepo) MarkAllRead(ctx context.Context, userID string) (int, error) {
	return m.markAllRead(ctx, userID)
}
func (m *mockRepo) Delete(ctx context.Context, userID, id string) error {
	return m.del(ctx, userID, id)
}
func (m *mockRepo) InsertIfAbsent(ctx context.Context, n notification.Notification) (notification.Notification, bool, error) {
	return m.insertIfAbsent(ctx, n)
}
func (m *mockRepo) GetRecipient(ctx context.Context, userID string) (notification.Recipient, error) {
	if m.getRecipient == nil {
		return notification.Recipient{UserID: userID}, nil
	}
	return m.getRecipient(ctx, userID)
}
func (m *mockRepo) GetPreferences(ctx context.Context, userID string) (notification.Preferences, error) {
	return m.getPreferences(ctx, userID)
}
func (m *mockRepo) UpsertPreferences(ctx context.Context, userID string, u notification.UpdatePreferences) (notification.Preferences, error) {
	return m.upsertPrefs(ctx, userID, u)
}
func (m *mockRepo) UpsertPushSubscription(_ context.Context, _ string, _ notification.PushSubscription) error {
	return nil
}
func (m *mockRepo) DeletePushSubscription(_ context.Context, _, _ string) error { return nil }
func (m *mockRepo) ListPushSubscriptions(_ context.Context, _ string) ([]notification.PushSubscription, error) {
	return nil, nil
}

// spyBroadcaster records the events it receives so tests can assert the WS seam.
type spyBroadcaster struct{ events []notification.Event }

func (s *spyBroadcaster) Broadcast(_ string, e notification.Event) { s.events = append(s.events, e) }

func appErr(err error) *apperror.AppError {
	var ae *apperror.AppError
	if errors.As(err, &ae) {
		return ae
	}
	return nil
}

// ── UnreadCount ────────────────────────────────────────────────────────────────

func TestService_UnreadCount(t *testing.T) {
	svc := notification.NewService(&mockRepo{
		countUnread: func(_ context.Context, _ string) (int, error) { return 3, nil },
	}, nil)

	resp, err := svc.UnreadCount(context.Background(), "u1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Count != 3 {
		t.Fatalf("count = %d, want 3", resp.Count)
	}
}

// ── List ───────────────────────────────────────────────────────────────────────

func TestService_List(t *testing.T) {
	now := time.Now()
	svc := notification.NewService(&mockRepo{
		list: func(_ context.Context, _ string, _ notification.ListNotificationsFilter) ([]notification.Notification, string, error) {
			return []notification.Notification{
				{ID: "n1", UserID: "u1", Type: notification.TypeMorningDigest, Title: "Due", CreatedAt: now},
			}, "next-cursor", nil
		},
	}, nil)

	resp, err := svc.List(context.Background(), "u1", notification.ListNotificationsFilter{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].ID != "n1" {
		t.Fatalf("items = %+v, want one item n1", resp.Items)
	}
	if resp.NextCursor != "next-cursor" {
		t.Fatalf("nextCursor = %q, want next-cursor", resp.NextCursor)
	}
	// Metadata must default to an empty object, never null.
	if string(resp.Items[0].Metadata) != "{}" {
		t.Fatalf("metadata = %s, want {}", resp.Items[0].Metadata)
	}
}

// ── MarkRead ───────────────────────────────────────────────────────────────────

func TestService_MarkRead(t *testing.T) {
	readAt := time.Now()
	tests := []struct {
		name       string
		repoResult notification.Notification
		repoErr    error
		wantCode   string
		wantStatus int
		wantRead   bool
	}{
		{
			name:       "happy path",
			repoResult: notification.Notification{ID: "n1", IsRead: true, ReadAt: &readAt},
			wantRead:   true,
		},
		{
			name:       "not found maps to NOTIFICATION_NOT_FOUND",
			repoErr:    apperror.New(http.StatusNotFound, apperror.ErrNotificationNotFound, "notification not found"),
			wantCode:   apperror.ErrNotificationNotFound,
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := notification.NewService(&mockRepo{
				markRead: func(_ context.Context, _, _ string) (notification.Notification, error) {
					return tt.repoResult, tt.repoErr
				},
			}, nil)

			view, err := svc.MarkRead(context.Background(), "u1", "n1")
			if tt.wantCode != "" {
				ae := appErr(err)
				if ae == nil || ae.Code != tt.wantCode || ae.Status != tt.wantStatus {
					t.Fatalf("err = %v, want code %s status %d", err, tt.wantCode, tt.wantStatus)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if view.IsRead != tt.wantRead {
				t.Fatalf("isRead = %v, want %v", view.IsRead, tt.wantRead)
			}
			if view.ReadAt == nil {
				t.Fatal("readAt = nil, want non-nil")
			}
		})
	}
}

// ── MarkAllRead ──────────────────────────────────────────────────────────────

func TestService_MarkAllRead(t *testing.T) {
	svc := notification.NewService(&mockRepo{
		markAllRead: func(_ context.Context, _ string) (int, error) { return 5, nil },
	}, nil)

	resp, err := svc.MarkAllRead(context.Background(), "u1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Count != 5 {
		t.Fatalf("count = %d, want 5", resp.Count)
	}
}

// ── Create funnel + Broadcaster seam ─────────────────────────────────────────

func TestService_Create(t *testing.T) {
	t.Run("inserts and broadcasts when a row is created", func(t *testing.T) {
		spy := &spyBroadcaster{}
		svc := notification.NewService(&mockRepo{
			insertIfAbsent: func(_ context.Context, n notification.Notification) (notification.Notification, bool, error) {
				n.CreatedAt = time.Now()
				return n, true, nil
			},
		}, spy)

		view, inserted, err := svc.Create(context.Background(), notification.Notification{
			UserID: "u1", Type: notification.TypeMorningDigest, Title: "Due",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !inserted {
			t.Fatal("inserted = false, want true")
		}
		if view.ID == "" {
			t.Fatal("view.ID empty — Create should generate an ID")
		}
		if len(spy.events) != 1 || spy.events[0].Type != "notification.created" {
			t.Fatalf("events = %+v, want one notification.created", spy.events)
		}
	})

	t.Run("duplicate: no insert, no broadcast", func(t *testing.T) {
		spy := &spyBroadcaster{}
		svc := notification.NewService(&mockRepo{
			insertIfAbsent: func(_ context.Context, _ notification.Notification) (notification.Notification, bool, error) {
				return notification.Notification{}, false, nil
			},
		}, spy)

		_, inserted, err := svc.Create(context.Background(), notification.Notification{UserID: "u1"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if inserted {
			t.Fatal("inserted = true, want false for duplicate")
		}
		if len(spy.events) != 0 {
			t.Fatalf("events = %+v, want none on duplicate", spy.events)
		}
	})

	t.Run("nil broadcaster is safe", func(t *testing.T) {
		svc := notification.NewService(&mockRepo{
			insertIfAbsent: func(_ context.Context, n notification.Notification) (notification.Notification, bool, error) {
				return n, true, nil
			},
		}, nil)

		_, inserted, err := svc.Create(context.Background(), notification.Notification{UserID: "u1"})
		if err != nil {
			t.Fatalf("unexpected error (nil broadcaster should not panic): %v", err)
		}
		if !inserted {
			t.Fatal("inserted = false, want true")
		}
	})
}
