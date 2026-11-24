// Promotions on chosen dishes (docs/PRICING_REVIEW.md step 4), worked out
// in the cart the same way the server does (internal/app/rules.go): each
// dish takes the dish promotion that saves the most on it, before any
// promotion on the whole order.
import type { Promotion } from './types'

export const isDishPromo = (p: { kind: string }) => p.kind === 'dish_percent' || p.kind === 'dish_second'

const half = (a: number, bp: number) => Math.floor((a * bp + 5000) / 10000) // half up, as the server rounds

// unitsOff is what promotion p takes off one dish, given what each unit costs.
function unitsOff(p: Promotion, units: number[]): number {
  const sorted = [...units].sort((a, b) => b - a)
  if (p.kind === 'dish_percent') return half(sorted.reduce((s, u) => s + u, 0), p.value)
  let off = 0
  for (let i = sorted.length - Math.floor(sorted.length / 2); i < sorted.length; i++) off += half(sorted[i], p.value)
  return off
}

// dishSavings is what the dish promotions take off lines in all.
export function dishSavings(promos: Promotion[], lines: { item_id: number; unit: number; qty: number }[]): number {
  const dish = promos.filter(isDishPromo)
  if (!dish.length) return 0
  const units = new Map<number, number[]>()
  for (const l of lines) {
    if (l.unit <= 0 || l.qty <= 0) continue
    const u = units.get(l.item_id) ?? []
    for (let i = 0; i < l.qty; i++) u.push(l.unit)
    units.set(l.item_id, u)
  }
  let total = 0
  for (const [item, us] of units) {
    const cost = us.reduce((s, u) => s + u, 0)
    let best = 0
    for (const p of dish) if (p.item_ids?.includes(item)) best = Math.max(best, Math.min(unitsOff(p, us), cost))
    total += best
  }
  return total
}

// dishPromoOf is the dish promotion shown on a dish, if any.
export const dishPromoOf = (promos: Promotion[], itemID: number) => promos.find(p => isDishPromo(p) && p.item_ids?.includes(itemID))
