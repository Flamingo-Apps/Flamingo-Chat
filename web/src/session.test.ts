import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiRequestError, friendlyError } from './api'
import {
  ACCOUNT_STORAGE_KEY,
  SESSION_STORAGE_KEY,
  clearSession,
  currentToken,
  hasPendingSignIn,
  isSessionRejected,
  pickStorage,
  readStoredSession,
  refreshStoredToken,
  restoreRetryDelay,
  saveAccount,
  saveSession,
  sessionVerdict,
} from './session'
import type { Account } from './types'

const account: Account = { id: 'acc-1', pseudonym: 'Fox', gender: 'unspecified', gender_chosen: false, google_linked: false, created_at_unix: 1 }
const other: Account = { ...account, id: 'acc-2', pseudonym: 'Otter' }

function memoryStorage(initial: Record<string, string> = {}) {
  const data = { ...initial }
  return {
    data,
    getItem: (k: string) => (k in data ? data[k] : null),
    setItem: (k: string, v: string) => void (data[k] = v),
    removeItem: (k: string) => void delete data[k],
  }
}

describe('stored session', () => {
  it('returns nothing without a token', () => {
    expect(readStoredSession(memoryStorage())).toBeNull()
    expect(readStoredSession(memoryStorage({ [ACCOUNT_STORAGE_KEY]: JSON.stringify(account) }))).toBeNull()
  })

  it('returns the token and the saved account when both are intact', () => {
    const storage = memoryStorage()
    saveSession(storage, 'tok', account)
    expect(readStoredSession(storage)).toEqual({ token: 'tok', account })
  })

  it('returns the token with no account when the saved copy is missing, garbage or the wrong shape', () => {
    expect(readStoredSession(memoryStorage({ [SESSION_STORAGE_KEY]: 'tok' }))).toEqual({ token: 'tok', account: null })
    expect(readStoredSession(memoryStorage({ [SESSION_STORAGE_KEY]: 'tok', [ACCOUNT_STORAGE_KEY]: '{not json' }))).toEqual({ token: 'tok', account: null })
    expect(readStoredSession(memoryStorage({ [SESSION_STORAGE_KEY]: 'tok', [ACCOUNT_STORAGE_KEY]: JSON.stringify({ id: 1 }) }))).toEqual({ token: 'tok', account: null })
  })

  it('never throws when storage is blocked', () => {
    const blocked = {
      getItem: () => {
        throw new Error('blocked')
      },
      setItem: () => {
        throw new Error('blocked')
      },
      removeItem: () => {
        throw new Error('blocked')
      },
    }
    expect(readStoredSession(blocked)).toBeNull()
    expect(currentToken(blocked)).toBeNull()
    expect(() => saveSession(blocked, 't', account)).not.toThrow()
    expect(() => saveAccount(blocked, 't', account)).not.toThrow()
    expect(() => refreshStoredToken(blocked, 'a', 'b')).not.toThrow()
    expect(() => clearSession(blocked)).not.toThrow()
  })

  it('clears both the token and the account copy', () => {
    const storage = memoryStorage()
    saveSession(storage, 'tok', account)
    clearSession(storage)
    expect(storage.data).toEqual({})
  })

  it('does not keep an older person\'s account copy next to a token saved without one (just back from Google)', () => {
    const storage = memoryStorage()
    saveSession(storage, 'guest-token', account)
    saveSession(storage, 'google-token') // no account yet
    expect(readStoredSession(storage)).toEqual({ token: 'google-token', account: null })
  })
})

describe('two tabs of one browser', () => {
  it('writes the saved account only for the session this tab still holds', () => {
    const storage = memoryStorage()
    saveSession(storage, 'mine', account)
    clearSession(storage) // another tab signed out
    saveAccount(storage, 'mine', account) // this tab, not yet told
    expect(storage.data).toEqual({})

    saveSession(storage, 'someone-else', other) // another tab signed in as a different person
    saveAccount(storage, 'mine', account)
    expect(readStoredSession(storage)).toEqual({ token: 'someone-else', account: other })
  })

  it('records a renewed token only if storage still holds the one it replaces', () => {
    const storage = memoryStorage()
    saveSession(storage, 'old', account)
    expect(refreshStoredToken(storage, 'old', 'renewed')).toBe(true)
    expect(currentToken(storage)).toBe('renewed')

    clearSession(storage) // signed out in another tab
    expect(refreshStoredToken(storage, 'renewed', 'zombie')).toBe(false)
    expect(currentToken(storage)).toBeNull() // the signed-out session is not brought back to life
  })
})

describe('storage that the browser blocks', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('falls back to a page-lifetime store instead of failing', () => {
    vi.stubGlobal('window', {
      get localStorage(): Storage {
        throw new DOMException('denied', 'SecurityError')
      },
    })
    const storage = pickStorage()
    saveSession(storage, 'tok', account)
    expect(readStoredSession(storage)).toEqual({ token: 'tok', account })
    clearSession(storage)
    expect(readStoredSession(storage)).toBeNull()
  })

  it('uses localStorage when it works', () => {
    const real = memoryStorage()
    vi.stubGlobal('window', { localStorage: real })
    expect(pickStorage()).toBe(real)
  })
})

describe('just back from Google', () => {
  it('knows a session is waiting in the address fragment', () => {
    expect(hasPendingSignIn('#auth_token=abc')).toBe(true)
    expect(hasPendingSignIn('#auth_error=sign_in_failed')).toBe(false)
    expect(hasPendingSignIn('')).toBe(false)
  })
})

// The gateway's real HTTP answers (services/gateway/internal/httpapi/errors.go):
// 401 unauthenticated, 403 permission_denied (with an underscore), 404 not_found.
// A test of this once used the WebSocket's spelling and passed while the real
// answer was never recognised.
describe('which answers end a saved session', () => {
  it('ends it only when the server refuses the session itself', () => {
    expect(sessionVerdict(new ApiRequestError('unauthenticated', 'invalid session token', 401))).toBe('expired')
    expect(sessionVerdict(new ApiRequestError('not_found', 'account not found', 404))).toBe('expired')
    expect(sessionVerdict(new ApiRequestError('permission_denied', 'banned', 403))).toBe('blocked')
    expect(isSessionRejected(new ApiRequestError('permission_denied', 'banned', 403))).toBe(true)
  })

  it('decides on the HTTP status, not on how a code is spelled', () => {
    expect(sessionVerdict(new ApiRequestError('permissiondenied', 'x', 403))).toBe('blocked')
    expect(sessionVerdict(new ApiRequestError('whatever', 'x', 401))).toBe('expired')
  })

  it('keeps the session when the server could not be asked or broke', () => {
    for (const status of [0, 408, 429, 500, 502, 503, 504]) expect(sessionVerdict(new ApiRequestError('unavailable', 'x', status))).toBe('unknown')
    expect(sessionVerdict(new ApiRequestError('bad_response', 'unexpected response', 502))).toBe('unknown') // an HTML error page
    expect(sessionVerdict(new TypeError('Failed to fetch'))).toBe('unknown') // offline
    expect(sessionVerdict(new DOMException('aborted', 'AbortError'))).toBe('unknown') // timed out
    expect(isSessionRejected(new TypeError('Failed to fetch'))).toBe(false)
  })

  it('retries quickly at first, then settles', () => {
    expect(restoreRetryDelay(0)).toBe(1500)
    expect(restoreRetryDelay(1)).toBe(3000)
    expect(restoreRetryDelay(20)).toBe(15_000)
  })
})

describe('what a person is told when a request fails', () => {
  it('never shows a network or parse error, or a service\'s internal text', () => {
    expect(friendlyError(new TypeError('Failed to fetch'))).toMatch(/Check your connection/)
    expect(friendlyError(new SyntaxError("Unexpected token '<'"))).not.toMatch(/Unexpected token/)
    expect(friendlyError(new ApiRequestError('internal', 'rpc error: code = Internal desc = list rooms: context canceled', 500))).toBe("Can't reach Flamingo right now. Please try again.")
    expect(friendlyError(new ApiRequestError('invalid_argument', 'account_id is required', 400))).toBe('Something went wrong. Please try again.')
    expect(friendlyError(new ApiRequestError('bad_response', 'unexpected response', 502))).toBe("Can't reach Flamingo right now. Please try again.")
  })

  it('passes through the messages the gateway writes for people', () => {
    expect(friendlyError(new ApiRequestError('resource_exhausted', 'you are creating channels too quickly, wait a few minutes', 429))).toBe('you are creating channels too quickly, wait a few minutes')
    expect(friendlyError(new ApiRequestError('already_exists', 'pseudonym already taken', 409))).toBe('pseudonym already taken')
  })
})
