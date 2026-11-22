// End-to-end walk through the delivery loop in a real browser:
// customer orders -> cashier accepts and assigns -> the kitchen screen and the
// staff workbench cook and pack -> the same person switches to the rider
// identity, picks up and delivers with cash -> owner reconciles.
// Usage: BASE_URL=http://localhost:8080 node e2e/flow.cjs   (needs seed data and OTP_DEV_ECHO=true)
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
await customerLogin(cust, null, { name: '王小明' })
const placeBtn = cust.locator('.bottombar .btn.primary')
await cust.waitForSelector('.totals')
await shot(cust, '03-checkout')
const total = (await cust.locator('.totals .sum b').last().innerText()).trim()
const disc = (await cust.locator('.totals .disc').innerText()).replace(/\s+/g, ' ')
// The store picks whichever live promotion saves more: normally 满200减20, but
// between 22:00 and 02:00 store time the late-night 10% off wins.
const promoName = ['满200减20', '夜宵九折'].find(n => disc.includes(n))
if (!promoName) throw new Error('promotion missing at checkout: ' + disc)
await placeBtn.click()
await cust.waitForURL('**/o/**')
await cust.waitForSelector('[data-testid=status]')
const status = () => cust.locator('[data-testid=status]').innerText()
const expectStatus = async (text) => {
  await cust.waitForFunction(t => document.querySelector('[data-testid=status]')?.textContent?.includes(t), text, { timeout: 8000 })
}
await expectStatus('等待商家接单')
const orderNo = (await cust.locator('[data-testid=order-no]').innerText()).trim()
await shot(cust, '04-pending')
await cust.locator(`.totals tr.disc:has-text("${promoName}")`).waitFor()   // the order keeps the discount
step(`customer placed ${orderNo}, total ${total}`)

// ---- cashier accepts and assigns ----
const desk = await open({ width: 1280, height: 800 })
await login(desk, 'cashier1')
await desk.click(`.ord:has-text("${orderNo}")`)
await shot(desk, '05-desk')
await desk.click('.detail button:has-text("接单")')
await desk.waitForSelector('.detail select')
// 小王 holds two posts: packing and rider
await desk.selectOption('.detail select', await desk.locator('.detail select option[data-rider=wang1]').getAttribute('value'))
await desk.waitForSelector('.detail :text("小王")')
await expectStatus('备餐中')   // pushed to the customer page over SSE, no reload
step('cashier accepted; customer page updated live to "preparing"')

// ---- kitchen ----
const kitchen = await open({ width: 1280, height: 800 })
await login(kitchen, 'kitchen1')
await kitchen.waitForURL('**/work')   // staff without back-office access land on the workbench
await kitchen.goto(BASE + '/kitchen')
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

// ---- workbench, in-store identity: 小张 works the fryer and the hot kitchen ----
const cook = await open({ width: 390, height: 844 })
await login(cook, 'zhang1')
await cook.waitForURL('**/work')
const fry = cook.locator('[data-task="盐酥鸡"]')
await fry.waitFor()
if (await cook.locator('[data-task="冰柠檬茶"]').count()) throw new Error('the workbench must only show the dishes of my own stations')
if ((await cook.locator('[data-testid=identity]').innerText()).trim() !== '店内') throw new Error('a cook has the in-store identity only')
await shot(cook, '06c-workbench-store')
await fry.locator('[data-act=start]').click()
await card.locator('.kline.preparing:has-text("盐酥鸡")').waitFor()   // the kitchen screen follows live
await fry.locator('[data-act=ready]').click()
await cook.locator('[data-testid=store-empty]').waitFor()
await cook.click('[data-testid=tab-done]')
await cook.locator('[data-ready="盐酥鸡"]').waitFor()
step('workbench: the cook saw only the fryer dish, started and finished it; the kitchen screen followed')
await card.locator('button.ok').first().click()   // the drink is finished on the kitchen screen

// ---- workbench: 小王 packs in store, then switches to the rider identity ----
const rider = await open({ width: 390, height: 844 })
await login(rider, 'wang1')
await rider.waitForURL('**/work')
const pack = rider.locator(`[data-pack="${orderNo}"]`)
await pack.locator(':text("待打包")').waitFor()
await rider.locator('[data-testid=rider-busy]').waitFor()   // told about the delivery waiting under the other identity
await pack.locator('[data-act=pack]').click()
const packBtn = rider.locator('[data-act=confirm-pack]')
if (!(await packBtn.isDisabled())) throw new Error('packing must need every line ticked')
for (const box of await rider.locator('[data-testid=pack-sheet] input[type=checkbox]').all()) await box.check()
await shot(rider, '07-packing')
await packBtn.click()
await pack.locator(':text("已打包")').waitFor()
await expectStatus('等待骑手取餐')
step('workbench: packed in store; customer sees "waiting for rider", not "out for delivery"')

await rider.click('[data-testid=identity]')
await rider.locator('[data-testid=identity-sheet] [data-id=rider]:has-text("现在有 1 个任务")').waitFor()
await shot(rider, '07b-switch-identity')
await rider.click('[data-testid=switch-go]')
const task = rider.locator(`[data-delivery="${orderNo}"]`)
await task.waitFor()
await shot(rider, '08-rider-pickup')
await task.locator('[data-act=open]').click()
await rider.click('[data-act=pickup]')
await expectStatus('配送中')
// a failed attempt: rider reports a problem, the desk sends the order out again
await rider.click('[data-act=problem]')
await rider.click('[data-testid=problem-sheet] button:has-text("联系不上顾客")')
await rider.click('[data-act=submit-problem]')
await expectStatus('配送异常')
await desk.click(`.ord:has-text("${orderNo}")`)
await desk.locator('.detail button:has-text("重新配送")').waitFor()
await shot(desk, '09a-delivery-failed')
await desk.click('.detail button:has-text("重新配送")')
await expectStatus('等待骑手取餐')
await task.locator('[data-act=open]').click()
await rider.click('[data-act=pickup]')
await expectStatus('配送中')
step('failed delivery was sent out again')
await rider.click('[data-act=arrive]')
await expectStatus('骑手已到达')
await rider.locator('[data-quick]').first().click()   // a round note: the page works out the change
await rider.locator('[data-testid=change]').waitFor()
await rider.click('.quick button:has-text("刚好")')
await shot(rider, '09-rider-deliver')
await rider.click('[data-act=deliver]')
await expectStatus('已送达')
await rider.click('[data-testid=tab-done]')
await rider.locator(`[data-done="${orderNo}"]`).waitFor()
await shot(cust, '10-delivered')
step('rider identity: picked up, arrived and delivered with cash')

// ---- owner reconciles ----
const owner = await open({ width: 1280, height: 800 })
await login(owner, 'owner1')
await owner.goto(BASE + '/merchant/cash')
const row = owner.locator(`tr:has-text("${orderNo}")`)
await row.locator('button:has-text("确认核款")').click()
await row.locator(':text("已核款")').waitFor()
await shot(owner, '11-cash')
await owner.goto(BASE + '/merchant/promos'); await owner.waitForSelector('table.data tbody tr b')
await owner.click('button:has-text("试算"):not(h2)')
await owner.locator('.tag.ok:has-text("采用")').waitFor()
await shot(owner, '11b-promotions')
step('promotions page lists offers and the trial picks one')
await owner.goto(BASE + '/merchant/menu'); await owner.waitForSelector('table.data'); await shot(owner, '12-menu-admin')
await owner.goto(BASE + '/merchant/settings'); await owner.waitForSelector('form.card'); await shot(owner, '13-settings')
step('owner reconciled the cash')

// ---- Excel import: download the template, upload it, review, confirm ----
await owner.goto(BASE + '/merchant/menu'); await owner.waitForSelector('table.data')
const dl = owner.waitForEvent('download')
await owner.click('button:has-text("导入 Excel")')
await owner.click('a:has-text("下载模板")')
const tpl = `${SHOTS}/menu-template.xlsx`
await (await dl).saveAs(tpl)
await owner.setInputFiles('[data-testid=import-file]', tpl)
await owner.locator('[data-testid=import-summary]:has-text("3 行可导入")').waitFor({ timeout: 20000 })
await shot(owner, '12b-import-preview')
await owner.click('[data-testid=import-apply]')
await owner.locator('.banner.ok:has-text("已导入")').waitFor()
step('menu spreadsheet was parsed in the background, reviewed and imported')

// ---- scheduled pickup order ----
const later = await open({ width: 390, height: 844 })
await later.goto(BASE + '/s/makati')
await later.waitForSelector('.item')
await later.click('.item:has-text("瓶装水") .stepper button.fill, .item:has-text("瓶装水") button.fill')
await later.click('.bottombar .btn.primary')
await customerLogin(later, null, { pickup: true })
await later.fill('input[autocomplete=name]', '李预约')
await later.click('[data-testid=when-later]')
const slot = later.locator('[data-testid=slot-sheet] [data-testid=slot]').nth(4)
const slotText = (await slot.innerText()).trim()
await slot.click()
await later.locator(`[data-testid=when-later]:has-text("${slotText}")`).waitFor()
await shot(later, '15-checkout-scheduled')
await later.locator('.bottombar .btn.primary').click()
await later.waitForURL('**/o/**')
await later.locator(`[data-testid=scheduled]:has-text("${slotText}")`).waitFor()
const laterNo = (await later.locator('[data-testid=order-no]').innerText()).trim()
await desk.click(`.ord:has-text("${laterNo}")`)
await desk.click('.detail button:has-text("接单")')
await later.waitForFunction(() => document.querySelector('[data-testid=status]')?.textContent?.includes('按预约时间'), null, { timeout: 8000 })
await shot(later, '16-order-scheduled')
await kitchen.waitForTimeout(800)
if (await kitchen.locator(`.card:has-text("${laterNo}")`).count()) throw new Error('a scheduled order must not reach the kitchen before its preparation time')
step(`scheduled order ${laterNo} for ${slotText} accepted and held back from the kitchen`)

// English view of the same storefront
const en = await open({ width: 390, height: 844 })
await en.goto(BASE + '/s/makati'); await en.waitForSelector('.item'); await en.click('.lang')
await en.waitForSelector('text=Popcorn Chicken'); await shot(en, '14-english')
step('language switch shows English names')

await browser.close()
if (errors.length) { console.error(errors.join('\n')); process.exit(1) }
console.log('E2E OK')
})().catch(e => { console.error(e); process.exit(1) })
