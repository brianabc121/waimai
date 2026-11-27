<script setup lang="ts">
import { checkoutPath, offersPath, ownSite, site, storePath, storeURL } from '../site'
import LangButton from './LangButton.vue'
import { computed, defineAsyncComponent, nextTick, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ApiError, get, post } from '../api'
import { addLine, linePrice, pruneNoteTags, useCart } from '../cart'
import { hasNotes } from '../notes'
import NotePicker from './NotePicker.vue'
import { locale, menuOriginal, money, pick, promoLabel, setLocale, t, mn, mt, sn } from '../i18n'
import { mallSpot } from '../mall'
import { defaultTemplate } from '../theme'
import { customer, customerLogout, loadCustomer, loginRoute, prettyPhone } from '../customer'
import { termsAt, type DeliveryTerms } from '../addresses'
import type { Item, Order, Promotion, Storefront } from '../types'
import '../addon'
import { dishPromoOf, dishSavings, isDishPromo } from '../dishPromo'
import CartPanel from './CartPanel.vue'
import DishDetail from './DishDetail.vue'
import MediaCarousel from './MediaCarousel.vue'
import MenuList from './MenuList.vue'
import StoreReviews from './StoreReviews.vue'
import StoreCoupons from './StoreCoupons.vue'
import { storeStored, type StoredOffers } from '../stored'
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
// ?append=<order link> adds dishes to that order (docs/ADD_ON_ORDER_DESIGN.md §8):
// a cart of its own, the order's service, no minimum and no required dishes.
const appendTok = typeof useRoute().query.append === 'string' ? String(useRoute().query.append) : ''
const cart = useCart(appendTok ? `${props.slug}#add:${appendTok.slice(0, 10)}` : props.slug)
const appendOrder = ref<Order | null>(null)
if (appendTok) void get<Order>(`/customer/orders/${appendTok}`).then(o => {
  appendOrder.value = o
  if (o.service_type !== 'dine_in') cart.service = o.service_type
}).catch(() => null)
function exitAppend() { router.replace(`/o/${appendTok}`) }

const store = ref<Storefront | null>(null)
const error = ref('')
const tab = ref<'menu' | 'reviews' | 'info'>('menu')
const showCart = ref(false)

function canonical(href: string) {
  let l = document.querySelector<HTMLLinkElement>('link[rel=canonical]')
  if (!l) { l = document.createElement('link'); l.rel = 'canonical'; document.head.appendChild(l) }
  l.href = href
}
onUnmounted(() => document.querySelector('link[rel=canonical]')?.remove())

// the store's top-up offer, for the bar that leads to its offers page
const stored = ref<StoredOffers | null>(null)
const bestTier = computed(() => [...(stored.value?.campaign?.tiers ?? [])].filter(x => !x.closed).sort((a, b) => b.bonus * a.pay - a.bonus * b.pay)[0] ?? null)

async function load() {
  error.value = ''
  try {
    const s = await get<Storefront>(`/stores/${props.slug}?lang=${locale.value}`)
    store.value = s
    void storeStored(props.slug).then(r => { stored.value = r }).catch(() => null)
    pruneNoteTags(items.value)
    document.title = sn(s.branch)
    // the store in the mall points search engines at the store's own page
    if (!ownSite() && site.store_domain && s.branch.store_no) canonical(storeURL(s.branch))
    // "Home" in the bottom navigation comes back to the store last looked at.
    rememberStore({ slug: props.slug, name: sn(s.branch), currency: s.branch.currency, country: s.branch.country_code })
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
// the menu comes in the customer's language: read it again when that changes
watch(locale, () => { menuOriginal.value = false; void load() })
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
  if (appendTok) { router.push(`/o/${appendTok}/append`); return }
  const to = checkoutPath(props.slug)
  // Ordering needs an account; the cart is kept while the customer logs in.
  if (!(await loadCustomer())) router.push(loginRoute(to, props.slug))
  else router.push(to)
}

// The account button: log in, or see who is logged in and log out.
const showAccount = ref(false)
async function account() {
  if (await loadCustomer()) showAccount.value = !showAccount.value
  else router.push(loginRoute(storePath(props.slug), props.slug))
}
async function logout() { await customerLogout(); showAccount.value = false }
const showAddresses = ref(false)
// Keeping the store among the customer's favourites needs an account.
const favorite = ref(false)
async function toggleFavorite() {
  if (!(await loadCustomer())) { router.push(loginRoute(storePath(props.slug), props.slug)); return }
  try {
    await setFavorite(props.slug, !favorite.value)
    favorite.value = !favorite.value
    toast.value = t(favorite.value ? 'mf.fav_on' : 'mf.fav_off')
    setTimeout(() => { toast.value = '' }, 2000)
  } catch { /* stays as it was */ }
}
const shopPin = computed(() => { const b = store.value?.branch; return b && b.lat != null && b.lng != null ? { lat: b.lat, lng: b.lng, name: sn(b), country: b.country_code } : null })

// Promotional pictures and videos; a store without any has no such area.
const media = computed(() => store.value?.media ?? [])
const tpl = computed(() => store.value?.templates?.delivery ?? defaultTemplate('delivery'))
const cur = computed(() => store.value?.branch.currency ?? 'PHP')
const items = computed(() => new Map((store.value?.categories ?? []).flatMap(c => c.items).map(i => [i.id, i])))
// A closed store can still take orders for later when it offers time slots.
const canOrder = computed(() => !!store.value && !store.value.branch.paused && (!!appendTok || store.value.branch.open_now || store.value.slots.length > 0))
const ahead = computed(() => canOrder.value && !store.value!.branch.open_now)

function orderable(it: Item) {
  return !it.sold_out && (cart.service === 'delivery' ? it.delivery_available : it.pickup_available)
}

const count = computed(() => cart.lines.reduce((n, l) => n + l.qty, 0))
const subtotal = computed(() => cart.lines.reduce((s, l) => s + (linePrice(l, items.value) ?? 0) * l.qty, 0))
const zone = computed(() => store.value?.zones.find(z => z.id === cart.zoneId) ?? null)
const minimum = computed(() => cart.service === 'delivery' && store.value && !appendTok
  ? Math.max(store.value.branch.min_order_amount, (mapMode.value ? terms.value?.min_order : zone.value?.min_order) ?? 0) : 0)
// Promotions for the chosen service, and a nudge towards the nearest one not yet reached.
const promos = computed(() => (store.value?.promotions ?? []).filter(p => cart.service === 'delivery' ? p.applies_delivery : p.applies_pickup && p.kind !== 'free_delivery'))
// Only offers running right now count on the cart bar.
const livePromos = computed(() => promos.value.filter(p => p.live_now !== false))
// What the dish promotions take off the cart; a promotion on the whole order
// counts its threshold on what is left (docs/PRICING_REVIEW.md step 4).
const dishOff = computed(() => appendTok ? 0 : dishSavings(livePromos.value, cart.lines.map(l => ({ item_id: l.item_id,
  unit: linePrice(l, items.value) ?? 0, qty: l.qty }))))
const goods = computed(() => subtotal.value - dishOff.value)
const nextPromo = computed(() => {
  const ahead = livePromos.value.filter(p => !isDishPromo(p) && p.threshold > goods.value).sort((a, b) => a.threshold - b.threshold)[0]
  return ahead && subtotal.value > 0 && !appendTok ? ahead : null
})
const dealOf = (it: Item) => {
  const p = dishPromoOf(livePromos.value, it.id)
  if (!p) return ''
  // on the dish itself "8折" says it all; "second one half price" stays whole
  const pct = p.value / 100
  return p.kind === 'dish_percent' ? t('pr.percent_off', { pct, zhe: (100 - pct) / 10 }) : promoLabel({ ...p, threshold: 0 }, cur.value)
}
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
  const sub = goods.value
  let best: Promotion | null = null, bestTotal = -1
  for (const p of livePromos.value) {
    if (isDishPromo(p) || sub <= 0 || p.threshold > sub) continue
    const total = p.kind === 'free_delivery' ? deliveryFee.value : saving(p, sub)
    // free delivery with the fee not yet known still counts, behind any money off
    if (total > bestTotal || (total === bestTotal && best?.kind === 'free_delivery' && p.kind !== 'free_delivery')) { best = p; bestTotal = total }
  }
  // an add-on's offer is worked out with the whole order, on the next page
  if (appendTok) return { promo: null, off: 0, now: sub }
  const off = (best && best.kind !== 'free_delivery' ? saving(best, sub) : 0) + dishOff.value
  return { promo: best, off, now: subtotal.value - off }
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
  if (it.groups.length || hasNotes(it)) dish.value = null
  open(it)
}

// ---- item sheet ----
const sheet = reactive<{ item: Item | null; chosen: Set<number>; qty: number; note: string; tags: number[] }>({ item: null, chosen: new Set(), qty: 1, note: '', tags: [] })

function open(it: Item) {
  if (!orderable(it)) return
  if (it.groups.length === 0 && !hasNotes(it)) { addLine(it, [], 1, ''); return }
  sheet.item = it; sheet.chosen = new Set(); sheet.qty = 1; sheet.note = ''; sheet.tags = []
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
  addLine(sheet.item, [...sheet.chosen], sheet.qty, sheet.note, sheet.tags)
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
const name = computed(() => store.value ? sn(store.value.branch) : '')
const one = (n: number) => n.toFixed(1).replace(/\.0$/, '')
const salesText = computed(() => { const n = store.value?.sales ?? 0; return n >= 1000 ? Math.floor(n / 100) * 100 + '+' : String(n) })
const showPromos = ref(false)
const toast = ref('')
async function share() {
  // on its own page the store shares that page, never the mall
  const url = ownSite() ? location.origin + '/' : location.href
  const data = { title: name.value, url }
  try {
    if (navigator.share) { await navigator.share(data); return }
    await navigator.clipboard.writeText(url)
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
  return all.map(c => ({ ...c, items: c.items.filter(i => (i.name + ' ' + i.name_en + ' ' + (i.name_tr ?? '')).toLowerCase().includes(q)) })).filter(c => c.items.length)
})

// ---- categories the order must contain something from ----
const missing = computed(() => onlineCats.value.filter(c => !appendTok && c.required && c.items.some(orderable)
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
      <div v-if="appendTok" class="addbanner" data-testid="addon-banner">
        <span class="grow">{{ t(appendOrder?.service_type === 'pickup' ? 'ao.c.banner_p' : 'ao.c.banner', { no: appendOrder?.public_no ?? '' }) }}</span>
        <button class="btn small" data-testid="addon-exit" @click="exitAppend">{{ t('ao.c.exit') }}</button>
      </div>
      <div class="hero" :class="{ media: media.length || tpl.cover_url }">
      <!-- promotional pictures and videos, else the cover: the same 16:9 frame in every store -->
      <div v-if="media.length" class="hero-media"><MediaCarousel :media="media" /><i class="lip"></i></div>
      <div v-else-if="tpl.cover_url" class="hero-media"><div class="still" data-testid="hero-cover"><img :src="tpl.cover_url" alt="" /></div><i class="lip"></i></div>
      <!-- the store card -->
      <header class="shophead">
        <div class="tools">
          <!-- back to the marketplace; delivery or pickup is chosen at checkout -->
          <router-link v-if="!ownSite()" to="/" class="ico" :aria-label="t('ml.back_mall')" :title="t('ml.back_mall')" data-testid="back-mall">
            <svg viewBox="0 0 24 24" width="24" height="24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="m14.5 5-7 7 7 7" /></svg><span class="lbl">{{ t('ml.back_mall') }}</span></router-link>
          <span class="grow"></span>
          <button class="ico" :class="{ on: searching }" :aria-label="t('sf.search')" data-testid="search-toggle" @click="toggleSearch">
            <svg viewBox="0 0 24 24" width="24" height="24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="10.5" cy="10.5" r="6.5" /><path d="m15.5 15.5 5 5" /></svg><span class="lbl">{{ t('sf.search') }}</span></button>
          <button v-if="!ownSite()" class="ico" :class="{ fav: favorite }" :aria-label="t(favorite ? 'mf.faved' : 'mf.fav')" :aria-pressed="favorite" data-testid="favorite" @click="toggleFavorite">
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
          <LangButton class="lang" />
        </div>
        <!-- the store's name, offers and notice; beside the pictures on a computer -->
        <div class="idwrap">
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
          <div class="chips brand"><span v-for="p in promos.slice(0, 3)" :key="p.id" :title="mn(p)">{{ promoLabel(p, cur) }}</span></div>
          <button class="more" data-testid="promos-more" @click="showPromos = true">{{ t('sf.promos_n', { n: promos.length }) }} ›</button>
        </div>
        <StoreCoupons :slug="slug" :currency="cur" />
        <router-link v-if="stored?.campaign" class="svbar" :to="offersPath(slug)" data-testid="offers-bar">
          <span class="svtag">{{ t('sv.c.topup') }}</span>
          <span class="grow">{{ bestTier ? t('sv.c.bar', { pay: money(bestTier.pay, cur), bonus: money(bestTier.bonus, cur) }) : stored.campaign.name }}</span>
          <span class="more">{{ t('sv.c.title') }} ›</span></router-link>
        <div class="notice" v-if="store.branch.notice" data-testid="notice">
          <span class="lbl">{{ t('sf.notice') }}</span>
          <!-- a notice too long for the line scrolls round and round -->
          <div ref="marq" class="marq" :class="{ run: scrolls }"><span :style="{ animationDuration: Math.max(8, (store.branch.notice_tr || store.branch.notice).length / 4) + 's' }">{{ store.branch.notice_tr || store.branch.notice }}<template v-if="scrolls"><i></i>{{ store.branch.notice_tr || store.branch.notice }}<i></i></template></span></div>
        </div>
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
        <section><h3>{{ t('sf.about') }}</h3><p :class="{ muted: !store.branch.intro }">{{ store.branch.intro_tr || store.branch.intro || t('sf.none') }}</p></section>
        <section><h3>{{ t('sf.notice_title') }}</h3><p :class="{ muted: !store.branch.notice }">{{ store.branch.notice_tr || store.branch.notice || t('sf.none') }}</p></section>
        <section><button class="reportbtn" data-testid="report" @click="openReport">{{ t('sf.report') }}</button></section>
      </div>

      <!-- menu (C01) -->
      <div v-else class="menuwrap">
        <div class="pre" v-if="!canOrder"><div class="banner warn">{{ store.branch.paused ? t('paused') : t('closed') }}</div></div>
        <div class="pre" v-else-if="ahead"><div class="banner info">{{ t('closed_can_schedule') }}</div></div>
        <div class="pre" v-if="searching">
          <input ref="searchBox" v-model="query" type="search" class="searchbox" :placeholder="t('sf.search')" data-testid="search-box" />
        </div>
        <div class="pre mt-note" v-if="store.machine_translated" data-testid="mt-note">
          <span>{{ t(menuOriginal ? 'sf.mt_original' : 'sf.mt_note') }}</span>
          <button type="button" class="linkish" data-testid="mt-toggle" @click="menuOriginal = !menuOriginal">{{ t(menuOriginal ? 'sf.mt_show_tr' : 'sf.mt_show_orig') }}</button>
        </div>
        <p v-if="query.trim() && !shownCats.length" class="muted" style="text-align:center;padding:36px 0;margin:0">{{ t('sf.search_none') }}</p>
        <MenuList ref="menu" :categories="shownCats" :currency="cur" :layout="tpl.layout" :can-order="orderable" :qty-of="qtyOf" :deal-of="dealOf" stats
          @add="open" @dec="dec" @show="dish = $event" />
      </div>
      </div>

      <!-- the cart beside the menu on a computer; smaller screens use the bar below -->
      <aside class="sidecart" v-if="tab === 'menu'" data-testid="side-cart">
        <div class="card">
          <h2>{{ t('cart') }}<span v-if="count" class="cnt" data-testid="side-count">{{ count }}</span></h2>
          <CartPanel :store="store" :items="items" :currency="cur" :orderable="orderable" />
          <div class="foot">
            <div v-if="count" class="sumline">
              <b class="now">{{ money(deal.now, cur) }}</b>
              <s v-if="deal.off" class="was">{{ money(subtotal, cur) }}</s>
            </div>
            <div v-if="count && (deal.promo || deal.off)" class="got" data-testid="side-deal"><template v-if="deal.promo">{{ t('sf.got_promo', { name: promoLabel(deal.promo, cur) }) }}</template><template v-if="deal.off"><template v-if="deal.promo"> · </template>{{ t('sf.saved', { amt: money(deal.off, cur) }) }}</template></div>
            <div v-if="count && minimum > subtotal" class="small muted">{{ t('below_min', { amt: money(minimum - subtotal, cur) }) }}</div>
            <div v-else-if="nextPromo" class="small" style="color:var(--brand-dark)">{{ t('more_for_promo', { amt: money(nextPromo.threshold - goods, cur), name: promoLabel(nextPromo, cur) }) }}</div>
            <button v-if="count && missing.length" class="btn block big must" data-testid="required-missing" @click="toRequired">{{ t('sf.required_pick') }} · {{ mn(missing[0]) }}</button>
            <button v-else class="btn primary block big" data-testid="go-checkout" :disabled="blocked" @click="checkout">{{ appendTok ? t('ao.c.go') : t('checkout') }}<template v-if="count"> · {{ money(deal.now, cur) }}</template></button>
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
              <span v-if="deal.promo || deal.off" class="got" data-testid="cart-deal">{{ deal.off || !deal.promo ? t('sf.saved', { amt: money(deal.off, cur) }) : promoLabel(deal.promo, cur) }}</span>
            </div>
            <div class="hint">
              <template v-if="!count">{{ t('cart_empty') }}</template>
              <span v-else-if="hasUnavailable" class="bad">{{ t('sf.has_unavailable') }}</span>
              <template v-else-if="minimum > subtotal">{{ t('below_min', { amt: money(minimum - subtotal, cur) }) }}</template>
              <template v-else-if="nextPromo">{{ t('more_for_promo', { amt: money(nextPromo.threshold - goods, cur), name: promoLabel(nextPromo, cur) }) }}</template>
              <template v-else-if="deal.promo">{{ t('sf.got_promo', { name: promoLabel(deal.promo, cur) }) }}</template>
            </div>
          </div>
          <button v-if="count && missing.length" class="go must" data-testid="required-missing" @click="toRequired">{{ t('sf.required_pick') }}</button>
          <button v-else class="btn primary go" :disabled="blocked" data-testid="go-checkout-bar" @click="checkout">{{ !canOrder ? (store.branch.paused ? t('paused') : t('closed')) : appendTok ? t('ao.c.go') : t('checkout') }}</button>
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
            <h2>{{ mn(sheet.item) }}</h2>
            <div class="muted small" v-if="sheet.item.description">{{ mt(sheet.item.description, sheet.item.description_tr) }}</div>
          </div>
          <div class="sheet-body stack">
            <div v-for="(g, gi) in sheet.item.groups" :key="g.id">
              <div class="row between"><h3>{{ mn(g) }}</h3><span class="muted small">{{ groupHint(g) }}</span></div>
              <label v-for="o in g.options" :key="o.id" class="opt" :class="{ off: o.sold_out }">
                <input :type="g.max_select === 1 ? 'radio' : 'checkbox'" :checked="sheet.chosen.has(o.id)" :disabled="o.sold_out"
                  @click.prevent="toggle(gi, o.id)" />
                <span class="grow">{{ mn(o) }}<span v-if="o.sold_out" class="muted small"> · {{ t('sold_out') }}</span></span>
                <span class="price muted" v-if="o.price_delta">+{{ money(o.price_delta, cur) }}</span>
              </label>
            </div>
            <section v-if="sheet.item.notes?.length" class="notes-sec" data-testid="sheet-notes">
              <h3>{{ t('nt.title') }}</h3>
              <div class="muted small" style="margin:-4px 0 10px">{{ t('nt.free') }}</div>
              <NotePicker v-model="sheet.tags" :groups="sheet.item.notes" />
            </section>
            <label class="field"><span>{{ t('note') }}</span><input v-model="sheet.note" maxlength="200" data-testid="sheet-note"
              :placeholder="sheet.item.notes?.length ? t('nt.other_ph') : t('note_ph')" /></label>
          </div>
          <div class="sheet-foot row between">
            <span class="stepper"><button @click="sheet.qty = Math.max(1, sheet.qty - 1)">−</button><span>{{ sheet.qty }}</span><button class="fill" @click="sheet.qty = Math.min(99, sheet.qty + 1)">+</button></span>
            <button class="btn primary grow" :disabled="!!sheetMissing" @click="confirmSheet">
              {{ sheetMissing ? `${t('required')}: ${mn(sheetMissing)}` : `${t('add_to_cart')} · ${money(sheetPrice, cur)}` }}
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
        <div class="sheet promosheet" data-testid="promo-sheet">
          <div class="carthead">
            <div class="grow">
              <h2>{{ t('sf.promos') }}<span class="cnt">{{ promos.length }}</span></h2>
              <p>{{ t('sf.promo_tip') }}</p>
            </div>
            <button class="x" :aria-label="t('close')" @click="showPromos = false">
              <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M6 6l12 12M18 6 6 18" /></svg></button>
          </div>
          <!-- every offer as a ticket: what it gives on the left, its rule on the right -->
          <div class="tickets">
            <div v-for="p in promos" :key="p.id" class="ticket" :class="{ idle: p.live_now === false }" :data-promo="p.id">
              <div class="stub" :class="p.kind">
                <svg v-if="p.kind === 'free_delivery'" viewBox="0 0 24 24" width="26" height="26" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M2.5 6.5h11v9h-11zM13.5 9.5h4l3 3v3h-7" /><circle cx="6.5" cy="17.5" r="1.8" /><circle cx="17" cy="17.5" r="1.8" /></svg>
                <b v-else-if="isDishPromo(p)">{{ promoLabel({ ...p, threshold: 0 }, cur) }}</b>
                <b v-else>{{ p.kind === 'amount_off' ? money(p.value, cur) : t('pr.percent_off', { pct: p.value / 100, zhe: (100 - p.value / 100) / 10 }) }}</b>
                <small>{{ p.threshold > 0 ? t('sf.promo_over', { th: money(p.threshold, cur) }) : t('sf.promo_any') }}</small>
              </div>
              <div class="rule">
                <div class="nm">{{ mn(p) }}</div>
                <div class="lbl">{{ promoLabel(p, cur) }}</div>
                <div class="when" v-if="p.daily_start && p.daily_end && p.daily_start !== p.daily_end">{{ t('sf.promo_hours', { from: p.daily_start.slice(0, 5), to: p.daily_end.slice(0, 5) }) }}</div>
                <div class="when idle" v-if="p.live_now === false">{{ t('sf.promo_paused') }}</div>
              </div>
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

      <div v-if="ownSite()" class="powered" data-testid="powered">{{ t('site.powered') }}</div>
      <div v-if="toast" class="toast">{{ toast }}</div>
      <CustomerTabs active="home" class="shoptabbar" />
    </template>
  </div>
</template>

<style scoped>
.addbanner { position: sticky; top: 0; z-index: 20; display: flex; align-items: center; gap: 10px; padding: 10px 14px;
  background: linear-gradient(90deg, #f6e2a8, #e9c46a); color: #5a1010; font-weight: 700; font-size: 14px; }
.mt-note { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; font-size: 12px; color: #8a7f75; padding: 4px 14px; }
.mt-note .linkish { background: none; border: 0; padding: 0; color: var(--brand, #b8322a); font-size: 12px; cursor: pointer; text-decoration: underline; }
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
  /* on the menu the room kept for the cart bar is part of the menu, so the
     category rail and the dishes run on under the bar instead of stopping above
     a strip of page */
  .shop:not(.solo) { padding-bottom: 0; --menu-pad: calc(96px + var(--tabs)); }
  .shop .bottombar.cartbar { bottom: var(--tabbar-h); }
  .shop .cartmask { padding-bottom: var(--tabs); }
  .shop .toast { bottom: calc(96px + var(--tabs)); }
}
.acct { position: relative; }
.acct-menu { position: absolute; inset-inline-end: 0; top: 44px; z-index: 30; background: var(--paper); border: 1px solid var(--line); border-radius: 12px; box-shadow: 0 8px 24px rgba(0, 0, 0, .14); padding: 12px; width: 200px; display: grid; gap: 6px; }
.lang { color: var(--muted); margin-inline-start: 4px; }
.ident { display: flex; gap: 12px; align-items: flex-start; }
.logo { width: 76px; height: 76px; border-radius: 10px; border: 1px solid var(--line); object-fit: cover; flex: none; background: var(--paper); }
.logo.ph { display: grid; place-items: center; background: var(--brand-soft); color: var(--brand-dark); font-size: 34px; font-weight: 700; border-color: transparent; }
.ident h1 { font-size: 20px; line-height: 1.25; overflow-wrap: anywhere; }
.meta { display: flex; flex-wrap: wrap; align-items: center; gap: 4px 12px; margin-top: 4px; font-size: 14px; }
.score { color: var(--star); font-weight: 700; font-size: 15px; } .score i { font-style: normal; margin-inline-end: 2px; }
.state { font-size: 12px; font-weight: 600; color: var(--ok); } .state.off { color: var(--warn); }
.chips { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 6px; min-width: 0; }
.chips span { border: 1px solid #cdbba3; color: var(--muted); border-radius: 5px; padding: 0 6px; font-size: 12px; line-height: 20px; white-space: nowrap; }
.chips.brand { margin: 0; flex: 1; flex-wrap: nowrap; overflow: hidden; }
.chips.brand span { border-color: var(--brand); color: var(--brand-dark); overflow: hidden; text-overflow: ellipsis; flex: 0 1 auto; }
.promoline { display: flex; align-items: center; gap: 8px; }
.svbar { display: flex; align-items: center; gap: 8px; margin-top: 8px; padding: 7px 10px; border-radius: 10px; background: linear-gradient(90deg, #fff1d6, #fbe2b8);
  border: 1px solid #efc983; color: #7a3e00; font-size: 13px; text-decoration: none; }
.svbar .svtag { background: #b8392a; color: #fff; border-radius: 5px; padding: 0 6px; font-size: 12px; font-weight: 700; white-space: nowrap; }
.svbar .grow { flex: 1; min-width: 0; font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.svbar .more { white-space: nowrap; color: #b8392a; }
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
.lip { position: absolute; inset-inline-start: 0; inset-inline-end: 0; bottom: -1px; height: 18px; background: var(--paper); border-radius: 18px 18px 0 0; pointer-events: none; }
.hero.media .shophead { padding-top: 0; }
@media (max-width: 767.98px) {
  .hero.media .tools { position: absolute; top: 8px; inset-inline-start: 12px; inset-inline-end: 10px; z-index: 5; color: #fff; }
  .hero.media .tools .ico, .hero.media .tools .lang { color: #fff; filter: drop-shadow(0 1px 2px rgba(0, 0, 0, .5)); }
  .hero.media .tools .lang { border-color: rgba(255, 255, 255, .85); }
  .hero.media .tools .ico:hover, .hero.media .tools .ico.on { background: rgba(255, 255, 255, .22); }
  .hero.media .acct-menu { color: var(--ink); }
}

/* the three tabs */
.shoptabs { position: sticky; top: 0; z-index: 15; display: flex; gap: 30px; height: 50px; margin-top: 10px; padding: 0 22px; background: var(--paper); border-radius: 18px 18px 0 0; border-bottom: 1px solid var(--line); }
.shoptabs button { position: relative; border: 0; background: none; padding: 0 2px; font-size: 16px; color: var(--muted); cursor: pointer; }
.shoptabs button.on { color: var(--ink); font-weight: 700; }
.shoptabs button.on::after { content: ''; position: absolute; left: 50%; bottom: 6px; width: 22px; height: 3px; margin-inline-start: -11px; border-radius: 2px; background: var(--brand); }
.shoptabs small { font-size: 11px; color: var(--muted); margin-inline-start: 2px; font-weight: 400; }

/* The menu fills what is left of the screen, so the category rail runs to the
   bottom however few dishes there are, but it never makes the page taller than
   the screen: the page is a column at least a screen high and the menu takes
   the rest of it. */
.shop { display: flex; flex-direction: column; min-height: 100vh; min-height: 100dvh; }
.shop-cols { flex: 1 0 auto; display: flex; flex-direction: column; }
.shop-main { flex: 1 0 auto; display: flex; flex-direction: column; }
.shop-main > * { flex: none; }
.menuwrap { background: var(--paper); flex: 1 0 auto !important; display: flex; flex-direction: column; }
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
.notes-sec { border-top: 1px solid var(--line); padding-top: 10px; }
.notes-sec h3 { margin: 0 0 6px; }

/* the cart bar: a white card with the basket, the sums and the checkout button */
.shop .bottombar.cartbar { background: none; border: 0; pointer-events: none; z-index: 30; }
.shop .bottombar.cartbar.over { z-index: 70; }
.shop .bottombar.cartbar > div { pointer-events: auto; display: flex; align-items: center; gap: 12px; max-width: 560px; height: 64px; margin: 0 auto calc(10px + env(safe-area-inset-bottom)); padding: 0 8px;
  width: calc(100% - 24px); background: #fff; color: var(--ink); border: 0; border-radius: 14px; box-shadow: 0 4px 18px rgba(59, 31, 20, .16); }
.shop .bottombar.cartbar.over > div { box-shadow: none; }
.bag { position: relative; flex: none; width: 48px; height: 48px; border: 0; border-radius: 12px; background: var(--soft-red); color: var(--red); display: grid; place-items: center; cursor: pointer; padding: 0; }
.bag.empty { background: #f1ece4; color: #b5a998; }
.bag .n { position: absolute; top: -7px; inset-inline-end: -9px; min-width: 22px; height: 20px; padding: 0 6px; border-radius: 999px; background: var(--red); color: #fff; font-size: 12px; font-weight: 700; line-height: 20px; border: 2px solid #fff; box-sizing: content-box; }
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

/* the offers: tickets with a red stub */
.promosheet { background: var(--paper); max-height: min(80vh, 640px); overflow: hidden; }
.promosheet .carthead { background: var(--red-bg); color: #fff; border-bottom: 0; border-radius: inherit; border-end-start-radius: 0; border-end-end-radius: 0; }
.promosheet .carthead h2 { color: #fff; }
.promosheet .carthead .cnt { color: #fff; background: rgba(255, 255, 255, .2); }
.promosheet .carthead p { color: rgba(255, 255, 255, .85); }
.promosheet .carthead .x { background: rgba(255, 255, 255, .18); color: #fff; }
.tickets { overflow-y: auto; padding: 14px 16px calc(18px + env(safe-area-inset-bottom)); display: grid; gap: 10px; }
.ticket { position: relative; display: flex; min-height: 84px; background: #fff; border-radius: 12px; box-shadow: 0 1px 6px rgba(59, 31, 20, .08); overflow: hidden; }
.stub { flex: none; width: 104px; display: grid; place-content: center; justify-items: center; gap: 2px; padding: 8px 6px; text-align: center; color: #fff; background: linear-gradient(150deg, #b8382a, var(--red)); position: relative; }
.stub.free_delivery { background: linear-gradient(150deg, #d9a43c, #c08a22); }
/* the punched edge between the stub and the rule */
.stub::after { content: ''; position: absolute; top: 6px; bottom: 6px; inset-inline-end: -4px; width: 8px; background: radial-gradient(circle at 4px 5px, #fff 3px, transparent 3.5px) 0 0 / 8px 10px repeat-y; }
.stub b { font-size: 22px; line-height: 1.1; font-weight: 700; font-variant-numeric: tabular-nums; overflow-wrap: anywhere; }
.stub small { font-size: 11px; opacity: .92; line-height: 1.3; }
.rule { flex: 1; min-width: 0; padding: 12px 14px; padding-inline-start: 18px; display: flex; flex-direction: column; justify-content: center; gap: 3px; }
.rule .nm { font-size: 15px; font-weight: 600; color: var(--ink); overflow-wrap: anywhere; }
.rule .lbl { align-self: flex-start; font-size: 12px; line-height: 20px; padding: 0 7px; border-radius: 5px; background: #fbe3dc; color: var(--red); }
.rule .when { font-size: 12px; color: var(--muted); }
.rule .when.idle { color: var(--warn); }
.ticket.idle .stub { filter: grayscale(.6); opacity: .8; }
@media (min-width: 768px) { .promosheet { max-width: 480px; } }

.powered { text-align: center; color: var(--muted); font-size: 12px; padding: 14px 0 calc(var(--tabbar-h, 56px) + 24px); }
@media (min-width: 768px) { .powered { padding-bottom: 96px; } }
.toast { position: fixed; left: 50%; bottom: 96px; transform: translateX(-50%); background: rgba(0,0,0,.8); color: #fff; padding: 8px 16px; border-radius: 999px; font-size: 14px; z-index: 90; }

.sidecart { display: none; }
.idwrap { display: contents; }
.tools .lbl { display: none; }
@media (min-width: 768px) {
  /* the store card: the tools along the top, the name and offers on the left,
     the pictures on the right */
  .hero { margin: 16px var(--gut) 0; background: var(--paper); border: 1px solid #efe3d1; border-radius: 16px; box-shadow: 0 2px 12px rgba(59, 31, 20, .05);
    padding: 10px 24px 20px; display: grid; grid-template-columns: minmax(0, 1fr) auto; column-gap: 28px; }
  .shophead { display: contents; }
  .tools { grid-column: 1 / -1; grid-row: 1; padding-bottom: 10px; margin-bottom: 16px; border-bottom: 1px solid #efe3d1; }
  .idwrap { grid-column: 1; grid-row: 2; display: grid; grid-template-columns: minmax(0, 1fr); gap: 10px; align-content: start; min-width: 0; }
  .hero-media { grid-column: 2; grid-row: 2; align-self: start; width: clamp(240px, 26vw, 320px); --promo-foot: 10px; border-radius: 12px; overflow: hidden; }
  .hero-media::before, .lip { display: none; }
  .hero.media .shophead { padding: 0; }
  .logo { width: 80px; height: 80px; border-radius: 14px; }
  .ident h1 { font-size: 22px; }
  .chips span { border: 0; background: #f3e6d2; color: #6f5a49; }
  .chips.brand span { background: #fbe3dc; color: var(--red); }
  .shoptabs { margin: 16px var(--gut) 0; padding: 0 22px; gap: 36px; border-radius: 14px; border: 1px solid #efe3d1; border-bottom-color: #efe3d1; }
  .shop-cols { padding: 0 var(--gut); }
  .shop-main { border-radius: 0 0 14px 14px; overflow: clip; }
  .about section { padding: 16px 26px; }
  .cartmask { padding: 0; align-items: flex-end; }
  .cartsheet { border-radius: 16px 16px 0 0; }
}
@media (min-width: 1024px) {
  /* the back link and the search say what they are */
  .tools .lbl { display: inline; }
  .tools > a.ico { width: auto; border-radius: 8px; padding: 0 10px 0 4px; gap: 4px; display: inline-flex; font-size: 14px; color: var(--muted); }
  .tools > a.ico svg { width: 20px; height: 20px; }
  .tools > button.ico:first-of-type { width: auto; height: 34px; border-radius: 9px; padding: 0 12px; gap: 6px; display: inline-flex; background: #f3e6d2; font-size: 13px; color: var(--muted); margin-inline-end: 6px; }
  .tools > button.ico:first-of-type svg { width: 17px; height: 17px; }
  .tools > button.ico:first-of-type.on { background: var(--gold-soft); color: var(--ink); }

  /* the menu and the cart side by side; the tabs above the menu only */
  .shop-cols { display: grid; grid-template-columns: minmax(0, 1fr) 340px; gap: 20px; align-items: start; margin-top: 14px; }
  .shop.solo .shop-cols { grid-template-columns: minmax(0, 1fr); }
  .shoptabs { margin-inline-end: calc(var(--gut) + 360px); }
  .shop.solo .shoptabs { margin-inline-end: var(--gut); }
  .shop-main { align-self: stretch; border-radius: 14px; }
  .shop.solo .shop-main { background: var(--paper); border: 1px solid #efe3d1; }

  /* the menu: the categories in a card of their own, every dish a card, two
     to a row; both stay under the tabs while the page scrolls */
  .shop { --menu-top: 64px; }
  .shoptabs { box-shadow: 0 -16px 0 var(--bg); }
  .menuwrap { background: none; }
  .menuwrap .pre { padding-inline: 0; }
  .menuwrap :deep(.menu.list .body) { --rail: 112px; background: none; gap: 14px; }
  .menuwrap :deep(.menu.list .cats) { position: sticky; top: var(--menu-top); width: var(--rail); padding: 6px; background: var(--paper); border: 1px solid #efe3d1; border-radius: 12px; max-height: calc(100vh - var(--menu-top) - 16px); }
  .menuwrap :deep(.menu.list .cats button) { border-radius: 8px; padding: 12px 10px; text-align: start; }
  .menuwrap :deep(.menu.list .cats button.on) { background: #fff; color: var(--red); box-shadow: 0 1px 4px rgba(59, 31, 20, .08); }
  .menuwrap :deep(.menu.list .cats button.on::before) { top: 10px; bottom: 10px; border-radius: 2px; }
  .menuwrap :deep(.menu.list .cats .badge) { top: 50%; transform: translateY(-50%); inset-inline-end: 8px; }
  .menuwrap :deep(.menu.list .lists) { padding: 0 0 20px; gap: 4px; }
  .menuwrap :deep(.menu.list .cat > h2) { font-size: 15px; font-weight: 700; color: var(--ink); padding: 4px 0 10px; }
  .menuwrap :deep(.menu.list .cat:not(:first-child) > h2) { padding-top: 16px; }
  .menuwrap :deep(.menu.list .items) { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
  .menuwrap :deep(.menu.list .item) { background: var(--paper); border: 1px solid #efe3d1; border-radius: 12px; padding: 12px; gap: 12px; }
  .menuwrap :deep(.menu.list .item:hover) { border-color: #e2cfb4; box-shadow: 0 4px 14px rgba(59, 31, 20, .08); }
  .menuwrap :deep(.menu.list .item img), .menuwrap :deep(.menu.list .item .ph) { width: 96px; height: 96px; border-radius: 10px; }
  .menuwrap :deep(.menu.list .item .bold) { font-size: 15px; }

  /* the cart */
  .sidecart { display: block; position: sticky; top: 12px; align-self: start; margin-top: -64px; }
  .sidecart .card { display: flex; flex-direction: column; padding: 0; max-height: calc(100vh - 24px); overflow: hidden; background: var(--paper); border: 1px solid #efe3d1; border-radius: 14px; box-shadow: 0 2px 12px rgba(59, 31, 20, .05); }
  .sidecart h2 { padding: 14px 16px 0; font-size: 17px; display: flex; align-items: center; justify-content: space-between; }
  .sidecart h2 .cnt { font-size: 12px; font-weight: 600; color: #8a5a12; background: var(--gold-soft); border-radius: 6px; padding: 0 8px; line-height: 20px; }
  .sidecart .foot { display: grid; gap: 8px; padding: 14px 16px 16px; border-top: 1px solid #efe3d1; }
  .sidecart .sumline { display: flex; align-items: baseline; gap: 8px; }
  .sidecart .now { font-size: 24px; font-variant-numeric: tabular-nums; }
  .sidecart .was { font-size: 13px; }
  .sidecart .got { font-size: 12px; color: var(--red); background: #fbe3dc; border-radius: 8px; padding: 7px 10px; }
  .sidecart .btn.primary { background: var(--red); border-color: var(--red); border-radius: 10px; min-height: 46px; }
  .sidecart .btn.primary:disabled { background: #d9cfc3; border-color: #d9cfc3; opacity: 1; }
  .sidecart .must { background: var(--gold); border-color: var(--gold); color: var(--red-deep); }
}
</style>
