// Shared by the browser tests.

// customerLogin is called right after pressing "checkout". A customer who is
// not logged in lands on the login page: this logs in with a fresh number (the
// code fills itself in when the API runs with OTP_DEV_ECHO=true) and waits for
// the checkout page. Someone already logged in goes straight to checkout.
// A delivery goes to a saved address; a customer who has none adds one, which
// is filled in here (address: { name, unit, at }). { pickup: true } chooses
// "pick up at the store" on the checkout page instead.
async function customerLogin(page, phone, address) {
  await page.waitForURL(u => u.pathname === '/account/login' || u.pathname.endsWith('/checkout'))
  if (new URL(page.url()).pathname === '/account/login') {
    await page.fill('[data-testid=login-phone]', phone || '9' + String(Math.floor(Math.random() * 1e9)).padStart(9, '0'))
    await page.check('[data-testid=login-agree]')
    await page.click('[data-testid=login-send]')
  }
  await page.waitForURL(u => u.pathname.endsWith('/checkout'))
  await page.waitForSelector('.co[data-ready]')
  if (address && address.pickup) {
    if (!(await page.locator('[data-testid=checkout-pickup]').count())) await page.click('[data-testid=service-pickup]')
    await page.waitForSelector('[data-testid=checkout-pickup]')
    return
  }
  // The form opens by itself where delivery is all the store offers; otherwise
  // "add a delivery address" on the page leads to it.
  const form = page.locator('[data-testid=address-form]'), empty = page.locator('[data-testid=checkout-address][data-empty]')
  if (await form.isVisible() || await empty.count()) {
    if (!(await form.isVisible())) await empty.click()
    await addAddress(page, address || {})
    await page.waitForSelector('.totals')
  }
  await pickZone(page)
}

// A store whose customers pick their delivery area by name asks for it on the
// checkout page: take the first one, unless one is chosen already.
async function pickZone(page) {
  const zone = page.locator('[data-testid=checkout-zone]')
  if (!(await zone.count()) || await zone.evaluate(el => el.selectedIndex > 0)) return
  await zone.selectOption({ index: 1 })
  await page.waitForSelector('[data-testid=delivery-fee]')
}

// addAddress fills the open "new address" form and saves it. at is "lat, lng"
// for the pin; without it the pin stays where the map opened. photo: true adds
// a (tiny) photo of the door.
const DOT = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==', 'base64')
async function addAddress(page, { name = '李小红', unit = '12F', at, wechat, photo } = {}) {
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
  if (wechat) await page.fill(tid('af-wechat'), wechat)
  if (photo) {
    await page.setInputFiles(tid('af-photo') + ' input[type=file]', { name: 'door.png', mimeType: 'image/png', buffer: DOT })
    await page.waitForSelector(tid('af-photo-img'))
  }
  await page.click(tid('af-save'))
  await page.waitForSelector(tid('address-form'), { state: 'detached' })
}

module.exports = { customerLogin, addAddress, pickZone }
