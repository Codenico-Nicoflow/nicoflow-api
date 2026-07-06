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

// Repository defines the data-access contract for the task domain.
type Repository interface {
	ListByProject(ctx context.Context, userID, projectID string, f ListTasksFilter) ([]Task, error)
	GetByID(ctx context.Context, userID, id string) (*Task, error)
	Create(ctx context.Context, t Task) (Task, error)
	Update(ctx context.Context, userID, id string, req UpdateTaskRequest, completedAt completedAtChange) (Task, error)
	Delete(ctx context.Context, userID, id string) error
	// ProjectOwned reports whether the project exists and belongs to the user.
	ProjectOwned(ctx context.Context, userID, projectID string) (bool, error)
	// CountActiveInbox counts only active+inbox tasks in a project (the calm plan limit).
	CountActiveInbox(ctx context.Context, userID, projectID string) (int, error)
	// NextDisplayOrder returns the order to append a new task at the end of a project.
	NextDisplayOrder(ctx context.Context, userID, projectID string) (int, error)
	// UpdateSchedule sets (scheduledFor=nil clears) the soft schedule + optional rollsOver.
	UpdateSchedule(ctx context.Context, userID, id string, scheduledFor *string, rollsOver *bool) (Task, error)
	// Repack moves a task to targetOrder and renumbers its project siblings 0..n-1.
	Repack(ctx context.Context, userID, id string, targetOrder int) (Task, error)
	// ListActiveInboxByUser returns the user's active+inbox tasks across ALL
	// projects — the candidate set for Focus and Time-Spread.
	ListActiveInboxByUser(ctx context.Context, userID string) ([]Task, error)
}

type pgRepo struct{ db *pgxpool.Pool }

// NewRepository creates a new postgres-backed task repository.
func NewRepository(db *pgxpool.Pool) Repository { return &pgRepo{db: db} }

const taskSelectCols = ` id, user_id, project_id, title, notes, status, priority, energy,
	rolls_over, scheduled_for, estimated_minutes, url, display_order,
	completed_at, created_at, updated_at `

func scanTask(row pgx.Row, t *Task) error {
	return row.Scan(
		&t.ID, &t.UserID, &t.ProjectID, &t.Title, &t.Notes, &t.Status, &t.Priority, &t.Energy,
		&t.RollsOver, &t.ScheduledFor, &t.EstimatedMinutes, &t.URL, &t.DisplayOrder,
		&t.CompletedAt, &t.CreatedAt, &t.UpdatedAt,
	)
}

func (r *pgRepo) ListByProject(ctx context.Context, userID, projectID string, f ListTasksFilter) ([]Task, error) {
	suffix, args, err := buildListQuery(userID, projectID, f)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx, `SELECT`+taskSelectCols+`FROM tasks`+suffix, args)
	if err != nil {
		return nil, fmt.Errorf("task.ListByProject query: %w", err)
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var t Task
		if err := scanTask(rows, &t); err != nil {
			return nil, fmt.Errorf("task.ListByProject scan: %w", err)
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func (r *pgRepo) ListActiveInboxByUser(ctx context.Context, userID string) ([]Task, error) {
	rows, err := r.db.Query(ctx,
		`SELECT`+taskSelectCols+`FROM tasks
		 WHERE user_id = @userID AND status IN ('active', 'inbox')`,
		pgx.NamedArgs{"userID": userID},
	)
	if err != nil {
		return nil, fmt.Errorf("task.ListActiveInboxByUser: %w", err)
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var t Task
		if err := scanTask(rows, &t); err != nil {
			return nil, fmt.Errorf("task.ListActiveInboxByUser scan: %w", err)
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func (r *pgRepo) GetByID(ctx context.Context, userID, id string) (*Task, error) {
	var t Task
	err := scanTask(
		r.db.QueryRow(ctx,
			`SELECT`+taskSelectCols+`FROM tasks WHERE id = @id AND user_id = @userID`,
			pgx.NamedArgs{"id": id, "userID": userID},
		),
		&t,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperror.New(http.StatusNotFound, apperror.ErrTaskNotFound, "task not found")
		}
		return nil, fmt.Errorf("task.GetByID: %w", err)
	}
	return &t, nil
}

func (r *pgRepo) Create(ctx context.Context, t Task) (Task, error) {
	err := scanTask(
		r.db.QueryRow(ctx, `
			INSERT INTO tasks
				(id, user_id, project_id, title, notes, status, priority, energy, rolls_over,
				 scheduled_for, estimated_minutes, url, display_order, completed_at,
				 created_at, updated_at)
			VALUES
				(@id, @userID, @projectID, @title, @notes, @status, @priority, @energy, @rollsOver,
				 @scheduledFor, @estimatedMinutes, @url, @displayOrder, @completedAt,
				 NOW(), NOW())
			RETURNING`+taskSelectCols,
			pgx.NamedArgs{
				"id":               t.ID,
				"userID":           t.UserID,
				"projectID":        t.ProjectID,
				"title":            t.Title,
				"notes":            t.Notes,
				"status":           t.Status,
				"priority":         t.Priority,
				"energy":           t.Energy,
				"rollsOver":        t.RollsOver,
				"scheduledFor":     t.ScheduledFor,
				"estimatedMinutes": t.EstimatedMinutes,
				"url":              t.URL,
				"displayOrder":     t.DisplayOrder,
				"completedAt":      t.CompletedAt,
			},
		),
		&t,
	)
	if err != nil {
		if isForeignKeyViolation(err) {
			return Task{}, apperror.New(http.StatusNotFound, apperror.ErrProjectNotFound, "project not found or does not belong to you")
		}
		return Task{}, fmt.Errorf("task.Create: %w", err)
	}
	return t, nil
}

// completedAtChange tells the repo how to update completed_at on a PATCH:
// leave it, set it to NOW(), or clear it to NULL.
type completedAtChange int

const (
	completedAtKeep completedAtChange = iota
	completedAtSetNow
	completedAtClear
)

func (r *pgRepo) Update(ctx context.Context, userID, id string, req UpdateTaskRequest, completedAt completedAtChange) (Task, error) {
	var completedExpr string
	switch completedAt {
	case completedAtSetNow:
		completedExpr = "NOW()"
	case completedAtClear:
		completedExpr = "NULL"
	default:
		completedExpr = "completed_at"
	}

	var t Task
	err := scanTask(
		r.db.QueryRow(ctx, `
			UPDATE tasks SET
				title             = COALESCE(@title, title),
				status            = COALESCE(@status, status),
				priority          = COALESCE(@priority, priority),
				energy            = COALESCE(@energy, energy),
				rolls_over        = COALESCE(@rollsOver, rolls_over),
				notes             = CASE WHEN @notesSet THEN @notes ELSE notes END,
				scheduled_for     = CASE WHEN @scheduledForSet THEN @scheduledFor ELSE scheduled_for END,
				estimated_minutes = CASE WHEN @estimatedMinutesSet THEN @estimatedMinutes ELSE estimated_minutes END,
				url               = CASE WHEN @urlSet THEN @url ELSE url END,
				completed_at      = `+completedExpr+`,
				updated_at        = NOW()
			WHERE id = @id AND user_id = @userID
			RETURNING`+taskSelectCols,
			pgx.NamedArgs{
				"title":               req.Title,
				"status":              req.Status,
				"priority":            req.Priority,
				"energy":              req.Energy,
				"rollsOver":           req.RollsOver,
				"notes":               req.Notes.Value,
				"notesSet":            req.Notes.Set,
				"scheduledFor":        req.ScheduledFor.Value,
				"scheduledForSet":     req.ScheduledFor.Set,
				"estimatedMinutes":    req.EstimatedMinutes.Value,
				"estimatedMinutesSet": req.EstimatedMinutes.Set,
				"url":                 req.URL.Value,
				"urlSet":              req.URL.Set,
				"id":                  id,
				"userID":              userID,
			},
		),
		&t,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Task{}, apperror.New(http.StatusNotFound, apperror.ErrTaskNotFound, "task not found")
		}
		return Task{}, fmt.Errorf("task.Update: %w", err)
	}
	return t, nil
}

func (r *pgRepo) Delete(ctx context.Context, userID, id string) error {
	tag, err := r.db.Exec(ctx,
		`DELETE FROM tasks WHERE id = @id AND user_id = @userID`,
		pgx.NamedArgs{"id": id, "userID": userID},
	)
	if err != nil {
		return fmt.Errorf("task.Delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return apperror.New(http.StatusNotFound, apperror.ErrTaskNotFound, "task not found")
	}
	return nil
}

func (r *pgRepo) ProjectOwned(ctx context.Context, userID, projectID string) (bool, error) {
	var exists bool
	err := r.db.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM projects WHERE id = @id AND user_id = @userID)`,
		pgx.NamedArgs{"id": projectID, "userID": userID},
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("task.ProjectOwned: %w", err)
	}
	return exists, nil
}

func (r *pgRepo) CountActiveInbox(ctx context.Context, userID, projectID string) (int, error) {
	var count int
	err := r.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM tasks
		 WHERE user_id = @userID AND project_id = @projectID AND status IN ('active', 'inbox')`,
		pgx.NamedArgs{"userID": userID, "projectID": projectID},
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("task.CountActiveInbox: %w", err)
	}
	return count, nil
}

func (r *pgRepo) NextDisplayOrder(ctx context.Context, userID, projectID string) (int, error) {
	var next int
	err := r.db.QueryRow(ctx,
		`SELECT COALESCE(MAX(display_order), -1) + 1 FROM tasks
		 WHERE user_id = @userID AND project_id = @projectID`,
		pgx.NamedArgs{"userID": userID, "projectID": projectID},
	).Scan(&next)
	if err != nil {
		return 0, fmt.Errorf("task.NextDisplayOrder: %w", err)
	}
	return next, nil
}

func (r *pgRepo) UpdateSchedule(ctx context.Context, userID, id string, scheduledFor *string, rollsOver *bool) (Task, error) {
	var t Task
	err := scanTask(
		r.db.QueryRow(ctx, `
			UPDATE tasks SET
				scheduled_for = @scheduledFor,
				rolls_over    = COALESCE(@rollsOver, rolls_over),
				updated_at    = NOW()
			WHERE id = @id AND user_id = @userID
			RETURNING`+taskSelectCols,
			pgx.NamedArgs{"scheduledFor": scheduledFor, "rollsOver": rollsOver, "id": id, "userID": userID},
		),
		&t,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Task{}, apperror.New(http.StatusNotFound, apperror.ErrTaskNotFound, "task not found")
		}
		return Task{}, fmt.Errorf("task.UpdateSchedule: %w", err)
	}
	return t, nil
}

// Repack moves the task to targetOrder within its project and renumbers all
// siblings to a contiguous 0..n-1 sequence, in a single transaction.
func (r *pgRepo) Repack(ctx context.Context, userID, id string, targetOrder int) (Task, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Task{}, fmt.Errorf("task.Repack begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var projectID string
	var moved Task
	if err := scanTask(
		tx.QueryRow(ctx, `SELECT`+taskSelectCols+`FROM tasks WHERE id = @id AND user_id = @userID FOR UPDATE`,
			pgx.NamedArgs{"id": id, "userID": userID}),
		&moved,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Task{}, apperror.New(http.StatusNotFound, apperror.ErrTaskNotFound, "task not found")
		}
		return Task{}, fmt.Errorf("task.Repack lock: %w", err)
	}
	projectID = moved.ProjectID

	// Current sibling ids in order, excluding the moved task.
	rows, err := tx.Query(ctx,
		`SELECT id FROM tasks WHERE user_id = @userID AND project_id = @projectID AND id <> @id
		 ORDER BY display_order ASC, id ASC`,
		pgx.NamedArgs{"userID": userID, "projectID": projectID, "id": id},
	)
	if err != nil {
		return Task{}, fmt.Errorf("task.Repack siblings: %w", err)
	}
	var ids []string
	for rows.Next() {
		var sid string
		if err := rows.Scan(&sid); err != nil {
			rows.Close()
			return Task{}, fmt.Errorf("task.Repack scan: %w", err)
		}
		ids = append(ids, sid)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Task{}, fmt.Errorf("task.Repack rows: %w", err)
	}

	// Insert the moved task at the clamped target position.
	pos := targetOrder
	if pos > len(ids) {
		pos = len(ids)
	}
	ordered := make([]string, 0, len(ids)+1)
	ordered = append(ordered, ids[:pos]...)
	ordered = append(ordered, id)
	ordered = append(ordered, ids[pos:]...)

	for order, sid := range ordered {
		if _, err := tx.Exec(ctx,
			`UPDATE tasks SET display_order = @order, updated_at = NOW() WHERE id = @id AND user_id = @userID`,
			pgx.NamedArgs{"order": order, "id": sid, "userID": userID},
		); err != nil {
			return Task{}, fmt.Errorf("task.Repack update: %w", err)
		}
	}

	if err := scanTask(
		tx.QueryRow(ctx, `SELECT`+taskSelectCols+`FROM tasks WHERE id = @id AND user_id = @userID`,
			pgx.NamedArgs{"id": id, "userID": userID}),
		&moved,
	); err != nil {
		return Task{}, fmt.Errorf("task.Repack reload: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Task{}, fmt.Errorf("task.Repack commit: %w", err)
	}
	return moved, nil
}

func isForeignKeyViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		return pgErr.SQLState() == "23503"
	}
	return false
}
