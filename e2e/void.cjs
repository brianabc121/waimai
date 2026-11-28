// Voiding dishes (docs/VOID_ITEM_DESIGN.md): a customer orders for delivery;
// the order taker voids a drink not started on their own; the kitchen starts
// the rice, and voiding one of those needs a manager's code; the kitchen
// screen shows it in red until a cook taps "got it"; the customer's page
// shows what was taken off; the void slip prints; the void log adds it up.
// Usage: BASE_URL=http://localhost:8080 node e2e/void.cjs   (needs seed data and OTP_DEV_ECHO=true)
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
async function login(page, user) {
  await page.goto(BASE + '/login')
  await page.fill('input[autocomplete=username]', user)
  await page.fill('input[type=password]', 'demo1234')
  await page.click('button.primary')
  await page.waitForURL(u => !/\/login$/.test(u.pathname))
}
const shot = (page, name, full = false) => page.screenshot({ path: `${SHOTS}/${name}.png`, fullPage: full })

// ---- the owner sets an approval code ----
const owner = await open({ width: 1440, height: 900 })
await login(owner, 'owner1')
const pinSet = await owner.evaluate(() => fetch('/api/v1/auth/approve-pin', { method: 'PUT', credentials: 'same-origin',
  headers: { 'X-Requested-With': 'e2e', 'Content-Type': 'application/json' }, body: JSON.stringify({ password: 'demo1234', pin: '246810' }) }).then(r => r.status))
expect(pinSet === 200, 'setting the approval code: ' + pinSet)

// ---- the customer orders ----
const cust = await open({ width: 390, height: 844 })
await cust.goto(BASE + '/s/makati')
await cust.waitForSelector('.item')
await cust.click('.item:has-text("卤肉饭") .stepper button.fill')
await cust.click('.item:has-text("卤肉饭") .stepper button.fill')
await cust.click('.item:has-text("冰柠檬茶") .stepper button.fill')
await cust.click('.bottombar .btn.primary')
await customerLogin(cust, null, { name: '陈小姐' })
await cust.waitForSelector('.totals')
await cust.locator('.bottombar .btn.primary').click()
await cust.waitForURL('**/o/**')
await cust.waitForSelector(tid('status'))
const orderNo = (await cust.locator(tid('order-no')).innerText()).trim()
step(`customer placed ${orderNo}`)

// ---- the order taker accepts and voids the drink, not started ----
const desk = await open({ width: 1440, height: 900 })
await login(desk, 'cashier1')
await desk.goto(BASE + '/merchant/orders')
await desk.click(`.ord:has-text("${orderNo}")`)
await desk.click('.detail button:has-text("接单")')
await desk.waitForSelector(tid('void-btn'))
await desk.click(`${tid('order-line')}:has-text("冰柠檬茶") ${tid('void-btn')}`)
await desk.waitForSelector(tid('void-stage'))
expect(/还没开始做/.test(await desk.locator(tid('void-stage')).innerText()), 'a drink not started is not waste')
await desk.click(`${tid('void-reason')}:has-text("沽清了")`)
await desk.waitForFunction(() => /少收/.test(document.querySelector('[data-testid=void-ok]')?.textContent || ''))
await shot(desk, 'v01-sheet-not-started')
await desk.click(tid('void-ok'))
await desk.waitForSelector(tid('void-done'))
const msg = await desk.locator(tid('void-message')).inputValue()
expect(msg.includes(orderNo) && msg.includes('冰柠檬茶') && msg.includes('/o/'), 'the message for the customer: ' + msg)
await shot(desk, 'v02-done-message')
await desk.click(tid('void-done-ok'))
await desk.locator(`${tid('order-line')}.voidline:has-text("冰柠檬茶")`).waitFor()
step('the order taker voided the drink on their own; the message for the customer is copied')

// ---- the kitchen starts the rice ----
const kitchen = await open({ width: 1280, height: 800 })
await login(kitchen, 'owner1')
await kitchen.goto(BASE + '/kitchen')
const rice = kitchen.locator(`.card:has-text("${orderNo}") .kline:has-text("卤肉饭")`)
await rice.locator('button:has-text("开始")').click()
await rice.locator('button:has-text("开始")').waitFor({ state: 'detached' })
await kitchen.locator(`.card:has-text("${orderNo}") ${tid('k-voided')}:has-text("冰柠檬茶")`).waitFor()
step('the kitchen started the rice; the drink shows struck through')

// ---- voiding one rice now needs a manager ----
await desk.click(`${tid('order-line')}:has-text("卤肉饭"):not(.voidline) ${tid('void-btn')}`)
await desk.waitForSelector(tid('void-need'))
expect(/损耗/.test(await desk.locator(tid('void-stage')).innerText()), 'a dish started counts as waste')
await desk.click(`${tid('void-reason')}:has-text("上太慢")`)
await shot(desk, 'v03-sheet-started-needs-manager')
await desk.click(tid('void-ok'))
await desk.waitForSelector(tid('approve-dialog'))
await desk.locator('[data-approver] .rtag.owner').click()
await desk.fill(tid('approve-pin'), '246810')
await desk.click(tid('approve-go'))
await desk.waitForSelector(tid('void-done'))
await desk.click(tid('void-done-ok'))
await desk.locator(tid('void-note') + ':has-text("上太慢")').waitFor()
await shot(desk, 'v04-order-after-voids')
step('voiding a rice the kitchen started took the owner’s code')

// ---- the kitchen sees it in red and says so ----
await kitchen.locator(tid('k-void-alert') + ':has-text("卤肉饭")').waitFor()
await shot(kitchen, 'v05-kitchen-alert')
await kitchen.click(tid('k-void-ack'))
await kitchen.locator(tid('k-void-alert')).waitFor({ state: 'detached' })
step('the kitchen screen showed the void in red until "got it"')

// ---- the customer sees what was taken off ----
await cust.reload()
await cust.locator(tid('void-banner')).waitFor()
await cust.locator(tid('od-voided')).first().waitFor()
await shot(cust, 'v06-customer-order', true)
const custText = await cust.locator('body').innerText()
expect(!custText.includes('收银 小陈'), 'the customer does not see who voided it')
step('the customer page shows the dishes taken off and the new total')

// ---- the void slip and the log ----
const slipHref = await desk.locator(tid('void-note') + ':has-text("上太慢") a').getAttribute('href')
const slip = await open({ width: 420, height: 700 })
await login(slip, 'owner1')
await slip.goto(BASE + slipHref)
await slip.locator(tid('ticket-void') + ':has-text("卤肉饭")').waitFor()
await shot(slip, 'v07-void-slip')
await owner.goto(BASE + '/merchant/voids')
await owner.locator(tid('voids-rows') + ' tbody tr').nth(1).waitFor()
await shot(owner, 'v08-void-log', true)
expect(await owner.locator(tid('voids-rows') + ' tbody tr').count() >= 2, 'the log lists both voids')
step('the void slip prints; the void log lists both voids')

const phone = await open({ width: 390, height: 844 })
await login(phone, 'cashier1')
await phone.goto(BASE + '/merchant/orders')
await phone.click(`.ord:has-text("${orderNo}")`)
await phone.click(`${tid('order-line')}:has-text("卤肉饭"):not(.voidline) ${tid('void-btn')}`)
await phone.locator(tid('void-error') + ':has-text("最后一件商品")').waitFor()
await shot(phone, 'v09-phone-last-dish')
await phone.click('.vh .x')
step('the last dish of a delivery cannot be voided (cancel instead); the sheet fits a phone')

if (errors.length) throw new Error(errors.join('\n'))
await browser.close()
console.log('void e2e passed')
})().catch(e => { console.error(e); process.exit(1) })
