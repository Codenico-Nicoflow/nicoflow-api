package emailutil

import (
	"strings"
	"testing"
)

func TestSendDueDigest_EmptyDSNIsNoOp(t *testing.T) {
	// No DSN → must not attempt a connection; returns nil.
	if err := SendDueDigest("x@y.test", []DigestTask{{Title: "A"}}, ""); err != nil {
		t.Fatalf("empty DSN should be a no-op, got %v", err)
	}
}

func TestSendDueDigest_NoTasksIsNoOp(t *testing.T) {
	// A configured DSN but no tasks → still a no-op (would otherwise dial).
	if err := SendDueDigest("x@y.test", nil, "smtp://user:pass@localhost:2525"); err != nil {
		t.Fatalf("no tasks should be a no-op, got %v", err)
	}
}

func TestBuildDueDigest(t *testing.T) {
	tests := []struct {
		name        string
		tasks       []DigestTask
		wantSubject string
	}{
		{"single", []DigestTask{{Title: "One"}}, "You have 1 task due soon"},
		{"plural", []DigestTask{{Title: "One"}, {Title: "Two"}}, "You have 2 tasks due soon"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subject, body := buildDueDigest(tt.tasks)
			if subject != tt.wantSubject {
				t.Fatalf("subject = %q, want %q", subject, tt.wantSubject)
			}
			for _, task := range tt.tasks {
				if !strings.Contains(body, task.Title) {
					t.Fatalf("body missing task %q", task.Title)
				}
			}
		})
	}
}

func TestBuildDueDigest_EscapesTitles(t *testing.T) {
	// Task titles are user-controlled and land in HTML — must be escaped.
	_, body := buildDueDigest([]DigestTask{{Title: `<script>alert(1)</script>`}})
	if strings.Contains(body, "<script>") {
		t.Fatalf("unescaped title leaked into body: %s", body)
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Fatalf("expected escaped title in body")
	}
}
