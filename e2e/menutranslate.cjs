// Menu translation in a real browser (docs/I18N_DESIGN.md §6): the platform
// switches on a translation service, the merchant's menu is translated in the
// background, the merchant corrects one item in the window behind its "Languages" button, a
// Thai customer sees the Thai menu with "show original", and the order keeps
// the dish's name in Thai while the kitchen sees the original.
// The test service ("stub") marks the text instead of translating it, so no
// key is needed: "[th] 盐酥鸡".
// Usage: BASE_URL=http://localhost:8080 node e2e/menutranslate.cjs   (seed data, OTP_DEV_ECHO=true)
const { chromium } = require('playwright')
const { mkdirSync } = require('node:fs')

;(async () => {

const BASE = process.env.BASE_URL || 'http://localhost:8080'
const SHOTS = process.env.SHOTS_DIR || 'e2e/shots'
mkdirSync(SHOTS, { recursive: true })
const insecure = process.env.E2E_INSECURE_TLS === '1'
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
async function signIn(page, user) {
  await page.goto(BASE + (/^admin/.test(user) ? '/admin/login' : /^((kitchen|rider|zhang|wang)\d|e2e\d+$)/.test(user) ? '/login?next=/work' : '/login'))
  await page.fill('input[autocomplete=username]', user)
  await page.fill('input[type=password]', 'demo1234')
  await page.click('button.primary')
  await page.waitForURL(u => !/\/login$/.test(u.pathname))
}

// ---- the platform turns on the (test) translation service ----
const adm = watch(await (await browser.newContext({ viewport: { width: 1280, height: 860 }, locale: 'zh-CN', ignoreHTTPSErrors: insecure })).newPage())
await signIn(adm, 'admin')
await adm.goto(BASE + '/admin/settings')
await adm.waitForSelector('[data-group=translate]')
const setProvider = async v => {
  const r = await adm.request.put(BASE + '/api/v1/admin/settings', { headers: { 'X-Requested-With': 'waimai-web' }, data: { values: { TRANSLATE_PROVIDER: v } } })
  expect(r.ok(), 'saving the translation service: ' + r.status())
}
await setProvider('stub')
await adm.reload(); await adm.waitForSelector('[data-group=translate] [data-testid=state]')
expect((await adm.textContent('[data-group=translate] [data-testid=state]')).includes('测试'), 'the settings page shows the service in use')
step('the platform sets up a translation service on the keys page')

try {
// ---- the merchant: the menu is translated after a change ----
const own = watch(await (await browser.newContext({ viewport: { width: 1280, height: 860 }, locale: 'zh-CN', ignoreHTTPSErrors: insecure })).newPage())
await signIn(own, 'owner2')
await own.goto(BASE + '/merchant/menu')
const btn = scope => own.locator(`[data-testid=langs-btn][data-scope="${scope}"]`).first()
await btn('item/11').waitFor()
const API = BASE + '/api/v1/merchant/branches/2'
const H = { headers: { 'X-Requested-With': 'waimai-web' } }
const view = async scope => (await own.request.get(`${API}/translations?scope=${scope}`)).json()
const tr = (v, entity, field, lang) => v.translations.find(x => x.entity === entity && x.field === field && x.lang === lang)
// start over from an earlier run: hand the Thai name back to the service
const reset = await own.request.put(API + '/translations', { ...H, data: { entity: 'item', entity_id: 11, field: 'name', lang: 'th', text: '' } })
expect(reset.ok(), 'resetting the Thai name: ' + reset.status())
// any save asks for a translation; this one changes nothing
const r = await own.request.put(API + '/menu-langs', { ...H, data: { menu_lang: 'zh', menu_langs_off: [] } })
expect(r.ok(), 'saving the menu language: ' + r.status())
for (let i = 0; i < 120; i++) {
  if (tr(await view('item/11'), 'item', 'name', 'th')?.text === '[th] 盐酥鸡') break
  await own.waitForTimeout(500)
}
// nothing of the other languages shows until the button is clicked
expect(!(await own.locator(tid('langs-sheet')).count()), 'no translations on the page itself')
await btn('item/11').click()
await own.waitForSelector(tid('langs-sheet'))
const text = field => own.locator(`${tid('langs-row-item-11-' + field)} ${tid('langs-text')}`)
await own.click(tid('langs-tab-th'))
await own.waitForFunction(() => document.querySelector('[data-testid=langs-row-item-11-name] [data-testid=langs-text]')?.value === '[th] 盐酥鸡', null, { timeout: 60000 })
await own.click(tid('langs-tab-zh-Hant'))
expect((await text('name').inputValue()).includes('鹽酥雞'), 'Traditional Chinese is converted, not translated')
await own.click(tid('langs-tab-en'))
expect((await text('name').inputValue()).includes('Popcorn Chicken'), 'the English the merchant typed is kept')
expect(await own.locator(`${tid('langs-row-item-11-name')} ${tid('langs-mine')}`).count(), 'and marked as the merchant\'s')
await own.waitForSelector(tid('langs-row-item-11-description'))
await own.screenshot({ path: `${SHOTS}/tr-01-merchant-window.png` })
step('the item is translated in the background; its languages show only in the window behind the button')

// the merchant corrects the Thai name
await own.click(tid('langs-tab-th'))
await text('name').fill('ไก่ป๊อป')
await own.click(tid('langs-save'))
await own.locator(`${tid('langs-row-item-11-name')} ${tid('langs-mine')}`).waitFor()
expect(tr(await view('item/11'), 'item', 'name', 'th')?.status === 'human', 'a corrected translation is the merchant\'s')
await own.screenshot({ path: `${SHOTS}/tr-02-merchant-corrected.png` })
await own.click(tid('langs-close'))
expect(!(await own.locator(tid('langs-failed')).count()), 'no red dot while the service works')
step('the merchant corrects one translation in the window')

// the whole table is behind the settings card, and finds a text by its translation
await own.goto(BASE + '/merchant/settings')
await own.waitForSelector(tid('langs-card'))
await own.locator(tid('langs-status')).waitFor()
await own.locator(tid('langs-card')).scrollIntoViewIfNeeded()
await own.screenshot({ path: `${SHOTS}/tr-04-settings-card.png` })
await own.click(tid('langs-all'))
await own.waitForSelector(tid('all-langs'))
await own.fill(tid('all-langs-q'), 'ไก่ป๊อป')
await own.locator(tid('all-langs-row-item-11-name')).waitFor()
expect(await own.locator('[data-testid^=all-langs-row-]').count() === 1, 'the search by a translation finds that one item')
await own.screenshot({ path: `${SHOTS}/tr-05-all-translations.png` })
await own.click(tid('all-langs-close'))
step('the settings card shows the state, and "all translations" finds an item by its Thai name')

// ---- a Thai customer ----
const th = watch(await (await browser.newContext({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 2, locale: 'th-TH', ignoreHTTPSErrors: insecure, geolocation: { latitude: 14.5547, longitude: 121.0244 }, permissions: ['geolocation'] })).newPage())
await th.goto(BASE + '/s/bgc')
await th.waitForSelector('.item')
await th.locator('text=ไก่ป๊อป').first().waitFor()
await th.locator('text=[th] 无盐地瓜条').first().waitFor()
await th.waitForSelector(tid('mt-note'))
await th.screenshot({ path: `${SHOTS}/tr-03-customer-thai.png` })
await th.click(tid('mt-toggle'))
await th.locator('text=盐酥鸡').first().waitFor()
expect(!(await th.locator('text=[th] 无盐地瓜条').count()), '"show original" shows the merchant\'s own menu')
await th.click(tid('mt-toggle'))
await th.locator('text=ไก่ป๊อป').first().waitFor()
step('a Thai customer reads the Thai menu, is told it is machine translated, and can see the original')

// a language the merchant turned off reads English
const off = await own.request.put(BASE + '/api/v1/merchant/branches/2/menu-langs', { headers: { 'X-Requested-With': 'waimai-web' }, data: { menu_lang: 'zh', menu_langs_off: ['th'] } })
expect(off.ok(), 'turning Thai off')
await th.reload(); await th.waitForSelector('.item')
await th.locator('text=Popcorn Chicken').first().waitFor()
await own.request.put(BASE + '/api/v1/merchant/branches/2/menu-langs', { headers: { 'X-Requested-With': 'waimai-web' }, data: { menu_lang: 'zh', menu_langs_off: [] } })
step('with Thai turned off, Thai customers get the English menu')

// the mall finds the store by the Thai name
await th.goto(BASE + '/')
await th.waitForSelector(tid('mall-search'))
await th.fill(tid('mall-search'), 'ไก่ป๊อป'); await th.keyboard.press('Enter')
await th.locator('text=ไก่ป๊อป').first().waitFor()
step('the mall search finds the dish by its Thai name')

// the mall's own categories follow once the platform has a translation service
let cat = null
for (let i = 0; i < 90 && !cat; i++) {
  const h = await (await th.request.get(BASE + '/api/v1/mall/home?lang=th')).json()
  cat = h.categories.find(c => (c.name_tr || '').startsWith('[th] '))
  if (!cat) await th.waitForTimeout(1000)
}
expect(cat, 'the mall categories were not translated into Thai')
await th.goto(BASE + '/'); await th.waitForSelector(tid('mall-search'))
await th.locator(`text=${cat.name_tr}`).filter({ visible: true }).first().waitFor()
step('the mall categories are shown in Thai')

// store details: the name goes into English and Traditional Chinese by itself,
// the other languages read English until the owner writes one
let st = null
for (let i = 0; i < 60; i++) {
  st = await view('branch/2')
  if (tr(st, 'branch', 'name', 'zh-Hant')) break
  await own.waitForTimeout(500)
}
expect(tr(st, 'branch', 'name', 'zh-Hant'), 'the store name is put into Traditional Chinese')
expect(!tr(st, 'branch', 'name', 'th'), 'the store name is not put into Thai')
await own.goto(BASE + '/merchant/settings')
await btn('branch/2').click()
await own.waitForSelector(tid('langs-sheet'))
await own.click(tid('langs-tab-th'))
const nameTh = own.locator(`${tid('langs-row-branch-2-name')} ${tid('langs-text')}`)
expect(await nameTh.inputValue() === '', 'the Thai name is left for the owner')
await own.screenshot({ path: `${SHOTS}/tr-06-store-window.png` })
await own.click(tid('langs-close'))
step('the store name has its own window; Thai is empty until the owner writes it')
const en = tr(st, 'branch', 'name', 'en')
if (en) {
  await th.goto(BASE + '/s/bgc'); await th.waitForSelector('.item')
  await th.locator(`text=${en.text}`).first().waitFor()
  step('Thai customers see the store name in English')
}
} finally {
  await setProvider('')
}

if (errors.length) throw new Error(errors.join('\n'))
await browser.close()
console.log('menutranslate: all good')
})().catch(e => { console.error(e); process.exit(1) })
