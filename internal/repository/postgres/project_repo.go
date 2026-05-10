package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"nicoflow-api/internal/model"
)

type ProjectRepo struct {
	db *pgxpool.Pool
}

func NewProjectRepo(db *pgxpool.Pool) *ProjectRepo {
	return &ProjectRepo{db: db}
}

func (r *ProjectRepo) Create(ctx context.Context, p *model.Project) error {

	_, err := r.db.Exec(ctx,
		`INSERT INTO projects (id, user_id, area_id, name, status, folder_icon, display_order, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		p.ID, p.UserID, p.AreaID, p.Name, p.Status, p.FolderIcon, p.DisplayOrder, p.CreatedAt, p.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("ProjectRepo.Create: %w", err)
	}
	return nil
}

func (r *ProjectRepo) List(ctx context.Context, userID string) ([]model.Project, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id, user_id, area_id, name, status, folder_icon,  display_order, created_at, updated_at
		 FROM projects WHERE user_id = $1 ORDER BY display_order ASC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("ProjectRepo.List: %w", err)
	}
	defer rows.Close()

	var result []model.Project
	for rows.Next() {
		var p model.Project
		if err := rows.Scan(&p.ID, &p.UserID, &p.AreaID, &p.Name, &p.Status, &p.FolderIcon, &p.DisplayOrder, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("ProjectRepo.List scan: %w", err)
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

func (r *ProjectRepo) ListByArea(ctx context.Context, areaId string) ([]model.Project, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id, user_id, area_id, name, status, display_order, created_at, updated_at
		 FROM projects WHERE area_id = $1 ORDER BY display_order ASC`,
		areaId,
	)
	if err != nil {
		return nil, fmt.Errorf("ProjectRepo.ListByArea: %w", err)
	}
	defer rows.Close()

	var result []model.Project
	for rows.Next() {
		var p model.Project
		if err := rows.Scan(&p.ID, &p.UserID, &p.AreaID, &p.Name, &p.Status, &p.DisplayOrder, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("ProjectRepo.ListByArea scan: %w", err)
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

func (r *ProjectRepo) FindByID(ctx context.Context, id string) (*model.Project, error) {
	p := &model.Project{}
	err := r.db.QueryRow(ctx,
		`SELECT id, user_id, area_id, name, status, display_order, created_at, updated_at
		 FROM projects WHERE id = $1`,
		id,
	).Scan(&p.ID, &p.UserID, &p.AreaID, &p.Name, &p.Status, &p.DisplayOrder, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("ProjectRepo.FindByID: %w", err)
	}
	return p, nil
}

func (r *ProjectRepo) Update(ctx context.Context, p *model.Project) error {
	_, err := r.db.Exec(ctx,
		`UPDATE projects SET area_id=$1, name=$2, status=$3, display_order=$4, updated_at=$5 WHERE id=$6`,
		p.AreaID, p.Name, p.Status, p.DisplayOrder, p.UpdatedAt, p.ID,
	)
	if err != nil {
		return fmt.Errorf("ProjectRepo.Update: %w", err)
	}
	return nil
}

func (r *ProjectRepo) Delete(ctx context.Context, id string) error {
	_, err := r.db.Exec(ctx, `DELETE FROM projects WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("ProjectRepo.Delete: %w", err)
	}
	return nil
}
