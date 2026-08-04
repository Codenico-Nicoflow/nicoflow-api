package search

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	mw "github.com/nicoflow/nicoflow-api/internal/middleware"
	"github.com/nicoflow/nicoflow-api/pkg/respond"
)

// Handler handles HTTP requests for the search domain.
type Handler struct{ svc Service }

// NewHandler creates a new search Handler.
func NewHandler(svc Service) *Handler { return &Handler{svc: svc} }

// Search godoc
// @Summary      Full-text search
// @Description  Searches the user's tasks, projects and areas via PostgreSQL full-text (plainto_tsquery), ranked by relevance.
// @Tags         search
// @Produce      json
// @Param        q      query     string  true   "Search term (2–100 chars)"
// @Param        types  query     string  false  "Comma-separated groups: task,project,area (omit for all)"
// @Param        limit  query     int     false  "Per-type cap (default 10, max 50)"
// @Security     BearerAuth
// @Success      200  {object}  SearchEnvelope  "Grouped results"
// @Failure      400  {object}  ErrorEnvelope   "INVALID_INPUT"
// @Failure      401  {object}  ErrorEnvelope   "UNAUTHORIZED"
// @Router       /search [get]
func (h *Handler) Search(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())

	qv := r.URL.Query()

	var types []string
	if raw := qv.Get("types"); raw != "" {
		types = strings.Split(raw, ",")
	}

	limit := 0 // 0 → service defaults to 10
	if l := qv.Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil {
			limit = parsed
		}
	}

	query, err := Validate(qv.Get("q"), types, limit)
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	resp, err := h.svc.Search(r.Context(), userID, query)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, resp)
}

func writeAppError(w http.ResponseWriter, r *http.Request, err error) {
	var ae *apperror.AppError
	if errors.As(err, &ae) {
		respond.Error(w, ae.Status, ae.Code, ae.Message)
		return
	}
	log.Error().Err(err).Str("request_id", mw.GetRequestID(r.Context())).Msg("unexpected internal error")
	respond.Error(w, http.StatusInternalServerError, apperror.ErrInternalServerError, "an unexpected error occurred")
}
