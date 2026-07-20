package jobs

import (
	"context"
	"net/http"

	"github.com/rs/zerolog/log"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	mw "github.com/nicoflow/nicoflow-api/internal/middleware"
	"github.com/nicoflow/nicoflow-api/pkg/respond"
)

// Handler exposes the internal job endpoints. These sit outside the public /v1
// contract and are guarded by the InternalToken middleware, not JWT.
type Handler struct {
	dueNotifier      *DueDateNotifier
	overdueNotifier  *OverdueNotifier
	dayStartNotifier *DayStartNotifier
	inboxNotifier    *InboxNotifier
	summaryNotifier  *SummaryNotifier
}

// NewHandler builds the jobs Handler.
func NewHandler(dueNotifier *DueDateNotifier, overdueNotifier *OverdueNotifier, dayStartNotifier *DayStartNotifier, inboxNotifier *InboxNotifier, summaryNotifier *SummaryNotifier) *Handler {
	return &Handler{
		dueNotifier:      dueNotifier,
		overdueNotifier:  overdueNotifier,
		dayStartNotifier: dayStartNotifier,
		inboxNotifier:    inboxNotifier,
		summaryNotifier:  summaryNotifier,
	}
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

// RunAll runs every sweep in sequence and returns a per-sweep breakdown map. The
// single hourly Render cron hits this one endpoint instead of curl-looping over
// five (which a shell-less cron command can't do reliably). Each sweep is
// idempotent, so re-running the whole set within an hour is safe. ?dryRun=true is
// propagated to every sweep. One sweep's failure aborts with 500; the rest are
// idempotent and pick up on the next tick.
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

	results := make(map[string]*SweepBreakdown, len(sweeps))
	for _, s := range sweeps {
		breakdown, err := s.run(r.Context(), dryRun)
		if err != nil {
			log.Error().Err(err).Str("request_id", mw.GetRequestID(r.Context())).Msgf("%s sweep failed", s.name)
			respond.Error(w, http.StatusInternalServerError, apperror.ErrInternalServerError, "sweep failed")
			return
		}
		results[s.name] = breakdown
	}
	respond.JSON(w, http.StatusOK, results)
}
