// End-to-end walk through the delivery loop in a real browser:
// customer orders -> cashier accepts and assigns -> kitchen cooks and packs
// -> rider picks up and delivers with cash -> owner reconciles.
// Usage: BASE_URL=http://localhost:8080 node e2e/flow.cjs   (needs seed data and OTP_DEV_ECHO=true)
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
  page.on('dialog', d => d.accept('e2e reason'))
  return page
}
async function login(page, user) {
  await page.goto(BASE + '/login')
  await page.fill('input[autocomplete=username]', user)
  await page.fill('input[type=password]', 'demo1234')
  await page.click('button.primary')
  await page.waitForURL(u => !u.pathname.startsWith('/login'))
}
const step = (s) => console.log('✓ ' + s)
const shot = (page, name) => page.screenshot({ path: `${SHOTS}/${name}.png`, fullPage: true })

// ---- customer ----
const cust = await open({ width: 390, height: 844 })
await cust.goto(BASE + '/s/makati')
await cust.waitForSelector('.item')
await cust.selectOption('select', { index: 1 })
// popcorn chicken has a required option group
await cust.click('.item:has-text("盐酥鸡") button')
await cust.waitForSelector('.sheet')
if (!(await cust.locator('.sheet-foot .btn.primary').isDisabled())) throw new Error('required option should block adding')
await cust.click('.sheet label.opt:has-text("中辣")')
await shot(cust, '01-options')
await cust.click('.sheet-foot .btn.primary')
await cust.click('.item:has-text("冰柠檬茶") .stepper button.fill')
await cust.click('.item:has-text("瓶装水") .stepper button.fill')
await shot(cust, '02-menu')
await cust.click('.bottombar .btn.primary')
await cust.waitForURL('**/checkout')
await cust.fill('input[autocomplete=name]', '王小明')
await cust.fill('input[autocomplete=street-address]', '88 Ayala Ave')
const placeBtn = cust.locator('.bottombar .btn.primary')
await cust.waitForSelector('.totals')
if (!(await placeBtn.isDisabled())) throw new Error('ordering must be blocked until the phone is verified')
await cust.fill('input[type=tel]', '+63917' + String(Date.now()).slice(-7))
await cust.click('button:has-text("发送验证码")')
await cust.waitForSelector('input[autocomplete=one-time-code]')
await cust.click('button:has-text("验证"):not(:has-text("发送"))')
await cust.waitForSelector('.banner.ok')
await shot(cust, '03-checkout')
const total = (await cust.locator('.totals tr.sum td').last().innerText()).trim()
await placeBtn.click()
await cust.waitForURL('**/o/**')
await cust.waitForSelector('[data-testid=status]')
const status = () => cust.locator('[data-testid=status]').innerText()
const expectStatus = async (text) => {
  await cust.waitForFunction(t => document.querySelector('[data-testid=status]')?.textContent?.includes(t), text, { timeout: 8000 })
}
await expectStatus('等待商家接单')
const orderNo = (await cust.locator('.topbar h1').innerText()).replace('订单号', '').trim()
await shot(cust, '04-pending')
step(`customer placed ${orderNo}, total ${total}`)

// ---- cashier accepts and assigns ----
const desk = await open({ width: 1280, height: 800 })
await login(desk, 'cashier1')
await desk.click(`.ord:has-text("${orderNo}")`)
await shot(desk, '05-desk')
await desk.click('.detail button:has-text("接单")')
await desk.waitForSelector('.detail select')
await desk.selectOption('.detail select', { label: '骑手 Juan' })
await desk.waitForSelector('.detail :text("骑手 Juan")')
await expectStatus('备餐中')   // pushed to the customer page over SSE, no reload
step('cashier accepted; customer page updated live to "preparing"')

// ---- kitchen ----
const kitchen = await open({ width: 1280, height: 800 })
await login(kitchen, 'kitchen1')
const card = kitchen.locator(`.card:has-text("${orderNo}")`)
await card.waitFor()
// station tabs: the drinks station sees only the drink, the fryer only the fried item
await kitchen.click('button:has-text("饮品台")')
await card.locator(':text("冰柠檬茶")').first().waitFor()
if (await card.locator('.kname:has-text("盐酥鸡")').count()) throw new Error('drinks station must not see fried items')
await kitchen.click('button:has-text("炸台")')
await card.locator('.kname:has-text("盐酥鸡")').waitFor()
if (await card.locator('.kname:has-text("冰柠檬茶")').count()) throw new Error('fryer must not see drinks')
await shot(kitchen, '06b-kitchen-station')
await kitchen.click('button:has-text("全部")')
await shot(kitchen, '06-kitchen')
step('kitchen stations each see only their own items')
await kitchen.click('.seg button:has-text("打包")')
await kitchen.locator(`.card:has-text("${orderNo}") :text("待齐菜")`).waitFor()   // cannot pack yet
await kitchen.click('.seg button:has-text("制作")')
for (let i = 0; i < 2; i++) await card.locator('button.ok').first().click().then(() => kitchen.waitForTimeout(250))
await kitchen.click('.seg button:has-text("打包")')
const pcard = kitchen.locator(`.card:has-text("${orderNo}")`)
await pcard.locator(':text("待打包")').waitFor()
const packBtn = pcard.locator('button.primary')
if (!(await packBtn.isDisabled())) throw new Error('packing must need every line ticked')
for (const box of await pcard.locator('input[type=checkbox]').all()) await box.check()
await shot(kitchen, '07-packing')
await packBtn.click()
await pcard.locator(':text("已打包")').waitFor()
await expectStatus('等待骑手取餐')
step('kitchen cooked and packed; customer sees "waiting for rider", not "out for delivery"')

// ---- rider ----
const rider = await open({ width: 390, height: 844 })
await login(rider, 'rider1')
const task = rider.locator(`.card:has-text("${orderNo}")`)
await task.waitFor()
await shot(rider, '08-rider-pickup')
await task.locator('button:has-text("确认取餐")').click()
await expectStatus('配送中')
// a failed attempt: rider reports a problem, the desk sends the order out again
await task.locator('button:has-text("配送异常")').click()
await expectStatus('配送异常')
await desk.click(`.ord:has-text("${orderNo}")`)
await desk.locator('.detail button:has-text("重新配送")').waitFor()
await shot(desk, '09a-delivery-failed')
await desk.click('.detail button:has-text("重新配送")')
await expectStatus('等待骑手取餐')
await task.locator('button:has-text("确认取餐")').click()
await expectStatus('配送中')
step('failed delivery was sent out again')
await task.locator('button:has-text("已到达")').click()
await expectStatus('骑手已到达')
await shot(rider, '09-rider-deliver')
await task.locator('button:has-text("确认送达")').click()
await expectStatus('已送达')
await shot(cust, '10-delivered')
step('rider picked up, arrived and delivered with cash')

// ---- owner reconciles ----
const owner = await open({ width: 1280, height: 800 })
await login(owner, 'owner1')
await owner.goto(BASE + '/merchant/cash')
const row = owner.locator(`tr:has-text("${orderNo}")`)
await row.locator('button:has-text("确认核款")').click()
await row.locator(':text("已核款")').waitFor()
await shot(owner, '11-cash')
await owner.goto(BASE + '/merchant/menu'); await owner.waitForSelector('table.data'); await shot(owner, '12-menu-admin')
await owner.goto(BASE + '/merchant/settings'); await owner.waitForSelector('form.card'); await shot(owner, '13-settings')
step('owner reconciled the cash')

// English view of the same storefront
const en = await open({ width: 390, height: 844 })
await en.goto(BASE + '/s/makati'); await en.waitForSelector('.item'); await en.click('.lang')
await en.waitForSelector('text=Popcorn Chicken'); await shot(en, '14-english')
step('language switch shows English names')

await browser.close()
if (errors.length) { console.error(errors.join('\n')); process.exit(1) }
console.log('E2E OK')
})().catch(e => { console.error(e); process.exit(1) })
