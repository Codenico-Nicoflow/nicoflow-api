ALTER TABLE ai_messages DROP COLUMN IF EXISTS content_json;
DROP INDEX IF EXISTS ai_tool_calls_tool_use_idx;
DROP INDEX IF EXISTS ai_tool_calls_pending_created_at_idx;
DROP INDEX IF EXISTS ai_tool_calls_session_status_idx;
DROP TABLE IF EXISTS ai_tool_calls;
