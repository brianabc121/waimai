// Entering an order from a delivery platform (docs/ORDER_TYPES_DESIGN.md §7)
// in a real browser: the 平台下单 page under 点单, the dishes at Grab prices,
// the two platform discounts, the check against the platform's amount, the
// commission and the expected payout; then the platform block on the order
// with another charge, a deduction and a typed commission.
// Hand numbers (seed data, Grab +20%, rounded to whole 1, commission 25% of the
// dishes after the store's discount):
//   卤肉饭 159.00 → 190.80 → 191.00, ×2 = 382.00; 冰柠檬茶 69.00 → 82.80 → 83.00
//   subtotal 465.00; store-funded 20.00, platform-funded 10.00 → customer paid 435.00
//   receivable 435.00 + 10.00 = 445.00; commission 25% × (465.00 − 20.00) = 111.25
//   payout 445.00 − 111.25 = 333.75
//   + other charge 5.00 → 328.75; platform takes back 36.00 → 292.75
//   commission typed 100.00 → 445.00 − 36.00 − 100.00 − 5.00 = 304.00; back to auto → 292.75
// Usage: BASE_URL=http://localhost:8080 node e2e/platformorder.cjs   (after e2e/platforms.cjs)
const { chromium } = require('playwright')
const { mkdirSync } = require('node:fs')

;(async () => {
const BASE = process.env.BASE_URL || 'http://localhost:8080'
const SHOTS = process.env.SHOTS_DIR || 'e2e/shots'
mkdirSync(SHOTS, { recursive: true })
const browser = await chromium.launch()
const errors = []
async function open(viewport, extra = {}) {
  const ctx = await browser.newContext({ viewport, locale: 'zh-CN', ...extra })
  const page = await ctx.newPage()
  page.on('pageerror', e => errors.push(`pageerror: ${e.message}`))
  page.on('console', m => { if (m.type() === 'error' && !/favicon|Failed to load resource/.test(m.text())) errors.push(`console: ${m.text()}`) })
  page.on('dialog', d => d.accept())
  return page
}
async function login(page, user) {
  await page.goto(BASE + '/login')
  await page.fill('input[autocomplete=username]', user)
  await page.fill('input[type=password]', 'demo1234')
  await page.click('button.primary')
  await page.waitForURL(u => !/\/login$/.test(u.pathname))
}
const tid = s => `[data-testid="${s}"]`
const step = s => console.log('✓ ' + s)
const shot = (p, n) => p.screenshot({ path: `${SHOTS}/${n}.png`, fullPage: false })
const money = s => Number(s.replace(/[^0-9.]/g, ''))
async function want(page, sel, v, what) {
  for (let i = 0; i < 40; i++) {
    const s = await page.locator(sel).innerText().catch(() => '')
    if (s && money(s) === v) return
    await page.waitForTimeout(150)
  }
  throw new Error(`${what}: want ${v}, got ${await page.locator(sel).innerText().catch(() => '?')}`)
}

// ---- the store's contract with Grab: 20% markup, 25% commission ----
const page = await open({ width: 1366, height: 900 })
await login(page, 'owner1')
await page.goto(BASE + '/merchant/settings')
const card = page.locator(tid('platforms-card'))
await card.waitFor()
const on = card.locator(tid('pf-on-Grab'))
if (!(await on.isChecked())) await on.check()
await card.locator(tid('pf-markup-Grab')).fill('20')
await card.locator(tid('pf-commission-Grab')).fill('25')
await card.locator(tid('pf-save')).click()
await card.locator('.banner.ok').waitFor()
step('Grab: 20% markup, 25% commission')

// ---- 点单 · 平台下单 ----
if (!(await page.locator(tid('nav-pos-platform')).isVisible())) await page.click(tid('nav-pos'))
await page.click(tid('nav-pos-platform'))
await page.waitForURL(/\/merchant\/order\/platform$/)
await page.locator(tid('po-platform') + '[data-platform=Grab]').click()
const no = 'GF-' + String(Date.now()).slice(-6)
await page.fill(tid('po-no'), no)
const dish = page.locator(tid('po-dish'))
async function add(code) {
  await dish.fill(code)
  await page.locator(tid('po-hits')).waitFor()
  await dish.press('Enter')
  if (await page.locator(tid('po-spec')).isVisible().catch(() => false)) await page.click(tid('po-spec-add'))
}
await add('b1')
await page.locator(tid('po-plus')).first().click()
await add('d1')
await want(page, tid('po-subtotal'), 465, 'subtotal at Grab prices')
const units = await page.locator(tid('po-unit')).allInnerTexts()
if (money(units[0]) !== 191 || money(units[1]) !== 83) throw new Error('Grab unit prices ' + units)
step('dishes at Grab prices: 191.00 × 2 + 83.00 = 465.00')
await page.fill(tid('po-merchant'), '20')
await page.fill(tid('po-platform-promo'), '10')
await want(page, tid('po-total'), 435, 'customer paid')
await want(page, tid('po-commission'), 111.25, 'commission')
await want(page, tid('po-payout'), 333.75, 'payout')
step('discounts 20.00 + 10.00: paid 435.00, commission 111.25, payout 333.75')
await page.fill(tid('po-check'), '465')
await page.locator(tid('po-check-ok')).waitFor()
await page.fill(tid('po-check'), '471')
await page.locator(tid('po-check-diff')).waitFor()
if (!/差 \D*6(\.00)?，/.test(await page.locator(tid('po-check-diff')).innerText())) throw new Error('check diff: ' + await page.locator(tid('po-check-diff')).innerText())
step('check against the tablet: same, then off by 6.00')
// a typed commission needs a reason; back to auto afterwards
await page.click(tid('po-edit-commission'))
await page.fill(tid('po-commission-in'), '100')
await want(page, tid('po-payout'), 345, 'payout with typed commission')
await page.click(tid('po-confirm'))
await page.locator(tid('po-error')).waitFor()
await page.click('button:has-text("恢复自动")')
await want(page, tid('po-payout'), 333.75, 'payout back to auto')
step('typed commission needs a reason; back to auto')
await shot(page, 'po-01-page')
await page.click(tid('po-confirm'))
await page.locator(tid('po-done')).waitFor()
const doneText = await page.locator(tid('po-done')).innerText()
if (!doneText.includes('Grab ' + no) || !/435(\.00)?/.test(doneText)) throw new Error('done: ' + doneText)
await shot(page, 'po-02-done')
step(`placed: Grab ${no}`)

// the same number again the same day is caught
await page.click(tid('po-next'))
await page.fill(tid('po-no'), no)
await add('d3')
await page.locator(tid('po-dup')).waitFor()
step('the same platform number is flagged as entered already')

// ---- the order: platform block ----
await page.locator(tid('po-dup') + ' a').click()
const blk = page.locator(tid('platform-block'))
await blk.waitFor()
if ((await blk.locator(tid('pb-no')).innerText()) !== no) throw new Error('platform no')
await want(page, tid('pb-payout'), 333.75, 'order payout')
const tag = page.locator('.order-card.sel, .sel').locator(tid('ot-tag')).first()
await blk.locator(tid('pb-add-fee')).click()
await blk.locator(tid('pb-amount')).fill('5')
await blk.locator(tid('pb-reason')).fill('广告费')
await blk.locator(tid('pb-save')).click()
await want(page, tid('pb-payout'), 328.75, 'payout after a charge')
await blk.locator(tid('pb-deduct')).click()
await blk.locator(tid('pb-amount')).fill('36')
await blk.locator(tid('pb-reason')).fill('顾客投诉漏菜')
await blk.locator(tid('pb-save')).click()
await want(page, tid('pb-payout'), 292.75, 'payout after a deduction')
await want(page, tid('pb-deducted'), 36, 'deducted')
await blk.locator(tid('pb-edit-commission')).click()
await blk.locator(tid('pb-amount')).fill('100')
await blk.locator(tid('pb-reason')).fill('合同约定')
await blk.locator(tid('pb-save')).click()
await want(page, tid('pb-payout'), 304, 'payout with typed commission')
await shot(page, 'po-03-order')
await blk.locator(tid('pb-auto-commission')).click()
await want(page, tid('pb-payout'), 292.75, 'payout back to auto')
if (await page.locator(tid('refund-btn')).count()) throw new Error('platform orders have no refund button')
step('order: charge 5.00, deduction 36.00, commission typed then auto; payouts 328.75 / 292.75 / 304.00 / 292.75')

// ---- a phone ----
const phone = await open({ width: 390, height: 844 }, { isMobile: true, hasTouch: true, deviceScaleFactor: 2 })
await login(phone, 'owner1')
await phone.goto(BASE + '/merchant/order/platform')
await phone.locator(tid('po-platform')).first().waitFor()
await phone.fill(tid('po-no'), 'GF-PH1')
await phone.fill(tid('po-dish'), 'b1')
await phone.locator(tid('po-hits')).waitFor()
await phone.press(tid('po-dish'), 'Enter')
await want(phone, tid('po-subtotal'), 191, 'phone subtotal')
const wide = await phone.evaluate(() => document.documentElement.scrollWidth > window.innerWidth + 1)
if (wide) throw new Error('phone: the page scrolls sideways')
const btn = await phone.locator(tid('po-confirm')).boundingBox()
if (!btn || btn.y + btn.height > 844) throw new Error('phone: confirm button not on screen')
await shot(phone, 'po-04-phone')
step('phone: one column, confirm button at the bottom, no sideways scroll')

if (errors.length) { console.error(errors.join('\n')); process.exit(1) }
await browser.close()
})().catch(e => { console.error(e); process.exit(1) })
