-- Rolls back the 'paused' addition: restores the migration 050 constraint.
-- Any rows with occurrence_status='paused' must be cleared first or this will fail.
ALTER TABLE tasks DROP CONSTRAINT tasks_occurrence_status_check;
ALTER TABLE tasks ADD CONSTRAINT tasks_occurrence_status_check
    CHECK (occurrence_status IS NULL OR occurrence_status IN ('missed', 'cancelled', 'skipped'));
