<script setup lang="ts">
// One order as its customer sees it: where it is, what can be done about it
// (cancel, ask for a refund, call, order again, review), what was paid, and
// where it goes. Anyone holding the link sees it; the list needs a login.
import { storePath } from '../site'
import { tagNames } from '../notes'
import { fromLineOptions } from '../extras'
import LineExtras from './LineExtras.vue'
import LangButton from './LangButton.vue'
import { computed, defineAsyncComponent, nextTick, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ApiError, api, get, post } from '../api'
import { contactName, contactPhone } from '../addresses'
import { prettyPhone } from '../dial'
import { loadCustomer } from '../customer'
import { locale, money, pick, reasonText, setLocale, t, whenText, mn, sn } from '../i18n'
import { useLive } from '../live'
import { closedStatus, finished as isFinished, orderAgain, stamp } from '../orders'
import type { Order, OrderReview } from '../types'
import './storeStrings'
import TotalsTable from './TotalsTable.vue'
import CustomerFrame from './me/CustomerFrame.vue'
import { couponTitle, lastDay } from '../coupons'
import '../voids'
import { chatEnabled } from '../chat'
import { addonErr, batchTitle, pendingAddon } from '../addon'

// The "contact store" entry shows only where chat is set up.
const chatOn = ref(false)
void chatEnabled().then(v => { chatOn.value = v })

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
const sorted = computed(() => [...(order.value?.items ?? [])].sort((a, b) => a.basket - b.basket || a.batch_seq - b.batch_seq || a.id - b.id))
const store = computed(() => order.value?.store)
const storeName = computed(() => store.value ? sn(store.value) : '')

// ---- back ----
// From the list, back returns to it. Someone who came another way (right
// after ordering, or from a texted link) goes to their orders when logged in,
// to the store otherwise; the page is never a dead end.
async function back() {
  const from = String(window.history.state?.back ?? '')
  if (from.startsWith('/orders')) { router.back(); return }
  if (await loadCustomer()) router.replace('/orders')
  else if (store.value) router.replace(storePath(store.value.slug))
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

// ---- how it looks ----
// A drawing for where the order is, the count of things in it and the icons of
// the actions (24×24 paths).
const ICONS = {
  clock: 'M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18zM12 7.500V12l3 2',
  bowl: 'M4 11.500h16a8 8 0 0 1-16 0zM9 4.500c-.8.9.8 1.7 0 2.600M12 4.500c-.8.9.8 1.700 0 2.600M15 4.500c-.8.9.8 1.700 0 2.600',
  bag: 'M5.500 8.500h13l-1 11.500h-11zM9 8.500V7a3 3 0 0 1 6 0v1.500',
  ride: 'M3.500 16.500a2.500 2.500 0 1 0 5 0 2.500 2.500 0 0 0-5 0zM15.500 16.500a2.500 2.500 0 1 0 5 0 2.500 2.500 0 0 0-5 0zM8.500 16.500h7M13.500 6.500h3l2.500 7.500M6 10h5l2.200 6.500',
  done: 'M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18zM8 12.300l2.800 2.800 5.200-5.600',
  shut: 'M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18zM9 9l6 6M15 9l-6 6',
  back: 'M4.500 12a7.500 7.500 0 1 0 2.200-5.300M4.500 4.500v3.700h3.700',
  phone: 'M6 3.500h3l1.500 4-2 1.300a10.500 10.500 0 0 0 6.700 6.700l1.300-2 4 1.500v3a1.500 1.500 0 0 1-1.600 1.500A16.500 16.500 0 0 1 4.500 5.100 1.500 1.500 0 0 1 6 3.500z',
  chat: 'M4.500 6.500A2.500 2.500 0 0 1 7 4h10a2.500 2.500 0 0 1 2.500 2.500v7A2.500 2.500 0 0 1 17 16h-5l-4.500 4v-4H7a2.500 2.500 0 0 1-2.500-2.500zM8.500 10h.01M12 10h.01M15.500 10h.01',
  again: 'M19.500 12a7.500 7.500 0 0 1-13.200 4.900M4.500 12a7.500 7.500 0 0 1 13.200-4.900M18 3.500v3.700h-3.700M6 20.500v-3.700h3.700',
}
const stateIcon = computed(() => {
  const s = status.value
  if (closed.value) return ICONS.shut
  if (finished.value) return ICONS.done
  if (s === 'preparing' || s === 'scheduled') return ICONS.bowl
  if (s === 'awaiting_rider' || s === 'ready_for_pickup') return ICONS.bag
  if (s === 'out_for_delivery' || s === 'rider_arrived') return ICONS.ride
  return ICONS.clock
})
// what the store took off and gave back (docs/VOID_ITEM_DESIGN.md §8.6)
const lastVoid = computed(() => (order.value?.voids ?? []).slice(-1)[0] ?? null)
const firstTotal = computed(() => order.value?.voids?.[0]?.total_before ?? null)
const itemCount = computed(() => order.value?.items.reduce((n, i) => n + i.qty, 0) ?? 0)

// ---- adding dishes (docs/ADD_ON_ORDER_DESIGN.md §8) ----
const canAdd = computed(() => !!order.value?.addon?.open && order.value.addon.customer_left > 0 && !order.value.addon.pending && !!store.value)
const waitingAdd = computed(() => order.value ? pendingAddon(order.value) : null)
// the last add-on the store turned down or let drop, said once under the state
const droppedAdd = computed(() => {
  const bs = order.value?.batches ?? []
  const last = bs[bs.length - 1]
  return last && last.seq > 1 && (last.status === 'rejected' || last.status === 'expired') ? last : null
})
const grouped = computed(() => (order.value?.batches ?? []).filter(b => b.status === 'accepted').length > 1)
const addMins = computed(() => waitingAdd.value?.expires_at ? Math.max(1, Math.ceil((new Date(waitingAdd.value.expires_at).getTime() - Date.now()) / 60000)) : 0)
function addDishes() { router.push({ path: storePath(store.value!.slug), query: { append: props.token } }) }
async function withdraw() {
  error.value = ''
  try { order.value = await api<Order>('DELETE', `/customer/orders/${props.token}/append/${waitingAdd.value!.seq}`) }
  catch (e) { error.value = addonErr(e) }
}

const copied = ref(false)
async function copyNo() {
  try { await navigator.clipboard.writeText(order.value!.public_no); copied.value = true; setTimeout(() => (copied.value = false), 1500) } catch { /* no clipboard */ }
}
</script>

<template>
  <div class="od warm" data-testid="order-page">
   <CustomerFrame active="orders">
    <!-- the red bar of every customer page; on an iPad or a computer the red
         bar above (CustomerNav) takes its place and this is the heading -->
    <header class="od-bar">
      <button class="od-back" :aria-label="t('back')" data-testid="order-back" @click="back">
        <svg class="rtl-flip" viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M15 5l-7 7 7 7" /></svg></button>
      <b>{{ t('ol.detail') }}</b>
      <span class="od-lang"><LangButton /></span>
    </header>

    <div v-if="missing" class="od-wrap"><div class="banner bad">{{ t('not_found') }}</div></div>
    <div v-else-if="!order" class="od-wrap muted">{{ t('loading') }}</div>
    <div v-else class="od-wrap">
      <div class="od-cols">
        <div class="od-col">
          <!-- where it is, and what can be done about it -->
          <section class="od-card od-now" :class="{ closed, done: finished }" data-testid="order-state">
            <div class="od-state">
              <span class="od-stateic" aria-hidden="true"><svg viewBox="0 0 24 24" width="28" height="28" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path :d="stateIcon" /></svg></span>
              <div class="od-title">
                <div class="od-tline">
                  <h1 data-testid="status">{{ finished ? t('od.done_title') + ' · ' : '' }}{{ t('s.' + status) }}</h1>
                  <span class="paid" :class="order.payment_status === 'paid' ? 'ok' : 'warn'" data-testid="state-paid">{{ t(order.payment_status) }}</span>
                </div>
                <p>{{ hint }}</p>
                <p v-if="order.scheduled_for" class="od-sched" data-testid="scheduled">{{ t(order.service_type === 'delivery' ? 'deliver_at' : 'pickup_at') }} {{ whenText(order.scheduled_for, order.timezone) }}</p>
              </div>
              <div v-if="order.service_type === 'pickup' && !closed && !finished" class="od-code">
                <small>{{ t('pickup_code') }}</small><b>{{ order.pickup_code }}</b>
              </div>
            </div>
            <ol v-if="!closed && !finished" class="steps">
              <li v-for="(s, i) in steps" :key="s" :class="{ done: i < stepIndex, cur: i === stepIndex }">
                <span class="dot"><svg v-if="i < stepIndex" viewBox="0 0 24 24" width="10" height="10" fill="none" stroke="currentColor" stroke-width="3.4" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12.500l4.500 4.500L19 7.500" /></svg></span>
                <span class="lbl">{{ t('s.' + s) }}</span></li>
            </ol>
            <div v-if="onTheWay" class="od-rider">
              <span class="ava"><svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path :d="ICONS.ride" /></svg></span>
              <span class="grow"><small>{{ t('rider') }}</small><b>{{ order.delivery!.rider_name }}</b></span>
              <a v-if="order.delivery!.rider_phone" :href="'tel:' + order.delivery!.rider_phone">{{ order.delivery!.rider_phone }}</a>
            </div>
            <!-- the actions: outlined, ordering again in solid red -->
            <div class="od-acts" data-testid="order-actions">
              <button v-if="status === 'pending_acceptance'" class="btn act" data-testid="act-cancel" @click="cancel">
                <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"><path :d="ICONS.shut" /></svg>{{ t('cancel_order') }}</button>
              <button v-if="canAftersale" class="btn act" data-testid="act-aftersale" @click="as.open = true">
                <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"><path :d="ICONS.back" /></svg>{{ t('as.apply') }}</button>
              <a v-if="onTheWay && order.delivery?.rider_phone" class="btn act" data-testid="act-rider" :href="'tel:' + order.delivery.rider_phone">
                <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"><path :d="ICONS.phone" /></svg>{{ t('od.call_rider') }}</a>
              <router-link v-if="chatOn && store" class="btn act" data-testid="act-chat" :to="`/chat/${store.slug}`">
                <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"><path :d="ICONS.chat" /></svg>{{ t('ch.contact_store') }}</router-link>
              <button class="btn act primary" data-testid="act-again" :disabled="againBusy" @click="again">
                <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path :d="ICONS.again" /></svg>{{ t('ol.again') }}</button>
            </div>
          </section>

          <div v-if="!online" class="banner warn">{{ t('offline') }}</div>
          <div v-if="error" class="banner bad">{{ error }}</div>

          <button v-if="canAdd" class="od-card od-add" data-testid="act-addon" @click="addDishes">
            <b>{{ t('ao.c.add') }}</b><span>{{ t(order.service_type === 'pickup' ? 'ao.c.hint_p' : 'ao.c.hint_d') }}</span>
            <small v-if="(order.addon?.customer_left ?? 0) < 3">{{ t('ao.c.left', { n: order.addon?.customer_left ?? 0 }) }}</small>
          </button>
          <section v-if="waitingAdd" class="od-card od-addwait" data-testid="addon-waiting">
            <div class="od-head"><h2>{{ batchTitle(waitingAdd.seq) }} · {{ t('ao.st.pending') }}</h2>
              <button class="btn small" data-testid="addon-withdraw" @click="withdraw">{{ t('ao.c.withdraw') }}</button></div>
            <div v-for="(l, i) in waitingAdd.lines" :key="i" class="od-line"><span>{{ mn(l) }} ×{{ l.qty }}</span><span>{{ money(l.line_total, order.currency) }}</span></div>
            <p class="muted small">{{ money(waitingAdd.total_before ?? 0, order.currency) }} → <b>{{ money(waitingAdd.total_after ?? 0, order.currency) }}</b> ·
              {{ t('ao.c.wait_note', { n: addMins }) }}</p>
          </section>
          <div v-else-if="droppedAdd" class="banner warn" data-testid="addon-dropped">{{ batchTitle(droppedAdd.seq) }} · {{ t('ao.st.' + droppedAdd.status) }}<template v-if="droppedAdd.reject_reason">（{{ droppedAdd.reject_reason }}）</template></div>

          <div v-if="lastVoid" class="banner warn" data-testid="void-banner">{{ t('vd.c.banner', { n: lastVoid.qty, dish: lastVoid.name, r: lastVoid.reason, s: money(lastVoid.total_before - lastVoid.total_after, order.currency) }) }}</div>
          <div v-if="order.refunded" class="banner info" data-testid="refund-banner">{{ t('vd.c.refund', { s: money(order.refunded, order.currency) }) }}</div>

          <router-link v-if="order.gift" to="/me/coupons" class="od-gift" data-testid="order-gift">
            <span class="ic">🎁</span>
            <span class="grow"><b>{{ t('cp.gift', { what: couponTitle(order.gift, order.gift.currency) }) }}</b>
              <span>{{ t('cp.gift_sub', { store: pick(order.gift.store_name, order.gift.store_name_en) || t('cp.all_stores'), d: lastDay(order.gift.expires_at) }) }}</span></span>
            <span class="chev"><span class="rtl-flip">›</span></span>
          </router-link>

          <TrackRider v-if="onTheWay" :token="token" />

          <section v-if="order.aftersale" class="od-card od-as" data-testid="aftersale" :data-status="order.aftersale.status">
            <div class="od-head"><h2>{{ t('as.' + order.aftersale.status) }}</h2><span class="muted small">{{ stamp(order.aftersale.created_at, order.timezone) }}</span></div>
            <p><span class="muted">{{ t('as.yours') }}：</span>{{ t('as.r.' + order.aftersale.reason) }}<template v-if="order.aftersale.detail"> · {{ order.aftersale.detail }}</template></p>
            <p v-if="order.aftersale.reply" class="od-reply" data-testid="aftersale-reply"><span class="muted">{{ t('as.reply') }}：</span>{{ order.aftersale.reply }}</p>
            <p v-if="order.aftersale.status === 'open'" class="muted small">{{ t('as.wait') }}</p>
          </section>

          <section v-if="review?.can_review" ref="reviewCard" class="od-card od-rate" data-testid="review-card">
            <div class="od-line" data-testid="rate-quick">
              <span class="ask">{{ t('od.satisfied') }}</span>
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
          <section v-else-if="review?.review" class="od-card od-rate" data-testid="review-done">
            <div class="od-line"><h2>{{ t('rv.done') }}</h2><span class="starpick static"><i v-for="n in 5" :key="n" :class="{ on: n <= review.review.rating }">★</i></span></div>
            <p v-if="review.review.comment" style="white-space:pre-wrap">{{ review.review.comment }}</p>
            <p class="muted small">{{ t('rv.shown_as', { name: review.review.display_name }) }}</p>
          </section>
        </div>

        <!-- what was bought: the store, the dishes, the sums -->
        <section class="od-card od-side od-receipt" data-testid="order-fees">
          <a v-if="store" class="od-store" :href="storePath(store.slug)" data-testid="order-store" @click.prevent="router.push(storePath(store.slug))">
            <img v-if="store.logo_url" :src="store.logo_url" alt="" />
            <span v-else class="ph"><svg viewBox="0 0 24 24" width="26" height="26" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path :d="ICONS.bowl" /></svg></span>
            <span class="grow"><b>{{ storeName }}</b><small>{{ t('od.fees') }} · {{ t('ol.n_items', { n: itemCount }) }}</small></span>
            <svg class="rtl-flip chev" viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 5l7 7-7 7" /></svg>
          </a>
          <h2 v-else>{{ t('od.fees') }}</h2>
          <div class="od-items">
            <template v-for="(it, i) in sorted" :key="it.id">
              <div v-if="multi && (i === 0 || sorted[i - 1].basket !== it.basket)" class="od-basket" data-testid="basket-head">{{ t('basket_n', { n: it.basket }) }}</div>
              <div v-else-if="!multi && grouped && (i === 0 || sorted[i - 1].batch_seq !== it.batch_seq)" class="od-basket" data-testid="addon-head">{{ batchTitle(it.batch_seq) }}</div>
              <div class="od-item" :class="{ voided: it.prep_status === 'cancelled' }">
                <img v-if="it.image_url" :src="it.image_url" alt="" loading="lazy" /><span v-else class="ph">{{ mn(it).slice(0, 1) }}</span>
                <div class="grow">
                  <div class="od-nrow"><span class="od-iname">{{ mn(it) }}</span><span class="price">{{ money(it.line_total, order.currency) }}</span></div>
                  <LineExtras v-bind="fromLineOptions(it.options, it.unit_price)" :flavors="tagNames(it.note_tags, true)" :note="it.note?.trim() ?? ''" :cur="order.currency" tid="od" />
                  <div v-if="it.prep_status === 'cancelled'" class="voidtag" data-testid="od-voided">{{ t('vd.c.struck') }}<template v-if="it.void_reason"> · {{ it.void_reason }}</template></div>
                  <div class="qty">×{{ it.qty }}</div>
                </div>
              </div>
            </template>
          </div>
          <div class="od-sums"><TotalsTable :totals="order.totals" :currency="order.currency" :delivery="order.service_type === 'delivery'" :discounts="order.discounts" :credit-paid="order.credit_paid" />
            <div v-if="firstTotal != null && firstTotal !== order.totals.total" class="od-was" data-testid="od-was">{{ t('vd.c.was', { s: money(firstTotal, order.currency) }) }}</div></div>
        </section>

        <section class="od-card od-infocard" data-testid="order-info">
          <h2>{{ t('od.info') }}</h2>
          <div v-if="order.service_type === 'delivery'" class="od-where">
            <div><small><svg class="pin" viewBox="0 0 24 24" width="15" height="15" aria-hidden="true"><path fill="currentColor" d="M12 2.500a7 7 0 0 0-7 7c0 5.100 7 12 7 12s7-6.900 7-12a7 7 0 0 0-7-7zm0 9.600a2.600 2.600 0 1 1 0-5.200 2.600 2.600 0 0 1 0 5.200z" /></svg>{{ t('od.address') }}</small>
              <b>{{ order.address_line }}</b>
              <b v-if="order.address_unit">{{ order.address_unit }}</b>
              <span>{{ contactName(order) }}<i v-if="contactName(order)" class="gap"></i>{{ prettyPhone(contactPhone(order)) }}</span>
              <span v-if="order.address_note" class="note">{{ order.address_note }}</span></div>
          </div>
          <div v-else-if="store" class="od-where">
            <div><small><svg class="pin" viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path :d="ICONS.bag" /></svg>{{ t('od.pickup_at_store') }}</small><b>{{ storeName }}</b><span>{{ store.address }}</span></div>
          </div>
          <dl class="od-info">
            <div><dt>{{ t('od.no') }}</dt>
              <dd><span data-testid="order-no">{{ order.public_no }}</span><button class="od-copy" @click="copyNo">{{ t(copied ? 'od.copied' : 'od.copy') }}</button></dd></div>
            <div><dt>{{ t('ol.ordered_at') }}</dt><dd>{{ stamp(order.created_at, order.timezone) }}</dd></div>
            <div v-if="order.completed_at && finished"><dt>{{ t('od.done_at') }}</dt><dd>{{ stamp(order.completed_at, order.timezone) }}</dd></div>
            <div><dt>{{ t('od.service') }}</dt><dd>{{ t(order.service_type === 'delivery' ? 'od.store_delivery' : 'od.self_pickup') }}</dd></div>
            <div><dt>{{ t('od.wanted') }}</dt>
              <dd>{{ order.scheduled_for ? whenText(order.scheduled_for, order.timezone) : t(order.service_type === 'delivery' ? 'od.asap' : 'od.asap_pickup') }}</dd></div>
            <div><dt>{{ t('pay_method') }}</dt>
              <dd>{{ t(order.payment_method) }}<span class="paid" :class="order.payment_status === 'paid' ? 'ok' : 'warn'">{{ t(order.payment_status) }}</span></dd></div>
            <div v-if="order.note"><dt>{{ t('order_note') }}</dt><dd>{{ order.note }}</dd></div>
          </dl>
        </section>
      </div>
    </div>
   </CustomerFrame>

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
.od-add { display: flex; flex-direction: column; align-items: flex-start; gap: 2px; width: 100%; text-align: start; cursor: pointer;
  border: 1.5px dashed var(--gold, #c9a24a); background: #fffaf0; color: var(--red, #8f251b); font: inherit; }
.od-add b { font-size: 16px; } .od-add span { font-size: 12.5px; color: #7a5b3a; } .od-add small { color: #b45309; }
.od-addwait { border-inline-start: 4px solid #f59e0b; }
.od-addwait .od-line { display: flex; justify-content: space-between; font-size: 14px; padding: 2px 0; }
/* Deep red, cream and gold like the rest of the customer's pages. Sizes: body
   14, notes 12, card titles 15–16, the state 20; weights up to 600/700. */
.od { min-height: 100vh; font-size: 14px; padding-bottom: calc(env(safe-area-inset-bottom) + 28px); background: var(--bg); }

/* the red bar: back, the page's name in the middle, the language */
.od-bar { position: sticky; top: 0; z-index: 10; display: grid; grid-template-columns: minmax(48px, auto) minmax(0, 1fr) minmax(48px, auto); align-items: center; height: 52px; padding-inline-end: 10px; background: var(--red-bg); color: #fff; }
.od-back { justify-self: start; border: 0; background: none; color: inherit; height: 52px; width: 48px; display: grid; place-items: center; cursor: pointer; }
.od-bar > b { text-align: center; font-size: 17px; font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.od-lang { justify-self: end; }
.od-lang :deep(.lang-btn) { border: 0; background: none; color: #fff; height: 32px; padding: 0 4px; font-size: 14px; cursor: pointer; }

.od-wrap { padding: 12px; }
.od-cols, .od-col { display: grid; gap: 12px; min-width: 0; align-content: start; }
.od-col:empty { display: none; }
.od-card { background: var(--paper); border: 1px solid #efe2cf; border-radius: 14px; padding: 14px; display: grid; gap: 12px; min-width: 0; box-shadow: 0 3px 12px rgba(90, 40, 15, .06); }
.od-card h2 { font-size: 16px; font-weight: 700; }
.od-line, .od-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; }

/* the state: an icon, what it is, why, and whether it is paid */
.od-state { display: flex; align-items: center; gap: 12px; }
.od-stateic { flex: none; width: 46px; height: 46px; border-radius: 50%; display: grid; place-items: center; color: var(--red); background: var(--gold-soft); border: 2px solid var(--gold); }
.od-now.done .od-stateic { color: #2f7a3f; background: #e3f1e5; border-color: #9fd0a8; }
.od-title { flex: 1; min-width: 0; display: grid; gap: 3px; }
.od-tline { display: flex; align-items: center; flex-wrap: wrap; gap: 4px 10px; }
.od-title h1 { font-size: 20px; font-weight: 700; line-height: 1.3; color: var(--ink); }
.od-title p { font-size: 13px; color: var(--ink); opacity: .85; line-height: 1.5; }
.od-title .od-sched { justify-self: start; margin-top: 2px; padding: 1px 8px; border-radius: 6px; background: var(--gold-soft); color: var(--red-deep); font-size: 12px; font-weight: 600; opacity: 1; }
.od-code { flex: none; display: grid; justify-items: center; padding: 6px 12px; border-radius: 10px; background: var(--red); color: #fff6e3; }
.od-code small { font-size: 11px; opacity: .85; }
.od-code b { font-size: 22px; font-weight: 700; line-height: 1.15; font-variant-numeric: tabular-nums; letter-spacing: 1px; }

/* the progress */
.steps { list-style: none; margin: 0; padding: 12px 0 0; display: flex; border-top: 1px solid var(--line); }
.steps li { flex: 1; display: grid; justify-items: center; gap: 6px; text-align: center; color: #a6978a; position: relative; padding: 0 2px; min-width: 0; }
.steps li + li::before { content: ""; position: absolute; top: 9px; right: 50%; width: 100%; height: 2px; background: #eadcc8; }
[dir="rtl"] .steps li + li::before { right: auto; left: 50%; }
.steps li.done + li::before { background: var(--red); }
.steps .dot { width: 20px; height: 20px; border-radius: 50%; background: #eadcc8; position: relative; z-index: 1; display: grid; place-items: center; color: #fff; border: 4px solid var(--paper); box-sizing: content-box; margin: -4px; }
.steps li.done .dot { background: var(--red); }
.steps li.cur .dot { background: var(--gold); box-shadow: 0 0 0 2px var(--red) inset; }
.steps li.cur .dot::after { content: ""; position: absolute; inset: -4px; border-radius: 50%; border: 2px solid var(--gold); animation: od-ping 1.8s ease-out infinite; }
@keyframes od-ping { from { transform: scale(.7); opacity: 1; } to { transform: scale(1.5); opacity: 0; } }
@media (prefers-reduced-motion: reduce) { .steps li.cur .dot::after { animation: none; } }
.steps .lbl { font-size: 11px; line-height: 1.35; overflow-wrap: anywhere; }
.steps li.done .lbl { color: var(--ink); }
.steps li.cur .lbl { color: var(--red); font-weight: 600; font-size: 12px; }

.od-rider { display: flex; align-items: center; gap: 10px; padding: 10px 12px; border-radius: 12px; background: #fbf1e1; }
.od-rider .ava { flex: none; width: 36px; height: 36px; border-radius: 50%; background: var(--red); color: #fff6e3; display: grid; place-items: center; }
.od-rider .grow { flex: 1; min-width: 0; display: grid; }
.od-rider small { font-size: 11px; color: var(--muted); }
.od-rider a { color: var(--red); font-weight: 600; font-variant-numeric: tabular-nums; }

/* the actions: two to a row, outlined in red, ordering again filled; an odd
   last one takes the whole row */
.od-acts { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; }
.od-acts > :last-child:nth-child(odd) { grid-column: 1 / -1; }
.act.btn { min-height: 44px; border-radius: 8px; border: 1px solid var(--red); background: #fff; color: var(--ink); font-size: 15px; font-weight: 600; text-decoration: none; gap: 8px; padding: 8px 10px; }
.act.btn svg { flex: none; color: var(--red); }
.act.btn:hover { background: #fdf3ef; }
.act.btn.primary { background: var(--red); border-color: var(--red); color: #fff; }
.act.btn.primary svg { color: #fff; }
.act.btn.primary:hover:not(:disabled) { background: var(--red-deep); }

.od-gift { display: flex; gap: 12px; align-items: center; background: linear-gradient(120deg, #fbe7b3, #f4cf73); border-radius: 14px; padding: 12px 14px; color: #5a2a12; text-decoration: none; }
.od-gift .ic { width: 40px; height: 40px; border-radius: 12px; background: var(--red); display: grid; place-items: center; font-size: 20px; flex: none; }
.od-gift .grow { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; } .od-gift b { font-size: 15px; font-weight: 600; } .od-gift .grow span { font-size: 12px; }
.od-gift .chev { font-size: 20px; }

.od-as { gap: 8px; border-inline-start: 3px solid var(--gold); }
.od-reply { background: #fbf1e1; border-radius: 10px; padding: 8px 10px; }
.od-rate .ask { font-weight: 600; }
.starpick button, .starpick i { border: 0; background: none; padding: 0 2px; font-size: 24px; line-height: 1; color: #e3d6c4; cursor: pointer; font-style: normal; }
.starpick.static i { font-size: 16px; cursor: default; padding: 0 1px; }
.starpick .on { color: var(--gold); text-shadow: 0 0 1px #c9962e; }
.od-form { display: grid; gap: 10px; border-top: 1px solid var(--line); padding-top: 12px; }
.od-form textarea { background: #fff; }

/* the store, its dishes, then the sums */
.od-receipt { gap: 0; padding: 0 14px; }
.od-store { display: flex; align-items: center; gap: 12px; padding: 14px 0 12px; border-bottom: 1px solid var(--line); color: var(--ink); text-decoration: none; }
.od-store img, .od-store .ph { width: 48px; height: 48px; border-radius: 10px; object-fit: cover; flex: none; }
.od-store .ph { background: var(--red); color: #fff6e3; display: grid; place-items: center; }
.od-store .grow { flex: 1; min-width: 0; display: grid; gap: 2px; }
.od-store b { font-size: 17px; font-weight: 700; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.od-store small { font-size: 13px; color: var(--muted); }
.od-store .chev { color: #8b7868; flex: none; }
.od-store:hover b { color: var(--red); }
.od-receipt > h2 { padding: 14px 0 12px; border-bottom: 1px solid var(--line); }
.od-items { display: grid; gap: 10px; padding: 12px 0; border-bottom: 1px solid var(--line); }
.od-basket { font-size: 12px; font-weight: 600; color: var(--muted); padding-top: 2px; }
.od-item { display: flex; gap: 12px; align-items: flex-start; }
.od-nrow { display: flex; justify-content: space-between; align-items: flex-start; gap: 8px; }
.od-item.voided .od-iname, .od-item.voided .qty, .od-item.voided .price { text-decoration: line-through; color: var(--muted); }
.od-item.voided img { opacity: .5; }
.od-item .voidtag { font-size: 12px; color: #b42318; margin-top: 2px; }
.od-was { text-align: end; font-size: 12px; color: var(--muted); text-decoration: line-through; }
.od-item img, .od-item .ph { width: 64px; height: 52px; border-radius: 8px; object-fit: cover; flex: none; }
.od-item .ph { background: #c2401c; color: #fff3da; font-weight: 600; font-size: 18px; display: grid; place-items: center; }
.od-item .grow { flex: 1; min-width: 0; }
.od-iname { font-size: 15px; font-weight: 600; overflow-wrap: anywhere; line-height: 1.4; }
.od-item .qty { color: var(--muted); font-size: 13px; margin-top: 3px; font-variant-numeric: tabular-nums; }
.od-item .price { flex: none; min-width: 56px; text-align: end; font-size: 15px; font-weight: 700; font-variant-numeric: tabular-nums; }
/* the sums: the discounts on a gold band, the total big and red */
.od-sums { padding: 8px 0 14px; }
.od-receipt :deep(.totals) { margin: 0 -6px; width: calc(100% + 12px); border-collapse: separate; border-spacing: 0; }
.od-receipt :deep(.totals td) { padding: 4px 6px; font-size: 14px; color: var(--ink); border: 0; }
.od-receipt :deep(.totals td:last-child) { text-align: end; font-variant-numeric: tabular-nums; white-space: nowrap; }
.od-receipt :deep(.totals tr.disc td) { background: var(--gold-soft); }
.od-receipt :deep(.totals tr.disc td:first-child) { border-radius: 6px 0 0 6px; }
.od-receipt :deep(.totals tr.disc td:last-child) { border-radius: 0 6px 6px 0; color: var(--red); font-weight: 600; }
[dir="rtl"] .od-receipt :deep(.totals tr.disc td:first-child) { border-radius: 0 6px 6px 0; }
[dir="rtl"] .od-receipt :deep(.totals tr.disc td:last-child) { border-radius: 6px 0 0 6px; }
.od-receipt :deep(.totals tr.disc + tr.disc td) { box-shadow: 0 -2px 0 var(--paper) inset; }
.od-receipt :deep(.totals tr.sum td) { padding-top: 14px; background: linear-gradient(var(--line), var(--line)) no-repeat; background-size: calc(100% - 6px) 1px; font-weight: 700; font-size: 18px; vertical-align: baseline; }
.od-receipt :deep(.totals tr.sum td:first-child) { background-position: right top; }
.od-receipt :deep(.totals tr.sum td:last-child) { color: var(--red); font-size: 24px; background-position: left top; }
[dir="rtl"] .od-receipt :deep(.totals tr.sum td:first-child) { background-position: left top; }
[dir="rtl"] .od-receipt :deep(.totals tr.sum td:last-child) { background-position: right top; }
.od-receipt :deep(.totals tr.sum + tr td) { font-size: 13px; padding-top: 0; color: var(--muted); }

/* where it goes and the order's facts */
.od-where { display: flex; padding: 2px 0 10px; }
.od-where .pin { flex: none; color: var(--red); }
.od-where > div { display: grid; gap: 2px; min-width: 0; text-align: start; }
.od-where small { display: flex; align-items: center; gap: 4px; font-size: 12px; color: var(--muted); }
.od-where b { font-weight: 500; font-size: 15px; overflow-wrap: anywhere; }
.od-where span { color: var(--muted); font-size: 13px; overflow-wrap: anywhere; }
.od-where .gap { display: inline-block; width: 10px; }
.od-where .note { color: var(--ink); }
.od-info { margin: 0; display: grid; }
.od-info > div { display: flex; justify-content: space-between; align-items: center; gap: 14px; min-height: 44px; padding: 8px 0; border-top: 1px solid var(--line); }
.od-info > div:first-child { border-top: 0; }
.od-info dt { color: var(--ink); flex: none; }
.od-info dd { margin: 0; text-align: end; overflow-wrap: anywhere; min-width: 0; display: flex; align-items: center; justify-content: flex-end; gap: 8px; flex-wrap: wrap; font-variant-numeric: tabular-nums; }
.od-copy { border: 1px solid var(--red); background: #fff; color: var(--red); border-radius: 6px; padding: 0 10px; font-size: 12px; line-height: 24px; cursor: pointer; }
.paid { flex: none; font-size: 12px; font-weight: 500; padding: 0 8px; border-radius: 6px; line-height: 22px; white-space: nowrap; }
.paid.warn { background: var(--gold-soft); color: #7a4a10; } .paid.ok { background: #e3f1e5; color: #2f7a3f; }

/* an iPad or a computer: the red bar above (CustomerNav) is the top, so the
   page's own bar becomes its heading */
@media (min-width: 768px) {
  .od { padding-bottom: 40px; }
  .od-bar { position: static; height: auto; background: none; color: var(--ink); grid-template-columns: auto minmax(0, 1fr); gap: 4px; padding: 0 0 12px; }
  .od-back { color: var(--ink); height: 36px; width: 36px; margin-inline-start: -8px; border-radius: 50%; }
  .od-back:hover { background: var(--gold-soft); }
  .od-bar > b { text-align: start; font-size: 20px; }
  .od-lang { display: none; }
  .od-wrap { padding: 0; }
  .od-cols { gap: 14px; }
  .od-card { padding: 16px 18px; }
  .od-receipt { padding: 0 18px; }
  .od-stateic { width: 54px; height: 54px; }
  .od-title h1 { font-size: 22px; }
  .od-acts { grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); }
  .od-acts > :last-child:nth-child(odd) { grid-column: auto; }
}
/* a computer puts the dishes and sums beside the state of the order */
@media (min-width: 1024px) {
  .od-cols { grid-template-columns: minmax(0, 1fr) minmax(340px, 400px); align-items: start; }
  .od-col, .od-infocard { grid-column: 1; }
  .od-side { grid-column: 2; grid-row: 1 / span 2; position: sticky; top: 16px; }
}
</style>
