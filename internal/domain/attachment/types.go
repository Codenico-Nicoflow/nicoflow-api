// Package attachment owns the persistence for file attachments (E-024). The
// owner is a polymorphic {type, id} pair so tasks (now) and notes (later) share
// one table. Quota enforcement lives in the guarded insert, not a read-modify-
// write, so parallel uploads can't race past the limits.
package attachment

import (
	"context"
	"time"
)

// Owner types. Kept as a small closed set so the polymorphic owner column can't
// drift into free-form values.
const (
	OwnerTypeTask = "task"
)

// Quota limits enforced by the guarded insert (SPEC §5 / NIC-1638).
//
//   - MaxBytesPerUser: total attachment bytes a single user may store.
//   - MaxFilesPerOwner: attachments a single owner (e.g. one task) may hold.
const (
	MaxBytesPerUser  int64 = 100 * 1024 * 1024 // 100 MB
	MaxFilesPerOwner int   = 20
)

// Attachment is one stored file. s3_key is the object-store key (unique); the
// bytes themselves live in S3, never the DB.
type Attachment struct {
	ID        string    `json:"id"`
	OwnerType string    `json:"ownerType"`
	OwnerID   string    `json:"ownerId"`
	UserID    string    `json:"userId"`
	FileName  string    `json:"fileName"`
	FileSize  int64     `json:"fileSize"`
	MimeType  string    `json:"mimeType"`
	S3Key     string    `json:"s3Key"`
	CreatedAt time.Time `json:"createdAt"`
}

// Repository is the data-access contract for attachments. Every method is
// row-scoped by user_id. Defined here (the consumer package) per project
// layering; the pg implementation lives in repository.go.
type Repository interface {
	// InsertGuarded atomically inserts a row only if it keeps the owner under
	// MaxFilesPerOwner and the user under MaxBytesPerUser, checked inside the
	// statement. Returns (row, true) on success, (_, false) when a limit would
	// be exceeded (0 rows inserted) — the caller then deletes the S3 object and
	// returns the matching plan-limit error.
	InsertGuarded(ctx context.Context, a Attachment) (Attachment, bool, error)

	// ListByOwner returns a user's attachments for one owner, newest first.
	ListByOwner(ctx context.Context, userID, ownerType, ownerID string) ([]Attachment, error)

	// GetByID returns one attachment scoped to the user, or a not-found error.
	GetByID(ctx context.Context, userID, id string) (Attachment, error)

	// Delete removes one attachment scoped to the user. Missing row → not-found.
	Delete(ctx context.Context, userID, id string) error

	// SumBytesForUser is the user's current total stored bytes.
	SumBytesForUser(ctx context.Context, userID string) (int64, error)

	// DeleteAllForOwner removes every attachment for an owner (explicit cleanup
	// on owner delete — there is no DB cascade on the polymorphic owner).
	// Returns the deleted rows so the caller can reclaim their S3 objects.
	DeleteAllForOwner(ctx context.Context, userID, ownerType, ownerID string) ([]Attachment, error)

	// ListByUser returns all of a user's attachments — the GC sweep (BE-4)
	// reconciles these rows against the object store.
	ListByUser(ctx context.Context, userID string) ([]Attachment, error)
}
