// Every amount is shown in the money of the store's country: a store that
// moves to Singapore shows S$ to its customers, in its back office and to the
// platform, whose currency box then follows the country.
// Usage: BASE_URL=http://localhost:8080 node e2e/currency.cjs
//   (needs seed data, OTP_DEV_ECHO=true and GOOGLE_MAPS_API_KEY=stub)
const { chromium } = require('playwright')
const { mkdirSync } = require('node:fs')
const { customerLogin } = require('./lib.cjs')

;(async () => {

const BASE = process.env.BASE_URL || 'http://localhost:8080'
const SHOTS = process.env.SHOTS_DIR || 'e2e/shots'
mkdirSync(SHOTS, { recursive: true })
const browser = await chromium.launch()
const errors = []
const opts = { locale: 'zh-CN', ignoreHTTPSErrors: process.env.E2E_INSECURE_TLS === '1' }
const tid = id => `[data-testid=${id}]`
const step = s => console.log('✓ ' + s)
const expect = (ok, what) => { if (!ok) throw new Error(what) }
async function open(viewport) {
  const ctx = await browser.newContext({ viewport, ...opts })
  const p = await ctx.newPage()
  p.on('pageerror', e => errors.push(`pageerror: ${e.message}`))
  return p
}
async function signIn(viewport, user) {
  const p = await open(viewport)
  await p.goto(BASE + '/login')
  await p.fill('input[autocomplete=username]', user)
  await p.fill('input[type=password]', 'demo1234')
  await p.click('button.primary')
  await p.waitForURL(u => !u.pathname.startsWith('/login'))
  return p
}
const SLUG = 'bgc'
const call = (p, method, path, body) => p.evaluate(async ([method, path, body]) => {
  const r = await fetch('/api/v1' + path, { method, credentials: 'same-origin', headers: { 'X-Requested-With': 'waimai-web', 'Content-Type': 'application/json' }, body: body && JSON.stringify(body) })
  return r.json()
}, [method, path, body])

const owner = await signIn({ width: 1280, height: 860 }, 'owner2')
const me = await call(owner, 'GET', '/auth/me')
const bid = me.roles.find(r => r.slug === SLUG).branch_id
// the demo store stays where the seed put it; only the country read off the map changes
const where = region => call(owner, 'PUT', `/merchant/branches/${bid}/location`, { lat: 14.5547, lng: 121.0244, address: '', region })
try {
  // 1. the store's country decides its currency
  let b = await where({ country_code: 'SG', country: 'Singapore', city: 'Singapore' })
  expect(b.currency === 'SGD', 'currency after the store is placed in Singapore: ' + JSON.stringify(b))
  step('store placed in Singapore pays in SGD')

  // 2. the merchant sees it on the map page and in the menu
  await owner.goto(BASE + '/merchant/delivery-map')
  await owner.waitForSelector(tid('store-currency'))
  const shown = await owner.textContent(tid('store-currency'))
  expect(shown.includes('SGD') && shown.includes('S$'), 'map page currency: ' + shown)
  await owner.screenshot({ path: `${SHOTS}/cur1-merchant-map.png` })
  await owner.goto(BASE + '/merchant/menu')
  await owner.waitForSelector('text=S$')
  expect(!(await owner.locator('text=₱').count()), 'a ₱ left in the merchant menu')
  await owner.screenshot({ path: `${SHOTS}/cur2-merchant-menu.png` })
  step('back office shows S$')

  // 3. the customer sees S$ on the menu, in the cart and at checkout
  const cust = await open({ width: 390, height: 844 })
  await cust.goto(BASE + '/s/' + SLUG)
  await cust.waitForSelector('.price')
  const prices = await cust.locator('.price').allTextContents()
  expect(prices.length && prices.every(t => t.startsWith('S$')), 'menu prices: ' + prices.slice(0, 5))
  await cust.screenshot({ path: `${SHOTS}/cur3-customer-menu.png` })
  await cust.click('.item:has-text("无盐地瓜条") .stepper button.fill')
  await cust.waitForSelector('.bottombar .btn.primary:not([disabled])')
  await cust.click('.bottombar .btn.primary')
  await customerLogin(cust, null, { pickup: true })
  await cust.waitForSelector('.co-row')
  const rows = await cust.locator('.co-row').allTextContents()
  expect(rows.some(t => t.includes('S$')) && !rows.some(t => t.includes('₱')), 'checkout rows: ' + rows)
  await cust.screenshot({ path: `${SHOTS}/cur4-checkout.png` })
  step('customer menu and checkout in S$')

  // 4. the platform: the currency box follows the country
  const admin = await signIn({ width: 1360, height: 900 }, 'admin')
  await admin.goto(BASE + `/admin/merchants`)
  await admin.click(`text=${me.roles.find(r => r.slug === SLUG).branch_name}`)
  await admin.waitForSelector('input[name=currency], button:has-text("编辑")')
  const edit = admin.locator('tr', { hasText: SLUG }).locator('button', { hasText: '编辑' })
  await edit.first().click()
  await admin.waitForSelector(tid('currency-hint'))
  expect(await admin.locator('input[name=currency]').isDisabled(), 'currency box should be locked')
  expect((await admin.textContent(tid('currency-hint'))).includes('SG'), 'hint names the country')
  await admin.screenshot({ path: `${SHOTS}/cur5-admin-branch.png` })
  step('platform admin: currency follows the country')
} finally {
  const b = await where({ country_code: 'PH', country: 'Philippines', province: 'Metro Manila', city: 'Makati' })
  expect(b.currency === 'PHP', 'back in the Philippines: ' + b.currency)
  step('store back in the Philippines pays in PHP')
}

expect(!errors.length, errors.join('\n'))
await browser.close()
console.log('currency: all good')
})().catch(e => { console.error(e); process.exit(1) })
