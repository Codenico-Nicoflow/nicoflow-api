-- Constrains occurrence_status to the vocabulary the recurrence engine and its
-- manual counterparts actually write: NULL (live/ordinary), 'missed' (reaped,
-- automatic or manual), 'cancelled' (historical; retained for any pre-existing
-- rows), or 'skipped' (a live occurrence the user explicitly opted out of
-- without breaking their streak — NIC-1997).
ALTER TABLE tasks ADD CONSTRAINT tasks_occurrence_status_check
    CHECK (occurrence_status IS NULL OR occurrence_status IN ('missed', 'cancelled', 'skipped'));
