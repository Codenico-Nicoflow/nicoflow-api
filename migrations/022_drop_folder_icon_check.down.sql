-- Restore the original 13-icon CHECK from migration 013.
ALTER TABLE projects
  ADD CONSTRAINT folder_icon_check
  CHECK (folder_icon IN ('inbox', 'calendar', 'alarm', 'search', 'settings', 'menu', 'folder', 'layer', 'zap', 'computer', 'user', 'sprout', 'bone'));
