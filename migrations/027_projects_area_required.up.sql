-- Enforce the Area › Project › Task hierarchy at the DB level: a project must
-- belong to an area. Migration 024 already made area deletion cascade to its
-- projects; this removes the last way a project could be area-less.
--
-- Area-less projects predate this rule and cannot be kept — delete them (their
-- tasks/subtasks cascade). This is irreversible; see the .down.sql note.
DELETE FROM projects WHERE area_id IS NULL;

ALTER TABLE projects ALTER COLUMN area_id SET NOT NULL;
