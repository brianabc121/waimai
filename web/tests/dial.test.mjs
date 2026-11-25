// Run with: node --test tests/dial.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import * as d from '../src/dial.ts'

test('the code follows the country, +63 where it is not known', () => {
  assert.equal(d.dialOf('PH'), '63')
  assert.equal(d.dialOf('sg'), '65')
  assert.equal(d.dialOf('US'), '1')
  assert.equal(d.dialOf(''), '63')
  assert.equal(d.dialOf(undefined), '63')
  assert.equal(d.dialOf('ZZ'), '63')
})

test('a saved number splits into its code and the rest', () => {
  assert.deepEqual(d.splitPhone('+639175550123'), { dial: '63', local: '9175550123' })
  assert.deepEqual(d.splitPhone('+8613800138000'), { dial: '86', local: '13800138000' })
  assert.deepEqual(d.splitPhone('+85291234567'), { dial: '852', local: '91234567' })
  assert.deepEqual(d.splitPhone('+12125550123'), { dial: '1', local: '2125550123' })
  assert.equal(d.splitPhone('09175550123'), null)
  assert.equal(d.splitPhone(''), null)
})

test('no code is the start of another, so the split is not ambiguous', () => {
  for (const a of d.DIALS) for (const b of d.DIALS) assert.ok(a === b || !b.startsWith(a), `${a} / ${b}`)
})

test('what is typed becomes one number', () => {
  assert.equal(d.e164('63', '917 555 0123'), '+639175550123')
  assert.equal(d.e164('63', '0917-555-0123'), '+639175550123')
  assert.equal(d.e164('63', '+63 917 555 0123'), '+639175550123')
  assert.equal(d.e164('63', '639175550123'), '+639175550123')
  assert.equal(d.e164('86', '138 0013 8000'), '+8613800138000')
  assert.equal(d.e164('65', '+65 9123 4567'), '+6591234567')
})

test('validity', () => {
  assert.ok(d.phoneOk('63', '917 555 0123'))
  assert.ok(!d.phoneOk('63', '817 555 0123'))
  assert.ok(!d.phoneOk('63', '917 555 012'))
  assert.ok(d.phoneOk('65', '9123 4567'))
  assert.ok(d.phoneOk('86', '13800138000'))
  assert.ok(!d.phoneOk('86', '12'))
  assert.ok(!d.phoneOk('86', '1380013800012345'))
})

test('pretty', () => {
  assert.equal(d.prettyPhone('+639175550123'), '+63 917 555 0123')
  assert.equal(d.prettyPhone('+8613800138000'), '+86 13800138000')
  assert.equal(d.prettyPhone('hello'), 'hello')
})

test('a login number must be a mobile where the rule is known', () => {
  assert.ok(d.phoneOk('86', '138 0013 8000', true))
  assert.ok(!d.phoneOk('86', '21 1234 5678', true))
  assert.ok(d.phoneOk('86', '21 1234 5678'))           // a landline is fine for an address
  assert.ok(d.phoneOk('65', '8123 4567', true))
  assert.ok(d.phoneOk('1', '(415) 555-0123', true))
  assert.ok(!d.phoneOk('63', '2 8123 4567'))            // at home the old rule stays
})

test('a trunk 0 and the code written twice are dropped, except in Italy', () => {
  assert.equal(d.e164('44', '07911 123456'), '+447911123456')
  assert.equal(d.e164('86', '0086 138 0013 8000'), '+8613800138000')
  assert.equal(d.e164('39', '06 1234 5678'), '+390612345678')
})

test('countries have names in the language shown', () => {
  assert.equal(d.countryName('PH', 'en'), 'Philippines')
  assert.equal(d.countryName('CN', 'zh-CN'), '中国')
  for (const cc of d.COMMON) assert.ok(d.CODES[cc], cc)
})
