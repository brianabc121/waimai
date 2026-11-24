// Manager approval (docs/STAFF_ACCESS_DESIGN.md §7) in a real browser: the
// owner takes "cancel orders" away from order takers and sets an approval
// code; at the counter the order taker asks for approval, a wrong code is
// refused, the right one cancels the order once.
// Usage: BASE_URL=http://localhost:8080 node e2e/approve.cjs   (needs seed data)
const { chromium } = require('playwright')
const { mkdirSync } = require('node:fs')

;(async () => {

const BASE = process.env.BASE_URL || 'http://localhost:8080'
const SHOTS = process.env.SHOTS_DIR || 'e2e/shots'
mkdirSync(SHOTS, { recursive: true })
const browser = await chromium.launch()
const errors = []
async function open(viewport = { width: 1280, height: 860 }) {
  const ctx = await browser.newContext({ viewport, locale: 'zh-CN', ignoreHTTPSErrors: process.env.E2E_INSECURE_TLS === '1' })
  const page = await ctx.newPage()
  page.on('pageerror', e => errors.push(`pageerror: ${e.message}`))
  page.on('console', m => { if (m.type() === 'error' && !/favicon|Failed to load resource/.test(m.text())) errors.push(`console: ${m.text()}`) })
  return page
}
const step = (s) => console.log('✓ ' + s)
const shot = (page, name) => page.screenshot({ path: `${SHOTS}/${name}.png` })
const tid = (s) => `[data-testid=${s}]`
// Sign-in is rate limited per address (10 a minute): wait out a refusal.
async function signIn(page, user) {
  await page.goto(BASE + '/login')
  for (let i = 0; ; i++) {
    await page.fill('input[autocomplete=username]', user)
    await page.fill('input[type=password]', 'demo1234')
    const res = page.waitForResponse(r => /\/auth\/login$/.test(r.url()))
    await page.click('button.primary')
    if ((await res).status() !== 429 || i >= 6) break
    await page.waitForTimeout(7000)
  }
  await page.waitForURL('**/merchant/orders')
}

// ---- the owner: no cancelling for order takers, and an approval code ----
const owner = await open()
await signIn(owner, 'owner1')
await owner.goto(BASE + '/merchant/staff/roles')
const cancelBox = owner.locator('tr[data-perm="order.cancel"] input[data-role=cashier]')
if (await cancelBox.isChecked()) {
  await cancelBox.click()
  await owner.click(tid('perm-save'))
  await owner.locator(tid('perm-notice')).waitFor()
}
await owner.click(tid('pin-open'))
await owner.fill(tid('pin-new'), '246810')
await owner.fill(tid('pin-password'), 'demo1234')
await owner.click(tid('pin-save'))
await owner.locator(tid('pin-saved')).waitFor()
await owner.locator(tid('pin-state') + ':has-text("已设置")').waitFor()
await shot(owner, 'a01-pin')
step('the owner took cancelling away from order takers and set an approval code')

// ---- a customer orders for pickup, the desk accepts it ----
const cust = await open({ width: 390, height: 780 })
await cust.goto(BASE + '/account/login')
await cust.fill(tid('login-phone'), '9' + String(Math.floor(Math.random() * 1e9)).padStart(9, '0'))
await cust.check(tid('login-agree'))
await cust.click(tid('login-send'))
await cust.waitForURL(u => u.pathname !== '/account/login')
const orderNo = await cust.evaluate(async () => {
  const call = (path, body) => fetch('/api/v1' + path, { method: 'POST', credentials: 'same-origin',
    headers: { 'X-Requested-With': 'e2e', 'Content-Type': 'application/json', 'Idempotency-Key': 'ap-' + Date.now() }, body: JSON.stringify(body) }).then(r => r.json())
  const store = await fetch('/api/v1/stores/makati').then(r => r.json())
  const item = store.categories.flatMap(c => c.items).find(i => !i.sold_out && i.stock_qty == null && !(i.groups || []).length)
  const q = await call('/stores/makati/quotes', { service_type: 'pickup', items: [{ item_id: item.id, qty: 1 }] })
  const o = await call('/stores/makati/orders', { quote_id: q.id, customer_name: '李小红' })
  return o.public_no || JSON.stringify(o.error || q)
})
if (!/^[A-Z0-9-]+$/i.test(orderNo)) throw new Error('placing the order: ' + orderNo)

const desk = await open()
await signIn(desk, 'cashier1')
await desk.click(`.ord:has-text("${orderNo}")`)
await desk.click(`.detail:has-text("${orderNo}") button:has-text("接单")`)
await desk.locator(tid('cancel-locked')).waitFor()
step('the order taker accepted the order and sees cancelling locked')

// ---- approval: a wrong code, then the right one ----
await desk.click(tid('cancel-locked'))
await desk.locator(tid('approve-dialog')).waitFor()
await desk.locator('[data-approver] .rtag.owner').waitFor()
await desk.fill(tid('approve-pin'), '111111')
await desk.click(tid('approve-go'))
await desk.locator(tid('approve-error') + ':has-text("还能试 4 次")').waitFor()
await shot(desk, 'a02-approve')
desk.once('dialog', d => d.accept('顾客来电取消'))
await desk.fill(tid('approve-pin'), '246810')
await desk.click(tid('approve-go'))
await desk.locator(tid('approved-by')).waitFor()
// the cancelled order leaves the active list and shows as cancelled in the history
await desk.waitForFunction(no => ![...document.querySelectorAll('.ord')].some(e => e.textContent.includes(no)), orderNo)
await desk.click('button:has-text("历史")')
await desk.locator(`.ord:has-text("${orderNo}") >> text=已取消`).first().waitFor()
await shot(desk, 'a03-approved')
step('a wrong code is refused; with the right one the order is cancelled once')

// put the table back for the other tests
await owner.goto(BASE + '/merchant/staff/roles')
await owner.click(tid('perm-reset'))
if (await owner.locator(tid('perm-save')).isEnabled()) { await owner.click(tid('perm-save')); await owner.locator(tid('perm-notice')).waitFor() }

if (errors.length) throw new Error('page errors:\n' + errors.join('\n'))
await browser.close()
console.log('approve e2e passed')
})().catch(e => { console.error(e); process.exit(1) })
