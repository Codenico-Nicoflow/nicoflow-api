package jwtutil_test

import (
	"testing"
	"time"

	"github.com/nicoflow/nicoflow-api/pkg/jwtutil"
)

const testSecret = "test-secret-minimum-32-bytes-long!!"

func TestIssueAndParse_Roundtrip(t *testing.T) {
	signed, err := jwtutil.Issue("usr_123", "user@example.com", "free", testSecret, 15*time.Minute)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	claims, err := jwtutil.Parse(signed, testSecret)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if claims.Subject != "usr_123" {
		t.Errorf("Subject = %q, want %q", claims.Subject, "usr_123")
	}
	if claims.Email != "user@example.com" {
		t.Errorf("Email = %q, want %q", claims.Email, "user@example.com")
	}
	if claims.Plan != "free" {
		t.Errorf("Plan = %q, want %q", claims.Plan, "free")
	}
}

func TestParse_ExpiredToken(t *testing.T) {
	signed, err := jwtutil.Issue("usr_123", "user@example.com", "free", testSecret, -1*time.Minute)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	_, err = jwtutil.Parse(signed, testSecret)
	if err == nil {
		t.Fatal("Parse() expected error for expired token, got nil")
	}
}

func TestParse_WrongSecret(t *testing.T) {
	signed, err := jwtutil.Issue("usr_123", "user@example.com", "free", testSecret, 15*time.Minute)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	_, err = jwtutil.Parse(signed, "wrong-secret-also-minimum-32-bytes!!")
	if err == nil {
		t.Fatal("Parse() expected error for wrong secret, got nil")
	}
}

func TestParse_MalformedToken(t *testing.T) {
	_, err := jwtutil.Parse("not.a.jwt", testSecret)
	if err == nil {
		t.Fatal("Parse() expected error for malformed token, got nil")
	}
}
