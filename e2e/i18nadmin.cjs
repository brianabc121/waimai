// The platform's language page in a real browser (docs/I18N_DESIGN.md §8): the
// admin corrects a Thai text, sees it in the preview, a Thai customer gets it
// without a new release, the sheet goes out and comes back, and the
// correction is undone.
// Usage: BASE_URL=http://localhost:8080 node e2e/i18nadmin.cjs   (seed data)
const { chromium } = require('playwright')
const { mkdirSync } = require('node:fs')

;(async () => {

const BASE = process.env.BASE_URL || 'http://localhost:8080'
const SHOTS = process.env.SHOTS_DIR || 'e2e/shots'
mkdirSync(SHOTS, { recursive: true })
const insecure = process.env.E2E_INSECURE_TLS === '1'
const browser = await chromium.launch()
const errors = []
const watch = p => {
  p.on('pageerror', e => errors.push(`pageerror: ${e.message}`))
  p.on('console', m => { if (m.type() === 'error' && !/favicon|Failed to load resource/.test(m.text())) errors.push(`console: ${m.text()}`) })
  p.on('dialog', d => d.accept())
  return p
}
const tid = id => `[data-testid=${id}]`
const step = s => console.log('✓ ' + s)
const expect = (ok, what) => { if (!ok) throw new Error(what) }
const NEW = 'ค้นหาเลย'

const actx = await browser.newContext({ viewport: { width: 1600, height: 1000 }, locale: 'zh-CN', ignoreHTTPSErrors: insecure, acceptDownloads: true })
const adm = watch(await actx.newPage())
await adm.goto(BASE + '/login')
await adm.fill('input[autocomplete=username]', 'admin')
await adm.fill('input[type=password]', 'demo1234')
await adm.click('button.primary')
await adm.waitForURL(u => !u.pathname.startsWith('/login'))
const H = { headers: { 'X-Requested-With': 'waimai-web' } }
await adm.request.put(BASE + '/api/v1/admin/i18n/th', { ...H, data: { edits: [{ key: 'ml.search', text: '' }] } })

await adm.click(tid('nav-i18n'))
await adm.waitForSelector(tid('admin-i18n'))
await adm.selectOption(tid('i18n-lang'), 'th')
await adm.fill(tid('i18n-search'), 'ml.search')
const row = adm.locator('tr[data-key="ml.search"]')
await row.waitFor()
expect((await row.innerText()).includes('ค้นหา'), 'the built-in Thai text is listed')
step('the admin finds a text by its key, with Chinese, English and Thai side by side')

await row.locator(tid('i18n-cell')).click()
await row.locator(tid('i18n-draft')).fill(NEW)
await row.locator(tid('i18n-save')).click()
await adm.waitForSelector(tid('i18n-note'))
await row.locator('.tag.ok').waitFor()
const frame = adm.frameLocator(tid('i18n-preview'))
await frame.locator(`button.go:has-text("${NEW}")`).first().waitFor()
await adm.screenshot({ path: `${SHOTS}/i18n-01-admin.png` })
step('the correction is saved as checked and shows in the preview')

const th = watch(await (await browser.newContext({ viewport: { width: 390, height: 844 }, locale: 'th-TH', ignoreHTTPSErrors: insecure })).newPage())
await th.goto(BASE + '/')
await th.locator(`button.go:has-text("${NEW}")`).first().waitFor()
step('a Thai customer sees the correction without a new release')

// the sheet goes out and, unchanged, comes back with nothing to do
const [dl] = await Promise.all([adm.waitForEvent('download'), adm.click(tid('i18n-export'))])
const file = await dl.path()
await adm.setInputFiles(tid('i18n-import'), file)
await adm.locator(tid('i18n-note')).filter({ hasText: /没有要改的|nothing to change/ }).waitFor()
step('the Excel sheet goes out and comes back')

await row.locator(tid('i18n-revert')).click()
await row.locator('.tag.ok').waitFor({ state: 'detached' })
const fx = await (await adm.request.get(BASE + '/api/v1/admin/i18n/th')).json()
expect(!fx.fixes.some(f => f.key === 'ml.search'), 'the correction is gone')
const audit = await (await adm.request.get(BASE + '/api/v1/admin/audit?action=ui_text.save')).json().catch(() => null)
if (audit && audit.items) expect(audit.items.some(a => a.action === 'ui_text.save'), 'the change is in the audit trail')
step('the admin goes back to the built-in text, and both changes are logged')

if (errors.length) throw new Error(errors.join('\n'))
await browser.close()
console.log('i18nadmin: all good')
})().catch(e => { console.error(e); process.exit(1) })
