// Package habit owns recurring personal commitments tracked by check-in (E-055).
// A habit is "read 20 minutes, 3 days a week" or "no drinking, every day" — not
// a task, and deliberately not a recurrence_rule: habits belong to no project
// and never materialize task rows.
//
// Two invariants shape the whole package. The server owns "today", resolved from
// users.timezone, because a client-supplied date is trivially spoofed to farm
// streaks. And history is immutable: each check-in freezes the target it was
// judged by, so editing a habit can never retroactively fail a completed day.
package habit

import (
	"context"
	"time"
)

// Domain event types emitted on mutation. Equal to the wire names by convention;
// the ws adapter maps them through an explicit table (never a cast).
const (
	EventCreated   = "habit.created"
	EventUpdated   = "habit.updated"
	EventDeleted   = "habit.deleted"
	EventCheckedIn = "habit.checked_in"
)

// Polarity values. Immutable after creation — see PolarityImmutable in service.go.
const (
	PolarityBuild = "build"
	PolarityQuit  = "quit"
)

// Schedule kinds. weekly_quota exists because "3 days a week" is how people
// describe flexible habits, and named-days-only cannot express it.
const (
	ScheduleDaily       = "daily"
	ScheduleWeekdays    = "weekdays"
	ScheduleWeeklyQuota = "weekly_quota"
)

// Streak units. A streak counts consecutive satisfied *periods*; schedule_kind
// decides whether a period is a day or a week. Carried on the wire so the client
// prints the right noun and picks the right heatmap granularity without
// reimplementing the rule.
const (
	StreakUnitDay  = "day"
	StreakUnitWeek = "week"
)

// Field bounds and defaults.
const (
	MaxNameLen    = 255
	MaxUnitLen    = 32
	MaxSubjectLen = 64
	MaxColorLen   = 32

	DefaultSubject = "custom"
	DefaultColor   = "indigo"

	// FreePlanHabitLimit is the number of *active* habits a free user may hold.
	// Archived habits do not count, so archiving frees a slot.
	FreePlanHabitLimit = 3
)

// Habit is the internal domain model.
//
// ByWeekday is populated only for ScheduleWeekdays and TimesPerWeek only for
// ScheduleWeeklyQuota; the habits_schedule_shape CHECK enforces the pairing at
// the database level and the service validates it before the insert is attempted.
type Habit struct {
	ID     string
	UserID string

	Name    string
	Subject string
	Color   string

	Polarity    string
	TargetValue int
	Unit        *string

	ScheduleKind string
	ByWeekday    []int16
	TimesPerWeek *int16

	DayCutoffHour     int16
	ScheduleChangedAt *time.Time

	ArchivedAt *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// HabitView is the wire shape. All IDs are strings; instants are RFC3339 UTC.
//
// The derived fields (StreakUnit, CurrentStreak, LongestStreak, DueToday,
// CompletedToday, TodayValue, PeriodProgress) are enriched in NIC-1925 and are
// deliberately absent here rather than hardcoded to zero — a fake zero is a
// number a client would render as truth.
type HabitView struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Subject string `json:"subject"`
	Color   string `json:"color"`

	Polarity    string  `json:"polarity"`
	TargetValue int     `json:"targetValue"`
	Unit        *string `json:"unit"`

	ScheduleKind string  `json:"scheduleKind"`
	ByWeekday    []int16 `json:"byWeekday"`
	TimesPerWeek *int16  `json:"timesPerWeek"`

	ArchivedAt *time.Time `json:"archivedAt"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
}

func toView(h Habit) HabitView {
	return HabitView{
		ID: h.ID, Name: h.Name, Subject: h.Subject, Color: h.Color,
		Polarity: h.Polarity, TargetValue: h.TargetValue, Unit: h.Unit,
		ScheduleKind: h.ScheduleKind, ByWeekday: h.ByWeekday, TimesPerWeek: h.TimesPerWeek,
		ArchivedAt: h.ArchivedAt, CreatedAt: h.CreatedAt, UpdatedAt: h.UpdatedAt,
	}
}

// CreateHabitRequest is the body for POST /habits. Polarity, subject, colour and
// target default when omitted so the minimal create is just a name.
type CreateHabitRequest struct {
	Name    string `json:"name"`
	Subject string `json:"subject"`
	Color   string `json:"color"`

	Polarity    string  `json:"polarity"`
	TargetValue *int    `json:"targetValue"`
	Unit        *string `json:"unit"`

	ScheduleKind string  `json:"scheduleKind"`
	ByWeekday    []int16 `json:"byWeekday"`
	TimesPerWeek *int16  `json:"timesPerWeek"`
}

// UpdateHabitRequest is the body for PATCH /habits/{id}. Every field is a
// pointer so an omitted field keeps its stored value.
//
// There is deliberately no Polarity field: it is immutable, and accepting it
// only to reject it would invite clients to send it. A caller that tries anyway
// is caught by the handler's explicit probe (see rejectPolarityChange).
type UpdateHabitRequest struct {
	Name    *string `json:"name"`
	Subject *string `json:"subject"`
	Color   *string `json:"color"`

	TargetValue *int    `json:"targetValue"`
	Unit        *string `json:"unit"`

	ScheduleKind *string `json:"scheduleKind"`
	ByWeekday    []int16 `json:"byWeekday"`
	TimesPerWeek *int16  `json:"timesPerWeek"`

	Archived *bool `json:"archived"`
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

// DeletedPayload is the habit.deleted event body — just enough for a client to
// drop the row.
type DeletedPayload struct {
	ID string `json:"id"`
}

// Service is the habit domain's business-logic contract consumed by the handler.
// plan is the caller's JWT claim, never a DB lookup.
type Service interface {
	List(ctx context.Context, userID string, includeArchived bool) ([]HabitView, error)
	Get(ctx context.Context, userID, id string) (HabitView, error)
	Create(ctx context.Context, userID, plan string, req CreateHabitRequest) (HabitView, error)
	Update(ctx context.Context, userID, plan, id string, req UpdateHabitRequest) (HabitView, error)
	Delete(ctx context.Context, userID, id string) error
}

// UpdateParams is one habit edit, already validated and merged by the service.
// The repository applies it verbatim — it makes no decisions about which fields
// changed.
type UpdateParams struct {
	ID     string
	UserID string

	Name    string
	Subject string
	Color   string

	TargetValue int
	Unit        *string

	ScheduleKind string
	ByWeekday    []int16
	TimesPerWeek *int16

	// Set when the schedule shape moved, so periods scored under the old shape
	// are left alone. Nil leaves the stored marker untouched.
	ScheduleChangedAt *time.Time

	// Tri-state: nil leaves archival untouched, true archives, false restores.
	Archived *bool
}

// Repository is the data-access contract for habits. Every method is row-scoped
// by user_id — another user's habit is invisible, never forbidden, so no query
// can become an existence oracle. Defined here (the consumer package) per
// project layering; the pg implementation lives in repository.go.
type Repository interface {
	// List returns a user's habits, newest first. includeArchived=false filters
	// to active rows only.
	List(ctx context.Context, userID string, includeArchived bool) ([]Habit, error)

	// GetByID returns one habit scoped to the user. Missing or not-owned →
	// HABIT_NOT_FOUND.
	GetByID(ctx context.Context, userID, id string) (Habit, error)

	// Create inserts a habit and returns the stored row.
	Create(ctx context.Context, h Habit) (Habit, error)

	// Update applies an edit scoped to the user and returns the new row.
	// ok=false means no row matched — missing or not owned.
	Update(ctx context.Context, p UpdateParams) (Habit, bool, error)

	// Archive soft-deletes one habit scoped to the user. ok=false when no row
	// matched. Archiving keeps the check-in history: it is the user's record of
	// what they did, and it is what makes un-archiving meaningful.
	Archive(ctx context.Context, userID, id string) (bool, error)

	// CountActive returns the user's active (non-archived) habit count, which is
	// what the free-plan limit is measured against.
	CountActive(ctx context.Context, userID string) (int, error)
}
