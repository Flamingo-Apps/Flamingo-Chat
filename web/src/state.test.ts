import { describe, expect, it } from 'vitest'
import { activeRandomChat, initialState, reducer, signInErrorMessage } from './state'
import type { Account, Message, Room } from './types'

const account: Account = {
  id: 'acc-1',
  pseudonym: 'Fox',
  gender: 'unspecified',
  gender_chosen: false,
  google_linked: false,
  created_at_unix: 1000,
}

function message(overrides: Partial<Message> = {}): Message {
  return {
    message_id: 'm1',
    room_id: 'room-1',
    sender_account_id: 'acc-2',
    sender_pseudonym: 'Hound',
    body: 'hi',
    sent_at_unix: 100,
    ...overrides,
  }
}

describe('reducer', () => {
  it('sets account and token on session', () => {
    const state = reducer(initialState, { type: 'session', account, token: 'tok' })
    expect(state.account).toEqual(account)
    expect(state.token).toBe('tok')
  })

  it('resets everything on logout', () => {
    const loggedIn = reducer(initialState, { type: 'session', account, token: 'tok' })
    expect(reducer(loggedIn, { type: 'logout' })).toEqual(initialState)
  })

  it('appends a live message frame to its room', () => {
    const state = reducer(initialState, {
      type: 'frame',
      frame: { type: 'message', message_id: 'm1', room_id: 'room-1', sender_account_id: 'acc-2', sender_pseudonym: 'Hound', body: 'hi', sent_at: 100 },
    })
    expect(state.messagesByRoom['room-1']).toEqual([message()])
  })

  it('does not duplicate a message already seen live when history loads the same one', () => {
    let state = reducer(initialState, {
      type: 'frame',
      frame: { type: 'message', message_id: 'm1', room_id: 'room-1', sender_account_id: 'acc-2', sender_pseudonym: 'Hound', body: 'hi', sent_at: 100 },
    })
    state = reducer(state, { type: 'messagesLoaded', roomId: 'room-1', messages: [message(), message({ message_id: 'm2', sent_at_unix: 50 })] })
    expect(state.messagesByRoom['room-1']).toHaveLength(2)
    expect(state.messagesByRoom['room-1'].map((m) => m.message_id)).toEqual(['m2', 'm1']) // sorted by sent_at
  })

  it('does not duplicate when the same live message frame arrives twice (e.g. after a reconnect replay)', () => {
    const frame = { type: 'message' as const, message_id: 'm1', room_id: 'room-1', sender_account_id: 'acc-2', sender_pseudonym: 'Hound', body: 'hi', sent_at: 100 }
    let state = reducer(initialState, { type: 'frame', frame })
    state = reducer(state, { type: 'frame', frame })
    expect(state.messagesByRoom['room-1']).toHaveLength(1)
  })

  it('tracks a match from queued through found to kept', () => {
    let state = reducer(initialState, { type: 'frame', frame: { type: 'match_queued' } })
    expect(state.matching).toBe(true)

    state = reducer(state, {
      type: 'frame',
      frame: { type: 'match_found', room_id: 'room-1', partner_pseudonym: 'Hound', partner_account_id: 'acc-2' },
    })
    expect(state.matching).toBe(false)
    expect(state.currentMatch).toEqual({
      roomId: 'room-1', partnerPseudonym: 'Hound', partnerAccountId: 'acc-2', youKept: false, partnerKept: false, kept: false,
      partnerLeftAt: null, foundSeq: expect.any(Number),
    })

    state = reducer(state, { type: 'frame', frame: { type: 'keep_status', room_id: 'room-1', you: true, partner: false, kept: false } })
    expect(state.currentMatch?.youKept).toBe(true)
    expect(state.currentMatch?.kept).toBe(false)

    state = reducer(state, { type: 'frame', frame: { type: 'match_ended', room_id: 'room-1', reason: 'you_left' } })
    expect(state.currentMatch).toBeNull()
  })

  it('stops showing "looking for a match" once the server confirms a cancel (DEC-0103)', () => {
    let state = reducer(initialState, { type: 'frame', frame: { type: 'match_queued' } })
    expect(state.matching).toBe(true)

    state = reducer(state, { type: 'frame', frame: { type: 'match_cancelled' } })
    expect(state.matching).toBe(false)
  })

  it('keeps a match that was found before a late match_cancelled arrives', () => {
    let state = reducer(initialState, { type: 'frame', frame: { type: 'match_queued' } })
    state = reducer(state, {
      type: 'frame',
      frame: { type: 'match_found', room_id: 'room-1', partner_pseudonym: 'Hound', partner_account_id: 'acc-2' },
    })

    state = reducer(state, { type: 'frame', frame: { type: 'match_cancelled' } })
    expect(state.matching).toBe(false)
    expect(state.currentMatch?.roomId).toBe('room-1')
  })

  it('ignores keep_status and match_ended for a room that is not the current match', () => {
    const state = reducer(initialState, { type: 'frame', frame: { type: 'match_ended', room_id: 'some-other-room', reason: 'partner_left' } })
    expect(state).toEqual(initialState)
  })

  it('shows a grace-window prompt instead of dropping the match when the partner leaves (DEC-0091)', () => {
    let state = reducer(initialState, { type: 'session', account, token: 'tok' })
    state = reducer(state, {
      type: 'frame',
      frame: { type: 'match_found', room_id: 'room-1', partner_account_id: 'acc-2', partner_pseudonym: 'Fox' },
    })

    state = reducer(state, { type: 'frame', frame: { type: 'match_ended', room_id: 'room-1', reason: 'partner_left' } })
    expect(state.currentMatch?.roomId).toBe('room-1')
    expect(state.currentMatch?.partnerLeftAt).not.toBeNull()

    // A solo keep vote landing during the grace window saves the chat for
    // the person who stayed: it leaves the Random screen for their Inbox,
    // read-only because the other person is gone (DEC-0106).
    state = reducer(state, { type: 'frame', frame: { type: 'keep_status', room_id: 'room-1', you: true, partner: false, kept: true } })
    expect(state.currentMatch).toBeNull()
    expect(state.savedMatch).toEqual({ roomId: 'room-1', partnerPseudonym: 'Fox', readOnly: true })
  })

  it('clears everything and records the reason on kicked', () => {
    const loggedIn = reducer(initialState, { type: 'session', account, token: 'tok' })
    const state = reducer(loggedIn, { type: 'frame', frame: { type: 'kicked', reason: 'banned' } })
    expect(state.account).toBeNull()
    expect(state.kicked).toBe('banned')
  })

  it('records presence counts', () => {
    const state = reducer(initialState, { type: 'frame', frame: { type: 'presence', total: 5, guests: 2, male: 2, female: 1 } })
    expect(state.presence).toEqual({ total: 5, guests: 2, male: 2, female: 1 })
  })

  describe('keeping a chat (DEC-0106)', () => {
    const matched = () => {
      let state = reducer(initialState, { type: 'session', account, token: 'tok' })
      state = reducer(state, {
        type: 'frame',
        frame: { type: 'match_found', room_id: 'room-1', partner_account_id: 'acc-2', partner_pseudonym: 'Quiet Otter' },
      })
      return state
    }

    it('records that the other person asked to keep, for the current chat only', () => {
      const state = reducer(matched(), { type: 'frame', frame: { type: 'keep_requested', room_id: 'room-1' } })
      expect(state.keepRequest?.roomId).toBe('room-1')

      const other = reducer(matched(), { type: 'frame', frame: { type: 'keep_requested', room_id: 'another-room' } })
      expect(other.keepRequest).toBeNull()
    })

    it('does not ask someone who has already said yes themselves', () => {
      let state = matched()
      state = reducer(state, { type: 'frame', frame: { type: 'keep_status', room_id: 'room-1', you: true, partner: false, kept: false } })
      state = reducer(state, { type: 'frame', frame: { type: 'keep_requested', room_id: 'room-1' } })
      expect(state.keepRequest).toBeNull()
    })

    it('lets the person carry on chatting, which closes the question', () => {
      let state = reducer(matched(), { type: 'frame', frame: { type: 'keep_requested', room_id: 'room-1' } })
      state = reducer(state, { type: 'keepRequestDismissed' })
      expect(state.keepRequest).toBeNull()
      expect(state.currentMatch?.roomId).toBe('room-1')
    })

    it('moves a mutually kept chat out of Random and into the Inbox', () => {
      let state = reducer(matched(), { type: 'frame', frame: { type: 'keep_requested', room_id: 'room-1' } })
      state = reducer(state, { type: 'frame', frame: { type: 'keep_status', room_id: 'room-1', you: true, partner: true, kept: true } })
      expect(state.currentMatch).toBeNull()
      expect(state.keepRequest).toBeNull()
      expect(state.savedMatch).toEqual({ roomId: 'room-1', partnerPseudonym: 'Quiet Otter', readOnly: false })
    })

    it('drops a pending question when the chat ends', () => {
      let state = reducer(matched(), { type: 'frame', frame: { type: 'keep_requested', room_id: 'room-1' } })
      state = reducer(state, { type: 'frame', frame: { type: 'match_ended', room_id: 'room-1', reason: 'partner_left' } })
      expect(state.keepRequest).toBeNull()
    })

    it('lets the saved-chat notice be dismissed', () => {
      let state = reducer(matched(), { type: 'frame', frame: { type: 'keep_status', room_id: 'room-1', you: true, partner: true, kept: true } })
      state = reducer(state, { type: 'savedMatchDismissed' })
      expect(state.savedMatch).toBeNull()
    })
  })

  describe('a Google sign-in that came back with an error', () => {
    it('shows a message instead of failing silently', () => {
      const state = reducer(initialState, { type: 'signInFailed', reason: 'sign_in_failed' })
      expect(state.lastError).toMatch(/KIIT Google account/)
    })

    it('has plain advice for every reason the gateway can send, and for an unknown one', () => {
      for (const reason of ['sign_in_failed', 'missing_code_or_state', 'invalid_or_expired_state', 'google_exchange_failed', 'something_new']) {
        expect(signInErrorMessage(reason)).not.toBe('')
      }
      expect(signInErrorMessage('something_new')).toMatch(/try again/i)
    })

    it('can be dismissed like any other error', () => {
      let state = reducer(initialState, { type: 'signInFailed', reason: 'google_exchange_failed' })
      state = reducer(state, { type: 'clearError' })
      expect(state.lastError).toBeNull()
    })
  })

  describe('restoring the random chat from the server after a refresh or a reconnect (DEC-0112)', () => {
    const room = (overrides: Partial<Room> = {}): Room => ({
      id: 'room-1',
      type: 'direct',
      kept: false,
      created_at_unix: 100,
      partner_pseudonym: 'Quiet Otter',
      partner_account_id: 'acc-2',
      ...overrides,
    })

    it('picks the open, unsaved direct chat, newest first, and ignores everything else', () => {
      const rooms = [
        room({ id: 'saved', kept: true, created_at_unix: 900 }),
        room({ id: 'ended', ended_at_unix: 50, created_at_unix: 800 }),
        room({ id: 'group', type: 'group', created_at_unix: 700 }),
        room({ id: 'older', created_at_unix: 100 }),
        room({ id: 'newer', created_at_unix: 200 }),
      ]
      expect(activeRandomChat(rooms)?.id).toBe('newer')
      expect(activeRandomChat([room({ kept: true })])).toBeNull()
    })

    it('puts someone back in their chat after a refresh', () => {
      const state = reducer(initialState, { type: 'matchReconciled', rooms: [room()] })
      expect(state.currentMatch).toEqual({
        roomId: 'room-1', partnerPseudonym: 'Quiet Otter', partnerAccountId: 'acc-2',
        youKept: false, partnerKept: false, kept: false, partnerLeftAt: null, foundSeq: expect.any(Number),
      })
      expect(state.matching).toBe(false)
      expect(state.rooms).toHaveLength(1)
    })

    it('keeps a chat that is already on screen exactly as it is, so live details are not lost', () => {
      let state = reducer(initialState, {
        type: 'frame',
        frame: { type: 'match_found', room_id: 'room-1', partner_pseudonym: 'Quiet Otter', partner_account_id: 'acc-2' },
      })
      state = reducer(state, { type: 'frame', frame: { type: 'keep_status', room_id: 'room-1', you: true, partner: false, kept: false } })
      const after = reducer(state, { type: 'matchReconciled', rooms: [room()] })
      expect(after.currentMatch?.youKept).toBe(true)
    })

    it('clears a chat that ended while the person was away, and says so', () => {
      let state = reducer(initialState, {
        type: 'frame',
        frame: { type: 'match_found', room_id: 'room-1', partner_pseudonym: 'Quiet Otter', partner_account_id: 'acc-2' },
      })
      state = reducer(state, { type: 'matchReconciled', rooms: [] })
      expect(state.currentMatch).toBeNull()
      expect(state.lastError).toBe('That chat has ended.')
    })

    it('leaves the short "keep this chat?" window alone when the partner has just left', () => {
      let state = reducer(initialState, {
        type: 'frame',
        frame: { type: 'match_found', room_id: 'room-1', partner_pseudonym: 'Quiet Otter', partner_account_id: 'acc-2' },
      })
      state = reducer(state, { type: 'frame', frame: { type: 'match_ended', room_id: 'room-1', reason: 'partner_left' } })
      const after = reducer(state, { type: 'matchReconciled', rooms: [] })
      expect(after.currentMatch?.roomId).toBe('room-1')
      expect(after.lastError).toBeNull()
    })

    it('switches to the chat the server names when it differs from the one on screen', () => {
      let state = reducer(initialState, {
        type: 'frame',
        frame: { type: 'match_found', room_id: 'old', partner_pseudonym: 'Old Fox', partner_account_id: 'acc-9' },
      })
      state = reducer(state, { type: 'matchReconciled', rooms: [room({ id: 'new' })] })
      expect(state.currentMatch?.roomId).toBe('new')
    })

    it('stops showing "looking for a match" when the server says they are already in a chat', () => {
      let state = reducer(initialState, { type: 'frame', frame: { type: 'match_queued' } })
      state = reducer(state, { type: 'matchReconciled', rooms: [room()] })
      expect(state.matching).toBe(false)
      expect(state.currentMatch?.roomId).toBe('room-1')
    })

    it('does nothing to a search in progress when there is no open chat', () => {
      let state = reducer(initialState, { type: 'frame', frame: { type: 'match_queued' } })
      state = reducer(state, { type: 'matchReconciled', rooms: [] })
      expect(state.matching).toBe(true)
      expect(state.lastError).toBeNull()
    })

    it('has a plain message for a session that has ended', () => {
      expect(signInErrorMessage('session_expired')).toMatch(/session has ended/i)
    })
  })

  describe('a match and its acknowledgement arriving in either order (INC-0030)', () => {
    const found = { type: 'match_found' as const, room_id: 'room-1', partner_pseudonym: 'Kind Jay', partner_account_id: 'acc-2' }

    it('searching, then found: the normal order', () => {
      let state = reducer(initialState, { type: 'searchRequested' })
      state = reducer(state, { type: 'frame', frame: { type: 'match_queued' } })
      expect(state.matching).toBe(true)
      state = reducer(state, { type: 'frame', frame: found })
      expect(state.matching).toBe(false)
      expect(state.currentMatch?.roomId).toBe('room-1')
    })

    it('found BEFORE the acknowledgement: the chat must survive the late "queued"', () => {
      // The person whose Find completed the pairing, on the afternoon it went wrong:
      // the match reached the screen ahead of "queued", which used to erase it
      // and leave "Looking for someone" up while the partner was already chatting.
      let state = reducer(initialState, { type: 'searchRequested' })
      state = reducer(state, { type: 'frame', frame: found })
      state = reducer(state, { type: 'frame', frame: { type: 'match_queued' } })
      expect(state.currentMatch?.roomId).toBe('room-1')
      expect(state.matching).toBe(false)
    })

    it('Next: the old chat is replaced when its end arrives, in either order', () => {
      let base = reducer(initialState, { type: 'frame', frame: { ...found, room_id: 'old' } })
      base = reducer(base, { type: 'searchRequested' }) // the person pressed Next

      // queued, then the old chat ends: now searching
      let a = reducer(base, { type: 'frame', frame: { type: 'match_queued' } })
      a = reducer(a, { type: 'frame', frame: { type: 'match_ended', room_id: 'old', reason: 'you_left' } })
      expect(a.currentMatch).toBeNull()
      expect(a.matching).toBe(true)

      // the new match first, then queued, then the old chat's end: in the new chat, searching over
      let b = reducer(base, { type: 'frame', frame: { ...found, room_id: 'new' } })
      b = reducer(b, { type: 'frame', frame: { type: 'match_queued' } })
      b = reducer(b, { type: 'frame', frame: { type: 'match_ended', room_id: 'old', reason: 'you_left' } })
      expect(b.currentMatch?.roomId).toBe('new')
      expect(b.matching).toBe(false)
    })

    it('a search that was only just requested still shows as searching after queued', () => {
      const state = reducer(reducer(initialState, { type: 'searchRequested' }), { type: 'frame', frame: { type: 'match_queued' } })
      expect(state.matching).toBe(true)
    })

    it('a lost announcement is healed by asking the server which chat the person is in', () => {
      let state = reducer(initialState, { type: 'searchRequested' })
      state = reducer(state, { type: 'frame', frame: { type: 'match_queued' } })
      state = reducer(state, {
        type: 'matchReconciled',
        rooms: [{ id: 'room-9', type: 'direct', kept: false, created_at_unix: 5, partner_pseudonym: 'Kind Jay', partner_account_id: 'acc-2' }],
      })
      expect(state.matching).toBe(false)
      expect(state.currentMatch?.roomId).toBe('room-9')
    })
  })

  describe('starting over', () => {
    it('a new sign-in does not keep an old "session ended" message on screen', () => {
      let state = reducer(initialState, { type: 'signInFailed', reason: 'session_expired' })
      expect(state.lastError).toMatch(/session has ended/i)
      state = reducer(state, { type: 'session', account, token: 'tok' })
      expect(state.lastError).toBeNull()
    })

    it('says plainly when an account cannot be used any more', () => {
      expect(signInErrorMessage('account_blocked')).toMatch(/can't be used/i)
    })
  })
})
