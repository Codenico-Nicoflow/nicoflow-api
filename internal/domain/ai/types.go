// Package ai contains the AI assistant domain.
package ai

import (
	"context"
	"encoding/json"
	"time"
)

// Session is a persisted AI conversation. status/title default in migration 009.
type Session struct {
	ID        string
	UserID    string
	Title     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// SessionMessage is one persisted turn of a conversation (ai_messages, mig 010).
// Distinct from the streaming Message in client.go, which never touches the DB.
//
// ContentJSON is the tool-aware companion column (migration 046): when set,
// it holds the ordered blocks (text + tool_use for assistant, tool_result for
// user) exactly as Claude sees them. Nil ⇒ pre-tool-loop turn where Content
// alone is the whole message.
type SessionMessage struct {
	ID          string
	SessionID   string
	Role        string
	Content     string
	ContentJSON json.RawMessage
	CreatedAt   time.Time
}

// SessionView is the wire shape for a session. IDs are application-generated strings.
type SessionView struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// MessageView is the wire shape for a persisted message.
type MessageView struct {
	ID        string    `json:"id"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"createdAt"`
}

// SessionDetailView is a session plus its messages (GET /ai/sessions/:id).
type SessionDetailView struct {
	SessionView
	Messages []MessageView `json:"messages"`
	// MessagesCursor seeds GET /ai/sessions/:id/messages for "load older history" —
	// it is the cursor that fetches the page immediately before Messages. Empty
	// when Messages already covers the whole session.
	MessagesCursor string `json:"messagesCursor"`
}

// UsageView is the quota-state read (GET /ai/usage). For Free plans scope is
// "lifetime" and month is nil; for Pro it is "month" and month is "YYYY-MM".
type UsageView struct {
	Used  int     `json:"used"`
	Limit int     `json:"limit"`
	Scope string  `json:"scope"`
	Month *string `json:"month"`
}

// ListMessagesFilter holds pagination params for the session message list.
type ListMessagesFilter struct {
	Cursor string
	Limit  int
}

// MessageListView is the paginated list response for GET /ai/sessions/:id/messages.
// Items are ordered ASC (oldest first) so the client can render a chat thread without
// reversing. The cursor points to the oldest message of the next (older) page —
// "load more history" semantics, identical to how most chat clients work.
type MessageListView struct {
	Items      []MessageView `json:"items"`
	NextCursor string        `json:"nextCursor"`
}

// CreateSessionRequest is the POST /ai/sessions body. Title is optional;
// empty falls back to the DB default.
type CreateSessionRequest struct {
	Title string `json:"title"`
}

// SendMessageRequest is the POST /ai/sessions/:id/messages body. Content is
// trimmed then length-validated (1..2000) before any provider call.
type SendMessageRequest struct {
	Content string `json:"content"`
}

// PromptContext is the volatile, per-user tail of the system prompt.
type PromptContext struct {
	Language  string
	OpenTasks int
	// Timezone is the user's IANA zone (users.timezone, default "UTC"). "Today"
	// in the prompt must be the user's local calendar date, not the server's —
	// a user east of UTC has already crossed midnight while the server hasn't.
	Timezone string
}

// SSE event payloads streamed over the response body (type-discriminated).
type (
	// deltaEvent carries one text delta.
	deltaEvent struct {
		Type string `json:"type"` // "delta"
		Text string `json:"text"`
	}
	// doneEvent terminates a successful stream with the persisted id + usage.
	doneEvent struct {
		Type      string    `json:"type"` // "done"
		MessageID string    `json:"messageId"`
		Usage     UsageView `json:"usage"`
	}
	// errorEvent terminates a mid-stream failure (HTTP status already committed).
	errorEvent struct {
		Type string `json:"type"` // "error"
		Code string `json:"code"`
	}
	// toolProposalEvent surfaces one pending write-tool proposal to the client
	// — the confirm/reject UI card is driven off this. Input is passed through
	// verbatim from the model.
	toolProposalEvent struct {
		Type               string          `json:"type"` // "tool_proposal"
		AssistantMessageID string          `json:"assistantMessageId"`
		ToolUseID          string          `json:"toolUseId"`
		ToolName           string          `json:"toolName"`
		Input              json.RawMessage `json:"input"`
	}
)

// ── write-tool command seams (NIC-1997) ─────────────────────────────────────
//
// Each interface is the narrow slice of a domain service one or more new AI
// write tools call into. Defined here (the consumer) so ai never imports
// recurrence/note/project/area/task/bucket directly — the same JSON-carrier
// pattern as TaskCommands/ProjectCommands above. Every concrete adapter is
// wired post-construction in main.go via NewToolExecutor's With... options; a
// nil seam simply excludes its tools from DefaultTools() rather than erroring
// at call time (see toolExecutor.availableTools).

// RecurrenceCommands is the narrow slice of the recurrence service the
// setup/adjust/pause/end-series tools call into.
type RecurrenceCommands interface {
	Create(ctx context.Context, userID, projectID, plan string, req RuleCreateInput) (RuleViewJSON, error)
	ConvertToRecurring(ctx context.Context, userID, taskID, plan string, req RuleCreateInput) (RuleViewJSON, error)
	Update(ctx context.Context, userID, id, plan string, req RuleUpdateInput) (RuleViewJSON, error)
	SetPaused(ctx context.Context, userID, id, plan string, paused bool) (RuleViewJSON, error)
	Delete(ctx context.Context, userID, id string) error
}

// NoteService is the narrow slice of the note service create_note and the
// note branch of process_bucket_item call into.
type NoteService interface {
	Create(ctx context.Context, userID, projectID, title string, content json.RawMessage) (NoteViewJSON, error)
}

// ProjectService is the narrow slice of the project service create_project /
// update_project call into.
type ProjectService interface {
	Create(ctx context.Context, userID, areaID, plan string, req ProjectCreateInput) (ProjectViewJSON, error)
	Update(ctx context.Context, userID, id string, req ProjectUpdateInput) (ProjectViewJSON, error)
}

// AreaService is the narrow slice of the area service create_area calls into.
type AreaService interface {
	Create(ctx context.Context, userID, plan string, name, color, icon string) (AreaViewJSON, error)
}

// SubtaskService is the narrow slice of the subtask service add_subtask /
// complete_subtask call into.
type SubtaskService interface {
	Add(ctx context.Context, userID, taskID, title string) (SubtaskViewJSON, error)
	SetDone(ctx context.Context, userID, taskID, subtaskID string, done bool) (SubtaskViewJSON, error)
}

// BucketService is the narrow slice of the bucket service process_bucket_item
// calls into.
type BucketService interface {
	Process(ctx context.Context, userID, id, plan string, req BucketProcessInput) (BucketViewJSON, error)
}

// RuleViewJSON / NoteViewJSON / ProjectViewJSON / AreaViewJSON / SubtaskViewJSON
// / BucketViewJSON are opaque JSON-carrier handles, exactly like TaskViewJSON —
// the executor never inspects them, just re-marshals through json.Marshal.
type (
	RuleViewJSON    struct{ Value any }
	NoteViewJSON    struct{ Value any }
	ProjectViewJSON struct{ Value any }
	AreaViewJSON    struct{ Value any }
	SubtaskViewJSON struct{ Value any }
	BucketViewJSON  struct{ Value any }
)

// RuleCreateInput mirrors recurrence.CreateRuleRequest.
type RuleCreateInput struct {
	Title            string
	Notes            *string
	Priority         string
	Energy           string
	EstimatedMinutes *int
	ScheduledTime    *string
	Freq             string
	Interval         int
	ByWeekday        []int
	ByMonthday       *int
	StartDate        string
	EndDate          *string
}

// RuleUpdateInput mirrors recurrence.UpdateRuleRequest's tri-state optional
// fields via the Set/Value pair (mirrors optional.Field's shape without ai
// importing pkg/optional's generic — the adapter reconstructs the real type).
type RuleUpdateInput struct {
	Title            *string
	NotesSet         bool
	Notes            *string
	Priority         *string
	Energy           *string
	EstimatedMinsSet bool
	EstimatedMinutes *int
	ScheduledTimeSet bool
	ScheduledTime    *string
	Freq             *string
	Interval         *int
	ByWeekday        *[]int
	ByMonthdaySet    bool
	ByMonthday       *int
	StartDate        *string
	EndDateSet       bool
	EndDate          *string
}

// ProjectCreateInput mirrors project.CreateProjectRequest.
type ProjectCreateInput struct {
	Name        string
	FolderIcon  string
	Description *string
}

// ProjectUpdateInput mirrors project.UpdateProjectRequest.
type ProjectUpdateInput struct {
	Name        *string
	AreaID      *string
	FolderIcon  *string
	DescSet     bool
	Description *string
}

// BucketProcessInput mirrors bucket.ProcessBucketRequest for the task/note
// processing paths the process_bucket_item tool supports (trash is handled
// without either sub-detail).
type BucketProcessInput struct {
	ProcessingResult string
	ProjectID        *string
	TaskTitle        string
	TaskNotes        *string
	TaskPriority     *string
	TaskEnergy       *string
	TaskScheduledFor *string
	NoteTitle        string
	NoteContent      json.RawMessage
}

func sessionToView(s Session) SessionView {
	return SessionView{
		ID:        s.ID,
		Title:     s.Title,
		CreatedAt: s.CreatedAt,
		UpdatedAt: s.UpdatedAt,
	}
}

func messageToView(m SessionMessage) MessageView {
	return MessageView{
		ID:        m.ID,
		Role:      m.Role,
		Content:   m.Content,
		CreatedAt: m.CreatedAt,
	}
}
