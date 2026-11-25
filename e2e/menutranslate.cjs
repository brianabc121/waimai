// Menu translation in a real browser (docs/I18N_DESIGN.md §6): the platform
// switches on a translation service, the merchant's menu is translated in the
// background, the merchant checks and corrects it on the "languages" page, a
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
await own.waitForSelector(tid('open-langs'))
// start over from an earlier run: hand the Thai name back to the service
const reset = await own.request.put(BASE + '/api/v1/merchant/branches/2/translations', { headers: { 'X-Requested-With': 'waimai-web' }, data: { entity: 'item', entity_id: 11, field: 'name', lang: 'th', text: '' } })
expect(reset.ok(), 'resetting the Thai name: ' + reset.status())
// any save asks for a translation; this one changes nothing
const r = await own.request.put(BASE + '/api/v1/merchant/branches/2/menu-langs', { headers: { 'X-Requested-With': 'waimai-web' }, data: { menu_lang: 'zh', menu_langs_off: [] } })
expect(r.ok(), 'saving the menu language: ' + r.status())
await own.click(tid('open-langs'))
await own.waitForSelector(tid('tr-dialog'))
const cell = (id, field, lang) => own.locator(tid(`tr-cell-item-${id}-${field}-${lang}`))
await cell(11, 'name', 'th').filter({ hasText: '[th] 盐酥鸡' }).waitFor({ timeout: 60000 })
expect((await cell(11, 'name', 'zh-Hant').textContent()).includes('鹽酥雞'), 'Traditional Chinese is converted, not translated')
expect((await cell(11, 'name', 'en').textContent()).includes('Popcorn Chicken'), 'the English the merchant typed is kept')
await own.screenshot({ path: `${SHOTS}/tr-01-merchant-grid.png` })
step('the menu is translated in the background and listed by language on the merchant\'s page')

// the merchant corrects the Thai name
await cell(11, 'name', 'th').click()
await own.fill(tid('tr-text'), 'ไก่ป๊อป')
await own.click(tid('tr-save'))
await cell(11, 'name', 'th').filter({ hasText: 'ไก่ป๊อป' }).waitFor()
expect((await cell(11, 'name', 'th').textContent()).includes('商家已改'), 'a corrected translation is marked as the merchant\'s')
// the option groups are listed too
await own.click(tid('tr-kind-option'))
await own.locator(tid('tr-cell-group-') + ', [data-testid^=tr-cell-group-]').first().waitFor()
await own.click(tid('tr-close'))
// one dish at a time
await own.click(tid('item-more-11'))
await own.click(tid('item-langs-11'))
await own.waitForSelector(tid('tr-row-item-11-description'))
await own.screenshot({ path: `${SHOTS}/tr-02-merchant-dish.png` })
await own.click(tid('tr-close'))
step('the merchant corrects a translation, and sees one dish\'s languages from its row')

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

// store details: Traditional Chinese is filled in and waits for the owner to confirm
const H = { headers: { 'X-Requested-With': 'waimai-web' } }
await own.request.put(BASE + '/api/v1/merchant/branches/2/store-texts', { ...H, data: { field: 'name', lang: 'zh-Hant', text: '' } })
for (let i = 0; i < 40; i++) { // converted again in the background
  const v = await (await own.request.get(BASE + '/api/v1/merchant/branches/2/store-texts')).json()
  if (v.translations.some(x => x.field === 'name' && x.lang === 'zh-Hant')) break
  await own.waitForTimeout(500)
}
await own.goto(BASE + '/merchant/settings')
const card = own.locator(tid('store-text-name-zh-Hant'))
await card.waitFor()
await own.waitForFunction(() => document.querySelector('[data-testid=store-text-name-zh-Hant] textarea')?.value)
await card.locator(tid('store-text-confirm')).click()
await card.locator('.tag.ok').waitFor()
const st = await (await own.request.get(BASE + '/api/v1/merchant/branches/2/store-texts')).json()
expect(st.translations.some(x => x.field === 'name' && x.lang === 'zh-Hant' && x.status === 'human'), 'the confirmed name is the owner\'s')
expect(!st.translations.some(x => x.lang === 'th'), 'store details are not put into Thai')
await card.scrollIntoViewIfNeeded(); await own.screenshot({ path: `${SHOTS}/tr-05-store-texts.png` })
step('the owner confirms the Traditional Chinese store name in the settings')
const en = st.translations.find(x => x.field === 'name' && x.lang === 'en')
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
