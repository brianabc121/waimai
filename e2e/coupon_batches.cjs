// Coupons, second batch (docs/COUPON_DESIGN.md): the store makes a coupon to
// claim, the platform makes a new-customer coupon for one country, a new
// customer gets the platform coupon on sign-up and claims the store's on its
// page, and checkout uses both together.
// Usage: BASE_URL=http://localhost:8080 node e2e/coupon_batches.cjs   (needs seed data and OTP_DEV_ECHO=true)
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
  return page
}
const step = (s) => console.log('✓ ' + s)
const shot = (page, name) => page.screenshot({ path: `${SHOTS}/${name}.png` })
const expect = (got, want, what) => { if (got !== want) throw new Error(`${what}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`) }
const tid = (id) => `[data-testid=${id}]`
const text = async (page, sel) => (await page.locator(sel).first().innerText()).replace(/\s+/g, ' ').trim()
async function signIn(page, user) {
  await page.goto(BASE + (/^admin/.test(user) ? '/admin/login' : /^((kitchen|rider|zhang|wang)\d|e2e\d+$)/.test(user) ? '/login?next=/work' : '/login'))
  await page.fill('input[autocomplete=username]', user)
  await page.fill('input[type=password]', 'demo1234')
  await page.click('button.primary')
  await page.waitForURL(u => !/\/login$/.test(u.pathname))
  return async (method, path, data) => {
    const r = await page.request.fetch(BASE + '/api/v1' + path, { method, data, headers: { 'X-Requested-With': 'e2e' } })
    if (!r.ok()) throw new Error(`${method} ${path}: ${r.status()} ${await r.text()}`)
    return r.json()
  }
}
const cleanup = []
try {
  // ---- the store makes a coupon to claim ----
  const owner = await open({ width: 1280, height: 900 })
  const ownerApi = await signIn(owner, 'owner1')
  const store = await ownerApi('GET', '/stores/makati')
  const bid = store.branch.id
  // earlier runs' coupons would crowd the page
  for (const b of (await ownerApi('GET', `/merchant/branches/${bid}/coupons/batches`)).batches)
    if (b.status !== 'ended') await ownerApi('POST', `/merchant/branches/${bid}/coupons/batches/${b.id}/status`, { status: 'ended' })
  await owner.goto(BASE + '/merchant/promos?tab=coupons')
  await owner.click(tid('claim-new'))
  await owner.selectOption(tid('claim-kind'), 'store_threshold')
  await owner.fill(tid('claim-threshold'), '100')
  await owner.fill(tid('claim-value'), '15')
  await owner.fill(tid('claim-days'), '7')
  await owner.fill(tid('claim-quota'), '50')
  await owner.click(tid('claim-save'))
  await owner.waitForSelector(tid('claim-form'), { state: 'detached' })
  expect((await text(owner, tid('claim-row'))).includes('满₱100减₱15'), true, 'claim batch row')
  await owner.locator(tid('claim-batches')).scrollIntoViewIfNeeded()
  await shot(owner, 'c7-merchant-claim')
  const claimBatch = (await ownerApi('GET', `/merchant/branches/${bid}/coupons/batches`)).batches[0]
  cleanup.push(() => ownerApi('POST', `/merchant/branches/${bid}/coupons/batches/${claimBatch.id}/status`, { status: 'ended' }))
  step('the store makes a ₱15-off-₱100 coupon, 50 to claim')

  // ---- the platform makes a new-customer coupon for the Philippines ----
  const admin = await open({ width: 1360, height: 900 })
  const adminApi = await signIn(admin, 'admin')
  for (const b of (await adminApi('GET', '/admin/coupons')).batches)
    if (b.status !== 'ended') await adminApi('POST', `/admin/coupons/${b.id}/status`, { status: 'ended' })
  await admin.goto(BASE + '/admin/coupons')
  await admin.click(tid('apc-new'))
  await admin.selectOption(tid('apc-country'), 'PH')
  await admin.selectOption(tid('apc-province'), '')
  await admin.fill(tid('apc-name'), '新人礼')
  await admin.fill(tid('apc-value'), '10')
  await admin.fill(tid('apc-days'), '14')
  await admin.selectOption(tid('apc-source'), 'new_customer')
  await admin.click(tid('apc-save'))
  await admin.waitForSelector(tid('apc-form'), { state: 'detached' })
  expect(await text(admin, tid('apc-region')), '菲律宾 · 全国', 'region')
  const platform = (await adminApi('GET', '/admin/coupons')).batches[0]
  cleanup.push(() => adminApi('POST', `/admin/coupons/${platform.id}/status`, { status: 'ended' }))
  // a manual one, given out by phone number below
  await admin.click(tid('apc-new'))
  await admin.selectOption(tid('apc-country'), 'PH')
  await admin.selectOption(tid('apc-province'), 'Metro Manila')
  await admin.fill(tid('apc-name'), '雨天补偿券')
  await admin.fill(tid('apc-value'), '5')
  await admin.selectOption(tid('apc-source'), 'manual')
  await admin.click(tid('apc-save'))
  await admin.waitForSelector(tid('apc-form'), { state: 'detached' })
  const manual = (await adminApi('GET', '/admin/coupons')).batches.find(b => b.source === 'manual' && b.status === 'active')
  cleanup.push(() => adminApi('POST', `/admin/coupons/${manual.id}/status`, { status: 'ended' }))
  await shot(admin, 'c8-admin-coupons')
  step('the platform makes a ₱10 new-customer coupon for the Philippines and a ₱5 one for Metro Manila')

  // ---- a new customer signs up and gets the platform coupon; claims the store's ----
  const cust = await open({ width: 390, height: 844 })
  await cust.goto(BASE + '/s/makati')
  await cust.waitForSelector(tid('store-coupon'))
  await shot(cust, 'c9-store-coupons')
  await cust.click(tid('claim-btn'))
  await cust.waitForURL(u => u.pathname === '/account/login')
  const phone = '9' + String(Math.floor(Math.random() * 1e9)).padStart(9, '0')
  await cust.fill(tid('login-phone'), phone)
  await cust.check(tid('login-agree'))
  await cust.click(tid('login-send'))
  await cust.waitForURL(u => u.pathname === '/mall/s/makati')
  await cust.waitForSelector(tid('claim-btn'))
  await cust.click(tid('claim-btn'))
  await cust.waitForSelector(tid('claim-note'))
  expect(await cust.locator(tid('claim-btn')).innerText(), '已领取', 'claimed')
  step('a new customer signs up from the store page and claims the store coupon')

  // the platform gives the manual coupon to this customer by phone number
  await admin.click(tid('apc-give'))
  await admin.fill(tid('apc-phones'), '+63' + phone + '\n+639000000009')
  await admin.click(tid('apc-give-go'))
  await admin.waitForSelector(tid('apc-note'))
  expect((await text(admin, tid('apc-note'))).includes('发出 1 张'), true, 'given by phone')
  step('the platform gives the Metro Manila coupon by phone number')

  await cust.goto(BASE + '/me/coupons')
  await cust.waitForSelector(tid('my-coupon'))
  expect(await cust.locator(tid('my-coupon')).count(), 3, 'three coupons')
  const all = (await cust.locator(tid('my-coupon')).allInnerTexts()).join(' ')
  expect(all.includes('菲律宾 店铺通用') && all.includes('Metro Manila'), true, 'platform coupon says where')
  await shot(cust, 'c10-my-coupons')

  // ---- checkout uses them together ----
  await cust.goto(BASE + '/s/makati')
  await cust.waitForSelector('.item')
  for (const d of ['卤肉饭', '卤肉饭', '冰柠檬茶']) await cust.click(`.item:has-text("${d}") .stepper button.fill`)
  await cust.click('.cartbar .btn.primary')
  await customerLogin(cust, null, { pickup: true })
  await cust.waitForSelector(tid('coupon-line'))
  const row = await text(cust, tid('coupon-row'))
  expect(row.includes('已选 2 张'), true, 'store and platform coupon chosen: ' + row)
  await cust.click(tid('coupon-row'))
  await cust.waitForSelector(tid('coupon-sheet'))
  await shot(cust, 'c11-checkout-stack')
  await cust.click(tid('coupon-done'))
  step('checkout picks the store coupon and a platform coupon together')
} finally {
  for (const f of cleanup) await f().catch(e => errors.push('cleanup: ' + e.message))
}
if (errors.length) throw new Error('browser errors:\n' + errors.join('\n'))
await browser.close()
console.log('coupon batches: all good')
})().catch(e => { console.error(e); process.exit(1) })
