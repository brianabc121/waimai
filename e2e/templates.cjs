// Storefront templates in a real browser: the owner edits the delivery
// channel's layout, a draft stays invisible to customers, publishing changes
// only that channel, and ordering works the same under the new layout.
// Usage: BASE_URL=http://localhost:8080 node e2e/templates.cjs   (needs seed data)
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
const shot = (page, name) => page.screenshot({ path: `${SHOTS}/${name}.png`, fullPage: true })
const layoutOf = async (page) => { await page.waitForSelector('.menu'); return page.locator('.menu').getAttribute('data-layout') }
const expect = (got, want, what) => { if (got !== want) throw new Error(`${what}: expected ${want}, got ${got}`) }

const owner = await open({ width: 1280, height: 900 })
await owner.goto(BASE + '/login')
await owner.fill('input[autocomplete=username]', 'owner1')
await owner.fill('input[type=password]', 'demo1234')
await owner.click('button.primary')
await owner.waitForURL(u => !u.pathname.startsWith('/login'))
await owner.goto(BASE + '/merchant/templates')
await owner.waitForSelector('[data-test=preview] .menu')

// the three seeded looks, as previews
for (const ch of ['delivery', 'qr', 'ipad']) {
  await owner.click(`[data-channel=${ch}]`)
  await owner.waitForTimeout(150)
  await shot(owner, `t0-preview-${ch}`)
}
await owner.click('[data-channel=delivery]')

const cust = await open({ width: 390, height: 844 })
await cust.goto(BASE + '/s/makati')
const before = await layoutOf(cust)
const target = before === 'grid' ? 'list' : 'grid'

// draft: preview changes, customers see nothing new
await owner.check(`input[name=layout][value=${target}]`)
expect(await owner.locator('[data-test=preview] .menu').getAttribute('data-layout'), target, 'preview layout')
await owner.click('[data-test=save-draft]')
await owner.waitForSelector('[data-test=has-draft]')
await cust.reload()
expect(await layoutOf(cust), before, 'storefront while a draft is saved')
step(`a saved draft (${target}) shows in the preview only`)

// publish: delivery changes, the dine-in channels do not
const qrBefore = await cust.evaluate(() => fetch('/api/v1/stores/makati').then(r => r.json()).then(s => JSON.stringify([s.templates.qr, s.templates.ipad])))
await owner.click('[data-test=publish-tpl]')
await owner.waitForSelector('[data-test=tpl-notice]')
await cust.reload()
expect(await layoutOf(cust), target, 'storefront after publishing')
const qrAfter = await cust.evaluate(() => fetch('/api/v1/stores/makati').then(r => r.json()).then(s => JSON.stringify([s.templates.qr, s.templates.ipad])))
expect(qrAfter, qrBefore, 'dine-in templates after publishing delivery')
await shot(cust, 't1-storefront-new-layout')
step('publishing delivery changed the storefront and left the table channels alone')

// the same menu and cart under the new layout
await cust.selectOption('select', { index: 1 })
await cust.click('.item:has-text("冰柠檬茶") .stepper button.fill')
await cust.click('.item:has-text("盐酥鸡") button')
await cust.click('.sheet label.opt:has-text("中辣")')
await cust.click('.sheet-foot .btn.primary')
await cust.click('.bottombar .btn.primary')
await customerLogin(cust)
await cust.locator('text=盐酥鸡').first().waitFor()
step('cart and checkout work the same under the new layout')

await browser.close()
if (errors.length) { console.error(errors.join('\n')); process.exit(1) }
console.log('TEMPLATES E2E OK')
})().catch(e => { console.error(e); process.exit(1) })
