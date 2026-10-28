import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { GatewayConnection, reconnectDelay } from './ws'

describe('reconnectDelay', () => {
  it('grows with the attempt number, capped, with up to 30% jitter', () => {
    for (let attempt = 0; attempt < 8; attempt++) {
      const base = Math.min(1000 * 2 ** attempt, 30_000)
      const delay = reconnectDelay(attempt)
      expect(delay).toBeGreaterThanOrEqual(base)
      expect(delay).toBeLessThanOrEqual(base * 1.3)
    }
  })

  it('never exceeds the cap even for a very high attempt count', () => {
    const delay = reconnectDelay(20)
    expect(delay).toBeLessThanOrEqual(30_000 * 1.3)
  })
})

// FakeWebSocket mimics just the surface GatewayConnection actually uses,
// so these tests don't need a real socket or server.
class FakeWebSocket {
  static OPEN = 1
  static instances: FakeWebSocket[] = []

  readyState = 0
  sent: string[] = []
  onopen: (() => void) | null = null
  onmessage: ((event: { data: string }) => void) | null = null
  onclose: (() => void) | null = null
  onerror: (() => void) | null = null

  url: string

  constructor(url: string) {
    this.url = url
    FakeWebSocket.instances.push(this)
  }

  send(data: string) {
    this.sent.push(data)
  }

  close() {
    this.readyState = 3
    this.onclose?.()
  }

  // Test helpers, not part of the real WebSocket surface.
  simulateOpen() {
    this.readyState = FakeWebSocket.OPEN
    this.onopen?.()
  }
  simulateMessage(data: unknown) {
    this.onmessage?.({ data: JSON.stringify(data) })
  }
  simulateServerClose() {
    this.readyState = 3
    this.onclose?.()
  }
}

describe('GatewayConnection', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    FakeWebSocket.instances = []
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  function factory(url: string) {
    return new FakeWebSocket(url) as unknown as WebSocket
  }

  it('sends an auth frame once the socket opens', () => {
    const conn = new GatewayConnection('tok-123', { onFrame: vi.fn(), onStatusChange: vi.fn() }, factory)
    conn.connect()
    const sock = FakeWebSocket.instances[0]
    sock.simulateOpen()
    expect(sock.sent).toEqual([JSON.stringify({ type: 'auth', token: 'tok-123' })])
  })

  it('reports status open and forwards frames once auth_ok arrives', () => {
    const onFrame = vi.fn()
    const onStatusChange = vi.fn()
    const conn = new GatewayConnection('tok', { onFrame, onStatusChange }, factory)
    conn.connect()
    const sock = FakeWebSocket.instances[0]
    sock.simulateOpen()

    sock.simulateMessage({ type: 'auth_ok', account: { id: 'a' } })
    expect(onStatusChange).toHaveBeenCalledWith('open')
    expect(onFrame).toHaveBeenCalledWith({ type: 'auth_ok', account: { id: 'a' } })

    sock.simulateMessage({ type: 'pong' })
    expect(onFrame).toHaveBeenCalledWith({ type: 'pong' })
  })

  it('sends a ping every 25 seconds once open', () => {
    const conn = new GatewayConnection('tok', { onFrame: vi.fn(), onStatusChange: vi.fn() }, factory)
    conn.connect()
    const sock = FakeWebSocket.instances[0]
    sock.simulateOpen()
    sock.simulateMessage({ type: 'auth_ok', account: { id: 'a' } })

    vi.advanceTimersByTime(25_000)
    expect(sock.sent).toContain(JSON.stringify({ type: 'ping' }))
  })

  it('reconnects after an unexpected close, but not after an explicit close', () => {
    const onStatusChange = vi.fn()
    const conn = new GatewayConnection('tok', { onFrame: vi.fn(), onStatusChange }, factory)
    conn.connect()
    FakeWebSocket.instances[0].simulateServerClose()
    expect(onStatusChange).toHaveBeenCalledWith('closed')

    vi.advanceTimersByTime(5_000)
    expect(FakeWebSocket.instances.length).toBe(2) // reconnected

    conn.close()
    const countBeforeWaiting = FakeWebSocket.instances.length
    vi.advanceTimersByTime(60_000)
    expect(FakeWebSocket.instances.length).toBe(countBeforeWaiting) // no further reconnect
  })

  it('does not send frames while the socket is not open', () => {
    const conn = new GatewayConnection('tok', { onFrame: vi.fn(), onStatusChange: vi.fn() }, factory)
    conn.connect()
    const sock = FakeWebSocket.instances[0]
    conn.send({ type: 'ping' })
    expect(sock.sent).toEqual([])
  })

  it('keeps backing off when a connection drops straight after logging in (no retry every second)', () => {
    const conn = new GatewayConnection('tok', { onFrame: vi.fn(), onStatusChange: vi.fn() }, factory)
    conn.connect()
    const loginAndDrop = () => {
      const sock = FakeWebSocket.instances[FakeWebSocket.instances.length - 1]
      sock.simulateOpen()
      sock.simulateMessage({ type: 'auth_ok', account: { id: 'a' } })
      sock.simulateServerClose()
    }
    loginAndDrop() // first drop: next try within about a second
    vi.advanceTimersByTime(1_400)
    expect(FakeWebSocket.instances.length).toBe(2)

    loginAndDrop() // dropped again straight after login: the wait must have grown
    vi.advanceTimersByTime(1_400)
    expect(FakeWebSocket.instances.length).toBe(2)
    vi.advanceTimersByTime(1_500)
    expect(FakeWebSocket.instances.length).toBe(3)
  })

  it('starts the backoff over once a connection has stayed up for a while', () => {
    const conn = new GatewayConnection('tok', { onFrame: vi.fn(), onStatusChange: vi.fn() }, factory)
    conn.connect()
    // Fail twice quickly so the attempt count is above zero.
    for (let i = 0; i < 2; i++) {
      FakeWebSocket.instances[FakeWebSocket.instances.length - 1].simulateServerClose()
      vi.advanceTimersByTime(5_000)
    }
    const sock = FakeWebSocket.instances[FakeWebSocket.instances.length - 1]
    sock.simulateOpen()
    sock.simulateMessage({ type: 'auth_ok', account: { id: 'a' } })
    vi.advanceTimersByTime(11_000) // healthy for longer than the stable period
    const before = FakeWebSocket.instances.length
    sock.simulateServerClose()
    vi.advanceTimersByTime(1_400) // back to the first, short wait
    expect(FakeWebSocket.instances.length).toBe(before + 1)
  })

  it('reconnects once, with one socket, when a failed socket fires both error and close', () => {
    const conn = new GatewayConnection('tok', { onFrame: vi.fn(), onStatusChange: vi.fn() }, factory)
    conn.connect()
    const sock = FakeWebSocket.instances[0]
    sock.onerror?.()
    sock.onclose?.()
    vi.advanceTimersByTime(60_000)
    expect(FakeWebSocket.instances.length).toBe(2)
  })

  it('closes the old socket instead of leaving it alive when it opens a new one', () => {
    const conn = new GatewayConnection('tok', { onFrame: vi.fn(), onStatusChange: vi.fn() }, factory)
    conn.connect()
    const first = FakeWebSocket.instances[0]
    first.simulateOpen()
    conn.connect() // e.g. the same connection asked to start again
    expect(first.readyState).toBe(3)
    expect(FakeWebSocket.instances.length).toBe(2)
  })

  it('says whether a send actually went out', () => {
    const conn = new GatewayConnection('tok', { onFrame: vi.fn(), onStatusChange: vi.fn() }, factory)
    expect(conn.send({ type: 'ping' })).toBe(false) // not connected yet
    conn.connect()
    const sock = FakeWebSocket.instances[0]
    expect(conn.send({ type: 'ping' })).toBe(false) // connecting
    sock.simulateOpen()
    expect(conn.send({ type: 'ping' })).toBe(true)
    sock.simulateServerClose()
    expect(conn.send({ type: 'ping' })).toBe(false) // dropped
  })

  it('reports trouble after several connections in a row that never log in, and not for ordinary drops', () => {
    const onAuthTrouble = vi.fn()
    const conn = new GatewayConnection('tok', { onFrame: vi.fn(), onStatusChange: vi.fn(), onAuthTrouble }, factory)
    conn.connect()
    const failOnce = () => {
      const sock = FakeWebSocket.instances[FakeWebSocket.instances.length - 1]
      sock.simulateOpen()
      sock.simulateServerClose() // closed by the gateway before auth_ok, as it does for a dead session
      vi.advanceTimersByTime(40_000)
    }
    failOnce()
    failOnce()
    expect(onAuthTrouble).not.toHaveBeenCalled()
    failOnce()
    expect(onAuthTrouble).toHaveBeenCalledTimes(1)

    // A connection that logged in and later dropped is an ordinary drop.
    const sock = FakeWebSocket.instances[FakeWebSocket.instances.length - 1]
    sock.simulateOpen()
    sock.simulateMessage({ type: 'auth_ok', account: { id: 'a' } })
    sock.simulateServerClose()
    vi.advanceTimersByTime(40_000)
    expect(onAuthTrouble).toHaveBeenCalledTimes(1)
  })

  it('nudge reconnects at once, but not while a socket exists or after a deliberate close', () => {
    const conn = new GatewayConnection('tok', { onFrame: vi.fn(), onStatusChange: vi.fn() }, factory)
    conn.connect()
    conn.nudge() // a socket already exists
    expect(FakeWebSocket.instances.length).toBe(1)

    FakeWebSocket.instances[0].simulateServerClose() // now waiting out a backoff
    conn.nudge()
    expect(FakeWebSocket.instances.length).toBe(2) // did not wait for the timer
    vi.advanceTimersByTime(60_000)
    expect(FakeWebSocket.instances.length).toBe(2) // and the old timer was cancelled

    conn.close()
    conn.nudge()
    expect(FakeWebSocket.instances.length).toBe(2)
  })
})
