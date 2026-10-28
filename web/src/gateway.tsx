import { createContext, useCallback, useContext, useEffect, useReducer, useRef, useState, type ReactNode } from 'react'
import * as api from './api'
import { clearSession, hasPendingSignIn, isSessionRejected, readStoredSession, saveAccount, saveSession } from './session'
import { activeRandomChat, initialState, reducer, type AppState } from './state'
import type { Account, ClientFrame } from './types'
import { GatewayConnection } from './ws'

// What the first render starts from. A returning person with a saved session
// and a saved copy of their account opens straight into the app, with no
// welcome screen shown first; the server is asked in the background. Someone
// whose session still has to be checked (no saved copy yet, or just back from
// Google) sees a short splash instead of the welcome screen.
function boot(): { state: AppState; restoring: boolean } {
  const stored = readStoredSession(localStorage)
  const pendingSignIn = hasPendingSignIn(window.location.hash)
  if (stored?.account && !pendingSignIn) return { state: { ...initialState, token: stored.token, account: stored.account }, restoring: false }
  return { state: initialState, restoring: Boolean(stored) || pendingSignIn }
}

interface GatewayContextValue {
  state: AppState
  // True while a saved or just-returned session is being checked and the app
  // has no account to show yet.
  restoring: boolean
  login: (account: Account, token: string) => void
  logout: () => void
  updateAccount: (account: Account) => void
  send: (frame: ClientFrame) => void
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

export function GatewayProvider({ children }: { children: ReactNode }) {
  const [initial] = useState(boot)
  const [state, dispatch] = useReducer(reducer, initial.state)
  const [restoring, setRestoring] = useState(initial.restoring)
  // Counts the events that say which chat a person is in. A rooms list that
  // started loading before one of them arrived is older than it, and must not
  // overwrite it.
  const matchEpochRef = useRef(0)
  const connectionRef = useRef<GatewayConnection | null>(null)
  // The latest messages, readable from the connection's callbacks, which
  // are created once per sign-in and would otherwise see a stale copy.
  const messagesRef = useRef(state.messagesByRoom)
  messagesRef.current = state.messagesByRoom

  const login = useCallback((account: Account, token: string) => {
    saveSession(localStorage, token, account)
    dispatch({ type: 'session', account, token })
  }, [])

  const logout = useCallback(() => {
    clearSession(localStorage)
    connectionRef.current?.close()
    dispatch({ type: 'logout' })
  }, [])

  const updateAccount = useCallback((account: Account) => {
    dispatch({ type: 'accountUpdated', account })
  }, [])

  const dismissKeepRequest = useCallback(() => dispatch({ type: 'keepRequestDismissed' }), [])
  const dismissSavedMatch = useCallback(() => dispatch({ type: 'savedMatchDismissed' }), [])
  const clearError = useCallback(() => dispatch({ type: 'clearError' }), [])

  const expireGraceWindow = useCallback((roomId: string) => {
    dispatch({ type: 'graceWindowExpired', roomId })
  }, [])

  // Keep the saved copy of the account current, so the next visit can open
  // straight into the app (see boot).
  useEffect(() => {
    if (state.account) saveAccount(localStorage, state.account)
  }, [state.account])

  // A removed account must not be shown again on the next visit.
  useEffect(() => {
    if (state.kicked) clearSession(localStorage)
  }, [state.kicked])

  // On first load, either pick up a session the Google redirect flow
  // (DEC-0092) just handed back in the URL fragment, or check the saved one.
  // Either way it is validated against GET /v1/me rather than trusted
  // outright: it may have expired, or the account may have been banned since
  // the last visit. Only a refusal from the server signs the person out; being
  // unable to reach it (a flaky connection) must not. The fragment is used
  // (not a query param) because it is never sent to any server and never
  // appears in access logs - see oauth_redirect.go's own comment.
  useEffect(() => {
    const hashParams = new URLSearchParams(window.location.hash.slice(1))
    const hashToken = hashParams.get('auth_token')
    const hashError = hashParams.get('auth_error')
    if (hashToken) {
      history.replaceState(null, '', window.location.pathname + window.location.search)
      api
        .getMe(hashToken)
        .then((account) => login(account, hashToken))
        .catch(() => dispatch({ type: 'signInFailed', reason: 'sign_in_failed' }))
        .finally(() => setRestoring(false))
      return
    }
    // The redirect came back without a session: say so, rather than landing
    // the person back where they were as if nothing had been tried. Then carry
    // on below, since a saved guest session may still be valid.
    if (hashError) {
      history.replaceState(null, '', window.location.pathname + window.location.search)
      dispatch({ type: 'signInFailed', reason: hashError })
    }
    const saved = readStoredSession(localStorage)
    if (!saved) {
      setRestoring(false)
      return
    }
    api
      .getMe(saved.token)
      .then((account) => (saved.account ? dispatch({ type: 'accountUpdated', account }) : login(account, saved.token)))
      .catch((err) => {
        if (!isSessionRejected(err)) return
        clearSession(localStorage)
        dispatch({ type: 'logout' })
        dispatch({ type: 'signInFailed', reason: 'session_expired' })
      })
      .finally(() => setRestoring(false))
    // login is stable (useCallback with no deps that change), so this
    // genuinely only needs to run once on mount.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    if (!state.token) {
      connectionRef.current?.close()
      connectionRef.current = null
      return
    }
    const token = state.token
    let everOpen = false
    // Ask the server which random chat this person is in, and show that: it
    // restores the chat after a refresh or a reconnect, and clears one that
    // ended while they were away. The reply is dropped if a live event about
    // the chat arrived while it was loading, since that is newer.
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
    }
    const conn = new GatewayConnection(token, {
      onFrame: (frame) => {
        if (frame.type === 'auth_ok') {
          dispatch({ type: 'accountUpdated', account: frame.account })
          if (frame.token) saveSession(localStorage, frame.token)
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
        // Back after a drop: messages sent while the connection was down were
        // not delivered live, so reload what each open room should hold
        // (docs/ARCHITECTURE.md §5.2). Duplicates are removed by message id.
        if (everOpen) {
          for (const roomId of Object.keys(messagesRef.current)) {
            api.listMessages(token, roomId).then((resp) => dispatch({ type: 'messagesLoaded', roomId, messages: resp.messages }), () => {})
          }
        }
        everOpen = true
      },
    })
    connectionRef.current = conn
    conn.connect()
    return () => conn.close()
  }, [state.token])

  const send = useCallback((frame: ClientFrame) => connectionRef.current?.send(frame), [])
  const syncRooms = useCallback(() => connectionRef.current?.send({ type: 'sync_rooms' }), [])

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

  return (
    <GatewayContext.Provider value={{ state, restoring, login, logout, updateAccount, send, loadRooms, loadMessages, expireGraceWindow, dismissKeepRequest, dismissSavedMatch, clearError, syncRooms }}>
      {children}
    </GatewayContext.Provider>
  )
}

export function useGateway(): GatewayContextValue {
  const ctx = useContext(GatewayContext)
  if (!ctx) throw new Error('useGateway must be used inside a GatewayProvider')
  return ctx
}
