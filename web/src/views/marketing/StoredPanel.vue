<script setup lang="ts">
// 财务 → 店铺余额 → 储值 (docs/STORED_VALUE_DESIGN.md §8.3, §10): what each
// customer holds in principal and bonus, every change, and paying stored
// value back. For the customer's own reasons the bonus used is kept back;
// for the store's, all the principal left is paid.
import { onMounted, reactive, ref, watch } from 'vue'
import { ApiError } from '../../api'
import { money, t, whenText } from '../../i18n'
import { can, session } from '../../session'
import ApproveDialog from '../ApproveDialog.vue'
import { branchStored, cashoutQuote, storedCashout, storedErr, type BranchStored, type CashoutQuote, type StoredHolder } from '../../stored'

const bid = session.branchId!
const data = ref<BranchStored | null>(null)
const view = ref<'holders' | 'entries'>('holders')
const error = ref(''), notice = ref('')
const m = (v: number) => money(v, data.value?.currency)
async function load() {
  try { data.value = await branchStored(bid) } catch (e) { error.value = storedErr(e) }
}
onMounted(load)

// paying back
const who = ref<StoredHolder | null>(null)
const q = ref<CashoutQuote | null>(null)
const f = reactive({ reason: 'customer' as 'customer' | 'store', pay_type: 'cash', consent: 'in_person', note: '' })
const busy = ref(false), approving = ref(false), qErr = ref('')
async function quote() {
  if (!who.value) return
  qErr.value = ''
  try { q.value = await cashoutQuote(bid, who.value.customer_id, f.reason) } catch (e) { qErr.value = storedErr(e) }
}
function open(h: StoredHolder) { who.value = h; q.value = null; Object.assign(f, { reason: 'customer', pay_type: 'cash', consent: 'in_person', note: '' }); void quote() }
watch(() => f.reason, quote)
async function go(headers?: Record<string, string>) {
  if (!who.value || !q.value) return
  busy.value = true; qErr.value = ''
  try {
    const r = await storedCashout(bid, who.value.customer_id, { ...f, expect: q.value.paid_out }, headers)
    notice.value = t('sv.o.done', { amt: m(r.paid_out) }); who.value = null; await load()
  } catch (e) {
    if (e instanceof ApiError && (e.code === 'STORED_CASHOUT_OWNER' || e.code === 'FORBIDDEN') && !headers) approving.value = true
    else qErr.value = storedErr(e)
  } finally { busy.value = false }
}
function start() { if (can('stored.cashout')) void go(); else approving.value = true }
</script>

<template>
  <div class="sp" data-testid="stored-panel">
    <div v-if="error" class="banner bad">{{ error }}</div>
    <div v-if="notice" class="banner ok" @click="notice = ''" data-testid="sp-notice">{{ notice }}</div>
    <template v-if="data">
      <div class="sp-sum">
        <span><b>{{ m(data.principal + data.bonus) }}</b> {{ t('sv.m.parts', { p: m(data.principal), b: m(data.bonus) }) }}</span>
        <span class="muted small">{{ t('sv.p.month', { n: data.month_cashouts, amt: m(data.month_paid_out) }) }}</span>
      </div>
      <div class="ox-views" style="margin-bottom:10px">
        <button :class="{ on: view === 'holders' }" @click="view = 'holders'">{{ t('sc.p.holders') }}</button>
        <button :class="{ on: view === 'entries' }" @click="view = 'entries'">{{ t('sc.p.entries') }}</button>
      </div>
      <div v-if="view === 'holders'" class="ox-scroll">
        <div v-if="!data.holders.length" class="ox-empty">{{ t('sc.p.none') }}</div>
        <table v-else class="data">
          <thead><tr><th>{{ t('sc.p.customer') }}</th><th class="num">{{ t('sv.p.principal') }}</th><th class="num">{{ t('sv.p.bonus') }}</th><th>{{ t('sv.p.last') }}</th><th></th></tr></thead>
          <tbody>
            <tr v-for="h in data.holders" :key="h.customer_id" data-testid="sp-holder">
              <td>{{ h.phone }} <span v-if="h.frozen" class="tag">{{ t('sc.p.frozen') }}</span></td>
              <td class="num"><span class="ox-money">{{ m(h.principal) }}</span></td><td class="num">{{ m(h.bonus) }}</td>
              <td class="small">{{ h.last_topup ? whenText(h.last_topup) : '' }}</td>
              <td class="right"><button v-if="h.principal + h.bonus > 0 && !h.frozen" class="btn small" data-testid="sp-cashout" @click="open(h)">
                {{ !can('stored.cashout') ? '🔒 ' : '' }}{{ t('sv.p.cashout') }}</button></td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-else class="ox-scroll">
        <div v-if="!data.entries.length" class="ox-empty">{{ t('sc.p.none') }}</div>
        <table v-else class="data">
          <thead><tr><th></th><th>{{ t('sc.p.customer') }}</th><th>{{ t('order_no') }}</th><th>{{ t('sc.p.staff') }}</th><th class="num">{{ t('sv.p.principal') }}</th><th class="num">{{ t('sv.p.bonus') }}</th></tr></thead>
          <tbody>
            <tr v-for="e in data.entries" :key="e.id" data-testid="sp-entry">
              <td><b>{{ t('sv.k.' + e.kind) }}</b><div class="muted small">{{ whenText(e.at) }}</div></td>
              <td>{{ e.phone }}</td><td><span class="ox-code">{{ e.public_no }}</span></td>
              <td>{{ e.by }}<div v-if="e.note" class="muted small">{{ e.note }}</div></td>
              <td class="num"><span class="ox-money" :class="e.principal > 0 ? 'in' : ''">{{ e.principal > 0 ? '+' : e.principal < 0 ? '−' : '' }}{{ m(Math.abs(e.principal)) }}</span></td>
              <td class="num">{{ e.bonus > 0 ? '+' : e.bonus < 0 ? '−' : '' }}{{ m(Math.abs(e.bonus)) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>

    <div v-if="who" class="vmask" @click.self="who = null">
      <div class="vsheet" data-testid="cashout-sheet">
        <div class="vh"><h3>{{ t('sv.p.cashout') }} · {{ who.phone }}</h3><button class="x" @click="who = null">✕</button></div>
        <div class="vb">
          <div class="lbl">{{ t('sv.o.reason') }}</div>
          <div class="chs">
            <button v-for="r in (['customer', 'store'] as const)" :key="r" class="ch" :class="{ on: f.reason === r }" :data-testid="'co-r-' + r" @click="f.reason = r">{{ t('sv.o.r.' + r) }}</button>
          </div>
          <div class="small muted">{{ t('sv.o.hint_' + f.reason) }}</div>
          <table v-if="q" class="calc">
            <tbody>
              <tr><td>{{ t('sv.o.left') }}</td><td class="num">{{ m(q.principal) }}</td></tr>
              <tr v-if="q.clawback"><td>{{ t('sv.o.used') }}</td><td class="num">−{{ m(q.clawback) }}</td></tr>
              <tr><td class="muted">{{ t('sv.o.bonus_left') }}</td><td class="num muted">{{ m(q.bonus) }}</td></tr>
              <tr class="sum"><td>{{ t('sv.o.pay') }}</td><td class="num" data-testid="co-pay">{{ m(q.paid_out) }}</td></tr>
            </tbody>
          </table>
          <div class="lbl">{{ t('sv.o.way') }}</div>
          <div class="chs"><button v-for="w in ['cash', 'transfer', 'online', 'other']" :key="w" class="ch" :class="{ on: f.pay_type === w }" @click="f.pay_type = w">{{ t('sv.o.w.' + w) }}</button></div>
          <div class="lbl">{{ t('sc.r.consent') }}</div>
          <div class="chs"><button v-for="c in ['in_person', 'phone', 'chat']" :key="c" class="ch" :class="{ on: f.consent === c }" @click="f.consent = c">{{ t('sc.r.c.' + c) }}</button></div>
          <label class="field" style="margin:0"><span>{{ t('sv.o.note') }}</span><input v-model="f.note" maxlength="200" /></label>
          <div v-if="qErr" class="banner bad small" data-testid="co-error">{{ qErr }}</div>
        </div>
        <div class="vf">
          <button class="btn" @click="who = null">{{ t('sv.e.cancel') }}</button>
          <button class="btn primary grow" :disabled="busy || !q" data-testid="co-go" @click="start">{{ !can('stored.cashout') ? '🔒 ' : '' }}{{ t('sv.o.go', { amt: m(q?.paid_out ?? 0) }) }}</button>
        </div>
      </div>
    </div>
    <ApproveDialog v-if="approving" :branch-id="bid" perm="stored.cashout" :what="t('sv.o.what')"
      @approved="tok => { approving = false; go({ 'X-Approval': tok }) }" @close="approving = false" />
  </div>
</template>

<style scoped>
.sp-sum { display: flex; flex-wrap: wrap; gap: 6px 16px; align-items: baseline; margin-bottom: 10px; }
.sp-sum b { font-size: 18px; }
.ox-views button { border: 0; background: none; padding: 6px 12px; font: inherit; cursor: pointer; }
.ox-views button.on { background: var(--brand, #a02c20); color: #fff; }
.ox-money.in { color: var(--brand-dark, #0a7); }
.vmask { position: fixed; inset: 0; background: rgba(0,0,0,.4); z-index: 30; display: flex; align-items: center; justify-content: center; padding: 16px; }
.vsheet { background: var(--card, #fff); border-radius: 14px; width: min(480px, 100%); max-height: calc(100vh - 32px); display: flex; flex-direction: column; overflow: hidden; }
.vh { display: flex; align-items: center; gap: 8px; padding: 14px 16px 8px; }
.vh h3 { margin: 0; font-size: 17px; flex: 1; min-width: 0; overflow-wrap: anywhere; }
.x { background: none; border: 0; font-size: 18px; cursor: pointer; color: var(--muted); }
.vb { padding: 0 16px 12px; overflow-y: auto; display: flex; flex-direction: column; gap: 10px; }
.lbl { font-size: 13px; color: var(--muted); margin-top: 4px; }
.chs { display: flex; flex-wrap: wrap; gap: 8px; }
.ch { border: 1px solid var(--line); background: #fff; border-radius: 10px; padding: 7px 12px; cursor: pointer; font-size: 14px; }
.ch.on { border-color: var(--brand); background: var(--brand-soft); box-shadow: 0 0 0 1px var(--brand); }
.vf { display: flex; gap: 10px; padding: 12px 16px calc(12px + env(safe-area-inset-bottom)); border-top: 1px solid var(--line); }
.vf .btn { min-height: 44px; } .grow { flex: 1; min-width: 0; }
.calc { width: 100%; border-collapse: collapse; } .calc td { padding: 5px 2px; } .calc tr.sum td { border-top: 1px solid var(--line); font-weight: 700; }
.calc .num { text-align: end; font-variant-numeric: tabular-nums; }
</style>
