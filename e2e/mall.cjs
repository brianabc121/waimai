// The marketplace in a real browser: the stores around where the device says
// the customer is, categories, search, sorting, changing the place, browsing a
// city, entering a store and coming back, what happens when the device will
// not say where it is, and how the merchant and the platform take a store off.
// Usage: BASE_URL=http://localhost:8080 node e2e/mall.cjs
//   (needs seed data, OTP_DEV_ECHO=true and GOOGLE_MAPS_API_KEY=stub)
const { chromium } = require('playwright')
const { mkdirSync } = require('node:fs')

;(async () => {

const BASE = process.env.BASE_URL || 'http://localhost:8080'
const SHOTS = process.env.SHOTS_DIR || 'e2e/shots'
mkdirSync(SHOTS, { recursive: true })
const browser = await chromium.launch()
const errors = []
const watch = p => {
  p.on('pageerror', e => errors.push(`pageerror: ${e.message}`))
  p.on('console', m => { if (m.type() === 'error' && !/favicon|Failed to load resource/.test(m.text())) errors.push(`console: ${m.text()}`) })
}
const opts = { locale: 'zh-CN', ignoreHTTPSErrors: process.env.E2E_INSECURE_TLS === '1' }
// Legazpi Village, Makati: the demo stores are around it.
const HERE = { geolocation: { latitude: 14.5547, longitude: 121.0244 }, permissions: ['geolocation'] }
const tid = id => `[data-testid=${id}]`
const step = s => console.log('✓ ' + s)
const expect = (ok, what) => { if (!ok) throw new Error(what) }
const store = slug => `${tid('mall-stores')} [data-slug=${slug}]`
const slugs = p => p.locator(`${tid('mall-stores')} ${tid('mall-store')}`).evaluateAll(els => els.map(e => e.dataset.slug))
// The list is asked for again after every change: wait until it has settled.
const settled = async p => { await p.waitForTimeout(150); await p.waitForFunction(() => !document.querySelector('.grid.dim')) }
async function staff(viewport, user) {
  const ctx = await browser.newContext({ viewport, ...opts })
  const p = await ctx.newPage()
  watch(p)
  await p.goto(BASE + '/login')
  await p.fill('input[autocomplete=username]', user)
  await p.fill('input[type=password]', 'demo1234')
  await p.click('button.primary')
  await p.waitForURL(u => !u.pathname.startsWith('/login'))
  return p
}

const ctx = await browser.newContext({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 2, ...opts, ...HERE })
const page = await ctx.newPage()
watch(page)

// ---- the home page finds the customer and lists the stores that deliver there ----
await page.goto(BASE + '/')
await page.waitForSelector(store('tea'))
let list = await slugs(page)
for (const s of ['makati', 'tea', 'chicken', 'cutlet', 'bento', 'luwei']) expect(list.includes(s), `${s} is missing from the stores: ${list}`)
expect(!list.includes('fries') && !list.includes('pasay'), `a store that does not deliver here is listed: ${list}`)
expect(list.indexOf('luwei') === list.length - 1, `the closed store is not last: ${list}`)
expect((await page.textContent(tid('mall-count'))).includes(String(list.length)), 'the count of stores')
const tea = await page.textContent(store('tea'))
expect(/4\.9/.test(tea) && /₱29/.test(tea) && /₱100/.test(tea) && /20 分钟/.test(tea), `the tea store's card: ${tea}`)
expect(/休息中/.test(await page.textContent(store('luwei'))), 'the closed store does not say so')
expect(await page.locator('[data-slug=fries]').count() === 0 && await page.locator(tid('mall-pickup')).count() === 0, 'a store that only offers pickup here is shown')
await page.waitForFunction(sel => /Stub St/.test(document.querySelector(sel).textContent), tid('mall-loc'))
expect(await page.locator(tid('customer-tabs')).isVisible(), 'no bottom navigation on a phone')
await page.screenshot({ path: `${SHOTS}/mall-01-home.png`, fullPage: true })
step('home page: only the stores that deliver to where the device is, the closed one last')

// Nothing about delivery areas reaches the customer.
const body = await page.textContent('body')
for (const word of ['配送范围', '配送区', 'Delivery area', 'polygon']) expect(!body.includes(word), `the page mentions delivery areas: ${word}`)
const raw = await page.evaluate(() => fetch('/api/v1/mall/stores?lat=14.5547&lng=121.0244').then(r => r.text()))
expect(!/zone|polygon|配送范围/i.test(raw), 'the list of stores tells about delivery areas')
step('no delivery area is shown or sent')

// ---- categories, search, sorting, filters ----
await page.click(`${tid('mall-categories')} [data-code=drinks]`)
await settled(page)
list = await slugs(page)
expect(list.length === 1 && list[0] === 'tea', `stores of the drinks category: ${list}`)
await page.click(`${tid('mall-categories')} button:first-child`)
await settled(page)

await page.fill(tid('mall-search'), '黄金鸡排')
await page.press(tid('mall-search'), 'Enter')
await page.waitForSelector(tid('mall-found'))
await settled(page)
list = await slugs(page)
expect(list.includes('cutlet') && !list.includes('tea') && !list.includes('bento'), `search by dish: ${list}`)
expect(/黄金鸡排/.test(await page.textContent(store('cutlet'))), 'the dish found is not shown')
await page.screenshot({ path: `${SHOTS}/mall-02-search.png` })
await page.fill(tid('mall-search'), '没有这个东西')
await page.press(tid('mall-search'), 'Enter')
await page.waitForSelector(tid('mall-no-match'))
await page.click(tid('mall-search-clear'))
await page.waitForSelector(store('tea'))
step('categories and search')

await page.click(`${tid('mall-sorts')} [data-sort=fee]`)
await settled(page)
list = await slugs(page)
expect(list.indexOf('tea') < list.indexOf('chicken') && list.indexOf('chicken') < list.indexOf('bento'), `sorted by fee: ${list}`)
await page.click(tid('mall-filter'))
await page.check('[data-filter=open]')
await settled(page)
expect(!(await slugs(page)).includes('luwei'), 'a closed store among those open now')
await page.uncheck('[data-filter=open]')
await page.click(tid('mall-filter'))
await page.click(`${tid('mall-sorts')} [data-sort=""]`)
await settled(page)
step('sorting and filters')

// ---- another place: a city, a pin on the map, and back to where the device is ----
await page.click(tid('mall-loc'))
await page.waitForSelector(tid('mall-relocate'))
await page.waitForTimeout(300)
await page.screenshot({ path: `${SHOTS}/mall-03-location.png` })
await page.click(tid('mall-cities'))
await page.click(`${tid('mall-city')}:has-text("Pasay")`)
await page.waitForSelector(store('pasay'))
list = await slugs(page)
expect(list.length === 1, `stores of Pasay: ${list}`)
expect(await page.locator(tid('mall-region-note')).isVisible(), 'browsing a city without the note about delivery')
expect(!/配送 ₱/.test(await page.textContent(store('pasay'))), 'a delivery fee without a place to deliver to')
await page.reload()
await page.waitForSelector(store('pasay'))
step('browse by city; the choice is kept when the page is opened again')

await page.click(tid('mall-loc'))
await page.click(tid('mall-map-pick'))
await page.waitForSelector(`${tid('pin-confirm')}:not([disabled])`)
await page.click(tid('pin-confirm'))
await page.waitForSelector(tid('mall-location'), { state: 'detached' })
await page.waitForSelector(tid('mall-stores'))
expect(await page.evaluate(() => JSON.parse(localStorage.getItem('mall_loc')).source) === 'place', 'the pinned place is not kept')
await page.click(tid('mall-loc'))
await page.click(tid('mall-relocate'))
await page.waitForFunction(() => JSON.parse(localStorage.getItem('mall_loc')).source === 'gps')
await page.waitForSelector(tid('mall-location'), { state: 'detached' })
await page.waitForSelector(store('tea'))
step('pin on the map, then located again')

// ---- into a store and back ----
await page.click(store('tea'))
await page.waitForURL(u => u.pathname === '/s/tea')
await page.waitForSelector('.item')
await page.click(tid('back-mall'))
await page.waitForURL(u => u.pathname === '/')
await page.waitForSelector(store('tea'))
await page.click(tid('mall-messages'))
await page.waitForURL(u => u.pathname === '/account/login')
step('into a store and back; messages ask a stranger to log in')

// ---- the device will not say where it is ----
const blind = await browser.newContext({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 2, ...opts })
const lost = await blind.newPage()
watch(lost)
await lost.goto(BASE + '/')
await lost.waitForSelector(tid('mall-denied'))
await lost.waitForTimeout(300)
await lost.screenshot({ path: `${SHOTS}/mall-04-denied.png` })
await lost.click(tid('mall-cities'))
await lost.click(`${tid('mall-city')}:has-text("Makati")`)
await lost.waitForSelector(store('tea'))
expect((await slugs(lost)).includes('fries'), 'a pickup-only store is missing from its city')
await blind.close()
step('no location: asked where to deliver, then browsing a city')

// ---- the merchant takes the store off, and puts it back ----
const owner = await staff({ width: 1280, height: 800 }, 'owner3')
await owner.goto(BASE + '/merchant/settings')
await owner.waitForSelector(tid('mall-listing'))
await owner.fill(tid('mall-tagline'), '现泡茶底，每天现煮珍珠')
await owner.uncheck(tid('mall-listed'))
await owner.click(tid('mall-listing-save'))
await owner.waitForSelector(tid('mall-listing-saved'))
await owner.screenshot({ path: `${SHOTS}/mall-05-merchant.png`, fullPage: true })
await page.goto(BASE + '/')
await page.waitForSelector(store('chicken'))
expect(!(await slugs(page)).includes('tea'), 'a store that opted out is listed')
await owner.check(tid('mall-listed'))
await owner.click(tid('mall-listing-save'))
await owner.waitForSelector(tid('mall-listing-saved'))
await page.reload()
await page.waitForSelector(store('tea'))
step('merchant: opt out of the marketplace and back in')

// ---- the platform: take a store off, banners ----
const admin = await staff({ width: 1280, height: 800 }, 'admin')
await admin.goto(BASE + '/admin/mall')
await admin.waitForSelector(`${tid('mall-row')}[data-slug=chicken]`)
const row = admin.locator(`${tid('mall-row')}[data-slug=chicken]`)
await row.locator(tid('mall-hide')).click()
await row.locator(tid('mall-unhide')).waitFor()
expect(/下架/.test(await row.locator(tid('mall-state')).textContent()), 'the state of a store taken off')
await page.reload()
await page.waitForSelector(store('tea'))
expect(!(await slugs(page)).includes('chicken'), 'a store the platform took off is listed')
await row.locator(tid('mall-unhide')).click()
await row.locator(tid('mall-hide')).waitFor()

const before = await admin.locator(tid('banner-row')).count()
await admin.click(tid('banner-new'))
await admin.fill(`${tid('banner-form')} [name=title]`, '测试活动')
await admin.fill(`${tid('banner-form')} [name=subtitle]`, '只是测试')
await admin.selectOption(`${tid('banner-form')} [name=link_slug]`, 'tea')
await admin.click(tid('banner-save'))
await admin.waitForFunction(([sel, n]) => document.querySelectorAll(sel).length === n + 1, [tid('banner-row'), before])
await admin.screenshot({ path: `${SHOTS}/mall-06-admin.png`, fullPage: true })
await page.reload()
await page.waitForSelector(store('chicken'))
expect((await page.textContent(tid('mall-banners'))).includes('测试活动'), 'the new banner is not on the home page')
admin.once('dialog', d => d.accept())
await admin.locator(`${tid('banner-row')}:has-text("测试活动") ${tid('banner-delete')}`).click()
await admin.waitForFunction(([sel, n]) => document.querySelectorAll(sel).length === n, [tid('banner-row'), before])
step('platform: take a store off and put it back; add and delete a banner')

// ---- a tablet and a computer ----
for (const [name, width, height, cols] of [['ipad', 1024, 768, 2], ['pc', 1440, 900, 3]]) {
  const c = await browser.newContext({ viewport: { width, height }, deviceScaleFactor: 2, ...opts, ...HERE })
  const p = await c.newPage()
  watch(p)
  await p.goto(BASE + '/')
  await p.waitForSelector(store('tea'))
  const tops = await p.locator(`${tid('mall-stores')} ${tid('mall-store')}`).evaluateAll(els => els.map(e => Math.round(e.getBoundingClientRect().top)))
  expect(tops.filter(t => t === tops[0]).length === cols, `${name}: ${cols} cards a row expected, tops ${tops}`)
  expect(!(await p.locator(tid('customer-tabs')).isVisible()), `${name}: the phone's bottom navigation shows`)
  expect(await p.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), `${name}: the page scrolls sideways`)
  await p.waitForTimeout(400)
  await p.screenshot({ path: `${SHOTS}/mall-07-${name}.png` })
  await c.close()
}
step('tablet: two cards a row; computer: three')

expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), 'the phone page scrolls sideways')
await browser.close()
if (errors.length) { console.error(errors.join('\n')); process.exit(1) }
console.log('mall: all good')

})().catch(e => { console.error(e); process.exit(1) })
