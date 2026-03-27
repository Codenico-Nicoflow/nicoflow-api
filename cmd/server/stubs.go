package main

// Nil repository stubs — replace with real pgx implementations before launch.

import (
	"context"
	"time"

	"nicoflow-api/internal/model"
)

// ── userRepo ────────────────────────────────────────────────────────────────

type nilUserRepo struct{}

func (nilUserRepo) Create(_ context.Context, _ *model.User) error                        { panic("not implemented") }
func (nilUserRepo) FindByEmail(_ context.Context, _ string) (*model.User, error)         { panic("not implemented") }
func (nilUserRepo) FindByID(_ context.Context, _ string) (*model.User, error)            { panic("not implemented") }

// ── refreshTokenRepo ────────────────────────────────────────────────────────

type nilRefreshTokenRepo struct{}

func (nilRefreshTokenRepo) Create(_ context.Context, _ *model.RefreshToken) error                    { panic("not implemented") }
func (nilRefreshTokenRepo) FindByTokenHash(_ context.Context, _ string) (*model.RefreshToken, error) { panic("not implemented") }
func (nilRefreshTokenRepo) DeleteByTokenHash(_ context.Context, _ string) error                      { panic("not implemented") }
func (nilRefreshTokenRepo) DeleteAllByUserID(_ context.Context, _ string) error                      { panic("not implemented") }

// ── areaRepo ────────────────────────────────────────────────────────────────

type nilAreaRepo struct{}

func (nilAreaRepo) Create(_ context.Context, _ *model.Area) error             { panic("not implemented") }
func (nilAreaRepo) List(_ context.Context, _ string) ([]model.Area, error)    { panic("not implemented") }
func (nilAreaRepo) FindByID(_ context.Context, _ string) (*model.Area, error) { panic("not implemented") }
func (nilAreaRepo) Update(_ context.Context, _ *model.Area) error             { panic("not implemented") }
func (nilAreaRepo) Delete(_ context.Context, _ string) error                  { panic("not implemented") }

// ── projectRepo ─────────────────────────────────────────────────────────────

type nilProjectRepo struct{}

func (nilProjectRepo) Create(_ context.Context, _ *model.Project) error             { panic("not implemented") }
func (nilProjectRepo) List(_ context.Context, _ string) ([]model.Project, error)    { panic("not implemented") }
func (nilProjectRepo) FindByID(_ context.Context, _ string) (*model.Project, error) { panic("not implemented") }
func (nilProjectRepo) Update(_ context.Context, _ *model.Project) error             { panic("not implemented") }
func (nilProjectRepo) Delete(_ context.Context, _ string) error                     { panic("not implemented") }

// ── taskRepo ────────────────────────────────────────────────────────────────

type nilTaskRepo struct{}

func (nilTaskRepo) Create(_ context.Context, _ *model.Task) error             { panic("not implemented") }
func (nilTaskRepo) List(_ context.Context, _ string) ([]model.Task, error)    { panic("not implemented") }
func (nilTaskRepo) FindByID(_ context.Context, _ string) (*model.Task, error) { panic("not implemented") }
func (nilTaskRepo) Update(_ context.Context, _ *model.Task) error             { panic("not implemented") }
func (nilTaskRepo) Delete(_ context.Context, _ string) error                  { panic("not implemented") }

// ── subtaskRepo ─────────────────────────────────────────────────────────────

type nilSubtaskRepo struct{}

func (nilSubtaskRepo) Create(_ context.Context, _ *model.Subtask) error                { panic("not implemented") }
func (nilSubtaskRepo) ListByTask(_ context.Context, _ string) ([]model.Subtask, error) { panic("not implemented") }
func (nilSubtaskRepo) FindByID(_ context.Context, _ string) (*model.Subtask, error)    { panic("not implemented") }
func (nilSubtaskRepo) Update(_ context.Context, _ *model.Subtask) error                { panic("not implemented") }
func (nilSubtaskRepo) Delete(_ context.Context, _ string) error                        { panic("not implemented") }

// ── userPlanRepo ─────────────────────────────────────────────────────────────

type nilUserPlanRepo struct{}

func (nilUserPlanRepo) FindByUserID(_ context.Context, _ string) (*model.UserPlan, error) { panic("not implemented") }
func (nilUserPlanRepo) Upsert(_ context.Context, _ *model.UserPlan) error                 { panic("not implemented") }

// ── webhookEventRepo ─────────────────────────────────────────────────────────

type nilWebhookEventRepo struct{}

func (nilWebhookEventRepo) EventExists(_ context.Context, _ string) (bool, error)       { panic("not implemented") }
func (nilWebhookEventRepo) Save(_ context.Context, _ *model.WebhookEvent) error         { panic("not implemented") }

// ── aiSessionRepo ────────────────────────────────────────────────────────────

type nilAISessionRepo struct{}

func (nilAISessionRepo) Create(_ context.Context, _ *model.AISession) error              { panic("not implemented") }
func (nilAISessionRepo) List(_ context.Context, _ string) ([]model.AISession, error)     { panic("not implemented") }
func (nilAISessionRepo) FindByID(_ context.Context, _ string) (*model.AISession, error)  { panic("not implemented") }
func (nilAISessionRepo) Delete(_ context.Context, _ string) error                        { panic("not implemented") }

// ── aiMessageRepo ────────────────────────────────────────────────────────────

type nilAIMessageRepo struct{}

func (nilAIMessageRepo) Create(_ context.Context, _ *model.AIMessage) error                     { panic("not implemented") }
func (nilAIMessageRepo) ListBySession(_ context.Context, _ string) ([]model.AIMessage, error)   { panic("not implemented") }

// ── aiUsageRepo ─────────────────────────────────────────────────────────────

type nilAIUsageMonthlyRepo struct{}

func (nilAIUsageMonthlyRepo) Upsert(_ context.Context, _ *model.AiUsageMonthly) error                              { panic("not implemented") }
func (nilAIUsageMonthlyRepo) FindByUserMonth(_ context.Context, _ string, _ time.Time) (*model.AiUsageMonthly, error) { panic("not implemented") }
