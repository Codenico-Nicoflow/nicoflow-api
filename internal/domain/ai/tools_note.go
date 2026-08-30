package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
)

// NoteBlockKind is the discriminant of a create_note / process_bucket_item
// (note) block. Mirrors the subset of the Tiptap/ProseMirror schema the AI
// tools can author — no image node (deliberately excluded).
type NoteBlockKind string

const (
	NoteBlockParagraph   NoteBlockKind = "paragraph"
	NoteBlockHeading     NoteBlockKind = "heading"
	NoteBlockBulletList  NoteBlockKind = "bulletList"
	NoteBlockOrderedList NoteBlockKind = "orderedList"
	NoteBlockTaskList    NoteBlockKind = "taskList"
	NoteBlockBlockquote  NoteBlockKind = "blockquote"
	NoteBlockCodeBlock   NoteBlockKind = "codeBlock"
	NoteBlockCallout     NoteBlockKind = "callout"
	NoteBlockTable       NoteBlockKind = "table"
)

// noteCalloutNodeName is the Tiptap custom node's registered type. Confirmed
// against nicoflow-frontend/src/features/Notes/editor/CalloutNode.tsx — the
// node comment there pins it to "callout" because the backend's own content
// allowlist (internal/domain/note/content.go allowedNodes) requires that exact
// name. Its real attrs are `icon` (NOTE_CALLOUT_ICONS: info|warning|success|
// idea|star|note|flag|question) and `colorToken` (the same swatchTokens
// content.go already validates: default|gray|brown|orange|yellow|green|blue|
// purple|pink|red) — there is no `variant` attr in the real schema.
//
// The tool's own input vocabulary stays the simpler 4-value `variant` enum
// (info/warn/success/danger) since that's what an LLM proposing a callout
// reasons in; calloutVariant maps each onto a concrete (icon, colorToken) pair
// at conversion time.
const noteCalloutNodeName = "callout"

var allowedCalloutVariants = map[string]bool{"info": true, "warn": true, "success": true, "danger": true}

// calloutIconByVariant / calloutColorByVariant translate the tool's variant
// enum into the real node attrs.
var (
	calloutIconByVariant = map[string]string{
		"info": "info", "warn": "warning", "success": "success", "danger": "flag",
	}
	calloutColorByVariant = map[string]string{
		"info": "blue", "warn": "yellow", "success": "green", "danger": "red",
	}
)

// NoteTaskItem is one line of a taskList block.
type NoteTaskItem struct {
	Text    string `json:"text"`
	Checked bool   `json:"checked,omitempty"`
}

// NoteBlock is one block of a create_note / process_bucket_item(note) document.
// Only the fields relevant to Kind are populated. Custom (Un)MarshalJSON keeps
// the plain-list-items field (Items, bulletList/orderedList) and the
// task-list-items field (Tasks, taskList) from ever colliding on one JSON key —
// two Go struct fields sharing a json tag is undefined/wrong under
// encoding/json, so they get distinct wire names instead, decoded through a
// Kind-discriminated intermediate (the same shape UpdateRuleRequest's
// optional.Field pattern uses for a tri-state field, generalized here to a
// whole-struct discriminated union).
type NoteBlock struct {
	Kind    NoteBlockKind
	Text    string         // paragraph, heading, blockquote, callout
	Level   int            // heading (1..6)
	Items   []string       // bulletList, orderedList
	Tasks   []NoteTaskItem // taskList
	Code    string         // codeBlock
	Lang    string         // codeBlock (optional)
	Variant string         // callout (info|warn|success|danger)
	Header  []string       // table (optional)
	Rows    [][]string     // table
}

// noteBlockWire is the on-the-wire shape for one block, keyed by kind. Field
// names deliberately differ (items vs tasks) so unmarshal is unambiguous.
type noteBlockWire struct {
	Kind    NoteBlockKind  `json:"kind"`
	Text    string         `json:"text,omitempty"`
	Level   int            `json:"level,omitempty"`
	Items   []string       `json:"items,omitempty"`
	Tasks   []NoteTaskItem `json:"tasks,omitempty"`
	Code    string         `json:"code,omitempty"`
	Lang    string         `json:"language,omitempty"`
	Variant string         `json:"variant,omitempty"`
	Header  []string       `json:"header,omitempty"`
	Rows    [][]string     `json:"rows,omitempty"`
}

// UnmarshalJSON decodes one block via its kind. bytes.NewReader mirrors nothing
// special here — plain json.Unmarshal into the wire shape already disambiguates
// items vs tasks by field name, so no two-pass Kind-then-Raw decode is needed.
func (b *NoteBlock) UnmarshalJSON(data []byte) error {
	var w noteBlockWire
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&w); err != nil {
		return fmt.Errorf("decode note block: %w", err)
	}
	*b = NoteBlock(w)
	return nil
}

// MarshalJSON is the mirror of UnmarshalJSON, for round-trip tests and any
// future re-serialization (e.g. logging a proposal's input).
func (b NoteBlock) MarshalJSON() ([]byte, error) {
	return json.Marshal(noteBlockWire(b))
}

// Validate checks the fields Kind actually requires, before any conversion.
// Split into one helper per kind purely to keep cyclomatic complexity down —
// the dispatch switch itself carries no branching logic.
func (b NoteBlock) Validate() error {
	switch b.Kind {
	case NoteBlockParagraph, NoteBlockBlockquote:
		return b.validateTextOnly()
	case NoteBlockHeading:
		return b.validateHeading()
	case NoteBlockBulletList, NoteBlockOrderedList:
		return b.validateItems()
	case NoteBlockTaskList:
		return b.validateTaskList()
	case NoteBlockCodeBlock:
		return b.validateCodeBlock()
	case NoteBlockCallout:
		return b.validateCallout()
	case NoteBlockTable:
		return b.validateTable()
	default:
		return invalidNoteBlock("unknown block kind: " + string(b.Kind))
	}
}

func (b NoteBlock) validateTextOnly() error {
	if b.Text == "" {
		return invalidNoteBlock(string(b.Kind) + " requires text")
	}
	return nil
}

func (b NoteBlock) validateHeading() error {
	if b.Text == "" {
		return invalidNoteBlock("heading requires text")
	}
	if b.Level < 1 || b.Level > 6 {
		return invalidNoteBlock("heading level must be 1..6")
	}
	return nil
}

func (b NoteBlock) validateItems() error {
	if len(b.Items) == 0 {
		return invalidNoteBlock(string(b.Kind) + " requires at least one item")
	}
	return nil
}

func (b NoteBlock) validateTaskList() error {
	if len(b.Tasks) == 0 {
		return invalidNoteBlock("taskList requires at least one task")
	}
	return nil
}

func (b NoteBlock) validateCodeBlock() error {
	if b.Code == "" {
		return invalidNoteBlock("codeBlock requires code")
	}
	return nil
}

func (b NoteBlock) validateCallout() error {
	if b.Text == "" {
		return invalidNoteBlock("callout requires text")
	}
	if !allowedCalloutVariants[b.Variant] {
		return invalidNoteBlock("callout variant must be one of: info, warn, success, danger")
	}
	return nil
}

func (b NoteBlock) validateTable() error {
	if len(b.Rows) == 0 {
		return invalidNoteBlock("table requires at least one row")
	}
	return nil
}

func invalidNoteBlock(msg string) error {
	return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "invalid note block: "+msg)
}

// ── ProseMirror conversion ──────────────────────────────────────────────────

// pmNode is a generic ProseMirror node — deliberately map[string]any (not a
// bare `any` field on a domain struct) purely as the wire-serialization shape
// for a document tree the frontend's Tiptap schema owns; every value here is
// JSON-marshaled straight through, mirroring the codebase's existing
// TaskViewJSON{Value any} JSON-round-trip pattern rather than duplicating
// Tiptap's node types in Go.
type pmNode map[string]any

// blocksToProseMirror converts validated blocks into a Tiptap/ProseMirror
// document: {"type":"doc","content":[...]}.
func blocksToProseMirror(blocks []NoteBlock) (json.RawMessage, error) {
	content := make([]pmNode, 0, len(blocks))
	for i, b := range blocks {
		if err := b.Validate(); err != nil {
			return nil, fmt.Errorf("block %d: %w", i, err)
		}
		node, err := blockToNode(b)
		if err != nil {
			return nil, fmt.Errorf("block %d: %w", i, err)
		}
		content = append(content, node)
	}
	doc := pmNode{"type": "doc", "content": content}
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("marshal prosemirror doc: %w", err)
	}
	return raw, nil
}

func textNode(text string) pmNode {
	return pmNode{"type": "text", "text": text}
}

func paragraphNode(text string) pmNode {
	p := pmNode{"type": "paragraph"}
	if text != "" {
		p["content"] = []pmNode{textNode(text)}
	}
	return p
}

func blockToNode(b NoteBlock) (pmNode, error) {
	switch b.Kind {
	case NoteBlockParagraph:
		return paragraphNode(b.Text), nil
	case NoteBlockHeading:
		return pmNode{
			"type":    "heading",
			"attrs":   pmNode{"level": b.Level},
			"content": []pmNode{textNode(b.Text)},
		}, nil
	case NoteBlockBulletList:
		return listNode("bulletList", plainListItems(b.Items)), nil
	case NoteBlockOrderedList:
		return listNode("orderedList", plainListItems(b.Items)), nil
	case NoteBlockTaskList:
		return listNode("taskList", taskListItems(b.Tasks)), nil
	case NoteBlockBlockquote:
		return pmNode{"type": "blockquote", "content": []pmNode{paragraphNode(b.Text)}}, nil
	case NoteBlockCodeBlock:
		attrs := pmNode{}
		if b.Lang != "" {
			attrs["language"] = b.Lang
		}
		return pmNode{
			"type":    "codeBlock",
			"attrs":   attrs,
			"content": []pmNode{textNode(b.Code)},
		}, nil
	case NoteBlockCallout:
		return pmNode{
			"type": noteCalloutNodeName,
			"attrs": pmNode{
				"icon":       calloutIconByVariant[b.Variant],
				"colorToken": calloutColorByVariant[b.Variant],
			},
			"content": []pmNode{paragraphNode(b.Text)},
		}, nil
	case NoteBlockTable:
		return tableNode(b.Header, b.Rows), nil
	default:
		return nil, invalidNoteBlock("unknown block kind: " + string(b.Kind))
	}
}

func listNode(kind string, items []pmNode) pmNode {
	return pmNode{"type": kind, "content": items}
}

func plainListItems(items []string) []pmNode {
	out := make([]pmNode, len(items))
	for i, s := range items {
		out[i] = pmNode{"type": "listItem", "content": []pmNode{paragraphNode(s)}}
	}
	return out
}

func taskListItems(items []NoteTaskItem) []pmNode {
	out := make([]pmNode, len(items))
	for i, it := range items {
		out[i] = pmNode{
			"type":    "taskItem",
			"attrs":   pmNode{"checked": it.Checked},
			"content": []pmNode{paragraphNode(it.Text)},
		}
	}
	return out
}

func tableNode(header []string, rows [][]string) pmNode {
	trs := make([]pmNode, 0, len(rows)+1)
	if len(header) > 0 {
		trs = append(trs, tableRowNode(header, "tableHeader"))
	}
	for _, row := range rows {
		trs = append(trs, tableRowNode(row, "tableCell"))
	}
	return pmNode{"type": "table", "content": trs}
}

func tableRowNode(cells []string, cellType string) pmNode {
	tds := make([]pmNode, len(cells))
	for i, c := range cells {
		tds[i] = pmNode{"type": cellType, "content": []pmNode{paragraphNode(c)}}
	}
	return pmNode{"type": "tableRow", "content": tds}
}
