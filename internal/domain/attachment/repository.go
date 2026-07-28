package attachment

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
)

type pgRepo struct{ db *pgxpool.Pool }

// NewRepository creates a postgres-backed attachment repository.
func NewRepository(db *pgxpool.Pool) Repository { return &pgRepo{db: db} }

const selectCols = ` id, owner_type, owner_id, user_id, file_name, file_size, mime_type, s3_key, created_at `

func scan(row pgx.Row, a *Attachment) error {
	return row.Scan(
		&a.ID, &a.OwnerType, &a.OwnerID, &a.UserID,
		&a.FileName, &a.FileSize, &a.MimeType, &a.S3Key, &a.CreatedAt,
	)
}

// InsertGuarded runs the byte + count checks inside the INSERT so two concurrent
// uploads can't both read "under limit" and both write. The row appears only if
// both sub-selects still hold at write time; 0 rows ⇒ a limit would be exceeded.
func (r *pgRepo) InsertGuarded(ctx context.Context, a Attachment) (Attachment, bool, error) {
	var out Attachment
	err := scan(
		r.db.QueryRow(ctx, `
			INSERT INTO file_attachments
				(id, owner_type, owner_id, user_id, file_name, file_size, mime_type, s3_key, created_at)
			SELECT $1, $2, $3, $4, $5, $6, $7, $8, NOW()
			WHERE (SELECT COALESCE(SUM(file_size), 0) FROM file_attachments WHERE user_id = $4) + $6::bigint <= $10
			  AND (SELECT COUNT(*) FROM file_attachments WHERE owner_type = $2 AND owner_id = $3) < $9
			RETURNING`+selectCols,
			a.ID, a.OwnerType, a.OwnerID, a.UserID,
			a.FileName, a.FileSize, a.MimeType, a.S3Key,
			MaxFilesPerOwner, MaxBytesPerUser,
		),
		&out,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		// Guard tripped — no row written. Not an error; the caller decides which
		// limit message to return and cleans up the orphaned S3 object.
		return Attachment{}, false, nil
	}
	// s3_key is UNIQUE: a retried confirm (dropped response, double tap) races
	// itself. Return the row the winner wrote so the retry reads as success
	// instead of a 500.
	if isUniqueViolation(err) {
		existing, getErr := r.GetByS3Key(ctx, a.UserID, a.S3Key)
		if getErr != nil {
			return Attachment{}, false, getErr
		}
		return existing, true, nil
	}
	if err != nil {
		return Attachment{}, false, fmt.Errorf("attachment.InsertGuarded: %w", err)
	}
	return out, true, nil
}

// pgUniqueViolation is postgres SQLSTATE 23505.
const pgUniqueViolation = "23505"

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation
}

// GetByS3Key resolves an already-confirmed attachment by its object key, scoped
// to the owning user.
func (r *pgRepo) GetByS3Key(ctx context.Context, userID, s3Key string) (Attachment, error) {
	var a Attachment
	err := scan(
		r.db.QueryRow(ctx, `SELECT`+selectCols+`FROM file_attachments WHERE user_id = $1 AND s3_key = $2`, userID, s3Key),
		&a,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Attachment{}, apperror.New(http.StatusNotFound, apperror.ErrResourceNotFound, "attachment not found")
	}
	if err != nil {
		return Attachment{}, fmt.Errorf("attachment.GetByS3Key: %w", err)
	}
	return a, nil
}

func (r *pgRepo) ListByOwner(ctx context.Context, userID, ownerType, ownerID string) ([]Attachment, error) {
	rows, err := r.db.Query(ctx, `
		SELECT`+selectCols+`
		FROM file_attachments
		WHERE user_id = $1 AND owner_type = $2 AND owner_id = $3
		ORDER BY created_at DESC, id DESC`,
		userID, ownerType, ownerID,
	)
	if err != nil {
		return nil, fmt.Errorf("attachment.ListByOwner: %w", err)
	}
	return collect(rows, "attachment.ListByOwner")
}

func (r *pgRepo) GetByID(ctx context.Context, userID, id string) (Attachment, error) {
	var a Attachment
	err := scan(
		r.db.QueryRow(ctx, `SELECT`+selectCols+`FROM file_attachments WHERE user_id = $1 AND id = $2`, userID, id),
		&a,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Attachment{}, apperror.New(http.StatusNotFound, apperror.ErrResourceNotFound, "attachment not found")
	}
	if err != nil {
		return Attachment{}, fmt.Errorf("attachment.GetByID: %w", err)
	}
	return a, nil
}

func (r *pgRepo) Delete(ctx context.Context, userID, id string) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM file_attachments WHERE user_id = $1 AND id = $2`, userID, id)
	if err != nil {
		return fmt.Errorf("attachment.Delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return apperror.New(http.StatusNotFound, apperror.ErrResourceNotFound, "attachment not found")
	}
	return nil
}

func (r *pgRepo) SumBytesForUser(ctx context.Context, userID string) (int64, error) {
	var total int64
	err := r.db.QueryRow(ctx,
		`SELECT COALESCE(SUM(file_size), 0) FROM file_attachments WHERE user_id = $1`, userID,
	).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("attachment.SumBytesForUser: %w", err)
	}
	return total, nil
}

// DeleteAllForOwner removes every attachment for one owner and returns the
// deleted rows (RETURNING) so the caller can reclaim their S3 objects. There is
// no DB cascade on the polymorphic owner, so this is the explicit cleanup path.
func (r *pgRepo) DeleteAllForOwner(ctx context.Context, userID, ownerType, ownerID string) ([]Attachment, error) {
	rows, err := r.db.Query(ctx, `
		DELETE FROM file_attachments
		WHERE user_id = $1 AND owner_type = $2 AND owner_id = $3
		RETURNING`+selectCols,
		userID, ownerType, ownerID,
	)
	if err != nil {
		return nil, fmt.Errorf("attachment.DeleteAllForOwner: %w", err)
	}
	return collect(rows, "attachment.DeleteAllForOwner")
}

func (r *pgRepo) ListByUser(ctx context.Context, userID string) ([]Attachment, error) {
	rows, err := r.db.Query(ctx, `
		SELECT`+selectCols+`FROM file_attachments WHERE user_id = $1 ORDER BY created_at DESC, id DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("attachment.ListByUser: %w", err)
	}
	return collect(rows, "attachment.ListByUser")
}

// AllKeys returns the s3_key of every row across all users. GC-only: it diffs
// the object store against this set, so it must not be user-scoped.
func (r *pgRepo) AllKeys(ctx context.Context) (map[string]struct{}, error) {
	rows, err := r.db.Query(ctx, `SELECT s3_key FROM file_attachments`)
	if err != nil {
		return nil, fmt.Errorf("attachment.AllKeys: %w", err)
	}
	defer rows.Close()
	keys := make(map[string]struct{})
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("attachment.AllKeys scan: %w", err)
		}
		keys[key] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("attachment.AllKeys rows: %w", err)
	}
	return keys, nil
}

// ListAllOwners returns the distinct owner pairs across all users. GC-only: the
// sweep checks each for existence, so it must span every user's rows.
func (r *pgRepo) ListAllOwners(ctx context.Context) ([]Owner, error) {
	rows, err := r.db.Query(ctx, `SELECT DISTINCT owner_type, owner_id FROM file_attachments`)
	if err != nil {
		return nil, fmt.Errorf("attachment.ListAllOwners: %w", err)
	}
	defer rows.Close()
	var out []Owner
	for rows.Next() {
		var o Owner
		if err := rows.Scan(&o.OwnerType, &o.OwnerID); err != nil {
			return nil, fmt.Errorf("attachment.ListAllOwners scan: %w", err)
		}
		out = append(out, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("attachment.ListAllOwners rows: %w", err)
	}
	return out, nil
}

// DeleteByOwner removes every row for one owner across all users, returning the
// deleted rows so the GC sweep can reclaim their S3 objects. GC-only (dead-owner
// reap); the user-scoped variant is DeleteAllForOwner.
func (r *pgRepo) DeleteByOwner(ctx context.Context, ownerType, ownerID string) ([]Attachment, error) {
	rows, err := r.db.Query(ctx, `
		DELETE FROM file_attachments
		WHERE owner_type = $1 AND owner_id = $2
		RETURNING`+selectCols,
		ownerType, ownerID,
	)
	if err != nil {
		return nil, fmt.Errorf("attachment.DeleteByOwner: %w", err)
	}
	return collect(rows, "attachment.DeleteByOwner")
}

func collect(rows pgx.Rows, op string) ([]Attachment, error) {
	defer rows.Close()
	var out []Attachment
	for rows.Next() {
		var a Attachment
		if err := scan(rows, &a); err != nil {
			return nil, fmt.Errorf("%s scan: %w", op, err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s rows: %w", op, err)
	}
	return out, nil
}
