-- Revert to allowing area-less projects. The deleted rows from the .up cannot
-- be restored (data loss is not reversible), but the column constraint is.
ALTER TABLE projects ALTER COLUMN area_id DROP NOT NULL;
