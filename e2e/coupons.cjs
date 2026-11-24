// Coupons in a real browser (docs/COUPON_DESIGN.md, first batch): the store
// switches on its welcome-back coupon, a new customer's first order earns it,
// the next checkout uses it by itself, the customer can change the choice,
// and "my offers" lists it as used.
// Usage: BASE_URL=http://localhost:8080 node e2e/coupons.cjs   (needs seed data and OTP_DEV_ECHO=true)
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
  const ctx = await browser.newContext({ viewport, locale: 'zh-CN', ignoreHTTPSErrors: process.env.E2E_INSECURE_TLS === '1' })
  const page = await ctx.newPage()
  page.on('pageerror', e => errors.push(`pageerror: ${e.message}`))
  page.on('console', m => { if (m.type() === 'error' && !/favicon|Failed to load resource/.test(m.text())) errors.push(`console: ${m.text()}`) })
  return page
}
const step = (s) => console.log('✓ ' + s)
const shot = (page, name) => page.screenshot({ path: `${SHOTS}/${name}.png` })
const expect = (got, want, what) => { if (got !== want) throw new Error(`${what}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`) }
const tid = (id) => `[data-testid=${id}]`
const text = async (page, sel) => (await page.locator(sel).first().innerText()).replace(/\s+/g, ' ').trim()

// ---- the store switches the welcome-back coupon on ----
const owner = await open({ width: 1280, height: 900 })
await owner.goto(BASE + '/login')
await owner.fill('input[autocomplete=username]', 'owner1')
await owner.fill('input[type=password]', 'demo1234')
await owner.click('button.primary')
await owner.waitForURL(u => !/\/login$/.test(u.pathname))
const api = async (method, path, data) => {
  const r = await owner.request.fetch(BASE + '/api/v1' + path, { method, data, headers: { 'X-Requested-With': 'e2e' } })
  if (!r.ok()) throw new Error(`${method} ${path}: ${r.status()} ${await r.text()}`)
  return r.json()
}
const store = await api('GET', '/stores/makati')
await owner.goto(BASE + '/merchant/promos')
await owner.click(tid('coupon-tab'))
await owner.waitForURL(u => u.search.includes('tab=coupons'))
await owner.waitForSelector(tid('repurchase-form'))
await owner.click(tid('repurchase-kind-store_free'))
await owner.fill(tid('repurchase-value'), '20')
await owner.fill(tid('repurchase-days'), '30')
await owner.fill(tid('repurchase-min'), '0')
const on = await owner.getAttribute(tid('repurchase-switch'), 'aria-pressed')
if (on !== 'true') await owner.click(tid('repurchase-switch'))
await owner.waitForSelector(`${tid('repurchase-switch')}[aria-pressed=true]`)
await owner.click(tid('repurchase-save'))
await owner.waitForSelector(tid('repurchase-saved'))
expect((await text(owner, tid('repurchase-preview'))).includes('无门槛减₱20'), true, 'preview shows the coupon')
await shot(owner, 'c1-merchant-repurchase')
const rule = await api('GET', `/merchant/branches/${store.branch.id}/coupons/repurchase`)
expect(rule.enabled && rule.value === 2000 && rule.valid_days === 30, true, 'rule saved')
step('the store switches on a ₱20 welcome-back coupon, 30 days')

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
  return o
}

// ---- a new customer's first order earns it ----
const cust = await open({ width: 390, height: 844 })
async function toCheckout(dishes) {
  await cust.goto(BASE + '/s/makati')
  await cust.waitForSelector('.item')
  for (const d of dishes) await cust.click(`.item:has-text("${d}") .stepper button.fill`)
  await cust.click('.cartbar .btn.primary')
  await customerLogin(cust, null, { pickup: true })
  await cust.fill('input[autocomplete=name]', '陈小姐')
  await cust.waitForSelector(tid('checkout-total'))
}
async function place() {
  await cust.locator('.bottombar .btn.primary').click()
  await cust.waitForURL('**/o/**')
  await cust.waitForSelector(tid('status'))
  return (await cust.locator(tid('order-no')).innerText()).trim()
}
await toCheckout(['卤肉饭', '冰柠檬茶'])
expect(await cust.locator(tid('coupon-row')).count(), 0, 'a new customer has no coupons')
const first = await place()
await finish(first)
await cust.reload()
await cust.waitForSelector(tid('order-gift'))
expect((await text(cust, tid('order-gift'))).includes('送你一张无门槛减₱20'), true, 'gift on the order page')
await shot(cust, 'c2-order-gift')
step(`order ${first} is picked up: the order page shows the ₱20 coupon it earned`)

// ---- the next checkout uses it by itself ----
await toCheckout(['卤肉饭'])
await cust.waitForSelector(tid('coupon-line'))
expect((await text(cust, tid('coupon-row'))).includes('已选 1 张'), true, 'one coupon chosen')
expect((await text(cust, tid('coupon-line'))).includes('−₱20'), true, 'coupon line')
const withCoupon = await text(cust, tid('checkout-total'))
await shot(cust, 'c3-checkout-coupon')
// the customer can choose not to use it, and then use it again
await cust.click(tid('coupon-row'))
await cust.waitForSelector(tid('coupon-sheet'))
await shot(cust, 'c4-coupon-sheet')
await cust.locator(`${tid('coupon-sheet')} [aria-pressed=true]`).first().click()
await cust.click(tid('coupon-done'))
await cust.waitForFunction(() => !document.querySelector('[data-testid=coupon-line]'))
expect((await text(cust, tid('coupon-row'))).includes('不使用'), true, 'none chosen')
await cust.click(tid('coupon-row'))
await cust.locator(`${tid('coupon-sheet')} [aria-pressed=false]`).first().click()
await cust.click(tid('coupon-done'))
await cust.waitForSelector(tid('coupon-line'))
expect(await text(cust, tid('checkout-total')), withCoupon, 'total with the coupon again')
step('the next checkout picks the coupon by itself; the customer can drop it and take it back')

const second = await place()
await cust.waitForSelector(tid('discount-line'))
expect((await text(cust, tid('order-fees'))).includes('复购礼券'), true, 'the order lists the coupon')
const o2 = await deskOrder(second)
expect(o2.discounts.some(d => d.kind === 'store_free' && d.amount === 2000), true, 'desk sees the coupon')
expect(o2.totals.merchant_discount >= 2000, true, 'store pays for it')
step(`order ${second} used the coupon; the store's desk sees it`)

// ---- my offers: it is now used ----
await cust.goto(BASE + '/me/coupons')
await cust.waitForSelector(`${tid('my-coupon')}, ${tid('coupons-empty')}`)
await cust.click(tid('coupon-tab-used'))
await cust.waitForSelector(tid('my-coupon'))
expect((await text(cust, tid('my-coupon'))).includes('复购礼券'), true, 'used coupon listed')
await shot(cust, 'c5-my-coupons-used')
// cancelling gives it back
await cust.goto(BASE + '/orders')
const o2c = await deskOrder(second)
await api('POST', `/merchant/orders/${o2c.id}/reject`, { expected_version: o2c.version, reason: '售罄' })
await cust.goto(BASE + '/me/coupons')
await cust.waitForSelector(tid('my-coupon'))
expect(await cust.locator(`${tid('coupon-tab-available')}`).innerText(), '可用 (1)', 'coupon back after the store rejected')
await shot(cust, 'c6-my-coupons')
step('the store rejects the order: the coupon is usable again')

// ---- the merchant page counts it ----
await owner.reload()
await owner.waitForSelector(tid('repurchase-stats'))
expect((await text(owner, tid('repurchase-stats'))).includes('送出'), true, 'stats shown')

if (errors.length) throw new Error('browser errors:\n' + errors.join('\n'))
await browser.close()
console.log('coupons: all good')
})().catch(e => { console.error(e); process.exit(1) })
