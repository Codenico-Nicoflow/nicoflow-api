package bucket

import (
	"net/http"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/pkg/respond"
)

// Handler handles HTTP requests for the bucket (inbox) domain.
type Handler struct{ svc Service }

// NewHandler creates a new bucket Handler.
func NewHandler(svc Service) *Handler { return &Handler{svc: svc} }

func notImplemented(w http.ResponseWriter, _ *http.Request) {
	respond.Error(w, http.StatusNotImplemented, apperror.ErrInternalServerError, "not implemented")
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request)    { notImplemented(w, r) }
func (h *Handler) Get(w http.ResponseWriter, r *http.Request)     { notImplemented(w, r) }
func (h *Handler) Create(w http.ResponseWriter, r *http.Request)  { notImplemented(w, r) }
func (h *Handler) Update(w http.ResponseWriter, r *http.Request)  { notImplemented(w, r) }
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request)  { notImplemented(w, r) }
func (h *Handler) Process(w http.ResponseWriter, r *http.Request) { notImplemented(w, r) }
