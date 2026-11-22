// The same pages on a computer, a tablet held upright and a phone: nothing
// scrolls sideways, the navigation is a side column or a drawer, tables turn
// into cards on a phone, and a customer on a computer orders from the cart
// beside the menu.
// Usage: BASE_URL=http://localhost:8080 node e2e/responsive.cjs   (needs seed data and OTP_DEV_ECHO=true)
const { chromium } = require('playwright')
const { customerLogin } = require('./lib.cjs')
const { mkdirSync } = require('node:fs')

;(async () => {

const BASE = process.env.BASE_URL || 'http://localhost:8080'
const SHOTS = process.env.SHOTS_DIR || 'e2e/shots'
mkdirSync(SHOTS, { recursive: true })
const browser = await chromium.launch()
const errors = []
const SIZES = { pc: { width: 1440, height: 900 }, pad: { width: 820, height: 1180 }, h5: { width: 390, height: 844 } }

async function open(size, storageState) {
  const ctx = await browser.newContext({ viewport: SIZES[size], locale: 'zh-CN', storageState, ignoreHTTPSErrors: process.env.E2E_INSECURE_TLS === '1' })
  const page = await ctx.newPage()
  page.on('pageerror', e => errors.push(`pageerror: ${e.message}`))
  page.on('console', m => { if (m.type() === 'error' && !/favicon|Failed to load resource/.test(m.text())) errors.push(`console: ${m.text()}`) })
  page.on('dialog', d => d.accept())
  return page
}
// Sign in once per account and reuse the session at the other sizes: sign-in is rate limited.
const sessions = {}
async function as(user, size) {
  if (sessions[user]) return open(size, sessions[user])
  const page = await open(size)
  await page.goto(BASE + '/login')
  for (let i = 0; ; i++) {
    await page.fill('input[autocomplete=username]', user)
    await page.fill('input[type=password]', 'demo1234')
    const res = page.waitForResponse(r => r.url().endsWith('/auth/login'))
    await page.click('button.primary')
    if ((await res).status() !== 429 || i >= 5) break
    await page.waitForTimeout(7000)
  }
  await page.waitForURL(u => !u.pathname.startsWith('/login'))
  sessions[user] = await page.context().storageState()
  return page
}
const step = (s) => console.log('✓ ' + s)
const shot = (page, name) => page.screenshot({ path: `${SHOTS}/${name}.png` })
async function fits(page, what) {
  await page.waitForLoadState('networkidle', { timeout: 5000 }).catch(() => {})
  await page.waitForTimeout(300)
  const over = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)
  if (over > 1) throw new Error(`${what}: the page scrolls sideways by ${over}px`)
}
const visible = (page, sel) => page.locator(sel).first().isVisible()

// ---- customer ----
// The login page, before anyone is logged in.
for (const size of Object.keys(SIZES)) {
  const c = await open(size)
  await c.goto(BASE + '/account/login?store=makati&next=/s/makati/checkout')
  await c.waitForSelector('[data-testid=login-phone]')
  await fits(c, `login ${size}`)
  await shot(c, `v00-login-${size}`)
  await c.context().close()
}
// One login serves the three sizes: only its cookie is carried over, not the cart.
let loggedIn
for (const size of Object.keys(SIZES)) {
  const c = await open(size, loggedIn)
  await c.goto(BASE + '/s/makati')
  await c.waitForSelector('.item')
  await fits(c, `store ${size}`)
  const side = await visible(c, '[data-testid=side-cart]')
  if (side !== (size === 'pc')) throw new Error(`store ${size}: the cart beside the menu should ${size === 'pc' ? '' : 'not '}show`)
  const zone = c.locator('select').first()
  if (await zone.count() && await zone.isVisible()) await zone.selectOption({ index: 1 })
  await c.click('.item:has-text("盐酥鸡") button')
  await c.waitForSelector('.sheet')
  await c.click('.sheet label.opt:has-text("中辣")')
  // a sheet rises from the bottom on a phone and is a dialog in the middle elsewhere
  const box = await c.locator('.sheet').boundingBox()
  const atBottom = Math.abs(box.y + box.height - SIZES[size].height) < 2
  if (atBottom !== (size === 'h5')) throw new Error(`store ${size}: the options sheet is in the wrong place`)
  await c.click('.sheet-foot .btn.primary')
  await c.click('.item:has-text("瓶装水") .stepper button.fill')
  await shot(c, `v01-store-${size}`)
  if (size === 'pc') {
    if (await visible(c, '.bottombar')) throw new Error('store pc: the bottom bar should be hidden')
    await c.locator('[data-testid=side-cart] :text("盐酥鸡")').waitFor()
    await c.click('[data-testid=go-checkout]')
  } else await c.click('.bottombar .btn.primary')
  await customerLogin(c)
  await c.waitForSelector('.totals')
  await fits(c, `checkout ${size}`)
  // on a computer the summary and the button stand beside the form
  const form = await c.locator('[data-testid=checkout-address]').boundingBox()
  const btn = await c.locator('.bottombar .btn.primary').boundingBox()
  if ((btn.x > form.x + form.width) !== (size === 'pc')) throw new Error(`checkout ${size}: the order button is in the wrong place`)
  await shot(c, `v02-checkout-${size}`)
  loggedIn = { cookies: await c.context().cookies(), origins: [] }
  await c.context().close()
}
step('customer: menu, options and checkout fit a computer, a tablet and a phone; a computer orders from the cart beside the menu')

// ---- store back office ----
const office = ['/merchant/orders', '/merchant/tables', '/kitchen', '/merchant/menu', '/merchant/templates', '/merchant/promos', '/merchant/cash',
  '/merchant/riders', '/merchant/delivery-map', '/merchant/staff', '/merchant/staff/attendance', '/merchant/staff/kitchen', '/merchant/staff/riders', '/merchant/settings']
for (const size of Object.keys(SIZES)) {
  const p = await as('owner1', size)
  for (const path of office) { await p.goto(BASE + path); await p.waitForSelector('.shell-main'); await fits(p, `${path} ${size}`) }
  await p.goto(BASE + '/merchant/staff')
  await p.waitForSelector('tr[data-staff=owner1]')
  if (size === 'pc') {
    if (!(await visible(p, '.staffbar')) || await visible(p, '.shell-top')) throw new Error('office pc: the navigation should be a side column')
    // the column folds away and comes back, and stays folded after a reload
    await p.click('[data-testid=nav-fold]')
    await p.locator('.shell-top').waitFor()
    await p.reload(); await p.waitForSelector('.shell-top')
    if (await visible(p, '.staffbar')) throw new Error('office pc: the folded navigation should stay folded')
    await p.click('[data-testid=nav-open]')
    await p.locator('.staffbar a:has-text("订单")').waitFor()
  } else {
    if (await visible(p, '.staffbar')) throw new Error(`office ${size}: the navigation should be closed`)
    await p.click('[data-testid=nav-open]')
    await p.locator('.staffbar a:has-text("菜单")').waitFor()
    await shot(p, `v03-office-nav-${size}`)
    await p.click('.staffbar a:has-text("菜单")')
    await p.waitForURL('**/merchant/menu')
    await p.locator('.staffbar').waitFor({ state: 'hidden' })   // choosing a page closes the drawer
    await p.goto(BASE + '/merchant/staff'); await p.waitForSelector('tr[data-staff=owner1]')
  }
  // a table is a table on a computer and a tablet, cards with headings on a phone
  const head = await visible(p, 'table.stafftable thead')
  if (head !== (size !== 'h5')) throw new Error(`office ${size}: the staff table is in the wrong shape`)
  if (size === 'h5') {
    const label = await p.locator('tr[data-staff=owner1] td').nth(2).getAttribute('data-label')
    if (label !== '岗位') throw new Error('office h5: a card line should carry its heading, got ' + label)
  }
  await shot(p, `v04-office-staff-${size}`)
  await p.context().close()
}
step('store back office: a side column on a computer, a drawer on a tablet and a phone; tables become cards on a phone')

// ---- platform back office ----
for (const size of Object.keys(SIZES)) {
  const p = await as('admin', size)
  for (const path of ['/admin/dashboard', '/admin/api', '/admin/worker', '/admin/merchants', '/admin/merchants/1', '/admin/items', '/admin/audit', '/admin/settings']) {
    await p.goto(BASE + path); await p.waitForSelector('.adm-page'); await fits(p, `${path} ${size}`)
  }
  if (size !== 'pc') {
    await p.click('[data-testid=nav-open]')
    await p.click('.adm-side a[href="/admin/merchants"]')
    await p.waitForURL('**/admin/merchants')
    await p.locator('.adm-side').waitFor({ state: 'hidden' })
  } else if (!(await visible(p, '.adm-side'))) throw new Error('admin pc: the navigation should be a side column')
  await shot(p, `v05-admin-${size}`)
  await p.context().close()
}
step('platform back office fits all three')

// ---- staff workbench ----
for (const size of Object.keys(SIZES)) {
  const p = await as('rider1', size)
  await p.goto(BASE + '/work'); await p.waitForSelector('[data-testid=tab-tasks]')
  await fits(p, `workbench ${size}`)
  await p.click('[data-testid=tab-me]'); await p.waitForSelector('[data-testid=me]')
  await fits(p, `workbench me ${size}`)
  await shot(p, `v06-workbench-${size}`)
  await p.context().close()
}
step('staff workbench fits all three')

await browser.close()
if (errors.length) { console.error(errors.join('\n')); process.exit(1) }
console.log('responsive e2e passed')

})().catch(e => { console.error(e); process.exit(1) })
