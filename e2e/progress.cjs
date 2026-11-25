// Cooking progress in a real browser (docs/COOKING_PROGRESS_DESIGN.md): an
// order moves from "not started" to "cooking" to "ready" as the kitchen taps,
// the desk rushes a dish, reads the dishes by station and today's finished
// orders, and the order page links to the progress of an order.
// Usage: BASE_URL=http://localhost:8080 node e2e/progress.cjs   (needs seed data)
const { chromium } = require('playwright')
const { customerLogin } = require('./lib.cjs')
const { mkdirSync } = require('node:fs')

;(async () => {

const BASE = process.env.BASE_URL || 'http://localhost:8080'
const SHOTS = process.env.SHOTS_DIR || 'e2e/shots'
mkdirSync(SHOTS, { recursive: true })
const browser = await chromium.launch()
const errors = []

async function open(viewport) {
  const ctx = await browser.newContext({ viewport, locale: 'zh-CN', deviceScaleFactor: Number(process.env.SHOT_SCALE || 1), ignoreHTTPSErrors: process.env.E2E_INSECURE_TLS === '1' })
  const page = await ctx.newPage()
  page.on('pageerror', e => errors.push(`pageerror: ${e.message}`))
  page.on('console', m => { if (m.type() === 'error' && !/favicon|Failed to load resource/.test(m.text())) errors.push(`console: ${m.text()}`) })
  return page
}
async function login(page, user) {
  await page.goto(BASE + (/^kitchen\d/.test(user) ? '/login?next=/work' : '/login'))
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

// ---- a customer orders 2 fries (fryer) and a braised pork rice (hot kitchen) for pickup ----
const cust = await open({ width: 390, height: 844 })
await cust.goto(BASE + '/s/makati')
await cust.waitForSelector('.item')
await cust.click('.item:has-text("无盐地瓜条") .stepper button.fill')
await cust.click('.item:has-text("无盐地瓜条") .stepper button.fill')
await cust.click('.item:has-text("卤肉饭") .stepper button.fill')
await cust.click('.bottombar .btn.primary')
await customerLogin(cust, null, { name: '陈先生' })
await cust.locator('.bottombar .btn.primary').click()
await cust.waitForURL('**/o/**')
const orderNo = (await cust.locator('[data-testid=order-no]').innerText()).trim()
const desk = await open({ width: 1440, height: 900 })
await login(desk, 'cashier1')
await desk.click(`.ord:has-text("${orderNo}")`)
await desk.click('.detail button:has-text("接单")')
await desk.waitForSelector('.detail select')
step(`order ${orderNo} placed and accepted`)

// ---- the desk opens cooking progress: the order has not started ----
await desk.click('[data-testid=nav-orders-fold]')   // the pages under 订单 start folded
await desk.click('[data-testid=nav-progress]')
await desk.waitForURL('**/merchant/progress')
const card = desk.locator(`.cp-card[data-order="${orderNo}"]`)
await desk.locator(`[data-testid=cp-col-wait] .cp-card[data-order="${orderNo}"]`).waitFor()
expect((await card.locator('[data-testid=cp-count]').innerText()).startsWith('0/3 份做好'), true, 'nothing ready yet')
step('the new order waits under "not started"')

// ---- the kitchen starts the fries: it moves to "cooking" ----
const kitchen = await open({ width: 1280, height: 800 })
await login(kitchen, 'kitchen1')
await kitchen.waitForURL(u => !/\/login$/.test(u.pathname))
const board = (await api(kitchen, 'GET', '/kitchen/branches/1/board')).body
expect(typeof board.target_minutes, 'number', 'the kitchen screen gets the cooking target')
const ko = board.orders.find(o => o.public_no === orderNo)
const fries = ko.items.find(i => i.name === '无盐地瓜条'), rice = ko.items.find(i => i.name === '卤肉饭')
expect((await api(kitchen, 'POST', `/kitchen/items/${fries.id}/start?via=board`)).status, 200, 'start the fries')
await desk.locator(`[data-testid=cp-col-cook] .cp-card[data-order="${orderNo}"]`).waitFor()
await card.locator('.cp-dl:has-text("无盐地瓜条") .m.p').waitFor()
step('started on the kitchen screen, the order moves to "cooking"')

// ---- the desk opens the order and rushes the rice ----
await card.click()
const drawer = desk.locator('[data-testid=cp-drawer]')
await drawer.waitFor()
await drawer.locator('.cp-tl:has-text("开始")').first().waitFor()
await drawer.locator('[data-testid=cp-dline]:has-text("卤肉饭") [data-testid=cp-rush]').click()
await desk.locator('[data-testid=cp-flash]').waitFor()
await drawer.locator('[data-testid=cp-dline]:has-text("卤肉饭") .cp-rush').waitFor()
const after = (await api(kitchen, 'GET', '/kitchen/branches/1/board')).body.orders.find(o => o.public_no === orderNo)
expect(!!after.items.find(i => i.id === rice.id).rush_at, true, 'the kitchen screen sees the rush')
await shot(desk, 'p03-drawer')
await drawer.locator('.cp-x').click()
step('the desk rushed the rice; the kitchen screen marks it')

// ---- by dish ----
await desk.click('[data-testid=cp-tab-dishes]')
await desk.locator('[data-testid=cp-dish-row]:has-text("无盐地瓜条")').first().waitFor()
expect(await desk.locator('[data-testid=cp-dish-group]').count() >= 2, true, 'grouped by station')
await shot(desk, 'p02-dishes')
await desk.click('[data-testid=cp-tab-orders]')
await shot(desk, 'p01-board')
step('dishes are counted by station')

// ---- everything ready: "ready", and today's finished list has it ----
for (const id of [fries.id, rice.id]) expect((await api(kitchen, 'POST', `/kitchen/items/${id}/ready?via=board`)).status, 200, 'ready')
await desk.locator(`[data-testid=cp-col-done] .cp-card[data-order="${orderNo}"]`).waitFor()
await card.locator('.cp-out').waitFor()
await desk.click('[data-testid=cp-tab-done]')
await desk.locator(`[data-testid=cp-done-row]:has-text("${orderNo}")`).waitFor()
await shot(desk, 'p04-done')
step('all ready: under "ready" and in today\'s finished orders')

// ---- the order page links to the progress of an order ----
await desk.click('.staffbar a[href="/merchant/orders"]')
const tag = desk.locator(`.ord:has-text("${orderNo}") [data-testid=cp-order-tag]`)
await tag.waitFor()
expect((await tag.innerText()).startsWith('3/3 份做好'), true, 'the order card counts what is ready')
await desk.click(`.ord:has-text("${orderNo}")`)
await desk.click('[data-testid=cp-order-link]')
await desk.waitForURL('**/merchant/progress?order=*')
await drawer.waitFor()
step('the order card shows 3/3; the order opens on the progress page')

// ---- the customer does not learn who cooked ----
const mine = await cust.evaluate(async () => {
  const token = location.pathname.split('/').pop()
  return (await fetch('/api/v1/customer/orders/' + token, { headers: { 'X-Requested-With': 'e2e' } })).json()
})
expect(mine.items.every(i => !i.started_by && !i.ready_by), true, 'no cook names for the customer')

// ---- the owner sets the target; a phone shows one column at a time ----
const owner = await open({ width: 1340, height: 900 })
await login(owner, 'owner1')
await owner.click('.staffbar a:has-text("设置")')
await owner.fill('[data-testid=cook-target]', '20')
const saved = owner.waitForResponse(r => r.url().endsWith('/settings') && r.request().method() === 'PUT')
await owner.locator('[data-testid=cook-target]').press('Enter')
expect((await saved).status(), 200, 'save the cooking target')
const prog = (await api(owner, 'GET', '/merchant/branches/1/progress')).body
expect(prog.target_minutes, 20, 'the target is saved')
step('the owner sets the cooking target to 20 minutes')

const phone = await open({ width: 390, height: 844 })
await login(phone, 'cashier1')
await phone.goto(BASE + '/merchant/progress')
await phone.locator('.cp-lanes').waitFor()
await phone.locator('.cp-lanes button').nth(2).click()
await phone.locator(`.cp-col.shown .cp-card[data-order="${orderNo}"]`).waitFor()
await shot(phone, 'p05-phone')
step('a phone shows one column at a time')

if (errors.length) throw new Error(errors.join('\n'))
await browser.close()
console.log('progress e2e passed')
})().catch(e => { console.error(e); process.exit(1) })
