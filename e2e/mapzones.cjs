// The merchant's delivery map in a real browser, on the tile-less test map:
// place the store, draw zones by clicking, decide an overlap, close a gap,
// reshape a zone, and switch customers to "pin on the map".
// Usage: BASE_URL=http://localhost:8080 node e2e/mapzones.cjs
// Needs seed data and the API started with GOOGLE_MAPS_API_KEY=stub.
const { chromium } = require('playwright')
const { customerLogin } = require('./lib.cjs')
const { mkdirSync } = require('node:fs')

;(async () => {

const BASE = process.env.BASE_URL || 'http://localhost:8080'
const SHOTS = process.env.SHOTS_DIR || 'e2e/shots'
mkdirSync(SHOTS, { recursive: true })
const browser = await chromium.launch()
const errors = []
const ctx = await browser.newContext({ viewport: { width: 1360, height: 900 }, locale: 'zh-CN', ignoreHTTPSErrors: process.env.E2E_INSECURE_TLS === '1' })
const page = await ctx.newPage()
page.on('pageerror', e => errors.push(`pageerror: ${e.message}`))
page.on('console', m => { if (m.type() === 'error' && !/favicon|Failed to load resource/.test(m.text())) errors.push(`console: ${m.text()}`) })
page.on('dialog', d => d.accept())
const step = (s) => console.log('✓ ' + s)
const shot = (name) => page.screenshot({ path: `${SHOTS}/${name}.png` })
const expect = (got, want, what) => { if (got !== want) throw new Error(`${what}: expected ${want}, got ${got}`) }
const tid = (id) => `[data-testid="${id}"]`
const mode = (m) => page.waitForSelector(`.dmap[data-mode=${m}]`)
const api = async (method, path, body) => {
  const r = await fetch(BASE + '/api/v1' + path, { method, headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'waimai-web' }, body: body ? JSON.stringify(body) : undefined })
  return r.json()
}

const cfg = await api('GET', '/config')
if (cfg.maps_key !== 'stub') throw new Error('start the API with GOOGLE_MAPS_API_KEY=stub for this test')

await page.goto(BASE + '/login')
await page.fill('input[autocomplete=username]', 'owner1')
await page.fill('input[type=password]', 'demo1234')
await page.click('button.primary')
await page.waitForURL(u => !u.pathname.startsWith('/login'))
// Start from a clean slate so the test can run again on the same database:
// "pick from a list", no outlines on the seeded zones, no zones of its own.
const own = async (method, path, data) => {
  const r = await page.request.fetch(BASE + '/api/v1' + path, { method, data, headers: { 'X-Requested-With': 'waimai-web' } })
  if (!r.ok()) throw new Error(`${method} ${path}: ${r.status()} ${await r.text()}`)
  return r.json()
}
const me = await own('GET', '/auth/me')
const bid = me.roles[0].branch_id
const menu = await own('GET', `/merchant/branches/${bid}/menu`)
if (menu.branch.zone_mode === 'map') await own('PUT', `/merchant/branches/${bid}/zone-mode`, { zone_mode: 'list', zone_tolerance_m: 200 })
for (const z of menu.zones) {
  if (z.name.startsWith('地图测试')) await own('DELETE', `/merchant/branches/${bid}/zones/${z.id}`)
  else if (z.polygon.length) await own('PUT', `/merchant/branches/${bid}/zones/${z.id}`, { ...z, polygon: null })
}
const seeded = ['马卡蒂 CBD', '萨尔塞多村', 'BGC'].map(n => menu.zones.find(z => z.name === n).id)
await own('PUT', `/merchant/branches/${bid}/zone-order`, { ids: seeded })

await page.goto(BASE + '/merchant/delivery-map')
await page.waitForSelector(tid('map'))

// ---- store location ----
if (await page.locator('.dmap[data-mode=browse]').count()) await page.click(tid('tool-location'))
await mode('location')
await page.fill(tid('loc-lat'), '14.5547')
await page.fill(tid('loc-lng'), '121.0244')
await page.fill(tid('loc-address'), '123 Ayala Ave, Makati')
await page.click(tid('save-location'))
await mode('browse')
expect(await page.textContent(tid('store-latlng')), '14.55470, 121.02440', 'saved store location')
step('the owner places the store on the map')

// Reload: the map opens centred on the store at street level (about 4.6 m per pixel).
await page.reload()
await mode('browse')
await page.waitForSelector(tid('store-pin'))
const box = await page.locator(tid('map')).boundingBox()
const C = { x: box.x + box.width / 2, y: box.y + box.height / 2 }
const click = (dx, dy) => page.mouse.click(C.x + dx, C.y + dy)
const rowOf = async (name) => {
  const names = await page.locator('[data-zone-name]').evaluateAll(els => els.map(e => e.getAttribute('data-zone-name')))
  return names.indexOf(name)
}
async function drawFor(name, pts) {
  await page.click(tid('edit-' + await rowOf(name)))
  await mode('draw')
  for (const [x, y] of pts) await click(x, y)
}

// ---- first zone: click four corners, close on the first one ----
await drawFor('马卡蒂 CBD', [[-200, -150], [0, -150], [0, 100], [-200, 100]])
expect(await page.textContent(tid('point-count')), '4 个点', 'points while drawing')
await shot('m1-drawing')
await click(-200, -150)
await mode('edit')
expect(await page.inputValue(tid('zone-name')), '马卡蒂 CBD', 'the form keeps the zone that is being drawn')
expect(await page.textContent(tid('vertex-count')), '4', 'corners of the finished outline')
await page.fill(tid('zone-eta'), '30')
await page.click(tid('save-zone'))
await mode('browse')
step('an outline is drawn by clicking and saved with fee and delivery time')

// ---- second zone overlaps the first: the owner decides who wins ----
await drawFor('萨尔塞多村', [[-80, -60], [150, -60], [150, 60], [-80, 60]])
await page.click(tid('finish-draw'))
await page.waitForSelector(tid('overlap-dialog'))
expect(await page.locator(tid('overlap-hatch')).count(), 1, 'the overlap is hatched')
expect(await page.isDisabled(tid('overlap-ok')), true, 'no choice yet')
await shot('m2-overlap')
await page.click(tid('choose-new'))
await page.click(tid('overlap-ok'))
await mode('edit')
await page.waitForSelector(tid('overlap-banner'))
await page.click(tid('save-zone'))
await mode('browse')
expect(await rowOf('萨尔塞多村') < await rowOf('马卡蒂 CBD'), true, '"new covers old" puts the new zone above')
step('an overlap asks which zone wins; "new covers old" moves the new zone up')

// ---- snapping: a corner clicked near a neighbour lands exactly on it ----
await page.click(tid('new-zone'))
await mode('draw')
await page.mouse.move(C.x - 195, C.y + 106)
await page.waitForSelector(tid('snap-ring'))
await page.click(tid('cancel-draw'))
await mode('browse')
step('the pointer snaps to a neighbouring corner while drawing')

// ---- third zone drawn 15 px (about 70 m) below the first, snapping off: a gap ----
await page.click(tid('new-zone'))
await mode('draw')
await page.click(tid('snap-switch'))
for (const [x, y] of [[-200, 115], [0, 115], [0, 250], [-200, 250]]) await click(x, y)
await click(-200, 115)
await mode('edit')
await page.fill(tid('zone-name'), '地图测试区')
await page.fill(tid('zone-name-en'), 'Map test')
await page.fill(tid('zone-fee'), '59')
await page.fill(tid('zone-min'), '300')
await page.click(tid('save-zone'))
await mode('browse')
await page.waitForSelector(tid('gap-count'))
await page.click(tid('open-gaps'))
await mode('gaps')
await page.waitForSelector(tid('gap-0'))
expect(await page.locator(tid('gap-shape')).count() > 0, true, 'the gap is drawn on the map')
await shot('m3-gaps')
await page.click(tid('close-gap-0'))
await page.waitForSelector(tid('no-gaps'))
await page.click(tid('undo-close'))
await page.waitForSelector(tid('gap-0'))
await page.click(tid('close-gap-0'))
await page.waitForSelector(tid('no-gaps'))
step('a gap between two zones is found, closed in one click, undone and closed again')

// ---- tolerance ----
await page.fill(tid('tolerance'), '150')
await page.click(tid('save-tolerance'))
await page.waitForSelector(tid('map-notice'))
await page.click(tid('gaps-back'))
await mode('browse')
step('the tolerance is saved')

// ---- reshape: drag a corner, add one, remove one ----
await page.click(tid('edit-' + await rowOf('地图测试区')))
await mode('edit')
const before = Number(await page.textContent(tid('vertex-count')))
const mid = await page.locator(tid('mid-1')).boundingBox()
await page.mouse.move(mid.x + mid.width / 2, mid.y + mid.height / 2)
await page.mouse.down()
await page.mouse.move(mid.x + 60, mid.y + 10, { steps: 4 })
await page.mouse.up()
expect(Number(await page.textContent(tid('vertex-count'))), before + 1, 'dragging a midpoint adds a corner')
await page.click(tid('vertex-2'), { button: 'right' })
expect(Number(await page.textContent(tid('vertex-count'))), before, 'right-click removes a corner')
await shot('m4-edit')
await page.click(tid('tool-browse'))
await mode('browse')
step('corners can be added by dragging a midpoint and removed with a right-click')

// ---- a circle around the store ----
await page.click(tid('tool-circle'))
await mode('circle')
await page.fill(tid('radius'), '0.3')
await page.click(tid('make-circle'))
await page.waitForSelector(tid('overlap-dialog'))
await page.click(tid('choose-old'))
await page.click(tid('overlap-ok'))
await mode('edit')
await page.fill(tid('zone-name'), '地图测试圆')
await page.click(tid('save-zone'))
await mode('browse')
expect(await rowOf('地图测试圆') > await rowOf('萨尔塞多村'), true, '"old covers new" keeps the new zone below')
step('a circle around the store becomes a zone; "old covers new" keeps it below')
await shot('m5-overview')

// ---- customers pin their address; the server decides the zone ----
await page.click(tid('mode-map'))
await page.waitForSelector(tid('mode-map') + '.on')
const at = await api('POST', '/stores/makati/locate', { lat: 14.5547, lng: 121.0244 })
expect(at.deliverable, true, 'the store itself can be delivered to')
expect(at.zone, undefined, 'a customer is not told which zone a spot is in')
const far = await api('POST', '/stores/makati/locate', { lat: 14.70, lng: 121.20 })
expect(far.deliverable, false, 'a far-away pin is out of range')
step('customers are placed by their pin: the covering zone wins, far away is out of range')

// ---- the customer side: pin the address, see the area, order ----
const phone = await browser.newContext({ viewport: { width: 390, height: 844 }, locale: 'zh-CN', ignoreHTTPSErrors: process.env.E2E_INSECURE_TLS === '1' })
const cust = await phone.newPage()
cust.on('pageerror', e => errors.push(`pageerror: ${e.message}`))
cust.on('console', m => { if (m.type() === 'error' && !/favicon|Failed to load resource/.test(m.text())) errors.push(`console: ${m.text()}`) })
const result = (state) => cust.waitForSelector(`[data-testid=pin-result][data-state=${state}]`)
const goTo = async (latlng) => { await cust.fill(tid('pin-search'), latlng); await cust.click(`${tid('pin-place')}:has-text("${latlng}")`) }
await cust.goto(BASE + '/s/makati')
await cust.waitForSelector('.item')
expect(await cust.locator('select').count(), 0, 'no area list in map mode')
await cust.click('.item:has-text("瓶装水") .stepper button.fill')
await cust.click('.item:has-text("冰柠檬茶") .stepper button.fill')
await cust.click(tid('deliver-to')) // "can you deliver to me?"
await cust.waitForSelector(tid('picker'))
await result('in')
expect(await cust.locator(tid('picker') + ' svg polygon, ' + tid('picker') + ' svg circle.zone').count(), 0, 'no zone outlines on the customer map')
await goTo('14.70, 121.20')
await result('out')
expect(await cust.isDisabled(tid('pin-confirm')), true, 'out of range cannot be confirmed')
expect(await cust.locator(tid('pin-pickup')).count(), 1, 'out of range offers pickup')
await cust.screenshot({ path: `${SHOTS}/m6-out-of-range.png` })
// about 80 m east of the top zone's edge: outside every outline, inside the tolerance
await goTo('14.5547, 121.0316')
await result('in')
// drag the map a little: the pin follows and is checked again
const pm = await cust.locator(tid('pin-map')).boundingBox()
await cust.mouse.move(pm.x + pm.width / 2, pm.y + pm.height / 2 + 60)
await cust.mouse.down()
await cust.mouse.move(pm.x + pm.width / 2, pm.y + pm.height / 2 + 75, { steps: 3 })
await cust.mouse.up()
await result('in')
await cust.screenshot({ path: `${SHOTS}/m7-near-edge.png` })
await cust.click(tid('pin-confirm'))
await cust.waitForSelector(tid('picker'), { state: 'detached' })
await cust.waitForSelector(tid('deliver-terms'))
expect((await cust.locator('body').innerText()).includes('萨尔塞多村'), false, 'the store page does not name the zone')
step('a customer asks about a spot: in range, out of range, and near the edge within the tolerance; no zone is shown')

await cust.click('.bottombar .btn.primary')
// the new address starts from the spot just looked up
await customerLogin(cust, null, { name: '地图顾客' })
expect((await cust.locator('body').innerText()).includes('萨尔塞多村'), false, 'checkout does not name the zone')
await cust.click('.bottombar .btn.primary')
await cust.waitForURL('**/o/**')
const orders = await own('GET', `/merchant/branches/${bid}/orders`)
const mine = (orders.orders ?? orders).find(o => o.customer_name === '地图顾客')
expect(mine.zone_name, '萨尔塞多村', 'the order is in the zone the server matched')
expect(mine.zone_distance_m > 30 && mine.zone_distance_m < 150, true, 'the order records how far outside the outline the pin was')
expect(Math.abs(mine.dest_lat - 14.5547) < 0.002 && Math.abs(mine.dest_lng - 121.0316) < 0.002, true, 'the order keeps the pin for the rider')
step('the order is priced by the matched zone and keeps the pin for the rider')

// Leave the store the way the other end-to-end tests expect it.
await page.click(tid('mode-list'))
await page.waitForSelector(tid('mode-list') + '.on')

if (errors.length) throw new Error('browser errors:\n' + errors.join('\n'))
await browser.close()
console.log('mapzones: all steps passed')

})().catch(e => { console.error(e); process.exit(1) })
