// Package notelink owns the persistence for note-to-note mention links
// (E-057a). A link is a directed edge, source note -> target note, created
// when a Tiptap @-mention node is saved into the source note's content. This
// package is storage only: the resync-on-save trigger and the mention-diff
// logic live in the note domain's update path, not here.
package notelink

import (
	"context"
	"time"
)

// Link is the domain model for one directed note-to-note mention.
type Link struct {
	ID           string
	SourceNoteID string
	TargetNoteID string
	CreatedAt    time.Time
}

// BacklinkNote is one entry in a backlinks result: the minimal note identity
// and title needed to render "linked from" without pulling in the note
// package's full Note type (which would invert the dependency direction —
// notelink is a leaf package, note will depend on it, not the other way).
type BacklinkNote struct {
	ID    string
	Title string
}

// Repository is the data-access contract for note links. Every method takes
// note IDs only — no user_id — because ownership of both endpoints is already
// enforced by the note domain before a link mutation is ever requested here.
type Repository interface {
	// CreateLink inserts one directed link. A duplicate (source, target) pair
	// is idempotent: ON CONFLICT DO NOTHING, so a caller never has to check
	// existence first.
	CreateLink(ctx context.Context, sourceNoteID, targetNoteID string) error

	// DeleteLink removes one directed link, if present. No error when absent.
	DeleteLink(ctx context.Context, sourceNoteID, targetNoteID string) error

	// ReplaceLinksForNote resyncs every outbound link of sourceNoteID to
	// exactly targetNoteIDs, in one transaction: rows for targets no longer
	// mentioned are deleted, rows for newly mentioned targets are inserted,
	// unchanged rows are left alone. Called on every content save. An empty
	// targetNoteIDs clears all outbound links for the note.
	ReplaceLinksForNote(ctx context.Context, sourceNoteID string, targetNoteIDs []string) error

	// GetBacklinks returns the notes that link to noteID, i.e. the source note
	// of every row where noteID is the target. Order is not guaranteed by this
	// layer.
	GetBacklinks(ctx context.Context, noteID string) ([]BacklinkNote, error)
}
