<script setup lang="ts">
// The store's page of store balances (docs/REFUND_BALANCE_DESIGN.md §8.2):
// what customers hold here, every change, the patterns worth a look, the
// owner's limits, and taking back a refund the store then paid in cash.
import { computed, onMounted, reactive, ref } from 'vue'
import { branchCredit, creditErr, revokeCredit, saveCreditLimits, type BranchCredit, type CreditAlert } from '../credit'
import { money, t, toMajor, toMinor, whenText } from '../i18n'
import { currentRole, session } from '../session'
import '../officeStrings'
import '../stored'
import StoredPanel from './marketing/StoredPanel.vue'

const data = ref<BranchCredit | null>(null)
const error = ref('')
const notice = ref('')
const tab = ref<'entries' | 'holders' | 'stored'>('entries')
const day = (d: Date) => d.toISOString().slice(0, 10)
const f = reactive({ from: day(new Date(Date.now() - 29 * 864e5)), to: day(new Date()) })
const lim = reactive({ over: '', daily: '' })
const isOwner = computed(() => currentRole()?.role === 'owner')
const cur = computed(() => data.value?.currency)
const busy = ref(0)

async function load() {
  error.value = ''
  try {
    data.value = await branchCredit(session.branchId!, { from: f.from, to: f.to })
    lim.over = String(toMajor(data.value.approve_over)); lim.daily = String(toMajor(data.value.daily_limit))
  } catch (e) { error.value = creditErr(e) }
}
onMounted(load)

async function revoke(refundID: number) {
  if (!confirm(t('sc.p.revoke_ask'))) return
  busy.value = refundID; error.value = ''
  try { await revokeCredit(refundID); notice.value = t('sc.p.revoked_ok'); await load() } catch (e) { error.value = creditErr(e) }
  busy.value = 0
}
async function saveLimits() {
  error.value = ''
  try { await saveCreditLimits(session.branchId!, toMinor(lim.over), toMinor(lim.daily)); notice.value = t('sc.p.saved') } catch (e) { error.value = creditErr(e) }
}
function alertText(a: CreditAlert) {
  return t('sc.p.a.' + a.kind, { p: a.phone ?? '', n: a.count ?? 0, no: a.public_no ?? '', by: a.by ?? '', s: money(a.amount ?? 0, cur.value) })
}
</script>

<template>
  <div class="ox warm">
  <div class="page ox-page" data-testid="credit-page">
    <div class="ox-head">
      <div><h1>{{ t('sc.p.title') }}</h1><p class="ox-sub">{{ t('sc.p.hint') }}</p></div>
      <div class="ox-acts">
        <label class="field inline"><span>{{ t('sc.p.from') }}</span><input v-model="f.from" type="date" @change="load" /></label>
        <label class="field inline"><span>{{ t('sc.p.to') }}</span><input v-model="f.to" type="date" @change="load" /></label>
      </div>
    </div>
    <div v-if="error" class="banner bad">{{ error }}</div>
    <div v-if="notice" class="banner ok" @click="notice = ''">{{ notice }}</div>

    <div v-if="data" class="ox-stats">
      <div class="ox-stat"><span class="l">{{ t('sc.p.owed') }}</span><b class="v" data-testid="credit-owed">{{ money(data.owed, cur) }}</b>
        <span class="s">{{ t('sc.p.customers', { n: data.customers }) }}</span></div>
      <div class="ox-stat"><span class="l">{{ t('sc.p.in') }}</span><b class="v">{{ money(data.in, cur) }}</b></div>
      <div class="ox-stat"><span class="l">{{ t('sc.p.used') }}</span><b class="v">{{ money(data.used, cur) }}</b></div>
      <div class="ox-stat"><span class="l">{{ t('sc.p.revoked') }}</span><b class="v">{{ money(data.revoked, cur) }}</b></div>
    </div>

    <div v-if="data?.alerts.length" class="banner warn" data-testid="credit-alerts">
      <b>{{ t('sc.p.alerts') }}</b>
      <div v-for="(a, i) in data.alerts" :key="i" class="small">· {{ alertText(a) }}</div>
    </div>

    <div v-if="data" class="ox-body">
      <aside class="ox-rail">
        <div class="ox-rail-list">
          <button class="ox-cat" :class="{ on: tab === 'entries' }" @click="tab = 'entries'"><span class="nm">{{ t('sc.p.entries') }}</span><span class="n">{{ data.entries.length }}</span></button>
          <button class="ox-cat" :class="{ on: tab === 'holders' }" @click="tab = 'holders'"><span class="nm">{{ t('sc.p.holders') }}</span><span class="n">{{ data.holders.length }}</span></button>
          <button class="ox-cat" :class="{ on: tab === 'stored' }" data-testid="credit-tab-stored" @click="tab = 'stored'"><span class="nm">{{ t('sv.p.tab') }}</span></button>
        </div>
        <div class="limits">
          <div class="ox-rail-h">{{ t('sc.p.limits') }}</div>
          <label class="field"><span>{{ t('sc.p.approve_over') }}</span><input v-model="lim.over" inputmode="decimal" :disabled="!isOwner" data-testid="credit-over" /></label>
          <label class="field"><span>{{ t('sc.p.daily_limit') }}</span><input v-model="lim.daily" inputmode="decimal" :disabled="!isOwner" data-testid="credit-daily" /></label>
          <button v-if="isOwner" class="btn primary" data-testid="credit-limits-save" @click="saveLimits">{{ t('sc.p.save') }}</button>
        </div>
      </aside>
      <section class="ox-panel">
        <template v-if="tab === 'entries'">
          <div v-if="!data.entries.length" class="ox-empty">{{ t('sc.p.none') }}</div>
          <div v-else class="ox-scroll">
            <table class="data">
              <thead><tr><th></th><th>{{ t('sc.p.customer') }}</th><th>{{ t('order_no') }}</th><th>{{ t('sc.p.staff') }}</th><th class="num">{{ t('vd.r.amount') }}</th><th class="num">{{ t('sc.p.balance') }}</th><th></th></tr></thead>
              <tbody>
                <tr v-for="e in data.entries" :key="e.id" data-testid="credit-row">
                  <td><b>{{ t('sc.k.' + e.kind) }}</b><div class="muted small">{{ whenText(e.at) }}</div></td>
                  <td>{{ e.phone }}</td>
                  <td><span class="ox-code">{{ e.public_no }}</span><div v-if="e.brand_name" class="muted small">{{ e.brand_name }}</div></td>
                  <td>{{ e.by }}<div v-if="e.note" class="muted small">{{ e.note }}</div></td>
                  <td class="num"><span class="ox-money" :class="e.amount > 0 ? 'in' : 'out'">{{ e.amount > 0 ? '+' : '−' }}{{ money(Math.abs(e.amount), cur) }}</span></td>
                  <td class="num"><span class="ox-money">{{ money(e.balance, cur) }}</span></td>
                  <td class="right"><button v-if="e.revocable" class="btn small" :disabled="busy === e.refund_id" data-testid="credit-revoke" @click="revoke(e.refund_id)">{{ t('sc.p.revoke') }}</button></td>
                </tr>
              </tbody>
            </table>
          </div>
        </template>
        <StoredPanel v-else-if="tab === 'stored'" />
        <template v-else>
          <div v-if="!data.holders.length" class="ox-empty">{{ t('sc.p.none') }}</div>
          <div v-else class="ox-scroll">
            <table class="data">
              <thead><tr><th>{{ t('sc.p.customer') }}</th><th class="num">{{ t('sc.p.balance') }}</th><th></th></tr></thead>
              <tbody>
                <tr v-for="h in data.holders" :key="h.customer_id" data-testid="credit-holder">
                  <td>{{ h.phone }}<div class="muted small">{{ whenText(h.since) }}</div></td>
                  <td class="num"><span class="ox-money">{{ money(h.balance, cur) }}</span></td>
                  <td class="right"><span v-if="h.frozen" class="tag">{{ t('sc.p.frozen') }}</span></td>
                </tr>
              </tbody>
            </table>
          </div>
        </template>
      </section>
    </div>
  </div>
  </div>
</template>

<style scoped>
.limits { display: flex; flex-direction: column; gap: 8px; margin-top: 14px; padding: 0 4px; }
.field.inline { display: inline-flex; align-items: center; gap: 6px; margin: 0; }
.ox-money.in { color: var(--brand-dark, #0a7); }
</style>
