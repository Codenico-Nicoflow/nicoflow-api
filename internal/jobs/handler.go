package jobs

import (
	"context"
	"net/http"

	"github.com/rs/zerolog/log"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	mw "github.com/nicoflow/nicoflow-api/internal/middleware"
	"github.com/nicoflow/nicoflow-api/pkg/respond"
)

// AttachmentGC runs the attachment garbage-collection sweep: orphan S3 objects
// (never-confirmed uploads) and dead-owner rows. Defined here (the consumer) so
// jobs never imports the attachment domain; the concrete is the attachment
// service, injected at wire-up. Nil disables the GC step (feature off).
type AttachmentGC interface {
	RunGC(ctx context.Context) (GCSummary, error)
}

// GCSummary mirrors attachment.GCSummary — the small reap tally the GC sweep
// returns (server-side log only, never user-facing).
type GCSummary struct {
	ObjectsDeleted int `json:"objectsDeleted"`
	RowsDeleted    int `json:"rowsDeleted"`
}

// RecurrenceSweep materializes due recurrence occurrences (E-050). Defined here
// (the consumer) so jobs never imports the recurrence domain; the concrete is the
// recurrence materializer, injected at wire-up. Nil skips the step.
type RecurrenceSweep interface {
	Run(ctx context.Context, dryRun bool) (*RecurrenceResult, error)
}

// RecurrenceResult mirrors recurrence.SweepResult. A run that considered due
// rules but materialized nothing is the signal a silent `generated:0` used to
// hide, so every counter is reported.
type RecurrenceResult struct {
	Considered       int `json:"considered"`
	Materialized     int `json:"materialized"`
	Reaped           int `json:"reaped"`
	SkippedPlanLimit int `json:"skippedPlanLimit"`
	SkippedExisting  int `json:"skippedExisting"`
	SkippedNotDue    int `json:"skippedNotDue"`
	SkippedBadZone   int `json:"skippedBadTimezone"`
}

// Handler exposes the internal job endpoints. These sit outside the public /v1
// contract and are guarded by the InternalToken middleware, not JWT.
type Handler struct {
	dueNotifier      *DueDateNotifier
	overdueNotifier  *OverdueNotifier
	dayStartNotifier *DayStartNotifier
	inboxNotifier    *InboxNotifier
	summaryNotifier  *SummaryNotifier
	attachmentGC     AttachmentGC
	recurrence       RecurrenceSweep
}

// NewHandler builds the jobs Handler. attachmentGC may be nil (attachment
// feature disabled) — the GC endpoint and run-all step then no-op.
func NewHandler(dueNotifier *DueDateNotifier, overdueNotifier *OverdueNotifier, dayStartNotifier *DayStartNotifier, inboxNotifier *InboxNotifier, summaryNotifier *SummaryNotifier, attachmentGC AttachmentGC) *Handler {
	return &Handler{
		dueNotifier:      dueNotifier,
		overdueNotifier:  overdueNotifier,
		dayStartNotifier: dayStartNotifier,
		inboxNotifier:    inboxNotifier,
		summaryNotifier:  summaryNotifier,
		attachmentGC:     attachmentGC,
	}
}

// WithRecurrence injects the recurrence sweep and returns the handler for
// chaining. Post-construction so the existing NewHandler callers need no change;
// wired once in main.go. Nil leaves the step out of run-all.
func (h *Handler) WithRecurrence(s RecurrenceSweep) *Handler {
	h.recurrence = s
	return h
}

// Recurrence runs one recurrence materialization sweep directly (ops/debug); the
// hourly cron reaches it through run-all. Idempotent — the partial unique index
// on (rule, occurrence_date) means a re-run within the same day creates nothing.
func (h *Handler) Recurrence(w http.ResponseWriter, r *http.Request) {
	if h.recurrence == nil {
		respond.JSON(w, http.StatusOK, RecurrenceResult{})
		return
	}
	res, err := h.recurrence.Run(r.Context(), r.URL.Query().Get("dryRun") == "true")
	if err != nil {
		log.Error().Err(err).Str("request_id", mw.GetRequestID(r.Context())).Msg("recurrence sweep failed")
		respond.Error(w, http.StatusInternalServerError, apperror.ErrInternalServerError, "sweep failed")
		return
	}
	respond.JSON(w, http.StatusOK, res)
}

// runFunc is a sweep's Run method: it takes dryRun and returns a breakdown.
type runFunc func(ctx context.Context, dryRun bool) (*SweepBreakdown, error)

// runSweep is the shared handler body for every /internal/jobs/* endpoint. It
// reads ?dryRun=true (compute + return the breakdown, insert nothing), runs the
// sweep, and writes the breakdown (considered / fired / skipped-by-reason).
func runSweep(w http.ResponseWriter, r *http.Request, name string, run runFunc) {
	dryRun := r.URL.Query().Get("dryRun") == "true"
	breakdown, err := run(r.Context(), dryRun)
	if err != nil {
		log.Error().Err(err).Str("request_id", mw.GetRequestID(r.Context())).Msgf("%s sweep failed", name)
		respond.Error(w, http.StatusInternalServerError, apperror.ErrInternalServerError, "sweep failed")
		return
	}
	respond.JSON(w, http.StatusOK, breakdown)
}

// DueNotify runs one due-date sweep. Invoked hourly by the Render Cron Job; safe
// to call repeatedly within an hour (idempotent via dedupe_key). ?dryRun=true
// computes the breakdown without inserting.
func (h *Handler) DueNotify(w http.ResponseWriter, r *http.Request) {
	runSweep(w, r, "due-date", h.dueNotifier.Run)
}

// OverdueNotify runs one overdue sweep. Invoked hourly by the Render Cron Job;
// safe to call repeatedly within a local day (idempotent via dedupe_key).
func (h *Handler) OverdueNotify(w http.ResponseWriter, r *http.Request) {
	runSweep(w, r, "overdue", h.overdueNotifier.Run)
}

// DayStart runs one start-of-day sweep (scheduled-today summary + plan-your-day
// nudge). Invoked hourly by the Render Cron Job; safe to re-run within a local day
// (idempotent via dedupe_key).
func (h *Handler) DayStart(w http.ResponseWriter, r *http.Request) {
	runSweep(w, r, "day-start", h.dayStartNotifier.Run)
}

// Inbox runs one inbox nudge sweep (unprocessed-count reminder + stale-capture
// warning, both Pro). Invoked hourly by the Render Cron Job; safe to re-run within
// the window (idempotent via dedupe_key).
func (h *Handler) Inbox(w http.ResponseWriter, r *http.Request) {
	runSweep(w, r, "inbox", h.inboxNotifier.Run)
}

// Summary runs one end-of-day sweep (daily completion summary + streak milestone,
// both Pro). Invoked hourly by the Render Cron Job; safe to re-run within a local
// day (idempotent via dedupe_key).
func (h *Handler) Summary(w http.ResponseWriter, r *http.Request) {
	runSweep(w, r, "summary", h.summaryNotifier.Run)
}

// AttachmentGC runs one attachment garbage-collection sweep: it reaps orphan S3
// objects (never-confirmed uploads) and dead-owner rows, returning a small tally.
// Invoked by the Render cron via the run-all aggregator (and directly for ops).
// Idempotent — a healthy pair is untouched, so re-running is safe. A nil GC (the
// attachment feature is disabled) returns a zero summary.
func (h *Handler) AttachmentGC(w http.ResponseWriter, r *http.Request) {
	if h.attachmentGC == nil {
		respond.JSON(w, http.StatusOK, GCSummary{})
		return
	}
	sum, err := h.attachmentGC.RunGC(r.Context())
	if err != nil {
		log.Error().Err(err).Str("request_id", mw.GetRequestID(r.Context())).Msg("attachment-gc sweep failed")
		respond.Error(w, http.StatusInternalServerError, apperror.ErrInternalServerError, "sweep failed")
		return
	}
	respond.JSON(w, http.StatusOK, sum)
}

// RunAllResult is the aggregated run-all payload: the notification sweeps keyed
// by name, plus the attachment-gc tally. Server-side log only, not user-facing.
type RunAllResult struct {
	Sweeps       map[string]*SweepBreakdown `json:"sweeps"`
	AttachmentGC *GCSummary                 `json:"attachmentGc,omitempty"`
	Recurrence   *RecurrenceResult          `json:"recurrence,omitempty"`
}

// RunAll runs every sweep in sequence and returns a combined result. The single
// hourly Render cron hits this one endpoint instead of curl-looping over each
// (which a shell-less cron command can't do reliably). Each sweep is idempotent,
// so re-running the whole set within an hour is safe. ?dryRun=true is propagated
// to the notification sweeps (the GC sweep has no dry-run and is skipped under
// it). One sweep's failure aborts with 500; the rest are idempotent and pick up
// on the next tick.
func (h *Handler) RunAll(w http.ResponseWriter, r *http.Request) {
	dryRun := r.URL.Query().Get("dryRun") == "true"
	sweeps := []struct {
		name string
		run  runFunc
	}{
		{"due-date", h.dueNotifier.Run},
		{"overdue", h.overdueNotifier.Run},
		{"day-start", h.dayStartNotifier.Run},
		{"inbox", h.inboxNotifier.Run},
		{"summary", h.summaryNotifier.Run},
	}

	result := RunAllResult{Sweeps: make(map[string]*SweepBreakdown, len(sweeps))}
	for _, s := range sweeps {
		breakdown, err := s.run(r.Context(), dryRun)
		if err != nil {
			log.Error().Err(err).Str("request_id", mw.GetRequestID(r.Context())).Msgf("%s sweep failed", s.name)
			respond.Error(w, http.StatusInternalServerError, apperror.ErrInternalServerError, "sweep failed")
			return
		}
		result.Sweeps[s.name] = breakdown
	}

	// Recurrence runs before the GC and honours dryRun (it computes the breakdown
	// without writing). A nil sweep is left out of the payload.
	if h.recurrence != nil {
		rec, err := h.recurrence.Run(r.Context(), dryRun)
		if err != nil {
			log.Error().Err(err).Str("request_id", mw.GetRequestID(r.Context())).Msg("recurrence sweep failed")
			respond.Error(w, http.StatusInternalServerError, apperror.ErrInternalServerError, "sweep failed")
			return
		}
		result.Recurrence = rec
	}

	// GC mutates the object store, so it's skipped under dryRun. A nil GC (feature
	// off) is left out of the payload.
	if !dryRun && h.attachmentGC != nil {
		sum, err := h.attachmentGC.RunGC(r.Context())
		if err != nil {
			log.Error().Err(err).Str("request_id", mw.GetRequestID(r.Context())).Msg("attachment-gc sweep failed")
			respond.Error(w, http.StatusInternalServerError, apperror.ErrInternalServerError, "sweep failed")
			return
		}
		result.AttachmentGC = &sum
	}

	respond.JSON(w, http.StatusOK, result)
}
