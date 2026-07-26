// Package attachment owns the persistence for file attachments (E-024). The
// owner is a polymorphic {type, id} pair so tasks (now) and notes (later) share
// one table. Quota enforcement lives in the guarded insert, not a read-modify-
// write, so parallel uploads can't race past the limits.
package attachment

import (
	"context"
	"time"

	"github.com/nicoflow/nicoflow-api/internal/storage"
)

// Owner types. Kept as a small closed set so the polymorphic owner column can't
// drift into free-form values.
const (
	OwnerTypeTask = "task"
)

// Domain event types emitted on mutation. Equal to the wire names by convention;
// the ws adapter maps them through an explicit table (never a cast).
const (
	EventCreated = "attachment.created"
	EventDeleted = "attachment.deleted"
)

// allowedMimeTypes is the explicit upload allowlist — no globs, no SVG (an SVG
// can carry script and would execute in the user's origin if ever rendered
// inline). A type outside this set is rejected at both upload-url and confirm.
var allowedMimeTypes = map[string]struct{}{
	"image/jpeg":         {},
	"image/png":          {},
	"image/gif":          {},
	"image/webp":         {},
	"application/pdf":    {},
	"text/plain":         {},
	"text/csv":           {},
	"application/zip":    {},
	"application/msword": {},
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": {},
	"application/vnd.ms-excel": {},
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet": {},
}

// isAllowedMime reports whether a MIME type may be uploaded/stored.
func isAllowedMime(mimeType string) bool {
	_, ok := allowedMimeTypes[mimeType]
	return ok
}

// AttachmentView is the API-facing shape. s3Key is deliberately omitted — the
// object key never leaves the server (it encodes the owner tree and is only used
// to mint presigned URLs).
type AttachmentView struct {
	ID        string    `json:"id"`
	OwnerType string    `json:"ownerType"`
	OwnerID   string    `json:"ownerId"`
	FileName  string    `json:"fileName"`
	FileSize  int64     `json:"fileSize"`
	MimeType  string    `json:"mimeType"`
	CreatedAt time.Time `json:"createdAt"`
}

func toView(a Attachment) AttachmentView {
	return AttachmentView{
		ID: a.ID, OwnerType: a.OwnerType, OwnerID: a.OwnerID,
		FileName: a.FileName, FileSize: a.FileSize, MimeType: a.MimeType, CreatedAt: a.CreatedAt,
	}
}

// DeletedPayload is the attachment.deleted event body — just enough for a client
// to drop the row and invalidate the owner's list, never stale file metadata.
type DeletedPayload struct {
	ID        string `json:"id"`
	OwnerType string `json:"ownerType"`
	OwnerID   string `json:"ownerId"`
}

// UploadURLRequest is the body for POST /attachments/upload-url.
type UploadURLRequest struct {
	OwnerType   string `json:"ownerType"`
	OwnerID     string `json:"ownerId"`
	FileName    string `json:"fileName"`
	MimeType    string `json:"mimeType"`
	ClaimedSize int64  `json:"fileSize"`
}

// UploadURLResponse returns the presigned PUT URL, the headers the client must
// send with the raw file body (Content-Type is signed in), and the s3Key the
// client echoes back to confirm. s3Key is a one-time upload target, not a stored
// secret. (Was a POST-policy { url, fields }; R2 doesn't support POST policy —
// NIC-1679 — so uploads are presigned PUT.)
type UploadURLResponse struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	S3Key   string            `json:"s3Key"`
}

// ConfirmRequest is the body for POST /attachments. Only the s3Key is trusted
// from the client; size and type are re-read from S3 via HeadObject.
type ConfirmRequest struct {
	S3Key    string `json:"s3Key"`
	FileName string `json:"fileName"`
}

// OwnerVerifier asserts that the caller owns the polymorphic owner referenced by
// an attachment. Defined in this (consumer) package; the task domain supplies an
// adapter. Unknown owner type → INVALID_INPUT; not owned / missing →
// RESOURCE_NOT_FOUND (never an existence leak).
type OwnerVerifier interface {
	VerifyOwner(ctx context.Context, userID, ownerType, ownerID string) error
}

// Storage is the object-store port the service needs. Satisfied by
// *storage.Client. Narrowed to just the operations used here.
type Storage interface {
	Enabled() bool
	PresignUpload(key, contentType string) (storage.PresignedUpload, error)
	PresignDownload(ctx context.Context, key, filename string) (string, error)
	Head(ctx context.Context, key string) (storage.HeadResult, error)
	Delete(ctx context.Context, key string) error
	List(ctx context.Context, prefix string) ([]string, error)
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

// Service is the attachment domain's business-logic contract consumed by the
// handler.
type Service interface {
	UploadURL(ctx context.Context, userID, plan string, req UploadURLRequest) (UploadURLResponse, error)
	Confirm(ctx context.Context, userID, plan string, req ConfirmRequest) (AttachmentView, error)
	ListByOwner(ctx context.Context, userID, ownerType, ownerID string) ([]AttachmentView, error)
	DownloadURL(ctx context.Context, userID, id string) (string, error)
	Delete(ctx context.Context, userID, id string) error

	// DeleteAllForOwner removes every attachment for an owner and best-effort
	// deletes their S3 objects. The task-delete flow calls it via the
	// task.AttachmentCleaner seam; a failed S3 delete never fails the call —
	// the GC sweep reconciles anything left behind.
	DeleteAllForOwner(ctx context.Context, userID, ownerType, ownerID string) error

	// RunGC reconciles the object store against the DB: it deletes objects with
	// no matching row (never-confirmed uploads) and rows whose owner has vanished
	// (plus their objects). Best-effort per object; returns a summary of what was
	// reaped. Storage-disabled ⇒ no-op summary.
	RunGC(ctx context.Context) (GCSummary, error)
}

// GCSummary is the attachment-gc sweep result — server-side log only, never
// user-facing.
type GCSummary struct {
	ObjectsDeleted int `json:"objectsDeleted"`
	RowsDeleted    int `json:"rowsDeleted"`
}

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

	// AllKeys returns the s3_key of every attachment row, across all users. The
	// GC sweep diffs the object store against this set to find never-confirmed
	// uploads (objects with no row). System-level: not user-scoped by design.
	AllKeys(ctx context.Context) (map[string]struct{}, error)

	// ListAllOwners returns the distinct (ownerType, ownerID) pairs referenced by
	// attachment rows, across all users. The GC sweep checks each against the
	// owner store to find dead-owner rows. System-level: not user-scoped.
	ListAllOwners(ctx context.Context) ([]Owner, error)

	// DeleteByOwner removes every attachment row for one owner, across all users,
	// returning the deleted rows so the GC sweep can reclaim their S3 objects.
	// System-level (dead-owner reap); the user-scoped path is DeleteAllForOwner.
	DeleteByOwner(ctx context.Context, ownerType, ownerID string) ([]Attachment, error)
}

// Owner is a polymorphic owner reference — the (type, id) pair the GC sweep
// checks for existence.
type Owner struct {
	OwnerType string
	OwnerID   string
}

// OwnerExistence reports whether a polymorphic owner still exists, system-wide
// (no user scope). The GC sweep uses it to find rows whose owner has vanished.
// Defined here (consumer package); the task domain supplies an adapter. An
// unknown owner type is treated as non-existent so its rows are reaped.
type OwnerExistence interface {
	OwnerExists(ctx context.Context, ownerType, ownerID string) (bool, error)
}
