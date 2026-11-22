// The map itself, behind a small interface. Zones, pins and handles are drawn
// by MapCanvas.vue in its own SVG layer, positioned with the web-mercator
// maths in geo.ts, so an adapter only has to show tiles, pan, zoom, report
// clicks, and hold that layer so that it moves with the map. Two adapters:
//   google - Google Maps JavaScript API, when GOOGLE_MAPS_API_KEY is a real key
//   stub   - a tile-less grid, when the key is the word "stub" (tests, demos)
import { fromScreen, toScreen, world, type LatLng, type Pt, type View } from './geo'
import { get } from './api'

// A place offered to the customer. `at` is missing on a typed suggestion until
// it is chosen: finding it costs a lookup, so `locate` fetches it on demand.
export interface Place { label: string; detail?: string; at?: LatLng; distance_m?: number; locate?: () => Promise<LatLng | null> }
// A place whose position is known (every search result).
export interface Spot extends Place { at: LatLng }

// Where a point is, in words. country_code is ISO 3166-1 alpha-2 ("PH").
export interface Region { country_code: string; country: string; province: string; city: string; district: string; postal_code: string }
// What typed searches are kept to: one country, and near one point first.
export interface Scope { country?: string; near?: LatLng }

// regionOf reads a geocoder's answer for a point (most exact result first).
function regionOf(results: any[]): Region | null {
  const part = (...types: string[]) => {
    for (const ty of types) for (const r of results) for (const c of r.address_components ?? []) if (c.types?.includes(ty)) return c
    return null
  }
  const country = part('country')
  if (!country) return null
  const name = (...types: string[]) => part(...types)?.long_name ?? ''
  return {
    country_code: String(country.short_name ?? '').toUpperCase(), country: country.long_name ?? '',
    province: name('administrative_area_level_1'),
    city: name('locality', 'postal_town', 'administrative_area_level_2'),
    district: name('sublocality_level_1', 'sublocality', 'neighborhood', 'administrative_area_level_3'),
    postal_code: name('postal_code'),
  }
}

// storeScope is the scope for a customer of one store. A store whose country
// was never read off the map (saved before that was done) is looked up once.
const countries = new Map<string, string>()
export async function storeScope(shop: { lat: number; lng: number; country?: string }, resolve: (at: LatLng) => Promise<Region | null>): Promise<Scope> {
  const near: LatLng = [shop.lat, shop.lng], k = near.join()
  let country = shop.country || countries.get(k) || ''
  if (!country) {
    country = (await resolve(near).catch(() => null))?.country_code ?? ''
    if (country) countries.set(k, country)
  }
  return { country: country || undefined, near }
}

function metresBetween(a: LatLng, b: LatLng): number {
  const rad = Math.PI / 180, dLat = (b[0] - a[0]) * rad, dLng = (b[1] - a[1]) * rad
  const h = Math.sin(dLat / 2) ** 2 + Math.cos(a[0] * rad) * Math.cos(b[0] * rad) * Math.sin(dLng / 2) ** 2
  return 2 * 6371000 * Math.asin(Math.sqrt(h))
}

// Where the drawing layer stands: a position is drawn at
// world(position, zoom) - (ox, oy), in pixels of the layer. zoom is fractional
// while the map animates between two levels.
export interface Layer { zoom: number; ox: number; oy: number }
export function layerOf(v: View, size: { w: number; h: number }): Layer {
  const c = world(v.center, v.zoom)
  return { zoom: v.zoom, ox: c.x - size.w / 2, oy: c.y - size.h / 2 }
}

// Mouse pointers. The hot spot is the pen's tip and the pin's foot, so a click
// lands exactly there; the keyword after it is for browsers without image cursors.
const svgCursor = (body: string, x: number, y: number, fallback: string) =>
  `url("data:image/svg+xml,${encodeURIComponent(`<svg xmlns='http://www.w3.org/2000/svg' width='32' height='32' viewBox='0 0 32 32'>${body}</svg>`)}") ${x} ${y}, ${fallback}`
export const CURSORS: Record<string, string> = {
  pen: svgCursor("<path d='M3 29l2.2-8L21.5 4.7a2.6 2.6 0 0 1 3.7 0l2.1 2.1a2.6 2.6 0 0 1 0 3.7L11 26.8z' fill='#fff' stroke='#1f2328' stroke-width='1.6' stroke-linejoin='round'/><path d='M18.6 7.6l5.8 5.8' stroke='#1f2328' stroke-width='1.6'/><path d='M3 29l2.2-8 5.8 5.8z' fill='#1f2328'/>", 3, 29, 'crosshair'),
  pin: svgCursor("<path d='M16 30C12 20 6 17 6 11a10 10 0 1 1 20 0c0 6-6 9-10 19z' fill='#d9480f' stroke='#fff' stroke-width='1.6'/><circle cx='16' cy='11' r='3.6' fill='#fff'/>", 16, 30, 'crosshair'),
  pointer: 'pointer',
}

export interface MapEvents {
  // view fires whenever the view or the layer's placement changed.
  view(): void
  click(at: LatLng): void
  dblclick(at: LatLng): void
  move(at: LatLng): void
}
export interface MapAdapter {
  kind: 'google' | 'stub'
  view(): View
  setView(v: View): void
  setType(t: 'roadmap' | 'satellite'): void
  // mount takes the drawing layer into the map, so that it pans and zooms as
  // one with the tiles; layer says where it stands now.
  mount(el: HTMLElement | SVGElement): void
  layer(): Layer
  // toLatLng converts a pixel of the map's box to a position, as shown now.
  toLatLng(p: Pt): LatLng
  // setCursor shows a pointer over the map ('' restores the map's own).
  setCursor(css: string): void
  // lock stops the map from panning while a handle on the layer is dragged.
  lock(on: boolean): void
  search(q: string): Promise<Spot[]>
  // suggest offers places while the customer types; nearby lists what stands
  // around a point, nearest first.
  suggest(q: string): Promise<Place[]>
  nearby(at: LatLng): Promise<Place[]>
  // scope keeps search and suggest to one country and puts places near a point
  // first (a customer's address is in the store's country, near the store);
  // null lifts it. resolve says where a point is, in words.
  scope(s: Scope | null): void
  resolve(at: LatLng): Promise<Region | null>
  destroy(): void
}

let keyPromise: Promise<string> | null = null
// mapsKey is '' when the platform has no map key: map features stay off.
export function mapsKey(): Promise<string> {
  keyPromise ??= get<{ maps_key: string }>('/config').then(c => c.maps_key || '', () => '')
  return keyPromise
}

// "14.5547, 121.0244" typed into a search box goes straight to that point.
export function parseLatLng(q: string): LatLng | null {
  const m = q.trim().match(/^(-?\d{1,2}(?:\.\d+)?)\s*[,，\s]\s*(-?\d{1,3}(?:\.\d+)?)$/)
  if (!m) return null
  const lat = Number(m[1]), lng = Number(m[2])
  return Math.abs(lat) <= 90 && Math.abs(lng) <= 180 ? [lat, lng] : null
}

// placeName names a point where there is no map on the page: the street or
// building the geocoder knows there. '' when it knows nothing.
export async function placeName(key: string, at: LatLng): Promise<string> {
  if (!key) return ''
  if (key === 'stub') return `Stub St ${at[0].toFixed(4)}, ${at[1].toFixed(4)}`
  try {
    const gm = await loadGoogle(key)
    const { results } = await new gm.Geocoder().geocode({ location: { lat: at[0], lng: at[1] } })
    const list: any[] = results ?? []
    // A named building reads better than a plus code or a bare street number.
    const best = list.find(r => (r.types ?? []).some((ty: string) => ['premise', 'point_of_interest', 'establishment', 'subpremise'].includes(ty)))
      ?? list.find(r => !(r.types ?? []).includes('plus_code')) ?? list[0]
    return best ? String(best.formatted_address ?? '').split(',').slice(0, 2).join(',').trim() : ''
  } catch { return '' }
}

export function createMap(key: string, el: HTMLElement, view: View, ev: MapEvents): Promise<MapAdapter> {
  return key === 'stub' ? Promise.resolve(stubMap(el, view, ev)) : googleMap(key, el, view, ev)
}

// ---- Google ----

declare global { interface Window { google?: any; __waimaiMapsReady?: () => void; gm_authFailure?: () => void } }

let loading: Promise<any> | null = null
function loadGoogle(key: string): Promise<any> {
  if (window.google?.maps?.Map) return Promise.resolve(window.google.maps)
  loading ??= new Promise((resolve, reject) => {
    window.__waimaiMapsReady = () => resolve(window.google.maps)
    // Google calls this when the key is rejected (wrong key, referrer not allowed, billing off).
    window.gm_authFailure = () => reject(new Error('MAPS_AUTH'))
    const s = document.createElement('script')
    s.src = 'https://maps.googleapis.com/maps/api/js?v=weekly&loading=async&callback=__waimaiMapsReady&key=' + encodeURIComponent(key)
    s.async = true
    s.onerror = () => { loading = null; reject(new Error('MAPS_LOAD')) }
    document.head.appendChild(s)
  })
  return loading
}

async function googleMap(key: string, el: HTMLElement, view: View, ev: MapEvents): Promise<MapAdapter> {
  const gm = await loadGoogle(key)
  const map = new gm.Map(el, {
    center: { lat: view.center[0], lng: view.center[1] }, zoom: view.zoom,
    // The drawing layer assumes a north-up web mercator map.
    isFractionalZoomEnabled: false, tilt: 0, heading: 0, rotateControl: false,
    disableDoubleClickZoom: true, clickableIcons: false, gestureHandling: 'greedy',
    streetViewControl: false, fullscreenControl: false, mapTypeControl: false, zoomControl: true,
  })
  const ll = (e: any): LatLng => [e.latLng.lat(), e.latLng.lng()]
  let session: any = null, scope: Scope | null = null
  const now = (): View => {
    const c = map.getCenter()
    return c ? { center: [c.lat(), c.lng()], zoom: map.getZoom() ?? view.zoom } : view
  }
  const box = () => ({ w: el.clientWidth, h: el.clientHeight })

  // The drawing layer lives in one of the map's own panes (Google's "custom
  // overlay"). Google moves the panes with the tiles while the map is dragged
  // and calls draw() whenever positions change, during a zoom too, so the
  // layer does not trail behind the map.
  let layer: Layer = layerOf(view, box()), host: HTMLElement | null = null, mounted: Element | null = null
  const overlay = new gm.OverlayView()
  overlay.onAdd = () => {
    host = document.createElement('div')
    host.className = 'mapc-host'
    // overlayMouseTarget is the pane that receives pointer events, which the
    // handles on the layer need.
    overlay.getPanes().overlayMouseTarget.appendChild(host)
    if (mounted) host.appendChild(mounted)
  }
  overlay.draw = () => {
    const proj = overlay.getProjection(), c = map.getCenter()
    if (!proj || !c) return
    // Two points one hundredth of a degree apart tell the scale, and so the
    // zoom the map shows at this instant.
    const a = proj.fromLatLngToDivPixel(c), b = proj.fromLatLngToDivPixel(new gm.LatLng(c.lat(), c.lng() + 0.01))
    if (!a || !b || !(b.x - a.x > 0)) return
    const zoom = Math.log2((b.x - a.x) * 36000 / 256), w = world([c.lat(), c.lng()], zoom)
    layer = { zoom, ox: w.x - a.x, oy: w.y - a.y }
    ev.view()
  }
  overlay.onRemove = () => { host?.remove(); host = null }
  overlay.setMap(map)

  // A click on something drawn on the layer belongs to the layer, not to the map.
  const onLayer = (e: any) => !!e.domEvent?.target?.closest?.('.mapc-layer')
  const subs = [
    map.addListener('bounds_changed', () => ev.view()),
    map.addListener('click', (e: any) => e.latLng && !onLayer(e) && ev.click(ll(e))),
    map.addListener('dblclick', (e: any) => e.latLng && !onLayer(e) && ev.dblclick(ll(e))),
    map.addListener('mousemove', (e: any) => e.latLng && ev.move(ll(e))),
  ]
  return {
    kind: 'google',
    view: now,
    setView(v) { map.setZoom(Math.round(v.zoom)); map.setCenter({ lat: v.center[0], lng: v.center[1] }) },
    setType(t) { map.setMapTypeId(t) },
    mount(e) { mounted = e; host?.appendChild(e) },
    layer: () => layer,
    toLatLng(p) {
      const l = overlay.getProjection()?.fromContainerPixelToLatLng(new gm.Point(p.x, p.y))
      return l ? [l.lat(), l.lng()] : fromScreen(p, now(), box())
    },
    // The pointer over a Google map is the map's to set, not the page's.
    setCursor(css) { map.setOptions({ draggableCursor: css || null }) },
    lock(on) { map.setOptions({ gestureHandling: on ? 'none' : 'greedy' }) },
    async search(q) {
      const direct = parseLatLng(q)
      if (direct) return [{ label: q.trim(), at: direct }]
      try {
        const req: any = { address: q, bounds: map.getBounds() }
        if (scope?.country) { req.componentRestrictions = { country: scope.country }; req.region = scope.country.toLowerCase() }
        const { results } = await new gm.Geocoder().geocode(req)
        return (results ?? []).slice(0, 5).map((r: any) => ({ label: r.formatted_address, at: [r.geometry.location.lat(), r.geometry.location.lng()] as LatLng }))
      } catch { return [] } // ZERO_RESULTS and friends arrive as rejections
    },
    // Needs "Places API (New)" on the same key. Without it both fall back to
    // the geocoder: whole-address search, and one street address for a point.
    async suggest(q) {
      const direct = parseLatLng(q)
      if (direct) return [{ label: q.trim(), at: direct }]
      try {
        const lib = await gm.importLibrary('places')
        session ??= new lib.AutocompleteSessionToken()
        const c = map.getCenter(), near = scope?.near ?? [c.lat(), c.lng()]
        const req: any = { input: q, sessionToken: session, locationBias: { center: { lat: near[0], lng: near[1] }, radius: scope?.near ? 50000 : 20000 } }
        // Without this "trump tower" typed in Manila finds the one in New York.
        if (scope?.country) { req.includedRegionCodes = [scope.country.toLowerCase()]; req.region = scope.country.toLowerCase() }
        const { suggestions } = await lib.AutocompleteSuggestion.fetchAutocompleteSuggestions(req)
        return (suggestions ?? []).filter((s: any) => s.placePrediction).slice(0, 6).map((s: any): Place => {
          const p = s.placePrediction
          return {
            label: p.mainText?.text ?? p.text?.text ?? '', detail: p.secondaryText?.text ?? '',
            async locate() {
              try {
                const place = p.toPlace()
                await place.fetchFields({ fields: ['location'] })
                session = null // one billing session ends when a suggestion is chosen
                return place.location ? [place.location.lat(), place.location.lng()] : null
              } catch { return null }
            },
          }
        })
      } catch { return (this as MapAdapter).search(q) }
    },
    async nearby(at) {
      const out: Place[] = []
      try {
        const lib = await gm.importLibrary('places')
        const { places } = await lib.Place.searchNearby({
          fields: ['displayName', 'formattedAddress', 'location'], maxResultCount: 5,
          locationRestriction: { center: { lat: at[0], lng: at[1] }, radius: 150 },
          rankPreference: lib.SearchNearbyRankPreference?.DISTANCE,
        })
        for (const p of places ?? []) {
          if (!p.location || !p.displayName) continue
          const where: LatLng = [p.location.lat(), p.location.lng()]
          out.push({ label: p.displayName, detail: p.formattedAddress ?? '', at: where, distance_m: Math.round(metresBetween(at, where)) })
        }
      } catch { /* Places is not enabled: the street address below still helps */ }
      try {
        const { results } = await new gm.Geocoder().geocode({ location: { lat: at[0], lng: at[1] } })
        const r = (results ?? [])[0]
        if (r) out.push({ label: r.formatted_address, detail: '', at, distance_m: 0 })
      } catch { /* nothing known about this spot */ }
      return out
    },
    scope(s) { scope = s },
    async resolve(at) {
      try {
        // English names, so that what is stored does not depend on the browser's language.
        const { results } = await new gm.Geocoder().geocode({ location: { lat: at[0], lng: at[1] }, language: 'en' })
        return regionOf(results ?? [])
      } catch { return null }
    },
    destroy() { subs.forEach(s => s.remove()); overlay.setMap(null) },
  }
}

// ---- stub: a grid you can pan and zoom, no network ----

function stubMap(el: HTMLElement, initial: View, ev: MapEvents): MapAdapter {
  let v: View = { center: initial.center, zoom: Math.round(initial.zoom) }
  let scope: Scope | null = null
  const size = () => ({ w: el.clientWidth, h: el.clientHeight })
  const at = (e: MouseEvent): LatLng => {
    const r = el.getBoundingClientRect()
    return fromScreen({ x: e.clientX - r.left, y: e.clientY - r.top }, v, size())
  }
  el.classList.add('stubmap')
  const paint = () => {
    const o = toScreen([0, 0], v, size())
    el.style.backgroundPosition = `${o.x}px ${o.y}px`
    ev.view()
  }
  let drag: { x: number; y: number; moved: boolean } | null = null
  const down = (e: PointerEvent) => { panned = false; if (e.button === 0) drag = { x: e.clientX, y: e.clientY, moved: false } }
  const move = (e: PointerEvent) => {
    if (drag && (drag.moved || Math.hypot(e.clientX - drag.x, e.clientY - drag.y) > 4)) {
      const s = size()
      v = { ...v, center: fromScreen({ x: s.w / 2 - (e.clientX - drag.x), y: s.h / 2 - (e.clientY - drag.y) }, v, s) }
      drag = { x: e.clientX, y: e.clientY, moved: true }
      paint()
    } else if (!drag) ev.move(at(e))
  }
  // The click that follows a pan is not a click on the map.
  let panned = false
  const up = () => { panned = !!drag?.moved; drag = null }
  const click = (e: MouseEvent) => { if (!panned) ev.click(at(e)); panned = false }
  const dbl = (e: MouseEvent) => ev.dblclick(at(e))
  const wheel = (e: WheelEvent) => {
    e.preventDefault()
    const zoom = Math.max(3, Math.min(20, v.zoom + (e.deltaY < 0 ? 1 : -1)))
    if (zoom === v.zoom) return
    // keep the point under the cursor where it is
    const r = el.getBoundingClientRect(), p = { x: e.clientX - r.left, y: e.clientY - r.top }, s = size(), keep = fromScreen(p, v, s)
    v = { center: fromScreen({ x: s.w - p.x, y: s.h - p.y }, { center: keep, zoom }, s), zoom }
    paint()
  }
  el.addEventListener('pointerdown', down); window.addEventListener('pointermove', move); window.addEventListener('pointerup', up)
  el.addEventListener('click', click); el.addEventListener('dblclick', dbl); el.addEventListener('wheel', wheel, { passive: false })
  paint()
  return {
    kind: 'stub',
    view: () => v,
    setView(n) { v = { center: n.center, zoom: Math.max(3, Math.min(20, Math.round(n.zoom))) }; paint() },
    setType() { /* one look only */ },
    mount() { /* the layer already lies over the grid and is redrawn with it */ },
    layer: () => layerOf(v, size()),
    toLatLng: p => fromScreen(p, v, size()),
    setCursor(css) { el.style.cursor = css },
    lock() { /* handles stop their own events; the grid never sees them */ },
    async search(q) {
      const direct = parseLatLng(q)
      return q.trim() ? [{ label: q.trim(), at: direct ?? v.center }] : []
    },
    // Made-up places, the same every time, so tests can rely on them.
    async suggest(q) {
      const direct = parseLatLng(q)
      if (direct) return [{ label: q.trim(), at: direct }]
      const name = q.trim()
      if (!name) return []
      // Scoped: around the point given, in the country given. Otherwise around the map's middle.
      const base = scope?.near ?? v.center, tail = scope?.country ? `, ${scope.country}` : ''
      return ['Tower', 'Mall', 'Residences'].map((kind, i): Place => {
        const where: LatLng = [base[0] + i * 0.0004, base[1] + i * 0.0004]
        return { label: `${name} ${kind}`, detail: `${i + 1} Stub St${tail}`, locate: async () => where }
      })
    },
    async nearby(at) {
      const spot = `${at[0].toFixed(4)}, ${at[1].toFixed(4)}`
      return [
        { label: 'Stub Tower', detail: `1 Stub St · ${spot}`, at: [at[0] + 0.0001, at[1]], distance_m: 11 },
        { label: 'Stub Mall', detail: `2 Stub St · ${spot}`, at: [at[0], at[1] + 0.0005], distance_m: 54 },
        { label: `Stub St ${spot}`, detail: '', at, distance_m: 0 },
      ]
    },
    scope(s) { scope = s },
    // Two made-up answers: the Philippines, and North America.
    async resolve(at) {
      if (at[0] > 4 && at[0] < 22 && at[1] > 116 && at[1] < 127) return { country_code: 'PH', country: 'Philippines', province: 'Metro Manila', city: 'Makati', district: 'Stub District', postal_code: '1200' }
      if (at[0] > 24 && at[0] < 50 && at[1] > -125 && at[1] < -66) return { country_code: 'US', country: 'United States', province: 'New York', city: 'New York', district: '', postal_code: '10022' }
      return null
    },
    destroy() {
      el.removeEventListener('pointerdown', down); window.removeEventListener('pointermove', move); window.removeEventListener('pointerup', up)
      el.removeEventListener('click', click); el.removeEventListener('dblclick', dbl); el.removeEventListener('wheel', wheel)
    },
  }
}
