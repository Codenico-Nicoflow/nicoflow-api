-- Task recurrence (E-050 / NIC-1770). A rule is a template plus a schedule and a
-- cursor; the tasks it produces are ordinary rows carrying a back-reference.
--
-- There is deliberately NO `count` column and NO `occurrences_created` counter.
-- Stats are derived by counting occurrence rows: a stored counter counts
-- materializations, not completions, and drifts the moment either path retries.
CREATE TABLE recurrence_rules (
    id                TEXT        NOT NULL PRIMARY KEY,
    user_id           TEXT        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    project_id        TEXT        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,

    -- Template stamped onto every materialized occurrence.
    title             TEXT        NOT NULL,
    notes             TEXT,
    priority          TEXT        NOT NULL DEFAULT 'medium',
    energy            TEXT        NOT NULL DEFAULT 'medium',
    estimated_minutes INT,

    -- Schedule, stored as columns rather than an RRULE string so the frontend can
    -- render a human summary without shipping a parser.
    freq              TEXT        NOT NULL CHECK (freq IN ('daily', 'weekly', 'monthly', 'yearly')),
    interval          INT         NOT NULL DEFAULT 1 CHECK (interval BETWEEN 1 AND 366),
    by_weekday        SMALLINT[],
    by_monthday       SMALLINT    CHECK (by_monthday IS NULL OR by_monthday = -1 OR by_monthday BETWEEN 1 AND 31),
    start_date        DATE        NOT NULL,
    end_date          DATE,

    -- Cursor. next_occurrence NULL = the series is exhausted (past end_date).
    next_occurrence   DATE,
    paused            BOOLEAN     NOT NULL DEFAULT FALSE,

    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_recurrence_rules_user ON recurrence_rules(user_id);
CREATE INDEX idx_recurrence_rules_project ON recurrence_rules(project_id);

-- The sweep scan: only non-paused, non-exhausted rules are ever due.
CREATE INDEX idx_recurrence_rules_due
    ON recurrence_rules(next_occurrence)
    WHERE paused = FALSE AND next_occurrence IS NOT NULL;

-- SET NULL, not CASCADE: deleting a rule orphans its history rather than
-- destroying it. Completed occurrences are the user's record of what they did.
ALTER TABLE tasks
    ADD COLUMN recurrence_rule_id TEXT REFERENCES recurrence_rules(id) ON DELETE SET NULL,
    ADD COLUMN occurrence_date    DATE;

-- The idempotency guarantee. One occurrence per (rule, date) at the DB level lets
-- the cron sweep and the sync-on-complete path race without coordination — the
-- loser takes a unique violation and treats it as success.
CREATE UNIQUE INDEX idx_tasks_rule_occurrence
    ON tasks(recurrence_rule_id, occurrence_date)
    WHERE recurrence_rule_id IS NOT NULL;

CREATE INDEX idx_tasks_recurrence_rule ON tasks(recurrence_rule_id) WHERE recurrence_rule_id IS NOT NULL;
