// The customer's orders in a real browser: the list with its tabs and search,
// the order page and its way back, a refund request the store answers,
// reviewing from the list and ordering the same again.
// Usage: BASE_URL=http://localhost:8080 node e2e/orders.cjs   (needs seed data and OTP_DEV_ECHO=true)
const { chromium } = require('playwright')
const { customerLogin } = require('./lib.cjs')
const { mkdirSync } = require('node:fs')

;(async () => {

const BASE = process.env.BASE_URL || 'http://localhost:8080'
const SHOTS = process.env.SHOTS_DIR || 'e2e/shots'
mkdirSync(SHOTS, { recursive: true })
const browser = await chromium.launch()
const errors = []
const REPLY = '已补送冰柠檬茶，抱歉让你久等'

async function open(viewport, ctx) {
  ctx = ctx || await browser.newContext({ viewport, locale: 'zh-CN', ignoreHTTPSErrors: process.env.E2E_INSECURE_TLS === '1' })
  const page = await ctx.newPage()
  if (viewport) await page.setViewportSize(viewport)
  page.on('pageerror', e => errors.push(`pageerror: ${e.message}`))
  page.on('console', m => { if (m.type() === 'error' && !/favicon|Failed to load resource/.test(m.text())) errors.push(`console: ${m.text()}`) })
  page.on('dialog', d => d.accept(d.type() === 'prompt' ? REPLY : undefined))
  return page
}
async function login(page, user) {
  await page.goto(BASE + (/^admin/.test(user) ? '/admin/login' : /^((kitchen|rider|zhang|wang)\d|e2e\d+$)/.test(user) ? '/login?next=/work' : '/login'))
  await page.fill('input[autocomplete=username]', user)
  await page.fill('input[type=password]', 'demo1234')
  await page.click('button.primary')
  await page.waitForURL(u => !/\/login$/.test(u.pathname))
}
const step = (s) => console.log('✓ ' + s)
const shot = (page, name) => page.screenshot({ path: `${SHOTS}/${name}.png` })
const expect = (got, want, what) => { if (got !== want) throw new Error(`${what}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`) }
const tid = (id) => `[data-testid=${id}]`
const text = async (page, sel) => (await page.locator(sel).first().innerText()).replace(/\s+/g, ' ').trim()
const path = (page) => new URL(page.url()).pathname
const listReady = (page) => page.waitForSelector(`${tid('order-list')}[data-state=ready]`)

const owner = await open({ width: 1280, height: 860 })
await login(owner, 'owner1')
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

// ---- nobody sees a list of orders without logging in ----
const cust = await open({ width: 390, height: 844 })
await cust.goto(BASE + '/orders')
await cust.waitForURL(u => u.pathname === '/account/login')
expect(new URL(cust.url()).searchParams.get('next'), '/orders', 'login comes back to the orders')
step('the order list asks for a login')

// ---- a pickup order; the order page is not a dead end ----
async function order(dishes) {
  await cust.goto(BASE + '/s/makati')
  await cust.waitForSelector('.item')
  for (const d of dishes) await cust.click(`.item:has-text("${d}") .stepper button.fill`)
  await cust.click('.cartbar .btn.primary')
  await customerLogin(cust, null, { pickup: true })
  await cust.fill('input[autocomplete=name]', '陈小姐')
  await cust.locator('.bottombar .btn.primary').click()
  await cust.waitForURL('**/o/**')
  await cust.waitForSelector(tid('status'))
  return (await cust.locator(tid('order-no')).innerText()).trim()
}
const first = await order(['卤肉饭', '冰柠檬茶'])
await cust.locator(tid('act-cancel')).waitFor()
expect(await cust.locator(tid('act-aftersale')).count(), 0, 'no refund request before the store accepts')
await cust.locator(tid('act-again')).waitFor()
expect(await cust.locator(tid('act-store')).count(), 0, 'no call-the-store button')
await shot(cust, 'o1-order-waiting')
await cust.click(tid('order-back'))
await cust.waitForURL(u => u.pathname === '/orders')
await listReady(cust)
await cust.locator(`[data-order="${first}"]`).waitFor()
expect(await text(cust, `[data-order="${first}"] ${tid('card-status')}`), '等待商家接单', 'card status')
expect(await text(cust, `[data-order="${first}"] .ol-name b`), '马卡蒂小馆', 'store on the card')
step(`order ${first}: back from the order page leads to the list`)

// list -> order -> back is the list again, and back from the list leaves it
await cust.click(`[data-order="${first}"] .ol-body`)
await cust.waitForURL('**/o/**')
await cust.waitForSelector(tid('status'))
await cust.click(tid('order-back'))
await cust.waitForURL(u => u.pathname === '/orders')
await listReady(cust)
await cust.click(tid('orders-back'))
await cust.waitForURL(u => u.pathname.startsWith('/mall/s/makati'))
step('back works from the order page and from the list')

// ---- tabs and search ----
await finish(first)
const second = await order(['瓶装水'])
await cust.click(tid('order-back'))
await listReady(cust)
expect(await cust.locator('.ol-card').count(), 2, 'orders in the list')
expect(await cust.locator('.ol-card').first().getAttribute('data-order'), second, 'newest first')
expect(await text(cust, `[data-order="${first}"] ${tid('card-status')}`), '待评价', 'finished card status')
await shot(cust, 'o2-list-phone')
const only = async (tab, no) => {
  await cust.click(`[data-tab=${tab}]`)
  await listReady(cust)
  await cust.waitForFunction(n => document.querySelectorAll('.ol-card').length === n, no ? 1 : 0)
  if (no) expect(await cust.locator('.ol-card').getAttribute('data-order'), no, `tab ${tab}`)
  else await cust.locator(tid('orders-empty')).waitFor()
}
await only('active', second)
await only('review', first)
await only('aftersale', null)
await cust.click('[data-tab=all]')
await listReady(cust)
await cust.fill(tid('orders-search'), '卤肉')
await cust.waitForFunction(() => document.querySelectorAll('.ol-card').length === 1)
expect(await cust.locator('.ol-card').getAttribute('data-order'), first, 'search by dish')
await cust.fill(tid('orders-search'), '没有这个菜')
await cust.locator(tid('orders-empty')).waitFor()
await cust.fill(tid('orders-search'), '')
await cust.waitForFunction(() => document.querySelectorAll('.ol-card').length === 2)
step('tabs and search narrow the list')

// ---- a refund request, answered by the store ----
await cust.click(`[data-order="${first}"] ${tid('card-more')}`)
await cust.click(`[data-order="${first}"] .ol-menu button:has-text("申请退款")`)
await cust.waitForSelector(tid('aftersale-sheet'))
await cust.locator(`${tid('aftersale-sheet')} button.primary`).click()
await cust.locator(`${tid('aftersale-sheet')} .banner.bad`).waitFor()   // a reason is needed
await cust.click(`${tid('aftersale-sheet')} label:has-text("少送了商品")`)
await cust.fill(`${tid('aftersale-sheet')} textarea`, '袋子里没有冰柠檬茶')
await shot(cust, 'o3-refund-request')
await cust.locator(`${tid('aftersale-sheet')} button.primary`).click()
await cust.waitForSelector(`${tid('aftersale')}[data-status=open]`)
expect(await cust.locator(tid('act-aftersale')).count(), 0, 'one request per order')

await owner.goto(BASE + '/merchant/orders')
await owner.click(`.ord:has-text("${first}")`)   // an unanswered request keeps a finished order on the desk
await owner.locator(`${tid('aftersale-box')}:has-text("袋子里没有冰柠檬茶")`).waitFor()
await shot(owner, 'o4-desk-request')
await owner.click(tid('aftersale-resolve'))
await cust.waitForSelector(`${tid('aftersale')}[data-status=resolved]`, { timeout: 8000 })
if (!(await text(cust, tid('aftersale-reply'))).includes(REPLY)) throw new Error('the store\'s reply is missing')
await cust.click(tid('order-back'))
await listReady(cust)
await only('aftersale', first)
expect(await text(cust, `[data-order="${first}"] ${tid('card-status')}`), '售后已处理', 'card status after the answer')
step('a refund request reached the desk and the answer came back')

// ---- review from the list ----
await cust.click('[data-tab=all]')
await listReady(cust)
await cust.click(`[data-order="${first}"] ${tid('card-review')}`)
await cust.waitForSelector(tid('review-form'))
for (const k of ['rating', 'taste', 'packaging']) await cust.click(`[data-score=${k}] .starpick button:nth-child(5)`)
await cust.locator(`${tid('review-form')} button.primary`).click()
await cust.waitForSelector(tid('review-done'))
await shot(cust, 'o5-order-done')
await cust.click(tid('order-back'))
await listReady(cust)
expect(await cust.locator(`[data-order="${first}"] ${tid('card-review')}`).count(), 0, 'no review button once reviewed')
step('the order was reviewed from the list')

// ---- order the same again ----
await cust.click(`[data-order="${first}"] ${tid('card-again')}`)
await cust.waitForURL(u => u.pathname === '/mall/s/makati')
await cust.waitForSelector('.item')
expect(await text(cust, tid('cart-count')), '2', 'dishes back in the cart')
await cust.goBack()
await listReady(cust)
step('"order again" filled the cart of that store')

// ---- the same pages on a tablet and a computer ----
for (const [name, vp] of [['ipad', { width: 820, height: 1180 }], ['pc', { width: 1440, height: 900 }]]) {
  const p = await open(vp, cust.context())
  await p.goto(BASE + '/orders')
  await listReady(p)
  if (await p.evaluate(() => document.documentElement.scrollWidth > innerWidth)) throw new Error(`the list scrolls sideways on ${name}`)
  await shot(p, `o6-list-${name}`)
  await p.click(`[data-order="${first}"] .ol-body`)
  await p.waitForSelector(tid('order-fees'))
  if (await p.evaluate(() => document.documentElement.scrollWidth > innerWidth)) throw new Error(`the order page scrolls sideways on ${name}`)
  await shot(p, `o7-order-${name}`)
  await p.close()
}
step('list and order page fit a tablet and a computer')

if (errors.length) throw new Error('browser errors:\n' + errors.join('\n'))
console.log('ALL OK')
await browser.close()

})().catch(e => { console.error(e); process.exit(1) })
