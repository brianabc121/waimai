// The rider side of the workbench in a real browser: clocking on, sharing the
// position, the desk's rider page, the customer's tracking map, delivery
// steps taken without signal, and the installable page.
// Usage: BASE_URL=http://localhost:8080 node e2e/duty.cjs   (needs seed data)
const { chromium } = require('playwright')
const { mkdirSync } = require('node:fs')

;(async () => {

const BASE = process.env.BASE_URL || 'http://localhost:8080'
const SHOTS = process.env.SHOTS_DIR || 'e2e/shots'
mkdirSync(SHOTS, { recursive: true })
const browser = await chromium.launch()
const errors = []

async function open(viewport, extra = {}) {
  const ctx = await browser.newContext({ viewport, locale: 'zh-CN', ignoreHTTPSErrors: process.env.E2E_INSECURE_TLS === '1', ...extra })
  const page = await ctx.newPage()
  page.on('pageerror', e => errors.push(`pageerror: ${e.message}`))
  page.on('console', m => { if (m.type() === 'error' && !/favicon|Failed to load resource|ERR_INTERNET_DISCONNECTED/.test(m.text())) errors.push(`console: ${m.text()}`) })
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
const step = (s) => console.log('✓ ' + s)
const shot = (page, name) => page.screenshot({ path: `${SHOTS}/${name}.png`, fullPage: true })
const api = (page, method, path, body) => page.evaluate(async ([method, path, body]) => {
  const r = await fetch('/api/v1' + path, { method, headers: { 'X-Requested-With': 'e2e', 'Content-Type': 'application/json' },
    body: body ? JSON.stringify(body) : undefined })
  return { status: r.status, body: await r.json().catch(() => null) }
}, [method, path, body])

// ---- a bottle of water for delivery: nothing to cook, so it can be packed at once ----
const cust = await open({ width: 390, height: 844 })
await cust.goto(BASE + '/s/makati')
await cust.waitForSelector('.item')
await cust.selectOption('select', { index: 1 })
await cust.click('.item:has-text("瓶装水") .stepper button.fill')
await cust.click('.bottombar .btn.primary')
await cust.waitForURL('**/checkout')
await cust.fill('input[autocomplete=name]', '李小红')
await cust.fill('input[autocomplete=street-address]', '12 Paseo de Roxas')
await cust.fill('input[type=tel]', '+63918' + String(Date.now()).slice(-7))
await cust.click('button:has-text("发送验证码")')
await cust.waitForSelector('input[autocomplete=one-time-code]')
await cust.click('button:has-text("验证"):not(:has-text("发送"))')
await cust.waitForSelector('.banner.ok')
await cust.locator('.bottombar .btn.primary').click()
await cust.waitForURL('**/o/**')
const expectStatus = (text) => cust.waitForFunction(t => document.querySelector('[data-testid=status]')?.textContent?.includes(t), text, { timeout: 10000 })
await expectStatus('等待商家接单')
const orderNo = (await cust.locator('.topbar h1').innerText()).replace('订单号', '').trim()

const desk = await open({ width: 1280, height: 800 })
await login(desk, 'cashier1')
await desk.click(`.ord:has-text("${orderNo}")`)
await desk.click('.detail button:has-text("接单")')
await desk.waitForSelector('.detail select')
await desk.selectOption('.detail select', await desk.locator('.detail select option[data-rider=wang1]').getAttribute('value'))
await desk.waitForSelector('.detail :text("小王")')
step(`order ${orderNo} accepted and assigned to 小王`)

// ---- 小王: pack in store, then the rider identity ----
const ctxOpts = { permissions: ['geolocation'], geolocation: { latitude: 14.5547, longitude: 121.0244, accuracy: 8 } }
const rider = await open({ width: 390, height: 844 }, ctxOpts)
await login(rider, 'wang1')
await rider.waitForURL('**/work')
const pack = rider.locator(`[data-pack="${orderNo}"]`)
await pack.locator('[data-act=pack]').click()
for (const box of await rider.locator('[data-testid=pack-sheet] input[type=checkbox]').all()) await box.check()
await rider.click('[data-act=confirm-pack]')
await pack.locator(':text("已打包")').waitFor()
// In the in-store identity nobody reads the position, even on duty.
await rider.click('[data-testid=identity]')
await rider.click('[data-testid=switch-go]')
const duty = rider.locator('[data-testid=duty]')
await duty.waitFor()
// Packing on the workbench without clocking on already started the shift:
// one shift for the person, whichever identity is in front.
await rider.waitForSelector('[data-testid=duty][data-on=true]')
await duty.locator('[data-act=duty]').click()
await rider.waitForSelector('[data-testid=duty][data-on=false]')
step('working without clocking on started the shift; the rider clocks off')
await shot(rider, 'd01-off-duty')

// the installable page
const manifest = await rider.evaluate(async () => (await fetch(document.querySelector('link[rel=manifest]').href)).json())
if (manifest.start_url !== '/work' || manifest.icons.length < 2) throw new Error('manifest: ' + JSON.stringify(manifest))
await rider.waitForFunction(() => navigator.serviceWorker.controller || navigator.serviceWorker.getRegistration().then(r => !!r?.active))
step('the workbench has a manifest and a service worker')

// ---- clock on: the position is shared and the store sees the rider on duty ----
await duty.locator('[data-act=duty]').click()
await rider.waitForSelector('[data-testid=duty][data-on=true][data-geo=ok]')
await rider.waitForFunction(async () => {
  const r = await fetch('/api/v1/rider/duty'); const s = await r.json(); return s.on_duty && s.lat != null
})
const st = (await api(rider, 'GET', '/rider/duty')).body
if (Math.abs(st.lat - 14.5547) > 0.001 || Math.abs(st.lng - 121.0244) > 0.001) throw new Error('position: ' + JSON.stringify(st))
await shot(rider, 'd02-on-duty')
const owner = await open({ width: 1280, height: 860 })
await login(owner, 'owner1')
await owner.waitForURL('**/merchant/orders')
await owner.goto(BASE + '/merchant/staff')
await owner.locator('tr[data-staff="wang1"]:has-text("上班中")').waitFor()
await owner.locator('tr[data-staff="rider1"]:has-text("没上班")').waitFor()
await shot(owner, 'd03-staff-on-duty')
step('clocked on: position reached the server, the staff page shows who is on duty')

// ---- the desk's rider page: the list in picking order and a pin on the map ----
await owner.click('.staffbar a:has-text("骑手")')
await owner.waitForURL('**/merchant/riders')
await owner.locator('[data-rider=wang1]:has-text("上班中")').waitFor()
await owner.locator('[data-rider=wang1]:has-text("1 单在送")').waitFor()
await owner.locator('[data-rider-pin=wang1]').waitFor()
const firstRider = await owner.locator('.rrow').first().getAttribute('data-rider')
if (firstRider !== 'wang1') throw new Error('the rider on duty should be listed first, got ' + firstRider)
await shot(owner, 'd03b-riders-map')
// The rider had the assignment on screen, so the desk does not flag it.
const seenOrder = (await api(owner, 'GET', '/merchant/branches/1/orders?scope=active')).body.orders.find(o => o.public_no === orderNo)
if (!seenOrder?.delivery?.seen_at) throw new Error('the assignment should be marked as seen by the rider')
step('riders page: on duty first, a pin on the map; the assignment is marked seen')
await owner.goto(BASE + '/merchant/staff')

// Switching back to the in-store identity pauses the position.
await rider.click('[data-testid=identity]')
await rider.click('[data-testid=switch-go]')
await rider.click('[data-testid=identity]')
await rider.click('[data-testid=switch-go]')
await rider.waitForSelector('[data-testid=duty][data-on=true]')

// ---- no signal: pickup, arrival and delivery are kept on the phone ----
const task = rider.locator(`[data-delivery="${orderNo}"]`)
await task.locator('[data-act=open]').click()
await rider.waitForSelector('[data-act=pickup]:not([disabled])')
if (await cust.locator('[data-testid=tracking]').count()) throw new Error('the customer must not see the rider before pickup')
await rider.click('[data-act=pickup]')
await expectStatus('配送中')
// On the way: the customer sees where the rider is.
await cust.locator('[data-testid=tracking][data-has-position=true]').waitFor()
await cust.locator('[data-testid=tracking-map] [data-testid=rider-pin]').waitFor()
await shot(cust, 'd03c-customer-tracking')
step('the customer sees the rider on a map once the order is on its way')

// ---- at the door: an optional photo, shrunk on the phone, shown to the desk ----
await rider.click('[data-act=arrive]')
await expectStatus('骑手已到达')
const picture = await cust.screenshot()   // any picture will do
await rider.setInputFiles('[data-testid=proof] input[type=file]', { name: 'door.png', mimeType: 'image/png', buffer: picture })
await rider.locator('[data-testid=proof-done]').waitFor()
await shot(rider, 'd03d-proof')
await desk.click(`.ord:has-text("${orderNo}")`)
const proofHref = await desk.locator('[data-testid=proof-row] a').getAttribute('href')
const proof = await desk.evaluate(async (u) => { const r = await fetch(u); return { type: r.headers.get('content-type'), size: (await r.blob()).size } }, proofHref)
if (proof.type !== 'image/jpeg' || proof.size < 1000 || proof.size > 600 * 1024) throw new Error('proof photo: ' + JSON.stringify(proof))
step(`the photo at the door reached the desk as a ${Math.round(proof.size / 1024)} KB JPEG`)

await rider.waitForSelector('[data-act=deliver]')
await rider.context().setOffline(true)
await rider.click('.quick button:has-text("刚好")')
await shot(rider, 'd04-offline-steps')
await rider.click('[data-act=deliver]')
await rider.locator('[data-testid=ops-waiting]:has-text("1 个操作")').waitFor()
await rider.click('[data-testid=tab-done]')
await rider.locator(`[data-done="${orderNo}"]`).waitFor()   // shown as delivered while it waits
if (!(await cust.locator('[data-testid=status]').innerText()).includes('骑手已到达')) throw new Error('nothing may reach the server while offline')
// The page opens again without signal, with the tasks and the waiting steps.
await rider.reload()
await rider.locator('[data-testid=ops-waiting]:has-text("1 个操作")').waitFor()
await shot(rider, 'd05-offline-reopened')
step('offline: the delivery was saved on the phone; the page reopened without signal')

await rider.context().setOffline(false)
await rider.locator('[data-testid=ops-waiting]').waitFor({ state: 'detached', timeout: 15000 })
await expectStatus('已送达')
await cust.locator('[data-testid=tracking]').waitFor({ state: 'detached' })   // delivered: the rider is no longer shown
const cash = (await api(owner, 'GET', '/merchant/branches/1/cash')).body
const mine = (cash?.collections ?? cash?.items ?? []).filter(c => c.public_no === orderNo)
if (mine.length > 1) throw new Error('the cash was recorded more than once')
step('back online: the saved step was sent and the order is delivered')

// ---- a step for a delivery that was reassigned meanwhile does not count ----
await rider.evaluate(() => localStorage.setItem('work-ops', JSON.stringify([
  { id: 'x1', task: 999999, order: 999999, no: 'TEST-000', action: 'arrive', body: {}, at: new Date().toISOString() }])))
await rider.reload()
await rider.locator('[data-testid=rider-notice]:has-text("TEST-000")').waitFor()
if (await rider.locator('[data-testid=ops-waiting]').count()) throw new Error('a refused step must leave the queue')
step('a step the server refuses is dropped and the rider is told')

// ---- clock off; signing out ends the shift too ----
await rider.click('[data-testid=tab-tasks]')
await rider.locator('[data-testid=duty] [data-act=duty]').click()
await rider.waitForSelector('[data-testid=duty][data-on=false]')
if ((await api(rider, 'GET', '/rider/duty')).body.lat !== null) throw new Error('the position must be forgotten after the shift')
await rider.locator('[data-testid=duty] [data-act=duty]').click()
await rider.waitForSelector('[data-testid=duty][data-on=true]')
await rider.click('[data-testid=tab-me]')
await rider.click('[data-testid=sign-out]')
await rider.waitForURL('**/login')
await owner.reload()
await owner.locator('tr[data-staff="wang1"]:has-text("没上班")').waitFor()
step('clocking off forgets the position; signing out ends the shift')

await browser.close()
if (errors.length) { console.error(errors.join('\n')); process.exit(1) }
console.log('duty e2e passed')

})().catch(e => { console.error(e); process.exit(1) })
