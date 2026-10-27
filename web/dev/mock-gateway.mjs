// A throwaway stand-in for the real gateway, for working on the web UI
// without the whole Tilt stack: `npm run dev:mock` from web/, then
// `npm run dev` in a second terminal (it talks to localhost:8080 by
// default, which is where this listens).
//
// It implements just enough of docs/ARCHITECTURE.md §5.1 and §5.2 to click
// through every screen: guest signup, a "Google" sign-in that succeeds at
// once, the once-only gender choice, group channels, random matching
// against a bot, and a live presence count. Nothing is stored beyond the
// process's memory, and nothing here is a model of the gateway's real
// rules (rate limits, bans, retention) - the real behaviour is checked
// against the real stack, not this.
import { createServer } from 'node:http'
import { randomUUID } from 'node:crypto'
import { WebSocketServer } from 'ws'

const PORT = Number(process.env.PORT ?? 8080)
const FRONTEND = process.env.FRONTEND_URL ?? 'http://localhost:8000'

const accounts = new Map() // token -> account
const rooms = new Map() // id -> room
const roomMessages = new Map() // id -> message[]
const sockets = new Set() // { ws, token, matchTimer }

const now = () => Math.floor(Date.now() / 1000)
const cors = {
  'Access-Control-Allow-Origin': '*',
  'Access-Control-Allow-Headers': 'Content-Type, Authorization',
  'Access-Control-Allow-Methods': 'GET, POST, OPTIONS',
}

function send(res, status, body) {
  res.writeHead(status, { 'Content-Type': 'application/json', ...cors })
  res.end(body === undefined ? undefined : JSON.stringify(body))
}
const fail = (res, status, code, message) => send(res, status, { error: { code, message } })

function newAccount(pseudonym, googleLinked = false) {
  const token = randomUUID()
  const account = {
    id: randomUUID(),
    pseudonym,
    gender: 'unspecified',
    gender_chosen: false,
    google_linked: googleLinked,
    created_at_unix: now(),
  }
  accounts.set(token, account)
  return { token, account }
}

function readBody(req) {
  return new Promise((resolve) => {
    let raw = ''
    req.on('data', (c) => (raw += c))
    req.on('end', () => {
      try {
        resolve(raw ? JSON.parse(raw) : {})
      } catch {
        resolve({})
      }
    })
  })
}

function tokenOf(req) {
  const h = req.headers.authorization ?? ''
  return h.startsWith('Bearer ') ? h.slice(7) : null
}

// A slowly wandering number, so the live count visibly moves.
let online = 128
function presence() {
  online = Math.max(40, online + Math.round((Math.random() - 0.45) * 6))
  const guests = Math.round(online * 0.6)
  const male = Math.round(online * 0.2)
  return { total: online, guests, male, female: online - guests - male }
}

const http = createServer(async (req, res) => {
  const url = new URL(req.url, `http://localhost:${PORT}`)
  if (req.method === 'OPTIONS') return send(res, 204)
  const token = tokenOf(req)
  const me = token ? accounts.get(token) : undefined

  if (req.method === 'GET' && url.pathname === '/v1/presence') return send(res, 200, presence())

  if (req.method === 'POST' && url.pathname === '/v1/auth/guest') {
    const { pseudonym } = await readBody(req)
    if (!pseudonym) return fail(res, 400, 'invalidargument', 'pseudonym is required')
    const { token: t, account } = newAccount(pseudonym)
    return send(res, 200, { session_token: t, account })
  }

  // The real flow bounces through Google and back; here it succeeds at once
  // and upgrades the guest in place, as docs/ARCHITECTURE.md §4.2 case B does.
  if (req.method === 'GET' && url.pathname === '/v1/auth/google/start') {
    const guest = accounts.get(url.searchParams.get('guest_token') ?? '')
    let t = url.searchParams.get('guest_token')
    if (guest) guest.google_linked = true
    else t = newAccount('Brave Heron', true).token
    res.writeHead(302, { Location: `${FRONTEND}/#auth_token=${t}`, ...cors })
    return res.end()
  }

  if (!me) return fail(res, 401, 'unauthenticated', 'sign in first')

  if (req.method === 'GET' && url.pathname === '/v1/me') return send(res, 200, me)

  if (req.method === 'POST' && url.pathname === '/v1/me/gender') {
    if (me.gender_chosen) return fail(res, 409, 'failedprecondition', 'gender has already been chosen')
    const { gender } = await readBody(req)
    me.gender = gender
    me.gender_chosen = true
    return send(res, 200, me)
  }

  if (req.method === 'GET' && url.pathname === '/v1/rooms') {
    return send(res, 200, { rooms: [...rooms.values()].filter((r) => r.members?.has(me.id)) })
  }

  if (req.method === 'POST' && url.pathname === '/v1/rooms') {
    const { name } = await readBody(req)
    const room = {
      id: randomUUID(), type: 'group', name, owner_account_id: me.id,
      invite_code: Math.random().toString(36).slice(2, 8).toUpperCase(), kept: false,
      created_at_unix: now(), members: new Set([me.id]),
    }
    rooms.set(room.id, room)
    return send(res, 200, room)
  }

  if (req.method === 'POST' && url.pathname === '/v1/rooms/join') {
    const { invite_code } = await readBody(req)
    const room = [...rooms.values()].find((r) => r.invite_code === String(invite_code).toUpperCase())
    if (!room) return fail(res, 404, 'notfound', 'no room with that invite code')
    room.members.add(me.id)
    return send(res, 200, room)
  }

  const leave = url.pathname.match(/^\/v1\/rooms\/([^/]+)\/leave$/)
  if (req.method === 'POST' && leave) {
    rooms.get(leave[1])?.members?.delete(me.id)
    return send(res, 204)
  }

  const msgs = url.pathname.match(/^\/v1\/rooms\/([^/]+)\/messages$/)
  if (req.method === 'GET' && msgs) return send(res, 200, { messages: roomMessages.get(msgs[1]) ?? [] })

  if (req.method === 'POST' && url.pathname === '/v1/reports') return send(res, 200, { report_id: randomUUID() })
  if (req.method === 'GET' && url.pathname === '/v1/blocks') return send(res, 200, { blocked_account_ids: [] })
  if (req.method === 'POST' && url.pathname === '/v1/blocks') return send(res, 200, { blocked: true })

  fail(res, 404, 'notfound', `${req.method} ${url.pathname} is not in the mock`)
})

const wss = new WebSocketServer({ server: http, path: '/v1/ws' })

const BOTS = ['Quiet Otter', 'Lucky Panda', 'Witty Lynx', 'Gentle Raven', 'Swift Gecko']
const BOT_LINES = ['hey!', 'haha same', 'which year are you in?', 'wait, really?', 'that is so true', 'lol okay okay', 'tell me more']

function post(roomId, sender, body) {
  const m = {
    message_id: randomUUID(), room_id: roomId, sender_account_id: sender.id,
    sender_pseudonym: sender.pseudonym, body, sent_at_unix: now(),
  }
  roomMessages.set(roomId, [...(roomMessages.get(roomId) ?? []), m])
  return {
    type: 'message', message_id: m.message_id, room_id: roomId, sender_account_id: sender.id,
    sender_pseudonym: sender.pseudonym, body, sent_at: m.sent_at_unix,
  }
}

wss.on('connection', (ws) => {
  const conn = { ws, account: null, matchTimer: null, room: null, bot: null }
  const out = (f) => ws.readyState === 1 && ws.send(JSON.stringify(f))
  const botSays = (text, delay = 1200) =>
    setTimeout(() => conn.room && conn.bot && out(post(conn.room.id, conn.bot, text)), delay)
  sockets.add(conn)

  const presenceTimer = setInterval(() => conn.account && out({ type: 'presence', ...presence() }), 5000)

  ws.on('message', (raw) => {
    let f
    try {
      f = JSON.parse(raw)
    } catch {
      return
    }
    if (f.type === 'auth') {
      conn.account = accounts.get(f.token)
      if (!conn.account) return ws.close()
      out({ type: 'auth_ok', account: conn.account })
      return out({ type: 'presence', ...presence() })
    }
    if (!conn.account) return
    switch (f.type) {
      case 'ping':
        return out({ type: 'pong' })
      case 'match_request':
        if (f.preference !== 'any' && !(conn.account.google_linked && conn.account.gender_chosen)) {
          return out({ type: 'error', code: 'permissiondenied', message: 'gender-filtered matching requires a verified account with a chosen gender' })
        }
        out({ type: 'match_queued' })
        conn.matchTimer = setTimeout(() => {
          conn.bot = { id: randomUUID(), pseudonym: BOTS[Math.floor(Math.random() * BOTS.length)] }
          conn.room = { id: randomUUID(), type: 'direct', kept: false, created_at_unix: now(), members: new Set([conn.account.id]) }
          rooms.set(conn.room.id, conn.room)
          out({ type: 'match_found', room_id: conn.room.id, partner_pseudonym: conn.bot.pseudonym, partner_account_id: conn.bot.id })
          botSays('hey, hi there!', 900)
        }, 2500)
        return
      case 'match_cancel':
        clearTimeout(conn.matchTimer)
        return out({ type: 'match_cancelled' })
      case 'send_message': {
        out(post(f.room_id, conn.account, f.body))
        if (conn.room?.id === f.room_id) botSays(BOT_LINES[Math.floor(Math.random() * BOT_LINES.length)])
        return
      }
      case 'keep_chat':
        if (conn.room) conn.room.kept = true
        return out({ type: 'keep_status', room_id: f.room_id, you: true, partner: true, kept: true })
      case 'match_next':
      case 'match_end': {
        const id = conn.room?.id
        conn.room = null
        if (id) out({ type: 'match_ended', room_id: id, reason: 'you_left' })
        if (f.type === 'match_next') {
          out({ type: 'match_queued' })
          conn.matchTimer = setTimeout(() => {
            conn.bot = { id: randomUUID(), pseudonym: BOTS[Math.floor(Math.random() * BOTS.length)] }
            conn.room = { id: randomUUID(), type: 'direct', kept: false, created_at_unix: now(), members: new Set([conn.account.id]) }
            rooms.set(conn.room.id, conn.room)
            out({ type: 'match_found', room_id: conn.room.id, partner_pseudonym: conn.bot.pseudonym, partner_account_id: conn.bot.id })
          }, 2000)
        }
        return
      }
    }
  })

  ws.on('close', () => {
    clearInterval(presenceTimer)
    clearTimeout(conn.matchTimer)
    sockets.delete(conn)
  })
})

http.listen(PORT, () => console.log(`mock gateway on http://localhost:${PORT} (ws /v1/ws), frontend expected at ${FRONTEND}`))
