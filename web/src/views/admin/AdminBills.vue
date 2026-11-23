<script setup lang="ts">
// The platform's bill page (docs/COUPON_DESIGN.md §9): each store's completed
// orders and discounts of a period, and the monthly statements of what the
// platform owes stores for its coupons, which are marked paid here.
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { get, post } from '../../api'
import { money, t } from '../../i18n'
import { setAdminView } from '../../session'
import type { BillOrder, Settlement } from '../../types'
import { addDays, addMonths, dayOf, monthEnd, monthStart } from '../staff/time'
import '../../bills'

interface StoreBill { branch_id: number; name: string; slug: string; currency: string; orders: number; food: number
  merchant_discount: number; platform_discount: number; paid: number }

const route = useRoute(), router = useRouter()
const tab = computed(() => route.query.tab === 'settlements' ? 'settlements' : 'stores')
const setTab = (k: string) => router.replace({ query: k === 'settlements' ? { tab: k } : {} })
const today = dayOf(Date.now(), 'Asia/Manila')
type Preset = 'today' | 'week' | 'month' | 'last_month' | 'custom'
const preset = ref<Preset>('month')
const from = ref(monthStart(today)), to = ref(today)
function choose(p: Preset) {
  preset.value = p
  if (p === 'today') { from.value = to.value = today }
  else if (p === 'week') { from.value = addDays(today, -6); to.value = today }
  else if (p === 'month') { from.value = monthStart(today); to.value = today }
  else if (p === 'last_month') { from.value = addMonths(today, -1); to.value = monthEnd(addMonths(today, -1)) }
}
const stores = ref<StoreBill[]>([])
const settlements = ref<Settlement[]>([])
const status = ref<'' | 'pending' | 'paid'>('pending')
const error = ref(''), note = ref('')
const orders = ref<Record<number, BillOrder[]>>({})

async function load() {
  error.value = ''
  try {
    if (tab.value === 'stores') stores.value = (await get<{ stores: StoreBill[] }>(`/admin/bills?from=${from.value}&to=${to.value}`)).stores
    else settlements.value = (await get<{ settlements: Settlement[] }>(`/admin/settlements?status=${status.value}`)).settlements
  } catch (e) { error.value = (e as Error).message }
}
onMounted(load)
watch([from, to, tab, status], load)

// sums per currency: amounts of different money do not add up
const totals = computed(() => {
  const m: Record<string, StoreBill> = {}
  for (const s of stores.value) {
    const x = m[s.currency] ??= { branch_id: 0, name: '', slug: '', currency: s.currency, orders: 0, food: 0, merchant_discount: 0, platform_discount: 0, paid: 0 }
    x.orders += s.orders; x.food += s.food; x.merchant_discount += s.merchant_discount; x.platform_discount += s.platform_discount; x.paid += s.paid
  }
  return Object.values(m)
})
const pending = computed(() => {
  const m: Record<string, number> = {}
  for (const s of settlements.value) if (s.status === 'pending') m[s.currency] = (m[s.currency] ?? 0) + s.amount
  return Object.entries(m)
})
function viewStore(s: StoreBill) { setAdminView({ id: s.branch_id, name: s.name, slug: s.slug, currency: s.currency }); location.href = '/merchant/bills' }

const paying = ref<Settlement | null>(null)
const f = reactive({ paid_on: today, transfer_ref: '' })
async function markPaid() {
  const s = paying.value!
  error.value = ''; note.value = ''
  try {
    await post(`/admin/settlements/${s.id}/paid`, { paid_on: f.paid_on, transfer_ref: f.transfer_ref })
    paying.value = null; f.transfer_ref = ''; await load()
  } catch (e) { error.value = (e as Error).message }
}
async function showOrders(s: Settlement) {
  if (orders.value[s.id]) { delete orders.value[s.id]; return }
  try { orders.value[s.id] = (await get<{ orders: BillOrder[] }>(`/admin/settlements/${s.id}/orders`)).orders } catch (e) { error.value = (e as Error).message }
}
</script>

<template>
  <div class="adm-page" data-testid="admin-bills">
    <div class="subtabs" role="tablist">
      <button type="button" role="tab" :class="{ on: tab === 'stores' }" data-testid="abl-tab-stores" @click="setTab('stores')">{{ t('abl.stores') }}</button>
      <button type="button" role="tab" :class="{ on: tab === 'settlements' }" data-testid="abl-tab-settle" @click="setTab('settlements')">{{ t('abl.settle') }}</button>
    </div>
    <div v-if="error" class="banner bad" data-testid="abl-error">{{ error }}</div>

    <template v-if="tab === 'stores'">
      <div class="row wrap" style="gap:6px;margin:10px 0">
        <button v-for="p in (['today', 'week', 'month', 'last_month', 'custom'] as Preset[])" :key="p" class="btn small" :class="{ primary: preset === p }"
          @click="p === 'custom' ? preset = 'custom' : choose(p)">{{ t('bl.p.' + p) }}</button>
        <span v-if="preset === 'custom'"><input v-model="from" type="date" :max="to" style="width:auto"> ~ <input v-model="to" type="date" :min="from" style="width:auto"></span>
      </div>
      <div class="card" style="padding:0;overflow:auto">
        <table class="data" data-testid="abl-stores">
          <thead><tr><th>{{ t('abl.store') }}</th><th class="r">{{ t('bl.c.orders') }}</th><th class="r">{{ t('bl.food') }}</th><th class="r">{{ t('abl.mdisc') }}</th>
            <th class="r">{{ t('abl.pdisc') }}</th><th class="r">{{ t('bl.paid') }}</th><th></th></tr></thead>
          <tbody>
            <tr v-for="s in stores" :key="s.branch_id + s.currency" data-testid="abl-store">
              <td><b>{{ s.name }}</b><div class="small muted">{{ s.slug }} · {{ s.currency }}</div></td>
              <td class="r">{{ s.orders }}</td><td class="r">{{ money(s.food, s.currency) }}</td><td class="r">{{ money(s.merchant_discount, s.currency) }}</td>
              <td class="r plat">{{ money(s.platform_discount, s.currency) }}</td><td class="r">{{ money(s.paid, s.currency) }}</td>
              <td class="r"><button class="btn small" @click="viewStore(s)">{{ t('abl.open_store') }} ↗</button></td>
            </tr>
            <tr v-if="!stores.length"><td colspan="7" class="muted">{{ t('abl.none') }}</td></tr>
          </tbody>
          <tfoot><tr v-for="x in totals" :key="x.currency"><td><b>{{ t('bl.k.total') }} · {{ x.currency }}</b></td><td class="r">{{ x.orders }}</td>
            <td class="r">{{ money(x.food, x.currency) }}</td><td class="r">{{ money(x.merchant_discount, x.currency) }}</td>
            <td class="r plat">{{ money(x.platform_discount, x.currency) }}</td><td class="r">{{ money(x.paid, x.currency) }}</td><td></td></tr></tfoot>
        </table>
      </div>
    </template>

    <template v-else>
      <div class="row wrap between" style="gap:8px;margin:10px 0">
        <div class="row" style="gap:6px">
          <button v-for="s in (['pending', 'paid', ''] as const)" :key="s" class="btn small" :class="{ primary: status === s }" :data-status="s" @click="status = s">
            {{ s ? t('bl.s.' + s) : t('abl.f.all') }}</button>
        </div>
        <div v-if="pending.length" class="small" data-testid="abl-pending">{{ t('abl.pending_sum') }}：<b v-for="[c, v] in pending" :key="c" style="margin-inline-start:6px">{{ money(v, c) }}</b></div>
      </div>
      <p class="muted small">{{ t('bl.settle_hint') }}</p>
      <div class="card" style="padding:0;overflow:auto">
        <table class="data" data-testid="abl-settlements">
          <thead><tr><th>{{ t('bl.month') }}</th><th>{{ t('abl.store') }}</th><th class="r">{{ t('bl.c.orders') }}</th><th class="r">{{ t('bl.amount') }}</th>
            <th>{{ t('bl.state') }}</th><th></th></tr></thead>
          <tbody>
            <template v-for="s in settlements" :key="s.id">
              <tr data-testid="abl-settlement">
                <td>{{ s.month }}</td><td><b>{{ s.store_name }}</b><div class="small muted">{{ s.store_slug }}</div></td>
                <td class="r">{{ s.orders }}</td><td class="r"><b>{{ money(s.amount, s.currency) }}</b></td>
                <td><span class="tag" :class="{ ok: s.status === 'paid' }">{{ t('bl.s.' + s.status) }}</span>
                  <div v-if="s.status === 'paid'" class="small muted">{{ t('bl.paid_on', { d: s.paid_on ?? '', ref: s.transfer_ref }) }}</div></td>
                <td class="r nowrap"><button class="btn small" @click="showOrders(s)">{{ t('bl.view_orders') }}</button>
                  <button v-if="s.status === 'pending'" class="btn small primary" data-testid="abl-mark" @click="paying = s">{{ t('abl.mark') }}</button></td>
              </tr>
              <tr v-if="paying?.id === s.id"><td colspan="6">
                <form class="row wrap pay" style="gap:8px" data-testid="abl-pay-form" @submit.prevent="markPaid">
                  <label class="field"><span>{{ t('abl.paid_on') }}</span><input v-model="f.paid_on" type="date" required data-testid="abl-paid-on" /></label>
                  <label class="field"><span>{{ t('abl.ref') }}</span><input v-model="f.transfer_ref" maxlength="100" required data-testid="abl-ref" /></label>
                  <button class="btn primary" data-testid="abl-pay">{{ t('save') }}</button><button type="button" class="btn" @click="paying = null">{{ t('cancel') }}</button>
                </form></td></tr>
              <tr v-if="orders[s.id]"><td colspan="6">
                <div v-for="o in orders[s.id]" :key="o.id" class="small row between"><span>{{ o.public_no }} · {{ dayOf(o.created_at, 'Asia/Manila') }}</span>
                  <span>{{ money(o.platform_free, s.currency) }}</span></div></td></tr>
            </template>
            <tr v-if="!settlements.length"><td colspan="6" class="muted">{{ t('bl.settle_none') }}</td></tr>
          </tbody>
        </table>
      </div>
    </template>
  </div>
</template>

<style scoped>
.r { text-align: end; white-space: nowrap; font-variant-numeric: tabular-nums; }
.plat { color: #6d28d9; }
.nowrap { white-space: nowrap; } .nowrap .btn { margin-inline-start: 6px; }
.pay { align-items: flex-end; } .pay .field { margin: 0; }
</style>
