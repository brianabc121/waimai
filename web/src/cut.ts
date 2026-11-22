// Cutting one delivery zone out of another, on the flat plane of geo.ts
// (metres). The polygon arithmetic is polygon-clipping's; this file turns its
// answers into what a zone can be: one outline, no holes.
import polygonClipping from 'polygon-clipping'
import { area, dist, type Pt } from './geo.ts'

// Pieces smaller than this are leftovers of the cut, not places to deliver to.
export const SLIVER_M2 = 500
// Two zones that share less than this only touch (rounding along a common border).
export const TOUCH_M2 = 25

type Ring = [number, number][]
const toRing = (r: Pt[]): Ring => r.map(p => [p.x, p.y])
// The library closes its rings (first point repeated); zones do not.
function fromRing(r: Ring): Pt[] {
  const out: Pt[] = []
  for (const [x, y] of r) {
    const p = { x, y }
    if (!out.length || dist(out[out.length - 1], p) > 0.01) out.push(p)
  }
  if (out.length > 1 && dist(out[0], out[out.length - 1]) <= 0.01) out.pop()
  return out
}

// sharedArea is how many square metres two outlines have in common.
export function sharedArea(a: Pt[], b: Pt[]): number {
  if (a.length < 3 || b.length < 3) return 0
  let sum = 0
  for (const poly of polygonClipping.intersection([toRing(a)], [toRing(b)])) {
    poly.forEach((ring, i) => { const m2 = area(fromRing(ring)); sum += i ? -m2 : m2 })
  }
  return sum
}
export const overlaps = (a: Pt[], b: Pt[]) => sharedArea(a, b) > TOUCH_M2

export interface Cut {
  // What is left of the subject, largest piece first; leftovers are dropped.
  pieces: Pt[][]
  // The clips would leave a hole in a piece. One outline cannot say that, so
  // such a cut is not made (the zone inside wins by list order instead).
  holes: boolean
}

// subtract removes the clips from the subject. Corners of the result that
// stand on a corner of an input outline are that very corner, so neighbours
// end up with identical coordinates along their common border.
export function subtract(subject: Pt[], clips: Pt[][]): Cut {
  const known = [subject, ...clips].flat()
  const snap = (p: Pt) => known.find(k => dist(k, p) < 1e-3) ?? p
  const pieces: Pt[][] = []
  let holes = false
  for (const poly of polygonClipping.difference([toRing(subject)], ...clips.map(c => [[toRing(c)]]))) {
    const outer = fromRing(poly[0]).map(snap)
    if (outer.length < 3 || area(outer) < SLIVER_M2) continue
    if (poly.slice(1).some(h => area(fromRing(h)) >= TOUCH_M2)) holes = true
    pieces.push(outer)
  }
  pieces.sort((a, b) => area(b) - area(a))
  return { pieces, holes }
}
