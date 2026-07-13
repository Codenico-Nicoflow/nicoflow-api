package search

import "testing"

func TestToPrefixTSQuery(t *testing.T) {
	tests := []struct {
		name string
		term string
		want string
	}{
		{"single word", "testing", "testing:*"},
		{"partial word gets prefix", "testin", "testin:*"},
		{"two words AND-joined", "testin proj", "testin:* & proj:*"},
		{"lowercased", "TestIN", "testin:*"},
		{"punctuation stripped", "proj! @task#", "proj:* & task:*"},
		{"collapses extra whitespace", "  a   b  ", "a:* & b:*"},
		{"empty term", "", ""},
		{"only punctuation", "!@#$", ""},
		{"non-ascii preserved", "משימה", "משימה:*"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := toPrefixTSQuery(tt.term); got != tt.want {
				t.Errorf("toPrefixTSQuery(%q) = %q, want %q", tt.term, got, tt.want)
			}
		})
	}
}
