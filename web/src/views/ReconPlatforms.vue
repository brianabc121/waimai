<script setup lang="ts">
// The reconciliation page's platform part (docs/ORDER_TYPES_DESIGN.md §9.5):
// takings by order type, and per delivery platform what it owes the store,
// what it charged and took back, the payout to expect, and what it actually
// paid as typed in from its statement. Every amount comes from the server.
import { computed, reactive, ref } from 'vue'
import { api, get } from '../api'
import { extend, money, t, toMinor } from '../i18n'
import { platformName } from '../platforms'
import '../platformOrders'
import { typeColor } from '../orderTypes'
import { clock, dayOf } from './staff/time'

extend({
  'rc.ty.title': ['营业额按类型', 'Takings by order type'],
  'rc.ty.hint': ['平台单按商家应收算（顾客实付 + 平台出资）', 'Platform orders count what the platform owes (paid + platform-funded)'],
  'rc.ty.share': ['占比', 'Share'],
  'rc.pf.title': ['平台订单', 'Platform orders'],
  'rc.pf.hint': ['预计到账 = 商家应收 − 佣金 − 其他扣费 − 平台扣款。实际到账照平台结算单填，只算结算周期在所选日期内的。',
    'Expected = owed − commission − other charges − taken back. Type the actual payout from the platform\'s statement; only statements inside the chosen dates count.'],
  'rc.pf.orders': ['单数', 'Orders'],
  'rc.pf.subtotal': ['平台价营业额', 'At platform prices'],
  'rc.pf.actual': ['实际到账', 'Actual payout'],
  'rc.pf.diff': ['差额', 'Difference'],
  'rc.pf.enter': ['填写', 'Enter'],
  'rc.pf.period': ['结算周期 {f} ~ {t}', 'Statement {f} ~ {t}'],
  'rc.pf.note_ph': ['备注，如结算单号', 'Note, e.g. statement number'],
  'rc.pf.detail': ['逐单明细', 'Orders'],
  'rc.pf.detail_hint': ['逐单的平台扣款是这张单的全部扣款，不分日期', "Taken back per order is all of that order's deductions, whatever the date"],
})

interface Payout { id: number; brand_id: number; platform_id: number; from: string; to: string; amount: number; note: string; by: string; at: string }
export interface ReconPlatform {
  brand_id: number; brand_name: string; platform_id: number; name: string; names: Record<string, string>; prefix: string; color: string; count: number
  subtotal: number; merchant: number; subsidy: number; paid: number; receivable: number; commission: number; other_fees: number
  deducted: number; payout: number; actual: number | null; diff: number | null; payouts: Payout[]
}
export interface ReconType { type: string; source: string; platform_id: number; name: string; names: Record<string, string>; color: string; count: number; amount: number }
interface POrder { order_id: number; public_no: string; paid_at: string; status: string; subtotal: number; merchant: number; subsidy: number
  paid: number; receivable: number; commission: number; other_fees: number; deducted: number; payout: number }

// several: the store has more than one brand, so a row is a brand's shop on a platform ("一品湘 Grab")
const props = defineProps<{ bid: number; from: string; to: string; currency: string; tz: string; total: number; platforms: ReconPlatform[]; types: ReconType[]
  several?: boolean }>()
const shopKey = (p: ReconPlatform) => `${p.brand_id}-${p.platform_id}`
const shopName = (p: ReconPlatform) => props.several ? `${p.brand_name} ${platformName(p)}` : platformName(p)
const emit = defineEmits<{ changed: [] }>()
const m = (v: number) => money(v, props.currency)
const signed = (v: number) => (v > 0 ? '+' : v < 0 ? '−' : '') + m(Math.abs(v))

const typeName = (x: ReconType) => x.platform_id ? platformName(x)
  : t('ot.t.' + x.type) + (x.source ? ' · ' + t('ot.ds.' + x.source) : '')
const share = (v: number) => props.total ? `${Math.round(v * 1000 / props.total) / 10}%` : '—'
const typeTotal = computed(() => props.types.reduce((s, x) => s + x.count, 0))

// the orders under a brand's shop on a platform
const open = ref('')
const orders = ref<POrder[]>([])
async function toggle(p: ReconPlatform) {
  const k = shopKey(p)
  if (open.value === k) { open.value = ''; return }
  open.value = k; orders.value = []
  orders.value = (await get<{ orders: POrder[] }>(`/merchant/branches/${props.bid}/reconcile/platform-orders?from=${props.from}&to=${props.to}` +
    `&platform=${p.platform_id}&brand=${p.brand_id}`)).orders
}

// the actual payout, typed in from the platform's statement for the chosen dates
const form = reactive({ key: '', brand: 0, id: 0, amount: '', note: '', busy: false, error: '' })
function enter(p: ReconPlatform) {
  const same = p.payouts.find(x => x.from === props.from && x.to === props.to)
  Object.assign(form, { key: shopKey(p), brand: p.brand_id, id: p.platform_id, amount: same ? String(same.amount / 100) : '', note: same?.note ?? '', error: '' })
}
async function save() {
  if (form.amount.trim() === '' || isNaN(Number(form.amount))) return
  form.busy = true; form.error = ''
  try {
    await api('PUT', `/merchant/branches/${props.bid}/reconcile/platform-payouts`,
      { brand_id: form.brand, platform_id: form.id, from: props.from, to: props.to, amount: toMinor(form.amount), note: form.note })
    form.key = ''
    emit('changed')
  } catch (e) { form.error = (e as Error).message }
  form.busy = false
}
async function drop(x: Payout) {
  try { await api('DELETE', `/merchant/branches/${props.bid}/reconcile/platform-payouts/${x.id}`); emit('changed') }
  catch (e) { form.error = (e as Error).message }
}
</script>

<template>
  <div class="ox-panel ox-scroll" data-testid="rc-types">
    <div class="ox-panel-h"><h2>{{ t('rc.ty.title') }}</h2><span class="muted small">{{ t('rc.ty.hint') }}</span></div>
    <table class="data">
      <thead><tr><th>{{ t('rc.col.kind') }}</th><th class="r">{{ t('rc.col.count') }}</th><th class="r">{{ t('rc.col.amount') }}</th><th class="r">{{ t('rc.ty.share') }}</th></tr></thead>
      <tbody>
        <tr v-for="x in types" :key="x.type + x.source + x.platform_id" :data-type="x.type + (x.source ? '-' + x.source : '') + (x.platform_id ? '-' + x.platform_id : '')">
          <td><i class="dot" :style="{ background: x.color || typeColor[x.type] }"></i>{{ typeName(x) }}</td>
          <td class="r">{{ x.count }}</td><td class="r"><b>{{ m(x.amount) }}</b></td><td class="r muted">{{ share(x.amount) }}</td>
        </tr>
        <tr v-if="!types.length"><td colspan="4" class="muted">{{ t('rc.none') }}</td></tr>
        <tr v-else class="sum"><td>{{ t('rc.c.total') }}</td><td class="r">{{ typeTotal }}</td><td class="r"><b>{{ m(total) }}</b></td><td></td></tr>
      </tbody>
    </table>
  </div>

  <div v-if="platforms.length" class="ox-panel ox-scroll" data-testid="rc-platforms">
    <div class="ox-panel-h"><h2>{{ t('rc.pf.title') }}</h2></div>
    <p class="muted small" style="margin:0 0 8px">{{ t('rc.pf.hint') }}</p>
    <table class="data pf">
      <thead><tr><th>{{ t('po.platform') }}</th><th class="r">{{ t('rc.pf.orders') }}</th><th class="r">{{ t('rc.pf.subtotal') }}</th>
        <th class="r">{{ t('po.merchant') }}</th><th class="r">{{ t('po.platform_funded') }}</th><th class="r">{{ t('po.receivable') }}</th>
        <th class="r">{{ t('po.commission') }}</th><th class="r">{{ t('po.o.fees') }}</th><th class="r">{{ t('po.o.deducted') }}</th>
        <th class="r">{{ t('po.payout') }}</th><th class="r">{{ t('rc.pf.actual') }}</th><th class="r">{{ t('rc.pf.diff') }}</th></tr></thead>
      <tbody>
        <template v-for="p in platforms" :key="shopKey(p)">
          <tr :data-platform="p.prefix" :data-brand="p.brand_id" class="prow" @click="toggle(p)">
            <td><span class="pname" :style="{ background: p.color }">{{ shopName(p) }}</span> <span class="caret" :class="{ on: open === shopKey(p) }">›</span></td>
            <td class="r">{{ p.count }}</td><td class="r">{{ m(p.subtotal) }}</td><td class="r neg">{{ p.merchant ? '−' + m(p.merchant) : '—' }}</td>
            <td class="r">{{ p.subsidy ? m(p.subsidy) : '—' }}</td><td class="r"><b data-testid="rc-pf-receivable">{{ m(p.receivable) }}</b></td>
            <td class="r neg">−{{ m(p.commission) }}</td><td class="r neg">{{ p.other_fees ? '−' + m(p.other_fees) : '—' }}</td>
            <td class="r neg">{{ p.deducted ? '−' + m(p.deducted) : '—' }}</td>
            <td class="r"><b data-testid="rc-pf-payout">{{ m(p.payout) }}</b></td>
            <td class="r" @click.stop><b v-if="p.actual != null" data-testid="rc-pf-actual">{{ m(p.actual) }}</b>
              <button class="btn small" data-testid="rc-pf-enter" @click="enter(p)">{{ p.actual != null ? t('edit') : t('rc.pf.enter') }}</button></td>
            <td class="r"><b v-if="p.diff != null" :class="{ neg: p.diff !== 0 }" data-testid="rc-pf-diff">{{ signed(p.diff) }}</b><span v-else class="muted">—</span></td>
          </tr>
          <tr v-if="form.key === shopKey(p)"><td colspan="12">
            <form class="row wrap" style="gap:8px" @submit.prevent="save">
              <span class="small muted">{{ t('rc.pf.period', { f: from, t: to }) }}</span>
              <input v-model="form.amount" inputmode="decimal" style="width:130px" :placeholder="t('rc.pf.actual')" data-testid="rc-pf-amount" />
              <input v-model="form.note" class="grow" maxlength="200" :placeholder="t('rc.pf.note_ph')" />
              <button class="btn primary" :disabled="form.busy" data-testid="rc-pf-save">{{ t('po.o.save') }}</button>
              <button type="button" class="btn" @click="form.key = ''">{{ t('ck.cancel') }}</button>
            </form>
            <div v-if="form.error" class="banner bad small">{{ form.error }}</div></td></tr>
          <tr v-for="x in p.payouts" :key="'x' + x.id" class="sub"><td colspan="10" class="small muted">{{ t('rc.pf.period', { f: x.from, t: x.to }) }}
            · {{ m(x.amount) }}<template v-if="x.note"> · {{ x.note }}</template><template v-if="x.by"> · {{ x.by }}</template></td>
            <td colspan="2" class="r"><button class="btn link small" data-testid="rc-pf-del" @click="drop(x)">{{ t('po.o.delete') }}</button></td></tr>
          <tr v-if="open === shopKey(p)"><td colspan="12" class="detail">
            <div class="small muted">{{ t('rc.pf.detail') }} · {{ t('rc.pf.detail_hint') }}</div>
            <table class="data inner" data-testid="rc-pf-orders">
              <thead><tr><th>{{ t('rc.col.time') }}</th><th>{{ t('rc.col.order') }}</th><th class="r">{{ t('rc.pf.subtotal') }}</th><th class="r">{{ t('po.merchant') }}</th>
                <th class="r">{{ t('po.platform_funded') }}</th><th class="r">{{ t('po.receivable') }}</th><th class="r">{{ t('po.commission') }}</th>
                <th class="r">{{ t('po.o.fees') }}</th><th class="r">{{ t('po.o.deducted') }}</th><th class="r">{{ t('po.payout') }}</th></tr></thead>
              <tbody>
                <tr v-for="o in orders" :key="o.order_id" :data-order="o.public_no">
                  <td class="nowrap">{{ dayOf(o.paid_at, tz).slice(5) }} {{ clock(o.paid_at, tz) }}</td>
                  <td><router-link :to="`/merchant/orders?order=${o.order_id}`">{{ o.public_no }}</router-link></td>
                  <td class="r">{{ m(o.subtotal) }}</td><td class="r">{{ o.merchant ? '−' + m(o.merchant) : '—' }}</td><td class="r">{{ o.subsidy ? m(o.subsidy) : '—' }}</td>
                  <td class="r">{{ m(o.receivable) }}</td><td class="r">−{{ m(o.commission) }}</td><td class="r">{{ o.other_fees ? '−' + m(o.other_fees) : '—' }}</td>
                  <td class="r">{{ o.deducted ? '−' + m(o.deducted) : '—' }}</td><td class="r"><b>{{ m(o.payout) }}</b></td>
                </tr>
              </tbody>
            </table></td></tr>
        </template>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.r { text-align: end; white-space: nowrap; font-variant-numeric: tabular-nums; }
.nowrap { white-space: nowrap; }
.neg { color: #b8392a; }
.dot { display: inline-block; width: 9px; height: 9px; border-radius: 50%; margin-inline-end: 6px; }
.pname { color: #fff; font-weight: 700; border-radius: 6px; padding: 1px 8px; white-space: nowrap; }
.prow { cursor: pointer; }
.caret { display: inline-block; color: var(--muted); transition: transform .15s; } .caret.on { transform: rotate(90deg); }
tr.sum td { font-weight: 700; border-top: 2px solid var(--line); }
tr.sub td { padding-top: 0; }
.detail { background: #fbf6ee; }
.inner { margin-top: 6px; font-size: 13px; }
.row.wrap { display: flex; flex-wrap: wrap; align-items: center; }
.grow { flex: 1 1 160px; min-width: 0; }
</style>
