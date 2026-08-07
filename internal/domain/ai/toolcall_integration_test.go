//go:build integration

package ai_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/internal/domain/ai"
	"github.com/nicoflow/nicoflow-api/internal/testutil"
)

// TestRepo_ToolCallLifecycle drives the ai_tool_calls table end-to-end:
// insert pending, list pending, claim → confirmed (idempotency guard), save
// result, and expire the still-pending ones.
func TestRepo_ToolCallLifecycle(t *testing.T) {
	pool := testutil.NewTestDB(t)
	cleanAITestData(t, pool)
	t.Cleanup(func() { cleanAITestData(t, pool) })

	userID := seedUser(t, pool, "pro")
	repo := ai.NewRepository(pool)

	sess, err := repo.CreateSession(context.Background(), ai.Session{
		ID: uuid.New().String(), UserID: userID, Title: "T",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Persist an assistant message the tool_calls FK will hang off.
	asstID := uuid.New().String()
	if err := repo.AppendAssistantMessageWithBlocks(
		context.Background(), sess.ID, asstID, "", json.RawMessage(`[]`),
	); err != nil {
		t.Fatal(err)
	}

	tc := ai.ToolCall{
		ID: uuid.New().String(), SessionID: sess.ID, UserID: userID,
		AssistantMessageID: asstID, ToolUseID: "tu_1", ToolName: ai.ToolCompleteTask,
		InputJSON: json.RawMessage(`{"taskId":"t1"}`),
	}
	if err := repo.InsertPending(context.Background(), tc); err != nil {
		t.Fatal(err)
	}

	// It's listed as pending.
	pending, err := repo.ListPendingForSession(context.Background(), userID, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].ToolUseID != "tu_1" {
		t.Fatalf("pending list = %+v", pending)
	}

	// First claim: succeeds and flips to confirmed.
	claimed, err := repo.ClaimPending(context.Background(), sess.ID, "tu_1", userID, ai.ToolCallStatusConfirmed)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.Status != ai.ToolCallStatusConfirmed {
		t.Errorf("status = %q, want confirmed", claimed.Status)
	}

	// Second claim: 409 CONFLICT — the guarded UPDATE returns 0 rows.
	_, err = repo.ClaimPending(context.Background(), sess.ID, "tu_1", userID, ai.ToolCallStatusConfirmed)
	var ae *apperror.AppError
	if !errors.As(err, &ae) || ae.Status != http.StatusConflict {
		t.Fatalf("second claim must be 409 CONFLICT, got %v", err)
	}

	// Save result.
	if err := repo.SaveResult(context.Background(), claimed.ID, json.RawMessage(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}

	// Expiry: seed a second pending row, roll its created_at back 8 days, run.
	old := ai.ToolCall{
		ID: uuid.New().String(), SessionID: sess.ID, UserID: userID,
		AssistantMessageID: asstID, ToolUseID: "tu_2", ToolName: ai.ToolRescheduleTask,
		InputJSON: json.RawMessage(`{"taskId":"t2"}`),
	}
	if err := repo.InsertPending(context.Background(), old); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(),
		`UPDATE ai_tool_calls SET created_at = NOW() - INTERVAL '8 days' WHERE id = $1`, old.ID,
	); err != nil {
		t.Fatal(err)
	}
	n, err := repo.ExpirePendingOlderThan(context.Background(), time.Now().AddDate(0, 0, -7))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("expired = %d, want 1", n)
	}
}
