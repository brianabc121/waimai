// The in-store work summary in a real browser: a cook makes a dish on their
// own workbench, another dish is marked on the shared kitchen screen, and the
// owner reads who made what under Staff > In-store work.
// Usage: BASE_URL=http://localhost:8080 node e2e/kitchenreport.cjs   (needs seed data)
const { chromium } = require('playwright')
const { customerLogin } = require('./lib.cjs')
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
  return page
}
async function login(page, user) {
  await page.goto(BASE + '/login')
  for (let i = 0; ; i++) {
    await page.fill('input[autocomplete=username]', user)
    await page.fill('input[type=password]', 'demo1234')
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
const num = async (loc) => Number((await loc.innerText()).trim())

// ---- owner: what the page says before ----
const owner = await open({ width: 1340, height: 900 })
await login(owner, 'owner1')
await owner.waitForURL('**/merchant/orders')
await owner.click('.staffbar a:has-text("员工")')
await owner.click('[data-testid=staff-tabs] [data-tab=kitchen]')
await owner.waitForURL('**/merchant/staff/kitchen')
const people = owner.locator('[data-testid=kr-people]')
await people.locator('[data-person=zhang1]').waitFor()
const before = {
  zhang: await num(people.locator('[data-person=zhang1] [data-col=qty]')),
  board: await people.locator('[data-person=board]').count() ? await num(people.locator('[data-person=board] [data-col=qty]')) : 0,
}
expect(await people.locator('[data-person=rider1]').count(), 0, 'riders are not listed')

// ---- a customer orders 2 fries (fryer) and a braised pork rice (hot kitchen) for pickup ----
const cust = await open({ width: 390, height: 844 })
await cust.goto(BASE + '/s/makati')
await cust.waitForSelector('.item')
await cust.click('.item:has-text("无盐地瓜条") .stepper button.fill')
await cust.click('.item:has-text("无盐地瓜条") .stepper button.fill')
await cust.click('.item:has-text("卤肉饭") .stepper button.fill')
await cust.click('.bottombar .btn.primary')
await customerLogin(cust, null, { name: '陈先生' })
await cust.locator('.bottombar .btn.primary').click()
await cust.waitForURL('**/o/**')
await cust.waitForFunction(() => document.querySelector('[data-testid=status]')?.textContent?.includes('等待商家接单'))
const orderNo = (await cust.locator('[data-testid=order-no]').innerText()).trim()
const desk = await open({ width: 1280, height: 800 })
await login(desk, 'cashier1')
await desk.click(`.ord:has-text("${orderNo}")`)
await desk.click('.detail button:has-text("接单")')
await desk.waitForSelector('.detail select')
step(`order ${orderNo} placed and accepted`)

// ---- the cook makes the fries on their own workbench ----
const cook = await open({ width: 390, height: 844 })
await login(cook, 'zhang1')
await cook.waitForURL('**/work')
const fries = cook.locator('[data-task="无盐地瓜条"]').last()
await fries.locator('[data-act=start]').click()
await fries.locator('[data-act=ready]').click()
await fries.waitFor({ state: 'detached' })
// ---- the rice is marked on the shared kitchen screen ----
const screen = await open({ width: 1280, height: 800 })
await login(screen, 'kitchen1')
await screen.waitForURL(u => !u.pathname.startsWith('/login'))
const board = (await api(screen, 'GET', '/kitchen/branches/1/board')).body.orders
const rice = board.find(o => o.public_no === orderNo).items.find(i => i.name === '卤肉饭')
expect((await api(screen, 'POST', `/kitchen/items/${rice.id}/ready?via=board`)).status, 200, 'ready on the kitchen screen')
step('fries made on the workbench, rice marked on the kitchen screen')

// ---- owner: the summary ----
await owner.reload()
await people.locator('[data-person=zhang1]').waitFor()
expect(await num(people.locator('[data-person=zhang1] [data-col=qty]')), before.zhang + 2, "the cook's portions")
expect(await num(people.locator('[data-person=board] [data-col=qty]')), before.board + 1, 'portions on the kitchen screen')
await owner.locator('[data-testid=kr-board-share]').waitFor()
await people.locator('[data-person=zhang1]').click()
const detail = owner.locator('[data-testid=kr-detail]')
await detail.locator(`tr[data-dish="无盐地瓜条"]:has-text("${orderNo}")`).waitFor()
const line = detail.locator(`tr[data-dish="无盐地瓜条"]:has-text("${orderNo}")`)
await line.locator('[data-col=via]:has-text("手机工作台")').waitFor()
await line.locator('.tag.info:has-text("炸台")').waitFor()
expect(await line.locator('[data-col=took] .muted').count(), 0, 'the fries have a time: start was tapped')
expect(parseInt(await detail.locator('[data-kpi=qty] .v').innerText(), 10) >= 2, true, 'the figure on top')
const today = await owner.locator('[data-testid=kr-bars] button.on').getAttribute('data-day')
await owner.locator('[data-testid=kr-stations]:has-text("炸台")').waitFor()
await shot(owner, 'k01-kitchen-summary')
step('the owner sees the fries under the cook, with station, time and where it was marked')

// filters: another station has nothing of this cook's today; slow only is empty
const total = await owner.locator('[data-testid=kr-item-total]').innerText()
await owner.click('[data-testid=kr-filter] [data-f=slow]')
await owner.waitForFunction(was => document.querySelector('[data-testid=kr-item-total]')?.textContent !== was, total)
expect(await line.count(), 0, 'the fries were not slow')
await owner.click('[data-testid=kr-filter] [data-f=all]')
await line.waitFor()

// the kitchen screen: the rice, with the account that was signed in
await people.locator('[data-person=board]').click()
const riceLine = detail.locator(`tr[data-dish="卤肉饭"]:has-text("${orderNo}")`)
await riceLine.locator('[data-col=via]:has-text("后厨大屏")').waitFor()
await riceLine.locator('[data-col=via]:has-text("后厨 阿明")').waitFor()
await riceLine.locator('[data-col=took]:has-text("没点开始")').waitFor()
await shot(owner, 'k02-kitchen-screen')
step('what was marked on the kitchen screen is kept apart, with the account signed in there')

// a week: the bar of today opens the day
await people.locator('[data-person=zhang1]').click()
await owner.click('.pills [data-preset=week]')
await owner.locator('[data-testid=kr-bars] button.on').nth(6).waitFor()
await shot(owner, 'k03-kitchen-week')
await owner.click(`[data-testid=kr-bars] button[data-day="${today}"]`)
await owner.locator(`[data-testid=kr-title]:has-text("${today}")`).waitFor()
await owner.click('.datepick [data-act=prev]')
await owner.locator('.pills [data-preset=yesterday].on').waitFor()
step('a period can be chosen; a bar opens its day')

// English
await owner.click('.staffbar .lang'); await owner.click('[data-testid=lang-en]')
await owner.locator('[data-testid=staff-tabs] :text("In-store work")').waitFor()
await owner.click('.staffbar .lang'); await owner.click('[data-testid=lang-zh]')

// a cashier has no access
expect((await api(desk, 'GET', `/merchant/branches/1/reports/kitchen?from=${today}&to=${today}`)).status, 403, 'a cashier cannot read the summary')
step('the page is bilingual; a cashier cannot read it')

await browser.close()
if (errors.length) { console.error(errors.join('\n')); process.exit(1) }
console.log('kitchen report e2e passed')

})().catch(e => { console.error(e); process.exit(1) })
