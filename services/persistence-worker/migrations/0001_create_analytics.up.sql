-- persistence-worker's own schema (DEC-0069) - separate from Chat's
-- schema, which it also writes into (see the INSERT in internal/store,
-- not a migration here: that table already exists by the time
-- persistence-worker needs it, created by Chat's own migration).
CREATE SCHEMA IF NOT EXISTS analytics;

-- A generic log of every event this worker consumes, across all three
-- Kafka topics, kept for analysis that doesn't need its own dedicated
-- table (match counts, report counts, keep-vote rates) - unlike
-- MessageSent, which also gets a structured row in Chat's own messages
-- table because ListMessages actually reads it back.
--
-- event_id is the primary key so a redelivered event (Kafka's
-- at-least-once guarantee) is a no-op insert, not a duplicate row -
-- the same idempotency shape DEC-0047 already established for
-- messages.
CREATE TABLE analytics.events (
    event_id UUID PRIMARY KEY,
    topic TEXT NOT NULL,
    -- The payload's oneof case name (e.g. "MessageSent", "MatchFound"),
    -- not the envelope type - "which kind of thing happened" is what
    -- analysis actually groups by.
    event_kind TEXT NOT NULL,
    payload BYTEA NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX events_topic_kind_occurred_at_idx ON analytics.events (topic, event_kind, occurred_at);
