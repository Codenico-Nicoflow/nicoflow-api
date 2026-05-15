package hashutil_test

import (
	"testing"

	"github.com/nicoflow/nicoflow-api/pkg/hashutil"
)

func TestHash_Roundtrip(t *testing.T) {
	plain := "MySecret1"
	hash, err := hashutil.Hash(plain)
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}
	if hash == plain {
		t.Fatal("Hash() returned plain text")
	}
	if !hashutil.Compare(hash, plain) {
		t.Fatal("Compare() returned false for valid password")
	}
}

func TestCompare_WrongPassword(t *testing.T) {
	hash, err := hashutil.Hash("Correct1")
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}
	if hashutil.Compare(hash, "Wrong123") {
		t.Fatal("Compare() returned true for wrong password")
	}
}

func TestCompare_EmptyPassword(t *testing.T) {
	hash, err := hashutil.Hash("Valid1Pass")
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}
	if hashutil.Compare(hash, "") {
		t.Fatal("Compare() returned true for empty password")
	}
}
