package project

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
)

// Repository defines the data-access contract for the project domain.
type Repository interface {
	List(ctx context.Context, userID string, f ListProjectsFilter) ([]Project, string, error)
	ListByArea(ctx context.Context, userID, areaID string, f ListProjectsFilter) ([]Project, string, error)
	GetByID(ctx context.Context, userID, id string) (*Project, error)
	Create(ctx context.Context, p Project) (Project, error)
	Update(ctx context.Context, userID, id string, req UpdateProjectRequest) (Project, error)
	Delete(ctx context.Context, userID, id string) error
	CountByUser(ctx context.Context, userID string) (int, error)
	Reorder(ctx context.Context, userID string, items []ReorderItem) (int, error)
}

type pgRepository struct {
	db *pgxpool.Pool
}

// NewRepository returns a PostgreSQL-backed Repository.
func NewRepository(db *pgxpool.Pool) Repository {
	return &pgRepository{db: db}
}

const projectSelectCols = ` id, user_id, area_id, name, status, folder_icon,
	due_date, is_favorite, description, display_order, created_at, updated_at `

func (r *pgRepository) List(ctx context.Context, userID string, f ListProjectsFilter) ([]Project, string, error) {
	limit, cursorOrder, cursorID, err := parsePaginationArgs(f.Limit, f.Cursor)
	if err != nil {
		return nil, "", err
	}

	where, args := buildProjectWhere(userID, f, cursorOrder, cursorID)
	args["limit"] = limit + 1

	rows, err := r.db.Query(ctx,
		`SELECT`+projectSelectCols+`FROM projects WHERE `+where+
			` AND (display_order, id) > (@cursorOrder, @cursorID)`+
			` ORDER BY display_order ASC, id ASC LIMIT @limit`,
		args,
	)
	if err != nil {
		return nil, "", fmt.Errorf("project.List query: %w", err)
	}
	defer rows.Close()

	return scanProjectsWithCursor(rows, limit)
}

func (r *pgRepository) ListByArea(ctx context.Context, userID, areaID string, f ListProjectsFilter) ([]Project, string, error) {
	limit, cursorOrder, cursorID, err := parsePaginationArgs(f.Limit, f.Cursor)
	if err != nil {
		return nil, "", err
	}

	f.AreaID = &areaID
	where, args := buildProjectWhere(userID, f, cursorOrder, cursorID)
	args["limit"] = limit + 1

	rows, err := r.db.Query(ctx,
		`SELECT`+projectSelectCols+`FROM projects WHERE `+where+
			` AND (display_order, id) > (@cursorOrder, @cursorID)`+
			` ORDER BY display_order ASC, id ASC LIMIT @limit`,
		args,
	)
	if err != nil {
		return nil, "", fmt.Errorf("project.ListByArea query: %w", err)
	}
	defer rows.Close()

	return scanProjectsWithCursor(rows, limit)
}

func (r *pgRepository) GetByID(ctx context.Context, userID, id string) (*Project, error) {
	var p Project
	err := r.db.QueryRow(ctx,
		`SELECT`+projectSelectCols+`FROM projects WHERE id = @id AND user_id = @userID`,
		pgx.NamedArgs{"id": id, "userID": userID},
	).Scan(
		&p.ID, &p.UserID, &p.AreaID, &p.Name, &p.Status, &p.FolderIcon,
		&p.DueDate, &p.IsFavorite, &p.Description, &p.DisplayOrder, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperror.New(http.StatusNotFound, apperror.ErrProjectNotFound, "project not found")
		}
		return nil, fmt.Errorf("project.GetByID: %w", err)
	}
	return &p, nil
}

func (r *pgRepository) Create(ctx context.Context, p Project) (Project, error) {
	err := r.db.QueryRow(ctx, `
		INSERT INTO projects
			(id, user_id, area_id, name, status, folder_icon, due_date, is_favorite, description, display_order, created_at, updated_at)
		VALUES
			(@id, @userID, @areaID, @name, @status, @folderIcon, @dueDate, @isFavorite, @description, @displayOrder, NOW(), NOW())
		RETURNING`+projectSelectCols,
		pgx.NamedArgs{
			"id":           p.ID,
			"userID":       p.UserID,
			"areaID":       p.AreaID,
			"name":         p.Name,
			"status":       p.Status,
			"folderIcon":   p.FolderIcon,
			"dueDate":      p.DueDate,
			"isFavorite":   p.IsFavorite,
			"description":  p.Description,
			"displayOrder": p.DisplayOrder,
		},
	).Scan(
		&p.ID, &p.UserID, &p.AreaID, &p.Name, &p.Status, &p.FolderIcon,
		&p.DueDate, &p.IsFavorite, &p.Description, &p.DisplayOrder, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return Project{}, apperror.New(http.StatusConflict, apperror.ErrDuplicateName, "a project with this name already exists")
		}
		if isForeignKeyViolation(err) {
			return Project{}, apperror.New(http.StatusNotFound, apperror.ErrAreaNotFound, "area not found or does not belong to you")
		}
		if isCheckViolation(err) {
			return Project{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "invalid project field value")
		}
		return Project{}, fmt.Errorf("project.Create: %w", err)
	}
	return p, nil
}

func (r *pgRepository) Update(ctx context.Context, userID, id string, req UpdateProjectRequest) (Project, error) {
	// areaID update requires special handling: nil pointer = don't change; pointer to "" = set NULL; pointer to id = set value.
	var areaIDExpr string
	args := pgx.NamedArgs{
		"name":           req.Name,
		"status":         req.Status,
		"folderIcon":     req.FolderIcon,
		"isFavorite":     req.IsFavorite,
		"dueDate":        req.DueDate.Value,
		"dueDateSet":     req.DueDate.Set,
		"description":    req.Description.Value,
		"descriptionSet": req.Description.Set,
		"id":             id,
		"userID":         userID,
	}

	if req.AreaID == nil {
		areaIDExpr = "area_id"
	} else if *req.AreaID == "" {
		areaIDExpr = "NULL"
	} else {
		areaIDExpr = "@newAreaID"
		args["newAreaID"] = *req.AreaID
	}

	var p Project
	err := r.db.QueryRow(ctx, `
		UPDATE projects SET
			name        = COALESCE(@name, name),
			status      = COALESCE(@status, status),
			folder_icon = COALESCE(@folderIcon, folder_icon),
			is_favorite = COALESCE(@isFavorite, is_favorite),
			due_date    = CASE WHEN @dueDateSet THEN @dueDate ELSE due_date END,
			description = CASE WHEN @descriptionSet THEN @description ELSE description END,
			area_id     = `+areaIDExpr+`,
			updated_at  = NOW()
		WHERE id = @id AND user_id = @userID
		RETURNING`+projectSelectCols,
		args,
	).Scan(
		&p.ID, &p.UserID, &p.AreaID, &p.Name, &p.Status, &p.FolderIcon,
		&p.DueDate, &p.IsFavorite, &p.Description, &p.DisplayOrder, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Project{}, apperror.New(http.StatusNotFound, apperror.ErrProjectNotFound, "project not found")
		}
		if isUniqueViolation(err) {
			return Project{}, apperror.New(http.StatusConflict, apperror.ErrDuplicateName, "a project with this name already exists")
		}
		if isCheckViolation(err) {
			return Project{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "invalid project field value")
		}
		return Project{}, fmt.Errorf("project.Update: %w", err)
	}
	return p, nil
}

func (r *pgRepository) Delete(ctx context.Context, userID, id string) error {
	tag, err := r.db.Exec(ctx,
		`DELETE FROM projects WHERE id = @id AND user_id = @userID`,
		pgx.NamedArgs{"id": id, "userID": userID},
	)
	if err != nil {
		return fmt.Errorf("project.Delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return apperror.New(http.StatusNotFound, apperror.ErrProjectNotFound, "project not found")
	}
	return nil
}

func (r *pgRepository) CountByUser(ctx context.Context, userID string) (int, error) {
	var count int
	err := r.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM projects WHERE user_id = @userID`,
		pgx.NamedArgs{"userID": userID},
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("project.CountByUser: %w", err)
	}
	return count, nil
}

func (r *pgRepository) Reorder(ctx context.Context, userID string, items []ReorderItem) (int, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("project.Reorder begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	total := 0
	for _, item := range items {
		tag, err := tx.Exec(ctx, `
			UPDATE projects SET display_order = @order, updated_at = NOW()
			WHERE id = @id AND user_id = @userID`,
			pgx.NamedArgs{"order": item.DisplayOrder, "id": item.ID, "userID": userID},
		)
		if err != nil {
			return 0, fmt.Errorf("project.Reorder update: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return 0, apperror.New(http.StatusNotFound, apperror.ErrProjectNotFound, fmt.Sprintf("project %q not found or not owned", item.ID))
		}
		total++
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("project.Reorder commit: %w", err)
	}
	return total, nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

func buildProjectWhere(userID string, f ListProjectsFilter, cursorOrder int, cursorID string) (string, pgx.NamedArgs) {
	clauses := []string{"user_id = @userID"}
	args := pgx.NamedArgs{
		"userID":      userID,
		"cursorOrder": cursorOrder,
		"cursorID":    cursorID,
	}

	if f.AreaID != nil {
		clauses = append(clauses, "area_id = @areaID")
		args["areaID"] = *f.AreaID
	}
	if f.Status != nil {
		clauses = append(clauses, "status = @status")
		args["status"] = *f.Status
	}
	if f.IsFavorite != nil {
		clauses = append(clauses, "is_favorite = @isFavorite")
		args["isFavorite"] = *f.IsFavorite
	}
	if f.Query != "" {
		clauses = append(clauses, "name ILIKE @query")
		args["query"] = "%" + f.Query + "%"
	}

	return strings.Join(clauses, " AND "), args
}

func scanProjectsWithCursor(rows pgx.Rows, limit int) ([]Project, string, error) {
	projects, err := scanProjects(rows)
	if err != nil {
		return nil, "", err
	}

	var next string
	if len(projects) > limit {
		last := projects[limit-1]
		next = encodeCursor(last.DisplayOrder, last.ID)
		projects = projects[:limit]
	}
	return projects, next, nil
}

func scanProjects(rows pgx.Rows) ([]Project, error) {
	var projects []Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(
			&p.ID, &p.UserID, &p.AreaID, &p.Name, &p.Status, &p.FolderIcon,
			&p.DueDate, &p.IsFavorite, &p.Description, &p.DisplayOrder, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("project scan: %w", err)
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

func parsePaginationArgs(rawLimit int, cursor string) (int, int, string, error) {
	limit := rawLimit
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	cursorOrder, cursorID, err := decodeCursor(cursor)
	if err != nil {
		return 0, 0, "", apperror.New(http.StatusBadRequest, apperror.ErrInvalidInput, "invalid cursor")
	}
	return limit, cursorOrder, cursorID, nil
}

func encodeCursor(order int, id string) string {
	raw := strconv.Itoa(order) + ":" + id
	return base64.StdEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(cursor string) (int, string, error) {
	if cursor == "" {
		return -1, "", nil
	}
	b, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return 0, "", fmt.Errorf("decodeCursor: %w", err)
	}
	parts := strings.SplitN(string(b), ":", 2)
	if len(parts) != 2 {
		return 0, "", fmt.Errorf("decodeCursor: malformed")
	}
	order, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, "", fmt.Errorf("decodeCursor order: %w", err)
	}
	return order, parts[1], nil
}

func isUniqueViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		return pgErr.SQLState() == "23505"
	}
	return false
}

func isForeignKeyViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		return pgErr.SQLState() == "23503"
	}
	return false
}

func isCheckViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		return pgErr.SQLState() == "23514"
	}
	return false
}
