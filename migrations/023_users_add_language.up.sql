-- Persist the user's preferred UI language so it syncs across devices and (later)
-- drives localized transactional emails. Mirrors the existing `theme` column
-- (migration 014). Allowed values are en|he|ru, validated in the service layer
-- (no DB CHECK, consistent with the icon-allowlist precedent — keeps the column
-- forgiving so adding a language is a code-only change).
ALTER TABLE users
  ADD COLUMN IF NOT EXISTS language VARCHAR(5) NOT NULL DEFAULT 'en';
