-- Switch full-text search from the 'english' config to 'simple' (E-019).
-- 'english' stems words ("testing" -> "test"), which breaks prefix matching:
-- a to_tsquery('testin:*') can't match the stored lexeme "test". 'simple' keeps
-- whole words, so type-ahead prefix search ("testin" -> "testing") works and it
-- also treats every language uniformly (no English-only stemming) — better for a
-- multi-language product. Regenerating a STORED generated column requires dropping
-- and re-adding it; the GIN indexes are recreated to point at the new definition.

DROP INDEX IF EXISTS tasks_search_idx;
DROP INDEX IF EXISTS projects_search_idx;
DROP INDEX IF EXISTS areas_search_idx;

ALTER TABLE tasks DROP COLUMN search_vector;
ALTER TABLE projects DROP COLUMN search_vector;
ALTER TABLE areas DROP COLUMN search_vector;

ALTER TABLE tasks
    ADD COLUMN search_vector tsvector
    GENERATED ALWAYS AS (
        to_tsvector('simple', coalesce(title, '') || ' ' || coalesce(notes, ''))
    ) STORED;

ALTER TABLE projects
    ADD COLUMN search_vector tsvector
    GENERATED ALWAYS AS (to_tsvector('simple', coalesce(name, ''))) STORED;

ALTER TABLE areas
    ADD COLUMN search_vector tsvector
    GENERATED ALWAYS AS (to_tsvector('simple', coalesce(name, ''))) STORED;

CREATE INDEX tasks_search_idx ON tasks USING GIN (search_vector);
CREATE INDEX projects_search_idx ON projects USING GIN (search_vector);
CREATE INDEX areas_search_idx ON areas USING GIN (search_vector);
