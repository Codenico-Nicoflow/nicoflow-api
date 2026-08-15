package recurrence

import (
	"context"
	"time"

	"github.com/nicoflow/nicoflow-api/pkg/optional"
)

// freePlanRuleLimit is the number of recurrence rules a free user may own
// (SPEC §5). Enforced in the service via COUNT(*) before insert; the plan comes
// from the JWT claim, never a DB lookup.
const freePlanRuleLimit = 3

// planFree is the JWT claim value for the free tier.
const planFree = "free"

const (
	maxTitleLen = 255
	maxNotesLen = 2000
)

// Domain event types emitted on mutation. Equal to the wire names by convention;
// the ws adapter maps them through an explicit table (never a cast).
const (
	EventCreated = "recurrence.created"
	EventUpdated = "recurrence.updated"
	EventDeleted = "recurrence.deleted"
)

// Occurrence statuses this domain reads. Active/Done/Cancelled mirror the task
// domain's status enum; duplicated as constants here so recurrence never
// imports task (the dependency runs the other way, via the task package's
// Materializer seam). Missed is synthetic — never a tasks.status value, always
// COALESCE(occurrence_status, status) in this package's queries — because a
// reaped occurrence's real status is 'cancelled' (so it behaves like any other
// cancelled task everywhere outside this package) while occurrence_status
// carries the "it lapsed, not the user" distinction the streak calc needs.
const (
	StatusActive    = "active"
	StatusDone      = "done"
	StatusMissed    = "missed"
	StatusCancelled = "cancelled"
)

var (
	allowedFreqs      = map[string]bool{FreqDaily: true, FreqWeekly: true, FreqMonthly: true, FreqYearly: true}
	allowedPriorities = map[string]bool{"low": true, "medium": true, "high": true}
	allowedEnergies   = map[string]bool{"low": true, "medium": true, "deep": true}
)

// Rule is the internal domain model. StartDate/EndDate/NextOccurrence are dates,
// not instants — "every Sunday" is Sunday in every timezone.
type Rule struct {
	ID               string
	UserID           string
	ProjectID        string
	Title            string
	Notes            *string
	Priority         string
	Energy           string
	EstimatedMinutes *int
	// ScheduledTime is HH:MM or nil for all-day. Stamped onto every occurrence.
	ScheduledTime  *string
	Freq           string
	Interval       int
	ByWeekday      []int
	ByMonthday     *int
	StartDate      time.Time
	EndDate        *time.Time
	NextOccurrence *time.Time
	Paused         bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// RuleView is the JSON response shape. All IDs are strings; dates are YYYY-MM-DD.
type RuleView struct {
	ID               string  `json:"id"`
	ProjectID        string  `json:"projectId"`
	Title            string  `json:"title"`
	Notes            *string `json:"notes"`
	Priority         string  `json:"priority"`
	Energy           string  `json:"energy"`
	EstimatedMinutes *int    `json:"estimatedMinutes"`
	ScheduledTime    *string `json:"scheduledTime"`
	Freq             string  `json:"freq"`
	Interval         int     `json:"interval"`
	ByWeekday        []int   `json:"byWeekday"`
	ByMonthday       *int    `json:"byMonthday"`
	StartDate        string  `json:"startDate"`
	EndDate          *string `json:"endDate"`
	NextOccurrence   *string `json:"nextOccurrence"`
	Paused           bool    `json:"paused"`
	CreatedAt        string  `json:"createdAt"`
	UpdatedAt        string  `json:"updatedAt"`
}

// ToView maps the domain model to its JSON shape.
func ToView(r Rule) RuleView {
	v := RuleView{
		ID: r.ID, ProjectID: r.ProjectID, Title: r.Title, Notes: r.Notes,
		Priority: r.Priority, Energy: r.Energy, EstimatedMinutes: r.EstimatedMinutes,
		ScheduledTime: r.ScheduledTime,
		Freq:          r.Freq, Interval: r.Interval, ByMonthday: r.ByMonthday,
		StartDate: FormatDate(r.StartDate), Paused: r.Paused,
		CreatedAt: r.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: r.UpdatedAt.UTC().Format(time.RFC3339),
	}
	// Always a JSON array, never null — the client renders it directly.
	v.ByWeekday = r.ByWeekday
	if v.ByWeekday == nil {
		v.ByWeekday = []int{}
	}
	if r.EndDate != nil {
		s := FormatDate(*r.EndDate)
		v.EndDate = &s
	}
	if r.NextOccurrence != nil {
		s := FormatDate(*r.NextOccurrence)
		v.NextOccurrence = &s
	}
	return v
}

// ListRulesResponse is the list response shape.
type ListRulesResponse struct {
	Items []RuleView `json:"items"`
}

// Ref is the recurrence.deleted payload — just enough to drop the row client-side.
type Ref struct {
	ID string `json:"id"`
}

// CreateRuleRequest is the body for POST /projects/:projectId/recurrence-rules.
type CreateRuleRequest struct {
	Title            string  `json:"title"`
	Notes            *string `json:"notes"`
	Priority         string  `json:"priority"`
	Energy           string  `json:"energy"`
	EstimatedMinutes *int    `json:"estimatedMinutes"`
	ScheduledTime    *string `json:"scheduledTime"`
	Freq             string  `json:"freq"`
	Interval         int     `json:"interval"`
	ByWeekday        []int   `json:"byWeekday"`
	ByMonthday       *int    `json:"byMonthday"`
	StartDate        string  `json:"startDate"`
	EndDate          *string `json:"endDate"`
}

// UpdateRuleRequest is the body for PATCH /recurrence-rules/:id — all optional.
// Schedule fields use optional.Field so "absent" and "explicit null" differ:
// clearing endDate un-exhausts a series, which absence must not do.
type UpdateRuleRequest struct {
	Title            *string                `json:"title"`
	Notes            optional.Field[string] `json:"notes"`
	Priority         *string                `json:"priority"`
	Energy           *string                `json:"energy"`
	EstimatedMinutes optional.Field[int]    `json:"estimatedMinutes"`
	ScheduledTime    optional.Field[string] `json:"scheduledTime"`
	Freq             *string                `json:"freq"`
	Interval         *int                   `json:"interval"`
	ByWeekday        *[]int                 `json:"byWeekday"`
	ByMonthday       optional.Field[int]    `json:"byMonthday"`
	StartDate        *string                `json:"startDate"`
	EndDate          optional.Field[string] `json:"endDate"`
}

// PauseRequest is the body for PATCH /recurrence-rules/:id/pause.
type PauseRequest struct {
	Paused bool `json:"paused"`
}

// Occurrence is the task row a rule materializes. The recurrence domain writes it
// directly (in the rule's transaction) rather than calling the task service, so
// instance #1 is atomic with rule creation and the two packages stay decoupled.
type Occurrence struct {
	ID               string
	UserID           string
	ProjectID        string
	RuleID           string
	Title            string
	Notes            *string
	Priority         string
	Energy           string
	EstimatedMinutes *int
	ScheduledTime    *string
	OccurrenceDate   time.Time
}

// Broadcaster receives a domain Event for real-time fan-out. Fire-and-forget:
// implementations must never block or fail the mutation. A nil Broadcaster is a
// valid no-op seam.
type Broadcaster interface {
	Broadcast(userID string, ev Event)
}

// Event is the domain-level real-time event. The ws adapter maps Type onto the
// wire EventType.
type Event struct {
	Type    string
	Payload any
}

// Service is the recurrence domain's business-logic contract consumed by the
// handler.
type Service interface {
	Create(ctx context.Context, userID, projectID, plan string, req CreateRuleRequest) (RuleView, error)
	List(ctx context.Context, userID string, projectID *string) (ListRulesResponse, error)
	Get(ctx context.Context, userID, id string) (RuleView, error)
	Update(ctx context.Context, userID, id, plan string, req UpdateRuleRequest) (RuleView, error)
	SetPaused(ctx context.Context, userID, id string, paused bool) (RuleView, error)
	Delete(ctx context.Context, userID, id string) error
	// Stats derives a rule's history: per-status counts plus the current streak.
	Stats(ctx context.Context, userID, id string) (StatsView, error)
}

// Repository is the data-access contract. Every method is row-scoped by user_id.
// Defined here (the consumer package) per project layering; the pg implementation
// lives in repository.go.
type Repository interface {
	// CreateWithOccurrence inserts the rule and materializes instance #1 in one
	// transaction, so a rule can never exist without its first task. The
	// occurrence's display_order is resolved inside the transaction.
	CreateWithOccurrence(ctx context.Context, r Rule, occ Occurrence) (Rule, error)

	// List returns the user's rules, optionally filtered to one project.
	List(ctx context.Context, userID string, projectID *string) ([]Rule, error)

	// GetByID returns one rule scoped to the user, or a not-found error. A rule
	// belonging to another user is indistinguishable from a missing one.
	GetByID(ctx context.Context, userID, id string) (Rule, error)

	// Update writes the changed rule and re-stamps the single live (un-done)
	// occurrence from the new template, in one transaction. Past occurrences are
	// left untouched — history is not rewritten.
	Update(ctx context.Context, r Rule) (Rule, error)

	// SetPaused flips the paused flag, scoped to the user.
	SetPaused(ctx context.Context, userID, id string, paused bool) (Rule, error)

	// Delete ends the series: the pending (un-done) occurrence is removed and the
	// rule dropped. Past occurrences survive with a NULL recurrence_rule_id via
	// the FK's ON DELETE SET NULL.
	Delete(ctx context.Context, userID, id string) error

	// CountByUser is the plan-limit count.
	CountByUser(ctx context.Context, userID string) (int, error)

	// ProjectOwned reports whether the project exists and belongs to the user.
	ProjectOwned(ctx context.Context, userID, projectID string) (bool, error)

	// ListDue returns every non-paused, non-exhausted rule whose cursor has come
	// due, joined to the owner's timezone so the sweep can decide "today" in the
	// user's local terms. System-scoped by design — this is the only path that is
	// not user-scoped, and it is reachable solely from the internal cron.
	ListDue(ctx context.Context) ([]DueRule, error)

	// GetForMaterialize loads one rule for the sync-on-complete path, scoped to
	// the user.
	GetForMaterialize(ctx context.Context, userID, ruleID string) (Rule, error)

	// Materialize inserts the next occurrence, reaps the prior un-done instance to
	// `missed`, and advances the cursor — all in one transaction, and only if the
	// project stays under its active-task limit. Returns what happened so the
	// sweep can report it and the caller can broadcast.
	Materialize(ctx context.Context, r Rule, occ Occurrence, limit int) (MaterializeResult, error)

	// CountOccurrencesByStatus tallies a rule's occurrences per status, and
	// ListOccurrenceStatuses returns them newest-first for the streak walk. Stats
	// are derived from these rows — no counter columns exist by design.
	CountOccurrencesByStatus(ctx context.Context, userID, ruleID string) (map[string]int, error)
	ListOccurrenceStatuses(ctx context.Context, userID, ruleID string) ([]string, error)

	// ReapOverdue is the date-based twin of Materialize's reap: it marks every
	// occurrence whose due date has passed in its owner's local timezone as
	// missed, regardless of whether a next occurrence has materialized yet.
	// System-scoped, cron-only. Returns the reaped occurrences for broadcast.
	ReapOverdue(ctx context.Context) ([]Occurrence, error)
}

// DueRule pairs a due rule with its owner's timezone — the sweep compares
// next_occurrence against the user's local today, not UTC's.
type DueRule struct {
	Rule     Rule
	Timezone string
}

// MaterializeResult reports what one materialization attempt did. Exactly one of
// Created / SkippedPlanLimit / SkippedExisting is true.
type MaterializeResult struct {
	// Created is the new occurrence, set only when one was inserted.
	Created *Occurrence
	// Reaped is true when the prior un-done instance was moved to `missed`.
	Reaped bool
	// SkippedPlanLimit means the project is at its active-task limit. The cursor
	// is deliberately NOT advanced: silently stepping past an occurrence the user
	// never saw is data loss, so the sweep retries next run.
	SkippedPlanLimit bool
	// SkippedExisting means an un-done instance for this rule already exists, so
	// the horizon (exactly one live instance per rule) is already satisfied.
	SkippedExisting bool
}

// StatsView is a rule's derived history: per-status counts plus the current
// streak. Never stored — a counter would count materializations, not
// completions, and drift.
type StatsView struct {
	Done      int `json:"done"`
	Missed    int `json:"missed"`
	Cancelled int `json:"cancelled"`
	Streak    int `json:"streak"`
}
