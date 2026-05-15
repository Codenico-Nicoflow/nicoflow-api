DROP INDEX IF EXISTS idx_users_username;

ALTER TABLE refresh_tokens
  DROP COLUMN IF EXISTS token_fingerprint;

ALTER TABLE users
  DROP COLUMN IF EXISTS plan,
  DROP COLUMN IF EXISTS status,
  DROP COLUMN IF EXISTS image_url,
  DROP COLUMN IF EXISTS theme,
  DROP COLUMN IF EXISTS last_name,
  DROP COLUMN IF EXISTS first_name,
  DROP COLUMN IF EXISTS username;
