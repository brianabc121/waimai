// Run with: node --test tests/now.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import { effectScope } from 'vue'
import { useNow } from '../src/now.ts'

test('the clock stops with the screen', () => {
  const live = new Set()
  const realSet = globalThis.setInterval, realClear = globalThis.clearInterval
  let n = 0
  globalThis.setInterval = fn => { const id = ++n; live.add(id); return id }
  globalThis.clearInterval = id => { live.delete(id) }
  try {
    for (let i = 0; i < 5; i++) { // opened and closed five times
      const scope = effectScope()
      const now = scope.run(() => useNow(30000))
      assert.equal(typeof now.value, 'number')
      assert.equal(live.size, 1)
      scope.stop()
      assert.equal(live.size, 0)
    }
  } finally { globalThis.setInterval = realSet; globalThis.clearInterval = realClear }
})
