// The dine-in desk (docs/DINE_IN_ORDER_DESIGN.md): staff on an iPad and on a
// phone open tables, order, send rounds, rush, serve, void, add one more,
// move a table and take payment, by tapping only. No text box may get the
// focus during the whole service.
//
// Usage: BASE_URL=http://localhost:8080 node e2e/dinedesk.cjs   (needs seed data)
const { chromium } = require('playwright')
const { mkdirSync } = require('node:fs')

;(async () => {

const BASE = process.env.BASE_URL || 'http://localhost:8080'
const SHOTS = process.env.SHOTS_DIR || 'e2e/shots'
mkdirSync(SHOTS, { recursive: true })
const browser = await chromium.launch()
const errors = []

async function open(viewport, touch = true) {
  const ctx = await browser.newContext({ viewport, locale: 'zh-CN', deviceScaleFactor: 2, hasTouch: touch, ignoreHTTPSErrors: process.env.E2E_INSECURE_TLS === '1' })
  // count every time a text box gets the focus
  await ctx.addInitScript(() => {
    window.__typed = []
    document.addEventListener('focusin', e => { if (e.target.matches('input:not([type=checkbox]):not([type=radio]), textarea')) window.__typed.push(location.pathname) }, true)
  })
  const page = await ctx.newPage()
  page.on('pageerror', e => errors.push(`pageerror: ${e.message}`))
  page.on('console', m => { if (m.type() === 'error' && !/favicon|Failed to load resource/.test(m.text())) errors.push(`console: ${m.text()}`) })
  page.on('dialog', d => { errors.push(`dialog: ${d.message()}`); d.dismiss() })
  return page
}
const step = (s) => console.log('✓ ' + s)
const shot = (page, name) => page.screenshot({ path: `${SHOTS}/${name}.png` })
const tid = (id) => `[data-testid="${id}"]`
async function login(page, user, next = '/login') {
  await page.goto(BASE + next)
  await page.fill('input[autocomplete=username]', user)
  await page.fill('input[type=password]', 'demo1234')
  await page.click('button.primary')
  await page.waitForURL(u => !/\/login$/.test(u.pathname))
  await page.evaluate(() => (window.__typed = []))
}
const api = (page, method, path, body) => page.evaluate(async ([method, path, body]) => {
  const r = await fetch('/api/v1' + path, { method, credentials: 'same-origin', headers: { 'X-Requested-With': 'waimai-web', 'X-Session': 'staff', 'Content-Type': 'application/json' }, body: body ? JSON.stringify(body) : undefined })
  const j = await r.json(); if (!r.ok) throw new Error(path + ': ' + JSON.stringify(j)); return j
}, [method, path, body])
const noTyping = async (page, what) => {
  const typed = await page.evaluate(() => window.__typed)
  if (typed.length) throw new Error(`${what}: a text box got the focus ${typed.length} times`)
}
const expectText = async (loc, re, what) => { const s = await loc.innerText(); if (!re.test(s)) throw new Error(`${what}: "${s}" does not match ${re}`) }
const BEEF = 5, RICE = 4, TEA = 9   // seed: 牛肉面 (spice required + flavour notes), 卤肉饭, 珍珠奶茶 (notes)

// ---------- iPad, landscape ----------
const pad = await open({ width: 1180, height: 820 })
await login(pad, 'cashier1')
await pad.goto(BASE + '/merchant/orders/new')
await pad.waitForSelector(tid('manual-order'))
await pad.click(tid('mode-dine'))
await pad.waitForSelector(tid('dine-desk'))
await pad.locator(tid('dd-area')).first().waitFor()
await pad.locator('[data-testid^="dd-table-"]').first().waitFor()
// free tables for this run: A and B in the hall (A moves to B), P a private room for the phone
const free = (await api(pad, 'GET', '/merchant/branches/1/tables')).tables.filter(x => x.active && !x.session)
const hall = free.filter(x => x.area === '大厅')
const A = hall[0]?.name, B = hall[1]?.name, P = free.find(x => x.area === '包间')?.name
if (!A || !B || !P) throw new Error('needs two free hall tables and a free private room; reseed the database')
await shot(pad, 'dd01-tables')
step('the 堂食 switch opens the table map, with the hall, rooms and terrace as tabs')

// open 01 for 3 guests with a table note
await pad.click(tid(`dd-table-${A}`))
await pad.waitForSelector(tid('dd-open'))
await pad.click(tid('dd-guests-3'))
await pad.locator(`${tid('dd-open')} .dd-chs .dd-ch:has-text("有小孩")`).click()
await shot(pad, 'dd02-open')
await pad.click(tid('dd-open-order'))
await pad.waitForSelector(tid('dd-round'))
step(`opened table ${A}: three guests, "有小孩"`)

// beef noodles: the sheet will not add it before a spice level is picked
const cat = (page, name) => page.locator(`[data-testid^="dd-cat-"]:has-text("${name}")`).click()
await cat(pad, '主食')
await pad.click(tid(`dd-dish-${BEEF}`))
await pad.waitForSelector(tid('dd-sheet'))
if (await pad.locator(tid('dd-sheet-add')).isEnabled()) throw new Error('the beef noodles were addable without a spice level')
await pad.locator(tid('dd-opt')).first().click()
await pad.locator(`${tid('dd-sheet')} ${tid('note-tag')}`).first().click()
await shot(pad, 'dd03-sheet')
await pad.click(tid('dd-sheet-add'))
await pad.click(tid(`dd-dish-${RICE}`))
await pad.click(tid(`dd-dish-${RICE}`))
await pad.locator(tid('dd-round-tag')).first().click()
await expectText(pad.locator(tid('dd-send')), /3/, 'send button')
await shot(pad, 'dd04-round')
await pad.click(tid('dd-send'))
await pad.waitForSelector(tid('dd-toast'))
await pad.locator(`${tid(`dd-table-${A}`)}:not(.free)`).waitFor()
step('round 1 sent (beef noodles with spice and a note, two braised pork rice, a whole-round note); back on the map')

// the side panel shows the table; rush the noodles from the bill
await pad.click(tid(`dd-table-${A}`))
await pad.locator(`${tid('dd-side')} .dd-mini`).first().waitFor()
await shot(pad, 'dd05-side')
await pad.click(tid('dd-bill'))
await pad.waitForSelector(tid('dd-bill-list'))
const beefRow = pad.locator(`${tid('dd-bill-item')}:has-text("牛肉面")`).first()
// a tap on a line opens its actions, a second tap closes them
const pickRow = async row => { await row.click(); if (!await pad.locator(tid('dd-again')).isVisible()) await row.click() }
await pickRow(beefRow)
await pad.click(tid('dd-rush'))
await pad.locator(`${tid('dd-bill-item')}:has-text("牛肉面") .dd-st.rush`).waitFor()
step('rushed the beef noodles')

// the kitchen sees the rush, the round note and the table note; it gets everything ready
const kitchen = await open({ width: 1280, height: 860 }, false)
await login(kitchen, 'kitchen1', '/login?next=/work')
await kitchen.goto(BASE + '/kitchen')
const card = kitchen.locator(`.card:has-text("桌 ${A}")`).first()
await card.waitFor()
const stationBtns = kitchen.locator('.btn.small').filter({ hasText: '·' })
let sawRush = false, sawRound = false, sawTableNote = false
for (let i = 0; i < await stationBtns.count(); i++) {
  await stationBtns.nth(i).click()
  if (await card.count()) {
    sawRush ||= await card.locator(tid('k-rush')).count() > 0
    sawRound ||= await card.locator(tid('k-round')).count() > 0
    sawTableNote ||= (await card.locator('h2').innerText()).includes('有小孩')
  }
}
if (!sawRush || !sawRound || !sawTableNote) throw new Error(`kitchen marks: rush ${sawRush}, round ${sawRound}, table note ${sawTableNote}`)
await shot(kitchen, 'dd06-kitchen')
const bill01 = await api(pad, 'GET', `/merchant/table-sessions/${(await api(pad, 'GET', '/merchant/branches/1/tables')).tables.find(x => x.name === A).session.id}`)
for (const it of bill01.order.items) if (it.needs_prep) await api(kitchen, 'POST', `/kitchen/items/${it.id}/ready`, {})
step('the kitchen screen shows 催菜, the whole-round note and the table note; all dishes ready')

// serve the noodles, void one rice, add one more noodles
await pickRow(beefRow)
await pad.click(tid('dd-serve'))
await pad.locator(`${tid('dd-bill-item')}:has-text("牛肉面") .dd-st.served`).waitFor()
await pickRow(pad.locator(`${tid('dd-bill-item')}:has-text("卤肉饭")`))
await pad.click(tid('dd-void'))
await pad.waitForSelector(tid('void-sheet'))
await pad.waitForSelector(tid('void-stage'))
await pad.locator(tid('void-reason')).first().click()
await shot(pad, 'dd07-void')
await pad.click(tid('void-ok'))
await pad.locator(`${tid('dd-bill-item')}.void`).waitFor()
await expectText(pad.locator(`${tid('dd-bill-item')}:has-text("卤肉饭"):not(.void)`), /×1/, 'rice left after voiding one')
step('served the noodles; voided one of the two rice with a tapped reason')

await pickRow(beefRow)
await pad.click(tid('dd-again'))
await pad.waitForSelector(tid('dd-round'))
await pad.locator(`${tid('dd-line')}:has-text("牛肉面")`).waitFor()
await pad.click(tid('dd-send'))
await pad.waitForSelector(tid('dd-toast'))
step('"one more" put the same noodles (spice and note) into round 2 and sent it')

// move 01 to 03, then pay with cash and change
await pad.click(tid(`dd-table-${A}`))
await pad.click(tid('dd-move'))
await pad.waitForSelector(tid('dd-move-pick'))
await shot(pad, 'dd08-move')
await pad.click(tid(`dd-move-${B}`))
await pad.locator(`${tid(`dd-table-${B}`)}:not(.free)`).waitFor()
await pad.locator(`${tid(`dd-table-${A}`)}.free`).waitFor()
step(`moved table ${A} to ${B}; ${A} is free again`)

const bill03 = await api(pad, 'GET', `/merchant/table-sessions/${(await api(pad, 'GET', '/merchant/branches/1/tables')).tables.find(x => x.name === B).session.id}`)
const round2 = bill03.order.items.filter(i => i.batch_seq === 2)
if (round2.length !== 1 || round2[0].qty !== 1 || round2[0].round_tags?.length) throw new Error('round 2 should be one noodles and nothing else: ' + JSON.stringify(round2.map(i => [i.name, i.qty, i.round_tags])))
for (const it of bill03.order.items) {
  if (it.prep_status === 'cancelled' || it.served_at) continue
  if (it.needs_prep && it.prep_status !== 'ready') await api(kitchen, 'POST', `/kitchen/items/${it.id}/ready`, {})
  await api(pad, 'POST', `/merchant/bill-items/${it.id}/serve`, {})
}
await pad.click(tid(`dd-table-${B}`))
await pad.click(tid('dd-pay'))
await pad.waitForSelector(tid('dd-pay-panel'))
await pad.locator(tid('dd-quick')).last().click()
await expectText(pad.locator(tid('dd-change')), /\d/, 'change')
await shot(pad, 'dd09-pay')
await pad.click(tid('dd-close'))
await pad.waitForSelector(tid('dd-closed'))
await shot(pad, 'dd10-closed')
await pad.click(tid('dd-done'))
await pad.locator(`${tid(`dd-table-${B}`)}.free`).waitFor()
const closed = await api(pad, 'GET', `/merchant/table-sessions/${bill03.session_id}`)
if (closed.status !== 'closed' || closed.order.pay_method !== '现金' || closed.order.amount_received <= closed.order.totals.total)
  throw new Error(`closed bill: ${closed.status} ${closed.order.pay_method} ${closed.order.amount_received}/${closed.order.totals.total}`)
await noTyping(pad, 'iPad')
step(`paid in cash with change and closed table ${B}; the iPad never focused a text box`)

// ---------- phone ----------
const phone = await open({ width: 390, height: 844 })
await login(phone, 'cashier1', '/login?next=' + encodeURIComponent('/merchant/orders/new?mode=dine'))
await phone.goto(BASE + '/merchant/orders/new?mode=dine')
await phone.waitForSelector(tid('dine-desk'))
await phone.click(tid(`dd-table-${P}`))
await phone.waitForSelector(tid('dd-open'))
await shot(phone, 'dd11-phone-open')
await phone.click(tid('dd-open-order'))
await phone.waitForSelector(tid('dd-send-bar'))
await cat(phone, '饮品')
await phone.click(tid(`dd-add-${TEA}`))
await phone.waitForSelector(tid('dd-sheet'))
await phone.locator(`${tid('dd-sheet')} ${tid('note-tag')}`).first().click()
await phone.click(tid('dd-sheet-add'))
await cat(phone, '主食')
await phone.click(tid(`dd-add-${RICE}`))
await shot(phone, 'dd12-phone-order')
await phone.click(tid('dd-open-round'))
await phone.locator(tid('dd-line')).nth(1).waitFor()
await shot(phone, 'dd13-phone-round')
await phone.click(tid('dd-send'))
await phone.waitForSelector(tid('dd-toast'))
await phone.locator(`${tid(`dd-table-${P}`)}:not(.free)`).waitFor()
await noTyping(phone, 'phone')
step(`on a phone: opened private room ${P}, ordered tea with a note and rice, sent; no text box focused`)

await browser.close()
if (errors.length) { console.error(errors.join('\n')); process.exit(1) }
console.log('DINE DESK E2E OK')
})().catch(e => { console.error(e); process.exit(1) })
