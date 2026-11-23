// Run with: node --test tests/zawgyi.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import { toUnicode, fixZawgyi } from '../src/zawgyi.ts'

test('Zawgyi is converted to Unicode, Unicode is kept', async () => {
  // coffee, Myanmar, fried rice, 2 chicken, typed in Zawgyi, and the Unicode forms
  const pairs = [['ေကာ္ဖီ', 'ကော်ဖီ'], ['ျမန္မာ', 'မြန်မာ'], ['ထမင္းေၾကာ္', 'ထမင်းကြော်'], ['ၾကက္သားထမင္းေၾကာ္', 'ကြက်သားထမင်းကြော်']]
  for (const [z, u] of pairs) {
    assert.deepEqual(await toUnicode(z), { body: u, converted: [u] }, z)
    assert.deepEqual(await toUnicode(u), { body: u, converted: [] }, u)
  }
  for (const s of ['မန္တလေး', 'မုန့်ဟင်းခါး', '宫保鸡丁 Kung Pao'])
    assert.deepEqual((await toUnicode(s)).converted, [], s)
})

test('a request body is walked, other values untouched', async () => {
  const body = { name: 'ျမန္မာ', options: [{ name: 'ကော်ဖီ' }, { name: 'ေကာ္ဖီ' }], price: 3, on: true, note: null }
  const r = await toUnicode(body)
  assert.deepEqual(r.body, { name: 'မြန်မာ', options: [{ name: 'ကော်ဖီ' }, { name: 'ကော်ဖီ' }], price: 3, on: true, note: null })
  assert.deepEqual(r.converted, ['မြန်မာ', 'ကော်ဖီ'])
  assert.equal(body.name, 'ျမန္မာ', 'the original body is not changed')
})

test('merchant, platform and customer writes are converted, sign-in and secrets are not', async () => {
  assert.equal((await fixZawgyi('/merchant/dishes', 'ျမန္မာ')).body, 'မြန်မာ')
  assert.equal((await fixZawgyi('/admin/mall/categories', 'ျမန္မာ')).body, 'မြန်မာ')
  assert.deepEqual((await fixZawgyi('/orders', { note: 'ျမန္မာ' })).body, { note: 'မြန်မာ' })
  assert.deepEqual((await fixZawgyi('/me/addresses', { detail: 'ျမန္မာ', door_code: 'ျမန္မာ' })).body, { detail: 'မြန်မာ', door_code: 'ျမန္မာ' })
  assert.equal((await fixZawgyi('/auth/login', 'ျမန္မာ')).body, 'ျမန္မာ')
  assert.deepEqual((await fixZawgyi('/me/password', { password: 'ျမန္မာ' })).body, { password: 'ျမန္မာ' })
})
