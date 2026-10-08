-- Text alignment in the reading view: ragged-right ('left') or justified.
ALTER TABLE later_prefs ADD COLUMN align TEXT NOT NULL DEFAULT 'left' CHECK (align IN ('left', 'justify'));
