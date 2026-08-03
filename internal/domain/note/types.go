// Package note owns the persistence for project notes (E-053). A note is a
// rich-text document (ProseMirror-shaped JSONB) filed under a project. Concurrent
// edits are resolved optimistically: every write carries the version it was based
// on and the UPDATE is guarded by it, so a stale save is rejected by the database
// rather than silently overwriting a newer document.
package note

import (
	"context"
	"encoding/json"
	"time"
)

// Domain event types emitted on mutation. Equal to the wire names by convention;
// the ws adapter maps them through an explicit table (never a cast).
const (
	EventCreated = "note.created"
	EventUpdated = "note.updated"
	EventDeleted = "note.deleted"
)

// Field bounds. Title matches the VARCHAR(255) column; the content cap keeps a
// single document from becoming a denial-of-service payload.
const (
	MaxTitleLen   = 255
	MaxContentLen = 1 << 20 // 1 MiB of serialized JSON
)

// EmptyDoc is the empty ProseMirror document — the same value as the column
// default, so a note created without a body reads back identically whether the
// default or this constant supplied it.
const EmptyDoc = `{"type":"doc","content":[]}`

// Note is the internal domain model. ProjectID is a pointer because deleting a
// project orphans its notes (ON DELETE SET NULL) rather than destroying them;
// the API still requires a project on create.
//
// Content is the raw document; ContentText is its flattened plain text, kept
// alongside so the generated search vector has an IMMUTABLE source. The two are
// written together and never independently.
type Note struct {
	ID          string
	UserID      string
	ProjectID   *string
	Title       string
	Content     json.RawMessage
	ContentText string
	Version     int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// NoteView is the full API-facing shape, returned by the scalar read. All IDs are
// strings; instants are RFC3339 UTC.
type NoteView struct {
	ID        string          `json:"id"`
	ProjectID *string         `json:"projectId"`
	Title     string          `json:"title"`
	Content   json.RawMessage `json:"content"`
	Version   int             `json:"version"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

// NoteListItem is the list shape. It carries an excerpt derived from
// content_text instead of the full document: a project's notes can hold large
// JSONB bodies, and a list never renders them.
type NoteListItem struct {
	ID        string    `json:"id"`
	ProjectID *string   `json:"projectId"`
	Title     string    `json:"title"`
	Excerpt   string    `json:"excerpt"`
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ExcerptLen is how much flattened text a list item carries. Long enough to
// preview the opening line, short enough that a large list stays cheap.
const ExcerptLen = 200

// Excerpt truncates flattened note text to ExcerptLen, cutting on a rune
// boundary so a multi-byte character is never split into invalid UTF-8.
func Excerpt(text string) string {
	runes := []rune(text)
	if len(runes) <= ExcerptLen {
		return text
	}
	return string(runes[:ExcerptLen])
}

// UpdateParams is one guarded save. Version is the version the client last read;
// the UPDATE only applies if the row is still at it.
type UpdateParams struct {
	ID          string
	UserID      string
	Version     int
	Title       string
	Content     json.RawMessage
	ContentText string
}

// Repository is the data-access contract for notes. Every method is row-scoped by
// user_id — another user's note is invisible, never forbidden, so no query can
// become an existence oracle. Defined here (the consumer package) per project
// layering; the pg implementation lives in repository.go.
type Repository interface {
	// ListByProject returns a user's notes for one project, most recently updated
	// first. Selects content_text for the excerpt and never content.
	ListByProject(ctx context.Context, userID, projectID string) ([]Note, error)

	// GetByID returns one note scoped to the user, including its full content.
	// Missing or not-owned → RESOURCE_NOT_FOUND.
	GetByID(ctx context.Context, userID, id string) (Note, error)

	// Create inserts a note and returns the stored row.
	Create(ctx context.Context, n Note) (Note, error)

	// Update applies a version-guarded save and returns the new row. ok=false
	// means 0 rows matched — the note is missing, not owned, or the client's
	// version is stale. The service distinguishes the two with ExistsForUser so a
	// conflict is reported as 409 and a miss as 404.
	Update(ctx context.Context, p UpdateParams) (Note, bool, error)

	// Delete removes one note scoped to the user. ok=false when no row matched.
	Delete(ctx context.Context, userID, id string) (bool, error)

	// ExistsForUser reports whether the user owns the note. Backs the attachment
	// OwnerVerifier seam and disambiguates a failed guarded update.
	ExistsForUser(ctx context.Context, userID, noteID string) (bool, error)
}
