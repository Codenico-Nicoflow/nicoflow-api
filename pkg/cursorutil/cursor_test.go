package cursorutil_test

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nicoflow/nicoflow-api/pkg/cursorutil"
)

// ── EncodeTime / DecodeTime round-trip ───────────────────────────────────────

func TestEncodeDecodeTime_RoundTrip(t *testing.T) {
	ts := time.Date(2024, 3, 15, 12, 30, 45, 123456789, time.UTC)
	id := "note_abc123"

	cursor := cursorutil.EncodeTime(ts, id)
	if cursor == "" {
		t.Fatal("EncodeTime returned empty string")
	}

	got, gotID, err := cursorutil.DecodeTime(cursor)
	if err != nil {
		t.Fatalf("DecodeTime: %v", err)
	}
	if !got.Equal(ts) {
		t.Errorf("time = %v, want %v", got, ts)
	}
	if gotID != id {
		t.Errorf("id = %q, want %q", gotID, id)
	}
}

func TestDecodeTime_EmptyCursor_ReturnsZero(t *testing.T) {
	ts, id, err := cursorutil.DecodeTime("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ts.IsZero() {
		t.Errorf("time = %v, want zero", ts)
	}
	if id != "" {
		t.Errorf("id = %q, want empty", id)
	}
}

func TestDecodeTime_MalformedBase64(t *testing.T) {
	_, _, err := cursorutil.DecodeTime("not_valid_base64!!!")
	if err == nil {
		t.Fatal("expected error for malformed base64, got nil")
	}
}

func TestDecodeTime_MalformedPayload(t *testing.T) {
	import64 := "bm9waXBl" // base64("nopipe") — no pipe separator
	_, _, err := cursorutil.DecodeTime(import64)
	if err == nil {
		t.Fatal("expected error for missing pipe separator, got nil")
	}
}

func TestDecodeTime_BadTimestamp(t *testing.T) {
	import64 := "bm90YXRpbWV8aWQ=" // base64("notatime|id")
	_, _, err := cursorutil.DecodeTime(import64)
	if err == nil {
		t.Fatal("expected error for non-RFC3339Nano timestamp, got nil")
	}
}

// ── EncodeInt / DecodeInt round-trip ─────────────────────────────────────────

func TestEncodeDecodeInt_RoundTrip(t *testing.T) {
	cases := []struct {
		key int
		id  string
	}{
		{0, "area_1"},
		{42, "area_abc"},
		{9999, "a:b:c"}, // id with colons — SplitN(n=2) must handle it
	}
	for _, tc := range cases {
		cursor := cursorutil.EncodeInt(tc.key, tc.id)
		gotKey, gotID, err := cursorutil.DecodeInt(cursor)
		if err != nil {
			t.Errorf("key=%d id=%q: DecodeInt error: %v", tc.key, tc.id, err)
			continue
		}
		if gotKey != tc.key {
			t.Errorf("key=%d id=%q: decoded key = %d", tc.key, tc.id, gotKey)
		}
		if gotID != tc.id {
			t.Errorf("key=%d id=%q: decoded id = %q", tc.key, tc.id, gotID)
		}
	}
}

func TestDecodeInt_EmptyCursor_ReturnsMinusOne(t *testing.T) {
	key, id, err := cursorutil.DecodeInt("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != -1 {
		t.Errorf("key = %d, want -1", key)
	}
	if id != "" {
		t.Errorf("id = %q, want empty", id)
	}
}

func TestDecodeInt_MalformedBase64(t *testing.T) {
	_, _, err := cursorutil.DecodeInt("!!!bad!!!")
	if err == nil {
		t.Fatal("expected error for malformed base64, got nil")
	}
}

func TestDecodeInt_MissingColon(t *testing.T) {
	import64 := "bm9jb2xvbg==" // base64("nocolon")
	_, _, err := cursorutil.DecodeInt(import64)
	if err == nil {
		t.Fatal("expected error for missing colon separator, got nil")
	}
}

func TestDecodeInt_BadKey(t *testing.T) {
	import64 := "bm90aW50OmlkZW50aWZpZXI=" // base64("notint:identifier")
	_, _, err := cursorutil.DecodeInt(import64)
	if err == nil {
		t.Fatal("expected error for non-integer key, got nil")
	}
}

// ── ParsePageParams ───────────────────────────────────────────────────────────

func parseReq(t *testing.T, rawQuery string) (string, int, error) {
	t.Helper()
	r := httptest.NewRequest("GET", "/?"+rawQuery, nil)
	return cursorutil.ParsePageParams(r)
}

func TestParsePageParams_NeitherParam(t *testing.T) {
	cursor, limit, err := parseReq(t, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cursor != "" {
		t.Errorf("cursor = %q, want empty", cursor)
	}
	if limit != 0 {
		t.Errorf("limit = %d, want 0", limit)
	}
}

func TestParsePageParams_CursorOnly(t *testing.T) {
	cursor, limit, err := parseReq(t, "cursor=abc123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cursor != "abc123" {
		t.Errorf("cursor = %q, want abc123", cursor)
	}
	if limit != 0 {
		t.Errorf("limit = %d, want 0", limit)
	}
}

func TestParsePageParams_LimitOnly(t *testing.T) {
	cursor, limit, err := parseReq(t, "limit=20")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cursor != "" {
		t.Errorf("cursor = %q, want empty", cursor)
	}
	if limit != 20 {
		t.Errorf("limit = %d, want 20", limit)
	}
}

func TestParsePageParams_BothParams(t *testing.T) {
	cursor, limit, err := parseReq(t, "cursor=xyz&limit=10")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cursor != "xyz" || limit != 10 {
		t.Errorf("got cursor=%q limit=%d, want xyz 10", cursor, limit)
	}
}

func TestParsePageParams_LimitBoundaries(t *testing.T) {
	cases := []struct {
		raw  string
		want int
	}{
		{"1", 1},
		{"100", 100},
	}
	for _, tc := range cases {
		_, limit, err := parseReq(t, "limit="+tc.raw)
		if err != nil {
			t.Errorf("limit=%s: unexpected error: %v", tc.raw, err)
		}
		if limit != tc.want {
			t.Errorf("limit=%s: got %d, want %d", tc.raw, limit, tc.want)
		}
	}
}

func TestParsePageParams_LimitOutOfRange(t *testing.T) {
	for _, raw := range []string{"0", "101", "-1", "200"} {
		_, _, err := parseReq(t, "limit="+raw)
		if err == nil {
			t.Errorf("limit=%s: expected error, got nil", raw)
		}
	}
}

func TestParsePageParams_LimitNotInteger(t *testing.T) {
	_, _, err := parseReq(t, "limit=abc")
	if err == nil {
		t.Fatal("expected error for non-integer limit, got nil")
	}
}
