// Platform support in a real browser, against a real OpenIM server: a customer
// and a store write to the platform, an administrator answers both from the
// shared inbox, each side sees the answer and "read", and the administrator
// marks the question resolved.
// Usage: BASE_URL=http://localhost:8080 node e2e/support.cjs
// (needs seed data, OTP_DEV_ECHO=true and the API started with OPENIM_API_URL;
// without chat set up the test says so and passes)
const { chromium } = require('playwright')
const { mkdirSync } = require('node:fs')

;(async () => {

const BASE = process.env.BASE_URL || 'http://localhost:8080'
const SHOTS = process.env.SHOTS_DIR || 'e2e/shots'
mkdirSync(SHOTS, { recursive: true })
const conf = await (await fetch(BASE + '/api/v1/config')).json()
if (!conf.chat) { console.log('chat is not set up on this server (OPENIM_API_URL): skipped'); return }

const browser = await chromium.launch()
const errors = []
const watch = p => {
  p.on('pageerror', e => errors.push(`pageerror: ${e.stack || e.message}`))
  p.on('console', m => { if (m.type() === 'error' && !/favicon|Failed to load resource/.test(m.text())) errors.push(`console: ${m.text()}`) })
}
const opts = { locale: 'zh-CN', ignoreHTTPSErrors: process.env.E2E_INSECURE_TLS === '1' }
const tid = id => `[data-testid=${id}]`
const step = s => console.log('✓ ' + s)
const expect = (ok, what) => { if (!ok) throw new Error(what) }
const local = '9' + String(Math.floor(Math.random() * 1e9)).padStart(9, '0')
const tail = local.slice(-4)
const staffLogin = async (p, user) => {
  await p.goto(BASE + '/login')
  await p.fill('input[autocomplete=username]', user)
  await p.fill('input[type=password]', 'demo1234')
  await p.click('button.primary')
  await p.waitForURL(u => !u.pathname.startsWith('/login'))
}
const pc = { viewport: { width: 1280, height: 800 }, deviceScaleFactor: 2, ...opts }
const phoneSize = { viewport: { width: 390, height: 844 }, deviceScaleFactor: 2, ...opts }

// ---- a customer writes from "my support" ----
const page = await (await browser.newContext(phoneSize)).newPage()
watch(page)
await page.goto(BASE + '/me/support/chat')
await page.waitForURL(u => u.pathname === '/account/login')
await page.fill(tid('login-phone'), local)
await page.check(tid('login-agree'))
await page.click(tid('login-send'))
await page.waitForURL(u => u.pathname === '/me/support/chat')
await page.goto(BASE + '/me/support')
await page.click(tid('support-online'))
await page.waitForURL(u => u.pathname === '/me/support/chat')
await page.waitForSelector(tid('chat-pane') + '[data-state=on]')
await page.waitForSelector(tid('chat-empty'))
const question = '我的退款三天了还没到账，订单尾号 ' + tail
await page.fill(tid('chat-input'), question)
await page.click(tid('chat-send'))
await page.waitForSelector(`${tid('chat-msg')}[data-mine=true][data-state=sent]`)
step('a customer opens "chat with support" from my support and sends a question')

// ---- a store writes too ----
const desk = await (await browser.newContext(pc)).newPage()
watch(desk)
await staffLogin(desk, 'owner1')
await desk.goto(BASE + '/merchant/support')
await desk.waitForSelector(tid('store-support'))
await desk.waitForSelector(tid('chat-pane') + '[data-state=on]')
const storeQ = '我们想改一下店铺的营业执照信息，要怎么提交？' + tail
await desk.fill(tid('chat-input'), storeQ)
await desk.press(tid('chat-input'), 'Enter')
await desk.waitForSelector(`${tid('chat-msg')}[data-mine=true][data-state=sent]`)
step('a store writes to support from its back office')

// ---- the platform answers both ----
const admin = await (await browser.newContext(pc)).newPage()
watch(admin)
await staffLogin(admin, 'admin')
await admin.goto(BASE + '/admin/support')
await admin.waitForSelector(tid('admin-support'))
await admin.waitForSelector(tid('support-badge'))
const crow = admin.locator(tid('as-row'), { hasText: question })
await crow.waitFor()
expect(await crow.locator(tid('as-row-unread')).count() === 1, 'the question is unread in the inbox')
await admin.locator(tid('as-row'), { hasText: storeQ }).waitFor()
await crow.click()
await admin.waitForSelector(`${tid('chat-msg')}[data-mine=false]:has-text("${question}")`)
await crow.locator(tid('as-row-unread')).waitFor({ state: 'detached' })
await page.waitForSelector(tid('chat-read'))
step('the platform sees both in its inbox; opening the customer\'s reads it, and the customer sees "read"')

const answer = '您好，退款已经重新提交，1 到 3 个工作日到账。'
await admin.fill(tid('chat-input'), answer)
await admin.press(tid('chat-input'), 'Enter')
await admin.waitForSelector(`${tid('chat-msg')}[data-mine=true][data-state=sent]`)
await page.waitForSelector(`${tid('chat-msg')}[data-mine=false]:has-text("${answer}")`)
await admin.waitForSelector(tid('chat-read'))
step('the platform answers; the customer gets it at once and the platform sees "read"')

await admin.locator(tid('as-row'), { hasText: storeQ }).click()
await admin.waitForSelector(`${tid('chat-msg')}[data-mine=false]:has-text("${storeQ}")`)
const storeA = '请在商户资料里上传新的执照照片，我们一个工作日内审核。'
await admin.fill(tid('chat-input'), storeA)
await admin.press(tid('chat-input'), 'Enter')
await desk.waitForSelector(`${tid('chat-msg')}[data-mine=false]:has-text("${storeA}")`)
await admin.screenshot({ path: `${SHOTS}/support-admin.png` })
await desk.screenshot({ path: `${SHOTS}/support-store.png` })
await page.screenshot({ path: `${SHOTS}/support-customer.png` })
step('the platform answers the store, and the store gets it')

// ---- resolved, and open again with the next question ----
await admin.locator(tid('as-row'), { hasText: answer }).click()
await admin.click(tid('as-resolve'))
await admin.waitForSelector(`${tid('as-row')}[data-party] .tag.ok`)
await admin.click(tid('as-open'))
await admin.fill(tid('as-search'), tail)
await admin.waitForFunction(sel => document.querySelectorAll(sel).length === 1, tid('as-row'))   // only the store's is still open
await page.fill(tid('chat-input'), '收到了，谢谢！')
await page.click(tid('chat-send'))
await admin.waitForFunction(sel => document.querySelectorAll(sel).length === 2, tid('as-row'))
step('the platform marks a question resolved; the next message opens it again')

// ---- the customer's message list keeps support on top ----
await page.goto(BASE + '/me/messages')
await page.waitForSelector(tid('chat-row-support'))
await page.screenshot({ path: `${SHOTS}/support-me-messages.png` })
const adminPhone = await (await browser.newContext(phoneSize)).newPage()
watch(adminPhone)
await staffLogin(adminPhone, 'admin')
await adminPhone.goto(BASE + '/admin/support')
await adminPhone.waitForSelector(tid('as-row'))
await adminPhone.screenshot({ path: `${SHOTS}/support-admin-phone.png` })
step('the customer\'s messages show support on top; the inbox works on a phone')

expect(!errors.length, 'browser errors:\n' + errors.join('\n'))
await browser.close()
console.log('support: all passed')

})().catch(e => { console.error(e); process.exit(1) })
