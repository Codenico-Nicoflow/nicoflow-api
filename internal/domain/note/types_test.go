package note_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/nicoflow/nicoflow-api/internal/domain/note"
)

func TestExcerpt(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		wantLen  int
		wantSame bool
	}{
		{name: "empty stays empty", in: "", wantLen: 0, wantSame: true},
		{name: "short text is untouched", in: "a short note", wantLen: 12, wantSame: true},
		{name: "exactly at the limit is untouched", in: strings.Repeat("a", note.ExcerptLen), wantLen: note.ExcerptLen, wantSame: true},
		{name: "longer text is truncated", in: strings.Repeat("a", note.ExcerptLen+50), wantLen: note.ExcerptLen},
		{name: "multi-byte text cuts on a rune boundary", in: strings.Repeat("ש", note.ExcerptLen+50), wantLen: note.ExcerptLen},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := note.Excerpt(tt.in)

			if n := utf8.RuneCountInString(got); n != tt.wantLen {
				t.Errorf("rune length = %d, want %d", n, tt.wantLen)
			}
			if !utf8.ValidString(got) {
				t.Error("excerpt is not valid UTF-8")
			}
			if tt.wantSame && got != tt.in {
				t.Errorf("got %q, want it unchanged", got)
			}
		})
	}
}
