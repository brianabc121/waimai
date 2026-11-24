// Promo media in a real browser: the merchant uploads a picture, a GIF and a
// video, crops them, orders and hides them; the customer's ordering page plays
// them at the top, and shows nothing there once they are gone.
// The store "bgc" is used: the demo data gives it no promo media.
// Usage: BASE_URL=http://localhost:8080 node e2e/media.cjs   (needs seed data)
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
const step = (s) => console.log('✓ ' + s)
const shot = (page, name) => page.screenshot({ path: `${SHOTS}/${name}.png` })
const expect = (got, want, what) => { if (got !== want) throw new Error(`${what}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`) }
const tid = (id) => `[data-testid=${id}]`
const cards = (page) => page.locator(tid('media-card'))

const owner = await open({ width: 1280, height: 900 })
await owner.goto(BASE + '/login')
await owner.fill('input[autocomplete=username]', 'owner2')
await owner.fill('input[type=password]', 'demo1234')
await owner.click('button.primary')
await owner.waitForURL(u => !/\/login$/.test(u.pathname))

// ---- test files, made in the browser: a square picture, a small GIF, a short video ----
const files = await owner.evaluate(async () => {
  const b64 = async (blob) => { const b = new Uint8Array(await blob.arrayBuffer()); let s = ''; for (const x of b) s += String.fromCharCode(x); return btoa(s) }
  const c = document.createElement('canvas'); c.width = 1200; c.height = 1200
  const g = c.getContext('2d')
  const paint = (hue) => {
    const grad = g.createLinearGradient(0, 0, 1200, 1200); grad.addColorStop(0, `hsl(${hue} 80% 55%)`); grad.addColorStop(1, `hsl(${hue + 60} 80% 35%)`)
    g.fillStyle = grad; g.fillRect(0, 0, 1200, 1200)
    g.fillStyle = '#fff'; g.font = 'bold 150px sans-serif'; g.textAlign = 'center'; g.fillText('PROMO', 600, 640)
    g.strokeStyle = '#fff'; g.lineWidth = 16; g.strokeRect(100, 100, 1000, 1000)
  }
  paint(20)
  const png = await new Promise(r => c.toBlob(r, 'image/png'))
  // three seconds of video, the colour changing all the time
  const rec = new MediaRecorder(c.captureStream(15), { mimeType: 'video/webm;codecs=vp8' })
  const chunks = []; rec.ondataavailable = e => chunks.push(e.data)
  rec.start()
  const t0 = performance.now()
  await new Promise(done => { const tick = () => { const t = performance.now() - t0; paint(t / 10); t < 3000 ? requestAnimationFrame(tick) : done() }; tick() })
  await new Promise(r => { rec.onstop = r; rec.stop() })
  return { png: await b64(png), webm: await b64(new Blob(chunks)) }
})
// 16 × 16, two frames
const GIF = Buffer.from('R0lGODlhEAAQAPAAAP9mAAAAACH/C05FVFNDQVBFMi4wAwEAAAAh+QQACgAAACwAAAAAEAAQAAACDoSPqcvtD6OctNqLsz4FADs=', 'base64')
const pick = (name, mimeType, buffer) => owner.setInputFiles(tid('media-file'), { name, mimeType, buffer })
const cropOf = async () => (await owner.getAttribute(tid('crop-stage') + '.ready', 'data-crop')).split(',').map(Number)

// ---- the customer sees no promo area yet ----
await owner.goto(BASE + '/merchant/media')
await owner.waitForSelector(tid('media-upload'))
while (await cards(owner).count()) {   // left over from a run that stopped half way
  const n = await cards(owner).count()
  await cards(owner).first().locator(tid('media-delete')).click()
  await owner.waitForFunction(k => document.querySelectorAll('[data-testid=media-card]').length < k, n)
}
await owner.waitForSelector(tid('media-none'))
const phone = await open({ width: 390, height: 844 })
await phone.goto(BASE + '/s/bgc')
await phone.waitForSelector('.shophead')
expect(await phone.locator(tid('promo')).count(), 0, 'promo area of a store without media')
step('a store without promo media shows no promo area')

// ---- a picture: crop, zoom, move ----
await pick('dish.png', 'image/png', Buffer.from(files.png, 'base64'))
let c = await cropOf()
expect(JSON.stringify(c), JSON.stringify([0, 0.219, 1, 0.563]), 'first crop of a square picture')
await owner.fill(tid('crop-zoom'), '2')
c = await cropOf()
expect(c[2], 0.5, 'crop width at 2×')
const box = await owner.locator(tid('crop-stage')).boundingBox()
await owner.mouse.move(box.x + box.width / 2, box.y + box.height / 2)
await owner.mouse.down()
await owner.mouse.move(box.x + box.width / 2 - 2000, box.y + box.height / 2 - 2000, { steps: 4 })
await owner.mouse.up()
c = await cropOf()
expect(c[0], 0.5, 'dragged to the right edge'); expect(c[1], 0.719, 'dragged to the lower edge')
await owner.fill(tid('crop-title'), '招牌牛肉面')
await shot(owner, 'pm1-cropper')
await owner.click(tid('crop-save'))
await owner.waitForSelector(tid('cropper'), { state: 'detached' })
expect(await cards(owner).count(), 1, 'cards after the picture')
expect(await cards(owner).first().getAttribute('data-kind'), 'image', 'kind of the picture')
step('a picture is uploaded with the crop the merchant chose')

// ---- a GIF, and a file that is no picture ----
await pick('wave.gif', 'image/gif', GIF)
await owner.waitForSelector(tid('crop-stage') + '.ready')
await owner.click(tid('crop-save'))
await owner.waitForSelector(tid('cropper'), { state: 'detached' })
await owner.waitForFunction(() => document.querySelectorAll('[data-testid=media-card]').length === 2)
await pick('notes.txt', 'text/plain', Buffer.from('hello'))
await owner.waitForSelector(tid('media-error'))
step('a GIF is taken, a text file is refused')

// ---- a video: the part to play and the cover ----
await pick('kitchen.webm', 'video/webm', Buffer.from(files.webm, 'base64'))
await owner.waitForSelector(tid('crop-stage') + '.ready')
await owner.fill(tid('crop-seek'), '1')
await owner.waitForFunction(() => Math.abs(document.querySelector('[data-testid=crop-stage] video').currentTime - 1) < 0.1)
await owner.click(tid('trim-start'))
await owner.click(tid('cover-here'))
expect((await owner.innerText(tid('crop-trim'))).startsWith('0:01'), true, 'the video starts at one second')
await shot(owner, 'pm2-cropper-video')
await owner.click(tid('crop-save'))
await owner.waitForSelector(tid('cropper'), { state: 'detached' })
await owner.waitForFunction(() => document.querySelectorAll('[data-testid=media-card]').length === 3)
await owner.waitForSelector(`${tid('media-card')}[data-kind=video] img`)   // its cover
step('a video is uploaded with a start time and a cover')

// ---- order and hide: video first, GIF hidden ----
await cards(owner).nth(2).locator(tid('media-up')).click()
await owner.waitForFunction(() => document.querySelectorAll('[data-testid=media-card]')[1]?.dataset.kind === 'video')
await cards(owner).nth(1).locator(tid('media-up')).click()
await owner.waitForFunction(() => document.querySelectorAll('[data-testid=media-card]')[0]?.dataset.kind === 'video')
await owner.locator(`${tid('media-card')}[data-kind=gif] ${tid('media-toggle')}`).click()
await owner.waitForSelector(`${tid('media-card')}[data-kind=gif][data-active=false]`)
await shot(owner, 'pm3-merchant-list')
step('the merchant puts the video first and hides the GIF')

// ---- the customer: video first, then the picture ----
await phone.reload()
await phone.waitForSelector(tid('promo'))
expect(await phone.locator(`${tid('promo')} .slide`).count(), 2, 'slides the customer gets')
expect(await phone.innerText(tid('promo-label')), '门店视频 · 1/2', 'label of the first slide')
const style = await phone.locator(`${tid('promo')} .slide[data-kind=image] img`).evaluate(e => [e.style.width, e.style.left, e.style.top])
expect(style[0], '200%', 'the picture is shown at twice the frame')
expect(style[1], '-100%', 'the picture is moved to its right half')
// the frame is 16:9 over the full width of the phone
const frame = await phone.locator(tid('promo')).boundingBox()
expect(Math.round(frame.width / frame.height * 100), 178, 'shape of the promo area')
await phone.click(tid('promo-play'))
await phone.waitForFunction(() => { const v = document.querySelector('[data-testid=promo] video'); return !v.paused && v.currentTime >= 1 })
step('the video plays from the chosen start')
await phone.locator(tid('promo')).evaluate(e => e.querySelector('.track').scrollTo({ left: e.clientWidth }))
await phone.waitForSelector(`${tid('promo')}[data-index="1"]`)
await phone.waitForFunction(() => document.querySelector('[data-testid=promo] video').paused)
expect(await phone.innerText(tid('promo-label')), '招牌牛肉面 · 2/2', 'label of the second slide')
await shot(phone, 'pm4-customer-phone')
// it moves on by itself: back to the first after five seconds
await phone.mouse.move(200, 700)
await phone.waitForSelector(`${tid('promo')}[data-index="0"]`, { timeout: 9000 })
step('the customer swipes through the media, and they move on by themselves')

const pc = await open({ width: 1440, height: 900 })
await pc.goto(BASE + '/s/bgc')
await pc.waitForSelector(tid('promo'))
const wide = await pc.locator(tid('promo')).boundingBox(), card = await pc.locator('.shophead').boundingBox()
if (!(wide.x + wide.width <= card.x && wide.width <= 460)) throw new Error(`on a computer the media should sit beside the store card: ${JSON.stringify([wide, card])}`)
await shot(pc, 'pm5-customer-pc')
step('on a computer the media sit beside the store card')

// ---- gone again ----
while (await cards(owner).count()) {
  const n = await cards(owner).count()
  await cards(owner).first().locator(tid('media-delete')).click()
  await owner.waitForFunction(k => document.querySelectorAll('[data-testid=media-card]').length < k, n)
}
await phone.reload()
await phone.waitForSelector('.shophead')
expect(await phone.locator(tid('promo')).count(), 0, 'promo area after everything was deleted')
step('deleting everything takes the promo area away')

// ---- a dish picture and the store logo: uploaded and cropped the same way ----
await owner.goto(BASE + '/merchant/menu')
const row = owner.locator('tr', { has: owner.locator('b', { hasText: /^盐酥鸡$/ }) })
await row.locator('button', { hasText: '编辑' }).click()
await owner.waitForSelector(tid('item-form'))
const before = await owner.inputValue(`${tid('item-image')} input.link`)
await owner.setInputFiles(`${tid('item-image')} input[type=file]`, { name: 'dish.png', mimeType: 'image/png', buffer: Buffer.from(files.png, 'base64') })
c = await cropOf()
expect(JSON.stringify(c), JSON.stringify([0, 0, 1, 1]), 'a square picture fills the square frame of a dish')
await owner.fill(tid('crop-zoom'), '2')
await shot(owner, 'pm6-dish-crop')
await owner.click(tid('crop-save'))
await owner.waitForSelector(tid('cropper'), { state: 'detached' })
const dish = await owner.getAttribute(`${tid('item-image')} .pic img`, 'src')
expect(dish.startsWith('/api/v1/images/'), true, 'address of the uploaded dish picture')
await shot(owner, 'pm7-dish-form')
await owner.click(`${tid('item-form')} .sheet-foot .btn.primary`)
await owner.waitForSelector(tid('item-form'), { state: 'detached' })
await phone.reload()
await phone.waitForSelector(`img[src="${dish}"]`)
const size = await phone.locator(`img[src="${dish}"]`).first().evaluate(e => e.decode().then(() => [e.naturalWidth, e.naturalHeight]))
expect(JSON.stringify(size), JSON.stringify([600, 600]), 'the stored picture is the cropped square (half of 1200)')
// put the demo picture back
await row.locator('button', { hasText: '编辑' }).click()
await owner.fill(`${tid('item-image')} input.link`, before)
await owner.locator(`${tid('item-image')} input.link`).blur()
await owner.click(`${tid('item-form')} .sheet-foot .btn.primary`)
await owner.waitForSelector(tid('item-form'), { state: 'detached' })
step('a dish picture is uploaded, cropped square and shown to customers')

await owner.goto(BASE + '/merchant/templates')
await owner.setInputFiles(`${tid('tpl-cover')} input[type=file]`, { name: 'cover.png', mimeType: 'image/png', buffer: Buffer.from(files.png, 'base64') })
c = await cropOf()
expect(c[3], 0.563, 'a cover is a 16:9 band, the shape of the promo area')
await owner.click(tid('crop-save'))
await owner.waitForSelector(tid('cropper'), { state: 'detached' })
await owner.waitForSelector('[data-test=preview] img.cover[src^="/api/v1/images/"]')
await shot(owner, 'pm8-template-cover')
step('the logo and the cover of a template are uploaded the same way (left unsaved)')

await browser.close()
if (errors.length) { console.error(errors.join('\n')); process.exit(1) }
console.log('promo media: all steps passed')

})().catch(e => { console.error(e); process.exit(1) })
