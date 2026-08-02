package googlecal

import (
	"context"
	"errors"
)

// Sentinel errors the OAuth client returns. Defined here, in the consumer
// package, so the service can classify a failure without importing the
// implementation.
var (
	// ErrOAuthDisabled means no Google credentials are configured.
	ErrOAuthDisabled = errors.New("googlecal: oauth is not configured")
	// ErrOAuthExchange covers any failure turning an authorization code into
	// tokens, including a grant that returned no refresh token.
	ErrOAuthExchange = errors.New("googlecal: oauth exchange failed")
	// ErrOAuthRevoke means Google could not be told to drop the grant.
	ErrOAuthRevoke = errors.New("googlecal: oauth revoke failed")
	// ErrStateInvalid covers an unknown, expired or already-consumed state.
	// The three are deliberately not distinguished — telling a caller which one
	// it was is an oracle for probing valid values.
	ErrStateInvalid = errors.New("googlecal: invalid oauth state")
)

// TokenSet is what a successful consent yields. The access token is absent on
// purpose: it lives for an hour, is re-derivable from the refresh token, and
// persisting it would create a second secret at rest for no benefit.
type TokenSet struct {
	RefreshToken Secret
	AccountEmail string
	Scopes       []string
}

// OAuthClient is Google's side of the consent flow. Implemented by
// internal/google; mocked in tests so CI never calls out.
type OAuthClient interface {
	Enabled() bool
	AuthCodeURL(state string) string
	Exchange(ctx context.Context, code string) (TokenSet, error)
	// Revoke invalidates the grant with Google. Implementations treat an
	// already-invalid token as success — the end state is what matters.
	Revoke(ctx context.Context, refreshToken Secret) error
}
