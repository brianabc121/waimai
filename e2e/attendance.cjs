// Attendance in a real browser: a cook clocks on in the workbench, the owner
// sees it under Staff > Attendance, corrects the shift with a reason, adds a
// shift nobody clocked, reads the month and voids a shift.
// Usage: BASE_URL=http://localhost:8080 node e2e/attendance.cjs   (needs seed data)
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
  page.on('dialog', d => d.accept())
  return page
}
async function login(page, user, password = 'demo1234') {
  await page.goto(BASE + '/login')
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

// The store keeps Manila time; the browser here may not.
const TZ = 'Asia/Manila'
const local = (ms) => {
  const p = Object.fromEntries(new Intl.DateTimeFormat('en-CA', { timeZone: TZ, hourCycle: 'h23', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
    .formatToParts(new Date(ms)).map(x => [x.type, x.value]))
  return { day: `${p.year}-${p.month}-${p.day}`, input: `${p.year}-${p.month}-${p.day}T${p.hour}:${p.minute}` }
}
const today = local(Date.now()).day
const earlier = new Date(Date.parse(today + 'T00:00:00Z') - 3 * 86400000).toISOString().slice(0, 10)

// ---- owner: start from a clean slate, so the script can run again ----
const owner = await open({ width: 1340, height: 900 })
await login(owner, 'owner1')
await owner.waitForURL('**/merchant/orders')
const page = '/merchant/branches/1/attendance'
for (const s of (await api(owner, 'GET', `${page}?from=${earlier}&to=${today}`)).body.shifts) {
  if (!s.voided) await api(owner, 'PATCH', `${page}/${s.id}`, { void: true, reason: 'e2e reset' })
}

// ---- the cook clocks on in the workbench ----
const cook = await open({ width: 390, height: 844 })
await login(cook, 'zhang1')
await cook.waitForURL('**/work')
const card = cook.locator('[data-testid=shift]')
await card.waitFor()
expect(await card.getAttribute('data-on'), 'false', 'the cook starts off')
await shot(cook, 't01-clock-off')
await card.locator('[data-act=shift]').click()
await cook.waitForSelector('[data-testid=shift][data-on=true]')
await shot(cook, 't02-clock-on')
step('the cook clocked on in the in-store workbench')

// ---- owner: Staff > Attendance, by day ----
await owner.click('.staffbar a:has-text("员工")')
await owner.waitForURL('**/merchant/staff')
await owner.locator('tr[data-staff="zhang1"]:has-text("上班中")').waitFor()
await owner.click('[data-testid=staff-tabs] [data-tab=attendance]')
await owner.waitForURL('**/merchant/staff/attendance')
const row = owner.locator('tr[data-att="zhang1"]')
await row.locator('.tag.ok:has-text("在岗")').waitFor()
await row.locator('[data-col=status]:has-text("正常")').waitFor()
expect((await owner.locator('[data-kpi=on] .v').innerText()).trim().startsWith('1'), true, 'one person at work')
await owner.locator('tr[data-att="rider1"]:has-text("没打卡")').waitFor()
await shot(owner, 't03-attendance-day')
step('the owner sees who is at work and who has no record')

// ---- correct the shift: it started two hours ago and ended an hour ago ----
await row.locator('[data-act=edit]').click()
const form = owner.locator('[data-testid=att-form]')
await form.waitFor()
await form.locator('[data-testid=att-save]').click()
await form.locator('input[name=start]').fill(local(Date.now() - 2 * 3600000).input)
await form.locator('input[name=end]').fill(local(Date.now() - 3600000).input)
await form.locator('[data-testid=att-result]:has-text("1:00")').waitFor()
await form.locator('[data-testid=att-save]').click()
await form.locator('[data-testid=att-error]:has-text("请填写原因")').waitFor()
await form.locator('textarea[name=reason]').fill('忘记点下班')
await shot(owner, 't04-attendance-edit')
await form.locator('[data-testid=att-save]').click()
await owner.locator('[data-testid=att-notice]').waitFor()
await row.locator('.tag.info:has-text("店长改过")').waitFor()
expect((await row.locator('[data-col=hours] b').innerText()).trim(), '1:00', 'hours after the correction')
step('the owner corrected the shift; a reason was required')

// The manager ended the shift, so the cook's workbench shows them off.
await cook.reload()
await cook.waitForSelector('[data-testid=shift][data-on=false]')
step('the cook is clocked off by the correction')

// ---- add a shift nobody clocked, three days ago ----
const loaded = owner.waitForResponse(r => r.url().includes(`/attendance?from=${earlier}`) && r.ok())
for (let i = 0; i < 3; i++) await owner.click('.datepick [data-act=prev]')
await loaded
await owner.locator(`[data-testid=att-title]:has-text("${earlier}")`).waitFor()
// On a first run the row is empty and has its own button; on a later run it
// holds the shifts earlier runs voided, and the shift is added from the top.
await owner.locator('tr[data-att="zhang1"] [data-act=fill]').waitFor() // the cook has nothing that day
if (await owner.locator('tr[data-att="rider1"] [data-act=fill]').count()) {
  await owner.locator('tr[data-att="rider1"] [data-act=fill]').click()
  await form.waitFor()
} else {
  await owner.click('[data-testid=att-add]')
  await form.locator('select[name=user]').selectOption({ label: '骑手 Juan' })
}
expect(await form.locator('input[name=start]').inputValue(), earlier + 'T09:00', 'default start')
await form.locator('textarea[name=reason]').fill('手机坏了')
await form.locator('[data-testid=att-save]').click()
const added = owner.locator('tr[data-att="rider1"]')
await added.locator('.tag.info:has-text("店长补的")').waitFor()
expect((await added.locator('[data-col=hours] b').innerText()).trim(), '9:00', 'hours of the added shift')
// the same hours again clash
await owner.click('[data-testid=att-add]')
await form.locator('select[name=user]').selectOption({ label: '骑手 Juan' })
await form.locator('textarea[name=reason]').fill('重复')
await form.locator('[data-testid=att-save]').click()
await form.locator('[data-testid=att-error]:has-text("已经有一班")').waitFor()
await form.locator('button:has-text("取消")').click()
step('the owner added a shift by hand; an overlapping one is refused')

// ---- by month ----
await owner.click('.pills [data-mode=month]')
const month = owner.locator('[data-testid=att-month]')
await month.waitFor()
if (earlier.slice(0, 7) === today.slice(0, 7)) {
  expect((await month.locator(`tr[data-att="rider1"] td[data-day="${earlier}"]`).innerText()).trim(), '9', 'the month cell')
  const todayCell = month.locator(`tr[data-att="zhang1"] td[data-day="${today}"]`)
  expect((await todayCell.innerText()).trim(), '1', 'the corrected day in the month')
  expect(await todayCell.getAttribute('class'), 'edited', 'a corrected day is marked')
}
expect((await month.locator('tr[data-att="rider1"] [data-col=hours]').innerText()).trim(), '9.0', 'month total')
await shot(owner, 't05-attendance-month')
await month.locator(`tr[data-att="rider1"] td[data-day="${earlier}"]`).click()
await owner.locator(`[data-testid=att-title]:has-text("${earlier}")`).waitFor()
step('the month shows hours per day and totals; a cell opens its day')

// ---- void ----
await added.locator('[data-act=edit]').click()
await form.locator('textarea[name=reason]').fill('记错人了')
await form.locator('[data-testid=att-void]').click()
await added.locator('[data-act=edit]').waitFor({ state: 'detached' })
await added.locator('.tag:has-text("已作废")').first().waitFor()
expect((await added.locator('[data-col=hours] b').innerText()).trim(), '0:00', 'a voided shift does not count')
step('a voided shift stays on the record and stops counting')

// ---- English ----
await owner.click('.staffbar .lang'); await owner.click('[data-testid=lang-en]')
await owner.locator('[data-testid=staff-tabs] :text("Attendance")').waitFor()
await owner.locator('.tag:has-text("Voided")').first().waitFor()
await owner.click('.staffbar .lang'); await owner.click('[data-testid=lang-zh]')
step('the page is bilingual')

// a cashier has no staff pages
const cashier = await open({ width: 1280, height: 800 })
await login(cashier, 'cashier1')
await cashier.waitForURL('**/merchant/orders')
expect((await api(cashier, 'GET', `${page}?from=${today}&to=${today}`)).status, 403, 'a cashier cannot read attendance')
// ...but clocks on from the back-office header
const pill = cashier.locator('[data-testid=office-shift]')
await pill.waitFor()
if (await pill.getAttribute('data-on') === 'true') { await pill.click(); await cashier.waitForSelector('[data-testid=office-shift][data-on=false]') }
await pill.click()
await cashier.waitForSelector('[data-testid=office-shift][data-on=true]')
await pill.click()
await cashier.waitForSelector('[data-testid=office-shift][data-on=false]')
step('a cashier cannot see attendance and clocks on from the back office')

await browser.close()
if (errors.length) { console.error(errors.join('\n')); process.exit(1) }
console.log('attendance e2e passed')

})().catch(e => { console.error(e); process.exit(1) })
