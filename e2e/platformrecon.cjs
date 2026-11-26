// The reconciliation page's platform part (docs/ORDER_TYPES_DESIGN.md §9.5) in
// a real browser: takings by order type adding up to the total, the Grab row
// (owed − commission − charges − taken back = expected payout), the actual
// payout typed in from the statement and the difference, the orders under
// Grab, and a platform payment that never counts as cash.
// Usage: BASE_URL=http://localhost:8080 node e2e/platformrecon.cjs   (after e2e/platformorder.cjs)
const { chromium } = require('playwright')
const { mkdirSync } = require('node:fs')

;(async () => {
const BASE = process.env.BASE_URL || 'http://localhost:8080'
const SHOTS = process.env.SHOTS_DIR || 'e2e/shots'
mkdirSync(SHOTS, { recursive: true })
const browser = await chromium.launch()
const errors = []
const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 }, locale: 'zh-CN' })
const page = await ctx.newPage()
page.on('pageerror', e => errors.push(`pageerror: ${e.message}`))
page.on('console', m => { if (m.type() === 'error' && !/favicon|Failed to load resource/.test(m.text())) errors.push(`console: ${m.text()}`) })
const tid = s => `[data-testid="${s}"]`
const step = s => console.log('✓ ' + s)
const cents = s => Math.round(Number(s.replace(/[^0-9.\-−]/g, '').replace('−', '-')) * 100)

await page.goto(BASE + '/login')
await page.fill('input[autocomplete=username]', 'owner1')
await page.fill('input[type=password]', 'demo1234')
await page.click('button.primary')
await page.waitForURL(u => !/\/login$/.test(u.pathname))
await page.goto(BASE + '/merchant/reconcile')
const pf = page.locator(tid('rc-platforms'))
await pf.waitFor()
const row = pf.locator('tr[data-platform=Grab]')
const cells = await row.locator('td').allInnerTexts()
// 0 platform, 1 orders, 2 subtotal, 3 store-funded, 4 platform-funded, 5 owed, 6 commission, 7 charges, 8 taken back, 9 payout
const v = i => cells[i].trim() === '—' ? 0 : Math.abs(cents(cells[i]))
const owed = v(5), payout = v(9)
if (owed - v(6) - v(7) - v(8) !== payout) throw new Error(`Grab row does not add up: ${cells.join(' | ')}`)
step(`Grab: owed ${owed / 100} − ${v(6) / 100} − ${v(7) / 100} − ${v(8) / 100} = payout ${payout / 100}`)

// the platform row in the takings is what Grab owes, and it is not cash
const method = page.locator(tid('rc-rows') + ' tr[data-method=Grab] td')
if (cents((await method.nth(2).innerText())) !== owed) throw new Error('takings row for Grab')
const types = page.locator(tid('rc-types'))
const total = cents(await page.locator(tid('rc-total')).innerText())
const sumRow = await types.locator('tr.sum td').nth(2).innerText()
if (cents(sumRow) !== total) throw new Error(`types add up to ${sumRow}, total ${total}`)
const amounts = await types.locator('tbody tr:not(.sum) td:nth-child(3)').allInnerTexts()
if (amounts.reduce((s, x) => s + cents(x), 0) !== total) throw new Error('type rows do not add up')
step(`takings by type add up to the total ${total / 100}; the Grab row is what Grab owes`)

// the statement: 10.00 more than expected
await row.locator(tid('rc-pf-enter')).click()
await pf.locator(tid('rc-pf-amount')).fill(String((payout + 1000) / 100))
await pf.locator(tid('rc-pf-save')).click()
await row.locator(tid('rc-pf-diff')).waitFor()
const diff = await row.locator(tid('rc-pf-diff')).innerText()
if (!diff.startsWith('+') || cents(diff) !== 1000) throw new Error('difference ' + diff)
step('actual payout typed in: difference +10.00')
await row.click()
await pf.locator(tid('rc-pf-orders') + ' tbody tr').first().waitFor()
const n = await pf.locator(tid('rc-pf-orders') + ' tbody tr').count()
if (n !== Number(cells[1])) throw new Error(`orders under Grab: ${n}, row says ${cells[1]}`)
await page.screenshot({ path: `${SHOTS}/rc-01-platforms.png`, fullPage: true })
step(`${n} Grab orders listed under the row`)
await pf.locator(tid('rc-pf-del')).first().click()
await row.locator(tid('rc-pf-diff')).waitFor({ state: 'detached' })
step('statement deleted')

if (errors.length) { console.error(errors.join('\n')); process.exit(1) }
await browser.close()
})().catch(e => { console.error(e); process.exit(1) })
