import { createContext, useCallback, useContext, useEffect, useReducer, useRef, type ReactNode } from 'react'
import * as api from './api'
import { initialState, reducer, type AppState } from './state'
import type { Account, ClientFrame } from './types'
import { GatewayConnection } from './ws'

const SESSION_STORAGE_KEY = 'flamingo_session_token'

interface GatewayContextValue {
  state: AppState
  login: (account: Account, token: string) => void
  logout: () => void
  send: (frame: ClientFrame) => void
  loadRooms: () => Promise<void>
  loadMessages: (roomId: string) => Promise<void>
  expireGraceWindow: (roomId: string) => void
}

const GatewayContext = createContext<GatewayContextValue | null>(null)

export function GatewayProvider({ children }: { children: ReactNode }) {
  const [state, dispatch] = useReducer(reducer, initialState)
  const connectionRef = useRef<GatewayConnection | null>(null)

  const login = useCallback((account: Account, token: string) => {
    localStorage.setItem(SESSION_STORAGE_KEY, token)
    dispatch({ type: 'session', account, token })
  }, [])

  const logout = useCallback(() => {
    localStorage.removeItem(SESSION_STORAGE_KEY)
    connectionRef.current?.close()
    dispatch({ type: 'logout' })
  }, [])

  const expireGraceWindow = useCallback((roomId: string) => {
    dispatch({ type: 'graceWindowExpired', roomId })
  }, [])

  // On first load, a saved token is validated against GET /v1/me rather
  // than trusted outright - it may have expired, or the account may have
  // been banned since the last visit.
  useEffect(() => {
    const saved = localStorage.getItem(SESSION_STORAGE_KEY)
    if (!saved) return
    api
      .getMe(saved)
      .then((account) => login(account, saved))
      .catch(() => localStorage.removeItem(SESSION_STORAGE_KEY))
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
    const conn = new GatewayConnection(state.token, {
      onFrame: (frame) => {
        if (frame.type === 'auth_ok') {
          dispatch({ type: 'accountUpdated', account: frame.account })
          if (frame.token) localStorage.setItem(SESSION_STORAGE_KEY, frame.token)
        }
        dispatch({ type: 'frame', frame })
      },
      onStatusChange: (status) => dispatch({ type: 'connectionStatus', status }),
    })
    connectionRef.current = conn
    conn.connect()
    return () => conn.close()
  }, [state.token])

  const send = useCallback((frame: ClientFrame) => connectionRef.current?.send(frame), [])

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
    <GatewayContext.Provider value={{ state, login, logout, send, loadRooms, loadMessages, expireGraceWindow }}>
      {children}
    </GatewayContext.Provider>
  )
}

export function useGateway(): GatewayContextValue {
  const ctx = useContext(GatewayContext)
  if (!ctx) throw new Error('useGateway must be used inside a GatewayProvider')
  return ctx
}
