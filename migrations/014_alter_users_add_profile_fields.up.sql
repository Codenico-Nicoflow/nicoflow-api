ALTER TABLE users
  ADD COLUMN IF NOT EXISTS username         VARCHAR(20)  UNIQUE,
  ADD COLUMN IF NOT EXISTS first_name       VARCHAR(100),
  ADD COLUMN IF NOT EXISTS last_name        VARCHAR(100),
  ADD COLUMN IF NOT EXISTS theme            VARCHAR(20)  NOT NULL DEFAULT 'light',
  ADD COLUMN IF NOT EXISTS image_url        TEXT,
  ADD COLUMN IF NOT EXISTS status           VARCHAR(20)  NOT NULL DEFAULT 'regular',
  ADD COLUMN IF NOT EXISTS plan             VARCHAR(20)  NOT NULL DEFAULT 'free';

CREATE INDEX IF NOT EXISTS idx_users_username ON users(username) WHERE username IS NOT NULL;

ALTER TABLE refresh_tokens
  ADD COLUMN IF NOT EXISTS token_fingerprint VARCHAR(64) UNIQUE;
