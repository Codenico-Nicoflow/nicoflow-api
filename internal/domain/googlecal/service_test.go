package googlecal_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/internal/domain/googlecal"
)

// --- mocks -------------------------------------------------------------------

type mockOAuth struct {
	enabled     bool
	exchangeErr error
	revokeErr   error
	tokens      googlecal.TokenSet
	revokedWith googlecal.Secret
	revokeCalls int
	lastState   string
}

func (m *mockOAuth) Enabled() bool { return m.enabled }

func (m *mockOAuth) AuthCodeURL(state string) string {
	m.lastState = state
	return "https://accounts.google.com/o/oauth2/v2/auth?state=" + state
}

func (m *mockOAuth) Exchange(_ context.Context, _ string) (googlecal.TokenSet, error) {
	if m.exchangeErr != nil {
		return googlecal.TokenSet{}, m.exchangeErr
	}
	return m.tokens, nil
}

func (m *mockOAuth) Revoke(_ context.Context, token googlecal.Secret) error {
	m.revokeCalls++
	m.revokedWith = token
	return m.revokeErr
}

type mockStates struct {
	created      map[string]string // fingerprint -> userID
	redirectPath string
	consumeErr   error
	consumeCalls int
	createErr    error
}

func newMockStates() *mockStates { return &mockStates{created: map[string]string{}} }

func (m *mockStates) Create(_ context.Context, userID, fingerprint, redirectPath string, _ time.Time) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.created[fingerprint] = userID
	m.redirectPath = redirectPath
	return nil
}

func (m *mockStates) Consume(_ context.Context, fingerprint string) (string, string, error) {
	m.consumeCalls++
	if m.consumeErr != nil {
		return "", "", m.consumeErr
	}
	userID, ok := m.created[fingerprint]
	if !ok {
		return "", "", googlecal.ErrStateInvalid
	}
	// Single-use: a second consume of the same value must fail.
	delete(m.created, fingerprint)
	return userID, m.redirectPath, nil
}

func (m *mockStates) DeleteExpired(_ context.Context) (int64, error) { return 0, nil }

type mockRepo struct {
	conn        googlecal.Connection
	getErr      error
	upserted    *googlecal.Connection
	deleteCalls int
}

func (m *mockRepo) Get(_ context.Context, _ string) (googlecal.Connection, error) {
	if m.getErr != nil {
		return googlecal.Connection{}, m.getErr
	}
	return m.conn, nil
}

func (m *mockRepo) Upsert(_ context.Context, c googlecal.Connection) (googlecal.Connection, error) {
	m.upserted = &c
	return c, nil
}

func (m *mockRepo) UpdateSelectedCalendars(_ context.Context, _ string, ids []string) (googlecal.Connection, error) {
	m.conn.SelectedCalendarIDs = ids
	return m.conn, nil
}

func (m *mockRepo) SetError(_ context.Context, _ string, _ *string) error { return nil }

func (m *mockRepo) Delete(_ context.Context, _ string) error {
	m.deleteCalls++
	return nil
}

func (m *mockRepo) UserTimezone(_ context.Context, _ string) (string, error) {
	return "UTC", nil
}

func notConnected() error {
	return apperror.New(http.StatusConflict, apperror.ErrGoogleNotConnected, "not connected")
}

func codeOf(t *testing.T, err error) string {
	t.Helper()
	var appErr *apperror.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("error = %T (%v), want *apperror.AppError", err, err)
	}
	return appErr.Code
}

// --- Connect -----------------------------------------------------------------

func TestService_Connect(t *testing.T) {
	tests := []struct {
		name         string
		enabled      bool
		next         string
		wantErrCode  string
		wantRedirect string
	}{
		{name: "issues a consent url", enabled: true, next: "/calendar", wantRedirect: "/calendar"},
		{name: "disabled integration is typed", enabled: false, wantErrCode: apperror.ErrGoogleAuthFailed},
		// An attacker-supplied `next` must never become a Location header —
		// an open redirect on our own domain is a phishing primitive.
		{name: "absolute url is dropped", enabled: true, next: "https://evil.com", wantRedirect: ""},
		{name: "protocol-relative url is dropped", enabled: true, next: "//evil.com", wantRedirect: ""},
		{name: "backslash trick is dropped", enabled: true, next: "/\\evil.com", wantRedirect: ""},
		{name: "crlf injection is dropped", enabled: true, next: "/ok\r\nX-Bad: 1", wantRedirect: ""},
		{name: "empty stays empty", enabled: true, next: "", wantRedirect: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			states := newMockStates()
			oauth := &mockOAuth{enabled: tt.enabled}
			svc := googlecal.NewService(&mockRepo{}, states, oauth)

			url, err := svc.Connect(context.Background(), "user-1", tt.next)

			if tt.wantErrCode != "" {
				if got := codeOf(t, err); got != tt.wantErrCode {
					t.Fatalf("error code = %q, want %q", got, tt.wantErrCode)
				}
				return
			}
			if err != nil {
				t.Fatalf("Connect() error = %v", err)
			}
			if !strings.HasPrefix(url, "https://accounts.google.com/") {
				t.Errorf("Connect() url = %q, want a Google consent url", url)
			}
			if states.redirectPath != tt.wantRedirect {
				t.Errorf("stored redirect = %q, want %q", states.redirectPath, tt.wantRedirect)
			}
			// The raw state goes to Google; only its fingerprint is stored.
			if _, stored := states.created[oauth.lastState]; stored {
				t.Error("raw state value was stored; only the fingerprint should be")
			}
			if _, stored := states.created[googlecal.StateFingerprint(oauth.lastState)]; !stored {
				t.Error("state fingerprint was not stored")
			}
		})
	}
}

// --- Callback ----------------------------------------------------------------

var validTokens = googlecal.TokenSet{
	RefreshToken: googlecal.Secret("1//0-refresh"),
	AccountEmail: "user@example.com",
	Scopes:       []string{"https://www.googleapis.com/auth/calendar.readonly"},
}

func TestService_Callback(t *testing.T) {
	t.Run("stores the connection on success", func(t *testing.T) {
		states := newMockStates()
		repo := &mockRepo{}
		oauth := &mockOAuth{enabled: true, tokens: validTokens}
		svc := googlecal.NewService(repo, states, oauth)

		if _, err := svc.Connect(context.Background(), "user-1", "/calendar"); err != nil {
			t.Fatalf("Connect() error = %v", err)
		}

		redirect, err := svc.Callback(context.Background(), oauth.lastState, "auth-code")
		if err != nil {
			t.Fatalf("Callback() error = %v", err)
		}
		if redirect != "/calendar" {
			t.Errorf("redirect = %q, want /calendar", redirect)
		}
		if repo.upserted == nil {
			t.Fatal("no connection was stored")
		}
		if repo.upserted.UserID != "user-1" {
			t.Errorf("stored UserID = %q, want the state's owner", repo.upserted.UserID)
		}
		if repo.upserted.RefreshToken.Reveal() != "1//0-refresh" {
			t.Error("stored refresh token does not match the exchanged one")
		}
		// The overlay must work immediately after consent, without a second step.
		if len(repo.upserted.SelectedCalendarIDs) != 1 || repo.upserted.SelectedCalendarIDs[0] != "primary" {
			t.Errorf("SelectedCalendarIDs = %v, want [primary] by default", repo.upserted.SelectedCalendarIDs)
		}
	})

}

func TestService_CallbackStateHandling(t *testing.T) {
	t.Run("rejects a replayed state and stores nothing", func(t *testing.T) {
		states := newMockStates()
		repo := &mockRepo{}
		oauth := &mockOAuth{enabled: true, tokens: validTokens}
		svc := googlecal.NewService(repo, states, oauth)

		if _, err := svc.Connect(context.Background(), "user-1", ""); err != nil {
			t.Fatalf("Connect() error = %v", err)
		}
		state := oauth.lastState

		if _, err := svc.Callback(context.Background(), state, "code"); err != nil {
			t.Fatalf("first Callback() error = %v", err)
		}
		repo.upserted = nil

		_, err := svc.Callback(context.Background(), state, "code")
		if err == nil {
			t.Fatal("replayed state was accepted")
		}
		if got := codeOf(t, err); got != apperror.ErrInvalidToken {
			t.Errorf("error code = %q, want %q", got, apperror.ErrInvalidToken)
		}
		if repo.upserted != nil {
			t.Error("a replayed callback created a connection")
		}
	})

	t.Run("consumes the state before exchanging the code", func(t *testing.T) {
		// Exchanging first would spend an attacker's code against a victim's
		// session before the CSRF check ran.
		states := newMockStates()
		states.consumeErr = googlecal.ErrStateInvalid
		repo := &mockRepo{}
		oauth := &mockOAuth{enabled: true, tokens: validTokens}
		svc := googlecal.NewService(repo, states, oauth)

		if _, err := svc.Callback(context.Background(), "some-state", "code"); err == nil {
			t.Fatal("Callback() succeeded with an invalid state")
		}
		if repo.upserted != nil {
			t.Error("connection stored despite an invalid state")
		}
	})

}

func TestService_CallbackFailures(t *testing.T) {
	tests := []struct {
		name        string
		enabled     bool
		state       string
		code        string
		exchangeErr error
		wantCode    string
	}{
		{name: "missing state", enabled: true, code: "c", wantCode: apperror.ErrInvalidInput},
		{name: "missing code", enabled: true, state: "s", wantCode: apperror.ErrInvalidInput},
		{name: "disabled integration", enabled: false, state: "s", code: "c", wantCode: apperror.ErrGoogleAuthFailed},
		{
			name: "exchange failure is typed and leaks nothing",
			// A grant with no refresh token is a connection that dies within the
			// hour — worse than a visible failure, because the user believes it
			// worked.
			enabled: true, state: "s", code: "c",
			exchangeErr: googlecal.ErrOAuthExchange,
			wantCode:    apperror.ErrGoogleAuthFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			states := newMockStates()
			oauth := &mockOAuth{enabled: tt.enabled, tokens: validTokens, exchangeErr: tt.exchangeErr}
			svc := googlecal.NewService(&mockRepo{}, states, oauth)

			if tt.exchangeErr != nil {
				// Give the state a chance to be valid so the exchange is reached.
				if _, err := svc.Connect(context.Background(), "user-1", ""); err != nil {
					t.Fatalf("Connect() error = %v", err)
				}
				tt.state = oauth.lastState
			}

			_, err := svc.Callback(context.Background(), tt.state, tt.code)
			if err == nil {
				t.Fatal("Callback() succeeded, want an error")
			}
			if got := codeOf(t, err); got != tt.wantCode {
				t.Errorf("error code = %q, want %q", got, tt.wantCode)
			}
			if strings.Contains(err.Error(), "1//0-refresh") {
				t.Error("error message leaked token material")
			}
		})
	}
}

// --- Disconnect --------------------------------------------------------------

func TestService_Disconnect(t *testing.T) {
	connected := googlecal.Connection{
		UserID:       "user-1",
		RefreshToken: googlecal.Secret("1//0-refresh"),
	}

	t.Run("revokes with Google then deletes locally", func(t *testing.T) {
		repo := &mockRepo{conn: connected}
		oauth := &mockOAuth{enabled: true}
		svc := googlecal.NewService(repo, newMockStates(), oauth)

		if err := svc.Disconnect(context.Background(), "user-1"); err != nil {
			t.Fatalf("Disconnect() error = %v", err)
		}
		if oauth.revokeCalls != 1 {
			t.Errorf("revoke calls = %d, want 1", oauth.revokeCalls)
		}
		if oauth.revokedWith.Reveal() != "1//0-refresh" {
			t.Error("revoked with the wrong token")
		}
		if repo.deleteCalls != 1 {
			t.Errorf("delete calls = %d, want 1", repo.deleteCalls)
		}
	})

	t.Run("deletes locally even when revoke fails", func(t *testing.T) {
		// Refusing to disconnect because Google is unreachable would trap the
		// user in a connection they have explicitly rejected.
		repo := &mockRepo{conn: connected}
		oauth := &mockOAuth{enabled: true, revokeErr: googlecal.ErrOAuthRevoke}
		svc := googlecal.NewService(repo, newMockStates(), oauth)

		if err := svc.Disconnect(context.Background(), "user-1"); err != nil {
			t.Fatalf("Disconnect() error = %v", err)
		}
		if repo.deleteCalls != 1 {
			t.Errorf("delete calls = %d, want 1 despite the revoke failure", repo.deleteCalls)
		}
	})

	t.Run("is idempotent when not connected", func(t *testing.T) {
		repo := &mockRepo{getErr: notConnected()}
		oauth := &mockOAuth{enabled: true}
		svc := googlecal.NewService(repo, newMockStates(), oauth)

		if err := svc.Disconnect(context.Background(), "user-1"); err != nil {
			t.Fatalf("Disconnect() error = %v, want nil", err)
		}
		if oauth.revokeCalls != 0 {
			t.Error("revoked despite no connection")
		}
	})

	t.Run("purges an undecryptable connection", func(t *testing.T) {
		// The grant cannot be revoked (the token is unreadable), but the row
		// must still go — leaving it would strand the user permanently.
		repo := &mockRepo{getErr: apperror.New(http.StatusBadGateway, apperror.ErrGoogleAuthFailed, "unreadable")}
		oauth := &mockOAuth{enabled: true}
		svc := googlecal.NewService(repo, newMockStates(), oauth)

		if err := svc.Disconnect(context.Background(), "user-1"); err != nil {
			t.Fatalf("Disconnect() error = %v", err)
		}
		if repo.deleteCalls != 1 {
			t.Errorf("delete calls = %d, want 1", repo.deleteCalls)
		}
	})
}

// Account deletion must revoke the grant, and must never fail because Google is
// unreachable (E-040 / GDPR).
func TestService_RevokeForUser(t *testing.T) {
	tests := []struct {
		name      string
		repo      *mockRepo
		revokeErr error
		wantDel   int
	}{
		{
			name:    "revokes and purges",
			repo:    &mockRepo{conn: googlecal.Connection{UserID: "user-1", RefreshToken: "tok"}},
			wantDel: 1,
		},
		{
			name:      "still purges when Google fails",
			repo:      &mockRepo{conn: googlecal.Connection{UserID: "user-1", RefreshToken: "tok"}},
			revokeErr: googlecal.ErrOAuthRevoke,
			wantDel:   1,
		},
		{
			name:    "no-ops when never connected",
			repo:    &mockRepo{getErr: notConnected()},
			wantDel: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := googlecal.NewService(tt.repo, newMockStates(), &mockOAuth{enabled: true, revokeErr: tt.revokeErr})

			// Returns nothing: deletion cannot be blocked by a downstream service.
			svc.RevokeForUser(context.Background(), "user-1")

			if tt.repo.deleteCalls != tt.wantDel {
				t.Errorf("delete calls = %d, want %d", tt.repo.deleteCalls, tt.wantDel)
			}
		})
	}
}

func TestService_Get(t *testing.T) {
	repo := &mockRepo{conn: googlecal.Connection{
		UserID:             "user-1",
		RefreshToken:       googlecal.Secret("1//0-refresh"),
		GoogleAccountEmail: "user@example.com",
	}}
	svc := googlecal.NewService(repo, newMockStates(), &mockOAuth{enabled: true})

	view, err := svc.Get(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if view.GoogleAccountEmail != "user@example.com" {
		t.Errorf("GoogleAccountEmail = %q", view.GoogleAccountEmail)
	}
}
