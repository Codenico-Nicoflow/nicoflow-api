package googlecal

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	mw "github.com/nicoflow/nicoflow-api/internal/middleware"
	"github.com/nicoflow/nicoflow-api/pkg/respond"
)

// Handler serves the Google connection endpoints. Parses/validates, reads claims
// from context, calls the service, writes the envelope — no business logic.
type Handler struct {
	svc Service
	// appBaseURL is where the callback sends the browser once consent is done.
	// The callback is the one endpoint a human lands on directly, so it redirects
	// rather than returning JSON.
	appBaseURL string
}

// NewHandler creates a Google connection Handler.
func NewHandler(svc Service, appBaseURL string) *Handler {
	return &Handler{svc: svc, appBaseURL: strings.TrimSuffix(appBaseURL, "/")}
}

// Connect handles GET /calendar/google/connect — returns the consent URL.
//
// Returns the URL as JSON rather than issuing a 302: the caller is an
// authenticated XHR carrying a Bearer token, and a redirect would be followed
// by fetch without the browser ever navigating.
func (h *Handler) Connect(w http.ResponseWriter, r *http.Request) {
	authURL, err := h.svc.Connect(r.Context(), mw.UserIDFromCtx(r.Context()), r.URL.Query().Get("next"))
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, ConnectResponse{AuthURL: authURL})
}

// Callback handles GET /calendar/google/callback — Google redirects the browser
// here after consent.
//
// This endpoint is UNAUTHENTICATED: the browser arrives from accounts.google.com
// with no Authorization header, so the `state` value is what identifies the user
// and proves the flow was started by them. It always redirects back into the app
// rather than rendering, since a user is looking at it.
func (h *Handler) Callback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	// The user pressed "Cancel" on the consent screen. Not an error worth a
	// scary page — send them back with a status the UI can read.
	if reason := q.Get("error"); reason != "" {
		log.Info().Str("reason", reason).Msg("googlecal: consent declined")
		h.redirect(w, r, "", "denied")
		return
	}

	redirectPath, err := h.svc.Callback(r.Context(), q.Get("state"), q.Get("code"))
	if err != nil {
		// The browser cannot act on a JSON error envelope here, so failures come
		// back as a status the SPA turns into a message.
		h.redirect(w, r, "", "failed")
		return
	}

	h.redirect(w, r, redirectPath, "connected")
}

// Get handles GET /calendar/google/connection.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	view, err := h.svc.Get(r.Context(), mw.UserIDFromCtx(r.Context()))
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view)
}

// Disconnect handles DELETE /calendar/google/connection. 204 — idempotent, so
// disconnecting an already-disconnected account is a success.
func (h *Handler) Disconnect(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Disconnect(r.Context(), mw.UserIDFromCtx(r.Context())); err != nil {
		h.writeErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// redirect sends the browser back into the SPA with a status query param.
//
// The destination is built by parsing the configured base URL and then
// overwriting only its Path and RawQuery. Scheme and Host therefore always come
// from configuration and are structurally unreachable from user input — string
// concatenation would leave "is this an open redirect?" a question about the
// sanitiser, which is how open redirects get shipped.
//
// `path` is sanitised at Connect time too; this is the second of the two checks,
// because this is the method that actually writes a Location header.
func (h *Handler) redirect(w http.ResponseWriter, r *http.Request, path, status string) {
	base, err := url.Parse(h.appBaseURL)
	if err != nil || base.Host == "" {
		// A misconfigured base URL must not become a redirect to whatever the
		// caller supplied. Relative Location keeps the browser on this origin.
		http.Redirect(w, r, "/?google="+url.QueryEscape(status), http.StatusFound)
		return
	}

	target := *base
	target.Path = "/settings"
	if strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "//") {
		target.Path = path
	}
	// Set, not append: the sanitised path carries no query of its own, and
	// building the query through url.Values escapes the value for us.
	target.RawQuery = url.Values{"google": {status}}.Encode()

	http.Redirect(w, r, target.String(), http.StatusFound)
}

func (h *Handler) writeErr(w http.ResponseWriter, r *http.Request, err error) {
	writeErr(w, r, err)
}

// writeErr turns a domain error into the standard envelope. Package-level so the
// connection and events handlers cannot drift into two different error shapes.
func writeErr(w http.ResponseWriter, r *http.Request, err error) {
	if ae, ok := errors.AsType[*apperror.AppError](err); ok {
		respond.Error(w, ae.Status, ae.Code, ae.Message)
		return
	}
	log.Error().Err(err).Str("request_id", mw.GetRequestID(r.Context())).Msg("googlecal: unexpected internal error")
	respond.Error(w, http.StatusInternalServerError, apperror.ErrInternalServerError, "an unexpected error occurred")
}
