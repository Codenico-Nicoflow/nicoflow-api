package attachment

import (
	"context"
	"errors"
	"testing"

	"github.com/nicoflow/nicoflow-api/internal/storage"
)

// ── GC-specific fakes ────────────────────────────────────────────────────────

// gcRepo is a richer repo fake for the GC + owner-delete paths: it tracks what
// was deleted and can be driven per-method.
type gcRepo struct {
	allKeys          map[string]struct{}
	owners           []Owner
	deleteByOwnerRet map[string][]Attachment // keyed by ownerType|ownerID
	forOwnerRet      []Attachment
	forOwnerErr      error

	deletedOwners   []Owner
	forOwnerDeleted []Owner
}

func (r *gcRepo) InsertGuarded(context.Context, Attachment) (Attachment, bool, error) {
	return Attachment{}, false, nil
}
func (r *gcRepo) ListByOwner(context.Context, string, string, string) ([]Attachment, error) {
	return nil, nil
}
func (r *gcRepo) GetByID(context.Context, string, string) (Attachment, error) {
	return Attachment{}, nil
}
func (r *gcRepo) GetByS3Key(context.Context, string, string) (Attachment, error) {
	return Attachment{}, nil
}
func (r *gcRepo) Delete(context.Context, string, string) error           { return nil }
func (r *gcRepo) SumBytesForUser(context.Context, string) (int64, error) { return 0, nil }
func (r *gcRepo) ListByUser(context.Context, string) ([]Attachment, error) {
	return nil, nil
}
func (r *gcRepo) DeleteAllForOwner(_ context.Context, _, ownerType, ownerID string) ([]Attachment, error) {
	if r.forOwnerErr != nil {
		return nil, r.forOwnerErr
	}
	r.forOwnerDeleted = append(r.forOwnerDeleted, Owner{ownerType, ownerID})
	return r.forOwnerRet, nil
}
func (r *gcRepo) AllKeys(context.Context) (map[string]struct{}, error) { return r.allKeys, nil }
func (r *gcRepo) ListAllOwners(context.Context) ([]Owner, error)       { return r.owners, nil }
func (r *gcRepo) DeleteByOwner(_ context.Context, ownerType, ownerID string) ([]Attachment, error) {
	r.deletedOwners = append(r.deletedOwners, Owner{ownerType, ownerID})
	return r.deleteByOwnerRet[ownerType+"|"+ownerID], nil
}

// gcStore lets the GC test control List and record Deletes; Delete can fail for
// a chosen key to prove best-effort counting.
type gcStore struct {
	enabled  bool
	keys     []string
	deleted  []string
	failKeys map[string]struct{}
}

func (s *gcStore) Enabled() bool { return s.enabled }
func (s *gcStore) PresignUpload(string, string) (storage.PresignedUpload, error) {
	return storage.PresignedUpload{}, nil
}
func (s *gcStore) PresignDownload(context.Context, string, string) (string, error) {
	return "", nil
}
func (s *gcStore) Head(context.Context, string) (storage.HeadResult, error) {
	return storage.HeadResult{}, nil
}
func (s *gcStore) List(_ context.Context, _ string) ([]string, error) { return s.keys, nil }
func (s *gcStore) Delete(_ context.Context, key string) error {
	if _, bad := s.failKeys[key]; bad {
		return errors.New("s3 delete failed")
	}
	s.deleted = append(s.deleted, key)
	return nil
}

// existOwners answers OwnerExists from a live set; anything not in the set is dead.
type existOwners struct {
	live map[string]struct{}
	err  error
}

func (e existOwners) OwnerExists(_ context.Context, ownerType, ownerID string) (bool, error) {
	if e.err != nil {
		return false, e.err
	}
	_, ok := e.live[ownerType+"|"+ownerID]
	return ok, nil
}

func gcSvc(repo Repository, store Storage, ext OwnerExistence) *service {
	return &service{repo: repo, store: store, ownerExt: ext}
}

// ── DeleteAllForOwner ────────────────────────────────────────────────────────

func TestDeleteAllForOwner_RemovesRowsAndReclaimsObjects(t *testing.T) {
	repo := &gcRepo{forOwnerRet: []Attachment{
		{ID: "a1", S3Key: "attachments/u1/task/t1/o1"},
		{ID: "a2", S3Key: "attachments/u1/task/t1/o2"},
	}}
	store := &gcStore{enabled: true}
	svc := gcSvc(repo, store, nil)

	if err := svc.DeleteAllForOwner(context.Background(), "u1", "task", "t1"); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(repo.forOwnerDeleted) != 1 {
		t.Fatalf("row delete ran %d times, want 1", len(repo.forOwnerDeleted))
	}
	if len(store.deleted) != 2 {
		t.Fatalf("S3 delete ran %d times, want 2 (one per key)", len(store.deleted))
	}
}

func TestDeleteAllForOwner_FailingS3StillClearsRows(t *testing.T) {
	repo := &gcRepo{forOwnerRet: []Attachment{{ID: "a1", S3Key: "k1"}}}
	store := &gcStore{enabled: true, failKeys: map[string]struct{}{"k1": {}}}
	svc := gcSvc(repo, store, nil)

	// A failing S3 delete is logged, not returned — the rows are already gone.
	if err := svc.DeleteAllForOwner(context.Background(), "u1", "task", "t1"); err != nil {
		t.Fatalf("S3 failure must not fail DeleteAllForOwner, got: %v", err)
	}
	if len(repo.forOwnerDeleted) != 1 {
		t.Fatalf("rows must still be cleared, ran %d", len(repo.forOwnerDeleted))
	}
}

func TestDeleteAllForOwner_RepoErrorPropagates(t *testing.T) {
	repo := &gcRepo{forOwnerErr: errors.New("db down")}
	store := &gcStore{enabled: true}
	svc := gcSvc(repo, store, nil)

	if err := svc.DeleteAllForOwner(context.Background(), "u1", "task", "t1"); err == nil {
		t.Fatal("expected repo error to propagate")
	}
}

// ── RunGC ────────────────────────────────────────────────────────────────────

func TestRunGC_ReapsOrphanObjects(t *testing.T) {
	// k_orphan has no row; k_known does. Only the orphan is deleted.
	repo := &gcRepo{allKeys: map[string]struct{}{"k_known": {}}}
	store := &gcStore{enabled: true, keys: []string{"k_known", "k_orphan"}}
	svc := gcSvc(repo, store, existOwners{})

	sum, err := svc.RunGC(context.Background())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if sum.ObjectsDeleted != 1 {
		t.Fatalf("objectsDeleted = %d, want 1", sum.ObjectsDeleted)
	}
	if len(store.deleted) != 1 || store.deleted[0] != "k_orphan" {
		t.Fatalf("wrong object reaped: %v", store.deleted)
	}
}

func TestRunGC_ReapsDeadOwnerRowsAndObjects(t *testing.T) {
	// owner t_dead has no live owner → its row + object are reaped; t_live stays.
	repo := &gcRepo{
		allKeys: map[string]struct{}{"k_dead": {}, "k_live": {}},
		owners:  []Owner{{"task", "t_dead"}, {"task", "t_live"}},
		deleteByOwnerRet: map[string][]Attachment{
			"task|t_dead": {{ID: "a1", S3Key: "k_dead"}},
		},
	}
	store := &gcStore{enabled: true, keys: []string{"k_dead", "k_live"}}
	ext := existOwners{live: map[string]struct{}{"task|t_live": {}}}
	svc := gcSvc(repo, store, ext)

	sum, err := svc.RunGC(context.Background())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(repo.deletedOwners) != 1 || repo.deletedOwners[0].OwnerID != "t_dead" {
		t.Fatalf("wrong dead owner reaped: %v", repo.deletedOwners)
	}
	if sum.RowsDeleted != 1 {
		t.Fatalf("rowsDeleted = %d, want 1", sum.RowsDeleted)
	}
	// k_dead reclaimed via the dead-owner pass (both keys have rows, so no orphan pass hit).
	if len(store.deleted) != 1 || store.deleted[0] != "k_dead" {
		t.Fatalf("wrong object reaped for dead owner: %v", store.deleted)
	}
}

func TestRunGC_LeavesHealthyDataUntouched(t *testing.T) {
	repo := &gcRepo{
		allKeys: map[string]struct{}{"k_live": {}},
		owners:  []Owner{{"task", "t_live"}},
	}
	store := &gcStore{enabled: true, keys: []string{"k_live"}}
	ext := existOwners{live: map[string]struct{}{"task|t_live": {}}}
	svc := gcSvc(repo, store, ext)

	sum, err := svc.RunGC(context.Background())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if sum.ObjectsDeleted != 0 || sum.RowsDeleted != 0 {
		t.Fatalf("healthy data touched: %+v", sum)
	}
	if len(store.deleted) != 0 || len(repo.deletedOwners) != 0 {
		t.Fatalf("healthy data deleted: objs=%v owners=%v", store.deleted, repo.deletedOwners)
	}
}

func TestRunGC_OwnerCheckErrorSkipsThatOwner(t *testing.T) {
	// An owner-existence error must not reap that owner's rows (fail safe).
	repo := &gcRepo{
		allKeys: map[string]struct{}{"k1": {}},
		owners:  []Owner{{"task", "t1"}},
	}
	store := &gcStore{enabled: true, keys: []string{"k1"}}
	ext := existOwners{err: errors.New("db down")}
	svc := gcSvc(repo, store, ext)

	sum, err := svc.RunGC(context.Background())
	if err != nil {
		t.Fatalf("owner-check error must be swallowed per-owner, got: %v", err)
	}
	if sum.RowsDeleted != 0 || len(repo.deletedOwners) != 0 {
		t.Fatalf("must not reap on existence error: %+v / %v", sum, repo.deletedOwners)
	}
}

func TestRunGC_StorageDisabledIsNoop(t *testing.T) {
	repo := &gcRepo{}
	store := &gcStore{enabled: false}
	svc := gcSvc(repo, store, nil)

	sum, err := svc.RunGC(context.Background())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if sum != (GCSummary{}) {
		t.Fatalf("disabled store must no-op, got %+v", sum)
	}
}
