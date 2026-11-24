// Languages in a real browser (docs/I18N_DESIGN.md §5, §9): the page follows
// the browser's language the first time, the language button changes it and a
// cookie remembers it, a link's ?lang= wins, Arabic turns the page right to
// left, the platform back office stays in Chinese or English, and a signed-in
// customer's account remembers the choice for a new browser.
// Usage: BASE_URL=http://localhost:8080 node e2e/i18n.cjs   (needs seed data, OTP_DEV_ECHO=true)
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
  return p
}
const insecure = process.env.E2E_INSECURE_TLS === '1'
const HERE = { geolocation: { latitude: 14.5547, longitude: 121.0244 }, permissions: ['geolocation'] }
const tid = id => `[data-testid=${id}]`
const step = s => console.log('✓ ' + s)
const expect = (ok, what) => { if (!ok) throw new Error(what) }
const phone = (locale, extra = {}) => browser.newContext({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 2, locale, ignoreHTTPSErrors: insecure, ...HERE, ...extra })
const html = p => p.evaluate(() => ({ lang: document.documentElement.lang, dir: document.documentElement.dir, data: document.documentElement.dataset.lang }))
const cookie = async ctx => (await ctx.cookies()).find(c => c.name === 'lang')?.value
const ph = p => p.getAttribute(tid('mall-search'), 'placeholder')
const pick = async (p, code) => { await p.locator(tid('lang-btn')).filter({ visible: true }).first().click(); await p.click(tid('lang-' + code)); await p.waitForFunction(c => document.documentElement.dataset.lang === c, code) }

// ---- the browser's language, the first time ----
const tw = await phone('zh-TW')
const p1 = watch(await tw.newPage())
await p1.goto(BASE + '/')
await p1.waitForSelector(tid('lang-btn'))
expect((await html(p1)).lang === 'zh-Hant', 'a Taiwanese browser should get Traditional Chinese')
expect(await ph(p1) === '搜尋店鋪或菜品', 'the page should be in Traditional Chinese: ' + await ph(p1))
expect(await cookie(tw) === undefined, 'following the browser sets no cookie')
step('a zh-TW browser opens in Traditional Chinese')

const th = await phone('th-TH')
const p2 = watch(await th.newPage())
await p2.goto(BASE + '/')
await p2.waitForSelector(tid('lang-btn'))
expect((await html(p2)).data === 'th', 'a Thai browser should get Thai')
expect(/[\u0E00-\u0E7F]/.test(await ph(p2)), 'the search box should be in Thai: ' + await ph(p2))
await p2.screenshot({ path: `${SHOTS}/i18n-01-mall-thai.png` })
step('a th-TH browser opens in Thai')

const de = await phone('de-DE')
const p3 = watch(await de.newPage())
await p3.goto(BASE + '/')
await p3.waitForSelector(tid('lang-btn'))
expect((await html(p3)).data === 'en' && await ph(p3) === 'Search stores or dishes', 'a language the site does not speak falls back to English')
step('an unsupported browser language falls back to English')

// ---- the button, the cookie ----
await pick(p3, 'ja')
expect(await cookie(de) === 'ja', 'choosing writes the cookie')
await p3.reload(); await p3.waitForSelector(tid('lang-btn'))
expect((await html(p3)).data === 'ja', 'the choice survives a reload')
expect(/[\u3040-\u30FF]/.test(await ph(p3)), 'the search box should be in Japanese: ' + await ph(p3))
await p3.click(tid('lang-btn'))
await p3.waitForSelector(tid('lang-sheet'))
await p3.screenshot({ path: `${SHOTS}/i18n-02-picker.png` })
await p3.click(tid('lang-auto'))
await p3.waitForFunction(() => document.documentElement.dataset.lang === 'en')
expect(await cookie(de) === undefined, '"same as browser" removes the cookie')
step('the language button switches, remembers and forgets')

// ---- right to left ----
await pick(p3, 'ar')
expect((await html(p3)).dir === 'rtl', 'Arabic turns the page right to left')
await p3.goto(BASE + '/s/makati'); await p3.waitForSelector('.item')
expect((await html(p3)).dir === 'rtl', 'still right to left on a store page')
await p3.screenshot({ path: `${SHOTS}/i18n-03-store-arabic.png` })
step('Arabic is right to left')

// ---- a link's ?lang= wins and is remembered ----
await p3.goto(BASE + '/s/makati?lang=my'); await p3.waitForSelector('.item')
expect((await html(p3)).data === 'my' && (await html(p3)).dir === 'ltr', '?lang=my wins over the cookie')
expect(await cookie(de) === 'my', '?lang= is remembered')
await p3.screenshot({ path: `${SHOTS}/i18n-04-store-burmese.png` })
step('?lang= in a link wins and is remembered')

// ---- the back office stays Chinese or English ----
const adm = watch(await (await browser.newContext({ viewport: { width: 1280, height: 800 }, locale: 'th-TH', ignoreHTTPSErrors: insecure })).newPage())
await adm.goto(BASE + '/admin/login')
await adm.fill('input[autocomplete=username]', 'admin'); await adm.fill('input[type=password]', 'demo1234')
await adm.click('button.primary')
await adm.waitForURL(/\/admin/)
await adm.waitForSelector('.adm')
expect((await html(adm)).lang === 'en' && (await html(adm)).dir === 'ltr', 'the back office shows English to a Thai browser')
expect(!(await adm.locator('.adm').evaluate(b => /[\u0E00-\u0E7F]/.test(b.innerText))), 'no Thai in the back office')
step('the platform back office stays in English for a Thai browser')

// ---- a signed-in customer's account remembers the language ----
const a = watch(await (await phone('en-US')).newPage())
const num = '9' + String(Math.floor(Math.random() * 1e9)).padStart(9, '0')
await a.goto(BASE + '/account/login')
await a.fill(tid('login-phone'), num); await a.check(tid('login-agree')); await a.click(tid('login-send'))
await a.waitForURL(u => u.pathname !== '/account/login')
await a.goto(BASE + '/me/profile'); await a.locator(tid('lang-btn')).filter({ visible: true }).first().waitFor()
await pick(a, 'vi')
await a.waitForTimeout(300)
// A second browser signed in to the same account (the session cookie, but no
// language cookie): signing in again would wait out the one-code-a-minute limit.
const bctx = await phone('en-US')
await bctx.addCookies((await a.context().cookies()).filter(c => c.name !== 'lang'))
const b = watch(await bctx.newPage())
await b.goto(BASE + '/me')
await b.waitForFunction(() => document.documentElement.dataset.lang === 'vi')
step('a new browser opens in the language the account chose')

if (errors.length) throw new Error(errors.join('\n'))
await browser.close()
console.log('i18n: all good')
})().catch(e => { console.error(e); process.exit(1) })
