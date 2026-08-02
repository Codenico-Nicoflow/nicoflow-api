-- Single-use OAuth `state` values (E-052 / NIC-1845). CSRF protection for the
-- Google callback: without it, an attacker can hand a victim a callback URL
-- carrying the attacker's authorization code and silently bind the attacker's
-- Google account to the victim's Nicoflow account.
--
-- Only the SHA-256 fingerprint is stored, never the raw value — the same reason
-- refresh and password-reset tokens are not stored raw. A leaked database dump
-- must not yield replayable state values. Unlike those tokens there is no bcrypt
-- companion hash: state is single-use, short-lived and high-entropy (32 bytes
-- from crypto/rand), so the fingerprint alone is the right cost/benefit.
--
-- `used_at` marks consumption rather than deleting the row, so a replay is
-- distinguishable from an expiry or a value that never existed — all three are
-- rejected identically to the client, but the difference matters when reading
-- logs after an incident.
CREATE TABLE google_oauth_states (
    state_fingerprint TEXT        NOT NULL PRIMARY KEY,
    user_id           TEXT        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- Where to send the browser after the callback completes. Validated against
    -- an allowlist before use — an open redirect here would be a phishing vector.
    redirect_path TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at    TIMESTAMPTZ NOT NULL,
    used_at       TIMESTAMPTZ
);

-- Sweep of expired/consumed rows, and the per-user lookup used when a second
-- connect attempt supersedes an abandoned one.
CREATE INDEX idx_google_oauth_states_user ON google_oauth_states(user_id);
CREATE INDEX idx_google_oauth_states_expiry ON google_oauth_states(expires_at);
