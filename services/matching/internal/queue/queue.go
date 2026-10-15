// Package queue implements the waiting-user matching queue
// docs/ARCHITECTURE.md §3.2 and §5.5 describe: two Redis sorted sets
// (mq:any, scanned by preference=any seekers, and mq:want:{gender},
// scanned by seekers who want a specific gender), one hash per waiting
// account (mq:entry:{account_id}) holding what it takes to decide a
// match, and the avoid:{account_id} sets Moderation populates.
package queue

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	PreferenceAny    = "any"
	PreferenceMale   = "male"
	PreferenceFemale = "female"

	GenderMale = "male"
	// Every other identity.v1.Gender value (unspecified, prefer-not-to-say)
	// is stored as-is and never satisfies a male/female preference - only
	// these two string constants are ever compared against for matching
	// purposes, so no third constant is needed here.
	GenderFemale = "female"
)

// staleAfter is docs/ARCHITECTURE.md §3.2's "entries older than 10
// minutes are dropped" rule.
const staleAfter = 10 * time.Minute

// Candidate is one account's matching-relevant attributes, trusted as
// given by the caller (gateway, per DEC-0014) rather than looked up
// again here - the same trust boundary RequestMatchRequest's own fields
// already establish.
type Candidate struct {
	AccountID  string
	Preference string // PreferenceAny, PreferenceMale or PreferenceFemale
	Gender     string // GenderMale, GenderFemale, or any other identity.v1.Gender string
	Verified   bool
	Pseudonym  string
	// PreviousPartner is who this account was last matched with, so the
	// same pairing isn't immediately repeated (docs/ARCHITECTURE.md
	// §3.2). Empty when there is none.
	PreviousPartner string
}

// MatchResult is what TryMatch decides: either a Partner to pair the
// seeker with right now, or (Matched: false) meaning the seeker has been
// enqueued instead.
type MatchResult struct {
	Matched bool
	Partner Candidate
}

// Queue is the matching queue's behavior, so server can be tested
// against a fake instead of real Redis (AGENTS.md's repository-interface
// convention).
type Queue interface {
	// TryMatch atomically looks for a compatible waiting candidate for
	// seeker; if none exists, it enqueues seeker instead. Atomicity (a
	// single Redis script) is what makes "look for a match, else
	// enqueue" race-free against two seekers arriving at once.
	TryMatch(ctx context.Context, seeker Candidate) (MatchResult, error)
	// Cancel removes accountID from the queue if it is waiting there,
	// reporting whether it actually found and removed an entry.
	Cancel(ctx context.Context, accountID string) (bool, error)
}

// tryMatchScript implements docs/ARCHITECTURE.md §3.2's algorithm:
// scan the oldest 100 candidates in the pool the seeker's own preference
// selects, dropping any older than staleAfter as it goes, matching the
// first one that is mutually compatible, or enqueuing the seeker if none
// is. KEYS are the three fixed/seeker-known keys; a candidate's own
// entry and avoid-set keys aren't known until the scan finds them, so
// they're built with string concatenation inside the script instead -
// safe here since this project's Redis is a single instance, never a
// cluster where key-to-node routing would require every key to be
// declared up front.
var tryMatchScript = redis.NewScript(`
local any_key, want_male_key, want_female_key = KEYS[1], KEYS[2], KEYS[3]
local seeker_id = ARGV[1]
local preference = ARGV[2]
local gender = ARGV[3]
local verified = ARGV[4] == "1"
local pseudonym = ARGV[5]
local now = tonumber(ARGV[6])
local previous_partner = ARGV[7]
local stale_seconds = tonumber(ARGV[8])
local cutoff = now - stale_seconds

local function entry_key(id) return "mq:entry:" .. id end
local function avoid_key(id) return "avoid:" .. id end

local function drop(id)
	redis.call("ZREM", any_key, id)
	redis.call("ZREM", want_male_key, id)
	redis.call("ZREM", want_female_key, id)
	redis.call("DEL", entry_key(id))
end

local scan_key = any_key
if preference == "male" then
	scan_key = want_male_key
elseif preference == "female" then
	scan_key = want_female_key
end

local matched_id, matched_pseudonym, matched_pref, matched_gender, matched_verified
local candidates = redis.call("ZRANGE", scan_key, 0, 99)
for _, candidate_id in ipairs(candidates) do
	if matched_id == nil and candidate_id ~= seeker_id then
		local score = redis.call("ZSCORE", scan_key, candidate_id)
		local fields = redis.call("HMGET", entry_key(candidate_id),
			"preference", "gender", "verified", "pseudonym", "previous_partner")
		local cpref, cgender, cverified, cpseudonym, cprev =
			fields[1], fields[2], fields[3], fields[4], fields[5]
		-- A missing hash (its own TTL already expired) is just as
		-- dangling as a stale score - both mean this ZSET member no
		-- longer refers to anyone actually waiting, so both are cleaned
		-- up the same way instead of being considered a candidate.
		if (not score) or tonumber(score) < cutoff or not cpref then
			drop(candidate_id)
		else
			local ok = true
			if cpref == "male" and not (verified and gender == "male") then ok = false end
			if cpref == "female" and not (verified and gender == "female") then ok = false end
			if ok and cprev == seeker_id then ok = false end
			if ok and previous_partner == candidate_id then ok = false end
			if ok and redis.call("SISMEMBER", avoid_key(seeker_id), candidate_id) == 1 then ok = false end
			if ok and redis.call("SISMEMBER", avoid_key(candidate_id), seeker_id) == 1 then ok = false end
			if ok then
				drop(candidate_id)
				matched_id, matched_pseudonym, matched_pref, matched_gender, matched_verified =
					candidate_id, cpseudonym, cpref, cgender, cverified
			end
		end
	end
end

if matched_id then
	return {"matched", matched_id, matched_pseudonym, matched_pref, matched_gender, matched_verified}
end

redis.call("HSET", entry_key(seeker_id),
	"preference", preference, "gender", gender,
	"verified", verified and "1" or "0",
	"pseudonym", pseudonym, "previous_partner", previous_partner)
-- The hash's own TTL bounds how long a waiting entry can ever linger
-- even in a pool nobody else ever scans again - ZSET membership alone
-- would otherwise dangle forever with no per-member expiry of its own.
redis.call("EXPIRE", entry_key(seeker_id), stale_seconds)
redis.call("ZADD", any_key, now, seeker_id)
if verified and (gender == "male" or gender == "female") then
	local want_key = want_male_key
	if gender == "female" then want_key = want_female_key end
	redis.call("ZADD", want_key, now, seeker_id)
end
return {"queued"}
`)

var cancelScript = redis.NewScript(`
local any_key, want_male_key, want_female_key, entry_key = KEYS[1], KEYS[2], KEYS[3], KEYS[4]
local existed = redis.call("EXISTS", entry_key)
redis.call("ZREM", any_key, ARGV[1])
redis.call("ZREM", want_male_key, ARGV[1])
redis.call("ZREM", want_female_key, ARGV[1])
redis.call("DEL", entry_key)
return existed
`)

// Redis is the Queue implementation backed by a real Redis instance.
type Redis struct {
	client *redis.Client
}

func NewRedis(client *redis.Client) *Redis {
	return &Redis{client: client}
}

func (r *Redis) TryMatch(ctx context.Context, seeker Candidate) (MatchResult, error) {
	verifiedArg := "0"
	if seeker.Verified {
		verifiedArg = "1"
	}
	res, err := tryMatchScript.Run(ctx, r.client,
		[]string{"mq:any", "mq:want:male", "mq:want:female"},
		seeker.AccountID, seeker.Preference, seeker.Gender, verifiedArg, seeker.Pseudonym,
		time.Now().Unix(), seeker.PreviousPartner, int64(staleAfter.Seconds()),
	).Result()
	if err != nil {
		return MatchResult{}, fmt.Errorf("queue: try match: %w", err)
	}

	fields, ok := res.([]interface{})
	if !ok || len(fields) == 0 {
		return MatchResult{}, fmt.Errorf("queue: unexpected script result %#v", res)
	}
	if fields[0] == "queued" {
		return MatchResult{Matched: false}, nil
	}
	if len(fields) != 6 {
		return MatchResult{}, fmt.Errorf("queue: unexpected matched result %#v", res)
	}
	return MatchResult{
		Matched: true,
		Partner: Candidate{
			AccountID:  fields[1].(string),
			Pseudonym:  fields[2].(string),
			Preference: fields[3].(string),
			Gender:     fields[4].(string),
			Verified:   fields[5].(string) == "1",
		},
	}, nil
}

func (r *Redis) Cancel(ctx context.Context, accountID string) (bool, error) {
	existed, err := cancelScript.Run(ctx, r.client,
		[]string{"mq:any", "mq:want:male", "mq:want:female", "mq:entry:" + accountID},
		accountID,
	).Int()
	if err != nil {
		return false, fmt.Errorf("queue: cancel: %w", err)
	}
	return existed == 1, nil
}
