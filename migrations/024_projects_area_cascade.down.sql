-- Revert to detaching projects (area_id → NULL) when an area is deleted.
ALTER TABLE projects DROP CONSTRAINT projects_area_id_fkey;

ALTER TABLE projects
    ADD CONSTRAINT projects_area_id_fkey
    FOREIGN KEY (area_id) REFERENCES areas(id) ON DELETE SET NULL;
