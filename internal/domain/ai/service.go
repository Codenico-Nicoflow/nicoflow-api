package ai

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	// planFree is the JWT plan claim value for the free tier.
	planFree = "free"

	// freeLifetimeLimit is the total AI messages a free user gets, ever.
	freeLifetimeLimit = 5
	// proMonthlyLimit is the per-calendar-month message allowance for Pro.
	proMonthlyLimit = 500
)

// Service defines the AI assistant business logic interface.
type Service interface {
	CreateSession(ctx context.Context, userID string, req CreateSessionRequest) (SessionView, error)
	ListSessions(ctx context.Context, userID string) ([]SessionView, error)
	GetSession(ctx context.Context, userID, id string) (SessionDetailView, error)
	DeleteSession(ctx context.Context, userID, id string) error
	Usage(ctx context.Context, userID, plan string) (UsageView, error)
}

type service struct {
	repo   Repository
	client Client
	model  string
}

// NewService creates a new AI service. client is the streaming provider seam
// (nil-safe: a disabled client is fine); model is the config-resolved Claude
// model ID — never hardcoded at the call site.
func NewService(repo Repository, client Client, model string) Service {
	return &service{repo: repo, client: client, model: model}
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
	msgs, err := s.repo.ListMessages(ctx, sess.ID)
	if err != nil {
		return SessionDetailView{}, err
	}
	views := make([]MessageView, len(msgs))
	for i, m := range msgs {
		views[i] = messageToView(m)
	}
	return SessionDetailView{
		SessionView: sessionToView(*sess),
		Messages:    views,
	}, nil
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
		return UsageView{Used: used, Limit: freeLifetimeLimit, Scope: "lifetime", Month: nil}, nil
	}

	month := time.Now().UTC().Format("2006-01")
	used, err := s.repo.UsageForMonth(ctx, userID, month+"-01")
	if err != nil {
		return UsageView{}, err
	}
	return UsageView{Used: used, Limit: proMonthlyLimit, Scope: "month", Month: &month}, nil
}
