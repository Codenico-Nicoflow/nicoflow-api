// Package bucket contains the inbox (quick-capture) domain.
package bucket

import (
	"encoding/json"
	"time"

	"github.com/nicoflow/nicoflow-api/internal/domain/note"
	"github.com/nicoflow/nicoflow-api/internal/domain/recurrence"
	"github.com/nicoflow/nicoflow-api/internal/domain/task"
)

// Processing result values for a bucket item.
const (
	ResultTask  = "task"
	ResultNote  = "note"
	ResultTrash = "trash"
)

// Bucket is the internal domain model for an inbox item.
type Bucket struct {
	ID               string
	UserID           string
	Content          string
	ProcessingResult *string
	ProjectID        *string
	CreatedTaskID    *string
	CreatedNoteID    *string
	ProcessedAt      *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// BucketView is the JSON response shape (IBucket) for a single item.
// CreatedNoteID is set when the item was processed into a note (E-053), the
// mirror of CreatedTaskID — it is what lets the client link back to whatever the
// thought became.
type BucketView struct {
	ID               string  `json:"id"`
	Content          string  `json:"content"`
	ProcessingResult *string `json:"processingResult"`
	ProjectID        *string `json:"projectId"`
	CreatedTaskID    *string `json:"createdTaskId"`
	CreatedNoteID    *string `json:"createdNoteId"`
	ProcessedAt      *string `json:"processedAt"`
	CreatedAt        string  `json:"createdAt"`
	UpdatedAt        string  `json:"updatedAt"`
}

// BucketListResponse is the list response for a user's inbox.
type BucketListResponse struct {
	Items []BucketView `json:"items"`
}

// CreateBucketRequest is the body for POST /bucket.
type CreateBucketRequest struct {
	Content string `json:"content"`
}

// UpdateBucketRequest is the body for PATCH /bucket/:id (unprocessed items only).
type UpdateBucketRequest struct {
	Content string `json:"content"`
}

// ProcessBucketRequest is the body for POST /bucket/:id/process.
// ProcessingResult ∈ {task, note, trash}. ProjectID + TaskDetails are required
// when ProcessingResult == "task"; ignored otherwise.
type ProcessBucketRequest struct {
	ProcessingResult string              `json:"processingResult"`
	ProjectID        *string             `json:"projectId"`
	TaskDetails      *ProcessTaskDetails `json:"taskDetails"`
	NoteDetails      *ProcessNoteDetails `json:"noteDetails"`
}

// ProcessNoteDetails is the subset of note fields the process dialog sends.
// Nil Content means "use the note service default" (the empty doc), so an older
// client that omits it is unaffected.
type ProcessNoteDetails struct {
	Title   string           `json:"title"`
	Content *json.RawMessage `json:"content" swaggertype:"object"`
}

// toNoteCreateRequest maps the process details onto the note create contract.
// A nil Content falls through to the note service's empty-doc default rather
// than being forced to an explicit value here.
func (d ProcessNoteDetails) toNoteCreateRequest(projectID string) note.CreateNoteRequest {
	req := note.CreateNoteRequest{ProjectID: projectID, Title: d.Title}
	if d.Content != nil {
		req.Content = *d.Content
	}
	return req
}

// ProcessTaskDetails is the subset of task fields the process dialog sends.
// Status is intentionally omitted so the task service fills its own default;
// every other field the dialog offers is carried through. Nil = "use the task
// service default", so an older client that omits a field is unaffected.
//
// Recurrence is an optional add-on: when set, the item is processed into a
// recurrence rule (which materializes instance #1) instead of a plain task —
// see ProcessRecurrenceDetails.
type ProcessTaskDetails struct {
	Title            string                    `json:"title"`
	Notes            *string                   `json:"notes"`
	Priority         *string                   `json:"priority"`
	Energy           *string                   `json:"energy"`
	RollsOver        *bool                     `json:"rollsOver"`
	ScheduledFor     *string                   `json:"scheduledFor"`
	ScheduledTime    *string                   `json:"scheduledTime"`
	EstimatedMinutes *int                      `json:"estimatedMinutes"`
	URL              *string                   `json:"url"`
	Recurrence       *ProcessRecurrenceDetails `json:"recurrence"`
}

// ProcessRecurrenceDetails is the recurrence-rule shape the process dialog
// sends when the captured item should become a recurring task instead of a
// one-off. Mirrors recurrence.CreateRuleRequest's schedule fields exactly —
// the task-template fields (title/notes/priority/energy/estimatedMinutes/
// scheduledTime) are drawn from the surrounding ProcessTaskDetails instead of
// being duplicated here.
type ProcessRecurrenceDetails struct {
	Freq       string  `json:"freq"`
	Interval   int     `json:"interval"`
	ByWeekday  []int   `json:"byWeekday"`
	ByMonthday *int    `json:"byMonthday"`
	StartDate  string  `json:"startDate"`
	EndDate    *string `json:"endDate"`
}

// toTaskCreateRequest maps the process details onto the task create contract.
// Priority/Energy fall through to the task service defaults when nil, as does
// scheduledFor — which the task service validates as an ISO date.
func (d ProcessTaskDetails) toTaskCreateRequest() task.CreateTaskRequest {
	req := task.CreateTaskRequest{
		Title:            d.Title,
		Notes:            d.Notes,
		RollsOver:        d.RollsOver,
		ScheduledFor:     d.ScheduledFor,
		ScheduledTime:    d.ScheduledTime,
		EstimatedMinutes: d.EstimatedMinutes,
		URL:              d.URL,
	}
	if d.Priority != nil {
		req.Priority = *d.Priority
	}
	if d.Energy != nil {
		req.Energy = *d.Energy
	}
	return req
}

// toRuleCreateRequest maps the process details onto the recurrence create
// contract. Only called when d.Recurrence is non-nil. Priority/Energy fall
// through to the recurrence service defaults when nil, matching
// toTaskCreateRequest's convention.
func (d ProcessTaskDetails) toRuleCreateRequest() recurrence.CreateRuleRequest {
	req := recurrence.CreateRuleRequest{
		Title:            d.Title,
		Notes:            d.Notes,
		EstimatedMinutes: d.EstimatedMinutes,
		ScheduledTime:    d.ScheduledTime,
		Freq:             d.Recurrence.Freq,
		Interval:         d.Recurrence.Interval,
		ByWeekday:        d.Recurrence.ByWeekday,
		ByMonthday:       d.Recurrence.ByMonthday,
		StartDate:        d.Recurrence.StartDate,
		EndDate:          d.Recurrence.EndDate,
	}
	if d.Priority != nil {
		req.Priority = *d.Priority
	}
	if d.Energy != nil {
		req.Energy = *d.Energy
	}
	return req
}

// BucketToView maps the domain model to its JSON response shape.
func BucketToView(b Bucket) BucketView {
	v := BucketView{
		ID:               b.ID,
		Content:          b.Content,
		ProcessingResult: b.ProcessingResult,
		ProjectID:        b.ProjectID,
		CreatedTaskID:    b.CreatedTaskID,
		CreatedNoteID:    b.CreatedNoteID,
		CreatedAt:        b.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:        b.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if b.ProcessedAt != nil {
		s := b.ProcessedAt.UTC().Format(time.RFC3339)
		v.ProcessedAt = &s
	}
	return v
}

// ProcessedRefs are the entity ids a process operation produced. Grouped rather
// than passed as loose pointers so adding a result kind doesn't grow the
// repository signature again; exactly one of TaskID/NoteID is set (both nil for
// trash).
type ProcessedRefs struct {
	TaskID    *string
	NoteID    *string
	ProjectID *string
}
