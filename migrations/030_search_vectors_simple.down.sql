-- Revert to the 'english' search config from migration 029.

DROP INDEX IF EXISTS tasks_search_idx;
DROP INDEX IF EXISTS projects_search_idx;
DROP INDEX IF EXISTS areas_search_idx;

ALTER TABLE tasks DROP COLUMN search_vector;
ALTER TABLE projects DROP COLUMN search_vector;
ALTER TABLE areas DROP COLUMN search_vector;

ALTER TABLE tasks
    ADD COLUMN search_vector tsvector
    GENERATED ALWAYS AS (
        to_tsvector('english', coalesce(title, '') || ' ' || coalesce(notes, ''))
    ) STORED;

ALTER TABLE projects
    ADD COLUMN search_vector tsvector
    GENERATED ALWAYS AS (to_tsvector('english', coalesce(name, ''))) STORED;

ALTER TABLE areas
    ADD COLUMN search_vector tsvector
    GENERATED ALWAYS AS (to_tsvector('english', coalesce(name, ''))) STORED;

CREATE INDEX tasks_search_idx ON tasks USING GIN (search_vector);
CREATE INDEX projects_search_idx ON projects USING GIN (search_vector);
CREATE INDEX areas_search_idx ON areas USING GIN (search_vector);
