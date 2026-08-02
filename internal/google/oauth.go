// Package google is a thin wrapper over Google's OAuth 2.0 web-server flow. It
// implements the googlecal.OAuthClient interface (defined in the consumer
// package) and is the only place that talks to accounts.google.com, so the rest
// of the codebase stays mockable and CI never makes a real call.
//
// Written against net/http rather than golang.org/x/oauth2: the flow needs
// exactly three calls (authorize URL, code exchange, revoke), and the library's
// token-refresh machinery would sit unused while adding a dependency that hides
// the wire format this code has to reason about.
package google

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/nicoflow/nicoflow-api/internal/domain/googlecal"
)

// Google's OAuth 2.0 endpoints.
// https://developers.google.com/identity/protocols/oauth2/web-server
const (
	authEndpoint   = "https://accounts.google.com/o/oauth2/v2/auth"
	tokenEndpoint  = "https://oauth2.googleapis.com/token"
	revokeEndpoint = "https://oauth2.googleapis.com/revoke"
	// userInfoEndpoint identifies which Google account consented, so the UI can
	// show it and the user can tell two accounts apart.
	userInfoEndpoint = "https://www.googleapis.com/oauth2/v2/userinfo"

	// CalendarReadonlyScope is the ONLY scope this integration requests. Read-only
	// by design (E-052 is an overlay, never a writer), and narrow enough to stay
	// in Google's "sensitive" tier rather than "restricted", which would require
	// a paid third-party security assessment.
	CalendarReadonlyScope = "https://www.googleapis.com/auth/calendar.readonly"

	// requestTimeout bounds every call to Google. Without it a hung TLS handshake
	// would occupy a request goroutine indefinitely.
	requestTimeout = 15 * time.Second
)

// Client talks to Google's OAuth endpoints. A disabled client (Enabled()==false)
// is safe to hold and returns a typed error from every method, so boot never
// fails just because Google credentials are absent — mirrors internal/storage
// and internal/anthropic.
type Client struct {
	clientID     string
	clientSecret string
	redirectURL  string
	http         *http.Client
	enabled      bool
}

// New builds the client. Any missing credential yields a disabled client and no
// error: a half-configured environment must read as "feature off", never as a
// client that will fail confusingly at the first request.
func New(clientID, clientSecret, redirectURL string) *Client {
	if clientID == "" || clientSecret == "" || redirectURL == "" {
		return &Client{}
	}
	return &Client{
		clientID:     clientID,
		clientSecret: clientSecret,
		redirectURL:  redirectURL,
		http:         &http.Client{Timeout: requestTimeout},
		enabled:      true,
	}
}

// Enabled reports whether OAuth credentials are configured.
func (c *Client) Enabled() bool { return c.enabled }

// AuthCodeURL builds the consent URL the browser is sent to.
//
// access_type=offline is what makes Google return a refresh token at all;
// prompt=consent forces the consent screen even on re-authorisation, because
// Google omits the refresh token on subsequent grants otherwise — and a
// reconnect that silently yields no token would look like success and behave
// like a broken connection.
func (c *Client) AuthCodeURL(state string) string {
	q := url.Values{
		"client_id":     {c.clientID},
		"redirect_uri":  {c.redirectURL},
		"response_type": {"code"},
		"scope":         {CalendarReadonlyScope},
		"access_type":   {"offline"},
		"prompt":        {"consent"},
		"state":         {state},
	}
	return authEndpoint + "?" + q.Encode()
}

// tokenResponse is Google's token-endpoint payload. Only the fields this flow
// uses are decoded.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
}

// Exchange trades an authorization code for tokens.
//
// The returned refresh token is the sensitive asset; the access token is used
// once here (to read the account email) and deliberately never persisted.
func (c *Client) Exchange(ctx context.Context, code string) (googlecal.TokenSet, error) {
	if !c.enabled {
		return googlecal.TokenSet{}, googlecal.ErrOAuthDisabled
	}

	form := url.Values{
		"client_id":     {c.clientID},
		"client_secret": {c.clientSecret},
		"code":          {code},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {c.redirectURL},
	}

	var token tokenResponse
	if err := c.postForm(ctx, tokenEndpoint, form, &token); err != nil {
		return googlecal.TokenSet{}, err
	}

	// No refresh token means we cannot read the calendar tomorrow, only today —
	// a connection that expires within the hour is worse than a failed one,
	// because the user believes they are connected.
	if token.RefreshToken == "" {
		return googlecal.TokenSet{}, fmt.Errorf("%w: no refresh token returned", googlecal.ErrOAuthExchange)
	}

	email, err := c.accountEmail(ctx, token.AccessToken)
	if err != nil {
		return googlecal.TokenSet{}, err
	}

	return googlecal.TokenSet{
		RefreshToken: googlecal.Secret(token.RefreshToken),
		AccountEmail: email,
		// Google may grant fewer scopes than requested, so record what we were
		// actually given rather than what we asked for.
		Scopes: strings.Fields(token.Scope),
	}, nil
}

// Revoke tells Google to invalidate the grant.
//
// Revoking an already-invalid token is treated as success: the caller's intent —
// that this grant no longer works — is satisfied either way, and failing here
// would strand a local row the user has asked to be rid of.
func (c *Client) Revoke(ctx context.Context, refreshToken googlecal.Secret) error {
	if !c.enabled {
		return googlecal.ErrOAuthDisabled
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, revokeEndpoint,
		strings.NewReader(url.Values{"token": {refreshToken.Reveal()}}.Encode()))
	if err != nil {
		return fmt.Errorf("google: revoke request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", googlecal.ErrOAuthRevoke, err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)

	// 400 is Google's answer for a token that is already invalid — the desired
	// end state, so it is not an error here.
	if resp.StatusCode >= 500 {
		return fmt.Errorf("%w: status %d", googlecal.ErrOAuthRevoke, resp.StatusCode)
	}
	return nil
}

// accountEmail reads which Google account consented.
func (c *Client) accountEmail(ctx context.Context, accessToken string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, userInfoEndpoint, nil)
	if err != nil {
		return "", fmt.Errorf("google: userinfo request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %v", googlecal.ErrOAuthExchange, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w: userinfo status %d", googlecal.ErrOAuthExchange, resp.StatusCode)
	}

	var body struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("%w: decode userinfo: %v", googlecal.ErrOAuthExchange, err)
	}
	return body.Email, nil
}

// postForm posts a form and decodes a JSON response.
//
// Google's error body can echo request parameters, so a non-2xx never has its
// body surfaced to the caller — only the status. The body would otherwise be a
// path for the client secret or the authorization code to reach a log.
func (c *Client) postForm(ctx context.Context, endpoint string, form url.Values, out *tokenResponse) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("google: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", googlecal.ErrOAuthExchange, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("%w: token endpoint status %d", googlecal.ErrOAuthExchange, resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%w: decode token response: %v", googlecal.ErrOAuthExchange, err)
	}
	return nil
}
