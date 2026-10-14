-- Adds what SignInWithGoogle, SetGender/AdminSetGender and guest cleanup
-- need (DEC-0027, DEC-0028, DEC-0026). badge_verified is dropped: once
-- google_sub exists, "is this account verified" is just "is google_sub
-- set", so keeping a second column that could drift out of sync with it
-- would only be a second source of truth for the same fact.
ALTER TABLE accounts
    ADD COLUMN google_sub TEXT UNIQUE,
    ADD COLUMN gender_set_at TIMESTAMPTZ,
    ADD COLUMN last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now();

ALTER TABLE accounts DROP COLUMN badge_verified;
