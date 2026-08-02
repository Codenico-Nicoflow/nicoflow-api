// Package cryptoutil provides authenticated symmetric encryption for secrets
// held at rest — currently the Google Calendar refresh token (E-052 / NIC-1838).
//
// AES-256-GCM is used rather than a plain cipher mode because the stored value
// must be tamper-evident, not merely unreadable: GCM authenticates the
// ciphertext, so a row edited in the database fails to decrypt instead of
// yielding attacker-chosen plaintext.
package cryptoutil

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// KeySize is the AES-256 key length in bytes.
const KeySize = 32

var (
	// ErrKeyMissing is returned when a Cipher is used without a configured key.
	// Callers guard with Enabled() first; this is the safety net that turns a
	// missing key into a typed error rather than a nil-pointer panic.
	ErrKeyMissing = errors.New("cryptoutil: no encryption key configured")

	// ErrInvalidKey reports a key that is not a base64-encoded 32 bytes.
	ErrInvalidKey = errors.New("cryptoutil: key must be base64-encoded 32 bytes")

	// ErrDecrypt reports any decryption failure. It is deliberately opaque —
	// distinguishing "wrong key" from "tampered ciphertext" would tell an
	// attacker which of the two they achieved.
	ErrDecrypt = errors.New("cryptoutil: decryption failed")
)

// Cipher encrypts and decrypts secrets with a single AES-256-GCM key.
//
// The zero value is a disabled Cipher: Enabled() reports false and both
// operations return ErrKeyMissing rather than panicking. This mirrors the
// storage and Anthropic clients, so an environment without Google credentials
// boots normally and the whole integration no-ops.
type Cipher struct {
	aead cipher.AEAD
}

// NewCipher builds a Cipher from a base64-encoded 32-byte key.
//
// An empty key yields a disabled Cipher and no error — that is the documented
// "feature off" path, not a misconfiguration. A key that is present but
// malformed IS an error: silently disabling encryption because someone fat
// fingered the value would store live tokens in plaintext.
func NewCipher(base64Key string) (*Cipher, error) {
	if base64Key == "" {
		return &Cipher{}, nil
	}

	key, err := base64.StdEncoding.DecodeString(base64Key)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidKey, err)
	}
	if len(key) != KeySize {
		return nil, fmt.Errorf("%w: got %d bytes", ErrInvalidKey, len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("cryptoutil: new cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cryptoutil: new gcm: %w", err)
	}

	return &Cipher{aead: aead}, nil
}

// Enabled reports whether a usable key is configured.
func (c *Cipher) Enabled() bool { return c != nil && c.aead != nil }

// Encrypt seals plaintext, returning nonce||ciphertext||tag.
//
// A fresh random nonce is generated per call and prepended to the output. Nonce
// reuse under a fixed key is catastrophic for GCM — it leaks the XOR of two
// plaintexts and breaks authentication — so the nonce is never derived from the
// data or from a counter that could restart after a redeploy.
func (c *Cipher) Encrypt(plaintext []byte) ([]byte, error) {
	if !c.Enabled() {
		return nil, ErrKeyMissing
	}

	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("cryptoutil: nonce: %w", err)
	}

	// Sealing into nonce appends the ciphertext to it, so the nonce travels
	// with the value it belongs to and cannot be paired with the wrong row.
	return c.aead.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt opens a value produced by Encrypt.
//
// Returns ErrDecrypt for a wrong key, a tampered ciphertext, or a truncated
// value — all three are indistinguishable to the caller by design.
func (c *Cipher) Decrypt(sealed []byte) ([]byte, error) {
	if !c.Enabled() {
		return nil, ErrKeyMissing
	}

	nonceSize := c.aead.NonceSize()
	if len(sealed) < nonceSize {
		return nil, ErrDecrypt
	}

	plaintext, err := c.aead.Open(nil, sealed[:nonceSize], sealed[nonceSize:], nil)
	if err != nil {
		return nil, ErrDecrypt
	}
	return plaintext, nil
}

// EncryptString is Encrypt for a string secret.
func (c *Cipher) EncryptString(plaintext string) ([]byte, error) {
	return c.Encrypt([]byte(plaintext))
}

// DecryptString is Decrypt returning a string.
func (c *Cipher) DecryptString(sealed []byte) (string, error) {
	plaintext, err := c.Decrypt(sealed)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}
