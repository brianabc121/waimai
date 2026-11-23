<script setup lang="ts">
// The store's bill (docs/COUPON_DESIGN.md §8): every delivery and pickup order
// of a period with what each discount took off, and a summary with the sums
// per kind of discount and the platform's coupon statements.
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { get } from '../api'
import { sourceName } from '../bills'
import { discountLabel, kindName } from '../coupons'
import { locale, money, t } from '../i18n'
import { session } from '../session'
import type { BillList, BillOrder, BillSummary } from '../types'
import { addDays, addMonths, clock, dayOf, monthEnd, monthStart } from './staff/time'

const route = useRoute(), router = useRouter()
const bid = session.branchId!
const tz = ref('Asia/Manila')
const today = computed(() => dayOf(Date.now(), tz.value))
type Preset = 'today' | 'yesterday' | 'week' | 'month' | 'last_month' | 'custom'
const preset = ref<Preset>('month')
const from = ref(''), to = ref(''), filter = ref('')
const tab = computed(() => route.query.tab === 'summary' ? 'summary' : 'orders')
const setTab = (k: string) => router.replace({ query: k === 'summary' ? { tab: k } : {} })
const list = ref<BillList | null>(null)
const sum = ref<BillSummary | null>(null)
const error = ref(''), open = ref<number | null>(null)
const settleOrders = ref<Record<number, BillOrder[]>>({})

function choose(p: Preset) {
  const d = today.value
  preset.value = p
  if (p === 'today') { from.value = to.value = d }
  else if (p === 'yesterday') { from.value = to.value = addDays(d, -1) }
  else if (p === 'week') { from.value = addDays(d, -6); to.value = d }
  else if (p === 'month') { from.value = monthStart(d); to.value = d }
  else if (p === 'last_month') { from.value = addMonths(d, -1); to.value = monthEnd(addMonths(d, -1)) }
}
async function load() {
  if (!from.value || !to.value || from.value > to.value) return
  error.value = ''
  const q = `from=${from.value}&to=${to.value}`
  try {
    if (tab.value === 'orders') {
      const l = await get<BillList>(`/merchant/branches/${bid}/bills?${q}&filter=${filter.value}`)
      if (l.timezone !== tz.value) { tz.value = l.timezone; if (preset.value !== 'custom') { choose(preset.value); return } }
      list.value = l
    } else sum.value = await get<BillSummary>(`/merchant/branches/${bid}/bills/summary?${q}`)
  } catch (e) { error.value = (e as Error).message }
}
onMounted(() => { choose('month'); void load() })
watch([from, to, filter, tab], () => { void load() })

const cur = computed(() => list.value?.currency ?? sum.value?.currency ?? session.user?.roles.find(r => r.branch_id === bid)?.currency ?? 'PHP')
const m = (v: number) => v ? money(v, cur.value) : '—'
const off = (v: number) => v ? '−' + money(v, cur.value) : '—'
const exportHref = computed(() => `/api/v1/merchant/branches/${bid}/bills?from=${from.value}&to=${to.value}&filter=${filter.value}&format=xlsx&lang=${locale.value}`)
const storeCoupons = (o: BillOrder) => o.store_free + o.store_threshold
const totalOff = computed(() => sum.value ? sum.value.kinds.reduce((s, k) => s + k.amount, 0) : 0)
const rate = computed(() => sum.value?.repurchase.issued ? Math.round(sum.value.repurchase.used * 100 / sum.value.repurchase.issued) : 0)
const FILTERS = ['', 'coupon', 'none', 'promotion', 'store_free', 'store_threshold', 'store_delivery', 'platform_free']
const filterName = (f: string) => f === '' ? t('bl.f.all') : f === 'coupon' || f === 'none' ? t('bl.f.' + f) : kindName(f as 'promotion')
async function showSettlement(id: number) {
  if (settleOrders.value[id]) { delete settleOrders.value[id]; return }
  try { settleOrders.value[id] = (await get<{ orders: BillOrder[] }>(`/merchant/branches/${bid}/settlements/${id}/orders`)).orders }
  catch (e) { error.value = (e as Error).message }
}
</script>

<template>
  <div class="page stack bills" style="max-width:1280px">
    <div class="row between wrap" style="gap:8px">
      <h1>{{ t('bl.nav') }}</h1>
      <a v-if="tab === 'orders'" class="btn" data-testid="bill-export" :href="exportHref" download>{{ t('bl.export') }}</a>
    </div>
    <div class="subtabs" role="tablist">
      <button type="button" role="tab" :class="{ on: tab === 'orders' }" data-testid="bill-tab-orders" @click="setTab('orders')">{{ t('bl.tab_orders') }}</button>
      <button type="button" role="tab" :class="{ on: tab === 'summary' }" data-testid="bill-tab-summary" @click="setTab('summary')">{{ t('bl.tab_sum') }}</button>
    </div>
    <div class="row wrap periods" style="gap:6px">
      <button v-for="p in (['today', 'yesterday', 'week', 'month', 'last_month', 'custom'] as Preset[])" :key="p" class="btn small" :class="{ primary: preset === p }"
        :data-preset="p" @click="p === 'custom' ? preset = 'custom' : choose(p)">{{ t('bl.p.' + p) }}</button>
      <span v-if="preset === 'custom'" class="custom"><input v-model="from" type="date" :max="to"> ~ <input v-model="to" type="date" :min="from" :max="today"></span>
      <select v-if="tab === 'orders'" v-model="filter" data-testid="bill-filter" class="filter">
        <option v-for="f in FILTERS" :key="f" :value="f">{{ filterName(f) }}</option></select>
    </div>
    <div v-if="error" class="banner bad">{{ error }}</div>

    <template v-if="tab === 'orders' && list">
      <p class="muted small" style="margin:0">{{ t('bl.hint') }}</p>
      <div v-if="list.count > list.shown" class="banner warn">{{ t('bl.cut', { n: list.shown }) }}</div>
      <div class="card" style="padding:0;overflow:auto">
        <table class="data bill" data-testid="bill-table">
          <thead><tr><th>{{ t('bl.no') }}</th><th>{{ t('bl.type') }}</th><th class="r">{{ t('bl.food') }}</th><th class="r">{{ t('bl.fee') }}</th>
            <th class="r">{{ t('bl.promo') }}</th><th class="r">{{ t('bl.store_coupon') }}</th><th class="r">{{ t('bl.fee_coupon') }}</th>
            <th class="r">{{ t('bl.platform') }}</th><th class="r">{{ t('bl.paid') }}</th><th class="r">{{ t('bl.takings') }}</th><th>{{ t('bl.state') }}</th></tr></thead>
          <tbody>
            <template v-for="o in list.orders" :key="o.id">
              <tr :class="{ off: o.state !== 'completed', has: o.discounts.length, opened: open === o.id }" data-testid="bill-row"
                @click="o.discounts.length && (open = open === o.id ? null : o.id)">
                <td><b>{{ o.public_no }}</b><div class="small muted">{{ dayOf(o.created_at, tz).slice(5) }} {{ clock(o.created_at, tz) }}</div></td>
                <td>{{ t('bl.' + o.service_type) }}</td>
                <td class="r">{{ m(o.food) }}</td><td class="r">{{ m(o.delivery_fee) }}</td>
                <td class="r neg">{{ off(o.promotion) }}</td><td class="r neg">{{ off(storeCoupons(o)) }}</td><td class="r neg">{{ off(o.store_delivery) }}</td>
                <td class="r plat">{{ o.platform_free ? '+' + money(o.platform_free, cur) : '—' }}</td>
                <td class="r">{{ o.state === 'completed' ? money(o.paid, cur) : money(0, cur) }}</td>
                <td class="r"><b>{{ o.state === 'completed' ? money(o.takings, cur) : money(0, cur) }}</b></td>
                <td><span class="tag" :class="{ ok: o.state === 'completed' }">{{ t('bl.st.' + o.state) }}</span>
                  <span v-if="o.discounts.length" class="chev">{{ open === o.id ? '▾' : '▸' }}</span></td>
              </tr>
              <tr v-if="open === o.id" class="detail" data-testid="bill-detail"><td colspan="11">
                <div v-for="(d, i) in o.discounts" :key="i" class="dline">
                  <span class="badge" :class="d.kind">{{ kindName(d.kind) }}</span>
                  <span>{{ discountLabel(d) }}</span>
                  <span v-if="d.coupon_id" class="muted small">{{ t('bl.coupon_no', { id: d.coupon_id }) }}</span>
                  <span v-if="d.source" class="muted small">{{ sourceName(d.source) }}</span>
                  <span class="muted small">{{ t('bl.who.' + d.funded_by) }}</span>
                  <b class="amt">−{{ money(d.amount, cur) }}</b>
                </div>
              </td></tr>
            </template>
            <tr v-if="!list.orders.length"><td colspan="11" class="muted">{{ t('bl.none') }}</td></tr>
          </tbody>
          <tfoot v-if="list.orders.length"><tr data-testid="bill-sum">
            <td colspan="2"><b>{{ t('bl.sum', { n: list.orders.filter(o => o.state === 'completed').length }) }}</b></td>
            <td class="r">{{ m(list.sum.food) }}</td><td class="r">{{ m(list.sum.delivery_fee) }}</td>
            <td class="r neg">{{ off(list.sum.promotion) }}</td><td class="r neg">{{ off(storeCoupons(list.sum)) }}</td><td class="r neg">{{ off(list.sum.store_delivery) }}</td>
            <td class="r plat">{{ list.sum.platform_free ? '+' + money(list.sum.platform_free, cur) : '—' }}</td>
            <td class="r">{{ money(list.sum.paid, cur) }}</td><td class="r"><b>{{ money(list.sum.takings, cur) }}</b></td><td></td>
          </tr></tfoot>
        </table>
      </div>
    </template>

    <template v-if="tab === 'summary' && sum">
      <div class="cards" data-testid="bill-cards">
        <div class="card"><span>{{ t('bl.c.orders') }}</span><b>{{ sum.orders }}</b><small v-if="sum.cancelled">{{ t('bl.c.cancelled', { n: sum.cancelled }) }}</small></div>
        <div class="card"><span>{{ t('bl.c.food') }}</span><b>{{ money(sum.food, cur) }}</b></div>
        <div class="card"><span>{{ t('bl.c.discount') }}</span><b>{{ money(sum.merchant_discount + sum.platform_discount, cur) }}</b>
          <small>{{ t('bl.c.discount_sub', { m: money(sum.merchant_discount, cur), p: money(sum.platform_discount, cur) }) }}</small></div>
        <div class="card"><span>{{ t('bl.c.paid') }}</span><b>{{ money(sum.paid, cur) }}</b></div>
        <div class="card"><span>{{ t('bl.c.due') }}</span><b class="plat">{{ money(sum.platform_discount, cur) }}</b></div>
        <div class="card"><span>{{ t('bl.c.takings') }}</span><b class="ok">{{ money(sum.takings, cur) }}</b></div>
      </div>
      <div class="cols">
        <div class="card stack">
          <h2>{{ t('bl.kinds') }}</h2>
          <table class="data" data-testid="bill-kinds">
            <thead><tr><th>{{ t('bl.k.kind') }}</th><th>{{ t('bl.k.who') }}</th><th class="r">{{ t('bl.k.orders') }}</th><th class="r">{{ t('bl.k.amount') }}</th><th class="r">{{ t('bl.k.avg') }}</th></tr></thead>
            <tbody><tr v-for="k in sum.kinds" :key="k.kind" :data-kind="k.kind">
              <td><span class="badge" :class="k.kind">{{ kindName(k.kind) }}</span></td><td>{{ t('bl.who.' + k.funded_by) }}</td>
              <td class="r">{{ k.orders }}</td><td class="r">{{ m(k.amount) }}</td><td class="r">{{ k.orders ? money(Math.round(k.amount / k.orders), cur) : '—' }}</td>
            </tr></tbody>
            <tfoot><tr><td colspan="3"><b>{{ t('bl.k.total') }}</b></td><td class="r"><b>{{ money(totalOff, cur) }}</b></td><td></td></tr></tfoot>
          </table>
          <h2>{{ t('bl.rep') }}</h2>
          <p class="small" style="margin:0">{{ t('bl.rep_line', { issued: sum.repurchase.issued, used: sum.repurchase.used, rate, sales: money(sum.repurchase.sales, cur) }) }}</p>
        </div>
        <div class="card stack">
          <h2>{{ t('bl.settle') }}</h2>
          <p class="muted small" style="margin:0">{{ t('bl.settle_hint') }}</p>
          <div v-if="!sum.settlements.length" class="muted small">{{ t('bl.settle_none') }}</div>
          <div v-for="s in sum.settlements" :key="s.id" class="settle" data-testid="bill-settlement">
            <div class="row between wrap" style="gap:6px">
              <div><b>{{ s.month }}</b> · {{ s.orders }} · <b>{{ money(s.amount, s.currency) }}</b></div>
              <span class="tag" :class="{ ok: s.status === 'paid' }">{{ t('bl.s.' + s.status) }}</span>
            </div>
            <div v-if="s.status === 'paid'" class="small muted">{{ t('bl.paid_on', { d: s.paid_on ?? '', ref: s.transfer_ref }) }}</div>
            <div class="row" style="gap:6px">
              <button class="btn small" @click="showSettlement(s.id)">{{ t('bl.view_orders') }}</button>
              <router-link class="btn small" to="/merchant/support">{{ t('bl.ask') }}</router-link>
            </div>
            <table v-if="settleOrders[s.id]" class="data small">
              <tbody><tr v-for="o in settleOrders[s.id]" :key="o.id"><td>{{ o.public_no }}</td><td>{{ dayOf(o.created_at, tz) }}</td>
                <td class="r">{{ money(o.platform_free, s.currency) }}</td></tr></tbody></table>
          </div>
        </div>
      </div>
      <div v-if="sum.days.length > 1" class="card" style="padding:0;overflow:auto">
        <table class="data">
          <thead><tr><th>{{ t('bl.day') }}</th><th class="r">{{ t('bl.c.orders') }}</th><th class="r">{{ t('bl.food') }}</th><th class="r">{{ t('bl.c.discount') }}</th>
            <th class="r">{{ t('bl.paid') }}</th><th class="r">{{ t('bl.takings') }}</th></tr></thead>
          <tbody><tr v-for="d in sum.days" :key="d.day"><td>{{ d.day }}</td><td class="r">{{ d.orders }}</td><td class="r">{{ money(d.food, cur) }}</td>
            <td class="r">{{ m(d.discount) }}</td><td class="r">{{ money(d.paid, cur) }}</td><td class="r">{{ money(d.takings, cur) }}</td></tr></tbody>
        </table>
      </div>
    </template>
  </div>
</template>

<style scoped>
.custom input { width: auto; }
.filter { width: auto; margin-left: auto; }
.r { text-align: right; white-space: nowrap; font-variant-numeric: tabular-nums; }
.bill td, .bill th { white-space: nowrap; }
.bill tr.has { cursor: pointer; } .bill tr.off td { color: var(--muted); } .bill tr.opened td { background: #fff8f1; }
.neg { color: #b8392a; } .plat { color: #6d28d9; }
.chev { margin-left: 6px; color: var(--muted); }
.detail td { background: #fffaf2; padding: 6px 12px 10px; }
.dline { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; padding: 3px 0; }
.dline .amt { margin-left: auto; color: #b8392a; }
.badge { display: inline-block; font-size: 12px; border-radius: 5px; padding: 1px 7px; background: #fde7d8; color: #9a3412; white-space: nowrap; }
.badge.store_delivery { background: #fdf0d5; color: #92400e; } .badge.platform_free { background: #ede9fe; color: #5b21b6; }
.badge.promotion { background: #e8eef7; color: #334e7a; }
.cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(170px, 1fr)); gap: 10px; }
.cards .card { display: flex; flex-direction: column; gap: 2px; padding: 12px 14px; }
.cards span { font-size: 12.5px; color: var(--muted); } .cards b { font-size: 21px; font-variant-numeric: tabular-nums; } .cards small { font-size: 12px; color: var(--muted); }
.cards b.ok { color: var(--ok, #18794e); }
.cols { display: grid; gap: 14px; } @media (min-width: 900px) { .cols { grid-template-columns: minmax(0, 3fr) minmax(0, 2fr); align-items: start; } }
.cols h2 { margin: 0; }
.settle { border-top: 1px solid var(--line); padding-top: 8px; display: grid; gap: 6px; }
</style>
