// Excel import of dishes. The owner of "bento" exports the current dishes,
// then uploads a sheet that updates one dish, adds a new category with
// pictures and options, and has three rows with mistakes; the preview names
// each mistake and only the good rows are written. Then a platform admin
// imports the blank template for "pasay" from the admin back office.
// Usage: BASE_URL=http://localhost:8080 node e2e/menuimport.cjs   (needs seed data)
const { chromium } = require('playwright')
const { mkdirSync, readFileSync } = require('node:fs')
const path = require('node:path')

;(async () => {

const BASE = process.env.BASE_URL || 'http://localhost:8080'
const SHOTS = process.env.SHOTS_DIR || 'e2e/shots'
mkdirSync(SHOTS, { recursive: true })
const browser = await chromium.launch()
const errors = []
async function open(viewport) {
  const ctx = await browser.newContext({ viewport, locale: 'zh-CN', acceptDownloads: true, ignoreHTTPSErrors: process.env.E2E_INSECURE_TLS === '1' })
  const page = await ctx.newPage()
  page.on('pageerror', e => errors.push(`pageerror: ${e.message}`))
  page.on('console', m => { if (m.type() === 'error' && !/favicon|Failed to load resource/.test(m.text())) errors.push(`console: ${m.text()}`) })
  return page
}
const step = (s) => console.log('✓ ' + s)
const shot = (page, name) => page.screenshot({ path: `${SHOTS}/${name}.png` })
const expect = (got, want, what) => { if (got !== want) throw new Error(`${what}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`) }
const tid = (id) => `[data-testid=${id}]`
async function signIn(page, user) {
  await page.goto(BASE + '/login')
  await page.fill('input[autocomplete=username]', user)
  await page.fill('input[type=password]', 'demo1234')
  await page.click('button.primary')
  await page.waitForURL(u => !u.pathname.startsWith('/login'))
}
async function download(page, selector, name) {
  const dl = page.waitForEvent('download')
  await page.click(selector)
  const file = `${SHOTS}/${name}`
  await (await dl).saveAs(file)
  const b = readFileSync(file)
  expect(b.subarray(0, 2).toString(), 'PK', `${name} is an xlsx`)
  return file
}

// ---- the store owner ----
const owner = await open({ width: 1280, height: 860 })
await signIn(owner, 'owner5')
await owner.goto(BASE + '/merchant/menu'); await owner.waitForSelector('table.data')
await owner.click(tid('open-import'))
await owner.waitForSelector(tid('import-dialog'))
await download(owner, tid('import-export'), 'menu-export.xlsx')
await download(owner, tid('import-template'), 'menu-template-bento.xlsx')
step('the template and the export of the current dishes download')

await owner.setInputFiles(tid('import-file'), path.join(__dirname, 'fixtures/menu-import.xlsx'))
await owner.locator(tid('import-summary') + ':has-text("3 行可导入")').waitFor({ timeout: 20000 })
const err = async (line) => (await owner.locator(`tr[data-line="${line}"] td:last-child`).innerText()).trim()
expect(await err(5), '价格格式不对', 'row 5')
expect((await err(6)).includes('图片链接不对'), true, 'row 6')
expect((await err(7)).includes('规格“冰量 少冰”写法不对') && (await err(7)).includes('是/否'), true, 'row 7')
expect(await err(2), '更新', 'row 2 updates the existing dish')
expect(await err(3), '新增', 'row 3 is published at once')
expect(await err(4), '新增（草稿）', 'row 4 is a draft')
await owner.locator('tr[data-line="3"] .imp-newcat').waitFor()
await owner.locator('tr[data-line="3"] img.imp-thumb').waitFor()
await shot(owner, 'mi1-merchant-preview')
step('the preview shows updates, new dishes, the new category and why three rows are refused')

await owner.click(tid('import-apply'))
await owner.locator(tid('import-done') + ':has-text("新增 2 个，更新 1 个")').waitFor()
await owner.click('.sheet-foot button:has-text("返回")')
await owner.locator('h2:has-text("甜品")').waitFor()
await owner.locator('tr:has-text("芒果冰") .tag.ok').waitFor()
await owner.locator('tr:has-text("豆花") .tag:has-text("草稿")').waitFor()
await owner.locator('tr:has-text("招牌便当") .price:has-text("229")').waitFor()
await shot(owner, 'mi2-merchant-menu')
step('two dishes added under a new category and one updated; the bad rows were skipped')

// the customer sees the published dish with its pictures and options
const phone = await open({ width: 390, height: 844 })
await phone.goto(BASE + '/s/bento'); await phone.waitForSelector('.item')
await phone.locator('.item:has-text("芒果冰")').first().waitFor()
expect(await phone.locator('.item:has-text("豆花")').count(), 0, 'the draft is not on the store page')
step('the published dish is on the store page, the draft is not')

// the phone layout of the dialog
const small = await open({ width: 390, height: 844 })
await signIn(small, 'owner5')
await small.goto(BASE + '/merchant/menu'); await small.waitForSelector('table.data')
await small.click(tid('open-import'))
await small.setInputFiles(tid('import-file'), path.join(__dirname, 'fixtures/menu-import.xlsx'))
await small.locator(tid('import-summary')).waitFor({ timeout: 20000 })
await shot(small, 'mi3-merchant-phone')
step('the dialog fits a phone')

// ---- a platform admin imports for another store ----
const admin = await open({ width: 1360, height: 900 })
await signIn(admin, 'admin')
const branches = (await (await admin.request.get(BASE + '/api/v1/admin/branches')).json()).branches
const pasay = branches.find(b => b.slug === 'pasay')
await admin.goto(BASE + '/admin/merchants/' + pasay.merchant_id)
await admin.locator(`tr[data-branch=pasay] ${tid('branch-import')}`).click()
await admin.waitForSelector(tid('import-dialog'))
await admin.locator(tid('import-dialog') + ' h2:has-text("帕赛烧腊")').waitFor()
const tpl = await download(admin, tid('import-template'), 'menu-template-pasay.xlsx')
await admin.setInputFiles(tid('import-file'), tpl)
await admin.locator(tid('import-summary') + ':has-text("3 行可导入")').waitFor({ timeout: 20000 })
await shot(admin, 'mi4-admin-preview')
await admin.click(tid('import-apply'))
await admin.locator(tid('import-done') + ':has-text("新增 3 个")').waitFor()
await admin.click('.sheet-foot button:has-text("返回")')
await admin.locator('tr[data-item="椒盐杏鲍菇"]').waitFor()
await shot(admin, 'mi5-admin-items')
const audit = await (await admin.request.get(BASE + '/api/v1/admin/audit?action=menu.import')).json()
expect(audit.items.some(r => r.target_id === pasay.id), true, 'the admin import is in the admin log')
step('the admin imported the template for pasay from the merchant page, and it is logged')

if (errors.length) throw new Error('browser errors:\n' + errors.join('\n'))
console.log('E2E OK')
await browser.close()
})().catch(e => { console.error(e); process.exit(1) })
