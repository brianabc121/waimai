// The menu page as redrawn (菜单管理): the category rail with counts, search
// and filters, list and grid, the "more" menu, moving a dish down (kept by the
// server), and the topping tab's live preview with sold-out toppings and the
// toppings' total. Usage: BASE_URL=http://localhost:8080 node e2e/menupage.cjs  (needs seed data)
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
const expect = (ok, what) => { if (!ok) throw new Error(what) }
async function open(viewport) {
  const ctx = await browser.newContext({ viewport, locale: 'zh-CN' })
  const page = await ctx.newPage()
  page.on('pageerror', e => errors.push(`pageerror: ${e.message}`))
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
const rows = (page) => page.locator('tr.mx-row')

const owner = await open({ width: 1440, height: 900 })
await login(owner, 'owner1')
await owner.goto(BASE + '/merchant/menu')
await owner.waitForSelector('table.data')
const menu0 = await api(owner, 'GET', '/merchant/branches/1/menu')
const total = menu0.categories.reduce((n, c) => n + c.items.length, 0)
expect((await owner.locator(tid('tab-items')).innerText()).includes(String(total)), 'the dishes tab shows how many dishes')
expect(await rows(owner).count() === Math.min(total, 20), 'every dish is listed')
const drinks = menu0.categories.find(c => c.name === '饮品')
await owner.click(tid('cat-饮品'))
await owner.waitForFunction(n => document.querySelectorAll('tr.mx-row').length === n, drinks.items.length)
expect((await owner.locator(tid('cat-饮品')).innerText()).includes(String(drinks.items.length)), 'the rail shows the count')
await owner.click(tid('cat-all'))
step('the rail lists the categories with their counts and narrows the list')

const all = menu0.categories.flatMap(c => c.items)
const count = (n) => owner.waitForFunction(n => document.querySelectorAll('tr.mx-row').length === n, Math.min(n, 20))
await owner.fill(tid('menu-search'), '奶茶')
await count(all.filter(i => (i.name + i.name_en + (i.code ?? '')).toLowerCase().includes('奶茶')).length)
await owner.fill(tid('menu-search'), 'b1')
await count(all.filter(i => [i.name, i.name_en, i.code ?? ''].some(x => x.toLowerCase().includes('b1'))).length)
await owner.fill(tid('menu-search'), '')
await owner.selectOption(tid('menu-status'), 'limited')
await count(all.filter(i => i.stock_qty !== null).length)
await owner.selectOption(tid('menu-status'), '')
await owner.selectOption(tid('menu-station'), 'none')
await count(all.filter(i => !i.needs_prep).length)
await owner.selectOption(tid('menu-station'), '')
await count(all.length)
step('search by name or code, and filter by status and station')

await owner.click(tid('view-grid'))
await owner.waitForSelector('.mx-card')
expect(await owner.locator('.mx-card').count() === Math.min(total, 20), 'the grid shows every dish')
await owner.screenshot({ path: `${SHOTS}/mp-grid.png` })
await owner.click(tid('view-list'))
await owner.waitForSelector('tr.mx-row')
step('list and grid views')

// move the first dish of the first category down, from its "more" menu
const first = menu0.categories[0]
await owner.click(tid('item-more-' + first.items[0].id))
await owner.click(`${tid('item-menu')} button[data-act="down"]`)
await owner.waitForFunction(async ([a, b]) => {
  const r = await fetch('/api/v1/merchant/branches/1/menu', { headers: { 'X-Requested-With': 'waimai-web', 'X-Session': 'staff' } })
  const m = await r.json(); const ids = m.categories[0].items.map(i => i.id); return ids[0] === b && ids[1] === a
}, [first.items[0].id, first.items[1].id])
await owner.click(tid('item-more-' + first.items[0].id))
await owner.click(`${tid('item-menu')} button[data-act="up"]`)
await owner.waitForFunction(async (a) => {
  const r = await fetch('/api/v1/merchant/branches/1/menu', { headers: { 'X-Requested-With': 'waimai-web', 'X-Session': 'staff' } })
  return (await r.json()).categories[0].items[0].id === a
}, first.items[0].id)
step('a dish moves down and back up, and the order is kept')

// several dishes at once: sold out, then back
const ids = first.items.slice(0, 2).map(i => i.id)
for (const id of ids) await owner.locator(`${tid('item-row-' + id)} input[type=checkbox]`).check()
await owner.click(`${tid('menu-bulk')} button[data-act="sold"]`)
await owner.waitForFunction(ids => ids.every(id => document.querySelector(`[data-testid="item-row-${id}"] .pill.sold`)), ids)
for (const id of ids) await owner.locator(`${tid('item-row-' + id)} input[type=checkbox]`).check()
await owner.click(`${tid('menu-bulk')} button[data-act="restore"]`)
await owner.waitForFunction(ids => ids.every(id => !document.querySelector(`[data-testid="item-row-${id}"] .pill.sold`)), ids)
step('several dishes marked sold out and back at once')
await owner.screenshot({ path: `${SHOTS}/mp-list.png` })

// ---- the topping tab: a sold-out topping cannot be picked, the total adds up ----
const tpl = await api(owner, 'POST', '/merchant/branches/1/topping-templates', { name: 'E2E预览', icon: '🧋', title: '加点好料', max_select: 2,
  options: [['P1', '珍珠', 1500, false], ['P2', '布丁', 2000, false], ['P3', '椰果', 1000, true]].map(([code, name, price, sold_out]) => ({ id: 0, code, name, name_en: '', price, sold_out })) })
await owner.goto(BASE + '/merchant/menu?tab=toppings')
await owner.click(tid('tp-item-E2E预览'))
await owner.waitForSelector(tid('tp-preview'))
const opts = owner.locator(tid('tp-pv-opt'))
expect(await opts.nth(2).isDisabled(), 'a sold-out topping cannot be picked')
await opts.nth(0).click(); await opts.nth(1).click()
expect((await owner.locator(tid('tp-pv-sum')).innerText()).includes('35'), 'two toppings add up to 35')
await owner.locator(tid('tp-row')).nth(1).locator(tid('tp-sold')).check()
await owner.waitForFunction(() => document.querySelector('[data-testid="tp-pv-sum"]').innerText.includes('15'))
await owner.screenshot({ path: `${SHOTS}/mp-toppings.png` })
await api(owner, 'DELETE', `/merchant/branches/1/topping-templates/${tpl.id}`)
step('the topping preview skips sold-out toppings and adds up the chosen ones')

// ---- a phone: nothing runs off the side ----
const phone = await open({ width: 390, height: 844 })
await login(phone, 'owner1')
for (const q of ['', '?tab=notes', '?tab=toppings']) {
  await phone.goto(BASE + '/merchant/menu' + q)
  await phone.waitForTimeout(800)
  const over = await phone.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(over <= 1, `the phone page ${q || 'dishes'} scrolls sideways by ${over}px`)
}
step('the three tabs fit a phone')

if (errors.length) throw new Error(errors.join('\n'))
console.log('menu page e2e passed')
await browser.close()
})().catch(e => { console.error(e); process.exit(1) })
