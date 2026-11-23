// The customer's own pages ("me") in a real browser: the page itself, the
// bottom navigation, the profile, favourites, messages, feedback and its
// answer, an application to open a store, the wallet the platform keeps, and
// what the platform says about itself.
// Usage: BASE_URL=http://localhost:8080 node e2e/me.cjs   (needs seed data and OTP_DEV_ECHO=true)
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
}
const opts = { locale: 'zh-CN', ignoreHTTPSErrors: process.env.E2E_INSECURE_TLS === '1' }
const ctx = await browser.newContext({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 2, ...opts })
const page = await ctx.newPage()
watch(page)
const tid = id => `[data-testid=${id}]`
const step = s => console.log('✓ ' + s)
const shot = (name, p = page) => p.screenshot({ path: `${SHOTS}/${name}.png`, fullPage: true })
const path = () => new URL(page.url()).pathname
const expect = (ok, what) => { if (!ok) throw new Error(what) }
const local = '9' + String(Math.floor(Math.random() * 1e9)).padStart(9, '0')
const DOT = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==', 'base64')

// The platform admin works in a window of their own.
const adminCtx = await browser.newContext({ viewport: { width: 1280, height: 800 }, ...opts })
const admin = await adminCtx.newPage()
watch(admin)
await admin.goto(BASE + '/login')
await admin.fill('input[autocomplete=username]', 'admin')
await admin.fill('input[type=password]', 'demo1234')
await admin.click('button.primary')
await admin.waitForURL(u => !u.pathname.startsWith('/login'))
const adminTab = async name => { await admin.goto(BASE + '/admin/customers'); await admin.click(tid('ac-tab-' + name)) }

// ---- not logged in ----
await page.goto(BASE + '/me')
await page.waitForSelector('.mehome[data-ready]')
await page.waitForSelector(tid('me-login'))
expect(await page.locator(tid('customer-tabs')).isVisible(), 'the bottom navigation is missing')
await page.click(tid('me-help'))
await page.waitForURL(u => u.pathname === '/me/help')
expect(await page.locator(tid('help-q')).count() === 6, 'account help should answer six questions')
await page.click(tid('me-back'))
await page.waitForURL(u => u.pathname === '/me')
step('"me" opens without an account: a login prompt, the navigation, and the help pages')

await page.click(tid('me-messages'))
await page.waitForURL(u => u.pathname === '/account/login')
await page.fill(tid('login-phone'), local)
await page.check(tid('login-agree'))
await page.click(tid('login-send'))
await page.waitForURL(u => u.pathname === '/me/messages')
await page.waitForSelector(`${tid('msg-empty')}, ${tid('chats-empty')}`)   // the chats come first where chat is set up
step('a page that belongs to the account goes through login and comes back')

// ---- the page itself ----
await page.goto(BASE + '/me')
await page.waitForSelector(tid('me-name'))
expect(/^0\d{3} \*\*\* \d{4}$/.test(await page.textContent(tid('me-name'))), 'without a nickname the masked number stands in')
const memberNo = (await page.textContent(tid('me-member'))).match(/M-\d{8}/)[0]
expect((await page.textContent(tid('me-balance'))).trim().replace(/^\D+/, '').startsWith('0'), 'a new account holds nothing')
step(`the page shows who is logged in (${memberNo}) and an empty wallet`)

// ---- profile and settings ----
await page.click(tid('me-settings'))
await page.waitForSelector(tid('profile-nickname'))
await page.fill(tid('profile-nickname'), '爱吃面的小红')
await page.click(tid('profile-save'))
await page.waitForSelector(tid('profile-save') + ':disabled')
await page.setInputFiles(tid('profile-avatar') + ' input[type=file]', { name: 'me.png', mimeType: 'image/png', buffer: DOT })
await page.waitForSelector(tid('profile-avatar-img'))
await shot('me2-profile')
await page.click(tid('lang-en'))
await page.locator('text=Log out').waitFor()
await page.click(tid('lang-zh'))
await page.click(tid('me-back'))
await page.waitForSelector(tid('me-avatar'))
expect(await page.textContent(tid('me-name')) === '爱吃面的小红', 'the nickname should show on the page')
step('profile: a nickname, a picture and the language are saved')

// ---- favourites, and the navigation on the store page ----
await page.goto(BASE + '/s/makati')
await page.waitForSelector('.item')
expect(await page.locator(tid('customer-tabs')).isVisible(), 'the store page should carry the navigation on a phone')
const bar = await page.locator('.cartbar > div').boundingBox(), tabs = await page.locator(tid('customer-tabs') + ' > div').boundingBox()
expect(bar.y + bar.height <= tabs.y + 1, 'the cart bar must sit above the navigation')
await page.click(tid('favorite'))
await page.waitForSelector(tid('favorite') + '[aria-pressed=true]')
await shot('me3-store-tabs')
await page.click(tid('tab-me'))
await page.waitForSelector(tid('me-name'))
await page.click(tid('me-favorites'))
await page.waitForSelector(tid('fav-store'))
expect(await page.locator(tid('fav-store')).count() === 1, 'the kept store should be listed')
await shot('me4-favorites')
await page.click(tid('fav-store'))
await page.waitForURL(u => u.pathname === '/s/makati')
await page.waitForSelector(tid('favorite') + '[aria-pressed=true]')
// "home" is the marketplace
await page.click(tid('tab-home'))
await page.waitForURL(u => u.pathname === '/')
await page.waitForSelector(tid('mall'))
step('a store is kept with the star and listed under favourites; "home" is the marketplace')

// ---- feedback, answered by the platform ----
await page.goto(BASE + '/me/feedback')
await page.waitForSelector(tid('feedback-form'))
await page.click(tid('feedback-submit'))
await page.waitForSelector(tid('feedback-error'))
await page.click('[data-kind=problem]')
const note = '地图有时打不开 ' + local
await page.fill(tid('feedback-content'), note)
await page.click(tid('feedback-submit'))
await page.waitForSelector(tid('feedback-sent'))
await adminTab('feedback')
const fb = admin.locator(tid('ac-feedback'), { hasText: note })
await fb.locator(tid('ac-reply')).fill('已经修好了，谢谢反馈')
await fb.locator(tid('ac-reply-send')).click()
await fb.waitFor({ state: 'detached' })
await page.goto(BASE + '/me')
await page.waitForSelector(tid('me-unread'))
await page.waitForSelector(tid('tab-unread'))
await page.click(tid('me-messages'))
await page.waitForSelector(tid('msg') + '[data-kind=service]')
await shot('me5-messages')
await page.click(tid('msg') + '[data-kind=service]')
await page.waitForURL(u => u.pathname === '/me/feedback')
await page.locator(tid('feedback-reply'), { hasText: '已经修好了' }).waitFor()
await shot('me6-feedback')
step('feedback reaches the platform, and its reply comes back as a message with a red dot')

// ---- an application to open a store ----
await page.goto(BASE + '/me/apply')
await page.waitForSelector(tid('apply-form'))
const shop = '阿红小馆 ' + local.slice(-4)
await page.fill(tid('apply-store'), shop)
await page.fill(tid('apply-contact'), '阿红')
await page.fill(tid('apply-phone'), '0917 555 0000')
// where the store is: picked on the map like a delivery address
await page.click(tid('apply-submit'))
await page.locator(tid('apply-error'), { hasText: '地图' }).waitFor()
await page.click(tid('apply-place'))
await page.waitForSelector(tid('picker'))
await page.waitForSelector(tid('pin-confirm') + ':not([disabled])')
await page.screenshot({ path: `${SHOTS}/me7-apply-map.png` })
await page.click(tid('pin-confirm') + ':not([disabled])')
await page.waitForSelector(tid('picker'), { state: 'detached' })
await page.locator(tid('apply-region'), { hasText: 'Philippines' }).waitFor()
await page.fill(tid('apply-unit'), '2F 12号铺')
await shot('me7-apply-place')
await page.click(tid('apply-submit'))
await page.waitForSelector(tid('apply-pending'))
expect(!(await page.locator(tid('apply-form')).count()), 'one application at a time')
await adminTab('applications')
const ap = admin.locator(tid('ac-application'), { hasText: shop })
await ap.locator(tid('ac-p-country'), { hasText: 'Philippines (PH)' }).waitFor()
await ap.locator(tid('ac-p-province'), { hasText: 'Metro Manila' }).waitFor()
await ap.locator(tid('ac-p-address'), { hasText: '2F 12号铺' }).waitFor()
expect(/^-?\d+\.\d{6}, -?\d+\.\d{6}/.test(await ap.locator(tid('ac-p-point')).innerText()), 'the point is shown')
await shot('me-admin-application', admin)
await ap.locator(tid('ac-decide-note')).fill('欢迎，我们会电话联系你')
await ap.locator(tid('ac-approve')).click()
await ap.waitFor({ state: 'detached' })
await page.reload()
await page.waitForSelector(tid('application') + '[data-status=approved]')
await shot('me7-apply')
step('an application picks the store on the map; the platform sees its country, province, area, address and point, and decides it once')

// ---- the wallet is a ledger the platform keeps ----
await adminTab('customers')
await admin.fill(tid('ac-search'), memberNo)
await admin.press(tid('ac-search'), 'Enter')
await admin.waitForFunction(n => document.querySelectorAll('[data-testid=ac-customer]').length === 1 && document.body.innerText.includes(n), memberNo)
await admin.click(tid('ac-adjust'))
await admin.fill(tid('ac-adjust-amount'), '93')
await admin.fill(tid('ac-adjust-note'), '订单退款')
await admin.click(tid('ac-adjust-save'))
await admin.waitForSelector(tid('ac-adjust-form'), { state: 'detached' })
await admin.locator(tid('ac-balance'), { hasText: '93' }).waitFor()
await shot('me-admin-customers', admin)
await page.goto(BASE + '/me')
await page.locator(tid('me-balance') + ' b', { hasText: /^\D*93$/ }).waitFor()
await page.waitForSelector('.mehome[data-ready]')
await shot('me1-home')
await page.click(tid('me-balance'))
await page.waitForSelector(tid('wallet-entry'))
expect((await page.textContent(tid('wallet-entry'))).includes('订单退款'), 'the entry says what it was for')
expect((await page.textContent(tid('wallet-note'))).includes('还没有开通'), 'the page must say the balance cannot be spent yet')
await shot('me8-wallet')
await page.goto(BASE + '/me/wallet/bonus')
await page.waitForSelector(tid('wallet-empty'))
step('the platform records a balance; the customer sees it, its history, and that it cannot be spent yet')

// ---- offers ----
await page.goto(BASE + '/me/coupons')
await page.waitForSelector(`${tid('offer')}, ${tid('offers-empty')}`)
await shot('me9-offers')
step('the offers page lists what the customer\'s stores run right now')

// ---- support and about, as the platform fills them in ----
await adminTab('info')
await admin.waitForSelector(tid('ac-info-form'))
const before = {}
for (const k of ['support_phone', 'support_hours', 'about']) before[k] = await admin.inputValue(tid('ac-info-' + k))
await admin.fill(tid('ac-info-support_phone'), '+63 2 8555 0100')
await admin.fill(tid('ac-info-support_hours'), '每天 9:00–21:00')
await admin.fill(tid('ac-info-about'), '我们帮餐厅自己接外卖订单。')
await admin.click(tid('ac-info-save'))
await admin.locator(tid('ac-info-save'), { hasText: '已保存' }).waitFor()
await page.goto(BASE + '/me/support')
await page.locator(tid('support-phone'), { hasText: '8555 0100' }).waitFor()
await shot('me10-support')
await page.goto(BASE + '/me/about')
await page.locator(tid('about-text'), { hasText: '我们帮餐厅' }).waitFor()
// leave the platform's texts as they were
for (const k in before) await admin.fill(tid('ac-info-' + k), before[k])
await admin.click(tid('ac-info-save'))
await admin.waitForResponse(r => r.url().endsWith('/admin/platform-info') && r.request().method() === 'PUT')
step('support and "about us" show what the platform filled in')

// ---- addresses open from the page; reviews start empty ----
await page.goto(BASE + '/me')
await page.click(tid('me-addresses'))
await page.waitForURL(u => u.pathname === '/me/addresses')
await page.waitForSelector(tid('address-sheet'))
await page.click(tid('me-back'))
await page.click(tid('me-reviews'))
await page.waitForSelector(tid('reviews-empty'))
step('the address book opens from the page, and "my reviews" starts empty')

// ---- logging out ----
page.once('dialog', d => d.accept())
await page.goto(BASE + '/me/profile')
await page.click(tid('profile-logout'))
await page.waitForURL(u => u.pathname === '/me')
await page.waitForSelector(tid('me-login'))
await page.goto(BASE + '/me/wallet/balance')
await page.waitForURL(u => u.pathname === '/account/login')
step('logging out ends it: the wallet asks for a login again')

// ---- a wide screen ----
const wide = await browser.newContext({ viewport: { width: 1280, height: 800 }, ...opts })
const wp = await wide.newPage()
watch(wp)
await wp.goto(BASE + '/me')
await wp.waitForSelector('.mehome[data-ready]')
expect(await wp.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), 'no sideways scrolling on a computer')
await wp.goto(BASE + '/s/makati')
await wp.waitForSelector('.item')
expect(!(await wp.locator(tid('customer-tabs')).isVisible()), 'on a computer the store page keeps its own layout')
step('on a computer the page is a centred column, and the store page is unchanged')

if (errors.length) throw new Error('browser errors:\n' + errors.join('\n'))
await browser.close()
console.log('me: all steps passed')

})().catch(e => { console.error(e); process.exit(1) })
