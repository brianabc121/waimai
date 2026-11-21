import { reactive, watch } from 'vue'
import type { Item } from './types'

export interface CartLine { key: string; item_id: number; qty: number; option_ids: number[]; note: string }

interface CartState { slug: string; service: 'delivery' | 'pickup'; zoneId: number | null; lines: CartLine[]
  name: string; addressLine: string; addressUnit: string; addressNote: string; note: string }

const empty = (slug: string): CartState => ({ slug, service: 'delivery', zoneId: null, lines: [], name: '', addressLine: '', addressUnit: '', addressNote: '', note: '' })

export const cart = reactive<CartState>(empty(''))

// One cart per store, kept in localStorage so a reload does not lose the selection.
export function useCart(slug: string) {
  if (cart.slug !== slug) {
    let saved: Partial<CartState> = {}
    try { saved = JSON.parse(localStorage.getItem('cart:' + slug) || '{}') } catch { /* ignore */ }
    Object.assign(cart, empty(slug), saved, { slug })
  }
  return cart
}

watch(cart, c => { if (c.slug) localStorage.setItem('cart:' + c.slug, JSON.stringify(c)) }, { deep: true })

export function addLine(item: Item, optionIds: number[], qty: number, note: string) {
  const ids = [...optionIds].sort((a, b) => a - b)
  const key = `${item.id}:${ids.join(',')}:${note.trim()}`
  const existing = cart.lines.find(l => l.key === key)
  if (existing) existing.qty = Math.min(99, existing.qty + qty)
  else cart.lines.push({ key, item_id: item.id, qty, option_ids: ids, note: note.trim() })
}

export function clearLines() { cart.lines = [] }

// linePrice returns the unit price of a cart line against the current menu,
// or null when the item or one of its options is gone.
export function linePrice(line: CartLine, items: Map<number, Item>): number | null {
  const it = items.get(line.item_id)
  if (!it) return null
  let p = it.price
  const all = new Map(it.groups.flatMap(g => g.options).map(o => [o.id, o]))
  for (const id of line.option_ids) {
    const o = all.get(id)
    if (!o) return null
    p += o.price_delta
  }
  return p
}
