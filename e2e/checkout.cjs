// Discounts at checkout and payment types (docs/CHECKOUT_DISCOUNT_DESIGN.md §5, §6):
// a table paid online with 10% off and a round-off, an order taker's discount
// approved by the owner's code, a staff delivery order paid online, the store's
// payment settings and the platform's payment methods.
//
// Usage: BASE_URL=http://localhost:8080 node e2e/checkout.cjs   (needs seed data)
const { chromium } = require('playwright')
const { mkdirSync } = require('node:fs')

;(async () => {
const BASE = process.env.BASE_URL || 'http://localhost:8080'
const SHOTS = process.env.SHOTS_DIR || 'e2e/shots'
mkdirSync(SHOTS, { recursive: true })
const browser = await chromium.launch()
const errors = []
async function open(viewport, admin = false) {
  const ctx = await browser.newContext({ viewport, locale: 'zh-CN', deviceScaleFactor: Number(process.env.DPR || 1), ignoreHTTPSErrors: process.env.E2E_INSECURE_TLS === '1' })
  const page = await ctx.newPage()
  page.on('pageerror', e => errors.push(`pageerror: ${e.message}`))
  page.on('console', m => { if (m.type() === 'error' && !/favicon|Failed to load resource/.test(m.text())) errors.push(`console: ${m.text()}`) })
  page.on('dialog', d => d.accept())
  page.admin = admin
  return page
}
const step = (s) => console.log('✓ ' + s)
const shot = (page, name) => page.screenshot({ path: `${SHOTS}/${name}.png` })
const tid = (id) => `[data-testid="${id}"]`
async function login(page, user, next = '/login') {
  await page.goto(BASE + next)
  await page.fill('input[autocomplete=username]', user)
  await page.fill('input[type=password]', user === 'admin' ? (process.env.ADMIN_PASSWORD || 'demo1234') : 'demo1234')
  await page.click('button.primary')
  await page.waitForURL(u => !/\/login$/.test(u.pathname))
}
const api = (page, method, path, body) => page.evaluate(async ([method, path, body, admin]) => {
  const r = await fetch('/api/v1' + path, { method, credentials: 'same-origin', headers: { 'X-Requested-With': 'waimai-web', 'X-Session': admin ? 'admin' : 'staff', 'Content-Type': 'application/json', 'Idempotency-Key': crypto.randomUUID() }, body: body ? JSON.stringify(body) : undefined })
  const j = await r.json(); if (!r.ok) throw new Error(path + ': ' + JSON.stringify(j)); return j
}, [method, path, body, page.admin])
const RICE = 4   // seed: 卤肉饭, no options

// a free table with a round of three braised pork rice, all served
async function tableWithFood(desk, kitchen) {
  const t = (await api(desk, 'GET', '/merchant/branches/1/tables')).tables.find(x => x.active && !x.session)
  if (!t) throw new Error('no free table; reseed the database')
  const bill = await api(desk, 'POST', `/merchant/tables/${t.id}/open`, { guests: 2, tags: [] })
  const b2 = await api(desk, 'POST', `/merchant/table-sessions/${bill.session_id}/batches`, { items: [{ item_id: RICE, qty: 3 }] })
  for (const it of b2.order.items) {
    if (it.needs_prep) await api(kitchen, 'POST', `/kitchen/items/${it.id}/ready`, {})
    await api(desk, 'POST', `/merchant/bill-items/${it.id}/serve`, {})
  }
  return { table: t.name, session: bill.session_id }
}
async function toBill(page, table) {
  await page.goto(BASE + '/merchant/orders/new?mode=dine')
  await page.waitForSelector(tid('dine-desk'))
  await page.click(tid(`dd-table-${table}`))
  await page.click(tid('dd-pay')).catch(() => null)
  await page.waitForSelector(tid('dd-pay-panel'))
}

// ---------- the owner: payment settings, an approval code ----------
const owner = await open({ width: 1280, height: 860 })
await login(owner, 'owner1')
await api(owner, 'PUT', '/auth/approve-pin', { pin: '246810', password: 'demo1234' })
await owner.goto(BASE + '/merchant/settings')
await owner.waitForSelector(tid('pay-settings'))
await owner.locator(tid('pay-set-channel')).first().waitFor()
const channels = await owner.locator(tid('pay-set-channel')).allInnerTexts()
if (!channels.some(x => /GCash/.test(x))) throw new Error('settings list no GCash: ' + channels)
await owner.locator(tid('pay-settings')).screenshot({ path: `${SHOTS}/ck01-pay-settings.png` })
step(`the store's payment methods list the country's online methods: ${channels.join(', ')}`)

const kitchen = await open({ width: 1280, height: 860 })
await login(kitchen, 'kitchen1', '/login?next=/work')

// ---------- a table: 10% off, a round-off, paid by GCash ----------
const pad = owner
await pad.setViewportSize({ width: 1180, height: 820 })
const t1 = await tableWithFood(pad, kitchen)
await toBill(pad, t1.table)
const due0 = await pad.locator(tid('dd-due')).innerText()
await pad.click(tid('dd-adj'))
await pad.waitForSelector(tid('adj-sheet'))
await pad.locator(tid('adj-pct')).nth(1).click()          // 9 折
await pad.click(tid('adj-mode-round'))
await pad.locator(tid('adj-round')).first().waitFor()
await pad.locator(tid('adj-reason')).first().click()       // 熟客
await pad.waitForFunction(() => /→/.test(document.querySelector('[data-testid=adj-summary]')?.textContent || ''))
await pad.waitForTimeout(400)
await shot(pad, 'ck02-adjust-sheet')
await pad.click(tid('adj-apply'))
await pad.waitForSelector(`${tid('dd-adj')}.set`)
const due1 = await pad.locator(tid('dd-due')).innerText()
if (due0 === due1) throw new Error('the amount to pay did not change: ' + due1)
await pad.click(tid('pay-online'))
await pad.locator(`${tid('pay-channel')}:has-text("GCash")`).click()
await shot(pad, 'ck03-dine-online')
await pad.click(tid('dd-close'))
await pad.waitForSelector(tid('dd-closed'))
await shot(pad, 'ck04-dine-closed')
const c1 = (await api(pad, 'GET', `/merchant/table-sessions/${t1.session}`)).order
if (c1.pay_type !== 'online' || c1.pay_method !== 'GCash' || c1.totals.adj_percent_bp !== 9000 || !c1.totals.staff_discount || !c1.totals.round_off
  || c1.amount_received !== c1.totals.total || c1.adj_reason !== '熟客')
  throw new Error('closed bill: ' + JSON.stringify({ pay: [c1.pay_type, c1.pay_method], t: c1.totals, r: c1.adj_reason, got: c1.amount_received }))
await pad.click(tid('dd-done'))
step(`table ${t1.table}: ${due0} → 9 折 and a round-off → ${due1.replace(due0, '')}, paid by GCash`)

// ---------- an order taker: the owner's code approves the discount ----------
const desk = await open({ width: 1180, height: 820 })
await login(desk, 'cashier1')
const t2 = await tableWithFood(desk, kitchen)
await toBill(desk, t2.table)
await desk.click(tid('dd-adj'))
await desk.waitForSelector(tid('approve-dialog'))
await desk.fill(tid('approve-pin'), '246810')
await desk.click(tid('approve-go'))
await desk.waitForSelector(tid('adj-sheet'))
await desk.click(tid('adj-mode-set'))
await desk.fill(tid('adj-set-input'), '100')
await desk.locator(tid('adj-reason')).nth(3).click()       // 投诉补偿
await desk.waitForFunction(() => /→ ₱100/.test(document.querySelector('[data-testid=adj-summary]')?.textContent || ''))
await desk.click(tid('adj-apply'))
await desk.locator(tid('dd-due') + ':has-text("₱100")').waitFor()
await desk.locator(tid('dd-quick')).first().click()
await desk.locator(tid('dd-change')).waitFor()
await shot(desk, 'ck05-cash-change')
await desk.click(tid('dd-close'))
await desk.waitForSelector(tid('dd-closed'))
const c2 = (await api(desk, 'GET', `/merchant/table-sessions/${t2.session}`)).order
if (c2.totals.total !== 10000 || c2.totals.adj_mode !== 'set_total' || c2.pay_type !== 'cash' || !c2.adj_approved_by || c2.amount_received <= 10000)
  throw new Error('approved bill: ' + JSON.stringify({ t: c2.totals, pay: c2.pay_type, by: [c2.adj_by, c2.adj_approved_by], got: c2.amount_received }))
await desk.click(tid('dd-done'))
step(`order taker set the amount to pay to ₱100 with ${c2.adj_approved_by}'s code; cash with change`)

// ---------- a delivery order paid online before it was placed ----------
await desk.goto(BASE + '/merchant/orders/new')
await desk.waitForSelector(tid('manual-order'))
await desk.fill(tid('mo-pick'), 'b1')
await desk.waitForSelector(`${tid('mo-pick-drop')} .it.on:has-text("卤肉饭")`)
await desk.press(tid('mo-pick'), 'Enter')
await desk.click(`${tid('mo-line')}:has-text("卤肉饭") ${tid('mo-plus')}`)
await desk.click(tid('mo-new-member'))
await desk.waitForSelector(tid('mo-temp'))
await desk.fill(tid('mo-phone'), '917555' + String(Math.floor(Math.random() * 1e4)).padStart(4, '0'))
await desk.fill(tid('mo-name'), '王先生')
await desk.click(tid('mo-src-wechat'))
await desk.fill(tid('mo-address'), '2621 Zamora St, Pasay')
await desk.click(tid('mo-name'))
const zone = desk.locator(tid('mo-zone'))
if (await zone.count()) await zone.selectOption({ index: 1 })
await desk.waitForFunction(() => /₱/.test(document.querySelector('[data-testid=mo-total]')?.textContent || ''))
await desk.selectOption(tid('mo-pay'), { label: '已在线付款 · GCash' })
await desk.click(tid('mo-adj'))
await desk.waitForSelector(tid('approve-dialog'))     // the last approval was spent on the table
await desk.fill(tid('approve-pin'), '246810')
await desk.click(tid('approve-go'))
await desk.waitForSelector(tid('adj-sheet'))
await desk.locator(tid('adj-pct')).nth(1).click()
await desk.waitForFunction(() => /→/.test(document.querySelector('[data-testid=adj-summary]')?.textContent || ''))
await desk.click(tid('adj-apply'))
await desk.waitForSelector(tid('mo-adj-amt'))
await desk.waitForTimeout(400)
await shot(desk, 'ck06-manual-order')
await desk.click(tid('mo-place'))
await desk.waitForSelector(tid('mo-done'), { timeout: 8000 }).catch(async e => {
  await shot(desk, 'ck-fail'); throw new Error('placing: ' + (await desk.locator('.banner.bad, .err, [data-testid=mo-error]').allInnerTexts()).join(' | '))
})
const no = (await desk.locator(`${tid('mo-done')} h2`).innerText()).match(/[A-Z][0-9-]+/)[0]
const recent = await api(desk, 'GET', '/merchant/branches/1/orders')
const o3 = (recent.orders ?? recent).find(o => o.public_no === no)
if (!o3 || o3.payment_status !== 'paid' || o3.pay_type !== 'online' || o3.pay_method !== 'GCash' || o3.totals.adj_percent_bp !== 9000 || !o3.adj_approved_by)
  throw new Error('prepaid order: ' + JSON.stringify(o3 && { pay: [o3.payment_status, o3.pay_type, o3.pay_method], t: o3.totals, by: o3.adj_approved_by }))
await desk.keyboard.press('Escape')
await desk.goto(BASE + '/merchant/orders?order=' + o3.id)
await desk.waitForSelector(tid('adj-row'))
await desk.waitForSelector(tid('pay-row'))
await desk.waitForSelector(tid('staff-discount-line'))
await shot(desk, 'ck07-order-detail')
step(`delivery order ${no}: 9 折 with approval, already paid by GCash`)

// ---------- the owner reconciles the day ----------
await owner.setViewportSize({ width: 1280, height: 860 })
await owner.goto(BASE + '/merchant/reconcile')
await owner.waitForSelector(tid('rc-rows'))
await owner.locator('[data-testid=rc-rows] tr[data-method="GCash"]').waitFor()
await owner.fill(tid('rc-counted'), '100')
await owner.locator(tid('rc-diff')).waitFor()
await owner.click(tid('rc-count-save'))
await owner.waitForSelector(tid('rc-counted-by'))
await shot(owner, 'ck09-reconcile-sum')
await owner.click(tid('rc-tab-online'))
await owner.waitForSelector(tid('rc-online'))
const first = owner.locator(`[data-testid=rc-online] tr[data-order="${no}"]`)
await first.locator(tid('rc-bad')).click()
await owner.fill(tid('rc-bad-note'), '没收到，联系顾客')
await owner.click(tid('rc-bad-save'))
await owner.waitForSelector(tid('rc-problems'))
await owner.locator(tid('rc-mark-all')).click()
await owner.waitForFunction(() => !document.querySelector('[data-testid=rc-mark-all]'))
await shot(owner, 'ck10-reconcile-online')
const o4 = await api(owner, 'GET', `/merchant/orders/${o3.id}`)
if (o4.pay_check !== 'problem' || o4.pay_check_note !== '没收到，联系顾客') throw new Error('order check: ' + o4.pay_check)
await owner.click(tid('rc-tab-disc'))
await owner.waitForSelector(tid('rc-by-staff'))
await owner.locator(`[data-testid=rc-disc] tr[data-order="${no}"]`).waitFor()
await shot(owner, 'ck11-reconcile-discounts')
step('the owner counted the cash, marked GCash payments arrived or wrong, and saw who gave discounts')

// ---------- the platform's payment methods ----------
const adm = await open({ width: 1280, height: 860 }, true)
await login(adm, 'admin', '/admin/login')
await adm.goto(BASE + '/admin/pay-methods')
await adm.locator('[data-cc="PH"]').click()
await adm.locator('tr[data-channel="GCash"]').waitFor()
await shot(adm, 'ck08-admin-pay-methods')
step('the platform lists online methods by country')

await browser.close()
if (errors.length) { console.error(errors.join('\n')); process.exit(1) }
console.log('checkout e2e passed')
})().catch(e => { console.error(e); process.exit(1) })
