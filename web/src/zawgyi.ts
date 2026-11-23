// Burmese typed in the old Zawgyi font encoding (docs/I18N_DESIGN.md §12).
// The system keeps Burmese in Unicode only; Zawgyi text looks right on the
// writer's phone but turns into nonsense for everyone else. When a merchant or
// the platform saves Burmese, Google's myanmar-tools model checks each text and
// Zawgyi is converted to Unicode before it is sent.
const burmese = /[က-႟]/

// Above this the model is sure the text is Zawgyi (Google's recommended cut-off).
const sure = 0.95

type Tools = { detect: (s: string) => number, convert: (s: string) => string }
let tools: Promise<Tools> | null = null

// The package is built for Node: the detector decodes its model with
// new Buffer(base64) when it is created. In the browser a stand-in that decodes
// with atob is put in place for that moment only.
function withBuffer<T>(make: () => T): T {
  const g = globalThis as any
  if (g.Buffer) return make()
  g.Buffer = function (b64: string) { return Uint8Array.from(atob(b64), ch => ch.charCodeAt(0)) }
  try { return make() } finally { delete g.Buffer }
}

// The model is about 90 KB, so it loads only when a write carries Burmese.
function load(): Promise<Tools> {
  return tools ??= import('myanmar-tools').then(mod => {
    const m = (mod as any).default ?? mod // CommonJS package
    const d = withBuffer(() => new m.ZawgyiDetector()), c = new m.ZawgyiConverter()
    return { detect: (s: string) => d.getZawgyiProbability(s), convert: (s: string) => c.zawgyiToUnicode(s) }
  })
}

function hasBurmese(body: unknown): boolean {
  if (typeof body === 'string') return burmese.test(body)
  if (Array.isArray(body)) return body.some(hasBurmese)
  if (body && typeof body === 'object') return Object.values(body).some(hasBurmese)
  return false
}

function walk(body: unknown, t: Tools, found: string[]): unknown {
  if (typeof body === 'string') {
    if (!burmese.test(body) || t.detect(body) < sure) return body
    const u = t.convert(body)
    if (u !== body) found.push(u)
    return u
  }
  if (Array.isArray(body)) return body.map(x => walk(x, t, found))
  if (body && typeof body === 'object') {
    const out: Record<string, unknown> = {}
    for (const [k, v] of Object.entries(body)) out[k] = walk(v, t, found)
    return out
  }
  return body
}

// toUnicode returns the body with every Zawgyi text converted to Unicode, and
// the converted texts. Bodies without Burmese come back as they are.
export async function toUnicode<T>(body: T): Promise<{ body: T, converted: string[] }> {
  if (!hasBurmese(body)) return { body, converted: [] }
  const converted: string[] = []
  const out = walk(body, await load(), converted) as T
  return { body: converted.length ? out : body, converted }
}

// fixZawgyi converts what a merchant or the platform saves. Customers' own
// texts (notes, chat) are left as typed.
export async function fixZawgyi<T>(path: string, body: T): Promise<{ body: T, converted: string[] }> {
  if (!/^\/(merchant|admin)\//.test(path)) return { body, converted: [] }
  try {
    return await toUnicode(body)
  } catch {
    return { body, converted: [] } // model failed to load: save as typed
  }
}

// notice shows a short message at the bottom of the page for a few seconds.
export function notice(text: string) {
  const el = document.createElement('div')
  el.className = 'zawgyi-notice'
  el.setAttribute('role', 'status')
  el.dataset.testid = 'zawgyi-notice'
  el.textContent = text
  document.body.appendChild(el)
  setTimeout(() => el.remove(), 6000)
}
