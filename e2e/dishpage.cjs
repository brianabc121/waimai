// The store page after the clean-up of 2026-11: the category rail runs to the
// bottom however few dishes there are, there is no "do you deliver to me" box,
// the picture area at the top has the same height in every store, and every
// dish opens a dish page with more pictures and details that the merchant sets.
// The store "bgc" is changed and put back; "makati" has promo media, "pasay" a cover.
// Usage: BASE_URL=http://localhost:8080 node e2e/dishpage.cjs   (needs seed data)
const { chromium } = require('playwright')
const { mkdirSync } = require('node:fs')

;(async () => {

const BASE = process.env.BASE_URL || 'http://localhost:8080'
const SHOTS = process.env.SHOTS_DIR || 'e2e/shots'
mkdirSync(SHOTS, { recursive: true })
const browser = await chromium.launch()
const errors = []

async function open(viewport) {
  const ctx = await browser.newContext({ viewport, locale: 'zh-CN', deviceScaleFactor: 2, ignoreHTTPSErrors: process.env.E2E_INSECURE_TLS === '1' })
  const page = await ctx.newPage()
  page.on('pageerror', e => errors.push(`pageerror: ${e.message}`))
  page.on('console', m => { if (m.type() === 'error' && !/favicon|Failed to load resource/.test(m.text())) errors.push(`console: ${m.text()}`) })
  page.on('dialog', d => d.accept())
  return page
}
const step = (s) => console.log('✓ ' + s)
const shot = (page, name) => page.screenshot({ path: `${SHOTS}/${name}.png` })
const expect = (got, want, what) => { if (got !== want) throw new Error(`${what}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`) }
const tid = (id) => `[data-testid=${id}]`
const PHONE = { width: 390, height: 844 }, PAD = { width: 820, height: 1180 }, PC = { width: 1366, height: 900 }

// ---- the merchant gives a dish more pictures and details ----
const owner = await open({ width: 1280, height: 900 })
await owner.goto(BASE + '/login')
await owner.fill('input[autocomplete=username]', 'owner2')
await owner.fill('input[type=password]', 'demo1234')
await owner.click('button.primary')
await owner.waitForURL(u => !u.pathname.startsWith('/login'))
const png = await owner.evaluate(async () => {
  const c = document.createElement('canvas'); c.width = 400; c.height = 300
  const g = c.getContext('2d'); g.fillStyle = '#c0392b'; g.fillRect(0, 0, 400, 300); g.fillStyle = '#f5d76e'; g.beginPath(); g.arc(200, 150, 100, 0, 7); g.fill()
  const b = new Uint8Array(await (await new Promise(r => c.toBlob(r, 'image/png'))).arrayBuffer()); let s = ''; for (const x of b) s += String.fromCharCode(x); return btoa(s)
})
const editDish = async () => {
  await owner.goto(BASE + '/merchant/menu')
  await owner.locator('tr', { has: owner.locator('b', { hasText: /^盐酥鸡$/ }) }).locator('button', { hasText: '编辑' }).click()
  await owner.waitForSelector(tid('item-form'))
}
const save = async () => {
  await owner.click(`${tid('item-form')} .sheet-foot .btn.primary`)
  await owner.waitForSelector(tid('item-form'), { state: 'detached' })
}
await editDish()
const had = await owner.locator(`${tid('item-photos')} [data-role=photo]`).count()
const hadDetails = await owner.inputValue(tid('item-details'))
await owner.setInputFiles(`${tid('item-photos')} input[type=file]`, { name: 'more.png', mimeType: 'image/png', buffer: Buffer.from(png, 'base64') })
await owner.click(tid('crop-save'))
await owner.waitForSelector(tid('cropper'), { state: 'detached' })
await owner.waitForSelector(`${tid('item-photos')} [data-role=photo]:nth-child(${had + 1}) img[src^="/api/v1/images/"]`)
await owner.fill(tid('item-details'), '主料：鸡腿肉\n份量：约 300 克')
await shot(owner, 'dp1-item-form')
await save()
step('the merchant adds a picture (uploaded and cropped) and details to a dish')

// ---- phone: rail, no address box, the dish page ----
const phone = await open(PHONE)
await phone.goto(BASE + '/s/bgc')
await phone.waitForSelector('.item')
expect(await phone.locator(tid('deliver-to')).count(), 0, 'no "do you deliver to me" box on the store page')
expect((await phone.textContent('body')).includes('看看能不能送到我这里'), false, 'no "do you deliver to me" text')
// one category only: the rail still runs down to the bottom of the screen
await phone.click(tid('search-toggle'))
await phone.fill(tid('search-box'), '盐酥鸡')
await phone.waitForFunction(() => document.querySelectorAll('.item').length === 1)
await phone.locator('.shoptabs').evaluate(e => window.scrollTo(0, e.getBoundingClientRect().top + scrollY))
const rail = await phone.evaluate(() => ({ body: document.querySelector('.menu .body').getBoundingClientRect().bottom, view: innerHeight }))
expect(rail.body >= rail.view - 1, true, `the grey rail stops at ${rail.body}px of ${rail.view}px`)
await shot(phone, 'dp2-phone-one-dish')
step('with a single dish the category rail runs to the bottom; no address box')

await phone.click('.item:has-text("盐酥鸡") .bold')
await phone.waitForSelector(tid('dish-detail'))
expect(await phone.textContent(tid('dish-count')), `1/${had + 2}`, 'the main picture and the more pictures')
expect(await phone.textContent(tid('dish-details')), '主料：鸡腿肉\n份量：约 300 克', 'details on the dish page')
await shot(phone, 'dp3-phone-dish')
await phone.click(tid('dish-next'))
await phone.waitForFunction(sel => document.querySelector(sel).textContent.startsWith('2/'), tid('dish-count'))
await phone.click(tid('dish-choose'))
await phone.waitForSelector(tid('dish-detail'), { state: 'detached' })
await phone.waitForSelector('.sheet .opt')
await phone.locator('.sheet-mask').last().click({ position: { x: 10, y: 10 } })
await phone.fill(tid('search-box'), '')
await phone.click('.item:has-text("无盐地瓜条") .bold')
await phone.click(tid('dish-add'))
await phone.waitForSelector(`${tid('dish-detail')} .stepper`)
expect(await phone.textContent(tid('cart-count')), '1', 'added from the dish page')
expect(await phone.textContent(tid('dish-details')), '商家还没有填写详情', 'a dish without details says so')
await phone.click(tid('dish-close'))
await phone.waitForSelector(tid('dish-detail'), { state: 'detached' })
await phone.click('.item:has-text("无盐地瓜条") .stepper button:not(.fill)')
step('a dish opens its page: pictures swipe, details show, options and adding work from there')

// ---- the picture area at the top: the same height in every store ----
const heroHeight = async (page, slug) => {
  await page.goto(BASE + '/s/' + slug)
  await page.waitForSelector('.item')
  return page.locator('.hero-media').evaluate(e => Math.round(e.getBoundingClientRect().height))
}
for (const [name, vp] of [['phone', PHONE], ['pad', PAD], ['pc', PC]]) {
  const page = await open(vp)
  const media = await heroHeight(page, 'makati'); await shot(page, `dp4-${name}-media`)
  const cover = await heroHeight(page, 'pasay'); await shot(page, `dp5-${name}-cover`)
  expect(Math.abs(media - cover) <= 1, true, `${name}: promo media ${media}px, cover ${cover}px`)
  await page.click('.item:has-text("烧腊双拼饭") .bold, .item >> nth=0')
  await page.waitForSelector(tid('dish-detail'))
  await shot(page, `dp6-${name}-dish`)
}
step('promo media and a cover take the same height at the top, on a phone, a tablet and a computer')

// ---- where a dish is offered: dine-in, delivery, pickup, all ticked by default ----
await owner.goto(BASE + '/merchant/menu')
await owner.click('button:has-text("新增菜品")')
await owner.waitForSelector(tid('item-form'))
for (const c of ['item-dine-in', 'item-delivery', 'item-pickup']) expect(await owner.isChecked(tid(c)), true, `${c} is ticked for a new dish`)
await owner.click(`${tid('item-form')} .sheet-foot .btn:not(.primary)`)
await owner.locator('tr', { has: owner.locator('b', { hasText: /^无盐地瓜条$/ }) }).locator('button', { hasText: '编辑' }).click()
await owner.uncheck(tid('item-delivery')); await owner.uncheck(tid('item-pickup'))
await shot(owner, 'dp7-item-channels')
await save()
const ph2 = await open(PHONE)
await ph2.goto(BASE + '/s/bgc')
await ph2.waitForSelector('.item')
expect(await ph2.locator('.item:has-text("无盐地瓜条")').count(), 0, 'a dine-in-only dish is not on the delivery menu')
await owner.locator('tr', { has: owner.locator('b', { hasText: /^无盐地瓜条$/ }) }).locator('button', { hasText: '编辑' }).click()
await owner.check(tid('item-delivery')); await owner.check(tid('item-pickup'))
await save()
step('a dish only for dine-in leaves the delivery menu; new dishes are offered everywhere')

// ---- put the dish back ----
await editDish()
await owner.click(`${tid('item-photos')} [data-role=photo]:nth-child(${had + 1}) [data-role=remove]`)
await owner.fill(tid('item-details'), hadDetails)
await save()
step('the dish is put back as it was')

await browser.close()
if (errors.length) { console.error(errors.join('\n')); process.exit(1) }
console.log('dish page: all steps passed')

})().catch(e => { console.error(e); process.exit(1) })
