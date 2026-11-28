// Run with: node --test tests/share.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import { shareUrl } from '../src/share.ts'

test('a shared store page carries no query', () => {
  const at = new URL('https://order.example.com/s/tea-house?append=abc.DEF-secret&x=1#menu')
  const url = shareUrl(at.origin, at.pathname)
  assert.equal(url, 'https://order.example.com/s/tea-house')
  assert.ok(!url.includes('append') && !url.includes('secret'))
})
