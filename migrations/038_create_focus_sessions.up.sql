-- Focus sessions (E-049 / NIC-1709). One row per contiguous active-focus run
-- ("segment"). Total time-on-task is derived as SUM over closed segments — there
-- is deliberately no cached total on `tasks`, which would drift the moment a
-- sweep or a concurrent close touched a segment.
--
-- ended_at IS NULL marks the single open segment. `last_seen` is bumped by the
-- ~30s heartbeat and is what a close stamps into `ended_at` (never NOW()), so a
-- browser that dies mid-run contributes the time it actually proved, not the
-- time until the sweep noticed.
CREATE TABLE focus_sessions (
    id         TEXT        NOT NULL PRIMARY KEY,
    user_id    TEXT        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    task_id    TEXT        NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ended_at   TIMESTAMPTZ,
    last_seen  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- One-open-per-user invariant. The open/close transaction already enforces this;
-- the index is the safety net that makes a second open segment impossible even
-- if a future caller skips that path.
CREATE UNIQUE INDEX idx_focus_sessions_one_open
    ON focus_sessions(user_id) WHERE ended_at IS NULL;

-- SUM fast path: cumulative time for one task reads only closed segments.
CREATE INDEX idx_focus_sessions_closed_task
    ON focus_sessions(user_id, task_id) WHERE ended_at IS NOT NULL;

-- Stale sweep: find open segments whose last_seen fell behind the cutoff.
CREATE INDEX idx_focus_sessions_stale
    ON focus_sessions(last_seen) WHERE ended_at IS NULL;
