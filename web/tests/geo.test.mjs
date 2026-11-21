// Run with: node --test tests/geo.test.mjs   (Node 22.18+ reads the .ts source directly)
import test from 'node:test'
import assert from 'node:assert/strict'
import * as g from '../src/geo.ts'

const sq = (x0, y0, x1, y1) => [{ x: x0, y: y0 }, { x: x1, y: y0 }, { x: x1, y: y1 }, { x: x0, y: y1 }]

test('contains matches the server rule', () => {
  const a = sq(0, 0, 1000, 1000)
  assert.ok(g.contains(a, { x: 500, y: 500 }))
  assert.ok(g.contains(a, { x: 1000, y: 500 }), 'on the edge')
  assert.ok(g.contains(a, { x: 0, y: 0 }), 'on a corner')
  assert.ok(!g.contains(a, { x: 1001, y: 500 }))
  const notch = [{ x: 0, y: 0 }, { x: 1000, y: 0 }, { x: 1000, y: 1000 }, { x: 600, y: 1000 }, { x: 600, y: 500 }, { x: 400, y: 500 }, { x: 400, y: 1000 }, { x: 0, y: 1000 }]
  assert.ok(!g.contains(notch, { x: 500, y: 900 }))
  assert.ok(g.contains(notch, { x: 500, y: 400 }))
})

test('overlaps: shared area only', () => {
  const a = sq(0, 0, 1000, 1000)
  assert.ok(g.overlaps(a, sq(500, 500, 1500, 1500)), 'crossing')
  assert.ok(g.overlaps(a, sq(200, 200, 400, 400)), 'one inside the other')
  assert.ok(g.overlaps(sq(200, 200, 400, 400), a))
  assert.ok(g.overlaps(a, sq(0, 0, 1000, 1000)), 'identical')
  assert.ok(!g.overlaps(a, sq(1000, 0, 2000, 1000)), 'sharing a border')
  assert.ok(!g.overlaps(a, sq(1000, 1000, 2000, 2000)), 'sharing a corner')
  assert.ok(!g.overlaps(a, sq(1050, 0, 2000, 1000)), 'apart')
  // a thin sliver poking in still counts
  assert.ok(g.overlaps(a, [{ x: 990, y: 400 }, { x: 2000, y: 0 }, { x: 2000, y: 1000 }]))
})

test('overlapArea is close to the real thing', () => {
  const got = g.overlapArea(sq(0, 0, 1000, 1000), sq(500, 500, 1500, 1500))
  assert.ok(Math.abs(got - 250000) < 15000, String(got))
  assert.equal(g.overlapArea(sq(0, 0, 10, 10), sq(20, 20, 30, 30)), 0)
})

test('selfIntersections finds a bow tie', () => {
  assert.equal(g.selfIntersections(sq(0, 0, 10, 10)).length, 0)
  assert.equal(g.selfIntersections([{ x: 0, y: 0 }, { x: 10, y: 10 }, { x: 10, y: 0 }, { x: 0, y: 10 }]).length, 1)
})

test('snap prefers corners, then edges', () => {
  const rings = [{ id: 7, ring: sq(0, 0, 1000, 1000) }]
  assert.deepEqual(g.snap({ x: 1008, y: 6 }, rings, 15), { p: { x: 1000, y: 0 }, zone: 7, kind: 'vertex' })
  const e = g.snap({ x: 1008, y: 500 }, rings, 15)
  assert.equal(e.kind, 'edge'); assert.deepEqual(e.p, { x: 1000, y: 500 })
  assert.equal(g.snap({ x: 1030, y: 500 }, rings, 15), null)
})

test('findGaps sees a strip between two zones and nothing else', () => {
  const a = { id: 1, ring: sq(0, 0, 1000, 1000) }, b = { id: 2, ring: sq(1050, 0, 2000, 1000) }
  const gaps = g.findGaps([a, b], 200)
  assert.equal(gaps.length, 1)
  assert.equal(gaps[0].a, 1); assert.equal(gaps[0].b, 2)
  assert.ok(Math.abs(gaps[0].width - 50) < 1, String(gaps[0].width))
  assert.ok(gaps[0].length > 900 && gaps[0].length < 1100, String(gaps[0].length))
  assert.equal(g.findGaps([a, { id: 2, ring: sq(1000, 0, 2000, 1000) }], 200).length, 0, 'touching')
  assert.equal(g.findGaps([a, { id: 2, ring: sq(900, 0, 2000, 1000) }], 200).length, 0, 'overlapping')
  assert.equal(g.findGaps([a, { id: 2, ring: sq(1500, 0, 2000, 1000) }], 200).length, 0, 'far apart')
  // a third zone covering the strip means there is no gap
  assert.equal(g.findGaps([a, b, { id: 3, ring: sq(900, -100, 1100, 1100) }], 200).filter(x => x.a === 1 && x.b === 2).length, 0)
})

test('closeGap moves the lower zone onto the higher one', () => {
  const high = sq(0, 0, 1000, 1000), low = sq(1050, 0, 2000, 1000)
  const fixed = g.closeGap(low, high, 200)
  assert.ok(fixed)
  assert.equal(g.findGaps([{ id: 1, ring: high }, { id: 2, ring: fixed }], 200).length, 0)
  assert.ok(!g.overlaps(high, fixed))
  assert.ok(Math.abs(g.area(fixed) - 1000 * 1000) < 1, 'low grew by the strip')
  // slanted and with different corner counts
  const h2 = [{ x: 0, y: 0 }, { x: 1000, y: 0 }, { x: 1100, y: 500 }, { x: 1000, y: 1000 }, { x: 0, y: 1000 }]
  const l2 = [{ x: 1060, y: -20 }, { x: 2000, y: 0 }, { x: 2000, y: 1000 }, { x: 1050, y: 1020 }]
  const f2 = g.closeGap(l2, h2, 200)
  assert.ok(f2)
  assert.equal(g.findGaps([{ id: 1, ring: h2 }, { id: 2, ring: f2 }], 200).length, 0)
  assert.equal(g.closeGap(sq(5000, 0, 6000, 1000), high, 200).length, 4, 'nothing to do when far apart')
})

test('mercator round trip and scale', () => {
  const view = { center: [14.5547, 121.0244], zoom: 15 }, size = { w: 1000, h: 800 }
  const c = g.toScreen(view.center, view, size)
  assert.ok(Math.abs(c.x - 500) < 1e-6 && Math.abs(c.y - 400) < 1e-6)
  const ll = g.fromScreen({ x: 123, y: 456 }, view, size), back = g.toScreen(ll, view, size)
  assert.ok(Math.abs(back.x - 123) < 1e-6 && Math.abs(back.y - 456) < 1e-6)
  // 100 px east at zoom 15 near Manila is about 462 m
  const east = g.fromScreen({ x: 600, y: 400 }, view, size), pl = g.plane(view.center)
  assert.ok(Math.abs(pl.to(east).x - 100 * g.metresPerPixel(view.center[0], 15)) < 1)
  const fit = g.fitView([[14.55, 121.02], [14.56, 121.03]], size)
  assert.ok(fit.zoom >= 14 && fit.zoom <= 16, String(fit.zoom))
})

test('labelPoint stays inside a concave zone', () => {
  const u = [{ x: 0, y: 0 }, { x: 300, y: 0 }, { x: 300, y: 300 }, { x: 200, y: 300 }, { x: 200, y: 100 }, { x: 100, y: 100 }, { x: 100, y: 300 }, { x: 0, y: 300 }]
  assert.ok(g.contains(u, g.labelPoint(u)))
})

test('an outside corner where zones meet is not a gap', () => {
  // b sits below-left of a; right of x=0 the ground between them is open country
  const a = [{ x: 0, y: 0 }, { x: 300, y: 0 }, { x: 300, y: 200 }, { x: 0, y: 200 }]
  const b = [{ x: -400, y: -40 }, { x: 0, y: -40 }, { x: 0, y: -300 }, { x: -400, y: -300 }]
  const left = [{ x: -400, y: -40 }, { x: 0, y: -40 }, { x: 0, y: 200 }, { x: -400, y: 200 }]
  assert.equal(g.findGaps([{ id: 1, ring: a }, { id: 2, ring: left }, { id: 3, ring: b }], 200).length, 0)
})
