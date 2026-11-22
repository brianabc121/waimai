<script setup lang="ts">
import { computed, defineAsyncComponent, nextTick, onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ApiError, get, post } from '../api'
import { addLine, linePrice, useCart } from '../cart'
import { locale, money, pick, promoLabel, setLocale, t } from '../i18n'
import { defaultTemplate, themeVars } from '../theme'
import { customer, customerLogout, loadCustomer, loginRoute, prettyPhone } from '../customer'
import type { Item, Located, Storefront } from '../types'
import CartPanel from './CartPanel.vue'
import MenuList from './MenuList.vue'
import StoreReviews from './StoreReviews.vue'
import './storeStrings'

// The map is only downloaded when a customer opens "where to deliver".
const DeliveryPicker = defineAsyncComponent(() => import('./DeliveryPicker.vue'))

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
    // Fall back to a service the store actually offers.
    if (cart.service === 'delivery' && !s.branch.delivery_enabled) cart.service = 'pickup'
    if (cart.service === 'pickup' && !s.branch.pickup_enabled) cart.service = 'delivery'
    if (cart.zoneId && !s.zones.some(z => z.id === cart.zoneId)) cart.zoneId = null
    if (s.branch.zone_mode === 'map') await relocate(s)
  } catch (e) {
    error.value = e instanceof ApiError && e.status === 404 ? t('not_found') : t('offline')
  }
}
onMounted(load)
onMounted(() => { void loadCustomer() })

// In map mode the delivery area follows the saved pin; areas may have been
// redrawn since the last visit, so the server is asked again.
const mapMode = computed(() => store.value?.branch.zone_mode === 'map')
const showPicker = ref(false)
const near = ref(0)
async function relocate(s: Storefront) {
  near.value = 0
  if (cart.lat == null || cart.lng == null) { cart.zoneId = null; return }
  const r = await post<Located>(`/stores/${props.slug}/locate`, { lat: cart.lat, lng: cart.lng }).catch(() => null)
  if (!r) return
  cart.zoneId = r.zone && s.zones.some(z => z.id === r.zone!.id) ? r.zone.id : null
  if (r.zone) near.value = r.distance_m
  else cart.lat = cart.lng = null
}
function pickerClosed() { showPicker.value = false; if (store.value) relocate(store.value) }
function toPickup() { cart.service = 'pickup'; showPicker.value = false }
async function checkout() {
  if (cart.service === 'delivery' && mapMode.value && !zone.value) { showPicker.value = true; return }
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
  ? Math.max(store.value.branch.min_order_amount, zone.value?.min_order ?? 0) : 0)
// Promotions for the chosen service, and a nudge towards the nearest one not yet reached.
const promos = computed(() => (store.value?.promotions ?? []).filter(p => cart.service === 'delivery' ? p.applies_delivery : p.applies_pickup && p.kind !== 'free_delivery'))
const nextPromo = computed(() => {
  const ahead = promos.value.filter(p => p.threshold > subtotal.value).sort((a, b) => a.threshold - b.threshold)[0]
  return ahead && subtotal.value > 0 ? ahead : null
})
const hasUnavailable = computed(() => cart.lines.some(l => {
  const it = items.value.get(l.item_id)
  return !it || !orderable(it) || linePrice(l, items.value) === null
}))

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
const shownCats = computed(() => {
  const all = store.value?.categories ?? []
  const q = query.value.trim().toLowerCase()
  if (!q) return all
  return all.map(c => ({ ...c, items: c.items.filter(i => (i.name + ' ' + i.name_en).toLowerCase().includes(q)) })).filter(c => c.items.length)
})

// ---- categories the order must contain something from ----
const missing = computed(() => (store.value?.categories ?? []).filter(c => c.required && c.items.some(orderable)
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
  <div class="phone wide shop" :class="{ solo: tab !== 'menu' }" :style="themeVars(tpl)">
    <div v-if="error" class="section stack">
      <div class="banner bad">{{ error }}</div>
      <button class="btn" @click="load">{{ t('retry') }}</button>
    </div>
    <div v-else-if="!store" class="section muted">{{ t('loading') }}</div>
    <template v-else>
      <img v-if="tpl.cover_url" :src="tpl.cover_url" alt="" class="cover" />
      <!-- the store card -->
      <header class="shophead">
        <div class="tools">
          <div class="seg light" v-if="store.branch.delivery_enabled && store.branch.pickup_enabled">
            <button :class="{ on: cart.service === 'delivery' }" @click="cart.service = 'delivery'">{{ t('delivery') }}</button>
            <button :class="{ on: cart.service === 'pickup' }" @click="cart.service = 'pickup'">{{ t('pickup') }}</button>
          </div>
          <span v-else class="bold">{{ t(cart.service) }}</span>
          <span class="grow"></span>
          <button class="ico" :class="{ on: searching }" :aria-label="t('sf.search')" data-testid="search-toggle" @click="toggleSearch">
            <svg viewBox="0 0 24 24" width="24" height="24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="10.5" cy="10.5" r="6.5" /><path d="m15.5 15.5 5 5" /></svg></button>
          <button class="ico" :aria-label="t('sf.share')" data-testid="share" @click="share">
            <svg viewBox="0 0 24 24" width="24" height="24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="18" cy="5" r="2.6" /><circle cx="6" cy="12" r="2.6" /><circle cx="18" cy="19" r="2.6" /><path d="m8.3 10.7 7.4-4.4M8.3 13.3l7.4 4.4" /></svg></button>
          <div class="acct">
            <button class="ico" :class="{ on: showAccount, in: customer.verified }" :aria-label="t('sf.account')" data-testid="account" @click="account">
              <svg viewBox="0 0 24 24" width="24" height="24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="12" cy="8.5" r="3.8" /><path d="M4.5 20c1.2-3.6 4-5.4 7.5-5.4s6.3 1.8 7.5 5.4" /></svg></button>
            <div v-if="showAccount && customer.verified" class="acct-menu" data-testid="account-menu">
              <div class="small muted">{{ t('sf.logged_in') }}</div>
              <div class="bold">{{ prettyPhone(customer.phone) }}</div>
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
        <div class="notice" v-if="store.branch.notice" data-testid="notice">
          <span class="lbl">{{ t('sf.notice') }}</span>
          <!-- a notice too long for the line scrolls round and round -->
          <div ref="marq" class="marq" :class="{ run: scrolls }"><span :style="{ animationDuration: Math.max(8, store.branch.notice.length / 4) + 's' }">{{ store.branch.notice }}<template v-if="scrolls"><i></i>{{ store.branch.notice }}<i></i></template></span></div>
        </div>
      </header>

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
        <div class="pre" v-if="cart.service === 'delivery' && mapMode">
          <button type="button" class="card row between" style="width:100%;text-align:left;cursor:pointer" data-testid="deliver-to" @click="showPicker = true">
            <span v-if="zone"><span class="muted small">{{ t('deliver_where') }}</span><br /><b>{{ cart.placeLabel || pick(zone.name, zone.name_en) }}</b>
              <span class="small muted"><br />{{ pick(zone.name, zone.name_en) }} · {{ t('delivery_fee') }} {{ money(zone.fee, cur) }}<template v-if="zone.eta_minutes"> · {{ t('eta_min', { n: zone.eta_minutes }) }}</template></span></span>
            <span v-else><b>📍 {{ t('pin_needed') }}</b></span>
            <span class="muted">›</span>
          </button>
        </div>
        <div class="pre" v-else-if="cart.service === 'delivery'">
          <label class="field" style="margin:0">
            <span>{{ t('deliver_to') }}</span>
            <select v-model="cart.zoneId">
              <option :value="null">{{ t('choose_zone') }}</option>
              <option v-for="z in store.zones" :key="z.id" :value="z.id">
                {{ pick(z.name, z.name_en) }} · {{ t('delivery_fee') }} {{ money(z.fee, cur) }}
              </option>
            </select>
          </label>
        </div>
        <div class="pre" v-if="searching">
          <input ref="searchBox" v-model="query" type="search" class="searchbox" :placeholder="t('sf.search')" data-testid="search-box" />
        </div>
        <p v-if="query.trim() && !shownCats.length" class="muted" style="text-align:center;padding:36px 0;margin:0">{{ t('sf.search_none') }}</p>
        <MenuList ref="menu" :categories="shownCats" :currency="cur" :layout="tpl.layout" :can-order="orderable" :qty-of="qtyOf" stats
          @add="open" @dec="dec" />
      </div>
      </div>

      <!-- the cart beside the menu on a computer; smaller screens use the bar below -->
      <aside class="sidecart" v-if="tab === 'menu'" data-testid="side-cart">
        <div class="card">
          <h2>{{ t('cart') }}<span v-if="count" class="muted small"> · {{ count }}</span></h2>
          <CartPanel :store="store" :items="items" :currency="cur" :orderable="orderable" />
          <div class="foot">
            <div v-if="count && minimum > subtotal" class="small muted">{{ t('below_min', { amt: money(minimum - subtotal, cur) }) }}</div>
            <div v-else-if="nextPromo" class="small" style="color:var(--brand-dark)">{{ t('more_for_promo', { amt: money(nextPromo.threshold - subtotal, cur), name: promoLabel(nextPromo, cur) }) }}</div>
            <button v-if="count && missing.length" class="btn block big must" data-testid="required-missing" @click="toRequired">{{ t('sf.required_pick') }} · {{ pick(missing[0].name, missing[0].name_en) }}</button>
            <button v-else class="btn primary block big" data-testid="go-checkout" :disabled="blocked" @click="checkout">{{ t('checkout') }}<template v-if="count"> · {{ money(subtotal, cur) }}</template></button>
          </div>
        </div>
      </aside>
      </div>

      <!-- cart bar -->
      <div class="bottombar cartbar" :class="{ over: showCart }" v-if="tab === 'menu' || count > 0 || showCart">
        <div>
          <button class="bag" :class="{ empty: !count }" :aria-label="t('cart')" data-testid="open-cart" @click="showCart = !showCart && count > 0">
            <svg viewBox="0 0 32 32" width="30" height="30" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12h22l-2.3 13.2a2 2 0 0 1-2 1.7H9.3a2 2 0 0 1-2-1.7z" /><path d="m11 12 4-7M21 12l-4-7M12.5 17v5M16 17v5M19.5 17v5" /></svg>
            <span v-if="count" class="n" data-testid="cart-count">{{ count }}</span>
          </button>
          <b class="amt" data-testid="cart-total" @click="showCart = !showCart && count > 0">{{ money(subtotal, cur) }}</b>
          <span class="hint">
            <template v-if="!count">{{ t('cart_empty') }}</template>
            <span v-else-if="hasUnavailable" style="color:#ff8a80">{{ t('sf.has_unavailable') }}</span>
            <template v-else-if="minimum > subtotal">{{ t('below_min', { amt: money(minimum - subtotal, cur) }) }}</template>
            <template v-else-if="nextPromo">{{ t('more_for_promo', { amt: money(nextPromo.threshold - subtotal, cur), name: promoLabel(nextPromo, cur) }) }}</template>
          </span>
          <button v-if="count && missing.length" class="go must" data-testid="required-missing" @click="toRequired">{{ t('sf.required_pick') }}</button>
          <button v-else class="btn primary go" :disabled="blocked" @click="checkout">{{ !canOrder ? (store.branch.paused ? t('paused') : t('closed')) : t('checkout') }}</button>
        </div>
      </div>

      <DeliveryPicker v-if="showPicker" :store="store" @close="pickerClosed" @pickup="toPickup" />

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
          <div class="tip"><span>{{ store.branch.baskets_enabled ? t('sf.basket_tip') : t('cart') }}</span>
            <button :aria-label="t('close')" @click="showCart = false">×</button></div>
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
    </template>
  </div>
</template>

<style scoped>
.shop { --star: #f5a623; --menu-top: 50px; background: #f3f3f3; }

/* the store card */
.shophead { background: #fff; border-radius: 0 0 18px 18px; padding: 12px 16px 14px; display: grid; grid-template-columns: minmax(0, 1fr); gap: 10px; }
.tools { display: flex; align-items: center; gap: 6px; }
.tools .seg.light button { padding: 5px 14px; font-size: 14px; }
.ico { width: 38px; height: 38px; border: 0; background: none; color: var(--ink); border-radius: 50%; display: grid; place-items: center; cursor: pointer; }
.ico:hover, .ico.on { background: #f2f2f2; }
.ico.in { color: var(--brand); }
.acct { position: relative; }
.acct-menu { position: absolute; right: 0; top: 44px; z-index: 30; background: #fff; border: 1px solid var(--line); border-radius: 12px; box-shadow: 0 8px 24px rgba(0, 0, 0, .14); padding: 12px; width: 200px; display: grid; gap: 6px; }
.lang { color: var(--muted); margin-left: 4px; }
.ident { display: flex; gap: 12px; align-items: flex-start; }
.logo { width: 76px; height: 76px; border-radius: 10px; border: 1px solid #ececec; object-fit: cover; flex: none; background: #fff; }
.logo.ph { display: grid; place-items: center; background: var(--brand-soft); color: var(--brand-dark); font-size: 34px; font-weight: 700; border-color: transparent; }
.ident h1 { font-size: 20px; line-height: 1.25; overflow-wrap: anywhere; }
.meta { display: flex; flex-wrap: wrap; align-items: center; gap: 4px 12px; margin-top: 4px; font-size: 14px; }
.score { color: var(--star); font-weight: 700; font-size: 15px; } .score i { font-style: normal; margin-right: 2px; }
.state { font-size: 12px; font-weight: 600; color: var(--ok); } .state.off { color: var(--warn); }
.chips { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 6px; min-width: 0; }
.chips span { border: 1px solid #b9bdc4; color: #7d828a; border-radius: 5px; padding: 0 6px; font-size: 12px; line-height: 20px; white-space: nowrap; }
.chips.brand { margin: 0; flex: 1; flex-wrap: nowrap; overflow: hidden; }
.chips.brand span { border-color: var(--brand); color: var(--brand-dark); overflow: hidden; text-overflow: ellipsis; flex: 0 1 auto; }
.promoline { display: flex; align-items: center; gap: 8px; }
.more { border: 0; background: none; color: #8b8f96; font-size: 14px; white-space: nowrap; cursor: pointer; padding: 0; }
.notice { display: flex; gap: 4px; color: #a2a6ad; font-size: 14px; }
.notice .lbl { flex: none; }
.marq { overflow: hidden; flex: 1; min-width: 0; white-space: nowrap; }
.marq span { display: inline-block; }
.marq i { display: inline-block; width: 48px; }
.marq.run span { animation: marq 14s linear infinite; }
@keyframes marq { to { transform: translateX(-50%); } }
@media (prefers-reduced-motion: reduce) { .marq.run span { animation: none; } }

/* the three tabs */
.shoptabs { position: sticky; top: 0; z-index: 15; display: flex; gap: 30px; height: 50px; margin-top: 10px; padding: 0 22px; background: #fff; border-radius: 18px 18px 0 0; }
.shoptabs button { position: relative; border: 0; background: none; padding: 0 2px; font-size: 16px; color: #666; cursor: pointer; }
.shoptabs button.on { color: var(--ink); font-weight: 700; }
.shoptabs button.on::after { content: ''; position: absolute; left: 50%; bottom: 6px; width: 22px; height: 3px; margin-left: -11px; border-radius: 2px; background: var(--brand); }
.shoptabs small { font-size: 11px; color: #8b8f96; margin-left: 2px; font-weight: 400; }

.menuwrap { background: #fff; min-height: 60vh; }
.pre { padding: 10px 14px 0; } .pre:last-of-type { padding-bottom: 10px; }
.searchbox { width: 100%; border: 0; background: #f2f2f2; border-radius: 999px; padding: 9px 16px; font-size: 15px; }

/* the store tab */
.about { display: grid; gap: 8px; background: #f3f3f3; }
.about section { background: #fff; padding: 14px 18px; }
.about h3 { font-size: 15px; font-weight: 500; margin-top: 12px; }
.about h3:first-child { margin-top: 0; }
.about p { margin: 6px 0 0; color: #5d6168; white-space: pre-wrap; overflow-wrap: anywhere; }
.about .zone { padding: 6px 0 0; color: #5d6168; }
.reportbtn { border: 0; background: none; padding: 0; color: #7a1b2b; font-size: 15px; cursor: pointer; }

.opt { display: flex; align-items: center; gap: 10px; padding: 10px 0; border-bottom: 1px solid var(--line); cursor: pointer; }
.opt.off { opacity: .5; }

/* the black cart bar */
.shop .bottombar.cartbar { background: none; border: 0; pointer-events: none; z-index: 30; }
.shop .bottombar.cartbar.over { z-index: 70; }
.shop .bottombar.cartbar > div { pointer-events: auto; display: flex; align-items: center; max-width: 560px; height: 56px; margin: 0 auto calc(10px + env(safe-area-inset-bottom)); padding: 0;
  width: calc(100% - 24px); background: #222; color: #fff; border: 0; border-radius: 999px; box-shadow: 0 6px 18px rgba(0,0,0,.25); }
.bag { position: relative; flex: none; width: 62px; height: 62px; margin: -8px 0 -4px 6px; border: 4px solid #222; border-radius: 50%; background: var(--brand); color: #fff; display: grid; place-items: center; cursor: pointer; }
.bag.empty { background: #4a4a4a; color: #bbb; }
.bag .n { position: absolute; top: -4px; right: -6px; min-width: 22px; height: 22px; padding: 0 6px; border-radius: 999px; background: #e5352b; color: #fff; font-size: 13px; font-weight: 700; line-height: 22px; border: 0; }
.amt { font-size: 22px; font-weight: 500; padding: 0 10px 0 12px; margin-left: 6px; border-left: 1px solid #555; white-space: nowrap; cursor: pointer; font-variant-numeric: tabular-nums; }
.hint { flex: 1; min-width: 0; color: #b9b9b9; font-size: 13px; font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; text-align: right; padding-right: 10px; }
.go { flex: none; align-self: stretch; min-width: 104px; padding: 0 16px; border: 0; border-radius: 0 999px 999px 0; font-size: 16px; font-weight: 700; cursor: pointer; }
.btn.go:disabled { background: #555; border-color: #555; color: #bdbdbd; opacity: 1; }
.go.must { background: #c9c9c9; color: #222; }

/* the cart sheet stands on the cart bar */
.cartmask { padding-bottom: calc(76px + env(safe-area-inset-bottom)); }
.cartsheet { max-height: min(74vh, 640px); overflow: hidden; }
.tip { display: flex; align-items: center; justify-content: center; gap: 8px; padding: 9px 44px; background: #f4f4f4; color: #333; font-size: 14px; text-align: center; position: relative; border-radius: 16px 16px 0 0; }
.tip button { position: absolute; right: 8px; top: 50%; transform: translateY(-50%); width: 32px; height: 32px; border: 0; background: none; font-size: 24px; line-height: 1; color: #8b8f96; cursor: pointer; }

.toast { position: fixed; left: 50%; bottom: 96px; transform: translateX(-50%); background: rgba(0,0,0,.8); color: #fff; padding: 8px 16px; border-radius: 999px; font-size: 14px; z-index: 90; }

.sidecart { display: none; }
@media (min-width: 768px) {
  .shophead { padding: 16px var(--gut) 18px; border-radius: 0; }
  .logo { width: 88px; height: 88px; }
  .ident h1 { font-size: 24px; }
  .shoptabs { margin: 12px calc(var(--gut) - 14px) 0; padding: 0 26px; gap: 40px; }
  .shop-main { border-radius: 0 0 14px 14px; overflow: clip; }
  .about section { padding: 16px 26px; }
  .cartmask { padding-bottom: 96px; align-items: flex-end; }
}
@media (min-width: 1024px) {
  .shoptabs { border-radius: 14px 14px 0 0; }
  .shop.solo .shop-cols { grid-template-columns: minmax(0, 1fr); }
  .sidecart { display: block; position: sticky; top: 12px; align-self: start; margin-top: -50px; }
  .sidecart .card { display: flex; flex-direction: column; padding: 0; max-height: calc(100vh - 24px); overflow: hidden; }
  .sidecart h2 { padding: 14px 16px 0; }
  .sidecart .foot { display: grid; gap: 8px; padding: 12px 16px 16px; border-top: 1px solid var(--line); }
  .sidecart .must { background: #eee; border-color: #ddd; }
  .shoptabs { margin-right: calc(var(--gut) - 14px + 352px); }
  .shop.solo .shoptabs { margin-right: calc(var(--gut) - 14px); }
}
</style>
