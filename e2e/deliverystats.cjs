// Customer addresses and where deliveries go, in a real browser
// (docs/DELIVERY_ADDRESS_STATS_DESIGN.md): the store's address list with
// search and the orders at an address, the ranking with each address's
// units, the 31-day limit, the map's dots and heat; the platform's page over
// every store, narrowed to one; on a PC, an iPad and a phone.
// Usage: BASE_URL=http://localhost:8080 node e2e/deliverystats.cjs
// (needs seed data and the API started with GOOGLE_MAPS_API_KEY=stub)
const { chromium } = require('playwright')
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
const uniq = String(Date.now()).slice(-7)

async function signIn(user, viewport) {
  const ctx = await browser.newContext({ viewport, deviceScaleFactor: Number(process.env.DPR || 1), locale: 'zh-CN',
    ignoreHTTPSErrors: process.env.E2E_INSECURE_TLS === '1' })
  const page = await ctx.newPage()
  page.on('pageerror', e => errors.push(`pageerror: ${e.message}`))
  page.on('console', m => { if (m.type() === 'error' && !/favicon|Failed to load resource/.test(m.text())) errors.push(`console: ${m.text()}`) })
  await page.goto(BASE + '/login')
  await page.fill('input[autocomplete=username]', user)
  await page.fill('input[type=password]', 'demo1234')
  await page.click('button.primary')
  await page.waitForURL(u => !u.pathname.startsWith('/login'))
  return page
}
const noSideScroll = async (page, what) =>
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1), true, `${what} fits without scrolling sideways`)

// ---- a few deliveries to look at ----
const page = await signIn('owner1', { width: 1440, height: 900 })
const call = (method, path, body, headers = {}) => page.evaluate(async ([method, path, body, headers]) => {
  const r = await fetch('/api/v1' + path, { method, headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'fetch', ...headers }, body: body ? JSON.stringify(body) : undefined, credentials: 'include' })
  return r.json()
}, [method, path, body, headers])
const bid = String((await call('GET', '/auth/me')).roles[0].branch_id)
const menu = await call('GET', `/merchant/branches/${bid}/menu`)
const item = menu.categories.flatMap(c => c.items).find(i => i.published && i.delivery_available !== false && !(i.groups ?? []).some(g => g.min_select > 0))
const building = `Tower ${uniq}, 6750 Ayala Ave`
const place = async (phone, line, unit, lat, lng) => {
  const r = await call('POST', `/merchant/branches/${bid}/staff-orders`, {
    member: { phone, source: 'wechat', name: `E2E ${phone.slice(-3)}`, address_line: line, address_unit: unit, lat, lng },
    service_type: 'delivery', items: [{ item_id: item.id, qty: 1 }], accept: true,
  }, { 'Idempotency-Key': 'ds-' + uniq + '-' + Math.random() })
  if (!r.order) throw new Error('could not place an order: ' + JSON.stringify(r))
}
await place(`+63917${uniq}`, building, '23F', 14.5551, 121.0248)
await place(`+63917${uniq}`, building.toLowerCase() + '.', '23F', 14.5551, 121.0248)
await place(`+63918${uniq}`, building, '18F', 14.5551, 121.0248)
step('three deliveries to one building, written two ways')

// ---- the store: the address list ----
await page.goto(BASE + '/merchant/orders')
await page.click(tid('nav-addresses'))
await page.waitForURL(u => u.pathname === '/merchant/addresses')
await page.waitForSelector(tid('dab-row'))
await page.screenshot({ path: `${SHOTS}/ds-01-store-list-pc.png` })
await page.fill(tid('dab-search'), uniq)
await page.press(tid('dab-search'), 'Enter')
await page.waitForFunction(() => document.querySelectorAll('[data-testid=dab-row]').length === 2)
const rows = await page.locator(tid('dab-row')).allInnerTexts()
expect(rows.some(r => r.includes('23F') && /\b2\b/.test(r)), true, 'the 23F customer has two orders, the address written two ways')
step('the address list finds the building, one row per customer and unit')
await page.locator(tid('dab-row')).filter({ hasText: '23F' }).locator(tid('dab-orders')).click()
await page.waitForFunction(() => document.querySelectorAll('[data-testid=dab-order]').length === 2)
step('the orders at an address')
await page.screenshot({ path: `${SHOTS}/ds-02-store-orders-pc.png` })
await page.keyboard.press('Escape')
await page.click('[data-testid=dab-orders-sheet] .sheet-foot .btn')

// ---- the ranking ----
await page.click(tid('tab-top'))
await page.waitForSelector(tid('dab-kpis'))
await page.click(tid('preset-today'))
await page.waitForFunction(b => [...document.querySelectorAll('[data-testid=top-row]')].some(r => r.innerText.includes(b)), building)
const row = page.locator(tid('top-row')).filter({ hasText: building })
expect((await row.innerText()).includes('3'), true, 'the building has three orders')
await row.locator(tid('top-detail')).click()
await page.waitForFunction(() => document.querySelectorAll('[data-testid=top-unit]').length === 2)
step('the ranking counts the building once, its two units in the details')
await page.click(tid('preset-d30'))
await page.waitForSelector(tid('top-row'))
await page.screenshot({ path: `${SHOTS}/ds-03-store-top-pc.png` })
await page.fill(tid('dab-from'), '2025-01-01')
await page.waitForSelector(tid('dab-too-long'))
step('more than 31 days is refused')
await page.click(tid('preset-d7'))

// ---- the map ----
await page.click(tid('tab-map'))
await page.waitForSelector(tid('map-dot'))
await page.waitForSelector(tid('heat-layer'))
expect(await page.locator(tid('map-store')).count() >= 1, true, 'the store is on the map')
step('the map shows dots, heat and the store')
await page.screenshot({ path: `${SHOTS}/ds-04-store-map-pc.png` })
await page.click(tid('mode-pins'))
await page.waitForFunction(() => !document.querySelector('[data-testid=heat-layer]'))
await page.click(tid('mode-heat'))
await page.waitForFunction(() => !document.querySelector('[data-testid=map-dot]') && !!document.querySelector('[data-testid=heat-layer]'))
await page.screenshot({ path: `${SHOTS}/ds-05-store-heat-pc.png` })
step('pins only, heat only')
await page.click(tid('mode-both'))
await page.click(tid('tab-top'))
await page.locator(tid('top-row')).filter({ hasText: building }).locator(tid('top-on-map')).click()
await page.waitForSelector(tid('map-card'))
expect((await page.locator(tid('map-card')).innerText()).includes(String(3)), true, 'the card counts three orders')
await page.screenshot({ path: `${SHOTS}/ds-06-store-map-card-pc.png` })
step('"On the map" opens the address on the map')
await noSideScroll(page, 'the store map on a PC')

// ---- iPad and phone ----
for (const [name, vp] of [['ipad', { width: 820, height: 1180 }], ['phone', { width: 390, height: 844 }]]) {
  const p = await signIn('owner1', vp)
  for (const tab of ['list', 'top', 'map']) {
    await p.goto(BASE + '/merchant/addresses?tab=' + tab)
    await p.waitForSelector(tab === 'map' ? tid('map-dot') : tab === 'top' ? tid('dab-kpis') : tid('dab-row'))
    await p.waitForTimeout(300)
    await noSideScroll(p, `${tab} on ${name}`)
    await p.screenshot({ path: `${SHOTS}/ds-07-store-${tab}-${name}.png` })
  }
  await p.context().close()
  step(`fits an ${name}`)
}

// ---- the platform ----
const admin = await signIn('admin', { width: 1440, height: 900 })
await admin.goto(BASE + '/admin/dashboard')
await admin.click(tid('nav-delivery-addresses'))
await admin.waitForURL(u => u.pathname === '/admin/delivery-addresses')
await admin.waitForSelector(tid('dab-row'))
await admin.selectOption(tid('dab-store'), bid)
await admin.fill(tid('dab-search'), uniq)
await admin.press(tid('dab-search'), 'Enter')
await admin.waitForFunction(() => document.querySelectorAll('[data-testid=dab-row]').length === 2)
await admin.screenshot({ path: `${SHOTS}/ds-08-admin-list-pc.png` })
step('the platform narrows to one store')
await admin.selectOption(tid('dab-store'), '0')
await admin.click(tid('tab-top'))
await admin.waitForSelector(tid('top-row'))
await admin.screenshot({ path: `${SHOTS}/ds-09-admin-top-pc.png` })
await admin.click(tid('tab-map'))
await admin.waitForSelector(tid('map-dot'))
expect(await admin.locator(tid('show-zones')).count(), 0, 'no zone outlines across stores')
await admin.screenshot({ path: `${SHOTS}/ds-10-admin-map-pc.png` })
step('the platform ranks and maps every store')

if (errors.length) throw new Error('browser errors:\n' + errors.join('\n'))
await browser.close()
console.log('deliverystats: all good')
})().catch(e => { console.error(e); process.exit(1) })
