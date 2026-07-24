package storage

import (
	"strings"
	"testing"
)

func TestNewObjectKey(t *testing.T) {
	key := NewObjectKey("u1", "task", "t9")
	if !strings.HasPrefix(key, "attachments/u1/task/t9/") {
		t.Fatalf("key = %q, want attachments/u1/task/t9/<uuid>", key)
	}
	// Leaf is a UUID, never a client-supplied name.
	leaf := key[strings.LastIndex(key, "/")+1:]
	if len(leaf) != 36 {
		t.Errorf("leaf = %q, want 36-char uuid", leaf)
	}
	// Two calls never collide.
	if NewObjectKey("u1", "task", "t9") == key {
		t.Error("NewObjectKey returned the same key twice")
	}
	// Key is under the user's prefix, so the confirm handler can assert ownership.
	if !strings.HasPrefix(key, UserKeyPrefix("u1")) {
		t.Errorf("key %q not under UserKeyPrefix", key)
	}
}

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"report.pdf", "report.pdf"},
		{"../../etc/passwd", "....etcpasswd"},
		{`a"b.png`, "ab.png"},
		{"with\nnewline.txt", "withnewline.txt"},
		{"back\\slash.csv", "backslash.csv"},
		{"   ", "download"},
		{"", "download"},
		{"héllo.png", "héllo.png"},
	}
	for _, tt := range tests {
		if got := sanitizeFilename(tt.in); got != tt.want {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
