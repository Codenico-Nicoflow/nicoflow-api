-- Full-text search (E-019): STORED generated tsvector columns + GIN indexes.
-- Generated columns self-populate on insert/update and backfill existing rows,
-- so no data migration is needed. Replaces the E-013 ILIKE search.

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
