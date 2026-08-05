package habit

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	mw "github.com/nicoflow/nicoflow-api/internal/middleware"
	"github.com/nicoflow/nicoflow-api/pkg/respond"
)

// MaxRequestBytes caps a habit write. Habit bodies are small and fixed-shape, so
// this is a floor against a malicious payload rather than a real product limit.
const MaxRequestBytes int64 = 1 << 16 // 64 KiB

// Handler serves the habit HTTP endpoints. It parses/validates the request,
// reads the auth claims from context, calls the service, and writes the
// envelope — no business logic.
type Handler struct {
	svc Service
}

// NewHandler creates a habit Handler.
func NewHandler(svc Service) *Handler { return &Handler{svc: svc} }

// List godoc
// @Summary      List habits
// @Description  Returns the caller's habits, newest first, each with a short heatmap window (14 cells) alongside its derived counters — enough for a board to draw a ribbon per card without a follow-up request. Archived habits are excluded unless includeArchived=true. Free users may hold 3 active habits; a downgraded user keeps every habit visible and checkable, only creation is blocked.
// @Tags         habits
// @Produce      json
// @Param        includeArchived  query     boolean  false  "Include archived habits"
// @Security     BearerAuth
// @Success      200  {object}  HabitListEnvelope  "Habits"
// @Router       /habits [get]
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	includeArchived := r.URL.Query().Get("includeArchived") == "true"
	views, err := h.svc.List(r.Context(), mw.UserIDFromCtx(r.Context()), includeArchived)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, views)
}

// Get godoc
// @Summary      Get a habit
// @Description  Retrieves a single habit with its derived counters and the heatmap window behind them. Cells are day cells for daily/weekdays habits and week cells (carrying quota progress) for weekly_quota habits — the granularity streakUnit announces. Cross-user access returns 404, never 403.
// @Tags         habits
// @Produce      json
// @Param        id   path      string  true  "Habit ID"
// @Security     BearerAuth
// @Success      200  {object}  HabitDetailEnvelope  "The habit with its history"
// @Failure      404  {object}  ErrorEnvelope        "HABIT_NOT_FOUND"
// @Router       /habits/{id} [get]
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	view, err := h.svc.Get(r.Context(), mw.UserIDFromCtx(r.Context()), chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view)
}

// Create godoc
// @Summary      Create a habit
// @Description  Creates a habit. scheduleKind is one of daily, weekdays (requires byWeekday) or weekly_quota (requires timesPerWeek). polarity is build or quit and is immutable afterwards. Free plan allows 3 active habits.
// @Tags         habits
// @Accept       json
// @Produce      json
// @Param        request  body      CreateHabitRequest  true  "Habit to create"
// @Security     BearerAuth
// @Success      201  {object}  HabitEnvelope  "Created"
// @Failure      403  {object}  ErrorEnvelope  "PLAN_LIMIT_EXCEEDED"
// @Failure      422  {object}  ErrorEnvelope  "INVALID_INPUT"
// @Router       /habits [post]
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateHabitRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	view, err := h.svc.Create(r.Context(), mw.UserIDFromCtx(r.Context()), mw.PlanFromCtx(r.Context()), req)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	respond.JSON(w, http.StatusCreated, view)
}

// Update godoc
// @Summary      Update a habit
// @Description  Edits a habit. polarity is immutable — sending it returns 422. Changing the schedule applies forward only; periods already scored keep their shape. Set archived to true to retire a habit (freeing a plan slot) or false to restore it.
// @Tags         habits
// @Accept       json
// @Produce      json
// @Param        id       path      string              true  "Habit ID"
// @Param        request  body      UpdateHabitRequest  true  "Fields to change"
// @Security     BearerAuth
// @Success      200  {object}  HabitEnvelope  "Updated"
// @Failure      403  {object}  ErrorEnvelope  "PLAN_LIMIT_EXCEEDED (restoring over the limit)"
// @Failure      404  {object}  ErrorEnvelope  "HABIT_NOT_FOUND"
// @Failure      422  {object}  ErrorEnvelope  "INVALID_INPUT (includes an attempted polarity change)"
// @Router       /habits/{id} [patch]
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	var req UpdateHabitRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	view, err := h.svc.Update(r.Context(), mw.UserIDFromCtx(r.Context()), mw.PlanFromCtx(r.Context()),
		chi.URLParam(r, "id"), req)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view)
}

// Delete godoc
// @Summary      Archive or delete a habit
// @Description  Archives a habit by default: the row is retired, the check-in history is retained — it is the user's record of what they did — and a plan slot is freed. Pass permanent=true to delete it outright instead, which cascades to every check-in and cannot be undone. The default is the reversible one on purpose. Cross-user access returns 404, never 403.
// @Tags         habits
// @Produce      json
// @Param        id         path      string   true   "Habit ID"
// @Param        permanent  query     boolean  false  "Destroy the habit and its history instead of archiving it"
// @Security     BearerAuth
// @Success      204  "Archived, or deleted when permanent=true"
// @Failure      404  {object}  ErrorEnvelope  "HABIT_NOT_FOUND"
// @Router       /habits/{id} [delete]
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	userID, id := mw.UserIDFromCtx(r.Context()), chi.URLParam(r, "id")

	// Archiving is the default because it is the reversible one: a client that
	// forgets the flag retires a habit rather than destroying its history.
	// Permanent deletion has to be asked for explicitly.
	del := h.svc.Delete
	if r.URL.Query().Get("permanent") == "true" {
		del = h.svc.DeletePermanently
	}

	if err := del(r.Context(), userID, id); err != nil {
		writeErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Today godoc
// @Summary      Habits due today
// @Description  Returns the habits still owed right now, for the Today-page strip. A weekdays habit appears only on its own days; a weekly-quota habit appears every day until its week's quota is met, then goes quiet. Archived and already-completed habits are excluded.
// @Tags         habits
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  HabitListEnvelope  "Habits due today"
// @Router       /habits/today [get]
func (h *Handler) Today(w http.ResponseWriter, r *http.Request) {
	views, err := h.svc.Today(r.Context(), mw.UserIDFromCtx(r.Context()))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, views)
}

// Subjects godoc
// @Summary      Habit subject catalog
// @Description  The canonical subject list. Subjects are cosmetic — they drive the card icon and never scheduling or targets. labelKey is an i18n key, not a display string. A client meeting an unknown slug renders a fallback icon rather than failing, so the catalog can gain entries without a client release.
// @Tags         habits
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  SubjectListEnvelope  "Subjects"
// @Router       /habits/subjects [get]
func (h *Handler) Subjects(w http.ResponseWriter, r *http.Request) {
	respond.JSON(w, http.StatusOK, SubjectCatalog)
}

// CheckIn godoc
// @Summary      Check in to a habit
// @Description  Records one dated entry. Omit date to check in for today — the server resolves it from the user's timezone and never accepts a client-supplied "today". Omit value to use the habit's target. Idempotent per (habit, date): a repeat call updates the value. A past date is a backfill and must fall inside the window (7 days for daily/weekdays habits, the current and previous week for weekly quota); future dates are refused.
// @Tags         habits
// @Accept       json
// @Produce      json
// @Param        id       path      string          true   "Habit ID"
// @Param        request  body      CheckInRequest  false  "Optional date and value"
// @Security     BearerAuth
// @Success      200  {object}  HabitEnvelope  "The habit"
// @Failure      404  {object}  ErrorEnvelope  "HABIT_NOT_FOUND"
// @Failure      422  {object}  ErrorEnvelope  "INVALID_INPUT (future date, outside the backfill window, negative value, or archived habit)"
// @Router       /habits/{id}/check-in [post]
func (h *Handler) CheckIn(w http.ResponseWriter, r *http.Request) {
	var req CheckInRequest
	if err := decodeOptionalBody(w, r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	view, err := h.svc.CheckIn(r.Context(), mw.UserIDFromCtx(r.Context()), chi.URLParam(r, "id"), req)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view)
}

// UndoCheckIn godoc
// @Summary      Undo a habit check-in
// @Description  Removes one dated entry, reverting the day to not-done. Omit date to undo today. Idempotent: undoing a date with no entry succeeds, because the day is already not done.
// @Tags         habits
// @Accept       json
// @Produce      json
// @Param        id       path      string              true   "Habit ID"
// @Param        request  body      UndoCheckInRequest  false  "Optional date"
// @Security     BearerAuth
// @Success      200  {object}  HabitEnvelope  "The habit"
// @Failure      404  {object}  ErrorEnvelope  "HABIT_NOT_FOUND"
// @Failure      422  {object}  ErrorEnvelope  "INVALID_INPUT (future date, outside the backfill window, or archived habit)"
// @Router       /habits/{id}/check-in [delete]
func (h *Handler) UndoCheckIn(w http.ResponseWriter, r *http.Request) {
	var req UndoCheckInRequest
	if err := decodeOptionalBody(w, r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	view, err := h.svc.UndoCheckIn(r.Context(), mw.UserIDFromCtx(r.Context()), chi.URLParam(r, "id"), req)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view)
}

// decodeOptionalBody decodes a body that may legitimately be absent. Checking in
// with no body is the common case ("I did it today"), and a DELETE carrying no
// body is normal for many clients, so an empty read is success rather than a
// malformed-JSON error.
func decodeOptionalBody[T CheckInRequest | UndoCheckInRequest](w http.ResponseWriter, r *http.Request, dst *T) error {
	r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBytes)

	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "invalid request body")
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "invalid request body")
	}
	return nil
}

// polarityProbe reads only the field the update body deliberately lacks. It
// exists so an attempted polarity change reports *why* it was refused instead of
// the generic "unknown field" DisallowUnknownFields would produce — the client
// has to explain the refusal to a user, and "create a new habit instead" is only
// derivable from a specific message.
type polarityProbe struct {
	Polarity *string `json:"polarity"`
}

// decodeBody enforces the size cap and decodes the JSON body. Both an oversized
// body and a malformed one surface as 422 INVALID_INPUT — the client cannot act
// differently on the distinction, and reporting "too large" separately would
// leak the exact threshold to a prober.
func decodeBody[T CreateHabitRequest | UpdateHabitRequest](w http.ResponseWriter, r *http.Request, dst *T) error {
	r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBytes)

	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "invalid request body")
	}

	// Probe before the strict decode so the polarity message wins over the
	// generic unknown-field one.
	if _, isUpdate := any(dst).(*UpdateHabitRequest); isUpdate {
		var probe polarityProbe
		if json.Unmarshal(raw, &probe) == nil && probe.Polarity != nil {
			return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput,
				"polarity cannot be changed; archive this habit and create a new one")
		}
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "invalid request body")
	}
	return nil
}

func writeErr(w http.ResponseWriter, r *http.Request, err error) {
	if ae, ok := errors.AsType[*apperror.AppError](err); ok {
		respond.Error(w, ae.Status, ae.Code, ae.Message)
		return
	}
	log.Error().Err(err).Str("request_id", mw.GetRequestID(r.Context())).Msg("habit: unexpected internal error")
	respond.Error(w, http.StatusInternalServerError, apperror.ErrInternalServerError, "an unexpected error occurred")
}
