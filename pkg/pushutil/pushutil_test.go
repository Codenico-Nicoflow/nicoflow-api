package pushutil

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"testing"
)

// AC4: unset VAPID → a no-op Sender that never errors.
func TestNew_UnsetIsNoOp(t *testing.T) {
	tests := []struct {
		name               string
		pub, priv, subject string
	}{
		{"all empty", "", "", ""},
		{"missing private", "pub", "", "mailto:a@b.c"},
		{"missing subject", "pub", "priv", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := New(tt.pub, tt.priv, tt.subject)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			res, err := s.Send(context.Background(), Subscription{Endpoint: "https://x"}, []byte("hi"))
			if err != nil || res.Expired {
				t.Fatalf("no-op send: res=%+v err=%v, want zero/nil", res, err)
			}
		})
	}
}

// A malformed private key is a configuration error surfaced at construction.
func TestNew_BadPrivateKey(t *testing.T) {
	if _, err := New("pub", "!!!not-base64!!!", "mailto:a@b.c"); err == nil {
		t.Fatal("expected error for malformed VAPID private key")
	}
}

// A valid base64url P-256 scalar builds a real sender without error.
func TestNew_ValidKeyBuildsSender(t *testing.T) {
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	priv := base64.RawURLEncoding.EncodeToString(key.Bytes())

	s, err := New("pub", priv, "mailto:a@b.c")
	if err != nil {
		t.Fatalf("New with valid key: %v", err)
	}
	if _, ok := s.(*client); !ok {
		t.Fatalf("expected a real *client sender, got %T", s)
	}
}
