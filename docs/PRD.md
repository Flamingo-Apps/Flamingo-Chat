# Product Requirements: Flamingo Chat

Status: v1 scope agreed on 2026-10-02. The reasoning behind each choice is recorded in [decisions.jsonl](decisions.jsonl); entries are referenced below by ID.

## Background

Flamingo Chat continues an earlier prototype of the same name. That version had three screens (choose your gender and who you want to talk to, wait for a match, chat) and reached about 400 users in its first day or two. Usage then dropped off. This rebuild has two aims: a product people return to after day two, and a system that is designed, deployed and measured properly.

## Problem

Students want a low-pressure way to talk to people on their campus they do not already know, without doing it under their real name. Instagram messages and campus WhatsApp groups require showing who you are up front and offer no way to meet someone you have no connection to.

## Users

Students at one college, KIIT. The app is distributed there and nowhere else. It is for meeting new people casually, not for messaging existing friends.

## Product principle

**Share something about yourself to use the same thing about others.** Anyone can try the app without giving anything. Features that depend on personal information, such as matching by gender, are open only to people who have provided that information themselves. (DEC-0026)

## Access tiers

| | Guest | Verified |
|---|---|---|
| How you join | Choose a pseudonym | Sign in with a KIIT Google account |
| Group rooms | Yes | Yes |
| Random 1:1 matching | Yes | Yes |
| Matching filtered by gender | No | Yes, after choosing a gender |
| Sign in on another device | No | Yes |
| Account lifetime | Deleted after 60 days without activity | Kept |

- A guest who signs in later keeps the same account, so their pseudonym and rooms carry over. (DEC-0026)
- Verification accepts only accounts that Google reports as belonging to KIIT's domain. (DEC-0027)
- After signing in, a user chooses male, female or prefer not to say. The choice is made once and is permanent; a correction goes through a request that an admin reviews. Users who prefer not to say cannot filter by gender and are not counted in gender totals. (DEC-0028)

## v1 scope

In scope (DEC-0031):

- **Accounts.** Guest signup with a pseudonym, protected by a CAPTCHA and rate limits. KIIT sign-in. One-time gender choice. Signing in on a new device. Changing your pseudonym.
- **1:1 chat.** Random matching for everyone. Gender-filtered matching for verified users who chose a gender. Skip to the next person, or leave.
- **Keep chatting.** If both people in a random chat choose to, the chat becomes a saved conversation they can return to.
- **Group rooms.** Create a room, join by invite code or link, leave. One default campus-wide room exists from the start. Anyone can create rooms, limited to a few per day per account. Invite codes are reusable and the room owner can replace them.
- **Presence.** A live count of people online, shown as totals for guests, male and female.
- **Safety.** Report and block from any chat. Reports go to an admin queue. Bans follow the Google account for verified users, and the account plus rate limits for guests.
- **History.** Group room messages are saved, so reloading does not empty the room. A random 1:1 chat disappears for its participants when it ends, unless both chose to keep chatting. Its messages are still kept on the server for moderation.
- **Legal.** A privacy policy and terms of use.
- **Operations.** Monitoring is running before the first real user (DEC-0024). Admin actions are done with a command-line tool from inside the cluster; there is no public admin interface (DEC-0029).

Out of scope for v1:

- images, voice and other media
- typing indicators and read receipts
- push notifications
- browsing or searching for groups
- profile pages
- native mobile apps; v1 is a web app designed for phones first
- more than one campus
- monetization

## Anonymity and data

Anonymity is between users, not from the operator. Other users see only a pseudonym. The backend keeps the link between an account and its Google identity, and keeps message data, so that reports can be acted on. This is acceptable because the audience is a single college. The sign-in screen must say plainly what is stored and that no other user sees it. (DEC-0001)

For a verified account the backend stores Google's permanent account identifier and the email address. It does not store the name or profile picture that Google also provides. (DEC-0027)

## Success measures

Every figure reported for these must come from saved evidence, not an estimate. (DEC-0025)

- Daily and weekly active users, and retention on day 1, day 3 and day 7. Retention past day 2 is where the earlier prototype failed.
- The share of matches that turn into a real conversation, and the share that both people choose to keep.
- Reports per active user, as a signal of abuse.
- The split between guests and verified users, and whether they behave differently.

## Non-functional requirements

- **Real-time.** Messages are delivered in under a second in normal conditions.
- **Independent services.** The backend is built as separate services from the start (DEC-0003).
- **Measured.** Every service exposes metrics and takes part in distributed tracing. Latency, throughput and concurrency figures come from load tests with a documented method.
- **Private presence.** Online counts are totals only and never reveal who is online.
- **Timely moderation.** A report can be acted on within minutes, not in a nightly batch.
- **Portable.** Nothing depends on a single cloud provider, so the system can be moved (DEC-0023).

## Open questions

- Why usage of the earlier prototype dropped after two days, beyond what "keep chatting" and persistent rooms address.
- How long messages and moderation records are kept, and who can read them.
- Who moderates during the pilot, besides the maintainer.

## Later

Not planned for v1 and revisited only with real usage data:

- **Group discovery.** A directory to browse and search public rooms. It needs enough rooms to be worth building.
- **Identity reveal.** The longer-term idea is a chat-first alternative to swipe-based dating: matched people talk anonymously and may reveal who they are once a condition is met, such as time, message count or mutual consent. This raises safety, moderation and legal questions that an anonymous chat does not, and it needs enough users on one campus to work at all. (DEC-0002)
