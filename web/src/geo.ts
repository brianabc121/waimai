// Geometry for the delivery map. Everything works on a flat plane in metres
// around a reference point: zones are a few kilometres across, where that is
// accurate to well under a metre. The server has the same point-in-polygon
// rule in internal/app/geo.go; keep the two in step.

export type LatLng = [number, number] // [latitude, longitude]
export interface Pt { x: number; y: number }

const M_PER_DEG = 111320

export interface Plane { to(l: LatLng): Pt; from(p: Pt): LatLng; ring(poly: LatLng[]): Pt[]; back(ring: Pt[]): LatLng[] }

export function plane(ref: LatLng): Plane {
  const kx = Math.cos(ref[0] * Math.PI / 180)
  const to = (l: LatLng): Pt => ({ x: (l[1] - ref[1]) * M_PER_DEG * kx, y: (l[0] - ref[0]) * M_PER_DEG })
  const from = (p: Pt): LatLng => [ref[0] + p.y / M_PER_DEG, ref[1] + p.x / (M_PER_DEG * kx)]
  return { to, from, ring: poly => poly.map(to), back: ring => ring.map(from) }
}

export const dist = (a: Pt, b: Pt) => Math.hypot(a.x - b.x, a.y - b.y)

// nearestOnSeg returns the point of segment a-b closest to p.
export function nearestOnSeg(p: Pt, a: Pt, b: Pt): { d: number; q: Pt; t: number } {
  const dx = b.x - a.x, dy = b.y - a.y, l2 = dx * dx + dy * dy
  const t = l2 > 0 ? Math.max(0, Math.min(1, ((p.x - a.x) * dx + (p.y - a.y) * dy) / l2)) : 0
  const q = { x: a.x + t * dx, y: a.y + t * dy }
  return { d: dist(p, q), q, t }
}

// nearestOnRing returns the closest point of the outline, and which edge it is on.
export function nearestOnRing(p: Pt, ring: Pt[]): { d: number; q: Pt; edge: number; t: number } {
  let best = { d: Infinity, q: p, edge: -1, t: 0 }
  for (let i = 0; i < ring.length; i++) {
    const n = nearestOnSeg(p, ring[i], ring[(i + 1) % ring.length])
    if (n.d < best.d) best = { ...n, edge: i }
  }
  return best
}

const ON_EDGE = 0.01

// contains: inside the ring or on its outline.
export function contains(ring: Pt[], p: Pt): boolean {
  let inside = false
  for (let i = 0, j = ring.length - 1; i < ring.length; j = i++) {
    const a = ring[i], b = ring[j]
    if ((a.y > p.y) !== (b.y > p.y) && p.x < (b.x - a.x) * (p.y - a.y) / (b.y - a.y) + a.x) inside = !inside
  }
  return inside || nearestOnRing(p, ring).d <= ON_EDGE
}

// strictlyInside: inside and more than `margin` metres away from the outline.
export function strictlyInside(ring: Pt[], p: Pt, margin = 0.5): boolean {
  return contains(ring, p) && nearestOnRing(p, ring).d > margin
}

const cross = (o: Pt, a: Pt, b: Pt) => (a.x - o.x) * (b.y - o.y) - (a.y - o.y) * (b.x - o.x)

// segmentsCross: a proper crossing. Touching ends and collinear overlap do not count.
export function segmentsCross(a: Pt, b: Pt, c: Pt, d: Pt): boolean {
  const d1 = cross(c, d, a), d2 = cross(c, d, b), d3 = cross(a, b, c), d4 = cross(a, b, d)
  return ((d1 > 0 && d2 < 0) || (d1 < 0 && d2 > 0)) && ((d3 > 0 && d4 < 0) || (d3 < 0 && d4 > 0))
}

// selfIntersections lists the pairs of edges (by index of their first point) that cross.
export function selfIntersections(ring: Pt[]): [number, number][] {
  const out: [number, number][] = [], n = ring.length
  for (let i = 0; i < n; i++) for (let j = i + 1; j < n; j++) {
    if (j === i + 1 || (i === 0 && j === n - 1)) continue
    if (segmentsCross(ring[i], ring[(i + 1) % n], ring[j], ring[(j + 1) % n])) out.push([i, j])
  }
  return out
}

export function area(ring: Pt[]): number {
  let s = 0
  for (let i = 0; i < ring.length; i++) { const j = (i + 1) % ring.length; s += ring[i].x * ring[j].y - ring[j].x * ring[i].y }
  return Math.abs(s) / 2
}

export function perimeter(ring: Pt[]): number {
  let s = 0
  for (let i = 0; i < ring.length; i++) s += dist(ring[i], ring[(i + 1) % ring.length])
  return s
}

export function centroid(ring: Pt[]): Pt {
  let x = 0, y = 0, a = 0
  for (let i = 0; i < ring.length; i++) {
    const p = ring[i], q = ring[(i + 1) % ring.length], f = p.x * q.y - q.x * p.y
    x += (p.x + q.x) * f; y += (p.y + q.y) * f; a += f
  }
  if (Math.abs(a) < 1e-9) return ring[0] ?? { x: 0, y: 0 }
  return { x: x / (3 * a), y: y / (3 * a) }
}

// labelPoint is a point inside the ring to put its name on: the centroid when
// that is inside, otherwise the middle of the widest horizontal cut through it.
export function labelPoint(ring: Pt[]): Pt {
  const c = centroid(ring)
  if (strictlyInside(ring, c, 0)) return c
  const xs: number[] = []
  for (let i = 0; i < ring.length; i++) {
    const a = ring[i], b = ring[(i + 1) % ring.length]
    if ((a.y > c.y) !== (b.y > c.y)) xs.push(a.x + (c.y - a.y) * (b.x - a.x) / (b.y - a.y))
  }
  xs.sort((p, q) => p - q)
  let best = c, w = 0
  for (let i = 0; i + 1 < xs.length; i += 2) if (xs[i + 1] - xs[i] > w) { w = xs[i + 1] - xs[i]; best = { x: (xs[i] + xs[i + 1]) / 2, y: c.y } }
  return best
}

// overlaps: the two rings share some area. Sharing only a border is not an overlap.
export function overlaps(a: Pt[], b: Pt[]): boolean {
  for (let i = 0; i < a.length; i++) for (let j = 0; j < b.length; j++) {
    if (segmentsCross(a[i], a[(i + 1) % a.length], b[j], b[(j + 1) % b.length])) return true
  }
  const probe = (r: Pt[], other: Pt[]) => r.some((p, i) => {
    const q = r[(i + 1) % r.length]
    return strictlyInside(other, p) || strictlyInside(other, { x: (p.x + q.x) / 2, y: (p.y + q.y) / 2 })
  })
  // Identical outlines, or one traced exactly along the other, have no corner
  // strictly inside; an interior point settles it.
  return probe(a, b) || probe(b, a) || strictlyInside(b, labelPoint(a)) || strictlyInside(a, labelPoint(b))
}

// overlapArea estimates the shared area on a grid; good enough for "about 0.2 km²".
export function overlapArea(a: Pt[], b: Pt[], cells = 70): number {
  const lo = (r: Pt[], k: 'x' | 'y') => Math.min(...r.map(p => p[k])), hi = (r: Pt[], k: 'x' | 'y') => Math.max(...r.map(p => p[k]))
  const x0 = Math.max(lo(a, 'x'), lo(b, 'x')), x1 = Math.min(hi(a, 'x'), hi(b, 'x'))
  const y0 = Math.max(lo(a, 'y'), lo(b, 'y')), y1 = Math.min(hi(a, 'y'), hi(b, 'y'))
  if (x1 <= x0 || y1 <= y0) return 0
  const dx = (x1 - x0) / cells, dy = (y1 - y0) / cells
  let n = 0
  for (let i = 0; i < cells; i++) for (let j = 0; j < cells; j++) {
    const p = { x: x0 + (i + .5) * dx, y: y0 + (j + .5) * dy }
    if (contains(a, p) && contains(b, p)) n++
  }
  return n * dx * dy
}

export function circle(center: Pt, radius: number, points = 24): Pt[] {
  return Array.from({ length: points }, (_, i) => {
    const t = 2 * Math.PI * i / points
    return { x: center.x + radius * Math.cos(t), y: center.y + radius * Math.sin(t) }
  })
}

export const maxReach = (from: Pt, ring: Pt[]) => Math.max(0, ...ring.map(p => dist(from, p)))

// ---- snapping ----

export interface Snap { p: Pt; zone: number; kind: 'vertex' | 'edge' }

// snap pulls p onto the nearest vertex of the other rings within `reach`
// metres, or failing that onto their nearest edge. Vertices win so that two
// zones end up sharing exactly the same corner points.
export function snap(p: Pt, rings: { id: number; ring: Pt[] }[], reach: number): Snap | null {
  let best: Snap | null = null, bd = reach
  for (const r of rings) for (const v of r.ring) {
    const d = dist(p, v)
    if (d <= bd) { bd = d; best = { p: v, zone: r.id, kind: 'vertex' } }
  }
  if (best) return best
  for (const r of rings) {
    const n = nearestOnRing(p, r.ring)
    if (n.d <= bd) { bd = n.d; best = { p: n.q, zone: r.id, kind: 'edge' } }
  }
  return best
}

// ---- gaps between zones ----

export interface Gap { a: number; b: number; width: number; length: number; shapes: Pt[][] }

// findGaps reports strips between two zones that are no wider than `reach`
// metres and belong to no zone. zones are in priority order; a is always the
// earlier one.
export function findGaps(zones: { id: number; ring: Pt[] }[], reach: number): Gap[] {
  const out: Gap[] = []
  for (let i = 0; i < zones.length; i++) for (let j = i + 1; j < zones.length; j++) {
    const g = gapBetween(zones[i].ring, zones[j].ring, zones, reach) ?? gapBetween(zones[j].ring, zones[i].ring, zones, reach)
    if (g) out.push({ a: zones[i].id, b: zones[j].id, ...g })
  }
  return out
}

function gapBetween(a: Pt[], b: Pt[], all: { ring: Pt[] }[], reach: number): { width: number; length: number; shapes: Pt[][] } | null {
  const step = Math.max(8, perimeter(a) / 800)
  // Walk a's outline; a sample is "in a gap" when b's outline is close but not
  // touching and the ground in between belongs to nobody.
  const samples: { p: Pt; q: Pt; d: number; gap: boolean }[] = []
  for (let i = 0; i < a.length; i++) {
    const s = a[i], e = a[(i + 1) % a.length], n = Math.max(1, Math.round(dist(s, e) / step))
    for (let k = 0; k < n; k++) {
      const p = { x: s.x + (e.x - s.x) * k / n, y: s.y + (e.y - s.y) * k / n }
      const near = nearestOnRing(p, b)
      const mid = { x: (p.x + near.q.x) / 2, y: (p.y + near.q.y) / 2 }
      // "In between" must be free ground: neither the middle nor the first
      // metre towards b may belong to any zone (a itself included, which rules
      // out measuring along a's own outline).
      const k1 = Math.min(1, near.d / 2) / (near.d || 1)
      const off = { x: p.x + (near.q.x - p.x) * k1, y: p.y + (near.q.y - p.y) * k1 }
      // The two outlines must face each other here: from b's side the way
      // back to a is about as short. Past the end of b (an outside corner
      // where zones meet) it is much shorter, and that is not a gap.
      const gap = near.d > 1 && near.d <= reach && !all.some(z => contains(z.ring, mid) || contains(z.ring, off)) &&
        nearestOnRing(near.q, a).d >= near.d * 0.9
      samples.push({ p, q: near.q, d: near.d, gap })
    }
  }
  if (!samples.some(s => s.gap)) return null
  // Collect runs of consecutive gap samples; the outline is a loop, so start
  // right after a sample that is not in a gap.
  const start = samples.findIndex(s => !s.gap)
  const order = start < 0 ? samples : [...samples.slice(start), ...samples.slice(0, start)]
  const shapes: Pt[][] = []
  let width = 0, length = 0, run: typeof samples = []
  const flush = () => {
    const len = (run.length - 1) * step
    // A strip is longer than it is wide; anything else is a notch.
    if (run.length >= 2 && len >= Math.max(15, 1.5 * Math.min(...run.map(s => s.d)))) {
      shapes.push([...run.map(s => s.p), ...run.map(s => s.q).reverse()])
      width = Math.max(width, ...run.map(s => s.d)); length += len
    }
    run = []
  }
  for (const s of order) { if (s.gap) run.push(s); else flush() }
  flush()
  return shapes.length ? { width, length, shapes } : null
}

// closeGap reshapes `low` so that it touches `high` wherever the two are
// within `reach` metres: high's nearby corners are added to low's outline and
// low's nearby corners move onto high's outline. high is not changed. Returns
// null when the result would not be a valid outline.
export function closeGap(low: Pt[], high: Pt[], reach: number): Pt[] | null {
  const inserts: { edge: number; t: number; p: Pt }[] = []
  for (const v of high) {
    if (contains(low, v)) continue
    const n = nearestOnRing(v, low)
    if (n.d > 0.5 && n.d <= reach) inserts.push({ edge: n.edge, t: n.t, p: v })
  }
  const out: Pt[] = []
  low.forEach((p, i) => {
    const n = nearestOnRing(p, high)
    out.push(!contains(high, p) && n.d > 0.5 && n.d <= reach ? n.q : p)
    inserts.filter(x => x.edge === i).sort((x, y) => x.t - y.t).forEach(x => out.push(x.p))
  })
  const clean = out.filter((p, i) => dist(p, out[(i + out.length - 1) % out.length]) > 0.5)
  if (clean.length < 3 || selfIntersections(clean).length || area(clean) < 1) return null
  return clean
}

// ---- web mercator, for placing things on a map view ----

export interface View { center: LatLng; zoom: number }
export interface Size { w: number; h: number }

const TILE = 256
export function world(l: LatLng, zoom: number): Pt {
  const s = TILE * 2 ** zoom, sin = Math.min(0.9999, Math.max(-0.9999, Math.sin(l[0] * Math.PI / 180)))
  return { x: (l[1] + 180) / 360 * s, y: (0.5 - Math.log((1 + sin) / (1 - sin)) / (4 * Math.PI)) * s }
}
export function unworld(p: Pt, zoom: number): LatLng {
  const s = TILE * 2 ** zoom, n = Math.PI - 2 * Math.PI * p.y / s
  return [180 / Math.PI * Math.atan(0.5 * (Math.exp(n) - Math.exp(-n))), p.x / s * 360 - 180]
}
// toScreen / fromScreen convert between a position and a pixel of a map that
// shows `view` in a box of `size`, north up.
export function toScreen(l: LatLng, view: View, size: Size): Pt {
  const c = world(view.center, view.zoom), p = world(l, view.zoom)
  return { x: p.x - c.x + size.w / 2, y: p.y - c.y + size.h / 2 }
}
export function fromScreen(p: Pt, view: View, size: Size): LatLng {
  const c = world(view.center, view.zoom)
  return unworld({ x: p.x + c.x - size.w / 2, y: p.y + c.y - size.h / 2 }, view.zoom)
}
export const metresPerPixel = (lat: number, zoom: number) => 156543.03392 * Math.cos(lat * Math.PI / 180) / 2 ** zoom

// fitView returns the view that shows all points with some margin.
export function fitView(points: LatLng[], size: Size, maxZoom = 17): View {
  const lats = points.map(p => p[0]), lngs = points.map(p => p[1])
  const center: LatLng = [(Math.min(...lats) + Math.max(...lats)) / 2, (Math.min(...lngs) + Math.max(...lngs)) / 2]
  for (let zoom = maxZoom; zoom > 2; zoom--) {
    const ps = points.map(p => toScreen(p, { center, zoom }, size))
    if (ps.every(p => p.x >= 40 && p.y >= 40 && p.x <= size.w - 40 && p.y <= size.h - 40)) return { center, zoom }
  }
  return { center, zoom: 3 }
}

export function fmtArea(m2: number): string {
  return m2 >= 1e5 ? (m2 / 1e6).toFixed(m2 >= 1e7 ? 0 : 1) + ' km²' : Math.round(m2 / 100) * 100 + ' m²'
}
export function fmtDist(m: number): string {
  return m >= 1000 ? (m / 1000).toFixed(1) + ' km' : Math.round(m) + ' m'
}
