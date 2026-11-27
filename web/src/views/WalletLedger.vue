<script setup lang="ts">
// The balance ledger (docs/BALANCE_CHECK.md §5), for the store's page of
// balances (branch set) and the platform (branch null).
import { onMounted, reactive, ref } from 'vue'
import { accountKinds, journalKinds, ledgerCSV, loadLedger, type LedgerRow, type LedgerTotal } from '../walletLedger'
import { money, t, whenText } from '../i18n'

const props = defineProps<{ branch: number | null }>()
const rows = ref<LedgerRow[]>([])
const totals = ref<LedgerTotal[]>([])
const more = ref(false)
const error = ref('')
const loading = ref(false)
const day = (d: Date) => d.toISOString().slice(0, 10)
const f = reactive({ phone: '', order: '', kind: '', account: '', store: '', platform: false,
  from: day(new Date(Date.now() - 30 * 864e5)), to: day(new Date(Date.now() + 864e5)) })

function query(before = 0) {
  const q: Record<string, string | number | boolean> = { phone: f.phone.trim(), order: f.order.trim(), kind: f.kind, account: f.account,
    from: f.from, to: f.to, before }
  if (props.branch === null) { q.branch = f.store.trim(); q.platform = f.platform }
  return q
}
async function load() {
  error.value = ''; loading.value = true
  try { const l = await loadLedger(props.branch, query()); rows.value = l.rows; totals.value = l.totals; more.value = l.more }
  catch (e) { error.value = (e as Error).message }
  loading.value = false
}
async function older() {
  const last = rows.value[rows.value.length - 1]
  if (!last) return
  try { const l = await loadLedger(props.branch, query(last.posting_id)); rows.value.push(...l.rows); more.value = l.more }
  catch (e) { error.value = (e as Error).message }
}
onMounted(load)

function ref_(r: LedgerRow): string[] {
  const out: string[] = []
  if (r.public_no || r.order_id) out.push(t('wl.ref.order', { n: r.public_no || String(r.order_id) }))
  if (r.refund_id) out.push(t('wl.ref.refund', { n: r.refund_id }))
  if (r.topup_id) out.push(t('wl.ref.topup', { n: r.topup_id }))
  if (r.cashout_id) out.push(t('wl.ref.cashout', { n: r.cashout_id }))
  if (r.reverses) out.push(t('wl.ref.reverses', { n: r.reverses }))
  return out
}
function csv() {
  const blob = new Blob([ledgerCSV(rows.value, k => t(k))], { type: 'text/csv;charset=utf-8' })
  const a = document.createElement('a')
  a.href = URL.createObjectURL(blob)
  a.download = `balance-ledger-${f.from}-${f.to}.csv`
  a.click()
  URL.revokeObjectURL(a.href)
}
</script>

<template>
  <div class="stack" data-testid="wallet-ledger">
    <p class="muted small">{{ t('wl.hint') }}</p>
    <div class="filters">
      <label class="field"><span>{{ t('wl.f.phone') }}</span><input v-model="f.phone" data-testid="wl-phone" @keyup.enter="load" /></label>
      <label class="field"><span>{{ t('wl.f.order') }}</span><input v-model="f.order" data-testid="wl-order" @keyup.enter="load" /></label>
      <label class="field"><span>{{ t('wl.f.kind') }}</span>
        <select v-model="f.kind" data-testid="wl-kind"><option value="">{{ t('wl.f.all') }}</option>
          <option v-for="k in journalKinds" :key="k" :value="k">{{ t('wl.k.' + k) }}</option></select></label>
      <label class="field"><span>{{ t('wl.f.account') }}</span>
        <select v-model="f.account"><option value="">{{ t('wl.f.all') }}</option>
          <option v-for="k in accountKinds" :key="k" :value="k">{{ t('wl.a.' + k) }}</option></select></label>
      <template v-if="branch === null">
        <label class="field"><span>{{ t('wl.f.store') }}</span><input v-model="f.store" inputmode="numeric" style="width:90px" /></label>
        <label class="field check"><input v-model="f.platform" type="checkbox" /><span>{{ t('wl.f.platform') }}</span></label>
      </template>
      <label class="field"><span>{{ t('wl.f.from') }}</span><input v-model="f.from" type="date" /></label>
      <label class="field"><span>{{ t('wl.f.to') }}</span><input v-model="f.to" type="date" /></label>
      <div class="acts"><button class="btn primary" :disabled="loading" data-testid="wl-search" @click="load">{{ t('wl.search') }}</button>
        <button class="btn" :disabled="!rows.length" @click="csv">{{ t('wl.csv') }}</button></div>
    </div>
    <div v-if="error" class="banner bad">{{ error }}</div>

    <div v-if="totals.length" class="totals">
      <div class="muted small">{{ t('wl.totals') }}</div>
      <div class="chips">
        <span v-for="x in totals" :key="x.currency + x.account_kind" class="chip" data-testid="wl-total">
          <b>{{ t('wl.a.' + x.account_kind) }}</b> {{ t('wl.t.in') }} {{ money(x.in, x.currency) }} · {{ t('wl.t.out') }} {{ money(x.out, x.currency) }}
        </span>
      </div>
    </div>

    <div v-if="!rows.length && !loading" class="muted">{{ t('wl.none') }}</div>
    <div v-else class="scroll">
      <table class="data">
        <thead><tr><th>{{ t('wl.c.time') }}</th><th>{{ t('wl.c.what') }}</th><th>{{ t('wl.c.account') }}</th>
          <th class="num">{{ t('wl.c.amount') }}</th><th class="num">{{ t('wl.c.balance') }}</th><th>{{ t('wl.c.other') }}</th>
          <th>{{ t('wl.c.ref') }}</th><th>{{ t('wl.c.by') }}</th></tr></thead>
        <tbody>
          <tr v-for="r in rows" :key="r.posting_id" data-testid="wl-row">
            <td class="nowrap">{{ whenText(r.at) }}<div class="muted small">#{{ r.journal_id }}</div></td>
            <td><b>{{ t('wl.k.' + r.kind) }}</b><div v-if="r.note" class="muted small">{{ r.note }}</div></td>
            <td>{{ t('wl.a.' + r.account_kind) }}<div class="muted small">{{ r.phone }}<template v-if="branch === null && r.store_name"> · {{ r.store_name }}</template></div></td>
            <td class="num"><span class="amt" :class="r.amount > 0 ? 'in' : 'out'">{{ r.amount > 0 ? '+' : '−' }}{{ money(Math.abs(r.amount), r.currency) }}</span></td>
            <td class="num">{{ money(r.balance, r.currency) }}<div class="muted small">№{{ r.seq }}</div></td>
            <td class="small"><div v-for="(c, i) in r.counter" :key="i">{{ t('wl.a.' + c.account_kind) }} {{ c.amount > 0 ? '+' : '−' }}{{ money(Math.abs(c.amount), r.currency) }}</div></td>
            <td class="small"><div v-for="x in ref_(r)" :key="x">{{ x }}</div></td>
            <td class="small">{{ r.by || (r.actor_type === 'system' ? t('wl.system') : r.actor_type) }}
              <div v-if="r.approved_by" class="muted">{{ t('wl.approved', { n: r.approved_by }) }}</div></td>
          </tr>
        </tbody>
      </table>
    </div>
    <button v-if="more" class="btn" data-testid="wl-more" @click="older">{{ t('wl.more') }}</button>
  </div>
</template>

<style scoped>
.filters { display: flex; flex-wrap: wrap; gap: 8px 12px; align-items: flex-end; }
.filters .field { margin: 0; }
.filters .field input:not([type=checkbox]), .filters .field select { min-width: 0; max-width: 180px; }
.filters .check { flex-direction: row; align-items: center; gap: 6px; }
.acts { display: flex; gap: 8px; }
.chips { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 4px; }
.chip { font-size: 12px; padding: 4px 8px; border-radius: 8px; background: var(--soft, #f3f3f3); }
.scroll { overflow-x: auto; }
.nowrap { white-space: nowrap; }
.amt.in { color: var(--ok, #0a7a3a); }
.amt.out { color: var(--bad, #b3261e); }
</style>
