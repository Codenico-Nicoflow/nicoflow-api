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

	"github.com/nicoflow/nicoflow-api/internal/domain/notelink"
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

// NoteDetailView is the scalar shape — the whole document. All IDs are strings;
// instants are RFC3339 UTC.
type NoteDetailView struct {
	ID        string          `json:"id"`
	ProjectID *string         `json:"projectId"`
	Title     string          `json:"title"`
	Content   json.RawMessage `json:"content" swaggertype:"object"`
	Version   int             `json:"version"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

// NoteView is the list shape. It carries an excerpt derived from content_text
// and deliberately has no Content field: a project's notes can hold large JSONB
// documents a list never renders, so shipping them would bloat every list
// response. Same principle as totalFocusSeconds being scalar-only (E-049).
// Never render a note body from a list response.
type NoteView struct {
	ID        string    `json:"id"`
	ProjectID *string   `json:"projectId"`
	Title     string    `json:"title"`
	Excerpt   string    `json:"excerpt"`
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func toDetailView(n Note) NoteDetailView {
	return NoteDetailView{
		ID: n.ID, ProjectID: n.ProjectID, Title: n.Title, Content: n.Content,
		Version: n.Version, CreatedAt: n.CreatedAt, UpdatedAt: n.UpdatedAt,
	}
}

func toView(n Note) NoteView {
	return NoteView{
		ID: n.ID, ProjectID: n.ProjectID, Title: n.Title, Excerpt: Excerpt(n.ContentText),
		Version: n.Version, CreatedAt: n.CreatedAt, UpdatedAt: n.UpdatedAt,
	}
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

// CreateNoteRequest is the body for POST /notes. Content is optional — omitted
// yields the empty-doc default. There is deliberately no contentText field: the
// mirror is always server-derived via flattenDoc, so the two columns cannot
// drift and a mobile client reimplements nothing.
type CreateNoteRequest struct {
	ProjectID string          `json:"projectId"`
	Title     string          `json:"title"`
	Content   json.RawMessage `json:"content" swaggertype:"object"`
}

// UpdateNoteRequest is the body for PATCH /notes/{id}. Version is required: it
// is the version the client last read, and the guarded UPDATE rejects the save
// if the row has moved on. Title and Content are pointers so an omitted field
// keeps its stored value.
type UpdateNoteRequest struct {
	Title   *string          `json:"title"`
	Content *json.RawMessage `json:"content" swaggertype:"object"`
	Version *int             `json:"version"`
}

// Broadcaster receives a domain Event for real-time fan-out. Fire-and-forget:
// implementations must never block or fail the mutation. A nil Broadcaster is a
// valid no-op seam.
type Broadcaster interface {
	Broadcast(userID string, ev Event)
}

// Event is the domain-level real-time event. The ws adapter maps Type onto the
// wire EventType.
type Event struct {
	Type    string
	Payload any
}

// DeletedPayload is the note.deleted event body — just enough for a client to
// drop the row, never stale document metadata.
type DeletedPayload struct {
	ID string `json:"id"`
}

// AttachmentCleaner removes a deleted owner's attachments. Defined here (the
// consumer) so the note package stays free of any attachment import; the
// concrete is the attachment service, injected at wire-up. Nil disables cleanup.
type AttachmentCleaner interface {
	DeleteAllForOwner(ctx context.Context, userID, ownerType, ownerID string) error
}

// ownerTypeNote is the polymorphic owner-type value notes register with the
// attachment domain. Kept local so the note package never imports attachment.
const ownerTypeNote = "note"

// ProjectOwnershipVerifier reports whether the caller owns a project. Defined in
// this (consumer) package so note imports no concrete project type; main.go
// supplies the adapter. A foreign or missing project must be reported as
// not-found, never forbidden — a 403 would confirm the project exists.
type ProjectOwnershipVerifier interface {
	VerifyProjectOwner(ctx context.Context, userID, projectID string) error
}

// BacklinkSource resolves and maintains the notes that mention a given note.
// notelink is a leaf package with no note import, so note may depend on its
// concrete Repository directly (it already satisfies this interface) — this
// alias exists so the Service constructor signature reads in domain terms
// rather than a cross-package repository type.
type BacklinkSource interface {
	GetBacklinks(ctx context.Context, noteID string) ([]notelink.BacklinkNote, error)
	ReplaceLinksForNote(ctx context.Context, sourceNoteID string, targetNoteIDs []string) error
}

// ListNotesFilter holds pagination params for the project note list.
type ListNotesFilter struct {
	Cursor string
	Limit  int
}

// mentionSearchLimit caps @-mention typeahead results. Small and fixed (no
// client-controlled limit param): a mention dropdown only ever renders a
// handful of rows, so there is nothing to page and no reason to let a caller
// ask for more.
const mentionSearchLimit = 10

// MentionResult is one row of an @-mention typeahead match — just enough to
// render and select a candidate. No excerpt: unlike the backlinks panel or
// full search, a mention dropdown shows titles only.
type MentionResult struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// NoteListView is the paginated list response for GET /notes?projectId=.
type NoteListView struct {
	Items      []NoteView `json:"items"`
	NextCursor string     `json:"nextCursor"`
}

// Service is the note domain's business-logic contract consumed by the handler.
// Notes are Free and unlimited — no method takes a plan, and none of them can
// return PLAN_LIMIT_EXCEEDED.
type Service interface {
	ListByProject(ctx context.Context, userID, projectID string, f ListNotesFilter) (NoteListView, error)
	Get(ctx context.Context, userID, id string) (NoteDetailView, error)
	Create(ctx context.Context, userID string, req CreateNoteRequest) (NoteDetailView, error)
	Update(ctx context.Context, userID, id string, req UpdateNoteRequest) (NoteDetailView, error)
	Delete(ctx context.Context, userID, id string) error

	// SearchMentions returns up to mentionSearchLimit title matches for the
	// @-mention typeahead, row-isolated to userID and excluding excludeID (the
	// note currently open for editing — it cannot usefully mention itself).
	SearchMentions(ctx context.Context, userID, term, excludeID string) ([]MentionResult, error)

	// GetBacklinks returns the list-shape view of every note that mentions id,
	// i.e. every note_links row where id is the target. RESOURCE_NOT_FOUND if id
	// doesn't exist or isn't owned by userID — checked before the backlink query
	// so a foreign id can't be distinguished from one with zero backlinks.
	GetBacklinks(ctx context.Context, userID, id string) ([]NoteView, error)

	// WithCleaner injects the attachment cleaner invoked best-effort on delete.
	// A post-construction option because the attachment service already depends
	// on this domain via OwnerVerifier — the concretes meet only in main.go.
	WithCleaner(c AttachmentCleaner) Service
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
	// first (keyset on updated_at DESC, id DESC). Selects content_text for the
	// excerpt and never content.
	ListByProject(ctx context.Context, userID, projectID string, f ListNotesFilter) ([]Note, string, error)

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

	// ExistsByID reports whether a note exists, system-wide (no user scope). The
	// attachment GC sweep uses it to find rows whose owner has vanished; it has
	// no request-path caller, so it can never become an existence oracle.
	ExistsByID(ctx context.Context, id string) (bool, error)

	// SearchMentions returns up to limit prefix matches on title/content against
	// the same GIN index the project note list search would use, scoped to
	// userID and excluding excludeID. Ordered by rank then recency, same as
	// the full search endpoint.
	SearchMentions(ctx context.Context, userID, term, excludeID string, limit int) ([]MentionResult, error)
}
