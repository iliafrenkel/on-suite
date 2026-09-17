-- internal/apps/flash/migrations/0003_tags.sql
CREATE TABLE flash_tags (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name    TEXT    NOT NULL
) STRICT;

CREATE UNIQUE INDEX flash_tags_user_name_idx ON flash_tags (user_id, name);

CREATE TABLE flash_card_tags (
    card_id INTEGER NOT NULL REFERENCES flash_cards (id) ON DELETE CASCADE,
    tag_id  INTEGER NOT NULL REFERENCES flash_tags (id) ON DELETE CASCADE,
    PRIMARY KEY (card_id, tag_id)
) STRICT, WITHOUT ROWID;
