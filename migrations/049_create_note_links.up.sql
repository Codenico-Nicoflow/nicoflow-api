-- note-to-note mention links (E-057a / NIC-1961). Every time a note's content
-- is saved, its @-mention nodes are diffed against this table and it is
-- resynced via ReplaceLinksForNote — this migration is the storage only, the
-- sync trigger lives in the existing note update path.
--
-- Both FKs cascade: deleting either the source or the target note removes the
-- link row. On target delete the mention chip itself is NOT rewritten (it
-- lives inside the source note's Tiptap JSON) — the frontend does a runtime
-- existence check on render (soft-orphan, per E-057b). There is deliberately
-- no block on deleting a note with inbound links.
CREATE TABLE note_links (
    id              TEXT        NOT NULL PRIMARY KEY,
    source_note_id  TEXT        NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
    target_note_id  TEXT        NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (source_note_id, target_note_id)
);

-- Outbound lookups (resync on save) key off source_note_id; the unique
-- constraint above already gives it an index. Backlinks need the reverse.
CREATE INDEX idx_note_links_target ON note_links(target_note_id);
