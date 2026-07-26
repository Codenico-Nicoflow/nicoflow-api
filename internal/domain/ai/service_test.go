package ai_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/internal/domain/ai"
)

// ── mock repository ───────────────────────────────────────────────────────────

type mockRepo struct {
	createSession   func(ctx context.Context, s ai.Session) (ai.Session, error)
	listSessions    func(ctx context.Context, userID string) ([]ai.Session, error)
	getSession      func(ctx context.Context, userID, id string) (*ai.Session, error)
	listMessages    func(ctx context.Context, sessionID string) ([]ai.SessionMessage, error)
	deleteSession   func(ctx context.Context, userID, id string) error
	usageSum        func(ctx context.Context, userID string) (int, error)
	usageForMonth   func(ctx context.Context, userID, month string) (int, error)
	reserveMonthly  func(ctx context.Context, userID, month string, limit int) (string, error)
	reserveLifetime func(ctx context.Context, userID, month string, limit int) (string, error)
	refundUsage     func(ctx context.Context, usageID string) error
}

func (m *mockRepo) CreateSession(ctx context.Context, s ai.Session) (ai.Session, error) {
	return m.createSession(ctx, s)
}
func (m *mockRepo) ListSessions(ctx context.Context, userID string) ([]ai.Session, error) {
	return m.listSessions(ctx, userID)
}
func (m *mockRepo) GetSession(ctx context.Context, userID, id string) (*ai.Session, error) {
	return m.getSession(ctx, userID, id)
}
func (m *mockRepo) ListMessages(ctx context.Context, sessionID string) ([]ai.SessionMessage, error) {
	return m.listMessages(ctx, sessionID)
}
func (m *mockRepo) DeleteSession(ctx context.Context, userID, id string) error {
	return m.deleteSession(ctx, userID, id)
}
func (m *mockRepo) UsageSum(ctx context.Context, userID string) (int, error) {
	return m.usageSum(ctx, userID)
}
func (m *mockRepo) UsageForMonth(ctx context.Context, userID, month string) (int, error) {
	return m.usageForMonth(ctx, userID, month)
}
func (m *mockRepo) ReserveMonthly(ctx context.Context, userID, month string, limit int) (string, error) {
	return m.reserveMonthly(ctx, userID, month, limit)
}
func (m *mockRepo) ReserveLifetime(ctx context.Context, userID, month string, limit int) (string, error) {
	return m.reserveLifetime(ctx, userID, month, limit)
}
func (m *mockRepo) RefundUsage(ctx context.Context, usageID string) error {
	return m.refundUsage(ctx, usageID)
}

func requireAppErr(t *testing.T, err error) *apperror.AppError {
	t.Helper()
	var ae *apperror.AppError
	if !errors.As(err, &ae) {
		t.Fatalf("expected AppError, got %v", err)
	}
	return ae
}

// ── CreateSession ─────────────────────────────────────────────────────────────

func TestService_CreateSession(t *testing.T) {
	var captured ai.Session
	repo := &mockRepo{
		createSession: func(_ context.Context, s ai.Session) (ai.Session, error) {
			captured = s
			s.Title = "New Conversation"
			s.CreatedAt = time.Now()
			s.UpdatedAt = s.CreatedAt
			return s, nil
		},
	}
	svc := ai.NewService(repo, nil, "")

	view, err := svc.CreateSession(context.Background(), "usr_1", ai.CreateSessionRequest{Title: "  "})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if captured.ID == "" {
		t.Error("expected a generated session ID")
	}
	if captured.UserID != "usr_1" {
		t.Errorf("UserID = %q, want usr_1", captured.UserID)
	}
	if captured.Title != "" {
		t.Errorf("blank title should pass empty to repo (DB default), got %q", captured.Title)
	}
	if view.ID != captured.ID {
		t.Errorf("view.ID = %q, want %q", view.ID, captured.ID)
	}
}

// ── ListSessions ──────────────────────────────────────────────────────────────

func TestService_ListSessions_PreservesOrder(t *testing.T) {
	repo := &mockRepo{
		listSessions: func(_ context.Context, _ string) ([]ai.Session, error) {
			return []ai.Session{{ID: "b"}, {ID: "a"}, {ID: "c"}}, nil
		},
	}
	svc := ai.NewService(repo, nil, "")

	views, err := svc.ListSessions(context.Background(), "usr_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := []string{views[0].ID, views[1].ID, views[2].ID}
	want := []string{"b", "a", "c"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v (repo order must be preserved)", got, want)
		}
	}
}

// ── GetSession ────────────────────────────────────────────────────────────────

func TestService_GetSession_WithMessages(t *testing.T) {
	repo := &mockRepo{
		getSession: func(_ context.Context, _, id string) (*ai.Session, error) {
			return &ai.Session{ID: id, Title: "T"}, nil
		},
		listMessages: func(_ context.Context, sessionID string) ([]ai.SessionMessage, error) {
			return []ai.SessionMessage{
				{ID: "m1", SessionID: sessionID, Role: "user", Content: "hi"},
				{ID: "m2", SessionID: sessionID, Role: "assistant", Content: "hello"},
			}, nil
		},
	}
	svc := ai.NewService(repo, nil, "")

	view, err := svc.GetSession(context.Background(), "usr_1", "sess_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if view.ID != "sess_1" {
		t.Errorf("ID = %q, want sess_1", view.ID)
	}
	if len(view.Messages) != 2 || view.Messages[0].ID != "m1" || view.Messages[1].ID != "m2" {
		t.Errorf("messages not mapped in order: %+v", view.Messages)
	}
}

func TestService_GetSession_ForeignID_404(t *testing.T) {
	notFound := apperror.New(http.StatusNotFound, apperror.ErrResourceNotFound, "session not found")
	repo := &mockRepo{
		getSession: func(_ context.Context, _, _ string) (*ai.Session, error) {
			return nil, notFound
		},
	}
	svc := ai.NewService(repo, nil, "")

	_, err := svc.GetSession(context.Background(), "usr_1", "foreign")
	ae := requireAppErr(t, err)
	if ae.Code != apperror.ErrResourceNotFound || ae.Status != http.StatusNotFound {
		t.Errorf("got %s/%d, want RESOURCE_NOT_FOUND/404", ae.Code, ae.Status)
	}
}

// ── DeleteSession ─────────────────────────────────────────────────────────────

func TestService_DeleteSession_ForeignID_404(t *testing.T) {
	repo := &mockRepo{
		deleteSession: func(_ context.Context, _, _ string) error {
			return apperror.New(http.StatusNotFound, apperror.ErrResourceNotFound, "session not found")
		},
	}
	svc := ai.NewService(repo, nil, "")

	err := svc.DeleteSession(context.Background(), "usr_1", "foreign")
	ae := requireAppErr(t, err)
	if ae.Code != apperror.ErrResourceNotFound {
		t.Errorf("code = %s, want RESOURCE_NOT_FOUND", ae.Code)
	}
}

// ── Usage ─────────────────────────────────────────────────────────────────────

func TestService_Usage(t *testing.T) {
	tests := []struct {
		name      string
		plan      string
		wantScope string
		wantLimit int
		wantUsed  int
		wantMonth bool // true = month is non-nil
	}{
		{name: "free lifetime", plan: "free", wantScope: "lifetime", wantLimit: 5, wantUsed: 3, wantMonth: false},
		{name: "pro month", plan: "pro", wantScope: "month", wantLimit: 500, wantUsed: 7, wantMonth: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &mockRepo{
				usageSum:      func(_ context.Context, _ string) (int, error) { return 3, nil },
				usageForMonth: func(_ context.Context, _, _ string) (int, error) { return 7, nil },
			}
			svc := ai.NewService(repo, nil, "")

			view, err := svc.Usage(context.Background(), "usr_1", tc.plan)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if view.Scope != tc.wantScope {
				t.Errorf("scope = %q, want %q", view.Scope, tc.wantScope)
			}
			if view.Limit != tc.wantLimit {
				t.Errorf("limit = %d, want %d", view.Limit, tc.wantLimit)
			}
			if view.Used != tc.wantUsed {
				t.Errorf("used = %d, want %d", view.Used, tc.wantUsed)
			}
			if tc.wantMonth {
				if view.Month == nil {
					t.Error("month = nil, want non-nil YYYY-MM")
				} else if _, err := time.Parse("2006-01", *view.Month); err != nil {
					t.Errorf("month %q not YYYY-MM: %v", *view.Month, err)
				}
			} else if view.Month != nil {
				t.Errorf("month = %v, want nil for free plan", *view.Month)
			}
		})
	}
}

// ── RunWithQuota ──────────────────────────────────────────────────────────────

func TestService_RunWithQuota_PlanRouting(t *testing.T) {
	tests := []struct {
		name      string
		plan      string
		wantLimit int
	}{
		{name: "free reserves lifetime", plan: "free", wantLimit: 5},
		{name: "pro reserves current month", plan: "pro", wantLimit: 500},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotUser, gotMonth string
			var gotLimit int
			var lifetimeCalled, monthlyCalled bool
			repo := &mockRepo{
				reserveLifetime: func(_ context.Context, userID, month string, limit int) (string, error) {
					lifetimeCalled, gotUser, gotMonth, gotLimit = true, userID, month, limit
					return "usage_1", nil
				},
				reserveMonthly: func(_ context.Context, userID, month string, limit int) (string, error) {
					monthlyCalled, gotUser, gotMonth, gotLimit = true, userID, month, limit
					return "usage_1", nil
				},
			}
			svc := ai.NewService(repo, nil, "")

			err := svc.RunWithQuota(context.Background(), "usr_1", tc.plan, func(context.Context) (bool, error) {
				return true, nil
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.plan == "free" && (!lifetimeCalled || monthlyCalled) {
				t.Fatal("free plan must reserve lifetime, not monthly")
			}
			if tc.plan == "pro" && (!monthlyCalled || lifetimeCalled) {
				t.Fatal("pro plan must reserve monthly, not lifetime")
			}
			if gotUser != "usr_1" {
				t.Errorf("userID = %q, want usr_1", gotUser)
			}
			if gotLimit != tc.wantLimit {
				t.Errorf("limit = %d, want %d", gotLimit, tc.wantLimit)
			}
			if want := time.Now().UTC().Format("2006-01") + "-01"; gotMonth != want {
				t.Errorf("month = %q, want %q", gotMonth, want)
			}
		})
	}
}

func TestService_RunWithQuota_Exhausted_FnNeverRuns(t *testing.T) {
	limitErr := apperror.New(http.StatusTooManyRequests, apperror.ErrAILimitReached, "AI request limit reached")
	repo := &mockRepo{
		reserveLifetime: func(_ context.Context, _, _ string, _ int) (string, error) {
			return "", limitErr
		},
	}
	svc := ai.NewService(repo, nil, "")

	fnCalled := false
	err := svc.RunWithQuota(context.Background(), "usr_1", "free", func(context.Context) (bool, error) {
		fnCalled = true
		return false, nil
	})
	if fnCalled {
		t.Fatal("fn must never run over quota — no provider call may be made")
	}
	ae := requireAppErr(t, err)
	if ae.Code != apperror.ErrAILimitReached || ae.Status != http.StatusTooManyRequests {
		t.Errorf("got %s/%d, want AI_LIMIT_REACHED/429", ae.Code, ae.Status)
	}
}

func TestService_RunWithQuota_RefundMatrix(t *testing.T) {
	fnErr := errors.New("provider exploded")
	tests := []struct {
		name       string
		streamed   bool
		fnErr      error
		wantRefund bool
	}{
		{name: "success streamed → no refund", streamed: true, fnErr: nil, wantRefund: false},
		{name: "success without stream → no refund", streamed: false, fnErr: nil, wantRefund: false},
		{name: "failure before any token → refund", streamed: false, fnErr: fnErr, wantRefund: true},
		{name: "mid-stream failure → no refund", streamed: true, fnErr: fnErr, wantRefund: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var refundedID string
			refundCalled := false
			repo := &mockRepo{
				reserveMonthly: func(_ context.Context, _, _ string, _ int) (string, error) {
					return "usage_9", nil
				},
				refundUsage: func(_ context.Context, usageID string) error {
					refundCalled, refundedID = true, usageID
					return nil
				},
			}
			svc := ai.NewService(repo, nil, "")

			err := svc.RunWithQuota(context.Background(), "usr_1", "pro", func(context.Context) (bool, error) {
				return tc.streamed, tc.fnErr
			})
			if !errors.Is(err, tc.fnErr) && err != nil {
				t.Fatalf("err = %v, want %v", err, tc.fnErr)
			}
			if tc.fnErr != nil && err == nil {
				t.Fatal("fn error must surface to the caller")
			}
			if refundCalled != tc.wantRefund {
				t.Fatalf("refund called = %v, want %v", refundCalled, tc.wantRefund)
			}
			if tc.wantRefund && refundedID != "usage_9" {
				t.Errorf("refunded id = %q, want the reserved usage_9", refundedID)
			}
		})
	}
}

func TestService_RunWithQuota_RefundFailure_KeepsOriginalError(t *testing.T) {
	fnErr := errors.New("provider exploded")
	refundErr := errors.New("refund broke")
	repo := &mockRepo{
		reserveMonthly: func(_ context.Context, _, _ string, _ int) (string, error) {
			return "usage_9", nil
		},
		refundUsage: func(_ context.Context, _ string) error { return refundErr },
	}
	svc := ai.NewService(repo, nil, "")

	err := svc.RunWithQuota(context.Background(), "usr_1", "pro", func(context.Context) (bool, error) {
		return false, fnErr
	})
	if !errors.Is(err, fnErr) || !errors.Is(err, refundErr) {
		t.Fatalf("err = %v, want both original and refund errors joined", err)
	}
}
