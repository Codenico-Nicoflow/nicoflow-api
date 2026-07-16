//go:build integration

package ws_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/nicoflow/nicoflow-api/internal/domain/notification"
	"github.com/nicoflow/nicoflow-api/internal/testutil"
	"github.com/nicoflow/nicoflow-api/internal/ws"
	"github.com/nicoflow/nicoflow-api/pkg/jwtutil"
)

const wsIntegrationSecret = "integration-test-secret-32-bytes!!"

// AC1 (NIC-1588): a connected WS client receives a notification.created frame the
// moment the notification service creates a notification — the full round trip
// service → adapter → hub → socket, over a real DB.
func TestWSRoundTrip_NotificationCreatedDelivered(t *testing.T) {
	pool := testutil.NewTestDB(t)
	userID := uuid.New().String()
	email := userID + "@ws.integration.test"
	_, err := pool.Exec(context.Background(),
		`INSERT INTO users (id, email, username, password_hash, plan)
		 VALUES ($1, $2, $3, 'x', 'free')`,
		userID, email, "u_"+userID[:8],
	)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM notifications WHERE user_id = $1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	})

	hub := ws.NewHub()
	svc := notification.NewService(notification.NewRepository(pool), ws.NewNotificationBroadcaster(hub))
	handler := ws.NewHandler(hub, wsIntegrationSecret, "")
	srv := httptest.NewServer(http.HandlerFunc(handler.Upgrade))
	defer srv.Close()

	token, err := jwtutil.Issue(userID, email, "free", wsIntegrationSecret, time.Minute)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	conn, _, err := websocket.DefaultDialer.Dial(
		"ws"+strings.TrimPrefix(srv.URL, "http")+"?token="+token, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Wait until the client is registered so the broadcast can't race the connect.
	deadline := time.Now().Add(2 * time.Second)
	for {
		hubClients := hub.ClientCount(userID)
		if hubClients == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("client never registered")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if _, _, err := svc.Create(context.Background(), notification.Notification{
		ID:     uuid.New().String(),
		UserID: userID,
		Type:   notification.TypeTaskDueSoon,
		Title:  "Task due",
	}); err != nil {
		t.Fatalf("create notification: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}
	if !strings.Contains(string(msg), "notification.created") {
		t.Errorf("frame = %q, want it to contain notification.created", msg)
	}
	if !strings.Contains(string(msg), "Task due") {
		t.Errorf("frame = %q, want it to carry the notification payload", msg)
	}
}
