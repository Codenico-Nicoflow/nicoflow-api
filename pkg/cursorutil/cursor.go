// Package cursorutil provides shared keyset-pagination helpers used across all
// domains that paginate with an opaque base64 cursor. Two cursor key types are
// supported:
//
//   - Time-based  (EncodeTime / DecodeTime)  — keyset on (created_at|updated_at, id) DESC.
//     Wire format: RFC3339Nano + "|" + id, then base64 std-encoding.
//
//   - Int-based   (EncodeInt / DecodeInt)    — keyset on (display_order, id) ASC.
//     Wire format: strconv.Itoa(key) + ":" + id, then base64 std-encoding.
//
// ParsePageParams reads the "cursor" and "limit" query params from a request.
// An absent limit returns 0 (caller/repo normalises to its default).  A present
// limit must be an integer in [1, 100]; anything else is an error the handler
// should surface as 422 INVALID_INPUT.
package cursorutil

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// EncodeTime builds a time-based cursor opaque token.
func EncodeTime(key time.Time, id string) string {
	raw := key.UTC().Format(time.RFC3339Nano) + "|" + id
	return base64.StdEncoding.EncodeToString([]byte(raw))
}

// DecodeTime unpacks a time-based cursor.  An empty cursor returns zero
// time / "" and is not an error — it means "no cursor, start from the top".
func DecodeTime(cursor string) (time.Time, string, error) {
	if cursor == "" {
		return time.Time{}, "", nil
	}
	b, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("cursorutil.DecodeTime: %w", err)
	}
	parts := strings.SplitN(string(b), "|", 2)
	if len(parts) != 2 {
		return time.Time{}, "", fmt.Errorf("cursorutil.DecodeTime: malformed cursor")
	}
	t, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, "", fmt.Errorf("cursorutil.DecodeTime timestamp: %w", err)
	}
	return t, parts[1], nil
}

// EncodeInt builds an int-based cursor opaque token.
func EncodeInt(key int, id string) string {
	raw := strconv.Itoa(key) + ":" + id
	return base64.StdEncoding.EncodeToString([]byte(raw))
}

// DecodeInt unpacks an int-based cursor.  An empty cursor returns -1 / "",
// which sits before the first valid display_order (0), so the first page
// needs no special-case WHERE clause.
func DecodeInt(cursor string) (int, string, error) {
	if cursor == "" {
		return -1, "", nil
	}
	b, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return 0, "", fmt.Errorf("cursorutil.DecodeInt: %w", err)
	}
	parts := strings.SplitN(string(b), ":", 2)
	if len(parts) != 2 {
		return 0, "", fmt.Errorf("cursorutil.DecodeInt: malformed cursor")
	}
	key, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, "", fmt.Errorf("cursorutil.DecodeInt key: %w", err)
	}
	return key, parts[1], nil
}

// ParsePageParams reads the "cursor" and "limit" query parameters.
//
//   - cursor: returned as-is (empty string when absent).
//   - limit: absent → 0 (caller defaults); present → must be [1, 100] or
//     an error is returned.
func ParsePageParams(r *http.Request) (cursor string, limit int, err error) {
	q := r.URL.Query()
	cursor = q.Get("cursor")

	raw := q.Get("limit")
	if raw == "" {
		return cursor, 0, nil
	}
	n, parseErr := strconv.Atoi(raw)
	if parseErr != nil {
		return "", 0, fmt.Errorf("cursorutil.ParsePageParams: limit is not an integer")
	}
	if n < 1 || n > 100 {
		return "", 0, fmt.Errorf("cursorutil.ParsePageParams: limit must be between 1 and 100")
	}
	return cursor, n, nil
}
