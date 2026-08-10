package ai

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	// planFree is the JWT plan claim value for the free tier.
	planFree = "free"

	// FreeLifetimeLimit is the total AI messages a free user gets, ever.
	FreeLifetimeLimit = 5
	// ProMonthlyLimit is the per-calendar-month message allowance for Pro.
	ProMonthlyLimit = 500

	// maxContentLen is the trimmed user-message ceiling (INVALID_INPUT above it).
	maxContentLen = 2000
	// maxResponseTokens caps a single completion.
	maxResponseTokens = 1024
)

// Service defines the AI assistant business logic interface.
type Service interface {
	CreateSession(ctx context.Context, userID string, req CreateSessionRequest) (SessionView, error)
	ListSessions(ctx context.Context, userID string) ([]SessionView, error)
	GetSession(ctx context.Context, userID, id string) (SessionDetailView, error)
	DeleteSession(ctx context.Context, userID, id string) error
	// ListSessionMessages returns paginated messages for a session, oldest-first.
	// Cursor-based on (created_at DESC, id DESC) internally, reversed before return.
	ListSessionMessages(ctx context.Context, userID, sessionID string, f ListMessagesFilter) (MessageListView, error)
	Usage(ctx context.Context, userID, plan string) (UsageView, error)
	// RunWithQuota atomically reserves one AI request (plan from the JWT
	// claim: Free = lifetime, Pro = current month), runs fn, and refunds the
	// reservation only when fn failed before any token streamed. Over quota →
	// 429 AI_LIMIT_REACHED and fn never runs.
	RunWithQuota(ctx context.Context, userID, plan string, fn QuotaFunc) error
	// SendMessage runs the full send pipeline: ownership → single-stream guard →
	// quota reserve → persist user msg (+ first-turn title) → stream Claude to
	// sink → persist assistant msg → bump updated_at → WS ai.session.updated.
	// Returns the persisted assistant message id for the done event. Pre-stream
	// failures surface as an *apperror.AppError so the handler can still set an
	// HTTP status; once tokens flow the sink owns the terminal event.
	SendMessage(ctx context.Context, userID, plan, sessionID string, req SendMessageRequest, sink StreamSink) (string, error)
	// ConfirmToolCall claims a pending proposal, runs its tool via the executor,
	// stores the result, and re-streams the follow-up assistant response over
	// sink. Idempotent: a second confirm → 409 CONFLICT. Does NOT reserve
	// additional quota — it completes the same paid turn as the original
	// SendMessage.
	ConfirmToolCall(ctx context.Context, userID, plan, sessionID, toolUseID string, sink StreamSink) (string, error)
	// RejectToolCall claims a pending proposal as rejected, appends a
	// user_rejected tool_result, and re-streams the assistant's follow-up.
	// Same idempotency + no-quota rules as ConfirmToolCall.
	RejectToolCall(ctx context.Context, userID, plan, sessionID, toolUseID string, sink StreamSink) (string, error)
	// ListPendingToolCalls returns the session's still-pending write proposals.
	ListPendingToolCalls(ctx context.Context, userID, sessionID string) ([]ToolCallView, error)
	// WithExecutor injects the tool executor (post-construction) and returns
	// the service for chaining. Nil disables tool_use.
	WithExecutor(e ToolExecutor) Service
}

// StreamSink is the handler-owned transport the service relays a completion
// through. Delta commits the HTTP status (text/event-stream) on first call and
// is where "streamed" becomes true; the service never imports net/http.
type StreamSink interface {
	// Delta writes one text chunk and flushes. The first call transitions the
	// response to a committed SSE stream.
	Delta(text string) error
	// ToolProposal writes one tool_proposal SSE frame — a write tool the model
	// wants to run pending user confirmation. Emitted at most once per
	// tool_use block; the loop stops after emitting write proposals.
	ToolProposal(assistantMessageID, toolUseID, toolName string, input json.RawMessage) error
}

// Broadcaster emits a real-time event to a user's live connections. The ws
// adapter implements it; a nil Broadcaster (tests / disabled) is a no-op.
type Broadcaster interface {
	Broadcast(userID string, ev Event)
}

// Event is the domain-level real-time event the ws layer maps to a wire type.
type Event struct {
	Type    string
	Payload any
}

// EventSessionUpdated fires after a session gains an assistant reply.
const EventSessionUpdated = "ai.session.updated"

// QuotaFunc is the metered unit of work. streamed reports whether at least
// one token reached the client — a streamed failure keeps the charge.
type QuotaFunc func(ctx context.Context) (streamed bool, err error)

type service struct {
	repo        Repository
	client      Client
	model       string
	broadcaster Broadcaster
	now         func() time.Time
	executor    ToolExecutor // nil ⇒ no tool support (tools omitted from every request)

	// active guards one in-flight stream per session (single-stream guard). A
	// second concurrent stream on the same session → 409 AI_STREAM_ACTIVE.
	mu     sync.Mutex
	active map[string]struct{}
}

// NewService creates a new AI service. client is the streaming provider seam
// (nil-safe: a disabled client is fine); model is the config-resolved Claude
// model ID — never hardcoded at the call site; broadcaster is the real-time seam
// (nil ⇒ no-op, mirrors the other domains).
func NewService(repo Repository, client Client, model string, broadcaster Broadcaster) Service {
	return &service{
		repo:        repo,
		client:      client,
		model:       model,
		broadcaster: broadcaster,
		now:         time.Now,
		active:      make(map[string]struct{}),
	}
}

// WithExecutor injects the tool executor and returns the service for chaining.
// Post-construction because the executor depends on task/project services that
// meet the ai service only in main.go — mirrors WithCleaner on the task service.
// Nil executor disables tool_use entirely (tools omitted from ChatRequest).
func (s *service) WithExecutor(e ToolExecutor) Service {
	s.executor = e
	return s
}

func (s *service) CreateSession(ctx context.Context, userID string, req CreateSessionRequest) (SessionView, error) {
	sess, err := s.repo.CreateSession(ctx, Session{
		ID:     uuid.New().String(),
		UserID: userID,
		Title:  strings.TrimSpace(req.Title),
	})
	if err != nil {
		return SessionView{}, err
	}
	return sessionToView(sess), nil
}

func (s *service) ListSessions(ctx context.Context, userID string) ([]SessionView, error) {
	sessions, err := s.repo.ListSessions(ctx, userID)
	if err != nil {
		return nil, err
	}
	views := make([]SessionView, len(sessions))
	for i, sess := range sessions {
		views[i] = sessionToView(sess)
	}
	return views, nil
}

func (s *service) GetSession(ctx context.Context, userID, id string) (SessionDetailView, error) {
	sess, err := s.repo.GetSession(ctx, userID, id)
	if err != nil {
		return SessionDetailView{}, err
	}
	msgs, cursor, err := s.repo.ListMessages(ctx, sess.ID, ListMessagesFilter{Limit: 50})
	if err != nil {
		return SessionDetailView{}, err
	}
	views := make([]MessageView, len(msgs))
	for i, m := range msgs {
		views[i] = messageToView(m)
	}
	return SessionDetailView{
		SessionView:    sessionToView(*sess),
		Messages:       views,
		MessagesCursor: cursor,
	}, nil
}

func (s *service) ListSessionMessages(ctx context.Context, userID, sessionID string, f ListMessagesFilter) (MessageListView, error) {
	// Ownership check: a guessed session id must yield 404, not an empty list.
	if _, err := s.repo.GetSession(ctx, userID, sessionID); err != nil {
		return MessageListView{}, err
	}
	msgs, next, err := s.repo.ListMessages(ctx, sessionID, f)
	if err != nil {
		return MessageListView{}, err
	}
	items := make([]MessageView, len(msgs))
	for i, m := range msgs {
		items[i] = messageToView(m)
	}
	return MessageListView{Items: items, NextCursor: next}, nil
}

func (s *service) DeleteSession(ctx context.Context, userID, id string) error {
	return s.repo.DeleteSession(ctx, userID, id)
}

func (s *service) Usage(ctx context.Context, userID, plan string) (UsageView, error) {
	if plan == planFree {
		used, err := s.repo.UsageSum(ctx, userID)
		if err != nil {
			return UsageView{}, err
		}
		return UsageView{Used: used, Limit: FreeLifetimeLimit, Scope: "lifetime", Month: nil}, nil
	}

	month := time.Now().UTC().Format("2006-01")
	used, err := s.repo.UsageForMonth(ctx, userID, month+"-01")
	if err != nil {
		return UsageView{}, err
	}
	return UsageView{Used: used, Limit: ProMonthlyLimit, Scope: "month", Month: &month}, nil
}

// monthStartUTC is the current calendar month as its YYYY-MM-01 first day.
func monthStartUTC() string {
	return time.Now().UTC().Format("2006-01") + "-01"
}

func (s *service) RunWithQuota(ctx context.Context, userID, plan string, fn QuotaFunc) error {
	month := monthStartUTC()
	var usageID string
	var err error
	if plan == planFree {
		usageID, err = s.repo.ReserveLifetime(ctx, userID, month, FreeLifetimeLimit)
	} else {
		usageID, err = s.repo.ReserveMonthly(ctx, userID, month, ProMonthlyLimit)
	}
	if err != nil {
		return err
	}

	streamed, err := fn(ctx)
	if err != nil && !streamed {
		// Zero-token failure: give the reservation back. Mid-stream failure or
		// client abort keeps the charge — tokens were generated. WithoutCancel
		// so an aborted request can still refund.
		if rerr := s.repo.RefundUsage(context.WithoutCancel(ctx), usageID); rerr != nil {
			return errors.Join(err, rerr)
		}
	}
	return err
}
