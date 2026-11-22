// Run with: node --test tests/cut.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import * as g from '../src/geo.ts'
import * as c from '../src/cut.ts'

const sq = (x0, y0, x1, y1) => [{ x: x0, y: y0 }, { x: x1, y: y0 }, { x: x1, y: y1 }, { x: x0, y: y1 }]
const near = (a, b, tol = 1) => assert.ok(Math.abs(a - b) <= tol, `${a} is not about ${b}`)

test('sharedArea and overlaps', () => {
  const a = sq(0, 0, 1000, 1000)
  near(c.sharedArea(a, sq(500, 500, 1500, 1500)), 250000)
  near(c.sharedArea(a, sq(200, 200, 400, 400)), 40000)
  assert.equal(c.sharedArea(a, sq(2000, 0, 3000, 1000)), 0)
  assert.ok(c.overlaps(a, sq(500, 500, 1500, 1500)))
  assert.ok(c.overlaps(a, a), 'identical')
  assert.ok(!c.overlaps(a, sq(1000, 0, 2000, 1000)), 'sharing a border')
  assert.ok(!c.overlaps(a, sq(999.99, 0, 2000, 1000)), 'a hair of overlap is touching')
})

test('cutting a corner off', () => {
  const old = sq(0, 0, 1000, 1000), fresh = sq(600, 600, 1600, 1600)
  const cut = c.subtract(old, [fresh])
  assert.equal(cut.pieces.length, 1)
  assert.ok(!cut.holes)
  const left = cut.pieces[0]
  assert.equal(left.length, 6, 'an L shape')
  near(g.area(left), 1000000 - 160000)
  assert.ok(!c.overlaps(left, fresh), 'nothing in common any more')
  // the corner of the new zone that was inside is now a corner of the old one, exactly
  assert.ok(left.includes(fresh[0]), 'the shared corner is the same point')
  assert.equal(g.findGaps([{ id: 1, ring: fresh }, { id: 2, ring: left }], 200).length, 0, 'no gap along the cut')
  assert.equal(g.selfIntersections(left).length, 0)
})

test('cut in two: both pieces come back, larger first', () => {
  const old = sq(0, 0, 1000, 400), band = sq(600, -100, 700, 500)
  const cut = c.subtract(old, [band])
  assert.equal(cut.pieces.length, 2)
  near(g.area(cut.pieces[0]), 600 * 400)
  near(g.area(cut.pieces[1]), 300 * 400)
})

test('leftovers are dropped', () => {
  // the band leaves a strip 1 m wide on the right: 400 m2
  const cut = c.subtract(sq(0, 0, 1000, 400), [sq(600, -100, 999, 500)])
  assert.equal(cut.pieces.length, 1)
})

test('a clip inside the subject would leave a hole', () => {
  const cut = c.subtract(sq(0, 0, 1000, 1000), [sq(400, 400, 600, 600)])
  assert.ok(cut.holes)
})

test('covered completely: nothing is left', () => {
  const cut = c.subtract(sq(400, 400, 600, 600), [sq(0, 0, 1000, 1000)])
  assert.equal(cut.pieces.length, 0)
  assert.ok(!cut.holes)
})

test('several clips at once', () => {
  const cut = c.subtract(sq(0, 0, 1000, 1000), [sq(-100, -100, 300, 300), sq(700, 700, 1100, 1100)])
  assert.equal(cut.pieces.length, 1)
  near(g.area(cut.pieces[0]), 1000000 - 90000 - 90000)
  assert.equal(cut.pieces[0].length, 8)
})
