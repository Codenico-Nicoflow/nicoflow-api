package storage

import (
	"strings"

	"github.com/google/uuid"
)

// keyPrefix is the fixed root under which every attachment object lives.
const keyPrefix = "attachments"

// NewObjectKey builds the S3 key for a new attachment. The layout is
// attachments/{userID}/{ownerType}/{ownerID}/{uuid} — a random UUID leaf, never
// the client filename, so the key leaks nothing and can't collide. The user
// segment lets the download handler cheaply assert a key belongs to its caller.
func NewObjectKey(userID, ownerType, ownerID string) string {
	return strings.Join([]string{keyPrefix, userID, ownerType, ownerID, uuid.NewString()}, "/")
}

// UserKeyPrefix is the prefix every key for a given user must start with. The
// confirm handler uses it to reject a key that isn't under the caller's tree.
func UserKeyPrefix(userID string) string {
	return keyPrefix + "/" + userID + "/"
}

// sanitizeFilename strips path separators, control characters, and quotes so the
// value is safe to embed in a Content-Disposition header. It never returns
// empty — a fully-stripped name falls back to "download".
func sanitizeFilename(name string) string {
	name = strings.Map(func(r rune) rune {
		switch {
		case r < 0x20 || r == 0x7f: // control chars
			return -1
		case r == '/' || r == '\\' || r == '"':
			return -1
		default:
			return r
		}
	}, name)
	name = strings.TrimSpace(name)
	if name == "" {
		return "download"
	}
	return name
}
