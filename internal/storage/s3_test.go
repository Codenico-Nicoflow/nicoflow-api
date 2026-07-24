package storage

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nicoflow/nicoflow-api/internal/config"
)

// fixedClient returns an enabled client with a pinned clock so signatures and
// policy expiry are deterministic. It builds no real AWS client (nil api) —
// enough for the pure policy/key/signing paths, which never call S3.
func fixedClient() *Client {
	return &Client{
		bucket:      "nicoflow-attachments",
		region:      "us-east-1",
		accessKeyID: "AKIAEXAMPLE",
		secretKey:   "secretExampleKey",
		endpoint:    "http://localhost:9000",
		now:         func() time.Time { return time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC) },
	}
}

func TestNew_ConfigGate(t *testing.T) {
	full := config.Config{
		AWSRegion: "us-east-1", AWSAccessKeyID: "k", AWSSecretKey: "s", S3Bucket: "b",
	}
	tests := []struct {
		name        string
		mutate      func(c *config.Config)
		wantEnabled bool
	}{
		{"all set → enabled", func(*config.Config) {}, true},
		{"no region", func(c *config.Config) { c.AWSRegion = "" }, false},
		{"no access key", func(c *config.Config) { c.AWSAccessKeyID = "" }, false},
		{"no secret", func(c *config.Config) { c.AWSSecretKey = "" }, false},
		{"no bucket", func(c *config.Config) { c.S3Bucket = "" }, false},
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

func TestPresignUpload_PolicyConditions(t *testing.T) {
	// fixedClient has a nil api (Enabled()==false), so test the policy builder
	// directly — it's a pure function of config + inputs, no S3 call.
	c := fixedClient()
	pp, err := c.buildPostPolicy("attachments/u1/task/t1/abc", "image/png", maxUploadBytes)
	if err != nil {
		t.Fatalf("buildPostPolicy: %v", err)
	}

	// Required form fields are present.
	for _, f := range []string{"key", "Content-Type", "x-amz-algorithm", "x-amz-credential", "x-amz-date", "policy", "x-amz-signature"} {
		if _, ok := pp.Fields[f]; !ok {
			t.Errorf("missing form field %q", f)
		}
	}
	if pp.Fields["Content-Type"] != "image/png" {
		t.Errorf("Content-Type field = %q, want image/png", pp.Fields["Content-Type"])
	}
	if pp.Fields["x-amz-algorithm"] != "AWS4-HMAC-SHA256" {
		t.Errorf("algorithm = %q", pp.Fields["x-amz-algorithm"])
	}
	if want := "http://localhost:9000/nicoflow-attachments"; pp.URL != want {
		t.Errorf("URL = %q, want %q", pp.URL, want)
	}

	// Decode the policy and assert the conditions S3 will enforce.
	raw, err := base64.StdEncoding.DecodeString(pp.Fields["policy"])
	if err != nil {
		t.Fatalf("decode policy: %v", err)
	}
	var doc struct {
		Conditions []json.RawMessage `json:"conditions"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal policy: %v", err)
	}
	blob := string(raw)
	if !strings.Contains(blob, `"content-length-range"`) {
		t.Error("policy missing content-length-range condition")
	}
	if !strings.Contains(blob, "20971520") { // 20 MiB
		t.Errorf("policy missing 20MB max in content-length-range: %s", blob)
	}
	if !strings.Contains(blob, `"Content-Type":"image/png"`) {
		t.Errorf("policy missing exact Content-Type condition: %s", blob)
	}
	if !strings.Contains(blob, `"key":"attachments/u1/task/t1/abc"`) {
		t.Errorf("policy missing exact key condition: %s", blob)
	}
}

func TestSignPolicy_Deterministic(t *testing.T) {
	c := fixedClient()
	sig1 := c.signPolicy("dGVzdA==", "20260724")
	sig2 := c.signPolicy("dGVzdA==", "20260724")
	if sig1 != sig2 {
		t.Fatal("signPolicy not deterministic for same input")
	}
	if len(sig1) != 64 { // hex-encoded SHA-256
		t.Errorf("signature length = %d, want 64 hex chars", len(sig1))
	}
	if c.signPolicy("dGVzdA==", "20260725") == sig1 {
		t.Error("signature must differ when the signing date differs")
	}
}
