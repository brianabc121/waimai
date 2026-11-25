// Ordering for a customer in a real browser: dishes found by code, a spec and
// add-ons counted, a new store member, the address area, placing the order
// with the confirmation copied, then finding the member again by number and
// seeing the order on the desk.
// Usage: BASE_URL=http://localhost:8080 node e2e/manualorder.cjs   (needs seed data)
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
const text = async (page, sel) => (await page.locator(sel).first().innerText()).replace(/\s+/g, ' ').trim()

const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: Number(process.env.DPR || 1), locale: 'zh-CN',
  permissions: ['clipboard-read', 'clipboard-write'], ignoreHTTPSErrors: process.env.E2E_INSECURE_TLS === '1' })
const page = await ctx.newPage()
page.on('pageerror', e => errors.push(`pageerror: ${e.message}`))
page.on('console', m => { if (m.type() === 'error' && !/favicon|Failed to load resource/.test(m.text())) errors.push(`console: ${m.text()}`) })
page.on('dialog', d => d.accept(d.type() === 'prompt' ? '' : undefined))
const shot = (name) => page.screenshot({ path: `${SHOTS}/${name}.png` })

await page.goto(BASE + '/login')
await page.fill('input[autocomplete=username]', 'owner1')
await page.fill('input[type=password]', 'demo1234')
await page.click('button.primary')
await page.waitForURL(u => !/\/login$/.test(u.pathname))
await page.goto(BASE + '/merchant/orders')
await page.click(tid('manual-order-new'))
await page.waitForURL(u => u.pathname === '/merchant/order/delivery')
await page.waitForSelector(tid('manual-order'))
step('the desk opens "order for a customer"')

// dishes by code: B1 goes straight in, B2 asks for its spice first
const pick = page.locator(tid('mo-pick'))
await pick.fill('b1')
await page.waitForSelector(`${tid('mo-pick-drop')} .it.on:has-text("卤肉饭")`)
await pick.press('Enter')
await page.waitForSelector(`${tid('mo-line')}:has-text("卤肉饭")`)
await page.click(`${tid('mo-line')}:has-text("卤肉饭") ${tid('mo-plus')}`)
expect(await text(page, `${tid('mo-line')}:has-text("卤肉饭") ${tid('mo-qty')}`), '2', 'braised pork rice count')
await pick.fill('a1')
await pick.press('Enter')
await page.waitForSelector(tid('mo-spec'))
await page.click(tid('mo-spec-add'))
await page.waitForSelector(`${tid('mo-line')}:has-text("盐酥鸡")`)
step('dishes found by code; a dish with a spice choice asks for it')

// add-ons on the selected dish: twice extra basil
await page.fill(tid('mo-addon-q'), '九层塔')
await page.press(tid('mo-addon-q'), 'Enter')
await page.fill(tid('mo-addon-q'), '九层塔')
await page.press(tid('mo-addon-q'), 'Enter')
await page.waitForSelector(`${tid('mo-addon')}:has-text("加九层塔")`)
expect((await text(page, tid('mo-addon'))).includes(' 2 '), true, 'two portions of basil: ' + await text(page, tid('mo-addon')))
// the group allows two kinds, not two portions: a third basil and a garlic still go in
await page.fill(tid('mo-addon-q'), '九层塔')
await page.press(tid('mo-addon-q'), 'Enter')
await page.fill(tid('mo-addon-q'), '加蒜')
await page.press(tid('mo-addon-q'), 'Enter')
await page.waitForSelector(`${tid('mo-addon')}:has-text("加蒜")`)
expect((await text(page, `${tid('mo-addon')}:has-text("九层塔")`)).includes(' 3 '), true, 'three portions of basil: ' + await text(page, tid('mo-addon')))
await page.click(`${tid('mo-addon')}:has-text("九层塔") button:has-text("−")`)
await page.click(`${tid('mo-addon')}:has-text("加蒜") button:has-text("−")`)
await page.waitForSelector(`${tid('mo-addon')}:has-text("加蒜")`, { state: 'detached' })
step('an add-on counted twice; the group limit is on kinds, so a third portion and a second kind fit')

// a new member: the next number, held for an hour
await page.click(tid('mo-new-member'))
await page.waitForSelector(tid('mo-temp'))
const no = (await page.locator(tid('mo-member')).inputValue()).trim()
expect(/^\d{6}$/.test(no), true, 'member number ' + no)
await page.fill(tid('mo-phone'), '9175552046')
await page.fill(tid('mo-name'), '陈小姐')
await page.click(tid('mo-src-wechat'))
await page.fill(tid('mo-address'), '2621 Zamora St, Pasay')
await page.click(tid('mo-name'))
// the store delivers by map, so the typed address is placed with "Open map" (the typed words stay)
await page.click(tid('mo-map'))
await page.waitForSelector(tid('pin-confirm') + ':not([disabled])')
await page.click(tid('pin-confirm'))
await page.waitForSelector('.okc')
await page.fill(tid('mo-staff-note'), '老顾客介绍')
await page.waitForFunction(() => !document.querySelector('[data-testid=mo-quote-error]'))
await page.waitForFunction(() => /₱/.test(document.querySelector('[data-testid=mo-total]')?.textContent || ''))
await page.locator(tid('mo-pick')).click()
await page.waitForTimeout(400)
await shot('manual-order-new-member')
step(`new member ${no}, phone, source and address filled`)

// place it: the confirmation is shown and copied
await page.click(tid('mo-place'))
await page.waitForSelector(tid('mo-done'))
const msg = await page.locator(tid('mo-done-text')).innerText()
const orderNo = (await text(page, `${tid('mo-done')} h2`)).match(/[A-Z][0-9-]+/)[0]
for (const want of ['订单确认', 'B1 卤肉饭 ×2', '加九层塔 ×2', '地址：2621 Zamora St, Pasay', '/o/']) {
  if (!msg.includes(want)) throw new Error(`confirmation lacks ${want}:\n${msg}`)
}
const clip = await page.evaluate(() => navigator.clipboard.readText()).catch(() => '')
expect(clip, msg, 'copied text')
await shot('manual-order-done')
step('order placed; the confirmation is copied')

// next order: Enter clears the page
await page.keyboard.press('Enter')
await page.waitForSelector(tid('mo-done'), { state: 'detached' })
expect(await page.locator(tid('mo-line')).count(), 0, 'lines after "next order"')

// find the member again by number
await page.fill(tid('mo-member'), String(Number(no)))
await page.click(tid('mo-find'))
await page.waitForSelector(tid('mo-found'))
expect(await page.locator(tid('mo-phone')).inputValue(), '9175552046', 'phone of the found member')
expect(await page.locator(tid('mo-name')).inputValue(), '陈小姐', 'name of the found member')
if (!(await text(page, tid('mo-found'))).includes('本周')) throw new Error('member stats missing')
await shot('manual-order-member-found')
step('the member is found again by number, with their details')

// unknown number
await page.fill(tid('mo-member'), '999999')
await page.click(tid('mo-find'))
await page.waitForSelector(`${tid('mo-member-hint')}.red`)
step('an unknown number says so')

// the desk shows who placed it
await page.goto(BASE + '/merchant/orders')
await page.waitForSelector(tid('by-staff-tag'))
await page.click(`.ord:has(${tid('by-staff-tag')}):has-text("${orderNo}")`)
await page.waitForSelector(tid('by-staff'))
const info = await text(page, tid('by-staff'))
if (!info.includes('微信') || !info.includes(no)) throw new Error('order desk: ' + info)
step('the order desk marks the order as placed by staff')

// the phone layout fits without sideways scrolling
await page.setViewportSize({ width: 390, height: 844 })
await page.goto(BASE + '/merchant/order/delivery')
await page.waitForSelector(tid('manual-order'))
const wide = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
expect(wide <= 0, true, 'page wider than the phone by ' + wide)
await page.fill(tid('mo-pick'), 'b1')
await page.press(tid('mo-pick'), 'Enter')
await page.waitForSelector(tid('mo-line'))
const bar = await page.locator(tid('mo-place')).boundingBox()
expect(bar && bar.y + bar.height <= 844, true, '"place order" stays on screen')
await shot('manual-order-phone')
step('fits a phone; "place order" stays at the bottom')

if (errors.length) throw new Error('browser errors:\n' + errors.join('\n'))
await browser.close()
console.log('manual order e2e passed')
})().catch(e => { console.error(e); process.exit(1) })
