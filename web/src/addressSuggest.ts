// Address suggestions, one module for every box an address is typed into
// (docs/ADDRESS_BOOK_DESIGN.md section 11): the order desk and the
// customer's place picker. The rule is the same everywhere: our own
// addresses first (the server's one module, app.SuggestAddresses), and
// Google only when nothing of ours matches or the person asks for more.
import { get } from './api'
import type { LatLng } from './geo'
import type { Place } from './maps'
import { addrParts } from './place'
import type { AddressHint } from './types'

// Who asks. desk: staff at a store's order desk (the phone the order is for,
// and the brand); otherwise a customer, at a store (slug) or around a point.
export type Ask =
  | { q: string; desk: number; brand: number | string; phone?: string }
  | { q: string; slug?: string; near?: LatLng | null }

// ownAddresses: what we have for the typed words. [] when the server cannot be reached.
export async function ownAddresses(ask: Ask): Promise<AddressHint[]> {
  const q = encodeURIComponent(ask.q.trim())
  let url: string
  if ('desk' in ask) url = `/merchant/branches/${ask.desk}/address-hints?q=${q}&phone=${encodeURIComponent(ask.phone ?? '')}&brand=${ask.brand}`
  else if (ask.slug) url = `/address-suggest?q=${q}&slug=${encodeURIComponent(ask.slug)}`
  else if (ask.near) url = `/address-suggest?q=${q}&lat=${ask.near[0]}&lng=${ask.near[1]}`
  else return []
  return (await get<{ addresses: AddressHint[] }>(url).catch(() => ({ addresses: [] as AddressHint[] }))).addresses ?? []
}

// Nothing of ours matches: Google is asked by itself after this pause, for at least this many letters.
export const AUTO_WAIT = 400, AUTO_MIN = 3
export const googleNext = (ours: AddressHint[], q: string) => !ours.length && [...q.trim()].length >= AUTO_MIN

// hintPlace shows one of our addresses the way a Google suggestion is shown:
// the place's name, then the street.
export function hintPlace(h: AddressHint): Place {
  const p = addrParts({ address_line: h.line, place_name: h.place_name })
  return { label: p.name, detail: p.street || undefined, id: h.place_id || undefined, at: h.lat != null && h.lng != null ? [h.lat, h.lng] : undefined, own: true }
}
