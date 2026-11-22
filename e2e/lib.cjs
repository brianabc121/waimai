// Shared by the browser tests.

// customerLogin is called right after pressing "checkout". A customer who is
// not logged in lands on the login page: this logs in with a fresh number (the
// code fills itself in when the API runs with OTP_DEV_ECHO=true) and waits for
// the checkout page. Someone already logged in goes straight to checkout.
async function customerLogin(page, phone) {
  await page.waitForURL(u => u.pathname === '/account/login' || u.pathname.endsWith('/checkout'))
  if (new URL(page.url()).pathname === '/account/login') {
    await page.fill('[data-testid=login-phone]', phone || '9' + String(Math.floor(Math.random() * 1e9)).padStart(9, '0'))
    await page.check('[data-testid=login-agree]')
    await page.click('[data-testid=login-send]')
  }
  await page.waitForURL(u => u.pathname.endsWith('/checkout'))
}

module.exports = { customerLogin }
