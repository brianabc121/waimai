// Preloaded by `make e2e` (node -r ./e2e/patient-login.cjs): the API allows
// about 10 sign-ins a minute from one address, and the tests run back to back
// sign in far more often than that. Every browser context retries a sign-in
// (or a switch of identity, an approval code, pairing a table pad) that the
// API turned away with 429, after a pause, so no single test has to.
const pw = require('playwright')

// the routes the API counts against that limit (internal/httpapi, loginLimit)
const LIMITED = /\/api\/v1\/((admin\/)?auth\/login|auth\/(token|entry|approve-pin)|table-devices\/pair)$/
const wait = (ms) => new Promise(r => setTimeout(r, ms))
async function patient(ctx) {
  await ctx.route(LIMITED, async route => {
    for (let i = 0; ; i++) {
      const res = await route.fetch()
      if (res.status() !== 429 || i >= 8) return route.fulfill({ response: res })
      await wait(7000)
    }
  })
  return ctx
}

const launch = pw.chromium.launch.bind(pw.chromium)
pw.chromium.launch = async (...args) => {
  const browser = await launch(...args)
  const newContext = browser.newContext.bind(browser)
  browser.newContext = async (...a) => patient(await newContext(...a))
  browser.newPage = async (...a) => (await browser.newContext(...a)).newPage()
  return browser
}
