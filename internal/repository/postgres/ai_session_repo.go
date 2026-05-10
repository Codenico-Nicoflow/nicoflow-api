package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nicoflow/nicoflow-api/internal/model"
)

type AISessionRepo struct {
	db *pgxpool.Pool
}

func NewAISessionRepo(db *pgxpool.Pool) *AISessionRepo {
	return &AISessionRepo{db: db}
}

func (r *AISessionRepo) Create(ctx context.Context, s *model.AISession) error {
	_, err := r.db.Exec(ctx,
		`INSERT INTO ai_sessions (id, user_id, title, status, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		s.ID, s.UserID, s.Title, s.Status, s.CreatedAt, s.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("AISessionRepo.Create: %w", err)
	}
	return nil
}

func (r *AISessionRepo) List(ctx context.Context, userID string) ([]model.AISession, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id, user_id, title, status, created_at, updated_at
		 FROM ai_sessions WHERE user_id = $1 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("AISessionRepo.List: %w", err)
	}
	defer rows.Close()

	var result []model.AISession
	for rows.Next() {
		var s model.AISession
		if err := rows.Scan(&s.ID, &s.UserID, &s.Title, &s.Status, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("AISessionRepo.List scan: %w", err)
		}
		result = append(result, s)
	}
	return result, rows.Err()
}

func (r *AISessionRepo) FindByID(ctx context.Context, id string) (*model.AISession, error) {
	s := &model.AISession{}
	err := r.db.QueryRow(ctx,
		`SELECT id, user_id, title, status, created_at, updated_at
		 FROM ai_sessions WHERE id = $1`,
		id,
	).Scan(&s.ID, &s.UserID, &s.Title, &s.Status, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("AISessionRepo.FindByID: %w", err)
	}
	return s, nil
}

func (r *AISessionRepo) Delete(ctx context.Context, id string) error {
	_, err := r.db.Exec(ctx, `DELETE FROM ai_sessions WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("AISessionRepo.Delete: %w", err)
	}
	return nil
}
