import { describe, expect, it } from 'vitest'
import { ApiRequestError } from './api'
import { ACCOUNT_STORAGE_KEY, SESSION_STORAGE_KEY, clearSession, hasPendingSignIn, isSessionRejected, readStoredSession, saveAccount, saveSession } from './session'
import type { Account } from './types'

const account: Account = { id: 'acc-1', pseudonym: 'Fox', gender: 'unspecified', gender_chosen: false, google_linked: false, created_at_unix: 1 }

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
    expect(() => saveSession(blocked, 't', account)).not.toThrow()
    expect(() => saveAccount(blocked, account)).not.toThrow()
    expect(() => clearSession(blocked)).not.toThrow()
  })

  it('clears both the token and the account copy', () => {
    const storage = memoryStorage()
    saveSession(storage, 'tok', account)
    clearSession(storage)
    expect(storage.data).toEqual({})
  })
})

describe('just back from Google', () => {
  it('knows a session is waiting in the address fragment', () => {
    expect(hasPendingSignIn('#auth_token=abc')).toBe(true)
    expect(hasPendingSignIn('#auth_error=sign_in_failed')).toBe(false)
    expect(hasPendingSignIn('')).toBe(false)
  })
})

describe('which errors sign someone out', () => {
  it('signs out only when the server refuses the session, not when it cannot be reached', () => {
    expect(isSessionRejected(new ApiRequestError('unauthenticated', 'expired'))).toBe(true)
    expect(isSessionRejected(new ApiRequestError('permissiondenied', 'banned'))).toBe(true)
    expect(isSessionRejected(new ApiRequestError('unavailable', 'try later'))).toBe(false)
    expect(isSessionRejected(new TypeError('Failed to fetch'))).toBe(false)
  })
})
