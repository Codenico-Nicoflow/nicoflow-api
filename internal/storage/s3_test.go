package storage

import (
	"context"
	"strings"
	"testing"

	"github.com/nicoflow/nicoflow-api/internal/config"
)

func TestNew_ConfigGate(t *testing.T) {
	full := config.Config{
		StorageRegion: "us-east-1", StorageAccessKeyID: "k", StorageSecretKey: "s", StorageBucket: "b",
	}
	tests := []struct {
		name        string
		mutate      func(c *config.Config)
		wantEnabled bool
	}{
		{"all set → enabled", func(*config.Config) {}, true},
		{"no region", func(c *config.Config) { c.StorageRegion = "" }, false},
		{"no access key", func(c *config.Config) { c.StorageAccessKeyID = "" }, false},
		{"no secret", func(c *config.Config) { c.StorageSecretKey = "" }, false},
		{"no bucket", func(c *config.Config) { c.StorageBucket = "" }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := full
			tt.mutate(&cfg)
			c, err := New(context.Background(), cfg)
			if err != nil {
				t.Fatalf("New: unexpected error: %v", err)
			}
			if got := c.Enabled(); got != tt.wantEnabled {
				t.Fatalf("Enabled() = %v, want %v", got, tt.wantEnabled)
			}
		})
	}
}

func TestDisabledClient_OperationsReturnErrStorageDisabled(t *testing.T) {
	c := &Client{} // disabled
	if _, err := c.PresignUpload("k", "image/png"); err != ErrStorageDisabled {
		t.Errorf("PresignUpload err = %v, want ErrStorageDisabled", err)
	}
	if _, err := c.PresignDownload(context.Background(), "k", "f.png"); err != ErrStorageDisabled {
		t.Errorf("PresignDownload err = %v, want ErrStorageDisabled", err)
	}
	if _, err := c.Head(context.Background(), "k"); err != ErrStorageDisabled {
		t.Errorf("Head err = %v, want ErrStorageDisabled", err)
	}
	if err := c.Delete(context.Background(), "k"); err != ErrStorageDisabled {
		t.Errorf("Delete err = %v, want ErrStorageDisabled", err)
	}
}

// PresignUpload on an enabled client returns a presigned PUT URL that pins
// Content-Type in the required headers. The live round-trip is covered by the
// integration suite; this asserts the shape without hitting a store.
func TestPresignUpload_ReturnsPutURLWithContentType(t *testing.T) {
	c, err := New(context.Background(), config.Config{
		StorageRegion: "us-east-1", StorageAccessKeyID: "AKIAEXAMPLE", StorageSecretKey: "secretExampleKey",
		StorageBucket: "nicoflow-attachments", StorageEndpoint: "http://localhost:9000",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	up, err := c.PresignUpload("attachments/u1/task/t1/abc", "image/png")
	if err != nil {
		t.Fatalf("PresignUpload: %v", err)
	}
	if up.URL == "" {
		t.Error("PresignUpload URL is empty")
	}
	if got := up.Headers["Content-Type"]; got != "image/png" {
		t.Errorf("Content-Type header = %q, want image/png", got)
	}
	if !strings.Contains(up.URL, "attachments/u1/task/t1/abc") {
		t.Errorf("URL %q does not reference the object key", up.URL)
	}
	if !strings.Contains(up.URL, "X-Amz-Signature") {
		t.Errorf("URL %q is not a presigned (SigV4) URL", up.URL)
	}
}
