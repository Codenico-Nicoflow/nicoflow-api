package googlecal

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
)

// Service is the connection lifecycle: consent, callback, disconnect.
type Service interface {
	// Connect issues a single-use state and returns the Google consent URL.
	Connect(ctx context.Context, userID, redirectPath string) (string, error)
	// Callback consumes the state, exchanges the code, and stores the connection.
	// Returns the redirect path recorded at Connect time.
	Callback(ctx context.Context, state, code string) (redirectPath string, err error)
	Get(ctx context.Context, userID string) (ConnectionView, error)
	// Disconnect revokes the grant WITH Google and then deletes the local row.
	Disconnect(ctx context.Context, userID string) error
	// RevokeForUser is the account-deletion hook. Never fails — see the
	// implementation for why deletion must not be blocked by Google.
	RevokeForUser(ctx context.Context, userID string)
}

type service struct {
	repo   Repository
	states StateRepository
	oauth  OAuthClient
}

// NewService wires the connection service.
func NewService(repo Repository, states StateRepository, oauth OAuthClient) Service {
	return &service{repo: repo, states: states, oauth: oauth}
}

func (s *service) Connect(ctx context.Context, userID, redirectPath string) (string, error) {
	if !s.oauth.Enabled() {
		return "", unavailable()
	}

	// An attacker-supplied redirect would turn our callback into an open
	// redirect — a phishing primitive on our own domain. Only in-app paths are
	// accepted, and anything else degrades to the default rather than erroring,
	// since a bad `next` is not worth failing a connect over.
	redirectPath = sanitiseRedirect(redirectPath)

	raw, fingerprint, err := GenerateState()
	if err != nil {
		return "", err
	}
	if err := s.states.Create(ctx, userID, fingerprint, redirectPath, time.Now().Add(StateTTL)); err != nil {
		return "", err
	}

	return s.oauth.AuthCodeURL(raw), nil
}

func (s *service) Callback(ctx context.Context, state, code string) (string, error) {
	if !s.oauth.Enabled() {
		return "", unavailable()
	}
	if state == "" || code == "" {
		return "", apperror.New(http.StatusBadRequest, apperror.ErrInvalidInput, "state and code are required")
	}

	// Consume before exchanging: the state proves this callback belongs to a
	// flow this user started. Exchanging first would let an attacker's code be
	// spent against a victim's session before the CSRF check ran.
	userID, redirectPath, err := s.states.Consume(ctx, StateFingerprint(state))
	if err != nil {
		return "", apperror.New(http.StatusBadRequest, apperror.ErrInvalidToken, "invalid or expired oauth state")
	}

	tokens, err := s.oauth.Exchange(ctx, code)
	if err != nil {
		// The underlying error may quote Google's response; log it server-side
		// with the user id and return a typed code carrying no detail.
		log.Error().Err(err).Str("user_id", userID).Msg("googlecal: token exchange failed")
		return "", authFailed("could not complete the Google connection")
	}

	if _, err := s.repo.Upsert(ctx, Connection{
		UserID:             userID,
		RefreshToken:       tokens.RefreshToken,
		GoogleAccountEmail: tokens.AccountEmail,
		Scopes:             tokens.Scopes,
		// Default the overlay to the primary calendar so the feature works
		// immediately after consent; the picker (NIC-1857) is progressive
		// disclosure, not a required second step.
		SelectedCalendarIDs: []string{"primary"},
	}); err != nil {
		return "", err
	}

	return redirectPath, nil
}

func (s *service) Get(ctx context.Context, userID string) (ConnectionView, error) {
	conn, err := s.repo.Get(ctx, userID)
	if err != nil {
		return ConnectionView{}, err
	}
	return conn.View(), nil
}

// Disconnect revokes with Google first, then deletes locally.
//
// Order matters and so does the failure policy. Deleting the row without
// revoking would leave a live grant behind after the user said "disconnect" —
// they would see it gone from Nicoflow while it still appears in their Google
// account permissions. That is the trust violation this method exists to avoid.
//
// A revoke failure is logged but does NOT abort the delete: refusing to
// disconnect because Google is unreachable traps the user in a connection they
// have explicitly rejected. The grant is theirs to remove at
// myaccount.google.com in that case, and the surviving row would be worse.
func (s *service) Disconnect(ctx context.Context, userID string) error {
	conn, err := s.repo.Get(ctx, userID)
	if err != nil {
		var appErr *apperror.AppError
		// Already disconnected — the caller's intent is satisfied.
		if errors.As(err, &appErr) && appErr.Code == apperror.ErrGoogleNotConnected {
			return nil
		}
		// A connection we cannot decrypt still needs its row removed; the grant
		// is unrevokable from here since the token is unreadable.
		if errors.As(err, &appErr) && appErr.Code == apperror.ErrGoogleAuthFailed {
			log.Warn().Str("user_id", userID).Msg("googlecal: disconnecting an unreadable connection; grant not revoked")
			return s.repo.Delete(ctx, userID)
		}
		return err
	}

	if err := s.oauth.Revoke(ctx, conn.RefreshToken); err != nil {
		log.Error().Err(err).Str("user_id", userID).Msg("googlecal: revoke failed; deleting local connection anyway")
	}

	return s.repo.Delete(ctx, userID)
}

// RevokeForUser is the account-deletion hook (E-040 / GDPR).
//
// Deleting a Nicoflow account must not leave a live third-party grant behind.
// This is easy to miss because account deletion is a soft delete — the row
// stays, so nothing forces the connection to be dealt with. It never returns an
// error: account deletion must not fail because Google is unreachable.
func (s *service) RevokeForUser(ctx context.Context, userID string) {
	if err := s.Disconnect(ctx, userID); err != nil {
		log.Error().Err(err).Str("user_id", userID).Msg("googlecal: revoke on account deletion failed")
	}
}

// sanitiseRedirect keeps only same-app absolute paths.
//
// Rejects anything with a scheme or authority — "//evil.com" is a
// protocol-relative URL that browsers treat as absolute, which is the classic
// way an open-redirect filter gets bypassed.
func sanitiseRedirect(path string) string {
	if path == "" || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return ""
	}
	if strings.Contains(path, "\\") || strings.ContainsAny(path, "\r\n") {
		return ""
	}
	return path
}

func unavailable() *apperror.AppError {
	return apperror.New(http.StatusServiceUnavailable, apperror.ErrGoogleAuthFailed,
		"the Google Calendar integration is not configured")
}

func authFailed(msg string) *apperror.AppError {
	return apperror.New(http.StatusBadGateway, apperror.ErrGoogleAuthFailed, msg)
}
