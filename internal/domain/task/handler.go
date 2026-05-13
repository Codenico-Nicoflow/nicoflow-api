package task

import (
	"net/http"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/pkg/respond"
)

// Handler handles HTTP requests for the task domain.
type Handler struct{ svc Service }

// NewHandler creates a new task Handler.
func NewHandler(svc Service) *Handler { return &Handler{svc: svc} }

func notImplemented(w http.ResponseWriter, _ *http.Request) {
	respond.Error(w, http.StatusNotImplemented, apperror.ErrInternalServerError, "not implemented")
}

func (h *Handler) ListByProject(w http.ResponseWriter, r *http.Request)      { notImplemented(w, r) }
func (h *Handler) Get(w http.ResponseWriter, r *http.Request)                { notImplemented(w, r) }
func (h *Handler) Create(w http.ResponseWriter, r *http.Request)             { notImplemented(w, r) }
func (h *Handler) Update(w http.ResponseWriter, r *http.Request)             { notImplemented(w, r) }
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request)             { notImplemented(w, r) }
func (h *Handler) ListSubtasks(w http.ResponseWriter, r *http.Request)       { notImplemented(w, r) }
func (h *Handler) CreateSubtask(w http.ResponseWriter, r *http.Request)      { notImplemented(w, r) }
func (h *Handler) UpdateSubtask(w http.ResponseWriter, r *http.Request)      { notImplemented(w, r) }
func (h *Handler) DeleteSubtask(w http.ResponseWriter, r *http.Request)      { notImplemented(w, r) }
func (h *Handler) TimeSpread(w http.ResponseWriter, r *http.Request)         { notImplemented(w, r) }
func (h *Handler) Search(w http.ResponseWriter, r *http.Request)             { notImplemented(w, r) }
func (h *Handler) ListAttachments(w http.ResponseWriter, r *http.Request)    { notImplemented(w, r) }
func (h *Handler) CreateAttachment(w http.ResponseWriter, r *http.Request)   { notImplemented(w, r) }
func (h *Handler) DownloadAttachment(w http.ResponseWriter, r *http.Request) { notImplemented(w, r) }
func (h *Handler) DeleteAttachment(w http.ResponseWriter, r *http.Request)   { notImplemented(w, r) }
