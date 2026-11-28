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

test('one failed read does not hold back the other', async () => {
  const { parts } = await import('../src/retry.ts')
  const r = await parts({ board: () => Promise.reject(new Error('502')), stations: () => Promise.resolve([1, 2]) })
  assert.deepEqual(r.values.stations, [1, 2])
  assert.equal(r.values.board, undefined)
  assert.equal(r.errors.board.message, '502')
  assert.equal(r.errors.stations, undefined)
})

test('a screen that never loaded, or loaded long ago, says so', async () => {
  const { screenState } = await import('../src/retry.ts')
  const t0 = 1_000_000
  assert.equal(screenState(null, false, t0, 90000), 'ok') // first read still running
  assert.equal(screenState(null, true, t0, 90000), 'never') // empty is not "no orders"
  assert.equal(screenState(t0, false, t0 + 30000, 90000), 'ok')
  assert.equal(screenState(t0, true, t0 + 30000, 90000), 'failing')
  assert.equal(screenState(t0, true, t0 + 91000, 90000), 'stale') // old tickets are marked
  assert.equal(screenState(t0, false, t0 + 91000, 90000), 'stale')
})

test('signing out only counts when the server knows', async () => {
  const { signOut } = await import('../src/retry.ts')
  const err = (status, code) => Object.assign(new Error(code), { status, code })
  assert.equal(await signOut(() => Promise.resolve({ ok: true })), true) // normal
  assert.equal(await signOut(() => Promise.reject(err(401, 'UNAUTHENTICATED'))), true) // already expired
  assert.equal(await signOut(() => Promise.reject(err(0, 'NETWORK'))), false) // no network
  assert.equal(await signOut(() => Promise.reject(err(500, 'INTERNAL'))), false) // database error
})
