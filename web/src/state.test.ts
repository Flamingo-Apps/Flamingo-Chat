import { describe, expect, it } from 'vitest'
import { initialState, reducer } from './state'
import type { Account, Message } from './types'

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
      partnerLeftAt: null,
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

    // A solo keep vote landing during the grace window should still work
    // the same way a normal mutual keep_status does.
    state = reducer(state, { type: 'frame', frame: { type: 'keep_status', room_id: 'room-1', you: true, partner: false, kept: true } })
    expect(state.currentMatch?.kept).toBe(true)
    expect(state.currentMatch?.roomId).toBe('room-1')

    state = reducer(state, { type: 'graceWindowExpired', roomId: 'room-1' })
    expect(state.currentMatch).toBeNull()
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
})
