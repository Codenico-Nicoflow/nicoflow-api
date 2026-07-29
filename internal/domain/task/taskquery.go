package task

import (
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
)

// sortColumns whitelists the API sortField → SQL column. Using a fixed map
// (never the raw param) keeps ORDER BY injection-safe.
var sortColumns = map[string]string{
	"":             "display_order",
	"displayOrder": "display_order",
	"scheduledFor": "scheduled_for",
	"priority":     "priority",
	"title":        "title",
	"createdAt":    "created_at",
	"energy":       "energy",
}

// buildListQuery returns the WHERE/ORDER suffix + named args for a project task
// list. Always scoped to user_id + project_id. Returns an apperror on a bad
// sortField/sortOrder.
func buildListQuery(userID, projectID string, f ListTasksFilter) (string, pgx.NamedArgs, error) {
	clauses := []string{"user_id = @userID", "project_id = @projectID"}
	args := pgx.NamedArgs{"userID": userID, "projectID": projectID}

	if f.Status != nil {
		clauses = append(clauses, "status = @status")
		args["status"] = *f.Status
	} else {
		// No explicit status filter → hide the terminal recurrence states. Years of
		// occurrence history would otherwise bury the working view. Only recurring
		// rows are hidden: a one-off `done` task still lists as it always has, so
		// this changes nothing for non-recurring projects.
		clauses = append(clauses, "(recurrence_rule_id IS NULL OR status NOT IN ('done', 'missed'))")
	}
	if f.Priority != nil {
		clauses = append(clauses, "priority = @priority")
		args["priority"] = *f.Priority
	}
	if f.Energy != nil {
		clauses = append(clauses, "energy = @energy")
		args["energy"] = *f.Energy
	}
	if f.Search != "" {
		clauses = append(clauses, "(title ILIKE @search OR notes ILIKE @search)")
		args["search"] = "%" + f.Search + "%"
	}

	col, ok := sortColumns[f.SortField]
	if !ok {
		return "", nil, apperror.New(http.StatusBadRequest, apperror.ErrInvalidInput, "invalid sortField")
	}
	dir := "ASC"
	switch strings.ToLower(f.SortOrder) {
	case "", "asc":
		dir = "ASC"
	case "desc":
		dir = "DESC"
	default:
		return "", nil, apperror.New(http.StatusBadRequest, apperror.ErrInvalidInput, "sortOrder must be asc or desc")
	}

	suffix := " WHERE " + strings.Join(clauses, " AND ") +
		" ORDER BY " + col + " " + dir + ", id ASC"
	return suffix, args, nil
}
