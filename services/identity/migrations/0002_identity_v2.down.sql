ALTER TABLE accounts ADD COLUMN badge_verified BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE accounts
    DROP COLUMN google_sub,
    DROP COLUMN gender_set_at,
    DROP COLUMN last_seen_at;
