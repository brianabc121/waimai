// The customer's store page in a real browser: the store card, the three
// tabs, baskets, a required category, a review after the order is finished,
// and a report that reaches the platform back office.
// Usage: BASE_URL=http://localhost:8080 node e2e/storefront.cjs   (needs seed data and OTP_DEV_ECHO=true)
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
  page.on('dialog', d => d.accept())
  return page
}
async function login(page, user) {
  await page.goto(BASE + '/login')
  await page.fill('input[autocomplete=username]', user)
  await page.fill('input[type=password]', 'demo1234')
  await page.click('button.primary')
  await page.waitForURL(u => !u.pathname.startsWith('/login'))
}
const step = (s) => console.log('✓ ' + s)
const shot = (page, name) => page.screenshot({ path: `${SHOTS}/${name}.png` })
const expect = (got, want, what) => { if (got !== want) throw new Error(`${what}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`) }
const tid = (id) => `[data-testid=${id}]`
const text = async (page, sel) => (await page.locator(sel).first().innerText()).replace(/\s+/g, ' ').trim()

const NOTICE = '本周新品上架，下单备注口味，我们现点现做，有任何问题请联系门店电话，欢迎下单后给我们评价！'
const owner = await open({ width: 1280, height: 860 })
await login(owner, 'owner1')
const api = async (method, path, data) => {
  const r = await owner.request.fetch(BASE + '/api/v1' + path, { method, data, headers: { 'X-Requested-With': 'e2e' } })
  if (!r.ok()) throw new Error(`${method} ${path}: ${r.status()} ${await r.text()}`)
  return r.json()
}
const drinks = owner.locator('.card:has(h2:has-text("饮品"))')
let madeRequired = false

try {
// ---- the owner writes a notice and makes drinks a required category ----
await owner.goto(BASE + '/merchant/settings')
await owner.fill('textarea[name=notice]', NOTICE)
await owner.locator('form:has(textarea[name=notice]) button.primary').click()
await owner.waitForSelector('.banner.ok')
await owner.goto(BASE + '/merchant/menu')
await drinks.waitFor()
if (!(await drinks.locator(tid('cat-required')).count())) await drinks.locator(tid('toggle-required')).click()
await drinks.locator(tid('cat-required')).waitFor()
madeRequired = true
step('owner saved a store notice and made a category required')

// ---- the store card and the tabs ----
const cust = await open({ width: 390, height: 844 })
await cust.goto(BASE + '/s/makati')
await cust.waitForSelector('.item')
expect(await text(cust, '.shophead h1'), '马卡蒂小馆', 'store name')
if (!(await text(cust, tid('notice'))).includes('本周新品上架')) throw new Error('the notice is missing from the store card')
await cust.locator('.shophead .chips:has-text("货到付款")').waitFor()
expect(await cust.locator('.shoptabs button').count(), 3, 'tabs')
await cust.locator('.cats button:has-text("饮品") .must').waitFor()
expect(await cust.locator('.shophead .seg').count(), 0, 'delivery or pickup is not chosen on the menu page')
// search narrows the menu
await cust.click(tid('search-toggle'))
await cust.fill(tid('search-box'), '柠檬')
expect(await cust.locator('.item').count(), 1, 'search results')
await cust.click(tid('search-toggle'))
step('store card shows name, tags and the notice; search narrows the menu')

// ---- a required category blocks checkout until something is chosen from it ----
await cust.click('.item:has-text("卤肉饭") .stepper button.fill')
expect(await text(cust, tid('cart-count')), '1', 'cart count')
expect(await text(cust, '.cats button:has-text("主食") .badge'), '1', 'category badge')
await cust.locator(`.cartbar ${tid('required-missing')}`).click()   // jumps to the category
await cust.waitForFunction(() => { const r = document.querySelector('.cat:last-child')?.getBoundingClientRect(); return r && r.top < innerHeight })
await cust.click('.item:has-text("瓶装水") .stepper button.fill')
await cust.locator('.cartbar .btn.primary:has-text("去结算")').waitFor()
step('checkout is blocked until the required category has a dish')

// ---- baskets: tick the second one, add to it, move a dish across ----
await cust.click(tid('open-cart'))
await cust.waitForSelector(tid('cart-sheet'))
await cust.click(`${tid('cart-sheet')} [data-basket="2"]`)
await cust.click('.tip button')
await cust.click('.item:has-text("冰柠檬茶") .stepper button.fill')
await cust.click(tid('open-cart'))
expect(await cust.locator(`${tid('cart-sheet')} .cline`).count(), 1, 'dishes in basket 2')
await cust.click(`${tid('cart-sheet')} [data-basket="1"]`)
expect(await cust.locator(`${tid('cart-sheet')} .cline`).count(), 2, 'dishes in basket 1')
await cust.locator(`${tid('cart-sheet')} [data-line="瓶装水"] ${tid('move-basket')}`).click()
await cust.locator(`${tid('cart-sheet')} [data-line="瓶装水"] .targets button:has-text("购物篮2")`).click()
expect(await cust.locator(`${tid('cart-sheet')} .cline`).count(), 1, 'dishes left in basket 1')
expect(await text(cust, `${tid('basket-subtotal')} b`), '₱159', 'basket 1 subtotal')
expect(await text(cust, `${tid('cart-subtotal')} b`), '₱258', 'all baskets')
expect(await text(cust, tid('cart-total')), '₱258', 'cart bar total')
await shot(cust, 'sf1-cart')
await cust.click('.cartbar .btn.primary')
await customerLogin(cust, null, { pickup: true })
expect(await cust.locator(tid('checkout-address')).count(), 0, 'no address block for a pickup')
expect(await cust.locator(tid('delivery-fee')).count(), 0, 'no delivery fee for a pickup')
await shot(cust, 'sf1b-checkout-pickup')
await cust.fill('input[autocomplete=name]', '陈小姐')
await cust.locator('.bottombar .btn.primary').click()
await cust.waitForURL('**/o/**')
await cust.waitForSelector(tid('status'))
const orderNo = (await cust.locator('.topbar h1').innerText()).replace('订单号', '').trim()
expect(await cust.locator(tid('basket-head')).count(), 2, 'baskets on the order page')
expect(await cust.locator(tid('review-form')).count(), 0, 'no review before the order is finished')
step(`order ${orderNo} was placed in two baskets`)

// ---- the store finishes the order; the desk shows the baskets ----
const store = await api('GET', '/stores/makati')
const list = await api('GET', `/merchant/branches/${store.branch.id}/orders`)
let o = (list.orders ?? list).find(x => x.public_no === orderNo)
await owner.goto(BASE + '/merchant/orders')
await owner.click(`.ord:has-text("${orderNo}")`)
await owner.locator('.detail .tag.info:has-text("篮2")').first().waitFor()
o = await api('POST', `/merchant/orders/${o.id}/accept`, { expected_version: o.version })
for (const it of o.items.filter(i => i.needs_prep)) o = await api('POST', `/kitchen/items/${it.id}/ready`, {})
o = await api('POST', `/kitchen/orders/${o.id}/pack`, { bags: 2, manifest_version: o.manifest_version })
await api('POST', `/merchant/orders/${o.id}/handoff`, { expected_version: 0 })
await cust.waitForFunction(() => document.querySelector('[data-testid=status]')?.textContent?.includes('已取餐'), null, { timeout: 8000 })

// ---- the customer reviews the order ----
await cust.waitForSelector(tid('review-form'))
await cust.locator(`${tid('review-form')} button.primary`).click()
await cust.locator(`${tid('review-form')} .banner.bad`).waitFor()   // every line needs stars
for (const [k, n] of [['rating', 5], ['taste', 5], ['packaging', 4]]) await cust.click(`[data-score=${k}] .starpick button:nth-child(${n})`)
expect(await cust.locator('[data-score=delivery]').count(), 0, 'a pickup order has no delivery score')
await cust.fill(`${tid('review-form')} textarea`, '卤肉饭很香，分两袋装得很清楚。')
await cust.locator(`${tid('review-form')} button.primary`).click()
await cust.waitForSelector(tid('review-done'))
await shot(cust, 'sf2-reviewed')
step('the finished order was reviewed once')

// ---- the review and the sales show on the store page ----
await cust.goto(BASE + '/s/makati')
await cust.waitForSelector('.item')
await cust.waitForSelector(tid('score'))
if ((await text(cust, tid('sales'))).includes(' 0 ')) throw new Error('sales should count the finished order')
if (!/销量:[1-9]/.test(await text(cust, '.item:has-text("卤肉饭") .stats'))) throw new Error('the dish should show what was sold')
if (!(await text(cust, '.item:has-text("今日限量便当") .stats')).includes('库存:')) throw new Error('a limited dish should show its stock')
await shot(cust, 'sf3-menu')
await cust.click('[data-tab=reviews]')
await cust.locator(`${tid('review')}:has-text("卤肉饭很香")`).waitFor()
await cust.locator(`${tid('review')}:has-text("卤肉饭很香") .chips span:has-text("冰柠檬茶")`).waitFor()
await shot(cust, 'sf4-reviews')
await cust.click('[data-filter=bad]')
await cust.waitForFunction(() => !document.body.innerText.includes('卤肉饭很香'))
await cust.click('[data-filter=good]')
await cust.locator(`${tid('review')}:has-text("卤肉饭很香")`).waitFor()
step('the store page shows the score, the review and what was sold')

// ---- the store tab and a report ----
await cust.click('[data-tab=info]')
await cust.locator('.about:has-text("营业时间")').waitFor()
await cust.locator('.about:has-text("本周新品上架")').waitFor()
await shot(cust, 'sf5-store')
await cust.click(tid('report'))
const mark = 'e2e ' + Date.now()
await cust.click(`${tid('report-sheet')} label:has-text("服务态度差")`)
await cust.fill(`${tid('report-sheet')} textarea`, mark)
await cust.locator(`${tid('report-sheet')} button.primary`).click()
await cust.locator(`${tid('report-sheet')} .banner.ok`).waitFor()
const admin = await open({ width: 1280, height: 860 })
await login(admin, 'admin')
await admin.goto(`${BASE}/admin/merchants/${store.branch.merchant_id}`)
await admin.locator(`${tid('reports')} tr:has-text("${mark}"):has-text("服务态度差")`).waitFor()
step('a report from the store page reaches the platform back office')

// ---- a computer keeps the baskets in the cart beside the menu ----
const pc = await open({ width: 1440, height: 900 })
await pc.goto(BASE + '/s/makati')
await pc.waitForSelector('.item')
await pc.click('.item:has-text("瓶装水") .stepper button.fill')
await pc.locator(`${tid('side-cart')} [data-basket="2"]`).waitFor()
await pc.locator(`${tid('side-cart')} ${tid('go-checkout')}`).waitFor()
if (await pc.locator('.cartbar').isVisible()) throw new Error('the cart bar should be hidden on a computer')
await shot(pc, 'sf6-pc')
step('on a computer the baskets are in the cart beside the menu')
} finally {
  // Leave the demo store as other tests expect it: no required category.
  if (madeRequired) {
    await owner.goto(BASE + '/merchant/menu')
    await drinks.waitFor()
    if (await drinks.locator(tid('cat-required')).count()) await drinks.locator(tid('toggle-required')).click()
    await drinks.locator(tid('cat-required')).waitFor({ state: 'detached' })
  }
}

await browser.close()
if (errors.length) { console.error(errors.join('\n')); process.exit(1) }
console.log('storefront e2e passed')
})().catch(e => { console.error(e); process.exit(1) })
