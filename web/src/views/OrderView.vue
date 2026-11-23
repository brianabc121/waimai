<script setup lang="ts">
// One order as its customer sees it: where it is, what can be done about it
// (cancel, ask for a refund, call, order again, review), what was paid, and
// where it goes. Anyone holding the link sees it; the list needs a login.
import LangButton from './LangButton.vue'
import { computed, defineAsyncComponent, nextTick, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ApiError, get, post } from '../api'
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
import { chatEnabled } from '../chat'

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
const sorted = computed(() => [...(order.value?.items ?? [])].sort((a, b) => a.basket - b.basket || a.id - b.id))
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
const itemCount = computed(() => order.value?.items.reduce((n, i) => n + i.qty, 0) ?? 0)

const copied = ref(false)
async function copyNo() {
  try { await navigator.clipboard.writeText(order.value!.public_no); copied.value = true; setTimeout(() => (copied.value = false), 1500) } catch { /* no clipboard */ }
}
</script>

<template>
  <div class="od warm" data-testid="order-page">
   <CustomerFrame active="orders">
    <!-- the head: red like the top of every customer page, with where the order is -->
    <header class="od-hero" :class="{ closed, done: finished }">
      <div class="od-bar">
        <button class="od-back" :aria-label="t('back')" data-testid="order-back" @click="back">
          <svg class="rtl-flip" viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M15 5l-7 7 7 7" /></svg>
          <span class="od-backtext">{{ t('od.all_orders') }}</span></button>
        <span v-if="order" class="od-barno">{{ order.public_no }}</span>
        <span class="od-lang"><LangButton /></span>
      </div>
      <div v-if="order" class="od-state">
        <span class="od-stateic" aria-hidden="true"><svg viewBox="0 0 24 24" width="30" height="30" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path :d="stateIcon" /></svg></span>
        <div class="od-title">
          <h1 data-testid="status">{{ finished ? t('od.done_title') + ' · ' : '' }}{{ t('s.' + status) }}</h1>
          <p>{{ hint }}</p>
          <p v-if="order.scheduled_for" class="od-sched" data-testid="scheduled">{{ t(order.service_type === 'delivery' ? 'deliver_at' : 'pickup_at') }} {{ whenText(order.scheduled_for, order.timezone) }}</p>
        </div>
        <div v-if="order.service_type === 'pickup' && !closed && !finished" class="od-code">
          <small>{{ t('pickup_code') }}</small><b>{{ order.pickup_code }}</b>
        </div>
      </div>
    </header>

    <div v-if="missing" class="od-wrap"><div class="banner bad">{{ t('not_found') }}</div></div>
    <div v-else-if="!order" class="od-wrap od-loading muted">{{ t('loading') }}</div>
    <div v-else class="od-wrap">
      <div class="od-cols">
        <div class="od-col">
          <!-- where it is, and what can be done about it -->
          <section class="od-card od-now">
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
            <div class="od-acts" data-testid="order-actions">
              <button v-if="status === 'pending_acceptance'" class="act" data-testid="act-cancel" @click="cancel">
                <i><svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path :d="ICONS.shut" /></svg></i><span>{{ t('cancel_order') }}</span></button>
              <button v-if="canAftersale" class="act" data-testid="act-aftersale" @click="as.open = true">
                <i><svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path :d="ICONS.back" /></svg></i><span>{{ t('as.apply') }}</span></button>
              <a v-if="onTheWay && order.delivery?.rider_phone" class="act" data-testid="act-rider" :href="'tel:' + order.delivery.rider_phone">
                <i><svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path :d="ICONS.phone" /></svg></i><span>{{ t('od.call_rider') }}</span></a>
              <router-link v-if="chatOn && store" class="act" data-testid="act-chat" :to="`/chat/${store.slug}`">
                <i><svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path :d="ICONS.chat" /></svg></i><span>{{ t('ch.contact_store') }}</span></router-link>
              <a v-if="store?.phone" class="act" data-testid="act-store" :href="'tel:' + store.phone">
                <i><svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path :d="ICONS.phone" /></svg></i><span>{{ t('od.call_store') }}</span></a>
              <button class="act main" data-testid="act-again" :disabled="againBusy" @click="again">
                <i><svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path :d="ICONS.again" /></svg></i><span>{{ t('ol.again') }}</span></button>
            </div>
          </section>

          <div v-if="!online" class="banner warn">{{ t('offline') }}</div>
          <div v-if="error" class="banner bad">{{ error }}</div>

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

        <!-- what was bought, like a till receipt -->
        <section class="od-card od-side od-receipt" data-testid="order-fees">
          <a v-if="store" class="od-store" :href="`/s/${store.slug}`" data-testid="order-store" @click.prevent="router.push(`/s/${store.slug}`)">
            <img v-if="store.logo_url" :src="store.logo_url" alt="" /><span v-else class="ph">{{ storeName.slice(0, 1) }}</span>
            <span class="grow"><b>{{ storeName }}</b><small>{{ t('od.fees') }} · {{ t('ol.n_items', { n: itemCount }) }}</small></span>
            <svg class="rtl-flip" viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 5l7 7-7 7" /></svg>
          </a>
          <h2 v-else>{{ t('od.fees') }}</h2>
          <i class="tear" aria-hidden="true"></i>
          <div class="od-items">
            <template v-for="(it, i) in sorted" :key="it.id">
              <div v-if="multi && (i === 0 || sorted[i - 1].basket !== it.basket)" class="od-basket" data-testid="basket-head">{{ t('basket_n', { n: it.basket }) }}</div>
              <div class="od-item">
                <img v-if="it.image_url" :src="it.image_url" alt="" loading="lazy" /><span v-else class="ph">{{ mn(it).slice(0, 1) }}</span>
                <div class="grow">
                  <div class="od-iname">{{ mn(it) }}</div>
                  <div class="sub" v-if="it.options.length">{{ it.options.map(o => mn(o)).join(' · ') }}</div>
                  <div class="sub" v-if="it.note">{{ t('note') }}: {{ it.note }}</div>
                </div>
                <span class="qty">×{{ it.qty }}</span>
                <span class="price">{{ money(it.line_total, order.currency) }}</span>
              </div>
            </template>
          </div>
          <i class="tear" aria-hidden="true"></i>
          <div class="od-sums"><TotalsTable :totals="order.totals" :currency="order.currency" :delivery="order.service_type === 'delivery'" :discounts="order.discounts" /></div>
        </section>

        <section class="od-card od-infocard" data-testid="order-info">
          <h2>{{ t('od.info') }}</h2>
          <div v-if="order.service_type === 'delivery'" class="od-where">
            <span class="pin"><svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M12 13a2.600 2.600 0 1 0 0-5.200 2.600 2.600 0 0 0 0 5.200zM12 21s6.500-5.800 6.500-10.600a6.500 6.500 0 0 0-13 0C5.500 15.200 12 21 12 21z" /></svg></span>
            <div><small>{{ t('od.address') }}</small>
              <b>{{ order.address_line }} {{ order.address_unit }}</b>
              <span>{{ contactName(order) }} {{ prettyPhone(contactPhone(order)) }}</span>
              <span v-if="order.address_note" class="note">{{ order.address_note }}</span></div>
          </div>
          <div v-else-if="store" class="od-where">
            <span class="pin"><svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path :d="ICONS.bag" /></svg></span>
            <div><small>{{ t('od.pickup_at_store') }}</small><b>{{ storeName }}</b><span>{{ store.address }}</span></div>
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
/* Deep red, cream and gold like the rest of the customer's pages. Sizes: body
   14, notes 12, card titles 15, the state 20; weights up to 600/700. */
.od { min-height: 100vh; font-size: 14px; padding-bottom: calc(env(safe-area-inset-bottom) + 28px); background: var(--bg); }

/* the red head; on a phone the first card stands on its lower edge */
.od-hero { color: #fff; padding: 0 16px 52px; background: var(--red-bg); border-radius: 0 0 50% 50% / 0 0 22px 22px; }
.od-hero.closed { background: linear-gradient(180deg, #6f5a4c, #8c7464); }
.od-bar { display: flex; align-items: center; gap: 8px; height: 52px; }
.od-back { border: 0; background: none; color: inherit; padding: 6px 6px 6px 0; margin-inline-start: -4px; cursor: pointer; display: flex; align-items: center; gap: 2px; font-size: 14px; font-weight: 600; }
.od-backtext { display: none; }
.od-barno { flex: 1; text-align: center; font-size: 12px; opacity: .8; font-variant-numeric: tabular-nums; letter-spacing: .3px; }
.od-lang :deep(.lang-btn) { border: 1px solid rgba(244, 207, 115, .7); background: rgba(255, 255, 255, .12); color: #fff; border-radius: 14px; height: 28px; padding: 0 10px; font-size: 12px; cursor: pointer; }
.od-state { display: flex; align-items: center; gap: 12px; padding: 4px 2px 0; }
.od-stateic { flex: none; width: 52px; height: 52px; border-radius: 50%; display: grid; place-items: center; color: var(--gold); background: rgba(255, 246, 227, .12); border: 1px solid rgba(244, 207, 115, .45); }
.od-title { flex: 1; min-width: 0; display: grid; gap: 3px; }
.od-title h1 { font-size: 20px; font-weight: 700; line-height: 1.3; color: #fff6e3; }
.od-title p { font-size: 12px; color: rgba(255, 246, 227, .85); line-height: 1.5; }
.od-title .od-sched { display: inline-block; justify-self: start; margin-top: 2px; padding: 1px 8px; border-radius: 6px; background: var(--gold); color: var(--red-deep); font-weight: 600; }
.od-code { flex: none; display: grid; justify-items: center; padding: 6px 12px; border-radius: 10px; background: #fff6e3; color: var(--red); box-shadow: 0 3px 10px rgba(60, 10, 0, .2); }
.od-code small { font-size: 11px; color: var(--muted); }
.od-code b { font-size: 22px; font-weight: 700; line-height: 1.15; font-variant-numeric: tabular-nums; letter-spacing: 1px; }

.od-wrap { padding: 0 12px; margin-top: -40px; position: relative; }
.od-loading { margin-top: 0; padding-top: 16px; }
.od-cols, .od-col { display: grid; gap: 12px; min-width: 0; align-content: start; }
.od-col:empty { display: none; }
.od-card { background: var(--paper); border-radius: 14px; padding: 14px; display: grid; gap: 12px; min-width: 0; box-shadow: 0 4px 16px rgba(90, 40, 15, .08); }
.od-card h2 { font-size: 15px; font-weight: 600; }
.od-now { box-shadow: 0 6px 18px rgba(90, 40, 15, .13); }
.od-line, .od-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; }

/* the progress */
.steps { list-style: none; margin: 0; padding: 2px 0 0; display: flex; }
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

/* the actions: round icons with their names, ordering again in red */
.od-acts { display: flex; justify-content: space-around; gap: 4px; padding-top: 12px; border-top: 1px dashed #e2d3bd; }
.steps:not(:empty) ~ .od-acts, .od-rider ~ .od-acts { margin-top: 0; }
.od-now > .od-acts:first-child { border-top: 0; padding-top: 0; }
.act { flex: 1 1 0; min-width: 0; max-width: 92px; border: 0; background: none; padding: 2px 0; display: grid; justify-items: center; gap: 5px; color: var(--ink); font-size: 12px; cursor: pointer; text-decoration: none; }
.act i { width: 40px; height: 40px; border-radius: 50%; display: grid; place-items: center; background: #f9e7dd; color: var(--red); transition: transform .15s; }
.act span { max-width: 100%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.act:hover i { transform: translateY(-1px); }
.act.main i { background: var(--red); color: #fff6e3; box-shadow: 0 3px 10px rgba(160, 44, 32, .3); }
.act.main span { color: var(--red); font-weight: 600; }
.act:disabled { opacity: .5; }

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
.od-form { display: grid; gap: 10px; border-top: 1px dashed #e2d3bd; padding-top: 12px; }
.od-form textarea { background: #fff; }

/* the receipt: the store on top, a torn line, the dishes, another, the sums */
.od-receipt { gap: 0; padding: 0; overflow: hidden; }
.od-store { display: flex; align-items: center; gap: 10px; padding: 14px; color: var(--ink); text-decoration: none; }
.od-store img, .od-store .ph { width: 40px; height: 40px; border-radius: 10px; object-fit: cover; flex: none; }
.od-store .ph, .od-item .ph { background: #c2401c; color: #fff3da; font-weight: 600; display: grid; place-items: center; }
.od-store .grow { flex: 1; min-width: 0; display: grid; }
.od-store b { font-size: 15px; font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.od-store small { font-size: 12px; color: var(--muted); }
.od-store svg { color: #b9a793; flex: none; }
.od-store:hover b { color: var(--red); }
.od-receipt > h2 { padding: 14px 14px 12px; }
.tear { position: relative; display: block; height: 0; margin: 0 14px; border-top: 1.5px dashed #e2d3bd; }
.tear::before, .tear::after { content: ""; position: absolute; top: -9px; width: 16px; height: 16px; border-radius: 50%; background: var(--bg); }
.tear::before { inset-inline-start: -22px; } .tear::after { inset-inline-end: -22px; }
.od-items { display: grid; gap: 12px; padding: 14px; }
.od-basket { font-size: 12px; font-weight: 600; color: var(--muted); padding-top: 2px; }
.od-item { display: flex; gap: 10px; align-items: flex-start; }
.od-item img, .od-item .ph { width: 52px; height: 52px; border-radius: 10px; object-fit: cover; flex: none; font-size: 18px; }
.od-item .grow { flex: 1; min-width: 0; padding-top: 2px; }
.od-iname { font-weight: 600; overflow-wrap: anywhere; line-height: 1.4; }
.od-item .sub { font-size: 12px; color: var(--muted); margin-top: 2px; overflow-wrap: anywhere; }
.od-item .qty { flex: none; color: var(--muted); font-size: 12px; padding-top: 3px; font-variant-numeric: tabular-nums; }
.od-item .price { flex: none; min-width: 56px; text-align: end; font-weight: 600; padding-top: 1px; font-variant-numeric: tabular-nums; }
.od-sums { padding: 10px 14px 14px; }
.od-receipt :deep(.totals) { margin: 0; width: 100%; border-collapse: collapse; }
.od-receipt :deep(.totals td) { padding: 3px 0; font-size: 13px; color: var(--muted); border: 0; }
.od-receipt :deep(.totals td:last-child) { text-align: end; color: var(--ink); font-variant-numeric: tabular-nums; }
.od-receipt :deep(.totals tr.disc td), .od-receipt :deep(.totals tr.disc td:last-child) { color: var(--red); }
.od-receipt :deep(.totals tr.sum td) { padding-top: 12px; border-top: 1px solid #eadcc8; color: var(--ink); font-weight: 600; font-size: 15px; }
.od-receipt :deep(.totals tr.sum td:last-child) { color: var(--red); font-size: 20px; font-weight: 700; }
.od-receipt :deep(.totals tr.sum + tr td) { font-size: 12px; padding-top: 0; }

/* where it goes and the order's facts */
.od-where { display: flex; gap: 10px; align-items: flex-start; padding: 12px; border-radius: 12px; background: #fbf1e1; }
.od-where .pin { flex: none; width: 32px; height: 32px; border-radius: 50%; background: var(--red); color: #fff6e3; display: grid; place-items: center; }
.od-where > div { display: grid; gap: 2px; min-width: 0; }
.od-where small { font-size: 12px; color: var(--muted); }
.od-where b { font-weight: 600; overflow-wrap: anywhere; }
.od-where span { color: var(--muted); font-size: 12px; overflow-wrap: anywhere; }
.od-where .note { color: var(--ink); }
.od-info { margin: 0; display: grid; }
.od-info > div { display: flex; justify-content: space-between; align-items: center; gap: 14px; padding: 9px 0; border-top: 1px dashed #eadcc8; }
.od-info > div:first-child { border-top: 0; padding-top: 0; }
.od-info dt { color: var(--muted); flex: none; }
.od-info dd { margin: 0; text-align: end; overflow-wrap: anywhere; min-width: 0; display: flex; align-items: center; justify-content: flex-end; gap: 6px; flex-wrap: wrap; }
.od-copy { border: 1px solid #d9c5a8; background: none; color: var(--muted); border-radius: 999px; padding: 0 9px; font-size: 11px; line-height: 20px; cursor: pointer; }
.paid { font-size: 11px; padding: 0 7px; border-radius: 6px; line-height: 18px; }
.paid.warn { background: var(--gold-soft); color: #9a5b12; } .paid.ok { background: #e3f1e5; color: #2f7a3f; }

/* an iPad or a computer: the top bar above (CustomerNav) is red already, so
   the head becomes a red card and the cards follow under it */
@media (min-width: 768px) {
  .od { padding-bottom: 40px; }
  .od-hero { border-radius: 16px; padding: 4px 22px 22px; }
  .od-lang, .od-barno { display: none; }
  .od-bar { height: 40px; }
  .od-back { color: rgba(255, 246, 227, .9); }
  .od-backtext { display: inline; }
  .od-state { padding: 0; gap: 16px; }
  .od-stateic { width: 60px; height: 60px; }
  .od-title h1 { font-size: 22px; }
  .od-title p { font-size: 13px; }
  .od-code { padding: 8px 18px; } .od-code b { font-size: 26px; }
  .od-wrap { padding: 0; margin-top: 14px; }
  .od-cols { gap: 14px; }
  .act { max-width: 110px; font-size: 13px; }
  .act i { width: 44px; height: 44px; }
}
/* a computer puts the receipt beside the state of the order */
@media (min-width: 1024px) {
  .od-cols { grid-template-columns: minmax(0, 1fr) minmax(320px, 380px); align-items: start; }
  .od-col, .od-infocard { grid-column: 1; }
  .od-side { grid-column: 2; grid-row: 1 / span 2; position: sticky; top: 16px; }
}
</style>
