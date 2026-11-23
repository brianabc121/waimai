// Drawing deliveries on a map (docs/DELIVERY_ADDRESS_STATS_DESIGN.md §3.3):
// merging dots that crowd each other, and painting a heat map. Google's own
// heat map layer is gone (2026), so the heat is painted here, on a canvas,
// and laid on the map's drawing layer like every other shape.

export interface Dot { x: number; y: number; n: number }
export interface Cluster<T> { x: number; y: number; n: number; items: T[] }

// cluster merges dots that fall in the same square of `cell` pixels into
// one, at their weighted middle. A lone dot stays where it is.
export function cluster<T extends Dot>(dots: T[], cell: number): Cluster<T>[] {
  const cells = new Map<string, Cluster<T>>()
  for (const d of dots) {
    const k = Math.floor(d.x / cell) + ':' + Math.floor(d.y / cell)
    const c = cells.get(k)
    if (c) { c.items.push(d); c.n += d.n } else cells.set(k, { x: 0, y: 0, n: d.n, items: [d] })
  }
  const out: Cluster<T>[] = []
  for (const c of cells.values()) {
    let x = 0, y = 0
    for (const d of c.items) { x += d.x * d.n; y += d.y * d.n }
    c.x = x / c.n; c.y = y / c.n
    out.push(c)
  }
  // Small ones first, so that the big ones are drawn on top.
  return out.sort((a, b) => a.n - b.n)
}

// dotRadius: bigger with more deliveries, but never huge.
export function dotRadius(n: number): number { return n >= 10 ? Math.min(26, 10 + Math.sqrt(n) * 1.7) : 4 + n * 0.6 }

// dotColor: gold for a few, deep red for many.
export function dotColor(n: number): string {
  return n >= 40 ? '#b93a08' : n >= 15 ? '#d9480f' : n >= 5 ? '#f08c00' : '#f2a541'
}

// The heat colours, from nothing through gold and orange to deep red.
const STOPS: [number, string][] = [[0, 'rgba(247,200,80,0)'], [0.18, 'rgba(247,200,80,0.55)'], [0.45, '#f08c00'], [0.72, '#d9480f'], [1, '#8f1d0b']]
export const HEAT_GRADIENT = 'linear-gradient(90deg, rgba(247,200,80,.25), #f7c850, #f08c00, #d9480f, #8f1d0b)'

let palette: Uint8ClampedArray | null = null
function colours(): Uint8ClampedArray {
  if (palette) return palette
  const c = document.createElement('canvas'); c.width = 256; c.height = 1
  const g = c.getContext('2d')!, lg = g.createLinearGradient(0, 0, 256, 0)
  for (const [at, col] of STOPS) lg.addColorStop(at, col)
  g.fillStyle = lg; g.fillRect(0, 0, 256, 1)
  return (palette = g.getImageData(0, 0, 256, 1).data)
}

// paintHeat paints dots (in pixels of a w×h box) as heat and returns the
// picture as a data URL, or '' when there is nothing to paint. Each dot is a
// blurred disc of `radius` pixels, as strong as its share of the busiest dot.
export function paintHeat(dots: Dot[], w: number, h: number, radius: number): string {
  w = Math.max(1, Math.round(w)); h = Math.max(1, Math.round(h))
  const near = dots.filter(d => d.x > -radius && d.y > -radius && d.x < w + radius && d.y < h + radius)
  if (!near.length) return ''
  const c = document.createElement('canvas'); c.width = w; c.height = h
  const g = c.getContext('2d', { willReadFrequently: true })
  if (!g) return ''
  const max = Math.max(...near.map(d => d.n))
  for (const d of near) {
    const grd = g.createRadialGradient(d.x, d.y, 0, d.x, d.y, radius)
    grd.addColorStop(0, 'rgba(0,0,0,1)'); grd.addColorStop(1, 'rgba(0,0,0,0)')
    g.globalAlpha = Math.min(1, 0.12 + 0.88 * Math.sqrt(d.n / max))
    g.fillStyle = grd
    g.fillRect(d.x - radius, d.y - radius, radius * 2, radius * 2)
  }
  const pal = colours(), img = g.getImageData(0, 0, w, h), px = img.data
  for (let i = 0; i < px.length; i += 4) {
    const v = px[i + 3]
    if (!v) continue
    px[i] = pal[v * 4]; px[i + 1] = pal[v * 4 + 1]; px[i + 2] = pal[v * 4 + 2]
    px[i + 3] = Math.min(225, pal[v * 4 + 3] * 0.85 + v * 0.15)
  }
  g.putImageData(img, 0, 0)
  return c.toDataURL()
}

// Dates are whole days, "YYYY-MM-DD", in the browser's clock.
const two = (n: number) => String(n).padStart(2, '0')
export const isoDay = (d: Date) => `${d.getFullYear()}-${two(d.getMonth() + 1)}-${two(d.getDate())}`
export function addDays(day: string, n: number): string {
  const [y, m, d] = day.split('-').map(Number)
  return isoDay(new Date(y, m - 1, d + n))
}
// spanDays counts the days from `from` to `to`, both included.
export function spanDays(from: string, to: string): number {
  const a = Date.UTC(...(from.split('-').map(Number).map((v, i) => i === 1 ? v - 1 : v) as [number, number, number]))
  const b = Date.UTC(...(to.split('-').map(Number).map((v, i) => i === 1 ? v - 1 : v) as [number, number, number]))
  return Math.round((b - a) / 86400000) + 1
}
// lastMonth is the calendar month before the one `today` is in.
export function lastMonth(today: string): [string, string] {
  const [y, m] = today.split('-').map(Number)
  return [isoDay(new Date(y, m - 2, 1)), isoDay(new Date(y, m - 1, 0))]
}
