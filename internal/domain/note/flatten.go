package note

import (
	"encoding/json"
	"strings"
)

// maxFlattenDepth bounds the recursive walk. A note body arrives as untrusted
// client JSON, and nesting is otherwise unbounded — a doc nested a few hundred
// thousand levels deep would exhaust the goroutine stack and take the process
// with it. Real editor content nests in the low tens (list › item › paragraph ›
// text), so this is far above any legitimate document while still finite.
const maxFlattenDepth = 100

// docNode is the only part of the Tiptap/ProseMirror shape the server reads.
// Marks, attrs and every other field are deliberately absent: the walk is
// schema-agnostic, so a new node type added on the client needs no server
// change. Content is []json.RawMessage rather than []docNode so one malformed
// child cannot fail the decode of its siblings.
type docNode struct {
	Text    string            `json:"text"`
	Content []json.RawMessage `json:"content"`
}

// flattenDoc walks a Tiptap/ProseMirror document and returns its plain text —
// the source of truth for note search and excerpts.
//
// The mirror is mandatory, not an optimization: extracting text from arbitrary
// JSONB is not IMMUTABLE, so Postgres cannot generate the tsvector directly from
// the content column (see migration 044).
//
// Text leaves are joined with a single space so words from adjacent blocks never
// fuse into one token ("alpha" + "beta" must not become "alphabeta", which would
// match neither). Malformed or non-object JSON yields whatever was parsed before
// the break — an empty string in the common case — and never an error: the write
// path validates document shape separately, and a save must not fail because the
// search mirror could not be derived.
func flattenDoc(doc json.RawMessage) string {
	var sb strings.Builder
	appendText(&sb, doc, 0)
	return strings.TrimSpace(sb.String())
}

// appendText walks one node, appending its text leaves in document order. It
// separates every leaf with a space rather than tracking which nodes are
// block-level: that would require the ProseMirror schema server-side, and an
// extra space between two inline runs is harmless to a tsvector, whereas a
// missing one between two blocks corrupts the token.
func appendText(sb *strings.Builder, raw json.RawMessage, depth int) {
	if depth > maxFlattenDepth {
		return
	}

	var n docNode
	if err := json.Unmarshal(raw, &n); err != nil {
		// Malformed or non-object (a bare string, number, array…) — nothing to
		// extract here. Siblings are unaffected; each is decoded on its own.
		return
	}

	// A node may carry both text and content; take the text first so document
	// order is preserved.
	if n.Text != "" {
		if sb.Len() > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(n.Text)
	}

	// Unknown node types are traversed for their children, never evaluated.
	for _, child := range n.Content {
		appendText(sb, child, depth+1)
	}
}
