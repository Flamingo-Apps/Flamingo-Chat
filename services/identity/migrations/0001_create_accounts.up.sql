CREATE TABLE accounts (
    id              UUID PRIMARY KEY,
    pseudonym       TEXT NOT NULL UNIQUE,
    badge_verified  BOOLEAN NOT NULL DEFAULT FALSE,
    gender          SMALLINT NOT NULL DEFAULT 0,
    verified_email  TEXT UNIQUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
