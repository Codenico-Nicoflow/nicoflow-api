package ai

import (
	"encoding/json"
	"testing"
)

// TestNoteBlock_RoundTrip proves bulletList (Items) and taskList (Tasks) never
// collide on the wire despite both carrying a plural "list of things" shape —
// the bug this file guards against is two Go fields sharing one JSON tag.
func TestNoteBlock_RoundTrip(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want NoteBlock
	}{
		{
			name: "bulletList",
			in:   `{"kind":"bulletList","items":["milk","eggs"]}`,
			want: NoteBlock{Kind: NoteBlockBulletList, Items: []string{"milk", "eggs"}},
		},
		{
			name: "orderedList",
			in:   `{"kind":"orderedList","items":["first","second"]}`,
			want: NoteBlock{Kind: NoteBlockOrderedList, Items: []string{"first", "second"}},
		},
		{
			name: "taskList",
			in:   `{"kind":"taskList","tasks":[{"text":"call mom","checked":true},{"text":"buy milk"}]}`,
			want: NoteBlock{Kind: NoteBlockTaskList, Tasks: []NoteTaskItem{
				{Text: "call mom", Checked: true},
				{Text: "buy milk"},
			}},
		},
		{
			name: "paragraph",
			in:   `{"kind":"paragraph","text":"hello world"}`,
			want: NoteBlock{Kind: NoteBlockParagraph, Text: "hello world"},
		},
		{
			name: "heading",
			in:   `{"kind":"heading","text":"Title","level":2}`,
			want: NoteBlock{Kind: NoteBlockHeading, Text: "Title", Level: 2},
		},
		{
			name: "codeBlock",
			in:   `{"kind":"codeBlock","code":"fmt.Println()","language":"go"}`,
			want: NoteBlock{Kind: NoteBlockCodeBlock, Code: "fmt.Println()", Lang: "go"},
		},
		{
			name: "callout",
			in:   `{"kind":"callout","text":"heads up","variant":"warn"}`,
			want: NoteBlock{Kind: NoteBlockCallout, Text: "heads up", Variant: "warn"},
		},
		{
			name: "table",
			in:   `{"kind":"table","header":["a","b"],"rows":[["1","2"]]}`,
			want: NoteBlock{Kind: NoteBlockTable, Header: []string{"a", "b"}, Rows: [][]string{{"1", "2"}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got NoteBlock
			if err := json.Unmarshal([]byte(tt.in), &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got.Kind != tt.want.Kind || got.Text != tt.want.Text || got.Level != tt.want.Level ||
				len(got.Items) != len(tt.want.Items) || len(got.Tasks) != len(tt.want.Tasks) ||
				got.Code != tt.want.Code || got.Lang != tt.want.Lang || got.Variant != tt.want.Variant {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}

			// Round-trip: marshal back out and re-decode, must match.
			raw, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var again NoteBlock
			if err := json.Unmarshal(raw, &again); err != nil {
				t.Fatalf("re-unmarshal: %v", err)
			}
			if again.Kind != got.Kind {
				t.Errorf("round-trip kind = %q, want %q", again.Kind, got.Kind)
			}
		})
	}
}

// TestNoteBlock_ItemsAndTasksNeverCollide is the direct regression test for the
// bug this file's design note calls out: a bulletList's items and a taskList's
// tasks must decode into their own distinct fields, never overwrite each other.
func TestNoteBlock_ItemsAndTasksNeverCollide(t *testing.T) {
	var bullets NoteBlock
	if err := json.Unmarshal([]byte(`{"kind":"bulletList","items":["a","b"]}`), &bullets); err != nil {
		t.Fatalf("unmarshal bulletList: %v", err)
	}
	if len(bullets.Tasks) != 0 {
		t.Errorf("bulletList decoded into Tasks: %+v", bullets.Tasks)
	}
	if len(bullets.Items) != 2 {
		t.Errorf("bulletList Items = %v, want 2 entries", bullets.Items)
	}

	var tasks NoteBlock
	if err := json.Unmarshal([]byte(`{"kind":"taskList","tasks":[{"text":"x"}]}`), &tasks); err != nil {
		t.Fatalf("unmarshal taskList: %v", err)
	}
	if len(tasks.Items) != 0 {
		t.Errorf("taskList decoded into Items: %v", tasks.Items)
	}
	if len(tasks.Tasks) != 1 {
		t.Errorf("taskList Tasks = %v, want 1 entry", tasks.Tasks)
	}
}

func TestNoteBlock_Validate(t *testing.T) {
	tests := []struct {
		name    string
		block   NoteBlock
		wantErr bool
	}{
		{"valid paragraph", NoteBlock{Kind: NoteBlockParagraph, Text: "hi"}, false},
		{"empty paragraph", NoteBlock{Kind: NoteBlockParagraph}, true},
		{"heading missing level", NoteBlock{Kind: NoteBlockHeading, Text: "t", Level: 0}, true},
		{"heading level out of range", NoteBlock{Kind: NoteBlockHeading, Text: "t", Level: 7}, true},
		{"valid heading", NoteBlock{Kind: NoteBlockHeading, Text: "t", Level: 1}, false},
		{"empty bulletList", NoteBlock{Kind: NoteBlockBulletList}, true},
		{"valid bulletList", NoteBlock{Kind: NoteBlockBulletList, Items: []string{"x"}}, false},
		{"empty taskList", NoteBlock{Kind: NoteBlockTaskList}, true},
		{"valid taskList", NoteBlock{Kind: NoteBlockTaskList, Tasks: []NoteTaskItem{{Text: "x"}}}, false},
		{"empty codeBlock", NoteBlock{Kind: NoteBlockCodeBlock}, true},
		{"callout bad variant", NoteBlock{Kind: NoteBlockCallout, Text: "t", Variant: "nope"}, true},
		{"valid callout", NoteBlock{Kind: NoteBlockCallout, Text: "t", Variant: "info"}, false},
		{"empty table", NoteBlock{Kind: NoteBlockTable}, true},
		{"unknown kind", NoteBlock{Kind: "bogus"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.block.Validate()
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// TestBlocksToProseMirror_NodeShapes proves every block kind converts to the
// exact ProseMirror node type/attrs the frontend's Tiptap schema and the
// backend's note/content.go allowlist both expect.
func TestBlocksToProseMirror_NodeShapes(t *testing.T) {
	blocks := []NoteBlock{
		{Kind: NoteBlockParagraph, Text: "hello"},
		{Kind: NoteBlockHeading, Text: "Title", Level: 2},
		{Kind: NoteBlockBulletList, Items: []string{"a", "b"}},
		{Kind: NoteBlockOrderedList, Items: []string{"1", "2"}},
		{Kind: NoteBlockTaskList, Tasks: []NoteTaskItem{{Text: "x", Checked: true}}},
		{Kind: NoteBlockBlockquote, Text: "quoted"},
		{Kind: NoteBlockCodeBlock, Code: "x := 1", Lang: "go"},
		{Kind: NoteBlockCallout, Text: "watch out", Variant: "warn"},
		{Kind: NoteBlockTable, Header: []string{"h1"}, Rows: [][]string{{"r1"}}},
	}
	raw, err := blocksToProseMirror(blocks)
	if err != nil {
		t.Fatalf("blocksToProseMirror: %v", err)
	}

	var doc struct {
		Type    string           `json:"type"`
		Content []map[string]any `json:"content"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if doc.Type != "doc" {
		t.Fatalf("top-level type = %q, want doc", doc.Type)
	}
	if len(doc.Content) != len(blocks) {
		t.Fatalf("content len = %d, want %d", len(doc.Content), len(blocks))
	}

	wantTypes := []string{
		"paragraph", "heading", "bulletList", "orderedList", "taskList",
		"blockquote", "codeBlock", "callout", "table",
	}
	for i, want := range wantTypes {
		if got := doc.Content[i]["type"]; got != want {
			t.Errorf("block %d type = %v, want %q", i, got, want)
		}
	}

	heading := doc.Content[1]
	attrs, _ := heading["attrs"].(map[string]any)
	if attrs["level"] != float64(2) {
		t.Errorf("heading level attr = %v, want 2", attrs["level"])
	}

	callout := doc.Content[7]
	cAttrs, _ := callout["attrs"].(map[string]any)
	if cAttrs["icon"] != "warning" || cAttrs["colorToken"] != "yellow" {
		t.Errorf("callout attrs = %+v, want icon=warning colorToken=yellow", cAttrs)
	}

	taskList := doc.Content[4]
	tlContent, _ := taskList["content"].([]any)
	if len(tlContent) != 1 {
		t.Fatalf("taskList content len = %d, want 1", len(tlContent))
	}
	taskItem, _ := tlContent[0].(map[string]any)
	if taskItem["type"] != "taskItem" {
		t.Errorf("taskItem type = %v, want taskItem", taskItem["type"])
	}
	tiAttrs, _ := taskItem["attrs"].(map[string]any)
	if tiAttrs["checked"] != true {
		t.Errorf("taskItem checked attr = %v, want true", tiAttrs["checked"])
	}
}

func TestBlocksToProseMirror_InvalidBlockFails(t *testing.T) {
	_, err := blocksToProseMirror([]NoteBlock{{Kind: NoteBlockParagraph}})
	if err == nil {
		t.Fatal("expected error for empty paragraph, got nil")
	}
}
