-- Drop the bucket FK column first: it references notes, so the table cannot go
-- while the column still points at it.
ALTER TABLE bucket DROP COLUMN IF EXISTS created_note_id;

DROP TABLE IF EXISTS notes;
