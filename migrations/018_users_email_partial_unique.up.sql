-- Replace the table-level UNIQUE constraint on email with a partial index
-- so soft-deleted users do not block re-registration with the same email.
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_email_key;

CREATE UNIQUE INDEX users_email_active_uniq ON users (email) WHERE deleted_at IS NULL;
