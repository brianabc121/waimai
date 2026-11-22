// The Android app on an emulator: the real APK, opened on the device, driven
// through the app's own web view. Customer and desk run in a browser on the
// computer. It checks what a browser tab cannot show: the app shell, the
// position read by the phone itself, and that it keeps coming while the app
// is in the background.
// Needs: seed data, the API on :8080, an emulator with the APK installed and
// built with WORKBENCH_URL=http://10.0.2.2:8080, adb on PATH.
// Usage: node e2e/android.cjs   (.github/workflows/android-emulator.yml runs it)
const { chromium, _android: android } = require('playwright')
const { mkdirSync, writeFileSync } = require('node:fs')
const { execFileSync } = require('node:child_process')

;(async () => {

const BASE = process.env.BASE_URL || 'http://localhost:8080'
const SHOTS = process.env.SHOTS_DIR || 'e2e/shots'
const PKG = process.env.WORKBENCH_APP_ID || 'app.waimai.workbench'
mkdirSync(SHOTS, { recursive: true })
const adb = (...args) => execFileSync('adb', args, { encoding: 'utf8' })
const sleep = (ms) => new Promise(r => setTimeout(r, ms))
const step = (s) => console.log('✓ ' + s)

const browser = await chromium.launch()
async function open(viewport) {
  const ctx = await browser.newContext({ viewport, locale: 'zh-CN' })
  return ctx.newPage()
}
async function login(page, user, base = BASE) {
  if (!/\/login/.test(page.url())) await page.goto(base + '/login')
  for (let i = 0; ; i++) {
    await page.fill('input[autocomplete=username]', user)
    await page.fill('input[type=password]', 'demo1234')
    const res = page.waitForResponse(r => r.url().endsWith('/auth/login'))
    await page.click('button.primary')
    if ((await res).status() !== 429 || i >= 5) return
    await page.waitForTimeout(7000)
  }
}
const api = (page, path) => page.evaluate(async (path) => (await fetch('/api/v1' + path)).json(), path)

// ---- a delivery order, accepted and given to 小王 ----
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
const expectStatus = (text) => cust.waitForFunction(t => document.querySelector('[data-testid=status]')?.textContent?.includes(t), text, { timeout: 20000 })
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
const owner = await open({ width: 1280, height: 860 })
await login(owner, 'owner1')
await owner.waitForURL('**/merchant/orders')
const wang = async () => (await api(owner, '/merchant/branches/1/riders')).riders.find(r => r.username === 'wang1')
async function until(what, fn, ms = 90000) {
  for (const end = Date.now() + ms; Date.now() < end; await sleep(2000)) { const v = await fn(); if (v) return v }
  throw new Error('timed out: ' + what)
}

// ---- the app on the phone ----
const [device] = await android.devices()
globalThis.device = device
if (!device) throw new Error('no Android device: is the emulator running?')
device.setDefaultTimeout(120000)
const shot = async (name) => writeFileSync(`${SHOTS}/${name}.png`, await device.screenshot())
const fix = (lat, lng) => adb('emu', 'geo', 'fix', String(lng), String(lat))
fix(14.5547, 121.0244)
await device.shell(`monkey -p ${PKG} -c android.intent.category.LAUNCHER 1`)
const view = await device.webView({ pkg: PKG })
const app = await view.page()
app.setDefaultTimeout(60000)
await app.waitForURL('**/login**')
const ua = await app.evaluate(() => navigator.userAgent)
const isNative = await app.evaluate(() => !!window.Capacitor?.isNativePlatform?.())
if (!/WorkbenchApp\/1/.test(ua) || !isNative) throw new Error('not running inside the app shell: ' + ua)
await shot('a01-app-login')
step('the app opened the sign-in page of the server; ' + ua.match(/Android [\d.]+/)?.[0])

await login(app, 'wang1')
await app.waitForURL('**/work')
const pack = app.locator(`[data-pack="${orderNo}"]`)
await pack.locator('[data-act=pack]').click()
for (const box of await app.locator('[data-testid=pack-sheet] input[type=checkbox]').all()) await box.check()
await app.click('[data-act=confirm-pack]')
await pack.locator(':text("已打包")').waitFor()
await shot('a02-store-identity')
step('signed in; packed the order in the in-store identity')

// ---- rider identity: clock on, the phone reads the position ----
await app.click('[data-testid=identity]')
await app.click('[data-testid=switch-go]')
const duty = app.locator('[data-testid=duty]')
await duty.waitFor()
await duty.locator('[data-act=duty]').click()
await app.waitForSelector('[data-testid=duty][data-on=true]')
// The emulator hands out a position only when it changes, so nudge it until one arrives.
let lat = 14.5547
const first = await until('first position on the server', async () => { fix(lat += 0.0003, 121.0244); await sleep(3000); const r = await wang(); return r?.lat != null && r })
  .catch(async e => {
    // Say what the app itself knows before giving up.
    console.error('duty card:', await duty.getAttribute('data-geo'), '|', await app.locator('[data-testid=geo-text]').innerText().catch(() => ''))
    console.error('points kept on the phone:', await app.evaluate(() => localStorage.getItem('work-points')))
    console.error('server:', JSON.stringify(await wang()))
    throw e
  })
if (Math.abs(first.lat - 14.555) > 0.01 || Math.abs(first.lng - 121.0244) > 0.01) throw new Error('position: ' + JSON.stringify(first))
await app.waitForSelector('[data-testid=duty][data-geo=ok]')
await shot('a03-on-duty')
// The notice in the status bar belongs to a foreground service; Android keeps the app alive for it.
const sharing = async () => /isForeground=true/.test((await device.shell(`dumpsys activity services ${PKG}`)).toString())
if (!(await sharing())) throw new Error('no status bar notice while sharing the position')
step(`clocked on: the phone's position reached the server (${first.lat.toFixed(4)}, ${first.lng.toFixed(4)}), the notice is in the status bar`)

// ---- the app in the background: the position keeps coming ----
await device.shell('input keyevent KEYCODE_HOME')
await sleep(20000)
const moved = await until('a position sent from the background', async () => {
  fix(lat += 0.002, 121.0300); await sleep(4000)
  const r = await wang(); return r && Math.abs(r.lng - 121.0300) < 0.0005 && r
}, 180000)
await device.shell('cmd statusbar expand-notifications')
await sleep(1500)
await shot('a04-background-notice')
await device.shell('cmd statusbar collapse')
step(`in the background the position still moved on the server (${moved.lat.toFixed(4)}, ${moved.lng.toFixed(4)})`)

// ---- back in the app: pick up, arrive, deliver ----
await device.shell(`monkey -p ${PKG} -c android.intent.category.LAUNCHER 1`)
await app.locator(`[data-delivery="${orderNo}"] [data-act=open]`).click()
await app.waitForSelector('[data-act=pickup]:not([disabled])')
await shot('a05-pickup')
await app.click('[data-act=pickup]')
await expectStatus('配送中')
await cust.locator('[data-testid=tracking][data-has-position=true]').waitFor()
await app.click('[data-act=arrive]')
await expectStatus('骑手已到达')
await app.waitForSelector('[data-act=deliver]')
await app.click('.quick button:has-text("刚好")')
await shot('a06-at-the-door')
await app.click('[data-act=deliver]')
await expectStatus('已送达')
await app.click('[data-testid=tab-done]')
await app.locator(`[data-done="${orderNo}"]`).waitFor()
await shot('a07-delivered')
step('picked up, arrived and delivered from the app; the customer saw each step')

// ---- clock off: the position is forgotten and the notice goes away ----
await app.click('[data-testid=tab-tasks]')
await app.locator('[data-testid=duty] [data-act=duty]').click()
await app.waitForSelector('[data-testid=duty][data-on=false]')
await until('position forgotten', async () => (await wang())?.lat == null, 20000)
await until('the notice is gone', async () => !(await sharing()), 20000)
await shot('a08-off-duty')
step('clocked off: the position is forgotten and the status bar notice is gone')

await device.close()
await browser.close()
console.log('android e2e passed')

})().catch(async e => {
  console.error(e)
  // What the phone showed when it went wrong.
  try { writeFileSync(`${process.env.SHOTS_DIR || 'e2e/shots'}/a99-failed.png`, await globalThis.device.screenshot()) } catch {}
  process.exit(1)
})
