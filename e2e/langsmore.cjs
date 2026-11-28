// The "Languages" button outside the catalog (docs/I18N_SIMPLE_DESIGN.md §4.8):
// a topping is translated once, on its template, for every item that uses it,
// and the merchant corrects it there; a brand has no "English name" box any
// more, its name has the same window as the store's.
// The test service ("stub") marks the text instead of translating it: "[th] 珍珠".
// Usage: BASE_URL=http://localhost:8080 node e2e/langsmore.cjs   (seed data)
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
  p.on('dialog', d => d.accept())
  return p
}
const tid = id => `[data-testid=${id}]`
const step = s => console.log('✓ ' + s)
const expect = (ok, what) => { if (!ok) throw new Error(what) }
const H = { headers: { 'X-Requested-With': 'waimai-web' } }
async function signIn(page, user) {
  await page.goto(BASE + (user === 'admin' ? '/admin/login' : '/login'))
  await page.fill('input[autocomplete=username]', user)
  await page.fill('input[type=password]', 'demo1234')
  await page.click('button.primary')
  await page.waitForURL(u => !/\/login$/.test(u.pathname))
}
const page = async () => watch(await (await browser.newContext({ viewport: { width: 1280, height: 860 }, locale: 'zh-CN', ignoreHTTPSErrors: process.env.E2E_INSECURE_TLS === '1' })).newPage())

const adm = await page()
await signIn(adm, 'admin')
const setProvider = async v => {
  const r = await adm.request.put(BASE + '/api/v1/admin/settings', { ...H, data: { values: { TRANSLATE_PROVIDER: v } } })
  expect(r.ok(), 'saving the translation service: ' + r.status())
}
await setProvider('stub')

const own = await page()
await signIn(own, 'owner2')
const API = BASE + '/api/v1/merchant/branches/2'
const call = async (method, path, data) => {
  const r = await own.request.fetch(API + path, { method, ...H, data })
  expect(r.ok(), `${method} ${path}: ${r.status()} ${await r.text()}`)
  return r.json().catch(() => ({}))
}
const NAME = '多语言测试加料'
const drop = async () => {
  for (const x of (await call('GET', '/topping-templates')).templates ?? []) if (x.name === NAME) await call('DELETE', '/topping-templates/' + x.id)
}
try {
await drop()
// ---- a topping template on an item ----
await call('POST', '/topping-templates', { name: NAME, title: '加料', max_select: 0, options: [{ id: 0, code: 'LG1', name: '珍珠', name_en: '', price: 500, sold_out: false }] })
const tpl = (await call('GET', '/topping-templates')).templates.find(x => x.name === NAME)
await call('PUT', `/topping-templates/${tpl.id}/items`, { item_ids: [11] })
const opt = tpl.options[0].id
const view = async scope => call('GET', '/translations?scope=' + scope)
let v
for (let i = 0; i < 120; i++) {
  v = await view('topping_template/' + tpl.id)
  if (v.translations.some(x => x.entity === 'topping_option' && x.lang === 'th')) break
  await own.waitForTimeout(500)
}
expect(v.fields.length === 2, 'the template\'s window holds its title and its topping: ' + JSON.stringify(v.fields))
const item = await view('item/11')
expect(!item.fields.some(f => f.text === '珍珠'), 'the item\'s copy of the topping is not translated again')
step('a topping is translated on its template, not on every item')

await own.goto(BASE + '/merchant/menu?tab=toppings')
await own.click(tid(`tp-item-${NAME}`))
await own.locator(`[data-testid=langs-btn][data-scope="topping_template/${tpl.id}"]`).click()
await own.waitForSelector(tid('langs-sheet'))
await own.click(tid('langs-tab-th'))
const box = own.locator(`${tid(`langs-row-topping_option-${opt}-name`)} ${tid('langs-text')}`)
expect(await box.inputValue() === '[th] 珍珠', 'the Thai topping: ' + await box.inputValue())
await box.fill('ไข่มุก')
await own.click(tid('langs-save'))
await own.locator(`${tid(`langs-row-topping_option-${opt}-name`)} ${tid('langs-mine')}`).waitFor()
await own.screenshot({ path: `${SHOTS}/lg-01-topping-window.png` })
await own.click(tid('langs-close'))
step('the merchant corrects the topping in the template\'s window')

const sf = await (await own.request.get(BASE + '/api/v1/stores/bgc?lang=th')).json()
const dish = sf.categories.flatMap(c => c.items).find(i => i.id === 11)
const group = dish.groups.find(g => g.topping_template_id === tpl.id)
expect(group && group.name_tr === '[th] 加料' && group.options[0].name_tr === 'ไข่มุก', 'the Thai customer\'s topping: ' + JSON.stringify(group))
step('a Thai customer reads the corrected topping on the item')

// ---- a brand ----
await own.goto(BASE + '/merchant/brands')
await own.waitForSelector(tid('brand-card'))
await own.locator('[data-testid^=brand-edit-]').first().click()
await own.waitForSelector(tid('brand-form'))
expect(!(await own.locator(tid('brand-f-name-en')).count()), 'the brand form has no English name box')
await own.locator(`${tid('brand-form')} ${tid('langs-btn')}`).click()
await own.waitForSelector(tid('langs-sheet'))
await own.click(tid('langs-tab-th'))
await own.locator('[data-testid^=langs-row-][data-testid$=-name]').first().waitFor()
await own.screenshot({ path: `${SHOTS}/lg-02-brand-window.png` })
await own.click(tid('langs-close'))
step('the brand form has one field for the name and the same Languages window')
} finally {
  await drop().catch(() => {})
  await setProvider('')
}

if (errors.length) throw new Error(errors.join('\n'))
await browser.close()
console.log('langsmore: all good')
})().catch(e => { console.error(e); process.exit(1) })
