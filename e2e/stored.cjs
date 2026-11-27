// Stored value, "top up X, get Y free" (docs/STORED_VALUE_DESIGN.md): the
// owner accepts the terms and sets up a campaign, the customer picks a tier
// on the store's offers page and shows the code, the cashier takes the money,
// the customer pays an order from it, and the store's pages list it all.
// Usage: BASE_URL=http://localhost:8080 node e2e/stored.cjs   (needs seed data and OTP_DEV_ECHO=true)
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
  const ctx = await browser.newContext({ viewport, locale: 'zh-CN', deviceScaleFactor: 2, ignoreHTTPSErrors: process.env.E2E_INSECURE_TLS === '1' })
  const page = await ctx.newPage()
  page.on('pageerror', e => errors.push(`pageerror: ${e.message}`))
  page.on('console', m => { if (m.type() === 'error' && !/favicon|Failed to load resource/.test(m.text())) errors.push(`console: ${m.text()}`) })
  return page
}
const step = (s) => console.log('✓ ' + s)
const shot = (page, name) => page.screenshot({ path: `${SHOTS}/${name}.png` })
const expect = (ok, what) => { if (!ok) throw new Error(what) }
const tid = (id) => `[data-testid=${id}]`

const owner = await open({ width: 1366, height: 860 })
await owner.goto(BASE + '/login')
await owner.fill('input[autocomplete=username]', 'owner1')
await owner.fill('input[type=password]', 'demo1234')
await owner.click('button.primary')
await owner.waitForURL(u => !/\/login/.test(u.pathname))

// ---- the owner accepts the terms and sets up a campaign ----
await owner.goto(BASE + '/merchant/marketing')
await owner.waitForSelector(tid('marketing-page'))
if (await owner.locator(tid('mk-terms-ok')).count()) {
  await shot(owner, 'sv01-marketing-terms')
  await owner.click(tid('mk-terms-ok'))
}
await owner.waitForSelector(tid('mk-new'))
// one campaign at a time: a second run of this test keeps the first one
if (!(await owner.locator(tid('mk-campaign')).count())) {
  await owner.click(tid('mk-new'))
  await owner.waitForSelector(tid('campaign-editor'))
  await owner.fill(tid('ce-name'), '充值送 · 开业')
  expect((await owner.locator(tid('ce-tier')).count()) === 3, 'three tiers to start with')
  await shot(owner, 'sv02-campaign-editor')
  await owner.click(tid('ce-save'))
  await owner.waitForSelector(tid('campaign-editor'), { state: 'detached' })
}
await owner.waitForSelector(tid('mk-campaign'))
expect((await owner.locator(tid('mk-tier')).count()) === 3, 'the campaign lists its tiers')
await shot(owner, 'sv03-marketing')
step('campaign set up with three tiers')

// ---- the customer: offers page, signs in, picks 1,000 get 150, a code ----
const cust = await open({ width: 390, height: 844 })
await cust.goto(BASE + '/s/makati')
await cust.waitForSelector('.item')
await cust.waitForSelector(tid('offers-bar'))
await shot(cust, 'sv04-store-offers-bar')
await cust.click(tid('offers-bar'))
await cust.waitForSelector(tid('offers-topup'))
await shot(cust, 'sv05-offers')
await cust.locator(tid('offers-tier')).nth(1).click()
await cust.click(tid('offers-go'))
await cust.waitForURL(u => u.pathname === '/account/login')
await cust.fill('[data-testid=login-phone]', '9' + String(Math.floor(Math.random() * 1e9)).padStart(9, '0'))
await cust.check('[data-testid=login-agree]')
await cust.click('[data-testid=login-send]')
await cust.waitForURL(u => /offers/.test(u.pathname))
await cust.waitForSelector(tid('offers-go'))
await cust.locator(tid('offers-tier')).nth(1).click()
await cust.click(tid('offers-go'))
await cust.waitForSelector(tid('topup-code-digits'))
const code = (await cust.locator(tid('topup-code-digits')).innerText()).replace(/\D/g, '')
expect(code.length === 6, `a six-digit code: ${code}`)
await shot(cust, 'sv06-topup-code')
step(`the customer shows code ${code}`)

// ---- the cashier takes the money ----
await owner.goto(BASE + '/merchant/order/topup')
await owner.waitForSelector(tid('topup-desk'))
await owner.fill(tid('td-code'), code)
await owner.click(tid('td-find'))
await owner.waitForSelector(tid('td-confirm'))
expect(/1,?000/.test(await owner.locator(tid('td-get')).innerText()), 'pay 1,000')
await shot(owner, 'sv07-topup-desk')
await owner.click(tid('td-ok'))
await owner.waitForSelector(tid('td-done'))
await owner.waitForSelector(tid('td-row'))
step('the cashier confirmed the top-up')

await cust.waitForSelector(tid('topup-paid'), { timeout: 15000 })
expect(/1,?150/.test(await cust.locator(tid('offers-balance')).innerText()), 'the balance shows 1,150')
await shot(cust, 'sv08-offers-paid')
step('the customer sees 1,150 in stored value')

// ---- the customer pays an order from it ----
await cust.goto(BASE + '/s/makati')
await cust.waitForSelector('.item')
for (const d of ['卤肉饭', '冰柠檬茶']) await cust.click(`.item:has-text("${d}") .stepper button.fill`)
await cust.click('.cartbar .btn.primary')
await customerLogin(cust, null, { pickup: true })
await cust.fill('input[autocomplete=name]', '陈小姐')
await cust.waitForSelector(tid('use-credit'))
await cust.waitForSelector(tid('credit-line'))
await shot(cust, 'sv09-checkout-stored')
await cust.locator('.bottombar .btn.primary').click()
await cust.waitForURL('**/o/**')
await cust.waitForSelector(tid('credit-paid-line'))
await shot(cust, 'sv10-order-stored')
step('an order paid from stored value')

await cust.goto(BASE + '/me/store-credit')
await cust.waitForSelector(tid('credit-store'))
await shot(cust, 'sv11-me-store-credit')

// ---- the store's pages ----
await owner.goto(BASE + '/merchant/marketing')
await owner.waitForSelector(tid('mk-topup'))
await shot(owner, 'sv12-marketing-after')
await owner.goto(BASE + '/merchant/credit')
await owner.click(tid('credit-tab-stored'))
await owner.waitForSelector(tid('stored-panel'))
await owner.waitForSelector(tid('sp-holder'))
await shot(owner, 'sv13-stored-panel')
await owner.click(`${tid('stored-panel')} button:has-text("变动记录")`)
await owner.waitForSelector(tid('sp-entry'))
expect((await owner.locator(tid('sp-entry')).count()) >= 2, 'a top-up and a spend')
await shot(owner, 'sv13b-stored-entries')
await owner.click(`${tid('stored-panel')} button:has-text("谁有余额")`)
await owner.click(tid('sp-cashout'))
await owner.waitForSelector(tid('cashout-sheet'))
await owner.waitForSelector(tid('co-pay'))
await shot(owner, 'sv14-cashout-sheet')
step('the store lists the holder and quotes a payback')

await owner.goto(BASE + '/merchant/reconcile')
await owner.waitForSelector(tid('rc-stored'))
await shot(owner, 'sv15-reconcile')
step('the reconcile page has the stored value card')

const phone = await open({ width: 390, height: 844 })
await phone.context().addCookies(await owner.context().cookies())
await phone.goto(BASE + '/merchant/order/topup')
await phone.waitForSelector(tid('topup-desk'))
await shot(phone, 'sv16-topup-desk-phone')
await phone.goto(BASE + '/merchant/marketing')
await phone.waitForSelector(tid('mk-campaign'))
await shot(phone, 'sv17-marketing-phone')

const admin = await open({ width: 1366, height: 860 })
await admin.goto(BASE + '/admin/login')
await admin.fill('input[autocomplete=username]', 'admin')
await admin.fill('input[type=password]', 'demo1234')
await admin.click('button.primary')
await admin.waitForURL(u => !/\/login/.test(u.pathname))
await admin.goto(BASE + '/admin/stored')
await admin.waitForSelector('table')
await shot(admin, 'sv18-admin-stored')
step('the platform page lists countries')

if (errors.length) throw new Error('page errors:\n' + errors.join('\n'))
await browser.close()
console.log('stored value: all good')
})().catch(e => { console.error(e); process.exit(1) })
