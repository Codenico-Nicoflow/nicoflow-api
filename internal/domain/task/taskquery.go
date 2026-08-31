package task

import (
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
)

type sortValueKind int

const (
	sortValueInt sortValueKind = iota
	sortValueTime
	sortValueText
)

// sortDescriptor pairs a whitelisted SQL column with the value kind that
// drives which cursor codec (int/time/text) encodes and decodes it. The
// keyset seek predicate is always built against this exact column, so it can
// never drift from the ORDER BY clause the way the old (created_at, id)
// hardcoded predicate did.
type sortDescriptor struct {
	// Expr is the SQL expression sorted/paginated on. scheduled_for is
	// nullable, so it is wrapped in COALESCE(scheduled_for, '') — a fixed
	// sentinel that sorts before any real date, applied identically in the
	// ORDER BY and the seek predicate so the two stay comparable.
	Expr string
	Kind sortValueKind
}

// sortDescriptors whitelists the API sortField → SQL sort expression. Using a
// fixed map (never the raw param) keeps ORDER BY injection-safe.
var sortDescriptors = map[string]sortDescriptor{
	"":             {Expr: "display_order", Kind: sortValueInt},
	"displayOrder": {Expr: "display_order", Kind: sortValueInt},
	"scheduledFor": {Expr: "COALESCE(scheduled_for, '')", Kind: sortValueText},
	"priority":     {Expr: "priority", Kind: sortValueText},
	"title":        {Expr: "title", Kind: sortValueText},
	"createdAt":    {Expr: "created_at", Kind: sortValueTime},
	"energy":       {Expr: "energy", Kind: sortValueText},
}

// buildListQuery returns the WHERE clause, the resolved sort descriptor +
// direction, and the named args for a project task list. The repo appends
// the keyset seek predicate (matched to the descriptor) and the ORDER BY
// itself, since both must agree on the same column. Always scoped to
// user_id + project_id. Returns an apperror on a bad sortField/sortOrder.
func buildListQuery(userID, projectID string, f ListTasksFilter) (whereSuffix string, sort sortDescriptor, dir string, args pgx.NamedArgs, err error) {
	clauses := []string{"user_id = @userID", "project_id = @projectID"}
	args = pgx.NamedArgs{"userID": userID, "projectID": projectID}

	if f.Status != nil {
		clauses = append(clauses, "status = @status")
		args["status"] = *f.Status
	} else {
		// No explicit status filter → hide done and terminal recurring
		// occurrences (missed/skipped/paused). Years of occurrence history would
		// otherwise bury the working view. NULL occurrence_status (the live
		// instance) always passes. A user-cancelled occurrence (status='cancelled',
		// occurrence_status=NULL) is intentionally kept visible — it was a
		// non-recurring cancellation, not a reaped/retired one. IS NULL guard is
		// required because NULL NOT IN (...) evaluates to NULL in SQL (not true),
		// which would wrongly exclude live instances.
		clauses = append(clauses, "(recurrence_rule_id IS NULL OR (status != 'done' AND (occurrence_status IS NULL OR occurrence_status NOT IN ('missed', 'skipped', 'paused'))))")
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

	sort, ok := sortDescriptors[f.SortField]
	if !ok {
		return "", sortDescriptor{}, "", nil, apperror.New(http.StatusBadRequest, apperror.ErrInvalidInput, "invalid sortField")
	}
	dir = "ASC"
	switch strings.ToLower(f.SortOrder) {
	case "", "asc":
		dir = "ASC"
	case "desc":
		dir = "DESC"
	default:
		return "", sortDescriptor{}, "", nil, apperror.New(http.StatusBadRequest, apperror.ErrInvalidInput, "sortOrder must be asc or desc")
	}

	whereSuffix = " WHERE " + strings.Join(clauses, " AND ")
	return whereSuffix, sort, dir, args, nil
}
