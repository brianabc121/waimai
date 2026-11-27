<script setup lang="ts">
// Customer addresses (docs/DELIVERY_ADDRESS_STATS_DESIGN.md): every address
// a store has delivered to, the addresses with the most deliveries in a
// period (31 days at most), and those addresses on a map with a heat map.
// A store sees its own; the platform (admin) every store, narrowed by
// merchant, store, country or city.
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ApiError, get } from '../../api'
import { has, money, t } from '../../i18n'
import { session } from '../../session'
import { mapsKey as loadMapsKey } from '../../maps'
import DeliveryMap, { type MapData } from './DeliveryMap.vue'
import { addDays, isoDay, lastMonth, spanDays } from './heat'
import '../members/memberStrings'
import './deliveryStrings'
import OxPager from '../OxPager.vue'
import '../../officeStrings'
import { brandsHere, multiBrand, useBrandPick } from '../../brands'
import BrandSeg from '../brands/BrandSeg.vue'
import type { BrandRef } from '../../types'

const props = defineProps<{ admin?: boolean }>()
const route = useRoute(), router = useRouter()

interface Row {
  branch_id: number; branch_name: string; merchant_name: string; cust: string; key: string; unit_key: string
  line: string; unit: string; name: string; phone: string; member_no: number | null; by_staff: boolean; by_customer: boolean
  orders: number; first_at: string; last_at: string; lat: number | null; lng: number | null
}
interface Top {
  key: string; line: string; city: string; orders: number; customers: number; amount: number | null; currency: string
  last_at: string; branch_name: string; branches: number; lat: number | null; lng: number | null
}
interface Unit { unit: string; name: string; phone: string; orders: number; branch_name: string }
interface Order {
  id: number; public_no: string; created_at: string; channel: string; service_type: string
  acceptance_status: string; fulfillment_status: string; total: number; currency: string; items: string
}
interface Branch { id: number; name: string; merchant_id: number; merchant_name: string; timezone?: string }
interface Region { country_code: string; country: string; city: string; stores: number }

type Tab = 'list' | 'top' | 'map'
const MAX_DAYS = 31
const SIZE = 50
const tab = ref<Tab>((['list', 'top', 'map'] as const).find(x => x === route.query.tab) ?? 'list')
watch(tab, v => { void router.replace({ query: { ...route.query, tab: v } }) })

// ---- scope (the platform's filters) and period ----
// "Today" is the store's: the periods are whole days by the store's clock.
function dayIn(tz: string): string {
  try { return tz ? new Intl.DateTimeFormat('en-CA', { timeZone: tz, year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date()) : isoDay(new Date()) }
  catch { return isoDay(new Date()) }
}
const today = ref(isoDay(new Date()))
const sc = reactive({ merchant: 0, branch: 0, country: '', city: '' })
const range = reactive({ from: addDays(today.value, -29), to: today.value })
const branches = ref<Branch[]>([])
const regions = ref<Region[]>([])
const merchants = computed(() => {
  const m = new Map<number, string>()
  for (const b of branches.value) m.set(b.merchant_id, b.merchant_name)
  return [...m.entries()].sort((a, b) => a[1].localeCompare(b[1]))
})
const storesOf = computed(() => branches.value.filter(b => !sc.merchant || b.merchant_id === sc.merchant))
const countries = computed(() => {
  const m = new Map<string, string>()
  for (const r of regions.value) m.set(r.country_code, r.country || r.country_code)
  return [...m.entries()]
})
const cities = computed(() => regions.value.filter(r => r.city && (!sc.country || r.country_code === sc.country)))
const days = computed(() => range.from && range.to ? spanDays(range.from, range.to) : 0)
const rangeOk = computed(() => days.value >= 1 && days.value <= MAX_DAYS)
const preset = computed(() => {
  if (range.to !== today.value) {
    const [a, b] = lastMonth(today.value)
    return range.from === a && range.to === b ? 'last_month' : ''
  }
  return days.value === 1 ? 'today' : days.value === 7 ? 'd7' : days.value === 30 ? 'd30' : ''
})
function setPreset(p: string) {
  if (p === 'last_month') [range.from, range.to] = lastMonth(today.value)
  else Object.assign(range, { to: today.value, from: addDays(today.value, p === 'today' ? 0 : p === 'd7' ? -6 : -29) })
}

function errText(e: unknown): string {
  if (e instanceof ApiError && e.code === 'RANGE_TOO_LONG') return t('da.too_long', { n: MAX_DAYS, d: days.value })
  return (e as Error).message
}
// one brand, or every brand together (docs/MULTI_BRAND_DESIGN.md §12); the
// platform picks a brand once it has picked a store
const { pick: brandPick } = useBrandPick('addresses', brandsHere, true)
const adminBrand = ref(0)
const adminBrands = ref<BrandRef[]>([])
watch(() => sc.branch, async id => {
  adminBrand.value = 0; adminBrands.value = []
  if (props.admin && id) adminBrands.value = (await get<{ brands: BrandRef[] }>(`/admin/delivery-addresses/brands?branch_id=${id}`).catch(() => ({ brands: [] }))).brands
})
const brandQ = () => props.admin ? (sc.branch ? adminBrand.value : 0) : brandPick.value
function qs(extra: Record<string, string | number>) {
  const u = new URLSearchParams()
  const all: Record<string, string | number> = props.admin
    ? { merchant_id: sc.merchant, branch_id: sc.branch, country: sc.country, city: sc.city, brand: brandQ(), ...extra } : { brand: brandQ(), ...extra }
  for (const [k, v] of Object.entries(all)) if (v !== '' && v !== 0) u.set(k, String(v))
  return '?' + u.toString()
}
const base = () => props.admin ? '/admin' : `/merchant/branches/${session.branchId}`
const listPath = () => props.admin ? '/admin/delivery-addresses' : base() + '/customer-addresses'
const statPath = (p: string) => base() + '/delivery-stats/' + p
const period = () => ({ from: range.from, to: range.to })

// ---- the address list ----
const f = reactive({ q: '', source: '', sort: 'recent', page: 1 })
const list = reactive({ items: [] as Row[], total: 0, customers: 0, addresses: 0, loaded: false, error: '' })
const pages = computed(() => Math.max(1, Math.ceil(list.total / SIZE)))
async function loadList() {
  try {
    const r = await get<{ items: Row[]; total: number; customers: number; addresses: number }>(
      listPath() + qs({ q: f.q.trim(), source: f.source, sort: f.sort, page: f.page, page_size: SIZE }))
    Object.assign(list, r, { loaded: true, error: '' })
  } catch (e) { list.error = errText(e) }
}
function search() { f.page = 1; void loadList() }
function go(p: number) { f.page = p; void loadList() }

// orders at one row's address
const ord = reactive({ row: null as Row | null, items: [] as Order[], total: 0, error: '' })
async function showOrders(r: Row) {
  Object.assign(ord, { row: r, items: [], total: 0, error: '' })
  try {
    const path = props.admin ? '/admin/delivery-addresses/orders' : base() + '/customer-addresses/orders'
    const q = new URLSearchParams({ cust: r.cust, key: r.key, unit: r.unit_key, page_size: '50' })
    if (props.admin) q.set('branch_id', String(r.branch_id))
    if (brandQ()) q.set('brand', String(brandQ()))
    const l = await get<{ items: Order[]; total: number }>(path + '?' + q.toString())
    ord.items = l.items; ord.total = l.total
  } catch (e) { ord.error = errText(e) }
}
function orderState(o: Order) {
  if (o.acceptance_status !== 'accepted') return t('a.' + o.acceptance_status)
  return has('f.' + o.fulfillment_status) ? t('f.' + o.fulfillment_status) : t('a.accepted')
}

// ---- the ranking ----
const top = reactive({ items: [] as Top[], orders: 0, addresses: 0, unpinned: 0, loaded: false, error: '' })
const openKey = ref(''), units = ref<Unit[]>([])
async function loadTop() {
  if (!rangeOk.value) return
  try {
    const r = await get<{ items: Top[]; orders: number; addresses: number; unpinned: number }>(statPath('top') + qs(period()))
    Object.assign(top, r, { loaded: true, error: '' })
    openKey.value = ''
  } catch (e) { top.error = errText(e) }
}
async function toggle(x: Top) {
  if (openKey.value === x.key) { openKey.value = ''; return }
  openKey.value = x.key; units.value = []
  try { units.value = (await get<{ items: Unit[] }>(statPath('top-detail') + qs({ ...period(), key: x.key }))).items }
  catch (e) { top.error = errText(e) }
}
const maxTop = computed(() => top.items[0]?.orders ?? 1)
const pct = (n: number) => top.orders ? (n / top.orders * 100).toFixed(1) + '%' : '—'
const avg = computed(() => top.addresses ? (top.orders / top.addresses).toFixed(1) : '0')

// ---- the map ----
const key = ref<string | null>(null)
const mapData = ref<MapData | null>(null)
const mapError = ref('')
const focus = ref('')
async function loadMap() {
  if (!rangeOk.value) return
  try {
    key.value ??= await loadMapsKey()
    mapData.value = await get<MapData>(statPath('points') + qs(period()))
    mapError.value = ''
  } catch (e) { mapError.value = errText(e) }
}
function onMap(k: string) { focus.value = ''; tab.value = 'map'; setTimeout(() => { focus.value = k }) }
function onRank(k: string) { tab.value = 'top'; setTimeout(() => { if (top.items.some(x => x.key === k) && openKey.value !== k) void toggle(top.items.find(x => x.key === k)!) }, 300) }

// ---- loading what the tab shows ----
const stale = reactive({ list: true, top: true, map: true })
const ready = ref(false)
function refresh() {
  if (!ready.value && tab.value !== 'list') return
  if (tab.value === 'list' && stale.list) { stale.list = false; void loadList() }
  if (tab.value === 'top' && stale.top && rangeOk.value) { stale.top = false; void loadTop() }
  if (tab.value === 'map' && stale.map && rangeOk.value) { stale.map = false; void loadMap() }
}
watch(tab, refresh)
watch(() => [range.from, range.to], () => { stale.top = stale.map = true; refresh() })
watch(() => [sc.merchant, sc.branch, sc.country, sc.city, brandPick.value, adminBrand.value], () => { stale.list = stale.top = stale.map = true; f.page = 1; refresh() })
watch(() => sc.merchant, () => { if (sc.branch && !storesOf.value.some(b => b.id === sc.branch)) sc.branch = 0 })
watch(() => sc.country, () => { if (sc.city && !cities.value.some(c => c.city === sc.city)) sc.city = '' })

onMounted(async () => {
  if (tab.value === 'list') refresh()
  let tz = ''
  if (props.admin) {
    const [b, r] = await Promise.all([
      get<{ branches: Branch[] }>('/admin/branches').catch(() => ({ branches: [] })),
      get<{ regions: Region[] }>('/admin/delivery-addresses/regions').catch(() => ({ regions: [] })),
    ])
    branches.value = b.branches; regions.value = r.regions
    const n = new Map<string, number>()
    for (const x of b.branches) if (x.timezone) n.set(x.timezone, (n.get(x.timezone) ?? 0) + 1)
    tz = [...n.entries()].sort((a, b) => b[1] - a[1])[0]?.[0] ?? ''
  } else {
    tz = (await get<{ branch: { timezone: string } }>(base() + '/menu').catch(() => null))?.branch.timezone ?? ''
  }
  today.value = dayIn(tz)
  Object.assign(range, { to: today.value, from: addDays(today.value, -29) })
  ready.value = true
  refresh()
})

const two = (n: number) => String(n).padStart(2, '0')
function when(iso: string): string {
  const d = new Date(iso)
  return `${d.getFullYear()}-${two(d.getMonth() + 1)}-${two(d.getDate())} ${two(d.getHours())}:${two(d.getMinutes())}`
}
const short = (iso: string) => when(iso).slice(5)
const day = (iso: string) => when(iso).slice(0, 10)
const gmaps = (p: { lat: number | null; lng: number | null }) => `https://www.google.com/maps/search/?api=1&query=${p.lat},${p.lng}`
const filtered = computed(() => !!(f.q || f.source))
</script>

<template>
  <div class="dab" data-testid="delivery-book">
    <div class="ox-head dab-head">
      <div><h1 v-if="!admin">{{ t('cm.addresses') }}</h1><p class="ox-sub">{{ t('ox.da.sub') }}</p></div>
      <span v-if="list.loaded" class="dab-count" data-testid="dab-count">{{ t('da.count', { a: list.addresses, c: list.customers }) }}</span>
    </div>

    <div class="ox-tabs">
      <button v-for="x in (['list', 'top', 'map'] as const)" :key="x" type="button" :class="{ on: tab === x }" :data-testid="'tab-' + x" @click="tab = x">{{ t('da.tab.' + x) }}</button>
    </div>

    <div v-if="admin" class="ox-tools dab-filters" data-testid="dab-scope">
      <select v-model.number="sc.merchant" data-testid="dab-merchant">
        <option :value="0">{{ t('da.merchant_all') }}</option>
        <option v-for="[id, name] in merchants" :key="id" :value="id">{{ name }}</option>
      </select>
      <select v-model.number="sc.branch" data-testid="dab-store">
        <option :value="0">{{ t('da.store_all') }}</option>
        <option v-for="b in storesOf" :key="b.id" :value="b.id">{{ b.name }}<template v-if="!sc.merchant && b.merchant_name !== b.name"> · {{ b.merchant_name }}</template></option>
      </select>
      <select v-model="sc.country" data-testid="dab-country">
        <option value="">{{ t('da.country_all') }}</option>
        <option v-for="[c, name] in countries" :key="c" :value="c">{{ name }}</option>
      </select>
      <select v-model="sc.city" data-testid="dab-city">
        <option value="">{{ t('da.city_all') }}</option>
        <option v-for="c in cities" :key="c.country_code + c.city" :value="c.city">{{ c.city }}</option>
      </select>
      <select v-if="adminBrands.length > 1" v-model.number="adminBrand" data-testid="dab-brand">
        <option :value="0">{{ t('br.all') }}</option>
        <option v-for="b in adminBrands" :key="b.id" :value="b.id">{{ b.name }}</option>
      </select>
    </div>
    <div v-else-if="multiBrand()" class="ox-tools" data-testid="dab-brands"><BrandSeg v-model="brandPick" :brands="brandsHere()" all /></div>

    <div v-if="tab !== 'list'" class="ox-tools ox-chips dab-range" data-testid="dab-range">
      <input v-model="range.from" type="date" :max="range.to" data-testid="dab-from" />
      <span class="muted">{{ t('da.to') }}</span>
      <input v-model="range.to" type="date" :min="range.from" :max="today" data-testid="dab-to" />
      <button v-for="p in ['today', 'd7', 'd30', 'last_month']" :key="p" type="button" :class="{ on: preset === p }" :data-testid="'preset-' + p" @click="setPreset(p)">{{ t('da.' + p) }}</button>
      <span v-if="!rangeOk && days > 0" class="dab-bad small" data-testid="dab-too-long">{{ t('da.too_long', { n: MAX_DAYS, d: days }) }}</span>
      <span v-else class="small muted">{{ t('da.max', { n: MAX_DAYS }) }}</span>
    </div>

    <!-- the address list -->
    <div v-if="tab === 'list'" class="ox-panel">
      <form class="ox-tools dab-filters" @submit.prevent="search">
        <label class="ox-search"><input v-model="f.q" type="search" :placeholder="t('da.search_ph')" data-testid="dab-search" /></label>
        <select v-model="f.source" data-testid="dab-source" @change="search">
          <option value="">{{ t('da.src.all') }}</option>
          <option value="staff">{{ t('da.src.staff') }}</option><option value="customer">{{ t('da.src.customer') }}</option>
        </select>
        <select v-model="f.sort" data-testid="dab-sort" @change="search">
          <option v-for="s in ['recent', 'orders', 'first']" :key="s" :value="s">{{ t('da.sort.' + s) }}</option>
        </select>
        <button class="btn primary">{{ t('da.search') }}</button>
      </form>
      <div v-if="list.error" class="banner bad">{{ list.error }}</div>
      <div class="ox-scroll">
        <table class="data manycols">
          <thead><tr>
            <th>{{ t('da.col.address') }}</th><th>{{ t('da.col.unit') }}</th><th>{{ t('da.col.customer') }}</th><th>{{ t('da.col.source') }}</th>
            <th v-if="admin">{{ t('da.col.store') }}</th><th class="num">{{ t('da.col.orders') }}</th>
            <th>{{ t('da.col.first') }}</th><th>{{ t('da.col.last') }}</th><th class="right">{{ t('ox.ops') }}</th>
          </tr></thead>
          <tbody>
            <tr v-for="r in list.items" :key="r.branch_id + r.cust + r.key + r.unit_key" data-testid="dab-row">
              <td class="dab-addr"><b class="ox-strong">{{ r.line }}</b> <span v-if="r.lat == null" class="tag warn">{{ t('da.no_pin') }}</span></td>
              <td :data-label="t('da.col.unit')">{{ r.unit || '—' }}</td>
              <td :data-label="t('da.col.customer')">{{ r.name || '—' }}<div class="small muted nowrap">{{ r.phone }}<template v-if="r.member_no != null"> · <span class="mono dab-no">{{ String(r.member_no).padStart(6, '0') }}</span></template></div></td>
              <td :data-label="t('da.col.source')"><span v-if="r.by_staff" class="ox-tag brand">{{ t('da.src.staff') }}</span> <span v-if="r.by_customer" class="ox-tag gold">{{ t('da.src.customer') }}</span></td>
              <td v-if="admin" :data-label="t('da.col.store')">{{ r.branch_name }}<div v-if="r.merchant_name !== r.branch_name" class="small muted">{{ r.merchant_name }}</div></td>
              <td class="num" :data-label="t('da.col.orders')"><b>{{ r.orders }}</b></td>
              <td class="nowrap muted" :data-label="t('da.col.first')">{{ day(r.first_at) }}</td>
              <td class="nowrap" :data-label="t('da.col.last')">{{ when(r.last_at) }}</td>
              <td class="right nowrap"><span class="ox-ops acts">
                <button class="op red" data-testid="dab-orders" @click="showOrders(r)">{{ t('da.orders') }}</button>
                <a v-if="r.lat != null" class="op" :href="gmaps(r)" target="_blank" rel="noopener" data-testid="dab-view">{{ t('da.view') }}</a></span></td>
            </tr>
            <tr v-if="list.loaded && !list.items.length"><td :colspan="admin ? 9 : 8" class="ox-empty dab-empty">{{ t(filtered ? 'da.no_match' : 'da.empty') }}</td></tr>
          </tbody>
        </table>
      </div>
      <div v-if="pages > 1" class="ox-foot dab-pager">
        <span>{{ t('da.page', { p: f.page, t: pages }) }}</span><span class="grow"></span>
        <OxPager :page="f.page" :pages="pages" testid="dab-pager" @go="go" />
      </div>
    </div>

    <!-- the ranking -->
    <template v-if="tab === 'top'">
      <div v-if="top.error" class="banner bad">{{ top.error }}</div>
      <div v-if="top.loaded" class="ox-stats dab-kpis" data-testid="dab-kpis">
        <div class="ox-stat"><span class="l">{{ t('da.k.orders') }}</span><b class="v">{{ top.orders.toLocaleString() }}</b></div>
        <div class="ox-stat"><span class="l">{{ t('da.k.addresses') }}</span><b class="v">{{ top.addresses.toLocaleString() }}</b></div>
        <div class="ox-stat"><span class="l">{{ t('da.k.avg') }}</span><b class="v">{{ avg }}</b></div>
        <div class="ox-stat"><span class="l">{{ t('da.k.unpinned') }}</span><b class="v" :class="{ red: top.unpinned > 0 }">{{ top.unpinned }}</b></div>
      </div>
      <div class="ox-panel ox-scroll">
        <table class="data dab-top">
          <thead><tr>
            <th>#</th><th>{{ t('da.col.address') }}</th><th class="num">{{ t('da.col.orders') }}</th><th>{{ t('da.col.share') }}</th>
            <th class="num">{{ t('da.col.customers') }}</th><th class="num">{{ t('da.col.amount') }}</th><th>{{ t('da.col.last_once') }}</th><th class="right">{{ t('ox.ops') }}</th>
          </tr></thead>
          <tbody>
            <template v-for="(x, i) in top.items" :key="x.key">
              <tr :class="{ open: openKey === x.key }" data-testid="top-row">
                <td><span class="rank" :class="'r' + (i + 1)">{{ i + 1 }}</span></td>
                <td class="dab-addr"><b>{{ x.line }}</b> <span v-if="x.lat == null" class="tag warn">{{ t('da.no_pin') }}</span>
                  <div class="small muted">{{ [x.city, admin ? x.branch_name + (x.branches > 1 ? ' ' + t('da.stores_n', { n: x.branches }) : '') : ''].filter(Boolean).join(' · ') }}</div>
                  <div class="meter"><i :style="{ width: (x.orders / maxTop * 100) + '%' }"></i></div>
                  <div class="dab-m small"><b>{{ t('da.n_orders', { n: x.orders }) }}</b> · {{ pct(x.orders) }} · {{ t('da.n_customers', { n: x.customers }) }}
                    · {{ x.amount != null ? money(x.amount, x.currency) : t('da.mixed') }} · {{ short(x.last_at) }}</div></td>
                <td class="num dab-d"><b>{{ x.orders }}</b></td>
                <td class="nowrap dab-d">{{ pct(x.orders) }}</td>
                <td class="num dab-d">{{ x.customers }}</td>
                <td class="num nowrap dab-d">{{ x.amount != null ? money(x.amount, x.currency) : t('da.mixed') }}</td>
                <td class="nowrap dab-d">{{ short(x.last_at) }}</td>
                <td class="right nowrap"><span class="ox-ops acts">
                  <button v-if="x.lat != null" class="op red" data-testid="top-on-map" @click="onMap(x.key)">{{ t('da.on_map') }}</button>
                  <button class="op" data-testid="top-detail" @click="toggle(x)">{{ openKey === x.key ? t('da.hide') + ' ▴' : t('da.detail') + ' ▾' }}</button></span></td>
              </tr>
              <tr v-if="openKey === x.key" class="open sub" data-testid="top-units">
                <td></td>
                <td colspan="7"><div v-for="(u, k) in units" :key="k" class="row small dab-unit" data-testid="top-unit">
                  <span class="dab-u">{{ u.unit || '—' }}</span><span class="grow">{{ u.name || '—' }} <span class="muted">{{ u.phone }}</span></span>
                  <span v-if="admin" class="muted">{{ u.branch_name }}</span><b>{{ t('da.n_orders', { n: u.orders }) }}</b>
                </div></td>
              </tr>
            </template>
            <tr v-if="top.loaded && !top.items.length"><td colspan="8" class="ox-empty dab-empty">{{ t('da.empty') }}</td></tr>
          </tbody>
        </table>
      </div>
      <p v-if="top.items.length" class="small muted">{{ t('da.top_hint', { n: 100 }) }}</p>
    </template>

    <!-- the map -->
    <template v-if="tab === 'map'">
      <div v-if="mapError" class="banner bad">{{ mapError }}</div>
      <div v-if="key === ''" class="banner info" data-testid="dab-no-key">{{ t('da.no_key') }}</div>
      <div v-else-if="mapData?.too_many" class="banner warn" data-testid="dab-too-many">{{ t('da.too_many', { n: '20,000' }) }}</div>
      <DeliveryMap v-else-if="key && mapData" class="dab-map" :maps-key="key" :data="mapData" :focus="focus" :admin="admin && !sc.branch" @rank="onRank" />
    </template>

    <!-- orders at one address -->
    <div v-if="ord.row" class="sheet-mask" @click.self="ord.row = null">
      <div class="sheet center dab-sheet" data-testid="dab-orders-sheet">
        <div class="sheet-head row between">
          <h2>{{ t('da.addr_orders') }}</h2>
          <button class="btn link" :aria-label="t('da.close')" @click="ord.row = null">✕</button>
        </div>
        <div class="sheet-body stack">
          <div class="small"><b>{{ [ord.row.line, ord.row.unit].filter(Boolean).join(' · ') }}</b>
            <div class="muted">{{ ord.row.name }} {{ ord.row.phone }}<template v-if="admin"> · {{ ord.row.branch_name }}</template></div></div>
          <div v-if="ord.error" class="banner bad">{{ ord.error }}</div>
          <div v-if="!ord.items.length && !ord.error" class="muted small">{{ t('da.no_orders') }}</div>
          <div v-for="o in ord.items" :key="o.id" class="dab-order" data-testid="dab-order">
            <div class="row wrap small">
              <b class="mono">{{ o.public_no }}</b><span class="muted">{{ when(o.created_at) }}</span>
              <span class="tag" :class="{ brand: o.channel === 'staff' }">{{ o.channel === 'staff' ? t('da.src.staff') : t('da.src.customer') }}</span>
              <span class="grow"></span><span>{{ orderState(o) }}</span><b>{{ money(o.total, o.currency) }}</b>
            </div>
            <div class="small dab-items">{{ o.items }}</div>
          </div>
        </div>
        <div class="sheet-foot row"><button class="btn grow" @click="ord.row = null">{{ t('da.close') }}</button></div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.dab { display: grid; gap: 14px; min-width: 0; }
.dab-count { color: var(--muted); font-size: 13.5px; align-self: flex-end; }
.dab-range { margin-bottom: 0; }
.dab-bad { color: var(--bad); font-weight: 600; }
.dab-addr { max-width: 320px; overflow-wrap: anywhere; }
.dab-addr .tag { white-space: nowrap; }
.dab-empty { text-align: center; padding: 24px !important; }
.nowrap { white-space: nowrap; }
.num { text-align: end; }
.mono { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; }
.dab-no { color: var(--brand); font-weight: 700; }
.dab-map { border-radius: 12px; overflow: hidden; border: 1px solid var(--line); }
td .tag { white-space: nowrap; }
@media (min-width: 768px) { .acts { flex-wrap: nowrap; } }
.rank { width: 28px; height: 28px; border-radius: 50%; display: inline-flex; align-items: center; justify-content: center; font-weight: 700; font-size: 13px; background: #f3ebdf; color: #6f5a4b; }
.rank.r1 { background: var(--brand); color: #fff; } .rank.r2 { background: #c8641e; color: #fff; } .rank.r3 { background: #d9a03a; color: #fff; }
.meter { height: 6px; border-radius: 3px; background: #f3ebdf; overflow: hidden; margin-top: 6px; max-width: 260px; }
.meter i { display: block; height: 100%; background: linear-gradient(90deg, #e0a53c, var(--brand)); border-radius: 3px; }
tr.open td { background: #fffaf5; }
.dab-unit { gap: 12px; padding: 3px 0; }
.dab-u { min-width: 120px; }
.dab-sheet { max-width: 680px; }
.dab-sheet h2 { font-size: 18px; }
.dab-order { border: 1px solid var(--line); border-radius: 10px; padding: 8px 10px; display: grid; gap: 4px; }
.dab-items { color: #3f4650; overflow-wrap: anywhere; }
.dab-m { display: none; color: var(--muted); margin-top: 4px; }
@media (max-width: 767px) {
  .dab-filters select { flex: 1 1 40%; }
  .dab-range input { flex: 1 1 38%; }
  .dab-addr { max-width: none; }
  .dab-u { min-width: 70px; }
  table.dab-top td:first-child { display: inline-block; vertical-align: top; margin-inline-end: 8px; }
  table.dab-top td.dab-addr { display: inline-block; width: calc(100% - 44px) !important; }
  table.dab-top td.dab-d { display: none; }
  table.dab-top td.dab-addr { padding-inline-start: 0 !important; text-align: start !important; }
  table.dab-top td.dab-addr::before { content: none !important; }
  .dab-m { display: block; }
  table.dab-top td:last-child { padding-inline-start: 44px; }
  table.dab-top .acts { justify-content: flex-start; }
}
</style>
