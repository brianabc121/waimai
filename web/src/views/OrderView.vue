<script setup lang="ts">
import { computed, defineAsyncComponent, reactive, ref, watch } from 'vue'
import { ApiError, get, post } from '../api'
import { locale, money, pick, reasonText, setLocale, t, whenText } from '../i18n'
import { useLive } from '../live'
import type { Order, OrderReview } from '../types'
import './storeStrings'
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

// An order split into baskets is listed basket by basket.
const multi = computed(() => !!order.value?.items.some(i => i.basket > 1))
const sorted = computed(() => [...(order.value?.items ?? [])].sort((a, b) => a.basket - b.basket || a.id - b.id))

// ---- the review of a finished order ----
const review = ref<OrderReview | null>(null)
const rv = reactive({ rating: 0, taste: 0, packaging: 0, delivery: 0, comment: '', busy: false, error: '' })
const finished = computed(() => ['delivered', 'picked_up'].includes(order.value?.display_status ?? ''))
watch(finished, async on => { if (on) review.value = await get<OrderReview>(`/customer/orders/${props.token}/review`).catch(() => null) }, { immediate: true })
const scores = computed(() => ['rating', 'taste', 'packaging', ...(order.value?.service_type === 'delivery' ? ['delivery'] : [])] as ('rating' | 'taste' | 'packaging' | 'delivery')[])
const scoreLabel = { rating: 'rv.overall', taste: 'sf.taste', packaging: 'sf.packaging', delivery: 'sf.delivery_score' }
async function submitReview() {
  if (scores.value.some(k => !rv[k])) { rv.error = t('rv.pick_all'); return }
  rv.busy = true; rv.error = ''
  try {
    review.value = await post<OrderReview>(`/customer/orders/${props.token}/review`,
      { rating: rv.rating, taste: rv.taste, packaging: rv.packaging, delivery: rv.delivery, comment: rv.comment })
  } catch (e) { rv.error = (e as Error).message }
  rv.busy = false
}

async function cancel() {
  if (!confirm(t('cancel_confirm'))) return
  error.value = ''
  try { order.value = await post<Order>(`/customer/orders/${props.token}/cancel`) }
  catch (e) { error.value = (e as Error).message }
}
</script>

<template>
  <div class="phone wide" style="padding-bottom:24px">
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

        <div class="two-cols"><div class="stack">
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

        <form v-if="review?.can_review" class="card stack" data-testid="review-form" @submit.prevent="submitReview">
          <h2>{{ t('rv.title') }}</h2>
          <div v-if="rv.error" class="banner bad">{{ rv.error }}</div>
          <div v-for="k in scores" :key="k" class="row between" :data-score="k">
            <span>{{ t(scoreLabel[k]) }}</span>
            <span class="starpick"><button v-for="n in 5" :key="n" type="button" :class="{ on: n <= rv[k] }" :aria-label="`${n}`" @click="rv[k] = n">★</button></span>
          </div>
          <textarea v-model="rv.comment" rows="3" maxlength="500" :placeholder="t('rv.comment_ph')"></textarea>
          <button class="btn primary block" :disabled="rv.busy">{{ t('rv.submit') }}</button>
        </form>
        <div v-else-if="review?.review" class="card stack" data-testid="review-done">
          <div class="row between"><h2>{{ t('rv.done') }}</h2><span class="starpick static"><i v-for="n in 5" :key="n" :class="{ on: n <= review.review.rating }">★</i></span></div>
          <p v-if="review.review.comment" style="white-space:pre-wrap;margin:0">{{ review.review.comment }}</p>
          <p class="muted small" style="margin:0">{{ t('rv.shown_as', { name: review.review.display_name }) }}</p>
        </div>
        </div>

        <div class="card stack side">
          <h2>{{ t('order_summary') }}</h2>
          <div v-if="order.service_type === 'delivery'" class="muted small">{{ order.address_line }} {{ order.address_unit }}</div>
          <template v-for="(it, i) in sorted" :key="it.id">
          <div v-if="multi && (i === 0 || sorted[i - 1].basket !== it.basket)" class="muted small bold" data-testid="basket-head">{{ t('basket_n', { n: it.basket }) }}</div>
          <div class="row top between">
            <div class="grow"><b>{{ it.qty }} ×</b> {{ pick(it.name, it.name_en) }}
              <div class="muted small" v-if="it.options.length">{{ it.options.map(o => pick(o.name, o.name_en)).join(' · ') }}</div>
              <div class="muted small" v-if="it.note">{{ t('note') }}: {{ it.note }}</div></div>
            <span class="price">{{ money(it.line_total, order.currency) }}</span>
          </div>
          </template>
          <TotalsTable :totals="order.totals" :currency="order.currency" :delivery="order.service_type === 'delivery'" />
        </div>
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
.starpick button, .starpick i { border: 0; background: none; padding: 0 3px; font-size: 26px; line-height: 1; color: #d5d8dd; cursor: pointer; font-style: normal; }
.starpick.static i { font-size: 18px; cursor: default; padding: 0 1px; }
.starpick .on { color: #f5a623; }
.step.cur { font-weight: 700; } .step.cur .dot { background: var(--brand); box-shadow: 0 0 0 4px var(--brand-soft); }
</style>
