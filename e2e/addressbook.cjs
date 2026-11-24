// The platform's address book in a real browser (docs/ADDRESS_BOOK_DESIGN.md):
// on the order desk, "More" loads Google's suggestions only once reached, a
// chosen one carries its position, "View" opens Google Maps at it, an unknown
// address gets its position from "Open map"; the placed orders put both
// addresses in the book, which the next order suggests first; the platform's
// address page lists them by region and refuses a duplicate.
// Usage: BASE_URL=http://localhost:8080 node e2e/addressbook.cjs
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

const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: Number(process.env.DPR || 1), locale: 'zh-CN',
  permissions: ['clipboard-read', 'clipboard-write'] })
const page = await ctx.newPage()
page.on('pageerror', e => errors.push(`pageerror: ${e.message}`))
page.on('console', m => { if (m.type() === 'error' && !/favicon|Failed to load resource/.test(m.text())) errors.push(`console: ${m.text()}`) })
page.on('dialog', d => d.accept())
const shot = (name) => page.screenshot({ path: `${SHOTS}/${name}.png` })
const run = Date.now().toString(36).slice(-4)

async function login(user) {
  await page.goto(BASE + (/^admin/.test(user) ? '/admin/login' : /^((kitchen|rider|zhang|wang)\d|e2e\d+$)/.test(user) ? '/login?next=/work' : '/login'))
  await page.fill('input[autocomplete=username]', user)
  await page.fill('input[type=password]', 'demo1234')
  await page.click('button.primary')
  await page.waitForURL(u => !/\/login$/.test(u.pathname))
}
await login('owner1')
// The store's region is read off the map when its delivery map page opens.
await page.goto(BASE + '/merchant/delivery-map')
await page.waitForTimeout(2500)

async function newOrder(phone) {
  await page.goto(BASE + '/merchant/orders/new')
  await page.waitForSelector(tid('manual-order'))
  await page.locator(tid('mo-pick')).fill('b1')
  await page.waitForSelector(`${tid('mo-pick-drop')} .it.on`)
  await page.locator(tid('mo-pick')).press('Enter')
  await page.waitForSelector(tid('mo-line'))
  await page.click(tid('mo-new-member'))
  await page.waitForSelector(tid('mo-temp'))
  await page.fill(tid('mo-phone'), phone)
  await page.click(tid('mo-src-wechat'))
}
async function place() {
  await page.waitForFunction(() => !document.querySelector('[data-testid=mo-quote-error]'))
  await page.click(tid('mo-place'))
  await page.waitForSelector(tid('mo-done'))
}

// 1. A new address: nothing known, so "More" is the only line; Google is not asked until it is reached.
const place1 = `Ayala ${run}`
await newOrder('917' + run.replace(/\D/g, '1').padEnd(7, '1').slice(0, 7))
const addr = page.locator(tid('mo-address'))
await addr.click()
await addr.pressSequentially(place1, { delay: 20 })
await page.waitForSelector(tid('mo-more'))
expect(await page.locator(tid('mo-google')).count(), 0, 'Google suggestions before reaching "More"')
await page.hover(tid('mo-more'))
await page.waitForSelector(tid('mo-google'))
await shot('ab1-more-google')
step('"More" loads Google suggestions only once reached')
await page.click(`${tid('mo-google')} >> nth=0`)
await page.waitForSelector('.okc')
const line1 = await addr.inputValue()
const href = await page.locator(tid('mo-view')).getAttribute('href')
if (!/^https:\/\/www\.google\.com\/maps\/search\/\?api=1&query=-?\d+\.\d+,-?\d+\.\d+$/.test(href || '')) throw new Error('view link: ' + href)
step(`a Google suggestion is chosen with its position; "View" opens ${href}`)
await place()
await page.keyboard.press('Enter')

// 2. An unknown address: "Open map" gives it a position, and the typed words stay.
const place2 = `Lane ${run} near the old church`
await newOrder('918' + run.replace(/\D/g, '2').padEnd(7, '2').slice(0, 7))
await addr.fill(place2)
await page.click(tid('mo-name'))
expect(await page.locator(tid('mo-view')).getAttribute('href'), null, 'view link without a position')
await page.click(tid('mo-map'))
await page.waitForSelector(tid('pin-confirm') + ':not([disabled])')
await page.click(tid('pin-confirm'))
await page.waitForSelector('.okc')
expect(await addr.inputValue(), place2, 'the typed address after "Open map"')
step('an unknown address gets its position from the map and keeps its words')
await place()
await page.keyboard.press('Enter')

// 3. The next order: both come from the address book first, in any word order.
await newOrder('919' + run.replace(/\D/g, '3').padEnd(7, '3').slice(0, 7))
await addr.click()
await addr.pressSequentially(`church ${run}`, { delay: 20 })
await page.waitForSelector(`${tid('mo-hint')}[data-kind=book]`)
expect((await page.locator(`${tid('mo-hint')}[data-kind=book]`).first().innerText()).includes(place2), true, 'book suggestion for the map address')
await addr.fill('')
await addr.pressSequentially(run, { delay: 20 })
await page.waitForFunction(() => document.querySelectorAll('[data-testid=mo-hint][data-kind=book]').length >= 2)
await page.keyboard.press('ArrowDown')
await shot('ab2-book-first')
await page.keyboard.press('Enter')
await page.waitForSelector('.okc')
step('the next order suggests both from the address book, chosen with the keyboard')

// 4. The platform's address page.
await ctx.clearCookies()
await login('admin')
await page.goto(BASE + '/admin/addresses')
await page.waitForSelector(tid('ab-row'))
await page.click(`${tid('ab-country')}:has-text("PH")`)
await page.click(`${tid('ab-province')} >> nth=0`)
await page.click(`${tid('ab-city')} >> nth=0`)
await page.fill(tid('ab-search'), run)
await page.press(tid('ab-search'), 'Enter')
await page.waitForFunction(() => document.querySelectorAll('[data-testid=ab-row]').length === 2)
await shot('ab3-admin')
step('the platform lists both addresses under their country / province / city')
await page.click(tid('ab-new'))
await page.fill(tid('ab-f-line'), line1.toUpperCase().replace(/,/g, ' ，'))
await page.fill(tid('ab-f-at'), '14.5547, 121.0244')
await page.click(tid('ab-save'))
await page.waitForSelector(tid('ab-form-error'))
expect(await page.locator(tid('ab-form-error')).innerText(), '这个地区已经有这个地址了', 'duplicate')
await shot('ab4-admin-duplicate')
await page.fill(tid('ab-f-line'), `Greenbelt ${run}`)
await page.click(tid('ab-save'))
await page.waitForSelector(tid('ab-form'), { state: 'detached' })
await page.waitForFunction(() => document.querySelectorAll('[data-testid=ab-row]').length === 3)
step('a duplicate written differently is refused; a new one is added')

// phone width
await page.setViewportSize({ width: 390, height: 844 })
await page.waitForTimeout(300)
const wide = await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth + 1)
expect(wide, false, 'page wider than the phone')
await shot('ab5-admin-phone')
step('the address page fits a phone')

if (errors.length) throw new Error(errors.join('\n'))
await browser.close()
console.log('address book: all good')
})().catch(e => { console.error(e); process.exit(1) })
