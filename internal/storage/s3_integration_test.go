//go:build integration

package storage

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"os"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/nicoflow/nicoflow-api/internal/config"
)

// newMinIOClient builds a real client against MinIO. It skips the test unless
// TEST_S3_ENDPOINT is set (CI without a MinIO service just skips these), and
// ensures the bucket exists first.
func newMinIOClient(t *testing.T) *Client {
	t.Helper()
	endpoint := os.Getenv("TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("TEST_S3_ENDPOINT not set — skipping MinIO integration test")
	}
	cfg := config.Config{
		AWSRegion:      envOr("TEST_S3_REGION", "us-east-1"),
		AWSAccessKeyID: envOr("TEST_S3_ACCESS_KEY", "minioadmin"),
		AWSSecretKey:   envOr("TEST_S3_SECRET_KEY", "minioadmin"),
		S3Bucket:       envOr("TEST_S3_BUCKET", "nicoflow-attachments-test"),
		AWSEndpoint:    endpoint,
	}
	c, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !c.Enabled() {
		t.Fatal("client should be enabled with full config")
	}
	// Best-effort create the bucket (ignore "already exists").
	_, _ = c.api.CreateBucket(context.Background(), &s3.CreateBucketInput{Bucket: aws.String(cfg.S3Bucket)})
	return c
}

func envOr(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}

// postFile builds the multipart form from a PostPolicy (fields first, file
// last) and POSTs it to the presigned URL, returning the HTTP status.
func postFile(t *testing.T, pp PostPolicy, body []byte) int {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range pp.Fields {
		if err := w.WriteField(k, v); err != nil {
			t.Fatalf("write field %s: %v", k, err)
		}
	}
	fw, err := w.CreateFormFile("file", "upload.bin")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fw.Write(body); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, pp.URL, &buf)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode
}

func TestMinIO_SignUploadHeadDelete(t *testing.T) {
	c := newMinIOClient(t)
	ctx := context.Background()
	key := NewObjectKey("u-int", "task", "t-int")
	png := []byte("\x89PNG\r\n\x1a\nfake-image-bytes")

	pp, err := c.PresignUpload(key, "image/png")
	if err != nil {
		t.Fatalf("PresignUpload: %v", err)
	}
	if status := postFile(t, pp, png); status < 200 || status >= 300 {
		t.Fatalf("valid upload POST status = %d, want 2xx", status)
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

func TestMinIO_RejectsWrongContentType(t *testing.T) {
	c := newMinIOClient(t)
	key := NewObjectKey("u-int", "task", "t-int")
	pp, err := c.PresignUpload(key, "image/png")
	if err != nil {
		t.Fatalf("PresignUpload: %v", err)
	}
	// Tamper: swap the Content-Type field to something the policy didn't sign.
	pp.Fields["Content-Type"] = "application/x-evil"
	if status := postFile(t, pp, []byte("data")); status < 400 {
		t.Fatalf("tampered Content-Type POST status = %d, want 4xx (S3 rejects)", status)
	}
}

func TestMinIO_RejectsOversize(t *testing.T) {
	c := newMinIOClient(t)
	key := NewObjectKey("u-int", "task", "t-int")
	pp, err := c.PresignUpload(key, "application/pdf")
	if err != nil {
		t.Fatalf("PresignUpload: %v", err)
	}
	oversize := make([]byte, maxUploadBytes+1)
	if status := postFile(t, pp, oversize); status < 400 {
		t.Fatalf("oversize POST status = %d, want 4xx (S3 rejects via content-length-range)", status)
	}
}
