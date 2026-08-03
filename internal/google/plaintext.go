package google

import (
	"html"
	"strings"
	"unicode"
)

// plainText flattens a Google event description to displayable text.
//
// Google returns descriptions as HTML — `<br>`, `<a href>`, `<b>`, and whatever
// a user pasted from a document. The overlay renders text, not markup, so the
// tags are removed HERE rather than at the client: doing it once at the edge
// means the browser, the future mobile app and every test see the same string,
// and no consumer is ever handed markup it might be tempted to render.
//
// This is a display transform, not a security boundary — the client must still
// treat the result as text and never as HTML.
func plainText(s string) string {
	if s == "" {
		return ""
	}

	var out strings.Builder
	out.Grow(len(s))

	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
			// A tag is a boundary between words: dropping it silently would
			// glue "line one<br>line two" into "line oneline two".
			out.WriteRune(' ')
		case r == '>':
			inTag = false
		case inTag:
			// Skipped — part of the tag.
		default:
			out.WriteRune(r)
		}
	}

	// Entities are unescaped after tag removal, so an escaped `&lt;b&gt;` in the
	// original text becomes visible characters instead of being treated as a tag.
	return collapseSpace(html.UnescapeString(out.String()))
}

// collapseSpace trims the string and reduces every run of whitespace — including
// the newlines Google embeds — to a single space, so a description renders as
// one preview line rather than a ragged block.
func collapseSpace(s string) string {
	return strings.Join(strings.FieldsFunc(s, unicode.IsSpace), " ")
}
