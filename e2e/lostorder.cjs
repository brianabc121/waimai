// The answer to "place order" is lost on the way back (weak signal): the
// server made the order, the page never heard. Pressing again, or reloading
// the page, must find THAT order and not make a second one
// (docs/CODE_REVIEW_FIX_PLAN.md H09).
// Usage: BASE_URL=http://localhost:8080 node e2e/lostorder.cjs   (needs seed data and OTP_DEV_ECHO=true)
const { chromium } = require('playwright')
const { customerLogin } = require('./lib.cjs')

;(async () => {
const BASE = process.env.BASE_URL || 'http://localhost:8080'
const browser = await chromium.launch()
const step = (s) => console.log('✓ ' + s)

// one customer per case, so that "how many orders do I have" is exact
async function toCheckout() {
  const ctx = await browser.newContext({ viewport: { width: 390, height: 844 }, locale: 'zh-CN', ignoreHTTPSErrors: process.env.E2E_INSECURE_TLS === '1' })
  const page = await ctx.newPage()
  await page.goto(BASE + '/s/makati')
  await page.waitForSelector('.item')
  await page.click('.item:has-text("瓶装水") .stepper button.fill')
  await page.click('.bottombar .btn.primary')
  await customerLogin(page, null, { name: '弱网' })
  await page.waitForSelector('.totals')
  // the server answers, the answer never arrives
  let lost = 0
  await page.route('**/api/v1/stores/makati/orders', async route => {
    if (route.request().method() !== 'POST' || lost > 0) return route.continue()
    lost++
    await route.fetch() // the order is made
    await route.abort('connectionreset')
  })
  return { ctx, page, lost: () => lost }
}
const orders = async (page) => (await (await page.request.get(BASE + '/api/v1/customer/orders')).json()).orders ?? []
const place = '[data-testid=place-order]'

// 1. press again
{
  const { ctx, page, lost } = await toCheckout()
  await page.click(place)
  await page.waitForSelector('.banner.bad')
  if (lost() !== 1 || !/\/checkout$/.test(new URL(page.url()).pathname)) throw new Error('the lost answer should leave the customer on the checkout page')
  if ((await orders(page)).length !== 1) throw new Error('the server should have made the order')
  await page.waitForFunction(sel => { const b = document.querySelector(sel); return b && !b.disabled }, place)
  await page.click(place)
  await page.waitForURL('**/o/**')
  await page.waitForSelector('[data-testid=status]')
  const list = await orders(page)
  if (list.length !== 1) throw new Error(`pressing again made ${list.length} orders`)
  step('answer lost, pressed again: the page went to the one order that exists')
  await ctx.close()
}

// 2. reload the page instead
{
  const { ctx, page } = await toCheckout()
  await page.click(place)
  await page.waitForSelector('.banner.bad')
  await page.reload()
  await page.waitForURL('**/o/**', { timeout: 15000 })
  await page.waitForSelector('[data-testid=status]')
  const list = await orders(page)
  if (list.length !== 1) throw new Error(`after a reload there are ${list.length} orders`)
  step('answer lost, page reloaded: it found the order by itself')
  await ctx.close()
}

await browser.close()
console.log('E2E OK')
})().catch(e => { console.error(e); process.exit(1) })
