// The queue display in a real browser (docs/QUEUE_DISPLAY_DESIGN.md): the
// owner turns both modes on and pairs two screens; a guest takes a number by
// QR, the desk gives a paper number, calls the suggested one to a free table
// (the guest's phone and the screen show it), seats it, moves a number up,
// misses and puts back another; the tea screen shows pickup numbers moving
// from "in line" to "ready" and the desk hands one over.
// Usage: BASE_URL=http://localhost:8080 node e2e/queue.cjs   (needs seed data)
const { chromium } = require('playwright')
const { customerLogin } = require('./lib.cjs')
const { mkdirSync } = require('node:fs')

;(async () => {

const BASE = process.env.BASE_URL || 'http://localhost:8080'
const SHOTS = process.env.SHOTS_DIR || 'e2e/shots'
mkdirSync(SHOTS, { recursive: true })
const browser = await chromium.launch()
const errors = []

async function open(viewport, locale = 'zh-CN') {
  const ctx = await browser.newContext({ viewport, locale, deviceScaleFactor: Number(process.env.SHOT_SCALE || 1), ignoreHTTPSErrors: process.env.E2E_INSECURE_TLS === '1' })
  const page = await ctx.newPage()
  page.on('pageerror', e => errors.push(`pageerror: ${e.message}`))
  page.on('console', m => { if (m.type() === 'error' && !/favicon|Failed to load resource|401|navigator.vibrate/.test(m.text())) errors.push(`console: ${m.text()}`) })
  page.on('dialog', d => d.accept())
  return page
}
async function login(page, user) {
  await page.goto(BASE + '/login')
  for (let i = 0; ; i++) {
    await page.fill('input[autocomplete=username]', user)
    await page.fill('input[type=password]', 'demo1234')
    const res = page.waitForResponse(r => r.url().endsWith('/auth/login'))
    await page.click('button.primary')
    if ((await res).status() !== 429 || i >= 5) return
    await page.waitForTimeout(7000)
  }
}
const api = (page, method, path, body) => page.evaluate(async ([method, path, body]) => {
  const r = await fetch('/api/v1' + path, { method, headers: { 'X-Requested-With': 'e2e', 'Content-Type': 'application/json' },
    body: body ? JSON.stringify(body) : undefined })
  return { status: r.status, body: await r.json().catch(() => null) }
}, [method, path, body])
const step = (s) => console.log('✓ ' + s)
const shot = (page, name) => page.screenshot({ path: `${SHOTS}/${name}.png` })
const expect = (got, want, what) => { if (got !== want) throw new Error(`${what}: want ${want}, got ${got}`) }

// ---- the owner turns the queue on ----
const desk = await open({ width: 1440, height: 900 })
await login(desk, 'owner1')
await desk.waitForURL(u => !/\/login/.test(u.pathname))
await api(desk, 'PUT', '/merchant/branches/1/queue/settings', { tea: false, restaurant: false, upgrade_minutes: 15, call_alert_minutes: 5, requeue_after: 3,
  dine_minutes: 45, ready_stay_minutes: 15, show_delivery: false, show_dine_in: true, sms: false, media: true, paused: false })
await desk.goto(BASE + '/merchant/queue')
await desk.locator('[data-testid=nav-queue]').waitFor()
await desk.locator('[data-testid=qv-tab-settings].on').waitFor()
await desk.check('[data-testid=qx-rest]')
await desk.check('[data-testid=qx-tea]')
await desk.click('[data-testid=qx-save]')
await desk.locator('[data-testid=qx-saved]').waitFor()
await desk.locator('[data-testid=qv-tab-line]').waitFor()
expect(await desk.locator('[data-testid=qx-group]').count() >= 3, true, 'default table sizes')
step('both modes on; the default table sizes are there')

// ---- two screens: the door (queue) and the counter (pickup, portrait) ----
async function addScreen(name, mode, orientation) {
  await desk.click('[data-testid=qx-add-screen]')
  await desk.fill('[data-testid=qx-sname]', name)
  const f = desk.locator('.addf')
  await f.locator('select').nth(0).selectOption(mode)
  await f.locator('select').nth(1).selectOption(orientation)
  await f.locator('select').nth(3).selectOption('en')
  await desk.click('[data-testid=qx-create-screen]')
  const code = desk.locator(`[data-testid=qx-screen]:has(input[value="${name}"]) [data-testid=qx-code]`)
  await desk.locator('[data-testid=qx-code]').first().waitFor()
  return (await desk.locator('[data-testid=qx-code]').last().innerText()).match(/\d{6}/)[0]
}
const doorCode = await addScreen('门口电视', 'restaurant', 'landscape')
await shot(desk, 'q06-settings')
const tv = await open({ width: 1920, height: 1080 })
await tv.goto(BASE + '/screen')
await tv.fill('[data-testid=qs-code]', '123456')
await tv.click('[data-testid=qs-pair]')
await tv.locator('[data-testid=qs-error]').waitFor()
await tv.fill('[data-testid=qs-code]', doorCode)
await tv.click('[data-testid=qs-pair]')
await tv.locator('[data-testid=qs-calling]').waitFor()
await tv.locator('[data-testid=qs-group-A]').waitFor()
step('the door TV paired with its code (a wrong code is refused)')

// ---- a guest takes a number by QR ----
const phone = await open({ width: 390, height: 844 })
await phone.goto(BASE + '/q/makati')
await phone.locator('[data-testid=qg-take]').waitFor()
await phone.click('[data-testid=qg-p2]')
await phone.check('[data-testid=qg-agree]')
await shot(phone, 'q05-take')
await phone.click('[data-testid=qg-take]')
await phone.waitForURL('**/q/t/**')
const guestNo = (await phone.locator('[data-testid=qt-number]').innerText()).trim()
expect(/^A\d{3}$/.test(guestNo), true, 'the guest\'s number is in A')
expect((await phone.locator('[data-testid=qt-ahead]').innerText()).trim(), '0', 'nobody ahead')
// the same browser cannot take a second number: it goes back to its own
await phone.goto(BASE + '/q/makati')
await phone.waitForURL('**/q/t/**')
step(`the guest took ${guestNo} by QR; coming back shows the same number`)

// ---- the desk gives paper numbers ----
await desk.click('[data-testid=qv-tab-line]')
async function give(n, print = false) {
  await desk.click('[data-testid=qv-take]')
  await desk.click(`[data-testid=qt-p${n}]`)
  await desk.locator('[data-testid=qt-group]').waitFor()
  await desk.click(print ? '[data-testid=qt-ok]' : '[data-testid=qt-ok-np]')
  await desk.locator('[data-testid=qt-taken]').waitFor()
  const no = (await desk.locator('[data-testid=qt-taken] .bign').innerText()).trim()
  await desk.click('[data-testid=qt-close]')
  return no
}
const b1 = await give(4)
expect(b1[0], 'B', 'a party of 4 goes to B')
const a2 = await give(1)
const a3 = await give(2)
expect(Number(a3.slice(1)), Number(a2.slice(1)) + 1, 'numbers run on per group')
await desk.locator(`[data-testid=ql-row][data-no="${a3}"]`).waitFor()
await shot(desk, 'q03-desk')
step(`the desk gave ${b1}, ${a2}, ${a3}`)

// ---- move A003 up with a reason ----
await desk.click(`[data-testid=ql-row][data-no="${a3}"] [data-testid=ql-more]`)
await desk.click(`[data-testid=ql-row][data-no="${a3}"] [data-testid=ql-jump]`)
await desk.click('[data-testid=qj-r-elder]')
await desk.click('[data-testid=qj-ok]')
await desk.locator('[data-testid=ql-flash]').waitFor()
const order = await desk.locator('[data-testid=ql-row][data-no^="A"]').evaluateAll(rs => rs.map(r => r.dataset.no))
expect(order[0], a3, 'the number moved to the front')
step('A003 moved to the front (elderly)')

// ---- the free table 08 suggests the head of A; call it there ----
const free08 = desk.locator('[data-testid=ql-free][data-table="08"]')
await free08.locator('[data-testid=ql-sug]').waitFor()
expect((await free08.locator('[data-testid=ql-sug] .no').innerText()).trim(), a3, 'table 08 suggests the head of A')
await free08.locator('[data-testid=ql-call-to]').click()
await desk.locator(`[data-testid=ql-row][data-no="${a3}"].called`).waitFor()
await tv.locator(`[data-testid=qs-callno]:has-text("${a3}")`).waitFor()
await shot(tv, 'q02-tv-restaurant')
step(`called ${a3} to table 08: the TV shows it`)

// ---- call the guest's number to the desk: their phone turns red ----
await desk.click(`[data-testid=ql-row][data-no="${guestNo}"] [data-testid=ql-call]`)
await phone.locator('[data-testid=qt-called]').waitFor()
expect((await phone.locator('[data-testid=qt-place]').innerText()).includes('前台'), true, 'called to the desk')
await shot(phone, 'q05-called')
step(`${guestNo} called: the guest's phone shows "到你了"`)

// ---- seat A003 at the held table ----
const seatedBefore = Number((await desk.locator('[data-testid=ql-seated]').innerText()).trim())
await desk.click(`[data-testid=ql-row][data-no="${a3}"] [data-testid=ql-seat]`)
await desk.locator('[data-testid=qs-table][data-table="08"].rec').click()
await desk.locator('[data-testid=qs-done]').waitFor()
await shot(desk, 'q04-seated')
await desk.click('[data-testid=qs-sheet] .sheet-foot .btn:not(.primary)')
await desk.locator(`[data-testid=ql-row][data-no="${a3}"]`).waitFor({ state: 'detached' })
await desk.locator(`[data-testid=ql-seated]:text-is("${seatedBefore + 1}")`).waitFor()
step(`${a3} seated at 08`)

// ---- the guest misses the call, then comes back ----
await desk.click(`[data-testid=ql-row][data-no="${guestNo}"] [data-testid=ql-more]`)
await desk.click(`[data-testid=ql-row][data-no="${guestNo}"] [data-testid=ql-skip]`)
await phone.locator('[data-testid=qt-skipped]').waitFor()
await tv.locator(`[data-testid=qs-missed]:has-text("${guestNo}")`).waitFor()
await desk.click('[data-testid=ql-g-skipped]')
await desk.click(`[data-testid=ql-row][data-no="${guestNo}"] [data-testid=ql-requeue]`)
await phone.locator('[data-testid=qt-waiting]').waitFor()
await desk.click('.ox-cat:first-child')
step(`${guestNo} missed and put back in line`)

// ---- the log ----
await desk.click('[data-testid=qv-tab-log]')
await desk.locator('[data-testid=ql-log]').first().waitFor()
expect(await desk.locator('[data-testid=ql-log]:has-text("插队")').count(), 1, 'the move up is logged')
step('the log shows every step')

// ---- the guest leaves the line ----
await phone.click('[data-testid=qt-cancel]')
await phone.locator('[data-testid=qt-closed]').waitFor()
step('the guest left the line from their phone')

// ---- the tea screen: pickup orders move to "ready" ----
await desk.click('[data-testid=qv-tab-settings]')
const teaCode = await addScreen('取餐台', 'tea', 'portrait')
const pad = await open({ width: 1080 / 2, height: 1920 / 2 })
await pad.goto(BASE + '/screen')
await pad.fill('[data-testid=qs-code]', teaCode)
await pad.click('[data-testid=qs-pair]')
await pad.locator('[data-testid=qs-ready]').waitFor()

const cust = await open({ width: 390, height: 844 })
const orders = []
for (let i = 0; i < 3; i++) {
  await cust.goto(BASE + '/s/makati')
  await cust.waitForSelector('.item')
  await cust.click('.item:has-text("无盐地瓜条") .stepper button.fill')
  await cust.click('.bottombar .btn.primary')
  await customerLogin(cust, null, { pickup: true })
  await cust.fill('input[autocomplete=name]', '林先生')
  await cust.locator('.bottombar .btn.primary').click()
  await cust.waitForURL('**/o/**')
  orders.push((await cust.locator('[data-testid=order-no]').innerText()).trim())
}
const active = (await api(desk, 'GET', '/merchant/branches/1/orders?scope=active')).body.orders
const mine = []
for (const no of orders) {
  const o = active.find(x => x.public_no === no)
  const r = await api(desk, 'POST', `/merchant/orders/${o.id}/accept`, { expected_version: o.version })
  expect(r.status, 200, 'accept')
  mine.push(r.body)
}
const prep = o => o.items.filter(i => i.needs_prep)
for (const it of prep(mine[0])) expect((await api(desk, 'POST', `/kitchen/items/${it.id}/ready`, {})).status, 200, 'ready')
for (const it of prep(mine[1])) expect((await api(desk, 'POST', `/kitchen/items/${it.id}/start`, {})).status, 200, 'start')
const code0 = mine[0].pickup_code
await pad.locator(`[data-testid=qs-hero]:text-is("${code0}")`).waitFor()
await pad.locator('[data-testid=qs-cook] .nums span').first().waitFor()
await pad.locator('[data-testid=qs-wait] .nums span').first().waitFor()
await pad.waitForTimeout(500)
await shot(pad, 'q07-tea-portrait')
step('the tea screen shows in line, preparing and ready')

await desk.click('[data-testid=qv-tab-pickup]')
const row = desk.locator(`[data-testid=qp-ready][data-code="${code0}"]`)
await row.waitFor()
await row.locator('[data-testid=qp-recall]').click()
await shot(desk, 'q07-pickup-desk')
await row.locator('[data-testid=qp-done]').click()
await row.waitFor({ state: 'detached' })
await pad.locator(`[data-testid=qs-hero]:text-is("${code0}")`).waitFor({ state: 'detached' })
const done = (await api(desk, 'GET', `/merchant/orders/${mine[0].id}`)).body
expect((done.order ?? done).fulfillment_status, 'picked_up', 'handed over')
step('the desk called again and handed it over; it left the screen')

// a landscape tea screen for the pictures
await api(desk, 'PUT', `/merchant/branches/1/queue/screens/${(await api(desk, 'GET', '/merchant/branches/1/queue/screens')).body.screens.find(s => s.mode === 'tea').id}`,
  { name: '取餐台', mode: 'tea', orientation: 'landscape', lang: 'zh', lang2: 'en' })
for (const it of prep(mine[1])) await api(desk, 'POST', `/kitchen/items/${it.id}/ready`, {})
const wide = await pad.context().newPage()
await wide.setViewportSize({ width: 1920, height: 1080 })
await wide.goto(BASE + '/screen')
await wide.locator('[data-testid=qs-hero]').waitFor()
await shot(wide, 'q01-tv-tea')
step('landscape tea screen with a second language')

if (errors.length) { console.error(errors.join('\n')); process.exitCode = 1 }
await browser.close()
})().catch(e => { console.error(e); process.exit(1) })
