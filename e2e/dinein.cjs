// End-to-end walk through one dine-in sitting at table 08 in a real browser:
// staff opens the table -> a phone scans and orders fried food -> the table
// tablet adds a drink -> each station sees only its own dish -> dishes are
// served -> a batch whose answer is lost is retried without duplicating ->
// the desk takes payment and closes -> the old phone is locked out and the
// next sitting starts with an empty bill.
// Usage: BASE_URL=http://localhost:8080 node e2e/dinein.cjs   (needs seed data)
const { chromium } = require('playwright')
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
  page.on('dialog', d => d.accept('e2e reason'))
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
const shot = (page, name) => page.screenshot({ path: `${SHOTS}/${name}.png`, fullPage: true })
const count = async (loc, n, what) => { const c = await loc.count(); if (c !== n) throw new Error(`${what}: expected ${n}, got ${c}`) }

// ---- desk: table code and tablet pairing ----
const desk = await open({ width: 1280, height: 860 })
await login(desk, 'cashier1')
await desk.goto(BASE + '/merchant/tables')
const card = desk.locator('[data-table="08"]')
await card.waitFor()
await card.locator('.gear').click()
const link = (await desk.locator('[data-test=table-link]').innerText()).trim()
await desk.click('[data-test=pair-tablet]')
const pairCode = (await desk.locator('[data-test=pair-code]').innerText()).trim()
await shot(desk, 'd01-table-code')
await desk.click('.sheet-head .btn.link')
step(`table 08 has a QR link and a pairing code`)

// ---- phone scans before the table is open ----
const phone = await open({ width: 390, height: 844 })
await phone.goto(link)
await phone.waitForSelector('[data-test=not-open]')
step('a scan before opening is turned away')

// a tap on a table opens it on the dine-in desk (代客下单 > 堂食)
await card.click()
await desk.waitForSelector('[data-testid=dd-open]')
await desk.click('[data-testid=dd-open-only]')
await desk.locator('[data-testid="dd-table-08"]:not(.free)').waitFor()
await desk.goto(BASE + '/merchant/tables')
await desk.locator('[data-table="08"] .tag.brand').waitFor()
step('cashier opened table 08 on the desk')

// ---- phone orders fried food ----
await phone.click('button:has-text("重试")')
await phone.waitForSelector('.item')
await phone.click('.item:has-text("盐酥鸡") button')
await phone.click('.sheet label.opt:has-text("中辣")')
await phone.click('.sheet-foot .btn.primary')
await shot(phone, 'd02-phone-menu')
await phone.click('.dbar .btn.primary')
await phone.click('[data-test=send-batch]')
await phone.waitForSelector('[data-test=sent]')
await count(phone.locator('[data-test=batch]'), 1, 'batches after the phone order')
step('phone sent batch 1 (popcorn chicken)')

// ---- tablet pairs and adds a drink ----
const pad = await open({ width: 1024, height: 768 })
await pad.goto(BASE + '/ipad')
await pad.fill('input[name=pair-code]', pairCode)
await pad.click('form .btn.primary')
await pad.waitForSelector('.item')
await pad.click('.item:has-text("冰柠檬茶") .stepper button.fill')
await pad.click('.dbar .btn.primary')
await pad.click('[data-test=send-batch]')
await pad.waitForSelector('[data-test=sent]')
await count(pad.locator('[data-test=batch]'), 2, 'batches on the tablet')
await shot(pad, 'd03-tablet-bill')
// the phone follows the same bill without reloading
await phone.locator('[data-test=batch]').nth(1).waitFor({ timeout: 8000 })
step('tablet sent batch 2 (lemon tea); both devices show one bill with two batches')

// ---- kitchen: each station gets only its own dish ----
const kitchen = await open({ width: 1280, height: 860 })
await login(kitchen, 'kitchen1')
await kitchen.goto(BASE + '/kitchen')
const ticket = kitchen.locator('.card:has-text("桌 08")')
await ticket.waitFor()
await kitchen.click('button:has-text("炸台")')
await count(ticket.locator('.kline'), 1, 'fryer lines'); await ticket.locator('.kline:has-text("盐酥鸡")').waitFor()
await shot(kitchen, 'd04-fryer')
await ticket.locator('button:has-text("备好")').click()
await kitchen.click('button:has-text("饮品台")')
await ticket.locator('.kline:has-text("冰柠檬茶")').waitFor()
await count(ticket.locator('.kline'), 1, 'drink station lines')
await ticket.locator('button:has-text("备好")').click()
step('fryer saw only the chicken, drinks only the tea; both marked ready')

await kitchen.click('[data-test=tab-serve]')
await shot(kitchen, 'd05-serve')
await ticket.locator('.kline:has-text("盐酥鸡") button').click()
await ticket.locator('.kline:has-text("冰柠檬茶") button').click()
await ticket.waitFor({ state: 'detached' })
await phone.locator('.bline .tag.ok').nth(1).waitFor({ timeout: 8000 })
step('both dishes served; the phone shows them as served')

// ---- lost answer: the batch reaches the server, the reply does not ----
await phone.click('.seg button:first-child')
await phone.click('.item:has-text("瓶装水") .stepper button.fill')
await phone.click('.dbar .btn.primary')
await phone.route('**/table-sessions/current/batches', async route => { await route.fetch(); await route.abort() })
await phone.click('[data-test=send-batch]')
await phone.waitForSelector('.sheet .banner.bad')
await phone.unroute('**/table-sessions/current/batches')
await phone.click('[data-test=send-batch]')
await phone.waitForSelector('[data-test=sent]')
await count(phone.locator('[data-test=batch]'), 3, 'batches after retrying a lost answer')
step('retry after a lost answer reused the key: still exactly three batches')

// ---- desk: bill, payment, close ----
await desk.goto(BASE + '/merchant/tables')
await desk.locator('[data-table="08"]').click()
await desk.waitForSelector('[data-testid=dd-pay-panel]')
await count(desk.locator('.dd-bill .round'), 3, 'rounds at the desk')
await shot(desk, 'd06-desk-bill')
await desk.click('[data-testid=dd-close]')
await desk.waitForSelector('[data-testid=dd-closed]')
await shot(desk, 'd07-desk-paid')
await desk.click('[data-testid=dd-done]')
await desk.goto(BASE + '/merchant/tables')
await desk.locator('[data-table="08"] .tag.ok').waitFor()
await desk.locator('table.data tbody tr').first().waitFor()
step('cashier took payment and closed table 08')

await phone.waitForSelector('[data-test=closed]', { timeout: 8000 })
await pad.waitForSelector('[data-test=idle]', { timeout: 8000 })
step('the phone is locked out and the tablet is back on the welcome screen')

// ---- next sitting ----
await desk.locator('[data-table="08"]').click()
await desk.click('[data-testid=dd-open-only]')
await desk.locator('[data-testid="dd-table-08"]:not(.free)').waitFor()
await pad.waitForSelector('.item', { timeout: 8000 })
await phone.click('button:has-text("重新进入")')
await phone.waitForSelector('.item')
await phone.click('[data-test=tab-bill]')
await count(phone.locator('[data-test=batch]'), 0, 'batches in the new sitting')
await shot(phone, 'd08-new-sitting')
step('the next sitting starts with an empty bill')

await browser.close()
if (errors.length) { console.error(errors.join('\n')); process.exit(1) }
console.log('DINE-IN E2E OK')
})().catch(e => { console.error(e); process.exit(1) })
