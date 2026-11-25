// 点单 and 消息: the back office has a "点单" group with two pages, 堂食下单 (the
// dine-in desk) and 外卖下单 (delivery and pickup). Each is its own page with
// no switch between them; old /merchant/orders/new links land on the page they
// meant; each page follows its own permission. Run on a computer, a tablet and
// a phone. Every group with pages under it folds and starts folded. 消息 groups 客户消息 (customers' chats) and 平台消息 (platform support).
// Usage: BASE_URL=http://localhost:8080 node e2e/pospages.cjs   (needs seed data)
const { chromium, devices } = require('playwright')
const { mkdirSync } = require('node:fs')

;(async () => {
const BASE = process.env.BASE_URL || 'http://localhost:8080'
const SHOTS = process.env.SHOTS_DIR || 'e2e/shots'
mkdirSync(SHOTS, { recursive: true })
const browser = await chromium.launch()
const errors = []
const step = (s) => console.log('✓ ' + s)
const expect = (got, want, what) => { if (got !== want) throw new Error(`${what}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`) }
const tid = (id) => `[data-testid=${id}]`
const path = (page) => new URL(page.url()).pathname
const query = (page) => Object.fromEntries(new URL(page.url()).searchParams)

async function open(name, opts) {
  const ctx = await browser.newContext({ ...opts, deviceScaleFactor: Number(process.env.DPR || opts.deviceScaleFactor || 1), locale: 'zh-CN' })
  const page = await ctx.newPage()
  page.on('pageerror', e => errors.push(`${name} pageerror: ${e.message}`))
  page.on('console', m => { if (m.type() === 'error' && !/favicon|Failed to load resource/.test(m.text())) errors.push(`${name} console: ${m.text()}`) })
  return page
}
async function login(page, user) {
  await page.goto(BASE + '/login')
  await page.fill('input[autocomplete=username]', user)
  await page.fill('input[type=password]', 'demo1234')
  await page.click('button.primary')
  await page.waitForURL(u => !/\/login(\/pick)?$/.test(u.pathname))
}
const api = (page, method, url, body) => page.evaluate(async ([method, url, body]) => {
  const r = await fetch('/api/v1' + url, { method, credentials: 'same-origin', headers: { 'X-Requested-With': 'waimai-web', 'X-Session': 'staff', 'Content-Type': 'application/json' }, body: body ? JSON.stringify(body) : undefined })
  if (!r.ok) throw new Error(`${method} ${url}: ${r.status} ${await r.text()}`)
  return r.json()
}, [method, url, body])
// the side menu: a column on a computer, a drawer behind ☰ on a tablet or a phone
// Pages under a group may be folded away: open the group first.
const GROUP = { 'nav-pos-dine': 'nav-pos', 'nav-pos-delivery': 'nav-pos', 'nav-members': 'nav-cm', 'nav-addresses': 'nav-cm',
  'nav-messages': 'nav-msg', 'nav-support': 'nav-msg' }
async function nav(page, id) {
  const head = GROUP[id] ? tid(GROUP[id]) : tid(id)
  if (!(await page.locator(head).isVisible())) await page.click(tid('nav-open'))
  await page.locator(head).waitFor({ state: 'visible' })
  if (!(await page.locator(tid(id)).count())) await page.click(head)
  await page.click(tid(id))
}

const sizes = [
  ['pc', { viewport: { width: 1440, height: 900 } }],
  ['ipad', { viewport: { width: 820, height: 1180 }, hasTouch: true, isMobile: true }],
  ['phone', { ...devices['iPhone 13'] }],
]
let owner
for (const [name, opts] of sizes) {
  const page = await open(name, opts)
  await login(page, 'owner1')
  await page.goto(BASE + '/merchant/orders')
  await page.locator(`${tid('nav-open')}:visible, ${tid('nav-pos')}:visible`).first().waitFor()
  if (name === 'pc') owner = page
  if (await page.locator(tid('nav-open')).isVisible()) await page.click(tid('nav-open'))
  await page.locator(tid('nav-pos')).waitFor({ state: 'visible' })
  const menu = () => page.locator('.shell-links > *').evaluateAll(els => els.map(e => e.getAttribute('data-testid') || e.textContent.trim()))
  // every group with pages under it starts folded (the orders page is in none of them)
  expect((await menu()).slice(0, 8).join(','), 'nav-pos,订单,nav-cm,桌台,后厨,nav-msg,菜单,nav-deco', `${name}: folded menu`)
  for (const id of ['nav-pos-dine', 'nav-progress', 'nav-members', 'nav-messages', 'nav-media', 'nav-cash'])
    expect(await page.locator(tid(id)).count(), 0, `${name}: ${id} folded away`)
  await page.waitForTimeout(300)   // the drawer slides in
  await page.screenshot({ path: `${SHOTS}/fold-${name}-folded.png` })
  for (const id of ['nav-pos', 'nav-orders-fold', 'nav-cm', 'nav-msg', 'nav-deco', 'nav-fin']) await page.click(tid(id))
  expect(await page.locator(tid('nav-pos')).getAttribute('aria-expanded'), 'true', `${name}: 点单 opened`)
  const opened = await menu()
  for (const want of ['nav-pos,nav-pos-dine,nav-pos-delivery,订单,nav-progress,nav-voids,nav-cm,nav-members,nav-addresses',
    'nav-msg,nav-messages,nav-support', 'nav-deco,nav-media,nav-templates', 'nav-fin,nav-cash,nav-bills,nav-reconcile']) {
    const first = want.split(',')[0], i = opened.indexOf(first)
    expect(opened.slice(i, i + want.split(',').length).join(','), want, `${name}: ${first} opened`)
  }
  const label = async id => (await page.locator(tid(id)).innerText()).trim()
  expect([await label('nav-pos'), await label('nav-pos-dine'), await label('nav-pos-delivery')].join(','), '点单,堂食下单,外卖下单', `${name}: 点单 names`)
  expect([await label('nav-msg'), await label('nav-messages'), await label('nav-support')].join(','), '消息,客户消息,平台消息', `${name}: 消息 names`)
  expect([await label('nav-cm'), await label('nav-members'), await label('nav-addresses')].join(','), '客户管理,会员列表,送餐地址', `${name}: 客户管理 names`)
  expect([await label('nav-deco'), await label('nav-media'), await label('nav-templates')].join(','), '店铺装修,宣传物料,模板', `${name}: 店铺装修 names`)
  expect([await label('nav-fin'), await label('nav-cash'), await label('nav-bills'), await label('nav-reconcile')].join(','), '财务,核款,账单,对账', `${name}: 财务 names`)
  await page.waitForTimeout(300)   // the drawer slides in
  await page.screenshot({ path: `${SHOTS}/fold-${name}-open.png` })
  await page.click(tid('nav-deco'))
  expect(await page.locator(tid('nav-media')).count(), 0, `${name}: 店铺装修 folds again`)
  // a page opened from a link opens its own group and only that one
  await page.goto(BASE + '/merchant/bills')
  await page.locator(`${tid('nav-open')}:visible, ${tid('nav-fin')}:visible`).first().waitFor()
  if (await page.locator(tid('nav-open')).isVisible()) await page.click(tid('nav-open'))
  await page.locator(tid('nav-bills')).waitFor({ state: 'visible' })
  expect(await page.locator(`${tid('nav-pos-dine')}, ${tid('nav-members')}, ${tid('nav-messages')}, ${tid('nav-media')}`).count(), 0, `${name}: other groups stay folded`)
  await page.waitForTimeout(300)
  await page.screenshot({ path: `${SHOTS}/fold-${name}-bills.png` })

  await nav(page, 'nav-pos-dine')
  await page.waitForURL(u => u.pathname === '/merchant/order/dine')
  await page.waitForSelector(tid('dine-desk'))
  await page.locator('[data-testid^="dd-table-"]').first().waitFor()
  expect((await page.locator(tid('dd-title')).innerText()).trim(), '堂食下单', `${name}: dine-in title`)
  expect(await page.locator(`${tid('mode-delivery')}, ${tid('mode-dine')}, .dd-seg, .mo-modes`).count(), 0, `${name}: no switch on dine-in`)
  await page.screenshot({ path: `${SHOTS}/pos-${name}-dine.png` })

  await nav(page, 'nav-pos-delivery')
  await page.waitForURL(u => u.pathname === '/merchant/order/delivery')
  await page.waitForSelector(tid('manual-order'))
  expect((await page.locator(tid('mo-title')).innerText()).trim(), '外卖下单', `${name}: delivery title`)
  expect(await page.locator(`${tid('mode-delivery')}, ${tid('mode-dine')}, .dd-seg, .mo-modes`).count(), 0, `${name}: no switch on delivery`)
  await page.fill(tid('mo-pick'), 'b1')
  await page.waitForSelector(`${tid('mo-pick-drop')} .it.on`)
  await page.press(tid('mo-pick'), 'Enter')
  await page.locator(tid('mo-line')).first().waitFor()
  await page.screenshot({ path: `${SHOTS}/pos-${name}-delivery.png` })
  await nav(page, 'nav-members')
  await page.waitForURL(u => u.pathname === '/merchant/members')
  expect((await page.locator('.ox-head h1').innerText()).trim(), '会员列表', `${name}: member list title`)
  await nav(page, 'nav-addresses')
  await page.waitForURL(u => u.pathname === '/merchant/addresses')
  expect((await page.locator('.ox-head h1').innerText()).trim(), '送餐地址', `${name}: delivery addresses title`)
  await nav(page, 'nav-messages')
  await page.waitForURL(u => u.pathname === '/merchant/messages')
  expect((await page.locator('.ox-head h1').innerText()).trim(), '客户消息', `${name}: customer messages title`)
  await page.screenshot({ path: `${SHOTS}/msg-${name}-customer.png` })
  await nav(page, 'nav-support')
  await page.waitForURL(u => u.pathname === '/merchant/support')
  expect((await page.locator('.ox-head h1').innerText()).trim(), '平台消息', `${name}: platform messages title`)
  await page.screenshot({ path: `${SHOTS}/msg-${name}-platform.png` })
  if (name !== 'pc') await page.context().close()
  step(`${name}: 点单 > 堂食下单 and 外卖下单 are separate pages without a switch`)
}

// ---------- the orders page and old links ----------
const page = owner
await page.goto(BASE + '/merchant/orders')
await page.click(tid('manual-order-new'))
await page.waitForURL(u => u.pathname === '/merchant/order/delivery')
expect((await page.locator(tid('mo-title')).innerText()).trim(), '外卖下单', '"+ 外卖下单" on the orders page')
const tables = (await api(page, 'GET', '/merchant/branches/1/tables')).tables.filter(x => x.active)
await page.goto(BASE + `/merchant/orders/new?mode=dine&table=${tables[0].id}`)
await page.waitForURL(u => u.pathname === '/merchant/order/dine')
expect(JSON.stringify(query(page)), JSON.stringify({ table: String(tables[0].id) }), 'old dine-in link keeps the table')
await page.waitForSelector(tid('dine-desk'))
await page.goto(BASE + '/merchant/orders/new')   // the page this device used last: dine-in
await page.waitForURL(u => u.pathname === '/merchant/order/dine')
await page.goto(BASE + '/merchant/orders/new?append=12345')
await page.waitForURL(u => u.pathname === '/merchant/order/delivery')
expect(query(page).append, '12345', 'old add-on link keeps the order')
await page.goto(BASE + '/merchant/orders/new')   // now delivery was last
await page.waitForURL(u => u.pathname === '/merchant/order/delivery')
step('"+ 外卖下单" and old /merchant/orders/new links go to the right page')

// ---------- each page follows its own permission ----------
const before = await api(page, 'GET', '/merchant/branches/1/role-perms')
const cashier = await open('cashier', { viewport: { width: 1440, height: 900 } })
try {
  await api(page, 'PUT', '/merchant/branches/1/role-perms', { cashier: before.cashier.filter(p => p !== 'table.use'), manager: before.manager })
  await login(cashier, 'cashier1')
  await cashier.goto(BASE + '/merchant/orders')
  await cashier.click(tid('nav-pos'))
  await cashier.waitForSelector(tid('nav-pos-delivery'))
  expect(await cashier.locator(tid('nav-pos-dine')).count(), 0, 'no dine-in link without 桌台收银')
  await cashier.goto(BASE + '/merchant/orders/new?mode=dine')
  await cashier.waitForURL(u => u.pathname === '/merchant/order/delivery')
  await cashier.goto(BASE + '/merchant/order/dine')
  await cashier.waitForSelector(tid('no-perm'))
  step('an order taker without 桌台收银 sees only 外卖下单')

  await api(page, 'PUT', '/merchant/branches/1/role-perms', { cashier: [...before.cashier.filter(p => p !== 'order.manual'), 'table.use'], manager: before.manager })
  await cashier.goto(BASE + '/merchant/orders/new?append=1')
  await cashier.waitForURL(u => u.pathname === '/merchant/order/dine')
  await cashier.waitForSelector(tid('nav-pos-dine'))
  expect(await cashier.locator(tid('nav-pos-delivery')).count(), 0, 'no delivery link without 代客下单')
  await cashier.waitForSelector(tid('dine-desk'))
  step('an order taker with only 桌台收银 sees only 堂食下单')
} finally {
  await api(page, 'PUT', '/merchant/branches/1/role-perms', { cashier: before.cashier, manager: before.manager })
}

if (errors.length) throw new Error(errors.join('\n'))
await browser.close()
console.log('pospages: all passed')
})().catch(e => { console.error(e); process.exit(1) })
