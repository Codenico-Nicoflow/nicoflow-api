package storage

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// PostPolicy is the browser-upload form the client POSTs a file to. Url is the
// bucket endpoint; Fields are the exact form fields (including the signed
// policy) that must be sent as multipart/form-data with the file field last.
type PostPolicy struct {
	URL    string            `json:"url"`
	Fields map[string]string `json:"fields"`
}

// postPolicyTTL is how long a signed upload form stays valid.
const postPolicyTTL = 15 * time.Minute

// postPolicyDoc is the S3 POST policy JSON document. Conditions are pre-encoded
// so a single condition can be either an object (exact-match) or an array
// (content-length-range) without falling back to interface{}.
type postPolicyDoc struct {
	Expiration string            `json:"expiration"`
	Conditions []json.RawMessage `json:"conditions"`
}

// eqCondition encodes a single exact-match S3 policy condition {"field":"value"}.
func eqCondition(field, value string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{field: value})
	return b
}

// buildPostPolicy hand-builds a presigned S3 POST policy (aws-sdk-go-v2 has no
// first-class PresignPost). S3 itself enforces the conditions at upload time:
// exact key, exact content-type, and a content-length-range — so a client that
// tampers with size or type is rejected by S3, never reaching the API.
func (c *Client) buildPostPolicy(key, contentType string, maxBytes int64) (PostPolicy, error) {
	now := c.now().UTC()
	date := now.Format("20060102")
	amzDate := now.Format("20060102T150405Z")
	credential := fmt.Sprintf("%s/%s/%s/s3/aws4_request", c.accessKeyID, date, c.region)

	// The policy document S3 validates the upload against. Order is irrelevant to
	// S3; each condition must be satisfied exactly (eq) or within range. Each
	// condition is pre-marshalled to json.RawMessage so the document stays fully
	// typed (no interface{}) despite mixing object and array shapes.
	lengthRange := fmt.Sprintf(`["content-length-range",1,%d]`, maxBytes)
	conditions := []json.RawMessage{
		eqCondition("bucket", c.bucket),
		eqCondition("key", key),
		eqCondition("Content-Type", contentType),
		json.RawMessage(lengthRange),
		eqCondition("x-amz-algorithm", "AWS4-HMAC-SHA256"),
		eqCondition("x-amz-credential", credential),
		eqCondition("x-amz-date", amzDate),
	}
	policy := postPolicyDoc{
		Expiration: now.Add(postPolicyTTL).Format("2006-01-02T15:04:05Z"),
		Conditions: conditions,
	}

	raw, err := json.Marshal(policy)
	if err != nil {
		return PostPolicy{}, fmt.Errorf("marshal post policy: %w", err)
	}
	encoded := base64.StdEncoding.EncodeToString(raw)
	signature := c.signPolicy(encoded, date)

	fields := map[string]string{
		"key":              key,
		"Content-Type":     contentType,
		"x-amz-algorithm":  "AWS4-HMAC-SHA256",
		"x-amz-credential": credential,
		"x-amz-date":       amzDate,
		"policy":           encoded,
		"x-amz-signature":  signature,
	}

	return PostPolicy{URL: c.bucketURL(), Fields: fields}, nil
}

// signPolicy derives the AWS SigV4 signing key and HMACs the base64 policy with
// it, yielding the x-amz-signature value.
func (c *Client) signPolicy(encodedPolicy, date string) string {
	kDate := hmacSHA256([]byte("AWS4"+c.secretKey), date)
	kRegion := hmacSHA256(kDate, c.region)
	kService := hmacSHA256(kRegion, "s3")
	kSigning := hmacSHA256(kService, "aws4_request")
	return hex.EncodeToString(hmacSHA256(kSigning, encodedPolicy))
}

func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}
