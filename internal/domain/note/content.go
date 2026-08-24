package note

import (
	"encoding/json"
	"fmt"
)

// allowedNodes is every ProseMirror node type the server accepts in a note
// document. It is intentionally a positive list, not a denylist: a client bug
// or a future editor change that emits an unrecognized node type must be
// rejected, not silently persisted or silently stripped (the E-053 blank-note
// bug class this guards against).
var allowedNodes = map[string]bool{
	// StarterKit base (E-053).
	"doc": true, "text": true, "paragraph": true, "heading": true,
	"bulletList": true, "orderedList": true, "listItem": true,
	"blockquote": true, "codeBlock": true, "hardBreak": true,
	"image": true,

	// Notes v2 (E-057 / NIC-1964).
	"table": true, "tableRow": true, "tableCell": true, "tableHeader": true,
	"callout": true, "divider": true, "horizontalRule": true,
	"toggle": true, "details": true, "detailsSummary": true, "detailsContent": true,
	"dateMention": true, "noteMention": true,
	"taskList": true, "taskItem": true,
}

// allowedMarks is every ProseMirror mark type the server accepts.
var allowedMarks = map[string]bool{
	"bold": true, "italic": true, "strike": true, "code": true, "link": true,
	"textColor": true, "highlight": true,
}

// swatchTokens are the only values accepted for a textColor/highlight mark's
// color attr. Fixed tokens, never arbitrary hex — an unrestricted color value
// is an open-ended injection surface into whatever renders it downstream.
var swatchTokens = map[string]bool{
	"default": true, "gray": true, "brown": true, "orange": true, "yellow": true,
	"green": true, "blue": true, "purple": true, "pink": true, "red": true,
}

// markNode is the subset of a ProseMirror mark this validator reads.
type markNode struct {
	Type  string          `json:"type"`
	Attrs json.RawMessage `json:"attrs"`
}

// contentNode is the subset of a ProseMirror node this validator reads. Attrs
// is decoded lazily per node type — most node types carry attrs the validator
// doesn't care about, so it is captured raw rather than typed.
type contentNode struct {
	Type    string            `json:"type"`
	Attrs   json.RawMessage   `json:"attrs"`
	Marks   []markNode        `json:"marks"`
	Content []json.RawMessage `json:"content"`
}

// colorAttrs is the attrs shape read for textColor/highlight marks.
type colorAttrs struct {
	Color string `json:"color"`
}

// validateNodeTree walks a Tiptap/ProseMirror document and rejects the first
// node or mark type outside the fixed allowlist, or a textColor/highlight mark
// carrying a color outside the fixed swatch tokens. Unlike flattenDoc (which
// must never fail a save), this IS the save's security boundary: a document
// containing a node type the server doesn't recognize must never reach the
// database, because a later reader (search excerpt, any future export) may
// treat an unknown shape as trusted.
//
// depth mirrors maxFlattenDepth's stack-exhaustion guard.
func validateNodeTree(raw json.RawMessage, depth int) error {
	if depth > maxFlattenDepth {
		return invalid("content is nested too deeply")
	}

	var n contentNode
	if err := json.Unmarshal(raw, &n); err != nil {
		return invalid("content contains a malformed node")
	}
	// A bare string/number/array (no "type" object) — treated as opaque and
	// skipped rather than rejected; only object nodes carry a type to check.
	if n.Type == "" {
		return nil
	}

	if !allowedNodes[n.Type] {
		return invalid(fmt.Sprintf("unrecognized content node type %q", n.Type))
	}

	for _, m := range n.Marks {
		if !allowedMarks[m.Type] {
			return invalid(fmt.Sprintf("unrecognized content mark type %q", m.Type))
		}
		if m.Type == "textColor" || m.Type == "highlight" {
			var ca colorAttrs
			if len(m.Attrs) > 0 {
				if err := json.Unmarshal(m.Attrs, &ca); err != nil {
					return invalid(fmt.Sprintf("mark %q has malformed attrs", m.Type))
				}
			}
			if ca.Color != "" && !swatchTokens[ca.Color] {
				return invalid(fmt.Sprintf("mark %q has an unrecognized color %q", m.Type, ca.Color))
			}
		}
	}

	for _, child := range n.Content {
		if err := validateNodeTree(child, depth+1); err != nil {
			return err
		}
	}
	return nil
}
