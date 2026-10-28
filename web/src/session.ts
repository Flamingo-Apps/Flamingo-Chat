import { ApiRequestError } from './api'
import type { Account } from './types'

// What the browser remembers between visits: the session token, and the last
// account the server told us about. The account copy lets the app open
// straight into the signed-in view instead of showing the welcome screen until
// the server has answered; the server is still asked, straight away, and its
// answer replaces the copy. Neither is shown to anyone else, and the token was
// already kept here.
export const SESSION_STORAGE_KEY = 'flamingo_session_token'
export const ACCOUNT_STORAGE_KEY = 'flamingo_account'

type ReadStorage = Pick<Storage, 'getItem'>
type WriteStorage = Pick<Storage, 'setItem' | 'removeItem'>

function looksLikeAccount(v: unknown): v is Account {
  if (typeof v !== 'object' || v === null) return false
  const a = v as Record<string, unknown>
  return typeof a.id === 'string' && typeof a.pseudonym === 'string' && typeof a.google_linked === 'boolean'
}

// readStoredSession returns the saved token and, if it is intact, the saved
// account. Storage can be blocked or hold garbage, so nothing here may throw.
export function readStoredSession(storage: ReadStorage): { token: string; account: Account | null } | null {
  try {
    const token = storage.getItem(SESSION_STORAGE_KEY)
    if (!token) return null
    // A damaged copy of the account costs only the copy, never the session.
    let account: Account | null = null
    try {
      const raw = storage.getItem(ACCOUNT_STORAGE_KEY)
      const parsed: unknown = raw ? JSON.parse(raw) : null
      if (looksLikeAccount(parsed)) account = parsed
    } catch {
      account = null
    }
    return { token, account }
  } catch {
    return null
  }
}

export function saveSession(storage: WriteStorage, token: string, account?: Account): void {
  try {
    storage.setItem(SESSION_STORAGE_KEY, token)
    if (account) storage.setItem(ACCOUNT_STORAGE_KEY, JSON.stringify(account))
  } catch {
    /* storage blocked: the person just signs in again next visit */
  }
}

export function saveAccount(storage: WriteStorage, account: Account): void {
  try {
    storage.setItem(ACCOUNT_STORAGE_KEY, JSON.stringify(account))
  } catch {
    /* as above */
  }
}

export function clearSession(storage: WriteStorage): void {
  try {
    storage.removeItem(SESSION_STORAGE_KEY)
    storage.removeItem(ACCOUNT_STORAGE_KEY)
  } catch {
    /* as above */
  }
}

// A Google sign-in that has just come back leaves its session in the address
// bar's fragment (DEC-0092); the app should wait for it, not flash the welcome.
export function hasPendingSignIn(hash: string): boolean {
  return new URLSearchParams(hash.replace(/^#/, '')).has('auth_token')
}

// isSessionRejected says whether the server refused the session itself (it
// expired, or the account was banned), as opposed to being unreachable. Only
// the first should sign someone out: a flaky connection must not.
export function isSessionRejected(err: unknown): boolean {
  return err instanceof ApiRequestError && (err.code === 'unauthenticated' || err.code === 'permissiondenied')
}
