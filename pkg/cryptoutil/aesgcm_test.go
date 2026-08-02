package cryptoutil_test

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/nicoflow/nicoflow-api/pkg/cryptoutil"
)

func newKey(t *testing.T) string {
	t.Helper()
	key := make([]byte, cryptoutil.KeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("rand.Read() error = %v", err)
	}
	return base64.StdEncoding.EncodeToString(key)
}

func newCipher(t *testing.T) *cryptoutil.Cipher {
	t.Helper()
	c, err := cryptoutil.NewCipher(newKey(t))
	if err != nil {
		t.Fatalf("NewCipher() error = %v", err)
	}
	return c
}

func TestNewCipher(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		wantErr error
		enabled bool
	}{
		{name: "valid 32-byte key", key: newKey(t), enabled: true},
		{name: "empty key disables rather than erroring", key: "", enabled: false},
		{
			name:    "key that is not base64",
			key:     "not-valid-base64!!!",
			wantErr: cryptoutil.ErrInvalidKey,
		},
		{
			name:    "base64 but too short",
			key:     base64.StdEncoding.EncodeToString([]byte("sixteen-bytes!!!")),
			wantErr: cryptoutil.ErrInvalidKey,
		},
		{
			name:    "base64 but too long",
			key:     base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("a"), 64)),
			wantErr: cryptoutil.ErrInvalidKey,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := cryptoutil.NewCipher(tt.key)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("NewCipher() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewCipher() error = %v", err)
			}
			if got := c.Enabled(); got != tt.enabled {
				t.Errorf("Enabled() = %v, want %v", got, tt.enabled)
			}
		})
	}
}

func TestCipher_Roundtrip(t *testing.T) {
	tests := []struct {
		name      string
		plaintext string
	}{
		{name: "typical refresh token", plaintext: "1//0eXaMpLeReFrEsHtOkEn-abcdef123456"},
		{name: "empty string", plaintext: ""},
		{name: "unicode", plaintext: "מפתח סודי 🔐"},
		{name: "long value", plaintext: strings.Repeat("token", 1000)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newCipher(t)

			sealed, err := c.EncryptString(tt.plaintext)
			if err != nil {
				t.Fatalf("EncryptString() error = %v", err)
			}

			got, err := c.DecryptString(sealed)
			if err != nil {
				t.Fatalf("DecryptString() error = %v", err)
			}
			if got != tt.plaintext {
				t.Errorf("DecryptString() = %q, want %q", got, tt.plaintext)
			}
		})
	}
}

// The stored value must not contain the plaintext — that is the whole point of
// encrypting it at rest.
func TestCipher_CiphertextHidesPlaintext(t *testing.T) {
	c := newCipher(t)
	plaintext := "1//0-super-secret-refresh-token"

	sealed, err := c.EncryptString(plaintext)
	if err != nil {
		t.Fatalf("EncryptString() error = %v", err)
	}

	if bytes.Contains(sealed, []byte(plaintext)) {
		t.Error("ciphertext contains the plaintext")
	}
}

// A fresh nonce per call means the same input never produces the same output.
// Deterministic ciphertext would leak which users share a value and, worse,
// would mean a repeated nonce under one key.
func TestCipher_EncryptIsNonDeterministic(t *testing.T) {
	c := newCipher(t)

	first, err := c.EncryptString("same input")
	if err != nil {
		t.Fatalf("EncryptString() error = %v", err)
	}
	second, err := c.EncryptString("same input")
	if err != nil {
		t.Fatalf("EncryptString() error = %v", err)
	}

	if bytes.Equal(first, second) {
		t.Error("two encryptions of the same plaintext produced identical ciphertext")
	}
}

func TestCipher_DecryptFailures(t *testing.T) {
	c := newCipher(t)
	sealed, err := c.EncryptString("the original token")
	if err != nil {
		t.Fatalf("EncryptString() error = %v", err)
	}

	tests := []struct {
		name   string
		sealed func() []byte
		cipher func() *cryptoutil.Cipher
	}{
		{
			name:   "wrong key",
			sealed: func() []byte { return sealed },
			cipher: func() *cryptoutil.Cipher { return newCipher(t) },
		},
		{
			name: "tampered ciphertext",
			sealed: func() []byte {
				bad := bytes.Clone(sealed)
				bad[len(bad)-1] ^= 0xFF
				return bad
			},
			cipher: func() *cryptoutil.Cipher { return c },
		},
		{
			name: "tampered nonce",
			sealed: func() []byte {
				bad := bytes.Clone(sealed)
				bad[0] ^= 0xFF
				return bad
			},
			cipher: func() *cryptoutil.Cipher { return c },
		},
		{
			name:   "truncated below nonce size",
			sealed: func() []byte { return []byte{0x01, 0x02} },
			cipher: func() *cryptoutil.Cipher { return c },
		},
		{
			name:   "empty input",
			sealed: func() []byte { return nil },
			cipher: func() *cryptoutil.Cipher { return c },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.cipher().Decrypt(tt.sealed())

			if !errors.Is(err, cryptoutil.ErrDecrypt) {
				t.Fatalf("Decrypt() error = %v, want ErrDecrypt", err)
			}
			// Failure must yield nothing usable, not partial garbage.
			if got != nil {
				t.Errorf("Decrypt() = %v, want nil on failure", got)
			}
		})
	}
}

// A disabled cipher must fail loudly rather than silently returning plaintext —
// the absent-key path disables the FEATURE, it never downgrades encryption.
func TestCipher_DisabledReturnsErrKeyMissing(t *testing.T) {
	c, err := cryptoutil.NewCipher("")
	if err != nil {
		t.Fatalf("NewCipher() error = %v", err)
	}
	if c.Enabled() {
		t.Fatal("Enabled() = true for an empty key")
	}

	if _, err := c.Encrypt([]byte("secret")); !errors.Is(err, cryptoutil.ErrKeyMissing) {
		t.Errorf("Encrypt() error = %v, want ErrKeyMissing", err)
	}
	if _, err := c.Decrypt([]byte("secret")); !errors.Is(err, cryptoutil.ErrKeyMissing) {
		t.Errorf("Decrypt() error = %v, want ErrKeyMissing", err)
	}
}

// The zero value is reachable via struct embedding; it must behave as disabled
// rather than panicking on a nil AEAD.
func TestCipher_ZeroValueIsSafe(t *testing.T) {
	var c cryptoutil.Cipher

	if c.Enabled() {
		t.Error("Enabled() = true for the zero value")
	}
	if _, err := c.Encrypt([]byte("secret")); !errors.Is(err, cryptoutil.ErrKeyMissing) {
		t.Errorf("Encrypt() error = %v, want ErrKeyMissing", err)
	}
}
