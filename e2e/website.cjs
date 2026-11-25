// The company website's form end to end (docs/WEBSITE_DESIGN.md §8): fill it in
// on the built site, see the thank-you, then find it in the platform's
// "Customers → Applications" tab, follow it up and see the badge go.
//
// Needs: the site built (cd site && npm run build) and served with /api passed
// on to the API, at SITE_URL (default http://localhost:4321); the API with the
// built back office at BASE_URL (default http://localhost:8080); a platform
// administrator ADMIN_USERNAME / ADMIN_PASSWORD (default: the seed's admin / demo1234).
const { chromium } = require('playwright')
const assert = require('assert')
const SITE = process.env.SITE_URL || 'http://localhost:4321'
const BASE = process.env.BASE_URL || 'http://localhost:8080'
const ADMIN = process.env.ADMIN_USERNAME || 'admin', PASS = process.env.ADMIN_PASSWORD || 'demo1234'

;(async () => {
  const browser = await chromium.launch()
  const errors = []
  const page = await browser.newPage({ viewport: { width: 390, height: 844 }, locale: 'zh-CN' })
  page.on('pageerror', e => errors.push(e.message))
  const store = '河马测试小馆 ' + Date.now()
  // a new number each run: the same number within a day updates the earlier application
  const local = '12' + String(Date.now()).slice(-7)

  await page.goto(SITE + '/zh/demo/')
  // nothing filled in: the form says what is missing and sends nothing
  await page.click('button[type=submit]')
  assert.match(await page.textContent('[data-f="store_name"] .err'), /店名/)
  assert.match(await page.textContent('[data-f="agree"] .err'), /隐私/)
  await page.fill('#store_name', store)
  await page.fill('#contact_name', '陈先生')
  await page.selectOption('#country_code', 'MY')
  assert.equal(await page.textContent('#dial'), '+60')
  await page.fill('#phone', '0' + local)
  await page.selectOption('#im_kind', 'whatsapp')
  await page.fill('#city', 'Kuala Lumpur')
  await page.click('text=中餐正餐')
  await page.click('text=2–5 家')
  await page.click('label.chk:has-text("堂食")')
  await page.click('label.chk:has-text("代客下单")')
  await page.fill('#note', '想下个月开始用')
  await page.waitForTimeout(3100) // a form sent within three seconds counts as a robot
  await page.check('input[name=agree]')
  await page.click('button[type=submit]')
  await page.waitForSelector('#done:not([hidden])')
  assert.match(await page.textContent('#done'), /abcfood123/)

  // the platform sees it
  const admin = await browser.newPage({ viewport: { width: 1440, height: 900 }, locale: 'zh-CN' })
  admin.on('pageerror', e => errors.push(e.message))
  await admin.goto(BASE + '/admin/login')
  await admin.fill('input[autocomplete=username], input[name=username], input[type=text]', ADMIN)
  await admin.fill('input[type=password]', PASS)
  await admin.click('button[type=submit], button.primary')
  await admin.waitForURL(/\/admin\/(dashboard|$)/)
  await admin.waitForSelector('[data-testid=applications-badge]')
  await admin.goto(BASE + '/admin/customers')
  await admin.click('[data-testid=ac-tab-applications]')
  await admin.selectOption('[data-testid=ac-source]', 'website')
  const card = admin.locator('[data-testid=ac-application]', { hasText: store })
  await card.waitFor()
  const text = await card.textContent()
  for (const want of ['官网', `+60 ${local}`, '中餐正餐', '2-5 家店', '堂食', '代客下单', 'WhatsApp', '/zh/demo/', '想下个月开始用', '待联系'])
    assert.ok(text.includes(want), `card shows ${want}: ${text}`)
  assert.equal(await card.locator('[data-testid=ac-im] a').getAttribute('href'), `https://wa.me/60${local}`)
  await card.screenshot({ path: process.env.SHOT_DIR ? process.env.SHOT_DIR + '/admin-lead-card.png' : '/dev/null' }).catch(() => {})
  await card.locator('[data-testid=ac-follow-up]').selectOption('contacted')
  await card.locator('[data-testid=ac-follow-note]').fill('WhatsApp 已联系')
  await card.locator('[data-testid=ac-follow-save]').click()
  await admin.waitForFunction(s => [...document.querySelectorAll('[data-testid=ac-application]')]
    .some(e => e.textContent.includes(s) && e.textContent.includes('已联系')), store)
  if (process.env.SHOT_DIR) await admin.screenshot({ path: process.env.SHOT_DIR + '/admin-leads.png' })
  assert.deepEqual(errors, [])
  await browser.close()
  console.log('website form: ok')
})().catch(e => { console.error(e); process.exit(1) })
