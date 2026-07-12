// Package bucket contains the inbox (quick-capture) domain.
package bucket

import "time"

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
	ProcessedAt      *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// BucketView is the JSON response shape (IBucket) for a single item.
// CreatedNoteID is always null for now — note processing is not implemented,
// but the field is present so the frontend IBucket contract matches.
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
}

// ProcessTaskDetails is the narrow subset of task fields the process dialog sends.
// Energy/status/rollsOver/scheduledFor are intentionally omitted so the task
// service fills its own defaults. The mapping to task.CreateTaskRequest lives in
// the service layer (Story 3), where task creation is orchestrated.
type ProcessTaskDetails struct {
	Title            string  `json:"title"`
	Notes            *string `json:"notes"`
	Priority         *string `json:"priority"`
	EstimatedMinutes *int    `json:"estimatedMinutes"`
	URL              *string `json:"url"`
}

// BucketToView maps the domain model to its JSON response shape.
func BucketToView(b Bucket) BucketView {
	v := BucketView{
		ID:               b.ID,
		Content:          b.Content,
		ProcessingResult: b.ProcessingResult,
		ProjectID:        b.ProjectID,
		CreatedTaskID:    b.CreatedTaskID,
		CreatedNoteID:    nil,
		CreatedAt:        b.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:        b.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if b.ProcessedAt != nil {
		s := b.ProcessedAt.UTC().Format(time.RFC3339)
		v.ProcessedAt = &s
	}
	return v
}
