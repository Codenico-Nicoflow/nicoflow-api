package task

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
)

// SubtaskRepository is the data-access contract for subtasks. Ownership is
// always enforced through the parent task's user_id.
type SubtaskRepository interface {
	TaskOwned(ctx context.Context, userID, taskID string) (bool, error)
	ListByTask(ctx context.Context, taskID string) ([]Subtask, error)
	Create(ctx context.Context, s Subtask) (Subtask, error)
	Update(ctx context.Context, userID, taskID, id string, req UpdateSubtaskRequest) (Subtask, error)
	Delete(ctx context.Context, userID, taskID, id string) error
	NextPosition(ctx context.Context, taskID string) (int, error)
}

type pgSubtaskRepo struct{ db *pgxpool.Pool }

// NewSubtaskRepository returns a postgres-backed SubtaskRepository.
func NewSubtaskRepository(db *pgxpool.Pool) SubtaskRepository { return &pgSubtaskRepo{db: db} }

const subtaskCols = ` id, task_id, title, done, position, created_at, updated_at `

func scanSubtask(row pgx.Row, s *Subtask) error {
	return row.Scan(&s.ID, &s.TaskID, &s.Title, &s.Done, &s.Position, &s.CreatedAt, &s.UpdatedAt)
}

func (r *pgSubtaskRepo) TaskOwned(ctx context.Context, userID, taskID string) (bool, error) {
	var exists bool
	err := r.db.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM tasks WHERE id = @id AND user_id = @userID)`,
		pgx.NamedArgs{"id": taskID, "userID": userID},
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("subtask.TaskOwned: %w", err)
	}
	return exists, nil
}

func (r *pgSubtaskRepo) ListByTask(ctx context.Context, taskID string) ([]Subtask, error) {
	rows, err := r.db.Query(ctx,
		`SELECT`+subtaskCols+`FROM subtasks WHERE task_id = @taskID ORDER BY position ASC, id ASC`,
		pgx.NamedArgs{"taskID": taskID},
	)
	if err != nil {
		return nil, fmt.Errorf("subtask.ListByTask: %w", err)
	}
	defer rows.Close()

	var subtasks []Subtask
	for rows.Next() {
		var s Subtask
		if err := scanSubtask(rows, &s); err != nil {
			return nil, fmt.Errorf("subtask.ListByTask scan: %w", err)
		}
		subtasks = append(subtasks, s)
	}
	return subtasks, rows.Err()
}

func (r *pgSubtaskRepo) Create(ctx context.Context, s Subtask) (Subtask, error) {
	err := scanSubtask(
		r.db.QueryRow(ctx, `
			INSERT INTO subtasks (id, task_id, title, done, position, created_at, updated_at)
			VALUES (@id, @taskID, @title, @done, @position, NOW(), NOW())
			RETURNING`+subtaskCols,
			pgx.NamedArgs{"id": s.ID, "taskID": s.TaskID, "title": s.Title, "done": s.Done, "position": s.Position},
		),
		&s,
	)
	if err != nil {
		return Subtask{}, fmt.Errorf("subtask.Create: %w", err)
	}
	return s, nil
}

func (r *pgSubtaskRepo) Update(ctx context.Context, userID, taskID, id string, req UpdateSubtaskRequest) (Subtask, error) {
	var s Subtask
	err := scanSubtask(
		r.db.QueryRow(ctx, `
			UPDATE subtasks SET
				title      = COALESCE(@title, title),
				done       = COALESCE(@done, done),
				position   = COALESCE(@position, position),
				updated_at = NOW()
			WHERE id = @id AND task_id = @taskID
			  AND task_id IN (SELECT id FROM tasks WHERE id = @taskID AND user_id = @userID)
			RETURNING`+subtaskCols,
			pgx.NamedArgs{"title": req.Title, "done": req.Done, "position": req.Position, "id": id, "taskID": taskID, "userID": userID},
		),
		&s,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Subtask{}, apperror.New(http.StatusNotFound, apperror.ErrResourceNotFound, "subtask not found")
		}
		return Subtask{}, fmt.Errorf("subtask.Update: %w", err)
	}
	return s, nil
}

func (r *pgSubtaskRepo) Delete(ctx context.Context, userID, taskID, id string) error {
	tag, err := r.db.Exec(ctx, `
		DELETE FROM subtasks
		WHERE id = @id AND task_id = @taskID
		  AND task_id IN (SELECT id FROM tasks WHERE id = @taskID AND user_id = @userID)`,
		pgx.NamedArgs{"id": id, "taskID": taskID, "userID": userID},
	)
	if err != nil {
		return fmt.Errorf("subtask.Delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return apperror.New(http.StatusNotFound, apperror.ErrResourceNotFound, "subtask not found")
	}
	return nil
}

func (r *pgSubtaskRepo) NextPosition(ctx context.Context, taskID string) (int, error) {
	var next int
	err := r.db.QueryRow(ctx,
		`SELECT COALESCE(MAX(position), -1) + 1 FROM subtasks WHERE task_id = @taskID`,
		pgx.NamedArgs{"taskID": taskID},
	).Scan(&next)
	if err != nil {
		return 0, fmt.Errorf("subtask.NextPosition: %w", err)
	}
	return next, nil
}
