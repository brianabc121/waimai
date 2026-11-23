// Bills (docs/COUPON_DESIGN.md §8, §9): an order paid partly by a platform
// coupon shows on the store's bill with each discount, the summary sums each
// kind, the bill downloads as Excel, and the platform sees the store's line.
// Usage: BASE_URL=http://localhost:8080 node e2e/bills.cjs   (needs seed data and OTP_DEV_ECHO=true)
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
  await page.goto(BASE + '/login')
  await page.fill('input[autocomplete=username]', user)
  await page.fill('input[type=password]', 'demo1234')
  await page.click('button.primary')
  await page.waitForURL(u => !u.pathname.startsWith('/login'))
  return async (method, path, data) => {
    const r = await page.request.fetch(BASE + '/api/v1' + path, { method, data, headers: { 'X-Requested-With': 'e2e' } })
    if (!r.ok()) throw new Error(`${method} ${path}: ${r.status()} ${await r.text()}`)
    return r.json()
  }
}
const cleanup = []
try {
  const owner = await open({ width: 1366, height: 900 })
  const ownerApi = await signIn(owner, 'owner1')
  const admin = await open({ width: 1366, height: 900 })
  const adminApi = await signIn(admin, 'admin')
  const store = await ownerApi('GET', '/stores/makati')

  // a platform coupon for a new customer, who orders with it
  const { id: batch } = await adminApi('POST', '/admin/coupons', { name: '账单测试券', value: 1000, valid_days: 7, source: 'manual', country_code: 'PH' })
  cleanup.push(() => adminApi('POST', `/admin/coupons/${batch}/status`, { status: 'ended' }))
  const cust = await open({ width: 390, height: 844 })
  await cust.goto(BASE + '/s/makati')
  await cust.waitForSelector('.item')
  for (const d of ['卤肉饭', '卤肉饭', '冰柠檬茶']) await cust.click(`.item:has-text("${d}") .stepper button.fill`)
  await cust.click('.cartbar .btn.primary')
  const phone = '9' + String(Math.floor(Math.random() * 1e9)).padStart(9, '0')
  await customerLogin(cust, phone, { pickup: true })
  const given = await adminApi('POST', `/admin/coupons/${batch}/issue`, { phones: ['+63' + phone] })
  expect(given.given, 1, 'coupon given')
  await cust.reload()
  await cust.waitForSelector(tid('coupon-line'))
  await cust.fill('input[autocomplete=name]', '林先生')
  await cust.locator('.bottombar .btn.primary').click()
  await cust.waitForURL('**/o/**')
  const no = (await cust.locator(tid('order-no')).innerText()).trim()
  // the store finishes it
  let o = (await ownerApi('GET', `/merchant/branches/${store.branch.id}/orders?scope=active`)).orders.find(x => x.public_no === no)
  o = await ownerApi('POST', `/merchant/orders/${o.id}/accept`, { expected_version: o.version })
  for (const it of o.items.filter(i => i.needs_prep)) o = await ownerApi('POST', `/kitchen/items/${it.id}/ready`, {})
  o = await ownerApi('POST', `/kitchen/orders/${o.id}/pack`, { bags: 1, manifest_version: o.manifest_version })
  await ownerApi('POST', `/merchant/orders/${o.id}/handoff`, { expected_version: 0 })
  step(`order ${no} used a ₱10 platform coupon and was picked up`)

  // ---- the store's bill ----
  await owner.goto(BASE + '/merchant/bills')
  await owner.waitForSelector(tid('bill-table'))
  await owner.selectOption(tid('bill-filter'), 'platform_free')
  const row = owner.locator(`${tid('bill-row')}:has-text("${no}")`)
  await row.waitFor()
  expect((await row.innerText()).includes('+₱10'), true, 'platform coupon column')
  await row.click()
  await owner.waitForSelector(tid('bill-detail'))
  const detail = await text(owner, tid('bill-detail'))
  expect(detail.includes('平台无门槛券') && detail.includes('平台发放'), true, 'detail: ' + detail)
  await owner.selectOption(tid('bill-filter'), '')
  await owner.waitForSelector(`${tid('bill-row')}:has-text("${no}")`)
  await owner.locator(`${tid('bill-row')}:has-text("${no}")`).click()
  await owner.waitForSelector(tid('bill-sum'))
  await shot(owner, 'c12-bill-orders')
  const href = await owner.getAttribute(tid('bill-export'), 'href')
  const x = await owner.request.get(BASE + href)
  expect(x.ok() && x.headers()['content-type'].includes('spreadsheet'), true, 'Excel download')
  step('the bill lists the order with each discount, filters by kind and downloads as Excel')

  await owner.click(tid('bill-tab-summary'))
  await owner.waitForSelector(tid('bill-kinds'))
  const plat = await text(owner, `${tid('bill-kinds')} [data-kind=platform_free]`)
  expect(/平台\s*\d+\s*₱/.test(plat), true, 'platform coupon row: ' + plat)
  await shot(owner, 'c13-bill-summary')
  step('the summary sums each kind of discount and who pays for it')

  // ---- the platform ----
  await admin.goto(BASE + '/admin/bills')
  await admin.waitForSelector(`${tid('abl-store')}:has-text("makati")`)
  await shot(admin, 'c14-admin-bills')
  await admin.click(tid('abl-tab-settle'))
  await admin.waitForSelector(`${tid('abl-settlement')}, ${tid('abl-settlements')} td.muted`)
  if (await admin.locator(tid('abl-mark')).count()) {
    await admin.locator(tid('abl-mark')).first().click()
    await admin.fill(tid('abl-ref'), 'BDO-' + Date.now())
    await shot(admin, 'c15-admin-settlements')
    await admin.click(tid('abl-pay'))
    await admin.waitForSelector(tid('abl-pay-form'), { state: 'detached' })
  }
  step('the platform sees each store and its coupon statements')
} finally {
  for (const f of cleanup) await f().catch(e => errors.push('cleanup: ' + e.message))
}
if (errors.length) throw new Error('browser errors:\n' + errors.join('\n'))
await browser.close()
console.log('bills: all good')
})().catch(e => { console.error(e); process.exit(1) })
