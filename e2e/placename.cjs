// A place asked for by name on the order desk (docs/ADDRESS_PLACE_DESIGN.md),
// against the stand-in for the Google Maps API (e2e/fake-google.js):
//   - with Places off, the geocoder answers "trump tower" with the street it
//     stands on: the desk says the answers are degraded and keeps the typed
//     name in front of the street;
//   - with Places on, the chosen place brings its full street address, in the
//     language of the store's country whatever the browser speaks, and "View"
//     opens that place by its id;
//   - the keys page tells which Google services answer.
// Usage: BASE_URL=http://localhost:8080 node e2e/placename.cjs   (needs seed data)
const { chromium } = require('playwright')
const { mkdirSync } = require('node:fs')
const path = require('node:path')

;(async () => {

const BASE = process.env.BASE_URL || 'http://localhost:8080'
const SHOTS = process.env.SHOTS_DIR || 'e2e/shots'
mkdirSync(SHOTS, { recursive: true })
const browser = await chromium.launch()
const errors = []
const step = (s) => console.log('✓ ' + s)
const expect = (got, want, what) => { if (got !== want) throw new Error(`${what}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`) }
const tid = (id) => `[data-testid=${id}]`

async function open(flags) {
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: Number(process.env.DPR || 1), locale: 'zh-CN' })
  await ctx.addInitScript({ path: path.join(__dirname, 'fake-google.js') })
  await ctx.addInitScript(f => Object.assign(window, f), flags)
  // Whatever key the server has, the page gets one that selects the Google adapter.
  await ctx.route('**/api/v1/config', async route => {
    const r = await route.fetch()
    await route.fulfill({ response: r, json: { ...(await r.json()), maps_key: 'fake-google-key' } })
  })
  const page = await ctx.newPage()
  page.on('pageerror', e => errors.push(`pageerror: ${e.message}`))
  page.on('console', m => { if (m.type() === 'error' && !/favicon|Failed to load resource/.test(m.text())) errors.push(`console: ${m.text()}`) })
  page.on('dialog', d => d.accept())
  return { ctx, page }
}
async function login(page, user) {
  await page.goto(BASE + (user === 'admin' ? '/admin/login' : '/login'))
  await page.fill('input[autocomplete=username]', user)
  await page.fill('input[type=password]', 'demo1234')
  await page.click('button.primary')
  await page.waitForURL(u => !/\/login$/.test(u.pathname))
}
async function ask(page, words) {
  await page.goto(BASE + '/merchant/order/delivery')
  await page.waitForSelector(tid('manual-order'))
  await page.click(tid('mo-new-member'))
  await page.waitForSelector(tid('mo-temp'))
  const addr = page.locator(tid('mo-address'))
  await addr.click()
  await addr.pressSequentially(words, { delay: 20 })
  await page.waitForSelector(tid('mo-more'))
  await page.hover(tid('mo-more'))
  await page.waitForSelector(tid('mo-google'))
  return addr
}

// 1. Places is not enabled on the key.
{
  const { ctx, page } = await open({ __fakeGeocodes: true })
  await login(page, 'owner1')
  const addr = await ask(page, 'trump tower')
  await page.waitForSelector(tid('mo-weak'))
  expect(await page.locator(`${tid('mo-google')} b`).first().innerText(), 'trump tower', 'the typed name leads a geocoder answer')
  expect((await page.locator(`${tid('mo-google')} .muted`).first().innerText()).startsWith('Makati Ave, Makati City'), true, 'the street follows the name')
  const g = await page.evaluate(() => window.__fakeAsked.geocode.filter(x => x.address).at(-1).language)
  expect(g, 'en', 'the geocoder is asked in the language of the store\'s country')
  await page.screenshot({ path: `${SHOTS}/pn1-weak.png` })
  await page.click(`${tid('mo-google')} >> nth=0`)
  await page.waitForSelector('.okc')
  expect((await addr.inputValue()).startsWith('trump tower, Makati Ave, Makati City'), true, 'the address keeps the typed name: ' + await addr.inputValue())
  step('with Places off the desk warns, and "trump tower" stays in front of the street the geocoder gives')

  // a typed street address that the geocoder repeats is not doubled
  await ask(page, '6750 Ayala Ave')
  expect(await page.locator(`${tid('mo-google')} b`).first().innerText(), '6750 Ayala Ave, PH', 'a street address stands as the geocoder wrote it')
  expect(await page.locator(`${tid('mo-google')} .muted`).count(), 0, 'no second line for a street address')
  step('a typed street address is not repeated as its own name')
  await ctx.close()
}

// 2. Places is enabled.
{
  const { ctx, page } = await open({ __fakeGeocodes: true, __fakePlaces: true })
  await login(page, 'owner1')
  const addr = await ask(page, 'trump tower')
  expect(await page.locator(tid('mo-weak')).count(), 0, 'no warning when Places answers')
  expect(await page.locator(`${tid('mo-google')} b`).first().innerText(), 'trump tower', 'the place name')
  const q = await page.evaluate(() => { const r = window.__fakeAsked.suggest.at(-1); return r.language + '/' + r.includedRegionCodes.join() })
  expect(q, 'en/ph', 'suggestions are asked in the country\'s language, though the browser is Chinese')
  await page.screenshot({ path: `${SHOTS}/pn2-places.png` })
  await page.click(`${tid('mo-google')} >> nth=0`)
  await page.waitForSelector('.okc')
  expect(await addr.inputValue(), 'trump tower, 1 Kalayaan Ave, Poblacion, Makati (en)', 'name, then the full street address in the language asked for')
  const d = await page.evaluate(() => window.__fakeAsked.details.at(-1))
  expect(d.id + '/' + d.language + '/' + d.fields.join(), 'place-1/en/location,formattedAddress,types', 'what is read of the chosen place')
  const href = await page.locator(tid('mo-view')).getAttribute('href')
  if (!/^https:\/\/www\.google\.com\/maps\/search\/\?api=1&query=-?\d+\.\d+,-?\d+\.\d+&query_place_id=place-1$/.test(href || '')) throw new Error('view link: ' + href)
  step(`a chosen place brings its full address in English; "View" opens ${href}`)

  // The name is kept beside the address, and a named place asks for the unit.
  expect(await page.locator(`${tid('mo-place-name')} b`).innerText(), 'trump tower', 'the place name under the address')
  await page.waitForSelector(tid('mo-unit-ask'))
  await page.screenshot({ path: `${SHOTS}/pn4-unit-ask.png` })
  await page.fill(tid('mo-unit'), '32F Unit 3205')
  expect(await page.locator(tid('mo-unit-ask')).count(), 0, 'no reminder once a unit is typed')
  step('a named place shows its name under the address and reminds staff to ask for the unit')

  // The order carries the name: the done text, the order page and the ticket show name and unit first, the street after.
  const phone = '917' + String(Date.now()).slice(-7)
  await page.fill(tid('mo-phone'), phone)
  const who = 'PN' + phone.slice(-5)
  await page.fill(tid('mo-name'), who)
  await page.click(tid('mo-src-wechat'))
  await page.locator(tid('mo-pick')).fill('b1')
  await page.waitForSelector(`${tid('mo-pick-drop')} .it.on`)
  await page.locator(tid('mo-pick')).press('Enter')
  await page.waitForSelector(tid('mo-line'))
  await page.waitForFunction(() => !document.querySelector('[data-testid=mo-quote-error]'))
  await page.click(tid('mo-place'))
  await page.waitForSelector(tid('mo-done'))
  const text = await page.locator(tid('mo-done-text')).innerText()
  if (!text.includes('trump tower · 32F Unit 3205，1 Kalayaan Ave, Poblacion, Makati (en)')) throw new Error('the text for the customer: ' + text)
  await page.keyboard.press('Enter')
  await page.goto(BASE + '/merchant/orders')
  await page.locator('.card.ord', { hasText: who }).first().click()
  const at = page.locator(`[data-testid=order-contact] ~ div ${tid('addr-text')}`).first()
  await at.waitFor()
  expect(await at.locator('.addr-name').innerText(), 'trump tower', 'the order page: name')
  expect((await at.locator('.addr-unit').innerText()).replace(/^[\s·]+/, ''), '32F Unit 3205', 'the order page: unit')
  expect(await at.locator('.addr-street').innerText(), '1 Kalayaan Ave, Poblacion, Makati (en)', 'the order page: street')
  const map = await page.locator('a[href*="query_place_id=place-1"]').count()
  expect(map > 0, true, 'the order page opens the place by its id')
  await page.screenshot({ path: `${SHOTS}/pn5-order.png` })
  step('the order shows "trump tower · 32F Unit 3205" first and the street below, here and in the text for the customer')

  // The next order for another number is offered the place from the address book, by name.
  await page.goto(BASE + '/merchant/order/delivery')
  await page.waitForSelector(tid('manual-order'))
  await page.click(tid('mo-new-member'))
  await page.waitForSelector(tid('mo-temp'))
  await addr.click()
  await addr.pressSequentially('trump', { delay: 20 })
  const hint = page.locator(`${tid('mo-hint')}[data-kind=book]`).first()
  await hint.waitFor()
  expect(await hint.locator('b').first().innerText(), 'trump tower', 'the address book offers the place by name')
  await page.screenshot({ path: `${SHOTS}/pn6-book.png` })
  await hint.click()
  expect(await page.locator(`${tid('mo-place-name')} b`).innerText(), 'trump tower', 'a chosen book address brings its name')
  if (!/query_place_id=place-1$/.test(await page.locator(tid('mo-view')).getAttribute('href') || '')) throw new Error('view link of a book address')
  step('the address book offers the place by name on the next order, with its id')

  // typing something else forgets the place
  await addr.fill('somewhere else')
  expect(await page.locator(tid('mo-view')).getAttribute('href'), null, 'no view link once the address is retyped')
  expect(await page.locator(tid('mo-place-name')).count(), 0, 'no place name once the address is retyped')
  await ctx.close()
}

// 3. The keys page says which services answer.
for (const [flags, want] of [[{ __fakeGeocodes: true }, 'false'], [{ __fakeGeocodes: true, __fakePlaces: true }, 'true']]) {
  const { ctx, page } = await open(flags)
  await login(page, 'admin')
  await page.goto(BASE + '/admin/settings')
  await page.click(tid('g-check-run'))
  await page.waitForSelector(`${tid('g-check')} [data-svc=places]`)
  expect(await page.locator(`${tid('g-check')} [data-svc=maps]`).getAttribute('data-ok'), 'true', 'maps')
  expect(await page.locator(`${tid('g-check')} [data-svc=geocoding]`).getAttribute('data-ok'), 'true', 'geocoding')
  expect(await page.locator(`${tid('g-check')} [data-svc=places]`).getAttribute('data-ok'), want, 'places')
  await page.locator('[data-group=maps]').screenshot({ path: `${SHOTS}/pn3-check-${want}.png` })
  await ctx.close()
}
step('the keys page tells Places on from Places off')

if (errors.length) throw new Error('browser errors:\n' + errors.join('\n'))
await browser.close()
console.log('placename: all steps passed')

})().catch(e => { console.error(e); process.exit(1) })
