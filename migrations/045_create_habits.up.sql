-- Habits (E-055 / NIC-1923). A habit is a recurring personal commitment tracked
-- by check-in, not by task completion.
--
-- Habits are deliberately NOT a flavour of recurrence_rules: that table requires
-- a project_id (habits are not project work, and free users cap at 5 projects),
-- and materializing habits as tasks would put ~365 rows per habit per year into
-- every task list, search result and time-spread bucket.
CREATE TABLE habits (
    id                  TEXT        NOT NULL PRIMARY KEY,
    user_id             TEXT        NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    name                TEXT        NOT NULL,

    -- Cosmetic only: drives the card icon and colour, never scheduling or targets.
    -- A free-form slug rather than an enum so the served catalog can gain entries
    -- without a migration, and an older mobile build renders a fallback icon
    -- instead of breaking on a slug it has never seen.
    subject             TEXT        NOT NULL DEFAULT 'custom',
    color               TEXT        NOT NULL DEFAULT 'indigo',

    -- build = do the thing (satisfied at value >= target), quit = don't
    -- (satisfied at value <= target, target typically 0). Immutable after
    -- creation: flipping it inverts the meaning of every historical check-in and
    -- no per-row snapshot can repair that. The service rejects the change.
    polarity            TEXT        NOT NULL DEFAULT 'build'
                                    CHECK (polarity IN ('build', 'quit')),

    -- A binary habit is just target_value = 1 rendered as a checkbox, so counts
    -- ("8 glasses") never need a later migration.
    target_value        INT         NOT NULL DEFAULT 1 CHECK (target_value >= 0),
    unit                TEXT,

    -- Schedule. Stored as columns rather than an RRULE string, matching
    -- recurrence_rules, so the client renders a human summary without a parser.
    schedule_kind       TEXT        NOT NULL DEFAULT 'daily'
                                    CHECK (schedule_kind IN ('daily', 'weekdays', 'weekly_quota')),
    by_weekday          SMALLINT[],
    times_per_week      SMALLINT    CHECK (times_per_week BETWEEN 1 AND 7),

    -- Ships inert at 0. Present now so "my day ends at 3am" is later a settings
    -- toggle rather than a migration over live check-in data.
    day_cutoff_hour     SMALLINT    NOT NULL DEFAULT 0
                                    CHECK (day_cutoff_hour BETWEEN 0 AND 23),

    -- Schedule edits apply forward only; periods before this marker keep the
    -- shape they were scored under.
    schedule_changed_at DATE,

    -- Soft delete. Archiving frees a plan slot while keeping the history, which
    -- is the user's record of what they did.
    archived_at         TIMESTAMPTZ,

    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- The shape rule the per-column CHECKs cannot express: each schedule kind
    -- requires exactly its own fields to be present.
    CONSTRAINT habits_schedule_shape CHECK (
        (schedule_kind = 'daily')
     OR (schedule_kind = 'weekdays'     AND by_weekday IS NOT NULL AND array_length(by_weekday, 1) > 0)
     OR (schedule_kind = 'weekly_quota' AND times_per_week IS NOT NULL)
    )
);

-- The list read and the plan-limit COUNT(*) both filter on active rows only.
CREATE INDEX idx_habits_user_active ON habits(user_id) WHERE archived_at IS NULL;

-- One dated record per habit. Rows carry the rule they were judged by so that
-- editing a habit can never rewrite history: raising a target from 20 to 30
-- minutes must not retroactively fail forty completed days.
CREATE TABLE habit_check_ins (
    id                TEXT        NOT NULL PRIMARY KEY,
    habit_id          TEXT        NOT NULL REFERENCES habits(id) ON DELETE CASCADE,

    -- Denormalised so every read filters by user_id without a join to habits.
    user_id           TEXT        NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    -- A real DATE, not VARCHAR (tasks.scheduled_for is a known wart we work
    -- around). Streak computation is date arithmetic and needs a date type.
    -- Always the user's LOCAL date, resolved server-side from users.timezone —
    -- a client-supplied date is trivially spoofed to farm streaks.
    check_in_date     DATE        NOT NULL,

    value             INT         NOT NULL DEFAULT 1 CHECK (value >= 0),

    -- Frozen at write time. satisfied is stored rather than derived so the
    -- streak query counts rows instead of re-evaluating every past day against
    -- the habit's current target.
    target_at_checkin INT         NOT NULL,
    satisfied         BOOLEAN     NOT NULL,

    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- One row per habit per day; a repeat check-in updates it rather than inserting.
CREATE UNIQUE INDEX idx_habit_check_ins_unique ON habit_check_ins(habit_id, check_in_date);

-- Serves the streak scan and the heatmap window in one shape.
CREATE INDEX idx_habit_check_ins_lookup ON habit_check_ins(user_id, check_in_date DESC);
