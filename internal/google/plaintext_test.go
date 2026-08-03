package google

import "testing"

func TestPlainText(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty stays empty"},
		{name: "plain text is untouched", in: "just a note", want: "just a note"},
		{
			name: "line break becomes a space rather than gluing words",
			in:   "line one<br>line two",
			want: "line one line two",
		},
		{
			name: "tags are removed but their text survives",
			in:   "<b>Agenda</b>: <i>ship</i>",
			want: "Agenda : ship",
		},
		{
			name: "link text is kept, the href is not",
			in:   `see <a href="https://example.com/x">the doc</a>`,
			want: "see the doc",
		},
		{
			name: "entities are unescaped after tags are stripped",
			in:   "R&amp;D &lt;notes&gt;",
			want: "R&D <notes>",
		},
		{
			name: "newlines and runs of space collapse to one",
			in:   "  first\n\n\tsecond   third  ",
			want: "first second third",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := plainText(tt.in); got != tt.want {
				t.Errorf("plainText(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
