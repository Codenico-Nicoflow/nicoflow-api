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
// @Description  Returns the caller's habits, newest first. Archived habits are excluded unless includeArchived=true. Free users may hold 3 active habits; a downgraded user keeps every habit visible and checkable, only creation is blocked.
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
// @Description  Retrieves a single habit. Cross-user access returns 404, never 403.
// @Tags         habits
// @Produce      json
// @Param        id   path      string  true  "Habit ID"
// @Security     BearerAuth
// @Success      200  {object}  HabitEnvelope  "The habit"
// @Failure      404  {object}  ErrorEnvelope  "HABIT_NOT_FOUND"
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
// @Summary      Archive a habit
// @Description  Soft-deletes a habit by archiving it. The check-in history is retained — it is the user's record of what they did — and archiving frees a plan slot. Cross-user access returns 404, never 403.
// @Tags         habits
// @Produce      json
// @Param        id   path      string  true  "Habit ID"
// @Security     BearerAuth
// @Success      204  "Archived"
// @Failure      404  {object}  ErrorEnvelope  "HABIT_NOT_FOUND"
// @Router       /habits/{id} [delete]
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Delete(r.Context(), mw.UserIDFromCtx(r.Context()), chi.URLParam(r, "id")); err != nil {
		writeErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
