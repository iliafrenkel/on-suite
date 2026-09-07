-- Enables FTS5 prefix indexing so a live filter matches while a word is
-- still being typed ("categ" matching "categorically"), not only once a
-- whole token is complete. FTS5's tokenize=/prefix= configuration cannot be
-- altered on an existing virtual table, so notes_fts is dropped and
-- recreated under the same name. The AFTER INSERT/DELETE/UPDATE triggers on
-- notes_nodes (0003_search.sql) reference notes_fts by name only, not by
-- any binding that could break across this — they keep working unchanged
-- once it exists again below.
DROP TABLE notes_fts;

CREATE VIRTUAL TABLE notes_fts USING fts5(
    title, note, content='notes_nodes', content_rowid='id',
    tokenize='unicode61', prefix='2 3 4'
);

INSERT INTO notes_fts(rowid, title, note) SELECT id, title, note FROM notes_nodes;
