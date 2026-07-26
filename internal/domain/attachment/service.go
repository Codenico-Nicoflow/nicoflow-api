package attachment

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/internal/storage"
)

// planPro is the tier gate for writes. Reads and delete are open on any plan so
// a downgraded user can still retrieve and clean up existing files.
const planPro = "pro"

// maxUploadBytes caps a single upload at 20 MB (the S3 POST policy enforces the
// same, so a tampered client is rejected by S3 too). Mirrored here for the cheap
// pre-check at upload-url time before we mint anything.
const maxUploadBytes int64 = 20 << 20

type service struct {
	repo     Repository
	store    Storage
	owners   OwnerVerifier
	ownerExt OwnerExistence // system-level owner existence; GC-only
	bcast    Broadcaster
	newKey   func(userID, ownerType, ownerID string) string
}

// NewService wires the attachment service. bcast may be nil (no-op real-time).
// ownerExt drives the GC dead-owner reap; it may be nil, which disables that
// half of the sweep (orphan-object reap still runs).
func NewService(repo Repository, store Storage, owners OwnerVerifier, ownerExt OwnerExistence, bcast Broadcaster) Service {
	return &service{
		repo:     repo,
		store:    store,
		owners:   owners,
		ownerExt: ownerExt,
		bcast:    bcast,
		newKey:   storage.NewObjectKey,
	}
}

// UploadURL mints a presigned POST for a new upload. Gate order: plan → config
// (503) → ownership → cheap claimed-size/type pre-check.
func (s *service) UploadURL(ctx context.Context, userID, plan string, req UploadURLRequest) (UploadURLResponse, error) {
	if plan != planPro {
		return UploadURLResponse{}, apperror.New(http.StatusForbidden, apperror.ErrPlanLimitExceeded, "file attachments require a Pro plan")
	}
	if !s.store.Enabled() {
		return UploadURLResponse{}, errStorageDisabled()
	}
	if err := s.verifyOwner(ctx, userID, req.OwnerType, req.OwnerID); err != nil {
		return UploadURLResponse{}, err
	}
	if strings.TrimSpace(req.FileName) == "" {
		return UploadURLResponse{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "fileName is required")
	}
	if !isAllowedMime(req.MimeType) {
		return UploadURLResponse{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "unsupported file type")
	}
	// Cheap claimed-size pre-check — the real enforcement is the S3 POST policy
	// (20 MB range) and the HeadObject re-read at confirm; this just fails fast.
	if req.ClaimedSize > maxUploadBytes {
		return UploadURLResponse{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "file exceeds the 20 MB limit")
	}

	key := s.newKey(userID, req.OwnerType, req.OwnerID)
	upload, err := s.store.PresignUpload(key, req.MimeType)
	if err != nil {
		return UploadURLResponse{}, fmt.Errorf("attachment.UploadURL presign: %w", err)
	}
	return UploadURLResponse{URL: upload.URL, Headers: upload.Headers, S3Key: key}, nil
}

// Confirm registers an uploaded object. It never trusts client-claimed metadata:
// HeadObject re-reads the real size and content-type from S3. A disallowed type,
// oversize file, or quota overflow deletes the orphaned object and returns the
// matching typed error. Gate order: plan → config → key-ownership → HeadObject
// re-validate → atomic quota insert.
func (s *service) Confirm(ctx context.Context, userID, plan string, req ConfirmRequest) (AttachmentView, error) {
	if plan != planPro {
		return AttachmentView{}, apperror.New(http.StatusForbidden, apperror.ErrPlanLimitExceeded, "file attachments require a Pro plan")
	}
	if !s.store.Enabled() {
		return AttachmentView{}, errStorageDisabled()
	}
	if strings.TrimSpace(req.S3Key) == "" {
		return AttachmentView{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "s3Key is required")
	}
	// The key must live under the caller's own tree (attachments/{userID}/...),
	// so a client can't confirm someone else's or an arbitrary object.
	if !strings.HasPrefix(req.S3Key, storage.UserKeyPrefix(userID)) {
		return AttachmentView{}, apperror.New(http.StatusNotFound, apperror.ErrResourceNotFound, "attachment not found")
	}
	ownerType, ownerID, ok := ownerFromKey(userID, req.S3Key)
	if !ok {
		return AttachmentView{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "malformed s3Key")
	}
	if err := s.verifyOwner(ctx, userID, ownerType, ownerID); err != nil {
		return AttachmentView{}, err
	}

	// Trust nothing the client claimed — read the real object metadata.
	head, err := s.store.Head(ctx, req.S3Key)
	if err != nil {
		return AttachmentView{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "uploaded object not found")
	}
	if !isAllowedMime(head.ContentType) || head.ContentLength > maxUploadBytes || head.ContentLength <= 0 {
		s.cleanup(ctx, req.S3Key)
		return AttachmentView{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "unsupported file type or size")
	}

	att := Attachment{
		ID:        uuid.NewString(),
		OwnerType: ownerType,
		OwnerID:   ownerID,
		UserID:    userID,
		FileName:  sanitizeName(req.FileName),
		FileSize:  head.ContentLength,
		MimeType:  head.ContentType,
		S3Key:     req.S3Key,
	}
	row, inserted, err := s.repo.InsertGuarded(ctx, att)
	if err != nil {
		return AttachmentView{}, err
	}
	if !inserted {
		// The guard tripped: either the per-owner count or the per-user byte cap
		// would be exceeded. Reclaim the orphaned object and map the right code.
		s.cleanup(ctx, req.S3Key)
		return AttachmentView{}, s.quotaError(ctx, userID, head.ContentLength)
	}

	s.emit(userID, Event{Type: EventCreated, Payload: toView(row)})
	return toView(row), nil
}

func (s *service) ListByOwner(ctx context.Context, userID, ownerType, ownerID string) ([]AttachmentView, error) {
	if err := s.verifyOwner(ctx, userID, ownerType, ownerID); err != nil {
		return nil, err
	}
	rows, err := s.repo.ListByOwner(ctx, userID, ownerType, ownerID)
	if err != nil {
		return nil, err
	}
	views := make([]AttachmentView, 0, len(rows))
	for _, a := range rows {
		views = append(views, toView(a))
	}
	return views, nil
}

func (s *service) DownloadURL(ctx context.Context, userID, id string) (string, error) {
	if !s.store.Enabled() {
		return "", errStorageDisabled()
	}
	a, err := s.repo.GetByID(ctx, userID, id)
	if err != nil {
		return "", err
	}
	url, err := s.store.PresignDownload(ctx, a.S3Key, a.FileName)
	if err != nil {
		return "", fmt.Errorf("attachment.DownloadURL presign: %w", err)
	}
	return url, nil
}

// Delete removes the DB row first, then the S3 object. DB-first means a failed
// S3 delete never leaves a dangling row the user can't see; the orphaned object
// is swept by the GC job. S3 delete is idempotent.
func (s *service) Delete(ctx context.Context, userID, id string) error {
	a, err := s.repo.GetByID(ctx, userID, id)
	if err != nil {
		return err
	}
	if err := s.repo.Delete(ctx, userID, id); err != nil {
		return err
	}
	if s.store.Enabled() {
		s.cleanup(ctx, a.S3Key)
	}
	s.emit(userID, Event{Type: EventDeleted, Payload: DeletedPayload{ID: a.ID, OwnerType: a.OwnerType, OwnerID: a.OwnerID}})
	return nil
}

// verifyOwner dispatches through the OwnerVerifier and normalizes an unknown
// owner type into INVALID_INPUT.
func (s *service) verifyOwner(ctx context.Context, userID, ownerType, ownerID string) error {
	if ownerType != OwnerTypeTask {
		return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "unknown owner type")
	}
	if strings.TrimSpace(ownerID) == "" {
		return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "ownerId is required")
	}
	return s.owners.VerifyOwner(ctx, userID, ownerType, ownerID)
}

// quotaError decides which limit tripped the guarded insert so the client gets an
// actionable message. Count-over-owner → PLAN_LIMIT_EXCEEDED; byte-over-user →
// STORAGE_LIMIT_EXCEEDED with usage in the message.
func (s *service) quotaError(ctx context.Context, userID string, incoming int64) error {
	if used, err := s.repo.SumBytesForUser(ctx, userID); err == nil && used+incoming > MaxBytesPerUser {
		return apperror.New(http.StatusForbidden, apperror.ErrStorageLimitExceeded,
			fmt.Sprintf("storage limit reached (%d of %d bytes used)", used, MaxBytesPerUser))
	}
	return apperror.New(http.StatusForbidden, apperror.ErrPlanLimitExceeded,
		fmt.Sprintf("attachment limit reached (max %d per item)", MaxFilesPerOwner))
}

// emit fires a real-time event fire-and-forget: a nil broadcaster is a no-op and
// a panicking one is recovered, so a broken transport can never fail the mutation
// that already committed.
func (s *service) emit(userID string, ev Event) {
	if s.bcast == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			log.Error().Interface("panic", r).Str("event", ev.Type).Msg("attachment: broadcast panicked — dropped")
		}
	}()
	s.bcast.Broadcast(userID, ev)
}

// cleanup best-effort deletes an orphaned S3 object; a failure is logged, not
// surfaced, since the GC sweep reconciles anything left behind. Reports whether
// the delete succeeded so the GC sweep can count what it actually reaped.
func (s *service) cleanup(ctx context.Context, key string) bool {
	if err := s.store.Delete(ctx, key); err != nil {
		log.Error().Err(err).Str("s3_key", key).Msg("attachment: failed to delete orphaned object")
		return false
	}
	return true
}

func errStorageDisabled() *apperror.AppError {
	return apperror.New(http.StatusServiceUnavailable, apperror.ErrServiceUnavailable, "file attachments are not available")
}

// ownerFromKey extracts {ownerType, ownerID} from a key produced by
// storage.NewObjectKey: attachments/{userID}/{ownerType}/{ownerID}/{uuid}.
func ownerFromKey(userID, key string) (ownerType, ownerID string, ok bool) {
	rest := strings.TrimPrefix(key, storage.UserKeyPrefix(userID))
	parts := strings.Split(rest, "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// sanitizeName trims a client filename to a safe stored value, never empty.
func sanitizeName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")
	if name == "" {
		return "file"
	}
	if len(name) > 255 {
		name = name[:255]
	}
	return name
}
