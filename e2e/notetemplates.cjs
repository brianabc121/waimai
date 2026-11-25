// Flavour note templates (docs/NOTE_TEMPLATE_DESIGN.md): the store edits its
// templates on the menu page and applies them to dishes; the customer picks
// tags on the dish, sees them in the cart, at checkout and on the order; the
// kitchen sees them on the ticket. The test template is deleted at the end.
// Usage: BASE_URL=http://localhost:8080 node e2e/notetemplates.cjs   (needs seed data)
const { chromium } = require('playwright')
const { mkdirSync } = require('node:fs')
const { customerLogin } = require('./lib.cjs')

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
const tid = (id) => `[data-testid=${id}]`
async function login(page, user, next = '/login') {
  await page.goto(BASE + next)
  await page.fill('input[autocomplete=username]', user)
  await page.fill('input[type=password]', 'demo1234')
  await page.click('button.primary')
  await page.waitForURL(u => !/\/login$/.test(u.pathname))
}

// ---- the store's template page ----
const owner = await open({ width: 1366, height: 900 })
await login(owner, 'owner1')
await owner.goto(BASE + '/merchant/menu?tab=notes')
await owner.waitForSelector(tid('nt-item-奶茶类'))
await owner.click(tid('nt-item-奶茶类'))
await owner.waitForSelector(tid('nt-editor'))
await shot(owner, 'nt-01-templates')
step('the store sees its templates and how many dishes use each')

// a new template with two groups, applied to the bottled water
await owner.click(`.nt-foot ${tid('nt-new')}`)
await owner.fill(tid('nt-name'), 'E2E测试')
const g0 = owner.locator(tid('nt-group')).first()
await g0.locator(tid('nt-group-name')).fill('冷热')
for (const x of ['常温', '冰的']) { await g0.locator(tid('nt-tag-input')).fill(x); await g0.locator(tid('nt-tag-input')).press('Enter') }
await owner.click(tid('nt-add-group'))
const g1 = owner.locator(tid('nt-group')).nth(1)
await g1.locator(tid('nt-multi')).click()
for (const x of ['不要吸管', '多给纸巾']) { await g1.locator(tid('nt-tag-input')).fill(x); await g1.locator(tid('nt-tag-input')).press('Enter') }
await owner.locator('.nt-preview ' + tid('note-tag')).first().waitFor()
await shot(owner, 'nt-02-new-template')
await owner.click(tid('nt-save'))
await owner.waitForSelector(tid('nt-item-E2E测试'))
await owner.click(tid('nt-apply'))
await owner.waitForSelector(tid('nt-apply-sheet'))
await owner.check(tid("nt-dish-瓶装水"))
await shot(owner, 'nt-03-apply')
await owner.click(tid('nt-apply-save'))
await owner.waitForSelector(tid('nt-apply-sheet'), { state: 'detached' })
await owner.locator(`${tid('nt-item-E2E测试')}:has-text("1")`).waitFor()
step('a new template is saved and applied to a dish')

// the dish list and the dish form show it
await owner.goto(BASE + '/merchant/menu')
await owner.locator(`tr:has-text("瓶装水") ${tid('item-notes-tag')}:has-text("E2E测试")`).waitFor()
await owner.locator('tr', { has: owner.locator('b', { hasText: /^瓶装水$/ }) }).locator('button', { hasText: '编辑' }).click()
await owner.waitForSelector(tid('item-notes'))
if (!(await owner.isChecked(tid("item-note-E2E测试")))) throw new Error('the dish form should tick the template')
await owner.locator(tid('item-notes')).scrollIntoViewIfNeeded()
await shot(owner, 'nt-04-item-form')
await owner.click(`${tid('item-form')} .sheet-foot .btn:not(.primary)`)
step('the dish list and the dish form show the template')

// ---- the customer ----
const cust = await open({ width: 390, height: 844 })
await cust.goto(BASE + '/s/makati')
await cust.waitForSelector('.item')
await cust.click(`.item:has-text("珍珠奶茶") ${tid('choose')}`)
await cust.waitForSelector(tid('sheet-notes'))
const tag = (name) => cust.locator(`${tid('sheet-notes')} ${tid('note-tag')}`, { hasText: new RegExp(`^${name}$`) }).first()
await tag('少糖').click()
await tag('半糖').click()   // pick one: replaces 少糖
await tag('去冰').click()
if (await tag('少糖').getAttribute('aria-pressed') !== 'false') throw new Error('a pick-one group keeps one tag')
await cust.fill(tid('sheet-note'), '珍珠多一点')
await shot(cust, 'nt-05-sheet')
await cust.click('.sheet-foot .btn.primary')
await cust.click('.item:has-text("瓶装水") ' + tid('choose'))
await cust.waitForSelector(tid('sheet-notes'))
await cust.click('.sheet-foot .btn.primary')   // all optional: nothing picked is fine
await cust.click('.bottombar .btn.primary')
await customerLogin(cust, null, { pickup: true })
await cust.fill('input[autocomplete=name]', '陈小姐')
// the flavour notes and the written note are on rows of their own
await cust.locator(`${tid('co-notes')}:has-text("半糖 · 去冰")`).waitFor()
await cust.locator(`${tid('co-memo')}:has-text("珍珠多一点")`).waitFor()
await shot(cust, 'nt-06-checkout')
await cust.click('.bottombar .btn.primary')
await cust.waitForURL('**/o/**')
await cust.locator(`${tid('od-notes')}:has-text("半糖")`).waitFor()
const orderNo = (await cust.locator(tid('order-no')).innerText()).trim()
await shot(cust, 'nt-07-order')
step(`the customer picked tags and placed ${orderNo}`)

// ---- the store sees them on the order ----
await owner.goto(BASE + '/merchant/orders')
await owner.click(`button.ord:has-text("${orderNo}")`)
const row = owner.locator(`.detail ${tid('line-notes')}`).first()
await row.waitFor()
const text = await row.innerText()
if (!/半糖/.test(text) || !/去冰/.test(text) || !/珍珠多一点/.test(text)) throw new Error('store order line: ' + text)
await shot(owner, 'nt-08-store-order')
step('the store sees the tags and the note on the order')

// ---- the template in other languages ----
await owner.goto(BASE + '/merchant/menu?tab=notes')
await owner.click(tid('nt-item-奶茶类'))
await owner.click(tid('nt-langs'))
await owner.waitForSelector(tid('tr-dialog'))
await owner.locator(`${tid('tr-dialog')} :text("少糖")`).first().waitFor()
await shot(owner, 'nt-09-template-langs')
await owner.click(tid('tr-close'))
step('the template opens its own translations')

// ---- an order taken by staff: the note cell offers the tags ----
async function staffNote(page, name) {
  await page.goto(BASE + '/merchant/orders/new')
  await page.waitForSelector(tid('mo-pick'))
  await page.fill(tid('mo-pick'), 'd2')
  await page.press(tid('mo-pick'), 'Enter')
  const cell = page.locator(tid('mo-line')).filter({ hasText: '珍珠奶茶' }).locator(tid('mo-note-in'))
  await cell.click()
  await page.waitForSelector(tid('mo-note-pop'))
  // on a phone the panel covers the row and has its own box
  const box = (await page.locator(tid('mo-note-pop-in')).isVisible()) ? page.locator(tid('mo-note-pop-in')) : cell
  await box.fill('少')
  await box.press('Enter')   // the first match: 少糖
  await page.locator(`${tid('mo-note-pop')} ${tid('note-tag')}`, { hasText: /^去冰$/ }).click()
  await box.fill('加珍珠')
  await shot(page, name)
  await box.press('Enter')   // no tag matches: kept as the note
  await page.waitForSelector(tid('mo-note-pop'), { state: 'detached' })
  const chips = await page.locator(`${tid('mo-line')} ${tid('mo-note')} .nchip`).allInnerTexts()
  if (chips.map(x => x.replace('✕', '').trim()).join(',') !== '少糖,去冰' || await cell.inputValue() !== '加珍珠') throw new Error('note cell: ' + chips + ' / ' + await cell.inputValue())
}
await staffNote(owner, 'nt-10-staff-order')
const ownerPhone = await open({ width: 390, height: 844 })
await login(ownerPhone, 'owner1')
await staffNote(ownerPhone, 'nt-11-staff-order-phone')
step('staff pick tags in the note cell by typing, on a computer and a phone')

// ---- the platform edits the store's templates ----
const admin = await open({ width: 1366, height: 900 })
await login(admin, 'admin', '/admin/login')
const branches = (await (await admin.request.get(BASE + '/api/v1/admin/branches')).json()).branches
const makati = branches.find(b => b.slug === 'makati')
await admin.goto(BASE + '/admin/items?branch_id=' + makati.id)
await admin.waitForSelector(tid('editing-banner'))
await admin.click(tid('open-notes'))
await admin.waitForSelector(`${tid('notes-drawer')} ${tid('nt-item-奶茶类')}`)
await shot(admin, 'nt-12-admin')
step('the platform opens the store\'s templates')

// ---- clean up ----
await owner.goto(BASE + '/merchant/menu?tab=notes')
await owner.click(tid('nt-item-E2E测试'))
await owner.click(tid('nt-delete'))
await owner.waitForSelector(tid('nt-item-E2E测试'), { state: 'detached' })
step('the test template is deleted')

if (errors.length) throw new Error('browser errors:\n' + errors.join('\n'))
await browser.close()
console.log('note templates: OK')

})().catch(e => { console.error(e); process.exit(1) })
