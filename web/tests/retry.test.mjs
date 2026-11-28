// Run with: node --test tests/retry.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import { latest, stableKey } from '../src/retry.ts'

function memory() {
  const m = new Map()
  return { getItem: k => m.get(k) ?? null, setItem: (k, v) => { m.set(k, v) }, removeItem: k => { m.delete(k) } }
}

test('the same content keeps its key, other content gets another', () => {
  const k = stableKey()
  const a = k.for({ items: [{ id: 1, qty: 2 }], note: '' })
  assert.equal(k.for({ items: [{ id: 1, qty: 2 }], note: '' }), a)
  const b = k.for({ items: [{ id: 1, qty: 3 }], note: '' })
  assert.notEqual(b, a)
  // going back to the first content is a new request, not the old one
  assert.notEqual(k.for({ items: [{ id: 1, qty: 2 }], note: '' }), a)
})

test('after done() the same content is a new request', () => {
  const k = stableKey()
  const a = k.for('x')
  k.done()
  assert.notEqual(k.for('x'), a)
})

test('a kept key survives a reload, and is dropped when done', () => {
  const keep = memory()
  const a = stableKey(keep, 'order-key:tea').for({ cart: 1 })
  // the page was reloaded: the same basket finds the same key
  const again = stableKey(keep, 'order-key:tea')
  assert.equal(again.for({ cart: 1 }), a)
  // another store's page has its own
  assert.notEqual(stableKey(keep, 'order-key:noodle').for({ cart: 1 }), a)
  again.done()
  assert.notEqual(stableKey(keep, 'order-key:tea').for({ cart: 1 }), a)
})

test('a late answer is told from the current one', async () => {
  const turn = latest()
  let shown = ''
  const ask = async (what, ms) => {
    const mine = turn.next()
    await new Promise(r => setTimeout(r, ms))
    if (mine()) shown = what
  }
  // the first request answers after the second
  await Promise.all([ask('old', 30), ask('new', 5)])
  assert.equal(shown, 'new')
})
