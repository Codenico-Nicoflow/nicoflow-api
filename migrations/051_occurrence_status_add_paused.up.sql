-- Extends the occurrence_status vocabulary to include 'paused': a live
-- occurrence that was retired when its series was paused by the user. The prior
-- constraint (migration 050) did not include 'paused' because the pause/retire
-- path did not exist yet (NIC-2000 hardening pass).
ALTER TABLE tasks DROP CONSTRAINT tasks_occurrence_status_check;
ALTER TABLE tasks ADD CONSTRAINT tasks_occurrence_status_check
    CHECK (occurrence_status IS NULL OR occurrence_status IN ('missed', 'cancelled', 'skipped', 'paused'));
