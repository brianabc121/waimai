<script setup lang="ts">
// The tables of a store: managers add them (name, area, seats) and pair
// tablets; everyone sees which are in use and the bills closed today. Opening,
// ordering and payment are on the dine-in desk (代客下单 > 堂食).
import { computed, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import QRCode from 'qrcode'
import { ApiError, get, post, put } from '../api'
import { money, minutesSince, t } from '../i18n'
import { useLive } from '../live'
import { can, currentRole, session } from '../session'
import type { Bill, DiningTable } from '../types'
import BillLines from './BillLines.vue'
import TotalsTable from './TotalsTable.vue'
import './dine/strings'

const bid = session.branchId!
const role = currentRole()!
const manager = can('table.manage')
const cur = role.currency

const tables = ref<DiningTable[]>([])
const recent = ref<Bill[]>([])
const error = ref('')
const now = ref(Date.now())
setInterval(() => (now.value = Date.now()), 30000)

const { online, refreshNow } = useLive(() => `/merchant/branches/${bid}/events`, async () => {
  const r = await get<{ tables: DiningTable[]; recent: Bill[] }>(`/merchant/branches/${bid}/tables`)
  tables.value = r.tables; recent.value = r.recent
})

const shown = computed(() => tables.value.filter(x => x.active || x.session || manager))
const waited = (iso: string) => { void now.value; return minutesSince(iso) }

function fail(e: unknown) {
  error.value = e instanceof ApiError && e.status === 0 ? t('submit_failed') : (e as Error).message
}

// Ordering, the bill and payment happen on the dine-in desk
// (docs/DINE_IN_ORDER_DESIGN.md): a tap on a table opens it there.
const router = useRouter()
function click(tb: DiningTable) {
  if (!tb.active && !tb.session) return
  void router.push({ path: '/merchant/orders/new', query: { mode: 'dine', table: String(tb.id), ...(tb.session ? { view: 'bill' } : {}) } })
}
const bill = ref<Bill | null>(null) // a closed bill from the list below, read only

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
// name, area and seats in one small form (docs/DINE_IN_ORDER_DESIGN.md §5.1)
const edit = reactive<{ table: DiningTable | null; open: boolean; name: string; area: string; seats: number; busy: boolean }>({ table: null, open: false, name: '', area: '', seats: 4, busy: false })
const areas = computed(() => [...new Set(tables.value.map(x => x.area).filter(Boolean))])
function startEdit(tb: DiningTable | null) {
  Object.assign(edit, { table: tb, open: true, name: tb?.name ?? '', area: tb?.area ?? '', seats: tb?.seats || 4, busy: false })
  error.value = ''
}
async function saveEdit() {
  if (!edit.name.trim()) return
  edit.busy = true
  const tb = edit.table
  const body = { name: edit.name.trim(), area: edit.area.trim(), seats: edit.seats, active: tb?.active ?? true, sort: tb?.sort ?? tables.value.length }
  try {
    if (tb) await put(`/merchant/branches/${bid}/tables/${tb.id}`, body)
    else await post(`/merchant/branches/${bid}/tables`, body)
    edit.open = false
  } catch (e) { fail(e) }
  edit.busy = false
  await refreshNow()
}
async function setActive(tb: DiningTable, active: boolean) {
  try { await put(`/merchant/branches/${bid}/tables/${tb.id}`, { name: tb.name, area: tb.area, seats: tb.seats, active, sort: tb.sort }) } catch (e) { fail(e) }
  await refreshNow()
}
</script>

<template>
  <div class="page">
    <div class="row between wrap" style="margin-bottom:12px">
      <h1>{{ t('tables') }}</h1>
      <div class="row" style="gap:8px">
        <router-link class="btn primary" to="/merchant/orders/new?mode=dine" data-test="to-desk">{{ t('dd.go_desk') }}</router-link>
        <button v-if="manager" class="btn" data-test="new-table" @click="startEdit(null)">{{ t('new_table') }}</button>
      </div>
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
        <div class="small muted">{{ [tb.area, t('dd.seats', { n: tb.seats || 4 })].filter(Boolean).join(' · ') }}</div>
        <template v-if="tb.session">
          <div class="price bold" style="font-size:18px">{{ money(tb.session.total, cur) }}</div>
          <div class="small muted">{{ t('n_guests', { n: tb.session.guests }) }} · {{ waited(tb.session.opened_at) }} {{ t('min') }}</div>
          <div class="small"><span v-if="tb.session.unserved" class="tag warn">{{ t('not_served_n', { n: tb.session.unserved }) }}</span></div>
        </template>
        <button class="btn link small gear" @click.stop="openSetup(tb)">{{ t('qr_code') }}</button>
      </div>
    </div>

    <h2 style="margin:22px 0 10px" v-if="recent.length">{{ t('recent_bills') }}</h2>
    <table class="data" v-if="recent.length">
      <thead><tr><th>{{ t('table') }}</th><th>{{ t('order_no') }}</th><th>{{ t('total') }}</th><th>{{ t('amount_received') }}</th><th>{{ t('closed_at') }}</th><th></th></tr></thead>
      <tbody>
        <tr v-for="b in recent" :key="b.session_id">
          <td>{{ b.table_name }}</td><td>{{ b.order.public_no }}</td><td>{{ money(b.order.totals.total, cur) }}</td>
          <td>{{ b.order.amount_received != null ? money(b.order.amount_received, cur) : '—' }}<span v-if="b.order.pay_method" class="muted"> · {{ b.order.pay_method }}</span></td>
          <td>{{ b.closed_at ? new Date(b.closed_at).toLocaleString() : '' }}</td>
          <td><button class="btn small" @click="bill = b">{{ t('bill') }}</button></td>
        </tr>
      </tbody>
    </table>

    <!-- a closed bill -->
    <div class="sheet-mask" v-if="bill" @click.self="bill = null">
      <div class="sheet center">
        <div class="sheet-head row between">
          <h2>{{ t('table') }} {{ bill.table_name }} · {{ bill.order.public_no }}
            <span class="tag" :class="bill.status === 'open' ? 'brand' : 'ok'" data-test="bill-status">{{ bill.status === 'open' ? t('occupied') : t('f.dined') }}</span></h2>
          <button class="btn link" @click="bill = null">✕</button>
        </div>
        <div class="sheet-body stack">
          <div class="small muted">{{ t('n_guests', { n: bill.guests }) }} · {{ t('opened_at') }} {{ new Date(bill.opened_at).toLocaleTimeString() }}
            <span v-if="bill.closed_at"> · {{ t('closed_at') }} {{ new Date(bill.closed_at).toLocaleTimeString() }}</span></div>
          <BillLines :order="bill.order" staff readonly />
          <TotalsTable :totals="bill.order.totals" :currency="cur" :delivery="false" />
          <div v-if="bill.order.amount_received != null" class="small muted">
            {{ t('amount_received') }} {{ money(bill.order.amount_received, cur) }} · {{ t('change_due') }} {{ money(bill.order.amount_received - bill.order.totals.total, cur) }}
            <span v-if="bill.order.pay_method"> · {{ bill.order.pay_method }}</span>
            <span v-if="bill.order.payment_note"> · {{ bill.order.payment_note }}</span></div>
          <router-link class="btn small" :to="`/ticket/${bill.order.id}`" target="_blank">{{ t('dd.print') }}</router-link>
        </div>
      </div>
    </div>

    <!-- add or change a table -->
    <div class="sheet-mask" v-if="edit.open" @click.self="edit.open = false">
      <div class="sheet center" data-test="table-form">
        <div class="sheet-head row between"><h2>{{ edit.table ? t('dd.edit_table') : t('new_table') }}</h2><button class="btn link" @click="edit.open = false">✕</button></div>
        <div class="sheet-body stack">
          <label class="field"><span>{{ t('table_name') }}</span><input v-model="edit.name" maxlength="20" name="table-name" /></label>
          <label class="field"><span>{{ t('dd.area') }}</span><input v-model="edit.area" maxlength="20" name="table-area" list="table-areas" :placeholder="t('dd.area_ph')" /></label>
          <datalist id="table-areas"><option v-for="a in areas" :key="a" :value="a" /></datalist>
          <div class="row between"><span>{{ t('dd.seats_lbl') }}</span>
            <span class="stepper"><button @click="edit.seats = Math.max(1, edit.seats - 1)">−</button><b data-test="seats">{{ edit.seats }}</b><button class="fill" @click="edit.seats = Math.min(99, edit.seats + 1)">+</button></span></div>
        </div>
        <div class="sheet-foot row between"><button class="btn" @click="edit.open = false">{{ t('cancel') }}</button>
          <button class="btn primary" data-test="save-table" :disabled="edit.busy || !edit.name.trim()" @click="saveEdit">{{ t('save') }}</button></div>
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
            <button class="btn small" data-test="edit-table" @click="startEdit(setup.table); setup.table = null">{{ t('dd.edit_table') }}</button>
            <button class="btn small" :disabled="!!setup.table.session && setup.table.active" @click="setActive(setup.table, !setup.table.active)">{{ setup.table.active ? t('disable') : t('enable') }}</button>
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
.gear { position: absolute; inset-inline-end: 8px; bottom: 6px; }
</style>
