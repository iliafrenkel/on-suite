-- N6: full-text search over title and note — spec §12. FTS5 is verified
-- present in modernc.org/sqlite v1.56.0, already in go.mod, so this adds no
-- dependency.
CREATE VIRTUAL TABLE notes_fts USING fts5(
    title, note, content='notes_nodes', content_rowid='id', tokenize='unicode61'
);

-- External-content FTS5 tables are not backfilled automatically: every
-- bullet that already existed before this migration needs one explicit
-- insert, or it stays unsearchable until its next edit.
INSERT INTO notes_fts(rowid, title, note) SELECT id, title, note FROM notes_nodes;

-- Keeps notes_fts in step with notes_nodes from here on. The 'delete'
-- special command (INSERT INTO notes_fts(notes_fts, rowid, title, note)
-- VALUES ('delete', ...)) is FTS5's own documented way to remove a row from
-- an external-content index — a plain DELETE FROM notes_fts is not
-- supported the same way for a content= table.
CREATE TRIGGER notes_fts_ai AFTER INSERT ON notes_nodes BEGIN
    INSERT INTO notes_fts(rowid, title, note) VALUES (new.id, new.title, new.note);
END;

CREATE TRIGGER notes_fts_ad AFTER DELETE ON notes_nodes BEGIN
    INSERT INTO notes_fts(notes_fts, rowid, title, note) VALUES ('delete', old.id, old.title, old.note);
END;

CREATE TRIGGER notes_fts_au AFTER UPDATE ON notes_nodes BEGIN
    INSERT INTO notes_fts(notes_fts, rowid, title, note) VALUES ('delete', old.id, old.title, old.note);
    INSERT INTO notes_fts(rowid, title, note) VALUES (new.id, new.title, new.note);
END;
