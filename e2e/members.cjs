// The member pages in a real browser: a store's own members (list, search,
// temporary numbers, details with order history, editing) and the
// platform's list of every store's members, narrowed to one store; on a
// PC, an iPad and a phone.
// Usage: BASE_URL=http://localhost:8080 node e2e/members.cjs   (needs seed data)
const { chromium } = require('playwright')
const { mkdirSync } = require('node:fs')

;(async () => {

const BASE = process.env.BASE_URL || 'http://localhost:8080'
const SHOTS = process.env.SHOTS_DIR || 'e2e/shots'
mkdirSync(SHOTS, { recursive: true })
const browser = await chromium.launch()
const errors = []
const step = (s) => console.log('✓ ' + s)
const expect = (got, want, what) => { if (got !== want) throw new Error(`${what}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`) }
const tid = (id) => `[data-testid=${id}]`
const uniq = String(Date.now()).slice(-7)

async function signIn(user, viewport) {
  const ctx = await browser.newContext({ viewport, deviceScaleFactor: Number(process.env.DPR || 1), locale: 'zh-CN',
    ignoreHTTPSErrors: process.env.E2E_INSECURE_TLS === '1' })
  const page = await ctx.newPage()
  page.on('pageerror', e => errors.push(`pageerror: ${e.message}`))
  page.on('console', m => { if (m.type() === 'error' && !/favicon|Failed to load resource/.test(m.text())) errors.push(`console: ${m.text()}`) })
  await page.goto(BASE + (/^admin/.test(user) ? '/admin/login' : /^((kitchen|rider|zhang|wang)\d|e2e\d+$)/.test(user) ? '/login?next=/work' : '/login'))
  await page.fill('input[autocomplete=username]', user)
  await page.fill('input[type=password]', 'demo1234')
  await page.click('button.primary')
  await page.waitForURL(u => !/\/login$/.test(u.pathname))
  return page
}
const noSideScroll = async (page, what) =>
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1), true, `${what} fits without scrolling sideways`)

// ---- the store ----
const page = await signIn('owner1', { width: 1440, height: 900 })
// a few members to look at: one filled in with an address, one temporary number
const call = (method, path, body) => page.evaluate(async ([method, path, body]) => {
  const r = await fetch('/api/v1' + path, { method, headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'fetch' }, body: body ? JSON.stringify(body) : undefined, credentials: 'include' })
  return r.json()
}, [method, path, body])
const bid = String((await call('GET', '/auth/me')).roles[0].branch_id)
const lina = await call('PUT', `/merchant/branches/${bid}/members/0`, { phone: `+63917${uniq}`, source: 'line', name: `Lina ${uniq}`,
  contact: 'lina.k', address_line: '12 Rizal Ave, Makati', address_unit: '5F', lat: 14.5547, lng: 121.0244, note: '不吃香菜' })
if (!lina.no) throw new Error('could not make a member: ' + JSON.stringify(lina))
const temp = await call('POST', `/merchant/branches/${bid}/members`)

await page.goto(BASE + '/merchant/orders')
await page.click(tid('nav-cm'))   // 客户管理 starts folded
await page.click(tid('nav-members'))
await page.waitForURL(u => u.pathname === '/merchant/members')
await page.waitForSelector(tid('member-row'))
expect(await page.locator(`[data-member="${temp.no}"] .tag.warn`).count(), 1, 'the temporary number is marked')
step('the store lists its members, the temporary number marked')
await page.screenshot({ path: `${SHOTS}/members-01-store-pc.png` })

await page.click(tid('members-temp'))
await page.waitForFunction(() => [...document.querySelectorAll('[data-testid=member-row]')].every(r => r.classList.contains('temp')))
step('only temporary numbers')
await page.click('.chips button >> nth=0')

await page.fill(tid('members-search'), uniq)
await page.press(tid('members-search'), 'Enter')
await page.waitForFunction(n => document.querySelectorAll('[data-testid=member-row]').length === 1 && !!document.querySelector(`[data-member="${n}"]`), lina.no)
step('found by phone')

await page.click(`[data-member="${lina.no}"] [data-testid=member-view]`)
await page.waitForSelector(tid('member-stats'))
const detail = await page.locator(tid('member-detail')).innerText()
for (const want of ['12 Rizal Ave, Makati', '不吃香菜', 'lina.k', '在谷歌地图查看']) if (!detail.includes(want)) throw new Error('detail lacks ' + want)
step('details show the address, its pin, the note and the counts')

await page.click(tid('member-detail-edit'))
await page.fill(tid('member-name'), `Lina K ${uniq}`)
await page.fill(tid('member-address'), '88 Ayala Ave, Makati')
await page.waitForSelector(tid('member-addr-changed'))
await page.fill(tid('member-phone'), '')
await page.click(tid('member-save'))
expect((await page.locator(tid('member-form-error')).innerText()).trim(), '请填电话', 'a phone is required')
await page.fill(tid('member-phone'), `+63917${uniq}`)
await page.click(tid('member-save'))
await page.waitForSelector(tid('member-form'), { state: 'detached' })
await page.waitForFunction(() => document.querySelector('[data-testid=member-detail]')?.textContent.includes('88 Ayala Ave'))
expect(await page.locator(tid('member-detail')).innerText().then(s => s.includes('还没在地图上定位')), true, 'the old pin is cleared')
await page.waitForFunction(n => document.querySelector(`[data-member="${n}"]`)?.textContent.includes('Lina K'), lina.no)
step('editing saves and the list follows')
// both addresses are kept: the new one is current, the old one can go
await page.waitForFunction(() => document.querySelectorAll('[data-testid=member-addr-item]').length === 2)
expect((await page.locator('[data-testid=member-addr-item].cur').innerText()).includes('88 Ayala Ave'), true, 'the new address is current')
expect((await page.locator(`[data-member="${lina.no}"] .mb-addr .tag`).innerText()).trim(), '+1 个', 'the list says there is one more address')
step('a member keeps every address, the latest current')
await page.screenshot({ path: `${SHOTS}/members-08-addresses-pc.png` })
page.once('dialog', d => d.accept())
await page.click(tid('member-address-delete'))
await page.waitForFunction(() => document.querySelectorAll('[data-testid=member-addr-item]').length === 1)
step('an old address can be deleted')
await page.screenshot({ path: `${SHOTS}/members-02-store-detail-pc.png` })
await page.locator(tid('member-detail')).locator('.sheet-foot .btn:not(.primary)').click()

// a member with orders: the one "order for a customer" made, if any
await page.fill(tid('members-search'), '')
await page.selectOption(tid('members-sort'), 'spent')
await page.waitForTimeout(300)
const top = page.locator(tid('member-row')).first()
if (Number((await top.locator('td.num').first().innerText()).trim()) > 0) {
  await top.locator(tid('member-view')).click()
  await page.waitForSelector(tid('member-order'))
  step('order history of a member who ordered')
  await page.screenshot({ path: `${SHOTS}/members-03-store-orders-pc.png` })
  await page.locator(tid('member-detail')).locator('.sheet-foot .btn:not(.primary)').click()
}

// ---- the platform ----
const admin = await signIn('admin', { width: 1440, height: 900 })
await admin.goto(BASE + '/admin/dashboard')
await admin.click(tid('nav-members'))
await admin.waitForURL(u => u.pathname === '/admin/members')
await admin.waitForSelector(tid('member-row'))
await admin.screenshot({ path: `${SHOTS}/members-04-admin-pc.png` })
await admin.selectOption(tid('members-store'), bid)
await admin.waitForTimeout(400)
await admin.fill(tid('members-search'), uniq)
await admin.press(tid('members-search'), 'Enter')
await admin.waitForFunction(n => document.querySelectorAll('[data-testid=member-row]').length === 1 && !!document.querySelector(`[data-member="${n}"]`), lina.no)
await admin.selectOption(tid('members-store'), '0')
step('the platform sees every store and narrows to one')
await admin.click(`[data-member="${lina.no}"] [data-testid=member-edit]`)
await admin.fill(tid('member-note'), `平台改的 ${uniq}`)
await admin.click(tid('member-save'))
await admin.waitForSelector(tid('member-form'), { state: 'detached' })
const saved = await call('GET', `/merchant/branches/${bid}/members/${lina.no}`)
expect(saved.note, `平台改的 ${uniq}`, 'the platform edited the note')
step('the platform edits a member')

// ---- iPad and phone ----
for (const [name, viewport] of [['ipad', { width: 820, height: 1180 }], ['phone', { width: 390, height: 844 }]]) {
  const p = await signIn('owner1', viewport)
  await p.goto(BASE + '/merchant/members')
  await p.waitForSelector(tid('member-row'))
  await noSideScroll(p, `${name} member list`)
  await p.screenshot({ path: `${SHOTS}/members-05-store-${name}.png` })
  await p.click(`[data-member="${lina.no}"] [data-testid=member-view]`)
  await p.waitForSelector(tid('member-stats'))
  await noSideScroll(p, `${name} member details`)
  await p.screenshot({ path: `${SHOTS}/members-06-store-detail-${name}.png` })
  const a = await signIn('admin', viewport)
  await a.goto(BASE + '/admin/members')
  await a.waitForSelector(tid('member-row'))
  await noSideScroll(a, `${name} platform member list`)
  await a.screenshot({ path: `${SHOTS}/members-07-admin-${name}.png` })
  step(`fits ${name === 'ipad' ? 'an iPad' : 'a phone'}`)
}

if (errors.length) throw new Error(errors.join('\n'))
await browser.close()
console.log('members e2e passed')
})().catch(e => { console.error(e); process.exit(1) })
