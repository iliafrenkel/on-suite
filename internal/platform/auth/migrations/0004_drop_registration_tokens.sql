-- internal/platform/auth/migrations/0004_drop_registration_tokens.sql
-- Removes the RFC 7592 client management tokens that dynamic registration
-- used to store (#629). ON Suite never mounted the management endpoint, so
-- they could not be used, and they never expired. Registration no longer
-- issues or stores them.
DELETE FROM oauth_sessions WHERE kind = 'registration';
