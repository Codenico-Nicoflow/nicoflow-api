//go:build integration

package attachment_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/internal/config"
	"github.com/nicoflow/nicoflow-api/internal/domain/attachment"
	"github.com/nicoflow/nicoflow-api/internal/storage"
	"github.com/nicoflow/nicoflow-api/internal/testutil"
)

// stubOwners approves any owner — the ownership dispatch itself is unit-tested;
// here we exercise the real storage + DB round-trip.
type stubOwners struct{ err error }

func (s stubOwners) VerifyOwner(context.Context, string, string, string) error { return s.err }

func newMinIOStore(t *testing.T) *storage.Client {
	t.Helper()
	endpoint := os.Getenv("TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("TEST_S3_ENDPOINT not set — skipping attachment MinIO integration test")
	}
	c, err := storage.New(context.Background(), config.Config{
		StorageRegion:      envOr("TEST_S3_REGION", "us-east-1"),
		StorageAccessKeyID: envOr("TEST_S3_ACCESS_KEY", "minioadmin"),
		StorageSecretKey:   envOr("TEST_S3_SECRET_KEY", "minioadmin"),
		StorageBucket:      envOr("TEST_S3_BUCKET", "nicoflow-attachments-test"),
		StorageEndpoint:    endpoint,
	})
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	if !c.Enabled() {
		t.Fatal("storage client should be enabled")
	}
	return c
}

func envOr(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}

func seedProUser(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	id := uuid.NewString()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO users (id, email, username, password_hash, plan)
		 VALUES ($1, $2, $3, 'x', 'pro')`,
		id, id+"@attach.svc.integration.test", "u_"+id[:8],
	)
	if err != nil {
		t.Fatalf("seedUser: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM file_attachments WHERE user_id = $1`, id)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, id)
	})
	return id
}

// uploadObject PUTs bytes to the presigned URL (with the signed headers) so the
// object really exists in the store before confirm re-heads it.
func uploadObject(t *testing.T, resp attachment.UploadURLResponse, body []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, resp.URL, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	for k, v := range resp.Headers {
		req.Header.Set(k, v)
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	defer func() { _ = r.Body.Close() }()
	if r.StatusCode < 200 || r.StatusCode >= 300 {
		t.Fatalf("upload PUT status = %d, want 2xx", r.StatusCode)
	}
}

// Full happy path against the real store + Postgres: upload-url → S3 PUT → confirm
// (HeadObject re-validate) → list → download-url → delete.
func TestService_MinIO_FullFlow(t *testing.T) {
	store := newMinIOStore(t)
	pool := testutil.NewTestDB(t)
	userID := seedProUser(t, pool)
	ownerID := uuid.NewString()
	ctx := context.Background()

	svc := attachment.NewService(attachment.NewRepository(pool), store, stubOwners{}, nil, nil)

	up, err := svc.UploadURL(ctx, userID, "pro", attachment.UploadURLRequest{
		OwnerType: "task", OwnerID: ownerID, FileName: "report.pdf", MimeType: "application/pdf", ClaimedSize: 12,
	})
	if err != nil {
		t.Fatalf("UploadURL: %v", err)
	}
	pdf := []byte("%PDF-1.4 xx")
	uploadObject(t, up, pdf)

	view, err := svc.Confirm(ctx, userID, "pro", attachment.ConfirmRequest{S3Key: up.S3Key, FileName: "report.pdf"})
	if err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if view.FileSize != int64(len(pdf)) || view.MimeType != "application/pdf" {
		t.Fatalf("confirm stored wrong metadata: %+v", view)
	}

	list, err := svc.ListByOwner(ctx, userID, "task", ownerID)
	if err != nil || len(list) != 1 || list[0].ID != view.ID {
		t.Fatalf("ListByOwner: %+v err=%v", list, err)
	}

	dl, err := svc.DownloadURL(ctx, userID, view.ID)
	if err != nil || dl == "" {
		t.Fatalf("DownloadURL: %q err=%v", dl, err)
	}

	if err := svc.Delete(ctx, userID, view.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if l, err := svc.ListByOwner(ctx, userID, "task", ownerID); err != nil || len(l) != 0 {
		t.Fatalf("after delete list should be empty: %+v err=%v", l, err)
	}
}

// A user confirming another user's uploaded key is rejected by the key-prefix
// ownership guard as RESOURCE_NOT_FOUND (no existence leak), end-to-end.
func TestService_MinIO_ForeignKeyConfirmRejected(t *testing.T) {
	store := newMinIOStore(t)
	pool := testutil.NewTestDB(t)
	userID := seedProUser(t, pool)
	ctx := context.Background()

	svc := attachment.NewService(attachment.NewRepository(pool), store, stubOwners{}, nil, nil)

	up, err := svc.UploadURL(ctx, userID, "pro", attachment.UploadURLRequest{
		OwnerType: "task", OwnerID: uuid.NewString(), FileName: "x.pdf", MimeType: "application/pdf", ClaimedSize: 3,
	})
	if err != nil {
		t.Fatalf("UploadURL: %v", err)
	}
	uploadObject(t, up, []byte("abc"))

	other := seedProUser(t, pool)
	if _, err := svc.Confirm(ctx, other, "pro", attachment.ConfirmRequest{S3Key: up.S3Key, FileName: "x"}); !isCode(err, apperror.ErrResourceNotFound) {
		t.Fatalf("foreign confirm: want RESOURCE_NOT_FOUND, got %v", err)
	}
}

func isCode(err error, want string) bool {
	var ae *apperror.AppError
	return errors.As(err, &ae) && ae.Code == want
}
