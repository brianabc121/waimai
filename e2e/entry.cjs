// Signing in (docs/STAFF_ACCESS_DESIGN.md §5) in a real browser: the two tabs
// of /login, the identity picker, switching between the back office and the
// workbench (the password again on the way back), staff who can only use the
// workbench, and the platform's own sign-in page.
// Usage: BASE_URL=http://localhost:8080 DATABASE_URL=... node e2e/entry.cjs   (needs seed data;
// DATABASE_URL lets it give owner1 a second store for the picker)
const { chromium } = require('playwright')
const { mkdirSync } = require('node:fs')
const { execFileSync } = require('node:child_process')

;(async () => {

const BASE = process.env.BASE_URL || 'http://localhost:8080'
const SHOTS = process.env.SHOTS_DIR || 'e2e/shots'
mkdirSync(SHOTS, { recursive: true })
const browser = await chromium.launch()
const errors = []
async function open(viewport = { width: 1280, height: 820 }) {
  const ctx = await browser.newContext({ viewport, locale: 'zh-CN', ignoreHTTPSErrors: process.env.E2E_INSECURE_TLS === '1' })
  const page = await ctx.newPage()
  page.on('pageerror', e => errors.push(`pageerror: ${e.message}`))
  page.on('console', m => { if (m.type() === 'error' && !/favicon|Failed to load resource/.test(m.text())) errors.push(`console: ${m.text()}`) })
  return page
}
const step = (s) => console.log('✓ ' + s)
const shot = (page, name) => page.screenshot({ path: `${SHOTS}/${name}.png`, fullPage: true })
const expect = (got, want, what) => { if (got !== want) throw new Error(`${what}: expected ${want}, got ${got}`) }
const tid = (s) => `[data-testid=${s}]`
// Sign-in is rate limited per address (10 a minute): wait out a refusal.
async function signIn(page, user, tab, path = '/login') {
  if (path) await page.goto(BASE + path)
  if (tab) await page.click(tid('login-tab-' + tab))
  for (let i = 0; ; i++) {
    await page.fill('input[autocomplete=username]', user)
    await page.fill('input[type=password]', 'demo1234')
    const res = page.waitForResponse(r => /\/auth\/login$/.test(r.url()))
    await page.click('button.primary')
    if ((await res).status() !== 429 || i >= 6) return
    await page.waitForTimeout(7000)
  }
}
const sql = (q) => process.env.DATABASE_URL && execFileSync('psql', [process.env.DATABASE_URL, '-Atc', q]).toString().trim()

// ---- the owner: back office, then the workbench, then back with the password ----
const owner = await open()
await owner.goto(BASE + '/login')
await owner.locator(tid('login-tab-office') + '[aria-selected=true]').waitFor()
await shot(owner, 'e01-login')
await signIn(owner, 'owner1', 'office')
await owner.waitForURL('**/merchant/orders')
expect((await owner.locator(tid('nav-role')).textContent()).includes('店主'), true, 'role tag')
step('owner signs in to the back office')

await owner.click(tid('switch-identity'))
await owner.waitForURL('**/login/pick')
expect(await owner.locator(tid('pick-row')).count(), 1, 'stores in the picker')
await owner.click(tid('pick-work'))
await owner.waitForURL('**/work')
step('switching to the workbench needs no password')

await owner.goto(BASE + '/merchant/menu')
await owner.waitForURL(u => u.pathname === '/login/pick')
await owner.click(tid('pick-row'))
await owner.fill(tid('pick-password'), 'wrong-pass')
await owner.click(tid('pick-enter'))
await owner.locator('.banner.bad:has-text("密码不对")').waitFor()
await shot(owner, 'e02-pick-password')
await owner.fill(tid('pick-password'), 'demo1234')
await owner.click(tid('pick-enter'))
await owner.waitForURL('**/merchant/menu')
step('back to the back office asks for the password again, and goes where it was going')

// ---- staff who can only use the workbench ----
const rider = await open({ width: 390, height: 780 })
await signIn(rider, 'rider1', 'office')
await rider.locator(tid('login-staff-only')).waitFor()
await shot(rider, 'e03-staff-only')
await rider.click(tid('login-go-work'))
await rider.waitForURL('**/work')
await rider.goto(BASE + '/merchant/orders')
await rider.waitForURL('**/work')
step('a rider asking for the back office is shown the workbench instead')

const kitchen = await open({ width: 390, height: 780 })
await signIn(kitchen, 'kitchen1', 'work')
await kitchen.waitForURL('**/work')
await kitchen.goto(BASE + '/login')
await kitchen.waitForURL('**/work')
await kitchen.context().clearCookies()
await kitchen.goto(BASE + '/login')
await kitchen.locator(tid('login-tab-work') + '[aria-selected=true]').waitFor()
step('the workbench tab is remembered on the device')

// ---- the platform signs in on its own page ----
const adm = await open()
await signIn(adm, 'admin')
await adm.locator('.banner.bad').waitFor()
expect(new URL(adm.url()).pathname, '/login', 'platform on the store sign-in')
await adm.goto(BASE + '/admin/merchants')
await adm.waitForURL(u => u.pathname === '/admin/login')
await shot(adm, 'e04-admin-login')
await signIn(adm, 'admin', null, null)
await adm.waitForURL('**/admin/merchants')
// the store's back office in the same browser is a separate sign-in
await adm.goto(BASE + '/merchant/orders')
await adm.waitForURL(u => u.pathname === '/login')
await signIn(adm, 'owner1', 'office')
await adm.waitForURL('**/merchant/orders')
await adm.goto(BASE + '/admin/merchants')
await adm.locator('.adm-side').waitFor({ timeout: 8000 }).catch(async e => { await shot(adm, 'debug'); console.log(adm.url()); throw e })
expect(new URL(adm.url()).pathname, '/admin/merchants', 'platform still signed in')
await adm.click('.adm-side button.link, .adm-side .btn.link').catch(() => {})
step('the platform and the stores keep separate sign-ins')
// the owner's sign-in survived the platform's sign-out
await owner.goto(BASE + '/admin/dashboard')
await owner.waitForURL(u => u.pathname === '/admin/login')
step('a store sign-in does not open the platform')

// ---- several stores: the picker ----
if (process.env.DATABASE_URL) {
  sql(`INSERT INTO user_branch_roles (user_id, branch_id, role) SELECT u.id, b.id, 'cashier' FROM users u, branches b
    WHERE u.username='owner1' AND b.slug='bgc' ON CONFLICT DO NOTHING`)
  try {
    const p = await open({ width: 390, height: 780 })
    await signIn(p, 'owner1', 'office')
    await p.waitForURL('**/login/pick')
    expect(await p.locator(tid('pick-row')).count(), 2, 'stores in the picker')
    await shot(p, 'e05-pick')
    await p.locator(tid('pick-row') + ':has(.rtag.cashier)').click()
    await p.waitForURL('**/merchant/orders')
    await p.locator(tid('nav-open')).click().catch(() => {})
    expect((await p.locator(tid('nav-role')).textContent()).includes('接单员'), true, 'role in the second store')
    step('someone with roles in two stores picks one, and works there with that role')
  } finally {
    sql(`DELETE FROM user_branch_roles r USING users u, branches b WHERE r.user_id=u.id AND r.branch_id=b.id
      AND u.username='owner1' AND b.slug='bgc'`)
  }
}

if (errors.length) throw new Error('page errors:\n' + errors.join('\n'))
await browser.close()
console.log('entry e2e passed')
})().catch(e => { console.error(e); process.exit(1) })
