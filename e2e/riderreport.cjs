// The rider work summary in a real browser: two customers pin their address,
// one rider takes both orders out in one trip and comes back, and the owner
// reads the trip under Staff > Rider work and opens its route on the map.
// The background worker looks at trips once a minute, so this takes about
// two minutes.
// Usage: BASE_URL=http://localhost:8080 node e2e/riderreport.cjs
//        (needs seed data and the API started with GOOGLE_MAPS_API_KEY=stub; with
//        GOOGLE_ROUTES_API_KEY=stub as well, the planned route is a road route)
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
const must = async (page, method, path, body) => {
  const r = await api(page, method, path, body)
  if (r.status >= 300) throw new Error(`${method} ${path}: ${r.status} ${JSON.stringify(r.body)}`)
  return r.body
}
const step = (s) => console.log('✓ ' + s)
const shot = (page, name) => page.screenshot({ path: `${SHOTS}/${name}.png`, fullPage: true })
const expect = (got, want, what) => { if (got !== want) throw new Error(`${what}: want ${want}, got ${got}`) }
const tid = (id) => `[data-testid=${id}]`
const sleepUntil = (ms) => new Promise(r => setTimeout(r, Math.max(0, ms - Date.now())))

// The store, and two addresses a few hundred metres away.
const S = [14.5547, 121.0244]
const A = [S[0] + 0.0039, S[1]], B = [S[0] + 0.0039, S[1] + 0.0036]

// ---- owner: the store is on the map and customers pin their address ----
const owner = await open({ width: 1400, height: 900 })
await login(owner, 'owner1')
await owner.waitForURL('**/merchant/orders')
const bid = 1
await must(owner, 'PUT', `/merchant/branches/${bid}/location`, { lat: S[0], lng: S[1], address: '123 Ayala Ave, Makati' })
const menu = await must(owner, 'GET', `/merchant/branches/${bid}/menu`)
const zone = menu.zones.find(z => z.active)
const hadOutline = zone.polygon.length > 0
if (!hadOutline) await must(owner, 'PUT', `/merchant/branches/${bid}/zones/${zone.id}`, { ...zone,
  polygon: [[S[0] - 0.01, S[1] - 0.01], [S[0] - 0.01, S[1] + 0.01], [S[0] + 0.01, S[1] + 0.01], [S[0] + 0.01, S[1] - 0.01]] })
const wasMode = menu.branch.zone_mode
await must(owner, 'PUT', `/merchant/branches/${bid}/zone-mode`, { zone_mode: 'map', zone_tolerance_m: 200 })
const restore = async () => {
  await must(owner, 'PUT', `/merchant/branches/${bid}/zone-mode`, { zone_mode: wasMode, zone_tolerance_m: menu.branch.zone_tolerance_m })
  if (!hadOutline) await must(owner, 'PUT', `/merchant/branches/${bid}/zones/${zone.id}`, { ...zone, polygon: null })
}

try {
  const summary = (from) => must(owner, 'GET', `/merchant/branches/${bid}/reports/riders?from=${from}&to=${from}`)
  const today = new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Manila' }).format(new Date())
  const before = (await summary(today)).riders.find(r => r.username === 'wang1') ?? { trips: 0, delivered: 0 }

  // ---- two customers, each with a pin ----
  const desk = await open({ width: 1280, height: 800 })
  await login(desk, 'cashier1')
  async function order(name, pin) {
    const cust = await open({ width: 390, height: 844 })
    await cust.goto(BASE + '/s/makati')
    await cust.waitForSelector('.item')
    await cust.click('.item:has-text("瓶装水") .stepper button.fill')
    await cust.click('.bottombar .btn.primary')
    await cust.waitForSelector(tid('picker'))
    await cust.fill(tid('pin-search'), pin.join(', '))
    await cust.press(tid('pin-search'), 'Enter')
    await cust.waitForSelector('[data-testid=pin-result][data-state=in]')
    await cust.waitForTimeout(400)
    await cust.click(tid('pin-confirm'))
    await cust.waitForSelector(tid('picker'), { state: 'detached' })
    await cust.click('.bottombar .btn.primary')
    await cust.waitForURL('**/checkout')
    await cust.fill('input[autocomplete=name]', name)
    await cust.fill('input[autocomplete=street-address]', name + ' St')
    await cust.fill('input[type=tel]', '+63918' + String(Date.now()).slice(-7))
    await cust.click('button:has-text("发送验证码")')
    await cust.waitForSelector('input[autocomplete=one-time-code]')
    await cust.click('button:has-text("验证"):not(:has-text("发送"))')
    await cust.waitForSelector('.banner.ok')
    await cust.locator('.bottombar .btn.primary').click()
    await cust.waitForURL('**/o/**')
    await cust.waitForFunction(() => document.querySelector('[data-testid=status]')?.textContent?.includes('等待商家接单'))
    const no = (await cust.locator('.topbar h1').innerText()).replace('订单号', '').trim()
    await cust.close()
    await desk.click(`.ord:has-text("${no}")`)
    await desk.click('.detail button:has-text("接单")')
    await desk.waitForSelector('.detail select')
    await desk.selectOption('.detail select', await desk.locator('.detail select option[data-rider=wang1]').getAttribute('value'))
    await desk.waitForSelector('.detail :text("小王")')
    return no
  }
  const noA = await order('North', A), noB = await order('East', B)
  step(`orders ${noA} and ${noB} placed with a pin, accepted and assigned to 小王`)

  // ---- 小王 packs both, then takes them out as a rider ----
  const rider = await open({ width: 390, height: 844 })
  await login(rider, 'wang1')
  await rider.waitForURL('**/work')
  for (const no of [noA, noB]) {
    const pack = rider.locator(`[data-pack="${no}"]`)
    await pack.locator('[data-act=pack]').click()
    for (const box of await rider.locator('[data-testid=pack-sheet] input[type=checkbox]').all()) await box.check()
    await rider.click('[data-act=confirm-pack]')
    await pack.locator(':text("已打包")').waitFor()
  }
  const tasks = (await must(rider, 'GET', '/rider/tasks')).tasks
  const task = (no) => tasks.find(o => o.public_no === no).delivery.id
  await must(rider, 'POST', `/rider/tasks/${task(noA)}/pickup`)
  await must(rider, 'POST', `/rider/tasks/${task(noB)}/pickup`)
  const t0 = Date.now()
  let s = await summary(today)
  let mine = s.riders.find(r => r.username === 'wang1')
  expect(mine.trips, before.trips + 1, 'two orders collected together are one trip')
  expect(mine.on_trip, true, 'the rider is on a trip')
  step('two orders collected at the store are one trip')

  // The phone reports the ride: north to A, east to B, straight back. It all
  // happens within a minute, at motorbike speed.
  const leg = (from, to, n) => Array.from({ length: n }, (_, i) => [from[0] + (to[0] - from[0]) * (i + 1) / n, from[1] + (to[1] - from[1]) * (i + 1) / n])
  const ride = [...leg(S, A, 3), ...leg(A, B, 3), ...leg(B, [S[0] + 0.0002, S[1] + 0.0001], 5)]
  const points = ride.map((p, i) => ({ lat: p[0], lng: p[1], accuracy: 8, at: new Date(t0 + (i + 1) * 5000).toISOString() }))
  expect((await must(rider, 'POST', '/rider/locations', { points })).accepted, points.length, 'positions accepted')
  await sleepUntil(t0 + 16000)
  await must(rider, 'POST', `/rider/tasks/${task(noA)}/deliver`, { collected_amount: tasks.find(o => o.public_no === noA).totals.total })
  await sleepUntil(t0 + 31000)
  await must(rider, 'POST', `/rider/tasks/${task(noB)}/deliver`, { collected_amount: tasks.find(o => o.public_no === noB).totals.total })

  // ---- owner: Staff > Rider work ----
  await owner.click('.staffbar a:has-text("员工")')
  await owner.click('[data-testid=staff-tabs] [data-tab=riders]')
  await owner.waitForURL('**/merchant/staff/riders')
  const row = owner.locator('[data-testid=rr-riders] tr[data-rider=wang1]')
  await row.waitFor()
  expect(Number(await row.locator('[data-col=delivered]').innerText()), before.delivered + 2, 'orders delivered')
  await row.click()
  const trips = owner.locator('[data-testid=rr-trips]')
  await trips.locator(`tr[data-trip]:has-text("${noA}"):has-text("${noB}")`).waitFor()
  await shot(owner, 'r01-rider-summary-on-the-road')

  // the worker sees the rider back within two turns
  let trip
  for (let i = 0; i < 40; i++) {
    const d = await must(owner, 'GET', `/merchant/branches/${bid}/reports/riders/${mine.id}?from=${today}&to=${today}`)
    trip = d.trips[d.trips.length - 1]
    if (trip.status === 'done' && trip.actual_m > 1000) break
    await owner.waitForTimeout(5000)
  }
  expect(trip.end_reason, 'returned', 'the trip ended back at the store')
  expect(trip.actual_m > 1250 && trip.actual_m < 1550, true, `about 1.4 km ridden (got ${trip.actual_m})`)
  // With GOOGLE_ROUTES_API_KEY=stub the planned route runs along streets; without, it is straight lines.
  const road = (await must(owner, 'GET', `/merchant/branches/${bid}/reports/riders?from=${today}&to=${today}`)).road_routes
  expect(trip.planned_source, road ? 'google' : 'straight', 'where the planned route comes from')
  if (road) expect(trip.planned_mode === 'TWO_WHEELER' && trip.planned_seconds > 0 && trip.compare_m > trip.actual_m - 200, true, `a road route (${trip.compare_m} m planned)`)
  else expect(Math.abs(trip.over_m) < 200, true, `ridden as planned (over ${trip.over_m})`)
  await owner.reload()
  await row.click()
  const line = trips.locator(`tr[data-trip="${trip.id}"]`)
  await line.waitFor()
  expect(await line.locator('[data-col=back] .tag').count(), 0, 'the trip shows when the rider was back')
  expect(await line.locator('[data-col=actual]').innerText(), `${(trip.actual_m / 1000).toFixed(1)} 公里`, 'distance in the list')
  await owner.locator(tid('rr-total')).waitFor()
  await shot(owner, 'r02-rider-summary')
  step('the summary lists the rider, the trip, its orders in the order dropped, and both distances')

  // ---- the route on the map ----
  await line.locator('[data-act=route]').click()
  await owner.waitForURL(`**/merchant/staff/trips/${trip.id}`)
  await owner.locator(tid('trip-map') + ' ' + tid('planned-line')).waitFor()
  await owner.locator(tid('trip-map') + ' ' + tid('actual-line')).first().waitFor()
  expect(await owner.locator(tid('trip-map') + ' [data-stop]').count(), 2, 'a pin for each drop')
  await owner.locator(`${tid('trip-map')} [data-stop="${noA}"]:has-text("1")`).waitFor()
  await owner.locator(tid('store-pin')).waitFor()
  expect(await owner.locator(tid('planned-km')).innerText(), `${(trip.compare_m / 1000).toFixed(1)} 公里`, 'planned distance')
  expect(await owner.locator(tid('actual-km')).innerText(), `${(trip.actual_m / 1000).toFixed(1)} 公里`, 'distance ridden')
  expect(await owner.locator(`${tid('trip-events')} li`).count() >= 5, true, 'collected, left, two drops, back')
  await owner.locator(`${tid('trip-events')} li:has-text("回到店里")`).waitFor()
  if (road) {
    await owner.locator(`${tid('planned-by')}:has-text("摩托车")`).waitFor()
    await owner.locator(`${tid('road-note')}:has-text("预计用时")`).waitFor()
  }
  await owner.waitForTimeout(600)
  await shot(owner, road ? 'r04-trip-route-google' : 'r03-trip-route')
  // the slider moves the marker; unticking a route hides its line
  const where = () => owner.locator(tid('trip-marker')).getAttribute('transform')
  const end = await where()
  await owner.locator('.slider input[name=at]').fill('20')
  expect(await where() !== end, true, 'the marker follows the slider')
  await owner.uncheck('input[name=planned]')
  expect(await owner.locator(tid('planned-line')).count(), 0, 'the planned line can be hidden')
  await owner.check('input[name=planned]')
  await owner.click('[data-act=back]')
  await owner.waitForURL('**/merchant/staff/riders')
  step('the route page draws both routes, the drops and what happened; the slider shows where the rider was')

  // English, and no access for a cashier
  await owner.click('.staffbar .lang')
  await owner.locator('[data-testid=staff-tabs] :text("Rider work")').waitFor()
  await owner.click('.staffbar .lang')
  expect((await api(desk, 'GET', `/merchant/branches/${bid}/reports/riders?from=${today}&to=${today}`)).status, 403, 'a cashier cannot read the summary')
  expect((await api(desk, 'GET', `/merchant/branches/${bid}/trips/${trip.id}`)).status, 403, 'a cashier cannot read a trip')
  step('the page is bilingual; a cashier cannot read it')

  // leave 小王 off duty, as the other tests expect
  await must(rider, 'POST', '/rider/duty', { on_duty: false })
} finally {
  await restore()
}

await browser.close()
if (errors.length) { console.error(errors.join('\n')); process.exit(1) }
console.log('rider report e2e passed')

})().catch(e => { console.error(e); process.exit(1) })
