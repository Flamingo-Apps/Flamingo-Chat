# PRD: Flamingo Chat

Status: draft, phase 1 only (anonymous chat). Phase 2 (dating evolution) is intentionally out of scope for this PRD - see [CONCEPT.md](CONCEPT.md) for the longer-term direction, revisit once phase 1 has real usage data.

Flamingo Chat is a continuation of an earlier prototype of the same name, which reached about 400 users over its first day or two before usage dropped off. This round rebuilds it properly: real system design, and a second attempt at what keeps people around past the first couple of days.

## Problem statement

Students at a given campus want a low-friction way to talk to other students they don't already know, without the social risk of doing it under their real identity. Existing options (Instagram DMs, campus WhatsApp groups) require revealing identity up front and don't have a structured way to meet people you don't already have a connection to.

## Target users

- Students at a single college (v1 scoped to one campus - your own college - not multi-campus)
- Primarily students looking to meet new people casually, not an existing-friends messaging replacement

## Scope - Phase 1

In scope:

- Two access tiers, no forced verification for the core product:
  - **Unverified**: sign up freely, access general anonymous chat and group rooms, no gender-specific matching
  - **Verified ("Badged")**: verify via college ID/email (exact mechanism TBD - likely college email OAuth), unlocks gender-specific match finder. Framed positively at the verification prompt (something like "Get Badged" / "Get Verified") rather than as an intimidating identity check
- Anonymous 1:1 chat, matched via a gender-wise match finder (verified users only)
- Group chat rooms (topic-based or campus-wide, open to unverified and verified users)
  - Creatable by users (creation permissions - all users or verified only - TBD, see open questions)
  - Joinable via invite link or code
- Stable per-account pseudonym (Reddit-style: persists across all of a user's conversations), user-chosen and user-changeable at any time
- Reporting/blocking a user mid-chat
- Live online user count, broken down by unverified / male / female, shown in the app

Out of scope for phase 1:

- The reveal/dating mechanic (phase 2)
- Multi-campus support
- Media sharing beyond text (images/voice notes deferred unless explicitly prioritized)
- Monetization
- Group search/discovery directory - deferred until there are enough groups for search to be worth building (see Future scope)

## Future scope (post-phase-1, not yet prioritized)

- **Group search/discovery**: a directory to browse/search public groups by name or topic. Deferred rather than cut, because it needs an indexing/search component that isn't worth building before there's enough group content to search. Revisit once group creation is live and there's real data on how many groups exist and whether people are already finding them via link/code sharing alone.

## Anonymity and data model

Anonymity is user-facing only, not a backend guarantee. The pseudonym is what other users see; the backend retains the real identity mapping and full message data for moderation purposes. This is an acceptable tradeoff because the audience is a single college (known, bounded user base), not a general public product - the privacy bar is "other users can't identify you," not "the operator can't either."

Data retention specifics (how long logs are kept, who can access them) still need a concrete policy, but the direction is: real-time messages flow through a fast in-memory layer (e.g. Redis) for live delivery, and are asynchronously persisted by workers into durable storage for moderation/history. The persistence layer should be treated as swappable/scalable from day one (not hard-wired to one database), since this is one of the areas you specifically want to learn by building it. Concrete choices (message broker, database engine, sharding strategy) belong in System Design, not this PRD.

## Success metrics (draft - needs your input)

- Daily/weekly active users, and specifically retention past day 2-3, since that's exactly where the previous prototype fell off
- Match-to-conversation rate (% of matches that result in an actual back-and-forth chat)
- Report rate per active user (proxy for safety/abuse problems)
- Verified vs. unverified split, and whether verified users behave differently (engagement, report rate)

## Non-functional requirements

- **Scalable and distributed by design**: built as independent services from the start, not a monolith split later
- **This project is also a deliberate learning vehicle** for distributed systems concepts - API gateways, caching layers, service decomposition, swappable/pluggable data stores, worker-based async persistence. Where reasonable for this scale, prefer demonstrating a solid, well-known distributed pattern over the single simplest solution, and treat System Design as the place to make that tradeoff explicit rather than silently defaulting to "simplest thing that works"
- Real-time messaging with low latency (sub-second delivery expectation)
- Moderation must be able to act in near-real-time on reports (not a nightly batch process)
- Live presence counts must be aggregate-only (unverified / male / female tallies) - never expose enough detail to identify who's online, and gender tallies only ever include verified users who set a gender
- **Observability and benchmarking are first-class deliverables, not an afterthought**: every service should expose Prometheus metrics and participate in distributed tracing (e.g. OpenTelemetry + Jaeger) from day one, since this project doubles as a portfolio piece and retrofitting observability later is more work than designing it in. Inter-service communication should use gRPC where it makes sense (chat/matching/moderation/presence), and the real-time delivery + persistence pipeline should run through a message broker (RabbitMQ/NATS/Kafka - pick one in System Design). Concrete latency/throughput/concurrency numbers should come from actual load testing (k6/vegeta/ghz) against the built system with a documented methodology, not estimated in advance

## Resolved decisions

1. **Campus verification**: college ID/email based (OAuth preferred, exact provider/flow TBD), required only for gender-specific matching, not for general use
2. **Gender field**: only collected/required for verified users going for gender-specific matching
3. **Moderation model**: backend keeps full data (identity + messages) for moderation; anonymity is enforced only at the user-facing layer. Single-college launch keeps legal/scale exposure low. Moderator staffing (you, volunteers) still TBD
4. **Anonymity persistence**: stable per-account pseudonym, user-changeable
5. **Launch campus**: your own college, continuing from the original Flamingo Chat prototype
6. **Data retention/architecture**: real-time cache (e.g. Redis) plus async worker-persisted durable storage, with the storage layer designed to be swappable/scalable - detailed design deferred to System Design

## Open questions

- What made the original prototype's usage drop off after a day or two, and what in this rebuild specifically addresses that (better system design alone may not fix a retention problem)?
- Exact verification mechanism: does your college provide OAuth, or does this need email-domain + magic link instead?
- Moderator staffing for the pilot: just you, or others?
- Concrete data retention window and policy for moderation logs
- Group creation permissions: can any user create a group, or verified users only? Unrestricted creation risks spam/low-quality groups; needs at least a rate limit even if unverified users are allowed to create
- Invite codes/links: do they expire, are they single-use or reusable, and can a group owner revoke/regenerate one?

## Milestones (draft)

1. System Design finalized (microservices architecture, data model, caching/persistence strategy)
2. Pilot build: signup (unverified + verified paths), 1:1 matching, group chat, basic chat, report/block
3. Closed pilot at your college
4. Iterate based on pilot metrics, with particular attention to day-2/day-3 retention, before expanding scope
