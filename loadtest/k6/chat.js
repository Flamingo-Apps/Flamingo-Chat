// Load test for Flamingo's chat path (DEC-0025, DEC-0109): the question it
// answers is "how many people can be chatting at once on this cluster before
// messages get slow or fail?"
//
// Every virtual user behaves like a student who opens the app and talks:
//   1. signs up as a guest                    (HTTP, once per user)
//   2. opens the WebSocket and authenticates
//   3. asks for a random match and waits to be paired with another virtual user
//   4. sends a message every SEND_EVERY_S seconds for SESSION_S seconds; the
//      partner times how long each one took to arrive
//   5. ends the chat and goes back to 3, for the whole test
// So it exercises the real path: gateway, chat, Kafka, the delivery worker,
// Redis and back out to the other user's socket. A message's "latency" is the
// time from the sender's clock to the receiver's, both read inside the same k6
// process, so there is no clock difference between them.
//
// One level of load per run: the number of simultaneous users (TARGET) is
// ramped up, held for HOLD_S seconds, and ramped down. Run several levels and
// read the results side by side to find where it stops coping (run.sh does).
import http from 'k6/http'
import ws from 'k6/ws'
import { check, sleep } from 'k6'
import { Counter, Rate, Trend } from 'k6/metrics'

const HTTP = __ENV.GATEWAY_HTTP || 'http://gateway:8080'
const WS = __ENV.GATEWAY_WS || 'ws://gateway:8080/v1/ws'
const TARGET = Number(__ENV.TARGET || 20)
const RAMP_S = Number(__ENV.RAMP_S || 30)
const HOLD_S = Number(__ENV.HOLD_S || 120)
const SESSION_S = Number(__ENV.SESSION_S || 60)
const SEND_EVERY_S = Number(__ENV.SEND_EVERY_S || 3)
const MATCH_TIMEOUT_S = Number(__ENV.MATCH_TIMEOUT_S || 20)

const msgLatency = new Trend('msg_latency_ms', true)
const matchWait = new Trend('match_wait_ms', true)
const sent = new Counter('msgs_sent')
const received = new Counter('msgs_received')
const frameErrors = new Counter('frame_errors')
// A send that reached a chat the partner had just closed: the server is right to
// refuse it. It is the end-of-session race of this script, not a backend fault.
const closedSends = new Counter('sends_to_closed_chat')
const sessionMatched = new Rate('session_matched')
const signupFailed = new Counter('signup_failed')
const connectFailed = new Counter('ws_connect_failed')

export const options = {
  scenarios: {
    users: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: `${RAMP_S}s`, target: TARGET },
        { duration: `${HOLD_S}s`, target: TARGET },
        { duration: '15s', target: 0 },
      ],
      gracefulRampDown: '20s',
      gracefulStop: '30s',
    },
  },
  // Not a pass/fail gate for the app, just the lines the results are read
  // against: a chat message should arrive well inside a second.
  thresholds: {
    msg_latency_ms: ['p(95)<1000'],
    session_matched: ['rate>0.95'],
    frame_errors: ['count<10'],
  },
  summaryTrendStats: ['avg', 'med', 'p(90)', 'p(95)', 'p(99)', 'max'],
}

let token = null // a virtual user keeps one account for the whole test

function signUp() {
  const res = http.post(`${HTTP}/v1/auth/guest`, JSON.stringify({}), { headers: { 'Content-Type': 'application/json' }, tags: { name: 'signup' } })
  if (res.status !== 200) {
    signupFailed.add(1)
    return null
  }
  return res.json('session_token')
}

export default function () {
  if (!token) {
    token = signUp()
    if (!token) {
      sleep(2)
      return
    }
  }

  let myId = null
  let roomId = null
  let matchRequestedAt = 0
  let chatStartedAt = 0
  let n = 0
  let gotMatch = false

  const res = ws.connect(WS, { tags: { name: 'ws' } }, (socket) => {
    socket.on('open', () => socket.send(JSON.stringify({ type: 'auth', token })))

    socket.on('message', (raw) => {
      let f
      try {
        f = JSON.parse(raw)
      } catch {
        return
      }
      switch (f.type) {
        case 'auth_ok':
          myId = f.account.id
          matchRequestedAt = Date.now()
          socket.send(JSON.stringify({ type: 'match_request', preference: 'any' }))
          socket.setTimeout(() => {
            if (!gotMatch) socket.close() // nobody to pair with: leave and try again
          }, MATCH_TIMEOUT_S * 1000)
          break
        case 'match_found':
          gotMatch = true
          roomId = f.room_id
          chatStartedAt = Date.now()
          matchWait.add(chatStartedAt - matchRequestedAt)
          socket.setInterval(() => {
            if (Date.now() - chatStartedAt > SESSION_S * 1000) {
              socket.send(JSON.stringify({ type: 'match_end' }))
              socket.setTimeout(() => socket.close(), 300)
              return
            }
            n += 1
            sent.add(1)
            socket.send(JSON.stringify({ type: 'send_message', room_id: roomId, client_msg_id: `${myId}-${n}`, body: `LT|${Date.now()}` }))
          }, SEND_EVERY_S * 1000)
          break
        case 'message':
          // Only the partner's messages measure delivery; our own echo does not.
          if (f.sender_account_id !== myId && typeof f.body === 'string' && f.body.startsWith('LT|')) {
            msgLatency.add(Date.now() - Number(f.body.slice(3)))
            received.add(1)
          }
          break
        case 'match_ended':
          // The partner left: end this session and queue again.
          if (f.reason === 'partner_left') socket.setTimeout(() => socket.close(), 100)
          break
        case 'error':
          if (typeof f.message === 'string' && f.message.includes('conversation is closed')) closedSends.add(1)
          else frameErrors.add(1)
          break
      }
    })

    socket.on('error', () => connectFailed.add(1))
    socket.setTimeout(() => socket.close(), (MATCH_TIMEOUT_S + SESSION_S + 10) * 1000) // never hang
  })

  check(res, { 'websocket opened': (r) => r && r.status === 101 })
  sessionMatched.add(gotMatch)
  sleep(1)
}

// The summary is printed as one JSON line after a marker, so loadtest/run.sh
// can pull it out of the Job's log.
export function handleSummary(data) {
  const m = (name, key) => data.metrics[name] && data.metrics[name].values[key]
  const out = {
    users: { target: TARGET, ramp_s: RAMP_S, hold_s: HOLD_S, session_s: SESSION_S, send_every_s: SEND_EVERY_S, vus_max: m('vus_max', 'value') },
    messages: { sent: m('msgs_sent', 'count'), received: m('msgs_received', 'count') },
    msg_latency_ms: { avg: m('msg_latency_ms', 'avg'), med: m('msg_latency_ms', 'med'), p90: m('msg_latency_ms', 'p(90)'), p95: m('msg_latency_ms', 'p(95)'), p99: m('msg_latency_ms', 'p(99)'), max: m('msg_latency_ms', 'max') },
    match_wait_ms: { med: m('match_wait_ms', 'med'), p95: m('match_wait_ms', 'p(95)') },
    sessions_matched_rate: m('session_matched', 'rate'),
    errors: { frame_errors: m('frame_errors', 'count'), sends_to_closed_chat: m('sends_to_closed_chat', 'count'), signup_failed: m('signup_failed', 'count'), ws_connect_failed: m('ws_connect_failed', 'count'), http_req_failed_rate: m('http_req_failed', 'rate') },
    thresholds: Object.fromEntries(Object.entries(data.metrics).filter(([, v]) => v.thresholds).map(([k, v]) => [k, Object.values(v.thresholds).every((t) => t.ok)])),
  }
  return { stdout: `\n===SUMMARY-JSON===\n${JSON.stringify(out)}\n` }
}
