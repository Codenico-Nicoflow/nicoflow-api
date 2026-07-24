//go:build integration

package ws_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/nicoflow/nicoflow-api/internal/domain/area"
	"github.com/nicoflow/nicoflow-api/internal/domain/bucket"
	"github.com/nicoflow/nicoflow-api/internal/domain/notification"
	"github.com/nicoflow/nicoflow-api/internal/domain/project"
	"github.com/nicoflow/nicoflow-api/internal/domain/task"
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

// NIC-1629: every domain mutation pushes its matching event over a live socket —
// the full round trip service → adapter → hub → socket over a real DB — and the
// bucket process→task path fires both bucket.processed AND task.created.
func TestWSRoundTrip_DomainEventsDelivered(t *testing.T) {
	pool := testutil.NewTestDB(t)
	userID := uuid.New().String()
	email := userID + "@ws.integration.test"
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO users (id, email, username, password_hash, plan)
		 VALUES ($1, $2, $3, 'x', 'pro')`,
		userID, email, "u_"+userID[:8],
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM subtasks WHERE task_id IN (SELECT id FROM tasks WHERE user_id = $1)`, userID)
		for _, stmt := range []string{
			`DELETE FROM tasks WHERE user_id = $1`,
			`DELETE FROM bucket WHERE user_id = $1`,
			`DELETE FROM projects WHERE user_id = $1`,
			`DELETE FROM areas WHERE user_id = $1`,
			`DELETE FROM users WHERE id = $1`,
		} {
			_, _ = pool.Exec(context.Background(), stmt, userID)
		}
	})

	hub := ws.NewHub()
	areaSvc := area.NewService(area.NewRepository(pool), ws.NewAreaBroadcaster(hub))
	projectSvc := project.NewService(project.NewRepository(pool), ws.NewProjectBroadcaster(hub))
	taskSvc := task.NewService(task.NewRepository(pool), nil, ws.NewTaskBroadcaster(hub))
	bucketSvc := bucket.NewService(bucket.NewRepository(pool), taskSvc, nil, ws.NewBucketBroadcaster(hub))

	handler := ws.NewHandler(hub, wsIntegrationSecret, "")
	srv := httptest.NewServer(http.HandlerFunc(handler.Upgrade))
	defer srv.Close()

	token, err := jwtutil.Issue(userID, email, "pro", wsIntegrationSecret, time.Minute)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	conn, _, err := websocket.DefaultDialer.Dial(
		"ws"+strings.TrimPrefix(srv.URL, "http")+"?token="+token, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	deadline := time.Now().Add(2 * time.Second)
	for hub.ClientCount(userID) != 1 {
		if time.Now().After(deadline) {
			t.Fatal("client never registered")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// readEvent pulls the next frame and returns its event name.
	readEvent := func() string {
		t.Helper()
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, msg, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read frame: %v", err)
		}
		var ev struct {
			Event string `json:"event"`
		}
		if err := json.Unmarshal(msg, &ev); err != nil {
			t.Fatalf("unmarshal frame: %v", err)
		}
		return ev.Event
	}
	expect := func(mutate func(), want ...string) {
		t.Helper()
		mutate()
		for _, w := range want {
			if got := readEvent(); got != w {
				t.Fatalf("event = %q, want %q", got, w)
			}
		}
	}

	ctx := context.Background()

	// Area create/update/delete.
	a, err := areaSvc.Create(ctx, userID, "pro", area.CreateAreaRequest{Name: "Home"})
	if err != nil {
		t.Fatalf("area create: %v", err)
	}
	if got := readEvent(); got != "area.created" {
		t.Fatalf("event = %q, want area.created", got)
	}
	name := "Home 2"
	expect(func() {
		if _, err := areaSvc.Update(ctx, userID, a.ID, area.UpdateAreaRequest{Name: &name}); err != nil {
			t.Fatalf("area update: %v", err)
		}
	}, "area.updated")

	// Project create (kept — tasks live in it).
	p, err := projectSvc.Create(ctx, userID, a.ID, "pro", project.CreateProjectRequest{Name: "Site"})
	if err != nil {
		t.Fatalf("project create: %v", err)
	}
	if got := readEvent(); got != "project.created" {
		t.Fatalf("event = %q, want project.created", got)
	}

	// Task create / status / delete.
	tk, err := taskSvc.Create(ctx, userID, p.ID, "pro", task.CreateTaskRequest{Title: "Do it"})
	if err != nil {
		t.Fatalf("task create: %v", err)
	}
	if got := readEvent(); got != "task.created" {
		t.Fatalf("event = %q, want task.created", got)
	}
	expect(func() {
		if _, err := taskSvc.SetStatus(ctx, userID, tk.ID, "pro", "done"); err != nil {
			t.Fatalf("task status: %v", err)
		}
	}, "task.status_changed")
	expect(func() {
		if err := taskSvc.Delete(ctx, userID, tk.ID); err != nil {
			t.Fatalf("task delete: %v", err)
		}
	}, "task.deleted")

	// Bucket create, then process→task fires BOTH events.
	b, err := bucketSvc.Create(ctx, userID, "capture me")
	if err != nil {
		t.Fatalf("bucket create: %v", err)
	}
	if got := readEvent(); got != "bucket.created" {
		t.Fatalf("event = %q, want bucket.created", got)
	}
	expect(func() {
		if _, err := bucketSvc.Process(ctx, userID, b.ID, "pro", bucket.ProcessBucketRequest{
			ProcessingResult: bucket.ResultTask,
			ProjectID:        &p.ID,
			TaskDetails:      &bucket.ProcessTaskDetails{Title: "from inbox"},
		}); err != nil {
			t.Fatalf("bucket process: %v", err)
		}
	}, "bucket.processed", "task.created")

	// Project + area delete round out the matrix.
	expect(func() {
		if err := projectSvc.Delete(ctx, userID, p.ID); err != nil {
			t.Fatalf("project delete: %v", err)
		}
	}, "project.deleted")
	expect(func() {
		if err := areaSvc.Delete(ctx, userID, a.ID); err != nil {
			t.Fatalf("area delete: %v", err)
		}
	}, "area.deleted")
}
