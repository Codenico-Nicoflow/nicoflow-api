ALTER TABLE projects ADD COLUMN folder_icon VARCHAR(50) NOT NULL DEFAULT 'folder',
ADD CONSTRAINT folder_icon_check
CHECK (folder_icon IN ('inbox', 'calendar', 'alarm', 'search', 'settings', 'menu', 'folder', 'layer', 'zap', 'computer', 'user', 'sprout', 'bone'));