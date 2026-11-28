// Item marketing in a real browser (商品营销): the owner sets a limit per
// order, a limit per day and second-one-half-price on items; the storefront
// shows them on the items; an order over the limit is refused in words.
// Usage: BASE_URL=http://localhost:8080 node e2e/itempromos.cjs   (needs seed data)
const { chromium } = require('playwright')
const { mkdirSync } = require('node:fs')

;(async () => {

const BASE = process.env.BASE_URL || 'http://localhost:8080'
const SHOTS = process.env.SHOTS_DIR || 'e2e/shots'
mkdirSync(SHOTS, { recursive: true })
const browser = await chromium.launch()
const errors = []
async function open(viewport) {
  const ctx = await browser.newContext({ viewport, locale: 'zh-CN', deviceScaleFactor: 2 })
  const page = await ctx.newPage()
  page.on('pageerror', e => errors.push(`pageerror: ${e.message}`))
  page.on('console', m => { if (m.type() === 'error' && !/favicon|Failed to load resource/.test(m.text())) errors.push(`console: ${m.text()}`) })
  return page
}
const step = (s) => console.log('✓ ' + s)
const shot = (page, name) => page.screenshot({ path: `${SHOTS}/${name}.png` })
const expect = (ok, what) => { if (!ok) throw new Error(what) }

const owner = await open({ width: 1440, height: 900 })
await owner.goto(BASE + '/login')
await owner.fill('input[autocomplete=username]', 'owner1')
await owner.fill('input[type=password]', 'demo1234')
await owner.click('button.primary')
await owner.waitForURL(u => !/\/login$/.test(u.pathname))

// the page is in the marketing group of the menu
await owner.goto(BASE + '/merchant/item-promos')
await owner.waitForSelector('[data-testid=nav-item-promos]')
await owner.waitForSelector('[data-testid=promo-new]')
expect(await owner.locator('[data-testid=coupon-tab]').count() === 0, 'the item page has no coupons tab')
step('商品营销 is in the menu')

const stamp = Date.now() % 100000
async function create(name, kind, value, items) {
  await owner.click('[data-testid=promo-new]')
  await owner.waitForSelector('[data-testid=promo-kind]')
  await owner.fill('.sheet input[required]', name)
  await owner.selectOption('[data-testid=promo-kind]', kind)
  if (kind.startsWith('dish_limit')) await owner.fill('[data-testid=limit-n]', String(value))
  await owner.waitForSelector('[data-testid=dish-pick] label.check')
  for (const it of items) await owner.locator('[data-testid=dish-pick] label.check', { hasText: it }).first().locator('input').check()
  if (name.includes('每单')) await shot(owner, 'ip-01-form')
  await owner.click('.sheet .btn.primary')
  await owner.waitForSelector('.sheet', { state: 'detached' })
  const row = owner.locator('tbody tr', { hasText: name })
  await row.locator('button.op.red', { hasText: '发布' }).click()
  await row.locator('.tag.ok').waitFor()
  return row
}
const perOrder = await create(`每单限购${stamp}`, 'dish_limit_order', 2, ['盐酥鸡'])
expect((await perOrder.innerText()).includes('每单限购 2 件'), 'the list says the limit per order')
await create(`每日限购${stamp}`, 'dish_limit_day', 3, ['卤肉饭'])
await create(`第二件半价${stamp}`, 'dish_second', 50, ['盐酥鸡'])
await shot(owner, 'ip-02-list')
step('a limit per order, a limit per day and second-one-half-price are published')

// 优惠 keeps the promotions on the whole order only
await owner.goto(BASE + '/merchant/promos')
await owner.waitForSelector('[data-testid=promo-new]')
expect(await owner.locator('tbody tr', { hasText: `每单限购${stamp}` }).count() === 0, 'item deals are not on the offers page')
step('优惠 does not list item deals')

// the storefront shows them on the items
const phone = await open({ width: 390, height: 844 })
await phone.goto(BASE + '/s/makati?lang=zh')
await phone.waitForSelector('[data-testid=dish-deal]')
const deals = await phone.locator('[data-testid=dish-deal]').allInnerTexts()
expect(deals.some(d => d.includes('第二件半价') && d.includes('每单限购 2 件')), 'the item shows its deal and limit: ' + deals.join(' | '))
expect(deals.some(d => d.includes('每人每天限购 3 件')), 'the item shows its limit per day: ' + deals.join(' | '))
await shot(phone, 'ip-03-store')
step('the storefront shows the limits on the items')

// over the limit: refused, in the reader's language
const store = await (await phone.request.get(BASE + '/api/v1/stores/makati')).json()
const chicken = store.categories.flatMap(c => c.items).find(i => i.name === '盐酥鸡')
expect(store.item_limits.some(l => l.item_id === chicken.id && l.per_order === 2), 'the store lists the limit')
const options = chicken.groups.filter(g => g.min_select > 0).flatMap(g => g.options.slice(0, g.min_select).map(o => o.id))
const msg = await phone.evaluate(async ({ id, options }) => {
  const r = await fetch('/api/v1/stores/makati/quotes', { method: 'POST', credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'waimai-web' },
    body: JSON.stringify({ service_type: 'pickup', items: [{ item_id: id, qty: 3, option_ids: options }] }) })
  return (await r.json()).error
}, { id: chicken.id, options })
expect(msg && msg.code === 'ITEM_LIMIT' && msg.details.limit === 2, 'three are refused: ' + JSON.stringify(msg))
step('three of an item limited to two are refused')

// tidy up, so that the other browser tests see the store as it was
await owner.goto(BASE + '/merchant/item-promos')
owner.on('dialog', d => d.accept())
for (const name of [`每单限购${stamp}`, `每日限购${stamp}`, `第二件半价${stamp}`]) {
  const row = owner.locator('tbody tr', { hasText: name })
  await row.locator('button.op.bad').click()
  await row.locator('.tag.bad').waitFor()
}
step('the deals are ended')

await browser.close()
if (errors.length) { console.error(errors.join('\n')); process.exit(1) }
console.log('item marketing: all passed')

})().catch(e => { console.error(e); process.exit(1) })
