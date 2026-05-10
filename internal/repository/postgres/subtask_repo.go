package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nicoflow/nicoflow-api/internal/model"
)

type SubtaskRepo struct {
	db *pgxpool.Pool
}

func NewSubtaskRepo(db *pgxpool.Pool) *SubtaskRepo {
	return &SubtaskRepo{db: db}
}

func (r *SubtaskRepo) Create(ctx context.Context, s *model.Subtask) error {
	_, err := r.db.Exec(ctx,
		`INSERT INTO subtasks (id, task_id, title, done, position, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		s.ID, s.TaskID, s.Title, s.Done, s.Position, s.CreatedAt, s.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("SubtaskRepo.Create: %w", err)
	}
	return nil
}

func (r *SubtaskRepo) ListByTask(ctx context.Context, taskID string) ([]model.Subtask, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id, task_id, title, done, position, created_at, updated_at
		 FROM subtasks WHERE task_id = $1 ORDER BY position ASC`,
		taskID,
	)
	if err != nil {
		return nil, fmt.Errorf("SubtaskRepo.ListByTask: %w", err)
	}
	defer rows.Close()

	var result []model.Subtask
	for rows.Next() {
		var s model.Subtask
		if err := rows.Scan(&s.ID, &s.TaskID, &s.Title, &s.Done, &s.Position, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("SubtaskRepo.ListByTask scan: %w", err)
		}
		result = append(result, s)
	}
	return result, rows.Err()
}

func (r *SubtaskRepo) FindByID(ctx context.Context, id string) (*model.Subtask, error) {
	s := &model.Subtask{}
	err := r.db.QueryRow(ctx,
		`SELECT id, task_id, title, done, position, created_at, updated_at
		 FROM subtasks WHERE id = $1`,
		id,
	).Scan(&s.ID, &s.TaskID, &s.Title, &s.Done, &s.Position, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("SubtaskRepo.FindByID: %w", err)
	}
	return s, nil
}

func (r *SubtaskRepo) Update(ctx context.Context, s *model.Subtask) error {
	_, err := r.db.Exec(ctx,
		`UPDATE subtasks SET title=$1, done=$2, position=$3, updated_at=$4 WHERE id=$5`,
		s.Title, s.Done, s.Position, s.UpdatedAt, s.ID,
	)
	if err != nil {
		return fmt.Errorf("SubtaskRepo.Update: %w", err)
	}
	return nil
}

func (r *SubtaskRepo) Delete(ctx context.Context, id string) error {
	_, err := r.db.Exec(ctx, `DELETE FROM subtasks WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("SubtaskRepo.Delete: %w", err)
	}
	return nil
}
