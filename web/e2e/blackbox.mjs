// Black-box test of the web app against the real backend (DEC-0104).
//
// "Black box" means it only does what a person or an attacker could do from
// outside: it drives the real app in real browsers and reads what comes
// back. It never calls a function or reads the database to decide whether
// something passed. The one exception is setup and clean-up on a LOCAL
// stack (resetting the guest-signup counter, banning an account through the
// admin tool, restarting the gateway), which the script refuses to do
// against anything that is not the local kind cluster.
//
// Run the stack first (`tilt up` from the repository root), then:
//   npm run test:blackbox            from web/
//
// The plan, one block per area. Each check has an id so a result can be
// pointed at:
//   BB-01 welcome screen          BB-09 live online count
//   BB-02 first visit, keyboard   BB-10 Google sign-in and gender (local fake)
//   BB-03 identity prompt         BB-11 gender-filtered matching
//   BB-04 two people chat         BB-12 gateway restart, reconnect
//   BB-05 hostile and odd input   BB-13 phone layout
//   BB-06 partner leaves, keep    BB-14 a banned person is removed
//   BB-07 cancel a search         BB-15 guest signup rate limit
//   BB-08 group channels          BB-16 no uncaught errors anywhere
//   BB-17 keep question: default is to keep chatting, mutual keep -> Inbox with names
//   BB-18 stop messaging closes a saved chat for both, and the server refuses sends
//   BB-19 deleting a chat removes it for one person, the other keeps it by name
//   BB-20 channel limits: a guest can be in 10, and guessing invite codes is throttled
//   BB-21 a refresh mid-chat puts the person back in it (DEC-0112)
//   BB-22 Next still works after the gateway restarts, with no internal text on screen
//   BB-23 a returning visitor lands straight in the app, never on the welcome screen
//   BB-24 a verified person sees the Verified check on a phone-sized screen
//   BB-25 a saved session with the server unreachable never shows the welcome screen
//   BB-26 a session the server refuses (401, 403, 404) is ended with a clear message
//   BB-27 a reload mid-chat never offers a new search while the chat is being restored
//   BB-28 a match that arrives before its acknowledgement is not wiped (INC-0030)
//   BB-29 a match announcement that never arrives is found by asking the server
//   BB-30 signing out in one tab signs out the other
//   BB-31 a dropped connection keeps the typed message and says why it cannot be sent
//   BB-32 a session that died while the app was open is noticed and ended
//   BB-33 closing the tab while searching leaves no ghost in the queue
//   BB-34 a refresh while searching keeps the search going
import { execFileSync } from 'node:child_process'
import { mkdirSync, writeFileSync } from 'node:fs'
import { chromium } from 'playwright'
import WebSocket from 'ws'

const WEB = process.env.WEB_URL ?? 'http://localhost:8000'
const GATEWAY = process.env.GATEWAY_URL ?? 'http://localhost:8080'
const OUT = new URL('./out/', import.meta.url).pathname
mkdirSync(OUT, { recursive: true })
const REPO = new URL('../../', import.meta.url).pathname

const results = []
const live = [] // every page opened, so a failing check can screenshot them all
const pageErrors = []
const consoleErrors = []
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const assert = (cond, msg) => {
  if (!cond) throw new Error(msg)
}

function sh(cmd, args, opts = {}) {
  return execFileSync(cmd, args, { encoding: 'utf8', cwd: REPO, timeout: 120_000, ...opts })
}

// Everything below that changes the stack only runs against the local kind cluster.
function requireLocalStack() {
  const ctx = sh('kubectl', ['config', 'current-context']).trim()
  assert(ctx.startsWith('kind-'), `refusing to run: kubectl context is "${ctx}", not a local kind cluster`)
  assert(WEB.includes('localhost') && GATEWAY.includes('localhost'), 'refusing to run against anything but localhost')
}
function resetAllLimits() {
  sh('kubectl', ['exec', 'deploy/redis', '--', 'sh', '-c', "redis-cli --scan --pattern 'rl:*' | xargs -r redis-cli del"])
}
// People still waiting to be matched, from an earlier run or a page that was
// closed, would be paired with this run's users. Local stack only.
function resetMatchingQueue() {
  sh('kubectl', ['exec', 'deploy/redis', '--', 'sh', '-c', "for p in 'mq:*' 'match:members:*'; do redis-cli --scan --pattern \"$p\" | xargs -r redis-cli del; done"])
}
function resetGuestLimiter() {
  sh('kubectl', ['exec', 'deploy/redis', '--', 'sh', '-c', "redis-cli --scan --pattern 'rl:guest_signup:*' | xargs -r redis-cli del"])
}

// ONLY=BB-21,BB-23 runs just those checks, for the ones that stand alone.
const ONLY = process.env.ONLY?.split(',')
async function run(id, title, fn) {
  if (ONLY && !ONLY.includes(id)) return
  const t0 = Date.now()
  try {
    await fn()
    results.push({ id, title, status: 'PASS', ms: Date.now() - t0 })
  } catch (e) {
    results.push({ id, title, status: 'FAIL', ms: Date.now() - t0, error: String(e.message).split('\n')[0] })
    for (const { page, label } of live) await page.screenshot({ path: `${OUT}${id}-${label}.png` }).catch(() => {})
  } finally {
    await closeScratch()
  }
  const r = results.at(-1)
  console.log(`${r.status === 'PASS' ? 'PASS' : 'FAIL'}  ${id}  ${title}${r.error ? `\n        ${r.error}` : ''}`)
}

// Users created for one check only. They are closed when the check ends, pass
// or fail, so a failure cannot leave pages behind that keep searching and get
// paired with the next check's users.
const scratch = []
async function newScratch(browser, label, opts = {}) {
  const u = await newUser(browser, label, opts)
  scratch.push(u)
  return u
}
async function closeScratch() {
  while (scratch.length) await scratch.pop().ctx.close().catch(() => {})
}

async function newUser(browser, label, opts = {}) {
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 }, ...opts })
  ctx.setDefaultTimeout(12_000)
  const page = await ctx.newPage()
  page.on('pageerror', (e) => pageErrors.push(`${label}: ${e}`))
  page.on('console', (m) => m.type() === 'error' && consoleErrors.push(`${label}: ${m.text()}`))
  live.push({ page, label })
  return { ctx, page, label }
}

const findMatchBtn = (p) => p.getByRole('button', { name: 'Find a match' })
const composer = (p) => p.locator('.chat__composer textarea')
const bubbles = (p) => p.locator('.chat__messages .bubble')
const texts = (p) => p.locator('.chat__messages .bubble > span:first-child') // message text only, without the time
const chip = (p, name) => p.getByRole('button', { name, exact: true })
const gotIt = async (p) => {
  const ok = p.getByRole('button', { name: 'Got it' })
  await ok.waitFor({ timeout: 4000 }).catch(() => {})
  if (await ok.isVisible()) await ok.click()
}
async function enterAsGuest(u) {
  await u.page.goto(WEB)
  await u.page.getByText("Nope, I'm new").click()
  await gotIt(u.page)
  await findMatchBtn(u.page).waitFor()
}
async function say(p, text) {
  await composer(p).fill(text)
  await p.keyboard.press('Enter')
}
const pseudonym = async (p) => (await p.locator('.profile__name').textContent())?.trim()
const onlineCount = async (p) => Number(((await p.locator('.online').first().textContent()) ?? '').match(/(\d[\d,]*)\s*online/)?.[1]?.replace(/,/g, '') ?? NaN)
const tokenOf = (p) => p.evaluate(() => localStorage.getItem('flamingo_session_token'))
const NAME = {} // pseudonyms by user label, filled in as people sign up

// What the server says if a client sends a message with a raw WebSocket: the
// way an attacker (or a buggy client) would, with no web app in between.
function rawSend(token, roomId, body) {
  return new Promise((resolve, reject) => {
    const ws = new WebSocket(`${GATEWAY.replace('http', 'ws')}/v1/ws`)
    const timer = setTimeout(() => { ws.close(); reject(new Error('no answer from the gateway')) }, 10_000)
    ws.on('open', () => ws.send(JSON.stringify({ type: 'auth', token })))
    ws.on('message', (raw) => {
      const f = JSON.parse(raw)
      if (f.type === 'auth_ok') ws.send(JSON.stringify({ type: 'send_message', room_id: roomId, client_msg_id: crypto.randomUUID(), body }))
      else if (f.type === 'error' || f.type === 'message') { clearTimeout(timer); ws.close(); resolve(f) }
    })
    ws.on('error', reject)
  })
}
const roomIdOf = (p, name) => p.evaluate(async ([g, who]) => {
  const t = localStorage.getItem('flamingo_session_token')
  const rooms = (await (await fetch(`${g}/v1/rooms`, { headers: { Authorization: `Bearer ${t}` } })).json()).rooms
  return rooms.find((r) => r.type === 'direct' && r.partner_pseudonym === who)?.id
}, [GATEWAY, name])
// How long a message takes to appear on another page, in ms; Infinity if it never does within `limit`.
async function arrival(sender, receiver, text, limit = 40_000) {
  const t0 = Date.now()
  await say(sender, text)
  const ok = await receiver.getByText(text, { exact: true }).waitFor({ timeout: limit }).then(() => true, () => false)
  return ok ? Date.now() - t0 : Infinity
}
const FAST = 3000 // a message should reach the other person well inside this
const show = (ms) => (ms === Infinity ? 'never (waited 40s)' : `${(ms / 1000).toFixed(1)}s`)

async function matchPair(a, b) {
  await Promise.all([findMatchBtn(a.page).click(), findMatchBtn(b.page).click()])
  await Promise.all([composer(a.page).waitFor({ timeout: 20_000 }), composer(b.page).waitFor({ timeout: 20_000 })])
}

requireLocalStack()
for (const url of [WEB, `${GATEWAY}/v1/presence`]) assert((await fetch(url).catch(() => null))?.ok, `${url} is not answering - is the stack up (tilt up)?`)
resetGuestLimiter()
resetMatchingQueue()

// Watches the page from before it loads and records what must never happen.
const watchForWelcome = () => {
  window.__sawWelcome = false
  new MutationObserver(() => {
    if (document.body && document.body.innerText.includes('Have you been here before?')) window.__sawWelcome = true
  }).observe(document, { childList: true, subtree: true, characterData: true })
}
const watchForOfferedSearch = () => {
  window.__sawOfferedSearch = false
  new MutationObserver(() => {
    const offered = [...document.querySelectorAll('button')].some((b) => /^Find a match$/.test((b.textContent ?? '').trim()) && !b.disabled)
    if (offered) window.__sawOfferedSearch = true
  }).observe(document, { childList: true, subtree: true, characterData: true, attributes: true })
}
const accountOf = (p) => p.evaluate(() => localStorage.getItem('flamingo_account'))
// The shapes the gateway really sends over HTTP (httpapi/errors.go): underscores in the code.
const gatewayError = (status, code, message) => ({ status, contentType: 'application/json', body: JSON.stringify({ error: { code, message } }) })

const browser = await chromium.launch()
let A, B, M

await run('BB-01', 'welcome screen: the question, both answers, and a live online count', async () => {
  const v = await newUser(browser, 'visitor')
  await v.page.goto(WEB)
  await v.page.getByRole('heading', { name: 'Have you been here before?' }).waitFor()
  await v.page.getByText("Nope, I'm new").waitFor()
  await v.page.getByText('Yes, sign me in').waitFor()
  await v.page.waitForFunction(() => /\d+\s*online/.test(document.querySelector('.online')?.textContent ?? ''))
  assert((await onlineCount(v.page)) >= 0, 'online count is not a number')
  assert((await v.page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)), 'welcome screen scrolls sideways')
  await v.ctx.close()
})

await run('BB-02', 'first visit using only the keyboard; Mochi introduces herself once; a reload keeps the session', async () => {
  A = await newUser(browser, 'A')
  await A.page.goto(WEB)
  for (let i = 0; i < 12; i++) {
    await A.page.keyboard.press('Tab')
    if ((await A.page.evaluate(() => document.activeElement?.textContent ?? '')).includes("Nope, I'm new")) break
  }
  await A.page.keyboard.press('Enter')
  await A.page.getByRole('heading', { name: "Hi, I'm Mochi!" }).waitFor()
  await A.page.getByRole('button', { name: 'Got it' }).click()
  await findMatchBtn(A.page).waitFor()
  const name = await pseudonym(A.page)
  assert(name && name.split(' ').length === 2, `unexpected pseudonym "${name}"`)
  NAME.A = name
  await A.page.reload()
  await findMatchBtn(A.page).waitFor()
  await sleep(1200)
  assert((await A.page.getByRole('heading', { name: "Hi, I'm Mochi!" }).count()) === 0, 'the intro showed again after a reload')
  assert((await pseudonym(A.page)) === name, 'the session did not survive a reload')
})

B = await newUser(browser, 'B')
await enterAsGuest(B)
NAME.B = await pseudonym(B.page)

await run('BB-03', 'identity prompt: opens, keeps Google behind a panel, previews the anonymous name, asks nothing of Google until pressed', async () => {
  const hits = []
  A.page.on('request', (r) => r.url().includes('/v1/auth/google/start') && hits.push(r.url()))
  const name = await pseudonym(A.page)
  await chip(A.page, 'Male').click()
  await A.page.getByRole('heading', { name: 'Want to unlock this?' }).waitFor()
  assert((await A.page.getByRole('dialog').textContent())?.includes(name), 'the card does not show the visitor their own name')
  assert((await A.page.locator('.google-signin').count()) === 0, 'the Google button is showing before the panel is opened')
  await A.page.getByRole('button', { name: 'Get your identity' }).click()
  await A.page.locator('.google-signin').waitFor()
  await A.page.getByRole('button', { name: 'Get your identity' }).click()
  assert((await A.page.locator('.google-signin').count()) === 0, 'the panel did not close again')
  await A.page.getByRole('button', { name: 'Maybe later' }).click()
  await A.page.getByRole('heading', { name: "Here's how others see you" }).waitFor()
  assert((await A.page.getByRole('dialog').textContent())?.includes(name), 'the preview does not show the anonymous name')
  await A.page.keyboard.press('Escape')
  await A.page.getByRole('dialog').waitFor({ state: 'detached' })
  assert(hits.length === 0, 'something started the Google sign-in without being asked')
})

await run('BB-04', 'two people match and chat both ways, each seeing the other as an anonymous name', async () => {
  const [na, nb] = [await pseudonym(A.page), await pseudonym(B.page)]
  await matchPair(A, B)
  assert((await A.page.locator('.chat__title').textContent()) === nb, "A's chat is not titled with B's name")
  assert((await B.page.locator('.chat__title').textContent()) === na, "B's chat is not titled with A's name")
  await say(A.page, 'hello from A')
  await B.page.getByText('hello from A').waitFor()
  await say(B.page, 'hello from B')
  await A.page.getByText('hello from B').waitFor()
  assert((await composer(A.page).inputValue()) === '', 'the box was not cleared after sending')
})

await run('BB-05', 'odd and hostile input: blanks, script tags, huge words, new lines, 2,000-character cap, a rapid burst in order', async () => {
  const before = await bubbles(B.page).count()
  await composer(A.page).fill('   ')
  await A.page.keyboard.press('Enter')
  assert(await A.page.getByRole('button', { name: 'Send' }).isDisabled(), 'Send is enabled for a blank message')
  await sleep(700)
  assert((await bubbles(B.page).count()) === before, 'a blank message reached the other person')

  const xss = '<img src=x onerror="window.__xss=1"><script>window.__xss=2</script>'
  await say(A.page, xss)
  await B.page.getByText('<img src=x', { exact: false }).waitFor()
  assert(!(await B.page.evaluate(() => window.__xss)) && !(await A.page.evaluate(() => window.__xss)), 'a message ran as code in the page')

  await say(A.page, 'x'.repeat(500))
  await B.page.locator('.bubble', { hasText: 'xxxxxxxxxx' }).last().waitFor()
  for (const u of [A, B]) assert(await u.page.locator('.chat__messages').evaluate((e) => e.scrollWidth <= e.clientWidth + 1), 'a long word makes the chat scroll sideways')

  await composer(A.page).fill('line one')
  await A.page.keyboard.press('Shift+Enter')
  await A.page.keyboard.type('line two')
  await A.page.keyboard.press('Enter')
  await B.page.locator('.bubble', { hasText: 'line two' }).waitFor()
  assert((await B.page.locator('.bubble', { hasText: 'line two' }).first().innerText()).includes('line one\nline two'), 'Shift+Enter did not keep the line break')

  await composer(A.page).focus()
  await A.page.keyboard.type('y'.repeat(2005))
  assert((await composer(A.page).inputValue()).length === 2000, 'the box accepts more than 2,000 characters')
  await composer(A.page).fill('')

  for (let i = 1; i <= 20; i++) await say(A.page, `burst ${i}`)
  await B.page.getByText('burst 20', { exact: true }).waitFor()
  const seen = (await texts(B.page).allInnerTexts()).map((t) => t.match(/^burst (\d+)$/)?.[1]).filter(Boolean).map(Number)
  assert(seen.length === 20 && seen.every((n, i) => n === i + 1), `a rapid burst arrived out of order or incomplete: ${seen.join(',')}`)
})

await run('BB-06', 'partner leaves: the one who stayed can save the chat, read-only, named after the other; the one who left gets nothing', async () => {
  await A.page.getByRole('button', { name: 'End', exact: true }).click()
  await findMatchBtn(A.page).waitFor()
  await B.page.getByRole('heading', { name: 'Your partner left' }).waitFor()
  assert(/\d+s/.test((await B.page.locator('.empty p').textContent()) ?? ''), 'no countdown is shown')
  await B.page.getByRole('button', { name: 'Save to Inbox' }).click()
  await B.page.getByRole('heading', { name: 'Saved to your Inbox!' }).waitFor()
  assert((await B.page.locator('.empty p').textContent())?.includes("can't message them again"), 'it does not say the chat is read-only')
  await B.page.getByRole('button', { name: 'Open in Inbox' }).click()
  await B.page.locator('.chat__title', { hasText: NAME.A }).waitFor()
  await B.page.getByText('hello from A').waitFor()
  assert((await composer(B.page).count()) === 0, 'a chat saved after the other person left can still be messaged')
  assert((await B.page.locator('.chat__closed').count()) === 1, 'no read-only notice')
  const code = (await rawSend(await tokenOf(B.page), await roomIdOf(B.page, NAME.A), 'sneaky')).code ?? ''
  assert(/precondition/i.test(code), `the server accepted or mis-handled a send to a closed chat: ${code}`)
  await A.page.locator('.nav-item', { hasText: 'Inbox' }).click()
  await A.page.getByText('Nothing here yet').waitFor()
  await B.page.locator('.nav-item', { hasText: 'Random chat' }).click()
  await A.page.locator('.nav-item', { hasText: 'Random chat' }).click()
})

await run('BB-07', 'cancelling a search clears the screen at once, and matching still works afterwards (DEC-0103)', async () => {
  await findMatchBtn(A.page).click()
  await A.page.getByText('Looking for someone...').waitFor()
  await A.page.getByRole('button', { name: 'Cancel' }).click()
  await findMatchBtn(A.page).waitFor({ timeout: 3000 })
  assert((await A.page.getByText('Looking for someone...').count()) === 0, 'still says searching after a cancel')
  await matchPair(A, B)
})

await run('BB-17', 'keep question: the default is to keep chatting; a mutual keep moves the chat to both Inboxes under each other\'s names', async () => {
  await A.page.getByRole('button', { name: 'Keep', exact: true }).click()
  await A.page.getByRole('button', { name: 'Waiting...' }).waitFor()
  await B.page.getByRole('heading', { name: `${NAME.A} wants to keep this chat` }).waitFor()
  const focused = await B.page.evaluate(() => document.activeElement?.textContent ?? '')
  assert(focused.includes('Keep chatting for now'), `the focused button is "${focused}", not the keep-chatting default`)
  await sleep(26_000) // saying nothing must never save a chat
  await B.page.getByRole('heading', { name: `${NAME.A} wants to keep this chat` }).waitFor({ state: 'detached' })
  assert((await composer(B.page).count()) === 1, 'the chat did not carry on after the question timed out')
  const t = await arrival(B.page, A.page, 'still here, not saved yet')
  assert(t <= FAST, `chat stopped working after the question: ${show(t)}`)
  await B.page.getByRole('button', { name: 'Keep', exact: true }).click() // A already asked, so this saves it
  for (const u of [A, B]) await u.page.getByRole('heading', { name: 'Saved to your Inbox!' }).waitFor()
  await Promise.all([A.page.getByRole('button', { name: 'Open in Inbox' }).click(), B.page.getByRole('button', { name: 'Open in Inbox' }).click()])
  await A.page.locator('.chat__title', { hasText: NAME.B }).waitFor()
  await B.page.locator('.chat__title', { hasText: NAME.A }).waitFor()
  assert((await A.page.getByText('Kept conversation').count()) === 0, 'the generic "Kept conversation" name is back')
  const ms = await arrival(A.page, B.page, 'hello from the inbox')
  assert(ms <= FAST, `a saved chat took ${show(ms)} to deliver a message`)
})

await run('BB-18', 'stop messaging closes a saved chat for both people at once, and the server refuses sends', async () => {
  await A.page.getByRole('button', { name: 'Stop messaging' }).click()
  await A.page.getByRole('heading', { name: 'Stop messaging?' }).waitFor()
  await A.page.getByRole('button', { name: 'Stop messaging', exact: true }).last().click()
  await A.page.locator('.chat__closed').waitFor()
  await B.page.locator('.chat__closed').waitFor({ timeout: 10_000 })
  for (const u of [A, B]) assert((await composer(u.page).count()) === 0, 'the box is still there after the chat was closed')
  const code = (await rawSend(await tokenOf(B.page), await roomIdOf(B.page, NAME.A), 'after close')).code ?? ''
  assert(/precondition/i.test(code), `the server did not refuse a send to a closed chat: ${code}`)
  await B.page.getByText('hello from the inbox').waitFor() // still readable
})

await run('BB-19', 'deleting removes the chat for that person only; the other still has it, read-only, under the same name', async () => {
  // B also still has the earlier read-only chat from BB-06, so count rows
  // rather than expecting an empty Inbox: deleting one removes exactly one.
  const before = await B.page.locator('.room').count()
  assert(before >= 2, `expected B to have two saved chats with A, found ${before}`)
  await B.page.getByRole('button', { name: 'Delete', exact: true }).first().click()
  await B.page.getByRole('heading', { name: 'Delete this chat?' }).waitFor()
  await B.page.getByRole('button', { name: 'Delete', exact: true }).last().click()
  await B.page.waitForFunction((n) => document.querySelectorAll('.room').length === n, before - 1)
  await A.page.reload()
  await A.page.locator('.nav-item', { hasText: 'Inbox' }).click()
  await A.page.locator('.room', { hasText: NAME.B }).waitFor()
  await A.page.locator('.room', { hasText: NAME.B }).click()
  await A.page.locator('.chat__closed').waitFor()
  await A.page.getByText('hello from the inbox').waitFor()
  for (const u of [A, B]) await u.page.locator('.nav-item', { hasText: 'Random chat' }).click()
})

await run('BB-08', 'group channel: create, join by invite code, talk both ways, leave', async () => {
  for (const u of [A, B]) await u.page.locator('.nav-item', { hasText: 'Channels' }).click()
  await A.page.locator('#new-channel').fill('blackbox room')
  await A.page.getByRole('button', { name: 'Create' }).click()
  await A.page.locator('.chat__subtitle', { hasText: 'Invite code:' }).waitFor()
  const code = (await A.page.locator('.chat__subtitle').textContent()).replace('Invite code:', '').trim()
  assert(/^\w{4,}$/.test(code), `odd invite code "${code}"`)
  await B.page.locator('#invite').fill(code)
  await B.page.getByRole('button', { name: 'Join' }).click()
  await B.page.locator('.chat__title', { hasText: 'blackbox room' }).waitFor()
  const first = await arrival(A.page, B.page, 'welcome to the room')
  assert(first <= FAST, `the first message in a new channel took ${show(first)} to reach the other person (should be under ${FAST / 1000}s)`)
  assert((await A.page.getByText('welcome to the room', { exact: true }).count()) === 1, 'the sender does not see their own message')
  const back = await arrival(B.page, A.page, 'glad to be here')
  assert(back <= FAST, `a reply in the channel took ${show(back)}`)
  await B.page.locator('.chat__actions').getByRole('button', { name: 'Leave' }).click()
  await B.page.locator('.room', { hasText: 'blackbox room' }).waitFor({ state: 'detached' })
  await B.page.locator('#invite').fill('NOSUCHCODE')
  await B.page.getByRole('button', { name: 'Join' }).click()
  await B.page.locator('.error-text').waitFor()
  for (const u of [A, B]) await u.page.locator('.nav-item', { hasText: 'Random chat' }).click()
})

await run('BB-09', 'the online count reflects the people connected (at least these two)', async () => {
  await A.page.waitForFunction(() => Number((document.querySelector('.online')?.textContent ?? '').match(/(\d+)\s*online/)?.[1]) >= 2, null, { timeout: 20_000 })
})

let V
await run('BB-10', 'Google sign-in (local fake) leads to the once-only gender choice, then Verified; a second change is refused', async () => {
  V = await newUser(browser, 'V')
  await V.page.goto(WEB)
  await V.page.getByText('Yes, sign me in').click()
  await V.page.locator('.google-signin').click()
  await V.page.getByRole('heading', { name: 'Last step, promise' }).waitFor()
  await V.page.getByRole('button', { name: 'Lock it in' }).isDisabled().then((d) => assert(d, 'Lock it in is enabled before a choice is made'))
  await V.page.locator('.choice', { hasText: 'Female' }).click()
  await V.page.getByRole('button', { name: 'Lock it in' }).click()
  await gotIt(V.page)
  await V.page.locator('.profile__tag', { hasText: 'Verified' }).waitFor()
  assert((await V.page.locator('.chip--locked').count()) === 0, 'the gender filters are still locked after verifying')
  const status = await V.page.evaluate(async (g) => {
    const res = await fetch(`${g}/v1/me/gender`, { method: 'POST', headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${localStorage.getItem('flamingo_session_token')}` }, body: JSON.stringify({ gender: 'male' }) })
    return res.status
  }, GATEWAY)
  assert(status >= 400 && status < 500, `changing gender a second time was not refused (HTTP ${status})`)
})

await run('BB-11', 'a verified person can search with a gender filter; a guest cannot even try (the card opens instead)', async () => {
  await chip(V.page, 'Male').click()
  assert((await chip(V.page, 'Male').getAttribute('class'))?.includes('chip--on'), 'the filter did not select')
  await findMatchBtn(V.page).click()
  await V.page.getByText('Looking for someone...').waitFor()
  assert((await V.page.locator('.error-text').count()) === 0, 'the backend rejected a verified search')
  await V.page.getByRole('button', { name: 'Cancel' }).click()
  await findMatchBtn(V.page).waitFor({ timeout: 3000 })
})

await run('BB-12', 'the gateway restarts mid-chat: the app shows it, reconnects by itself, and chat carries on', async () => {
  for (const u of [A, B]) await u.page.locator('.nav-item', { hasText: 'Random chat' }).click()
  await matchPair(A, B)
  await say(A.page, 'before the restart')
  await B.page.getByText('before the restart', { exact: true }).waitFor()
  sh('kubectl', ['rollout', 'restart', 'deploy/gateway'])
  sh('kubectl', ['rollout', 'status', 'deploy/gateway', '--timeout=120s'])
  for (const u of [A, B]) await u.page.waitForFunction(() => document.querySelector('.online')?.classList.contains('online--open'), null, { timeout: 90_000 })
  const after = await arrival(A.page, B.page, 'after the restart')
  assert(after <= FAST, `after a reconnect the random chat took ${show(after)} to deliver a message (should be under ${FAST / 1000}s)`)
})

await run('BB-13', 'phone layout: bottom navigation, no sideways scroll, back button in a conversation, composer in view', async () => {
  M = await newUser(browser, 'phone', { viewport: { width: 390, height: 844 }, deviceScaleFactor: 2, isMobile: true, hasTouch: true })
  const noSideScroll = (p) => p.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)
  await M.page.goto(WEB)
  assert(await noSideScroll(M.page), 'welcome scrolls sideways on a phone')
  await M.page.getByText("Nope, I'm new").click()
  await gotIt(M.page)
  await findMatchBtn(M.page).waitFor()
  assert(await M.page.locator('.bottom-nav').isVisible(), 'no bottom navigation')
  assert(!(await M.page.locator('.sidebar').isVisible()), 'the desktop sidebar shows on a phone')
  assert(await noSideScroll(M.page), 'the app scrolls sideways on a phone')
  await M.page.locator('.bottom-nav__item', { hasText: 'Channels' }).click()
  await M.page.locator('#new-channel').fill('phone room')
  await M.page.getByRole('button', { name: 'Create' }).click()
  await composer(M.page).waitFor()
  assert(await M.page.locator('.chat__back').isVisible(), 'no back button inside a conversation')
  const box = await composer(M.page).boundingBox()
  assert(box && box.y + box.height <= 844, 'the message box is below the screen')
  await M.page.locator('.chat__back').click()
  await M.page.locator('#new-channel').waitFor()
})

await run('BB-14', 'a banned person is removed from the app and cannot get back in (admin tool, local)', async () => {
  const me = await B.page.evaluate(async (g) => (await (await fetch(`${g}/v1/me`, { headers: { Authorization: `Bearer ${localStorage.getItem('flamingo_session_token')}` } })).json()), GATEWAY)
  assert(me?.id, 'could not read the account id')
  sh('go', ['run', './tools/admin', 'ban', `-account=${me.id}`, '-reason=black-box test', '-by=blackbox'], { env: { ...process.env, MODERATION_ADDR: 'localhost:50055' } })
  try {
    await B.page.getByText('You were removed').waitFor({ timeout: 30_000 })
    await B.page.reload()
    await B.page.getByText("Nope, I'm new").waitFor()
  } finally {
    sh('go', ['run', './tools/admin', 'unban', `-account=${me.id}`, '-by=blackbox'], { env: { ...process.env, MODERATION_ADDR: 'localhost:50055' } })
  }
})

await run('BB-20', 'a guest can be in 10 channels and no more; guessing invite codes is throttled after 10 tries a minute', async () => {
  const post = (u, path, body) =>
    u.page.evaluate(async ([g, path, body]) => {
      const res = await fetch(`${g}${path}`, { method: 'POST', headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${localStorage.getItem('flamingo_session_token')}` }, body: JSON.stringify(body) })
      const j = await res.json().catch(() => ({}))
      return { status: res.status, message: j.error?.message ?? '' }
    }, [GATEWAY, path, body])
  resetAllLimits()
  let made = 0
  let refused
  for (let i = 0; i < 14 && !refused; i++) {
    const r = await post(A, '/v1/rooms', { name: `cap test ${i}` })
    if (r.status === 200) made++
    else refused = r
  }
  // A is already in one channel from BB-08, so the cap of 10 stops the 10th creation.
  assert(refused?.status === 429 && /most allowed|too quickly/.test(refused.message), `expected a refusal after about 10 channels, got ${made} made and ${JSON.stringify(refused)}`)
  assert(made >= 8 && made <= 10, `a guest created ${made} channels before being stopped`)

  resetAllLimits()
  const statuses = []
  for (let i = 0; i < 13; i++) statuses.push((await post(V, '/v1/rooms/join', { invite_code: `NOSUCH${i}` })).status)
  assert(statuses.slice(0, 10).every((c) => c === 404), `the first ten wrong guesses should just be "not found": ${statuses}`)
  assert(statuses.slice(10).every((c) => c === 429), `guesses past ten a minute should be throttled: ${statuses}`)
  resetAllLimits()
})

await run('BB-15', 'the sixth guest signup from one address is refused with a clear message, and the page is not left stuck', async () => {
  resetGuestLimiter()
  let refusedAt = 0
  let last
  for (let i = 1; i <= 7 && !refusedAt; i++) {
    const u = await newUser(browser, `signup${i}`)
    await u.page.goto(WEB)
    await u.page.getByText("Nope, I'm new").click()
    const outcome = await Promise.race([findMatchBtn(u.page).waitFor().then(() => 'in'), u.page.locator('.error-text').waitFor().then(() => 'error')])
    // Every attempt counts toward the limit, including one that fails because
    // the random name was already taken (INC-0025); only "too many" is the limit.
    if (outcome === 'error' && /too many/i.test((await u.page.locator('.error-text').textContent()) ?? '')) {
      refusedAt = i
      last = u
    } else await u.ctx.close()
  }
  assert(refusedAt === 6, `expected the 6th signup to be refused, got ${refusedAt || 'none'}`)
  assert(/too many/i.test((await last.page.locator('.error-text').textContent()) ?? ''), 'the refusal message is not clear')
  assert(await last.page.getByText("Nope, I'm new").isEnabled(), 'the button stays stuck after a refusal')
  resetGuestLimiter()
})

await run('BB-21', 'a refresh in the middle of a random chat puts the person back in it, and the chat carries on both ways (DEC-0112)', async () => {
  resetGuestLimiter()
  const P = await newScratch(browser, 'refresher')
  const Q = await newScratch(browser, 'stayer')
  await enterAsGuest(P)
  await enterAsGuest(Q)
  NAME.P = await pseudonym(P.page)
  NAME.Q = await pseudonym(Q.page)
  await matchPair(P, Q)
  await say(P.page, 'hello before the refresh')
  await Q.page.getByText('hello before the refresh', { exact: true }).waitFor()

  await P.page.reload()
  // No "Find a match" to press: the chat is simply back, with what was said.
  await composer(P.page).waitFor({ timeout: 20_000 })
  await P.page.getByText('hello before the refresh', { exact: true }).waitFor()
  await P.page.locator('.chat__title', { hasText: NAME.Q }).waitFor()
  assert((await findMatchBtn(P.page).isDisabled()), 'the person is back in the chat but can still start a second search')
  const toQ = await arrival(P.page, Q.page, 'after the refresh, P to Q')
  const toP = await arrival(Q.page, P.page, 'after the refresh, Q to P')
  assert(toQ <= FAST && toP <= FAST, `after a refresh the chat took ${show(toQ)} and ${show(toP)} to deliver (should be under ${FAST / 1000}s)`)

  // The one who stayed never lost their chat, and Next still means something.
  await P.page.getByRole('button', { name: 'End', exact: true }).click()
  await findMatchBtn(P.page).waitFor()
  await Q.page.getByRole('heading', { name: 'Your partner left' }).waitFor()
  await P.ctx.close()
  await Q.ctx.close()
})

await run('BB-22', 'Next still works after the gateway restarts, and nothing internal is ever shown (DEC-0112)', async () => {
  resetGuestLimiter()
  const P = await newScratch(browser, 'next-after-restart')
  const Q = await newScratch(browser, 'partner')
  await enterAsGuest(P)
  await enterAsGuest(Q)
  await matchPair(P, Q)
  await say(P.page, 'before the gateway restarts')
  await Q.page.getByText('before the gateway restarts', { exact: true }).waitFor()

  sh('kubectl', ['rollout', 'restart', 'deploy/gateway'])
  sh('kubectl', ['rollout', 'status', 'deploy/gateway', '--timeout=120s'])
  for (const u of [P, Q]) await u.page.waitForFunction(() => document.querySelector('.online')?.classList.contains('online--open'), null, { timeout: 90_000 })

  // The new connection never saw the match being made. Next must still work.
  await P.page.getByRole('button', { name: 'Next', exact: true }).click()
  await P.page.getByText('Looking for someone...').waitFor({ timeout: 15_000 })
  await Q.page.getByRole('heading', { name: 'Your partner left' }).waitFor({ timeout: 15_000 })
  for (const u of [P, Q]) {
    const shown = (await u.page.locator('.error-text').allTextContents()).join(' ')
    assert(!/current_room_id|account_id|required|rpc error|internal/i.test(shown), `internal text on screen: ${shown}`)
  }
  await P.ctx.close()
  await Q.ctx.close()
})

await run('BB-23', 'a returning visitor lands straight in the app and never sees the welcome screen, even when the server is slow', async () => {
  resetGuestLimiter()
  const U = await newUser(browser, 'returning')
  await enterAsGuest(U)
  await U.ctx.addInitScript(watchForWelcome)
  const second = await U.ctx.newPage()
  second.on('pageerror', (e) => pageErrors.push(`returning: ${e}`))
  // The server takes four seconds to answer who this is. The app must not wait for it.
  let answeredAt = Infinity
  await second.route('**/v1/me', async (route) => {
    await new Promise((r) => setTimeout(r, 4000))
    answeredAt = Date.now()
    await route.continue()
  })
  await second.goto(WEB)
  await second.locator('.profile__name').waitFor({ timeout: 15_000 })
  const shownAt = Date.now()
  assert(!(await second.evaluate(() => window.__sawWelcome)), 'the welcome screen showed before the app')
  assert(shownAt < answeredAt, 'the app waited for the server before showing anything')
  await U.ctx.close()
})

await run('BB-24', 'a verified person sees the Verified check on a phone-sized screen, and a guest does not', async () => {
  assert(V, 'BB-10 did not leave a verified person to look at')
  await V.page.setViewportSize({ width: 390, height: 844 })
  await V.page.locator('.topbar__verified').waitFor({ timeout: 5000 })
  assert((await V.page.locator('.topbar__me').getAttribute('aria-label'))?.includes('verified'), 'the profile button does not say verified')
  await V.page.setViewportSize({ width: 1440, height: 900 })
  assert(M, 'BB-13 did not leave a guest on a phone')
  assert((await M.page.locator('.topbar__verified').count()) === 0, 'a guest was shown the Verified check')
})

await run('BB-25', 'a saved session with the server unreachable never shows the welcome screen; it says so, retries, and recovers', async () => {
  resetGuestLimiter()
  const U = await newScratch(browser, 'unreachable')
  await enterAsGuest(U)
  const name = await pseudonym(U.page)
  const token = await tokenOf(U.page)
  await U.page.evaluate(() => localStorage.removeItem('flamingo_account')) // a session saved before the account copy existed
  const second = await U.ctx.newPage()
  await second.addInitScript(watchForWelcome)
  let down = true
  await second.route('**/v1/me', (route) => (down ? route.abort('failed') : route.continue()))
  await second.goto(WEB)
  await second.getByText("Can't reach Flamingo right now").waitFor({ timeout: 10_000 })
  assert(!(await second.evaluate(() => window.__sawWelcome)), 'the welcome screen showed while the server was unreachable')
  assert((await second.getByText("Nope, I'm new").count()) === 0, 'it offered a new account, which would have replaced the saved one')
  assert((await tokenOf(second)) === token, 'the saved session was lost')
  down = false // the server is back
  await second.getByRole('button', { name: 'Try again' }).click()
  await second.locator('.profile__name').waitFor({ timeout: 10_000 })
  assert((await pseudonym(second)) === name, 'a different person came back')
  assert(!(await second.evaluate(() => window.__sawWelcome)), 'the welcome screen showed on the way back')
  await U.ctx.close()
})

await run('BB-26', 'a session the server refuses (401, 403 or 404) is ended with a clear message, and storage is cleared', async () => {
  resetGuestLimiter()
  const U = await newScratch(browser, 'refused')
  await enterAsGuest(U)
  const token = await tokenOf(U.page)
  const account = await accountOf(U.page)
  const cases = [
    ['expired (401)', gatewayError(401, 'unauthenticated', 'invalid session token'), /session has ended/i],
    ['account deleted (404)', gatewayError(404, 'not_found', 'account not found'), /session has ended/i],
    ['blocked (403)', gatewayError(403, 'permission_denied', 'account is banned'), /can't be used/i],
  ]
  for (const [label, reply, message] of cases) {
    await U.page.evaluate(([t, a]) => { localStorage.setItem('flamingo_session_token', t); localStorage.setItem('flamingo_account', a) }, [token, account])
    await U.page.unroute('**/v1/me').catch(() => {})
    await U.page.route('**/v1/me', (route) => route.fulfill(reply))
    await U.page.reload()
    await U.page.getByRole('heading', { name: 'Have you been here before?' }).waitFor({ timeout: 10_000 })
    await U.page.getByText(message).waitFor({ timeout: 5000 })
    assert((await tokenOf(U.page)) === null && (await accountOf(U.page)) === null, `${label}: the refused session was kept in storage`)
  }
  // And a server error is NOT a refusal.
  await U.page.evaluate(([t, a]) => { localStorage.setItem('flamingo_session_token', t); localStorage.setItem('flamingo_account', a) }, [token, account])
  await U.page.unroute('**/v1/me').catch(() => {})
  await U.page.route('**/v1/me', (route) => route.fulfill(gatewayError(503, 'unavailable', 'try later')))
  await U.page.reload()
  await U.page.locator('.profile__name').waitFor({ timeout: 10_000 })
  assert((await tokenOf(U.page)) === token, 'a 503 ended the session')
  await U.ctx.close()
})

await run('BB-27', 'a reload mid-chat never offers a new search while the chat is being restored, and the chat comes back', async () => {
  resetGuestLimiter()
  const P = await newScratch(browser, 'reload-mid-chat')
  const Q = await newScratch(browser, 'reload-partner')
  await enterAsGuest(P)
  await enterAsGuest(Q)
  await matchPair(P, Q)
  await say(P.page, 'before the reload')
  await Q.page.getByText('before the reload', { exact: true }).waitFor()
  await P.page.addInitScript(watchForOfferedSearch)
  // Slow the server's answer so the "restoring" moment is long enough to see.
  await P.page.route('**/v1/rooms', async (route) => { await new Promise((r) => setTimeout(r, 1200)); await route.continue() })
  for (let i = 0; i < 3; i++) await P.page.reload() // an impatient person pressing Ctrl+R
  await composer(P.page).waitFor({ timeout: 20_000 })
  await P.page.getByText('before the reload', { exact: true }).waitFor()
  assert(!(await P.page.evaluate(() => window.__sawOfferedSearch)), 'Find a match was offered to someone who was mid-chat')
  await P.ctx.close()
  await Q.ctx.close()
})

await run('BB-28', 'a match that reaches the screen before its acknowledgement is not wiped (INC-0030)', async () => {
  resetGuestLimiter()
  const A = await newScratch(browser, 'waits-first')
  const P = await newScratch(browser, 'completes-pairing')
  // P's connection delivers "queued" 500 ms late, so the match comes first: the order that stranded a person.
  await P.page.routeWebSocket(/\/v1\/ws/, (ws) => {
    const server = ws.connectToServer()
    ws.onMessage((m) => server.send(m))
    server.onMessage((m) => {
      if (JSON.parse(String(m)).type === 'match_queued') setTimeout(() => ws.send(m), 500)
      else ws.send(m)
    })
  })
  await enterAsGuest(A)
  await enterAsGuest(P)
  await findMatchBtn(A.page).click()
  await A.page.getByText('Looking for someone...').waitFor()
  await findMatchBtn(P.page).click() // P's request completes the pairing
  await composer(P.page).waitFor({ timeout: 15_000 })
  await P.page.waitForTimeout(1500) // long enough for the late "queued" to arrive
  assert((await composer(P.page).count()) === 1, 'the chat was erased by the late acknowledgement')
  assert((await P.page.getByText('Looking for someone...').count()) === 0, 'it still says searching while chatting')
  const t = await arrival(A.page, P.page, 'both sides are in the chat')
  assert(t <= FAST, `delivery took ${show(t)}`)
  await A.ctx.close()
  await P.ctx.close()
})

await run('BB-29', 'a match announcement that never arrives is found by asking the server', async () => {
  resetGuestLimiter()
  const A = await newScratch(browser, 'waits')
  const P = await newScratch(browser, 'never-told')
  await P.page.routeWebSocket(/\/v1\/ws/, (ws) => {
    const server = ws.connectToServer()
    ws.onMessage((m) => server.send(m))
    server.onMessage((m) => {
      if (JSON.parse(String(m)).type !== 'match_found') ws.send(m) // lost on the way
    })
  })
  await enterAsGuest(A)
  await enterAsGuest(P)
  await findMatchBtn(A.page).click()
  await A.page.getByText('Looking for someone...').waitFor()
  await findMatchBtn(P.page).click()
  await composer(P.page).waitFor({ timeout: 15_000 }) // found by the periodic check, not by the lost message
  await A.ctx.close()
  await P.ctx.close()
})

await run('BB-30', 'signing out in one tab signs out the other tab of the same browser', async () => {
  resetGuestLimiter()
  const U = await newScratch(browser, 'two-tabs')
  await enterAsGuest(U)
  const second = await U.ctx.newPage()
  await second.goto(WEB)
  await second.locator('.profile__name').waitFor()
  await U.page.getByRole('button', { name: 'Sign out' }).first().click()
  await U.page.getByRole('dialog').getByRole('button', { name: 'Sign out' }).click()
  await U.page.getByRole('heading', { name: 'Have you been here before?' }).waitFor()
  await second.getByRole('heading', { name: 'Have you been here before?' }).waitFor({ timeout: 5000 })
  assert((await tokenOf(second)) === null, 'the second tab brought the signed-out session back')
  await U.ctx.close()
})

await run('BB-31', 'a dropped connection keeps the typed message, says why it cannot be sent, and lets it go once back', async () => {
  resetGuestLimiter()
  const P = await newScratch(browser, 'drops')
  const Q = await newScratch(browser, 'drops-partner')
  let current = null
  let hold = false
  await P.page.routeWebSocket(/\/v1\/ws/, (ws) => {
    if (hold) return void ws.close()
    current = ws
    const server = ws.connectToServer()
    ws.onMessage((m) => server.send(m))
    server.onMessage((m) => ws.send(m))
  })
  await enterAsGuest(P)
  await enterAsGuest(Q)
  await matchPair(P, Q)
  hold = true
  current.close() // the connection drops and cannot come back for now
  await P.page.locator('.chat__offline').waitFor({ timeout: 10_000 })
  await composer(P.page).fill('typed while offline')
  await P.page.keyboard.press('Enter')
  assert((await composer(P.page).inputValue()) === 'typed while offline', 'the message was thrown away while offline')
  assert(await P.page.getByRole('button', { name: 'Send' }).isDisabled(), 'Send was enabled with no connection')
  assert(await P.page.getByRole('button', { name: 'Next', exact: true }).isDisabled(), 'Next was enabled with no connection')
  hold = false
  await P.page.locator('.chat__offline').waitFor({ state: 'detached', timeout: 40_000 })
  await P.page.keyboard.press('Enter')
  await Q.page.getByText('typed while offline', { exact: true }).waitFor({ timeout: 10_000 })
  await P.ctx.close()
  await Q.ctx.close()
})

await run('BB-32', 'a session that dies while the app is open is noticed and ended, not retried in silence', async () => {
  resetGuestLimiter()
  const U = await newScratch(browser, 'dies-open')
  let live = null
  let dead = false
  await U.page.routeWebSocket(/\/v1\/ws/, (ws) => {
    if (dead) return void ws.close() // the gateway closes a dead session's login without a word
    live = ws
    const server = ws.connectToServer()
    ws.onMessage((m) => server.send(m))
    server.onMessage((m) => ws.send(m))
  })
  await enterAsGuest(U)
  // The account is cleaned up on the server while this tab is open.
  await U.page.route('**/v1/me', (route) => route.fulfill(gatewayError(404, 'not_found', 'account not found')))
  dead = true
  live.close() // the connection drops and every login after it is closed
  // Three failed logins later the app asks the server, hears "no such account", and ends the session.
  await U.page.getByRole('heading', { name: 'Have you been here before?' }).waitFor({ timeout: 40_000 })
  await U.page.getByText(/session has ended/i).waitFor({ timeout: 5000 })
  assert((await tokenOf(U.page)) === null, 'the dead session was kept in storage')
  await U.ctx.close()
})

await run('BB-33', 'someone who closes the tab while searching leaves no ghost: the next two people are paired with each other (INC-0030)', async () => {
  resetGuestLimiter()
  const gone = await newScratch(browser, 'closes-tab')
  await enterAsGuest(gone)
  await findMatchBtn(gone.page).click()
  await gone.page.getByText('Looking for someone...').waitFor()
  await gone.ctx.close() // the tab closes with the search still open
  await sleep(6000) // longer than the gateway's grace period locally (3 s)
  const A = await newScratch(browser, 'pair-a')
  const B = await newScratch(browser, 'pair-b')
  await enterAsGuest(A)
  await enterAsGuest(B)
  NAME.A33 = await pseudonym(A.page)
  NAME.B33 = await pseudonym(B.page)
  await matchPair(A, B)
  await A.page.locator('.chat__title', { hasText: NAME.B33 }).waitFor({ timeout: 5000 })
  await B.page.locator('.chat__title', { hasText: NAME.A33 }).waitFor({ timeout: 5000 })
  const t = await arrival(A.page, B.page, 'paired with each other, not with a ghost')
  assert(t <= FAST, `delivery took ${show(t)}`)
})

await run('BB-34', 'a refresh while searching keeps the search going: the person is still found', async () => {
  resetGuestLimiter()
  const A = await newScratch(browser, 'refreshes-while-searching')
  const B = await newScratch(browser, 'finds-them')
  await enterAsGuest(A)
  await enterAsGuest(B)
  await findMatchBtn(A.page).click()
  await A.page.getByText('Looking for someone...').waitFor()
  await A.page.reload() // back within the grace period
  await sleep(4000) // longer than the grace, so a lost search would have been cancelled by now
  await findMatchBtn(B.page).click()
  await composer(B.page).waitFor({ timeout: 15_000 })
  await composer(A.page).waitFor({ timeout: 15_000 }) // A was found, even though the page was refreshed while waiting
})

await run('BB-16', 'no uncaught errors in any page during the whole run', async () => {
  assert(pageErrors.length === 0, `uncaught page errors: ${pageErrors.slice(0, 3).join(' | ')}`)
})

await browser.close()

const failed = results.filter((r) => r.status === 'FAIL')
const unexpectedConsole = consoleErrors.filter((c) => !/Failed to load resource|WebSocket connection|net::ERR/.test(c))
console.log(`\n${results.length - failed.length}/${results.length} passed. Console errors: ${consoleErrors.length} (${unexpectedConsole.length} not explained by the deliberate 4xx and restarts in BB-12, 14 and 15).`)
if (unexpectedConsole.length) console.log(unexpectedConsole.slice(0, 5).join('\n'))
writeFileSync(`${OUT}results.json`, JSON.stringify({ ranAt: new Date().toISOString(), results, consoleErrors }, null, 2))
process.exit(failed.length ? 1 : 0)
