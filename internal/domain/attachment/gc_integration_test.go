//go:build integration

package attachment_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/nicoflow/nicoflow-api/internal/domain/attachment"
	"github.com/nicoflow/nicoflow-api/internal/testutil"
)

// stubExistence reports every owner alive EXCEPT those in dead. The DB/bucket is
// shared across integration tests, so a live-set model would falsely reap other
// tests' rows; the dead-set keeps the sweep scoped to this test's owner. The
// task-repo adapter is wired in main.go — here we drive liveness directly to
// exercise the sweep against real MinIO + Postgres.
type stubExistence struct{ dead map[string]struct{} }

func (s stubExistence) OwnerExists(_ context.Context, ownerType, ownerID string) (bool, error) {
	_, isDead := s.dead[ownerType+"|"+ownerID]
	return !isDead, nil
}

// confirmObject uploads bytes and confirms them, returning the stored view — the
// real path that plants a (row, object) pair for the GC test to reconcile.
func confirmObject(t *testing.T, svc attachment.Service, userID, ownerID string) attachment.AttachmentView {
	t.Helper()
	ctx := context.Background()
	up, err := svc.UploadURL(ctx, userID, "pro", attachment.UploadURLRequest{
		OwnerType: "task", OwnerID: ownerID, FileName: "f.pdf", MimeType: "application/pdf", ClaimedSize: 4,
	})
	if err != nil {
		t.Fatalf("UploadURL: %v", err)
	}
	uploadObject(t, up, []byte("%PDF"))
	view, err := svc.Confirm(ctx, userID, "pro", attachment.ConfirmRequest{S3Key: up.S3Key, FileName: "f.pdf"})
	if err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	return view
}

// TestService_MinIO_GC_Reconciles seeds an orphan object (no row), a dead-owner
// row, and a healthy pair, then asserts the sweep reaps the first two and leaves
// the third intact (AC3/AC4/AC5).
func TestService_MinIO_GC_Reconciles(t *testing.T) {
	store := newMinIOStore(t)
	pool := testutil.NewTestDB(t)
	userID := seedProUser(t, pool)
	ctx := context.Background()

	liveOwner := uuid.NewString()
	deadOwner := uuid.NewString()

	// Healthy pair: live owner, confirmed attachment (must survive).
	dead := map[string]struct{}{"task|" + deadOwner: {}}
	svcLive := attachment.NewService(attachment.NewRepository(pool), store, stubOwners{},
		stubExistence{dead: dead}, nil)
	liveView := confirmObject(t, svcLive, userID, liveOwner)

	// Dead-owner row: confirmed against an owner the sweep will report as gone.
	deadView := confirmObject(t, svcLive, userID, deadOwner)

	// Orphan object: a presigned upload that was posted but never confirmed — an
	// object under attachments/ with no matching row.
	orphan, err := svcLive.UploadURL(ctx, userID, "pro", attachment.UploadURLRequest{
		OwnerType: "task", OwnerID: liveOwner, FileName: "orphan.pdf", MimeType: "application/pdf", ClaimedSize: 4,
	})
	if err != nil {
		t.Fatalf("UploadURL(orphan): %v", err)
	}
	uploadObject(t, orphan, []byte("%PDF"))

	// GC treats deadOwner as gone → its row + object are reaped, the orphan object
	// is reaped, the healthy pair is untouched. Counts use >= because the shared
	// integration DB/bucket may hold unrelated rows; membership asserts pin down
	// this test's own three fixtures precisely.
	gcSvc := attachment.NewService(attachment.NewRepository(pool), store, stubOwners{},
		stubExistence{dead: dead}, nil)

	if _, err := gcSvc.RunGC(ctx); err != nil {
		t.Fatalf("RunGC: %v", err)
	}

	// AC5: healthy attachment still present.
	if l, err := gcSvc.ListByOwner(ctx, userID, "task", liveOwner); err != nil || len(l) != 1 || l[0].ID != liveView.ID {
		t.Fatalf("healthy attachment must survive GC: %+v err=%v", l, err)
	}
	// AC4: dead-owner row gone.
	if l, err := gcSvc.ListByOwner(ctx, userID, "task", deadOwner); err != nil || len(l) != 0 {
		t.Fatalf("dead-owner row must be reaped: %+v err=%v", l, err)
	}
	_ = deadView

	// AC3: the orphan object is gone — HeadObject now errors (confirm re-heads,
	// so a missing object fails there). A fresh confirm on the orphan key must
	// fail because the object no longer exists.
	if _, err := gcSvc.Confirm(ctx, userID, "pro", attachment.ConfirmRequest{S3Key: orphan.S3Key, FileName: "orphan.pdf"}); err == nil {
		t.Fatal("orphan object should have been reaped by GC, but confirm still found it")
	}
}
