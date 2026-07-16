// Package pushutil wraps the web-push library behind a small, fakeable surface.
// It is a no-op when VAPID is unconfigured, mirroring emailutil's empty-DSN
// behaviour so local/dev environments run without keys.
package pushutil

import (
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"

	webpush "github.com/marknefedov/go-webpush/v2"
)

// Subscription is a browser push subscription, decoupled from the underlying
// library type so callers (repository, dispatch) don't import go-webpush.
type Subscription struct {
	Endpoint  string
	P256dhKey string
	AuthKey   string
}

// Sender delivers an encrypted payload to a single push subscription.
type Sender interface {
	// Send pushes payload to sub. A nil error means accepted. If the subscription
	// is gone (404/410), Expired is true so the caller can prune the row.
	Send(ctx context.Context, sub Subscription, payload []byte) (Result, error)
}

// Result reports what happened to a send.
type Result struct {
	Expired bool // push service returned 404/410 → prune the subscription
}

// client is the real, VAPID-configured Sender.
type client struct {
	wp      *webpush.Client
	keys    *webpush.VAPIDKeys
	subject string
}

// noop is the Sender used when VAPID is unconfigured: every send is a silent no-op.
type noop struct{}

func (noop) Send(context.Context, Subscription, []byte) (Result, error) { return Result{}, nil }

// New builds a Sender from base64url VAPID keys and a subject. When any of the
// three is empty it returns a no-op Sender (safe local/dev). A malformed private
// key is a configuration error and is returned.
func New(publicKey, privateKey, subject string) (Sender, error) {
	if publicKey == "" || privateKey == "" || subject == "" {
		return noop{}, nil
	}
	keys, err := vapidKeysFromBase64(privateKey)
	if err != nil {
		return nil, fmt.Errorf("pushutil.New: %w", err)
	}
	return &client{wp: webpush.NewClient(webpush.Config{}), keys: keys, subject: subject}, nil
}

// Send encrypts and POSTs payload to the subscription's endpoint, classifying an
// expired subscription so the caller can prune it.
func (c *client) Send(ctx context.Context, sub Subscription, payload []byte) (Result, error) {
	wpSub := &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys:     webpush.Keys{}, // filled below
	}
	keys, err := webpush.DecodeSubscriptionKeys(sub.AuthKey, sub.P256dhKey)
	if err != nil {
		return Result{}, fmt.Errorf("pushutil.Send: decode keys: %w", err)
	}
	wpSub.Keys = keys

	resp, err := webpush.SendNotification(ctx, payload, wpSub, &webpush.SendOptions{
		Subject:   c.subject,
		VAPIDKeys: c.keys,
		TTL:       86400,
	})
	if err != nil {
		var pushErr *webpush.PushServiceError
		if errors.As(err, &pushErr) {
			return Result{Expired: pushErr.SubscriptionExpired}, err
		}
		return Result{}, fmt.Errorf("pushutil.Send: %w", err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	return Result{}, nil
}

// vapidKeysFromBase64 reconstructs the VAPID keypair from a base64url-encoded P-256
// private scalar (the standard `VAPID_PRIVATE_KEY` form, per RFC 8292). It derives
// the public point via crypto/ecdh (the non-deprecated path) before handing an
// *ecdsa.PrivateKey to the web-push library.
func vapidKeysFromBase64(privateKey string) (*webpush.VAPIDKeys, error) {
	d, err := base64.RawURLEncoding.DecodeString(privateKey)
	if err != nil {
		return nil, fmt.Errorf("decode private key: %w", err)
	}
	ecdhKey, err := ecdh.P256().NewPrivateKey(d)
	if err != nil {
		return nil, fmt.Errorf("invalid VAPID private key: %w", err)
	}
	// The ECDH public key is the uncompressed point (0x04 || X || Y); split it back
	// into the X/Y scalars an ecdsa.PublicKey needs.
	pub := ecdhKey.PublicKey().Bytes()
	half := (len(pub) - 1) / 2
	priv := &ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{
			Curve: elliptic.P256(),
			X:     new(big.Int).SetBytes(pub[1 : 1+half]),
			Y:     new(big.Int).SetBytes(pub[1+half:]),
		},
		D: new(big.Int).SetBytes(d),
	}
	return webpush.ECDSAToVAPIDKeys(priv)
}
