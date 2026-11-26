// The store's own page (docs/ORDER_TYPES_DESIGN.md §5): <store_no>.localhost
// shows the store at the root with no way into the mall; an order there is an
// Online- order; "me" keeps that store's pages only; old /s/ links in the
// mall lead to the store inside the mall; the merchant's "store link" page.
// Usage: BASE_URL=http://localhost:8080 node e2e/storesite.cjs   (seed data, OTP_DEV_ECHO=true)
const { chromium } = require('playwright')
const { customerLogin } = require('./lib.cjs')
const { mkdirSync } = require('node:fs')

;(async () => {
const BASE = process.env.BASE_URL || 'http://localhost:8080'
const base = new URL(BASE)
const OWN = `${base.protocol}//10001.localhost:${base.port}` // makati
const SHOTS = process.env.SHOTS_DIR || 'e2e/shots'
mkdirSync(SHOTS, { recursive: true })
const browser = await chromium.launch({ args: ['--host-resolver-rules=MAP *.localhost 127.0.0.1'] })
const errors = []
async function open(viewport) {
  const ctx = await browser.newContext({ viewport, locale: 'zh-CN' })
  const page = await ctx.newPage()
  page.on('pageerror', e => errors.push(`pageerror: ${e.message}`))
  page.on('console', m => { if (m.type() === 'error' && !/favicon|Failed to load resource/.test(m.text())) errors.push(`console: ${m.text()}`) })
  page.on('dialog', d => d.accept())
  return page
}
const step = s => console.log('✓ ' + s)
const shot = (p, n) => p.screenshot({ path: `${SHOTS}/${n}.png`, fullPage: false })

// ---- the store's own page ----
const cust = await open({ width: 390, height: 844 })
await cust.goto(OWN + '/')
await cust.waitForSelector('.item')
if (await cust.locator('[data-testid=back-mall]').count()) throw new Error('own page links back to the mall')
if (await cust.locator('[data-testid=favorite]').count()) throw new Error('own page has the favourite button')
await cust.locator('[data-testid=powered]').waitFor()
const homeTab = (await cust.locator('[data-testid=tab-home]').innerText()).trim()
if (homeTab !== '点餐') throw new Error('home tab reads ' + homeTab)
await shot(cust, 'site-01-store')
step('own page shows the store at / without the mall')

await cust.click('.item:has-text("冰柠檬茶") .stepper button.fill')
await cust.click('.bottombar .btn.primary')
await customerLogin(cust, null, { pickup: true })
if (new URL(cust.url()).pathname !== '/checkout') throw new Error('checkout at ' + cust.url())
await cust.fill('[data-testid=checkout-pickup] input[autocomplete=name]', '王小明')
await cust.locator('.bottombar .btn.primary').click()
await cust.waitForURL('**/o/**')
const no = (await cust.locator('[data-testid=order-no]').innerText()).trim()
if (!/Online-\d+/.test(no)) throw new Error('order number ' + no)
await shot(cust, 'site-02-order')
step('order on the own page: ' + no)

// mall pages lead back to the store
for (const p of ['/s/bgc', '/mall/s/bgc', '/me/favorites', '/me/apply']) {
  await cust.goto(OWN + p)
  await cust.waitForURL(u => u.pathname === '/')
}
await cust.goto(OWN + '/me/messages')
await cust.waitForURL(u => u.pathname === '/chat/makati')
await cust.goto(OWN + '/me')
await cust.waitForSelector('[data-testid=me-who]')
for (const id of ['me-favorites', 'me-support', 'me-apply', 'me-balance']) {
  if (await cust.locator(`[data-testid=${id}]`).count()) throw new Error(`own page "me" shows ${id}`)
}
await shot(cust, 'site-03-me')
await cust.goto(OWN + '/orders')
await cust.waitForSelector(`text=${no}`)
step('"me" and the orders keep to the store')

// ---- the mall ----
const mall = await open({ width: 390, height: 844 })
await mall.goto(BASE + '/s/makati')
await mall.waitForURL(u => u.pathname === '/mall/s/makati')
await mall.waitForSelector('.item')
await mall.locator('[data-testid=back-mall]').waitFor()
step('old link in the mall opens the store in the mall')

// ---- the merchant's store link page ----
const desk = await open({ width: 1280, height: 860 })
await desk.goto(BASE + '/login')
await desk.fill('input[autocomplete=username]', 'owner1')
await desk.fill('input[type=password]', 'demo1234')
await desk.click('button.primary')
await desk.waitForURL(u => !/\/login/.test(u.pathname))
await desk.goto(BASE + '/merchant/site-link')
const url = (await desk.locator('[data-testid=sl-url]').innerText()).trim()
if (!url.includes('10001.localhost')) throw new Error('store link ' + url)
await desk.locator('[data-testid=sl-qr]').waitFor()
await desk.fill('[data-testid=sl-buy-input]', 'makati-kitchen.ph')
await desk.click('[data-testid=sl-buy] button.primary')
await desk.locator('[data-testid=sl-domain]').first().waitFor()
await shot(desk, 'site-04-merchant')
step('merchant store link page: ' + url)

await browser.close()
if (errors.length) { console.error(errors.join('\n')); process.exit(1) }
console.log('storesite OK')
})().catch(e => { console.error(e); process.exit(1) })
