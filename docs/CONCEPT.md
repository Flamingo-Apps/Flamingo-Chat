# Flamingo Chat

## Pitch

Campus-wide anonymous chat app, continuing an earlier prototype of the same name (~400 users over its first day or two, then usage dropped off). Includes group chat and a gender-wise match finder. Long-term direction: evolve into a campus dating app, but instead of swipe-based matching, users start by chatting anonymously and are allowed to reveal identity after some threshold (time, message count, or mutual opt-in).

See [PRD.md](PRD.md) for the resolved phase 1 decisions (verification, anonymity model, data architecture direction).

## Phase 1 scope (chat app)

- Anonymous 1:1 and group chat, scoped to a single campus (verified via student email or similar)
- Gender-wise match finder for 1:1 anonymous chat
- Group features (topic-based or campus-wide rooms)

## Phase 2 direction (dating evolution)

- Drop the general anonymous chat/group features, keep the matching core
- Chat-first flow: matched users talk anonymously, then unlock a reveal step after a defined condition
- Reveal condition is undecided: time-based, message-count-based, mutual consent, or a combination

## Open questions

- How is campus membership verified (student email domain, ID upload, invite code)?
- What counts as "gender-wise" matching - self-reported, and how is it verified/abused-proofed?
- Moderation: how is harassment/abuse handled in an anonymous context? Who moderates?
- Legal: age verification, data retention for anonymous chats, liability for a dating feature built on top of an anonymous chat base
- Cold start: how do you get enough users from one campus before the match pool is too thin to be useful?
- Is phase 1 (pure chat) valuable enough to stand alone, or is it just a funnel to phase 2?

## Risks

- Network effects: unlike the other two ideas, this one is close to useless below a critical mass of users
- Safety/moderation is a first-class problem, not an afterthought, given anonymity + dating
- Legal exposure is higher than the other two ideas (dating + minors risk if campus includes under-18 students, data privacy for anonymous identities)
