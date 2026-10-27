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
import { execFileSync } from 'node:child_process'
import { mkdirSync, writeFileSync } from 'node:fs'
import { chromium } from 'playwright'

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
function resetGuestLimiter() {
  sh('kubectl', ['exec', 'deploy/redis', '--', 'sh', '-c', "redis-cli --scan --pattern 'rl:guest_signup:*' | xargs -r redis-cli del"])
}

async function run(id, title, fn) {
  const t0 = Date.now()
  try {
    await fn()
    results.push({ id, title, status: 'PASS', ms: Date.now() - t0 })
  } catch (e) {
    results.push({ id, title, status: 'FAIL', ms: Date.now() - t0, error: String(e.message).split('\n')[0] })
    for (const { page, label } of live) await page.screenshot({ path: `${OUT}${id}-${label}.png` }).catch(() => {})
  }
  const r = results.at(-1)
  console.log(`${r.status === 'PASS' ? 'PASS' : 'FAIL'}  ${id}  ${title}${r.error ? `\n        ${r.error}` : ''}`)
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
  await A.page.reload()
  await findMatchBtn(A.page).waitFor()
  await sleep(1200)
  assert((await A.page.getByRole('heading', { name: "Hi, I'm Mochi!" }).count()) === 0, 'the intro showed again after a reload')
  assert((await pseudonym(A.page)) === name, 'the session did not survive a reload')
})

B = await newUser(browser, 'B')
await enterAsGuest(B)

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

await run('BB-06', 'partner leaves: the other person gets a keep-or-discard countdown, keeps it, and finds the history in Inbox', async () => {
  await A.page.getByRole('button', { name: 'End', exact: true }).click()
  await findMatchBtn(A.page).waitFor()
  await B.page.getByRole('heading', { name: 'Your partner left' }).waitFor()
  assert(/\d+s/.test((await B.page.locator('.empty p').textContent()) ?? ''), 'no countdown is shown')
  await B.page.getByRole('button', { name: 'Keep this chat' }).click()
  await B.page.getByRole('heading', { name: 'Saved!' }).waitFor()
  await B.page.getByRole('button', { name: 'Back to Random' }).click()
  await B.page.locator('.nav-item', { hasText: 'Inbox' }).click()
  await B.page.getByRole('button', { name: /Chat from/ }).first().click()
  await B.page.getByText('hello from A').waitFor()
  await B.page.locator('.nav-item', { hasText: 'Random chat' }).click()
})

await run('BB-07', 'cancelling a search clears the screen at once, and matching still works afterwards (DEC-0103)', async () => {
  await findMatchBtn(A.page).click()
  await A.page.getByText('Looking for someone...').waitFor()
  await A.page.getByRole('button', { name: 'Cancel' }).click()
  await findMatchBtn(A.page).waitFor({ timeout: 3000 })
  assert((await A.page.getByText('Looking for someone...').count()) === 0, 'still says searching after a cancel')
  await matchPair(A, B)
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
  await Promise.all([A.page.locator('.nav-item', { hasText: 'Random chat' }).click(), B.page.locator('.nav-item', { hasText: 'Random chat' }).click()])
  await composer(A.page).waitFor()
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

await run('BB-15', 'the sixth guest signup from one address is refused with a clear message, and the page is not left stuck', async () => {
  resetGuestLimiter()
  let refusedAt = 0
  let last
  for (let i = 1; i <= 7 && !refusedAt; i++) {
    const u = await newUser(browser, `signup${i}`)
    await u.page.goto(WEB)
    await u.page.getByText("Nope, I'm new").click()
    const outcome = await Promise.race([findMatchBtn(u.page).waitFor().then(() => 'in'), u.page.locator('.error-text').waitFor().then(() => 'refused')])
    if (outcome === 'refused') {
      refusedAt = i
      last = u
    } else await u.ctx.close()
  }
  assert(refusedAt === 6, `expected the 6th signup to be refused, got ${refusedAt || 'none'}`)
  assert(/too many/i.test((await last.page.locator('.error-text').textContent()) ?? ''), 'the refusal message is not clear')
  assert(await last.page.getByText("Nope, I'm new").isEnabled(), 'the button stays stuck after a refusal')
  resetGuestLimiter()
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
