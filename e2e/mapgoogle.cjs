// The delivery map against a stand-in for the Google Maps API
// (e2e/fake-google.js): the drawing layer sits inside the map and stays on
// its positions while the map is dragged and zoomed, the pointer becomes a
// pen while drawing, and clicks and drags on the layer do not leak to the map.
// It checks how our code drives that interface, not the real map's looks.
// Usage: BASE_URL=http://localhost:8080 node e2e/mapgoogle.cjs   (needs seed data)
const { chromium } = require('playwright')
const { mkdirSync } = require('node:fs')
const path = require('node:path')

;(async () => {

const BASE = process.env.BASE_URL || 'http://localhost:8080'
const SHOTS = process.env.SHOTS_DIR || 'e2e/shots'
mkdirSync(SHOTS, { recursive: true })
const browser = await chromium.launch()
const errors = []
const ctx = await browser.newContext({ viewport: { width: 1360, height: 900 }, locale: 'zh-CN', ignoreHTTPSErrors: process.env.E2E_INSECURE_TLS === '1' })
await ctx.addInitScript({ path: path.join(__dirname, 'fake-google.js') })
// Whatever key the server has, the page gets one that selects the Google adapter.
await ctx.route('**/api/v1/config', async route => {
  const r = await route.fetch()
  await route.fulfill({ response: r, json: { ...(await r.json()), maps_key: 'fake-google-key' } })
})
const page = await ctx.newPage()
page.on('pageerror', e => errors.push(`pageerror: ${e.message}`))
page.on('console', m => { if (m.type() === 'error' && !/favicon|Failed to load resource/.test(m.text())) errors.push(`console: ${m.text()}`) })
page.on('dialog', d => d.accept())
const step = (s) => console.log('✓ ' + s)
const expect = (got, want, what) => { if (got !== want) throw new Error(`${what}: expected ${want}, got ${got}`) }
const close = (got, want, what, tol = 1) => { if (!(Math.abs(got - want) <= tol)) throw new Error(`${what}: expected about ${want}, got ${got}`) }
const tid = (id) => `[data-testid="${id}"]`
const mode = (m) => page.waitForSelector(`.dmap[data-mode=${m}]`)

await page.goto(BASE + '/login')
await page.fill('input[autocomplete=username]', 'owner2') // a store no other test draws on
await page.fill('input[type=password]', 'demo1234')
await page.click('button.primary')
await page.waitForURL(u => !u.pathname.startsWith('/login'))

// A store location and one drawn zone of this test's own, set up through the API.
const own = async (method, url, data) => {
  const r = await page.request.fetch(BASE + '/api/v1' + url, { method, data, headers: { 'X-Requested-With': 'waimai-web' } })
  if (!r.ok()) throw new Error(`${method} ${url}: ${r.status()} ${await r.text()}`)
  return r.json()
}
const me = await own('GET', '/auth/me')
const bid = me.roles[0].branch_id
const STORE = [14.5547, 121.0244]
// an empty region: as a store saved before regions were read off the map
await own('PUT', `/merchant/branches/${bid}/location`, { lat: STORE[0], lng: STORE[1], address: '123 Ayala Ave, Makati', region: {} })
const NAME = '谷歌测试区'
const CORNERS = [[14.5580, 121.0260], [14.5580, 121.0300], [14.5550, 121.0300], [14.5550, 121.0260]]
// Only this test's zone is drawn on the map while it runs; the others get their outlines back at the end.
const others = []
for (const z of (await own('GET', `/merchant/branches/${bid}/menu`)).zones) {
  if (z.name.startsWith('谷歌测试')) await own('DELETE', `/merchant/branches/${bid}/zones/${z.id}`)
  else if (z.polygon.length) { others.push(z); await own('PUT', `/merchant/branches/${bid}/zones/${z.id}`, { ...z, polygon: null }) }
}
await own('POST', `/merchant/branches/${bid}/zones`, { name: NAME, name_en: 'Google test', fee: 1000, min_order: 0, active: true, sort: 99, polygon: CORNERS, color: '#7c3aed', eta_minutes: null })

await page.goto(BASE + '/merchant/delivery-map')
await mode('browse')
// The location above was saved without a region (as before this was stored):
// opening the page reads it off the map and saves it.
await page.locator(`${tid('store-region')}:has-text("Philippines · Metro Manila · Makati · Bel-Air (1209)")`).waitFor()
const saved = (await own('GET', `/merchant/branches/${bid}/menu`)).branch
expect([saved.country_code, saved.country, saved.province, saved.city, saved.district, saved.postal_code].join('|'), 'PH|Philippines|Metro Manila|Makati|Bel-Air|1209', 'region saved with the store')
expect(await page.evaluate(() => window.__fakeAsked.geocode.at(-1).language), 'en', 'the region is asked for in English')
step('a store saved without a region gets country, province, city, district and postcode from the map')
await page.waitForSelector('.fake-pane .mapc-layer g[data-zone] polygon')
step('the drawing layer is mounted inside the map\'s own pane')

// Where the zone's first corner is drawn on screen, and where the map says it is.
const zone = (await own('GET', `/merchant/branches/${bid}/menu`)).zones.find(z => z.name === NAME)
const drawn = () => page.evaluate(id => {
  const poly = document.querySelector(`.mapc-layer g[data-zone="${id}"] polygon`)
  const p = poly.points.getItem(0), m = poly.getScreenCTM(), box = document.querySelector('[data-testid=map]').getBoundingClientRect()
  return { x: p.x * m.a + p.y * m.c + m.e - box.left, y: p.x * m.b + p.y * m.d + m.f - box.top }
}, zone.id)
const truth = () => page.evaluate(([lat, lng]) => window.__fakeMaps.at(-1).toContainer(lat, lng), CORNERS[0])
// Both read in the same instant, so that an animation cannot come between them.
const offBy = () => page.evaluate(({ id, corner }) => {
  const poly = document.querySelector(`.mapc-layer g[data-zone="${id}"] polygon`), t = window.__fakeMaps.at(-1).toContainer(corner[0], corner[1])
  const p = poly.points.getItem(0), m = poly.getScreenCTM(), box = document.querySelector('[data-testid=map]').getBoundingClientRect()
  return { dx: p.x * m.a + p.y * m.c + m.e - box.left - t.x, dy: p.x * m.b + p.y * m.d + m.f - box.top - t.y }
}, { id: zone.id, corner: CORNERS[0] })
const same = async (what) => { const d = await offBy(); close(d.dx, 0, what + ' (x)'); close(d.dy, 0, what + ' (y)') }
const settled = () => page.waitForFunction(() => { const m = window.__fakeMaps.at(-1); return m.shown === m.getZoom() })
await settled() // the page fits the map to the zones when it opens
await same('at rest')

// ---- panning: the layer rides with the map, also in the middle of a drag ----
const box = await page.locator(tid('map')).boundingBox()
const C = { x: box.x + box.width / 2, y: box.y + box.height / 2 }
const before = await drawn()
await page.mouse.move(C.x - 200, C.y + 250)
await page.mouse.down()
await page.mouse.move(C.x - 140, C.y + 290, { steps: 5 })
const mid = await drawn()
close(mid.x - before.x, 60, 'the zone follows the drag before the button is released (x)')
close(mid.y - before.y, 40, 'the zone follows the drag before the button is released (y)')
await same('during a drag')
await page.mouse.move(C.x - 80, C.y + 330, { steps: 5 })
await page.mouse.up()
await same('after a drag')
step('dragging the map carries the zones with it, with no catching up')

// ---- zooming: on every animation frame the zone is where the map puts it ----
const frames = await page.evaluate(async ({ id, corner }) => {
  const map = window.__fakeMaps.at(-1), out = []
  const poly = () => document.querySelector(`.mapc-layer g[data-zone="${id}"] polygon`)
  const box = document.querySelector('[data-testid=map]').getBoundingClientRect()
  map.setZoom(map.getZoom() - 1)
  for (let i = 0; i < 40; i++) {
    await new Promise(r => requestAnimationFrame(r))
    // Vue redraws on the next tick; look just after it.
    await new Promise(r => setTimeout(r))
    const p = poly().points.getItem(0), m = poly().getScreenCTM(), t = map.toContainer(corner[0], corner[1])
    out.push({ zoom: map.shown, dx: p.x * m.a + p.y * m.c + m.e - box.left - t.x, dy: p.x * m.b + p.y * m.d + m.f - box.top - t.y })
    if (map.shown === map.getZoom()) break
  }
  return out
}, { id: zone.id, corner: CORNERS[0] })
const between = frames.filter(f => f.zoom % 1 !== 0)
if (between.length < 3) throw new Error('the zoom did not animate: ' + JSON.stringify(frames))
for (const f of frames) if (Math.abs(f.dx) > 1 || Math.abs(f.dy) > 1) throw new Error(`the zone is off by ${f.dx.toFixed(1)}, ${f.dy.toFixed(1)} px at zoom ${f.zoom.toFixed(2)}`)
await settled()
await same('after a zoom')
step(`zooming keeps the zones on their positions through all ${between.length} in-between frames`)

// ---- a click on a zone selects it and is not also a click on the map ----
const onZone = await truth()
await page.mouse.click(box.x + onZone.x + 25, box.y + onZone.y + 25)
await page.waitForSelector('.dmap-zone.sel')
await page.waitForTimeout(100)
expect(await page.locator('.dmap-zone.sel').getAttribute('data-zone-name'), NAME, 'the clicked zone stays selected')
step('a click on a zone belongs to the zone, the map underneath does not take it')

// ---- the pointer ----
const cursor = () => page.evaluate(() => window.__fakeMaps.at(-1).opts.draggableCursor || '')
expect(await cursor(), '', 'the map\'s own pointer while browsing')
await page.click(tid('tool-draw'))
await mode('draw')
expect(/^url\(.*\) 3 29, crosshair$/.test(await cursor()), true, 'a pen, with its tip as the hot spot, while drawing')
await page.screenshot({ path: `${SHOTS}/g1-pen.png` })

// ---- drawing by clicking still works through the map's click events ----
const click = (dx, dy) => page.mouse.click(C.x + dx, C.y + dy)
await click(-260, -200); await click(-60, -200); await click(-60, -40); await click(-260, -40)
expect(await page.textContent(tid('point-count')), '4 个点', 'points while drawing')
await page.mouse.move(C.x - 258, C.y - 198)
await page.waitForFunction(() => window.__fakeMaps.at(-1).opts.draggableCursor === 'pointer')
await click(-260, -200)
await mode('edit')
expect(await cursor(), '', 'the pen is put away once the outline is closed')
expect(await page.textContent(tid('vertex-count')), '4', 'corners of the finished outline')
step('the pointer is a pen while drawing, a hand over the first corner, and the map\'s own afterwards')

// ---- dragging a corner moves the corner, not the map ----
const centre = () => page.evaluate(() => { const c = window.__fakeMaps.at(-1).getCenter(); return [c.lat(), c.lng()] })
const c0 = await centre()
const v = await page.locator(tid('vertex-2')).boundingBox()
await page.mouse.move(v.x + v.width / 2, v.y + v.height / 2)
await page.mouse.down()
await page.mouse.move(v.x + v.width / 2 + 50, v.y + v.height / 2 + 30, { steps: 4 })
await page.mouse.up()
const v2 = await page.locator(tid('vertex-2')).boundingBox()
close(v2.x - v.x, 50, 'the corner follows the pointer (x)', 2)
close(v2.y - v.y, 30, 'the corner follows the pointer (y)', 2)
const c1 = await centre()
expect(c1[0] === c0[0] && c1[1] === c0[1], true, 'the map stays put while a corner is dragged')
await page.click(tid('tool-browse'))
await mode('browse')
step('dragging a corner moves the corner and leaves the map where it is')

await page.click(tid('tool-location'))
await mode('location')
expect(/^url\(.*\) 16 30, crosshair$/.test(await cursor()), true, 'a pin while placing the store')
await page.click(tid('tool-browse'))

// ---- the customer's address search stays in the store's country ----
const slug = saved.slug
try {
  const cust = await ctx.newPage()
  cust.on('pageerror', e => errors.push(`pageerror: ${e.message}`))
  await cust.setViewportSize({ width: 390, height: 844 })
  await cust.goto(`${BASE}/s/${slug}`)
  await cust.click(tid('deliver-to'))
  await cust.waitForSelector(tid('picker'))
  await cust.waitForSelector(tid('pin-result'))
  const c = await cust.evaluate(() => { const c = window.__fakeMaps.at(-1).getCenter(); return [c.lat(), c.lng()] })
  close(c[0], STORE[0], 'the customer map opens on the store (lat)', 0.0001); close(c[1], STORE[1], 'the customer map opens on the store (lng)', 0.0001)
  // Without the Places library the search is the geocoder's.
  await cust.evaluate(() => { window.__fakeGeocodes = true })
  await cust.fill(tid('pin-search'), 'trump tower')
  await cust.locator(`${tid('pin-place')}:has-text("trump tower, PH")`).waitFor()
  const g = await cust.evaluate(() => { const r = window.__fakeAsked.geocode.filter(x => x.address).at(-1); return { country: r.componentRestrictions?.country, region: r.region } })
  expect(g.country + '/' + g.region, 'PH/ph', 'the geocoder is kept to the store\'s country')
  // With it, suggestions are.
  await cust.evaluate(() => { window.__fakePlaces = true })
  await cust.fill(tid('pin-search'), 'trump towers')
  await cust.locator(`${tid('pin-place')}:has-text("Places, ph")`).waitFor()
  const s = await cust.evaluate(() => { const r = window.__fakeAsked.suggest.at(-1); return { codes: r.includedRegionCodes, bias: r.locationBias } })
  expect(s.codes.join(), 'ph', 'suggestions are kept to the store\'s country')
  close(s.bias.center.lat, STORE[0], 'suggestions look near the store first (lat)', 0.0001); close(s.bias.center.lng, STORE[1], 'suggestions look near the store first (lng)', 0.0001)
  expect(await cust.locator(tid('picker') + ' svg polygon').count(), 0, 'no zone outlines on the customer map')
  await cust.close()
  step('a customer searching "trump tower" is only offered places in the store\'s country, near the store')
} finally { /* nothing to put back */ }

await own('DELETE', `/merchant/branches/${bid}/zones/${zone.id}`)
for (const z of others) await own('PUT', `/merchant/branches/${bid}/zones/${z.id}`, z)
if (errors.length) throw new Error('browser errors:\n' + errors.join('\n'))
await browser.close()
console.log('mapgoogle: all steps passed')

})().catch(e => { console.error(e); process.exit(1) })
