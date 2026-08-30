package notification

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

// Repository defines the data-access contract for the notification domain.
type Repository interface {
	List(ctx context.Context, userID string, f ListNotificationsFilter) ([]Notification, string, error)
	CountUnread(ctx context.Context, userID string) (int, error)
	MarkRead(ctx context.Context, userID, id string) (Notification, error)
	MarkAllRead(ctx context.Context, userID string) (int, error)
	Delete(ctx context.Context, userID, id string) error
	// DeleteByEntity removes every notification whose metadata names the given
	// entity — e.g. metadata->>'entityType'='task' AND metadata->>'entityId'=id.
	// Best-effort cancellation callers (task.Skip, NIC-1997) use this rather than
	// Delete-by-id because they don't know which notification ids, if any, exist
	// for the entity. Idempotent: zero matches is not an error.
	DeleteByEntity(ctx context.Context, userID, entityType, entityID string) (int, error)
	// InsertIfAbsent inserts the notification, skipping it if a row with the same
	// (user_id, dedupe_key) already exists. Returns the inserted row and true, or a
	// zero row and false when it was a duplicate. Used by producers (cron) for
	// idempotent generation.
	InsertIfAbsent(ctx context.Context, n Notification) (Notification, bool, error)

	// GetRecipient returns the plan + email needed to gate a user's out-of-app
	// notification channels. Row-scoped by user; excludes soft-deleted users.
	GetRecipient(ctx context.Context, userID string) (Recipient, error)
	// GetPreferences returns the user's notification preferences, or the defaults
	// when no row exists (absent row = defaults, never an error).
	GetPreferences(ctx context.Context, userID string) (Preferences, error)
	// UpsertPreferences lazily creates or partially updates the user's preferences
	// row and returns the result.
	UpsertPreferences(ctx context.Context, userID string, u UpdatePreferences) (Preferences, error)

	// UpsertPushSubscription stores a web-push subscription, upserting on
	// (user_id, endpoint) so a repeat subscribe refreshes rather than duplicates.
	UpsertPushSubscription(ctx context.Context, userID string, sub PushSubscription) error
	// DeletePushSubscription removes a user's subscription by endpoint. Idempotent —
	// deleting an absent endpoint is not an error.
	DeletePushSubscription(ctx context.Context, userID, endpoint string) error
	// ListPushSubscriptions returns all of a user's stored subscriptions.
	ListPushSubscriptions(ctx context.Context, userID string) ([]PushSubscription, error)
}

type pgRepository struct {
	db *pgxpool.Pool
}

// NewRepository returns a PostgreSQL-backed Repository.
func NewRepository(db *pgxpool.Pool) Repository {
	return &pgRepository{db: db}
}

func (r *pgRepository) List(ctx context.Context, userID string, f ListNotificationsFilter) ([]Notification, string, error) {
	limit := f.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	cursorCreated, cursorID, err := cursorutil.DecodeTime(f.Cursor)
	if err != nil {
		return nil, "", apperror.New(http.StatusBadRequest, apperror.ErrInvalidInput, "invalid cursor")
	}

	args := pgx.NamedArgs{
		"userID":        userID,
		"limit":         limit + 1,
		"cursorCreated": cursorCreated,
		"cursorID":      cursorID,
		"isRead":        f.IsRead,
		"hasCursor":     f.Cursor != "",
	}

	// Keyset paginate on (created_at DESC, id DESC). The @hasCursor guard lets the
	// first page skip the cursor predicate. @isRead is NULL when no filter is set.
	rows, err := r.db.Query(ctx, `
		SELECT id, user_id, type, title, body, metadata, is_read, read_at, dedupe_key, created_at
		FROM notifications
		WHERE user_id = @userID
		  AND (@isRead::boolean IS NULL OR is_read = @isRead)
		  AND (NOT @hasCursor OR (created_at, id) < (@cursorCreated, @cursorID))
		ORDER BY created_at DESC, id DESC
		LIMIT @limit`,
		args,
	)
	if err != nil {
		return nil, "", fmt.Errorf("notification.List query: %w", err)
	}
	defer rows.Close()

	items, err := scanNotifications(rows)
	if err != nil {
		return nil, "", err
	}

	var next string
	if len(items) > limit {
		last := items[limit-1]
		next = cursorutil.EncodeTime(last.CreatedAt, last.ID)
		items = items[:limit]
	}
	return items, next, nil
}

func (r *pgRepository) CountUnread(ctx context.Context, userID string) (int, error) {
	var count int
	err := r.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM notifications WHERE user_id = @userID AND is_read = FALSE`,
		pgx.NamedArgs{"userID": userID},
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("notification.CountUnread: %w", err)
	}
	return count, nil
}

func (r *pgRepository) MarkRead(ctx context.Context, userID, id string) (Notification, error) {
	var n Notification
	err := r.db.QueryRow(ctx, `
		UPDATE notifications
		SET is_read = TRUE, read_at = NOW()
		WHERE id = @id AND user_id = @userID
		RETURNING id, user_id, type, title, body, metadata, is_read, read_at, dedupe_key, created_at`,
		pgx.NamedArgs{"id": id, "userID": userID},
	).Scan(&n.ID, &n.UserID, &n.Type, &n.Title, &n.Body, &n.Metadata, &n.IsRead, &n.ReadAt, &n.DedupeKey, &n.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Notification{}, apperror.New(http.StatusNotFound, apperror.ErrNotificationNotFound, "notification not found")
		}
		return Notification{}, fmt.Errorf("notification.MarkRead: %w", err)
	}
	return n, nil
}

func (r *pgRepository) MarkAllRead(ctx context.Context, userID string) (int, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE notifications
		SET is_read = TRUE, read_at = NOW()
		WHERE user_id = @userID AND is_read = FALSE`,
		pgx.NamedArgs{"userID": userID},
	)
	if err != nil {
		return 0, fmt.Errorf("notification.MarkAllRead: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

func (r *pgRepository) Delete(ctx context.Context, userID, id string) error {
	tag, err := r.db.Exec(ctx,
		`DELETE FROM notifications WHERE id = @id AND user_id = @userID`,
		pgx.NamedArgs{"id": id, "userID": userID},
	)
	if err != nil {
		return fmt.Errorf("notification.Delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return apperror.New(http.StatusNotFound, apperror.ErrNotificationNotFound, "notification not found")
	}
	return nil
}

// DeleteByEntity removes every notification tagged with the given entity in its
// JSONB metadata. entityType/entityId are the same keys notify.go's meta()
// helper writes (e.g. {"entityType":"task","entityId":"<id>"}).
func (r *pgRepository) DeleteByEntity(ctx context.Context, userID, entityType, entityID string) (int, error) {
	tag, err := r.db.Exec(ctx, `
		DELETE FROM notifications
		WHERE user_id = @userID
		  AND metadata->>'entityType' = @entityType
		  AND metadata->>'entityId' = @entityID`,
		pgx.NamedArgs{"userID": userID, "entityType": entityType, "entityID": entityID},
	)
	if err != nil {
		return 0, fmt.Errorf("notification.DeleteByEntity: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

func (r *pgRepository) InsertIfAbsent(ctx context.Context, n Notification) (Notification, bool, error) {
	metadata := n.Metadata
	if len(metadata) == 0 {
		metadata = []byte(`{}`)
	}
	var out Notification
	err := r.db.QueryRow(ctx, `
		INSERT INTO notifications (id, user_id, type, title, body, metadata, dedupe_key, created_at)
		VALUES (@id, @userID, @type, @title, @body, @metadata, @dedupeKey, NOW())
		ON CONFLICT (user_id, dedupe_key) WHERE dedupe_key IS NOT NULL DO NOTHING
		RETURNING id, user_id, type, title, body, metadata, is_read, read_at, dedupe_key, created_at`,
		pgx.NamedArgs{
			"id":        n.ID,
			"userID":    n.UserID,
			"type":      n.Type,
			"title":     n.Title,
			"body":      n.Body,
			"metadata":  metadata,
			"dedupeKey": n.DedupeKey,
		},
	).Scan(&out.ID, &out.UserID, &out.Type, &out.Title, &out.Body, &out.Metadata, &out.IsRead, &out.ReadAt, &out.DedupeKey, &out.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// ON CONFLICT DO NOTHING returned no row: a duplicate was skipped.
			return Notification{}, false, nil
		}
		return Notification{}, false, fmt.Errorf("notification.InsertIfAbsent: %w", err)
	}
	return out, true, nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

func scanNotifications(rows pgx.Rows) ([]Notification, error) {
	var out []Notification
	for rows.Next() {
		var n Notification
		if err := rows.Scan(
			&n.ID, &n.UserID, &n.Type, &n.Title, &n.Body, &n.Metadata,
			&n.IsRead, &n.ReadAt, &n.DedupeKey, &n.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("notification scan: %w", err)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
