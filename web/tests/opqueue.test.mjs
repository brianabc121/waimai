// Run with: node --test tests/opqueue.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import { opQueue } from '../src/views/work/opqueue.ts'

function memory() {
  const m = new Map()
  return { m, getItem: k => m.get(k) ?? null, setItem: (k, v) => { m.set(k, v) }, removeItem: k => { m.delete(k) } }
}
const op = (id, task) => ({ id, task, order: task, no: 'N' + task, action: 'deliver', body: {}, at: '2026-01-01T00:00:00Z' })

test('the next rider neither sees nor sends what the last one left', async () => {
  const keep = memory(), sent = []
  let online = false
  const q = opQueue(keep, async o => { if (!online) throw { code: 'NETWORK', status: 0 }; sent.push(o.id) })
  q.own(1)
  q.enqueue(op('a', 10)); q.enqueue(op('b', 11))
  assert.equal((await q.send()).length, 0) // offline: both stay
  assert.equal(q.state.ops.length, 2)
  q.drop() // A signs out
  assert.equal(q.state.ops.length, 0)

  q.own(2) // B signs in on the same phone
  online = true
  assert.equal(q.state.ops.length, 0)
  await q.send()
  assert.deepEqual(sent, [])
  q.enqueue(op('c', 20))
  await q.send()
  assert.deepEqual(sent, ['c'])

  q.drop(); q.own(1) // A comes back: their own steps are still theirs
  assert.deepEqual(q.state.ops.map(o => o.id), ['a', 'b'])
  await q.send()
  assert.deepEqual(sent, ['c', 'a', 'b'])
  assert.equal(keep.m.size, 0)
})

test('a send that is under way stops when the rider signs out', async () => {
  const keep = memory(), sent = []
  let release
  const q = opQueue(keep, o => new Promise(ok => { sent.push(o.id); release = ok }))
  q.own(1)
  q.enqueue(op('a', 10)); q.enqueue(op('b', 11))
  const run = q.send()
  await Promise.resolve()
  q.drop(); q.own(2)
  q.enqueue(op('z', 30))
  release()
  await run
  assert.deepEqual(sent, ['a']) // b was never sent
  assert.deepEqual(q.state.ops.map(o => o.id), ['z']) // B's queue was not touched
  assert.equal(JSON.parse(keep.getItem('work-ops:1')).length, 2) // a is sent again later: the server ignores a repeat
})

test('a refused step takes the later steps of the same delivery with it; the old shared key is dropped', async () => {
  const keep = memory()
  keep.setItem('work-ops', JSON.stringify([op('old', 1)]))
  const q = opQueue(keep, async o => { if (o.task === 10) throw { code: 'NOT_YOURS', status: 403, message: 'no' } })
  q.own(5)
  assert.equal(keep.getItem('work-ops'), null)
  assert.equal(q.state.ops.length, 0)
  q.enqueue(op('a', 10)); q.enqueue(op('b', 10)); q.enqueue(op('c', 11))
  const r = await q.send()
  assert.deepEqual(r.map(x => x.no), ['N10'])
  assert.equal(q.state.ops.length, 0)
})
