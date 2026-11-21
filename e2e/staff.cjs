// Staff page and workbench identities in a real browser: the owner adds a
// person with two posts, edits, resets the password, disables, restores and
// deletes them; the person's workbench follows the posts they are given.
// Usage: BASE_URL=http://localhost:8080 node e2e/staff.cjs   (needs seed data)
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
  page.on('dialog', d => d.accept())
  return page
}
// Sign-in is rate limited per address (10 a minute). This script signs in
// often, and may run right after the other suites, so wait out a refusal.
async function login(page, user, password = 'demo1234') {
  await page.goto(BASE + '/login')
  for (let i = 0; ; i++) {
    await page.fill('input[autocomplete=username]', user)
    await page.fill('input[type=password]', password)
    const res = page.waitForResponse(r => r.url().endsWith('/auth/login'))
    await page.click('button.primary')
    if ((await res).status() !== 429 || i >= 5) return
    await page.waitForTimeout(7000)
  }
}
const step = (s) => console.log('✓ ' + s)
const shot = (page, name) => page.screenshot({ path: `${SHOTS}/${name}.png`, fullPage: true })
const expect = (got, want, what) => { if (got !== want) throw new Error(`${what}: want ${want}, got ${got}`) }

const name = 'e2e' + String(Date.now()).slice(-7)

// ---- owner: the staff page ----
const owner = await open({ width: 1280, height: 860 })
await login(owner, 'owner1')
await owner.waitForURL('**/merchant/orders')
await owner.click('.staffbar a:has-text("员工")')
await owner.waitForURL('**/merchant/staff')
const row = (u) => owner.locator(`tr[data-staff="${u}"]`)
await row('zhang1').locator('.post:has-text("炸台")').waitFor()
await row('zhang1').locator('.post:has-text("热厨")').waitFor()
await row('wang1').locator('.post.packing').waitFor()
await row('wang1').locator('.post.rider').waitFor()
// seeded kitchen and rider accounts were moved onto posts
await row('kitchen1').locator('.post.packing').waitFor()
expect(await row('kitchen1').locator('.post.station').count(), 3, 'kitchen1 stations')
expect(await row('rider1').locator('.post').count(), 1, 'rider1 posts')
await shot(owner, 's01-staff-list')
step('staff page lists everyone with their posts')

// add: fryer + rider
await owner.click('[data-testid=staff-new]')
const form = owner.locator('[data-testid=staff-form]')
await form.locator('input[name=display_name]').fill('小赵')
await form.locator('input[name=username]').fill(name)
await form.locator('input[name=password]').fill('demo1234')
await form.locator('input[name=phone]').fill('0917 555 0199')
await form.locator('[data-post="station-炸台"]').click()
await form.locator('[data-post=rider]').click()
await shot(owner, 's02-staff-new')
await owner.click('[data-testid=staff-save]')
await row(name).locator('.post.rider').waitFor()
await row(name).locator('.post.station:has-text("炸台")').waitFor()
await owner.click('[data-testid=filter-rider]')
await row(name).waitFor()
expect(await row('zhang1').count(), 0, 'the rider filter hides non-riders')
await owner.click('.chips button:has-text("全部")')
step('owner added a person with two posts; the rider filter finds them')

// a duplicate username is refused with a readable message
await owner.click('[data-testid=staff-new]')
await form.locator('input[name=display_name]').fill('重复')
await form.locator('input[name=username]').fill(name)
await form.locator('input[name=password]').fill('demo1234')
await owner.click('[data-testid=staff-save]')
await owner.locator('[data-testid=staff-error]:has-text("已经有人用了")').waitFor()
await form.locator('button:has-text("取消")').click()

// ---- the new person: two identities on the workbench ----
const me = await open({ width: 390, height: 844 })
await login(me, name)
await me.waitForURL('**/work')
await me.locator('[data-testid=work-store]').waitFor()
await me.click('[data-testid=identity]')
await me.locator('[data-testid=identity-sheet] [data-id=store]:has-text("炸台")').waitFor()
await me.click('[data-testid=switch-go]')
await me.locator('[data-testid=rider-empty]').waitFor()
expect((await me.locator('[data-testid=identity]').innerText()).trim(), '骑手', 'identity after switching')
await me.reload()
await me.locator('[data-testid=rider-empty]').waitFor()   // the choice is remembered on the device
await me.click('[data-testid=tab-me]')
expect(await me.locator('[data-testid=my-posts] .tag').count(), 2, 'posts on the Me tab')
await shot(me, 's03-workbench-me')
step('the new person signs in to the workbench, switches identity, and the choice sticks')

// ---- owner edits: rider post off, packing on. The open workbench follows without signing in again ----
await row(name).locator('button:has-text("编辑")').click()
await form.locator('[data-post=rider]').click()
await form.locator('[data-post=packing]').click()
await owner.click('[data-testid=staff-save]')
await row(name).locator('.post.packing').waitFor()
expect(await row(name).locator('.post.rider').count(), 0, 'rider post removed')
await me.locator('[data-testid=my-posts] .tag:has-text("打包台")').waitFor({ timeout: 8000 })
expect(await me.locator('[data-testid=my-posts] .tag:has-text("骑手")').count(), 0, 'rider post gone on the workbench')
await me.click('[data-testid=tab-tasks]')
await me.locator('[data-testid=work-store]').waitFor()
expect((await me.locator('[data-testid=identity]').innerText()).trim(), '店内', 'falls back to the in-store identity')
step('removing the rider post takes the rider identity away on the open workbench')

// ---- reset password: old sessions end, the new password works ----
await row(name).locator('button:has-text("编辑")').click()
await form.locator('button:has-text("重置密码")').click()
await form.locator('input[name=new_password]').fill('newpass99')
await form.locator('button:has-text("确定")').click()
await owner.locator('[data-testid=staff-notice]:has-text("密码已重置")').waitFor()
await me.goto(BASE + '/work')
await me.waitForURL('**/login**')
await login(me, name, 'newpass99')
await me.waitForURL('**/work')
step('password reset signed the person out; the new password works')

// ---- disable, restore, delete ----
await row(name).locator('button:has-text("编辑")').click()
await owner.click('[data-testid=staff-toggle]')
await row(name).locator('button:has-text("恢复")').waitFor()
await me.goto(BASE + '/work')
await me.waitForURL('**/login**')
await row(name).locator('button:has-text("恢复")').click()
await row(name).locator('button:has-text("恢复")').waitFor({ state: 'detached' })
await row(name).locator('button:has-text("编辑")').click()
await owner.click('[data-testid=staff-delete]')
await row(name).waitFor({ state: 'detached' })
await login(me, name, 'newpass99')
await me.locator('.banner.bad').waitFor()
step('disabled, restored and deleted; a deleted person cannot sign in')

// the owner cannot disable or delete themselves, and their role is locked
await row('owner1').locator('button:has-text("编辑")').click()
expect(await owner.locator('[data-testid=staff-toggle]').count(), 0, 'no disable button for yourself')
expect(await form.locator('[data-role=owner]').isDisabled(), true, 'own role is locked')
await form.locator('button:has-text("取消")').click()

// ---- a manager cannot manage the owner ----
await owner.click('[data-testid=staff-new]')
await form.locator('input[name=display_name]').fill('店长')
await form.locator('input[name=username]').fill(name + 'm')
await form.locator('input[name=password]').fill('demo1234')
await form.locator('[data-role=manager]').click()
await owner.click('[data-testid=staff-save]')
await row(name + 'm').waitFor()
const mgr = await open({ width: 1280, height: 860 })
await login(mgr, name + 'm')
await mgr.waitForURL('**/merchant/orders')
await mgr.goto(BASE + '/merchant/staff')
await mgr.locator(`tr[data-staff="${name}m"]`).waitFor()
expect(await mgr.locator('tr[data-staff="owner1"] button:has-text("编辑")').count(), 0, 'a manager gets no edit button on an owner')
await mgr.locator('tr[data-staff="zhang1"] button:has-text("编辑")').click()
expect(await mgr.locator('[data-testid=staff-form] [data-role=owner]').count(), 0, 'a manager cannot grant owner')
await mgr.locator('[data-testid=staff-form] button:has-text("取消")').click()
// clean up
await row(name + 'm').locator('button:has-text("编辑")').click()
await owner.click('[data-testid=staff-delete]')
await row(name + 'm').waitFor({ state: 'detached' })
step('a manager can manage staff but not the owner')

// ---- a rider-only person and a person without posts ----
const r = await open({ width: 390, height: 844 })
await login(r, 'rider1')
await r.waitForURL('**/work')
await r.locator('[data-testid=work-rider]').waitFor()
expect(await r.locator('[data-testid=work-store]').count(), 0, 'a rider-only person has no in-store view')
await r.goto(BASE + '/rider')
await r.waitForURL('**/work')   // the old address still works
step('a rider-only person sees deliveries only; /rider redirects to the workbench')

await browser.close()
if (errors.length) { console.error(errors.join('\n')); process.exit(1) }
console.log('staff e2e passed')

})().catch(e => { console.error(e); process.exit(1) })
