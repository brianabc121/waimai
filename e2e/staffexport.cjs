// The last pieces of the staff reports in a real browser: the owner changes
// what counts as a slow dish and a detour, a cook reads their own attendance
// on the Me tab, and the owner downloads the three lists as Excel.
// Usage: BASE_URL=http://localhost:8080 node e2e/staffexport.cjs   (needs seed data)
const { chromium } = require('playwright')
const { mkdirSync, readFileSync } = require('node:fs')

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
  page.on('dialog', d => d.accept())
  return page
}
async function login(page, user, password = 'demo1234') {
  await page.goto(BASE + (/^admin/.test(user) ? '/admin/login' : /^((kitchen|rider|zhang|wang)\d|e2e\d+$)/.test(user) ? '/login?next=/work' : '/login'))
  for (let i = 0; ; i++) {
    await page.fill('input[autocomplete=username]', user)
    await page.fill('input[type=password]', password)
    const res = page.waitForResponse(r => r.url().endsWith('/auth/login'))
    await page.click('button.primary')
    if ((await res).status() !== 429 || i >= 5) return
    await page.waitForTimeout(7000)
  }
}
const api = (page, method, path, body) => page.evaluate(async ([method, path, body]) => {
  const r = await fetch('/api/v1' + path, { method, headers: { 'X-Requested-With': 'e2e', 'Content-Type': 'application/json' },
    body: body ? JSON.stringify(body) : undefined })
  return { status: r.status, body: await r.json().catch(() => null) }
}, [method, path, body])
const step = (s) => console.log('✓ ' + s)
const shot = (page, name) => page.screenshot({ path: `${SHOTS}/${name}.png`, fullPage: true })
const expect = (got, want, what) => { if (got !== want) throw new Error(`${what}: want ${want}, got ${got}`) }

const today = new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Manila' }).format(new Date())

// ---- the owner sets what counts as slow and as a detour ----
const owner = await open({ width: 1340, height: 900 })
await login(owner, 'owner1')
await owner.waitForURL('**/merchant/orders')
await owner.goto(BASE + '/merchant/settings')
const slow = owner.locator('input[name=kitchen_slow_minutes]')
await slow.waitFor()
const before = { slow: await slow.inputValue(), pct: await owner.inputValue('input[name=detour_percent]'), km: await owner.inputValue('input[name=detour_km]') }
async function setRules(v) {
  await slow.fill(v.slow); await owner.fill('input[name=detour_percent]', v.pct); await owner.fill('input[name=detour_km]', v.km)
  const res = owner.waitForResponse(r => r.url().endsWith('/settings') && r.request().method() === 'PUT')
  await slow.locator('xpath=ancestor::form').locator('button.primary').click()
  expect((await res).status(), 200, 'settings saved')
  await owner.waitForSelector('.banner.ok')
}
await setRules({ slow: '8', pct: '50', km: '2.5' })
await shot(owner, 'x01-report-settings')
const period = `from=${today}&to=${today}`
expect((await api(owner, 'GET', `/merchant/branches/1/reports/kitchen?${period}`)).body.slow_minutes, 8, 'slow minutes in the kitchen report')
const rr = (await api(owner, 'GET', `/merchant/branches/1/reports/riders?${period}`)).body
expect(rr.detour_percent, 50, 'detour percent'); expect(rr.detour_meters, 2500, 'detour metres')
await setRules(before)
step('the owner changed the slow-dish and detour rules, the reports follow, and put them back')

// ---- the cook reads their own attendance ----
const cook = await open({ width: 390, height: 844 })
await login(cook, 'zhang1')
await cook.waitForURL('**/work')
const card = cook.locator('[data-testid=shift]')
await card.waitFor()
const wasOn = await card.getAttribute('data-on') === 'true'
if (!wasOn) { await card.locator('[data-act=shift]').click(); await cook.waitForSelector('[data-testid=shift][data-on=true]') }
await cook.click('[data-testid=tab-me]')
const mine = cook.locator('[data-testid=my-shifts]')
await mine.locator('[data-testid=my-days]').waitFor()
expect(await mine.locator('[data-testid=my-month]').innerText(), today.slice(0, 7), 'the month shown')
if (!/^[1-9]/.test(await mine.locator('[data-testid=my-days]').innerText())) throw new Error('no day worked this month')
await mine.locator('[data-act=my-shifts-open]').click()
await mine.locator('[data-testid=my-shift]').first().waitFor()
await shot(cook, 'x02-my-attendance')
await mine.locator('[data-act=prev-month]').click()
await cook.waitForFunction(m => document.querySelector('[data-testid=my-month]')?.textContent !== m, today.slice(0, 7))
expect(await mine.locator('[data-act=next-month]').isEnabled(), true, 'can come back to this month')
expect((await api(cook, 'GET', `/merchant/branches/1/attendance?${period}`)).status, 403, 'the cook cannot read the store attendance')
step('the cook sees their own shifts of the month on the Me tab, read-only')

// ---- the owner downloads the three lists as Excel ----
for (const [path, name] of [['attendance', 'attendance'], ['kitchen', 'kitchen'], ['riders', 'riders']]) {
  await owner.goto(`${BASE}/merchant/staff/${path}`)
  const link = owner.locator('[data-testid=export]')
  await link.waitFor()
  const [file] = await Promise.all([owner.waitForEvent('download'), link.click()])
  if (!file.suggestedFilename().startsWith(name + '_') || !file.suggestedFilename().endsWith('.xlsx')) throw new Error('file name: ' + file.suggestedFilename())
  const saved = `${SHOTS}/${name}.xlsx`
  await file.saveAs(saved)
  const bytes = readFileSync(saved)
  if (bytes.length < 1500 || bytes.subarray(0, 2).toString() !== 'PK') throw new Error(name + ': not an Excel file')
}
await owner.goto(`${BASE}/merchant/staff/attendance`)
await owner.locator('[data-testid=export]').waitFor()
await shot(owner, 'x03-export-button')
step('the owner downloaded attendance, kitchen work and rider work as Excel')

if (!wasOn) { await cook.click('[data-testid=tab-tasks]'); await card.locator('[data-act=shift]').click(); await cook.waitForSelector('[data-testid=shift][data-on=false]') }
await browser.close()
if (errors.length) { console.error(errors.join('\n')); process.exit(1) }
console.log('staffexport e2e passed')

})().catch(e => { console.error(e); process.exit(1) })
