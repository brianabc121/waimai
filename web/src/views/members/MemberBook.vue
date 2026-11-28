<script setup lang="ts">
// The member list: a store's own members in its back office, or every
// store's members on the platform (admin). Opens a member's details and
// order history, and edits their details.
import { addrText } from '../../place'
import AddrText from '../AddrText.vue'
import { computed, onMounted, reactive, ref } from 'vue'
import { ApiError, del, get, put } from '../../api'
import { has, money, t } from '../../i18n'
import { session } from '../../session'
import OxPager from '../OxPager.vue'
import '../../officeStrings'
import '../manualStrings'
import './memberStrings'
import { brandsHere, multiBrand, useBrandPick } from '../../brands'
import BrandSeg from '../brands/BrandSeg.vue'

const props = defineProps<{ admin?: boolean }>()
// Members are kept per brand and never shown merged (docs/MULTI_BRAND_DESIGN.md §8).
const { pick: brandPick } = useBrandPick('members')

interface Stats { week: number; month: number; quarter: number; orders: number; spent: number; last_at: string | null; favorites: string[] }
interface Member {
  id: number; brand_id: number; no: number; status: string; phone: string; name: string; source: string; contact: string
  address_line: string; address_unit: string; address_note: string; lat: number | null; lng: number | null; note: string
  place_name?: string; place_id?: string
  expires_at: string | null; created_at: string
  branch_id: number; branch_name: string; brand_name: string; brand_code: string; merchant_name: string; currency: string; orders: number; spent: number; last_at: string | null
  stats?: Stats
  address_count: number; addresses?: Address[]
}
interface Address { id: number; line: string; place_name?: string; unit: string; note: string; lat: number | null; lng: number | null; uses: number; last_used_at: string; current: boolean }
interface MemberOrder {
  id: number; public_no: string; created_at: string; channel: string; order_source: string; service_type: string
  acceptance_status: string; fulfillment_status: string; total: number; currency: string; items: string; address_line: string; linked: boolean
}
interface Branch { id: number; name: string; merchant_id: number; merchant_name: string }

const SOURCES = ['wechat', 'line', 'whatsapp', 'phone', 'other']
const SORTS = ['new', 'recent', 'orders', 'spent']
const SIZE = 20

const f = reactive({ q: '', status: '', source: '', sort: 'new', branch: 0, page: 1 })
const list = reactive({ items: [] as Member[], total: 0, temp: 0, loaded: false })
const error = ref('')
const branches = ref<Branch[]>([])

const pad = (n: number) => String(n).padStart(6, '0')
const two = (n: number) => String(n).padStart(2, '0')
function when(iso: string | null): string {
  if (!iso) return '—'
  const d = new Date(iso)
  return `${d.getFullYear()}-${two(d.getMonth() + 1)}-${two(d.getDate())} ${two(d.getHours())}:${two(d.getMinutes())}`
}
const day = (iso: string) => when(iso).slice(0, 10)
const srcText = (s: string) => s ? (has(`vo.src.${s}`) ? t(`vo.src.${s}`) : s) : '—'
const minutesLeft = (m: Member) => Math.max(1, Math.ceil((new Date(m.expires_at ?? 0).getTime() - Date.now()) / 60000))
const address = (m: Member) => addrText(m)
const pages = computed(() => Math.max(1, Math.ceil(list.total / SIZE)))
const pageOrders = computed(() => list.items.reduce((n, m) => n + m.orders, 0))
const pageSpent = computed(() => {
  const cur = list.items[0]?.currency
  if (!cur || list.items.some(m => m.currency !== cur)) return '—'
  return money(list.items.reduce((n, m) => n + m.spent, 0), cur)
})
const srcTone = (s: string) => ({ wechat: 'gold', line: 'blue', whatsapp: 'blue', phone: 'brand' } as Record<string, string>)[s] ?? ''
const filtered = computed(() => !!(f.q || f.status || f.source || f.branch))

function errText(e: unknown): string {
  if (e instanceof ApiError && has(`vo.err.${e.code}`)) return t(`vo.err.${e.code}`, { s: '' })
  return (e as Error).message
}

const base = () => `/merchant/branches/${session.branchId}`
function query(extra: Record<string, string | number>) {
  const u = new URLSearchParams()
  for (const [k, v] of Object.entries(extra)) if (v !== '' && v !== 0) u.set(k, String(v))
  return '?' + u.toString()
}
async function load() {
  const q = { q: f.q.trim(), status: f.status, source: f.source, sort: f.sort, page: f.page, page_size: SIZE }
  try {
    const r = props.admin
      ? await get('/admin/members' + query({ ...q, branch_id: f.branch }))
      : await get(base() + '/member-list' + query({ ...q, brand: brandPick.value }))
    Object.assign(list, r, { loaded: true })
    error.value = ''
  } catch (e) { error.value = errText(e) }
}
function search() { f.page = 1; void load() }
function go(p: number) { f.page = p; void load() }
function onlyStore(b: number) { f.branch = b; search() }

onMounted(async () => {
  void load()
  if (props.admin) branches.value = (await get<{ branches: Branch[] }>('/admin/branches').catch(() => ({ branches: [] }))).branches
})
const merchants = computed(() => {
  const m = new Map<string, Branch[]>()
  for (const b of branches.value) m.set(b.merchant_name, [...(m.get(b.merchant_name) ?? []), b])
  return [...m.entries()]
})

// ---- one member ----
const memberPath = (m: Member, sub = '') => props.admin ? `/admin/members/${m.id}${sub}` : `${base()}/members/${m.no}${sub}?brand=${m.brand_id || brandPick.value}`
const withQ = (url: string, extra: Record<string, string | number>) => url + (url.includes('?') ? '&' + query(extra).slice(1) : query(extra))
const open = reactive({ m: null as Member | null, orders: [] as MemberOrder[], total: 0, page: 1, error: '' })
async function show(m: Member) {
  Object.assign(open, { m, orders: [], total: 0, page: 1, error: '' })
  try {
    open.m = await get<Member>(memberPath(m))
    await loadOrders()
  } catch (e) { open.error = errText(e) }
}
async function loadOrders() {
  if (!open.m) return
  const r = await get<{ items: MemberOrder[]; total: number }>(withQ(memberPath(open.m, '/orders'), { page: open.page, page_size: 10 }))
  open.orders = r.items; open.total = r.total
}
function ordersGo(p: number) { open.page = p; void loadOrders().catch(e => { open.error = errText(e) }) }
const orderPages = computed(() => Math.max(1, Math.ceil(open.total / 10)))
const mapLink = (m: { lat: number | null; lng: number | null }) => `https://www.google.com/maps/search/?api=1&query=${m.lat},${m.lng}`
async function dropAddress(a: Address) {
  const m = open.m
  if (!m || !confirm(t('sm.a.delete_confirm', { s: [a.line, a.unit].filter(Boolean).join(' · ') }))) return
  try {
    await del(memberPath(m, `/addresses/${a.id}`))
    m.addresses = (m.addresses ?? []).filter(x => x.id !== a.id)
    const row = list.items.find(x => x.id === m.id)
    if (row) row.address_count = Math.max(0, row.address_count - 1)
  } catch (e) { open.error = errText(e) }
}
function orderState(o: MemberOrder) {
  if (o.acceptance_status !== 'accepted') return t('a.' + o.acceptance_status)
  return has('f.' + o.fulfillment_status) ? t('f.' + o.fulfillment_status) : t('a.accepted')
}
const orderTag = (o: MemberOrder) => o.channel === 'staff' ? t('vo.tag', { s: srcText(o.order_source) }) : t('sm.o.self')

// ---- editing ----
const ed = reactive({ m: null as Member | null, phone: '', name: '', source: '', contact: '', address_line: '', address_unit: '',
  address_note: '', note: '', error: '', saving: false })
function edit(m: Member) {
  Object.assign(ed, { m, phone: m.phone, name: m.name, source: m.source, contact: m.contact, address_line: m.address_line,
    address_unit: m.address_unit, address_note: m.address_note, note: m.note, error: '', saving: false })
}
const addrChanged = computed(() => !!ed.m && ed.m.lat != null && ed.address_line.trim() !== ed.m.address_line)
async function save() {
  const m = ed.m
  if (!m) return
  ed.saving = true
  const keepPin = !addrChanged.value
  try {
    const saved = await put<Member>(memberPath(m), {
      phone: ed.phone, name: ed.name, source: ed.source, contact: ed.contact, address_line: ed.address_line,
      address_unit: ed.address_unit, address_note: ed.address_note, note: ed.note,
      lat: keepPin ? m.lat : null, lng: keepPin ? m.lng : null,
      // the server drops the place's name once the address no longer starts with it
      place_name: m.place_name ?? '', place_id: keepPin ? m.place_id ?? '' : '',
    })
    ed.m = null
    if (open.m?.id === m.id) await show(saved)
    await load()
  } catch (e) { ed.error = errText(e) } finally { ed.saving = false }
}
</script>

<template>
  <div class="mbook" data-testid="members-page">
    <div v-if="!admin" class="ox-head">
      <div><h1>{{ t('cm.members') }}</h1><p class="ox-sub">{{ t('ox.sm.sub') }}</p></div>
    </div>
    <div v-if="!admin && multiBrand()" class="mb-brands" data-testid="members-brands">
      <BrandSeg v-model="brandPick" :brands="brandsHere()" @update:model-value="search" />
      <span class="muted small">{{ t('br.members_note') }}</span>
    </div>

    <div class="ox-stats" data-testid="members-stats">
      <div class="ox-stat"><span class="l">{{ t('ox.sm.total') }}</span><b class="v" data-testid="members-count" :data-n="list.total">{{ list.total }}</b>
        <span class="s">{{ t('sm.count', { n: list.total }) }}</span></div>
      <div class="ox-stat"><span class="l">{{ t('sm.temp') }}</span><b class="v" :class="{ red: list.temp > 0 }">{{ list.temp }}</b>
        <span class="s">{{ t('ox.sm.temp_s') }}</span></div>
      <div class="ox-stat"><span class="l">{{ t('ox.sm.page_orders') }}</span><b class="v">{{ pageOrders }}</b>
        <span class="s">{{ t('ox.sm.page_note') }}</span></div>
      <div class="ox-stat"><span class="l">{{ t('ox.sm.page_spent') }}</span><b class="v">{{ pageSpent }}</b>
        <span class="s">{{ t('ox.sm.page_note') }}</span></div>
    </div>

    <div class="ox-panel">
      <form class="ox-tools" @submit.prevent="search">
        <label class="ox-search"><input v-model="f.q" type="search" :placeholder="t('sm.search_ph')" data-testid="members-search" /></label>
        <select v-if="admin" v-model.number="f.branch" data-testid="members-store" @change="search">
          <option :value="0">{{ t('sm.store_all') }}</option>
          <optgroup v-for="[name, bs] in merchants" :key="name" :label="name">
            <option v-for="b in bs" :key="b.id" :value="b.id">{{ b.name }}</option>
          </optgroup>
        </select>
        <select v-model="f.source" data-testid="members-source" @change="search">
          <option value="">{{ t('sm.any_source') }}</option>
          <option v-for="s in SOURCES" :key="s" :value="s">{{ srcText(s) }}</option>
        </select>
        <select v-model="f.sort" data-testid="members-sort" @change="search">
          <option v-for="s in SORTS" :key="s" :value="s">{{ t('sm.sort.' + s) }}</option>
        </select>
        <button class="btn primary">{{ t('sm.search') }}</button>
      </form>

      <div class="ox-chips chips mb-chips">
        <button :class="{ on: f.status === '' }" @click="f.status = ''; search()">{{ t('sm.all') }}</button>
        <button :class="{ on: f.status === 'active' }" @click="f.status = 'active'; search()">{{ t('sm.active') }}</button>
        <button :class="{ on: f.status === 'temp' }" data-testid="members-temp" @click="f.status = 'temp'; search()">{{ t('sm.temp') }}<b v-if="list.temp" class="n">{{ list.temp }}</b></button>
      </div>

      <div v-if="error" class="banner bad" data-testid="members-error">{{ error }}</div>

      <div class="ox-scroll">
        <table class="data manycols">
          <thead><tr>
            <th>{{ t('sm.col.no') }}</th><th>{{ t('sm.col.name') }}</th><th>{{ t('sm.col.phone') }}</th><th>{{ t('sm.col.source') }}</th>
            <th v-if="admin">{{ t('sm.col.store') }}</th><th>{{ t('sm.col.address') }}</th>
            <th class="num">{{ t('sm.col.orders') }}</th><th class="num">{{ t('sm.col.spent') }}</th><th>{{ t('sm.col.last') }}</th><th class="right">{{ t('ox.ops') }}</th>
          </tr></thead>
          <tbody>
            <tr v-for="m in list.items" :key="m.id" :data-member="m.no" :class="{ temp: m.status === 'temp' }" data-testid="member-row">
              <td><a href="#" class="mono mb-no ox-code" @click.prevent="show(m)">{{ (multiBrand() && !admin ? m.brand_code : '') + pad(m.no) }}</a>
                <span v-if="m.status === 'temp'" class="tag warn ox-pill" :title="t('sm.temp_hint')">{{ t('sm.temp') }} · {{ t('sm.temp_left', { n: minutesLeft(m) }) }}</span></td>
              <td><span class="ox-strong">{{ m.name || '—' }}</span></td>
              <td class="nowrap">{{ m.phone || '—' }}</td>
              <td><span v-if="m.source" class="ox-tag" :class="srcTone(m.source)">{{ srcText(m.source) }}</span><span v-else>—</span><span v-if="m.contact" class="muted small"> {{ m.contact }}</span></td>
              <td v-if="admin"><a href="#" @click.prevent="onlyStore(m.branch_id)">{{ m.branch_name }}</a><span v-if="m.brand_name && m.brand_name !== m.branch_name" class="small"> · {{ m.brand_name }}</span><div v-if="m.merchant_name !== m.branch_name" class="muted small">{{ m.merchant_name }}</div></td>
              <td class="mb-addr">{{ address(m) || '—' }}<span v-if="m.address_count > 1" class="tag" :title="t('sm.a.more_title')">{{ t('sm.a.more', { n: m.address_count - 1 }) }}</span></td>
              <td class="num">{{ m.orders }}</td>
              <td class="num"><span class="ox-money">{{ money(m.spent, m.currency) }}</span></td>
              <td class="nowrap">{{ m.last_at ? when(m.last_at) : t('sm.never') }}</td>
              <td class="right nowrap"><span class="ox-ops acts">
                <button class="op red" data-testid="member-view" @click="show(m)">{{ t('sm.view') }}</button>
                <button class="op" data-testid="member-edit" @click="edit(m)">{{ t('sm.edit') }}</button></span></td>
            </tr>
            <tr v-if="list.loaded && !list.items.length"><td :colspan="admin ? 10 : 9" class="ox-empty mb-empty">{{ t(filtered ? 'sm.no_match' : 'sm.empty') }}</td></tr>
          </tbody>
        </table>
      </div>

      <div v-if="list.total" class="ox-foot">
        <span>{{ t('ox.range', { a: (f.page - 1) * SIZE + 1, b: Math.min(f.page * SIZE, list.total), n: list.total }) }}</span>
        <span class="grow"></span>
        <OxPager :page="f.page" :pages="pages" testid="members-pager" @go="go" />
      </div>
    </div>

    <!-- one member -->
    <div v-if="open.m" class="sheet-mask" @click.self="open.m = null">
      <div class="sheet center mb-sheet" data-testid="member-detail">
        <div class="sheet-head row between">
          <h2><span class="mono">{{ pad(open.m.no) }}</span> {{ open.m.name }}
            <span v-if="open.m.status === 'temp'" class="tag warn">{{ t('sm.temp') }}</span></h2>
          <button class="btn link" :aria-label="t('sm.close')" @click="open.m = null">✕</button>
        </div>
        <div class="sheet-body stack">
          <div v-if="open.error" class="banner bad">{{ open.error }}</div>
          <div v-if="open.m.status === 'temp'" class="banner warn small">{{ t('sm.temp_hint') }}</div>
          <dl class="mb-info">
            <template v-if="admin"><dt>{{ t('sm.col.store') }}</dt><dd>{{ open.m.branch_name }}<template v-if="open.m.brand_name && open.m.brand_name !== open.m.branch_name"> · {{ open.m.brand_name }}</template> <span v-if="open.m.merchant_name !== open.m.branch_name" class="muted small">· {{ open.m.merchant_name }}</span></dd></template>
            <dt>{{ t('sm.col.phone') }}</dt><dd>{{ open.m.phone || '—' }}</dd>
            <dt>{{ t('sm.col.source') }}</dt><dd>{{ srcText(open.m.source) }}</dd>
            <template v-if="open.m.contact"><dt>{{ t('sm.d.contact') }}</dt><dd>{{ open.m.contact }}</dd></template>
            <template v-if="open.m.note"><dt>{{ t('sm.d.note') }}</dt><dd class="pre">{{ open.m.note }}</dd></template>
            <dt>{{ t('sm.col.joined') }}</dt><dd>{{ day(open.m.created_at) }}</dd>
          </dl>

          <h3>{{ t('sm.a.title', { n: open.m.addresses?.length ?? 0 }) }}</h3>
          <div v-if="!open.m.addresses?.length" class="muted small">—</div>
          <div v-else class="mb-addrs" data-testid="member-addr-list">
            <div v-for="a in open.m.addresses" :key="a.id" class="mb-addr-item" :class="{ cur: a.current }" data-testid="member-addr-item">
              <div class="row wrap small">
                <span class="grow"><AddrText :o="{ address_line: a.line, address_unit: a.unit, place_name: a.place_name }" /></span>
                <span v-if="a.current" class="tag brand">{{ t('sm.a.current') }}</span>
                <button v-else class="btn small" data-testid="member-address-delete" @click="dropAddress(a)">{{ t('sm.a.delete') }}</button>
              </div>
              <div v-if="a.note" class="small muted">{{ a.note }}</div>
              <div class="row wrap small muted">
                <span>{{ a.uses ? t('sm.a.uses', { n: a.uses }) : t('sm.a.saved') }}</span><span>· {{ day(a.last_used_at) }}</span>
                <a v-if="a.lat != null" :href="mapLink(a)" target="_blank" rel="noopener">{{ t('sm.d.map') }}</a>
                <span v-else>{{ t('sm.d.no_pin') }}</span>
              </div>
            </div>
            <p class="muted small">{{ t('sm.a.hint') }}</p>
          </div>

          <template v-if="open.m.stats">
            <h3>{{ t('sm.d.stats') }}</h3>
            <div class="mb-stats" data-testid="member-stats">
              <div><b>{{ open.m.stats.orders }}</b><span>{{ t('sm.d.total') }}</span></div>
              <div><b>{{ money(open.m.stats.spent, open.m.currency) }}</b><span>{{ t('sm.col.spent') }}</span></div>
              <div><b>{{ open.m.stats.week }}</b><span>{{ t('vo.week') }}</span></div>
              <div><b>{{ open.m.stats.month }}</b><span>{{ t('vo.month') }}</span></div>
              <div><b>{{ open.m.stats.quarter }}</b><span>{{ t('vo.quarter') }}</span></div>
            </div>
            <p v-if="open.m.stats.favorites.length" class="small">{{ t('vo.fav', { s: open.m.stats.favorites.join('、') }) }}</p>
          </template>

          <h3>{{ t('sm.d.orders') }}</h3>
          <div v-if="!open.orders.length" class="muted small">{{ t('sm.d.no_orders') }}</div>
          <div v-else class="mb-orders">
            <div v-for="o in open.orders" :key="o.id" class="mb-order" data-testid="member-order">
              <div class="row wrap small">
                <b class="mono">{{ o.public_no }}</b><span class="muted">{{ when(o.created_at) }}</span>
                <span class="tag">{{ t(o.service_type) }}</span><span class="tag info">{{ orderTag(o) }}</span>
                <span v-if="!o.linked" class="tag">{{ t('sm.d.by_phone') }}</span>
                <span class="grow"></span>
                <span :class="{ muted: o.acceptance_status === 'rejected' || o.acceptance_status === 'cancelled' }">{{ orderState(o) }}</span>
                <b>{{ money(o.total, o.currency) }}</b>
              </div>
              <div class="small mb-items">{{ o.items }}</div>
            </div>
            <p v-if="open.orders.some(o => !o.linked)" class="muted small">{{ t('sm.d.by_phone_hint') }}</p>
            <div v-if="orderPages > 1" class="row small muted">
              <span class="grow"></span><span>{{ t('sm.page', { p: open.page, t: orderPages }) }}</span>
              <button class="btn small" :disabled="open.page <= 1" @click="ordersGo(open.page - 1)">{{ t('sm.prev') }}</button>
              <button class="btn small" :disabled="open.page >= orderPages" @click="ordersGo(open.page + 1)">{{ t('sm.next') }}</button>
            </div>
          </div>
        </div>
        <div class="sheet-foot row">
          <button class="btn grow" @click="open.m = null">{{ t('sm.close') }}</button>
          <button class="btn primary grow" data-testid="member-detail-edit" @click="edit(open.m)">{{ t('sm.edit') }}</button>
        </div>
      </div>
    </div>

    <!-- editing -->
    <div v-if="ed.m" class="sheet-mask mb-edit-mask" @click.self="ed.m = null">
      <form class="sheet center" data-testid="member-form" @submit.prevent="save">
        <div class="sheet-head"><h2>{{ t('sm.edit_title', { no: pad(ed.m.no) }) }}</h2></div>
        <div class="sheet-body">
          <div v-if="ed.error" class="banner bad" style="margin-bottom:12px" data-testid="member-form-error">{{ ed.error }}</div>
          <div v-if="ed.m.status === 'temp'" class="banner warn small" style="margin-bottom:12px">{{ t('sm.temp_hint') }}</div>
          <div class="mb-two">
            <label class="field"><span>{{ t('vo.phone') }} *</span><input v-model="ed.phone" inputmode="tel" maxlength="20" data-testid="member-phone" /></label>
            <label class="field"><span>{{ t('vo.name') }}</span><input v-model="ed.name" maxlength="80" :placeholder="t('vo.name_ph')" data-testid="member-name" /></label>
            <label class="field"><span>{{ t('vo.source') }} *</span>
              <select v-model="ed.source" data-testid="member-source"><option value="" disabled>—</option>
                <option v-for="s in SOURCES" :key="s" :value="s">{{ srcText(s) }}</option></select></label>
            <label class="field"><span>{{ t('vo.contact') }}</span><input v-model="ed.contact" maxlength="80" :placeholder="t('vo.contact_ph')" /></label>
          </div>
          <label class="field"><span>{{ t('vo.address') }}</span><input v-model="ed.address_line" maxlength="300" data-testid="member-address" /></label>
          <div v-if="addrChanged" class="small muted" style="margin:-6px 0 10px" data-testid="member-addr-changed">{{ t('sm.addr_changed') }}</div>
          <div class="mb-two">
            <label class="field"><span>{{ t('sm.d.unit') }}</span><input v-model="ed.address_unit" maxlength="100" /></label>
            <label class="field"><span>{{ t('sm.d.addr_note') }}</span><input v-model="ed.address_note" maxlength="300" /></label>
          </div>
          <label class="field"><span>{{ t('vo.member_note') }}</span><textarea v-model="ed.note" rows="2" maxlength="300" :placeholder="t('vo.member_note_ph')" data-testid="member-note"></textarea></label>
        </div>
        <div class="sheet-foot row">
          <button type="button" class="btn grow" @click="ed.m = null">{{ t('sm.cancel') }}</button>
          <button class="btn primary grow" :disabled="ed.saving" data-testid="member-save">{{ t('sm.save') }}</button>
        </div>
      </form>
    </div>
  </div>
</template>

<style scoped>
.mbook { display: grid; gap: 14px; min-width: 0; }
.mb-chips { margin-bottom: 10px; }
.mono { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; }
.mb-no { font-weight: 700; margin-inline-end: 6px; }
.mb-addr { max-width: 260px; overflow-wrap: anywhere; }
.mb-empty { text-align: center; padding: 24px !important; }
tr.temp td { background: #fffaf0; }
.mb-no { text-decoration: none; }
.nowrap { white-space: nowrap; }
td .tag { white-space: nowrap; }
@media (min-width: 768px) { .acts { flex-wrap: nowrap; } }
.num { text-align: end; }
.mb-sheet { max-width: 760px; }
.mb-sheet h2 { font-size: 18px; }
.mb-sheet h3 { font-size: 15px; margin-top: 4px; }
.mb-edit-mask { z-index: 61; }
.mb-info { display: grid; grid-template-columns: max-content 1fr; gap: 6px 14px; margin: 0; font-size: 14px; }
.mb-info dt { color: var(--muted); }
.mb-info dd { margin: 0; overflow-wrap: anywhere; }
.mb-info .pre { white-space: pre-wrap; }
.mb-stats { display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); gap: 8px; }
.mb-stats > div { background: var(--bg); border-radius: 10px; padding: 8px 10px; display: grid; gap: 2px; min-width: 0; }
.mb-stats b { font-size: 16px; overflow-wrap: anywhere; }
.mb-stats span { font-size: 12px; color: var(--muted); }
.mb-orders, .mb-addrs { display: grid; gap: 8px; }
.mb-addr-item { border: 1px solid var(--line); border-radius: 10px; padding: 8px 10px; display: grid; gap: 3px; overflow-wrap: anywhere; }
.mb-brands { display: flex; flex-wrap: wrap; align-items: center; gap: 8px 12px; margin-bottom: 12px; }
.mb-addr-item.cur { border-color: var(--brand); background: var(--brand-soft); }
.mb-addr .tag { margin-inline-start: 6px; }
.mb-order { border: 1px solid var(--line); border-radius: 10px; padding: 8px 10px; display: grid; gap: 4px; }
.mb-items { color: #3f4650; overflow-wrap: anywhere; }
.mb-two { display: grid; grid-template-columns: 1fr 1fr; gap: 0 12px; }
@media (max-width: 767px) {
  .mb-stats { grid-template-columns: repeat(3, minmax(0, 1fr)); }
  .mb-two { grid-template-columns: 1fr; }
  .mb-addr { max-width: none; }
}
</style>
