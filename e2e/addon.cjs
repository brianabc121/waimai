// Adding dishes to a delivery order before the rider takes it
// (docs/ADD_ON_ORDER_DESIGN.md): the customer orders; the desk accepts and
// adds a dish for them from the order desk; the kitchen sees "add-on 1"; the
// customer adds a dish of their own, which waits for the store; the desk
// accepts it; the add-on ticket prints that batch only.
// Usage: BASE_URL=http://localhost:8080 node e2e/addon.cjs   (needs seed data and OTP_DEV_ECHO=true)
const { chromium } = require('playwright')
const { customerLogin } = require('./lib.cjs')
const { mkdirSync } = require('node:fs')

;(async () => {

const BASE = process.env.BASE_URL || 'http://localhost:8080'
const SHOTS = process.env.SHOTS_DIR || 'e2e/shots'
mkdirSync(SHOTS, { recursive: true })
const browser = await chromium.launch()
const errors = []
const step = (s) => console.log('✓ ' + s)
const tid = (id) => `[data-testid=${id}]`
const expect = (ok, what) => { if (!ok) throw new Error(what) }

async function open(viewport, extra = {}) {
  const ctx = await browser.newContext({ viewport, locale: 'zh-CN', deviceScaleFactor: Number(process.env.DPR || 1),
    permissions: ['clipboard-read', 'clipboard-write'], ignoreHTTPSErrors: process.env.E2E_INSECURE_TLS === '1', ...extra })
  const page = await ctx.newPage()
  page.on('pageerror', e => errors.push(`pageerror: ${e.message}`))
  page.on('console', m => { if (m.type() === 'error' && !/favicon|Failed to load resource/.test(m.text())) errors.push(`console: ${m.text()}`) })
  page.on('dialog', d => d.accept('e2e'))
  return page
}
async function login(page, user, next = '') {
  await page.goto(BASE + '/login' + next)
  await page.fill('input[autocomplete=username]', user)
  await page.fill('input[type=password]', 'demo1234')
  await page.click('button.primary')
  await page.waitForURL(u => !/\/login$/.test(u.pathname))
}
const shot = (page, name, full = false) => page.screenshot({ path: `${SHOTS}/${name}.png`, fullPage: full })

// ---- the customer orders ----
const cust = await open({ width: 390, height: 844 })
await cust.goto(BASE + '/s/makati')
await cust.waitForSelector('.item')
await cust.click('.item:has-text("冰柠檬茶") .stepper button.fill')
await cust.click('.item:has-text("冰柠檬茶") .stepper button.fill')
await cust.click('.item:has-text("瓶装水") .stepper button.fill')
await cust.click('.bottombar .btn.primary')
await customerLogin(cust, null, { name: '陈小姐' })
await cust.waitForSelector('.totals')
await cust.locator('.bottombar .btn.primary').click()
await cust.waitForURL('**/o/**')
await cust.waitForSelector(tid('status'))
const orderUrl = cust.url()
const orderNo = (await cust.locator(tid('order-no')).innerText()).trim()
// before the store accepts, the customer's own add-on simply joins the order
await cust.waitForSelector(tid('act-addon'))
step(`customer placed ${orderNo}; "add dishes" is offered`)

// ---- the desk accepts, then adds a dish the customer asked for on the phone ----
const desk = await open({ width: 1440, height: 900 })
await login(desk, 'owner1')
await desk.goto(BASE + '/merchant/orders')
await desk.click(`.ord:has-text("${orderNo}")`)
await desk.click('.detail button:has-text("接单")')
await desk.waitForSelector(tid('addon-new'))
await desk.click(tid('addon-new'))
await desk.waitForURL(u => u.pathname === '/merchant/orders/new' && u.searchParams.has('append'))
await desk.waitForSelector(tid('ao-orig'))
expect((await desk.locator(tid('ao-title')).innerText()).includes(orderNo), 'the desk says which order it adds to')
expect(await desk.locator(tid('mo-member')).count() === 0, 'the member box is not shown when adding to an order')
const pick = desk.locator(tid('mo-pick'))
await pick.fill('b1')
await desk.waitForSelector(`${tid('mo-pick-drop')} .it.on:has-text("卤肉饭")`)
await pick.press('Enter')
await desk.waitForSelector(`${tid('mo-line')}:has-text("卤肉饭")`)
await desk.click(`${tid('ao-round')}:has-text("加急")`)
await desk.waitForFunction(() => /\+₱/.test(document.querySelector('[data-testid=ao-extra]')?.textContent || ''))
await desk.locator(tid('mo-pick')).blur()
await shot(desk, 'addon-01-desk-append')
const extra = (await desk.locator(tid('ao-extra')).innerText()).trim()
await desk.click(tid('ao-place'))
await desk.waitForSelector(tid('ao-done'))
const msg = await desk.locator(tid('ao-done-text')).innerText()
expect(msg.includes(orderNo) && msg.includes('卤肉饭') && msg.includes('/o/'), 'message for the customer: ' + msg)
await shot(desk, 'addon-02-desk-done')
step(`desk added 卤肉饭 (${extra}); the message for the customer is ready`)

// the order on the desk: the original and add-on 1, each with its header
await desk.click(tid('ao-back'))
await desk.click(`.ord:has-text("${orderNo}")`)
await desk.waitForSelector(`${tid('batch-head')}:has-text("加单 1")`)
expect(await desk.locator(`${tid('addon-tag')}`).count() > 0, 'the order card has an add-on tag')
await shot(desk, 'addon-03-desk-order')
step('the order shows the original and add-on 1')

// ---- the kitchen sees "add-on 1" on the same card ----
const kitchen = await open({ width: 1280, height: 800 })
await login(kitchen, 'kitchen1', '?next=/work')
await kitchen.goto(BASE + '/kitchen')
const card = kitchen.locator(`.card:has-text("${orderNo}")`)
await card.locator(tid('k-addon')).waitFor()
await card.locator(`${tid('k-addon-line')}:has-text("加单 1")`).waitFor()
await card.locator(`${tid('k-round')}:has-text("加急")`).waitFor()
await shot(kitchen, 'addon-04-kitchen')
step('the kitchen card says 加单 1 and 加急')

// ---- the customer adds a dish; it waits for the store ----
await cust.goto(orderUrl)
await cust.waitForSelector(`${tid('addon-head')}:has-text("加单 1")`)
await shot(cust, 'addon-05-customer-order', true)
await cust.click(tid('act-addon'))
await cust.waitForSelector(tid('addon-banner'))
await cust.waitForSelector('.item')
await cust.click('.item:has-text("瓶装水") .stepper button.fill')
await shot(cust, 'addon-06-customer-store')
await cust.click('.bottombar .btn.primary')
await cust.waitForURL('**/append')
await cust.waitForSelector(tid('addon-sums'))
await shot(cust, 'addon-07-customer-confirm', true)
await cust.click(tid('addon-send'))
await cust.waitForURL(u => /^\/o\/[^/]+$/.test(u.pathname))
await cust.waitForSelector(tid('addon-waiting'))
await shot(cust, 'addon-08-customer-waiting', true)
step('the customer sent an add-on; it waits for the store')

// ---- the desk accepts it ----
await desk.waitForSelector(`.ord:has-text("${orderNo}") ${tid('addon-pending-tag')}`, { timeout: 10000 })
await desk.click(`.ord:has-text("${orderNo}")`)
await desk.waitForSelector(tid('addon-pending'))
await shot(desk, 'addon-09-desk-pending')
await desk.click(tid('addon-accept'))
await desk.waitForSelector(`${tid('batch-head')}:has-text("加单 2")`)
await cust.waitForSelector(tid('addon-waiting'), { state: 'detached', timeout: 10000 })
await cust.waitForSelector(`${tid('addon-head')}:has-text("加单 2")`)
step('the desk accepted it; the customer page shows add-on 2')

// ---- the add-on ticket prints that batch only ----
const href = await desk.locator(`${tid('batch-head')}:has-text("加单 1") ${tid('batch-print')}`).getAttribute('href')
await desk.goto(BASE + href)
await desk.waitForSelector(tid('ticket-batch'))
expect((await desk.locator(tid('ticket-batch')).innerText()).includes('加单 1'), 'ticket head')
expect(await desk.locator('.line:has-text("冰柠檬茶")').count() === 0, 'the add-on ticket lists that batch only')
step('the ticket of add-on 1 lists its dish only')

await browser.close()
if (errors.length) { console.error(errors.join('\n')); process.exit(1) }
console.log('addon e2e passed')
})().catch(e => { console.error(e); process.exit(1) })
