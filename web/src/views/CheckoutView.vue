<script setup lang="ts">
import { computed, defineAsyncComponent, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { listAddresses, sorted, usable, whoLine, type Address } from '../addresses'
import { ApiError, get, newKey, post } from '../api'
import { clearLines, linePrice, quoteItems, useCart } from '../cart'
import { customer, loadCustomer, loginRoute, prettyPhone } from '../customer'
import { locale, money, pick, t, whenText, mn } from '../i18n'
import { mallAddressId } from '../mall'
import type { Item, Quote, Storefront } from '../types'
import { discountLabel, kindBadge } from '../coupons'

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
const shopPin = computed(() => { const b = store.value?.branch; return b && b.lat != null && b.lng != null ? { lat: b.lat, lng: b.lng, name: pick(b.name, b.name_en), country: b.country_code } : null })
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

// The idempotency key belongs to one confirmed quote. It is kept across a
// reload so that a retry after a timeout finds the original order.
let idemKey = ''
function keyFor(quoteId: string) {
  const saved = sessionStorage.getItem('order-key')
  if (saved?.startsWith(quoteId + '|')) return saved.slice(quoteId.length + 1)
  const k = newKey()
  sessionStorage.setItem('order-key', quoteId + '|' + k)
  return k
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
async function requote() {
  error.value = ''; short.value = 0
  if (cart.lines.length === 0) { router.replace(`/s/${props.slug}`); return }
  // Without an address there is nothing to price a delivery for yet.
  if (delivery.value && !address.value) { quote.value = null; return }
  if (mustSchedule.value && !scheduled.value) { quote.value = null; error.value = slots.value.length ? t('pick_time') : t('closed'); return }
  try {
    quote.value = await post<Quote>(`/stores/${props.slug}/quotes`, {
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
    idemKey = keyFor(quote.value.id)
  } catch (e) {
    quote.value = null
    error.value = describe(e)
    // The offered times move on; fetch fresh ones when the chosen one is gone.
    if (e instanceof ApiError && (e.code === 'SCHEDULE_INVALID' || e.code === 'STORE_CLOSED')) await loadStore()
    if (e instanceof ApiError && e.code === 'ADDRESS_NOT_FOUND') { use(null); await loadAddresses() }
  }
}

// Ordering needs an account: without one, log in first and come back here.
const toLogin = () => router.replace(loginRoute(`/s/${props.slug}/checkout`, props.slug))
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
interface Shown { item_id: number; name: string; options: string; note: string; qty: number; total: number | null; basket: number }
// The dishes as priced by the server; before there is a price (no address
// yet), as the menu has them.
const lines = computed<Shown[]>(() => {
  if (quote.value) return quote.value.lines.map(l => ({ item_id: l.item_id, name: mn(l), options: l.options.map(o => mn(o)).join(' · '),
    note: l.note, qty: l.qty, total: l.line_total, basket: l.basket || 1 }))
  const sent = quoteItems()
  return cart.lines.map((l, i) => {
    const it = items.value.get(l.item_id), unit = linePrice(l, items.value)
    const names = new Map((it?.groups ?? []).flatMap(g => g.options).map(o => [o.id, mn(o)]))
    return { item_id: l.item_id, name: it ? mn(it) : '', options: l.option_ids.map(id => names.get(id) ?? '').filter(Boolean).join(' · '),
      note: l.note, qty: l.qty, total: unit == null ? null : unit * l.qty, basket: sent[i].basket }
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

const ready = computed(() => !!quote.value && customer.verified && (delivery.value ? !!address.value : cart.name.trim() !== ''))

async function submit() {
  if (!quote.value || busy.value) return
  busy.value = true
  error.value = ''; notice.value = ''
  try {
    const r = await post(`/stores/${props.slug}/orders`, {
      // a delivery takes the name and the address from the saved address
      quote_id: quote.value.id, customer_name: delivery.value ? '' : cart.name, note: cart.note,
    }, { 'Idempotency-Key': idemKey })
    clearLines()
    sessionStorage.removeItem('order-key')
    localStorage.setItem('last-order:' + props.slug, r.access_token)
    router.replace(`/o/${r.access_token}`)
  } catch (e) {
    if (e instanceof ApiError && (e.code === 'QUOTE_CHANGED' || e.code === 'QUOTE_EXPIRED' || e.code === 'QUOTE_USED')) {
      // Show the new amount first; the customer confirms again.
      await requote()
      notice.value = t('amount_changed')
    } else if (e instanceof ApiError && e.code === 'PHONE_NOT_VERIFIED') {
      customer.verified = false
      toLogin()
    } else {
      // On a network error the key is kept, so pressing again is a safe retry.
      error.value = describe(e)
    }
  } finally { busy.value = false }
}
</script>

<template>
  <div class="phone wide co" :data-ready="loaded || undefined">
    <div class="topbar co-top"><router-link :to="`/s/${slug}`" :aria-label="t('back_to_menu')" class="co-back"><span class="rtl-flip">‹</span></router-link><h1>{{ t('co.title') }}</h1></div>
    <div class="section two-cols">
      <div class="stack">
      <div v-if="bothServices" class="co-seg" role="tablist" data-testid="service-toggle">
        <button type="button" role="tab" :aria-selected="delivery" :class="{ on: delivery }" data-testid="service-delivery" @click="setService('delivery')">{{ t('co.svc_delivery') }}</button>
        <button type="button" role="tab" :aria-selected="!delivery" :class="{ on: !delivery }" data-testid="service-pickup" @click="setService('pickup')">{{ t('co.svc_pickup') }}</button>
      </div>
      <div v-if="error" class="banner bad">{{ error }} <button class="btn link" @click="requote">{{ t('retry') }}</button></div>
      <div v-if="notice" class="banner warn">{{ notice }}</div>

      <!-- where to, and when -->
      <div class="card co-card">
        <button v-if="delivery" type="button" class="co-addr" data-testid="checkout-address" :data-empty="!addresses.length || undefined" @click="openSheet">
          <span class="grow" v-if="address">
            <b><span v-if="address.label" class="tag brand">{{ t('ad.label_' + address.label) }}</span>{{ address.place_name }}</b>
            <span>{{ address.unit }}<template v-if="address.place_address"> · {{ address.place_address }}</template></span>
            <span class="small muted">{{ whoLine(address) }}</span>
          </span>
          <span class="grow ph" v-else>{{ t(addresses.length ? 'ad.choose' : 'co.add_address') }}</span>
          <span class="chev"><span class="rtl-flip">›</span></span>
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
          <b>{{ store ? pick(store.branch.name, store.branch.name_en) : '' }}</b>
          <span v-if="store?.branch.address">{{ store.branch.address }}</span>
          <label class="field" style="margin:8px 0 0"><span>{{ t('co.pickup_name') }}</span><input v-model="cart.name" maxlength="80" autocomplete="name" /></label>
        </div>
        <button type="button" class="co-when" :class="{ on: !scheduled && !mustSchedule, off: mustSchedule }" :disabled="mustSchedule" data-testid="when-now" @click="now">
          <b>{{ t(delivery ? 'co.now_delivery' : 'co.now_pickup') }}</b>
          <span v-if="mustSchedule">{{ t('co.closed_now') }}</span>
          <span v-else-if="delivery && !address">{{ t('co.need_address') }}</span>
          <span v-else-if="eta" data-testid="eta">{{ t('co.eta', { n: eta }) }}</span>
        </button>
        <button v-if="slots.length" type="button" class="co-when" :class="{ on: !!scheduled }" data-testid="when-later" @click="showSlots = true">
          <b>{{ t(delivery ? 'co.later_delivery' : 'co.later_pickup') }}</b>
          <span>{{ scheduled ? whenText(scheduled, tz) : t('co.pick_time') }} ›</span>
        </button>
      </div>

      <div class="card co-card">
        <label class="field" style="margin:0"><span>{{ t('order_note') }}</span><input v-model="cart.note" maxlength="300" :placeholder="t('co.note_ph')" /></label>
      </div>
      </div>

      <aside class="stack side">
      <!-- the order -->
      <div class="card co-card" v-if="store">
        <div class="co-shop"><b>{{ pick(store.branch.name, store.branch.name_en) }}</b><span class="co-tag">{{ t(delivery ? 'co.by_store' : 'pickup') }}</span></div>
        <div v-for="b in baskets" :key="b.n" class="co-basket">
          <div v-if="baskets.length > 1" class="co-bhead">🧺 {{ t('co.basket', { n: b.n }) }}</div>
          <div v-for="(l, i) in b.lines" :key="i" class="co-line">
            <img v-if="image(l.item_id)" :src="image(l.item_id)" alt="" class="co-thumb" loading="lazy" />
            <span v-else class="co-thumb none">{{ l.name.slice(0, 1) }}</span>
            <div class="grow">
              <div class="n">{{ l.name }}</div>
              <div class="small muted" v-if="l.options">{{ l.options }}</div>
              <div class="small muted" v-if="l.note">{{ t('note') }}: {{ l.note }}</div>
              <div class="small muted">x {{ l.qty }}</div>
            </div>
            <b class="price" v-if="l.total != null">{{ money(l.total, cur) }}</b>
          </div>
          <div v-if="baskets.length > 1" class="co-row"><span>{{ t('co.basket_subtotal') }}</span><b>{{ money(b.subtotal, cur) }}</b></div>
        </div>
        <div class="co-fees totals">
          <div class="co-row"><span>{{ t('subtotal') }}</span><span>{{ money(subtotal, cur) }}</span></div>
          <div class="co-row" v-if="quote?.totals.packaging_fee"><span><i class="ic">{{ t('ic.bag') }}</i>{{ t('packaging_fee') }}</span><span>{{ money(quote.totals.packaging_fee, cur) }}</span></div>
          <div class="co-row" v-if="delivery"><span><i class="ic">{{ t('ic.ride') }}</i>{{ t('delivery_fee') }}</span>
            <span v-if="quote" data-testid="delivery-fee">{{ money(quote.totals.delivery_fee, cur) }}</span><span v-else class="muted">{{ t('co.fee_later') }}</span></div>
          <template v-if="quote">
            <div class="co-row" v-if="quote.totals.service_charge"><span>{{ t('service_charge') }}</span><span>{{ money(quote.totals.service_charge, cur) }}</span></div>
            <div class="co-row" v-if="quote.totals.tax_amount && !quote.totals.tax_inclusive"><span>{{ t('tax') }} {{ quote.totals.tax_rate_bp / 100 }}%</span><span>{{ money(quote.totals.tax_amount, cur) }}</span></div>
            <div class="co-row disc" v-if="quote.totals.promotion_discount"><span><i class="ic promo">{{ kindBadge('promotion') }}</i>{{ pick(quote.totals.promotion_name, quote.totals.promotion_name_en) || t('discount') }}</span><span>−{{ money(quote.totals.promotion_discount, cur) }}</span></div>
            <button v-if="choices.length" type="button" class="co-coupons" data-testid="coupon-row" @click="showCoupons = true">
              <span>{{ t('cp.coupons') }} <small>{{ coupons.length ? t('cp.picked', { n: coupons.length }) : choices.some(c => c.usable) ? t('cp.none_picked') : t('cp.none_usable', { n: choices.length }) }}</small></span>
              <span><b v-if="couponOff">{{ t('cp.saves', { amt: money(couponOff, cur) }) }}</b> ›</span>
            </button>
            <div v-for="c in coupons" :key="c.id" class="co-row disc" :class="{ plat: c.funded_by === 'platform' }" data-testid="coupon-line">
              <span><i class="ic" :class="c.kind">{{ kindBadge(c.kind) }}</i>{{ discountLabel({ ...c, coupon_id: c.id }) }}</span><span>−{{ money(c.amount, cur) }}</span></div>
            <div class="co-row" v-if="quote.totals.discount && (coupons.length || !quote.totals.promotion_discount)"><b>{{ t('cp.total_off') }}</b><b class="off">{{ money(quote.totals.discount, cur) }}</b></div>
            <div class="co-row sum"><b>{{ t('total') }}</b><b>{{ money(quote.totals.total, cur) }}</b></div>
            <div class="small muted" v-if="quote.totals.platform_subsidy">{{ t('cp.platform_note') }}</div>
            <div class="co-row small muted" v-if="quote.totals.tax_amount && quote.totals.tax_inclusive"><span>{{ t('tax_included') }} {{ quote.totals.tax_rate_bp / 100 }}%</span><span>{{ money(quote.totals.tax_amount, cur) }}</span></div>
          </template>
        </div>
        <div class="co-meta">
          <div class="co-row"><span class="muted">{{ t('pay_method') }}</span><span>{{ t(delivery ? 'cod' : 'pay_at_pickup') }}</span></div>
          <div class="co-row" v-if="customer.verified" data-testid="account-phone"><span class="muted">{{ t('co.account') }}</span><span>{{ prettyPhone(customer.phone) }}</span></div>
        </div>
      </div>

      <div class="bottombar inline"><div>
        <div class="co-bar">
          <div class="amt" data-testid="checkout-total">
            <b>{{ money(quote ? quote.totals.total : subtotal, cur) }}</b>
            <span v-if="quote?.totals.discount">{{ t('co.saved', { amt: money(quote.totals.discount, cur) }) }}</span>
            <span v-else-if="!quote && delivery">{{ t('co.no_fee_yet') }}</span>
          </div>
          <button class="btn primary" :class="{ off: !ready }" :disabled="busy || (!ready && !(delivery && !address) && !(mustSchedule && !scheduled && slots.length > 0))" data-testid="place-order" @click="press">{{ buttonText }}</button>
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
.co { background: linear-gradient(#ffe3d3, var(--bg) 150px) no-repeat, var(--bg); }
.co-top { display: flex; align-items: center; background: none; color: var(--ink); border: 0; }
.co-top h1 { flex: 1; text-align: center; font-size: 18px; margin: 0 34px 0 0; }
.co-back { font-size: 30px; line-height: 1; width: 34px; color: var(--ink); text-decoration: none; }
.co-seg { display: flex; background: #fff; border-radius: 14px; padding: 4px; gap: 4px; }
.co-seg button { flex: 1; border: 0; background: none; border-radius: 10px; padding: 11px 8px; font: inherit; font-size: 16px; font-weight: 600; color: #6b7280; cursor: pointer; }
.co-seg button.on { background: var(--brand); color: #fff; }
.co-card { border-radius: 16px; padding: 14px; border: 0; }
.co-addr { display: flex; align-items: center; gap: 10px; width: 100%; text-align: start; border: 0; background: none; padding: 4px 0 14px; cursor: pointer; font: inherit; color: inherit; border-bottom: 1px solid #f0f1f3; }
.co-addr .grow { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 3px; }
.co-addr b { font-size: 18px; } .co-addr .tag { margin-inline-end: 8px; font-weight: 400; vertical-align: 2px; }
.co-addr .ph { color: var(--brand); font-weight: 700; font-size: 19px; padding: 6px 0; }
.co-addr .chev { color: #8b919a; font-size: 24px; flex: none; }
.co-pick { display: flex; flex-direction: column; gap: 3px; padding: 4px 0 14px; border-bottom: 1px solid #f0f1f3; }
.co-pick b { font-size: 18px; }
.co-when { display: flex; justify-content: space-between; align-items: center; gap: 10px; width: 100%; margin-top: 12px; padding: 14px; border: 1.5px solid #d9dde3; border-radius: 12px; background: #fff; font: inherit; color: inherit; cursor: pointer; text-align: start; }
.co-when b { font-size: 16px; } .co-when span { color: #6b7280; font-size: 14px; text-align: end; }
.co-when.on { border-color: var(--brand); background: #fff1e8; } .co-when.on span { color: var(--brand); font-weight: 600; }
.co-when.off { background: #f4f5f7; color: #9aa1ab; cursor: default; }
.co-shop { display: flex; justify-content: space-between; align-items: center; padding-bottom: 12px; border-bottom: 1px solid #f0f1f3; }
.co-shop b { font-size: 17px; }
.co-tag { border: 1px solid var(--brand); color: var(--brand); border-radius: 6px; font-size: 12px; padding: 1px 7px; }
.co-basket { padding: 10px 0 2px; border-bottom: 1px solid #f0f1f3; }
.co-bhead { color: #6b7280; font-size: 13px; margin-bottom: 8px; }
.co-line { display: flex; gap: 12px; align-items: flex-start; margin-bottom: 12px; }
.co-line .grow { flex: 1; min-width: 0; } .co-line .n { font-size: 15px; }
.co-thumb { width: 54px; height: 54px; border-radius: 10px; object-fit: cover; flex: none; }
.co-thumb.none { display: grid; place-items: center; background: #fdebd9; color: #c2570f; font-size: 20px; font-weight: 600; }
.co-row { display: flex; justify-content: space-between; align-items: center; gap: 10px; padding: 6px 0; }
.co-fees { padding: 8px 0; border-bottom: 1px solid #f0f1f3; }
.co-fees .ic { display: inline-grid; place-items: center; width: 20px; height: 20px; border-radius: 5px; background: var(--brand); color: #fff; font-size: 12px; font-style: normal; margin-inline-end: 8px; }
.co-fees .disc span:last-child { color: var(--brand); }
.co-fees .ic.promo { background: #d97706; width: auto; padding: 0 4px; } .co-fees .ic.store_delivery { background: #b8792a; }
.co-fees .ic.platform_free { background: #6d28d9; width: auto; padding: 0 4px; }
.co-fees .disc.plat span:last-child { color: #6d28d9; }
.co-fees .off { color: var(--brand); }
.co-coupons { display: flex; justify-content: space-between; align-items: center; width: 100%; margin: 4px 0; padding: 10px 12px; border: 1px solid #f3d7a6; background: #fff3e0; border-radius: 10px; font: inherit; color: inherit; cursor: pointer; }
.co-coupons small { color: var(--muted); margin-inline-start: 4px; } .co-coupons b { color: var(--brand); }
.co-fees .sum { font-size: 17px; border-top: 1px solid #f0f1f3; margin-top: 6px; padding-top: 10px; }
.co-meta { padding-top: 8px; }
.co-bar { display: flex; align-items: stretch; background: #1f2329; border-radius: 999px; overflow: hidden; box-shadow: 0 6px 20px rgba(0,0,0,.18); }
.co-bar .amt { flex: 1; min-width: 0; color: #fff; padding: 8px 8px 8px 22px; display: flex; flex-direction: column; justify-content: center; }
.co-bar .amt b { font-size: 21px; line-height: 1.2; } .co-bar .amt span { font-size: 12px; color: #c9cdd3; }
.co-bar .btn { border-radius: 0; min-width: 128px; padding: 0 20px; font-size: 17px; font-weight: 700; min-height: 54px; }
.co-bar .btn.off { background: #c9cdd3; color: #6b7280; border-color: #c9cdd3; opacity: 1; }
.co :deep(.bottombar > div), .co .bottombar > div { background: none; border-top: 0; }
.co-slots { display: grid; gap: 8px; }
.co-slots button { padding: 13px 14px; border: 1.5px solid #d9dde3; border-radius: 10px; background: #fff; font: inherit; text-align: start; cursor: pointer; }
.co-slots button.on { border-color: var(--brand); background: #fff1e8; color: var(--brand); font-weight: 600; }
@media (max-width: 899px) { .co { padding-bottom: 96px; } }
</style>
