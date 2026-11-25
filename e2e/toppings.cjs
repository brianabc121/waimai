// Topping templates (加料模板): the owner writes "麻辣烫" (AC02 上海青 20,
// AC03 油麦菜 30) on the menu's new tab and applies it to the braised pork
// rice, adds a second template from the dish form, then the cashier adds the
// toppings by code when ordering for a customer and by tapping at a table.
// Usage: BASE_URL=http://localhost:8080 node e2e/toppings.cjs   (needs seed data)
const { chromium } = require('playwright')
const { mkdirSync } = require('node:fs')

;(async () => {

const BASE = process.env.BASE_URL || 'http://localhost:8080'
const SHOTS = process.env.SHOTS_DIR || 'e2e/shots'
mkdirSync(SHOTS, { recursive: true })
const browser = await chromium.launch()
const errors = []
const step = (s) => console.log('✓ ' + s)
const tid = (id) => `[data-testid="${id}"]`
const expectText = async (loc, re, what) => { const s = (await loc.innerText()).replace(/\s+/g, ' '); if (!re.test(s)) throw new Error(`${what}: "${s}" does not match ${re}`) }

async function open(viewport) {
  const ctx = await browser.newContext({ viewport, locale: 'zh-CN', deviceScaleFactor: Number(process.env.DPR || 1), ignoreHTTPSErrors: process.env.E2E_INSECURE_TLS === '1' })
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
const api = (page, method, path, body) => page.evaluate(async ([method, path, body]) => {
  const r = await fetch('/api/v1' + path, { method, credentials: 'same-origin', headers: { 'X-Requested-With': 'waimai-web', 'X-Session': 'staff', 'Content-Type': 'application/json' }, body: body ? JSON.stringify(body) : undefined })
  const j = await r.json(); if (!r.ok) throw new Error(path + ': ' + JSON.stringify(j)); return j
}, [method, path, body])

// write a template on the toppings tab: rows are [code, name, price]
async function writeTemplate(page, name, rows) {
  await page.click(tid('tp-new'))
  await page.waitForSelector(tid('tp-editor'))
  await page.fill(tid('tp-name'), name)
  for (let i = 0; i < rows.length; i++) {
    const row = page.locator(tid('tp-row')).nth(i)
    await row.locator(tid('tp-code')).fill(rows[i][0])
    await row.locator(tid('tp-row-name')).fill(rows[i][1])
    await row.locator(tid('tp-price')).fill(String(rows[i][2]))
  }
  await page.click(tid('tp-save'))
  await page.waitForSelector(tid('tp-notice'))
  await page.locator(tid(`tp-item-${name}`)).waitFor()
}

// The rice with toppings opens a sheet on the storefront instead of a plain "+",
// which the tests after this one do not expect: the templates go again at the
// end (and from a run that stopped half way, at the start). Their option
// groups leave the dishes with them.
const MINE = ['麻辣烫', '羊肉泡馍加料', '重复']
async function dropTemplates(page) {
  for (const x of (await api(page, 'GET', '/merchant/branches/1/topping-templates')).templates)
    if (MINE.includes(x.name)) await api(page, 'DELETE', `/merchant/branches/1/topping-templates/${x.id}`)
}

// ---------- the owner on a PC ----------
const owner = await open({ width: 1366, height: 860 })
await login(owner, 'owner1')
await dropTemplates(owner)
await owner.goto(BASE + '/merchant/menu')
await owner.click(tid('tab-toppings'))
await owner.waitForSelector(tid('topping-templates'))
await writeTemplate(owner, '麻辣烫', [['ac02', '上海青', 20], ['AC03', '油麦菜', 30]])
await expectText(owner.locator('.tp-chips'), /AC02\s*上海青\s*\+.*20.*AC03\s*油麦菜\s*\+.*30/, 'preview chips')
await owner.screenshot({ path: `${SHOTS}/tp01-template.png` })
step('wrote 麻辣烫: AC02 上海青 20, AC03 油麦菜 30 (the code typed in lower case is kept upper case)')

// a duplicate code is refused
await owner.click(tid('tp-new'))
await owner.fill(tid('tp-name'), '重复')
for (const i of [0, 1]) {
  const row = owner.locator(tid('tp-row')).nth(i)
  await row.locator(tid('tp-code')).fill('X1')
  await row.locator(tid('tp-row-name')).fill('料' + i)
}
await owner.click(tid('tp-save'))
await expectText(owner.locator(tid('tp-error')), /X1/, 'duplicate code error')
step('a code used twice is refused')
await owner.click(tid('tp-item-麻辣烫'))

// apply it to the braised pork rice
await owner.click(tid('tp-apply'))
await owner.waitForSelector(tid('tp-apply-sheet'))
await owner.locator(`${tid('tp-dish-卤肉饭')} input`).check()
await owner.screenshot({ path: `${SHOTS}/tp02-apply.png` })
await owner.click(tid('tp-apply-save'))
await owner.waitForSelector(tid('tp-apply-sheet'), { state: 'detached' })
await expectText(owner.locator('.tp-use'), /卤肉饭/, 'used by')
step('applied 麻辣烫 to 卤肉饭')

// a second template, applied from the dish form of the beef noodles
await writeTemplate(owner, '羊肉泡馍加料', [['AB01', '烤羊肉', 100], ['AB02', '烤五花', 80]])
await owner.click(tid('tab-items'))
const beefRow = owner.locator('tr:has-text("牛肉面")').first()
await beefRow.locator('button:has-text("编辑")').click()
await owner.waitForSelector(tid('item-toppings'))
await owner.locator(tid('item-topping-羊肉泡馍加料')).check()
await owner.locator(tid('item-toppings')).scrollIntoViewIfNeeded()
await owner.screenshot({ path: `${SHOTS}/tp03-dish-form.png` })
await owner.click('.sheet-foot button.primary')
await owner.waitForSelector(tid('item-toppings'), { state: 'detached' })
await expectText(owner.locator(`tr:has-text("牛肉面") ${tid('item-toppings-tag')}`), /羊肉泡馍加料/, 'beef noodle tag')
await expectText(owner.locator(`tr:has-text("卤肉饭") ${tid('item-toppings-tag')}`), /麻辣烫/, 'rice tag')
await owner.screenshot({ path: `${SHOTS}/tp04-menu.png` })
step('the dish form picks a template; the menu list shows which dishes have toppings')

// saving the dish form again keeps the topping groups once, and its own spice group
const menu = await api(owner, 'GET', '/merchant/branches/1/menu')
const beef = menu.categories.flatMap(c => c.items).find(x => x.name === '牛肉面')
const tg = beef.groups.filter(g => g.topping_template_id)
if (tg.length !== 1 || tg[0].options.map(o => o.code + o.name).join() !== 'AB01烤羊肉,AB02烤五花') throw new Error('beef topping groups: ' + JSON.stringify(beef.groups))
if (!beef.groups.some(g => !g.topping_template_id && g.min_select > 0)) throw new Error('the beef noodles lost their spice group')
step('the dish keeps its own groups next to the template group')

// the phone layout of the toppings tab
const phone = await open({ width: 390, height: 844 })
await login(phone, 'owner1')
await phone.goto(BASE + '/merchant/menu?tab=toppings')
await phone.click(tid('tp-item-麻辣烫'))
await phone.waitForSelector(tid('tp-editor'))
await phone.screenshot({ path: `${SHOTS}/tp05-phone.png`, fullPage: true })
const over = await phone.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
if (over > 1) throw new Error(`the phone page scrolls sideways by ${over}px`)
step('the toppings tab fits a phone')

// ---------- the cashier orders for a customer: toppings by code ----------
const desk = await open({ width: 1440, height: 900 })
await login(desk, 'cashier1')
await desk.goto(BASE + '/merchant/order/delivery')
await desk.waitForSelector(tid('manual-order'))
const pick = desk.locator(tid('mo-pick'))
await pick.fill('b1')
await desk.waitForSelector(`${tid('mo-pick-drop')} .it.on:has-text("卤肉饭")`)
await pick.press('Enter')
await desk.waitForSelector(`${tid('mo-line')}:has-text("卤肉饭")`)
await desk.fill(tid('mo-addon-q'), 'ac0')
await desk.locator('.addon-hits .chip:has-text("AC02")').waitFor()
await desk.fill(tid('mo-addon-q'), 'AC03')
await desk.press(tid('mo-addon-q'), 'Enter')
await desk.fill(tid('mo-addon-q'), 'ac02')
await desk.press(tid('mo-addon-q'), 'Enter')
await desk.locator(`${tid('mo-addon')}:has-text("上海青")`).waitFor()
await desk.locator(`${tid('mo-addon')}:has-text("油麦菜")`).waitFor()
// 159 + 30 + 20
await expectText(desk.locator(`${tid('mo-line')}:has-text("卤肉饭")`), /209/, 'rice with two toppings')
await desk.screenshot({ path: `${SHOTS}/tp06-manual.png` })
step('typing AC02 / AC03 under the rice adds 上海青 and 油麦菜; the line is 209')

// ---------- dine-in: the sheet opens for a dish with toppings ----------
await desk.goto(BASE + '/merchant/order/dine')
await desk.waitForSelector(tid('dine-desk'))
const free = (await api(desk, 'GET', '/merchant/branches/1/tables')).tables.filter(x => x.active && !x.session)
if (!free.length) throw new Error('needs a free table; reseed the database')
await desk.click(tid(`dd-table-${free[free.length - 1].name}`))
await desk.click(tid('dd-guests-2'))
await desk.click(tid('dd-open-order'))
await desk.waitForSelector(tid('dd-round'))
await desk.locator(`[data-testid^="dd-cat-"]:has-text("主食")`).click()
await desk.click(tid(`dd-dish-${(menu.categories.flatMap(c => c.items).find(x => x.name === '卤肉饭')).id}`))
await desk.waitForSelector(tid('dd-sheet'))
await desk.locator(`${tid('dd-sheet')} ${tid('dd-opt')}:has-text("油麦菜")`).click()
await expectText(desk.locator(tid('dd-sheet')), /AC02/, 'codes on the chips')
await desk.screenshot({ path: `${SHOTS}/tp07-dine-sheet.png` })
await desk.click(tid('dd-sheet-add'))
await expectText(desk.locator(tid('dd-round')), /油麦菜/, 'round line')
step('at a table, tapping the rice opens the sheet with AC02 上海青 / AC03 油麦菜')

await dropTemplates(owner)
if (errors.length) throw new Error('page errors:\n' + errors.join('\n'))
await browser.close()
console.log('toppings e2e passed')
})().catch(e => { console.error(e); process.exit(1) })
