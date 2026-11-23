// Run with: node --test tests/zawgyi.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import { looksZawgyi, zawgyiIn } from '../src/zawgyi.ts'

test('Zawgyi is told from Unicode Burmese', () => {
  // coffee, Myanmar, Mandalay, fried rice, noodle soup in Unicode
  for (const u of ['ကော်ဖီ', 'မြန်မာ', 'မန္တလေး', 'ထမင်းကြော်', 'မုန့်ဟင်းခါး', 'ကြက်သား ၂ ပွဲ', 'Coffee ကော်ဖီ'])
    assert.equal(looksZawgyi(u), false, u)
  // the same words typed in Zawgyi
  for (const z of ['ေကာ္ဖီ', 'ျမန္မာ', 'ထမင္းေၾကာ္', 'ၾကက္သား ၂ ပြဲ', 'Coffee ေကာ္ဖီ'])
    assert.equal(looksZawgyi(z), true, z)
  assert.equal(looksZawgyi('宫保鸡丁 Kung Pao'), false)
})

test('zawgyiIn walks a request body', () => {
  assert.deepEqual(zawgyiIn({ name: 'ေကာ္ဖီ', options: [{ name: 'ကော်ဖီ' }, { name: 'ျမန္မာ' }], price: 3 }), ['ေကာ္ဖီ', 'ျမန္မာ'])
})
