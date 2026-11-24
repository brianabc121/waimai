import { reactive, watch } from 'vue'
import type { Item } from './types'

// basket is 1 to MAX_BASKETS: a customer may keep the dishes of one order apart (one bowl each, say).
export interface CartLine { key: string; item_id: number; qty: number; option_ids: number[]; note: string; basket: number
  // the chosen flavour notes (docs/NOTE_TEMPLATE_DESIGN.md); carts saved before them have none
  note_tags?: number[] }
export const MAX_BASKETS = 4

interface CartState { slug: string; service: 'delivery' | 'pickup'; zoneId: number | null; lines: CartLine[]
  // The basket new dishes go into.
  basket: number
  // The delivery pin of stores that locate customers on a map.
  lat: number | null; lng: number | null; placeLabel: string
  // The saved address this order goes to (logged-in customers).
  addressId: number | null
  name: string; addressLine: string; addressUnit: string; addressNote: string; note: string }

const empty = (slug: string): CartState => ({ slug, service: 'delivery', zoneId: null, lines: [], basket: 1, lat: null, lng: null, placeLabel: '', addressId: null, name: '', addressLine: '', addressUnit: '', addressNote: '', note: '' })

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
      if (!Array.isArray(l.note_tags)) l.note_tags = []
      l.key = lineKey(l.basket, l.item_id, l.option_ids, l.note, l.note_tags)
    }
  }
  return cart
}

watch(cart, c => { if (c.slug) localStorage.setItem('cart:' + c.slug, JSON.stringify(c)) }, { deep: true })

const lineKey = (basket: number, itemId: number, ids: number[], note: string, tags: number[] = []) =>
  `${basket}|${itemId}:${ids.join(',')}:${[...tags].sort((a, b) => a - b).join(',')}:${note.trim()}`

export function addLine(item: Item, optionIds: number[], qty: number, note: string, tags: number[] = []) {
  const ids = [...optionIds].sort((a, b) => a - b)
  const key = lineKey(cart.basket, item.id, ids, note, tags)
  const existing = cart.lines.find(l => l.key === key)
  if (existing) existing.qty = Math.min(99, existing.qty + qty)
  else cart.lines.push({ key, item_id: item.id, qty, option_ids: ids, note: note.trim(), basket: cart.basket, note_tags: [...tags] })
}

// moveLine puts a line into another basket, joining the same dish already there.
export function moveLine(line: CartLine, basket: number) {
  const key = lineKey(basket, line.item_id, line.option_ids, line.note, line.note_tags)
  const there = cart.lines.find(l => l.key === key)
  if (there && there !== line) {
    there.qty = Math.min(99, there.qty + line.qty)
    cart.lines.splice(cart.lines.indexOf(line), 1)
  } else { line.basket = basket; line.key = key }
}

// quoteItems is the cart as the server takes it, with the baskets in use numbered from 1.
export function quoteItems() {
  const used = [...new Set(cart.lines.map(l => l.basket))].sort((a, b) => a - b)
  return cart.lines.map(l => ({ item_id: l.item_id, qty: l.qty, option_ids: l.option_ids, note: l.note, note_tag_ids: l.note_tags ?? [],
    basket: used.indexOf(l.basket) + 1 }))
}

// replaceCart fills a store's cart with the lines of an earlier order.
export function replaceCart(slug: string, service: 'delivery' | 'pickup', lines: Omit<CartLine, 'key'>[]) {
  useCart(slug)
  cart.service = service
  cart.basket = 1
  cart.lines = []
  for (const l of lines) {
    const ids = [...l.option_ids].sort((a, b) => a - b)
    const basket = l.basket >= 1 && l.basket <= MAX_BASKETS ? l.basket : 1
    const tags = l.note_tags ?? []
    const key = lineKey(basket, l.item_id, ids, l.note, tags)
    const same = cart.lines.find(x => x.key === key)
    if (same) same.qty = Math.min(99, same.qty + l.qty)
    else cart.lines.push({ key, item_id: l.item_id, qty: l.qty, option_ids: ids, note: l.note.trim(), basket, note_tags: [...tags] })
  }
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

// pruneNoteTags drops flavour notes the store no longer offers from the cart
// (a tag renamed keeps its id; one deleted or paused is gone).
export function pruneNoteTags(items: Map<number, Item>) {
  for (const l of cart.lines) {
    const it = items.get(l.item_id)
    if (!it || !l.note_tags?.length) continue
    const live = new Set((it.notes ?? []).flatMap(g => g.tags.map(x => x.id)))
    const keep = l.note_tags.filter(id => live.has(id))
    if (keep.length !== l.note_tags.length) { l.note_tags = keep; l.key = lineKey(l.basket, l.item_id, l.option_ids, l.note, keep) }
  }
}
