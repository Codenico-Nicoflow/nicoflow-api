package area

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/internal/domain/project"
)

// Repository defines the data-access contract for the area domain.
type Repository interface {
	List(ctx context.Context, userID string, f ListAreasFilter) ([]Area, string, error)
	ListWithProjects(ctx context.Context, userID string) ([]AreaWithProjects, error)
	GetByID(ctx context.Context, userID, id string) (*Area, error)
	Create(ctx context.Context, a Area) (Area, error)
	Update(ctx context.Context, userID, id string, req UpdateAreaRequest) (Area, error)
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

func (r *pgRepository) List(ctx context.Context, userID string, f ListAreasFilter) ([]Area, string, error) {
	limit := f.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	cursorOrder, cursorID, err := decodeCursor(f.Cursor)
	if err != nil {
		return nil, "", apperror.New(http.StatusBadRequest, apperror.ErrInvalidInput, "invalid cursor")
	}

	args := pgx.NamedArgs{
		"userID":      userID,
		"limit":       limit + 1,
		"cursorOrder": cursorOrder,
		"cursorID":    cursorID,
		"query":       "%" + f.Query + "%",
	}

	rows, err := r.db.Query(ctx, `
		SELECT id, user_id, name, color, icon, display_order, created_at, updated_at
		FROM areas
		WHERE user_id = @userID
		  AND name ILIKE @query
		  AND (display_order, id) > (@cursorOrder, @cursorID)
		ORDER BY display_order ASC, id ASC
		LIMIT @limit`,
		args,
	)
	if err != nil {
		return nil, "", fmt.Errorf("area.List query: %w", err)
	}
	defer rows.Close()

	areas, err := scanAreas(rows)
	if err != nil {
		return nil, "", err
	}

	var next string
	if len(areas) > limit {
		last := areas[limit-1]
		next = encodeCursor(last.DisplayOrder, last.ID)
		areas = areas[:limit]
	}
	return areas, next, nil
}

func (r *pgRepository) ListWithProjects(ctx context.Context, userID string) ([]AreaWithProjects, error) {
	rows, err := r.db.Query(ctx, `
		SELECT
			a.id, a.user_id, a.name, a.color, a.icon, a.display_order, a.created_at, a.updated_at,
			p.id, p.user_id, p.area_id, p.name, p.status, p.folder_icon,
			p.due_date, p.is_favorite, p.description, p.display_order, p.created_at, p.updated_at
		FROM areas a
		LEFT JOIN projects p ON p.area_id = a.id AND p.user_id = @userID
		WHERE a.user_id = @userID
		ORDER BY a.display_order ASC, a.id ASC, p.display_order ASC, p.id ASC`,
		pgx.NamedArgs{"userID": userID},
	)
	if err != nil {
		return nil, fmt.Errorf("area.ListWithProjects query: %w", err)
	}
	defer rows.Close()

	areaMap := make(map[string]*AreaWithProjects)
	var order []string

	for rows.Next() {
		var a Area
		var (
			pID, pUserID, pName, pStatus, pFolderIcon *string
			pAreaID, pDescription                     *string
			pIsFavorite                               *bool
			pDisplayOrder                             *int
			pDueDate                                  *time.Time
			pCreatedAt, pUpdatedAt                    *time.Time
		)

		if err := rows.Scan(
			&a.ID, &a.UserID, &a.Name, &a.Color, &a.Icon, &a.DisplayOrder, &a.CreatedAt, &a.UpdatedAt,
			&pID, &pUserID, &pAreaID, &pName, &pStatus, &pFolderIcon,
			&pDueDate, &pIsFavorite, &pDescription, &pDisplayOrder, &pCreatedAt, &pUpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("area.ListWithProjects scan: %w", err)
		}

		if _, exists := areaMap[a.ID]; !exists {
			awp := &AreaWithProjects{Area: a, Projects: []project.Project{}}
			areaMap[a.ID] = awp
			order = append(order, a.ID)
		}

		if pID != nil {
			// Joined on area_id = a.ID, so a matched project always carries this area.
			areaID := a.ID
			if pAreaID != nil {
				areaID = *pAreaID
			}
			p := project.Project{
				ID:          *pID,
				UserID:      *pUserID,
				AreaID:      areaID,
				Name:        *pName,
				Status:      *pStatus,
				FolderIcon:  *pFolderIcon,
				IsFavorite:  pIsFavorite != nil && *pIsFavorite,
				Description: pDescription,
				DueDate:     pDueDate,
			}
			if pDisplayOrder != nil {
				p.DisplayOrder = *pDisplayOrder
			}
			if pCreatedAt != nil {
				p.CreatedAt = *pCreatedAt
			}
			if pUpdatedAt != nil {
				p.UpdatedAt = *pUpdatedAt
			}
			areaMap[a.ID].Projects = append(areaMap[a.ID].Projects, p)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("area.ListWithProjects rows: %w", err)
	}

	result := make([]AreaWithProjects, 0, len(order))
	for _, id := range order {
		result = append(result, *areaMap[id])
	}
	return result, nil
}

func (r *pgRepository) GetByID(ctx context.Context, userID, id string) (*Area, error) {
	var a Area
	err := r.db.QueryRow(ctx, `
		SELECT id, user_id, name, color, icon, display_order, created_at, updated_at
		FROM areas
		WHERE id = @id AND user_id = @userID`,
		pgx.NamedArgs{"id": id, "userID": userID},
	).Scan(&a.ID, &a.UserID, &a.Name, &a.Color, &a.Icon, &a.DisplayOrder, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperror.New(http.StatusNotFound, apperror.ErrAreaNotFound, "area not found")
		}
		return nil, fmt.Errorf("area.GetByID: %w", err)
	}
	return &a, nil
}

func (r *pgRepository) Create(ctx context.Context, a Area) (Area, error) {
	err := r.db.QueryRow(ctx, `
		INSERT INTO areas (id, user_id, name, color, icon, display_order, created_at, updated_at)
		VALUES (@id, @userID, @name, @color, @icon, @displayOrder, NOW(), NOW())
		RETURNING id, user_id, name, color, icon, display_order, created_at, updated_at`,
		pgx.NamedArgs{
			"id":           a.ID,
			"userID":       a.UserID,
			"name":         a.Name,
			"color":        a.Color,
			"icon":         a.Icon,
			"displayOrder": a.DisplayOrder,
		},
	).Scan(&a.ID, &a.UserID, &a.Name, &a.Color, &a.Icon, &a.DisplayOrder, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return Area{}, apperror.New(http.StatusConflict, apperror.ErrDuplicateName, "an area with this name already exists")
		}
		return Area{}, fmt.Errorf("area.Create: %w", err)
	}
	return a, nil
}

func (r *pgRepository) Update(ctx context.Context, userID, id string, req UpdateAreaRequest) (Area, error) {
	var a Area
	err := r.db.QueryRow(ctx, `
		UPDATE areas
		SET name       = COALESCE(@name, name),
		    color      = COALESCE(@color, color),
		    icon       = COALESCE(@icon, icon),
		    updated_at = NOW()
		WHERE id = @id AND user_id = @userID
		RETURNING id, user_id, name, color, icon, display_order, created_at, updated_at`,
		pgx.NamedArgs{
			"name":   req.Name,
			"color":  req.Color,
			"icon":   req.Icon,
			"id":     id,
			"userID": userID,
		},
	).Scan(&a.ID, &a.UserID, &a.Name, &a.Color, &a.Icon, &a.DisplayOrder, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Area{}, apperror.New(http.StatusNotFound, apperror.ErrAreaNotFound, "area not found")
		}
		if isUniqueViolation(err) {
			return Area{}, apperror.New(http.StatusConflict, apperror.ErrDuplicateName, "an area with this name already exists")
		}
		return Area{}, fmt.Errorf("area.Update: %w", err)
	}
	return a, nil
}

func (r *pgRepository) Delete(ctx context.Context, userID, id string) error {
	tag, err := r.db.Exec(ctx,
		`DELETE FROM areas WHERE id = @id AND user_id = @userID`,
		pgx.NamedArgs{"id": id, "userID": userID},
	)
	if err != nil {
		return fmt.Errorf("area.Delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return apperror.New(http.StatusNotFound, apperror.ErrAreaNotFound, "area not found")
	}
	return nil
}

func (r *pgRepository) CountByUser(ctx context.Context, userID string) (int, error) {
	var count int
	err := r.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM areas WHERE user_id = @userID`,
		pgx.NamedArgs{"userID": userID},
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("area.CountByUser: %w", err)
	}
	return count, nil
}

func (r *pgRepository) Reorder(ctx context.Context, userID string, items []ReorderItem) (int, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("area.Reorder begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	total := 0
	for _, item := range items {
		tag, err := tx.Exec(ctx, `
			UPDATE areas SET display_order = @order, updated_at = NOW()
			WHERE id = @id AND user_id = @userID`,
			pgx.NamedArgs{"order": item.DisplayOrder, "id": item.ID, "userID": userID},
		)
		if err != nil {
			return 0, fmt.Errorf("area.Reorder update: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return 0, apperror.New(http.StatusNotFound, apperror.ErrAreaNotFound, fmt.Sprintf("area %q not found or not owned", item.ID))
		}
		total++
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("area.Reorder commit: %w", err)
	}
	return total, nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

func scanAreas(rows pgx.Rows) ([]Area, error) {
	var areas []Area
	for rows.Next() {
		var a Area
		if err := rows.Scan(&a.ID, &a.UserID, &a.Name, &a.Color, &a.Icon, &a.DisplayOrder, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, fmt.Errorf("area scan: %w", err)
		}
		areas = append(areas, a)
	}
	return areas, rows.Err()
}

// encodeCursor encodes display_order and id into an opaque base64 cursor.
func encodeCursor(order int, id string) string {
	raw := strconv.Itoa(order) + ":" + id
	return base64.StdEncoding.EncodeToString([]byte(raw))
}

// decodeCursor decodes the cursor. Returns -1/"" (before first row) on empty input.
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
		return 0, "", fmt.Errorf("decodeCursor: malformed cursor")
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
