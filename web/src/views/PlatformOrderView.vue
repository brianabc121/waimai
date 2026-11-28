<script setup lang="ts">
// 点单 · 平台下单 (docs/ORDER_TYPES_DESIGN.md §7): an order taken on Grab,
// foodpanda or another delivery platform, copied in by staff so the kitchen
// makes it and the books see it. Left: the dishes at the platform's prices.
// Right: the platform, its order number, who collects it, the two platform
// discounts, a check against the platform's amount, the commission and what
// the store should get paid. Every amount comes from the server's quote.
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ApiError, get, post } from '../api'
import { stableKey } from '../retry'
import { has, mn, money, t, toMinor, whenText } from '../i18n'
import { currentRole, session } from '../session'
import type { Item, OptionGroup, Order, PlatformQuote, QuoteLine, Storefront } from '../types'
import { type BranchPlatform, bpToPct, platformName } from '../platforms'
import '../platformOrders'
import { multiBrand } from '../brands'
import type { BrandRef } from '../types'
import NotePicker from './NotePicker.vue'
import { cartTags, hasNotes } from '../notes'

const router = useRouter()
const bid = session.branchId!
const base = `/merchant/branches/${bid}`

const store = ref<Storefront | null>(null)
// Each brand has its own shops on the platforms (一品湘 Grab, 好吃党 Grab,
// docs/MULTI_BRAND_DESIGN.md §7): staff pick the shop the order came in on.
interface Shop { key: string; brand: BrandRef; p: BranchPlatform }
const shops = ref<Shop[]>([])
const platforms = computed(() => shops.value.map(s => s.p))
const loaded = ref(false)
const loadError = ref('')
const cur = computed(() => quote.value?.currency ?? store.value?.branch.currency ?? currentRole()?.currency ?? 'PHP')
const m = (v: number) => money(v, cur.value)
const items = computed<Item[]>(() => (store.value?.categories ?? []).flatMap(c => c.items))
onMounted(async () => {
  try {
    const p = await get<{ shops: { brand: BrandRef; platforms: BranchPlatform[] }[] }>(`${base}/platform-orders/platforms`)
    shops.value = (p.shops ?? []).flatMap(x => x.platforms.map(pl => ({ key: `${x.brand.id}-${pl.id}`, brand: x.brand, p: pl })))
    let last = ''
    try { last = localStorage.getItem(`po-shop-${bid}`) || '' } catch { /* private window */ }
    f.shop = (shops.value.find(x => x.key === last) ?? shops.value[0])?.key ?? ''
    await loadMenu()
  } catch (e) { loadError.value = (e as Error).message }
  loaded.value = true
})

// ---------- the form ----------
const f = reactive({
  shop: '', no: '', pickup: 'rider' as 'rider' | 'customer', when: '', customer: '', note: '', staffNote: '',
  merchant: '', platformPromo: '', packaging: '', check: '',
  editCommission: false, commission: '', commissionReason: '',
})
const shop = computed(() => shops.value.find(s => s.key === f.shop) ?? null)
const platform = computed(() => shop.value?.p ?? null)
const shopName = (s: Shop) => multiBrand() ? `${s.brand.name} ${platformName(s.p)}` : platformName(s.p)
// the brand's menu: its dishes, names and prices; dishes it does not sell leave the order
let menuBrand = -1
async function loadMenu() {
  const slug = currentRole()?.slug, brand = shop.value?.brand.id ?? 0
  if (!slug || brand === menuBrand) return
  menuBrand = brand
  store.value = await get<Storefront>(`/stores/${slug}?brand=${brand}`)
  const ids = new Set(items.value.map(i => i.id))
  lines.value = lines.value.filter(l => ids.has(l.item.id)).map(l => ({ ...l, item: items.value.find(i => i.id === l.item.id)! }))
}
watch(() => f.shop, k => {
  try { if (k) localStorage.setItem(`po-shop-${bid}`, k) } catch { /* private window */ }
  loadMenu().catch(e => { loadError.value = (e as Error).message })
})
// a number that does not look like the platform's numbers (a typo, the wrong platform)
const noOdd = computed(() => {
  const p = platform.value, no = f.no.trim()
  if (!p?.no_pattern || !no) return false
  try { return !new RegExp(p.no_pattern, 'i').test(no) } catch { return false }
})

// ---------- the dishes ----------
// Options of a group that takes one (大碗/小碗) are the dish's spec; the others
// are add-ons. Prices are not shown until the server priced them for the platform.
interface Line { key: number; item: Item; qty: number; picks: Record<number, number>; note: string; tags: number[] }
let nextKey = 1
const lines = ref<Line[]>([])
const selKey = ref(0)
const isSpec = (g: OptionGroup) => g.max_select === 1
const optionsOf = (l: Line) => l.item.groups.flatMap(g => g.options.map(o => ({ g, o, n: l.picks[o.id] ?? 0 })))
const specText = (l: Line) => optionsOf(l).filter(x => x.n && isSpec(x.g)).map(x => mn(x.o)).join('、')
const addonsOf = (l: Line) => optionsOf(l).filter(x => x.n && !isSpec(x.g))
const hasAddons = (l: Line) => l.item.groups.some(g => !isSpec(g))
const groupCount = (l: Line, g: OptionGroup) => g.options.reduce((s, o) => s + (l.picks[o.id] ? 1 : 0), 0)
const count = computed(() => lines.value.reduce((s, l) => s + l.qty, 0))
// the priced line of each dish line: the quote keeps the order of the lines sent
const priced = (i: number): QuoteLine | null => quote.value?.lines[i] ?? null

function addLine(item: Item, picks: Record<number, number> = {}) {
  const l: Line = { key: nextKey++, item, qty: 1, picks, note: '', tags: [] }
  lines.value.push(l)
  selKey.value = l.key
}
function removeLine(l: Line) { lines.value = lines.value.filter(x => x !== l) }
function setQty(l: Line, d: number) { l.qty = Math.max(1, Math.min(999, l.qty + d)) }
function setAddon(l: Line, g: OptionGroup, id: number, d: number) {
  const n = (l.picks[id] ?? 0) + d
  if (d > 0 && !l.picks[id] && groupCount(l, g) >= g.max_select) return
  if (n > 20) return
  if (n <= 0) delete l.picks[id]
  else l.picks[id] = n
}
const flavorKey = ref(0)

// the dish box: a code or a name, Enter adds the first match
const pickQ = ref(''), pickAt = ref(0), pickOpen = ref(false)
const pickInput = ref<HTMLInputElement>()
const hits = computed(() => {
  const q = pickQ.value.trim().toLowerCase()
  if (!q) return []
  const code = items.value.filter(i => (i.code ?? '').toLowerCase().startsWith(q))
  const name = items.value.filter(i => !code.includes(i) && (i.name.toLowerCase().includes(q) || i.name_en.toLowerCase().includes(q) || mn(i).toLowerCase().includes(q)))
  return [...code, ...name].slice(0, 8)
})
watch(pickQ, () => { pickAt.value = 0; pickOpen.value = true })
const closePick = () => setTimeout(() => (pickOpen.value = false), 150)
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

// the spec sheet: for a dish with a size or a choice it must have
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

// ---------- the price, from the server ----------
const quote = ref<PlatformQuote | null>(null)
const quoteError = ref('')
const error = ref('')
const amount = (s: string) => s.trim() === '' ? 0 : toMinor(s)
const commissionTyped = computed(() => f.editCommission && f.commission.trim() !== '' && !isNaN(Number(f.commission)))
const body = () => ({
  platform_id: platform.value?.id ?? 0, brand_id: shop.value?.brand.id ?? 0, platform_no: f.no.trim(), pickup: f.pickup, scheduled_for: f.when || null,
  customer_name: f.customer.trim(), note: f.note.trim(), staff_note: f.staffNote.trim(),
  items: lines.value.map(l => ({ item_id: l.item.id, qty: l.qty, note: l.note, note_tag_ids: l.tags,
    option_ids: Object.entries(l.picks).flatMap(([id, n]) => Array(n).fill(Number(id))) })),
  packaging_fee: amount(f.packaging), merchant_promo: amount(f.merchant), platform_promo: amount(f.platformPromo),
  commission: commissionTyped.value ? toMinor(f.commission) : null,
  commission_reason: commissionTyped.value ? f.commissionReason.trim() : '',
})
let quoteTimer = 0, quoteSeq = 0
watch(() => JSON.stringify(body()), () => {
  clearTimeout(quoteTimer)
  quoteTimer = window.setTimeout(requote, 300)
})
async function requote() {
  const seq = ++quoteSeq
  if (!f.shop || !lines.value.length) { quote.value = null; quoteError.value = ''; return }
  try {
    // the reason is asked for when placing: a commission being typed is still priced
    const b = body()
    const q = await post<PlatformQuote>(`${base}/platform-orders/quote`, { ...b, commission_reason: b.commission != null ? b.commission_reason || '-' : '' })
    if (seq === quoteSeq) { quote.value = q; quoteError.value = '' }
  } catch (e) {
    if (seq !== quoteSeq) return
    quote.value = null; quoteError.value = errText(e)
  }
}
const totals = computed(() => quote.value?.totals ?? null)
const merchantOff = computed(() => quote.value?.totals.merchant_discount ?? 0)
const platformOff = computed(() => quote.value?.totals.platform_subsidy ?? 0)
// the amount on the platform's tablet against the dishes (or the total) worked out here: shown, never changed
const checkDiff = computed(() => {
  const q = quote.value
  if (!q || f.check.trim() === '' || isNaN(Number(f.check))) return null
  const typed = toMinor(f.check)
  if (typed === q.totals.subtotal || typed === q.totals.total) return 0
  return typed - q.totals.subtotal
})
const commissionRule = computed(() => {
  const q = quote.value
  if (!q) return ''
  return t('po.commission_rule', { r: bpToPct(q.commission_rate_bp) + '%', b: t('po.base.' + q.commission_base) })
})
function backToAuto() { f.editCommission = false; f.commission = ''; f.commissionReason = '' }
function startEdit() {
  f.editCommission = true
  if (!f.commission && quote.value) f.commission = String(quote.value.commission / 100)
}

function errText(e: unknown): string {
  if (e instanceof ApiError) {
    for (const k of [`po.err.${e.code}`, `vo.err.${e.code}`]) if (has(k)) return t(k, { s: '' })
  }
  return (e as Error).message
}

// ---------- placing ----------
const placing = ref(false)
// the same entry sent again (its answer was lost) is the same order
const placeKeys = stableKey()
const done = ref<Order | null>(null)
const dupGone = ref(false)
async function place() {
  error.value = ''
  if (!f.shop) { error.value = t('po.need_platform'); return }
  if (!f.no.trim()) { error.value = t('po.need_no'); return }
  if (!lines.value.length) { error.value = t('po.need_items'); return }
  if (commissionTyped.value && !f.commissionReason.trim()) { error.value = t('po.need_reason'); return }
  placing.value = true
  try {
    const sent = { ...body(), expected_total: totals.value?.total ?? null }
    const r = await post<{ order: Order }>(`${base}/platform-orders`, sent, { 'Idempotency-Key': placeKeys.for(sent) })
    placeKeys.done()
    done.value = r.order
  } catch (e) {
    error.value = errText(e)
    if (e instanceof ApiError && (e.code === 'QUOTE_CHANGED' || e.code === 'DUPLICATE')) void requote()
  } finally { placing.value = false }
}
function next() {
  done.value = null
  lines.value = []
  Object.assign(f, { no: '', pickup: 'rider', when: '', customer: '', note: '', staffNote: '', merchant: '', platformPromo: '', packaging: '', check: '' })
  backToAuto()
  quote.value = null; quoteError.value = ''; error.value = ''
  void nextTick(() => document.querySelector<HTMLInputElement>('[data-testid="po-no"]')?.focus())
}
function onKey(e: KeyboardEvent) { if (done.value && e.key === 'Enter') { e.preventDefault(); next() } }
onMounted(() => window.addEventListener('keydown', onKey))
onBeforeUnmount(() => window.removeEventListener('keydown', onKey))
const publicNo = computed(() => platform.value && f.no.trim() ? `${platform.value.prefix} ${f.no.trim()}` : '')
</script>

<template>
  <div class="ox warm po" data-testid="platform-order">
    <div class="page ox-page">
      <div class="ox-head">
        <div><h1>{{ t('pos.platform') }}</h1><p class="ox-sub">{{ t('po.sub') }}</p></div>
        <span class="store muted">{{ store?.branch.name }}</span>
      </div>
      <div v-if="loadError" class="banner bad">{{ loadError }}</div>
      <div v-else-if="loaded && !platforms.length" class="banner info" data-testid="po-none">{{ t('po.none') }}</div>

      <div v-if="platforms.length" class="po-body">
        <!-- the dishes -->
        <section class="ox-panel flush lines">
          <div class="thead"><span>{{ t('po.col.no') }}</span><span>{{ t('po.col.item') }}</span><span class="c">{{ t('po.col.qty') }}</span>
            <span class="r">{{ t('po.col.price') }}</span><span class="r">{{ t('po.col.sum') }}</span><span>{{ t('po.col.note') }}</span><span></span></div>
          <div class="rows" data-testid="po-lines">
            <div v-if="!lines.length" class="empty muted">{{ t('po.empty') }}</div>
            <template v-for="(l, i) in lines" :key="l.key">
              <div class="tr" :class="{ sel: l.key === selKey }" data-testid="po-line" @click="selKey = l.key">
                <span class="code">{{ l.item.code }}</span>
                <span class="name"><b>{{ mn(l.item) }}</b><span v-if="specText(l)" class="spec">{{ specText(l) }}</span></span>
                <span class="c"><span class="qty"><button type="button" aria-label="−" @click.stop="setQty(l, -1)">−</button><b data-testid="po-qty">{{ l.qty }}</b><button type="button" class="f" aria-label="+" data-testid="po-plus" @click.stop="setQty(l, 1)">+</button></span></span>
                <span class="r" data-testid="po-unit">{{ priced(i) ? m(priced(i)!.unit_price) : '—' }}</span>
                <span class="r b" data-testid="po-sum">{{ priced(i) ? m(priced(i)!.line_total) : '—' }}</span>
                <span class="notes">
                  <span v-for="(n, ti) in cartTags(l.item, l.tags)" :key="ti" class="nchip">{{ n }}</span>
                  <button v-if="hasNotes(l.item)" type="button" class="btn small" data-testid="po-flavor" @click.stop="flavorKey = flavorKey === l.key ? 0 : l.key">{{ t('po.flavor') }}</button>
                  <input v-model="l.note" class="note-in" maxlength="100" :placeholder="t('vo.note_ph')" data-testid="po-note-in" @click.stop />
                </span>
                <span class="ops"><button type="button" class="del" :title="t('vo.remove')" data-testid="po-remove" @click.stop="removeLine(l)">✕</button></span>
              </div>
              <div v-if="flavorKey === l.key" class="tr sub"><span></span><div class="subbox"><NotePicker v-model="l.tags" :groups="l.item.notes ?? []" /></div></div>
              <div v-if="hasAddons(l) && (addonsOf(l).length || l.key === selKey)" class="tr sub">
                <span></span>
                <div class="subbox addons">
                  <span v-for="a in addonsOf(l)" :key="a.o.id" class="addon" data-testid="po-addon">+ {{ mn(a.o) }}
                    <span class="qty sm"><button type="button" @click="setAddon(l, a.g, a.o.id, -1)">−</button>{{ a.n }}<button type="button" class="f" @click="setAddon(l, a.g, a.o.id, 1)">+</button></span></span>
                  <template v-if="l.key === selKey">
                    <button v-for="x in optionsOf(l).filter(x => !isSpec(x.g) && !x.o.sold_out && !x.n).slice(0, 10)" :key="x.o.id" type="button" class="chip"
                      @click="setAddon(l, x.g, x.o.id, 1)"><small v-if="x.o.code" class="ocode">{{ x.o.code }}</small>＋{{ mn(x.o) }}</button>
                  </template>
                </div>
              </div>
            </template>
          </div>
          <!-- the dark bar: what the platform shows, what the store gets -->
          <div class="bar" data-testid="po-bar">
            <span><i>{{ t('po.count') }}</i><b>{{ count }}</b></span>
            <span><i>{{ t('po.subtotal') }}</i><b data-testid="po-subtotal">{{ totals ? m(totals.subtotal) : '—' }}</b></span>
            <span v-if="totals?.packaging_fee"><i>{{ t('po.packaging') }}</i><b>{{ m(totals.packaging_fee) }}</b></span>
            <span><i>{{ t('po.merchant') }}</i><b data-testid="po-merchant-off">{{ merchantOff ? '−' + m(merchantOff) : '—' }}</b></span>
            <span><i>{{ t('po.platform_funded') }}</i><b data-testid="po-platform-off">{{ platformOff ? '−' + m(platformOff) : '—' }}</b></span>
            <span><i>{{ t('po.paid') }}</i><b class="big" data-testid="po-total">{{ totals ? m(totals.total) : '—' }}</b></span>
            <span><i>{{ t('po.commission') }}</i><b data-testid="po-bar-commission">{{ quote ? '−' + m(quote.commission) : '—' }}</b></span>
            <span><i>{{ t('po.payout') }}</i><b class="big gold" data-testid="po-payout">{{ quote ? m(quote.payout) : '—' }}</b></span>
          </div>
          <div v-if="quoteError" class="qerr" data-testid="po-quote-error">{{ quoteError }}</div>
        </section>

        <!-- the platform, the number, the discounts -->
        <form class="ox-panel form" @submit.prevent="place">
          <div class="fld">
            <label class="k">{{ t('po.platform') }}</label>
            <div class="pfs" role="radiogroup">
              <button v-for="x in shops" :key="x.key" type="button" class="pf" :class="{ on: f.shop === x.key }" :style="{ '--pc': x.p.color }"
                role="radio" :aria-checked="f.shop === x.key" :data-platform="x.p.prefix" :data-brand="x.brand.id" data-testid="po-platform" @click="f.shop = x.key">
                <img v-if="x.p.logo_url" :src="x.p.logo_url" alt="" /><span>{{ shopName(x) }}</span></button>
            </div>
          </div>
          <div class="fld">
            <label class="k" for="po-no">{{ t('po.no') }} <em>*</em></label>
            <div class="nobox"><span class="pre" :style="{ background: platform?.color }">{{ platform?.prefix }}</span>
              <input id="po-no" v-model="f.no" class="inp" maxlength="40" autocomplete="off" data-testid="po-no"
                :placeholder="platform?.no_hint ? t('po.no_ph', { s: platform.no_hint }) : ''" /></div>
            <div v-if="quote?.duplicate && !dupGone" class="warn" data-testid="po-dup">{{ t('po.dup', { t: whenText(quote.duplicate.created_at, store?.branch.timezone) }) }}
              <router-link :to="{ path: '/merchant/orders', query: { order: quote.duplicate.order_id } }">{{ t('po.dup_view') }}</router-link></div>
            <div v-else-if="noOdd" class="hint gold" data-testid="po-no-odd">{{ t('po.no_bad', { s: platform?.no_hint || '' }) }}</div>
          </div>
          <div class="fld">
            <label class="k">{{ t('po.pickup') }}</label>
            <div class="seg">
              <button v-for="k in (['rider', 'customer'] as const)" :key="k" type="button" :class="{ on: f.pickup === k }" :data-testid="'po-pk-' + k" @click="f.pickup = k">{{ t('po.pk.' + k) }}</button>
            </div>
          </div>
          <div class="fld">
            <label class="k" for="po-when">{{ t('po.when') }}</label>
            <select id="po-when" v-model="f.when" class="inp">
              <option value="">{{ t('po.asap') }}</option>
              <option v-for="s in store?.slots ?? []" :key="s" :value="s">{{ whenText(s, store?.branch.timezone) }}</option>
            </select>
          </div>
          <div class="fld two">
            <div><label class="k" for="po-cust">{{ t('po.customer') }}</label>
              <input id="po-cust" v-model="f.customer" class="inp" maxlength="60" :placeholder="t('po.customer_ph')" data-testid="po-customer" /></div>
            <div><label class="k" for="po-staff">{{ t('po.staff_note') }}</label>
              <input id="po-staff" v-model="f.staffNote" class="inp" maxlength="200" /></div>
          </div>
          <div class="fld">
            <label class="k" for="po-note">{{ t('po.note') }}</label>
            <input id="po-note" v-model="f.note" class="inp" maxlength="200" :placeholder="t('po.note_ph')" data-testid="po-note" />
          </div>
          <div class="fld pick">
            <label class="k" for="po-dish">{{ t('po.dishes') }}</label>
            <input id="po-dish" ref="pickInput" v-model="pickQ" class="inp" autocomplete="off" :placeholder="t('po.dish_ph')" data-testid="po-dish"
              @keydown="pickKey" @focus="pickOpen = true" @blur="closePick" />
            <div v-if="pickOpen && hits.length" class="hits" data-testid="po-hits">
              <button v-for="(it, i) in hits" :key="it.id" type="button" :class="{ at: i === pickAt, out: it.sold_out }" @mousedown.prevent="choose(it)">
                <small class="code">{{ it.code }}</small><span class="grow">{{ mn(it) }}</span><small v-if="it.sold_out" class="muted">{{ t('sold_out') }}</small></button>
            </div>
          </div>
          <div class="fld">
            <label class="k">{{ t('po.promos') }}</label>
            <div class="two">
              <div><span class="sk">{{ t('po.merchant') }}</span><input v-model="f.merchant" class="inp" inputmode="decimal" placeholder="0" data-testid="po-merchant" :title="t('po.merchant_hint')" /></div>
              <div><span class="sk">{{ t('po.platform_funded') }}</span><input v-model="f.platformPromo" class="inp" inputmode="decimal" placeholder="0" data-testid="po-platform-promo" :title="t('po.platform_hint')" /></div>
            </div>
            <div class="hint muted">{{ t('po.merchant_hint') }}；{{ t('po.platform_hint') }}</div>
          </div>
          <div class="fld two">
            <div><label class="k" for="po-pack">{{ t('po.packaging') }}</label><input id="po-pack" v-model="f.packaging" class="inp" inputmode="decimal" placeholder="0" data-testid="po-packaging" /></div>
            <div><label class="k" for="po-check">{{ t('po.check') }}</label><input id="po-check" v-model="f.check" class="inp" inputmode="decimal" :placeholder="t('po.check_ph')" data-testid="po-check" /></div>
          </div>
          <div v-if="checkDiff === 0" class="hint ok" data-testid="po-check-ok">✓ {{ t('po.check_ok') }}</div>
          <div v-else-if="checkDiff != null" class="warn" data-testid="po-check-diff">{{ t('po.check_diff', { s: m(Math.abs(checkDiff)) }) }}</div>

          <div class="money" data-testid="po-money">
            <div class="mrow"><span>{{ t('po.receivable') }}</span><b>{{ quote ? m(quote.receivable) : '—' }}</b></div>
            <div class="mrow"><span>{{ t('po.commission') }} <small class="muted">{{ commissionRule }}</small>
              <small v-if="commissionTyped" class="tag gold">{{ t('po.manual') }}</small></span>
              <b data-testid="po-commission">{{ quote ? '−' + m(quote.commission) : '—' }}</b></div>
            <div v-if="!f.editCommission" class="mrow end"><button type="button" class="btn link small" data-testid="po-edit-commission" @click="startEdit">{{ t('po.edit_commission') }}</button></div>
            <div v-else class="cedit">
              <input v-model="f.commission" class="inp" inputmode="decimal" :aria-label="t('po.commission_amt')" :placeholder="t('po.commission_amt')" data-testid="po-commission-in" />
              <input v-model="f.commissionReason" class="inp" maxlength="200" :placeholder="t('po.reason_ph')" data-testid="po-commission-reason" />
              <button type="button" class="btn small" @click="backToAuto">{{ t('po.auto_commission') }}</button>
            </div>
            <div class="mrow payout"><span>{{ t('po.payout') }}</span><b>{{ quote ? m(quote.payout) : '—' }}</b></div>
          </div>
          <div v-if="error" class="banner bad" data-testid="po-error">{{ error }}</div>
          <button class="btn primary confirm" :disabled="placing" data-testid="po-confirm">
            {{ publicNo && totals ? t('po.confirm_bar', { no: publicNo, s: m(totals.total) }) : t('po.confirm') }}</button>
        </form>
      </div>
    </div>

    <!-- the spec sheet -->
    <div v-if="spec" class="modal-mask" @click.self="spec = null">
      <div class="modal-box" data-testid="po-spec">
        <h3>{{ mn(spec.item) }}</h3>
        <div v-for="g in spec.item.groups" :key="g.id" class="sg">
          <div class="muted small">{{ mn(g) }}</div>
          <div class="chips">
            <button v-for="o in g.options" :key="o.id" type="button" class="chip" :class="{ on: spec.picks[o.id] }" :disabled="o.sold_out"
              data-testid="po-spec-opt" @click="specToggle(g, o.id)">{{ mn(o) }}</button>
          </div>
        </div>
        <div v-if="specMissing" class="hint gold">{{ t('po.spec_missing', { s: mn(specMissing) }) }}</div>
        <div class="row end"><button type="button" class="btn" @click="spec = null">{{ t('cancel') }}</button>
          <button type="button" class="btn primary" :disabled="!!specMissing" data-testid="po-spec-add" @click="specAdd">{{ t('po.spec_add') }}</button></div>
      </div>
    </div>

    <!-- placed -->
    <div v-if="done" class="modal-mask">
      <div class="modal-box" data-testid="po-done">
        <h3>✓ {{ t('po.done') }}</h3>
        <p class="big"><b>{{ done.public_no }}</b> · {{ m(done.totals.total) }}</p>
        <p v-if="done.platform" class="muted">{{ t('po.payout') }} {{ m(done.platform.payout) }}</p>
        <div class="row end wrap">
          <a class="btn" :href="`/ticket/${done.id}`" target="_blank" rel="noopener" data-testid="po-print">{{ t('po.print') }}</a>
          <button type="button" class="btn" data-testid="po-view" @click="router.push({ path: '/merchant/orders', query: { order: done.id } })">{{ t('po.view') }}</button>
          <button type="button" class="btn primary" data-testid="po-next" @click="next">{{ t('po.next') }} ↵</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.po-body { display: grid; grid-template-columns: minmax(0, 1fr) 400px; gap: 14px; align-items: start; }
.po-body > * { min-width: 0; }
.store { font-size: 14px; }
/* the dishes */
.lines { display: flex; flex-direction: column; min-height: 420px; }
.thead, .tr { display: grid; grid-template-columns: 64px minmax(0, 1.4fr) 108px 90px 96px minmax(0, 1.3fr) 40px; gap: 8px; align-items: center; padding: 8px 14px; }
.thead { background: var(--head-bg); font-size: 13px; color: #5b4636; font-weight: 600; }
.rows { flex: 1; }
.tr { border-bottom: 1px solid var(--line); }
.tr.sel { background: #fdf6ec; }
.tr.sub { padding-top: 0; border-bottom-style: dashed; }
.subbox { grid-column: 2 / -1; padding: 4px 0 8px; }
.addons { display: flex; flex-wrap: wrap; gap: 6px; align-items: center; }
.addon { display: inline-flex; gap: 6px; align-items: center; background: #fff7e0; border-radius: 8px; padding: 3px 8px; font-size: 13px; }
.code { color: var(--muted); font-variant-numeric: tabular-nums; font-size: 13px; }
.name b { display: block; } .spec { font-size: 12.5px; color: var(--muted); }
.c { text-align: center; } .r { text-align: end; font-variant-numeric: tabular-nums; } .r.b { font-weight: 700; }
.qty { display: inline-flex; align-items: center; gap: 6px; }
.qty button { width: 26px; height: 26px; border-radius: 6px; border: 1px solid var(--line); background: #fff; cursor: pointer; font: inherit; }
.qty button.f { background: var(--brand); color: #fff; border-color: var(--brand); }
.qty.sm button { width: 22px; height: 22px; font-size: 12px; }
.notes { display: flex; flex-wrap: wrap; gap: 4px; align-items: center; }
.nchip { background: var(--pink); color: var(--brand); border-radius: 999px; padding: 1px 8px; font-size: 12px; }
.note-in { flex: 1 1 90px; min-width: 0; border: 1px solid transparent; background: transparent; padding: 4px 6px; border-radius: 6px; font: inherit; font-size: 13px; }
.note-in:focus, .tr.sel .note-in { border-color: var(--line); background: #fff; }
.ops .del { border: 0; background: none; color: var(--bad); cursor: pointer; font-size: 15px; }
.empty { padding: 40px 14px; text-align: center; }
.chip { border: 1px solid var(--line); background: #fff; border-radius: 999px; padding: 4px 10px; font: inherit; font-size: 13px; cursor: pointer; }
.chip.on { background: var(--brand); color: #fff; border-color: var(--brand); }
.ocode { color: var(--muted); margin-inline-end: 4px; }
.bar { display: flex; flex-wrap: wrap; gap: 8px 22px; align-items: flex-end; background: #3a2218; color: #f6e9d6; padding: 12px 16px; border-radius: 0 0 12px 12px; }
.bar span { display: grid; gap: 2px; }
.bar i { font-style: normal; font-size: 12px; opacity: .75; }
.bar b { font-variant-numeric: tabular-nums; }
.bar .big { font-size: 18px; } .bar .gold { color: #f3c969; }
.qerr { color: var(--bad); padding: 8px 14px; font-size: 13.5px; }
/* the form */
.form { display: grid; gap: 12px; position: sticky; top: 12px; }
.fld { display: grid; gap: 6px; position: relative; }
.fld.two, .two { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; }
.k { font-weight: 600; font-size: 13.5px; color: #2f1810; } .k em { color: var(--bad); font-style: normal; }
.sk { display: block; font-size: 12.5px; color: var(--muted); margin-bottom: 2px; }
.inp { width: 100%; background: #fff; }
.pfs { display: flex; gap: 8px; overflow-x: auto; scrollbar-width: none; }
.pf { --pc: #888; flex: 0 0 auto; display: inline-flex; align-items: center; gap: 6px; min-height: 44px; padding: 6px 16px; border-radius: 10px;
  border: 2px solid var(--line); background: #fff; font: inherit; font-weight: 700; cursor: pointer; color: var(--pc); }
.pf img { width: 22px; height: 22px; border-radius: 5px; object-fit: cover; }
.pf.on { background: var(--pc); border-color: var(--pc); color: #fff; }
.nobox { display: flex; align-items: stretch; }
.nobox .pre { display: inline-grid; place-items: center; padding: 0 10px; color: #fff; font-weight: 700; border-radius: 8px 0 0 8px; background: #888; }
.nobox .inp { border-start-start-radius: 0; border-end-start-radius: 0; font-size: 16px; font-weight: 600; }
.seg { display: flex; border: 1px solid var(--line); border-radius: 8px; overflow: hidden; background: #fff; }
.seg button { flex: 1; border: 0; background: none; padding: 9px 6px; font: inherit; cursor: pointer; color: #5b4636; }
.seg button.on { background: var(--pink); color: var(--brand); font-weight: 700; }
.hits { position: absolute; top: 100%; inset-inline: 0; z-index: 20; background: #fff; border: 1px solid var(--line); border-radius: 10px; box-shadow: 0 8px 24px rgba(0,0,0,.12); overflow: hidden; }
.hits button { display: flex; gap: 8px; width: 100%; border: 0; background: none; padding: 9px 12px; font: inherit; text-align: start; cursor: pointer; }
.hits button.at { background: var(--pink); } .hits button.out { opacity: .5; }
.hint { font-size: 12.5px; } .hint.ok { color: var(--ok); } .hint.gold { color: #9a6a00; }
.warn { background: #fff4d6; color: #7a5200; border-radius: 8px; padding: 6px 10px; font-size: 13px; }
.money { border-top: 1px dashed var(--line); padding-top: 10px; display: grid; gap: 6px; }
.mrow { display: flex; justify-content: space-between; align-items: center; gap: 8px; }
.mrow.end { justify-content: flex-end; }
.mrow.payout { font-size: 17px; color: var(--brand); }
.mrow b { font-variant-numeric: tabular-nums; }
.cedit { display: grid; grid-template-columns: 100px 1fr auto; gap: 6px; }
.tag.gold { background: #fff1c7; color: #7a5200; border-radius: 6px; padding: 0 6px; margin-inline-start: 4px; }
.confirm { min-height: 48px; font-size: 16px; }
.modal-mask { position: fixed; inset: 0; background: rgba(0,0,0,.35); display: grid; place-items: center; z-index: 50; padding: 16px; }
.modal-box { background: var(--card, #fff); border-radius: 14px; padding: 18px; width: min(440px, 100%); display: grid; gap: 10px; }
.modal-box h3 { margin: 0; } .modal-box .big { font-size: 18px; margin: 0; }
.sg .chips { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 4px; }
.row.end { justify-content: flex-end; gap: 8px; }
/* tablet and phone: one column, the confirm button stays at the bottom */
@media (max-width: 1100px) {
  .po-body { grid-template-columns: minmax(0, 1fr); }
  .form { position: static; order: -1; }
  .lines { min-height: 0; }
}
@media (max-width: 767px) {
  .thead { display: none; }
  .tr { grid-template-columns: minmax(0, 1fr) auto auto 28px; padding: 10px 12px; }
  .tr .code, .tr > .r:not(.b) { display: none; }
  .tr .notes { grid-column: 1 / -1; order: 9; }
  .subbox { grid-column: 1 / -1; }
  .tr.sub > span:first-child { display: none; }
  .fld.two, .two { grid-template-columns: 1fr; }
  .po .page { padding-bottom: calc(76px + env(safe-area-inset-bottom)); }
  .confirm { position: fixed; inset-inline: 12px; bottom: calc(10px + env(safe-area-inset-bottom)); z-index: 30; box-shadow: 0 6px 18px rgba(0,0,0,.2); }
  .bar { gap: 6px 14px; }
}
</style>
