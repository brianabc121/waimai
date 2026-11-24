// "Order for a customer" by picture: staff who do not remember the codes open
// the dish pictures, tap dishes (one tap adds one, a dish with options asks
// first), and the dishes land on the order as if typed by code. Run on a
// computer, a tablet and a phone.
// Usage: BASE_URL=http://localhost:8080 node e2e/manualpic.cjs   (needs seed data)
const { chromium, devices } = require('playwright')
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

const sizes = [
  ['pc', { viewport: { width: 1440, height: 900 } }],
  ['ipad', { viewport: { width: 820, height: 1180 }, hasTouch: true, isMobile: true }],
  ['phone', { ...devices['iPhone 13'] }],
]
for (const [name, opts] of sizes) {
  const ctx = await browser.newContext({ ...opts, deviceScaleFactor: Number(process.env.DPR || opts.deviceScaleFactor || 1), locale: 'zh-CN' })
  const page = await ctx.newPage()
  page.on('pageerror', e => errors.push(`${name} pageerror: ${e.message}`))
  page.on('console', m => { if (m.type() === 'error' && !/favicon|Failed to load resource/.test(m.text())) errors.push(`${name} console: ${m.text()}`) })
  page.on('dialog', d => d.accept())
  await page.goto(BASE + '/login')
  await page.fill('input[autocomplete=username]', 'owner1')
  await page.fill('input[type=password]', 'demo1234')
  await page.click('button.primary')
  await page.waitForURL(u => !/\/login$/.test(u.pathname))
  await page.goto(BASE + '/merchant/orders/new?mode=delivery')
  await page.waitForSelector(tid('manual-order'))

  // by code still works
  await page.fill(tid('mo-pick'), 'b1')
  await page.waitForSelector(`${tid('mo-pick-drop')} .it.on`)
  await page.press(tid('mo-pick'), 'Enter')
  await page.waitForSelector(`${tid('mo-line')}:has-text("卤肉饭")`)
  if (name !== 'pc') await page.locator(tid('mo-pick')).blur()

  await page.click(tid('mo-by-pic'))
  await page.waitForSelector(tid('mo-pic'))
  // the dish already taken by code is counted on its picture
  const cats = page.locator('[data-testid^=mo-pic-cat-]')
  const find = async (sel) => {
    for (let i = 0; i < await cats.count(); i++) {
      await cats.nth(i).click()
      const c = page.locator(sel).first()
      if (await c.count()) return c
    }
    return null
  }
  const rice = await find('[data-testid^=mo-pic-dish-]:has-text("卤肉饭")')
  if (!rice) throw new Error('no rice')
  const riceId = (await rice.getAttribute('data-testid')).replace('mo-pic-dish-', '')
  if (name === 'phone') { await page.click(tid(`mo-pic-add-${riceId}`)); await page.click(tid(`mo-pic-add-${riceId}`)) }
  else { await rice.click(); await rice.click() }
  expect((await rice.locator(name === 'phone' ? '.dd-stp b' : '.n').innerText()).trim(), '3', `${name} count on the picture`)
  await page.screenshot({ path: `${SHOTS}/pic-${name}-1-tiles.png` })
  // a dish with options asks first
  const spec = await find('[data-testid^=mo-pic-dish-]:has(.mk:has-text("规格")):not(.out)')
  if (!spec) throw new Error('no dish with options')
  if (name === 'phone') await spec.locator('[data-testid^=mo-pic-add-]').click(); else await spec.click()
  await page.waitForSelector(tid('mo-spec'))
  await page.screenshot({ path: `${SHOTS}/pic-${name}-2-spec.png` })
  await page.click(tid('mo-spec-add'))
  await page.waitForSelector(tid('mo-spec'), { state: 'detached' })
  if (name === 'phone') {
    await page.click(tid('mo-pic-open-lines'))
    await page.waitForSelector(tid('mo-pic-lines'))
  }
  expect(await page.locator(tid('mo-pic-line')).count(), 2, `${name} lines in the picker`)
  await page.screenshot({ path: `${SHOTS}/pic-${name}-3-added.png` })
  if (name === 'phone') await page.click(`${tid('mo-pic-lines')} ${tid('mo-pic-done')}`)
  else await page.click(tid('mo-pic-done'))
  await page.waitForSelector(tid('mo-pic'), { state: 'detached' })
  // back on the order: the rice is one line of 3, the dish with options one more line
  expect(await page.locator(tid('mo-line')).count(), 2, `${name} order lines`)
  expect((await page.locator(`${tid('mo-line')}:has-text("卤肉饭") ${tid('mo-qty')}`).innerText()).trim(), '3', `${name} rice count`)
  await page.screenshot({ path: `${SHOTS}/pic-${name}-4-order.png`, fullPage: name !== 'pc' })
  const wide = await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth + 1)
  if (wide) throw new Error(`${name}: the page scrolls sideways`)
  step(`${name}: dishes added by picture and by code`)
  await ctx.close()
}
await browser.close()
if (errors.length) { console.error(errors.join('\n')); process.exit(1) }
console.log('manualpic: all good')
})().catch(e => { console.error(e); process.exit(1) })
