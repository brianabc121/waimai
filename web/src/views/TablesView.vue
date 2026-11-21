<script setup lang="ts">
// Front desk for dine-in: open tables, see each bill by round, add dishes,
// void, take payment and close. Managers also maintain tables and tablets.
import { computed, reactive, ref, watch } from 'vue'
import QRCode from 'qrcode'
import { ApiError, get, post, put } from '../api'
import { money, minutesSince, t, toMajor, toMinor } from '../i18n'
import { useLive } from '../live'
import { currentRole, session } from '../session'
import type { BatchLine, Bill, DiningTable, OrderItem, Storefront } from '../types'
import BillLines from './BillLines.vue'
import DineMenu from './DineMenu.vue'
import TotalsTable from './TotalsTable.vue'

const bid = session.branchId!
const role = currentRole()!
const manager = ['owner', 'manager'].includes(role.role)
const cur = role.currency

const tables = ref<DiningTable[]>([])
const recent = ref<Bill[]>([])
const bill = ref<Bill | null>(null)       // the bill panel
const error = ref('')
const panelError = ref('')
const now = ref(Date.now())
setInterval(() => (now.value = Date.now()), 30000)

async function loadBill(id: number) {
  const b = await get<Bill>(`/merchant/table-sessions/${id}`)
  if (bill.value?.session_id === id && b.order.version < bill.value.order.version) return
  if (bill.value?.session_id === id) bill.value = b
}
const { online, refreshNow } = useLive(() => `/merchant/branches/${bid}/events`, async () => {
  const r = await get<{ tables: DiningTable[]; recent: Bill[] }>(`/merchant/branches/${bid}/tables`)
  tables.value = r.tables; recent.value = r.recent
  if (bill.value?.status === 'open') await loadBill(bill.value.session_id)
})

const shown = computed(() => tables.value.filter(x => x.active || x.session || manager))
const waited = (iso: string) => { void now.value; return minutesSince(iso) }

function fail(e: unknown, panel = false) {
  let msg = (e as Error).message
  if (e instanceof ApiError) {
    if (e.code === 'VERSION_CONFLICT') msg = t('bill_changed')
    else if (e.code === 'ITEMS_IN_PROGRESS') msg = t('items_in_progress', { n: e.details?.count ?? '' })
    else if (e.code === 'QUOTE_CHANGED') msg = t('amount_changed')
    else if (e.status === 0) msg = t('submit_failed')
  }
  if (panel) panelError.value = msg; else error.value = msg
}

// ---- open ----
const opening = reactive<{ table: DiningTable | null; guests: number }>({ table: null, guests: 2 })
async function click(tb: DiningTable) {
  error.value = ''; panelError.value = ''
  if (tb.session) {
    try { bill.value = await get<Bill>(`/merchant/table-sessions/${tb.session.id}`); mode.value = 'bill' } catch (e) { fail(e) }
  } else if (tb.active) { opening.table = tb; opening.guests = 2 }
}
async function openTable() {
  const tb = opening.table!
  try {
    bill.value = await post<Bill>(`/merchant/tables/${tb.id}/open`, { guests: opening.guests })
    opening.table = null; mode.value = 'bill'
  } catch (e) { fail(e); opening.table = null }
  await refreshNow()
}

// ---- bill panel ----
const mode = ref<'bill' | 'add' | 'pay'>('bill')
const store = ref<Storefront | null>(null)
const busy = ref(false)
const menu = ref<InstanceType<typeof DineMenu> | null>(null)
const pay = reactive({ received: 0, note: '', manifest: 0 })

async function startAdd() {
  panelError.value = ''
  try { store.value = await get<Storefront>(`/stores/${role.slug}`); mode.value = 'add' } catch (e) { fail(e, true) }
}
async function addBatch(lines: BatchLine[], expectedTotal: number, key: string) {
  busy.value = true; panelError.value = ''
  try {
    bill.value = await post<Bill>(`/merchant/table-sessions/${bill.value!.session_id}/batches`, { items: lines, expected_total: expectedTotal }, { 'Idempotency-Key': key })
    menu.value?.clear(); mode.value = 'bill'
  } catch (e) {
    fail(e, true)
    store.value = await get<Storefront>(`/stores/${role.slug}`).catch(() => store.value)
  }
  busy.value = false
  void refreshNow()
}
async function itemAction(it: OrderItem, action: 'serve' | 'void') {
  panelError.value = ''
  let body = {}
  if (action === 'void') {
    const reason = prompt(t('void_reason'))
    if (reason === null) return
    if (!reason.trim()) { panelError.value = t('reason_required'); return }
    body = { reason: reason.trim() }
  }
  try { bill.value = await post<Bill>(`/merchant/bill-items/${it.id}/${action}`, body) } catch (e) { fail(e, true) }
  void refreshNow()
}
function startPay() {
  const o = bill.value!.order
  pay.received = toMajor(o.totals.total); pay.note = ''; pay.manifest = o.manifest_version
  panelError.value = ''; mode.value = 'pay'
}
// A round added or a dish voided while the cashier is on the payment step:
// show the new amount and make them confirm again.
watch(() => bill.value?.order.manifest_version, v => {
  if (mode.value === 'pay' && bill.value && v !== pay.manifest) {
    pay.manifest = v!; pay.received = toMajor(bill.value.order.totals.total); panelError.value = t('bill_changed')
  }
})
const change = computed(() => bill.value ? toMinor(pay.received) - bill.value.order.totals.total : 0)
async function closeTable() {
  busy.value = true; panelError.value = ''
  try {
    bill.value = await post<Bill>(`/merchant/table-sessions/${bill.value!.session_id}/close`,
      { expected_manifest_version: pay.manifest, amount_received: toMinor(pay.received), note: pay.note })
    mode.value = 'bill'
  } catch (e) {
    fail(e, true)
    if (e instanceof ApiError && e.code === 'VERSION_CONFLICT') {
      bill.value = await get<Bill>(`/merchant/table-sessions/${bill.value!.session_id}`)
      pay.manifest = bill.value.order.manifest_version; pay.received = toMajor(bill.value.order.totals.total)
    }
  }
  busy.value = false
  void refreshNow()
}
const unserved = computed(() => bill.value?.order.items.filter(i => i.prep_status !== 'cancelled' && !i.served_at).length ?? 0)

// ---- table settings: QR, tablets, rename ----
const setup = reactive<{ table: DiningTable | null; qr: string; code: string; copied: boolean }>({ table: null, qr: '', code: '', copied: false })
const ipadUrl = location.origin + '/ipad'
const link = (tb: DiningTable) => `${location.origin}/t/${tb.qr_token}`
async function openSetup(tb: DiningTable) {
  setup.table = tb; setup.code = ''; setup.copied = false
  setup.qr = await QRCode.toDataURL(link(tb), { width: 220, margin: 1 })
}
watch(tables, list => { if (setup.table) setup.table = list.find(x => x.id === setup.table!.id) ?? null })
async function copy() { try { await navigator.clipboard.writeText(link(setup.table!)); setup.copied = true } catch { /* shown as text anyway */ } }
async function pairTablet() {
  try { setup.code = (await post<{ code: string }>(`/merchant/tables/${setup.table!.id}/devices`, { name: '' })).code } catch (e) { fail(e) }
  await refreshNow()
}
async function unpair(id: number) {
  try { await post(`/merchant/branches/${bid}/table-devices/${id}/revoke`) } catch (e) { fail(e) }
  await refreshNow()
}
async function saveTable(tb: DiningTable | null, patch: Partial<DiningTable> = {}) {
  let name = tb?.name ?? ''
  if (!tb || patch.name !== undefined) {
    const v = prompt(t('table_name'), name)
    if (v === null || !v.trim()) return
    name = v.trim()
  }
  const body = { name, active: patch.active ?? tb?.active ?? true, sort: tb?.sort ?? tables.value.length }
  try {
    if (tb) await put(`/merchant/branches/${bid}/tables/${tb.id}`, body)
    else await post(`/merchant/branches/${bid}/tables`, body)
  } catch (e) { fail(e) }
  await refreshNow()
}
</script>

<template>
  <div class="page">
    <div class="row between wrap" style="margin-bottom:12px">
      <h1>{{ t('tables') }}</h1>
      <button v-if="manager" class="btn" @click="saveTable(null)">{{ t('new_table') }}</button>
    </div>
    <div v-if="!online" class="banner warn" style="margin-bottom:12px">{{ t('offline') }}</div>
    <div v-if="error" class="banner bad" style="margin-bottom:12px">{{ error }}</div>

    <div v-if="shown.length === 0" class="card muted">{{ t('no_tables') }}</div>
    <div class="tgrid">
      <div v-for="tb in shown" :key="tb.id" class="card tcard" :class="{ busy: tb.session, off: !tb.active }" :data-table="tb.name" @click="click(tb)">
        <div class="row between">
          <b class="tname">{{ tb.name }}</b>
          <span class="tag" :class="tb.session ? 'brand' : tb.active ? 'ok' : ''">{{ tb.session ? t('occupied') : tb.active ? t('free') : t('disabled') }}</span>
        </div>
        <template v-if="tb.session">
          <div class="price bold" style="font-size:18px">{{ money(tb.session.total, cur) }}</div>
          <div class="small muted">{{ t('n_guests', { n: tb.session.guests }) }} · {{ waited(tb.session.opened_at) }} {{ t('min') }}</div>
          <div class="small"><span v-if="tb.session.unserved" class="tag warn">{{ t('not_served_n', { n: tb.session.unserved }) }}</span></div>
        </template>
        <div v-else class="small muted">{{ tb.active ? t('open_table') : '' }}</div>
        <button class="btn link small gear" @click.stop="openSetup(tb)">{{ t('qr_code') }}</button>
      </div>
    </div>

    <h2 style="margin:22px 0 10px" v-if="recent.length">{{ t('recent_bills') }}</h2>
    <table class="data" v-if="recent.length">
      <thead><tr><th>{{ t('table') }}</th><th>{{ t('order_no') }}</th><th>{{ t('total') }}</th><th>{{ t('amount_received') }}</th><th>{{ t('closed_at') }}</th><th></th></tr></thead>
      <tbody>
        <tr v-for="b in recent" :key="b.session_id">
          <td>{{ b.table_name }}</td><td>{{ b.order.public_no }}</td><td>{{ money(b.order.totals.total, cur) }}</td>
          <td>{{ b.order.amount_received != null ? money(b.order.amount_received, cur) : '—' }}</td>
          <td>{{ b.closed_at ? new Date(b.closed_at).toLocaleString() : '' }}</td>
          <td><button class="btn small" @click="bill = b; mode = 'bill'; panelError = ''">{{ t('bill') }}</button></td>
        </tr>
      </tbody>
    </table>

    <!-- open a table -->
    <div class="sheet-mask" v-if="opening.table" @click.self="opening.table = null">
      <div class="sheet center">
        <div class="sheet-head"><h2>{{ t('open_table') }} · {{ opening.table.name }}</h2></div>
        <div class="sheet-body row between">
          <span>{{ t('guests') }}</span>
          <span class="stepper"><button @click="opening.guests = Math.max(1, opening.guests - 1)">−</button><b>{{ opening.guests }}</b><button class="fill" @click="opening.guests = Math.min(99, opening.guests + 1)">+</button></span>
        </div>
        <div class="sheet-foot row between"><button class="btn" @click="opening.table = null">{{ t('cancel') }}</button>
          <button class="btn primary" data-test="confirm-open" @click="openTable">{{ t('open_table') }}</button></div>
      </div>
    </div>

    <!-- bill panel -->
    <div class="sheet-mask" v-if="bill" @click.self="bill = null">
      <div class="sheet center" :class="{ big: mode === 'add' }">
        <div class="sheet-head row between">
          <h2>{{ t('table') }} {{ bill.table_name }} · {{ bill.order.public_no }}
            <span class="tag" :class="bill.status === 'open' ? 'brand' : 'ok'" data-test="bill-status">{{ bill.status === 'open' ? t('occupied') : t('f.dined') }}</span></h2>
          <button class="btn link" @click="mode === 'bill' ? (bill = null) : (mode = 'bill')">{{ mode === 'bill' ? '✕' : t('back') }}</button>
        </div>
        <div class="sheet-body stack" :style="mode === 'add' ? 'padding:0' : ''">
          <div v-if="panelError" class="banner bad" :style="mode === 'add' ? 'margin:12px 14px 0' : ''" data-test="panel-error">{{ panelError }}</div>

          <template v-if="mode === 'bill'">
            <div class="small muted">{{ t('n_guests', { n: bill.guests }) }} · {{ t('opened_at') }} {{ new Date(bill.opened_at).toLocaleTimeString() }}
              <span v-if="bill.closed_at"> · {{ t('closed_at') }} {{ new Date(bill.closed_at).toLocaleTimeString() }}</span></div>
            <BillLines :order="bill.order" staff :readonly="bill.status !== 'open'" @serve="itemAction($event, 'serve')" @void="itemAction($event, 'void')" />
            <TotalsTable :totals="bill.order.totals" :currency="cur" :delivery="false" />
            <div v-if="bill.status === 'closed' && bill.order.amount_received != null" class="small muted">
              {{ t('amount_received') }} {{ money(bill.order.amount_received, cur) }} · {{ t('change_due') }} {{ money(bill.order.amount_received - bill.order.totals.total, cur) }}
              <span v-if="bill.order.payment_note"> · {{ bill.order.payment_note }}</span></div>
          </template>

          <DineMenu v-else-if="mode === 'add' && store" ref="menu" :categories="store.categories" :currency="cur" :busy="busy" wide
            :storage-key="`staff-cart:${bill.session_id}`" @submit="addBatch" />

          <template v-else-if="mode === 'pay'">
            <TotalsTable :totals="bill.order.totals" :currency="cur" :delivery="false" />
            <div v-if="unserved" class="banner warn small">{{ t('not_served_n', { n: unserved }) }}</div>
            <label class="field"><span>{{ t('amount_received') }} ({{ cur }})</span><input type="number" step="0.01" min="0" v-model.number="pay.received" name="received" /></label>
            <div class="row between"><span>{{ t('change_due') }}</span><b :style="{ color: change < 0 ? 'var(--bad)' : '' }">{{ change < 0 ? t('amount_short') : money(change, cur) }}</b></div>
            <label class="field"><span>{{ t('payment_note') }}</span><input v-model="pay.note" maxlength="200" /></label>
          </template>
        </div>
        <div class="sheet-foot row between" v-if="bill.status === 'open' && mode !== 'add'">
          <template v-if="mode === 'bill'">
            <button class="btn" data-test="add-dishes" @click="startAdd">{{ t('add_dishes') }}</button>
            <button class="btn primary" data-test="pay" @click="startPay">{{ t('close_table') }}</button>
          </template>
          <template v-else>
            <span class="price bold">{{ money(bill.order.totals.total, cur) }}</span>
            <button class="btn primary" data-test="confirm-close" :disabled="busy || change < 0" @click="closeTable">{{ t('confirm_close') }}</button>
          </template>
        </div>
      </div>
    </div>

    <!-- table code, tablets, settings -->
    <div class="sheet-mask" v-if="setup.table" @click.self="setup.table = null">
      <div class="sheet center">
        <div class="sheet-head row between"><h2>{{ t('table') }} {{ setup.table.name }}</h2><button class="btn link" @click="setup.table = null">✕</button></div>
        <div class="sheet-body stack">
          <div class="row top" style="gap:14px">
            <img :src="setup.qr" alt="QR" width="160" height="160" />
            <div class="stack grow">
              <div class="small muted">{{ t('qr_hint') }}</div>
              <code class="small" style="word-break:break-all" data-test="table-link">{{ link(setup.table) }}</code>
              <button class="btn small" @click="copy">{{ setup.copied ? t('copied') : t('copy_link') }}</button>
            </div>
          </div>
          <h3>{{ t('tablets') }}</h3>
          <div v-for="d in setup.table.devices" :key="d.id" class="row between">
            <span>{{ d.name || t('table') + ' ' + setup.table.name }} <span class="tag" :class="d.paired ? 'ok' : 'warn'">{{ d.paired ? new Date(d.paired_at!).toLocaleDateString() : t('pending_pair') }}</span></span>
            <button class="btn small" @click="unpair(d.id)">{{ t('unpair') }}</button>
          </div>
          <div v-if="setup.code" class="banner info">
            <div>{{ t('pair_code') }}: <b style="font-size:22px;letter-spacing:3px" data-test="pair-code">{{ setup.code }}</b></div>
            <div class="small">{{ t('pair_hint', { url: ipadUrl }) }}</div>
          </div>
          <button class="btn" data-test="pair-tablet" @click="pairTablet">{{ t('pair_tablet') }}</button>
          <div class="row" v-if="manager" style="gap:8px">
            <button class="btn small" @click="saveTable(setup.table, { name: '' })">{{ t('rename') }}</button>
            <button class="btn small" :disabled="!!setup.table.session && setup.table.active" @click="saveTable(setup.table, { active: !setup.table.active })">{{ setup.table.active ? t('disable') : t('enable') }}</button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.tgrid { display: grid; grid-template-columns: repeat(auto-fill, minmax(170px, 1fr)); gap: 12px; }
.tcard { cursor: pointer; min-height: 118px; display: flex; flex-direction: column; gap: 4px; position: relative; }
.tcard.busy { border-color: var(--brand); background: var(--brand-soft); }
.tcard.off { opacity: .55; }
.tname { font-size: 22px; }
.gear { position: absolute; right: 8px; bottom: 6px; }
.sheet.big { max-width: 980px; height: 88vh; }
</style>
