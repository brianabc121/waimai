// The "new address" form: the store chooses which contact names (WeChat,
// Telegram, LINE, WhatsApp) it asks customers for, and the telephone code
// follows the country the store is in.
// Usage: BASE_URL=http://localhost:8080 node e2e/addressform.cjs   (needs seed data and OTP_DEV_ECHO=true)
;(async () => {
const { chromium } = require('playwright')
const fs = require('fs')
const { addAddress } = require('./lib.cjs')

const BASE = process.env.BASE_URL || 'http://localhost:8080'
const SHOTS = process.env.SHOTS_DIR || 'e2e/shots'
fs.mkdirSync(SHOTS, { recursive: true })

const browser = await chromium.launch()
const errors = []
const insecure = process.env.E2E_INSECURE_TLS === '1'
const watch = p => {
  p.on('pageerror', e => errors.push(`pageerror: ${e.message}`))
  p.on('console', m => { if (m.type() === 'error' && !/favicon|Failed to load resource/.test(m.text())) errors.push(`console: ${m.text()}`) })
  return p
}
const tid = id => `[data-testid=${id}]`
const step = s => console.log('✓ ' + s)

// ---- the owner chooses what to ask for ----
const owner = watch(await (await browser.newContext({ viewport: { width: 1280, height: 900 }, locale: 'zh-CN', ignoreHTTPSErrors: insecure })).newPage())
await owner.goto(BASE + '/login')
for (let i = 0; ; i++) {
  await owner.fill('input[autocomplete=username]', 'owner1')
  await owner.fill('input[type=password]', 'demo1234')
  const res = owner.waitForResponse(r => r.url().endsWith('/auth/login'))
  await owner.click('button.primary')
  if ((await res).status() !== 429 || i >= 5) break
  await owner.waitForTimeout(7000)
}
await owner.waitForURL(u => u.pathname.startsWith('/merchant'))
const ask = async (fields) => {
  await owner.goto(BASE + '/merchant/settings')
  const box = name => owner.locator(`${tid('contact-fields')} input[value=${name}]`)
  await box('wechat').waitFor()
  for (const f of ['wechat', 'telegram', 'line', 'whatsapp']) await box(f).setChecked(fields.includes(f))
  await owner.locator(`form:has(${tid('contact-fields')}) button.primary`).click()
  await owner.waitForSelector('.banner.ok')
}

try {
await ask(['wechat', 'line', 'whatsapp'])
await owner.locator(tid('contact-fields')).scrollIntoViewIfNeeded()
await owner.screenshot({ path: `${SHOTS}/af1-store-settings.png` })
step('the owner ticks WeChat, LINE and WhatsApp in the store settings')

// ---- the customer's form asks for exactly those ----
const ctx = await browser.newContext({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 2, locale: 'zh-CN', ignoreHTTPSErrors: insecure })
const page = watch(await ctx.newPage())
const local = '9' + String(Math.floor(Math.random() * 1e9)).padStart(9, '0')
await page.goto(BASE + '/s/makati')
await page.waitForSelector('.item')
await page.click('.item:has-text("瓶装水") .stepper button.fill')
await page.click('.bottombar .btn.primary')
await page.waitForURL(u => u.pathname === '/account/login')
await page.fill(tid('login-phone'), local)
await page.check(tid('login-agree'))
await page.click(tid('login-send'))
await page.waitForURL(u => u.pathname.endsWith('/checkout'))
await page.waitForSelector('.co[data-ready]')
await page.click(tid('checkout-address') + '[data-empty]')
await page.waitForSelector(tid('address-form'))
const shown = async () => {
  await page.waitForSelector(tid('af-note'))
  const out = []
  for (const f of ['wechat', 'telegram', 'line', 'whatsapp']) if (await page.locator(tid('af-' + f)).count()) out.push(f)
  return out.join(',')
}
if ((await shown()) !== 'wechat,line,whatsapp') throw new Error('the form asks for ' + await shown())
if ((await page.inputValue(tid('af-dial'))) !== '63' || (await page.inputValue(tid('af-phone'))) !== local) throw new Error('the phone should start as +63 and the login number')
await page.screenshot({ path: `${SHOTS}/af2-form-empty.png` })
await page.fill(tid('af-line'), 'wang.line')
await page.fill(tid('af-whatsapp'), '+63 917 555 0123')
await page.fill(tid('af-note'), '到了请打电话')
await page.click('.af-chips button:has-text("家")')
// nothing on the page is wider than the phone
if (await page.evaluate(() => document.documentElement.scrollWidth > innerWidth || [...document.querySelectorAll('.af-body *')].some(e => e.getBoundingClientRect().right > innerWidth + 1)))
  throw new Error('the form is wider than the screen')
await addAddress(page, { name: '王小明', unit: '12F', wechat: 'wang88', photo: true })
await page.waitForSelector('.totals')
step('the form shows those three and not Telegram; the phone starts as +63 with the login number')

await page.locator('.bottombar .btn.primary').click()
await page.waitForURL('**/o/**')
await page.locator('text=12F').first().waitFor()
const orderNo = (await page.locator('.topbar h1').innerText()).replace('订单号', '').trim()
await owner.goto(BASE + '/merchant/orders')
await owner.click(`.ord:has-text("${orderNo}")`)
const im = owner.locator(tid('order-im'))
for (const x of ['wang88', 'LINE', 'wang.line', 'WhatsApp', '+63 917 555 0123']) await im.locator(`text=${x}`).first().waitFor()
step('the store sees the WeChat, LINE and WhatsApp names with the order')

// ---- a store that asks for none shows none ----
await ask([])
await page.goto(BASE + '/s/makati')
await page.waitForSelector('.item')
await page.click(tid('account'))
await page.click(tid('my-addresses'))
await page.click(tid('address-new'))
await page.waitForSelector(tid('address-form'))
if ((await shown()) !== '') throw new Error('a store that asks for none still shows ' + await shown())
await page.screenshot({ path: `${SHOTS}/af3-form-no-contact-names.png` })
step('with nothing ticked the form shows none of them')

// ---- a store in another country starts on that country's code ----
await page.route('**/api/v1/stores/makati', async route => {
  const res = await route.fetch(), body = await res.json()
  body.branch.country_code = 'SG'
  await route.fulfill({ response: res, json: body })
})
await page.reload()
await page.waitForSelector('.item')
await page.click(tid('account'))
await page.click(tid('my-addresses'))
await page.click(tid('address-new'))
await page.waitForSelector(tid('address-form'))
// the login number is not a number of that country, so it is not filled in
if ((await page.inputValue(tid('af-dial'))) !== '65' || (await page.inputValue(tid('af-phone'))) !== '') throw new Error('a store in Singapore should start on +65 with no number')
await page.click(tid('af-save'))
await page.fill(tid('af-phone'), '12')
await page.fill(tid('af-name'), '陈')
await page.fill(tid('af-phone'), '9123 4567')
await page.screenshot({ path: `${SHOTS}/af4-form-other-country.png` })
await addAddress(page, { name: '陈大文', unit: '5A' })
await page.locator(`${tid('address-row')}:has-text("+65 91234567")`).waitFor()
// an address keeps the code it was saved with, whatever store it is opened from
await page.click(`${tid('address-row')}:has-text("12F") ${tid('address-edit')}`)
if ((await page.inputValue(tid('af-dial'))) !== '63' || (await page.inputValue(tid('af-phone'))) !== local) throw new Error('a saved number keeps its own code')
step('a store in Singapore starts on +65; saved numbers keep their own code')
await ctx.close()
} finally {
  // leave the demo store as the seed made it
  await ask(['wechat', 'telegram']).catch(e => errors.push('restore: ' + e.message))
}

await browser.close()
if (errors.length) { console.error(errors.join('\n')); process.exit(1) }
console.log('address form e2e passed')
})().catch(e => { console.error(e); process.exit(1) })
