DROP INDEX IF EXISTS idx_tasks_user_energy;
DROP INDEX IF EXISTS idx_tasks_project_order;

ALTER TABLE tasks
    DROP COLUMN IF EXISTS completed_at,
    DROP COLUMN IF EXISTS display_order,
    DROP COLUMN IF EXISTS url,
    DROP COLUMN IF EXISTS estimated_minutes,
    DROP COLUMN IF EXISTS priority,
    DROP COLUMN IF EXISTS rolls_over,
    DROP COLUMN IF EXISTS energy;
