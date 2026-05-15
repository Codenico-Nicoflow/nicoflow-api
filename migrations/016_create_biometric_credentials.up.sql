-- Reserved for FIDO2/WebAuthn biometric authentication (v2).
-- Routes POST /v1/auth/biometric/register and POST /v1/auth/biometric/verify
-- are registered but return 501 until this is active.
CREATE TABLE biometric_credentials (
  id            TEXT        NOT NULL PRIMARY KEY,
  user_id       TEXT        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  credential_id TEXT        NOT NULL UNIQUE,
  public_key    BYTEA       NOT NULL,
  counter       BIGINT      NOT NULL DEFAULT 0,
  platform      VARCHAR(20) NOT NULL DEFAULT 'web',
  created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_used_at  TIMESTAMPTZ
);

CREATE INDEX idx_biometric_user_id ON biometric_credentials(user_id);
