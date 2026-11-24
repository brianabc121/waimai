// The keys page of the platform back office in a real browser: save a map
// key and see the customer pages get it without a restart, see it masked,
// get a refusal for keys that do not work together, then clear it again.
// Usage: BASE_URL=http://localhost:8080 node e2e/settings.cjs   (needs seed data)
const { chromium } = require('playwright')
const { mkdirSync } = require('node:fs')

;(async () => {

const BASE = process.env.BASE_URL || 'http://localhost:8080'
const SHOTS = process.env.SHOTS_DIR || 'e2e/shots'
mkdirSync(SHOTS, { recursive: true })
const browser = await chromium.launch()
const errors = []
const ctx = await browser.newContext({ viewport: { width: 1360, height: 900 }, locale: 'zh-CN', ignoreHTTPSErrors: process.env.E2E_INSECURE_TLS === '1' })
const page = await ctx.newPage()
page.on('pageerror', e => errors.push(`pageerror: ${e.message}`))
page.on('console', m => { if (m.type() === 'error' && !/favicon|Failed to load resource/.test(m.text())) errors.push(`console: ${m.text()}`) })
const step = (s) => console.log('✓ ' + s)
const shot = (name) => page.screenshot({ path: `${SHOTS}/${name}.png`, fullPage: true })
const expect = (got, want, what) => { if (got !== want) throw new Error(`${what}: expected ${want}, got ${got}`) }
// what a customer page is told, asked without the administrator's cookie
const publicConfig = async () => (await (await ctx.request.get(BASE + '/api/v1/config')).json())
const row = (key) => page.locator(`[data-key=${key}]`)
const card = (group) => page.locator(`[data-group=${group}]`)

await page.goto(BASE + '/admin/login')
await page.fill('input[autocomplete=username]', 'admin')
await page.fill('input[type=password]', 'demo1234')
await page.click('button.primary')
await page.waitForURL('**/admin/dashboard')
await page.click('.adm-side a[href="/admin/settings"]')
await page.waitForSelector('[data-group=maps]')
// start from a clean slate if an earlier run was cut short
page.on('dialog', d => d.accept())
while (await page.locator('[data-testid=clear]').count()) {
  await page.locator('[data-testid=clear]').first().click()
  await page.waitForSelector('[data-testid=set-saved]')
  await page.waitForTimeout(100)
}
await page.reload()
await page.waitForSelector('[data-group=maps]')
const before = await publicConfig()
step('the keys page opens from the side menu')

// save a map key
const KEY = 'AIzaSy-e2e-browser-key-7f3Q'
expect(await card('maps').locator('[data-testid=save]').isDisabled(), true, 'save is off until something is typed')
await row('GOOGLE_MAPS_API_KEY').locator('input').fill(KEY)
await card('maps').locator('[data-testid=save]').click()
await card('maps').locator('[data-testid=set-saved]').waitFor()
expect(await row('GOOGLE_MAPS_API_KEY').locator('[data-testid=mask]').innerText(), '••••••••7f3Q', 'masked key')
expect(await row('GOOGLE_MAPS_API_KEY').locator('[data-testid=source]').innerText(), '后台设置', 'source')
expect(await row('GOOGLE_MAPS_API_KEY').locator('input').inputValue(), '', 'the box is empty again')
expect((await publicConfig()).maps_key, KEY, 'customer pages get the key without a restart')
if ((await page.content()).includes(KEY)) throw new Error('the page holds the key in the clear')
step('a saved map key is masked and in use at once')

// keys that do not work together are refused
await row('SMS_PROVIDER').locator('select').selectOption('twilio')
await row('TWILIO_ACCOUNT_SID').locator('input').fill('AC0000')
await card('sms').locator('[data-testid=save]').click()
await card('sms').locator('[data-testid=set-error]').waitFor()
await shot('s1-settings')
await row('SMS_PROVIDER').locator('select').selectOption('log')
step('Twilio without its token is refused')

// a web push pair made on the page
await card('web_push').locator('[data-testid=new-pair]').click()
await card('web_push').locator('[data-testid=set-saved]').waitFor()
const pub = await row('VAPID_PUBLIC_KEY').locator('input').inputValue()
if (pub.length < 40) throw new Error('no public key after making a pair: ' + pub)
expect((await publicConfig()).push_key, pub, 'push key for the workbench')
step('a web push key pair is made and in use')

// it survives a reload, and the audit trail names the key but not its value
await page.reload()
await row('GOOGLE_MAPS_API_KEY').locator('[data-testid=mask]').waitFor()
await page.click('.adm-side a[href="/admin/audit"]')
await page.waitForSelector('tr[data-action="settings.update"]')
const trail = await page.locator('tr[data-action="settings.update"]').first().innerText()
if (trail.includes(KEY) || !/VAPID_PUBLIC_KEY|GOOGLE_MAPS_API_KEY/.test(trail)) throw new Error('audit row: ' + trail)
step('the change is in the audit trail without the key itself')

// clear everything: back to what the server had
await page.click('.adm-side a[href="/admin/settings"]')
await page.waitForSelector('[data-group=maps]')
while (await page.locator('[data-testid=clear]').count()) {
  await page.locator('[data-testid=clear]').first().click()
  await page.waitForSelector('[data-testid=set-saved]')
  await page.waitForTimeout(100)
}
const after = await publicConfig()
expect(after.maps_key, before.maps_key, 'map key after clearing')
expect(after.push_key, before.push_key, 'push key after clearing')
step('cleared keys fall back to the server environment')

await browser.close()
if (errors.length) { console.error(errors.join('\n')); process.exit(1) }
console.log('settings e2e passed')

})().catch(e => { console.error(e); process.exit(1) })
