<script setup lang="ts">
// The member list: a store's own members in its back office, or every
// store's members on the platform (admin). Opens a member's details and
// order history, and edits their details.
import { computed, onMounted, reactive, ref } from 'vue'
import { ApiError, get, put } from '../../api'
import { has, money, t } from '../../i18n'
import { session } from '../../session'
import '../manualStrings'
import './memberStrings'

const props = defineProps<{ admin?: boolean }>()

interface Stats { week: number; month: number; quarter: number; orders: number; spent: number; last_at: string | null; favorites: string[] }
interface Member {
  id: number; no: number; status: string; phone: string; name: string; source: string; contact: string
  address_line: string; address_unit: string; address_note: string; lat: number | null; lng: number | null; note: string
  expires_at: string | null; created_at: string
  branch_id: number; branch_name: string; merchant_name: string; currency: string; orders: number; spent: number; last_at: string | null
  stats?: Stats
}
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
const address = (m: Member) => [m.address_line, m.address_unit].filter(Boolean).join(' · ')
const pages = computed(() => Math.max(1, Math.ceil(list.total / SIZE)))
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
      : await get(base() + '/member-list' + query(q))
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
const memberPath = (m: Member) => props.admin ? `/admin/members/${m.id}` : `${base()}/members/${m.no}`
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
  const r = await get<{ items: MemberOrder[]; total: number }>(memberPath(open.m) + '/orders' + query({ page: open.page, page_size: 10 }))
  open.orders = r.items; open.total = r.total
}
function ordersGo(p: number) { open.page = p; void loadOrders().catch(e => { open.error = errText(e) }) }
const orderPages = computed(() => Math.max(1, Math.ceil(open.total / 10)))
const mapLink = (m: Member) => `https://www.google.com/maps/search/?api=1&query=${m.lat},${m.lng}`
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
    })
    ed.m = null
    if (open.m?.id === m.id) await show(saved)
    await load()
  } catch (e) { ed.error = errText(e) } finally { ed.saving = false }
}
</script>

<template>
  <div class="mbook" data-testid="members-page">
    <div class="row between wrap mb-head">
      <h1 v-if="!admin">{{ t('sm.title') }}</h1>
      <span class="small muted" data-testid="members-count">{{ t('sm.count', { n: list.total }) }}<template v-if="list.temp"> · {{ t('sm.count_temp', { n: list.temp }) }}</template></span>
    </div>

    <form class="mb-filters" @submit.prevent="search">
      <input v-model="f.q" type="search" class="mb-q" :placeholder="t('sm.search_ph')" data-testid="members-search" />
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
      <button class="btn">{{ t('sm.search') }}</button>
    </form>

    <div class="chips">
      <button :class="{ on: f.status === '' }" @click="f.status = ''; search()">{{ t('sm.all') }}</button>
      <button :class="{ on: f.status === 'active' }" @click="f.status = 'active'; search()">{{ t('sm.active') }}</button>
      <button :class="{ on: f.status === 'temp' }" data-testid="members-temp" @click="f.status = 'temp'; search()">{{ t('sm.temp') }}<b v-if="list.temp" class="chip-n">{{ list.temp }}</b></button>
    </div>

    <div v-if="error" class="banner bad" data-testid="members-error">{{ error }}</div>

    <div class="card mb-table">
      <table class="data manycols">
        <thead><tr>
          <th>{{ t('sm.col.no') }}</th><th>{{ t('sm.col.name') }}</th><th>{{ t('sm.col.phone') }}</th><th>{{ t('sm.col.source') }}</th>
          <th v-if="admin">{{ t('sm.col.store') }}</th><th>{{ t('sm.col.address') }}</th>
          <th class="num">{{ t('sm.col.orders') }}</th><th class="num">{{ t('sm.col.spent') }}</th><th>{{ t('sm.col.last') }}</th><th></th>
        </tr></thead>
        <tbody>
          <tr v-for="m in list.items" :key="m.id" :data-member="m.no" :class="{ temp: m.status === 'temp' }" data-testid="member-row">
            <td><a href="#" class="mono mb-no" @click.prevent="show(m)">{{ pad(m.no) }}</a>
              <span v-if="m.status === 'temp'" class="tag warn" :title="t('sm.temp_hint')">{{ t('sm.temp') }} · {{ t('sm.temp_left', { n: minutesLeft(m) }) }}</span></td>
            <td>{{ m.name || '—' }}</td>
            <td class="nowrap">{{ m.phone || '—' }}</td>
            <td>{{ srcText(m.source) }}<span v-if="m.contact" class="muted small"> · {{ m.contact }}</span></td>
            <td v-if="admin"><a href="#" @click.prevent="onlyStore(m.branch_id)">{{ m.branch_name }}</a><div v-if="m.merchant_name !== m.branch_name" class="muted small">{{ m.merchant_name }}</div></td>
            <td class="mb-addr">{{ address(m) || '—' }}</td>
            <td class="num">{{ m.orders }}</td>
            <td class="num nowrap">{{ money(m.spent, m.currency) }}</td>
            <td class="nowrap">{{ m.last_at ? when(m.last_at) : t('sm.never') }}</td>
            <td class="right nowrap"><span class="row acts">
              <button class="btn small" data-testid="member-view" @click="show(m)">{{ t('sm.view') }}</button>
              <button class="btn small" data-testid="member-edit" @click="edit(m)">{{ t('sm.edit') }}</button></span></td>
          </tr>
          <tr v-if="list.loaded && !list.items.length"><td :colspan="admin ? 10 : 9" class="muted mb-empty">{{ t(filtered ? 'sm.no_match' : 'sm.empty') }}</td></tr>
        </tbody>
      </table>
    </div>

    <div v-if="pages > 1" class="row small muted mb-pager">
      <span class="grow"></span><span>{{ t('sm.page', { p: f.page, t: pages }) }}</span>
      <button class="btn small" :disabled="f.page <= 1" @click="go(f.page - 1)">{{ t('sm.prev') }}</button>
      <button class="btn small" :disabled="f.page >= pages" data-testid="members-next" @click="go(f.page + 1)">{{ t('sm.next') }}</button>
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
            <template v-if="admin"><dt>{{ t('sm.col.store') }}</dt><dd>{{ open.m.branch_name }} <span v-if="open.m.merchant_name !== open.m.branch_name" class="muted small">· {{ open.m.merchant_name }}</span></dd></template>
            <dt>{{ t('sm.col.phone') }}</dt><dd>{{ open.m.phone || '—' }}</dd>
            <dt>{{ t('sm.col.source') }}</dt><dd>{{ srcText(open.m.source) }}</dd>
            <template v-if="open.m.contact"><dt>{{ t('sm.d.contact') }}</dt><dd>{{ open.m.contact }}</dd></template>
            <dt>{{ t('sm.col.address') }}</dt>
            <dd>{{ address(open.m) || '—' }}<template v-if="open.m.address_note"><br /><span class="muted small">{{ open.m.address_note }}</span></template>
              <template v-if="open.m.address_line"><br />
                <a v-if="open.m.lat != null" :href="mapLink(open.m)" target="_blank" rel="noopener" class="small">{{ t('sm.d.map') }}</a>
                <span v-else class="muted small">{{ t('sm.d.no_pin') }}</span></template></dd>
            <template v-if="open.m.note"><dt>{{ t('sm.d.note') }}</dt><dd class="pre">{{ open.m.note }}</dd></template>
            <dt>{{ t('sm.col.joined') }}</dt><dd>{{ day(open.m.created_at) }}</dd>
          </dl>

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
.mbook { display: grid; gap: 12px; min-width: 0; }
.mb-head h1 { font-size: 20px; }
.mb-filters { display: flex; flex-wrap: wrap; gap: 8px; }
.mb-filters .mb-q { flex: 1 1 260px; min-width: 0; max-width: 420px; }
.mb-filters select { width: auto; max-width: 100%; }
.mb-table { overflow-x: auto; padding: 0; }
.chips { display: flex; gap: 6px; flex-wrap: wrap; }
.chips button { padding: 6px 14px; border-radius: 999px; background: #fff; border: 1px solid var(--line); font-size: 13px; color: var(--muted); cursor: pointer; }
.chips button.on { background: var(--brand); border-color: var(--brand); color: #fff; font-weight: 700; }
.mono { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; }
.mb-no { font-weight: 700; margin-inline-end: 6px; }
.mb-addr { max-width: 260px; overflow-wrap: anywhere; }
.mb-empty { text-align: center; padding: 24px !important; }
tr.temp td { background: #fffaf0; }
.nowrap { white-space: nowrap; }
.chip-n { margin-inline-start: 4px; }
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
.mb-orders { display: grid; gap: 8px; }
.mb-order { border: 1px solid var(--line); border-radius: 10px; padding: 8px 10px; display: grid; gap: 4px; }
.mb-items { color: #3f4650; overflow-wrap: anywhere; }
.mb-two { display: grid; grid-template-columns: 1fr 1fr; gap: 0 12px; }
@media (max-width: 767px) {
  .mb-stats { grid-template-columns: repeat(3, minmax(0, 1fr)); }
  .mb-two { grid-template-columns: 1fr; }
  .mb-filters .mb-q { flex-basis: 100%; max-width: none; }
  .mb-addr { max-width: none; }
}
</style>
