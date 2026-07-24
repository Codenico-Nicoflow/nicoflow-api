// Package storage provides the object-storage client and presigned URL
// generation for file attachments (E-024). The backend is S3-compatible:
// Cloudflare R2 in staging/prod and MinIO locally — the same client speaks to
// both (and to raw AWS S3) via aws-sdk-go-v2.
//
// The feature is config-gated: if any of the four STORAGE_* env vars is unset
// the constructor returns a disabled client whose operations report
// ErrStorageDisabled. Handlers translate that into a typed 503 — a silent
// no-op is never acceptable here, because a swallowed upload is data loss.
package storage

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/nicoflow/nicoflow-api/internal/config"
)

// ErrStorageDisabled is returned by every operation when the storage feature is
// not configured. Callers map it to a 503 SERVICE_UNAVAILABLE.
var ErrStorageDisabled = errors.New("storage: file attachments feature is disabled (storage not configured)")

// maxUploadBytes caps an upload at 20 MB — enforced by the S3 POST policy, not
// just the API, so the bytes never reach us.
const maxUploadBytes int64 = 20 << 20

// downloadTTL is how long a presigned GET (download) URL stays valid.
const downloadTTL = time.Hour

// Client wraps S3 for presigned uploads/downloads and object head/delete.
// A zero-value / disabled Client (Enabled()==false) is safe to hold; its
// operations all return ErrStorageDisabled.
type Client struct {
	api         *s3.Client
	presign     *s3.PresignClient
	bucket      string
	region      string
	accessKeyID string
	secretKey   string
	// endpoint is the storage endpoint override (R2 or MinIO) or "" for real AWS S3.
	endpoint string
	// now is injected so tests can pin the signing clock; defaults to time.Now.
	now func() time.Time
}

// New builds the storage client from config. When any of STORAGE_REGION,
// STORAGE_ACCESS_KEY_ID, STORAGE_SECRET_ACCESS_KEY, or STORAGE_BUCKET is empty
// the feature is disabled and a disabled client is returned (nil error) — the
// caller decides the 503 at the request boundary, so boot never fails just
// because storage isn't set up.
func New(ctx context.Context, cfg config.Config) (*Client, error) {
	if cfg.StorageRegion == "" || cfg.StorageAccessKeyID == "" || cfg.StorageSecretKey == "" || cfg.StorageBucket == "" {
		return &Client{}, nil
	}

	awsConfig, err := awscfg.LoadDefaultConfig(ctx,
		awscfg.WithRegion(cfg.StorageRegion),
		awscfg.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.StorageAccessKeyID, cfg.StorageSecretKey, ""),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("storage: load aws config: %w", err)
	}

	api := s3.NewFromConfig(awsConfig, func(o *s3.Options) {
		// MinIO (and any non-AWS S3) needs an explicit endpoint + path-style
		// addressing, since virtual-host buckets require AWS DNS.
		if cfg.StorageEndpoint != "" {
			o.BaseEndpoint = aws.String(cfg.StorageEndpoint)
			o.UsePathStyle = true
		}
	})

	return &Client{
		api:         api,
		presign:     s3.NewPresignClient(api),
		bucket:      cfg.StorageBucket,
		region:      cfg.StorageRegion,
		accessKeyID: cfg.StorageAccessKeyID,
		secretKey:   cfg.StorageSecretKey,
		endpoint:    cfg.StorageEndpoint,
		now:         time.Now,
	}, nil
}

// Enabled reports whether the storage feature is configured.
func (c *Client) Enabled() bool { return c.api != nil }

// PresignUpload returns a presigned POST policy the browser posts a file to.
// contentType must already be validated against the MIME allowlist by the
// caller; S3 pins it exactly. maxBytes defaults to 20 MB.
func (c *Client) PresignUpload(key, contentType string) (PostPolicy, error) {
	if !c.Enabled() {
		return PostPolicy{}, ErrStorageDisabled
	}
	return c.buildPostPolicy(key, contentType, maxUploadBytes)
}

// PresignDownload returns a presigned GET URL that forces a download with the
// given filename (never inline render, so a stored HTML/SVG can't execute in the
// user's origin). filename is sanitized before it reaches the header.
func (c *Client) PresignDownload(ctx context.Context, key, filename string) (string, error) {
	if !c.Enabled() {
		return "", ErrStorageDisabled
	}
	disposition := fmt.Sprintf("attachment; filename*=UTF-8''%s", url.PathEscape(sanitizeFilename(filename)))
	req, err := c.presign.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket:                     aws.String(c.bucket),
		Key:                        aws.String(key),
		ResponseContentDisposition: aws.String(disposition),
	}, s3.WithPresignExpires(downloadTTL))
	if err != nil {
		return "", fmt.Errorf("storage: presign download: %w", err)
	}
	return req.URL, nil
}

// HeadResult is the trustworthy object metadata read back from S3 at confirm
// time — the API stores these, never the client's claimed values.
type HeadResult struct {
	ContentLength int64
	ContentType   string
}

// Head returns the real size and content-type S3 recorded for the object. Used
// at confirm to re-validate an upload instead of trusting the client.
func (c *Client) Head(ctx context.Context, key string) (HeadResult, error) {
	if !c.Enabled() {
		return HeadResult{}, ErrStorageDisabled
	}
	out, err := c.api.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return HeadResult{}, fmt.Errorf("storage: head object: %w", err)
	}
	res := HeadResult{}
	if out.ContentLength != nil {
		res.ContentLength = *out.ContentLength
	}
	if out.ContentType != nil {
		res.ContentType = *out.ContentType
	}
	return res, nil
}

// Delete removes an object. It is idempotent: deleting a missing key is not an
// error (S3 DeleteObject returns 204 either way), so cleanup can run safely
// after a partial failure.
func (c *Client) Delete(ctx context.Context, key string) error {
	if !c.Enabled() {
		return ErrStorageDisabled
	}
	_, err := c.api.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		// A NoSuchKey is still a success for idempotent delete.
		if _, ok := errors.AsType[*types.NoSuchKey](err); ok {
			return nil
		}
		return fmt.Errorf("storage: delete object: %w", err)
	}
	return nil
}

// bucketURL is the POST target for browser uploads: path-style for MinIO/custom
// endpoints, virtual-host style for real AWS S3.
func (c *Client) bucketURL() string {
	if c.endpoint != "" {
		return strings.TrimRight(c.endpoint, "/") + "/" + c.bucket
	}
	return fmt.Sprintf("https://%s.s3.%s.amazonaws.com", c.bucket, c.region)
}
