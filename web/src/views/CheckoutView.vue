<script setup lang="ts">
import { storePath, checkoutPath, offersPath } from '../site'
import { storeStored, type StoredOffers } from '../stored'
import { cartTags, tagNames } from '../notes'
import { fromCart, fromLineOptions, type Extras } from '../extras'
import LineExtras from './LineExtras.vue'
import { computed, defineAsyncComponent, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { listAddresses, sorted, usable, whoLine, type Address } from '../addresses'
import { api, ApiError, get, post } from '../api'
import { latest, stableKey } from '../retry'
import { clearLines, linePrice, quoteItems, useCart } from '../cart'
import { customer, loadCustomer, loginRoute, prettyPhone } from '../customer'
import { locale, money, pick, t, whenText, mn, sn } from '../i18n'
import { mallAddressId } from '../mall'
import type { Item, Quote, Storefront } from '../types'
import { discountLabel, kindBadge } from '../coupons'
import './storeStrings'

const AddressSheet = defineAsyncComponent(() => import('./AddressSheet.vue'))
const CouponSheet = defineAsyncComponent(() => import('./CouponSheet.vue'))

const props = defineProps<{ slug: string }>()
const router = useRouter()
const cart = useCart(props.slug)

const quote = ref<Quote | null>(null)
const error = ref('')
const notice = ref('')
const busy = ref(false)

// Scheduling: '' means as soon as possible. When the store is closed right
// now, a time must be chosen.
const store = ref<Storefront | null>(null)
const scheduled = ref('')
const tz = computed(() => store.value?.branch.timezone)
const slots = computed(() => store.value?.slots ?? [])
const mapMode = computed(() => store.value?.branch.zone_mode === 'map')
const mustSchedule = computed(() => !!store.value && !store.value.branch.open_now)
async function loadStore() {
  store.value = await get<Storefront>(`/stores/${props.slug}?lang=${locale.value}`).catch(() => null)
  if (scheduled.value && !slots.value.includes(scheduled.value)) scheduled.value = ''
  // Fall back to a service the store actually offers.
  const b = store.value?.branch
  if (b && cart.service === 'delivery' && !b.delivery_enabled) cart.service = 'pickup'
  if (b && cart.service === 'pickup' && !b.pickup_enabled) cart.service = 'delivery'
}
// Delivery or pickup is chosen here, on top of the page, when the store offers both.
const bothServices = computed(() => !!store.value && store.value.branch.delivery_enabled && store.value.branch.pickup_enabled)
async function setService(s: 'delivery' | 'pickup') {
  if (cart.service === s || busy.value) return
  cart.service = s; notice.value = ''
  if (s === 'delivery') await loadAddresses()
  await requote()
}

// A delivery goes to one of the customer's saved addresses. The one used last
// time on this device stays chosen; otherwise the default, or the first this
// store delivers to.
const delivery = computed(() => cart.service === 'delivery')
const addresses = ref<Address[]>([])
const address = computed(() => addresses.value.find(a => a.id === cart.addressId && usable(a)) ?? null)
const showSheet = ref(false), startNew = ref(false)
const shopPin = computed(() => { const b = store.value?.branch; return b && b.lat != null && b.lng != null ? { lat: b.lat, lng: b.lng, name: sn(b), country: b.country_code } : null })
function use(a: Address | null) {
  cart.addressId = a?.id ?? null
  // The store page shows the same spot.
  if (a && a.lat != null && a.lng != null) Object.assign(cart, { lat: a.lat, lng: a.lng, placeLabel: a.place_name })
}
async function loadAddresses() {
  if (!delivery.value) return
  const r = await listAddresses(mapMode.value ? props.slug : undefined).catch(() => null)
  if (!r) return
  addresses.value = sorted(r.addresses)
  // The address chosen in the marketplace comes first, when this store delivers to it.
  if (!address.value) use(addresses.value.find(a => a.id === mallAddressId() && usable(a)) ?? addresses.value.find(usable) ?? null)
  // Nothing saved yet: the first thing to do is to add an address. Where the
  // customer may also pick up, the page is shown first, so that they can say so.
  if (!addresses.value.length && !bothServices.value) { startNew.value = true; showSheet.value = true }
}
// With nothing saved there is no list to show: go straight to the form.
function openSheet() { startNew.value = !addresses.value.length; showSheet.value = true }
function chosen(a: Address) { use(a); showSheet.value = false; void requote() }
function bookChanged(list: Address[]) {
  addresses.value = list
  // the chosen address was deleted, or moved to where the store does not deliver: fall back to another
  if (cart.addressId != null && !address.value) { use(list.find(usable) ?? null); void requote() }
}

// The idempotency key belongs to what is being ordered (the basket and the
// choices on this page), not to one price quote: asking the price again
// after a lost answer, or reloading the page, keeps the key, so the server
// finds the first try instead of making a second order.
const keys = stableKey(sessionStorage, 'order-key:' + props.slug)
const orderPrint = () => ({ service: cart.service, zone: cart.zoneId, address: delivery.value ? cart.addressId : null, items: quoteItems(),
  when: scheduled.value || null, coupons: couponIds.value, name: delivery.value ? '' : cart.name, note: cart.note, credit: useCredit.value })
// An order request that was never answered may have gone through. Its key
// is kept until that is known.
const pendingName = 'order-pending:' + props.slug
function placed(token: string) {
  clearLines()
  keys.done()
  sessionStorage.removeItem(pendingName)
  localStorage.setItem('last-order:' + props.slug, token)
  router.replace(`/o/${token}`)
}
// Before the customer orders anything else: did the unanswered request make
// an order? True when it did (the page goes to that order).
async function confirmPending(sending = ''): Promise<boolean> {
  const key = sessionStorage.getItem(pendingName)
  // the same request again finds its own order
  if (!key || key === sending) return false
  try {
    const r = await api<{ access_token: string }>('GET', `/stores/${props.slug}/orders/placed`, undefined, { 'Idempotency-Key': key })
    placed(r.access_token)
    return true
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) return false
    throw e
  }
}

function describe(e: unknown): string {
  if (!(e instanceof ApiError)) return String(e)
  switch (e.code) {
    case 'NETWORK': return t('offline')
    case 'ITEM_UNAVAILABLE': return t('unavailable')
    case 'OUT_OF_ZONE': return mapMode.value ? t('ad.cannot') : t('choose_zone')
    case 'ADDRESS_NOT_FOUND': return t('co.address_gone')
    case 'STORE_CLOSED': return t('closed')
    case 'SCHEDULE_INVALID': return t('schedule_invalid')
    case 'REQUIRED_MISSING': return t('required_missing_named', { names: ((e.details?.categories ?? []) as { name: string; name_en: string }[]).map(c => mn(c)).join('、') })
    case 'BELOW_MINIMUM': short.value = (e.details?.minimum ?? 0) - (e.details?.subtotal ?? 0); return t('below_min', { amt: money((e.details?.minimum ?? 0) - (e.details?.subtotal ?? 0), quote.value?.currency) })
    default: return e.message
  }
}

// How much is missing to the store's minimum for this address.
const short = ref(0)

// Coupons: the server picks the best ones until the customer chooses.
const couponIds = ref<number[] | null>(null)
const showCoupons = ref(false)
const coupons = computed(() => quote.value?.coupons ?? [])
const choices = computed(() => quote.value?.coupon_choices ?? [])
const couponOff = computed(() => coupons.value.reduce((s, c) => s + c.amount, 0))
function couponsChosen(ids: number[]) { couponIds.value = ids; showCoupons.value = false; void requote() }
// Only the answer to the last question counts, and while it is on its way
// the price on the page is not one to order with.
const turn = latest()
const quoting = ref(false)
async function requote() {
  error.value = ''; short.value = 0
  const mine = turn.next()
  quoting.value = false
  if (cart.lines.length === 0) { router.replace(storePath(props.slug)); return }
  // Without an address there is nothing to price a delivery for yet.
  if (delivery.value && !address.value) { quote.value = null; return }
  if (mustSchedule.value && !scheduled.value) { quote.value = null; error.value = slots.value.length ? t('pick_time') : t('closed'); return }
  quoting.value = true
  try {
    // an order whose answer was lost is found before anything is priced again
    if (await confirmPending()) return
    const q = await post<Quote>(`/stores/${props.slug}/quotes`, {
      service_type: cart.service,
      zone_id: cart.service === 'delivery' ? cart.zoneId : null,
      // Stores that locate customers on a map decide the area from the address's pin.
      address_id: delivery.value ? cart.addressId : null,
      items: quoteItems(),
      scheduled_for: scheduled.value || null,
      // null lets the server choose the coupons that save most
      coupon_ids: couponIds.value,
      // the order keeps the dishes' names in this language
      lang: locale.value,
    })
    if (!mine()) return
    quote.value = q
  } catch (e) {
    if (!mine()) return
    quote.value = null
    error.value = describe(e)
    // The offered times move on; fetch fresh ones when the chosen one is gone.
    if (e instanceof ApiError && (e.code === 'SCHEDULE_INVALID' || e.code === 'STORE_CLOSED')) await loadStore()
    if (e instanceof ApiError && e.code === 'ADDRESS_NOT_FOUND') { use(null); await loadAddresses() }
  } finally { if (mine()) quoting.value = false }
}

// Ordering needs an account: without one, log in first and come back here.
const toLogin = () => router.replace(loginRoute(checkoutPath(props.slug), props.slug))
onMounted(async () => {
  if (!(await loadCustomer(true))) { toLogin(); return }
  await loadStore()
  await loadAddresses()
  await requote()
  loaded.value = true
})
const loaded = ref(false)

// ---- what is shown ----
const cur = computed(() => quote.value?.currency ?? store.value?.branch.currency)
const items = computed(() => new Map((store.value?.categories ?? []).flatMap(c => c.items).map((i): [number, Item] => [i.id, i])))
interface Shown extends Extras { item_id: number; name: string; flavors: string[]; note: string; qty: number; total: number | null; basket: number }
// The dishes as priced by the server; before there is a price (no address
// yet), as the menu has them.
const lines = computed<Shown[]>(() => {
  if (quote.value) return quote.value.lines.map(l => ({ item_id: l.item_id, name: mn(l), ...fromLineOptions(l.options, l.unit_price),
    flavors: tagNames(l.note_tags, true), note: l.note?.trim() ?? '', qty: l.qty, total: l.line_total, basket: l.basket || 1 }))
  const sent = quoteItems()
  return cart.lines.map((l, i) => {
    const it = items.value.get(l.item_id), unit = linePrice(l, items.value)
    return { item_id: l.item_id, name: it ? mn(it) : '', ...fromCart(it, l.option_ids),
      flavors: cartTags(it, l.note_tags), note: l.note?.trim() ?? '', qty: l.qty, total: unit == null ? null : unit * l.qty, basket: sent[i].basket }
  }).filter(l => l.name)
})
const baskets = computed(() => {
  const by = new Map<number, Shown[]>()
  for (const l of lines.value) by.set(l.basket, [...(by.get(l.basket) ?? []), l])
  return [...by.entries()].sort((a, b) => a[0] - b[0]).map(([n, ls]) => ({ n, lines: ls, subtotal: ls.reduce((s, l) => s + (l.total ?? 0), 0) }))
})
const subtotal = computed(() => quote.value?.totals.subtotal ?? lines.value.reduce((s, l) => s + (l.total ?? 0), 0))
const image = (id: number) => items.value.get(id)?.image_url ?? ''
// About how long a delivery to the chosen address takes, when the store has said.
const eta = computed(() => !delivery.value || !address.value ? null
  : mapMode.value ? address.value.delivery?.eta_minutes ?? null : store.value?.zones.find(z => z.id === cart.zoneId)?.eta_minutes ?? null)
const showSlots = ref(false)
function now() { if (mustSchedule.value) return; scheduled.value = ''; void requote() }
function later(s: string) { scheduled.value = s; showSlots.value = false; void requote() }
// The button answers even when the order cannot go yet: it leads to what is missing.
function press() {
  if (delivery.value && !address.value) { openSheet(); return }
  if (mustSchedule.value && !scheduled.value && slots.value.length) { showSlots.value = true; return }
  void submit()
}
const buttonText = computed(() => busy.value ? t('placing') : short.value > 0 ? t('co.short', { amt: money(short.value, cur.value) }) : t('place_order'))

// What the bottom bar says: how many dishes, and the offer the order earns.
const count = computed(() => lines.value.reduce((n, l) => n + l.qty, 0))
const promoName = computed(() => quote.value?.totals.promotion_discount ? pick(quote.value.totals.promotion_name, quote.value.totals.promotion_name_en) : '')

// The store balance (docs/REFUND_BALANCE_DESIGN.md §5.2): used unless the
// customer says no; what the page showed is sent, so a balance that changed
// meanwhile is shown again before anything is spent.
// a top-up offer, when what the customer holds does not cover the order
// (docs/STORED_VALUE_DESIGN.md §5, Q11)
const stored = ref<StoredOffers | null>(null)
void storeStored(props.slug).then(r => { stored.value = r }).catch(() => null)
const upsell = computed(() => {
  if (!quote.value || (quote.value.store_credit ?? 0) >= quote.value.totals.total) return null
  const tiers = (stored.value?.campaign?.tiers ?? []).filter(x => !x.closed && x.pay + x.bonus + (quote.value?.store_credit ?? 0) >= (quote.value?.totals.total ?? 0))
  return tiers.sort((a, b) => a.pay - b.pay)[0] ?? null
})
const useCredit = ref(true)
const credit = computed(() => useCredit.value ? quote.value?.credit ?? 0 : 0)
const due = computed(() => quote.value ? quote.value.totals.total - credit.value : subtotal.value)

const ready = computed(() => !!quote.value && !quoting.value && customer.verified && (delivery.value ? !!address.value : cart.name.trim() !== ''))

async function submit() {
  if (!quote.value || busy.value || quoting.value) return
  busy.value = true
  error.value = ''; notice.value = ''
  const idemKey = keys.for(orderPrint())
  try {
    if (await confirmPending(idemKey)) return
    const r = await post(`/stores/${props.slug}/orders`, {
      // a delivery takes the name and the address from the saved address
      quote_id: quote.value.id, customer_name: delivery.value ? '' : cart.name, note: cart.note,
      use_credit: credit.value > 0, credit_expected: credit.value > 0 ? credit.value : undefined,
    }, { 'Idempotency-Key': idemKey })
    placed(r.access_token)
  } catch (e) {
    if (e instanceof ApiError && (e.code === 'QUOTE_CHANGED' || e.code === 'QUOTE_EXPIRED' || e.code === 'QUOTE_USED' || e.code === 'CREDIT_CHANGED')) {
      // Show the new amount first; the customer confirms again.
      await requote()
      notice.value = t(e.code === 'CREDIT_CHANGED' ? 'co.credit_changed' : 'amount_changed')
    } else if (e instanceof ApiError && e.code === 'PHONE_NOT_VERIFIED') {
      customer.verified = false
      toLogin()
    } else {
      // No answer, or the server failed half way: the order may exist. The
      // key is kept, so pressing again (also after a reload) finds it.
      if (e instanceof ApiError && (e.code === 'NETWORK' || e.status >= 500)) {
        sessionStorage.setItem(pendingName, idemKey)
        error.value = t('submit_failed')
      } else error.value = describe(e)
    }
  } finally { busy.value = false }
}
</script>

<template>
  <div class="phone wide co warm" :data-ready="loaded || undefined">
    <header class="co-top">
      <router-link :to="storePath(slug)" :aria-label="t('back_to_menu')" class="co-back">
        <svg class="rtl-flip" viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m15 5-7 7 7 7" /></svg></router-link>
      <h1>{{ t('co.title') }}</h1><span></span>
    </header>
    <div class="section two-cols co-body">
      <div class="stack co-main">
      <div v-if="bothServices" class="co-seg" role="tablist" data-testid="service-toggle">
        <button type="button" role="tab" :aria-selected="delivery" :class="{ on: delivery }" data-testid="service-delivery" @click="setService('delivery')">{{ t('co.svc_delivery') }}</button>
        <button type="button" role="tab" :aria-selected="!delivery" :class="{ on: !delivery }" data-testid="service-pickup" @click="setService('pickup')">{{ t('co.svc_pickup') }}</button>
      </div>
      <div v-if="error" class="banner bad co-msg">{{ error }} <button class="btn link" @click="requote">{{ t('retry') }}</button></div>
      <div v-if="notice" class="banner warn co-msg">{{ notice }}</div>

      <!-- where to, and when -->
      <div class="card co-card co-where">
        <div class="co-label">{{ t(delivery ? 'co.deliver_to' : 'co.pickup_at') }}</div>
        <button v-if="delivery" type="button" class="co-addr" data-testid="checkout-address" :data-empty="!addresses.length || undefined" @click="openSheet">
          <svg class="pin" viewBox="0 0 24 24" width="22" height="22" fill="currentColor"><path d="M12 2a7 7 0 0 0-7 7c0 5 7 13 7 13s7-8 7-13a7 7 0 0 0-7-7zm0 9.5A2.5 2.5 0 1 1 12 6.5a2.5 2.5 0 0 1 0 5z" /></svg>
          <span class="grow" v-if="address">
            <b><span v-if="address.label" class="tag brand">{{ t('ad.label_' + address.label) }}</span>{{ address.place_name }}</b>
            <span class="sub">{{ address.unit }}<template v-if="address.place_address"> · {{ address.place_address }}</template></span>
            <span class="sub">{{ whoLine(address) }}</span>
          </span>
          <span class="grow ph" v-else>{{ t(addresses.length ? 'ad.choose' : 'co.add_address') }}</span>
          <span class="chev"><svg class="rtl-flip" viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m9 5 7 7-7 7" /></svg></span>
        </button>
        <!-- a store whose customers pick their area by name -->
        <label v-if="delivery && store && !mapMode && store.zones.length" class="field" style="margin:12px 0 0">
          <span>{{ t('deliver_to') }}</span>
          <select v-model="cart.zoneId" data-testid="checkout-zone" @change="requote">
            <option :value="null">{{ t('choose_zone') }}</option>
            <option v-for="z in store.zones" :key="z.id" :value="z.id">{{ pick(z.name, z.name_en) }} · {{ t('delivery_fee') }} {{ money(z.fee, cur) }}</option>
          </select>
        </label>
        <div v-if="!delivery" class="co-pick" data-testid="checkout-pickup">
          <div class="co-addr static">
            <svg class="pin" viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"><path d="M4 10v10h16V10M3 4h18l-1 6H4z" /><path d="M10 20v-5h4v5" /></svg>
            <span class="grow"><b>{{ store ? sn(store.branch) : '' }}</b><span class="sub" v-if="store?.branch.address">{{ store.branch.address }}</span></span>
          </div>
          <label class="field" style="margin:10px 0 0"><span>{{ t('co.pickup_name') }}</span><input v-model="cart.name" maxlength="80" autocomplete="name" /></label>
        </div>
        <div class="co-whens">
          <button type="button" class="co-when" :class="{ on: !scheduled && !mustSchedule, off: mustSchedule }" :disabled="mustSchedule" data-testid="when-now" @click="now">
            <svg v-if="delivery" class="ic" viewBox="0 0 24 24" width="24" height="24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><circle cx="6" cy="17" r="2.5" /><circle cx="18" cy="17" r="2.5" /><path d="M8.5 17h7M15 6h2l3 8.5M6 14.5V10h6l2 4.5M9 6h3v4" /></svg>
            <svg v-else class="ic" viewBox="0 0 24 24" width="24" height="24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M5 8h14l-1.2 11.2a1 1 0 0 1-1 .8H7.2a1 1 0 0 1-1-.8z" /><path d="M9 8V6a3 3 0 0 1 6 0v2" /></svg>
            <span class="txt"><b>{{ t(delivery ? 'co.now_delivery' : 'co.now_pickup') }}</b>
              <small v-if="mustSchedule">{{ t('co.closed_now') }}</small>
              <small v-else-if="delivery && !address">{{ t('co.need_address') }}</small>
              <small v-else-if="eta" data-testid="eta">{{ t('co.eta', { n: eta }) }}</small></span>
            <svg v-if="!scheduled && !mustSchedule" class="ok" viewBox="0 0 24 24" width="22" height="22"><circle cx="12" cy="12" r="11" fill="currentColor" /><path d="m7 12.5 3.2 3L17 9" fill="none" stroke="#fff" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" /></svg>
          </button>
          <button v-if="slots.length" type="button" class="co-when" :class="{ on: !!scheduled }" data-testid="when-later" @click="showSlots = true">
            <svg class="ic" viewBox="0 0 24 24" width="24" height="24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><rect x="3.5" y="5" width="17" height="15" rx="2" /><path d="M3.5 10h17M8 3v4M16 3v4M8 14h.01M12 14h.01M16 14h.01" /></svg>
            <span class="txt"><b>{{ t(delivery ? 'co.later_delivery' : 'co.later_pickup') }}</b>
              <small>{{ scheduled ? whenText(scheduled, tz) : t('co.pick_time') }}</small></span>
            <svg v-if="scheduled" class="ok" viewBox="0 0 24 24" width="22" height="22"><circle cx="12" cy="12" r="11" fill="currentColor" /><path d="m7 12.5 3.2 3L17 9" fill="none" stroke="#fff" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" /></svg>
          </button>
        </div>
      </div>

      <div class="card co-card co-note">
        <label class="field" style="margin:0"><span>{{ t('order_note') }}</span><input v-model="cart.note" maxlength="300" :placeholder="t('co.note_ph')" /></label>
      </div>

      <div class="card co-card co-pay">
        <h2>{{ t('pay_method') }}</h2>
        <div class="co-paywith">
          <svg class="ic" viewBox="0 0 32 24" width="36" height="27" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round"><rect x="2" y="4" width="28" height="16" rx="2" /><circle cx="16" cy="12" r="3.5" /><path d="M6 8v8M26 8v8" /></svg>
          <span class="grow"><b>{{ t(delivery ? 'co.cod' : 'pay_at_pickup') }}</b><small>{{ t('co.pay_cash') }}</small></span>
          <svg class="ok" viewBox="0 0 24 24" width="22" height="22"><circle cx="12" cy="12" r="11" fill="currentColor" /><path d="m7 12.5 3.2 3L17 9" fill="none" stroke="#fff" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" /></svg>
        </div>
        <label v-if="quote?.store_credit" class="co-credit" data-testid="use-credit">
          <input v-model="useCredit" type="checkbox" />
          <span class="grow"><b>{{ t('co.use_credit') }}</b><small>{{ t('co.credit_have', { amt: money(quote.store_credit, cur) }) }}</small></span>
          <b v-if="useCredit && quote.credit" class="off">−{{ money(quote.credit, cur) }}</b>
        </label>
        <router-link v-if="upsell" class="co-upsell" :to="offersPath(slug)" data-testid="co-upsell">
          <span class="grow">{{ t('sv.co.upsell', { pay: money(upsell.pay, cur), bonus: money(upsell.bonus, cur) }) }}</span><b>{{ t('sv.c.go') }} ›</b></router-link>
        <div class="co-row co-acct" v-if="customer.verified" data-testid="account-phone"><span>{{ t('co.account') }}</span><span>{{ prettyPhone(customer.phone) }}</span></div>
      </div>
      </div>

      <aside class="stack side co-side">
      <!-- the order -->
      <div class="card co-card co-order" v-if="store">
        <div class="co-shop">
          <span class="logo"><img v-if="store.templates?.delivery?.logo_url" :src="store.templates.delivery.logo_url" alt="" />
            <svg v-else viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M3 11h18a9 9 0 0 1-18 0zM8 7c0-1 1-1.5 1-2.5M12 7c0-1 1-1.5 1-2.5M16 7c0-1 1-1.5 1-2.5" /></svg></span>
          <b class="clamp1">{{ sn(store.branch) }}</b><span class="co-tag">{{ t(delivery ? 'co.by_store' : 'pickup') }}</span></div>
        <div v-for="b in baskets" :key="b.n" class="co-basket">
          <div v-if="baskets.length > 1" class="co-bhead">🧺 {{ t('co.basket', { n: b.n }) }}</div>
          <div v-for="(l, i) in b.lines" :key="i" class="co-line">
            <img v-if="image(l.item_id)" :src="image(l.item_id)" alt="" class="co-thumb" loading="lazy" />
            <span v-else class="co-thumb none">{{ l.name.slice(0, 1) }}</span>
            <div class="grow">
              <div class="co-nrow"><span class="n clamp2">{{ l.name }}</span><b class="price" v-if="l.total != null">{{ money(l.total, cur) }}</b></div>
              <LineExtras :specs="l.specs" :toppings="l.toppings" :base="l.base" :flavors="l.flavors" :note="l.note" :cur="cur ?? ''" tid="co" />
              <div class="qty">×{{ l.qty }}</div>
            </div>
          </div>
          <div v-if="baskets.length > 1" class="co-row"><span>{{ t('co.basket_subtotal') }}</span><b>{{ money(b.subtotal, cur) }}</b></div>
        </div>
        <div class="co-fees totals">
          <div class="co-row"><span>{{ t('subtotal') }}</span><span>{{ money(subtotal, cur) }}</span></div>
          <div class="co-row" v-if="quote?.totals.packaging_fee"><span>{{ t('packaging_fee') }}</span><span>{{ money(quote.totals.packaging_fee, cur) }}</span></div>
          <div class="co-row" v-if="delivery"><span>{{ t('delivery_fee') }}</span>
            <span v-if="quote" data-testid="delivery-fee">{{ money(quote.totals.delivery_fee, cur) }}</span><span v-else class="muted">{{ t('co.fee_later') }}</span></div>
          <template v-if="quote">
            <div class="co-row" v-if="quote.totals.service_charge"><span>{{ t('service_charge') }}</span><span>{{ money(quote.totals.service_charge, cur) }}</span></div>
            <div class="co-row" v-if="quote.totals.tax_amount && !quote.totals.tax_inclusive"><span>{{ t('tax') }} {{ quote.totals.tax_rate_bp / 100 }}%</span><span>{{ money(quote.totals.tax_amount, cur) }}</span></div>
            <div v-for="d in quote.dish_discounts ?? []" :key="'d' + d.promotion_id" class="co-row disc hl" data-testid="dish-line">
              <span>{{ pick(d.name, d.name_en) || t('cp.kind.dish_promotion') }}</span><span>−{{ money(d.amount, cur) }}</span></div>
            <div class="co-row disc hl" v-if="quote.totals.promotion_discount" data-testid="promo-line"><span>{{ promoName || t('discount') }}</span><span>−{{ money(quote.totals.promotion_discount, cur) }}</span></div>
            <button v-if="choices.length" type="button" class="co-coupons" data-testid="coupon-row" @click="showCoupons = true">
              <span>{{ t('cp.coupons') }} <small>{{ coupons.length ? t('cp.picked', { n: coupons.length }) : choices.some(c => c.usable) ? t('cp.none_picked') : t('cp.none_usable', { n: choices.length }) }}</small></span>
              <span><b v-if="couponOff">{{ t('cp.saves', { amt: money(couponOff, cur) }) }}</b> <span class="rtl-flip">›</span></span>
            </button>
            <div v-for="c in coupons" :key="c.id" class="co-row disc hl" :class="{ plat: c.funded_by === 'platform' }" data-testid="coupon-line">
              <span><i class="ic" :class="c.kind">{{ kindBadge(c.kind) }}</i>{{ discountLabel({ ...c, coupon_id: c.id }) }}</span><span>−{{ money(c.amount, cur) }}</span></div>
            <div class="co-row" v-if="quote.totals.discount && (coupons.length || quote.dish_discounts?.length || !quote.totals.promotion_discount)"><span>{{ t('cp.total_off') }}</span><span class="off">−{{ money(quote.totals.discount, cur) }}</span></div>
            <div class="co-row sum"><b>{{ t('total') }}</b><b class="due">{{ money(quote.totals.total, cur) }}</b></div>
            <template v-if="credit">
              <div class="co-row disc hl" data-testid="credit-line"><span>{{ t('co.credit_line') }}</span><span>−{{ money(credit, cur) }}</span></div>
              <div class="co-row sum"><b>{{ t(delivery ? 'co.due_cod' : 'co.due_pickup') }}</b><b class="due" data-testid="checkout-due">{{ money(due, cur) }}</b></div>
            </template>
            <div class="small muted" v-if="quote.totals.platform_subsidy">{{ t('cp.platform_note') }}</div>
            <div class="co-row small muted" v-if="quote.totals.tax_amount && quote.totals.tax_inclusive"><span>{{ t('tax_included') }} {{ quote.totals.tax_rate_bp / 100 }}%</span><span>{{ money(quote.totals.tax_amount, cur) }}</span></div>
          </template>
        </div>
      </div>

      <!-- the bar: the basket, what is due and saved, and the button -->
      <div class="bottombar inline co-barwrap"><div>
        <div class="co-bar">
          <span class="bag" aria-hidden="true">
            <svg viewBox="0 0 24 24" width="24" height="24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"><path d="M3.5 9.5h17l-1.7 9.3a1.6 1.6 0 0 1-1.6 1.3H6.8a1.6 1.6 0 0 1-1.6-1.3z" /><path d="m8.5 9.5 3-5.5M15.5 9.5l-3-5.5M9.5 13.5v3M14.5 13.5v3" /></svg>
            <span v-if="count" class="n">{{ count }}</span>
          </span>
          <div class="amt" data-testid="checkout-total">
            <div class="line1"><b>{{ money(due, cur) }}</b>
              <span v-if="quote?.totals.discount" class="got">{{ t('co.saved', { amt: money(quote.totals.discount, cur) }) }}</span></div>
            <div class="hint" v-if="promoName">{{ t('sf.got_promo', { name: promoName }) }}</div>
            <div class="hint" v-else-if="!quote && delivery">{{ t('co.no_fee_yet') }}</div>
          </div>
          <button class="btn primary go" :class="{ off: !ready }" :disabled="busy || (!ready && !(delivery && !address) && !(mustSchedule && !scheduled && slots.length > 0))" data-testid="place-order" @click="press">{{ buttonText }}</button>
        </div>
      </div></div>
      </aside>
    </div>

    <AddressSheet v-if="showSheet" :slug="mapMode ? slug : undefined" :currency="store?.branch.currency" :selected="cart.addressId" :shop="shopPin" :country="store?.branch.country_code" :fields="store?.branch.contact_fields" :start-new="startNew"
      @select="chosen" @changed="bookChanged" @close="showSheet = false" />
    <CouponSheet v-if="showCoupons && quote" :choices="choices" :currency="cur ?? 'PHP'" @done="couponsChosen" @close="showCoupons = false" />
    <div v-if="showSlots" class="sheet-mask" data-testid="slot-sheet" @click.self="showSlots = false">
      <div class="sheet">
        <div class="sheet-head row between"><b>{{ t(delivery ? 'co.later_delivery' : 'co.later_pickup') }}</b><button class="btn link" @click="showSlots = false">{{ t('close') }}</button></div>
        <div class="sheet-body co-slots">
          <button v-for="s in slots" :key="s" type="button" :class="{ on: scheduled === s }" data-testid="slot" @click="later(s)">{{ whenText(s, tz) }}</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* The customer's warm look: a red bar with the 七宝纹 pattern, cream cards and gold highlights.
   Type: body 14, secondary 12, card titles 15, the bar 17, amounts 17–22. */
.co { --soft-red: #f9e4dc; font-size: 14px; }
.co-top { position: sticky; top: 0; z-index: 25; display: grid; grid-template-columns: 48px 1fr 48px; align-items: center; height: 52px; background: var(--red-bg); color: #fff; }
.co-top h1 { margin: 0; text-align: center; font-size: 17px; font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.co-back { height: 52px; display: grid; place-items: center; color: #fff; }
.co-body { padding: 10px 12px; }
.co-card { border: 0; border-radius: 12px; padding: 12px 14px; background: var(--paper); box-shadow: 0 3px 12px rgba(90, 40, 15, .08); }
.co-card h2 { margin: 0 0 8px; font-size: 15px; font-weight: 600; }
.co .sub { color: var(--muted); font-size: 12px; line-height: 1.45; }
.co .clamp1 { min-width: 0; }

/* phone: one column in the order of the picture — service, where, the order, note, payment */
@media (max-width: 899px) {
  .co-body { display: flex; flex-direction: column; gap: 10px; }
  .co-main, .co-side { display: contents; }
  .co-body .co-main > *, .co-body .co-side > * { margin-top: 0; }
  .co-seg { order: 1; } .co-msg { order: 2; } .co-where { order: 3; } .co-order { order: 4; } .co-note { order: 5; } .co-pay { order: 6; }
  .co { padding-bottom: calc(96px + env(safe-area-inset-bottom)); }
}

.co-seg { display: flex; background: var(--paper); border: 1px solid var(--line); border-radius: 12px; padding: 3px; gap: 3px; }
.co-seg button { flex: 1; min-width: 0; border: 0; background: none; border-radius: 9px; padding: 9px 8px; font: inherit; font-size: 15px; font-weight: 600; color: var(--ink); cursor: pointer; }
.co-seg button.on { background: var(--red); color: #fff; }

.co-label { color: var(--muted); font-size: 12px; margin-bottom: 6px; }
.co-addr { display: flex; align-items: center; gap: 10px; width: 100%; text-align: start; border: 0; background: none; padding: 0 0 12px; cursor: pointer; font: inherit; color: inherit; }
.co-addr.static { cursor: default; padding: 0; }
.co-addr .pin { flex: none; color: var(--red); align-self: flex-start; margin-top: 1px; }
.co-addr .grow { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
.co-addr b { font-size: 16px; font-weight: 600; } .co-addr .tag { margin-inline-end: 6px; font-weight: 400; vertical-align: 2px; }
.co-addr .ph { color: var(--red); font-weight: 600; font-size: 16px; padding: 2px 0; }
.co-addr .chev { color: #a9998a; flex: none; display: grid; }
.co-pick { padding-bottom: 12px; }
.co-whens { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; padding-top: 12px; border-top: 1px solid var(--line); }
.co-whens > :only-child { grid-column: 1 / -1; }
.co-when { display: flex; align-items: center; gap: 6px; min-width: 0; min-height: 44px; padding: 5px 9px; border: 1px solid var(--line); border-radius: 10px; background: #fff; font: inherit; color: inherit; cursor: pointer; text-align: start; }
.co-when .ic { flex: none; width: 19px; height: 19px; color: var(--red); }
.co-when .txt { flex: 1; min-width: 0; display: flex; flex-direction: column; }
.co-when b { font-size: 13px; font-weight: 600; line-height: 1.3; }
.co-when small { color: var(--muted); font-size: 11px; line-height: 1.3; }
.co-when .ok { flex: none; width: 17px; height: 17px; color: var(--red); }
.co-when.on { border: 1.5px solid var(--red); background: var(--gold-soft); }
.co-when.on small { color: var(--red); }
.co-when.off { background: #f3eee7; color: #a9998a; cursor: default; } .co-when.off .ic { color: #b5a998; }

.co-shop { display: flex; align-items: center; gap: 10px; padding-bottom: 10px; }
.co-shop .logo { flex: none; width: 36px; height: 36px; border-radius: 50%; overflow: hidden; background: var(--red); color: #fff; display: grid; place-items: center; }
.co-shop .logo img { width: 100%; height: 100%; object-fit: cover; }
.co-shop b { flex: 1; font-size: 16px; font-weight: 600; }
.co-tag { flex: none; border: 1px solid #cdb48a; color: #7a5a2a; background: #fbf3e3; border-radius: 6px; font-size: 11px; padding: 1px 7px; white-space: nowrap; }
.co-basket { padding: 2px 0; }
/* several baskets: a line between one and the next */
.co-basket + .co-basket { margin-top: 8px; padding-top: 10px; border-top: 1px solid var(--line); }
.co-bhead { color: var(--muted); font-size: 12px; margin: 6px 0; }
.co-line { display: flex; gap: 12px; align-items: flex-start; margin-bottom: 10px; }
.co-line .grow { flex: 1; min-width: 0; } .co-line .n { font-size: 15px; font-weight: 600; }
.co-nrow { display: flex; justify-content: space-between; align-items: flex-start; gap: 8px; }
.co-line .qty { color: var(--muted); font-size: 13px; margin-top: 3px; }
.co-line .price { flex: none; min-width: 56px; text-align: end; font-size: 15px; font-weight: 700; font-variant-numeric: tabular-nums; }
.co-thumb { width: 64px; height: 56px; border-radius: 8px; object-fit: cover; flex: none; }
.co-thumb.none { display: grid; place-items: center; background: var(--gold-soft); color: var(--red); font-size: 20px; font-weight: 600; }
.co-row { display: flex; justify-content: space-between; align-items: center; gap: 10px; padding: 4px 0; }
.co-row > span:last-child, .co-row > b:last-child { flex: none; font-variant-numeric: tabular-nums; }
.co-fees { padding: 8px 0 0; border-top: 1px solid var(--line); }
.co-fees .hl { background: var(--gold-soft); color: var(--red); border-radius: 6px; padding: 4px 8px; margin: 2px -4px; }
.co-fees .ic { display: inline-grid; place-items: center; min-width: 20px; padding: 0 4px; height: 18px; white-space: nowrap; border-radius: 4px; background: var(--red); color: #fff; font-size: 11px; font-style: normal; margin-inline-end: 6px; }
.co-fees .ic.store_delivery { background: #b8792a; } .co-fees .ic.platform_free { background: #6d28d9; }
.co-fees .disc.plat span:last-child { color: #6d28d9; }
.co-fees .off { color: var(--red); }
.co-coupons { display: flex; justify-content: space-between; align-items: center; gap: 8px; width: 100%; margin: 4px 0; padding: 8px 12px; border: 1px solid var(--red); background: #fff; border-radius: 10px; font: inherit; color: inherit; cursor: pointer; text-align: start; }
.co-coupons small { color: var(--muted); margin-inline-start: 4px; } .co-coupons b { color: var(--red); }
.co-fees .sum { border-top: 1px solid var(--line); margin-top: 6px; padding-top: 10px; }
.co-fees .sum b:first-child { font-size: 15px; font-weight: 600; }
.co-fees .sum .due { color: var(--red); font-size: 22px; font-weight: 700; }

.co-note .field > span { font-size: 15px; font-weight: 600; color: var(--ink); }
.co-note input { background: #fff; }
.co-paywith { display: flex; align-items: center; gap: 12px; }
.co-upsell { display: flex; align-items: center; gap: 10px; margin-top: 10px; padding: 9px 12px; border-radius: 10px; background: #fff1d6; border: 1px dashed #e3b766;
  color: #7a3e00; font-size: 13px; text-decoration: none; }
.co-upsell b { color: var(--red); white-space: nowrap; }
.co-credit { display: flex; align-items: center; gap: 12px; margin-top: 12px; padding: 10px 12px; border: 1px solid var(--gold, #d9b26b); border-radius: 10px; background: #fff8ea; cursor: pointer; }
.co-credit input { width: 18px; height: 18px; accent-color: var(--red); flex: none; }
.co-credit .grow { flex: 1; min-width: 0; display: flex; flex-direction: column; }
.co-credit small { color: var(--muted); }
.co-credit .off { color: var(--red); white-space: nowrap; }
.co-paywith .ic { flex: none; color: var(--red); }
.co-paywith .grow { flex: 1; min-width: 0; display: flex; flex-direction: column; }
.co-paywith b { font-size: 15px; font-weight: 600; } .co-paywith small { color: var(--muted); font-size: 12px; }
.co-paywith .ok { flex: none; color: var(--red); }
.co-acct { margin-top: 8px; padding-top: 8px; border-top: 1px solid var(--line); color: var(--muted); font-size: 12px; }

/* the bar: a white card like the menu's cart bar */
.co .co-barwrap > div { background: none; border-top: 0; padding: 0 12px calc(10px + env(safe-area-inset-bottom)); }
.co-bar { display: flex; align-items: center; gap: 12px; min-height: 64px; padding: 8px; background: #fff; border-radius: 14px; box-shadow: 0 4px 18px rgba(59, 31, 20, .16); }
.bag { position: relative; flex: none; width: 48px; height: 48px; border-radius: 12px; background: var(--gold-soft); color: var(--red); display: grid; place-items: center; }
.bag .n { position: absolute; top: -7px; inset-inline-end: -9px; min-width: 22px; height: 20px; padding: 0 6px; border-radius: 999px; background: var(--red); color: #fff; font-size: 12px; font-weight: 700; line-height: 20px; text-align: center; border: 2px solid #fff; box-sizing: content-box; }
.co-bar .amt { flex: 1; min-width: 0; }
.co-bar .line1 { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; min-width: 0; }
.co-bar .line1 b { font-size: 20px; font-weight: 700; line-height: 1.2; font-variant-numeric: tabular-nums; }
.co-bar .got { font-size: 11px; line-height: 18px; padding: 0 6px; border-radius: 4px; background: var(--gold-soft); color: var(--red); font-weight: 600; white-space: nowrap; }
.co-bar .hint { color: var(--muted); font-size: 12px; line-height: 1.4; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; margin-top: 1px; }
.co-bar .go { flex: none; min-height: 46px; min-width: 96px; max-width: 45%; padding: 6px 16px; border-radius: 10px; font-size: 16px; font-weight: 700; background: var(--red); border-color: var(--red); color: #fff; }
.co-bar .go.off { background: #d9cfc3; border-color: #d9cfc3; color: #fff; opacity: 1; }

.co-slots { display: grid; gap: 8px; }
.co-slots button { padding: 12px 14px; border: 1px solid var(--line); border-radius: 10px; background: #fff; font: inherit; text-align: start; cursor: pointer; }
.co-slots button.on { border: 1.5px solid var(--red); background: var(--gold-soft); color: var(--red); font-weight: 600; }

/* iPad and computer: the page between gutters; from 900 the order and the bar sit on the right */
@media (min-width: 768px) {
  .co.phone.wide > .co-body { padding: 16px var(--gut) 24px; }
  .co-top { height: 56px; }
  .co .bottombar.co-barwrap { background: none; border-top: 0; }
  .co .co-barwrap > div { max-width: 760px; }
}
@media (min-width: 900px) {
  .co-side { top: 72px; }
  .co-body .co-main > * + *, .co-body .co-side > * + * { margin-top: 10px; }
  .co .co-barwrap > div { padding: 0; }
  .co-bar { box-shadow: 0 3px 12px rgba(90, 40, 15, .1); }
}
</style>
