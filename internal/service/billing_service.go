package service

import (
	"context"

	"nicoflow-api/internal/repository"
)

type BillingService struct {
	webhookEvents repository.WebhookEventRepo
	plans         repository.UserPlanRepo
}

func NewBillingService(webhookEvents repository.WebhookEventRepo, plans repository.UserPlanRepo) *BillingService {
	return &BillingService{webhookEvents: webhookEvents, plans: plans}
}

func (s *BillingService) CheckoutURL(ctx context.Context, userID string) (string, error) {
	panic("not implemented")
}

func (s *BillingService) PortalURL(ctx context.Context, userID string) (string, error) {
	panic("not implemented")
}

func (s *BillingService) HandleWebhook(ctx context.Context, payload []byte, sig string) error {
	panic("not implemented")
}
