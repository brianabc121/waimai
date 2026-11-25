// Splits what was chosen with a dish into specs and toppings (加料), for
// LineExtras.vue. A topping is an option from a topping template, or a
// priced choice in an optional group (the same rule as the server's
// LineOption.Topping).
import { mn } from './i18n'
import type { Item, LineOption } from './types'

export interface Extras { specs: string[]; toppings: { name: string; price: number }[] }

export function fromLineOptions(opts: LineOption[]): Extras {
  const out: Extras = { specs: [], toppings: [] }
  for (const o of opts) {
    if (o.topping) out.toppings.push({ name: mn(o), price: o.price_delta })
    else out.specs.push(mn(o))
  }
  return out
}

export function fromCart(it: Item | undefined, ids: number[]): Extras {
  const out: Extras = { specs: [], toppings: [] }
  if (!it) return out
  for (const g of it.groups) for (const o of g.options) {
    if (!ids.includes(o.id)) continue
    if (g.topping_template_id || (g.min_select === 0 && o.price_delta > 0)) out.toppings.push({ name: mn(o), price: o.price_delta })
    else out.specs.push(mn(o))
  }
  return out
}
