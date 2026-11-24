<script setup lang="ts">
// The void log (docs/VOID_ITEM_DESIGN.md §8.7): one day of voids and refunds
// by dish, with breakdowns by reason, staff and dish.
import { ref, watch } from 'vue'
import { get } from '../api'
import { money, t, whenText } from '../i18n'
import { session } from '../session'
import '../voids'

interface Sum { key: string; count: number; qty: number; amount: number; approved: number }
interface Row { kind: 'void' | 'refund'; id: number; order_id: number; public_no: string; service_type: string; table_name: string
  name: string; qty: number; amount: number; reason: string; reason_kind: string; was_status: string; wasted: boolean; method?: string
  by: string; approved_by: string; ack_at: string | null; ack_by: string; at: string }
interface Log { date: string; currency: string; timezone: string; voids: number; void_qty: number; void_amount: number; wasted_amount: number
  refund_amount: number; refund_orders: number; by_reason: Sum[]; by_staff: Sum[]; by_dish: Sum[]; rows: Row[] }

const bid = session.branchId!
const date = ref('')
const log = ref<Log | null>(null)
const error = ref('')
async function load() {
  error.value = ''
  try {
    log.value = await get<Log>(`/merchant/branches/${bid}/voids${date.value ? `?date=${date.value}` : ''}`)
    if (!date.value) date.value = log.value.date
  } catch (e) { error.value = (e as Error).message }
}
void load()
watch(date, (v, old) => { if (old && v) void load() })
const m = (v: number) => money(v, log.value?.currency)
const clock = (iso: string) => whenText(iso, log.value?.timezone).split(' ').pop()
const where = (r: Row) => r.service_type === 'dine_in' && r.table_name ? `${t('table')} ${r.table_name}` : r.public_no
function shift(days: number) {
  const d = new Date(date.value + 'T12:00:00Z'); d.setUTCDate(d.getUTCDate() + days)
  date.value = d.toISOString().slice(0, 10)
}
</script>

<template>
  <div class="page vlog">
    <div class="row between wrap" style="margin-bottom:12px">
      <h1 style="margin:0">{{ t('vd.log.title') }}</h1>
      <div class="row" style="gap:6px">
        <button class="btn small" @click="shift(-1)">‹</button>
        <input v-model="date" type="date" data-testid="voids-date" />
        <button class="btn small" @click="shift(1)">›</button>
      </div>
    </div>
    <div v-if="error" class="banner bad">{{ error }}</div>
    <template v-if="log">
      <div class="kpis" data-testid="voids-kpis">
        <div class="card kpi"><span>{{ t('vd.log.count') }}</span><b>{{ log.voids }}</b><small>{{ log.void_qty }}</small></div>
        <div class="card kpi"><span>{{ t('vd.log.amount') }}</span><b>{{ m(log.void_amount) }}</b></div>
        <div class="card kpi warn"><span>{{ t('vd.log.wasted') }}</span><b>{{ m(log.wasted_amount) }}</b></div>
        <div class="card kpi"><span>{{ t('vd.log.refund') }}</span><b>{{ m(log.refund_amount) }}</b><small>{{ t('vd.log.refund_n', { n: log.refund_orders }) }}</small></div>
      </div>
      <div v-if="!log.rows.length" class="card muted" style="margin-top:12px">{{ t('vd.log.none') }}</div>
      <template v-else>
        <div class="sums">
          <div v-for="g in ([['by_reason', log.by_reason], ['by_staff', log.by_staff], ['by_dish', log.by_dish]] as const)" :key="g[0]" class="card">
            <h3>{{ t('vd.log.' + g[0]) }}</h3>
            <div v-for="x in g[1]" :key="x.key" class="srow"><span class="grow">{{ x.key || '—' }}</span>
              <span class="muted small">{{ t('vd.log.times', { n: x.count }) }}<template v-if="x.approved"> · 🔒{{ x.approved }}</template></span><b>{{ m(x.amount) }}</b></div>
          </div>
        </div>
        <div class="card" style="margin-top:12px">
          <h3>{{ t('vd.log.rows') }}</h3>
          <div class="tablewrap">
            <table class="rows" data-testid="voids-rows">
              <thead><tr><th>{{ t('vd.log.time') }}</th><th>{{ t('vd.log.order') }}</th><th>{{ t('vd.log.dish') }}</th><th>{{ t('vd.log.qty') }}</th><th>{{ t('vd.log.money') }}</th>
                <th>{{ t('vd.log.reason') }}</th><th>{{ t('vd.log.stage') }}</th><th>{{ t('vd.log.who') }}</th><th>{{ t('vd.log.approved') }}</th><th>{{ t('vd.log.ack') }}</th></tr></thead>
              <tbody>
                <tr v-for="r in log.rows" :key="r.kind + r.id" :class="{ wasted: r.wasted, refund: r.kind === 'refund' }">
                  <td>{{ clock(r.at) }}</td>
                  <td><router-link :to="{ path: '/merchant/orders', query: { order: r.order_id } }">{{ where(r) }}</router-link></td>
                  <td>{{ r.name }}</td><td>{{ r.qty || '' }}</td><td class="num">{{ m(r.amount) }}</td>
                  <td>{{ r.reason }} <small class="muted">{{ t('vd.kind.' + r.reason_kind) }}</small></td>
                  <td><template v-if="r.kind === 'refund'">{{ t('vd.log.refund_row') }} · {{ r.method }}</template><template v-else>{{ t('vd.stage.' + r.was_status) }}</template></td>
                  <td>{{ r.by }}</td><td>{{ r.approved_by }}</td>
                  <td>{{ r.ack_at ? `${clock(r.ack_at)} ${r.ack_by}` : '' }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </template>
    </template>
  </div>
</template>

<style scoped>
.kpis { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 10px; }
.kpi { display: flex; flex-direction: column; gap: 4px; }
.kpi span { color: var(--muted); font-size: 13px; }
.kpi b { font-size: 22px; font-variant-numeric: tabular-nums; }
.kpi small { color: var(--muted); }
.kpi.warn b { color: #a85b00; }
.sums { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 10px; margin-top: 12px; }
.sums h3, .card h3 { margin: 0 0 8px; font-size: 15px; }
.srow { display: flex; align-items: baseline; gap: 8px; padding: 4px 0; border-top: 1px dashed var(--line); }
.srow .grow { flex: 1; min-width: 0; overflow-wrap: anywhere; }
.tablewrap { overflow-x: auto; }
.rows { width: 100%; border-collapse: collapse; font-size: 13.5px; }
.rows th, .rows td { text-align: start; padding: 6px 8px; border-bottom: 1px solid var(--line); white-space: nowrap; }
.rows td.num { font-variant-numeric: tabular-nums; }
.rows tr.wasted td:first-child { box-shadow: inset 3px 0 #d97706; }
.rows tr.refund { background: #f5f8ff; }
@media (max-width: 767px) {
  .kpis { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .sums { grid-template-columns: 1fr; }
}
</style>
