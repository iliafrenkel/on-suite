-- SQLite refuses UNIQUE on a column added via ALTER TABLE on a STRICT
-- table ("Cannot add a UNIQUE column"), verified against SQLite for this
-- project (spec §4) — the same reason paste_snippets' own share_slug is
-- UNIQUE inline instead: that table's column arrived in its original
-- CREATE TABLE, this one does not. The index below permits many NULLs,
-- since SQLite treats NULLs as distinct, so every not-yet-shared node can
-- carry one.
ALTER TABLE notes_nodes ADD COLUMN share_slug TEXT;
CREATE UNIQUE INDEX notes_nodes_share_slug_idx ON notes_nodes (share_slug);
