// The platform back office in a real browser: onboard a merchant, manage its
// accounts and menu, look at its store read-only, then suspend, delete and
// restore it. Every step is checked from the customer's side too.
// Usage: BASE_URL=http://localhost:8080 node e2e/admin.cjs   (needs seed data)
const { chromium } = require('playwright')
const { mkdirSync } = require('node:fs')

;(async () => {

const BASE = process.env.BASE_URL || 'http://localhost:8080'
const SHOTS = process.env.SHOTS_DIR || 'e2e/shots'
mkdirSync(SHOTS, { recursive: true })
const browser = await chromium.launch()
const errors = []
async function open(viewport) {
  const ctx = await browser.newContext({ viewport, locale: 'zh-CN', ignoreHTTPSErrors: process.env.E2E_INSECURE_TLS === '1' })
  const page = await ctx.newPage()
  page.on('pageerror', e => errors.push(`pageerror: ${e.message}`))
  page.on('console', m => { if (m.type() === 'error' && !/favicon|Failed to load resource/.test(m.text())) errors.push(`console: ${m.text()}`) })
  return page
}
const step = (s) => console.log('✓ ' + s)
const shot = (page, name) => page.screenshot({ path: `${SHOTS}/${name}.png`, fullPage: true })
const expect = (got, want, what) => { if (got !== want) throw new Error(`${what}: expected ${want}, got ${got}`) }
async function signIn(page, user, password) {
  await page.goto(BASE + '/login')
  await page.fill('input[autocomplete=username]', user)
  await page.fill('input[type=password]', password)
  await page.click('button.primary')
  await page.waitForURL(u => !u.pathname.startsWith('/login'))
}
// answer the next prompt()/confirm() with the given text
const answer = (page, text) => page.once('dialog', d => d.accept(text))

const tag = Date.now().toString(36)
const NAME = `新记饺子 ${tag}`, SLUG = `xinji-${tag}`, OWNER = `xinji-${tag}`

const admin = await open({ width: 1360, height: 900 })
await signIn(admin, 'admin', 'demo1234')
await admin.waitForURL('**/admin/dashboard')
for (const card of ['api', 'worker', 'db', 'queue']) await admin.waitForSelector(`[data-health=${card}][data-state]`)
expect(await admin.getAttribute('[data-health=db]', 'data-state'), 'ok', 'database health')
expect(await admin.getAttribute('[data-health=worker]', 'data-state'), 'ok', 'worker health')
expect(await admin.locator('.chart svg').count(), 2, 'charts on the overview')
await shot(admin, 'a0-dashboard')
step('the administrator lands on the overview with four health cards')

// ---- API monitoring: a customer opens a menu, the request shows up by route template ----
const visitor = await open({ width: 390, height: 844 })
await visitor.goto(BASE + '/s/makati')
await visitor.waitForSelector('.item, [data-item], h1')
await admin.click('.adm-side a[href="/admin/api"]')
await admin.waitForSelector('tr[data-route="GET /api/v1/stores/:slug"]', { timeout: 40000 })
if ((await admin.content()).includes('/stores/makati')) throw new Error('a store link leaked into the API metrics')
if (await admin.locator('tr[data-route*="/admin/metrics"], tr[data-route$="/events"]').count()) throw new Error('monitoring polls or event streams are counted as requests')
expect(await admin.locator('.chart svg').count(), 3, 'charts on the API page')
const plot = await admin.locator('.chart svg').first().boundingBox()
await admin.mouse.move(plot.x + plot.width - 20, plot.y + plot.height / 2)
await admin.waitForSelector('.chart .tip')
await shot(admin, 'a0-api')
await admin.mouse.move(0, 0)
await admin.click('[data-range="24h"]')
await admin.waitForSelector('[data-range="24h"].on')
await admin.click('th[data-sort=p95]')
await admin.fill('[data-testid=route-filter]', 'stores/:slug')
await admin.waitForFunction(() => [...document.querySelectorAll('tr[data-route]')].every(r => r.dataset.route.includes('/stores/:slug')))
step('API monitoring counts requests by route template and the charts answer to the pointer')

await admin.click('.adm-side a[href="/admin/merchants"]')
await admin.waitForURL('**/admin/merchants')
await admin.waitForSelector('tr[data-merchant]')
step('the merchant list opens from the sidebar')

// ---- onboard a merchant ----
await admin.click('[data-testid=new-merchant]')
const cf = admin.locator('[data-testid=merchant-create]')
await cf.locator('[name=merchant_name]').fill(NAME)
await cf.locator('[name=contact_name]').fill('刘新')
await cf.locator('[name=contact_phone]').fill('+63 927 555 0135')
await cf.locator('[name=note]').fill('朋友介绍，先试用一个月')
await cf.locator('[name=branch_name]').fill('新记饺子（马卡蒂店）')
await cf.locator('[name=branch_name_en]').fill('Xinji Dumplings')
await cf.locator('[name=slug]').fill(SLUG)
await cf.locator('[name=username]').fill(OWNER)
await cf.locator('[name=display_name]').fill('刘新')
await cf.locator('[name=password]').fill('first-pass-1')
await shot(admin, 'a1-merchant-new')
await cf.locator('button.primary').click()
await admin.waitForSelector(`tr[data-merchant="${NAME}"]`)
await admin.fill('[data-testid=merchant-search]', SLUG)
await admin.waitForFunction(() => document.querySelectorAll('tr[data-merchant]').length === 1)
await admin.fill('[data-testid=merchant-search]', '')
await shot(admin, 'a2-merchants')
step('a new merchant is created with its store and owner, and can be found by its link')

const cust = await open({ width: 420, height: 800 })
await cust.goto(`${BASE}/s/${SLUG}`)
await cust.waitForSelector('.shophead')
step('the new store link works for customers')

// ---- accounts ----
await admin.click(`tr[data-merchant="${NAME}"] a.bold`)
await admin.waitForSelector('[data-testid=merchant-name]')
expect(await admin.textContent('[data-testid=merchant-name]'), NAME, 'detail page name')
await admin.click('[data-testid=new-account]')
const af = admin.locator('[data-testid=account-form]')
await af.locator('[name=role]').selectOption('cashier')
await af.locator('[name=username]').fill(`cash-${tag}`)
await af.locator('[name=display_name]').fill('收银')
await af.locator('[name=password]').fill('first-pass-1')
await af.locator('button.primary').click()
await admin.waitForSelector(`tr[data-user="cash-${tag}"]`)
answer(admin, '')
await admin.click(`tr[data-user="${OWNER}"] [data-testid=reset-password]`)
await admin.waitForSelector('[data-testid=new-password] b')
const newPassword = (await admin.textContent('[data-testid=new-password] b')).trim()
if (newPassword.length < 12) throw new Error('generated password looks wrong: ' + newPassword)
const ownerPage = await open({ width: 1280, height: 900 })
await signIn(ownerPage, OWNER, newPassword)
await ownerPage.waitForURL('**/merchant/orders')
step('an account is added, and a reset password is shown once and works')

// ---- menu: category, item, toggles, bulk ----
await admin.click(`tr[data-branch="${SLUG}"] a:has-text("商品")`)
await admin.waitForSelector('[data-testid=editing-banner]')
await admin.click('[data-testid=manage-cats]')
await admin.fill('[data-testid=cats-drawer] [name=cat_name]', '饺子')
await admin.click('[data-testid=cats-drawer] form .btn.primary')
await admin.waitForSelector('[data-cat="饺子"]')
await admin.click('[data-testid=cats-drawer] .df .btn')
for (const [name, en, price] of [['猪肉白菜饺', 'Pork & Cabbage Dumplings', '168'], ['三鲜饺', 'Three Delicacies Dumplings', '188']]) {
  await admin.click('[data-testid=new-item]')
  const f = admin.locator('[data-testid=item-form]')
  await f.locator('input').nth(0).fill(name)
  await f.locator('input').nth(1).fill(en)
  await f.locator('input[type=number]').first().fill(price)
  await f.locator('label.check input').first().check() // published
  await f.locator('.sheet-foot .btn.primary').click()
  await admin.waitForSelector(`tr[data-item="${name}"]`)
}
await shot(admin, 'a3-items')
await cust.reload()
await cust.waitForSelector('text=猪肉白菜饺')
step('a category and two items added by the administrator show up for customers')

await admin.click('tr[data-item="三鲜饺"] [data-testid=sw-sold-out]')
await admin.waitForSelector('tr[data-item="三鲜饺"] [data-testid=sw-sold-out].on')
await admin.check('tr[data-item="猪肉白菜饺"] input[type=checkbox]')
await admin.click('[data-testid=bulk-unpublish]')
await admin.waitForSelector('tr[data-item="猪肉白菜饺"] [data-testid=sw-published]:not(.on)')
await cust.reload()
await cust.waitForSelector('text=三鲜饺')
expect(await cust.locator('text=猪肉白菜饺').count(), 0, 'unpublished item on the storefront')
answer(admin, '')
await admin.click('tr[data-item="猪肉白菜饺"] [data-testid=delete-item]')
await admin.waitForFunction(() => !document.querySelector('tr[data-item="猪肉白菜饺"]'))
step('sold out, bulk unpublish and delete work, and customers see the result')

// ---- read-only look at the store ----
await admin.goBack()
await admin.waitForSelector(`tr[data-branch="${SLUG}"]`)
await admin.click(`tr[data-branch="${SLUG}"] [data-testid=view-store]`)
await admin.waitForURL('**/merchant/orders')
await admin.waitForSelector('[data-testid=admin-viewing]')
await admin.goto(BASE + '/merchant/menu')
await admin.waitForSelector('text=三鲜饺')
await admin.click('tr:has-text("三鲜饺") button:has-text("恢复供应")')
await admin.waitForSelector('.banner.bad')
await shot(admin, 'a4-store-readonly')
await admin.click('[data-testid=admin-viewing] button')
await admin.waitForURL('**/admin/merchants')
step('the administrator can look at the store back office but cannot change anything there')

// ---- suspend, delete, restore ----
await admin.click(`tr[data-merchant="${NAME}"] a.bold`)
await admin.waitForSelector('[data-testid=suspend]')
await shot(admin, 'a5-merchant-detail')
answer(admin, '资料待补')
await admin.click('[data-testid=suspend]')
await admin.waitForSelector('[data-testid=resume]')
await cust.reload()
await cust.waitForSelector('text=暂停接单')
await ownerPage.reload()
await ownerPage.waitForSelector('.staffbar')
step('a suspended merchant takes no orders, while its staff can still sign in')

answer(admin, 'not the name')
await admin.click('[data-testid=delete]')
await admin.waitForSelector('.banner.bad')
answer(admin, NAME)
await admin.click('[data-testid=delete]')
await admin.waitForURL('**/admin/merchants')
await cust.reload()
await cust.waitForSelector('text=没有找到')
await ownerPage.goto(BASE + '/merchant/orders')
await ownerPage.waitForURL('**/login**')
await admin.click('[data-testid=tab-deleted]')
await admin.waitForSelector(`tr[data-merchant="${NAME}"]`)
step('deleting needs the merchant name; the link dies and staff are signed out')

await admin.click(`tr[data-merchant="${NAME}"] button:has-text("恢复")`)
await admin.waitForFunction((n) => !document.querySelector(`tr[data-merchant="${n}"]`), NAME)
await cust.reload()
await cust.waitForSelector('text=暂停接单')
step('a deleted merchant can be restored and comes back suspended')

// ---- audit trail ----
await admin.goto(BASE + '/admin/audit')
await admin.waitForSelector('tr[data-action]')
for (const a of ['merchant.create', 'user.reset_password', 'item.create', 'item.bulk_unpublish', 'merchant.suspend', 'merchant.delete', 'merchant.restore']) {
  if (await admin.locator(`tr[data-action="${a}"]`).count() === 0) throw new Error('no audit entry for ' + a)
}
if ((await admin.content()).includes(newPassword)) throw new Error('the generated password is in the audit trail')
await shot(admin, 'a6-audit')
step('every change is in the audit trail, without the password')

// ---- worker and job queue ----
await admin.click('.adm-side a[href="/admin/worker"]')
await admin.waitForSelector('tr[data-instance=online]')
for (const task of ['expire_pending', 'release_scheduled', 'jobs', 'rider_trips', 'cleanup']) {
  await admin.waitForSelector(`tr[data-task=${task}] .tag.ok`)
}
step('the worker page shows a live worker and its periodic tasks')

const failed = Number(await admin.textContent('[data-testid=q-failed]'))
if (failed < 2) throw new Error(`expected the two failed demo jobs, found ${failed}: run the seed again`)
expect(await admin.textContent('[data-testid=failed-badge]'), String(failed), 'failed badge in the sidebar')
await admin.click('[data-tab=failed]')
await admin.waitForFunction(() => { const r = document.querySelectorAll('tr[data-job]'); return r.length > 0 && [...r].every(x => x.dataset.status === 'failed') })
await shot(admin, 'a7-worker')
const first = admin.locator('tr[data-job]').first()
const jobId = await first.getAttribute('data-job')
if (!/^\+63900•+\d\d$/.test((await first.locator('td').nth(2).textContent()).trim())) throw new Error('the phone number of a job is not masked')
await first.locator('[data-act=details]').click()
await admin.waitForSelector('[data-testid=job-message]')
await shot(admin, 'a8-job-detail')
await admin.click('[data-testid=job-detail] .df button:has-text("关闭")')
step('failed jobs are listed with a masked phone number; the message is in the details')

answer(admin, '')
await first.locator('[data-act=discard]').click()
await admin.waitForFunction((id) => !document.querySelector(`tr[data-job="${id}"]`), jobId)
await admin.click('[data-tab=discarded]')
await admin.waitForSelector(`tr[data-job="${jobId}"][data-status=discarded]`)
answer(admin, '')
await admin.click('[data-testid=retry-all]')
await admin.waitForSelector('[data-testid=notice]')
await admin.waitForFunction(() => document.querySelector('[data-testid=q-failed]').textContent === '0')
await admin.waitForFunction(() => !document.querySelector('[data-testid=failed-badge]'))
await admin.click(`tr[data-job="${jobId}"] [data-act=retry]`)
await admin.click('[data-tab=done]')
await admin.waitForSelector(`tr[data-job="${jobId}"][data-status=done]`, { timeout: 30000 })
step('a job can be discarded, brought back and retried; retry-all clears the failed ones')

// ---- a merchant account has no way in ----
const other = await open({ width: 1280, height: 900 })
await signIn(other, 'owner1', 'demo1234')
await other.goto(BASE + '/admin/merchants')
await other.waitForSelector('.banner.bad')
expect(await other.locator('tr[data-merchant]').count(), 0, 'merchant rows visible to a merchant account')
step('a merchant account cannot use the back office')

await browser.close()
if (errors.length) { console.error(errors.join('\n')); process.exit(1) }
console.log('admin back office: all steps passed')

})().catch(e => { console.error(e); process.exit(1) })
