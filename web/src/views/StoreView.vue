<script setup lang="ts">
import { computed, defineAsyncComponent, nextTick, onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ApiError, get, post } from '../api'
import { addLine, linePrice, useCart } from '../cart'
import { locale, money, pick, promoLabel, setLocale, t } from '../i18n'
import { mallSpot } from '../mall'
import { defaultTemplate } from '../theme'
import { customer, customerLogout, loadCustomer, loginRoute, prettyPhone } from '../customer'
import { termsAt, type DeliveryTerms } from '../addresses'
import type { Item, Promotion, Storefront } from '../types'
import CartPanel from './CartPanel.vue'
import DishDetail from './DishDetail.vue'
import MediaCarousel from './MediaCarousel.vue'
import MenuList from './MenuList.vue'
import StoreReviews from './StoreReviews.vue'
import StoreCoupons from './StoreCoupons.vue'
import './storeStrings'
import CustomerTabs from './me/CustomerTabs.vue'
import { isFavorite, loadProfile, rememberStore, setFavorite } from './me/me'
import '../orders'
import '../media'
import { chatEnabled } from '../chat'

// The "contact store" entry shows only where chat is set up.
const chatOn = ref(false)
void chatEnabled().then(v => { chatOn.value = v })

// The address book is only downloaded when a customer opens it.
const AddressSheet = defineAsyncComponent(() => import('./AddressSheet.vue'))

const props = defineProps<{ slug: string }>()
const router = useRouter()
const cart = useCart(props.slug)

const store = ref<Storefront | null>(null)
const error = ref('')
const tab = ref<'menu' | 'reviews' | 'info'>('menu')
const showCart = ref(false)

async function load() {
  error.value = ''
  try {
    const s = await get<Storefront>(`/stores/${props.slug}`)
    store.value = s
    document.title = pick(s.branch.name, s.branch.name_en)
    // "Home" in the bottom navigation comes back to the store last looked at.
    rememberStore({ slug: props.slug, name: pick(s.branch.name, s.branch.name_en), currency: s.branch.currency })
    void isFavorite(props.slug).then(on => { favorite.value = on }).catch(() => null)
    void loadProfile()
    // Fall back to a service the store actually offers.
    if (cart.service === 'delivery' && !s.branch.delivery_enabled) cart.service = 'pickup'
    if (cart.service === 'pickup' && !s.branch.pickup_enabled) cart.service = 'delivery'
    if (cart.zoneId && !s.zones.some(z => z.id === cart.zoneId)) cart.zoneId = null
    if (s.branch.zone_mode === 'map') await relocate()
  } catch (e) {
    error.value = e instanceof ApiError && e.status === 404 ? t('not_found') : t('offline')
  }
}
onMounted(load)
onMounted(() => { void loadCustomer() })

// In map mode the delivery terms (the minimum order) follow the spot the
// customer is at: the pin saved in the cart, else where the marketplace placed
// them. Areas may have been redrawn since the last visit, so the server is
// asked again. The address itself is chosen at checkout.
const mapMode = computed(() => store.value?.branch.zone_mode === 'map')
// What the store says about delivering there. Which of its areas that is stays with the store.
const terms = ref<DeliveryTerms | null>(null)
async function relocate() {
  cart.zoneId = null
  const pinned = cart.lat != null && cart.lng != null
  const at = pinned ? { lat: cart.lat!, lng: cart.lng! } : mallSpot()
  if (!at) { terms.value = null; return }
  const r = await termsAt(props.slug, at.lat, at.lng).catch(() => null)
  if (!r) return
  terms.value = r.deliverable ? r : null
  if (!r.deliverable && pinned) cart.lat = cart.lng = null
}
async function checkout() {
  const to = `/s/${props.slug}/checkout`
  // Ordering needs an account; the cart is kept while the customer logs in.
  if (!(await loadCustomer())) router.push(loginRoute(to, props.slug))
  else router.push(to)
}

// The account button: log in, or see who is logged in and log out.
const showAccount = ref(false)
async function account() {
  if (await loadCustomer()) showAccount.value = !showAccount.value
  else router.push(loginRoute(`/s/${props.slug}`, props.slug))
}
async function logout() { await customerLogout(); showAccount.value = false }
const showAddresses = ref(false)
// Keeping the store among the customer's favourites needs an account.
const favorite = ref(false)
async function toggleFavorite() {
  if (!(await loadCustomer())) { router.push(loginRoute(`/s/${props.slug}`, props.slug)); return }
  try {
    await setFavorite(props.slug, !favorite.value)
    favorite.value = !favorite.value
    toast.value = t(favorite.value ? 'mf.fav_on' : 'mf.fav_off')
    setTimeout(() => { toast.value = '' }, 2000)
  } catch { /* stays as it was */ }
}
const shopPin = computed(() => { const b = store.value?.branch; return b && b.lat != null && b.lng != null ? { lat: b.lat, lng: b.lng, name: pick(b.name, b.name_en), country: b.country_code } : null })

// Promotional pictures and videos; a store without any has no such area.
const media = computed(() => store.value?.media ?? [])
const tpl = computed(() => store.value?.templates?.delivery ?? defaultTemplate('delivery'))
const cur = computed(() => store.value?.branch.currency ?? 'PHP')
const items = computed(() => new Map((store.value?.categories ?? []).flatMap(c => c.items).map(i => [i.id, i])))
// A closed store can still take orders for later when it offers time slots.
const canOrder = computed(() => !!store.value && !store.value.branch.paused && (store.value.branch.open_now || store.value.slots.length > 0))
const ahead = computed(() => canOrder.value && !store.value!.branch.open_now)

function orderable(it: Item) {
  return !it.sold_out && (cart.service === 'delivery' ? it.delivery_available : it.pickup_available)
}

const count = computed(() => cart.lines.reduce((n, l) => n + l.qty, 0))
const subtotal = computed(() => cart.lines.reduce((s, l) => s + (linePrice(l, items.value) ?? 0) * l.qty, 0))
const zone = computed(() => store.value?.zones.find(z => z.id === cart.zoneId) ?? null)
const minimum = computed(() => cart.service === 'delivery' && store.value
  ? Math.max(store.value.branch.min_order_amount, (mapMode.value ? terms.value?.min_order : zone.value?.min_order) ?? 0) : 0)
// Promotions for the chosen service, and a nudge towards the nearest one not yet reached.
const promos = computed(() => (store.value?.promotions ?? []).filter(p => cart.service === 'delivery' ? p.applies_delivery : p.applies_pickup && p.kind !== 'free_delivery'))
// Only offers running right now count on the cart bar.
const livePromos = computed(() => promos.value.filter(p => p.live_now !== false))
const nextPromo = computed(() => {
  const ahead = livePromos.value.filter(p => p.threshold > subtotal.value).sort((a, b) => a.threshold - b.threshold)[0]
  return ahead && subtotal.value > 0 ? ahead : null
})
// The offer the cart already earns, worked out as the server will at checkout:
// the one that saves the most. The delivery fee is known only once there is an
// address, so free delivery is weighed with the fee of where the customer is.
const deliveryFee = computed(() => cart.service === 'delivery' ? (mapMode.value ? terms.value?.fee : zone.value?.fee) ?? 0 : 0)
function saving(p: Promotion, sub: number) {
  if (p.kind === 'amount_off') return Math.min(p.value, sub)
  if (p.kind === 'percent_off') return Math.min(Math.round(sub * p.value / 10000), p.max_discount ?? Infinity, sub)
  return 0
}
const deal = computed(() => {
  const sub = subtotal.value
  let best: Promotion | null = null, bestTotal = -1
  for (const p of livePromos.value) {
    if (sub <= 0 || p.threshold > sub) continue
    const total = p.kind === 'free_delivery' ? deliveryFee.value : saving(p, sub)
    // free delivery with the fee not yet known still counts, behind any money off
    if (total > bestTotal || (total === bestTotal && best?.kind === 'free_delivery' && p.kind !== 'free_delivery')) { best = p; bestTotal = total }
  }
  const off = best && best.kind !== 'free_delivery' ? saving(best, sub) : 0
  return { promo: best, off, now: sub - off }
})
const hasUnavailable = computed(() => cart.lines.some(l => {
  const it = items.value.get(l.item_id)
  return !it || !orderable(it) || linePrice(l, items.value) === null
}))

// ---- the dish page ----
const dish = ref<Item | null>(null)
// Choosing options goes on in the options sheet, over the dish page's place.
function addFromDish() {
  const it = dish.value!
  if (it.groups.length) dish.value = null
  open(it)
}

// ---- item sheet ----
const sheet = reactive<{ item: Item | null; chosen: Set<number>; qty: number; note: string }>({ item: null, chosen: new Set(), qty: 1, note: '' })

function open(it: Item) {
  if (!orderable(it)) return
  if (it.groups.length === 0) { addLine(it, [], 1, ''); return }
  sheet.item = it; sheet.chosen = new Set(); sheet.qty = 1; sheet.note = ''
}

function toggle(groupIdx: number, optionId: number) {
  const g = sheet.item!.groups[groupIdx]
  const inGroup = g.options.filter(o => sheet.chosen.has(o.id)).map(o => o.id)
  if (sheet.chosen.has(optionId)) { sheet.chosen.delete(optionId); return }
  if (g.max_select === 1) inGroup.forEach(id => sheet.chosen.delete(id))
  else if (inGroup.length >= g.max_select) return
  sheet.chosen.add(optionId)
}

const sheetMissing = computed(() => sheet.item?.groups.find(g =>
  g.options.filter(o => sheet.chosen.has(o.id)).length < g.min_select) ?? null)
const sheetPrice = computed(() => {
  if (!sheet.item) return 0
  const extra = sheet.item.groups.flatMap(g => g.options).filter(o => sheet.chosen.has(o.id)).reduce((s, o) => s + o.price_delta, 0)
  return (sheet.item.price + extra) * sheet.qty
})

function confirmSheet() {
  if (!sheet.item || sheetMissing.value) return
  addLine(sheet.item, [...sheet.chosen], sheet.qty, sheet.note)
  sheet.item = null
}

function groupHint(g: { min_select: number; max_select: number }) {
  if (g.min_select > 0 && g.min_select === g.max_select) return `${t('required')} · ${t('pick_n', { n: g.min_select })}`
  if (g.min_select > 0) return `${t('required')} · ${t('up_to_n', { n: g.max_select })}`
  return `${t('optional')} · ${t('up_to_n', { n: g.max_select })}`
}

// The − on a dish takes one from the basket that is being filled.
function dec(it: Item) {
  const l = cart.lines.find(x => x.item_id === it.id && x.basket === cart.basket)
  if (!l) return
  if (--l.qty <= 0) cart.lines.splice(cart.lines.indexOf(l), 1)
}

function qtyOf(it: Item) {
  return cart.lines.filter(l => l.item_id === it.id && l.basket === cart.basket).reduce((n, l) => n + l.qty, 0)
}

// ---- the store card ----
const name = computed(() => store.value ? pick(store.value.branch.name, store.value.branch.name_en) : '')
const one = (n: number) => n.toFixed(1).replace(/\.0$/, '')
const salesText = computed(() => { const n = store.value?.sales ?? 0; return n >= 1000 ? Math.floor(n / 100) * 100 + '+' : String(n) })
const showPromos = ref(false)
const toast = ref('')
async function share() {
  const data = { title: name.value, url: location.href }
  try {
    if (navigator.share) { await navigator.share(data); return }
    await navigator.clipboard.writeText(location.href)
    toast.value = t('sf.link_copied')
    setTimeout(() => { toast.value = '' }, 2000)
  } catch { /* the customer closed the share sheet */ }
}

// The notice scrolls only when it does not fit on its line.
const marq = ref<HTMLElement | null>(null)
const scrolls = ref(false)
watch(marq, el => { if (el) scrolls.value = el.scrollWidth > el.clientWidth + 1 }, { flush: 'post' })

// ---- search within the menu ----
const searching = ref(false)
const query = ref('')
const searchBox = ref<HTMLInputElement | null>(null)
async function toggleSearch() {
  searching.value = !searching.value
  query.value = ''
  if (searching.value) { tab.value = 'menu'; await nextTick(); searchBox.value?.focus() }
}
// Dishes offered only at the table are not on the delivery menu.
const onlineCats = computed(() => (store.value?.categories ?? []).map(c => ({ ...c, items: c.items.filter(i => i.delivery_available || i.pickup_available) })))
const shownCats = computed(() => {
  const all = onlineCats.value
  const q = query.value.trim().toLowerCase()
  if (!q) return all
  return all.map(c => ({ ...c, items: c.items.filter(i => (i.name + ' ' + i.name_en).toLowerCase().includes(q)) })).filter(c => c.items.length)
})

// ---- categories the order must contain something from ----
const missing = computed(() => onlineCats.value.filter(c => c.required && c.items.some(orderable)
  && !cart.lines.some(l => items.value.get(l.item_id)?.category_id === c.id)))
const menu = ref<InstanceType<typeof MenuList> | null>(null)
async function toRequired() {
  showCart.value = false; searching.value = false; query.value = ''; tab.value = 'menu'
  await nextTick()
  menu.value?.scrollTo(missing.value[0].id)
}
const blocked = computed(() => !canOrder.value || hasUnavailable.value || minimum.value > subtotal.value || count.value === 0)

// ---- hours ----
const hoursText = computed(() => {
  const b = store.value!.branch
  if (b.open_time === b.close_time) return t('sf.all_day')
  return t('sf.daily', { from: b.open_time, to: (b.close_time < b.open_time ? t('sf.next_day') : '') + b.close_time })
})

// ---- report the store ----
const reasons = ['food_safety', 'wrong_info', 'bad_service', 'fraud', 'other']
const report = reactive({ open: false, reason: '', detail: '', contact: '', busy: false, error: '', done: false })
function openReport() { Object.assign(report, { open: true, reason: '', detail: '', contact: '', busy: false, error: '', done: false }) }
async function sendReport() {
  report.busy = true; report.error = ''
  try {
    await post(`/stores/${props.slug}/reports`, { reason: report.reason, detail: report.detail, contact: report.contact })
    report.done = true
  } catch (e) { report.error = (e as Error).message }
  report.busy = false
}
</script>

<template>
  <div class="phone wide shop warm" :class="{ solo: tab !== 'menu' }">
    <div v-if="error" class="section stack">
      <div class="banner bad">{{ error }}</div>
      <button class="btn" @click="load">{{ t('retry') }}</button>
    </div>
    <div v-else-if="!store" class="section muted">{{ t('loading') }}</div>
    <template v-else>
      <div class="hero" :class="{ media: media.length || tpl.cover_url }">
      <!-- promotional pictures and videos, else the cover: the same 16:9 frame in every store -->
      <div v-if="media.length" class="hero-media"><MediaCarousel :media="media" /><i class="lip"></i></div>
      <div v-else-if="tpl.cover_url" class="hero-media"><div class="still" data-testid="hero-cover"><img :src="tpl.cover_url" alt="" /></div><i class="lip"></i></div>
      <!-- the store card -->
      <header class="shophead">
        <div class="tools">
          <!-- back to the marketplace; delivery or pickup is chosen at checkout -->
          <router-link to="/" class="ico" :aria-label="t('ml.back_mall')" :title="t('ml.back_mall')" data-testid="back-mall">
            <svg viewBox="0 0 24 24" width="24" height="24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="m14.5 5-7 7 7 7" /></svg></router-link>
          <span class="grow"></span>
          <button class="ico" :class="{ on: searching }" :aria-label="t('sf.search')" data-testid="search-toggle" @click="toggleSearch">
            <svg viewBox="0 0 24 24" width="24" height="24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="10.5" cy="10.5" r="6.5" /><path d="m15.5 15.5 5 5" /></svg></button>
          <button class="ico" :class="{ fav: favorite }" :aria-label="t(favorite ? 'mf.faved' : 'mf.fav')" :aria-pressed="favorite" data-testid="favorite" @click="toggleFavorite">
            <svg viewBox="0 0 24 24" width="24" height="24" :fill="favorite ? 'currentColor' : 'none'" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3.500l2.6 5.3 5.9.9-4.3 4.1 1 5.8-5.2-2.8-5.2 2.8 1-5.8-4.3-4.1 5.9-.900z" /></svg></button>
          <button class="ico" :aria-label="t('sf.share')" data-testid="share" @click="share">
            <svg viewBox="0 0 24 24" width="24" height="24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="18" cy="5" r="2.6" /><circle cx="6" cy="12" r="2.6" /><circle cx="18" cy="19" r="2.6" /><path d="m8.3 10.7 7.4-4.4M8.3 13.3l7.4 4.4" /></svg></button>
          <div class="acct">
            <button class="ico" :class="{ on: showAccount, in: customer.verified }" :aria-label="t('sf.account')" data-testid="account" @click="account">
              <svg viewBox="0 0 24 24" width="24" height="24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="12" cy="8.5" r="3.8" /><path d="M4.5 20c1.2-3.6 4-5.4 7.5-5.4s6.3 1.8 7.5 5.4" /></svg></button>
            <div v-if="showAccount && customer.verified" class="acct-menu" data-testid="account-menu">
              <div class="small muted">{{ t('sf.logged_in') }}</div>
              <div class="bold">{{ prettyPhone(customer.phone) }}</div>
              <router-link to="/me" class="btn small block" data-testid="my-center">{{ t('me.me') }}</router-link>
              <button class="btn small block" data-testid="my-orders" @click="router.push('/orders')">{{ t('ol.title') }}</button>
              <button class="btn small block" data-testid="my-addresses" @click="showAddresses = true; showAccount = false">{{ t('ad.mine') }}</button>
              <button class="btn small block" data-testid="logout" @click="logout">{{ t('sf.logout') }}</button>
            </div>
          </div>
          <button class="lang" @click="setLocale(locale === 'zh' ? 'en' : 'zh')">{{ locale === 'zh' ? 'EN' : '中文' }}</button>
        </div>
        <div class="ident">
          <img v-if="tpl.logo_url" :src="tpl.logo_url" alt="" class="logo" />
          <div v-else class="logo ph">{{ name.slice(0, 1) }}</div>
          <div class="grow">
            <h1>{{ name }}</h1>
            <div class="meta">
              <span v-if="store.rating.count" class="score" data-testid="score"><i>★</i>{{ one(store.rating.average) }}</span>
              <span v-else class="muted">{{ t('sf.no_rating') }}</span>
              <span class="muted" data-testid="sales">{{ t('sf.sales', { n: salesText }) }}</span>
              <span class="state" :class="{ off: !canOrder }">{{ canOrder ? t('open') : store.branch.paused ? t('paused') : t('closed') }}</span>
            </div>
            <div class="chips">
              <span v-if="store.branch.delivery_enabled">{{ t('sf.own_delivery') }}</span>
              <span v-if="store.branch.pickup_enabled">{{ t('sf.self_pickup') }}</span>
              <span v-if="store.branch.delivery_enabled">{{ t('sf.cod') }}</span>
            </div>
          </div>
        </div>
        <div class="promoline" v-if="promos.length">
          <div class="chips brand"><span v-for="p in promos.slice(0, 3)" :key="p.id" :title="pick(p.name, p.name_en)">{{ promoLabel(p, cur) }}</span></div>
          <button class="more" data-testid="promos-more" @click="showPromos = true">{{ t('sf.promos_n', { n: promos.length }) }} ›</button>
        </div>
        <StoreCoupons :slug="slug" :currency="cur" />
        <div class="notice" v-if="store.branch.notice" data-testid="notice">
          <span class="lbl">{{ t('sf.notice') }}</span>
          <!-- a notice too long for the line scrolls round and round -->
          <div ref="marq" class="marq" :class="{ run: scrolls }"><span :style="{ animationDuration: Math.max(8, store.branch.notice.length / 4) + 's' }">{{ store.branch.notice }}<template v-if="scrolls"><i></i>{{ store.branch.notice }}<i></i></template></span></div>
        </div>
      </header>
      </div>

      <nav class="shoptabs">
        <button :class="{ on: tab === 'menu' }" data-tab="menu" @click="tab = 'menu'">{{ t('sf.tab_menu') }}</button>
        <button :class="{ on: tab === 'reviews' }" data-tab="reviews" @click="tab = 'reviews'">{{ t('sf.tab_reviews') }}<small v-if="store.rating.count">{{ store.rating.count }}</small></button>
        <button :class="{ on: tab === 'info' }" data-tab="info" @click="tab = 'info'">{{ t('sf.tab_store') }}</button>
      </nav>

      <div class="shop-cols"><div class="shop-main">
      <StoreReviews v-if="tab === 'reviews'" :slug="slug" :summary="store.rating" :timezone="store.branch.timezone" :delivery="store.branch.delivery_enabled" />

      <!-- the store (C02) -->
      <div v-else-if="tab === 'info'" class="about">
        <section>
          <h3>{{ t('sf.hours') }}</h3><p>{{ hoursText }}</p>
          <template v-if="store.branch.delivery_enabled">
            <h3>{{ t('sf.delivery_hours') }}</h3>
            <p>{{ t('sf.delivery_same') }}<template v-if="store.slots.length">，{{ t('sf.can_schedule') }}</template></p>
          </template>
        </section>
        <section v-if="store.branch.address || store.branch.phone">
          <template v-if="store.branch.address"><h3>{{ t('address') }}</h3><p>{{ store.branch.address }}</p></template>
          <template v-if="store.branch.phone"><h3>{{ t('phone') }}</h3><p><a :href="'tel:' + store.branch.phone">{{ store.branch.phone }}</a></p></template>
        </section>
        <section v-if="chatOn">
          <router-link class="btn block" data-testid="store-chat" :to="`/chat/${store.branch.slug}`">{{ t('ch.contact_store') }}</router-link>
        </section>
        <section v-if="store.branch.delivery_enabled && store.zones.length">
          <h3>{{ t('zones') }}</h3>
          <div v-for="z in store.zones" :key="z.id" class="row between zone">
            <span>{{ pick(z.name, z.name_en) }}</span><span class="price">{{ money(z.fee, cur) }}</span>
          </div>
        </section>
        <section><h3>{{ t('sf.about') }}</h3><p :class="{ muted: !store.branch.intro }">{{ store.branch.intro || t('sf.none') }}</p></section>
        <section><h3>{{ t('sf.notice_title') }}</h3><p :class="{ muted: !store.branch.notice }">{{ store.branch.notice || t('sf.none') }}</p></section>
        <section><button class="reportbtn" data-testid="report" @click="openReport">{{ t('sf.report') }}</button></section>
      </div>

      <!-- menu (C01) -->
      <div v-else class="menuwrap">
        <div class="pre" v-if="!canOrder"><div class="banner warn">{{ store.branch.paused ? t('paused') : t('closed') }}</div></div>
        <div class="pre" v-else-if="ahead"><div class="banner info">{{ t('closed_can_schedule') }}</div></div>
        <div class="pre" v-if="searching">
          <input ref="searchBox" v-model="query" type="search" class="searchbox" :placeholder="t('sf.search')" data-testid="search-box" />
        </div>
        <p v-if="query.trim() && !shownCats.length" class="muted" style="text-align:center;padding:36px 0;margin:0">{{ t('sf.search_none') }}</p>
        <MenuList ref="menu" :categories="shownCats" :currency="cur" :layout="tpl.layout" :can-order="orderable" :qty-of="qtyOf" stats
          @add="open" @dec="dec" @show="dish = $event" />
      </div>
      </div>

      <!-- the cart beside the menu on a computer; smaller screens use the bar below -->
      <aside class="sidecart" v-if="tab === 'menu'" data-testid="side-cart">
        <div class="card">
          <h2>{{ t('cart') }}<span v-if="count" class="muted small"> · {{ count }}</span></h2>
          <CartPanel :store="store" :items="items" :currency="cur" :orderable="orderable" />
          <div class="foot">
            <div v-if="count" class="sumline">
              <b class="now">{{ money(deal.now, cur) }}</b>
              <s v-if="deal.off" class="was">{{ money(subtotal, cur) }}</s>
            </div>
            <div v-if="count && deal.promo" class="got" data-testid="side-deal">{{ t('sf.got_promo', { name: promoLabel(deal.promo, cur) }) }}<template v-if="deal.off"> · {{ t('sf.saved', { amt: money(deal.off, cur) }) }}</template></div>
            <div v-if="count && minimum > subtotal" class="small muted">{{ t('below_min', { amt: money(minimum - subtotal, cur) }) }}</div>
            <div v-else-if="nextPromo" class="small" style="color:var(--brand-dark)">{{ t('more_for_promo', { amt: money(nextPromo.threshold - subtotal, cur), name: promoLabel(nextPromo, cur) }) }}</div>
            <button v-if="count && missing.length" class="btn block big must" data-testid="required-missing" @click="toRequired">{{ t('sf.required_pick') }} · {{ pick(missing[0].name, missing[0].name_en) }}</button>
            <button v-else class="btn primary block big" data-testid="go-checkout" :disabled="blocked" @click="checkout">{{ t('checkout') }}<template v-if="count"> · {{ money(deal.now, cur) }}</template></button>
          </div>
        </div>
      </aside>
      </div>

      <!-- cart bar: what the cart costs now, what it would have cost, the offer it earns and the next one -->
      <div class="bottombar cartbar" :class="{ over: showCart }" v-if="tab === 'menu' || count > 0 || showCart">
        <div>
          <button class="bag" :class="{ empty: !count }" :aria-label="t('cart')" data-testid="open-cart" @click="showCart = !showCart && count > 0">
            <svg viewBox="0 0 24 24" width="24" height="24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"><path d="M3.5 9.5h17l-1.7 9.3a1.6 1.6 0 0 1-1.6 1.3H6.8a1.6 1.6 0 0 1-1.6-1.3z" /><path d="m8.5 9.5 3-5.5M15.5 9.5l-3-5.5M9.5 13.5v3M14.5 13.5v3" /></svg>
            <span v-if="count" class="n" data-testid="cart-count">{{ count }}</span>
          </button>
          <div class="sums" @click="showCart = !showCart && count > 0">
            <div class="line1">
              <b class="amt" data-testid="cart-total">{{ money(deal.now, cur) }}</b>
              <s v-if="deal.off" class="was" data-testid="cart-was">{{ money(subtotal, cur) }}</s>
              <span v-if="deal.promo" class="got" data-testid="cart-deal">{{ deal.off ? t('sf.saved', { amt: money(deal.off, cur) }) : promoLabel(deal.promo, cur) }}</span>
            </div>
            <div class="hint">
              <template v-if="!count">{{ t('cart_empty') }}</template>
              <span v-else-if="hasUnavailable" class="bad">{{ t('sf.has_unavailable') }}</span>
              <template v-else-if="minimum > subtotal">{{ t('below_min', { amt: money(minimum - subtotal, cur) }) }}</template>
              <template v-else-if="nextPromo">{{ t('more_for_promo', { amt: money(nextPromo.threshold - subtotal, cur), name: promoLabel(nextPromo, cur) }) }}</template>
              <template v-else-if="deal.promo">{{ t('sf.got_promo', { name: promoLabel(deal.promo, cur) }) }}</template>
            </div>
          </div>
          <button v-if="count && missing.length" class="go must" data-testid="required-missing" @click="toRequired">{{ t('sf.required_pick') }}</button>
          <button v-else class="btn primary go" :disabled="blocked" @click="checkout">{{ !canOrder ? (store.branch.paused ? t('paused') : t('closed')) : t('checkout') }}</button>
        </div>
      </div>

      <AddressSheet v-if="showAddresses" manage :slug="mapMode ? slug : undefined" :currency="cur" :shop="shopPin" :country="store?.branch.country_code" :fields="store?.branch.contact_fields" @close="showAddresses = false" />

      <!-- the dish page -->
      <DishDetail v-if="dish" :item="dish" :currency="cur" :can-order="orderable(dish)" :qty="qtyOf(dish)"
        @add="addFromDish" @dec="dec(dish!)" @close="dish = null" />

      <!-- item options (C04/C05) -->
      <div class="sheet-mask" v-if="sheet.item" @click.self="sheet.item = null">
        <div class="sheet">
          <div class="sheet-head">
            <h2>{{ pick(sheet.item.name, sheet.item.name_en) }}</h2>
            <div class="muted small" v-if="sheet.item.description">{{ sheet.item.description }}</div>
          </div>
          <div class="sheet-body stack">
            <div v-for="(g, gi) in sheet.item.groups" :key="g.id">
              <div class="row between"><h3>{{ pick(g.name, g.name_en) }}</h3><span class="muted small">{{ groupHint(g) }}</span></div>
              <label v-for="o in g.options" :key="o.id" class="opt" :class="{ off: o.sold_out }">
                <input :type="g.max_select === 1 ? 'radio' : 'checkbox'" :checked="sheet.chosen.has(o.id)" :disabled="o.sold_out"
                  @click.prevent="toggle(gi, o.id)" />
                <span class="grow">{{ pick(o.name, o.name_en) }}<span v-if="o.sold_out" class="muted small"> · {{ t('sold_out') }}</span></span>
                <span class="price muted" v-if="o.price_delta">+{{ money(o.price_delta, cur) }}</span>
              </label>
            </div>
            <label class="field"><span>{{ t('note') }}</span><input v-model="sheet.note" maxlength="200" :placeholder="t('note_ph')" /></label>
          </div>
          <div class="sheet-foot row between">
            <span class="stepper"><button @click="sheet.qty = Math.max(1, sheet.qty - 1)">−</button><span>{{ sheet.qty }}</span><button class="fill" @click="sheet.qty = Math.min(99, sheet.qty + 1)">+</button></span>
            <button class="btn primary grow" :disabled="!!sheetMissing" @click="confirmSheet">
              {{ sheetMissing ? `${t('required')}: ${pick(sheetMissing.name, sheetMissing.name_en)}` : `${t('add_to_cart')} · ${money(sheetPrice, cur)}` }}
            </button>
          </div>
        </div>
      </div>

      <!-- cart (C06): rises above the cart bar, which stays in reach -->
      <div class="sheet-mask cartmask" v-if="showCart" @click.self="showCart = false">
        <div class="sheet cartsheet" data-testid="cart-sheet">
          <div class="carthead">
            <div class="grow">
              <h2>{{ t('cart') }}<span class="cnt">{{ count }}</span></h2>
              <p v-if="store.branch.baskets_enabled">{{ t('sf.basket_tip') }}</p>
            </div>
            <button class="x" :aria-label="t('close')" data-testid="close-cart" @click="showCart = false">
              <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M6 6l12 12M18 6 6 18" /></svg></button>
          </div>
          <CartPanel :store="store" :items="items" :currency="cur" :orderable="orderable" @empty="showCart = false" />
        </div>
      </div>

      <!-- every offer -->
      <div class="sheet-mask" v-if="showPromos" @click.self="showPromos = false">
        <div class="sheet" data-testid="promo-sheet">
          <div class="sheet-head row between"><h2>{{ t('sf.promos') }}</h2><button class="btn link" @click="showPromos = false">{{ t('close') }}</button></div>
          <div class="sheet-body stack">
            <div v-for="p in promos" :key="p.id" class="row between">
              <span>{{ pick(p.name, p.name_en) }}</span><span class="tag brand">{{ promoLabel(p, cur) }}</span>
            </div>
          </div>
        </div>
      </div>

      <!-- report the store -->
      <div class="sheet-mask" v-if="report.open" @click.self="report.open = false">
        <form class="sheet" data-testid="report-sheet" @submit.prevent="sendReport">
          <div class="sheet-head"><h2>{{ t('sf.report') }}</h2></div>
          <div class="sheet-body stack" v-if="report.done"><div class="banner ok">{{ t('sf.report_done') }}</div></div>
          <div class="sheet-body stack" v-else>
            <div v-if="report.error" class="banner bad">{{ report.error }}</div>
            <div><div class="muted small" style="margin-bottom:4px">{{ t('sf.report_reason') }}</div>
              <label v-for="r in reasons" :key="r" class="opt"><input type="radio" name="reason" :value="r" v-model="report.reason" /><span class="grow">{{ t('rr.' + r) }}</span></label></div>
            <label class="field"><span>{{ t('sf.report_detail') }}</span><textarea v-model="report.detail" rows="3" maxlength="500"></textarea></label>
            <label class="field"><span>{{ t('sf.report_contact') }}</span><input v-model="report.contact" maxlength="80" /></label>
          </div>
          <div class="sheet-foot row">
            <button type="button" class="btn grow" @click="report.open = false">{{ report.done ? t('close') : t('cancel') }}</button>
            <button v-if="!report.done" class="btn primary grow" :disabled="report.busy || !report.reason || (report.reason === 'other' && !report.detail.trim())">{{ t('sf.report_send') }}</button>
          </div>
        </form>
      </div>

      <div v-if="toast" class="toast">{{ toast }}</div>
      <CustomerTabs active="home" class="shoptabbar" />
    </template>
  </div>
</template>

<style scoped>
.shop { --star: #f5a623; --menu-top: 50px; background: var(--bg);
  /* the marketplace's colours: cream cards on a beige page, prices in the deep red */
  --menu-bg: var(--paper); --rail-bg: #f3e6d2; --price: var(--red); --soft-red: #f9e4dc; }

/* the store card */
.shophead { background: var(--paper); border-radius: 0 0 18px 18px; padding: 12px 16px 14px; display: grid; grid-template-columns: minmax(0, 1fr); gap: 10px; }
.tools { display: flex; align-items: center; gap: 6px; }
.tools .seg.light button { padding: 5px 14px; font-size: 14px; }
.ico { width: 38px; height: 38px; border: 0; background: none; color: var(--ink); border-radius: 50%; display: grid; place-items: center; cursor: pointer; }
.ico:hover, .ico.on { background: var(--gold-soft); }
.ico.in { color: var(--brand); }
.ico.fav { color: var(--star); }
/* on a phone the customer's navigation lies under the cart bar */
.shoptabbar { display: none; }
@media (max-width: 767.98px) {
  .shoptabbar { display: block; }
  .shop { --tabs: calc(var(--tabbar-h) + env(safe-area-inset-bottom)); padding-bottom: calc(96px + var(--tabs)); }
  .shop .bottombar.cartbar { bottom: var(--tabbar-h); }
  .shop .cartmask { padding-bottom: var(--tabs); }
  .shop .toast { bottom: calc(96px + var(--tabs)); }
}
.acct { position: relative; }
.acct-menu { position: absolute; right: 0; top: 44px; z-index: 30; background: var(--paper); border: 1px solid var(--line); border-radius: 12px; box-shadow: 0 8px 24px rgba(0, 0, 0, .14); padding: 12px; width: 200px; display: grid; gap: 6px; }
.lang { color: var(--muted); margin-left: 4px; }
.ident { display: flex; gap: 12px; align-items: flex-start; }
.logo { width: 76px; height: 76px; border-radius: 10px; border: 1px solid var(--line); object-fit: cover; flex: none; background: var(--paper); }
.logo.ph { display: grid; place-items: center; background: var(--brand-soft); color: var(--brand-dark); font-size: 34px; font-weight: 700; border-color: transparent; }
.ident h1 { font-size: 20px; line-height: 1.25; overflow-wrap: anywhere; }
.meta { display: flex; flex-wrap: wrap; align-items: center; gap: 4px 12px; margin-top: 4px; font-size: 14px; }
.score { color: var(--star); font-weight: 700; font-size: 15px; } .score i { font-style: normal; margin-right: 2px; }
.state { font-size: 12px; font-weight: 600; color: var(--ok); } .state.off { color: var(--warn); }
.chips { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 6px; min-width: 0; }
.chips span { border: 1px solid #cdbba3; color: var(--muted); border-radius: 5px; padding: 0 6px; font-size: 12px; line-height: 20px; white-space: nowrap; }
.chips.brand { margin: 0; flex: 1; flex-wrap: nowrap; overflow: hidden; }
.chips.brand span { border-color: var(--brand); color: var(--brand-dark); overflow: hidden; text-overflow: ellipsis; flex: 0 1 auto; }
.promoline { display: flex; align-items: center; gap: 8px; }
.more { border: 0; background: none; color: var(--muted); font-size: 14px; white-space: nowrap; cursor: pointer; padding: 0; }
.notice { display: flex; gap: 4px; color: var(--muted); font-size: 14px; }
.notice .lbl { flex: none; }
.marq { overflow: hidden; flex: 1; min-width: 0; white-space: nowrap; }
.marq span { display: inline-block; }
.marq i { display: inline-block; width: 48px; }
.marq.run span { animation: marq 14s linear infinite; }
@keyframes marq { to { transform: translateX(-50%); } }
@media (prefers-reduced-motion: reduce) { .marq.run span { animation: none; } }

/* With promo media the card stands on the lower edge of the pictures and the
   buttons lie over them; from tablet width up the pictures sit beside the card. */
.hero { position: relative; }
.hero-media { position: relative; --promo-foot: 28px; }
.still { aspect-ratio: 16 / 9; background: #1c1c1c; overflow: hidden; }
.still img { display: block; width: 100%; height: 100%; object-fit: cover; }
.hero-media::before { content: ''; position: absolute; inset: 0 0 auto; height: 72px; z-index: 1; pointer-events: none; background: linear-gradient(rgba(0, 0, 0, .45), transparent); }
.lip { position: absolute; left: 0; right: 0; bottom: -1px; height: 18px; background: var(--paper); border-radius: 18px 18px 0 0; pointer-events: none; }
.hero.media .shophead { padding-top: 0; }
@media (max-width: 767.98px) {
  .hero.media .tools { position: absolute; top: 8px; left: 12px; right: 10px; z-index: 5; color: #fff; }
  .hero.media .tools .ico, .hero.media .tools .lang { color: #fff; filter: drop-shadow(0 1px 2px rgba(0, 0, 0, .5)); }
  .hero.media .tools .lang { border-color: rgba(255, 255, 255, .85); }
  .hero.media .tools .ico:hover, .hero.media .tools .ico.on { background: rgba(255, 255, 255, .22); }
  .hero.media .acct-menu { color: var(--ink); }
}

/* the three tabs */
.shoptabs { position: sticky; top: 0; z-index: 15; display: flex; gap: 30px; height: 50px; margin-top: 10px; padding: 0 22px; background: var(--paper); border-radius: 18px 18px 0 0; border-bottom: 1px solid var(--line); }
.shoptabs button { position: relative; border: 0; background: none; padding: 0 2px; font-size: 16px; color: var(--muted); cursor: pointer; }
.shoptabs button.on { color: var(--ink); font-weight: 700; }
.shoptabs button.on::after { content: ''; position: absolute; left: 50%; bottom: 6px; width: 22px; height: 3px; margin-left: -11px; border-radius: 2px; background: var(--brand); }
.shoptabs small { font-size: 11px; color: var(--muted); margin-left: 2px; font-weight: 400; }

/* the menu fills the screen under the tabs, so the category rail runs to the bottom however few dishes there are */
.menuwrap { background: var(--paper); min-height: calc(100vh - 50px); min-height: calc(100dvh - 50px); display: flex; flex-direction: column; }
.menuwrap > .menu { flex: 1; }
.pre { padding: 10px 14px 0; } .pre:last-of-type { padding-bottom: 10px; }
.searchbox { width: 100%; border: 0; background: var(--rail-bg); border-radius: 999px; padding: 9px 16px; font-size: 15px; }

/* the store tab */
.about { display: grid; gap: 8px; background: var(--bg); }
.about section { background: var(--paper); padding: 14px 18px; }
.about h3 { font-size: 15px; font-weight: 500; margin-top: 12px; }
.about h3:first-child { margin-top: 0; }
.about p { margin: 6px 0 0; color: var(--muted); white-space: pre-wrap; overflow-wrap: anywhere; }
.about .zone { padding: 6px 0 0; color: var(--muted); }
.reportbtn { border: 0; background: none; padding: 0; color: #7a1b2b; font-size: 15px; cursor: pointer; }

.opt { display: flex; align-items: center; gap: 10px; padding: 10px 0; border-bottom: 1px solid var(--line); cursor: pointer; }
.opt.off { opacity: .5; }

/* the cart bar: a white card with the basket, the sums and the checkout button */
.shop .bottombar.cartbar { background: none; border: 0; pointer-events: none; z-index: 30; }
.shop .bottombar.cartbar.over { z-index: 70; }
.shop .bottombar.cartbar > div { pointer-events: auto; display: flex; align-items: center; gap: 12px; max-width: 560px; height: 64px; margin: 0 auto calc(10px + env(safe-area-inset-bottom)); padding: 0 8px;
  width: calc(100% - 24px); background: #fff; color: var(--ink); border: 0; border-radius: 14px; box-shadow: 0 4px 18px rgba(59, 31, 20, .16); }
.shop .bottombar.cartbar.over > div { box-shadow: none; }
.bag { position: relative; flex: none; width: 48px; height: 48px; border: 0; border-radius: 12px; background: var(--soft-red); color: var(--red); display: grid; place-items: center; cursor: pointer; padding: 0; }
.bag.empty { background: #f1ece4; color: #b5a998; }
.bag .n { position: absolute; top: -7px; right: -9px; min-width: 22px; height: 20px; padding: 0 6px; border-radius: 999px; background: var(--red); color: #fff; font-size: 12px; font-weight: 700; line-height: 20px; border: 2px solid #fff; box-sizing: content-box; }
.sums { flex: 1; min-width: 0; cursor: pointer; }
.line1 { display: flex; align-items: baseline; gap: 6px; min-width: 0; white-space: nowrap; }
.amt { font-size: 20px; font-weight: 700; line-height: 1.2; font-variant-numeric: tabular-nums; }
.was { color: #a9998a; font-size: 12px; font-variant-numeric: tabular-nums; }
.line1 .got { min-width: 0; overflow: hidden; text-overflow: ellipsis; align-self: center; font-size: 11px; line-height: 16px; padding: 0 5px; border-radius: 4px; background: var(--soft-red); color: var(--red); font-weight: 600; }
.hint { color: var(--muted); font-size: 12px; line-height: 1.4; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; margin-top: 1px; }
.hint .bad { color: var(--bad); }
.go { flex: none; height: 46px; min-width: 88px; padding: 0 16px; border: 0; border-radius: 10px; font-size: 16px; font-weight: 700; cursor: pointer; }
.btn.go { background: var(--red); border-color: var(--red); color: #fff; }
.btn.go:disabled { background: #d9cfc3; border-color: #d9cfc3; color: #fff; opacity: 1; }
.go.must { background: var(--gold); color: var(--red-deep); }

/* the cart sheet: a header, the baskets, the dishes and the fees; the cart bar sits in its foot */
.cartmask { padding-bottom: 0; }
.cartsheet { max-height: min(80vh, 700px); overflow: hidden; padding-bottom: calc(74px + env(safe-area-inset-bottom)); background: #fff; }
.carthead { display: flex; align-items: flex-start; gap: 12px; padding: 16px 16px 12px; border-bottom: 1px solid #f1ebe2; }
.carthead h2 { font-size: 18px; display: flex; align-items: center; gap: 8px; }
.carthead .cnt { font-size: 12px; font-weight: 500; color: var(--muted); background: #f3eee7; border-radius: 6px; padding: 0 7px; line-height: 20px; }
.carthead p { margin: 4px 0 0; font-size: 12px; color: var(--muted); }
.carthead .x { flex: none; width: 36px; height: 36px; border: 0; border-radius: 50%; background: #f3eee7; color: var(--muted); display: grid; place-items: center; cursor: pointer; padding: 0; }

.toast { position: fixed; left: 50%; bottom: 96px; transform: translateX(-50%); background: rgba(0,0,0,.8); color: #fff; padding: 8px 16px; border-radius: 999px; font-size: 14px; z-index: 90; }

.sidecart { display: none; }
@media (min-width: 768px) {
  .shophead { padding: 16px var(--gut) 18px; border-radius: 0; }
  .hero.media { display: grid; grid-template-columns: min(44%, 460px) minmax(0, 1fr); gap: 0 24px; align-items: start; background: var(--paper); padding: 16px var(--gut) 18px; }
  .hero.media .shophead { padding: 0; }
  .hero-media { --promo-foot: 12px; border-radius: 14px; overflow: hidden; }
  .hero-media::before, .lip { display: none; }
  .logo { width: 88px; height: 88px; }
  .ident h1 { font-size: 24px; }
  .shoptabs { margin: 12px calc(var(--gut) - 14px) 0; padding: 0 26px; gap: 40px; }
  .shop-main { border-radius: 0 0 14px 14px; overflow: clip; }
  .about section { padding: 16px 26px; }
  .cartmask { padding: 0; align-items: flex-end; }
  .cartsheet { border-radius: 16px 16px 0 0; }
}
@media (min-width: 1024px) {
  .shoptabs { border-radius: 14px 14px 0 0; }
  .shop.solo .shop-cols { grid-template-columns: minmax(0, 1fr); }
  .sidecart { display: block; position: sticky; top: 12px; align-self: start; margin-top: -50px; }
  .sidecart .card { display: flex; flex-direction: column; padding: 0; max-height: calc(100vh - 24px); overflow: hidden; }
  .sidecart h2 { padding: 14px 16px 0; }
  .sidecart .card { background: #fff; border-color: var(--line); }
  .sidecart .foot { display: grid; gap: 6px; padding: 12px 16px 16px; border-top: 1px solid var(--line); }
  .sidecart .sumline { display: flex; align-items: baseline; gap: 8px; }
  .sidecart .now { font-size: 22px; font-variant-numeric: tabular-nums; }
  .sidecart .was { font-size: 13px; }
  .sidecart .got { font-size: 13px; color: var(--red); }
  .sidecart .btn.primary { background: var(--red); border-color: var(--red); }
  .sidecart .must { background: #eee; border-color: #ddd; }
  .shoptabs { margin-right: calc(var(--gut) - 14px + 352px); }
  .shop.solo .shoptabs { margin-right: calc(var(--gut) - 14px); }
}
</style>
