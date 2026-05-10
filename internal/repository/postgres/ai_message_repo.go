package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nicoflow/nicoflow-api/internal/model"
)

type AIMessageRepo struct {
	db *pgxpool.Pool
}

func NewAIMessageRepo(db *pgxpool.Pool) *AIMessageRepo {
	return &AIMessageRepo{db: db}
}

func (r *AIMessageRepo) Create(ctx context.Context, m *model.AIMessage) error {
	_, err := r.db.Exec(ctx,
		`INSERT INTO ai_messages (id, session_id, role, content, created_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		m.ID, m.SessionID, m.Role, m.Content, m.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("AIMessageRepo.Create: %w", err)
	}
	return nil
}

func (r *AIMessageRepo) ListBySession(ctx context.Context, sessionID string) ([]model.AIMessage, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id, session_id, role, content, created_at
		 FROM ai_messages WHERE session_id = $1 ORDER BY created_at ASC`,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("AIMessageRepo.ListBySession: %w", err)
	}
	defer rows.Close()

	var result []model.AIMessage
	for rows.Next() {
		var m model.AIMessage
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Content, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("AIMessageRepo.ListBySession scan: %w", err)
		}
		result = append(result, m)
	}
	return result, rows.Err()
}
