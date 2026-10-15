CREATE TABLE reports (
    id UUID PRIMARY KEY,
    reporter_account_id UUID NOT NULL,
    reported_account_id UUID NOT NULL,
    -- Empty when the report isn't about a specific room (ARCHITECTURE
    -- §3.5 doesn't require one).
    room_id UUID,
    reason TEXT NOT NULL,
    resolved BOOLEAN NOT NULL DEFAULT false,
    -- Not UUID: whoever (or whatever) operates tools/admin (DEC-0029) is
    -- not necessarily a real identity account - nothing in this project
    -- gives admins their own accounts, so this is just an opaque
    -- identifier string, the same as moderationv1's own resolved_by
    -- field is typed.
    resolved_by TEXT,
    outcome TEXT,
    filed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ
);

-- One row per direction - if A blocks B, that's the only row; whether B
-- also blocks A is a separate, independent row. The mutual avoid:{id}
-- Redis sets ARCHITECTURE §3.5 describes are a derived, durable-free
-- cache of this table for Matching to read cheaply, not the source of
-- truth.
CREATE TABLE blocks (
    account_id UUID NOT NULL,
    blocked_account_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (account_id, blocked_account_id)
);

-- One row per account, re-used (not re-inserted) across ban/unban/re-ban
-- cycles, so the row's own history - who banned it, who unbanned it, and
-- when - never needs more than one place to look.
CREATE TABLE bans (
    account_id UUID PRIMARY KEY,
    reason TEXT NOT NULL,
    -- Not UUID - see reports.resolved_by's comment.
    banned_by TEXT NOT NULL,
    banned_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    unbanned_by TEXT,
    unbanned_at TIMESTAMPTZ
);

CREATE TABLE gender_change_requests (
    id UUID PRIMARY KEY,
    account_id UUID NOT NULL,
    -- Matches identity.v1.Gender's wire values, the same convention
    -- chat's rooms.type column already uses for chat.v1.RoomType.
    requested_gender SMALLINT NOT NULL,
    note TEXT,
    resolved BOOLEAN NOT NULL DEFAULT false,
    approved BOOLEAN NOT NULL DEFAULT false,
    -- Not UUID - see reports.resolved_by's comment.
    resolved_by TEXT,
    requested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ
);

-- Every admin action (ResolveReport, BanAccount, UnbanAccount,
-- ResolveGenderChangeRequest) logs one row here, in addition to updating
-- its own table - ARCHITECTURE §6's data model calls for a record of
-- "who, whom, why, when, outcome" independent of whatever table the
-- action itself touched.
CREATE TABLE admin_actions (
    id UUID PRIMARY KEY,
    -- Not UUID - see reports.resolved_by's comment. target_account_id,
    -- by contrast, is always a real identity account - the one the
    -- action was taken against.
    actor_account_id TEXT NOT NULL,
    action TEXT NOT NULL,
    target_account_id UUID,
    detail TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
