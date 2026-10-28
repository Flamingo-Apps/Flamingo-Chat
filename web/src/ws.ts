import type { ClientFrame, ServerFrame } from './types'

// VITE_GATEWAY_WS_URL defaults to the gateway's dev WebSocket endpoint.
const WS_URL = import.meta.env.VITE_GATEWAY_WS_URL ?? 'ws://localhost:8080/v1/ws'

// docs/ARCHITECTURE.md §5.2: "Clients reconnect with exponential backoff
// and jitter, send ping every 25 seconds."
const PING_INTERVAL_MS = 25_000
const BASE_RECONNECT_DELAY_MS = 1_000
const MAX_RECONNECT_DELAY_MS = 30_000
// A connection counts as healthy, and the backoff starts over, only after it
// has stayed up this long. Resetting at login let one that kept dropping
// straight after logging in retry about once a second, indefinitely.
const STABLE_AFTER_MS = 10_000

export type ConnectionStatus = 'connecting' | 'open' | 'closed'

export interface GatewayConnectionHandlers {
  onFrame: (frame: ServerFrame) => void
  onStatusChange: (status: ConnectionStatus) => void
  // Called after several attempts in a row ended without ever logging in. The
  // gateway closes the same way for a dead session as for a hiccup, so the
  // caller should ask the server whether the session is still good.
  onAuthTrouble?: () => void
}

// This many connection attempts in a row that never reach auth_ok count as
// trouble worth asking the server about.
const AUTH_TROUBLE_AFTER = 3

// GatewayConnection owns the WebSocket's lifecycle (connect, auth,
// reconnect, keepalive) - everything else (room/message state,
// duplicate removal) is the store's job, not this transport layer's, so
// this stays testable without a real socket (reconnectDelay below is
// exported for exactly that).
export class GatewayConnection {
  private token: string
  private handlers: GatewayConnectionHandlers
  private ws: WebSocket | null = null
  private pingTimer: ReturnType<typeof setInterval> | null = null
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null
  private stableTimer: ReturnType<typeof setTimeout> | null = null
  private reconnectAttempt = 0
  private failedWithoutAuth = 0
  private closedByCaller = false
  private wsFactory: (url: string) => WebSocket

  constructor(token: string, handlers: GatewayConnectionHandlers, wsFactory: (url: string) => WebSocket = (url) => new WebSocket(url)) {
    this.token = token
    this.handlers = handlers
    this.wsFactory = wsFactory
  }

  connect(): void {
    this.closedByCaller = false
    this.open()
  }

  close(): void {
    this.closedByCaller = true
    this.stopPing()
    this.clearStableTimer()
    if (this.reconnectTimer) clearTimeout(this.reconnectTimer)
    this.reconnectTimer = null
    this.detach()
  }

  // nudge reconnects at once instead of waiting out the backoff, for when the
  // network has just come back or the tab has been brought forward. It does
  // nothing if a socket already exists or the connection was closed on purpose.
  nudge(): void {
    if (this.closedByCaller || this.ws) return
    if (this.reconnectTimer) clearTimeout(this.reconnectTimer)
    this.reconnectTimer = null
    this.open()
  }

  // send returns whether the frame was actually handed to an open socket. A
  // caller must not behave as if it was sent (clear the message box, say
  // "searching") when it was not.
  send(frame: ClientFrame): boolean {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(frame))
      return true
    }
    return false
  }

  private open(): void {
    this.detach()
    this.handlers.onStatusChange('connecting')
    const ws = this.wsFactory(WS_URL)
    this.ws = ws

    ws.onopen = () => {
      ws.send(JSON.stringify({ type: 'auth', token: this.token } satisfies ClientFrame))
    }
    ws.onmessage = (event) => {
      const frame = JSON.parse(event.data) as ServerFrame
      if (frame.type === 'auth_ok') {
        this.failedWithoutAuth = 0
        this.clearStableTimer()
        this.stableTimer = setTimeout(() => (this.reconnectAttempt = 0), STABLE_AFTER_MS)
        this.startPing()
        this.handlers.onStatusChange('open')
      }
      this.handlers.onFrame(frame)
    }
    // A failed socket fires both error and close. Only the first, and only for
    // the socket still in use, may start a reconnect: acting on both opened two
    // sockets and left the first one alive with nothing watching it.
    ws.onclose = () => this.socketEnded(ws)
    ws.onerror = () => this.socketEnded(ws)
  }

  // detach drops the current socket without reacting to its closing.
  private detach(): void {
    const old = this.ws
    this.ws = null
    if (!old) return
    old.onopen = null
    old.onmessage = null
    old.onclose = null
    old.onerror = null
    try {
      old.close()
    } catch {
      /* already closed */
    }
  }

  private socketEnded(ws: WebSocket): void {
    if (ws !== this.ws) return
    this.ws = null
    // A socket that closed before ever logging in. Counted so a session that
    // has died (expired, account deleted) is noticed instead of retried for
    // ever in silence.
    if (this.stableTimer === null && !this.closedByCaller) {
      this.failedWithoutAuth += 1
      if (this.failedWithoutAuth % AUTH_TROUBLE_AFTER === 0) this.handlers.onAuthTrouble?.()
    }
    this.handleDisconnect()
  }

  private clearStableTimer(): void {
    if (this.stableTimer) clearTimeout(this.stableTimer)
    this.stableTimer = null
  }

  private handleDisconnect(): void {
    this.stopPing()
    this.clearStableTimer()
    this.handlers.onStatusChange('closed')
    if (this.closedByCaller) return

    const delay = reconnectDelay(this.reconnectAttempt)
    this.reconnectAttempt += 1
    if (this.reconnectTimer) clearTimeout(this.reconnectTimer)
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null
      this.open()
    }, delay)
  }

  private startPing(): void {
    this.stopPing()
    this.pingTimer = setInterval(() => this.send({ type: 'ping' }), PING_INTERVAL_MS)
  }

  private stopPing(): void {
    if (this.pingTimer) clearInterval(this.pingTimer)
    this.pingTimer = null
  }
}

// reconnectDelay is exponential backoff (capped) plus up to 30% jitter,
// so many clients disconnected by the same event (a gateway instance
// restarting) don't all retry in lockstep.
export function reconnectDelay(attempt: number): number {
  const exponential = Math.min(BASE_RECONNECT_DELAY_MS * 2 ** attempt, MAX_RECONNECT_DELAY_MS)
  const jitter = exponential * 0.3 * Math.random()
  return exponential + jitter
}
