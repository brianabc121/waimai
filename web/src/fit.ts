// Short texts that would not fit get a smaller font first (docs/I18N_DESIGN.md
// §15): a button, tag, tab, link in a bar or one-line heading whose text runs
// onto a second line, or is cut off with "…", is made smaller step by step,
// down to three quarters of its size and never under 11px, until it fits on
// one line. If even the smallest size does not fit, it stays at the smallest
// and wraps or ends in "…", as styles.css lets it. Running text (descriptions, notices, messages) keeps its
// size. Like iOS's "adjusts font size to fit width".
//
// One observer watches the page; an element is measured again only when its
// text or the room it has changes. data-fit on any element opts it in;
// data-nofit opts one out.

const SELECTOR = [
  '.btn', '.tag', '.pill', '.lang-btn', '.clamp1', '[data-fit]',
  // tabs and bars
  '.subtabs button', '.shoptabs button', '.ol-tabs button', '.sorts button', '.mtabs button',
  '.cnav .links a', '.mlinks .txt', '.ctabs a', '.staffbar a',
  // small labels: an order's state, a store's services and offers
  '.ol-status', '.chips span',
].join(',')
const MIN_PX = 11
const MIN_SCALE = 0.75

interface Seen { text: string; room: number }
const seen = new WeakMap<HTMLElement, Seen>()
// a size the page itself gave the element inline, put back before measuring
const own = new WeakMap<HTMLElement, string>()

// wraps is true when a piece of the element's text runs onto a second line.
// Each text is looked at on its own, so a button laid out in two rows on
// purpose (a price above a note) is not taken for one that wraps.
function wraps(el: HTMLElement): boolean {
  const r = document.createRange()
  const walk = document.createTreeWalker(el, NodeFilter.SHOW_TEXT)
  for (let n = walk.nextNode(); n; n = walk.nextNode()) {
    if (!n.textContent?.trim()) continue
    r.selectNodeContents(n)
    let last: DOMRect | null = null
    for (const b of r.getClientRects()) {
      if (b.width < 0.5 || b.height < 0.5) continue
      if (last && b.top > last.top + last.height * 0.6) return true
      last = b
    }
  }
  return false
}
// cut is true when one-line text is cut off ("…").
const cut = (el: HTMLElement, s: CSSStyleDeclaration) =>
  /nowrap|pre/.test(s.whiteSpace) && /hidden|clip/.test(s.overflowX) && el.scrollWidth > el.clientWidth + 1
const fits = (el: HTMLElement) => {
  const s = getComputedStyle(el)
  return !cut(el, s) && !wraps(el)
}

function fit(el: HTMLElement) {
  const text = el.textContent?.trim() ?? ''
  const room = el.parentElement?.clientWidth ?? 0
  const was = seen.get(el)
  if (was && was.text === text && was.room === room) return
  if (!own.has(el)) own.set(el, el.style.fontSize)
  el.style.fontSize = own.get(el)!
  if (!text || el.offsetParent === null && getComputedStyle(el).position !== 'fixed') { seen.delete(el); return }
  const base = parseFloat(getComputedStyle(el).fontSize) || 15
  seen.set(el, { text, room })
  if (fits(el)) return
  const floor = Math.max(MIN_PX, base * MIN_SCALE)
  if (floor >= base) return
  // halve the gap between too big and small enough a few times
  let lo = floor, hi = base
  el.style.fontSize = floor + 'px'
  // even the smallest does not fit: it stays at the smallest and wraps or
  // ends in "…" there (more of it shows, in fewer lines)
  if (!fits(el)) return
  for (let i = 0; i < 5 && hi - lo > 0.5; i++) {
    const mid = (lo + hi) / 2
    el.style.fontSize = mid + 'px'
    if (fits(el)) lo = mid; else hi = mid
  }
  el.style.fontSize = Math.floor(lo * 2) / 2 + 'px'
}

let queued = false
function run() {
  queued = false
  for (const el of document.querySelectorAll<HTMLElement>(SELECTOR)) {
    if (el.closest('[data-nofit]')) continue
    fit(el)
  }
}
function soon() {
  if (queued) return
  queued = true
  requestAnimationFrame(run)
}

export function fitTexts(root: HTMLElement) {
  // the whole body: the language picker and some dialogs live outside the app
  void root
  new MutationObserver(soon).observe(document.body, { childList: true, subtree: true, characterData: true })
  window.addEventListener('resize', soon)
  void document.fonts?.ready.then(soon)
  soon()
}
