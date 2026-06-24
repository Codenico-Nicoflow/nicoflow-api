-- Deleting an Area now hard-deletes its projects (and their tasks, via the
-- existing projects→tasks cascade) instead of detaching them. Switch the
-- projects.area_id FK from ON DELETE SET NULL to ON DELETE CASCADE.
ALTER TABLE projects DROP CONSTRAINT projects_area_id_fkey;

ALTER TABLE projects
    ADD CONSTRAINT projects_area_id_fkey
    FOREIGN KEY (area_id) REFERENCES areas(id) ON DELETE CASCADE;
