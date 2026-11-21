// The map itself, behind a small interface. Zones, pins and handles are drawn
// by MapCanvas.vue in its own SVG layer, positioned with the web-mercator
// maths in geo.ts, so an adapter only has to show tiles, pan, zoom and report
// clicks. Two adapters:
//   google - Google Maps JavaScript API, when GOOGLE_MAPS_API_KEY is a real key
//   stub   - a tile-less grid, when the key is the word "stub" (tests, demos)
import { fromScreen, toScreen, type LatLng, type View } from './geo'
import { get } from './api'

export interface Place { label: string; at: LatLng }
export interface MapEvents {
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
  search(q: string): Promise<Place[]>
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
    // The overlay assumes north-up web mercator at whole zoom levels.
    isFractionalZoomEnabled: false, tilt: 0, heading: 0, rotateControl: false,
    disableDoubleClickZoom: true, clickableIcons: false, gestureHandling: 'greedy',
    streetViewControl: false, fullscreenControl: false, mapTypeControl: false, zoomControl: true,
  })
  const ll = (e: any): LatLng => [e.latLng.lat(), e.latLng.lng()]
  const subs = [
    map.addListener('bounds_changed', () => ev.view()),
    map.addListener('click', (e: any) => e.latLng && ev.click(ll(e))),
    map.addListener('dblclick', (e: any) => e.latLng && ev.dblclick(ll(e))),
    map.addListener('mousemove', (e: any) => e.latLng && ev.move(ll(e))),
  ]
  return {
    kind: 'google',
    view() {
      const c = map.getCenter()
      return c ? { center: [c.lat(), c.lng()], zoom: map.getZoom() ?? view.zoom } : view
    },
    setView(v) { map.setZoom(Math.round(v.zoom)); map.setCenter({ lat: v.center[0], lng: v.center[1] }) },
    setType(t) { map.setMapTypeId(t) },
    async search(q) {
      const direct = parseLatLng(q)
      if (direct) return [{ label: q.trim(), at: direct }]
      try {
        const { results } = await new gm.Geocoder().geocode({ address: q, bounds: map.getBounds() })
        return (results ?? []).slice(0, 5).map((r: any) => ({ label: r.formatted_address, at: [r.geometry.location.lat(), r.geometry.location.lng()] as LatLng }))
      } catch { return [] } // ZERO_RESULTS and friends arrive as rejections
    },
    destroy() { subs.forEach(s => s.remove()) },
  }
}

// ---- stub: a grid you can pan and zoom, no network ----

function stubMap(el: HTMLElement, initial: View, ev: MapEvents): MapAdapter {
  let v: View = { center: initial.center, zoom: Math.round(initial.zoom) }
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
    async search(q) {
      const direct = parseLatLng(q)
      return q.trim() ? [{ label: q.trim(), at: direct ?? v.center }] : []
    },
    destroy() {
      el.removeEventListener('pointerdown', down); window.removeEventListener('pointermove', move); window.removeEventListener('pointerup', up)
      el.removeEventListener('click', click); el.removeEventListener('dblclick', dbl); el.removeEventListener('wheel', wheel)
    },
  }
}
