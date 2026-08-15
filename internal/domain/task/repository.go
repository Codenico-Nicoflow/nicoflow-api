package task

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/pkg/cursorutil"
)

// Repository defines the data-access contract for the task domain.
type Repository interface {
	ListByProject(ctx context.Context, userID, projectID string, f ListTasksFilter) ([]Task, string, error)
	GetByID(ctx context.Context, userID, id string) (*Task, error)
	Create(ctx context.Context, t Task) (Task, error)
	Update(ctx context.Context, userID, id string, req UpdateTaskRequest, completedAt completedAtChange) (Task, error)
	Delete(ctx context.Context, userID, id string) error
	// ProjectOwned reports whether the project exists and belongs to the user.
	ProjectOwned(ctx context.Context, userID, projectID string) (bool, error)
	// CountActive counts only active tasks in a project (the calm plan limit).
	CountActive(ctx context.Context, userID, projectID string) (int, error)
	// CountNonTerminalByProject counts a project's active tasks. Zero means the
	// project is fully complete — the signal for the project_completed notification.
	CountNonTerminalByProject(ctx context.Context, userID, projectID string) (int, error)
	// NextDisplayOrder returns the order to append a new task at the end of a project.
	NextDisplayOrder(ctx context.Context, userID, projectID string) (int, error)
	// UpdateSchedule sets (nil clears) the soft schedule + time-of-day + optional rollsOver.
	UpdateSchedule(ctx context.Context, userID, id string, scheduledFor, scheduledTime *string, rollsOver *bool) (Task, error)
	// ListByDateRange returns the user's tasks scheduled within an inclusive date
	// range, in calendar-grid order. No roll-forward: a task is returned on the
	// date it is actually scheduled for (see calendar.go).
	ListByDateRange(ctx context.Context, userID, from, to string) ([]Task, error)
	// Repack moves a task to targetOrder and renumbers its project siblings 0..n-1.
	Repack(ctx context.Context, userID, id string, targetOrder int) (Task, error)
	// ListActiveByUser returns the user's active tasks across ALL projects —
	// the candidate set for Focus and Time-Spread.
	ListActiveByUser(ctx context.Context, userID string) ([]Task, error)
	// ExistsByID reports whether a task exists, system-wide (no user scope). Used
	// by the attachment GC sweep to detect dead owners; not for user requests.
	ExistsByID(ctx context.Context, id string) (bool, error)
	// IsOpenable reports whether the user owns the task and it can still accrue
	// focus time, i.e. it is not in a terminal status. A task the user does not
	// own is indistinguishable from a missing one.
	IsOpenable(ctx context.Context, userID, id string) (bool, error)
	// ListForUser returns tasks the user owns, filtered by the optional
	// UserListFilter fields. Backs the AI tool executor's list_tasks.
	ListForUser(ctx context.Context, userID string, f UserListFilter) ([]Task, error)
	// MarkMissed is the manual twin of the recurrence sweep's overdue reap: it
	// marks one recurring occurrence missed, guarded to today-or-past in the
	// owner's local timezone. Returns nil (no error) when the WHERE clause
	// matched nothing — the service disambiguates not-found from not-eligible.
	MarkMissed(ctx context.Context, userID, id string) (*Task, error)
}

type pgRepo struct{ db *pgxpool.Pool }

// NewRepository creates a new postgres-backed task repository.
func NewRepository(db *pgxpool.Pool) Repository { return &pgRepo{db: db} }

// occurrence_date is a DATE but travels the wire as YYYY-MM-DD, matching
// scheduled_for — cast in the projection so it scans straight into a *string.
// scheduled_time is a TIME but travels the wire as HH:MM — formatted in the
// projection so it scans straight into a *string and never leaks the seconds
// Postgres would render (`09:00:00`), which the contract does not include.
// The two subtask counts are correlated scalar subqueries rather than an
// enrichment seam (unlike TotalFocusSeconds): they are cheap — subtasks is
// indexed on task_id — and every read needs them, because the client blocks a
// complete on OpenSubtaskCount > 0 wherever a task can be checked off.
const taskSelectCols = ` id, user_id, project_id, title, notes, status, priority, energy,
	rolls_over, scheduled_for, to_char(scheduled_time, 'HH24:MI'), estimated_minutes, url, display_order,
	completed_at, created_at, updated_at, recurrence_rule_id, occurrence_date::text, occurrence_status,
	(SELECT COUNT(*) FROM subtasks s WHERE s.task_id = tasks.id),
	(SELECT COUNT(*) FROM subtasks s WHERE s.task_id = tasks.id AND s.done = FALSE) `

func scanTask(row pgx.Row, t *Task) error {
	return row.Scan(
		&t.ID, &t.UserID, &t.ProjectID, &t.Title, &t.Notes, &t.Status, &t.Priority, &t.Energy,
		&t.RollsOver, &t.ScheduledFor, &t.ScheduledTime, &t.EstimatedMinutes, &t.URL, &t.DisplayOrder,
		&t.CompletedAt, &t.CreatedAt, &t.UpdatedAt, &t.RecurrenceRuleID, &t.OccurrenceDate, &t.OccurrenceStatus,
		&t.SubtaskCount, &t.OpenSubtaskCount,
	)
}

func (r *pgRepo) ListByProject(ctx context.Context, userID, projectID string, f ListTasksFilter) ([]Task, string, error) {
	limit := f.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	whereSuffix, sort, dir, args, err := buildListQuery(userID, projectID, f)
	if err != nil {
		return nil, "", err
	}

	// The seek predicate MUST be built on the same expression the ORDER BY
	// sorts on — a mismatch (e.g. sorting by display_order but seeking on
	// created_at) silently duplicates/skips rows across pages. Each value
	// kind gets its own decode + comparable SQL literal, keyed to whichever
	// column sort.Expr actually is.
	hasCursor := f.Cursor != ""
	args["hasCursor"] = hasCursor
	op := ">"
	if dir == "DESC" {
		op = "<"
	}

	switch sort.Kind {
	case sortValueInt:
		cursorKey, cursorID, decErr := cursorutil.DecodeInt(f.Cursor)
		if decErr != nil {
			return nil, "", apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "invalid cursor")
		}
		args["cursorKey"] = cursorKey
		args["cursorID"] = cursorID
	case sortValueTime:
		cursorKey, cursorID, decErr := cursorutil.DecodeTime(f.Cursor)
		if decErr != nil {
			return nil, "", apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "invalid cursor")
		}
		args["cursorKey"] = cursorKey
		args["cursorID"] = cursorID
	default: // sortValueText
		cursorKey, cursorID, decErr := cursorutil.DecodeString(f.Cursor)
		if decErr != nil {
			return nil, "", apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "invalid cursor")
		}
		args["cursorKey"] = cursorKey
		args["cursorID"] = cursorID
	}
	args["limit"] = limit + 1

	sql := `SELECT` + taskSelectCols + `FROM tasks` +
		whereSuffix +
		` AND (NOT @hasCursor OR (` + sort.Expr + `, id) ` + op + ` (@cursorKey, @cursorID))` +
		` ORDER BY ` + sort.Expr + ` ` + dir + `, id ` + dir + ` LIMIT @limit`
	rows, err := r.db.Query(ctx, sql, args)
	if err != nil {
		return nil, "", fmt.Errorf("task.ListByProject query: %w", err)
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var t Task
		if err := scanTask(rows, &t); err != nil {
			return nil, "", fmt.Errorf("task.ListByProject scan: %w", err)
		}
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}

	var next string
	if len(tasks) > limit {
		last := tasks[limit-1]
		next = encodeListCursor(sort.Kind, last, f.SortField)
		tasks = tasks[:limit]
	}
	return tasks, next, nil
}

// encodeListCursor mirrors the decode branch in ListByProject: the cursor
// must carry the value of whatever column sort.Expr actually sorted on.
func encodeListCursor(kind sortValueKind, last Task, sortField string) string {
	switch kind {
	case sortValueInt:
		return cursorutil.EncodeInt(last.DisplayOrder, last.ID)
	case sortValueTime:
		return cursorutil.EncodeTime(last.CreatedAt, last.ID)
	default: // sortValueText
		var key string
		switch sortField {
		case "priority":
			key = last.Priority
		case "title":
			key = last.Title
		case "energy":
			key = last.Energy
		case "scheduledFor":
			if last.ScheduledFor != nil {
				key = *last.ScheduledFor
			}
		}
		return cursorutil.EncodeString(key, last.ID)
	}
}

func (r *pgRepo) ListActiveByUser(ctx context.Context, userID string) ([]Task, error) {
	rows, err := r.db.Query(ctx,
		`SELECT`+taskSelectCols+`FROM tasks
		 WHERE user_id = @userID AND status = 'active'`,
		pgx.NamedArgs{"userID": userID},
	)
	if err != nil {
		return nil, fmt.Errorf("task.ListActiveByUser: %w", err)
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var t Task
		if err := scanTask(rows, &t); err != nil {
			return nil, fmt.Errorf("task.ListActiveByUser scan: %w", err)
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// ListByDateRange is the calendar's read. Both bounds are inclusive ISO dates.
// Unlike Time Spread it applies no roll-forward and no status filter: a month
// grid has to show an overdue or completed task on the day it was actually
// scheduled for, or the grid lies about history.
func (r *pgRepo) ListByDateRange(ctx context.Context, userID, from, to string) ([]Task, error) {
	// scheduled_for is a VARCHAR holding an ISO date, so a plain string
	// comparison is both correct (ISO sorts lexicographically = chronologically)
	// and index-friendly. Casting either side to DATE would defeat the index.
	rows, err := r.db.Query(ctx,
		`SELECT`+taskSelectCols+`FROM tasks
		 WHERE user_id = @userID
		   AND scheduled_for IS NOT NULL
		   AND scheduled_for >= @from
		   AND scheduled_for <= @to
		 ORDER BY scheduled_for ASC, scheduled_time ASC NULLS FIRST, display_order ASC, id ASC`,
		pgx.NamedArgs{"userID": userID, "from": from, "to": to},
	)
	if err != nil {
		return nil, fmt.Errorf("task.ListByDateRange: %w", err)
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var t Task
		if err := scanTask(rows, &t); err != nil {
			return nil, fmt.Errorf("task.ListByDateRange scan: %w", err)
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// MarkMissed is the manual twin of the recurrence sweep's overdue reap (see
// recurrence.Repository.ReapOverdue): same terminal write
// (status='cancelled', occurrence_status='missed'), just triggered by the user
// instead of the cron. The WHERE clause carries every precondition — recurring,
// still active, not already reaped, occurrence date today-or-earlier in the
// owner's local timezone — so a 0-row result is the single signal the service
// needs to distinguish "not found" from "not eligible" (it disambiguates via a
// follow-up GetByID). Two distinct guards deliberately live in the same WHERE
// rather than a chain of separate checks: eligibility here is a database-time
// fact (today, in this user's zone), and checking it anywhere but at the query
// closes a race no application-layer check can close. The target table stays
// unaliased (never `tasks t`) because RETURNING can't see a FROM-joined
// alias, and taskSelectCols' subtask subqueries are written against the bare
// `tasks.id` name.
func (r *pgRepo) MarkMissed(ctx context.Context, userID, id string) (*Task, error) {
	var t Task
	err := scanTask(
		r.db.QueryRow(ctx, `
			UPDATE tasks SET status = 'cancelled', occurrence_status = 'missed', updated_at = NOW()
			FROM users u
			WHERE tasks.id = @id AND tasks.user_id = @userID AND u.id = tasks.user_id
			  AND tasks.recurrence_rule_id IS NOT NULL
			  AND tasks.status = 'active'
			  AND tasks.occurrence_status IS NULL
			  AND tasks.occurrence_date <= ((NOW() AT TIME ZONE COALESCE(u.timezone, 'UTC'))::date)
			RETURNING`+taskSelectCols,
			pgx.NamedArgs{"id": id, "userID": userID},
		),
		&t,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("task.MarkMissed: %w", err)
	}
	return &t, nil
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
				 scheduled_for, scheduled_time, estimated_minutes, url, display_order, completed_at,
				 created_at, updated_at)
			VALUES
				(@id, @userID, @projectID, @title, @notes, @status, @priority, @energy, @rollsOver,
				 @scheduledFor, @scheduledTime::time, @estimatedMinutes, @url, @displayOrder, @completedAt,
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
				"scheduledTime":    t.ScheduledTime,
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
				scheduled_time    = CASE WHEN @scheduledTimeSet THEN @scheduledTime::time ELSE scheduled_time END,
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
				"scheduledTime":       req.ScheduledTime.Value,
				"scheduledTimeSet":    req.ScheduledTime.Set,
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

func (r *pgRepo) ExistsByID(ctx context.Context, id string) (bool, error) {
	var exists bool
	err := r.db.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM tasks WHERE id = @id)`,
		pgx.NamedArgs{"id": id},
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("task.ExistsByID: %w", err)
	}
	return exists, nil
}

// IsOpenable gates opening a focus segment: the caller must own the task and it
// must not be terminal (done/cancelled).
func (r *pgRepo) IsOpenable(ctx context.Context, userID, id string) (bool, error) {
	var openable bool
	err := r.db.QueryRow(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM tasks
			WHERE id = @id AND user_id = @user_id
			  AND status NOT IN ('done', 'cancelled')
		)`,
		pgx.NamedArgs{"id": id, "user_id": userID},
	).Scan(&openable)
	if err != nil {
		return false, fmt.Errorf("task.IsOpenable: %w", err)
	}
	return openable, nil
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

func (r *pgRepo) CountActive(ctx context.Context, userID, projectID string) (int, error) {
	var count int
	err := r.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM tasks
		 WHERE user_id = @userID AND project_id = @projectID AND status = 'active'`,
		pgx.NamedArgs{"userID": userID, "projectID": projectID},
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("task.CountActive: %w", err)
	}
	return count, nil
}

func (r *pgRepo) CountNonTerminalByProject(ctx context.Context, userID, projectID string) (int, error) {
	var count int
	err := r.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM tasks
		 WHERE user_id = @userID AND project_id = @projectID
		   AND status NOT IN ('done', 'cancelled')`,
		pgx.NamedArgs{"userID": userID, "projectID": projectID},
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("task.CountNonTerminalByProject: %w", err)
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

func (r *pgRepo) UpdateSchedule(ctx context.Context, userID, id string, scheduledFor, scheduledTime *string, rollsOver *bool) (Task, error) {
	var t Task
	err := scanTask(
		r.db.QueryRow(ctx, `
			UPDATE tasks SET
				scheduled_for  = @scheduledFor,
				scheduled_time = @scheduledTime::time,
				rolls_over     = COALESCE(@rollsOver, rolls_over),
				updated_at     = NOW()
			WHERE id = @id AND user_id = @userID
			RETURNING`+taskSelectCols,
			pgx.NamedArgs{
				"scheduledFor":  scheduledFor,
				"scheduledTime": scheduledTime,
				"rollsOver":     rollsOver,
				"id":            id,
				"userID":        userID,
			},
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
