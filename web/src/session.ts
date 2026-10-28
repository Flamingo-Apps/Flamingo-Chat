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
type KeyStorage = ReadStorage & WriteStorage

// A storage that lives only as long as the page. Used when the browser blocks
// localStorage altogether (some private or locked-down modes throw on merely
// touching it): the person stays signed in until they close the tab, instead
// of the whole app failing to start.
function memoryStorage(): KeyStorage {
  const data = new Map<string, string>()
  return {
    getItem: (k) => data.get(k) ?? null,
    setItem: (k, v) => void data.set(k, String(v)),
    removeItem: (k) => void data.delete(k),
  }
}

// pickStorage returns localStorage if it really works, else a page-lifetime
// fallback. Touching the global can itself throw, so it is inside the try.
export function pickStorage(): KeyStorage {
  try {
    const probe = '__flamingo_probe'
    window.localStorage.setItem(probe, '1')
    window.localStorage.removeItem(probe)
    return window.localStorage
  } catch {
    return memoryStorage()
  }
}

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
    // A token saved without its account must not keep an older person's copy:
    // the two would disagree on the next start.
    if (account) storage.setItem(ACCOUNT_STORAGE_KEY, JSON.stringify(account))
    else storage.removeItem(ACCOUNT_STORAGE_KEY)
  } catch {
    /* storage blocked: the person just signs in again next visit */
  }
}

// currentToken is what storage holds right now, which another tab may have
// changed since this one last looked.
export function currentToken(storage: ReadStorage): string | null {
  try {
    return storage.getItem(SESSION_STORAGE_KEY)
  } catch {
    return null
  }
}

// saveAccount keeps the saved copy of the account, but only for the session
// this tab still holds. Without the check, a tab that was signed out (or
// replaced by a different sign-in in another tab) would write its stale
// account back and leave storage holding a token and an account that disagree.
export function saveAccount(storage: KeyStorage, token: string, account: Account): void {
  if (currentToken(storage) !== token) return
  try {
    storage.setItem(ACCOUNT_STORAGE_KEY, JSON.stringify(account))
  } catch {
    /* as above */
  }
}

// refreshStoredToken records a renewed token the server handed out, but only
// if storage still holds the one it replaces. Otherwise a tab that was signed
// out elsewhere would bring the old session back to life.
export function refreshStoredToken(storage: KeyStorage, previous: string, renewed: string): boolean {
  if (currentToken(storage) !== previous) return false
  try {
    storage.setItem(SESSION_STORAGE_KEY, renewed)
    return true
  } catch {
    return false
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

// sessionVerdict says what GET /v1/me's failure means for the saved session.
// The server refusing the session itself is the only thing that signs a person
// out: 401 (the token is bad or expired), 403 (the account may not use the
// app) and 404 (the account no longer exists, say a guest cleaned up after 60
// idle days). The decision is on the HTTP status, not the spelling of an error
// code (the gateway writes "permission_denied" over HTTP and "permissiondenied"
// over the WebSocket, and a test once passed on the wrong one). Everything else,
// a timeout, no network, a 5xx, an HTML error page, is "could not check": the
// person keeps their session and the app retries.
export type SessionVerdict = 'expired' | 'blocked' | 'unknown'

export function sessionVerdict(err: unknown): SessionVerdict {
  if (!(err instanceof ApiRequestError)) return 'unknown'
  if (err.status === 401 || err.status === 404) return 'expired'
  if (err.status === 403) return 'blocked'
  return 'unknown'
}

export function isSessionRejected(err: unknown): boolean {
  return sessionVerdict(err) !== 'unknown'
}

// How long to wait before asking the server again about a session it has not
// yet answered for: quick at first, then settling at fifteen seconds.
export function restoreRetryDelay(attempt: number): number {
  return Math.min(1500 * 2 ** attempt, 15_000)
}
