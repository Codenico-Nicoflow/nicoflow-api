package task

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
)

// UserListFilter is the input for ListForUser — the same knobs as
// ListTasksFilter, plus the optional project and scheduled-date window, minus
// the required-project scope. Nil pointer = filter not applied. Limit is
// clamped at userListDefaultLimit / userListMaxLimit to keep tool payloads
// bounded.
type UserListFilter struct {
	Status        *string
	Priority      *string
	Energy        *string
	ProjectID     *string
	ScheduledFrom *string
	ScheduledTo   *string
	Search        string
	Limit         int
}

const (
	userListDefaultLimit = 20
	userListMaxLimit     = 50
)

// ListForUser is the tool-executor read: every task the caller owns, filtered
// server-side. Mirrors ListByProject's validator surface without the project
// requirement so the assistant can search across the whole workspace before
// proposing a write. Row-isolated by user_id; the underlying repo enforces it.
func (s *service) ListForUser(ctx context.Context, userID string, f UserListFilter) (ListTasksResponse, error) {
	if err := validateUserListFilter(&f); err != nil {
		return ListTasksResponse{}, err
	}
	if f.ProjectID != nil {
		owned, err := s.repo.ProjectOwned(ctx, userID, *f.ProjectID)
		if err != nil {
			return ListTasksResponse{}, err
		}
		if !owned {
			return ListTasksResponse{}, apperror.New(http.StatusNotFound, apperror.ErrProjectNotFound, "project not found")
		}
	}
	tasks, err := s.repo.ListForUser(ctx, userID, f)
	if err != nil {
		return ListTasksResponse{}, err
	}
	items := make([]TaskView, len(tasks))
	for i, t := range tasks {
		items[i] = TaskToView(t)
	}
	return ListTasksResponse{Items: items}, nil
}

// validateUserListFilter validates enums + dates and clamps Limit in place.
func validateUserListFilter(f *UserListFilter) error {
	if f.Status != nil && !allowedStatuses[*f.Status] {
		return errInvalidStatus()
	}
	if f.Priority != nil && !allowedPriorities[*f.Priority] {
		return errInvalidPriority()
	}
	if f.Energy != nil && !allowedEnergies[*f.Energy] {
		return errInvalidEnergy()
	}
	if err := validateScheduledFor(f.ScheduledFrom); err != nil {
		return err
	}
	if err := validateScheduledFor(f.ScheduledTo); err != nil {
		return err
	}
	if f.Limit <= 0 {
		f.Limit = userListDefaultLimit
	}
	if f.Limit > userListMaxLimit {
		f.Limit = userListMaxLimit
	}
	return nil
}

// ListForUser (repository) is the cross-project search behind the tool
// executor's list_tasks. Every user-scoped filter is optional; row-level
// isolation is unconditional. ORDER BY is deterministic (scheduled_for,
// display_order, id) so pagination-by-limit is stable across calls.
func (r *pgRepo) ListForUser(ctx context.Context, userID string, f UserListFilter) ([]Task, error) {
	clauses := []string{"user_id = @userID"}
	args := pgx.NamedArgs{"userID": userID}

	if f.Status != nil {
		clauses = append(clauses, "status = @status")
		args["status"] = *f.Status
	}
	if f.Priority != nil {
		clauses = append(clauses, "priority = @priority")
		args["priority"] = *f.Priority
	}
	if f.Energy != nil {
		clauses = append(clauses, "energy = @energy")
		args["energy"] = *f.Energy
	}
	if f.ProjectID != nil {
		clauses = append(clauses, "project_id = @projectID")
		args["projectID"] = *f.ProjectID
	}
	if f.ScheduledFrom != nil {
		clauses = append(clauses, "scheduled_for IS NOT NULL AND scheduled_for >= @scheduledFrom")
		args["scheduledFrom"] = *f.ScheduledFrom
	}
	if f.ScheduledTo != nil {
		clauses = append(clauses, "scheduled_for IS NOT NULL AND scheduled_for <= @scheduledTo")
		args["scheduledTo"] = *f.ScheduledTo
	}
	if f.Search != "" {
		clauses = append(clauses, "(title ILIKE @search OR notes ILIKE @search)")
		args["search"] = "%" + f.Search + "%"
	}
	args["limit"] = f.Limit

	rows, err := r.db.Query(ctx,
		`SELECT`+taskSelectCols+`FROM tasks WHERE `+strings.Join(clauses, " AND ")+
			` ORDER BY scheduled_for ASC NULLS LAST, display_order ASC, id ASC LIMIT @limit`,
		args,
	)
	if err != nil {
		return nil, fmt.Errorf("task.ListForUser: %w", err)
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var t Task
		if err := scanTask(rows, &t); err != nil {
			return nil, fmt.Errorf("task.ListForUser scan: %w", err)
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}
