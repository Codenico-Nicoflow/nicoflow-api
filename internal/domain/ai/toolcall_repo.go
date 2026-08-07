package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
)

// ToolCallStatus lifecycle: pending → confirmed | rejected | expired.
const (
	ToolCallStatusPending   = "pending"
	ToolCallStatusConfirmed = "confirmed"
	ToolCallStatusRejected  = "rejected"
	ToolCallStatusExpired   = "expired"
)

// ToolCall is one persisted write-tool proposal. Read tools never produce rows.
type ToolCall struct {
	ID                 string
	SessionID          string
	UserID             string
	AssistantMessageID string
	ToolUseID          string
	ToolName           string
	InputJSON          json.RawMessage
	Status             string
	ResultJSON         json.RawMessage
	CreatedAt          time.Time
	ResolvedAt         *time.Time
}

// ToolCallView is the wire shape for GET .../tool-calls.
type ToolCallView struct {
	ID                 string          `json:"id"`
	SessionID          string          `json:"sessionId"`
	AssistantMessageID string          `json:"assistantMessageId"`
	ToolUseID          string          `json:"toolUseId"`
	ToolName           string          `json:"toolName"`
	Input              json.RawMessage `json:"input"`
	Status             string          `json:"status"`
	Result             json.RawMessage `json:"result,omitempty"`
	CreatedAt          time.Time       `json:"createdAt"`
	ResolvedAt         *time.Time      `json:"resolvedAt,omitempty"`
}

func toolCallToView(tc ToolCall) ToolCallView {
	return ToolCallView{
		ID: tc.ID, SessionID: tc.SessionID,
		AssistantMessageID: tc.AssistantMessageID,
		ToolUseID:          tc.ToolUseID,
		ToolName:           tc.ToolName,
		Input:              tc.InputJSON,
		Status:             tc.Status,
		Result:             tc.ResultJSON,
		CreatedAt:          tc.CreatedAt,
		ResolvedAt:         tc.ResolvedAt,
	}
}

// ToolCallRepository is the persistence seam for AI write-tool proposals.
// Defined here (the consumer) per the project's interface-ownership rule.
type ToolCallRepository interface {
	// InsertPending records one new pending proposal — one row per Claude
	// tool_use block. Returns ErrToolCallLimitReached if the session-turn
	// already has 8 rows sharing this assistant_message_id lineage.
	InsertPending(ctx context.Context, tc ToolCall) error
	// CountForAssistantMessage returns how many tool_call rows are already
	// associated with the given assistant message (the anti-abuse ceiling
	// prevents a runaway assistant from proposing forever).
	CountForAssistantMessage(ctx context.Context, sessionID, assistantMessageID string) (int, error)
	// ClaimPending atomically flips a pending row to the given status,
	// returning the row. 0 rows updated → 409 CONFLICT (already resolved,
	// expired, or the wrong session). The guarded UPDATE is the double-
	// click / race guard, not a pre-flight SELECT.
	ClaimPending(ctx context.Context, sessionID, toolUseID, userID, newStatus string) (ToolCall, error)
	// SaveResult stamps the executor's result_json on an already-resolved
	// row so a GET reflects what actually happened.
	SaveResult(ctx context.Context, id string, result json.RawMessage) error
	// ListPendingForSession returns pending proposals in a session, oldest
	// first — the "what am I waiting on?" list.
	ListPendingForSession(ctx context.Context, userID, sessionID string) ([]ToolCall, error)
	// GetByToolUseID reads one row by (session, tool_use_id), row-isolated
	// by user_id. 404 if missing.
	GetByToolUseID(ctx context.Context, userID, sessionID, toolUseID string) (ToolCall, error)
	// ExpirePendingOlderThan is the nightly sweep body — flips every
	// pending row older than cutoff to 'expired' and returns the count.
	ExpirePendingOlderThan(ctx context.Context, cutoff time.Time) (int, error)
}

// MaxToolCallsPerTurn is the hard cap on tool_call rows tied to a single
// assistant_message_id — beyond this the service stops proposing and returns
// a plain-text stop message instead.
const MaxToolCallsPerTurn = 8

// ErrToolCallLimitReached signals the per-turn ceiling was hit.
var ErrToolCallLimitReached = errors.New("tool call limit reached for this turn")

// pgToolCallRepo is embedded into the existing pgRepo so callers keep one
// Repository handle. Broken into its own file solely for readability — the
// receiver is still *pgRepo (defined in repository.go).

func (r *pgRepo) InsertPending(ctx context.Context, tc ToolCall) error {
	if tc.InputJSON == nil {
		tc.InputJSON = json.RawMessage(`{}`)
	}
	if _, err := r.db.Exec(ctx, `
		INSERT INTO ai_tool_calls
		    (id, session_id, user_id, assistant_message_id, tool_use_id, tool_name, input_json, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'pending', NOW())`,
		tc.ID, tc.SessionID, tc.UserID, tc.AssistantMessageID, tc.ToolUseID, tc.ToolName, tc.InputJSON,
	); err != nil {
		return fmt.Errorf("ai.InsertPendingToolCall: %w", err)
	}
	return nil
}

func (r *pgRepo) CountForAssistantMessage(ctx context.Context, sessionID, assistantMessageID string) (int, error) {
	var n int
	err := r.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM ai_tool_calls WHERE session_id = $1 AND assistant_message_id = $2`,
		sessionID, assistantMessageID,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("ai.CountToolCallsForAssistantMessage: %w", err)
	}
	return n, nil
}

func (r *pgRepo) ClaimPending(ctx context.Context, sessionID, toolUseID, userID, newStatus string) (ToolCall, error) {
	var tc ToolCall
	err := r.db.QueryRow(ctx, `
		UPDATE ai_tool_calls
		   SET status = $4, resolved_at = NOW()
		 WHERE session_id = $1 AND tool_use_id = $2 AND user_id = $3 AND status = 'pending'
		 RETURNING id, session_id, user_id, assistant_message_id, tool_use_id, tool_name, input_json, status, result_json, created_at, resolved_at`,
		sessionID, toolUseID, userID, newStatus,
	).Scan(&tc.ID, &tc.SessionID, &tc.UserID, &tc.AssistantMessageID, &tc.ToolUseID, &tc.ToolName, &tc.InputJSON, &tc.Status, &tc.ResultJSON, &tc.CreatedAt, &tc.ResolvedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ToolCall{}, apperror.New(http.StatusConflict, apperror.ErrConflict, "tool call is not pending")
	}
	if err != nil {
		return ToolCall{}, fmt.Errorf("ai.ClaimPendingToolCall: %w", err)
	}
	return tc, nil
}

func (r *pgRepo) SaveResult(ctx context.Context, id string, result json.RawMessage) error {
	if _, err := r.db.Exec(ctx,
		`UPDATE ai_tool_calls SET result_json = $2 WHERE id = $1`,
		id, result,
	); err != nil {
		return fmt.Errorf("ai.SaveToolCallResult: %w", err)
	}
	return nil
}

func (r *pgRepo) ListPendingForSession(ctx context.Context, userID, sessionID string) ([]ToolCall, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, session_id, user_id, assistant_message_id, tool_use_id, tool_name, input_json, status, result_json, created_at, resolved_at
		  FROM ai_tool_calls
		 WHERE session_id = $1 AND user_id = $2 AND status = 'pending'
		 ORDER BY created_at ASC, id ASC`,
		sessionID, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("ai.ListPendingToolCalls query: %w", err)
	}
	defer rows.Close()

	out := []ToolCall{}
	for rows.Next() {
		var tc ToolCall
		if err := rows.Scan(&tc.ID, &tc.SessionID, &tc.UserID, &tc.AssistantMessageID, &tc.ToolUseID, &tc.ToolName, &tc.InputJSON, &tc.Status, &tc.ResultJSON, &tc.CreatedAt, &tc.ResolvedAt); err != nil {
			return nil, fmt.Errorf("ai.ListPendingToolCalls scan: %w", err)
		}
		out = append(out, tc)
	}
	return out, rows.Err()
}

func (r *pgRepo) GetByToolUseID(ctx context.Context, userID, sessionID, toolUseID string) (ToolCall, error) {
	var tc ToolCall
	err := r.db.QueryRow(ctx, `
		SELECT id, session_id, user_id, assistant_message_id, tool_use_id, tool_name, input_json, status, result_json, created_at, resolved_at
		  FROM ai_tool_calls
		 WHERE session_id = $1 AND tool_use_id = $2 AND user_id = $3`,
		sessionID, toolUseID, userID,
	).Scan(&tc.ID, &tc.SessionID, &tc.UserID, &tc.AssistantMessageID, &tc.ToolUseID, &tc.ToolName, &tc.InputJSON, &tc.Status, &tc.ResultJSON, &tc.CreatedAt, &tc.ResolvedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ToolCall{}, apperror.New(http.StatusNotFound, apperror.ErrResourceNotFound, "tool call not found")
	}
	if err != nil {
		return ToolCall{}, fmt.Errorf("ai.GetToolCallByToolUseID: %w", err)
	}
	return tc, nil
}

func (r *pgRepo) ExpirePendingOlderThan(ctx context.Context, cutoff time.Time) (int, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE ai_tool_calls
		   SET status = 'expired', resolved_at = NOW()
		 WHERE status = 'pending' AND created_at < $1`,
		cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("ai.ExpirePendingToolCalls: %w", err)
	}
	return int(tag.RowsAffected()), nil
}
