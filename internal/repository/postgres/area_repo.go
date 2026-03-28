package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"nicoflow-api/internal/model"
)

type AreaRepo struct {
	db *pgxpool.Pool
}

func NewAreaRepo(db *pgxpool.Pool) *AreaRepo {
	return &AreaRepo{db: db}
}

func (r *AreaRepo) Create(ctx context.Context, a *model.Area) error {
	_, err := r.db.Exec(ctx,
		`INSERT INTO areas (id, user_id, name, color, display_order, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		a.ID, a.UserID, a.Name, a.Color, a.DisplayOrder, a.CreatedAt, a.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("AreaRepo.Create: %w", err)
	}
	return nil
}

func (r *AreaRepo) List(ctx context.Context, userID string) ([]model.Area, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id, user_id, name, color, display_order, created_at, updated_at
		 FROM areas WHERE user_id = $1 ORDER BY display_order ASC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("AreaRepo.List: %w", err)
	}
	defer rows.Close()

	var result []model.Area
	for rows.Next() {
		var a model.Area
		if err := rows.Scan(&a.ID, &a.UserID, &a.Name, &a.Color, &a.DisplayOrder, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, fmt.Errorf("AreaRepo.List scan: %w", err)
		}
		result = append(result, a)
	}
	return result, rows.Err()
}

func (r *AreaRepo) FindByID(ctx context.Context, id string) (*model.Area, error) {
	a := &model.Area{}
	err := r.db.QueryRow(ctx,
		`SELECT id, user_id, name, color, display_order, created_at, updated_at
		 FROM areas WHERE id = $1`,
		id,
	).Scan(&a.ID, &a.UserID, &a.Name, &a.Color, &a.DisplayOrder, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("AreaRepo.FindByID: %w", err)
	}
	return a, nil
}

func (r *AreaRepo) Update(ctx context.Context, a *model.Area) error {
	_, err := r.db.Exec(ctx,
		`UPDATE areas SET name=$1, color=$2, display_order=$3, updated_at=$4 WHERE id=$5`,
		a.Name, a.Color, a.DisplayOrder, a.UpdatedAt, a.ID,
	)
	if err != nil {
		return fmt.Errorf("AreaRepo.Update: %w", err)
	}
	return nil
}

func (r *AreaRepo) Delete(ctx context.Context, id string) error {
	_, err := r.db.Exec(ctx, `DELETE FROM areas WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("AreaRepo.Delete: %w", err)
	}
	return nil
}
