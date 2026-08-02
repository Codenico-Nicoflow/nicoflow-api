-- Google Calendar connections (E-052 / NIC-1838). One row per connected user:
-- the account link, not a session. `user_id` is the PK rather than a separate id
-- because a user has at most one Google connection — a UNIQUE index on a surrogate
-- key would say the same thing less clearly and allow a second row to be inserted
-- before the constraint caught it.
--
-- `refresh_token_encrypted` is AES-256-GCM ciphertext (nonce prepended), never
-- plaintext: a leaked Google refresh token exposes every meeting title, attendee
-- and location the user has, and read-only scope does not lower that stake. The
-- access token is deliberately NOT stored — it is short-lived and re-derived from
-- the refresh token on demand, so persisting it would add a second secret at rest
-- for no benefit.
--
-- BYTEA rather than TEXT so the ciphertext is never silently re-encoded by a
-- client or collation change.
CREATE TABLE google_calendar_connections (
    user_id                 TEXT        NOT NULL PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    refresh_token_encrypted BYTEA       NOT NULL,
    google_account_email    TEXT        NOT NULL,
    -- Which of the user's calendars to overlay. Empty ⇒ nothing selected yet;
    -- the service defaults to the primary calendar. Capped at 5 in the service
    -- layer (NIC-1857) because each selected calendar is a separate API call per
    -- ranged fetch. IDs, never display names — names change, IDs do not.
    selected_calendar_ids   TEXT[]      NOT NULL DEFAULT '{}',
    -- Scopes actually granted. Google may return fewer than requested, and a
    -- re-consent can change them, so what we asked for is not evidence of what
    -- we hold.
    scopes                  TEXT[]      NOT NULL DEFAULT '{}',
    connected_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_sync_at            TIMESTAMPTZ,
    -- Last failure reason, surfaced to the user as the reconnect prompt (NIC-1870).
    -- NULL ⇒ healthy. Never carries token material.
    last_error              TEXT
);
