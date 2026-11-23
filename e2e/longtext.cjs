// Long translations must not break the layout (docs/I18N_DESIGN.md §15).
// Every customer page, the store desk and the staff workbench are opened at
// phone, tablet and computer width with pseudo-localisation on (?pseudo=1:
// every text about 40% longer, short ones longer still) and in the real
// languages that run longest or tallest (Russian, French, Burmese, Khmer) or
// turn the page around (Arabic). On each page:
//   - nothing scrolls sideways,
//   - no text runs out of its box or past the screen edge,
//   - no text is cut off without an ellipsis (… shows that more is there).
// AUDIT=1 lists every problem and carries on; otherwise the first page with
// one fails the test. LANGS=pseudo,ru SIZES=h5 narrows the run.
// Usage: BASE_URL=http://localhost:8080 node e2e/longtext.cjs   (needs seed data and OTP_DEV_ECHO=true)
const { chromium } = require('playwright')
const { customerLogin } = require('./lib.cjs')
const { mkdirSync } = require('node:fs')

;(async () => {

const BASE = process.env.BASE_URL || 'http://localhost:8080'
const SHOTS = process.env.SHOTS_DIR || 'e2e/shots'
const AUDIT = process.env.AUDIT === '1'
const FULL = process.env.FULL_SHOTS === '1'
mkdirSync(SHOTS + '/longtext', { recursive: true })
const browser = await chromium.launch()
const errors = []
const problems = []
const ALL_SIZES = { h5: { width: 390, height: 844 }, pad: { width: 820, height: 1180 }, pc: { width: 1440, height: 900 } }
const SIZES = Object.fromEntries(Object.entries(ALL_SIZES).filter(([k]) => !process.env.SIZES || process.env.SIZES.split(',').includes(k)))
const LANGS = (process.env.LANGS || 'pseudo,ru,fr,my,km,ar').split(',')
const HERE = { geolocation: { latitude: 14.5547, longitude: 121.0244 }, permissions: ['geolocation'] }
const tid = id => `[data-testid=${id}]`
const step = s => console.log('✓ ' + s)

async function open(size, lang, storageState) {
  const ctx = await browser.newContext({ viewport: SIZES[size], locale: 'en-US', storageState, ignoreHTTPSErrors: process.env.E2E_INSECURE_TLS === '1', ...HERE })
  // the language as a cookie, and pseudo-localisation for the tab
  const u = new URL(BASE)
  await ctx.addCookies([{ name: 'lang', value: lang === 'pseudo' ? 'en' : lang, domain: u.hostname, path: '/' }])
  if (lang === 'pseudo') await ctx.addInitScript(() => sessionStorage.setItem('pseudo', '1'))
  const page = await ctx.newPage()
  page.on('pageerror', e => errors.push(`pageerror: ${e.message}`))
  page.on('dialog', d => d.accept())
  return page
}

// check looks over the page as it stands and returns what is wrong.
function check() {
  const out = []
  const vw = document.documentElement.clientWidth
  const side = document.documentElement.scrollWidth - vw
  if (side > 1) {
    // name the widest box, the one that pushes the page out
    let widest = null
    for (const el of document.body.querySelectorAll('*')) {
      const r = el.getBoundingClientRect()
      if (r.right > vw + 1 && r.width > 0 && (!widest || r.width >= widest.getBoundingClientRect().width)) widest = el
    }
    out.push(`the page scrolls sideways by ${side}px` + (widest ? ` (widest: ${widest.tagName.toLowerCase()}.${[...widest.classList].join('.')}, ${Math.round(widest.getBoundingClientRect().width)}px)` : ''))
  }
  const scrolls = el => { const s = getComputedStyle(el); return /auto|scroll/.test(s.overflowX) }
  const name = el => {
    const t = el.getAttribute('data-testid'); if (t) return `[${t}]`
    let n = el.tagName.toLowerCase()
    if (el.classList.length) n += '.' + [...el.classList].slice(0, 2).join('.')
    const p = el.parentElement
    return (p && p !== document.body ? name(p).split(' ').slice(-1)[0] + ' > ' : '') + n
  }
  const ownText = el => [...el.childNodes].filter(n => n.nodeType === 3).map(n => n.textContent.trim()).join(' ').trim()
  const seen = new Set()
  for (const el of document.body.querySelectorAll('*')) {
    if (/^(SCRIPT|STYLE|SVG|svg|path|OPTION|INPUT|TEXTAREA|SELECT|CANVAS|VIDEO|IMG|IFRAME)$/.test(el.tagName)) continue
    const txt = ownText(el)
    if (!txt) continue
    const r = el.getBoundingClientRect()
    if (r.width === 0 || r.height === 0) continue
    const s = getComputedStyle(el)
    if (s.visibility === 'hidden' || +s.opacity === 0) continue
    // inside something that scrolls sideways (a row of chips, a carousel), or a map
    let a = el.parentElement, scroller = false, clipped = null
    while (a && a !== document.body) {
      if (scrolls(a) || a.classList.contains('maplibregl-map') || a.classList.contains('gm-style')) { scroller = true; break }
      const as = getComputedStyle(a)
      if (!clipped && /hidden|clip/.test(as.overflowX) && a.getBoundingClientRect().width < r.width + 1) clipped = a
      if (as.position === 'fixed') break
      a = a.parentElement
    }
    if (scroller) continue
    // a slide of a carousel waiting off to the side, or a scrolling notice
    if (el.closest('.marq')) continue
    let box = el.parentElement, off = false
    while (box && box !== document.body) {
      if (/hidden|clip/.test(getComputedStyle(box).overflowX)) {
        const cr = box.getBoundingClientRect()
        if (r.left >= cr.right - 1 || r.right <= cr.left + 1) { off = true; break }
      }
      box = box.parentElement
    }
    if (off) continue
    const short = txt.length > 40 ? txt.slice(0, 40) + '…' : txt
    const say = what => { const k = name(el) + what; if (!seen.has(k)) { seen.add(k); out.push(`${name(el)} "${short}": ${what}`) } }
    // past the screen edge
    if (r.right > vw + 1 || r.left < -1) { say(`runs past the screen edge (${Math.round(r.left)}–${Math.round(r.right)} of ${vw})`); continue }
    const ellipsis = s.textOverflow === 'ellipsis' || (s.webkitLineClamp && s.webkitLineClamp !== 'none')
    // its own text is wider than its box
    if (el.scrollWidth > el.clientWidth + 1 && el.clientWidth > 0 && s.display !== 'inline') {
      if (/hidden|clip/.test(s.overflowX)) { if (!ellipsis) say(`text cut off without … (${el.scrollWidth} in ${el.clientWidth})`) }
      else if (!scrolls(el)) say(`text spills out of its box (${el.scrollWidth} in ${el.clientWidth})`)
    }
    // taller than a clamped box without the …
    if (/hidden|clip/.test(s.overflowY) && el.scrollHeight > el.clientHeight + 2 && el.clientHeight > 0 && !ellipsis && !scrolls(el) && !/auto|scroll/.test(s.overflowY))
      say(`text cut off at the bottom (${el.scrollHeight} in ${el.clientHeight})`)
    // spills out of a parent that clips it
    if (clipped) {
      const cr = clipped.getBoundingClientRect()
      if ((r.right > cr.right + 1 || r.left < cr.left - 1) && !ellipsis) say(`hidden past the edge of ${name(clipped).split(' ').slice(-1)[0]}`)
    }
  }
  return out
}

async function look(page, what, size, lang, file) {
  // pages with live updates never go quiet: wait for the requests of the moment only
  await page.waitForLoadState('networkidle', { timeout: 1500 }).catch(() => {})
  await page.evaluate(() => document.fonts.ready)
  await page.waitForTimeout(250)
  const found = await page.evaluate(check)
  await page.screenshot({ path: `${SHOTS}/longtext/${lang}-${size}-${file}.png`, fullPage: FULL })
  for (const f of found) problems.push(`${lang} ${size} ${what}: ${f}`)
  if (found.length && !AUDIT) throw new Error(`${what} (${lang}, ${size}):\n  ` + found.join('\n  '))
}

// A customer with an order, and staff sessions, made once and reused.
async function staffSession(user) {
  const page = await open('pc', 'en')
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
  const s = await page.context().storageState()
  await page.context().close()
  return s
}
const setup = await open('h5', 'zh')
await setup.goto(BASE + '/s/makati')
await setup.waitForSelector('.item')
await setup.click('.item:has-text("冰柠檬茶") .stepper button.fill')
await setup.click('.item:has-text("瓶装水") .stepper button.fill')
await setup.click('.bottombar .btn.primary')
await customerLogin(setup, null, {})
await setup.waitForSelector('.totals')
await setup.click('.bottombar .btn.primary')
await setup.waitForURL(u => u.pathname.startsWith('/o/'))
const orderPath = new URL(setup.url()).pathname
const customer = await setup.context().storageState()
await setup.context().close()
const owner = await staffSession('owner1')
const rider = await staffSession('rider1')
step('a customer with an order, the store owner and a rider are signed in')

const customerPages = [
  ['mall', '/', '.mall'],
  ['store', '/s/makati', '.item'],
  ['orders', '/orders', `${tid('order-list')}[data-state=ready]`],
  ['order', orderPath, '.totals, .od'],
  ['me', '/me', 'body'],
  ['me-coupons', '/me/coupons', 'body'],
  ['me-addresses', '/me/addresses', 'body'],
  ['me-favorites', '/me/favorites', 'body'],
  ['me-messages', '/me/messages', 'body'],
  ['me-support', '/me/support', 'body'],
  ['me-apply', '/me/apply', 'body'],
  ['me-feedback', '/me/feedback', 'body'],
  ['me-help', '/me/help', 'body'],
  ['me-about', '/me/about', 'body'],
  ['me-profile', '/me/profile', 'body'],
  ['me-wallet', '/me/wallet/balance', 'body'],
  ['me-reviews', '/me/reviews', 'body'],
]
const staffPages = [
  ['desk-orders', '/merchant/orders', 'body'],
  ['desk-menu', '/merchant/menu', 'body'],
  ['desk-promos', '/merchant/promos', 'body'],
  ['desk-settings', '/merchant/settings', 'body'],
  ['desk-staff', '/merchant/staff', 'body'],
  ['desk-bills', '/merchant/bills', 'body'],
  ['desk-cash', '/merchant/cash', 'body'],
]

for (const lang of LANGS) {
  for (const size of Object.keys(SIZES)) {
    // signed out: the login page
    let p = await open(size, lang)
    await p.goto(BASE + '/account/login?store=makati&next=/s/makati/checkout')
    await p.waitForSelector(tid('login-phone'))
    await look(p, 'login', size, lang, 'login')
    await p.context().close()

    p = await open(size, lang, customer)
    for (const [what, path, ready] of customerPages) {
      await p.goto(BASE + path)
      await p.waitForSelector(ready).catch(() => {})
      await look(p, what, size, lang, what)
      if (what === 'store') {
        // a dish's own page, and the cart
        await p.locator('.item').first().click()
        await p.waitForTimeout(500)
        await look(p, 'dish', size, lang, 'dish')
        await p.keyboard.press('Escape')
        await p.goto(BASE + path); await p.waitForSelector('.item')
        // popcorn chicken has options (its name is translated: find it by price order)
        await p.locator('.item').nth(1).locator('button').last().click()
        await p.waitForTimeout(400)
        if (await p.locator('.sheet').count()) {
          await look(p, 'options', size, lang, 'options')
          await p.keyboard.press('Escape')
        }
        await p.locator('.item .stepper button.fill').first().click().catch(() => {})
        if (size !== 'pc') {
          await p.locator('.bottombar .cart, .bottombar [data-testid=cart-open], .bottombar .bag').first().click({ timeout: 2000 }).catch(() => {})
          await p.waitForTimeout(400)
        }
        await look(p, 'cart', size, lang, 'cart')
        await p.goto(BASE + '/s/makati/checkout')
        await p.waitForSelector('.co[data-ready]').catch(() => {})
        await look(p, 'checkout', size, lang, 'checkout')
      }
    }
    await p.context().close()

    p = await open(size, lang, owner)
    for (const [what, path, ready] of staffPages) {
      await p.goto(BASE + path)
      await p.waitForSelector(ready).catch(() => {})
      await look(p, what, size, lang, what)
    }
    await p.context().close()

    if (size !== 'pc') {
      p = await open(size, lang, rider)
      await p.goto(BASE + '/work')
      await look(p, 'workbench', size, lang, 'work')
      await p.context().close()
    }
    step(`${lang} at ${size}`)
  }
}

await browser.close()
if (problems.length) console.log('\n' + problems.join('\n'))
if (errors.length) { console.error(errors.join('\n')); process.exit(1) }
if (problems.length && !AUDIT) process.exit(1)
console.log(AUDIT ? `\n${problems.length} problems` : '\nlong texts fit everywhere')
})().catch(e => { console.error(e); process.exit(1) })
