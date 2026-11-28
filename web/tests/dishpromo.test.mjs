// Run with: node --test tests/dishpromo.test.mjs
// The cart works dish promotions out as internal/app/rules.go does
// (internal/app/dish_promo_test.go has the same numbers).
import test from 'node:test'
import assert from 'node:assert/strict'
import { dishSavings } from '../src/dishPromo.ts'

const tea = 1, chicken = 2
const half = { id: 1, kind: 'dish_second', value: 5000, item_ids: [tea] }
const pct = { id: 2, kind: 'dish_percent', value: 2000, item_ids: [chicken] }
const m5 = { id: 3, kind: 'amount_off', value: 5000, threshold: 30000 }

test('second at half price and a dish at 20% off', () => {
  const lines = [{ item_id: tea, unit: 6900, qty: 3 }, { item_id: chicken, unit: 16900, qty: 1 }]
  assert.equal(dishSavings([half, pct, m5], lines), 3450 + 3380)
  assert.equal(dishSavings([half], [{ item_id: tea, unit: 6900, qty: 1 }]), 0)
  assert.equal(dishSavings([half], [{ item_id: tea, unit: 6900, qty: 4 }]), 6900)
  // docs/PRICING_CHECK.md S19: in pairs, so the fifth is at its price again
  assert.equal(dishSavings([half], [{ item_id: tea, unit: 3000, qty: 4 }]), 3000)
  assert.equal(dishSavings([half], [{ item_id: tea, unit: 3000, qty: 5 }]), 3000)
})

test('the cheaper units get the second-one price, and a dish takes its best promotion', () => {
  const lines = [{ item_id: tea, unit: 8000, qty: 1 }, { item_id: tea, unit: 6000, qty: 1 }]
  assert.equal(dishSavings([half], lines), 3000)
  const big = { id: 4, kind: 'dish_percent', value: 3000, item_ids: [tea] }
  assert.equal(dishSavings([half, big], lines), 4200)
})
