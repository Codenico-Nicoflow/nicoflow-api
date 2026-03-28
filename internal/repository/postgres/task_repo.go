package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"nicoflow-api/internal/model"
)

type TaskRepo struct {
	db *pgxpool.Pool
}

func NewTaskRepo(db *pgxpool.Pool) *TaskRepo {
	return &TaskRepo{db: db}
}

func (r *TaskRepo) Create(ctx context.Context, t *model.Task) error {
	_, err := r.db.Exec(ctx,
		`INSERT INTO tasks (id, user_id, project_id, title, notes, due_date, scheduled_for, status, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		t.ID, t.UserID, t.ProjectID, t.Title, t.Notes, t.DueDate, t.ScheduledFor, t.Status, t.CreatedAt, t.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("TaskRepo.Create: %w", err)
	}
	return nil
}

func (r *TaskRepo) List(ctx context.Context, userID string) ([]model.Task, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id, user_id, project_id, title, notes, due_date, scheduled_for, status, created_at, updated_at
		 FROM tasks WHERE user_id = $1 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("TaskRepo.List: %w", err)
	}
	defer rows.Close()

	var result []model.Task
	for rows.Next() {
		var t model.Task
		if err := rows.Scan(&t.ID, &t.UserID, &t.ProjectID, &t.Title, &t.Notes, &t.DueDate, &t.ScheduledFor, &t.Status, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, fmt.Errorf("TaskRepo.List scan: %w", err)
		}
		result = append(result, t)
	}
	return result, rows.Err()
}

func (r *TaskRepo) ListByProject(ctx context.Context, projectId string) ([]model.Task, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id, user_id, project_id, title, notes, due_date, scheduled_for, status, created_at, updated_at
		 FROM tasks WHERE project_id = $1 ORDER BY display_order ASC`,
		projectId,
	)
	if err != nil {
		return nil, fmt.Errorf("TaskRepo.ListByProject: %w", err)
	}
	defer rows.Close()

	var result []model.Task
	for rows.Next() {
		var t model.Task
		if err := rows.Scan(&t.ID, &t.UserID, &t.ProjectID, &t.Title, &t.Notes, &t.DueDate, &t.ScheduledFor, &t.Status, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, fmt.Errorf("TaskRepo.ListByProject scan: %w", err)
		}
		result = append(result, t)
	}
	return result, rows.Err()
}

func (r *TaskRepo) FindByID(ctx context.Context, id string) (*model.Task, error) {
	t := &model.Task{}
	err := r.db.QueryRow(ctx,
		`SELECT id, user_id, project_id, title, notes, due_date, scheduled_for, status, created_at, updated_at
		 FROM tasks WHERE id = $1`,
		id,
	).Scan(&t.ID, &t.UserID, &t.ProjectID, &t.Title, &t.Notes, &t.DueDate, &t.ScheduledFor, &t.Status, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("TaskRepo.FindByID: %w", err)
	}
	return t, nil
}

func (r *TaskRepo) Update(ctx context.Context, t *model.Task) error {
	_, err := r.db.Exec(ctx,
		`UPDATE tasks SET project_id=$1, title=$2, notes=$3, due_date=$4, scheduled_for=$5, status=$6, updated_at=$7 WHERE id=$8`,
		t.ProjectID, t.Title, t.Notes, t.DueDate, t.ScheduledFor, t.Status, t.UpdatedAt, t.ID,
	)
	if err != nil {
		return fmt.Errorf("TaskRepo.Update: %w", err)
	}
	return nil
}

func (r *TaskRepo) Delete(ctx context.Context, id string) error {
	_, err := r.db.Exec(ctx, `DELETE FROM tasks WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("TaskRepo.Delete: %w", err)
	}
	return nil
}
