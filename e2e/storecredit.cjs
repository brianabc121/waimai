// Store credit (docs/REFUND_BALANCE_DESIGN.md): the store refunds a dish into
// the customer's balance at the store, the customer sees it under "me", pays
// part of the next order with it at checkout, and the store's page lists it.
// Usage: BASE_URL=http://localhost:8080 node e2e/storecredit.cjs   (needs seed data and OTP_DEV_ECHO=true)
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
const api = async (method, path, data) => {
  const r = await owner.request.fetch(BASE + '/api/v1' + path, { method, data, headers: { 'X-Requested-With': 'e2e' } })
  if (!r.ok()) throw new Error(`${method} ${path}: ${r.status()} ${await r.text()}`)
  return r.json()
}
const store = await api('GET', '/stores/makati')
const deskOrder = async (no) => {
  for (const scope of ['active', 'history']) {
    const l = await api('GET', `/merchant/branches/${store.branch.id}/orders?scope=${scope}`)
    const o = l.orders.find(x => x.public_no === no)
    if (o) return o
  }
  throw new Error(`order ${no} is not on the desk`)
}
async function finish(no) {
  let o = await deskOrder(no)
  o = await api('POST', `/merchant/orders/${o.id}/accept`, { expected_version: o.version })
  for (const it of o.items.filter(i => i.needs_prep)) o = await api('POST', `/kitchen/items/${it.id}/ready`, {})
  o = await api('POST', `/kitchen/orders/${o.id}/pack`, { bags: 1, manifest_version: o.manifest_version })
  await api('POST', `/merchant/orders/${o.id}/handoff`, { expected_version: 0 })
}

const cust = await open({ width: 390, height: 844 })
async function order(dishes, useCredit) {
  await cust.goto(BASE + '/s/makati')
  await cust.waitForSelector('.item')
  for (const d of dishes) await cust.click(`.item:has-text("${d}") .stepper button.fill`)
  await cust.click('.cartbar .btn.primary')
  await customerLogin(cust, null, { pickup: true })
  await cust.fill('input[autocomplete=name]', '陈小姐')
  if (useCredit) {
    await cust.waitForSelector(tid('use-credit'))
    await cust.waitForSelector(tid('credit-line'))
    await shot(cust, 'sc03-checkout-credit')
  }
  await cust.locator('.bottombar .btn.primary').click()
  await cust.waitForURL('**/o/**')
  await cust.waitForSelector(tid('status'))
  return (await cust.locator(tid('order-no')).innerText()).trim()
}

// ---- a paid order; the store refunds a dish into the balance ----
const first = await order(['卤肉饭', '冰柠檬茶'], false)
await finish(first)
await owner.goto(BASE + '/merchant/orders')
await owner.click('button:has-text("历史")')
await owner.click(`.ord:has-text("${first}")`)
await owner.click(tid('refund-btn'))
await owner.waitForSelector(tid('refund-sheet'))
await owner.click(`${tid('refund-line')}:has-text("卤肉饭") ${tid('refund-more')}`)
await owner.click(`${tid('refund-reason')} >> nth=0`)
await owner.click(tid('refund-to-credit'))
await owner.click(`${tid('refund-consent')} >> nth=0`)
await owner.waitForSelector(tid('refund-split'))
await shot(owner, 'sc01-refund-to-credit')
await owner.click(tid('refund-ok'))
await owner.waitForSelector(tid('refund-to-credit-tag'))
step(`order ${first}: a dish refunded into the store balance`)

// ---- the customer sees it under "me" ----
await cust.goto(BASE + '/me/store-credit')
await cust.waitForSelector(tid('credit-store'))
await shot(cust, 'sc02-me-store-credit')
await cust.click(tid('credit-store'))
await cust.waitForSelector(tid('credit-entry'))
expect(/退款存入/.test(await cust.locator(tid('credit-entry')).first().innerText()), 'the refund is in the history')
await shot(cust, 'sc02b-me-store-credit-entries')
step('the customer sees the balance and its history')

// ---- and pays part of the next order with it ----
const second = await order(['卤肉饭', '冰柠檬茶', '瓶装水'], true)
await cust.waitForSelector(tid('credit-paid-line'))
await cust.locator(tid('due-line')).scrollIntoViewIfNeeded()
await shot(cust, 'sc04-order-credit-paid')
step(`order ${second} paid partly from the balance`)

// ---- the store's page ----
await owner.goto(BASE + '/merchant/credit')
await owner.waitForSelector(tid('credit-row'))
expect((await owner.locator(tid('credit-row')).count()) === 2, 'a refund and a spend')
await shot(owner, 'sc05-merchant-credit')
step('the store page lists the refund and the spend')

const phone = await open({ width: 390, height: 844 })
await phone.context().addCookies(await owner.context().cookies())
await phone.goto(BASE + '/merchant/credit')
await phone.waitForSelector(tid('credit-row'))
await shot(phone, 'sc06-merchant-credit-phone')

if (errors.length) throw new Error('page errors:\n' + errors.join('\n'))
await browser.close()
console.log('store credit: all good')
})().catch(e => { console.error(e); process.exit(1) })
