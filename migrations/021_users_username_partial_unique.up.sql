-- Replace the unconditional UNIQUE constraint on username with a partial index
-- so soft-deleted users do not block re-registration with the same username.
-- Mirrors migration 018, which did the same for email. Before this, a
-- soft-deleted account permanently squatted its username (email was already
-- freed on soft-delete, leaving an inconsistent re-registration experience).
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_username_key;

CREATE UNIQUE INDEX users_username_active_uniq ON users (username) WHERE deleted_at IS NULL;
