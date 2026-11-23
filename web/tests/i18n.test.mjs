// Every language other than Chinese and English must have every text of the
// customer, store and staff pages, with the same {placeholders} as English.
import test from 'node:test'
import assert from 'node:assert/strict'
import { check } from '../scripts/i18n.mjs'

test('every language is complete and keeps the placeholders', () => {
  assert.deepEqual(check(), [])
})
