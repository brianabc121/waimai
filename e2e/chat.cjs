// Chat between a customer and a store in a real browser, against a real OpenIM
// server: the customer writes from the store's chat page, the store's desk
// sees it and answers, both sides see "read" and their unread numbers, and the
// platform reads the conversation and stops the customer from writing.
// Usage: BASE_URL=http://localhost:8080 node e2e/chat.cjs
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
const staffLogin = async (p, user) => {
  await p.goto(BASE + '/login')
  await p.fill('input[autocomplete=username]', user)
  await p.fill('input[type=password]', 'demo1234')
  await p.click('button.primary')
  await p.waitForURL(u => !u.pathname.startsWith('/login'))
}

// ---- the customer writes ----
const ctx = await browser.newContext({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 2, ...opts })
const page = await ctx.newPage()
watch(page)
await page.goto(BASE + '/chat/makati')
await page.waitForURL(u => u.pathname === '/account/login')
await page.fill(tid('login-phone'), local)
await page.check(tid('login-agree'))
await page.click(tid('login-send'))
await page.waitForURL(u => u.pathname === '/chat/makati')
await page.waitForSelector(tid('chat-pane') + '[data-state=on]')
await page.waitForSelector(tid('chat-empty'))
const hello = '你好，可以少放辣吗 ' + local.slice(-4)
await page.fill(tid('chat-input'), hello)
await page.click(tid('chat-send'))
await page.waitForSelector(`${tid('chat-msg')}[data-mine=true][data-state=sent]`)
step('a customer logs in, opens the chat with a store and sends a message')

// ---- the store's desk answers ----
const deskCtx = await browser.newContext({ viewport: { width: 1280, height: 800 }, deviceScaleFactor: 2, ...opts })
const desk = await deskCtx.newPage()
watch(desk)
await staffLogin(desk, 'owner1')
await desk.goto(BASE + '/merchant/messages')
await desk.waitForSelector(tid('messages-page'))
const row = desk.locator(tid('chat-row'), { hasText: hello })
await row.waitFor()
expect(await row.locator(tid('chat-row-unread')).count() === 1, 'the new conversation should count as unread at the desk')
expect(await desk.locator('.navnum').first().isVisible(), 'the navigation should show the unread number')
await row.click()
await desk.waitForSelector(`${tid('chat-msg')}[data-mine=false]:has-text("${hello}")`)
await row.locator(tid('chat-row-unread')).waitFor({ state: 'detached' })
step('the desk lists the conversation as unread, and opening it reads it')

await page.waitForSelector(tid('chat-read'))
step('the customer sees "read" under the message')

const answer = '可以的，给您做微辣'
await desk.fill(tid('chat-input'), answer)
await desk.press(tid('chat-input'), 'Enter')
await desk.waitForSelector(`${tid('chat-msg')}[data-mine=true][data-state=sent]`)
await page.waitForSelector(`${tid('chat-msg')}[data-mine=false]:has-text("${answer}")`)
await desk.waitForSelector(tid('chat-read'))
await desk.screenshot({ path: `${SHOTS}/chat-desk.png` })
await page.screenshot({ path: `${SHOTS}/chat-customer.png` })
step('the desk answers with Enter; the customer gets it at once and the desk sees "read"')

// ---- a second device of the store stays on line and gets the message too ----
const phoneCtx = await browser.newContext({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 2, ...opts })
const phone = await phoneCtx.newPage()
watch(phone)
await staffLogin(phone, 'cashier1')
await phone.goto(BASE + '/merchant/messages')
await phone.locator(tid('chat-row'), { hasText: local.slice(-4) }).click()
await phone.waitForSelector(`${tid('chat-msg')}:has-text("${hello}")`)
const more = '大概多久能送到？'
await page.fill(tid('chat-input'), more)
await page.click(tid('chat-send'))
await phone.waitForSelector(`${tid('chat-msg')}[data-mine=false]:has-text("${more}")`)
await desk.waitForSelector(`${tid('chat-msg')}[data-mine=false]:has-text("${more}")`)
expect(await desk.locator(tid('chat-down')).count() === 0, 'the first device was signed out by the second')
await phone.screenshot({ path: `${SHOTS}/chat-desk-phone.png` })
step('two members of staff are on line at once and both get the next message')

// ---- a picture, quick replies, an order card ----
const DOT = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==', 'base64')
await page.setInputFiles(tid('chat-file'), { name: 'dish.png', mimeType: 'image/png', buffer: DOT })
await page.waitForSelector(`${tid('chat-msg')}[data-mine=true][data-state=sent] ${tid('chat-image')}`)
await desk.waitForSelector(`${tid('chat-msg')}[data-mine=false] ${tid('chat-image')} img`)
expect(await desk.locator(`${tid('chat-image')} img`).evaluate(img => img.complete && img.naturalWidth > 0), 'the desk should see the picture itself')
step('the customer sends a picture and the desk sees it')

await desk.click(tid('chat-quick-manage'))
await desk.waitForSelector(tid('chat-setup'))
while (await desk.locator(tid('chat-quick-del')).count()) {   // what an earlier run left
  const n = await desk.locator(tid('chat-quick-del')).count()
  await desk.locator(tid('chat-quick-del')).first().click()
  await desk.waitForFunction(([sel, n]) => document.querySelectorAll(sel).length < n, [tid('chat-quick-del'), n])
}
await desk.click(tid('chat-quick-defaults'))
await desk.waitForFunction(sel => document.querySelectorAll(sel).length === 4, tid('chat-quick-del'))
await desk.fill(tid('chat-quick-new'), '稍等，我去后厨确认一下')
await desk.click(tid('chat-quick-add'))
await desk.waitForFunction(sel => document.querySelectorAll(sel).length === 5, tid('chat-quick-del'))
await desk.click(`${tid('chat-setup')} .sheet-head button`)
await desk.click(`${tid('chat-quick-item')}:has-text("后厨确认")`)
expect(await desk.inputValue(tid('chat-input')) === '稍等，我去后厨确认一下', 'a quick reply fills the box')
await desk.press(tid('chat-input'), 'Enter')
await page.waitForSelector(`${tid('chat-msg')}[data-mine=false]:has-text("后厨确认")`)
step('the desk keeps quick replies and answers with one')

// the customer orders (through the API, the order pages have tests of their own) and sends the order
const orderNo = await page.evaluate(async () => {
  const call = (path, body) => fetch('/api/v1' + path, { method: 'POST', credentials: 'same-origin',
    headers: { 'X-Requested-With': 'e2e', 'Content-Type': 'application/json', 'Idempotency-Key': 'chat-' + Date.now() }, body: JSON.stringify(body) }).then(r => r.json())
  const store = await fetch('/api/v1/stores/makati').then(r => r.json())
  const item = store.categories.flatMap(c => c.items).find(i => !i.sold_out && i.stock_qty == null && !(i.groups || []).length)
  const q = await call('/stores/makati/quotes', { service_type: 'pickup', items: [{ item_id: item.id, qty: 1 }] })
  const o = await call('/stores/makati/orders', { quote_id: q.id, customer_name: '李小红' })
  return o.public_no || (o.error && JSON.stringify(o.error)) || JSON.stringify(q)
})
expect(/^[A-Z0-9-]+$/i.test(orderNo), 'placing an order for the card: ' + orderNo)
await page.reload()
await page.waitForSelector(`${tid('chat-msg')}:has-text("后厨确认")`)
await page.click(tid('chat-order-btn'))
await page.click(`${tid('chat-order-pick')}:has-text("${orderNo}")`)
await page.waitForSelector(`${tid('chat-msg')}[data-mine=true][data-state=sent] ${tid('chat-order-card')}:has-text("${orderNo}")`)
await desk.waitForSelector(`${tid('chat-msg')}[data-mine=false] ${tid('chat-order-card')}:has-text("${orderNo}")`)
await desk.screenshot({ path: `${SHOTS}/chat-desk-cards.png` })
await page.screenshot({ path: `${SHOTS}/chat-customer-cards.png` })
await page.click(`${tid('chat-order-card')}:has-text("${orderNo}")`)
await page.waitForURL(u => u.pathname.startsWith('/o/'))
await page.goBack()
await page.waitForSelector(tid('chat-pane') + '[data-state=on]')
step('the customer sends an order as a card; it opens the order')

// from the order desk: "message the customer", and the line that says a customer wrote
await desk.goto(BASE + '/merchant/orders')
await desk.click(`.order-card:has-text("${orderNo}"), [data-testid=order-row]:has-text("${orderNo}"), :text("${orderNo}")`)
await desk.click(tid('order-chat'))
await desk.waitForURL(u => u.pathname === '/merchant/messages' && u.searchParams.get('c'))
await desk.waitForSelector(`${tid('chat-order-card')}:has-text("${orderNo}")`)
// the page first counts what is unread already; only what comes after that is "new"
const counted = desk.waitForResponse(r => r.url().includes('/chats?') && r.url().includes('unread=1'))
await desk.goto(BASE + '/merchant/orders')
await desk.waitForSelector(`:text("${orderNo}")`)
await counted
await page.fill(tid('chat-input'), '我到店门口了')
await page.click(tid('chat-send'))
await desk.waitForSelector(tid('chat-toast'))
await desk.click(`${tid('chat-toast')} a`)
await desk.waitForURL(u => u.pathname === '/merchant/messages')
await desk.locator(tid('chat-row'), { hasText: local.slice(-4) }).click()
await desk.waitForSelector(`${tid('chat-msg')}:has-text("我到店门口了")`)
step('the order desk leads to the customer\'s chat, and is told when a customer writes')

// ---- the customer's list of conversations ----
await page.goto(BASE + '/me/messages')
await page.waitForSelector(tid('chats'))
expect(await page.locator(tid('chat-row')).count() === 1, 'the customer should have one conversation')
expect((await page.locator(tid('chat-row')).innerText()).includes('我到店门口了'), 'the list should show the last message')
await page.screenshot({ path: `${SHOTS}/chat-me-messages.png` })
step('"my messages" lists the conversation with its last message')

// ---- the platform reads it, and stops the customer ----
const adminCtx = await browser.newContext({ viewport: { width: 1280, height: 800 }, deviceScaleFactor: 2, ...opts })
const admin = await adminCtx.newPage()
watch(admin)
await staffLogin(admin, 'admin')
await admin.goto(BASE + '/admin/chats')
await admin.waitForSelector(tid('admin-chats'))
await admin.fill(tid('ax-search'), local.slice(-4))
await admin.waitForFunction(sel => document.querySelectorAll(sel).length === 1, tid('ax-row'))
await admin.click(tid('ax-row'))
await admin.waitForSelector(`${tid('ax-msg')}:has-text("${hello}")`)
expect(await admin.locator(tid('ax-msg')).count() === 7, 'the platform should see all seven messages')
expect(await admin.locator(tid('ax-image')).count() === 1 && (await admin.locator(tid('ax-order')).innerText()).includes(orderNo), 'the platform should see the picture and the order')
expect((await admin.locator(tid('ax-messages')).innerText()).includes('owner1'), 'the answer should name the member of staff')
expect(await admin.locator(tid('chat-input')).count() === 0, 'the platform must not be able to write')
await admin.screenshot({ path: `${SHOTS}/chat-admin.png` })
step('the platform finds the conversation and reads it, without a box to write in')

admin.on('dialog', d => d.accept())
await page.goto(BASE + '/chat/makati')
await page.waitForSelector(`${tid('chat-msg')}:has-text("${answer}")`)
await admin.click(tid('ax-mute'))
await admin.waitForSelector(tid('ax-mute') + '[data-on]')
// the page learns of it within seconds and puts the box away; until then the chat server refuses
const refused = await page.fill(tid('chat-input'), '还在吗', { timeout: 3000 }).then(() => page.click(tid('chat-send'), { timeout: 3000 })).then(() => true, () => false)
if (refused) {
  await page.waitForSelector(tid('chat-failed'))
  expect((await page.locator(tid('chat-failed')).innerText()).includes('不能发消息'), 'the refusal should say why')
}
await page.waitForSelector(tid('chat-blocked'))
await admin.reload()
await admin.waitForSelector(`${tid('ax-msg')}:has-text("${hello}")`)
expect(await admin.locator(tid('ax-msg')).count() === 7, 'a refused message must not be kept')
step('a customer the platform has muted is refused by the chat server, and then can read but not write')
await admin.click(tid('ax-mute'))
await admin.waitForSelector(tid('ax-mute') + ':not([data-on])')

// ---- reminders off for one store, reports, a store's block ----
page.on('dialog', d => d.accept())
desk.on('dialog', d => d.accept())
await page.reload()
await page.waitForSelector(tid('chat-pane') + '[data-state=on]')
await page.waitForSelector(tid('chat-input'))
await page.click(tid('chat-more'))
await page.click(tid('chat-quiet'))
await page.waitForSelector(tid('chat-quiet-note'))
await page.click(tid('chat-more'))
await page.click(tid('chat-quiet'))
await page.waitForSelector(tid('chat-quiet-note'), { state: 'detached' })
await page.click(tid('chat-more'))
await page.screenshot({ path: `${SHOTS}/chat-customer-menu.png` })
await page.click(tid('chat-report-open'))
await page.check(tid('chat-report-abuse'))
await page.fill(tid('chat-report-detail'), '说话不客气')
await page.screenshot({ path: `${SHOTS}/chat-report.png` })
await page.click(tid('chat-report-send'))
await page.waitForSelector(tid('chat-note'))
step('the customer turns reminders about the store off and on, and reports the conversation')

await desk.click(tid('chat-block'))
await desk.waitForSelector(tid('chat-blocked-note'))
await page.fill(tid('chat-input'), '为什么不回我')
await page.click(tid('chat-send'))
await page.waitForSelector(tid('chat-failed'))
expect((await page.locator(tid('chat-failed')).innerText()).includes('请打电话'), 'a blocked customer is told to call')
await desk.click(tid('chat-report-open'))
await desk.check(tid('chat-report-spam'))
await desk.click(tid('chat-report-send'))
await desk.waitForSelector(tid('chat-note'))
await desk.screenshot({ path: `${SHOTS}/chat-desk-blocked.png` })
await desk.click(tid('chat-block'))
await desk.waitForSelector(tid('chat-blocked-note'), { state: 'detached' })
step('the desk blocks the customer, who is then refused, reports them and lifts the block')

await admin.goto(BASE + '/admin/chats')
await admin.click(tid('ax-reported'))
await admin.fill(tid('ax-search'), local.slice(-4))
await admin.waitForFunction(sel => document.querySelectorAll(sel).length === 1, tid('ax-row'))
expect((await admin.locator(tid('ax-row-reports')).innerText()).includes('2'), 'the platform sees both reports on the conversation')
await admin.click(tid('ax-row'))
await admin.waitForSelector(tid('ax-report'))
expect(await admin.locator(tid('ax-report')).count() === 2, 'both reports are listed')
await admin.screenshot({ path: `${SHOTS}/chat-admin-reports.png` })
await admin.click(tid('ax-resolve'))
await admin.waitForSelector(tid('ax-resolve'), { state: 'detached' })
await admin.waitForSelector(tid('ax-empty'))
step('the platform filters reported conversations, reads the reports and marks them dealt with')

await admin.goto(BASE + '/admin')
await admin.waitForSelector('[data-health=chat][data-state=ok]')
step('the platform\'s front page shows the chat server as healthy')

await page.click(tid('chat-more'))
await page.click(tid('chat-hide'))
await page.waitForURL(u => u.pathname === '/me/messages')
await page.waitForSelector(tid('chats-empty'))
step('the customer removes the conversation from their own list')

expect(!errors.length, 'browser errors:\n' + errors.join('\n'))
await browser.close()
console.log('chat: all passed')

})().catch(e => { console.error(e); process.exit(1) })
