//go:build integration

package attachment_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/internal/domain/attachment"
	"github.com/nicoflow/nicoflow-api/internal/testutil"
)

const testEmailSuffix = "@attach.integration.test"

func cleanTestData(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	queries := []string{
		`DELETE FROM file_attachments WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%' || $1)`,
		`DELETE FROM users WHERE email LIKE '%' || $1`,
	}
	for _, q := range queries {
		if _, err := pool.Exec(context.Background(), q, testEmailSuffix); err != nil {
			t.Fatalf("cleanTestData: %v", err)
		}
	}
}

func seedUser(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	id := uuid.NewString()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO users (id, email, username, password_hash, plan)
		 VALUES ($1, $2, $3, 'x', 'free')`,
		id, id+testEmailSuffix, "u_"+id[:8],
	)
	if err != nil {
		t.Fatalf("seedUser: %v", err)
	}
	return id
}

func newRepo(t *testing.T) (attachment.Repository, *pgxpool.Pool) {
	t.Helper()
	pool := testutil.NewTestDB(t)
	cleanTestData(t, pool)
	t.Cleanup(func() { cleanTestData(t, pool) })
	return attachment.NewRepository(pool), pool
}

func newAttachment(userID, ownerID string, size int64) attachment.Attachment {
	return attachment.Attachment{
		ID:        uuid.NewString(),
		OwnerType: attachment.OwnerTypeTask,
		OwnerID:   ownerID,
		UserID:    userID,
		FileName:  "file.pdf",
		FileSize:  size,
		MimeType:  "application/pdf",
		S3Key:     "attachments/" + userID + "/" + uuid.NewString(),
	}
}

// Insert → list-by-owner → get → sum → delete, all scoped to the user.
func TestRepo_InsertListGetSumDelete(t *testing.T) {
	r, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)
	ownerID := uuid.NewString()

	a := newAttachment(userID, ownerID, 1024)
	got, ok, err := r.InsertGuarded(ctx, a)
	if err != nil || !ok {
		t.Fatalf("InsertGuarded: ok=%v err=%v", ok, err)
	}
	if got.ID != a.ID || got.FileSize != 1024 || got.CreatedAt.IsZero() {
		t.Fatalf("unexpected row: %+v", got)
	}

	list, err := r.ListByOwner(ctx, userID, attachment.OwnerTypeTask, ownerID)
	if err != nil || len(list) != 1 || list[0].ID != a.ID {
		t.Fatalf("ListByOwner: %+v err=%v", list, err)
	}

	one, err := r.GetByID(ctx, userID, a.ID)
	if err != nil || one.ID != a.ID {
		t.Fatalf("GetByID: %+v err=%v", one, err)
	}

	sum, err := r.SumBytesForUser(ctx, userID)
	if err != nil || sum != 1024 {
		t.Fatalf("SumBytesForUser: got %d err=%v", sum, err)
	}

	if err := r.Delete(ctx, userID, a.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := r.GetByID(ctx, userID, a.ID); !isNotFound(err) {
		t.Fatalf("GetByID after delete: want not-found, got %v", err)
	}
}

// AC4: user B can't list, get, or delete user A's attachments.
func TestRepo_RowIsolation(t *testing.T) {
	r, pool := newRepo(t)
	ctx := context.Background()
	a := seedUser(t, pool)
	b := seedUser(t, pool)
	ownerID := uuid.NewString()

	att := newAttachment(a, ownerID, 512)
	if _, ok, err := r.InsertGuarded(ctx, att); err != nil || !ok {
		t.Fatalf("InsertGuarded: ok=%v err=%v", ok, err)
	}

	// Same owner id, different user → no rows.
	if list, err := r.ListByOwner(ctx, b, attachment.OwnerTypeTask, ownerID); err != nil || len(list) != 0 {
		t.Fatalf("ListByOwner for B: %+v err=%v", list, err)
	}
	if _, err := r.GetByID(ctx, b, att.ID); !isNotFound(err) {
		t.Fatalf("GetByID for B: want not-found, got %v", err)
	}
	if err := r.Delete(ctx, b, att.ID); !isNotFound(err) {
		t.Fatalf("Delete for B: want not-found, got %v", err)
	}
	// A's row survives B's attempts.
	if list, err := r.ListByOwner(ctx, a, attachment.OwnerTypeTask, ownerID); err != nil || len(list) != 1 {
		t.Fatalf("A's row must survive: %+v err=%v", list, err)
	}
}

// AC2: a user at (MaxBytes - 4MB); two concurrent 4MB inserts → exactly one wins,
// total never exceeds the cap.
func TestRepo_ConcurrentByteGuard(t *testing.T) {
	r, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)
	ownerID := uuid.NewString()

	const four = 4 * 1024 * 1024
	// Seed the user right up to cap-minus-one-slot.
	seed := newAttachment(userID, ownerID, attachment.MaxBytesPerUser-four)
	if _, ok, err := r.InsertGuarded(ctx, seed); err != nil || !ok {
		t.Fatalf("seed insert: ok=%v err=%v", ok, err)
	}

	var wg sync.WaitGroup
	results := make([]bool, 2)
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// distinct owner so the count cap never interferes — this tests bytes only.
			_, ok, err := r.InsertGuarded(ctx, newAttachment(userID, uuid.NewString(), four))
			results[i], errs[i] = ok, err
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("insert %d errored: %v", i, err)
		}
	}
	won := 0
	for _, ok := range results {
		if ok {
			won++
		}
	}
	if won != 1 {
		t.Fatalf("want exactly one insert to win, got %d", won)
	}
	sum, err := r.SumBytesForUser(ctx, userID)
	if err != nil {
		t.Fatalf("SumBytesForUser: %v", err)
	}
	if sum > attachment.MaxBytesPerUser {
		t.Fatalf("byte cap exceeded: sum=%d cap=%d", sum, attachment.MaxBytesPerUser)
	}
}

// AC3: an owner already holding MaxFilesPerOwner → the next insert writes 0 rows.
func TestRepo_CountCap(t *testing.T) {
	r, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)
	ownerID := uuid.NewString()

	for i := 0; i < attachment.MaxFilesPerOwner; i++ {
		if _, ok, err := r.InsertGuarded(ctx, newAttachment(userID, ownerID, 1)); err != nil || !ok {
			t.Fatalf("fill insert %d: ok=%v err=%v", i, ok, err)
		}
	}
	// The 21st.
	_, ok, err := r.InsertGuarded(ctx, newAttachment(userID, ownerID, 1))
	if err != nil {
		t.Fatalf("cap insert errored: %v", err)
	}
	if ok {
		t.Fatalf("insert past count cap should write 0 rows")
	}
	list, err := r.ListByOwner(ctx, userID, attachment.OwnerTypeTask, ownerID)
	if err != nil || len(list) != attachment.MaxFilesPerOwner {
		t.Fatalf("owner should hold exactly %d, got %d err=%v", attachment.MaxFilesPerOwner, len(list), err)
	}
}

// DeleteAllForOwner removes the owner's rows and returns them; a different owner
// of the same user is untouched.
func TestRepo_DeleteAllForOwner(t *testing.T) {
	r, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)
	ownerA := uuid.NewString()
	ownerB := uuid.NewString()

	for i := 0; i < 3; i++ {
		if _, ok, err := r.InsertGuarded(ctx, newAttachment(userID, ownerA, 1)); err != nil || !ok {
			t.Fatalf("insert ownerA: %v", err)
		}
	}
	if _, ok, err := r.InsertGuarded(ctx, newAttachment(userID, ownerB, 1)); err != nil || !ok {
		t.Fatalf("insert ownerB: %v", err)
	}

	deleted, err := r.DeleteAllForOwner(ctx, userID, attachment.OwnerTypeTask, ownerA)
	if err != nil || len(deleted) != 3 {
		t.Fatalf("DeleteAllForOwner: %d deleted err=%v", len(deleted), err)
	}
	if list, err := r.ListByOwner(ctx, userID, attachment.OwnerTypeTask, ownerA); err != nil || len(list) != 0 {
		t.Fatalf("ownerA should be empty: %+v err=%v", list, err)
	}
	if list, err := r.ListByOwner(ctx, userID, attachment.OwnerTypeTask, ownerB); err != nil || len(list) != 1 {
		t.Fatalf("ownerB must survive: %+v err=%v", list, err)
	}
	// ListByUser (GC helper) still sees ownerB's single row.
	if all, err := r.ListByUser(ctx, userID); err != nil || len(all) != 1 {
		t.Fatalf("ListByUser: %+v err=%v", all, err)
	}
}

func isNotFound(err error) bool {
	var ae *apperror.AppError
	if errors.As(err, &ae) {
		return ae.Code == apperror.ErrResourceNotFound
	}
	return false
}
