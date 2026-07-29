DROP INDEX IF EXISTS idx_tasks_recurrence_rule;
DROP INDEX IF EXISTS idx_tasks_rule_occurrence;

ALTER TABLE tasks
    DROP COLUMN IF EXISTS occurrence_date,
    DROP COLUMN IF EXISTS recurrence_rule_id;

DROP TABLE IF EXISTS recurrence_rules;
