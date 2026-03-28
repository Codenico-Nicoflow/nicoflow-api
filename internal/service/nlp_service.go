package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"nicoflow-api/internal/repository"
)

var ErrNLPPlanRequired = errors.New("PRO plan required for NLP")

type NLPResult struct {
	Title        string  `json:"title"`
	DueDate      *string `json:"due_date,omitempty"`
	ScheduledFor *string `json:"scheduled_for,omitempty"`
}

type NLPService struct {
	plans repository.UserPlanRepo
}

func NewNLPService(plans repository.UserPlanRepo) *NLPService {
	return &NLPService{plans: plans}
}

func (s *NLPService) Parse(ctx context.Context, userID, input string) (*NLPResult, error) {
	plan, err := s.plans.FindByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("NLPService.Parse: %w", err)
	}
	if plan == nil || plan.Plan != "pro" {
		return nil, ErrNLPPlanRequired
	}
	return parseNLP(input), nil
}

func parseNLP(input string) *NLPResult {
	lower := strings.ToLower(input)
	result := &NLPResult{Title: input}

	today := time.Now().Format("2006-01-02")
	tomorrow := time.Now().AddDate(0, 0, 1).Format("2006-01-02")

	switch {
	case strings.Contains(lower, "today"):
		sf := "today"
		result.ScheduledFor = &sf
		result.DueDate = &today
	case strings.Contains(lower, "tomorrow"):
		sf := "tomorrow"
		result.ScheduledFor = &sf
		result.DueDate = &tomorrow
	case strings.Contains(lower, "this week"):
		sf := "this_week"
		result.ScheduledFor = &sf
	}

	return result
}
