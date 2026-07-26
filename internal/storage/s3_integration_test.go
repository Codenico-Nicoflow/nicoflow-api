//go:build integration

package storage

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/nicoflow/nicoflow-api/internal/config"
)

// newStoreClient builds a real client against the configured S3-compatible store
// (MinIO locally, Cloudflare R2 in the NIC-1679 spike). It skips unless
// TEST_S3_ENDPOINT is set, and ensures the bucket exists first. Point it at R2
// with TEST_S3_ENDPOINT=https://<acct>.r2.cloudflarestorage.com + TEST_S3_REGION=auto.
func newStoreClient(t *testing.T) *Client {
	t.Helper()
	endpoint := os.Getenv("TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("TEST_S3_ENDPOINT not set — skipping storage integration test")
	}
	cfg := config.Config{
		StorageRegion:      envOr("TEST_S3_REGION", "us-east-1"),
		StorageAccessKeyID: envOr("TEST_S3_ACCESS_KEY", "minioadmin"),
		StorageSecretKey:   envOr("TEST_S3_SECRET_KEY", "minioadmin"),
		StorageBucket:      envOr("TEST_S3_BUCKET", "nicoflow-attachments-test"),
		StorageEndpoint:    endpoint,
	}
	c, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !c.Enabled() {
		t.Fatal("client should be enabled with full config")
	}
	// Best-effort create the bucket (ignore "already exists").
	_, _ = c.api.CreateBucket(context.Background(), &s3.CreateBucketInput{Bucket: aws.String(cfg.StorageBucket)})
	return c
}

func envOr(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}

// putFile PUTs the raw body to a presigned upload with the given Content-Type
// header, returning the HTTP status. Passing a contentType different from the one
// signed into the URL lets a test verify the store rejects the mismatch.
func putFile(t *testing.T, up PresignedUpload, contentType string, body []byte) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, up.URL, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode
}

func TestStore_SignUploadHeadDelete(t *testing.T) {
	c := newStoreClient(t)
	ctx := context.Background()
	key := NewObjectKey("u-int", "task", "t-int")
	png := []byte("\x89PNG\r\n\x1a\nfake-image-bytes")

	up, err := c.PresignUpload(key, "image/png")
	if err != nil {
		t.Fatalf("PresignUpload: %v", err)
	}
	// Send exactly the signed Content-Type.
	if status := putFile(t, up, up.Headers["Content-Type"], png); status < 200 || status >= 300 {
		t.Fatalf("valid upload PUT status = %d, want 2xx", status)
	}

	head, err := c.Head(ctx, key)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if head.ContentLength != int64(len(png)) {
		t.Errorf("Head ContentLength = %d, want %d", head.ContentLength, len(png))
	}
	if head.ContentType != "image/png" {
		t.Errorf("Head ContentType = %q, want image/png", head.ContentType)
	}

	if err := c.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	// Idempotent: deleting again is a no-op.
	if err := c.Delete(ctx, key); err != nil {
		t.Errorf("second Delete not idempotent: %v", err)
	}
}

// Content-Type is NOT part of the enforcement boundary at upload time on a
// presigned PUT: R2 accepts whatever Content-Type header the client sends (it
// doesn't sign the header into the URL like strict S3 does), so a mismatch is
// stored, not rejected. That's fine — the real guard is the confirm-time
// HeadObject re-read (attachment.Confirm rejects a disallowed MIME then). This
// test pins that contract: whatever type the client PUTs is what Head reads back,
// so confirm sees the true stored type — never the type the client *claimed* at
// upload-url. Size is likewise a confirm-time concern (PUT can't range-check).
func TestStore_HeadReflectsUploadedContentType(t *testing.T) {
	c := newStoreClient(t)
	ctx := context.Background()
	key := NewObjectKey("u-int", "task", "t-int")

	// Sign for image/png but PUT with a different Content-Type.
	up, err := c.PresignUpload(key, "image/png")
	if err != nil {
		t.Fatalf("PresignUpload: %v", err)
	}
	if status := putFile(t, up, "application/pdf", []byte("%PDF-1.4 data")); status < 200 || status >= 300 {
		t.Fatalf("PUT status = %d, want 2xx", status)
	}
	defer func() { _ = c.Delete(ctx, key) }()

	// Head reflects the ACTUAL uploaded type — confirm validates against this,
	// not the client's upload-url claim.
	head, err := c.Head(ctx, key)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if head.ContentType != "application/pdf" {
		t.Errorf("Head ContentType = %q, want application/pdf (the uploaded type)", head.ContentType)
	}
}
