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

// Backfill windows. The cap is load-bearing rather than cosmetic: unbounded
// backfill makes a streak meaningless, because a user could fabricate a
// 365-day run in one sitting. Forgiving about *logging*, strict about *doing*.
const (
	// BackfillDays is how far back a day-scheduled habit may be corrected.
	BackfillDays = 7

	// BackfillWeeks is the quota-habit equivalent: the current week and the one
	// before it. A closed quota week locks.
	BackfillWeeks = 1
)

// DateLayout is the wire format for a check-in date. Dates are calendar days in
// the user's zone, never instants.
const DateLayout = "2006-01-02"

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
// Every derived field is computed server-side. The client performs no streak
// math at all, which is what keeps the web app and the future mobile app from
// disagreeing about a number the user is emotionally invested in.
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

	// StreakUnit tells the client which noun to print ("12 days" vs "5 weeks")
	// and which heatmap granularity to draw. Normalising everything to weeks was
	// rejected: it would show "0 weeks" to someone on a 6-day run, which kills
	// the daily feedback the feature is built on.
	StreakUnit    string `json:"streakUnit"`
	CurrentStreak int    `json:"currentStreak"`
	LongestStreak int    `json:"longestStreak"`

	DueToday       bool `json:"dueToday"`
	CompletedToday bool `json:"completedToday"`

	// LoggedToday is whether an entry exists for today — NOT whether the period
	// is satisfied. The two diverge on a quota habit: at 1 of 3 the week is
	// unmet (CompletedToday false) but today is logged, and a control that
	// keys off CompletedToday would check in again rather than undo.
	LoggedToday bool `json:"loggedToday"`

	TodayValue int `json:"todayValue"`

	// PeriodProgress is the quota habit's "2 of 3 this week". Null for day
	// habits — the client must not read a missing value as 0/0.
	PeriodProgress *PeriodProgress `json:"periodProgress"`

	// Cells is the heatmap window. A list read carries ListRibbonDays of it and
	// a scalar read RibbonDays — same field, different width, so a board and a
	// detail page render the same component against the same shape.
	//
	// Optional rather than required: it is omitted wherever there is no window
	// to show, and a client that predates it simply renders no ribbon instead of
	// breaking. Same contract style as periodProgress.
	Cells []CellView `json:"cells,omitempty"`

	ArchivedAt *time.Time `json:"archivedAt"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
}

// PeriodProgress is progress through the current period. Only weekly-quota
// habits have a period that accumulates; a day habit is simply done or not.
type PeriodProgress struct {
	Current int `json:"current"`
	Target  int `json:"target"`
}

// HabitDetailView adds the heatmap window to the scalar read. Cells are day
// cells for day habits and week cells for quota habits — the granularity the
// StreakUnit already announced.
// HabitDetailView is the scalar shape. It is HabitView with a wider Cells
// window — an alias rather than a distinct struct, because once the list
// carries cells too the only difference between the two reads is how much
// history they include, and a second type would imply a difference that no
// longer exists.
type HabitDetailView = HabitView

// CellView is one square in the history ribbon.
//
// Scheduled is what lets the client draw an off-schedule day as a baseline
// rather than a gap: a Mon/Wed/Fri habit must not look like it failed four days
// a week. For a week cell it is always true — a quota week is always "on".
type CellView struct {
	Date      string `json:"date"`
	Scheduled bool   `json:"scheduled"`
	Value     int    `json:"value"`
	Satisfied bool   `json:"satisfied"`

	// Set only on week cells, where a period accumulates toward a quota.
	Progress *PeriodProgress `json:"progress"`
}

// toView renders a habit with no history — the shape used before check-ins are
// loaded. Derived counters are zero and dueToday is computed from the schedule
// alone, so an empty habit still reports honestly rather than optimistically.
func toView(h Habit) HabitView {
	return HabitView{
		ID: h.ID, Name: h.Name, Subject: h.Subject, Color: h.Color,
		Polarity: h.Polarity, TargetValue: h.TargetValue, Unit: h.Unit,
		ScheduleKind: h.ScheduleKind, ByWeekday: h.ByWeekday, TimesPerWeek: h.TimesPerWeek,
		StreakUnit: streakUnitFor(h),
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

// CheckIn is one dated record of doing (or not doing) a habit.
//
// TargetAtCheckIn and Satisfied are frozen at write time. They exist so that
// editing a habit can never rewrite history — raising a target from 20 to 30
// minutes must not retroactively fail forty completed days — and so the streak
// query counts rows instead of re-evaluating every past day.
type CheckIn struct {
	ID        string
	HabitID   string
	UserID    string
	Date      time.Time
	Value     int
	TargetAt  int
	Satisfied bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// CheckInRequest is the body for POST /habits/{id}/check-in.
//
// Date is optional and, when present, is a *backfill*: the client corrects a
// past day it forgot to log. It can never name today — the server resolves that
// from users.timezone, because a client-supplied "today" is trivially spoofed to
// farm streaks and is wrong whenever a device clock drifts or a user travels.
//
// Value is optional and defaults to the habit's target, so the common case
// ("I did it") is an empty body.
type CheckInRequest struct {
	Date  *string `json:"date"`
	Value *int    `json:"value"`
}

// UndoCheckInRequest is the body for DELETE /habits/{id}/check-in. Date is
// optional and defaults to the user's today, mirroring CheckInRequest.
type UndoCheckInRequest struct {
	Date *string `json:"date"`
}

// Clock supplies the current instant. Injected so tests can pin "now" and prove
// the local-date rules without depending on the machine's zone — the UTC answer
// is right in a UTC container and wrong for a real user in Auckland.
type Clock func() time.Time

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
	Get(ctx context.Context, userID, id string) (HabitDetailView, error)
	Create(ctx context.Context, userID, plan string, req CreateHabitRequest) (HabitView, error)
	Update(ctx context.Context, userID, plan, id string, req UpdateHabitRequest) (HabitView, error)
	// Delete archives a habit: the row is retired, the history kept, the plan
	// slot freed. Named for the HTTP verb it serves; DeletePermanently is the
	// one that actually destroys anything.
	Delete(ctx context.Context, userID, id string) error

	// DeletePermanently removes a habit and its entire history. There is no
	// undo and no export, so callers are expected to confirm first.
	DeletePermanently(ctx context.Context, userID, id string) error

	// Today returns the habits due right now, feeding the Today-page strip.
	// A separate call rather than an extension of the task time-spread: welding
	// the task service to the habit service would undo the domain separation
	// this epic is built on, and the client fetches both in parallel anyway.
	Today(ctx context.Context, userID string) ([]HabitView, error)

	// CheckIn records (or corrects) one dated entry and returns the habit.
	// Idempotent on (habit, date): a second call updates the value rather than
	// erroring, so a double-tap is not a duplicate row.
	CheckIn(ctx context.Context, userID, id string, req CheckInRequest) (HabitView, error)

	// UndoCheckIn removes one dated entry. Idempotent: undoing a date with no
	// entry succeeds, because the caller's intent — "this day is not done" —
	// is already true.
	UndoCheckIn(ctx context.Context, userID, id string, req UndoCheckInRequest) (HabitView, error)
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

	// Delete removes a habit and, by cascade, its whole check-in history.
	// Irreversible — the counterpart to Archive, not a synonym for it.
	Delete(ctx context.Context, userID, id string) (bool, error)

	// CountActive returns the user's active (non-archived) habit count, which is
	// what the free-plan limit is measured against.
	CountActive(ctx context.Context, userID string) (int, error)

	// UpsertCheckIn writes one dated entry, replacing any existing entry for the
	// same (habit, date). The unique index on that pair makes the upsert the
	// idempotency guarantee rather than a read-then-write race.
	UpsertCheckIn(ctx context.Context, c CheckIn) (CheckIn, error)

	// DeleteCheckIn removes one dated entry scoped to the user. ok=false when no
	// row matched, which the service treats as success — the day is already not
	// done.
	DeleteCheckIn(ctx context.Context, userID, habitID string, date time.Time) (bool, error)

	// UserTimezone returns the user's IANA zone, defaulting to UTC when unset.
	// Lives on the habit repository rather than reaching into the auth domain:
	// one column read is not worth a cross-domain dependency.
	UserTimezone(ctx context.Context, userID string) (string, error)

	// ListCheckIns returns every check-in from `since` onward for the given
	// habits, keyed by habit id. One query serves a whole list read — the
	// alternative is N queries for N habits, which is the shape that turns a
	// derived streak from cheap into a problem.
	ListCheckIns(ctx context.Context, userID string, habitIDs []string, since time.Time) (map[string][]CheckIn, error)
}
