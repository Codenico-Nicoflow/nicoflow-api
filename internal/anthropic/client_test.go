package anthropic

import (
	"context"
	"errors"
	"testing"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/internal/domain/ai"
)

func TestNew_EmptyKeyDisabled(t *testing.T) {
	if New("").Enabled() {
		t.Fatal("empty key must produce a disabled client")
	}
	if !New("sk-ant-xxx").Enabled() {
		t.Fatal("non-empty key must produce an enabled client")
	}
}

func TestStream_DisabledReturnsUnavailable(t *testing.T) {
	_, err := New("").Stream(context.Background(), ai.ChatRequest{Model: "m"})

	var appErr *apperror.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("want *apperror.AppError, got %T", err)
	}
	if appErr.Code != apperror.ErrAIUnavailable {
		t.Errorf("code = %q, want %q", appErr.Code, apperror.ErrAIUnavailable)
	}
}
