// Measures, for the person whose "Find a match" completes a pairing (the
// seeker), whether match_queued or match_found reaches them first, and how far
// apart (INC-0030). It pairs two throwaway guests over and over on the LOCAL
// stack and prints a tally. In a run of 210 pairings a few arrived in the wrong
// order, the later one by about 10 ms at most.
//
// "never_found" trials are the matcher's own limit of 30 match requests a minute
// per person being hit, not lost messages: the experiment makes about 40 a minute.
//
// usage: node web/e2e/frame-order.mjs <trials>   (refuses anything but localhost)
import { createRequire } from 'node:module'
import { execSync } from 'node:child_process'
const require = createRequire('/home/aro/Flamingo/Flamingo-Chat/web/package.json')
const WebSocket = require('ws')

const API = process.env.API ?? 'http://localhost:8080'
if (!API.includes('localhost')) throw new Error('refusing to run against anything but localhost')
const WS = API.replace('http', 'ws') + '/v1/ws'
const TRIALS = Number(process.argv[2] ?? 40)
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

async function guest() {
  const r = await fetch(`${API}/v1/auth/guest`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' })
  return (await r.json()).session_token
}

function connect(token, name) {
  return new Promise((resolve, reject) => {
    const ws = new WebSocket(WS, { headers: { Origin: 'http://localhost:8000' } })
    const c = { name, ws, log: [], send: (f) => ws.send(JSON.stringify(f)) }
    ws.on('open', () => ws.send(JSON.stringify({ type: 'auth', token })))
    ws.on('message', (raw) => {
      const f = JSON.parse(raw)
      c.log.push({ t: performance.now(), type: f.type, f })
      if (f.type === 'auth_ok') resolve(c)
    })
    ws.on('error', reject)
    setTimeout(() => reject(new Error('no auth_ok for ' + name)), 15000)
  })
}
const waitFor = async (c, type, from, ms = 8000) => {
  const t0 = Date.now()
  while (Date.now() - t0 < ms) {
    const hit = c.log.slice(from).find((e) => e.type === type)
    if (hit) return hit
    await sleep(5)
  }
  return null
}

const a = await connect(await guest(), 'A')
const b = await connect(await guest(), 'B')

const tally = { queued_first: 0, found_first: 0, never_found: 0, never_queued: 0 }
const gaps = []
for (let i = 0; i < TRIALS; i++) {
  const la = a.log.length, lb = b.log.length
  a.send({ type: 'match_request', preference: 'any' })
  await waitFor(a, 'match_queued', la)
  b.send({ type: 'match_request', preference: 'any' }) // B completes the pairing: B is the seeker
  const found = await waitFor(b, 'match_found', lb)
  await sleep(300) // let a late match_queued arrive
  const queued = b.log.slice(lb).find((e) => e.type === 'match_queued')
  if (!found) {
    tally.never_found++
    const q = execSync("kubectl exec deploy/redis -- sh -c 'echo any=$(redis-cli zcard mq:any) entries=$(redis-cli --scan --pattern \"mq:entry:*\" | wc -l)'").toString().trim()
    console.log(`trial ${i}: no match_found for B. queue now: ${q}; B frames this trial: ${b.log.slice(lb).map((e) => e.type).join(',')}; A frames: ${a.log.slice(la).map((e) => e.type).join(',')}`)
  }
  else if (!queued) tally.never_queued++
  else if (queued.t < found.t) { tally.queued_first++; gaps.push(found.t - queued.t) }
  else { tally.found_first++; gaps.push(found.t - queued.t) }
  // End the chat so the next trial starts clean.
  b.send({ type: 'match_end' })
  if (!found) { b.send({ type: 'match_cancel' }); a.send({ type: 'match_cancel' }); await sleep(200) } else await waitFor(a, 'match_ended', la)
  await sleep(150)
}
console.log(JSON.stringify(tally))
gaps.sort((x, y) => x - y)
console.log('found minus queued (ms; negative = found arrived FIRST): min', gaps[0]?.toFixed(1), 'median', gaps[Math.floor(gaps.length / 2)]?.toFixed(1), 'max', gaps.at(-1)?.toFixed(1))
a.ws.close(); b.ws.close()
process.exit(0)
