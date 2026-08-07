-- ai_tool_calls persists every write-tool proposal the assistant produces.
-- Read tools are transparent and never land here.
--
-- Row lifecycle: pending → confirmed | rejected | expired (nightly sweep).
-- One row per tool_use block; a single assistant turn can carry several.
--
-- ON DELETE CASCADE on both session_id and assistant_message_id keeps the
-- audit trail from outliving its context.
CREATE TABLE ai_tool_calls (
    id                    TEXT NOT NULL PRIMARY KEY,
    session_id            TEXT NOT NULL REFERENCES ai_sessions(id) ON DELETE CASCADE,
    user_id               TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    assistant_message_id  TEXT NOT NULL REFERENCES ai_messages(id) ON DELETE CASCADE,
    tool_use_id           TEXT NOT NULL,
    tool_name             TEXT NOT NULL,
    input_json            JSONB NOT NULL,
    status                TEXT NOT NULL DEFAULT 'pending',
    result_json           JSONB,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at           TIMESTAMPTZ,
    CHECK (status IN ('pending', 'confirmed', 'rejected', 'expired'))
);

-- The pending-lookup path (list pending for a session, expire pending older
-- than 7 days) is the hot query.
CREATE INDEX ai_tool_calls_session_status_idx
    ON ai_tool_calls (session_id, status);

-- The 7-day sweep filters by (status, created_at); a partial index keyed to
-- pending keeps it small and skips already-resolved rows.
CREATE INDEX ai_tool_calls_pending_created_at_idx
    ON ai_tool_calls (created_at)
    WHERE status = 'pending';

-- tool_use_id is Claude's own identifier for one proposed call. The
-- confirm/reject endpoints route by it; we look up scoped by (session_id,
-- tool_use_id) so a stray id from another session can't be resolved.
CREATE INDEX ai_tool_calls_tool_use_idx
    ON ai_tool_calls (session_id, tool_use_id);

-- ai_messages gains a content_json column that losslessly persists the
-- multi-block assistant turn (text + tool_use blocks in the order Claude
-- emitted them). The existing content TEXT stays for backward compat and
-- keeps holding the concatenated visible text — the FE reads it unchanged.
ALTER TABLE ai_messages
    ADD COLUMN content_json JSONB;
