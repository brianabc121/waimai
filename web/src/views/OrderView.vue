<script setup lang="ts">
import { computed, defineAsyncComponent, ref } from 'vue'
import { ApiError, get, post } from '../api'
import { locale, money, pick, reasonText, setLocale, t, whenText } from '../i18n'
import { useLive } from '../live'
import type { Order } from '../types'
import TotalsTable from './TotalsTable.vue'

// Only customers whose order is on its way download the map.
const TrackRider = defineAsyncComponent(() => import('./TrackRider.vue'))

const props = defineProps<{ token: string }>()
const order = ref<Order | null>(null)
const missing = ref(false)
const error = ref('')

const { online } = useLive(() => `/customer/orders/${props.token}/events`, async () => {
  try {
    const o = await get<Order>(`/customer/orders/${props.token}`)
    // Never let a slower, older response overwrite a newer state.
    if (!order.value || o.version >= order.value.version) order.value = o
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) missing.value = true
    throw e
  }
})

const steps = computed(() => order.value?.service_type === 'pickup'
  ? ['pending_acceptance', 'preparing', 'ready_for_pickup', 'picked_up']
  : ['pending_acceptance', 'preparing', 'awaiting_rider', 'out_for_delivery', 'delivered'])
const stepIndex = computed(() => {
  const s = order.value?.display_status ?? ''
  return steps.value.indexOf(s === 'rider_arrived' ? 'out_for_delivery' : s === 'scheduled' ? 'preparing' : s)
})
const closed = computed(() => ['rejected', 'cancelled', 'delivery_failed'].includes(order.value?.display_status ?? ''))

async function cancel() {
  if (!confirm(t('cancel_confirm'))) return
  error.value = ''
  try { order.value = await post<Order>(`/customer/orders/${props.token}/cancel`) }
  catch (e) { error.value = (e as Error).message }
}
</script>

<template>
  <div class="phone" style="padding-bottom:24px">
    <div class="topbar row between">
      <h1>{{ t('order_no') }} {{ order?.public_no }}</h1>
      <button class="lang" @click="setLocale(locale === 'zh' ? 'en' : 'zh')">{{ locale === 'zh' ? 'EN' : '中文' }}</button>
    </div>
    <div class="section stack">
      <div v-if="missing" class="banner bad">{{ t('not_found') }}</div>
      <div v-else-if="!order" class="muted">{{ t('loading') }}</div>
      <template v-else>
        <div v-if="!online" class="banner warn">{{ t('offline') }}</div>
        <div v-if="error" class="banner bad">{{ error }}</div>

        <div class="card stack">
          <h2 :style="{ color: closed ? 'var(--bad)' : 'var(--brand-dark)' }" data-testid="status">{{ t('s.' + order.display_status) }}</h2>
          <p v-if="closed && order.close_reason" class="muted">{{ t('reason') }}: {{ reasonText(order.close_reason) }}</p>
          <p v-if="order.scheduled_for" class="bold" data-testid="scheduled">{{ t(order.service_type === 'delivery' ? 'deliver_at' : 'pickup_at') }} {{ whenText(order.scheduled_for, order.timezone) }}</p>
          <div v-if="!closed" class="steps">
            <div v-for="(s, i) in steps" :key="s" class="step" :class="{ done: i <= stepIndex, cur: i === stepIndex }">
              <span class="dot"></span><span class="small">{{ t('s.' + s) }}</span>
            </div>
          </div>
          <div v-if="order.service_type === 'pickup'" class="row between">
            <span class="muted">{{ t('pickup_code') }}</span><span class="bold" style="font-size:24px">{{ order.pickup_code }}</span>
          </div>
          <div v-if="order.delivery && ['out_for_delivery', 'rider_arrived'].includes(order.display_status)" class="row between">
            <span class="muted">{{ t('rider') }}</span>
            <span>{{ order.delivery.rider_name }} <a v-if="order.delivery.rider_phone" :href="'tel:' + order.delivery.rider_phone">{{ order.delivery.rider_phone }}</a></span>
          </div>
          <div class="row between"><span class="muted">{{ t('pay_method') }}</span>
            <span>{{ t(order.payment_method) }} · <span class="tag" :class="order.payment_status === 'paid' ? 'ok' : 'warn'">{{ t(order.payment_status) }}</span></span></div>
          <button v-if="order.display_status === 'pending_acceptance'" class="btn danger block" @click="cancel">{{ t('cancel_order') }}</button>
        </div>

        <TrackRider v-if="order.delivery && ['out_for_delivery', 'rider_arrived'].includes(order.display_status)" :token="token" />

        <div class="card stack">
          <h2>{{ t('order_summary') }}</h2>
          <div v-if="order.service_type === 'delivery'" class="muted small">{{ order.zone_name }} · {{ order.address_line }} {{ order.address_unit }}</div>
          <div v-for="it in order.items" :key="it.id" class="row top between">
            <div class="grow"><b>{{ it.qty }} ×</b> {{ pick(it.name, it.name_en) }}
              <div class="muted small" v-if="it.options.length">{{ it.options.map(o => pick(o.name, o.name_en)).join(' · ') }}</div>
              <div class="muted small" v-if="it.note">{{ t('note') }}: {{ it.note }}</div></div>
            <span class="price">{{ money(it.line_total, order.currency) }}</span>
          </div>
          <TotalsTable :totals="order.totals" :currency="order.currency" :delivery="order.service_type === 'delivery'" />
        </div>
      </template>
    </div>
  </div>
</template>

<style scoped>
.steps { display: flex; flex-direction: column; gap: 8px; }
.step { display: flex; align-items: center; gap: 10px; color: var(--muted); }
.step .dot { width: 12px; height: 12px; border-radius: 50%; background: var(--line); flex: none; }
.step.done { color: var(--ink); } .step.done .dot { background: var(--ok); }
.step.cur { font-weight: 700; } .step.cur .dot { background: var(--brand); box-shadow: 0 0 0 4px var(--brand-soft); }
</style>
