package ai

import (
	"net/http"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/pkg/respond"
)

// Handler handles HTTP requests for the AI assistant domain.
type Handler struct{ svc Service }

// NewHandler creates a new AI Handler.
func NewHandler(svc Service) *Handler { return &Handler{svc: svc} }

func notImplemented(w http.ResponseWriter, _ *http.Request) {
	respond.Error(w, http.StatusNotImplemented, apperror.ErrInternalServerError, "not implemented")
}

func (h *Handler) ListSessions(w http.ResponseWriter, r *http.Request)  { notImplemented(w, r) }
func (h *Handler) GetSession(w http.ResponseWriter, r *http.Request)    { notImplemented(w, r) }
func (h *Handler) CreateSession(w http.ResponseWriter, r *http.Request) { notImplemented(w, r) }
func (h *Handler) DeleteSession(w http.ResponseWriter, r *http.Request) { notImplemented(w, r) }
func (h *Handler) ListMessages(w http.ResponseWriter, r *http.Request)  { notImplemented(w, r) }
func (h *Handler) SendMessage(w http.ResponseWriter, r *http.Request)   { notImplemented(w, r) }
func (h *Handler) ParseNLP(w http.ResponseWriter, r *http.Request)      { notImplemented(w, r) }
