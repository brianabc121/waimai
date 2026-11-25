// Splits what was chosen with a dish into specs and toppings (加料), for
// LineExtras.vue. A topping is an option from a topping template, or a
// priced choice in an optional group (the same rule as the server's
// LineOption.Topping).
import { mn } from './i18n'
import type { Item, LineOption } from './types'

// base is the price of one before its toppings (null when not known).
export interface Extras { specs: string[]; toppings: { name: string; price: number }[]; base: number | null }

export function fromLineOptions(opts: LineOption[], unit: number | null = null): Extras {
  const out: Extras = { specs: [], toppings: [], base: null }
  for (const o of opts) {
    if (o.topping) out.toppings.push({ name: mn(o), price: o.price_delta })
    else out.specs.push(mn(o))
  }
  if (unit != null) out.base = unit - out.toppings.reduce((n, x) => n + x.price, 0)
  return out
}

export function fromCart(it: Item | undefined, ids: number[]): Extras {
  const out: Extras = { specs: [], toppings: [], base: null }
  if (!it) return out
  out.base = it.price
  for (const g of it.groups) for (const o of g.options) {
    if (!ids.includes(o.id)) continue
    if (g.topping_template_id || (g.min_select === 0 && o.price_delta > 0)) out.toppings.push({ name: mn(o), price: o.price_delta })
    else { out.specs.push(mn(o)); out.base! += o.price_delta }
  }
  return out
}
