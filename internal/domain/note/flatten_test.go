package note_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nicoflow/nicoflow-api/internal/domain/note"
)

func TestFlattenDoc(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		want string
	}{
		{
			name: "empty doc",
			doc:  `{"type":"doc","content":[]}`,
			want: "",
		},
		{
			name: "doc with no content key",
			doc:  `{"type":"doc"}`,
			want: "",
		},
		{
			name: "single paragraph",
			doc:  `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"hello"}]}]}`,
			want: "hello",
		},
		{
			// AC1 — heading, marked inline runs and a bullet list, all in order.
			name: "nested marks and lists keep document order",
			doc: `{"type":"doc","content":[
				{"type":"heading","attrs":{"level":1},"content":[{"type":"text","text":"Roadmap"}]},
				{"type":"paragraph","content":[
					{"type":"text","text":"ship"},
					{"type":"text","marks":[{"type":"bold"}],"text":"the"},
					{"type":"text","marks":[{"type":"italic"}],"text":"editor"}
				]},
				{"type":"bulletList","content":[
					{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"first"}]}]},
					{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"second"}]}]}
				]}
			]}`,
			want: "Roadmap ship the editor first second",
		},
		{
			// AC2 — the failure that matters: "alphabeta" would match neither word.
			name: "adjacent paragraphs do not fuse",
			doc: `{"type":"doc","content":[
				{"type":"paragraph","content":[{"type":"text","text":"alpha"}]},
				{"type":"paragraph","content":[{"type":"text","text":"beta"}]}
			]}`,
			want: "alpha beta",
		},
		{
			name: "text-less nodes contribute nothing",
			doc: `{"type":"doc","content":[
				{"type":"paragraph","content":[{"type":"text","text":"before"}]},
				{"type":"horizontalRule"},
				{"type":"image","attrs":{"src":"https://example.test/x.png"}},
				{"type":"paragraph","content":[{"type":"text","text":"after"}]}
			]}`,
			want: "before after",
		},
		{
			name: "node carrying both content and text",
			doc: `{"type":"doc","content":[
				{"type":"oddNode","text":"outer","content":[{"type":"text","text":"inner"}]}
			]}`,
			want: "outer inner",
		},
		{
			// AC3 — a node type the server has never seen still yields its children.
			name: "unknown node type is traversed",
			doc: `{"type":"doc","content":[
				{"type":"someFutureEmbed","attrs":{"id":42},"content":[
					{"type":"paragraph","content":[{"type":"text","text":"nested text"}]}
				]}
			]}`,
			want: "nested text",
		},
		{
			name: "table cell text is extracted",
			doc: `{"type":"doc","content":[{"type":"table","content":[
				{"type":"tableRow","content":[
					{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"cell one"}]}]},
					{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"cell two"}]}]}
				]}
			]}]}`,
			want: "cell one cell two",
		},
		{
			// AC4 — degenerate input is safe.
			name: "malformed JSON yields empty string",
			doc:  `{"type":"doc","content":[`,
			want: "",
		},
		{
			name: "non-object JSON yields empty string",
			doc:  `"just a string"`,
			want: "",
		},
		{
			name: "JSON null yields empty string",
			doc:  `null`,
			want: "",
		},
		{
			name: "empty input yields empty string",
			doc:  ``,
			want: "",
		},
		{
			// One bad child must not cost its siblings — each is decoded alone.
			name: "malformed child does not discard its siblings",
			doc: `{"type":"doc","content":[
				{"type":"paragraph","content":[{"type":"text","text":"kept"}]},
				"not an object",
				12345,
				{"type":"paragraph","content":[{"type":"text","text":"also kept"}]}
			]}`,
			want: "kept also kept",
		},
		{
			name: "wrong types on known fields are ignored",
			doc:  `{"type":"doc","text":123,"content":"not an array"}`,
			want: "",
		},
		{
			name: "empty text leaves add no separator",
			doc: `{"type":"doc","content":[
				{"type":"text","text":""},
				{"type":"text","text":"solo"}
			]}`,
			want: "solo",
		},
		{
			name: "multi-byte text survives intact",
			doc: `{"type":"doc","content":[
				{"type":"paragraph","content":[{"type":"text","text":"שלום"}]},
				{"type":"paragraph","content":[{"type":"text","text":"мир"}]}
			]}`,
			want: "שלום мир",
		},
		{
			name: "text already containing whitespace is preserved",
			doc:  `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"two  spaces"}]}]}`,
			want: "two  spaces",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := note.FlattenDocForTest(json.RawMessage(tt.doc))
			if got != tt.want {
				t.Errorf("flattenDoc() = %q, want %q", got, tt.want)
			}
		})
	}
}

// A hostile document must not take the process down with a stack overflow, and
// must not hang. Depth is bounded, so text past the limit is simply dropped.
func TestFlattenDocDepthGuard(t *testing.T) {
	const depth = 50_000
	doc := strings.Repeat(`{"type":"x","content":[`, depth) +
		`{"type":"text","text":"deep"}` +
		strings.Repeat(`]}`, depth)

	got := note.FlattenDocForTest(json.RawMessage(doc))

	if got != "" {
		t.Errorf("flattenDoc() = %q, want %q — text past the depth limit is dropped", got, "")
	}
}

// Text at a legitimate nesting depth is still reached — the guard bounds abuse,
// it does not truncate real documents.
func TestFlattenDocReachesRealisticDepth(t *testing.T) {
	const depth = 20
	doc := strings.Repeat(`{"type":"x","content":[`, depth) +
		`{"type":"text","text":"deep"}` +
		strings.Repeat(`]}`, depth)

	if got := note.FlattenDocForTest(json.RawMessage(doc)); got != "deep" {
		t.Errorf("flattenDoc() = %q, want %q", got, "deep")
	}
}

func FuzzFlattenDoc(f *testing.F) {
	f.Add(`{"type":"doc","content":[{"type":"text","text":"hi"}]}`)
	f.Add(`{"type":"doc"}`)
	f.Add(`{`)
	f.Add(`null`)

	// The contract is total: any input at all returns a string, never a panic.
	f.Fuzz(func(t *testing.T, doc string) {
		_ = note.FlattenDocForTest(json.RawMessage(doc))
	})
}
