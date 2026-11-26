// Delivery platforms and platform prices (docs/ORDER_TYPES_DESIGN.md §6):
// the platform's list in the back office, a store turning Grab on with a 20%
// markup, the menu showing each dish's Grab price, and a price set by hand.
// Usage: BASE_URL=http://localhost:8080 node e2e/platforms.cjs   (seed data)
const { chromium } = require('playwright')
const { mkdirSync } = require('node:fs')

;(async () => {
const BASE = process.env.BASE_URL || 'http://localhost:8080'
const SHOTS = process.env.SHOTS_DIR || 'e2e/shots'
mkdirSync(SHOTS, { recursive: true })
const browser = await chromium.launch()
const errors = []
async function open(viewport) {
  const ctx = await browser.newContext({ viewport, locale: 'zh-CN' })
  const page = await ctx.newPage()
  page.on('pageerror', e => errors.push(`pageerror: ${e.message}`))
  page.on('console', m => { if (m.type() === 'error' && !/favicon|Failed to load resource/.test(m.text())) errors.push(`console: ${m.text()}`) })
  return page
}
async function login(page, user, admin) {
  await page.goto(BASE + (admin ? '/admin/login' : '/login'))
  await page.fill('input[autocomplete=username]', user)
  await page.fill('input[type=password]', 'demo1234')
  await page.click('button.primary')
  await page.waitForURL(u => !/\/login$/.test(u.pathname))
}
const step = s => console.log('✓ ' + s)
const shot = (p, n) => p.screenshot({ path: `${SHOTS}/${n}.png`, fullPage: false })
const money = s => Number(s.replace(/[^0-9.]/g, ''))

// ---- the store turns Grab on ----
const desk = await open({ width: 1280, height: 860 })
await login(desk, 'owner1')
await desk.goto(BASE + '/merchant/settings')
const card = desk.locator('[data-testid=platforms-card]')
await card.waitFor()
await card.scrollIntoViewIfNeeded()
const on = card.locator('[data-testid=pf-on-Grab]')
if (!(await on.isChecked())) await on.check()
await card.locator('[data-testid=pf-markup-Grab]').fill('20')
await card.locator('[data-testid=pf-save]').click()
await card.locator('.banner.ok').waitFor()
await shot(desk, 'pf-01-settings')
step('store sells on Grab with a 20% markup')

// ---- the menu lists the Grab price of each dish ----
await desk.goto(BASE + '/merchant/menu')
const row = desk.locator('tr.mx-row').first()
await row.waitFor()
const store = money(await row.locator('.c-price').evaluate(td => td.firstChild.textContent))
const grab = row.locator('.pfline[data-platform=Grab]')
const auto = money(await grab.innerText())
// 20% on, rounded to a whole 1 (the default): checked against the hand samples in the Go tests
if (Math.abs(auto - Math.round(store * 1.2)) > 0.001) throw new Error(`auto Grab price ${auto} for ${store}`)
await shot(desk, 'pf-02-menu')
step(`menu: ${store} in store, ${auto} on Grab`)

// ---- a price set by hand wins, and clearing it goes back to the auto one ----
await row.locator('button.op.red').click()
const form = desk.locator('[data-testid=item-form]')
const input = form.locator('[data-testid=item-platform-prices] [data-platform=Grab] input')
await input.waitFor()
if (!(await input.getAttribute('placeholder')).includes(String(auto))) throw new Error('placeholder lacks the auto price')
await input.fill('188')
await shot(desk, 'pf-03-form')
await form.locator('button[type=submit], button.primary').last().click()
await form.waitFor({ state: 'detached' })
await desk.locator('.pfline[data-platform=Grab].own').first().waitFor()
if (money(await row.locator('.pfline[data-platform=Grab]').innerText()) !== 188) throw new Error('hand-set price not shown')
step('hand-set Grab price 188.00 shown')
await row.locator('button.op.red').click()
await input.fill('')
await form.locator('button[type=submit], button.primary').last().click()
await form.waitFor({ state: 'detached' })
if (money(await row.locator('.pfline[data-platform=Grab]').innerText()) !== auto) throw new Error('cleared price did not go back to auto')
step('cleared: back to the auto price')

// ---- the platform's list ----
const adm = await open({ width: 1280, height: 860 })
await login(adm, 'admin', true)
await adm.goto(BASE + '/admin/platforms')
const g = adm.locator('tr[data-platform=Grab]')
await g.waitFor()
const stores = Number((await g.locator('td.num').nth(2).innerText()).trim())
if (stores < 1) throw new Error('Grab shows no stores')
await shot(adm, 'pf-04-admin')
step(`admin list: Grab on ${stores} store(s)`)

if (errors.length) { console.error(errors.join('\n')); process.exit(1) }
await browser.close()
})().catch(e => { console.error(e); process.exit(1) })
