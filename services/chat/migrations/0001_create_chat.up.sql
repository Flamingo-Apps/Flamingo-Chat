CREATE TABLE rooms (
    id UUID PRIMARY KEY,
    -- Matches chat.v1.RoomType's wire values (1 = direct, 2 = group), kept
    -- in sync by convention rather than a Postgres enum, the same choice
    -- identity's gender column made (see its migrations).
    type SMALLINT NOT NULL,
    name TEXT,
    owner_account_id UUID,
    invite_code TEXT UNIQUE,
    -- Direct rooms only; true once both participants have voted keep
    -- (DEC-0031).
    kept BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ended_at TIMESTAMPTZ
);

-- A member row with left_at set is a former member, kept for history
-- (ListMessages still needs to confirm a caller was ever in the room) and
-- so rejoining a group room via invite code resets left_at rather than
-- fighting the primary key.
CREATE TABLE room_members (
    room_id UUID NOT NULL REFERENCES rooms (id),
    account_id UUID NOT NULL,
    joined_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    left_at TIMESTAMPTZ,
    PRIMARY KEY (room_id, account_id)
);

-- Written by the persistence worker consuming chat.events, not by Chat's
-- own SendMessage path, which only produces to Kafka (DEC-0047,
-- docs/ARCHITECTURE.md §3.1) - Chat owns this schema because the
-- persistence worker is effectively its background half. message_id is
-- the primary key specifically so the persistence worker's insert can be
-- "ON CONFLICT DO NOTHING" and stay correct under Kafka's at-least-once
-- redelivery.
CREATE TABLE messages (
    message_id UUID PRIMARY KEY,
    room_id UUID NOT NULL REFERENCES rooms (id),
    sender_account_id UUID NOT NULL,
    sender_pseudonym TEXT NOT NULL,
    body TEXT NOT NULL,
    sent_at TIMESTAMPTZ NOT NULL
);

-- ListMessages pages newest-first by (sent_at, message_id) - message_id
-- breaks ties between messages with the same timestamp, since sent_at
-- alone is not guaranteed unique.
CREATE INDEX messages_room_sent_at_idx ON messages (room_id, sent_at DESC, message_id DESC);

CREATE TABLE keep_votes (
    room_id UUID NOT NULL REFERENCES rooms (id),
    account_id UUID NOT NULL,
    voted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (room_id, account_id)
);
