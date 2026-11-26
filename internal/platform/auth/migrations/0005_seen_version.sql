-- internal/platform/auth/migrations/0005_seen_version.sql
-- Remembers the last ON Suite version each user saw the changelog for, so
-- the shell can show a "What's new" badge after an upgrade. Existing users
-- get '' and see the badge once; new accounts are created with the running
-- version (Store.SetVersion), so nobody is badged on their first login.
ALTER TABLE users ADD COLUMN seen_version TEXT NOT NULL DEFAULT '';
