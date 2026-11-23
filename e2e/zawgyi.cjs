// Burmese typed in the old Zawgyi encoding is converted to Unicode when a
// merchant saves it (docs/I18N_DESIGN.md §12, Google myanmar-tools).
// Usage: BASE_URL=http://localhost:8080 node e2e/zawgyi.cjs   (seed data)
const { chromium } = require('playwright')

const BASE = process.env.BASE_URL || 'http://localhost:8080'
const insecure = process.env.E2E_INSECURE_TLS === '1'
const tid = id => `[data-testid=${id}]`
const step = s => console.log('✓ ' + s)
const expect = (ok, what) => { if (!ok) throw new Error(what) }

;(async () => {
  const browser = await chromium.launch()
  const page = await (await browser.newContext({ viewport: { width: 1280, height: 860 }, locale: 'zh-CN', ignoreHTTPSErrors: insecure })).newPage()
  const errors = []
  page.on('pageerror', e => errors.push(e.message))
  page.on('dialog', d => { errors.push('unexpected dialog: ' + d.message()); d.dismiss() })
  await page.goto(BASE + '/login')
  await page.fill('input[autocomplete=username]', 'owner1')
  await page.fill('input[type=password]', 'demo1234')
  await page.click('button.primary')
  await page.waitForURL(u => u.pathname.startsWith('/merchant'))

  await page.goto(BASE + '/merchant/settings')
  const notice = page.locator('textarea[name=notice]')
  await notice.waitFor()
  const before = await notice.inputValue()
  const save = async text => {
    await notice.fill(text)
    const res = page.waitForResponse(r => /\/merchant\/branches\/\d+\/settings$/.test(r.url()) && r.request().method() === 'PUT')
    await page.locator('form:has(textarea[name=notice]) button.primary').click()
    return (await res).request().postDataJSON()
  }
  try {
    // "Myanmar fried rice" typed in Zawgyi
    const sent = await save('ျမန္မာ ထမင္းေၾကာ္')
    expect(sent.notice === 'မြန်မာ ထမင်းကြော်', 'sent as Unicode: ' + sent.notice)
    await page.waitForSelector(tid('zawgyi-notice'))
    const msg = await page.locator(tid('zawgyi-notice')).innerText()
    expect(msg.includes('Zawgyi') && msg.includes('မြန်မာ'), 'notice: ' + msg)
    await page.waitForSelector('.banner.ok')
    expect(await notice.inputValue() === 'မြန်မာ ထမင်းကြော်', 'saved as Unicode')
    step('Zawgyi typed by the merchant is saved as Unicode, with a notice')

    await page.waitForSelector(tid('zawgyi-notice'), { state: 'detached', timeout: 10000 })
    const kept = await save('မြန်မာ ထမင်းကြော်')
    expect(kept.notice === 'မြန်မာ ထမင်းကြော်', 'Unicode kept')
    await page.waitForTimeout(500)
    expect(await page.locator(tid('zawgyi-notice')).count() === 0, 'no notice for Unicode')
    step('Unicode Burmese is saved as typed, without a notice')
  } finally {
    await save(before)
  }
  expect(errors.length === 0, errors.join('\n'))
  await browser.close()
  console.log('zawgyi e2e passed')
})().catch(e => { console.error(e); process.exit(1) })
