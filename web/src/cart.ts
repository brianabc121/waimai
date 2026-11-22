import { reactive, watch } from 'vue'
import type { Item } from './types'

// basket is 1 to MAX_BASKETS: a customer may keep the dishes of one order apart (one bowl each, say).
export interface CartLine { key: string; item_id: number; qty: number; option_ids: number[]; note: string; basket: number }
export const MAX_BASKETS = 4

interface CartState { slug: string; service: 'delivery' | 'pickup'; zoneId: number | null; lines: CartLine[]
  // The basket new dishes go into.
  basket: number
  // The delivery pin of stores that locate customers on a map.
  lat: number | null; lng: number | null; placeLabel: string
  name: string; addressLine: string; addressUnit: string; addressNote: string; note: string }

const empty = (slug: string): CartState => ({ slug, service: 'delivery', zoneId: null, lines: [], basket: 1, lat: null, lng: null, placeLabel: '', name: '', addressLine: '', addressUnit: '', addressNote: '', note: '' })

export const cart = reactive<CartState>(empty(''))

// One cart per store, kept in localStorage so a reload does not lose the selection.
export function useCart(slug: string) {
  if (cart.slug !== slug) {
    let saved: Partial<CartState> = {}
    try { saved = JSON.parse(localStorage.getItem('cart:' + slug) || '{}') } catch { /* ignore */ }
    Object.assign(cart, empty(slug), saved, { slug })
    // Carts saved before baskets existed have none.
    if (!(cart.basket >= 1 && cart.basket <= MAX_BASKETS)) cart.basket = 1
    for (const l of cart.lines) {
      if (!(l.basket >= 1 && l.basket <= MAX_BASKETS)) l.basket = 1
      l.key = lineKey(l.basket, l.item_id, l.option_ids, l.note)
    }
  }
  return cart
}

watch(cart, c => { if (c.slug) localStorage.setItem('cart:' + c.slug, JSON.stringify(c)) }, { deep: true })

const lineKey = (basket: number, itemId: number, ids: number[], note: string) => `${basket}|${itemId}:${ids.join(',')}:${note.trim()}`

export function addLine(item: Item, optionIds: number[], qty: number, note: string) {
  const ids = [...optionIds].sort((a, b) => a - b)
  const key = lineKey(cart.basket, item.id, ids, note)
  const existing = cart.lines.find(l => l.key === key)
  if (existing) existing.qty = Math.min(99, existing.qty + qty)
  else cart.lines.push({ key, item_id: item.id, qty, option_ids: ids, note: note.trim(), basket: cart.basket })
}

// moveLine puts a line into another basket, joining the same dish already there.
export function moveLine(line: CartLine, basket: number) {
  const key = lineKey(basket, line.item_id, line.option_ids, line.note)
  const there = cart.lines.find(l => l.key === key)
  if (there && there !== line) {
    there.qty = Math.min(99, there.qty + line.qty)
    cart.lines.splice(cart.lines.indexOf(line), 1)
  } else { line.basket = basket; line.key = key }
}

// quoteItems is the cart as the server takes it, with the baskets in use numbered from 1.
export function quoteItems() {
  const used = [...new Set(cart.lines.map(l => l.basket))].sort((a, b) => a - b)
  return cart.lines.map(l => ({ item_id: l.item_id, qty: l.qty, option_ids: l.option_ids, note: l.note, basket: used.indexOf(l.basket) + 1 }))
}

export function clearLines() { cart.lines = []; cart.basket = 1 }

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
