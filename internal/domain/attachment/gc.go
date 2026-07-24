package attachment

import (
	"context"

	"github.com/rs/zerolog/log"

	"github.com/nicoflow/nicoflow-api/internal/storage"
)

// DeleteAllForOwner removes every attachment row for one owner, then best-effort
// deletes each object from S3. Row removal is authoritative; a failed S3 delete
// is logged, not returned — the object becomes an orphan the GC sweep reaps.
// The task-delete flow reaches this through the task.AttachmentCleaner seam.
func (s *service) DeleteAllForOwner(ctx context.Context, userID, ownerType, ownerID string) error {
	rows, err := s.repo.DeleteAllForOwner(ctx, userID, ownerType, ownerID)
	if err != nil {
		return err
	}
	if s.store.Enabled() {
		for _, a := range rows {
			s.cleanup(ctx, a.S3Key)
		}
	}
	return nil
}

// RunGC reconciles the object store against the DB in two passes:
//
//	(a) orphan objects — an S3 object under attachments/ with no matching row
//	    (a never-confirmed upload) is deleted.
//	(b) dead-owner rows — a row whose owner no longer exists has its row deleted
//	    and its S3 object reclaimed.
//
// Every S3 delete is best-effort; a failure is logged and the sweep continues,
// so one bad object can't strand the rest. Storage-disabled ⇒ no-op.
func (s *service) RunGC(ctx context.Context) (GCSummary, error) {
	var sum GCSummary
	if !s.store.Enabled() {
		return sum, nil
	}

	// (a) Orphan objects: object keys minus known row keys.
	keys, err := s.store.List(ctx, storage.KeyPrefix())
	if err != nil {
		return sum, err
	}
	known, err := s.repo.AllKeys(ctx)
	if err != nil {
		return sum, err
	}
	for _, key := range keys {
		if _, ok := known[key]; ok {
			continue
		}
		if s.cleanup(ctx, key) {
			sum.ObjectsDeleted++
		}
	}

	// (b) Dead-owner rows: rows whose owner no longer exists.
	owners, err := s.repo.ListAllOwners(ctx)
	if err != nil {
		return sum, err
	}
	for _, o := range owners {
		exists, err := s.ownerAlive(ctx, o.OwnerType, o.OwnerID)
		if err != nil {
			log.Error().Err(err).Str("owner_type", o.OwnerType).Str("owner_id", o.OwnerID).
				Msg("attachment gc: owner-existence check failed — skipping")
			continue
		}
		if exists {
			continue
		}
		reaped, err := s.repo.DeleteByOwner(ctx, o.OwnerType, o.OwnerID)
		if err != nil {
			log.Error().Err(err).Str("owner_type", o.OwnerType).Str("owner_id", o.OwnerID).
				Msg("attachment gc: dead-owner row delete failed — skipping")
			continue
		}
		sum.RowsDeleted += len(reaped)
		for _, a := range reaped {
			if s.cleanup(ctx, a.S3Key) {
				sum.ObjectsDeleted++
			}
		}
	}

	return sum, nil
}

// ownerAlive reports whether an owner still exists. A nil OwnerExistence (dead-
// owner reap disabled) treats every owner as alive, so nothing is reaped.
func (s *service) ownerAlive(ctx context.Context, ownerType, ownerID string) (bool, error) {
	if s.ownerExt == nil {
		return true, nil
	}
	return s.ownerExt.OwnerExists(ctx, ownerType, ownerID)
}
