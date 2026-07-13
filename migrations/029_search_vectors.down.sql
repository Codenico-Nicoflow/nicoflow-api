DROP INDEX IF EXISTS areas_search_idx;
DROP INDEX IF EXISTS projects_search_idx;
DROP INDEX IF EXISTS tasks_search_idx;

ALTER TABLE areas DROP COLUMN IF EXISTS search_vector;
ALTER TABLE projects DROP COLUMN IF EXISTS search_vector;
ALTER TABLE tasks DROP COLUMN IF EXISTS search_vector;
