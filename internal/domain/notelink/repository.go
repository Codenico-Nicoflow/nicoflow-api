package notelink

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type pgRepo struct{ db *pgxpool.Pool }

// NewRepository creates a postgres-backed note-link repository.
func NewRepository(db *pgxpool.Pool) Repository { return &pgRepo{db: db} }

func (r *pgRepo) CreateLink(ctx context.Context, sourceNoteID, targetNoteID string) error {
	_, err := r.db.Exec(ctx,
		`INSERT INTO note_links (id, source_note_id, target_note_id)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (source_note_id, target_note_id) DO NOTHING`,
		uuid.NewString(), sourceNoteID, targetNoteID,
	)
	if err != nil {
		return fmt.Errorf("notelink.CreateLink: %w", err)
	}
	return nil
}

func (r *pgRepo) DeleteLink(ctx context.Context, sourceNoteID, targetNoteID string) error {
	_, err := r.db.Exec(ctx,
		`DELETE FROM note_links WHERE source_note_id = $1 AND target_note_id = $2`,
		sourceNoteID, targetNoteID,
	)
	if err != nil {
		return fmt.Errorf("notelink.DeleteLink: %w", err)
	}
	return nil
}

// ReplaceLinksForNote deletes-then-inserts inside one transaction rather than
// diffing in application code: the two-statement form is simpler, and the
// table is small per note (a note mentions a handful of others, not
// thousands), so there is no per-row cost to reclaim by diffing.
func (r *pgRepo) ReplaceLinksForNote(ctx context.Context, sourceNoteID string, targetNoteIDs []string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("notelink.ReplaceLinksForNote begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed

	if _, err := tx.Exec(ctx,
		`DELETE FROM note_links WHERE source_note_id = $1`,
		sourceNoteID,
	); err != nil {
		return fmt.Errorf("notelink.ReplaceLinksForNote delete: %w", err)
	}

	for _, targetNoteID := range targetNoteIDs {
		if _, err := tx.Exec(ctx,
			`INSERT INTO note_links (id, source_note_id, target_note_id)
			 VALUES ($1, $2, $3)
			 ON CONFLICT (source_note_id, target_note_id) DO NOTHING`,
			uuid.NewString(), sourceNoteID, targetNoteID,
		); err != nil {
			return fmt.Errorf("notelink.ReplaceLinksForNote insert: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("notelink.ReplaceLinksForNote commit: %w", err)
	}
	return nil
}

func (r *pgRepo) GetBacklinks(ctx context.Context, noteID string) ([]BacklinkNote, error) {
	rows, err := r.db.Query(ctx,
		`SELECT n.id, n.title
		   FROM note_links nl
		   JOIN notes n ON n.id = nl.source_note_id
		  WHERE nl.target_note_id = $1`,
		noteID,
	)
	if err != nil {
		return nil, fmt.Errorf("notelink.GetBacklinks: %w", err)
	}
	defer rows.Close()

	var out []BacklinkNote
	for rows.Next() {
		var b BacklinkNote
		if err := rows.Scan(&b.ID, &b.Title); err != nil {
			return nil, fmt.Errorf("notelink.GetBacklinks scan: %w", err)
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("notelink.GetBacklinks rows: %w", err)
	}
	return out, nil
}
