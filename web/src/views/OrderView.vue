<script setup lang="ts">
// One order as its customer sees it: where it is, what can be done about it
// (cancel, ask for a refund, call, order again, review), what was paid, and
// where it goes. Anyone holding the link sees it; the list needs a login.
import { computed, defineAsyncComponent, nextTick, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ApiError, get, post } from '../api'
import { contactName, contactPhone } from '../addresses'
import { prettyPhone } from '../dial'
import { loadCustomer } from '../customer'
import { locale, money, pick, reasonText, setLocale, t, whenText } from '../i18n'
import { useLive } from '../live'
import { closedStatus, finished as isFinished, orderAgain, stamp } from '../orders'
import type { Order, OrderReview } from '../types'
import './storeStrings'
import TotalsTable from './TotalsTable.vue'

// Only customers whose order is on its way download the map.
const TrackRider = defineAsyncComponent(() => import('./TrackRider.vue'))

const props = defineProps<{ token: string }>()
const router = useRouter()
const route = useRoute()
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

const status = computed(() => order.value?.display_status ?? '')
const steps = computed(() => order.value?.service_type === 'pickup'
  ? ['pending_acceptance', 'preparing', 'ready_for_pickup', 'picked_up']
  : ['pending_acceptance', 'preparing', 'awaiting_rider', 'out_for_delivery', 'delivered'])
const stepIndex = computed(() => {
  const s = status.value
  return steps.value.indexOf(s === 'rider_arrived' ? 'out_for_delivery' : s === 'scheduled' ? 'preparing' : s)
})
const closed = computed(() => closedStatus(status.value))
const finished = computed(() => isFinished(status.value))
const onTheWay = computed(() => !!order.value?.delivery && ['out_for_delivery', 'rider_arrived'].includes(status.value))
const hint = computed(() => closed.value ? (order.value?.close_reason ? `${t('reason')}: ${reasonText(order.value.close_reason)}` : t('od.h.closed'))
  : finished.value ? t('od.thanks') : t('od.h.' + status.value))

// An order split into baskets is listed basket by basket.
const multi = computed(() => !!order.value?.items.some(i => i.basket > 1))
const sorted = computed(() => [...(order.value?.items ?? [])].sort((a, b) => a.basket - b.basket || a.id - b.id))
const store = computed(() => order.value?.store)
const storeName = computed(() => store.value ? pick(store.value.name, store.value.name_en) : '')

// ---- back ----
// From the list, back returns to it. Someone who came another way (right
// after ordering, or from a texted link) goes to their orders when logged in,
// to the store otherwise; the page is never a dead end.
async function back() {
  const from = String(window.history.state?.back ?? '')
  if (from.startsWith('/orders')) { router.back(); return }
  if (await loadCustomer()) router.replace('/orders')
  else if (store.value) router.replace(`/s/${store.value.slug}`)
  else if (from) router.back()
  else router.replace('/')
}

// ---- the review of a finished order ----
const review = ref<OrderReview | null>(null)
const rv = reactive({ open: false, rating: 0, taste: 0, packaging: 0, delivery: 0, comment: '', busy: false, error: '' })
const reviewCard = ref<HTMLElement>()
watch(finished, async on => {
  if (!on) return
  review.value = await get<OrderReview>(`/customer/orders/${props.token}/review`).catch(() => null)
  // "review" on a card of the list opens the form straight away
  if (route.query.review && review.value?.can_review) { rv.open = true; await nextTick(); reviewCard.value?.scrollIntoView({ block: 'center' }) }
}, { immediate: true })
const scores = computed(() => ['rating', 'taste', 'packaging', ...(order.value?.service_type === 'delivery' ? ['delivery'] : [])] as ('rating' | 'taste' | 'packaging' | 'delivery')[])
const scoreLabel = { rating: 'rv.overall', taste: 'sf.taste', packaging: 'sf.packaging', delivery: 'sf.delivery_score' }
function quickRate(n: number) { rv.rating = n; rv.open = true }
async function submitReview() {
  if (scores.value.some(k => !rv[k])) { rv.error = t('rv.pick_all'); return }
  rv.busy = true; rv.error = ''
  try {
    review.value = await post<OrderReview>(`/customer/orders/${props.token}/review`,
      { rating: rv.rating, taste: rv.taste, packaging: rv.packaging, delivery: rv.delivery, comment: rv.comment })
  } catch (e) { rv.error = (e as Error).message }
  rv.busy = false
}

// ---- cancel, refund request, order again ----
async function cancel() {
  if (!confirm(t('cancel_confirm'))) return
  error.value = ''
  try { order.value = await post<Order>(`/customer/orders/${props.token}/cancel`) }
  catch (e) { error.value = (e as Error).message }
}

const reasons = ['wrong_item', 'missing_item', 'quality', 'late', 'not_received', 'changed_mind', 'other']
const as = reactive({ open: false, reason: '', detail: '', busy: false, error: '' })
// The store takes requests on orders it accepted, for 7 days after ordering.
const canAftersale = computed(() => !!order.value && order.value.acceptance_status === 'accepted' && !order.value.aftersale &&
  Date.now() - new Date(order.value.created_at).getTime() < 7 * 86400000)
watch(canAftersale, on => { if (on && route.query.aftersale) as.open = true }, { immediate: true })
async function sendAftersale() {
  if (!as.reason || (as.reason === 'other' && !as.detail.trim())) { as.error = t(as.reason ? 'as.detail_ph' : 'as.reason'); return }
  as.busy = true; as.error = ''
  try {
    order.value = await post<Order>(`/customer/orders/${props.token}/aftersale`, { reason: as.reason, detail: as.detail })
    as.open = false
  } catch (e) { as.error = (e as Error).message }
  as.busy = false
}

const againBusy = ref(false)
async function again() {
  againBusy.value = true; error.value = ''
  try { const note = await orderAgain(router, props.token); if (note) window.alert(note) }
  catch (e) { error.value = (e as Error).message }
  againBusy.value = false
}

const copied = ref(false)
async function copyNo() {
  try { await navigator.clipboard.writeText(order.value!.public_no); copied.value = true; setTimeout(() => (copied.value = false), 1500) } catch { /* no clipboard */ }
}
</script>

<template>
  <div class="od" data-testid="order-page">
    <header class="od-bar">
      <button class="od-back" :aria-label="t('back')" data-testid="order-back" @click="back">
        <svg viewBox="0 0 24 24" width="24" height="24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M15 5l-7 7 7 7" /></svg>
        <span class="od-backtext">{{ t('od.all_orders') }}</span></button>
      <button class="od-lang lang" @click="setLocale(locale === 'zh' ? 'en' : 'zh')">{{ locale === 'zh' ? 'EN' : '中文' }}</button>
    </header>

    <div v-if="missing" class="od-wrap"><div class="banner bad">{{ t('not_found') }}</div></div>
    <div v-else-if="!order" class="od-wrap muted">{{ t('loading') }}</div>
    <div v-else class="od-wrap">
      <section class="od-top">
        <h1 :class="{ bad: closed }" data-testid="status">{{ finished ? t('od.done_title') + ' · ' : '' }}{{ t('s.' + status) }}</h1>
        <p class="muted">{{ hint }}</p>
        <p v-if="order.scheduled_for" class="bold" data-testid="scheduled">{{ t(order.service_type === 'delivery' ? 'deliver_at' : 'pickup_at') }} {{ whenText(order.scheduled_for, order.timezone) }}</p>
        <div class="od-acts" data-testid="order-actions">
          <button v-if="status === 'pending_acceptance'" class="pill" data-testid="act-cancel" @click="cancel">{{ t('cancel_order') }}</button>
          <button v-if="canAftersale" class="pill" data-testid="act-aftersale" @click="as.open = true">{{ t('as.apply') }}</button>
          <a v-if="onTheWay && order.delivery?.rider_phone" class="pill" data-testid="act-rider" :href="'tel:' + order.delivery.rider_phone">{{ t('od.call_rider') }}</a>
          <a v-if="store?.phone" class="pill" data-testid="act-store" :href="'tel:' + store.phone">{{ t('od.call_store') }}</a>
          <button class="pill brand" data-testid="act-again" :disabled="againBusy" @click="again">{{ t('ol.again') }}</button>
        </div>
      </section>

      <div v-if="!online" class="banner warn">{{ t('offline') }}</div>
      <div v-if="error" class="banner bad">{{ error }}</div>

      <div class="od-cols">
        <div class="od-col">
          <section v-if="!closed && !finished" class="od-card">
            <ol class="steps">
              <li v-for="(s, i) in steps" :key="s" :class="{ done: i <= stepIndex, cur: i === stepIndex }"><span class="dot"></span><span class="small">{{ t('s.' + s) }}</span></li>
            </ol>
            <div v-if="order.service_type === 'pickup'" class="od-line"><span class="muted">{{ t('pickup_code') }}</span><b class="code">{{ order.pickup_code }}</b></div>
            <div v-if="onTheWay" class="od-line"><span class="muted">{{ t('rider') }}</span>
              <span>{{ order.delivery!.rider_name }} <a v-if="order.delivery!.rider_phone" :href="'tel:' + order.delivery!.rider_phone">{{ order.delivery!.rider_phone }}</a></span></div>
          </section>

          <TrackRider v-if="onTheWay" :token="token" />

          <section v-if="order.aftersale" class="od-card" data-testid="aftersale" :data-status="order.aftersale.status">
            <div class="od-line"><h2>{{ t('as.' + order.aftersale.status) }}</h2><span class="muted small">{{ stamp(order.aftersale.created_at, order.timezone) }}</span></div>
            <p><span class="muted">{{ t('as.yours') }}：</span>{{ t('as.r.' + order.aftersale.reason) }}<template v-if="order.aftersale.detail"> · {{ order.aftersale.detail }}</template></p>
            <p v-if="order.aftersale.reply" data-testid="aftersale-reply"><span class="muted">{{ t('as.reply') }}：</span>{{ order.aftersale.reply }}</p>
            <p v-if="order.aftersale.status === 'open'" class="muted small">{{ t('as.wait') }}</p>
          </section>

          <section v-if="review?.can_review" ref="reviewCard" class="od-card" data-testid="review-card">
            <div class="od-line" data-testid="rate-quick">
              <span>{{ t('od.satisfied') }}</span>
              <span class="starpick"><button v-for="n in 5" :key="n" type="button" :class="{ on: n <= rv.rating }" :aria-label="`${n}`" @click="quickRate(n)">★</button></span>
            </div>
            <form v-if="rv.open" class="od-form" data-testid="review-form" @submit.prevent="submitReview">
              <div v-if="rv.error" class="banner bad">{{ rv.error }}</div>
              <div v-for="k in scores" :key="k" class="od-line" :data-score="k">
                <span>{{ t(scoreLabel[k]) }}</span>
                <span class="starpick"><button v-for="n in 5" :key="n" type="button" :class="{ on: n <= rv[k] }" :aria-label="`${n}`" @click="rv[k] = n">★</button></span>
              </div>
              <textarea v-model="rv.comment" rows="3" maxlength="500" :placeholder="t('rv.comment_ph')"></textarea>
              <button class="btn primary block" :disabled="rv.busy">{{ t('rv.submit') }}</button>
            </form>
          </section>
          <section v-else-if="review?.review" class="od-card" data-testid="review-done">
            <div class="od-line"><h2>{{ t('rv.done') }}</h2><span class="starpick static"><i v-for="n in 5" :key="n" :class="{ on: n <= review.review.rating }">★</i></span></div>
            <p v-if="review.review.comment" style="white-space:pre-wrap">{{ review.review.comment }}</p>
            <p class="muted small">{{ t('rv.shown_as', { name: review.review.display_name }) }}</p>
          </section>

        </div>

        <section class="od-card od-side" data-testid="order-fees">
          <h2>{{ t('od.fees') }}</h2>
          <a v-if="store" class="od-store" :href="`/s/${store.slug}`" data-testid="order-store" @click.prevent="router.push(`/s/${store.slug}`)">
            <img v-if="store.logo_url" :src="store.logo_url" alt="" /><span v-else class="ph">{{ storeName.slice(0, 1) }}</span>
            <span class="grow">{{ storeName }}</span>
            <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 5l7 7-7 7" /></svg>
          </a>
          <template v-for="(it, i) in sorted" :key="it.id">
            <div v-if="multi && (i === 0 || sorted[i - 1].basket !== it.basket)" class="muted small bold" data-testid="basket-head">{{ t('basket_n', { n: it.basket }) }}</div>
            <div class="od-item">
              <img v-if="it.image_url" :src="it.image_url" alt="" loading="lazy" /><span v-else class="ph">{{ pick(it.name, it.name_en).slice(0, 1) }}</span>
              <div class="grow">
                <div class="od-iname">{{ pick(it.name, it.name_en) }}</div>
                <div class="muted small" v-if="it.options.length">{{ it.options.map(o => pick(o.name, o.name_en)).join(' · ') }}</div>
                <div class="muted small" v-if="it.note">{{ t('note') }}: {{ it.note }}</div>
                <div class="muted small">x{{ it.qty }}</div>
              </div>
              <span class="price">{{ money(it.line_total, order.currency) }}</span>
            </div>
          </template>
          <TotalsTable :totals="order.totals" :currency="order.currency" :delivery="order.service_type === 'delivery'" />
        </section>

        <section class="od-card od-infocard" data-testid="order-info">
          <h2>{{ t('od.info') }}</h2>
          <dl class="od-info">
            <template v-if="order.service_type === 'delivery'">
              <dt>{{ t('od.address') }}</dt>
              <dd><div>{{ order.address_line }} {{ order.address_unit }}</div>
                <div class="muted">{{ contactName(order) }} {{ prettyPhone(contactPhone(order)) }}</div>
                <div v-if="order.address_note" class="muted small">{{ order.address_note }}</div></dd>
            </template>
            <template v-else-if="store">
              <dt>{{ t('od.pickup_at_store') }}</dt>
              <dd><div>{{ storeName }}</div><div class="muted">{{ store.address }}</div></dd>
            </template>
            <dt>{{ t('od.no') }}</dt>
            <dd><span data-testid="order-no">{{ order.public_no }}</span> <button class="od-copy" @click="copyNo">{{ t(copied ? 'od.copied' : 'od.copy') }}</button></dd>
            <dt>{{ t('ol.ordered_at') }}</dt><dd>{{ stamp(order.created_at, order.timezone) }}</dd>
            <template v-if="order.completed_at && finished"><dt>{{ t('od.done_at') }}</dt><dd>{{ stamp(order.completed_at, order.timezone) }}</dd></template>
            <dt>{{ t('od.service') }}</dt><dd>{{ t(order.service_type === 'delivery' ? 'od.store_delivery' : 'od.self_pickup') }}</dd>
            <dt>{{ t('od.wanted') }}</dt>
            <dd>{{ order.scheduled_for ? whenText(order.scheduled_for, order.timezone) : t(order.service_type === 'delivery' ? 'od.asap' : 'od.asap_pickup') }}</dd>
            <dt>{{ t('pay_method') }}</dt>
            <dd>{{ t(order.payment_method) }} · <span class="tag" :class="order.payment_status === 'paid' ? 'ok' : 'warn'">{{ t(order.payment_status) }}</span></dd>
            <template v-if="order.note"><dt>{{ t('order_note') }}</dt><dd>{{ order.note }}</dd></template>
          </dl>
        </section>
      </div>
    </div>

    <div v-if="as.open" class="sheet-mask" data-testid="aftersale-sheet" @click.self="as.open = false">
      <form class="sheet" @submit.prevent="sendAftersale">
        <div class="sheet-head row between"><h2>{{ t('as.title') }}</h2><button type="button" class="btn link" @click="as.open = false">{{ t('close') }}</button></div>
        <div class="sheet-body stack">
          <div class="banner info">{{ t('as.cod_note') }}</div>
          <div v-if="as.error" class="banner bad">{{ as.error }}</div>
          <div>
            <div class="muted small" style="margin-bottom:6px">{{ t('as.reason') }}</div>
            <label v-for="r in reasons" :key="r" class="check"><input v-model="as.reason" type="radio" name="as-reason" :value="r" />{{ t('as.r.' + r) }}</label>
          </div>
          <textarea v-model="as.detail" rows="3" maxlength="500" :placeholder="t('as.detail_ph')"></textarea>
        </div>
        <div class="sheet-foot"><button class="btn primary block" :disabled="as.busy">{{ t('as.submit') }}</button></div>
      </form>
    </div>
  </div>
</template>

<style scoped>
.od { min-height: 100vh; padding-bottom: calc(env(safe-area-inset-bottom) + 28px); }
.od-bar, .od-wrap { max-width: 760px; margin: 0 auto; }
.od-bar { display: flex; align-items: center; justify-content: space-between; padding: 10px 12px 2px; position: sticky; top: 0; z-index: 10; background: var(--bg); }
.od-back { border: 0; background: none; padding: 6px 8px 6px 2px; margin-left: -2px; cursor: pointer; display: flex; align-items: center; gap: 2px; border-radius: 999px; }
.od-backtext { display: none; font-weight: 600; }
.od-lang { border: 1px solid var(--line); background: #fff; border-radius: 999px; padding: 6px 12px; font-size: 13px; font-weight: 600; cursor: pointer; }
.od-wrap { padding: 4px 12px 0; display: grid; gap: 10px; }
.od-top { padding: 2px 6px 4px; display: grid; gap: 6px; }
.od-top h1 { font-size: 24px; } .od-top h1.bad { color: var(--bad); }
.od-acts { display: flex; gap: 8px; margin-top: 8px; overflow-x: auto; scrollbar-width: none; padding-bottom: 2px; }
.od-acts::-webkit-scrollbar { display: none; }
.pill { display: inline-flex; align-items: center; border: 1px solid #d1d5db; background: #fff; border-radius: 999px; padding: 8px 16px; cursor: pointer; white-space: nowrap; color: var(--ink); font-weight: 600; min-height: 40px; flex: none; }
.pill.brand { border-color: var(--brand); color: var(--brand-dark); margin-left: auto; }
.pill:disabled { opacity: .5; }
.od-cols, .od-col { display: grid; gap: 10px; min-width: 0; align-content: start; }
.od-col:empty { display: none; }
.od-card { background: #fff; border-radius: 12px; padding: 14px; display: grid; gap: 10px; min-width: 0; }
.od-line { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.code { font-size: 24px; }
.steps { list-style: none; margin: 0; padding: 0; display: flex; }
.steps li { flex: 1; display: grid; justify-items: center; gap: 6px; text-align: center; color: var(--muted); position: relative; padding: 0 2px; }
.steps li + li::before { content: ""; position: absolute; top: 5px; right: 50%; width: 100%; height: 2px; background: var(--line); }
.steps li.done + li.done::before, .steps li.done:not(:first-child)::before { background: var(--ok); }
.steps .dot { width: 12px; height: 12px; border-radius: 50%; background: var(--line); position: relative; z-index: 1; }
.steps li.done { color: var(--ink); } .steps li.done .dot { background: var(--ok); }
.steps li.cur { font-weight: 700; } .steps li.cur .dot { background: var(--brand); box-shadow: 0 0 0 4px var(--brand-soft); }
.starpick button, .starpick i { border: 0; background: none; padding: 0 3px; font-size: 26px; line-height: 1; color: #d5d8dd; cursor: pointer; font-style: normal; }
.starpick.static i { font-size: 18px; cursor: default; padding: 0 1px; }
.starpick .on { color: #f5a623; }
.od-form { display: grid; gap: 10px; border-top: 1px solid var(--line); padding-top: 10px; }
.od-store { display: flex; align-items: center; gap: 8px; background: var(--bg); border-radius: 8px; padding: 8px 10px; color: var(--ink); }
.od-store img, .od-store .ph { width: 22px; height: 22px; border-radius: 6px; object-fit: cover; flex: none; }
.od-store .ph, .od-item .ph { background: var(--brand-soft); color: var(--brand-dark); font-weight: 700; display: grid; place-items: center; }
.od-store svg { color: var(--muted); flex: none; }
.od-item { display: flex; gap: 10px; align-items: flex-start; }
.od-item img, .od-item .ph { width: 56px; height: 56px; border-radius: 8px; object-fit: cover; flex: none; font-size: 20px; }
.od-iname { font-weight: 600; overflow-wrap: anywhere; }
.od-info { margin: 0; display: grid; grid-template-columns: max-content minmax(0, 1fr); gap: 10px 14px; }
.od-info dt { color: var(--muted); } .od-info dd { margin: 0; overflow-wrap: anywhere; }
.od-copy { border: 1px solid var(--line); background: #fff; border-radius: 999px; padding: 1px 10px; font-size: 12px; cursor: pointer; margin-left: 6px; }
@media (min-width: 768px) { .od-bar { padding-top: 16px; } .od-backtext { display: inline; } .od-wrap { gap: 12px; } }
/* a computer puts what was bought beside the state of the order */
@media (min-width: 1024px) {
  .od-bar, .od-wrap { max-width: 1100px; }
  .od-cols { grid-template-columns: minmax(0, 1fr) minmax(340px, 420px); gap: 14px; align-items: start; }
  .od-col, .od-infocard { grid-column: 1; }
  .od-side { grid-column: 2; grid-row: 1 / span 2; position: sticky; top: 64px; }
}
</style>
