<script setup lang="ts">
// Order for a customer: the customer sent their order over WeChat, LINE,
// WhatsApp or the phone, and staff put it in here (docs/MANUAL_ORDER_DESIGN.md).
// Left: the dishes as a table. Right: the store member, the address and the
// dish search. The store's own members are found by number, phone or name;
// "new member" holds the next number for an hour.
import { computed, defineAsyncComponent, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ApiError, get, post, put } from '../api'
import { stableKey } from '../retry'
import { has, mn, money, pick, promoLabel, t, whenText } from '../i18n'
import { can, currentRole, session } from '../session'
import { dialOf, e164, localDigits, splitPhone } from '../dial'
import type { AddonQuote, AddressHint, Adjust, DinePresets, Item, OptionGroup, Order, PayOptions, Quote, Storefront, StoreMember, Totals } from '../types'
import AdjustSheet from './checkout/AdjustSheet.vue'
import ApproveDialog from './ApproveDialog.vue'
import { channelName, pctText } from '../checkout'
import { addonErr, addonNo, batchSource, batchTitle, itemsByBatch } from '../addon'
import { presetList } from './dine/strings'
import type { Picked } from './PlacePicker.vue'
import { googleMapsLink, mapsKey, suggestPlaces, type Place } from '../maps'
import './manualStrings'
import './dine/strings'
import NotePicker from './NotePicker.vue'
import { cartTags, hasNotes, toggleTag } from '../notes'
import { deliveryBrands, useBrandPick } from '../brands'
import BrandSeg from './brands/BrandSeg.vue'

const PlacePicker = defineAsyncComponent(() => import('./PlacePicker.vue'))
const router = useRouter()
const route = useRoute()

// 点单 · 外卖下单: delivery and pickup. Dine-in has its own page
// (DineOrderView); this one is remembered for old "order for a customer" links.
// ?append=<order id> adds dishes to that order (docs/ADD_ON_ORDER_DESIGN.md §6.2).
const appendId = Number(route.query.append) || 0
try { localStorage.setItem('mo-mode', 'delivery') } catch { /* private window */ }
const bid = session.branchId!
const base = `/merchant/branches/${bid}`
const SOURCES = ['wechat', 'line', 'whatsapp', 'phone', 'other']
// The brand the order is placed under (docs/MULTI_BRAND_DESIGN.md §7): the
// member, the order number (HCD000223) and the order follow it.
const { pick: brandPick, brand } = useBrandPick('delivery', deliveryBrands)
const bq = () => `brand=${brandPick.value}`

const store = ref<Storefront | null>(null)
const loadError = ref('')
const cur = computed(() => store.value?.branch.currency ?? currentRole()?.currency ?? 'PHP')
const m = (v: number) => money(v, cur.value)
const items = computed<Item[]>(() => (store.value?.categories ?? []).flatMap(c => c.items))
const dial = computed(() => dialOf(store.value?.branch.country_code))
// The menu is the brand's (docs/MULTI_BRAND_DESIGN.md §5): its dishes, names
// and prices; dishes the brand does not sell leave the cart when it changes.
async function loadStore(brandId: number) {
  const slug = currentRole()?.slug
  if (!slug) return
  try { store.value = await get<Storefront>(`/stores/${slug}` + (brandId ? `?brand=${brandId}` : '')) } catch (e) { loadError.value = (e as Error).message }
  const sold = new Map(items.value.map(i => [i.id, i]))
  lines.value = lines.value.filter(l => sold.has(l.item.id)).map(l => ({ ...l, item: sold.get(l.item.id)! }))
}
onMounted(async () => {
  await loadStore(appendId ? 0 : brandPick.value)
  if (store.value && !store.value.branch.delivery_enabled && store.value.branch.pickup_enabled) service.value = 'pickup'
  try { payOpts.value = await get<PayOptions>(`${base}/pay-options`) } catch { /* only cash on delivery */ }
})
watch(brandPick, v => { if (!appendId) void loadStore(v) })

// ---------- adding to an order ----------
// The order's own dishes are shown locked above the ones being added; the
// customer, the address and the fees stay as they are on the order.
const target = ref<Order | null>(null)
const targetErr = ref('')
const roundPresets = ref<string[]>(presetList(null, 'round_tags'))
const roundTags = ref<string[]>([])
const aq = ref<AddonQuote | null>(null)
async function loadTarget() {
  try { target.value = await get<Order>(`/merchant/orders/${appendId}`) } catch { targetErr.value = t('ao.m.missing') }
  // dishes are added at the order's brand's names and prices
  if (target.value?.brand_id && target.value.brand_id !== store.value?.brand?.id) void loadStore(target.value.brand_id)
}
if (appendId) {
  void loadTarget()
  if (can('table.use')) void get<{ presets: DinePresets }>(`/merchant/branches/${session.branchId}/dine-desk`)
    .then(r => { roundPresets.value = presetList(r.presets, 'round_tags') }).catch(() => {})
}
function toggleRound(tag: string) {
  roundTags.value = roundTags.value.includes(tag) ? roundTags.value.filter(x => x !== tag) : [...roundTags.value, tag]
}
const targetState = computed(() => {
  const o = target.value
  if (!o) return ''
  const parts = [t('a.' + o.acceptance_status)]
  if (o.acceptance_status === 'accepted') parts.push(t('p.' + o.packing_status))
  if (o.delivery) parts.push(`${t('rider')} ${o.delivery.rider_name}`)
  return parts.join(' · ')
})

// ---------- the dishes ----------
// Options of a group that takes one (大碗/小碗) are the dish's spec, shown
// after its name; the others are add-ons, each with a count, in the yellow block.
interface Line { key: number; item: Item; qty: number; picks: Record<number, number>; note: string; tags: number[] }
let nextKey = 1
const lines = ref<Line[]>([])
const selKey = ref(0)
const isSpec = (g: OptionGroup) => g.max_select === 1
const optionsOf = (l: Line) => l.item.groups.flatMap(g => g.options.map(o => ({ g, o, n: l.picks[o.id] ?? 0 })))
const specText = (l: Line) => optionsOf(l).filter(x => x.n && isSpec(x.g)).map(x => mn(x.o)).join('、')
const addonsOf = (l: Line) => optionsOf(l).filter(x => x.n && !isSpec(x.g))
const hasAddons = (l: Line) => l.item.groups.some(g => !isSpec(g))
const unit = (l: Line) => l.item.price + optionsOf(l).reduce((s, x) => s + x.o.price_delta * x.n, 0)
// how many different options of the group are picked: the group's limit is on kinds, not portions
const groupCount = (l: Line, g: OptionGroup) => g.options.reduce((s, o) => s + (l.picks[o.id] ? 1 : 0), 0)
const count = computed(() => lines.value.reduce((s, l) => s + l.qty, 0))
const subtotal = computed(() => lines.value.reduce((s, l) => s + unit(l) * l.qty, 0))

function addLine(item: Item, picks: Record<number, number> = {}) {
  const l: Line = { key: nextKey++, item, qty: 1, picks, note: '', tags: [] }
  lines.value.push(l)
  selKey.value = l.key
}
function copyLine(l: Line) {
  const c: Line = { ...l, key: nextKey++, picks: { ...l.picks }, tags: [...l.tags] }
  lines.value.splice(lines.value.indexOf(l) + 1, 0, c)
  selKey.value = c.key
}
// The note cell (docs/NOTE_TEMPLATE_DESIGN.md 6.2): on a dish with flavour
// notes it opens a panel of its tags under the cell (from the bottom on a
// phone). What is typed filters the tags; Enter picks the first match, or
// keeps the words as the note when nothing matches. Tab or Esc closes it.
const noteKey = ref(0)
const notePos = ref<Record<string, string>>({})
const noteLine = computed(() => lines.value.find(x => x.key === noteKey.value && hasNotes(x.item)) ?? null)
const noteTagsOf = (l: Line) => cartTags(l.item, l.tags)
function openNote(l: Line, e: Event) {
  selKey.value = l.key
  if (!hasNotes(l.item)) return
  const r = ((e.target as HTMLElement).closest('.note-cell') as HTMLElement).getBoundingClientRect()
  const w = Math.max(r.width, 300), left = Math.min(r.left, window.innerWidth - w - 8)
  notePos.value = { top: `${r.bottom + 4}px`, left: `${Math.max(8, left)}px`, width: `${w}px` }
  noteKey.value = l.key
  // a phone: the panel covers the row, so it has its own box to type in
  if (window.matchMedia('(max-width: 1000px)').matches) void nextTick(() => notePopIn.value?.focus())
}
const notePopIn = ref<HTMLInputElement>()
function noteMatch(l: Line) {
  const q = l.note.trim().toLowerCase()
  if (!q) return null
  for (const g of l.item.notes ?? []) for (const x of g.tags) if (mn(x).toLowerCase().includes(q) || x.name.toLowerCase().includes(q)) return { g, x }
  return null
}
function noteEnter(l: Line) {
  const hit = noteMatch(l)
  if (hit) { if (!l.tags.includes(hit.x.id)) l.tags = toggleTag(l.tags, hit.g, hit.x.id); l.note = '' }
  else noteKey.value = 0
}
function dropTag(l: Line, id: number) { l.tags = l.tags.filter(x => x !== id) }
function removeLine(l: Line) { lines.value = lines.value.filter(x => x !== l) }
function setQty(l: Line, d: number) { l.qty = Math.max(1, Math.min(999, l.qty + d)) }
function setAddon(l: Line, g: OptionGroup, id: number, d: number) {
  const n = (l.picks[id] ?? 0) + d
  if (d > 0 && !l.picks[id] && groupCount(l, g) >= g.max_select) return
  if (n > 20) return
  if (n <= 0) delete l.picks[id]
  else l.picks[id] = n
}

// The add-on box under the selected dish: type a code or a name, Enter adds one.
const addonQ = ref('')
const addonHits = computed(() => {
  const l = lines.value.find(x => x.key === selKey.value)
  const q = addonQ.value.trim().toLowerCase()
  if (!l || !q) return []
  // an exact topping code (AC02) comes first, then codes and names that contain the text
  const rank = (o: { code?: string; name: string; name_en: string }) => (o.code ?? '').toLowerCase() === q ? 0
    : (o.code ?? '').toLowerCase().startsWith(q) ? 1 : o.name.toLowerCase().includes(q) || o.name_en.toLowerCase().includes(q) ? 2 : 9
  return optionsOf(l).filter(x => !isSpec(x.g) && !x.o.sold_out && rank(x.o) < 9)
    .sort((x, y) => rank(x.o) - rank(y.o)).slice(0, 6)
})
function addAddon(l: Line) {
  const x = addonHits.value[0]
  if (!x) return
  setAddon(l, x.g, x.o.id, 1)
  addonQ.value = ''
}

// ---------- choosing a dish ----------
const pickQ = ref(''), pickAt = ref(0), pickOpen = ref(false)
const pickInput = ref<HTMLInputElement>()
const hits = computed(() => {
  const q = pickQ.value.trim().toLowerCase()
  if (!q) return []
  const code = items.value.filter(i => (i.code ?? '').toLowerCase().startsWith(q))
  const name = items.value.filter(i => !code.includes(i) && (i.name.toLowerCase().includes(q) || i.name_en.toLowerCase().includes(q)))
  return [...code, ...name].slice(0, 8)
})
watch(pickQ, () => { pickAt.value = 0; pickOpen.value = true })
// On a phone the list of dishes opens under the box: bring the box to the top first.
function pickFocus(e: FocusEvent) {
  pickOpen.value = true
  if (window.matchMedia('(max-width: 1000px)').matches) (e.target as HTMLElement).closest('.pick')?.scrollIntoView({ block: 'start', behavior: 'smooth' })
}
function pickKey(e: KeyboardEvent) {
  if (e.key === 'ArrowDown') { pickAt.value = Math.min(pickAt.value + 1, hits.value.length - 1); e.preventDefault() }
  else if (e.key === 'ArrowUp') { pickAt.value = Math.max(pickAt.value - 1, 0); e.preventDefault() }
  else if (e.key === 'Enter') { e.preventDefault(); const it = hits.value[pickAt.value]; if (it) choose(it) }
  else if (e.key === 'Escape') pickOpen.value = false
}
function choose(it: Item) {
  if (it.sold_out) return
  pickQ.value = ''; pickOpen.value = false
  if (it.groups.some(g => isSpec(g) || g.min_select > 0)) { openSpec(it); return }
  addLine(it)
  void nextTick(() => pickInput.value?.focus())
}

// The spec sheet: shown for a dish with a size or a choice it must have.
const spec = ref<{ item: Item; picks: Record<number, number> } | null>(null)
function openSpec(it: Item) {
  const picks: Record<number, number> = {}
  for (const g of it.groups) if (isSpec(g) && g.min_select > 0) {
    const o = g.options.find(o => !o.sold_out)
    if (o) picks[o.id] = 1
  }
  spec.value = { item: it, picks }
}
function specToggle(g: OptionGroup, id: number) {
  const p = spec.value!.picks
  if (isSpec(g)) { for (const o of g.options) delete p[o.id]; p[id] = 1; return }
  if (p[id]) delete p[id]
  else if (g.options.reduce((s, o) => s + (p[o.id] ? 1 : 0), 0) < g.max_select) p[id] = 1
}
const specMissing = computed(() => spec.value?.item.groups.find(g => g.options.reduce((s, o) => s + (spec.value!.picks[o.id] ?? 0), 0) < g.min_select) ?? null)
function specAdd() {
  if (!spec.value || specMissing.value) return
  addLine(spec.value.item, { ...spec.value.picks })
  spec.value = null
  void nextTick(() => pickInput.value?.focus())
}

// ---------- the member ----------
const f = reactive({ no: 0, status: '' as '' | 'temp' | 'active', expires: null as string | null, phone: '', name: '', source: '',
  contact: '', line: '', unit: '', addrNote: '', lat: null as number | null, lng: null as number | null, note: '' })
const stats = ref<StoreMember['stats'] | null>(null)
const memberQ = ref(''), memberHits = ref<StoreMember[]>([]), memberAt = ref(0), memberOpen = ref(false)
const memberMsg = ref<'' | 'found' | 'not_found'>('')
const pad = (n: number) => String(n).padStart(6, '0')
let memberTimer = 0, quietQ = false
// the number put in the box by the page itself is not a search
function showNo(n: number) { quietQ = true; memberQ.value = pad(n); void nextTick(() => { quietQ = false; memberOpen.value = false }) }
watch(memberQ, q => {
  clearTimeout(memberTimer)
  // hits of what was typed before must not be taken for this
  memberAt.value = 0; memberHits.value = []
  if (!q.trim() || quietQ) return
  memberTimer = window.setTimeout(async () => {
    const r = await get<{ members: StoreMember[] }>(`${base}/members?q=${encodeURIComponent(q.trim())}&${bq()}`).catch(() => ({ members: [] }))
    if (memberQ.value === q) { memberHits.value = r.members; memberOpen.value = true }
  }, 200)
})
function memberKey(e: KeyboardEvent) {
  if (e.key === 'ArrowDown') { memberAt.value = Math.min(memberAt.value + 1, memberHits.value.length - 1); e.preventDefault() }
  else if (e.key === 'ArrowUp') { memberAt.value = Math.max(memberAt.value - 1, 0); e.preventDefault() }
  else if (e.key === 'Enter') { e.preventDefault(); void findMember() }
  else if (e.key === 'Escape') memberOpen.value = false
}
function fill(x: StoreMember) {
  const sp = splitPhone(x.phone)
  Object.assign(f, { no: x.no, status: x.status, expires: x.expires_at, name: x.name, source: x.source, contact: x.contact,
    line: x.address_line, unit: x.address_unit, addrNote: x.address_note, lat: x.lat, lng: x.lng, note: x.note,
    phone: sp && sp.dial === dial.value ? sp.local : x.phone })
  stats.value = x.stats ?? null
  showNo(x.no)
  if (f.line) feeOverride.value = null
}
async function openMember(no: number) {
  try {
    fill(await get<StoreMember>(`${base}/members/${no}?${bq()}`))
    memberMsg.value = 'found'
  } catch {
    // another customer is being looked for: forget the one shown
    const q = memberQ.value
    if (f.no) clearMember()
    memberQ.value = q; memberMsg.value = 'not_found'
  }
}
async function findMember() {
  const hit = memberOpen.value ? memberHits.value[memberAt.value] : undefined
  if (hit) return openMember(hit.no)
  const q = memberQ.value.trim()
  if (/^\d{1,8}$/.test(q)) return openMember(Number(q))
  if (memberHits.value.length === 1) return openMember(memberHits.value[0].no)
  memberMsg.value = q ? 'not_found' : ''
}
async function newMember() {
  error.value = ''
  try {
    const x = await post<StoreMember>(`${base}/members?${bq()}`)
    // a new number for the customer being typed in: keep what is already filled
    Object.assign(f, { no: x.no, status: x.status, expires: x.expires_at })
    stats.value = null; memberMsg.value = ''
    showNo(x.no)
  } catch (e) { error.value = errText(e) }
}
function clearMember() {
  Object.assign(f, { no: 0, status: '', expires: null, phone: '', name: '', source: '', contact: '', line: '', unit: '', addrNote: '', lat: null, lng: null, note: '' })
  stats.value = null; memberQ.value = ''; memberMsg.value = ''
}
const now = ref(Date.now())
const clock = window.setInterval(() => (now.value = Date.now()), 20000)
onBeforeUnmount(() => clearInterval(clock))
const minutesLeft = computed(() => f.expires ? Math.max(0, Math.ceil((new Date(f.expires).getTime() - now.value) / 60000)) : 0)

const phoneE164 = computed(() => f.phone.trim().startsWith('+') ? '+' + f.phone.replace(/\D/g, '') : f.phone.trim() ? e164(dial.value, f.phone) : '')
const memberBody = () => ({ no: f.no, phone: phoneE164.value, name: f.name, source: f.source, contact: f.contact,
  address_line: service.value === 'delivery' || f.line ? f.line : '', address_unit: f.unit, address_note: f.addrNote,
  lat: f.lat, lng: f.lng, note: f.note })
const saved = ref(false)
async function saveMember() {
  error.value = ''
  try {
    fill(await put<StoreMember>(`${base}/members/${f.no}?${bq()}`, memberBody()))
    saved.value = true; setTimeout(() => (saved.value = false), 2500)
  } catch (e) { error.value = errText(e) }
}

// ---------- the address ----------
const service = ref<'delivery' | 'pickup'>('delivery')
const zoneId = ref<number | null>(null)
const listMode = computed(() => store.value?.branch.zone_mode === 'list')
const feeOverride = ref<number | null>(null)
const picking = ref(false)
const hintsOpen = ref(false), hints = ref<AddressHint[]>([])
let hintTimer = 0, hintSeq = 0
// The address box offers, in order: this number's own addresses, the
// platform's address book (this store's region), the store's past deliveries,
// and last "More": Google's suggestions, fetched only once staff move onto it,
// since every Google lookup is paid for.
const mapKey = ref('')
onMounted(() => { void mapsKey().then(k => (mapKey.value = k)) })
const gq = reactive({ list: [] as Place[], loading: false, asked: '' })
type Row = { k: 'hint'; h: AddressHint; head?: string } | { k: 'more' } | { k: 'google'; p: Place; head?: string }
const HEADS: Record<string, string> = { mine: 'vo.hints_mine', book: 'vo.hints_book', store: 'vo.hints_store' }
const rows = computed<Row[]>(() => {
  const out: Row[] = []
  for (const kind of ['mine', 'book', 'store']) {
    hints.value.filter(h => (h.kind ?? (h.mine ? 'mine' : 'store')) === kind)
      .forEach((h, i) => out.push({ k: 'hint', h, head: i === 0 ? t(HEADS[kind]) : undefined }))
  }
  if (mapKey.value && f.line.trim()) {
    out.push({ k: 'more' })
    gq.list.forEach((p, i) => out.push({ k: 'google', p, head: i === 0 ? t('vo.hints_google') : undefined }))
  }
  return out
})
const hintAt = ref(-1)
function lineInput() {
  // a typed address is no longer where the pin is
  f.lat = f.lng = null
  gq.list = []; gq.asked = ''; hintAt.value = -1
  clearTimeout(hintTimer)
  hintTimer = window.setTimeout(loadHints, 250)
}
async function loadHints() {
  const n = ++hintSeq
  const r = await get<{ addresses: AddressHint[] }>(`${base}/address-hints?q=${encodeURIComponent(f.line.trim())}&phone=${encodeURIComponent(phoneE164.value)}&${bq()}`)
    .catch(() => ({ addresses: [] }))
  if (n !== hintSeq) return
  hints.value = r.addresses
  // only while the address is still being typed in
  hintsOpen.value = document.activeElement?.getAttribute('data-testid') === 'mo-address'
}
// "More" was reached: ask Google once for what is typed now.
async function loadGoogle() {
  const q = f.line.trim()
  if (!mapKey.value || !q || gq.asked === q) return
  gq.asked = q; gq.loading = true
  const b = store.value?.branch
  const near = b?.lat != null && b?.lng != null ? [b.lat, b.lng] as [number, number] : undefined
  const list = await suggestPlaces(mapKey.value, q, { country: b?.country_code || undefined, near }).catch(() => [])
  if (gq.asked !== q) return
  gq.list = list; gq.loading = false
}
function useHint(h: AddressHint) {
  Object.assign(f, { line: h.line, unit: h.unit || f.unit, addrNote: h.note || f.addrNote, lat: h.lat, lng: h.lng })
  hintsOpen.value = false
}
async function useGoogle(p: Place) {
  hintsOpen.value = false
  const line = [p.label, p.detail].filter(Boolean).join(', ')
  const at = p.at ?? await p.locate?.() ?? null
  f.line = line.slice(0, 300)
  f.lat = at ? at[0] : null; f.lng = at ? at[1] : null
  gq.list = []; gq.asked = ''
}
function pickRow(r: Row) {
  if (r.k === 'hint') useHint(r.h)
  else if (r.k === 'google') void useGoogle(r.p)
  else void loadGoogle()
}
function hover(i: number) {
  hintAt.value = i
  if (rows.value[i]?.k === 'more') void loadGoogle()
}
function addrKey(e: KeyboardEvent) {
  if (!hintsOpen.value || !rows.value.length) return
  if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
    e.preventDefault()
    const n = rows.value.length
    hover(e.key === 'ArrowDown' ? (hintAt.value + 1) % n : (hintAt.value - 1 + n) % n)
  } else if (e.key === 'Enter' && hintAt.value >= 0 && !e.isComposing) {
    e.preventDefault()
    const r = rows.value[hintAt.value]
    if (r) pickRow(r)
  } else if (e.key === 'Escape') hintsOpen.value = false
}
const viewLink = (lat: number | null, lng: number | null) => lat != null && lng != null ? googleMapsLink(lat, lng) : ''
const later = (fn: () => void) => setTimeout(fn, 150)
const closeHints = () => later(() => (hintsOpen.value = false))
const origin = location.origin
function picked(p: Picked) {
  f.lat = p.lat; f.lng = p.lng
  if (!f.line.trim()) f.line = [p.name, p.address].filter(Boolean).join(', ')
  picking.value = false
}
function adjustFee() {
  const v = prompt(t('vo.fee_prompt'), feeOverride.value != null ? String(feeOverride.value / 100) : '')
  if (v === null) return
  feeOverride.value = v.trim() === '' || isNaN(Number(v)) ? null : Math.max(0, Math.round(Number(v) * 100))
}

// ---------- when, notes ----------
const when = ref('')
const orderNote = ref(''), staffNote = ref('')
const accept = ref(localStorage.getItem('mo-accept') !== 'off')
watch(accept, v => localStorage.setItem('mo-accept', v ? 'on' : 'off'))

// ---------- the price ----------
const quote = ref<Quote | null>(null)
const quoteError = ref('')
const error = ref('')
const orderBody = () => ({
  brand_id: brandPick.value, member: memberBody(), service_type: service.value, zone_id: listMode.value ? zoneId.value : null,
  items: lines.value.map(l => ({ item_id: l.item.id, qty: l.qty, note: l.note, note_tag_ids: l.tags,
    option_ids: Object.entries(l.picks).flatMap(([id, n]) => Array(n).fill(Number(id))) })),
  scheduled_for: when.value || null, delivery_fee: service.value === 'delivery' ? feeOverride.value : null,
  note: orderNote.value, staff_note: staffNote.value, accept: accept.value,
  adjust: adjust.value, pay: payChannel.value ? { type: 'online', channel_id: payChannel.value } : null,
})

// ---------- discount at checkout, paid online (docs/CHECKOUT_DISCOUNT_DESIGN.md §5, §6.2) ----------
const adjust = ref<Adjust | null>(null)
const adjOpen = ref(false)
const approval = ref('')
const approving = ref(false)
const approvedNote = ref('')
const payOpts = ref<PayOptions | null>(null)
const payChannel = ref(0)
function openAdjust() {
  if (can('checkout.discount') || approval.value) adjOpen.value = true
  else approving.value = true
}
function approved(token: string, name: string) {
  approving.value = false; approval.value = token
  approvedNote.value = t('ck.approved', { name }); adjOpen.value = true
}
const previewAdj = async (a: Adjust): Promise<Totals> => (await post<Quote>(`${base}/staff-quotes`, { ...orderBody(), adjust: a.percent_bp || a.mode ? a : null })).totals
const isNarrow = () => window.matchMedia('(max-width: 700px)').matches
function setAdjust(a: Adjust | null) { adjOpen.value = false; adjust.value = a }
// a round-off or a typed amount no longer fits once the dishes change (§5.4)
watch(() => JSON.stringify(orderBody().items), () => {
  const a = adjust.value
  if (a?.mode) adjust.value = a.percent_bp ? { ...a, mode: '', amount: 0 } : null
})
const adjLabel = computed(() => {
  const a = adjust.value, x = totals.value
  if (!a) return t('ck.adj_none')
  const parts: string[] = []
  if (a.percent_bp) parts.push(pctText(a.percent_bp) + (x?.staff_discount ? ` −${m(x.staff_discount)}` : ''))
  if (a.mode === 'round') parts.push(t('ck.round_off') + (x?.round_off ? ` −${m(x.round_off)}` : ''))
  if (a.mode === 'set_total') parts.push(a.amount === 0 ? t('ck.free') : t('ck.set_off') + (x?.round_off ? ` −${m(x.round_off)}` : ''))
  return parts.join(' · ') + (a.reason ? ` · ${a.reason}` : '')
})
let quoteTimer = 0, quoteSeq = 0
watch(() => JSON.stringify([brandPick.value, orderBody().items, service.value, zoneId.value, f.lat, f.lng, feeOverride.value, when.value, phoneE164.value, roundTags.value, adjust.value]), () => {
  clearTimeout(quoteTimer)
  quoteTimer = window.setTimeout(requote, 300)
})
async function requote() {
  const seq = ++quoteSeq
  if (appendId) return requoteAddon(seq)
  if (!lines.value.length) { quote.value = null; quoteError.value = ''; return }
  try {
    const q = await post<Quote>(`${base}/staff-quotes`, orderBody())
    if (seq === quoteSeq) { quote.value = q; quoteError.value = '' }
  } catch (e) {
    if (seq !== quoteSeq) return
    quote.value = null
    // a delivery not yet located is the usual state while typing: say nothing
    quoteError.value = e instanceof ApiError && e.code === 'OUT_OF_ZONE' && f.lat == null && !listMode.value ? '' : errText(e)
  }
}
async function requoteAddon(seq: number) {
  if (!lines.value.length) { aq.value = null; quoteError.value = ''; return }
  try {
    const q = await post<AddonQuote>(`/merchant/orders/${appendId}/append/quote`, { items: orderBody().items, round_tags: roundTags.value })
    if (seq === quoteSeq) { aq.value = q; quoteError.value = '' }
  } catch (e) {
    if (seq !== quoteSeq) return
    aq.value = null; quoteError.value = addonErr(e)
  }
}
const totals = computed(() => quote.value?.totals ?? null)
const total = computed(() => totals.value?.total ?? (lines.value.length ? subtotal.value + (store.value?.branch.packaging_fee ?? 0) : 0))
const extra = computed(() => aq.value?.extra ?? subtotal.value)
const promo = computed(() => {
  // the offer this order gets, else the one it could get
  const tt = totals.value
  if (tt?.promotion_id) return pick(tt.promotion_name, tt.promotion_name_en)
  const p = store.value?.promotions?.find(p => p.live_now && (service.value === 'delivery' ? p.applies_delivery : p.applies_pickup))
  return p ? promoLabel(p, cur.value) : ''
})
const feeText = computed(() => {
  if (service.value === 'pickup') return '—'
  if (!totals.value) return t('vo.unknown')
  return totals.value.delivery_fee ? m(totals.value.delivery_fee) : t('vo.free')
})

function errText(e: unknown): string {
  if (e instanceof ApiError) {
    const k = `vo.err.${e.code}`
    if (has(k)) return t(k, { s: e.details?.minimum != null ? m(e.details.minimum) : '' })
  }
  return (e as Error).message
}

// ---------- copying ----------
async function copy(text: string) {
  try { await navigator.clipboard.writeText(text); return } catch { /* fall back below */ }
  const ta = document.createElement('textarea')
  ta.value = text; document.body.appendChild(ta); ta.select()
  try { document.execCommand('copy') } finally { ta.remove() }
}
const toast = ref('')
function flash(s: string) { toast.value = s; setTimeout(() => { if (toast.value === s) toast.value = '' }, 2200) }

// The message for the customer. Before ordering it lists what was put in; after, it is the order itself.
function message(o?: Order, token?: string): string {
  const name = brand.value && deliveryBrands().length > 1 ? brand.value.name : store.value ? store.value.branch.name : ''
  const out: string[] = []
  if (o) out.push(t('vo.msg.head', { store: name, no: o.public_no }))
  const rows = o ? o.items.map(i => ({ code: items.value.find(x => x.id === i.item_id)?.code ?? '', name: i.name, qty: i.qty, sum: i.line_total,
    opts: [...i.options.map(x => x.name), ...(i.note_tags ?? []).map(x => x.name)], note: i.note }))
    : lines.value.map(l => ({ code: l.item.code ?? '', name: l.item.name, qty: l.qty, sum: unit(l) * l.qty,
      opts: [...optionsOf(l).filter(x => x.n).map(x => x.n > 1 ? `${x.o.name} ×${x.n}` : x.o.name), ...noteTagsOf(l)], note: l.note }))
  for (const r of rows) {
    const extra = [...r.opts, r.note].filter(Boolean).join('，')
    out.push(`${r.code ? r.code + ' ' : ''}${r.name}${extra ? `（${extra}）` : ''} ×${r.qty}　${m(r.sum)}`)
  }
  const tt = o?.totals ?? totals.value
  if (tt) {
    const svc = o?.service_type ?? service.value
    out.push(t('vo.msg.fees', { p: m(tt.packaging_fee), d: tt.discount ? '−' + m(tt.discount) : '—',
      f: svc === 'pickup' ? '—' : tt.delivery_fee ? m(tt.delivery_fee) : t('vo.free') }))
    out.push(t('vo.msg.total', { s: m(tt.total), pay: t(svc === 'pickup' ? 'pay_at_pickup' : 'cod') }))
  }
  if (o) {
    if (o.service_type === 'pickup') out.push(t('vo.msg.pickup', { s: o.pickup_code }))
    else out.push(t('vo.msg.addr', { s: [o.address_line, o.address_unit].filter(Boolean).join('，') }))
    if (o.scheduled_for) out.push(t('vo.msg.when', { s: whenText(o.scheduled_for, o.timezone) }))
    if (token) out.push(t('vo.msg.link', { s: `${location.origin}/o/${token}` }))
  } else if (service.value === 'delivery' && f.line) out.push(t('vo.msg.addr', { s: [f.line, f.unit].filter(Boolean).join('，') }))
  return out.join('\n')
}
async function copyDraft() { await copy(message()); flash(t('vo.copied')) }

// ---------- replies (话术) ----------
const scriptsOpen = ref(false), scriptsEditing = ref(false), scriptsText = ref('')
const scripts = computed(() => (localStorage.getItem('mo-scripts') ?? t('vo.scripts_default', { store: (deliveryBrands().length > 1 ? brand.value?.name : '') || store.value?.branch.name || '' }))
  .split('\n').map(s => s.trim()).filter(Boolean))
const scriptsVer = ref(0)
const scriptList = computed(() => { void scriptsVer.value; return scripts.value })
function editScripts() { scriptsText.value = scriptList.value.join('\n'); scriptsEditing.value = true }
function saveScripts() { localStorage.setItem('mo-scripts', scriptsText.value); scriptsVer.value++; scriptsEditing.value = false }
async function copyScript(s: string) { await copy(s); flash(t('vo.copied')); scriptsOpen.value = false }

// ---------- placing ----------
const placing = ref(false)
// Pressing again with nothing changed is the same request: after a lost
// answer it finds the order the first try made instead of making another.
const placeKeys = stableKey(), addonKeys = stableKey()
const done = ref<{ order: Order; token: string; text: string } | null>(null)
async function place() {
  error.value = ''
  if (!lines.value.length) { error.value = t('vo.need_items'); return }
  if (appendId) return placeAddon()
  placing.value = true
  try {
    const body = { ...orderBody(), expected_total: adjust.value || payChannel.value ? totals.value?.total ?? null : null }
    const r = await post<{ order: Order; member: StoreMember; access_token: string }>(`${base}/staff-orders`, body,
      { 'Idempotency-Key': placeKeys.for(body), ...(adjust.value && approval.value ? { 'X-Approval': approval.value } : {}) })
    placeKeys.done()
    if (adjust.value) approval.value = ''
    const text = message(r.order, r.access_token)
    done.value = { order: r.order, token: r.access_token, text }
    await copy(text)
    void loadRecent()
  } catch (e) { error.value = errText(e) } finally { placing.value = false }
}
const doneSeq = ref(0)
async function placeAddon() {
  placing.value = true
  try {
    const body = { items: orderBody().items, round_tags: roundTags.value, expected_total: aq.value?.after.total ?? null }
    const r = await post<{ order: Order; seq: number; access_token: string }>(`/merchant/orders/${appendId}/append`,
      body, { 'Idempotency-Key': addonKeys.for(body) })
    addonKeys.done()
    const text = addonMessage(r.order, r.seq, r.access_token)
    doneSeq.value = r.seq
    done.value = { order: r.order, token: r.access_token, text }
    target.value = r.order
    // the dishes are on the order now, shown with it above
    lines.value = []; aq.value = null; roundTags.value = []
    await copy(text)
  } catch (e) {
    error.value = addonErr(e)
    if (e instanceof ApiError && e.code === 'QUOTE_CHANGED') void requote()
    if (e instanceof ApiError && e.code === 'ADDON_CLOSED') void loadTarget()
  } finally { placing.value = false }
}
// The message for the customer after an add-on: what was added and the new total.
function addonMessage(o: Order, seq: number, token: string): string {
  const b = o.batches?.find(x => x.seq === seq)
  const out = [o.customer_name ? t('ao.msg.head', { name: o.customer_name, no: o.public_no }) : t('ao.msg.head0', { no: o.public_no })]
  for (const i of o.items.filter(i => i.batch_seq === seq)) {
    const code = items.value.find(x => x.id === i.item_id)?.code ?? ''
    const extra = [...i.options.map(x => x.name), ...(i.note_tags ?? []).map(x => x.name), i.note].filter(Boolean).join(' ')
    out.push(`${code ? code + ' ' : ''}${i.name} ×${i.qty}${extra ? `（${extra}）` : ''} ${m(i.line_total)}`)
  }
  if (aq.value?.promo_up && o.totals.promotion_name) out.push(t('ao.m.promo_up', { s: pick(o.totals.promotion_name, o.totals.promotion_name_en) }))
  out.push(t(o.service_type === 'pickup' ? 'ao.msg.total_p' : 'ao.msg.total_d', { s: m(o.totals.total), o: m(b?.total_before ?? o.totals.total) }))
  if (token) out.push(t('ao.msg.link', { s: `${location.origin}/o/${token}` }))
  return out.join('\n')
}
function next() {
  done.value = null
  lines.value = []; clearMember()
  feeOverride.value = null; zoneId.value = null; when.value = ''; orderNote.value = ''; staffNote.value = ''
  adjust.value = null; payChannel.value = 0; approvedNote.value = ''
  quote.value = null; quoteError.value = ''; error.value = ''
  void nextTick(() => document.querySelector<HTMLInputElement>('[data-testid="mo-member"]')?.focus())
}
function onKey(e: KeyboardEvent) { if (done.value && e.key === 'Enter') { e.preventDefault(); if (appendId) router.push('/merchant/orders'); else next() } }
onMounted(() => window.addEventListener('keydown', onKey))
onBeforeUnmount(() => window.removeEventListener('keydown', onKey))

// ---------- the column on the right: waiting and recent orders by staff ----------
const recent = ref<Order[]>([])
async function loadRecent() {
  const r = await get<{ orders: Order[] }>(`${base}/orders?scope=active`).catch(() => ({ orders: [] as Order[] }))
  recent.value = r.orders
}
void loadRecent()
const waiting = computed(() => recent.value.filter(o => o.acceptance_status === 'pending'))
const byStaff = computed(() => recent.value.filter(o => o.order_source).slice(0, 8))
const srcText = (s: string) => has(`vo.src.${s}`) ? t(`vo.src.${s}`) : s
</script>

<template>
  <div class="mo warm" data-testid="manual-order">
    <div class="mo-top">
      <button class="back" @click="router.push('/merchant/orders')"><span class="rtl-flip">‹</span> {{ t('vo.back') }}</button>
      <h1 v-if="appendId" data-testid="ao-title">{{ t('ao.m.title', { no: target?.public_no ?? '' }) }}</h1>
      <h1 v-else data-testid="mo-title">{{ t('pos.delivery') }}</h1>
      <span v-if="appendId && target" class="tstate">{{ targetState }}</span>
      <BrandSeg v-if="!appendId && deliveryBrands().length > 1" v-model="brandPick" :brands="deliveryBrands()" class="mo-brands" data-testid="mo-brands"
        @update:model-value="clearMember()" />
      <span class="store">{{ store?.branch.name }}</span>
    </div>
    <div v-if="loadError || targetErr" class="banner bad" style="margin:12px">{{ loadError || targetErr }}</div>
    <div v-if="target?.addon && !target.addon.open" class="banner bad" style="margin:12px" data-testid="ao-closed">{{ t('ao.closed') }}</div>
    <div class="mo-body">
      <!-- the dishes -->
      <div v-if="noteLine" class="note-mask" @click="noteKey = 0"></div>
      <div v-if="noteLine" class="note-pop" :style="notePos" data-testid="mo-note-pop" @mousedown.prevent>
        <div class="np-head"><b>{{ mn(noteLine.item) }}</b><span class="muted small">{{ t('nt.free') }}</span>
          <button type="button" class="np-done" @click="noteKey = 0">{{ t('nt.done') }}</button></div>
        <input ref="notePopIn" v-model="noteLine.note" class="note-in np-in" maxlength="100" :placeholder="t('vo.note_ph')" data-testid="mo-note-pop-in"
          @keydown.enter.prevent="noteEnter(noteLine)" @keydown.esc="noteKey = 0" />
        <NotePicker v-model="noteLine.tags" :groups="noteLine.item.notes ?? []" :filter="noteLine.note.trim()" />
        <div v-if="noteLine.note.trim() && !noteMatch(noteLine)" class="muted small np-foot">{{ t('nt.enter_note', { s: noteLine.note.trim() }) }}</div>
      </div>
      <section class="order">
        <div class="thead"><span>{{ t('vo.col.no') }}</span><span>{{ t('vo.col.item') }}</span><span class="c">{{ t('vo.col.qty') }}</span>
          <span class="r">{{ t('vo.col.price') }}</span><span class="r">{{ t('vo.col.sum') }}</span><span>{{ t('vo.col.note') }}</span><span></span></div>
        <div class="rows" data-testid="mo-lines">
          <template v-if="target">
            <template v-for="g in itemsByBatch(target)" :key="'b' + g.seq">
              <div class="bh" data-testid="ao-batch">🔒 {{ batchTitle(g.seq) }}<template v-if="g.batch"> · {{ whenText(g.batch.at, target.timezone).split(' ').pop() }} · {{ batchSource(target, g.batch) }}</template>
                <template v-if="g.batch?.round_tags?.length"> · {{ g.batch.round_tags.join(' · ') }}</template></div>
              <div v-for="it in g.items" :key="it.id" class="tr lock" data-testid="ao-orig">
                <span class="no"></span>
                <span class="name"><span v-if="items.find(x => x.id === it.item_id)?.code" class="code">{{ items.find(x => x.id === it.item_id)?.code }}</span><b>{{ pick(it.name, it.name_en) }}</b>
                  <span v-if="it.options.length" class="spec">{{ it.options.map(o => pick(o.name, o.name_en)).join('、') }}</span></span>
                <span class="c">{{ it.qty }}</span><span class="r">{{ m(it.unit_price) }}</span><span class="r b">{{ m(it.line_total) }}</span>
                <span class="muted small">{{ [...(it.note_tags ?? []).map(x => x.name), it.note].filter(Boolean).join(' · ') }}</span><span></span>
              </div>
            </template>
            <div class="bh add" data-testid="ao-this">＋ {{ t('ao.m.this') }} · {{ batchTitle((target.batches?.length ?? 1) + 1) }}</div>
          </template>
          <div v-if="!lines.length" class="empty muted">{{ t('vo.empty') }}</div>
          <template v-for="(l, i) in lines" :key="l.key">
            <div class="tr" :class="{ sel: l.key === selKey }" data-testid="mo-line" @click="selKey = l.key">
              <span class="no">{{ i + 1 }}</span>
              <span class="name"><span v-if="l.item.code" class="code">{{ l.item.code }}</span><b>{{ mn(l.item) }}</b>
                <span v-if="specText(l)" class="spec">{{ specText(l) }}</span></span>
              <span class="c"><span class="qty"><button :aria-label="'−'" @click.stop="setQty(l, -1)">−</button><b data-testid="mo-qty">{{ l.qty }}</b><button class="f" data-testid="mo-plus" @click.stop="setQty(l, 1)">+</button></span></span>
              <span class="r">{{ m(unit(l)) }}</span><span class="r b">{{ m(unit(l) * l.qty) }}</span>
              <div class="note-cell" :class="{ open: noteKey === l.key && hasNotes(l.item), tpl: hasNotes(l.item) }" data-testid="mo-note" @click.stop="openNote(l, $event)">
                <span v-for="(n, ti) in noteTagsOf(l)" :key="ti" class="nchip">{{ n }}<button type="button" aria-label="✕" @click.stop="dropTag(l, l.tags[ti])">✕</button></span>
                <input v-model="l.note" class="note-in" :placeholder="l.tags.length ? '' : hasNotes(l.item) ? t('nt.choose') + ' / ' + t('vo.note_ph') : t('vo.note_ph')" maxlength="100"
                  data-testid="mo-note-in" @focus="openNote(l, $event)" @keydown.enter.prevent="noteEnter(l)" @keydown.esc="noteKey = 0" @keydown.tab="noteKey = 0" />
              </div>
              <span class="ops"><button :title="t('vo.copy_line')" @click.stop="copyLine(l)">⧉</button><button class="del" :title="t('vo.remove')" data-testid="mo-remove" @click.stop="removeLine(l)">✕</button></span>
            </div>
            <div v-if="hasAddons(l) && (addonsOf(l).length || l.key === selKey)" class="tr sub">
              <span></span>
              <div class="addons">
                <div v-for="a in addonsOf(l)" :key="a.o.id" class="addon" data-testid="mo-addon">
                  <span>+ {{ mn(a.o) }}</span>
                  <span class="qty sm"><button @click="setAddon(l, a.g, a.o.id, -1)">−</button>{{ a.n }}<button class="f" @click="setAddon(l, a.g, a.o.id, 1)">+</button></span>
                  <span class="r">{{ m(a.o.price_delta * a.n) }}</span>
                </div>
                <div v-if="l.key === selKey" class="addon-add">
                  <input v-model="addonQ" :placeholder="'＋ ' + t('vo.addon_ph')" data-testid="mo-addon-q" @keydown.enter.prevent="addAddon(l)" />
                  <span class="addon-hits">
                    <button v-for="x in (addonQ ? addonHits : optionsOf(l).filter(x => !isSpec(x.g) && !x.o.sold_out).slice(0, 8))" :key="x.o.id" class="chip"
                      @click="setAddon(l, x.g, x.o.id, 1); addonQ = ''"><small v-if="x.o.code" class="ocode">{{ x.o.code }}</small>{{ mn(x.o) }} {{ x.o.price_delta ? '+' + m(x.o.price_delta) : '' }}</button>
                  </span>
                </div>
              </div>
            </div>
          </template>
        </div>
        <div class="foot">
          <div v-if="appendId" class="tot ao-tot" data-testid="ao-bar">
            <span><span class="k">{{ t('vo.count') }}</span><b class="n">{{ count }}</b></span>
            <span><span class="k">{{ t('ao.m.before') }}</span>{{ target ? m(target.totals.total) : '—' }}</span>
            <span>→</span>
            <span><span class="k">{{ t('ao.m.after') }}</span><b data-testid="ao-after">{{ aq ? m(aq.after.total) : '—' }}</b></span>
            <span><span class="k">{{ t('ao.m.extra') }}</span><b class="n" data-testid="ao-extra">{{ aq ? '+' + m(aq.extra) : '—' }}</b></span>
            <span v-if="aq?.promo_up" class="tag gold" data-testid="ao-promo-up">{{ t('ao.m.promo_up', { s: pick(aq.after.promotion_name, aq.after.promotion_name_en) }) }}</span>
            <span class="grow"></span>
          </div>
          <div v-else class="tot">
            <span><span class="k">{{ t('vo.count') }}</span><b class="n">{{ count }}</b></span>
            <span><span class="k">{{ t('vo.packaging') }}</span>{{ totals ? m(totals.packaging_fee) : '—' }}</span>
            <span><span class="k">{{ t('vo.delivery_fee') }}</span>{{ feeText }}</span>
            <span><span class="k">{{ t('vo.discount') }}</span>{{ totals?.discount ? '−' + m(totals.discount) : '—' }}</span>
            <span v-if="totals?.staff_discount || totals?.round_off" class="gold-k" data-testid="mo-adj-amt"><span class="k">{{ t('ck.staff_disc') }}</span>−{{ m((totals.staff_discount ?? 0) + (totals.round_off ?? 0)) }}</span>
            <span><span class="k">{{ t('vo.total') }}</span><b data-testid="mo-total">{{ m(total) }}</b></span>
            <span class="grow"></span>
            <button class="btn copy" :disabled="!lines.length" @click="copyDraft">{{ t('vo.copy_order') }}</button>
          </div>
          <div v-if="target" class="cust">
            <span>{{ t('vo.f.name') }}</span><span>{{ target.customer_name || '—' }}</span>
            <span>{{ t('vo.f.phone') }}</span><span>{{ target.contact_phone || target.customer_phone || '—' }}</span>
            <span>{{ t('vo.f.address') }}</span><span>{{ target.service_type === 'pickup' ? t('vo.pickup') + ' · ' + target.pickup_code : [target.address_line, target.address_unit].filter(Boolean).join(' · ') }}</span>
          </div>
          <div v-else class="cust">
            <span>{{ t('vo.f.member') }}</span><span>{{ f.no ? pad(f.no) : '—' }}<template v-if="f.status === 'temp'">（{{ t('vo.temp') }}）</template></span>
            <span>{{ t('vo.f.name') }}</span><span>{{ f.name || '—' }}</span>
            <span>{{ t('vo.f.source') }}</span><span>{{ f.source ? srcText(f.source) : '—' }}</span>
            <span>{{ t('vo.f.phone') }}</span><span>{{ phoneE164 || '—' }}</span>
            <span>{{ t('vo.f.address') }}</span><span>{{ service === 'pickup' ? t('vo.pickup') : [f.line, f.unit, quote?.zone_name].filter(Boolean).join(' · ') || '—' }}</span>
          </div>
          <div v-if="quoteError" class="qerr" data-testid="mo-quote-error">{{ quoteError }}</div>
        </div>
      </section>

      <!-- the member, the address, the dish search -->
      <section class="panel">
        <div class="pscroll">
          <template v-if="appendId">
            <div class="memberbox" data-testid="ao-ro">
              <div class="row between"><b>{{ target?.public_no }} · {{ target ? t(target.service_type) : '' }}</b>
                <span v-if="target?.member_no" class="muted">#{{ pad(target.member_no) }}</span></div>
              <div>{{ target?.customer_name }} · {{ target?.contact_phone || target?.customer_phone }}</div>
              <div v-if="target?.service_type === 'delivery'">{{ [target.address_line, target.address_unit].filter(Boolean).join('，') }}</div>
              <div v-if="target?.service_type === 'delivery'" class="small">{{ t('vo.delivery_fee') }}: {{ t('ao.m.fee_same', { s: target.totals.delivery_fee ? m(target.totals.delivery_fee) : t('vo.free') }) }}</div>
              <div class="muted small">{{ t('ao.m.ro') }}</div>
            </div>
            <div class="sec top"><span class="k">{{ t('ao.m.round_tags') }}</span>
              <div class="srcs"><button v-for="r in roundPresets" :key="r" :class="{ on: roundTags.includes(r) }" data-testid="ao-round" @click="toggleRound(r)">{{ r }}</button></div></div>
          </template>
          <template v-else>
          <div class="sec rel idrow">
            <span class="k">{{ t('vo.member_id') }}</span>
            <input v-model="memberQ" class="inp idin" data-testid="mo-member" :placeholder="t('vo.member_ph')" @keydown="memberKey"
              @focus="memberOpen = !!memberHits.length" @blur="later(() => (memberOpen = false))" />
            <button class="btn" data-testid="mo-find" @click="findMember">🔍 {{ t('vo.find') }}</button>
            <span class="grow"></span>
            <button class="btn primary" data-testid="mo-new-member" @click="newMember">{{ t('vo.new_member') }}</button>
            <div v-if="memberOpen && memberQ" class="drop" style="left:80px;right:0;top:40px">
              <div v-for="(x, i) in memberHits" :key="x.id" class="it" :class="{ on: i === memberAt }" @mousedown.prevent="openMember(x.no)">
                <em>{{ pad(x.no) }}</em>　{{ x.name }}　{{ x.phone }}<span class="grow"></span><span v-if="x.status === 'temp'" class="tag warn">{{ t('vo.temp') }}</span>
              </div>
              <div v-if="!memberHits.length" class="it muted">{{ t('vo.no_match') }}</div>
              <div class="hint">{{ t('vo.lookup_hint') }}</div>
            </div>
          </div>
          <div v-if="f.status === 'temp'" class="memberbox temp" data-testid="mo-temp">
            <div class="row between"><span class="row"><b>#{{ pad(f.no) }}</b><span class="tag warn">{{ t('vo.temp') }}</span></span>
              <span class="muted small">⏱ {{ t('vo.temp_left', { n: minutesLeft }) }}</span></div>
            <div class="muted small">{{ t('vo.temp_hint') }}</div>
          </div>
          <div v-else-if="f.no" class="memberbox" data-testid="mo-found">
            <div class="row between"><span class="row"><b>#{{ pad(f.no) }} {{ f.name }}</b><span class="tag ok">{{ t('vo.found') }}</span></span>
              <button class="btn small" @click="clearMember">✕</button></div>
            <div v-if="stats" class="stats"><span class="k">{{ t('vo.stats') }}</span><span>{{ t('vo.week') }} <b>{{ stats.week }}</b></span>
              <span>{{ t('vo.month') }} <b>{{ stats.month }}</b></span><span>{{ t('vo.quarter') }} <b>{{ stats.quarter }}</b></span>
              <span class="muted small">{{ t('vo.spent', { s: m(stats.spent) }) }}</span></div>
            <div v-if="stats && (stats.last_at || stats.favorites.length)" class="muted small">
              <template v-if="stats.last_at">{{ t('vo.last', { d: whenText(stats.last_at, store?.branch.timezone) }) }}</template>
              <template v-if="stats.favorites.length"> · {{ t('vo.fav', { s: stats.favorites.join('、') }) }}</template></div>
          </div>
          <div v-else class="memberbox"><div class="muted small" :class="{ red: memberMsg === 'not_found' }" data-testid="mo-member-hint">
            {{ memberMsg === 'not_found' ? t('vo.not_found') : t('vo.member_hint') }}</div></div>

          <div class="sec"><span class="k">{{ t('vo.phone') }} <i class="req">*</i></span>
            <label class="inp"><span class="cc">+{{ dial }}</span><input v-model="f.phone" data-testid="mo-phone" inputmode="tel" @blur="f.phone = f.phone.trim().startsWith('+') ? f.phone : localDigits(dial, f.phone)" /></label></div>
          <div class="sec"><span class="k">{{ t('vo.name') }}</span><input v-model="f.name" class="inp" data-testid="mo-name" maxlength="80" :placeholder="t('vo.name_ph')" /></div>
          <div class="sec"><span class="k">{{ t('vo.source') }} <i class="req">*</i></span>
            <div class="srcs"><button v-for="s in SOURCES" :key="s" :class="{ on: f.source === s }" :data-testid="'mo-src-' + s" @click="f.source = s">{{ t('vo.src.' + s) }}</button></div></div>
          <div class="sec"><span class="k">{{ t('vo.contact') }}</span><input v-model="f.contact" class="inp" maxlength="80" :placeholder="t('vo.contact_ph')" /></div>

          <div class="sec"><span class="k">{{ t('vo.address') }} <i v-if="service === 'delivery'" class="req">*</i></span>
            <div class="btnrow rel">
              <button class="btn blue" :disabled="service === 'pickup'" data-testid="mo-map" @click="picking = true">{{ t('vo.open_map') }}</button>
              <a class="btn blue" :class="{ off: service === 'pickup' || f.lat == null }" data-testid="mo-view" target="_blank" rel="noopener"
                :href="viewLink(f.lat, f.lng) || undefined" :title="f.lat == null ? t('vo.view_none') : t('vo.view_hint')"
                @click="f.lat == null && $event.preventDefault()">{{ t('vo.view') }}</a>
              <button class="btn blue" :class="{ on: feeOverride != null }" :disabled="service === 'pickup'" data-testid="mo-fee" @click="adjustFee">{{ t('vo.fee_adjust') }}</button>
              <button class="btn blue" :class="{ on: service === 'pickup' }" data-testid="mo-pickup"
                @click="service = service === 'pickup' ? 'delivery' : 'pickup'">{{ service === 'pickup' ? t('vo.delivery') : t('vo.pickup') }}</button>
              <button class="btn blue" @click="scriptsOpen = !scriptsOpen">{{ t('vo.scripts') }}</button>
              <div v-if="scriptsOpen" class="huashu">
                <div class="row between"><b>{{ t('vo.scripts_title') }}</b>
                  <button v-if="!scriptsEditing" class="btn small" @click="editScripts">{{ t('vo.scripts_edit') }}</button></div>
                <template v-if="scriptsEditing">
                  <textarea v-model="scriptsText" rows="6" class="inp" style="width:100%;margin-top:6px"></textarea>
                  <div class="muted small">{{ t('vo.scripts_hint') }}</div>
                  <div class="row" style="justify-content:flex-end;margin-top:6px"><button class="btn small" @click="scriptsEditing = false">{{ t('vo.cancel') }}</button>
                    <button class="btn small primary" @click="saveScripts">{{ t('vo.save') }}</button></div>
                </template>
                <button v-for="s in scriptList" v-else :key="s" class="it" @click="copyScript(s)">{{ s }}</button>
              </div>
            </div>
          </div>
          <template v-if="service === 'delivery'">
            <div class="sec"><span class="k"></span>
              <div class="chips">
                {{ t('vo.zone') }}
                <select v-if="listMode" v-model="zoneId" class="zsel" data-testid="mo-zone">
                  <option :value="null">{{ t('vo.unknown') }}</option>
                  <option v-for="z in store?.zones ?? []" :key="z.id" :value="z.id">{{ mn(z) }} · {{ m(z.fee) }}</option>
                </select>
                <span v-else class="tag" :class="{ bad: quote?.zone_name }">{{ quote?.zone_name || t('vo.unknown') }}</span>
                {{ t('vo.delivery_fee') }} <span class="tag">{{ feeText }}<template v-if="feeOverride != null"> · {{ t('vo.fee_set') }}</template></span>
                <template v-if="promo">{{ t('vo.promo') }} <span class="tag bad">{{ promo }}</span></template>
              </div>
            </div>
            <div class="sec rel"><span class="k kt"></span>
              <div class="grow stack0">
                <textarea v-model="f.line" class="inp ta" rows="2" data-testid="mo-address" maxlength="300" :placeholder="t('vo.addr_ph')"
                  @input="lineInput" @focus="loadHints" @blur="closeHints" @keydown="addrKey"></textarea>
                <input v-model="f.unit" class="inp" maxlength="100" :placeholder="t('vo.unit_ph')" />
                <div class="small" :class="f.lat != null ? 'okc' : 'muted'">{{ f.lat != null ? '📍 ' + t('vo.pinned') : t('vo.not_pinned') }}</div>
              </div>
              <div v-if="hintsOpen && rows.length" class="drop" style="left:80px;right:0;top:64px" data-testid="mo-hints">
                <template v-for="(r, i) in rows" :key="i">
                  <div v-if="r.k !== 'more' && r.head" class="grp">{{ r.head }}</div>
                  <div v-if="r.k === 'hint'" class="it" :class="{ on: i === hintAt }" :data-kind="r.h.kind" data-testid="mo-hint"
                    @mousedown.prevent="pickRow(r)" @mouseenter="hover(i)">
                    <span class="grow">{{ r.h.line }}<span v-if="r.h.unit" class="muted">，{{ r.h.unit }}</span></span>
                    <span v-if="r.h.zone_name" class="tag bad">{{ r.h.zone_name }}</span>
                    <a v-if="r.h.lat != null" class="look" :href="viewLink(r.h.lat, r.h.lng)" target="_blank" rel="noopener"
                      @mousedown.stop @click.stop>{{ t('vo.view_short') }}</a>
                  </div>
                  <div v-else-if="r.k === 'more'" class="it more" :class="{ on: i === hintAt }" data-testid="mo-more"
                    @mousedown.prevent="pickRow(r)" @mouseenter="hover(i)">
                    <span class="grow">{{ t('vo.more') }}</span>
                    <span class="muted small">{{ gq.loading ? t('vo.more_loading') : gq.asked && !gq.list.length ? t('vo.more_none') : t('vo.more_hint') }}</span>
                  </div>
                  <div v-else class="it" :class="{ on: i === hintAt }" data-testid="mo-google" @mousedown.prevent="pickRow(r)" @mouseenter="hover(i)">
                    <span class="grow">{{ r.p.label }}<span v-if="r.p.detail" class="muted"> · {{ r.p.detail }}</span></span>
                  </div>
                </template>
                <div class="hint">{{ t('vo.hints_map') }}</div>
              </div>
            </div>
          </template>
          <div class="sec"><span class="k">{{ t('vo.member_note') }}</span><input v-model="f.note" class="inp" maxlength="300" :placeholder="t('vo.member_note_ph')" /></div>
          <div v-if="f.status === 'temp'" class="sec"><span class="k"></span>
            <button class="btn grow" data-testid="mo-save-member" @click="saveMember">{{ saved ? '✓ ' + t('vo.member_saved') : t('vo.save_member') }}</button></div>
          <div class="sec"><span class="k">{{ t('vo.when') }}</span>
            <select v-model="when" class="inp">
              <option value="">{{ service === 'pickup' ? t('vo.asap_pickup') : t('vo.asap') }}</option>
              <option v-for="s in store?.slots ?? []" :key="s" :value="s">{{ whenText(s, store?.branch.timezone) }}</option>
            </select>
            <span class="k auto">{{ t('vo.pay') }}</span>
            <select v-model="payChannel" class="inp" data-testid="mo-pay">
              <option :value="0">{{ t(service === 'pickup' ? 'ck.pay_store' : 'ck.cod') }}</option>
              <option v-for="c in payOpts?.channels ?? []" :key="c.id" :value="c.id">{{ t('ck.paid_online') }} · {{ channelName(c) }}</option>
            </select>
          </div>
          <div class="sec"><span class="k">{{ t('ck.adj') }}</span>
            <button class="inp adjbtn" :class="{ set: !!adjust }" data-testid="mo-adj" @click="openAdjust">{{ adjLabel }}</button>
          </div>
          <div v-if="approvedNote && adjust" class="sec"><span class="k"></span><span class="muted small">{{ approvedNote }}</span></div>
          <div class="sec"><span class="k">{{ t('vo.order_note') }}</span><input v-model="orderNote" class="inp" maxlength="300" :placeholder="t('vo.order_note_ph')" /></div>
          <div class="sec"><span class="k">{{ t('vo.staff_note') }}</span><input v-model="staffNote" class="inp" maxlength="300" data-testid="mo-staff-note" :placeholder="t('vo.staff_note_ph')" /></div>
          </template>
          <div class="sec pick rel"><span class="k brandc">{{ t('vo.pick') }}</span>
            <input ref="pickInput" v-model="pickQ" class="inp" data-testid="mo-pick" autocomplete="off" @keydown="pickKey" @focus="pickFocus" @blur="later(() => (pickOpen = false))" />
            <div v-if="pickOpen && pickQ.trim()" class="drop" style="left:80px;right:0;top:44px" data-testid="mo-pick-drop">
              <div v-for="(it, i) in hits" :key="it.id" class="it" :class="{ on: i === pickAt, off: it.sold_out }" @mousedown.prevent="choose(it)">
                <em>{{ it.code }}</em> {{ mn(it) }}<span class="grow"></span>
                <span v-if="it.sold_out" class="tag">{{ t('vo.sold_out') }}</span>{{ m(it.price) }}
              </div>
              <div v-if="!hits.length" class="it muted">{{ t('vo.no_match') }}</div>
            </div>
          </div>
          <div class="muted small pickhint">{{ t('vo.pick_hint') }}</div>
        </div>
        <div class="confirmbar">
          <div v-if="error" class="banner bad" data-testid="mo-error">{{ error }}</div>
          <label v-if="!appendId" class="row small"><input v-model="accept" type="checkbox" /> {{ t('vo.accept') }}</label>
          <button v-if="appendId" class="btn primary confirm" data-testid="ao-place" :disabled="placing || !target?.addon?.open" @click="place">
            {{ placing ? t('vo.placing') : t('ao.m.confirm', { s: m(extra) }) }}</button>
          <button v-else class="btn primary confirm" data-testid="mo-place" :disabled="placing" @click="place">
            {{ placing ? t('vo.placing') : t('vo.confirm', { s: m(total) }) }}</button>
        </div>
      </section>

      <!-- waiting and recent -->
      <aside class="queue">
        <h3>{{ t('vo.waiting') }} <span v-if="waiting.length" class="pill-num">{{ waiting.length }}</span></h3>
        <router-link v-for="o in waiting.slice(0, 5)" :key="o.id" class="qi" to="/merchant/orders"><b>{{ o.public_no }}</b>
          <span>{{ t(o.service_type === 'pickup' ? 'vo.pickup' : 'vo.delivery') }} · {{ o.customer_name || o.customer_phone }} · {{ money(o.totals.total, o.currency) }}</span></router-link>
        <div v-if="!waiting.length" class="qi muted">—</div>
        <h3>{{ t('vo.recent') }}</h3>
        <router-link v-for="o in byStaff" :key="o.id" class="qi" to="/merchant/orders"><b>{{ o.public_no }}</b>
          <span>{{ o.customer_name || o.customer_phone }} · {{ money(o.totals.total, o.currency) }}</span>
          <span class="muted">{{ srcText(o.order_source ?? '') }} · {{ o.placed_by }}</span></router-link>
        <div v-if="!byStaff.length" class="qi muted">—</div>
      </aside>
    </div>

    <!-- options of a dish -->
    <div v-if="spec" class="sheet-mask" @click.self="spec = null">
      <div class="sheet" data-testid="mo-spec">
        <div class="sheet-head"><b>{{ t('vo.spec_title') }} · {{ mn(spec.item) }}</b><button class="btn small" @click="spec = null">✕</button></div>
        <div class="sheet-body">
          <div v-for="g in spec.item.groups" :key="g.id" class="sgroup">
            <div class="row between"><b>{{ mn(g) }}</b>
              <span class="muted small">{{ g.min_select ? t('vo.spec_need', { n: g.min_select }) : t('vo.spec_upto', { n: g.max_select }) }}</span></div>
            <div class="row wrap">
              <button v-for="o in g.options" :key="o.id" class="chip" :class="{ on: spec.picks[o.id] }" :disabled="o.sold_out" @click="specToggle(g, o.id)">
                <small v-if="o.code" class="ocode">{{ o.code }}</small>{{ mn(o) }}<template v-if="o.price_delta"> +{{ m(o.price_delta) }}</template><template v-if="o.sold_out"> · {{ t('vo.sold_out') }}</template></button>
            </div>
          </div>
        </div>
        <div class="sheet-foot"><button class="btn primary" style="width:100%" data-testid="mo-spec-add" :disabled="!!specMissing" @click="specAdd">{{ t('vo.spec_add') }}</button></div>
      </div>
    </div>

    <PlacePicker v-if="picking && store" class="mo-picker" :slug="store.branch.slug" :currency="cur" :start="f.lat != null && f.lng != null ? { lat: f.lat, lng: f.lng } : null"
      :name="f.line" :shop="store.branch.lat != null && store.branch.lng != null ? { lat: store.branch.lat, lng: store.branch.lng, name: store.branch.name, country: store.branch.country_code } : null"
      keepable @done="picked" @close="picking = false" />

    <AdjustSheet v-if="adjOpen" :currency="cur" :reasons="payOpts?.reasons" :initial="adjust" :bottom="isNarrow()" :preview="previewAdj"
      @close="adjOpen = false" @apply="setAdjust" />
    <ApproveDialog v-if="approving" :branch-id="bid" perm="checkout.discount" :what="t('ck.approve_what')" @approved="approved" @close="approving = false" />
    <div v-if="toast" class="toast">✓ {{ toast }}</div>
    <div v-if="done && appendId" class="sheet-mask center">
      <div class="modal" data-testid="ao-done">
        <h2>{{ t('ao.m.done', { no: done.order.public_no }) }}</h2>
        <div class="muted">{{ t('ao.m.done_hint') }}</div>
        <pre class="copytext" data-testid="ao-done-text">{{ done.text }}</pre>
        <div class="row wrap" style="justify-content:flex-end;margin-top:14px">
          <a class="btn" :href="`/ticket/${done.order.id}?batch=${doneSeq}`" target="_blank" data-testid="ao-print">{{ t('ao.print_batch') }} · {{ t('ao.n', { n: addonNo(doneSeq) }) }}</a>
          <button class="btn" @click="copy(done.text); flash(t('vo.copied'))">{{ t('vo.copy_again') }}</button>
          <button class="btn primary" data-testid="ao-back" @click="router.push('/merchant/orders')">{{ t('ao.m.back') }}</button>
        </div>
      </div>
    </div>
    <div v-else-if="done" class="sheet-mask center">
      <div class="modal" data-testid="mo-done">
        <h2>{{ t('vo.done_title', { no: done.order.public_no }) }}</h2>
        <div class="muted">{{ done.order.acceptance_status === 'accepted' ? t('vo.done_accepted') : t('vo.done_pending') }}</div>
        <pre class="copytext" data-testid="mo-done-text">{{ done.text }}</pre>
        <div class="row wrap" style="justify-content:flex-end;margin-top:14px">
          <a class="btn" :href="`/ticket/${done.order.id}`" target="_blank">{{ t('vo.print') }}</a>
          <button class="btn" @click="copy(`${origin}/o/${done.token}`); flash(t('vo.copied'))">{{ t('vo.copy_link') }}</button>
          <button class="btn" @click="copy(done.text); flash(t('vo.copied'))">{{ t('vo.copy_again') }}</button>
          <button class="btn primary" data-testid="mo-next" @click="next">{{ t('vo.next') }}</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* the customer pages' deep red, paper and gold (.warm in styles.css) */
.mo { --dark: #5a1a12; --dark2: #8f251b; --edge: #dccbb2; --row: #f3e9d8;
  --addon: #fbe7b3; --addon-line: #ecc96b;
  display: flex; flex-direction: column; height: calc(100vh - var(--shell-top, 0px)); background: var(--bg); position: relative; font-size: 14px; }
.mo button { font: inherit; cursor: pointer; }
.mo-top { background: var(--red-bg); color: #fff; display: flex; align-items: center; gap: 12px; padding: 10px 16px; flex: none; }
.mo-top .back { background: none; border: 0; color: var(--gold-soft); font-weight: 600; }
.mo-top h1 { font-size: 17px; margin: 0; }
.mo-top .mo-brands { color: var(--ink, #3b1f14); }
@media (max-width: 760px) { .mo-top { flex-wrap: wrap; } .mo-top .mo-brands { order: 9; width: 100%; } }
.mo-top .store { color: var(--gold); }
.mo-body { flex: 1; display: grid; grid-template-columns: minmax(0, 1fr) 470px 190px; min-height: 0; }
.c { text-align: center; } .r { text-align: right; } .b { font-weight: 700; } .red { color: #b42318; } .okc { color: #18794e; }
.small { font-size: 12.5px; } .grow { flex: 1; min-width: 0; } .wrap { flex-wrap: wrap; }
.rel { position: relative; }
.tstate { color: var(--gold-soft); font-size: 13px; }
.bh { padding: 6px 12px; font-size: 12.5px; color: #7a5b3a; background: #efe4d0; border-bottom: 1px solid var(--edge); }
.bh.add { background: #fff3dc; color: #b45309; font-weight: 700; }
.tr.lock { background: #f4efe6; color: #6b5d4d; }
.tr.lock .name b { font-weight: 600; }
.ao-tot .tag.gold { background: var(--gold); color: #5a1010; border: 0; }
.sec.top { align-items: flex-start; }

.order { display: flex; flex-direction: column; min-height: 0; background: var(--paper); border-right: 1px solid var(--line); }
.thead, .tr { display: grid; grid-template-columns: 30px minmax(140px, 1fr) 92px 70px 78px minmax(90px, 160px) 62px; gap: 10px; align-items: center; padding: 0 14px; }
.thead { background: var(--dark2); color: var(--gold-soft); font-size: 12.5px; font-weight: 600; height: 36px; flex: none; }
.rows { flex: 1; overflow: auto; }
.empty { padding: 40px 20px; text-align: center; }
.tr { min-height: 48px; border-bottom: 1px solid var(--row); background: var(--paper); }
.tr.sel { background: #fdf0d2; box-shadow: inset 3px 0 0 var(--brand); }
.tr .no { color: var(--muted); }
.name b { font-size: 14.5px; }
.code { display: inline-block; min-width: 26px; font-size: 11.5px; font-weight: 700; color: var(--brand-dark); background: var(--brand-soft); border-radius: 4px; padding: 0 4px; margin-inline-end: 6px; text-align: center; }
.spec { color: var(--muted); font-size: 12.5px; margin-inline-start: 6px; }
.qty { display: inline-flex; align-items: center; gap: 8px; font-weight: 700; }
.qty button { width: 24px; height: 24px; border-radius: 6px; border: 0; display: grid; place-items: center; background: var(--gold-soft); color: var(--brand-dark); font-weight: 700; padding: 0; }
.qty button.f { background: var(--brand); color: #fff; }
.qty.sm button { width: 20px; height: 20px; font-size: 12px; }
.qty.sm button:not(.f) { background: #fff; }
.note-in { border: 1px solid var(--edge); background: #fff; border-radius: 6px; padding: 4px 8px; font-size: 12.5px; min-height: 28px; width: 100%; min-width: 0; }
.note-cell { display: flex; flex-wrap: wrap; align-items: center; gap: 4px; min-width: 0; }
.note-cell .note-in { flex: 1 1 60px; }
.note-cell.tpl:has(.nchip) { border: 1px solid var(--edge); background: #fff; border-radius: 6px; padding: 3px; }
.note-cell.tpl:has(.nchip) .note-in { border: 0; padding: 1px 4px; min-height: 22px; }
.note-cell.open { outline: 2px solid var(--brand); outline-offset: 1px; border-radius: 6px; }
.nchip { display: inline-flex; align-items: center; gap: 2px; background: var(--gold-soft); color: var(--brand-dark); border-radius: 5px; padding: 1px 2px 1px 7px;
  font-size: 12px; font-weight: 600; max-width: 100%; overflow-wrap: anywhere; }
.nchip button { border: 0; background: none; color: inherit; font-size: 10px; padding: 0 4px; cursor: pointer; opacity: .7; }
.note-mask { position: fixed; inset: 0; z-index: 40; }
.note-pop { position: fixed; z-index: 41; background: var(--paper, #fff); border: 1px solid var(--line); border-radius: 10px; box-shadow: 0 10px 28px rgba(59, 31, 20, .18);
  padding: 12px; max-height: min(60vh, 420px); overflow-y: auto; }
.np-head { display: flex; align-items: baseline; gap: 8px; margin-bottom: 10px; }
.np-head b { font-size: 14px; }
.np-done { margin-inline-start: auto; border: 1px solid var(--brand); color: var(--brand); background: #fff; border-radius: 6px; padding: 3px 12px; font: inherit; font-size: 13px; cursor: pointer; }
.np-foot { margin-top: 10px; }
.np-in { display: none; }
.ops { display: flex; gap: 6px; justify-content: flex-end; }
.ops button { width: 26px; height: 26px; border-radius: 6px; border: 0; background: var(--row); color: var(--muted); font-weight: 700; padding: 0; }
.ops button.del { color: #b42318; }
.tr.sub { min-height: 0; border-bottom: 0; }
.addons { grid-column: 2 / 6; background: var(--addon); border: 1px solid var(--addon-line); border-radius: 10px; padding: 6px 10px; margin: 0 0 8px; display: flex; flex-direction: column; gap: 4px; }
.addon { display: grid; grid-template-columns: 1fr 90px 70px; align-items: center; font-size: 13.5px; }
.addon-add { display: flex; flex-direction: column; gap: 4px; }
.addon-add input { border: 1px dashed #c9a24a; border-radius: 6px; padding: 3px 8px; font-size: 12.5px; background: rgba(255,255,255,.7); max-width: 360px; }
.addon-hits { display: flex; flex-wrap: wrap; gap: 4px; }
.ocode { font-size: 11px; font-weight: 700; color: var(--red, #b3261e); margin-inline-end: 4px; }
.chip { border: 1px solid var(--edge); background: #fff; color: var(--ink); border-radius: 999px; padding: 3px 10px; font-size: 12.5px; }
.chip.on { background: var(--brand); border-color: var(--brand); color: #fff; }
.chip:disabled { opacity: .45; }

.foot { background: var(--dark); color: #f3e6d0; padding: 10px 16px 12px; flex: none; }
.tot { display: flex; align-items: baseline; gap: 22px; flex-wrap: wrap; }
.tot b { color: #fff; font-size: 22px; } .tot b.n { font-size: 17px; } .tot .k { color: #d9b98a; font-size: 12.5px; margin-inline-end: 4px; }
.cust { margin-top: 6px; display: grid; grid-template-columns: 130px 1fr; row-gap: 2px; font-size: 13px; color: #f3e6d0; }
.cust span:nth-child(odd) { color: #d9b98a; }
.btn.copy { background: var(--gold); color: var(--dark); border: none; }
.qerr { margin-top: 6px; color: #fecaca; font-size: 13px; }

.panel { background: var(--paper); display: flex; flex-direction: column; min-height: 0; border-right: 1px solid var(--line); }
.pscroll { flex: 1; overflow: auto; padding: 12px 14px; display: flex; flex-direction: column; gap: 7px; }
.sec { display: flex; align-items: center; gap: 8px; }
.sec .k { width: 72px; flex: none; color: var(--muted); font-size: 13px; font-weight: 600; }
.sec .k.kt { align-self: flex-start; }
.sec .k.auto { width: auto; }
.brandc { color: var(--brand-dark) !important; }
.inp { flex: 1; border: 1px solid var(--edge); border-radius: 8px; padding: 6px 10px; background: #fff; min-height: 34px; display: flex; align-items: center; gap: 6px; min-width: 0; font: inherit; }
.adjbtn { cursor: pointer; color: var(--muted, #8a7a70); text-align: start; border-style: dashed; }
.adjbtn.set { border-style: solid; border-color: #e8c97a; background: #fdf3d6; color: #8a5a12; font-weight: 600; }
.gold-k { color: #8a5a12; font-weight: 600; }
.inp:focus, .inp:focus-within { outline: none; border-color: var(--brand); box-shadow: 0 0 0 3px rgba(160, 44, 32, .15); }
.inp input { border: 0; outline: 0; flex: 1; min-width: 0; font: inherit; padding: 0; }
.inp .cc { background: var(--row); border-radius: 5px; padding: 0 6px; font-weight: 600; }
.inp.ta { min-height: 56px; resize: vertical; }
.inp.ro { background: var(--row); color: var(--ink); }
.idin { flex: 0 0 130px; font-weight: 700; font-size: 15px; }
.stack0 { display: flex; flex-direction: column; gap: 6px; }
.memberbox { border: 1px solid var(--line); border-radius: 10px; padding: 10px 12px; display: flex; flex-direction: column; gap: 6px; background: #fff; }
.memberbox.temp { background: #fdf3dc; border-color: var(--addon-line); }
.stats { display: grid; grid-template-columns: 72px repeat(4, auto); gap: 6px; font-size: 13px; align-items: center; }
.stats .k { color: var(--muted); font-weight: 600; }
.btnrow { display: flex; gap: 5px; flex-wrap: wrap; }
.btn.blue { background: #fff; border-color: var(--red); color: var(--red); padding: 5px 9px; font-size: 13px; }
.btn.blue.on { background: var(--red); color: #fff; }
.chips { display: flex; gap: 6px; align-items: center; flex-wrap: wrap; font-size: 12.5px; color: var(--muted); }
.zsel { border: 1px solid var(--edge); background: #fff; border-radius: 6px; padding: 2px 6px; font: inherit; }
.srcs { display: flex; gap: 5px; flex-wrap: wrap; }
.srcs button { padding: 4px 11px; border-radius: 999px; border: 1px solid var(--edge); background: #fff; font-size: 13px; font-weight: 600; color: var(--ink); }
.srcs button.on { background: var(--brand); border-color: var(--brand); color: #fff; }
.req { color: #b42318; font-style: normal; }
.drop { position: absolute; background: #fff; border: 1px solid var(--edge); border-radius: 10px; box-shadow: 0 10px 28px rgba(0,0,0,.16); overflow: hidden; z-index: 5; }
.drop .it { padding: 7px 12px; border-bottom: 1px solid var(--row); display: flex; align-items: center; gap: 8px; font-size: 13.5px; cursor: pointer; }
.drop .it.on, .drop .it:hover { background: var(--brand-soft); }
.drop .it.off { opacity: .5; }
.drop .it em { font-style: normal; color: var(--brand-dark); font-weight: 700; }
.drop .grp { padding: 4px 12px; background: var(--row); font-size: 11.5px; color: var(--muted); font-weight: 700; }
.drop .it.more { color: var(--brand-dark); font-weight: 700; border-top: 1px dashed var(--edge); }
.drop .it .look { font-size: 12px; white-space: nowrap; }
.btnrow a.btn.off { opacity: .45; cursor: not-allowed; }
.drop .hint { padding: 6px 12px; font-size: 12px; color: var(--muted); background: var(--paper); }
.pick .inp { background: #fff; border-color: var(--brand); font-weight: 700; font-size: 15px; }
.pickhint { padding-inline-start: 80px; }
.huashu { position: absolute; top: 38px; right: 0; width: 380px; max-width: 90vw; background: #fff; border: 1px solid var(--edge); border-radius: 12px; box-shadow: 0 10px 28px rgba(0,0,0,.18); z-index: 6; padding: 10px; }
.huashu .it { display: block; width: 100%; text-align: start; padding: 7px 9px; border-radius: 8px; font-size: 13px; border: 1px solid var(--line); margin-top: 6px; background: #fff; white-space: pre-wrap; }
.huashu .it:hover { background: var(--brand-soft); }
.confirmbar { padding: 8px 14px 14px; display: flex; flex-direction: column; gap: 6px; flex: none; }
.confirm { padding: 12px; font-size: 17px; border-radius: 10px; width: 100%; }

.queue { background: var(--bg); display: flex; flex-direction: column; min-height: 0; overflow: auto; }
.queue h3 { margin: 0; font-size: 13.5px; background: var(--dark2); color: var(--gold-soft); padding: 9px 12px; display: flex; justify-content: space-between; }
.qi { padding: 9px 12px; border-bottom: 1px solid var(--line); font-size: 12.5px; display: flex; flex-direction: column; gap: 2px; background: var(--paper); color: inherit; text-decoration: none; }
.qi b { font-size: 13.5px; }
.pill-num { background: var(--gold); color: var(--dark); border-radius: 999px; padding: 0 8px; font-weight: 700; font-size: 12px; }

.sgroup { display: flex; flex-direction: column; gap: 6px; margin-bottom: 12px; }
.sheet-mask.center { display: grid; place-items: center; }
.modal { width: 520px; max-width: calc(100vw - 32px); background: var(--paper); border-radius: 14px; padding: 22px 24px; box-shadow: 0 20px 50px rgba(0,0,0,.25); }
.modal h2 { margin: 0 0 4px; font-size: 19px; }
.copytext { background: #fff; border: 1px solid var(--edge); border-radius: 10px; padding: 12px 14px; line-height: 1.7; font-size: 13.5px; margin: 12px 0 0; white-space: pre-wrap; font-family: inherit; max-height: 50vh; overflow: auto; }
.toast { position: fixed; left: 50%; top: 60px; transform: translateX(-50%); background: #18794e; color: #fff; padding: 8px 18px; border-radius: 999px; font-weight: 700; z-index: 60; box-shadow: 0 6px 16px rgba(0,0,0,.18); }
.mo-picker { position: fixed; inset: 0; z-index: 50; background: #fff; }

/* the column of waiting orders only where the dishes still have room */
@media (max-width: 1700px) {
  .mo-body { grid-template-columns: minmax(0, 1fr) 440px; }
  .queue { display: none; }
}
@media (max-width: 1300px) {
  .mo-body { grid-template-columns: minmax(0, 1fr) 400px; }
}
/* Tablet and phone (H5): one column in the order staff work in: the
   customer, then the dish search and the dishes, with the total and "place
   order" kept at the bottom of the screen. */
@media (max-width: 1000px) {
  .mo { height: auto; min-height: calc(100vh - var(--shell-top, 0px)); }
  .mo-body { display: flex; flex-direction: column; gap: 8px; padding-top: 12px; }
  .panel, .pscroll { display: contents; }
  .pscroll > * { margin-inline: 14px; }
  .pick { order: 2; margin-top: 8px; padding-top: 14px; border-top: 6px solid var(--row); margin-inline: 0; padding-inline: 14px; }
  .pickhint { order: 2; margin-top: -4px; padding-inline-start: 0; }
  .order { order: 3; border-right: 0; border-top: 1px solid var(--line); }
  .confirmbar { order: 4; position: sticky; bottom: 0; z-index: 6; background: var(--paper); border-top: 1px solid var(--line);
    box-shadow: 0 -6px 16px rgba(59, 31, 20, .08); padding-bottom: calc(12px + env(safe-area-inset-bottom)); }
  .queue { display: none; }
  /* fields stay clear of the bar at the bottom while typing */
  .inp, .note-in, .addon-add input { scroll-margin-bottom: 150px; }
  .pick { scroll-margin-top: calc(var(--shell-top, 0px) + 4px); }
  .drop { z-index: 7; max-height: 50vh; overflow-y: auto; }
  .sec.rel > .drop { left: 0 !important; right: 0 !important; top: 100% !important; margin-top: 4px; }
  .sec.pick > .drop { left: 14px !important; right: 14px !important; }
  .rows { overflow: visible; }
  .thead { display: none; }
  .tr { grid-template-columns: 24px minmax(0, 1fr) auto auto; row-gap: 6px; padding: 10px 12px; }
  .tr > .r:not(.b) { display: none; }
  .tr .note-cell { grid-column: 2 / 4; }
  .tr .note-in { min-height: 34px; }
  .note-pop { top: auto !important; left: 0 !important; width: auto !important; right: 0; bottom: 0; border-radius: 14px 14px 0 0; max-height: 60vh;
    padding-bottom: calc(14px + env(safe-area-inset-bottom)); }
  .note-mask { background: rgba(0, 0, 0, .3); }
  .np-in { display: block; min-height: 40px; font-size: 15px; margin-bottom: 12px; }
  .addons { grid-column: 1 / -1; }
  .qty { gap: 10px; }
  .qty button { width: 32px; height: 32px; border-radius: 8px; font-size: 16px; }
  .qty.sm button { width: 28px; height: 28px; }
  .ops button { width: 34px; height: 34px; }
  .chip { padding: 6px 12px; }
  .foot .cust { display: none; }
  .foot { padding: 10px 14px; }
  .tot { gap: 6px 16px; }
  .sec { flex-wrap: wrap; }
  .idin { flex: 1 0 130px; }
}
@media (pointer: coarse) { .pickhint { display: none; } }
/* Phone: each label above its field, fields full width and taller. */
@media (max-width: 767px) {
  .sec:not(.idrow) { flex-direction: column; align-items: stretch; gap: 4px; }
  .sec .k { width: auto; }
  .sec .k:empty { display: none; }
  .sec .k.kt { display: none; }
  .inp { min-height: 42px; }
  .idrow .grow { display: none; }
  .idrow .btn { min-height: 42px; }
  .btnrow .btn.blue { flex: 1 1 auto; min-height: 40px; }
  .srcs button { padding: 7px 13px; }
  .confirm { font-size: 16px; }
}
</style>
