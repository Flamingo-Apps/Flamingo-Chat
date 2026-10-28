import { createContext, useCallback, useContext, useEffect, useReducer, useRef, useState, type ReactNode } from 'react'
import * as api from './api'
import {
  SESSION_STORAGE_KEY,
  clearSession,
  currentToken,
  pickStorage,
  readStoredSession,
  refreshStoredToken,
  restoreRetryDelay,
  saveAccount,
  saveSession,
  sessionVerdict,
  type SessionVerdict,
} from './session'
import { activeRandomChat, initialState, reducer, type AppState } from './state'
import type { Account, ClientFrame } from './types'
import { GatewayConnection } from './ws'

const storage = pickStorage()

// How the app starts, decided once. The rule: a person who has a saved session
// is never shown the welcome screen unless the server explicitly refuses that
// session.
//  - Saved session and a saved copy of the account: open straight into the
//    app, and ask the server in the background.
//  - Saved session without a copy (or just back from Google): show a splash
//    while the server is asked, with a retry state if it cannot be reached.
//  - Nothing saved: the welcome screen.
// A session handed back by Google arrives in the address bar's fragment
// (DEC-0092). It is written to storage at once and the address cleaned, so the
// token does not linger in the URL and a reload in the middle of checking it
// still finds it.
interface Boot {
  state: AppState
  // True when the app has no account to show yet and must wait for the server.
  blocking: boolean
  // True when a saved session needs checking with the server.
  check: boolean
  notice: string | null
}

let booted: Boot | null = null
function boot(): Boot {
  if (booted) return booted
  const params = new URLSearchParams(window.location.hash.slice(1))
  const hashToken = params.get('auth_token')
  const hashError = params.get('auth_error')
  if (hashToken || hashError) history.replaceState(null, '', window.location.pathname + window.location.search)
  if (hashToken) saveSession(storage, hashToken) // also drops any older saved account
  const stored = readStoredSession(storage)
  const base: Boot = { state: initialState, blocking: false, check: false, notice: hashError }
  if (!stored) return (booted = base)
  return (booted = {
    ...base,
    state: stored.account ? { ...initialState, token: stored.token, account: stored.account } : initialState,
    blocking: !stored.account,
    check: true,
  })
}

type RestorePhase = 'idle' | 'checking' | 'holding'

interface GatewayContextValue {
  state: AppState
  // True while a saved or just-returned session is being checked and the app
  // has no account to show yet.
  restoring: boolean
  // True when that check could not be completed because the server could not
  // be reached: the person keeps their session and sees a retry state.
  holding: boolean
  retryRestore: () => void
  // True from the start of a visit that has a saved session until the server
  // has said which random chat the person is in, so "Find a match" is not
  // offered to someone who is mid-chat.
  matchRestoring: boolean
  login: (account: Account, token: string) => void
  logout: () => void
  updateAccount: (account: Account) => void
  // Returns whether the frame was actually sent. A caller must not behave as if
  // it was (clear the message box) when the connection is down.
  send: (frame: ClientFrame) => boolean
  loadRooms: () => Promise<void>
  loadMessages: (roomId: string) => Promise<void>
  expireGraceWindow: (roomId: string) => void
  dismissKeepRequest: () => void
  dismissSavedMatch: () => void
  clearError: () => void
  // Tell the gateway to start listening to a room just created or joined
  // over HTTP, which it would otherwise only notice up to 25 seconds later
  // (INC-0024).
  syncRooms: () => void
}

const GatewayContext = createContext<GatewayContextValue | null>(null)

// An error shown to the person clears itself after this long, so an old one
// never sits on the screen with nothing to dismiss it.
const ERROR_VISIBLE_MS = 8000
// While searching, ask the server this often whether a chat has been made: the
// match arrives as a message that can be lost or arrive out of order, and the
// database is the truth (DEC-0112, INC-0030). While chatting, a slower check.
const SEARCH_POLL_MS = 4000
const CHAT_POLL_MS = 30_000

export function GatewayProvider({ children }: { children: ReactNode }) {
  const [initial] = useState(boot)
  const [state, dispatch] = useReducer(reducer, initial.state)
  const [phase, setPhase] = useState<RestorePhase>(initial.blocking ? 'checking' : 'idle')
  const [matchChecked, setMatchChecked] = useState(!initial.check)
  // Counts the events that say which chat a person is in. A rooms list that
  // started loading before one of them arrived is older than it, and must not
  // overwrite it.
  const matchEpochRef = useRef(0)
  // Bumped whenever the session changes (sign in, sign out, rejected). An
  // answer from the server for an older session is ignored, so a slow reply
  // can never bring back a session the person has since left.
  const genRef = useRef(0)
  const connectionRef = useRef<GatewayConnection | null>(null)
  const reconcileRef = useRef<(() => void) | null>(null)
  const retryTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const tokenRef = useRef<string | null>(state.token)
  tokenRef.current = state.token
  const accountIdRef = useRef<string | null>(state.account?.id ?? null)
  accountIdRef.current = state.account?.id ?? null
  // The latest messages, readable from the connection's callbacks, which
  // are created once per sign-in and would otherwise see a stale copy.
  const messagesRef = useRef(state.messagesByRoom)
  messagesRef.current = state.messagesByRoom
  // What the person asked for last, so a search can be asked for again after a
  // dropped connection (the server cancels a search whose owner has gone).
  const matchingRef = useRef(state.matching)
  matchingRef.current = state.matching
  const preferenceRef = useRef<'any' | 'male' | 'female'>('any')

  const stopRetry = useCallback(() => {
    if (retryTimerRef.current) clearTimeout(retryTimerRef.current)
    retryTimerRef.current = null
  }, [])

  const login = useCallback(
    (account: Account, token: string) => {
      genRef.current += 1
      stopRetry()
      saveSession(storage, token, account)
      dispatch({ type: 'session', account, token })
      setPhase('idle')
    },
    [stopRetry],
  )

  const logout = useCallback(() => {
    genRef.current += 1
    stopRetry()
    clearSession(storage)
    connectionRef.current?.close()
    dispatch({ type: 'logout' })
    setPhase('idle')
  }, [stopRetry])

  // The server refused the saved session: forget it and say why.
  const rejectSession = useCallback(
    (verdict: SessionVerdict) => {
      logout()
      dispatch({ type: 'signInFailed', reason: verdict === 'blocked' ? 'account_blocked' : 'session_expired' })
    },
    [logout],
  )

  // Ask the server whether a saved session is still good. Only a refusal ends
  // it. Not being able to ask (offline, a slow or broken server) keeps the
  // session and tries again, with a retry state if there is no account to show.
  const checkSession = useCallback(
    (token: string, hadAccount: boolean, attempt = 0) => {
      const gen = genRef.current
      stopRetry()
      api
        .getMe(token)
        .then((account) => {
          if (gen !== genRef.current) return
          if (hadAccount) dispatch({ type: 'accountUpdated', account })
          else login(account, token)
          setPhase('idle')
        })
        .catch((err) => {
          if (gen !== genRef.current) return
          const verdict = sessionVerdict(err)
          if (verdict !== 'unknown') return rejectSession(verdict)
          if (!hadAccount) setPhase('holding')
          retryTimerRef.current = setTimeout(() => {
            if (gen === genRef.current) checkSession(token, hadAccount, attempt + 1)
          }, restoreRetryDelay(attempt))
        })
    },
    [login, rejectSession, stopRetry],
  )

  const retryRestore = useCallback(() => {
    const token = tokenRef.current ?? currentToken(storage)
    if (!token) return
    setPhase('checking')
    checkSession(token, Boolean(accountIdRef.current))
  }, [checkSession])

  const updateAccount = useCallback((account: Account) => {
    dispatch({ type: 'accountUpdated', account })
  }, [])

  const dismissKeepRequest = useCallback(() => dispatch({ type: 'keepRequestDismissed' }), [])
  const dismissSavedMatch = useCallback(() => dispatch({ type: 'savedMatchDismissed' }), [])
  const clearError = useCallback(() => dispatch({ type: 'clearError' }), [])

  const expireGraceWindow = useCallback((roomId: string) => {
    dispatch({ type: 'graceWindowExpired', roomId })
  }, [])

  // On first load: say why a Google sign-in failed, and check the saved session.
  useEffect(() => {
    if (initial.notice) dispatch({ type: 'signInFailed', reason: initial.notice })
    const saved = readStoredSession(storage)
    if (initial.check && saved) checkSession(saved.token, Boolean(initial.state.account))
    else setPhase('idle')
    return stopRetry
    // Runs once on mount: everything it uses is stable or read at that moment.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // Keep the saved copy of the account current, so the next visit can open
  // straight into the app (see boot), but only for the session this tab holds.
  useEffect(() => {
    if (state.account && state.token) saveAccount(storage, state.token, state.account)
  }, [state.account, state.token])

  // A removed account must not be shown again on the next visit.
  useEffect(() => {
    if (state.kicked) clearSession(storage)
  }, [state.kicked])

  // An error clears itself after a while.
  useEffect(() => {
    if (!state.lastError) return
    const id = setTimeout(() => dispatch({ type: 'clearError' }), ERROR_VISIBLE_MS)
    return () => clearTimeout(id)
  }, [state.lastError])

  // Another tab of this browser changed the session (signed out, or signed in as
  // someone else): follow it, instead of carrying on as an account the browser
  // no longer holds.
  useEffect(() => {
    const onStorage = (e: StorageEvent) => {
      if (e.key !== SESSION_STORAGE_KEY && e.key !== null) return
      const now = currentToken(storage)
      const mine = tokenRef.current
      if (!mine || now === mine) return
      if (!now) {
        // Signed out elsewhere. Nothing to clear, the other tab did.
        genRef.current += 1
        stopRetry()
        connectionRef.current?.close()
        dispatch({ type: 'logout' })
        return
      }
      // A different token. The same account with a renewed token needs nothing;
      // a different account means this tab is showing the wrong person.
      api
        .getMe(now)
        .then((account) => {
          if (account.id !== accountIdRef.current) window.location.reload()
        })
        .catch(() => {})
    }
    window.addEventListener('storage', onStorage)
    return () => window.removeEventListener('storage', onStorage)
  }, [stopRetry])

  useEffect(() => {
    if (!state.token) {
      connectionRef.current?.close()
      connectionRef.current = null
      return
    }
    const token = state.token
    let everOpen = false
    // Ask the server which random chat this person is in, and show that: it
    // restores the chat after a refresh or a reconnect, clears one that ended
    // while they were away, and finds a match whose announcement never reached
    // this screen. The reply is dropped if a live event about the chat arrived
    // while it was loading, since that is newer.
    const reconcile = () => {
      const epoch = matchEpochRef.current
      api
        .listRooms(token)
        .then((resp) => {
          if (epoch !== matchEpochRef.current) return
          dispatch({ type: 'matchReconciled', rooms: resp.rooms })
          const active = activeRandomChat(resp.rooms)
          if (active) {
            api.listMessages(token, active.id).then((m) => dispatch({ type: 'messagesLoaded', roomId: active.id, messages: m.messages }), () => {})
          }
        })
        .catch(() => {})
        .finally(() => setMatchChecked(true))
    }
    reconcileRef.current = reconcile
    // Never leave "getting your chat" up for longer than this if the server is slow.
    const giveUp = setTimeout(() => setMatchChecked(true), 6000)
    const conn = new GatewayConnection(token, {
      onFrame: (frame) => {
        if (frame.type === 'auth_ok') {
          dispatch({ type: 'accountUpdated', account: frame.account })
          if (frame.token) refreshStoredToken(storage, token, frame.token)
        }
        if (frame.type === 'match_found' || frame.type === 'match_ended' || frame.type === 'match_queued' || frame.type === 'match_cancelled') {
          matchEpochRef.current += 1
        }
        dispatch({ type: 'frame', frame })
        // The server refused Next or End because it knows of no open chat: our
        // screen is out of date, so ask it what is true.
        if (frame.type === 'error' && frame.code === 'failedprecondition') reconcile()
        // A chat being saved or closed, or someone leaving a room, changes
        // what the Inbox and the room lists should show.
        if (frame.type === 'keep_status' || frame.type === 'match_ended' || frame.type === 'room_left' || frame.type === 'room_joined') {
          api.listRooms(token).then((resp) => dispatch({ type: 'roomsLoaded', rooms: resp.rooms }), () => {})
        }
      },
      onStatusChange: (status) => {
        dispatch({ type: 'connectionStatus', status })
        if (status !== 'open') return
        reconcile()
        // Back after a drop while searching: the server may have cancelled the
        // search in the meantime (it does when a person has been gone a while),
        // and asking again is harmless if it has not. Without this the screen
        // would say "Looking for someone" for a search that no longer exists.
        if (everOpen && matchingRef.current) conn.send({ type: 'match_request', preference: preferenceRef.current })
        // Back after a drop: messages sent while the connection was down were
        // not delivered live, so reload what each open room should hold
        // (docs/ARCHITECTURE.md section 5.2). Duplicates are removed by message id.
        if (everOpen) {
          for (const roomId of Object.keys(messagesRef.current)) {
            api.listMessages(token, roomId).then((resp) => dispatch({ type: 'messagesLoaded', roomId, messages: resp.messages }), () => {})
          }
        }
        everOpen = true
      },
      // Several logins in a row failed. The gateway closes the same way for a
      // dead session as for a hiccup, so ask it; only a refusal signs out.
      onAuthTrouble: () => {
        if (retryTimerRef.current) return
        checkSession(token, true)
      },
    })
    connectionRef.current = conn
    conn.connect()
    // The network came back, or the tab was brought forward: do not wait out
    // the reconnect delay, and check which chat this person is in.
    const wake = () => {
      conn.nudge()
      if (!document.hidden) reconcile()
    }
    window.addEventListener('online', wake)
    document.addEventListener('visibilitychange', wake)
    return () => {
      clearTimeout(giveUp)
      window.removeEventListener('online', wake)
      document.removeEventListener('visibilitychange', wake)
      reconcileRef.current = null
      conn.close()
    }
  }, [state.token, checkSession])

  // The safety net: while searching or chatting, keep asking the server which
  // chat this person is in, so a lost or out-of-order announcement heals itself.
  const inChat = Boolean(state.currentMatch)
  useEffect(() => {
    if (!state.token || !(state.matching || inChat)) return
    const id = setInterval(
      () => {
        if (!document.hidden) reconcileRef.current?.()
      },
      state.matching ? SEARCH_POLL_MS : CHAT_POLL_MS,
    )
    return () => clearInterval(id)
  }, [state.token, state.matching, inChat])

  const send = useCallback((frame: ClientFrame): boolean => {
    // Remember when a match was asked for: a "queued" that arrives after the
    // match itself must not undo it (see the reducer's match_queued).
    if (frame.type === 'match_request') preferenceRef.current = frame.preference
    if (frame.type === 'match_request' || frame.type === 'match_next') dispatch({ type: 'searchRequested' })
    return connectionRef.current?.send(frame) ?? false
  }, [])
  const syncRooms = useCallback(() => void connectionRef.current?.send({ type: 'sync_rooms' }), [])

  const loadRooms = useCallback(async () => {
    if (!state.token) return
    const resp = await api.listRooms(state.token)
    dispatch({ type: 'roomsLoaded', rooms: resp.rooms })
  }, [state.token])

  const loadMessages = useCallback(
    async (roomId: string) => {
      if (!state.token) return
      const resp = await api.listMessages(state.token, roomId)
      dispatch({ type: 'messagesLoaded', roomId, messages: resp.messages })
    },
    [state.token],
  )

  const restoring = !state.account && phase !== 'idle'
  const matchRestoring = Boolean(state.token) && !matchChecked

  return (
    <GatewayContext.Provider
      value={{
        state,
        restoring,
        holding: phase === 'holding',
        retryRestore,
        matchRestoring,
        login,
        logout,
        updateAccount,
        send,
        loadRooms,
        loadMessages,
        expireGraceWindow,
        dismissKeepRequest,
        dismissSavedMatch,
        clearError,
        syncRooms,
      }}
    >
      {children}
    </GatewayContext.Provider>
  )
}

export function useGateway(): GatewayContextValue {
  const ctx = useContext(GatewayContext)
  if (!ctx) throw new Error('useGateway must be used inside a GatewayProvider')
  return ctx
}
