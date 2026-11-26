<script setup lang="ts">
// The platform block of an order taken on a delivery platform
// (docs/ORDER_TYPES_DESIGN.md §7.4, §9): its number on the platform, who
// collects it, what the platform owes the store, the commission and other
// charges, what the platform took back, and the expected payout. Staff with
// "platform orders" may change the commission (with a reason), add or remove
// a charge and record a deduction. The amounts come from the server.
import { ref } from 'vue'
import { del, post, put } from '../api'
import { money, t, toMinor } from '../i18n'
import { can } from '../session'
import type { Order } from '../types'
import { bpToPct, platformName } from '../platforms'
import '../platformOrders'

const props = defineProps<{ order: Order }>()
const emit = defineEmits<{ changed: [] }>()
const m = (v: number) => money(v, props.order.currency)
const form = ref<'' | 'commission' | 'fee' | 'deduct'>('')
const amt = ref(''), reason = ref(''), error = ref(''), busy = ref(false)
const commission = () => props.order.platform?.fees.find(f => f.kind === 'commission') ?? null
const others = () => props.order.platform?.fees.filter(f => f.kind === 'other') ?? []

function open(k: typeof form.value) {
  form.value = form.value === k ? '' : k
  error.value = ''; reason.value = ''
  amt.value = k === 'commission' ? String((commission()?.amount ?? 0) / 100) : ''
}
async function run(fn: () => Promise<unknown>) {
  error.value = ''; busy.value = true
  try { await fn(); form.value = ''; emit('changed') } catch (e) { error.value = (e as Error).message } finally { busy.value = false }
}
const url = (s: string) => `/merchant/orders/${props.order.id}/${s}`
function save() {
  if (amt.value.trim() === '' || isNaN(Number(amt.value))) { error.value = t('po.o.amount'); return }
  if (!reason.value.trim()) { error.value = t('reason_required'); return }
  const body = { amount: toMinor(amt.value), reason: reason.value.trim() }
  if (form.value === 'commission') return run(() => put(url('platform-commission'), body))
  if (form.value === 'fee') return run(() => post(url('platform-fees'), body))
  return run(() => post(url('platform-deductions'), body))
}
const autoCommission = () => run(() => put(url('platform-commission'), { amount: null }))
const dropFee = (id: number) => run(() => del(url(`platform-fees/${id}`)))
</script>

<template>
  <div v-if="order.platform" class="pblock" :style="{ '--pc': order.platform.color }" data-testid="platform-block">
    <div class="ph"><span class="pname">{{ platformName(order.platform) }}</span>
      <span>{{ t('po.o.no') }} <b data-testid="pb-no">{{ order.platform.order_no }}</b></span>
      <span class="tag">{{ t('po.pk.' + order.platform.pickup) }}</span></div>
    <div class="kv"><span>{{ t('po.receivable') }}</span><b data-testid="pb-receivable">{{ m(order.platform.receivable) }}</b></div>
    <div class="kv"><span>{{ t('po.commission') }}
      <small v-if="commission()" class="muted">{{ commission()!.manual ? t('po.manual') + ' · ' + commission()!.reason : bpToPct(commission()!.rate_bp) + '% × ' + m(commission()!.base) }}</small></span>
      <b data-testid="pb-commission">−{{ m(order.platform.commission) }}</b></div>
    <div v-for="f in others()" :key="f.id" class="kv" data-testid="pb-fee"><span>{{ t('po.o.fees') }} · {{ f.reason }}
      <button v-if="can('order.platform')" class="btn link small" :disabled="busy" data-testid="pb-fee-del" @click="dropFee(f.id)">{{ t('po.o.delete') }}</button></span>
      <b>−{{ m(f.amount) }}</b></div>
    <div v-if="order.platform.deducted" class="kv"><span>{{ t('po.o.deducted') }}</span><b data-testid="pb-deducted">−{{ m(order.platform.deducted) }}</b></div>
    <div class="kv payout"><span>{{ t('po.payout') }}</span><b data-testid="pb-payout">{{ m(order.platform.payout) }}</b></div>
    <div v-if="can('order.platform')" class="acts">
      <button class="btn small" data-testid="pb-edit-commission" @click="open('commission')">{{ t('po.edit_commission') }}</button>
      <button v-if="commission()?.manual" class="btn small" :disabled="busy" data-testid="pb-auto-commission" @click="autoCommission">{{ t('po.auto_commission') }}</button>
      <button class="btn small" data-testid="pb-add-fee" @click="open('fee')">{{ t('po.o.add_fee') }}</button>
      <button class="btn small" data-testid="pb-deduct" @click="open('deduct')">{{ t('po.o.deduct') }}</button>
    </div>
    <form v-if="form" class="pform" data-testid="pb-form" @submit.prevent="save">
      <div v-if="form === 'deduct'" class="muted small">{{ t('po.o.deduct_hint') }}</div>
      <div class="row">
        <input v-model="amt" class="inp amt" inputmode="decimal" :placeholder="t('po.o.amount')" :aria-label="t('po.o.amount')" data-testid="pb-amount" />
        <input v-model="reason" class="inp grow" maxlength="200" :aria-label="t('po.reason')"
          :placeholder="form === 'fee' ? t('po.o.fee_ph') : form === 'deduct' ? t('po.o.deduct_ph') : t('po.reason_ph')" data-testid="pb-reason" />
        <button class="btn primary small" :disabled="busy" data-testid="pb-save">{{ t('po.o.save') }}</button>
      </div>
    </form>
    <div v-if="error" class="banner bad small">{{ error }}</div>
  </div>
</template>

<style scoped>
.pblock { --pc: #888; border: 1px solid var(--line); border-inline-start: 4px solid var(--pc); border-radius: 10px; padding: 10px 12px; display: grid; gap: 6px; background: #fffdf9; }
.ph { display: flex; flex-wrap: wrap; gap: 6px 12px; align-items: center; }
.pname { background: var(--pc); color: #fff; font-weight: 700; border-radius: 6px; padding: 1px 8px; }
.kv { display: flex; justify-content: space-between; gap: 8px; align-items: baseline; }
.kv b { font-variant-numeric: tabular-nums; }
.kv.payout { border-top: 1px dashed var(--line); padding-top: 6px; color: var(--brand); font-size: 16px; }
.acts { display: flex; flex-wrap: wrap; gap: 6px; }
.pform .row { display: flex; gap: 6px; flex-wrap: wrap; }
.pform .amt { width: 110px; } .pform .grow { flex: 1 1 160px; min-width: 0; }
</style>
