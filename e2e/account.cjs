// Customer accounts in a real browser: ordering needs a login, the login is a
// texted code and nothing else, and logging out really ends it.
// Usage: BASE_URL=http://localhost:8080 node e2e/account.cjs   (needs seed data and OTP_DEV_ECHO=true)
const { chromium } = require('playwright')
const { mkdirSync } = require('node:fs')

;(async () => {

const BASE = process.env.BASE_URL || 'http://localhost:8080'
const SHOTS = process.env.SHOTS_DIR || 'e2e/shots'
mkdirSync(SHOTS, { recursive: true })
const browser = await chromium.launch()
const errors = []
const ctx = await browser.newContext({ viewport: { width: 390, height: 844 }, locale: 'zh-CN', ignoreHTTPSErrors: process.env.E2E_INSECURE_TLS === '1' })
const page = await ctx.newPage()
page.on('pageerror', e => errors.push(`pageerror: ${e.message}`))
page.on('console', m => { if (m.type() === 'error' && !/favicon|Failed to load resource/.test(m.text())) errors.push(`console: ${m.text()}`) })
const tid = id => `[data-testid=${id}]`
const step = s => console.log('✓ ' + s)
const shot = name => page.screenshot({ path: `${SHOTS}/${name}.png`, fullPage: true })
const path = () => new URL(page.url()).pathname
const local = '9' + String(Math.floor(Math.random() * 1e9)).padStart(9, '0')
const pretty = `+63 ${local.slice(0, 3)} ${local.slice(3, 6)} ${local.slice(6)}`

// Browsing and filling the cart need no account.
await page.goto(BASE + '/s/makati')
await page.waitForSelector('.item')
await page.selectOption('select', { index: 1 })
await page.click('.item:has-text("瓶装水") .stepper button.fill')
await page.click('.bottombar .btn.primary')
await page.waitForURL(u => u.pathname === '/account/login')
await page.waitForSelector(tid('login-phone'))
if (await page.locator('text=密码').count()) throw new Error('the login page must not offer a password')
await page.locator('.sname:has-text("马卡蒂")').waitFor()
await shot('ac1-login')
step('checkout without an account goes to the login page, which shows the store and has no password')

// Opening the checkout address directly does the same.
await page.goto(BASE + '/s/makati/checkout')
await page.waitForURL(u => u.pathname === '/account/login')
await page.waitForSelector(tid('login-phone'))

// A wrong number and a missing tick are caught before anything is sent.
await page.fill(tid('login-phone'), '12345')
await page.click(tid('login-send'))
await page.locator(`${tid('login-error')}:has-text("+63")`).waitFor()
await page.fill(tid('login-phone'), '0' + local)       // a leading zero is fine
await page.click(tid('login-send'))
await page.locator(`${tid('login-error')}:has-text("同意")`).waitFor()
if (!(await page.locator(tid('login-submit')).isDisabled())) throw new Error('login must wait for the code')
step('a wrong number and an unticked agreement are refused')

// A wrong code is refused; the right one logs in and returns to checkout with the cart.
await page.route('**/api/v1/otp/request', async route => {
  const res = await route.fetch()
  const body = await res.json()
  page.__code = body.dev_code
  delete body.dev_code                                  // as in production: the code only arrives by text
  await route.fulfill({ response: res, json: body })
})
await page.check(tid('login-agree'))
await page.click(tid('login-send'))
await page.locator(`${tid('login-sent')}:has-text("${pretty}")`).waitFor()
if (!/秒后可重发/.test(await page.locator(tid('login-send')).innerText())) throw new Error('the resend countdown is missing')
await shot('ac2-code-sent')
const wrong = page.__code === '000000' ? '000001' : '000000'
await page.fill(tid('login-code'), wrong)
await page.locator(`${tid('login-error')}:has-text("验证码")`).waitFor()
await page.fill(tid('login-code'), page.__code)         // six digits log in by themselves
await page.waitForURL(u => u.pathname.endsWith('/checkout'))
await page.waitForSelector('.totals')
await page.locator(`${tid('account-phone')}:has-text("${pretty}")`).waitFor()
await page.locator('text=瓶装水').first().waitFor()
step('a wrong code is refused; the right one logs in and comes back to checkout with the cart')

// The account button shows who is logged in; logging out ends it.
await page.goto(BASE + '/s/makati')
await page.waitForSelector('.item')
await page.click(tid('account'))
await page.locator(`${tid('account-menu')}:has-text("${pretty}")`).waitFor()
await shot('ac3-account-menu')
await page.click(tid('logout'))
await page.waitForSelector(tid('account-menu'), { state: 'detached' })
await page.goto(BASE + '/s/makati/checkout')
await page.waitForURL(u => u.pathname === '/account/login')
step('the account button shows the phone, and logging out sends checkout back to the login page')

await browser.close()
if (errors.length) { console.error(errors.join('\n')); process.exit(1) }
console.log('account e2e passed')

})().catch(e => { console.error(e); process.exit(1) })
