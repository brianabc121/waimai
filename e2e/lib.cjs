// Shared by the browser tests.

// customerLogin is called right after pressing "checkout". A customer who is
// not logged in lands on the login page: this logs in with a fresh number (the
// code fills itself in when the API runs with OTP_DEV_ECHO=true) and waits for
// the checkout page. Someone already logged in goes straight to checkout.
// A delivery goes to a saved address; a customer who has none is shown the
// form for a new one, which is filled in here (address: { name, unit, at }).
async function customerLogin(page, phone, address) {
  await page.waitForURL(u => u.pathname === '/account/login' || u.pathname.endsWith('/checkout'))
  if (new URL(page.url()).pathname === '/account/login') {
    await page.fill('[data-testid=login-phone]', phone || '9' + String(Math.floor(Math.random() * 1e9)).padStart(9, '0'))
    await page.check('[data-testid=login-agree]')
    await page.click('[data-testid=login-send]')
  }
  await page.waitForURL(u => u.pathname.endsWith('/checkout'))
  const form = page.locator('[data-testid=address-form]')
  await form.or(page.locator('.totals')).first().waitFor()
  if (await form.isVisible()) {
    await addAddress(page, address || {})
    await page.waitForSelector('.totals')
  }
}

// addAddress fills the open "new address" form and saves it. at is "lat, lng"
// for the pin; without it the pin stays where the map opened.
async function addAddress(page, { name = '李小红', unit = '12F', at } = {}) {
  const tid = (id) => `[data-testid=${id}]`
  await page.waitForSelector(tid('address-form'))
  await page.fill(tid('af-name'), name)
  const place = page.locator(tid('af-place')), street = page.locator(tid('af-street'))
  await place.or(street).first().waitFor()
  if (await street.count()) await street.fill('12 Paseo de Roxas')
  else {
    // beside the form on a wide screen, over it on a phone
    if (!(await page.locator(tid('pin-confirm')).isVisible())) await place.click()
    if (at) {
      await page.fill(tid('pin-search'), at)
      await page.click(`${tid('pin-place')}:has-text("${at}")`)   // the suggestion, not what was listed before
    }
    await page.click(tid('pin-confirm') + ':not([disabled])')
    await page.waitForSelector(tid('af-place') + ' b')
  }
  await page.fill(tid('af-unit'), unit)
  await page.click(tid('af-save'))
  await page.waitForSelector(tid('address-form'), { state: 'detached' })
}

module.exports = { customerLogin, addAddress }
