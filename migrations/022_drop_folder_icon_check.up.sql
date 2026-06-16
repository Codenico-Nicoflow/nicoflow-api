-- Migration 013 added a CHECK constraint pinning projects.folder_icon to 13
-- icons. The icon set is now validated in the service layer (project.AllowedIcons,
-- a superset of the frontend picker) and areas.icon has no such CHECK, so the DB
-- constraint only causes drift: a service-valid themed icon (e.g. 'rocket') passed
-- validation then violated the CHECK on INSERT -> 500. Drop it; the service layer
-- is the single source of truth for allowed icons.
ALTER TABLE projects DROP CONSTRAINT IF EXISTS folder_icon_check;
