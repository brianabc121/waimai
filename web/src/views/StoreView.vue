<script setup lang="ts">
import { computed, defineAsyncComponent, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ApiError, get, post } from '../api'
import { addLine, clearLines, linePrice, useCart } from '../cart'
import { locale, money, pick, promoLabel, setLocale, t } from '../i18n'
import { defaultTemplate, themeVars } from '../theme'
import type { Item, Located, Storefront } from '../types'
import MenuList from './MenuList.vue'

// The map is only downloaded when a customer opens "where to deliver".
const DeliveryPicker = defineAsyncComponent(() => import('./DeliveryPicker.vue'))

const props = defineProps<{ slug: string }>()
const router = useRouter()
const cart = useCart(props.slug)

const store = ref<Storefront | null>(null)
const error = ref('')
const tab = ref<'menu' | 'info'>('menu')
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
function checkout() {
  if (cart.service === 'delivery' && mapMode.value && !zone.value) { showPicker.value = true; return }
  router.push(`/s/${props.slug}/checkout`)
}

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

function optionNames(l: { item_id: number; option_ids: number[] }) {
  const it = items.value.get(l.item_id)
  if (!it) return ''
  const all = new Map(it.groups.flatMap(g => g.options).map(o => [o.id, o]))
  return l.option_ids.map(id => all.get(id)).filter(Boolean).map(o => pick(o!.name, o!.name_en)).join(' · ')
}

function changeQty(i: number, d: number) {
  const l = cart.lines[i]
  l.qty += d
  if (l.qty <= 0) cart.lines.splice(i, 1)
  if (cart.lines.length === 0) showCart.value = false
}

function clearAll() {
  if (confirm(t('clear_confirm'))) { clearLines(); showCart.value = false }
}

function qtyOf(it: Item) {
  return cart.lines.filter(l => l.item_id === it.id).reduce((n, l) => n + l.qty, 0)
}

</script>

<template>
  <div class="phone wide shop" :style="themeVars(tpl)">
    <div v-if="error" class="section stack">
      <div class="banner bad">{{ error }}</div>
      <button class="btn" @click="load">{{ t('retry') }}</button>
    </div>
    <div v-else-if="!store" class="section muted">{{ t('loading') }}</div>
    <template v-else>
      <div class="topbar">
        <div class="row between">
          <h1 class="row" style="gap:8px"><img v-if="tpl.logo_url" :src="tpl.logo_url" alt="" class="logo" />{{ pick(store.branch.name, store.branch.name_en) }}</h1>
          <button class="lang" @click="setLocale(locale === 'zh' ? 'en' : 'zh')">{{ locale === 'zh' ? 'EN' : '中文' }}</button>
        </div>
        <div class="row between" style="margin-top:10px">
          <div class="seg" v-if="store.branch.delivery_enabled && store.branch.pickup_enabled">
            <button :class="{ on: cart.service === 'delivery' }" @click="cart.service = 'delivery'">{{ t('delivery') }}</button>
            <button :class="{ on: cart.service === 'pickup' }" @click="cart.service = 'pickup'">{{ t('pickup') }}</button>
          </div>
          <span v-else class="bold">{{ t(cart.service) }}</span>
          <div class="seg">
            <button :class="{ on: tab === 'menu' }" @click="tab = 'menu'">{{ t('menu') }}</button>
            <button :class="{ on: tab === 'info' }" @click="tab = 'info'">{{ t('store_info') }}</button>
          </div>
        </div>
      </div>

      <img v-if="tpl.cover_url" :src="tpl.cover_url" alt="" class="cover" />
      <div class="shop-cols"><div class="shop-main">
      <div class="section" v-if="!canOrder">
        <div class="banner warn">{{ store.branch.paused ? t('paused') : t('closed') }}</div>
      </div>
      <div class="section" v-else-if="ahead"><div class="banner info">{{ t('closed_can_schedule') }}</div></div>

      <div class="section promos" v-if="tab === 'menu' && promos.length">
        <span v-for="p in promos" :key="p.id" class="tag brand" :title="pick(p.name, p.name_en)">{{ promoLabel(p, cur) }}</span>
      </div>

      <!-- store info (C02) -->
      <div v-if="tab === 'info'" class="section">
        <div class="card stack">
          <div class="row between"><h2>{{ pick(store.branch.name, store.branch.name_en) }}</h2>
            <span class="tag" :class="canOrder ? 'ok' : 'warn'">{{ canOrder ? t('open') : store.branch.paused ? t('paused') : t('closed') }}</span></div>
          <div><div class="muted small">{{ t('hours') }}</div>
            {{ store.branch.open_time === store.branch.close_time ? t('all_day') : store.branch.open_time + ' – ' + store.branch.close_time }}</div>
          <div v-if="store.branch.address"><div class="muted small">{{ t('address') }}</div>{{ store.branch.address }}</div>
          <div v-if="store.branch.phone"><div class="muted small">{{ t('phone') }}</div><a :href="'tel:' + store.branch.phone">{{ store.branch.phone }}</a></div>
          <div v-if="store.branch.delivery_enabled && store.zones.length">
            <div class="muted small">{{ t('zones') }}</div>
            <div v-for="z in store.zones" :key="z.id" class="row between">
              <span>{{ pick(z.name, z.name_en) }}</span><span class="price">{{ money(z.fee, cur) }}</span>
            </div>
          </div>
        </div>
      </div>

      <!-- menu (C01) -->
      <template v-else>
        <div class="section" v-if="cart.service === 'delivery' && mapMode" style="padding-bottom:0">
          <button type="button" class="card row between" style="width:100%;text-align:left;cursor:pointer" data-testid="deliver-to" @click="showPicker = true">
            <span v-if="zone"><span class="muted small">{{ t('deliver_where') }}</span><br /><b>{{ cart.placeLabel || pick(zone.name, zone.name_en) }}</b>
              <span class="small muted"><br />{{ pick(zone.name, zone.name_en) }} · {{ t('delivery_fee') }} {{ money(zone.fee, cur) }}<template v-if="zone.eta_minutes"> · {{ t('eta_min', { n: zone.eta_minutes }) }}</template></span></span>
            <span v-else><b>📍 {{ t('pin_needed') }}</b></span>
            <span class="muted">›</span>
          </button>
        </div>
        <div class="section" v-else-if="cart.service === 'delivery'" style="padding-bottom:0">
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
        <MenuList :categories="store.categories" :currency="cur" :layout="tpl.layout" :can-order="orderable" :qty-of="qtyOf"
          @add="open" @dec="it => changeQty(cart.lines.findIndex(l => l.item_id === it.id), -1)" />
      </template>
      </div>

      <!-- the cart beside the menu on a computer; smaller screens use the bar below -->
      <aside class="sidecart" v-if="tab === 'menu'" data-testid="side-cart">
        <div class="card">
          <div class="row between"><h2>{{ t('cart') }}<span v-if="count" class="muted small"> · {{ count }}</span></h2>
            <button v-if="count" class="btn link" @click="clearAll">{{ t('clear') }}</button></div>
          <p v-if="!count" class="muted empty">{{ t('cart_empty') }}</p>
          <div v-else class="lines stack">
            <div v-for="(l, i) in cart.lines" :key="l.key" class="row top between">
              <div class="grow">
                <div class="bold">{{ items.get(l.item_id) ? pick(items.get(l.item_id)!.name, items.get(l.item_id)!.name_en) : '—' }}</div>
                <div class="muted small" v-if="optionNames(l)">{{ optionNames(l) }}</div>
                <div class="muted small" v-if="l.note">{{ t('note') }}: {{ l.note }}</div>
                <div v-if="!items.get(l.item_id) || !orderable(items.get(l.item_id)!) || linePrice(l, items) === null" class="small" style="color:var(--bad)">{{ t('unavailable') }}</div>
                <div v-else class="price small">{{ money(linePrice(l, items)! * l.qty, cur) }}</div>
              </div>
              <span class="stepper"><button @click="changeQty(i, -1)">−</button><span>{{ l.qty }}</span><button class="fill" @click="changeQty(i, 1)">+</button></span>
            </div>
          </div>
          <template v-if="count">
            <div v-if="minimum > subtotal" class="small muted">{{ t('below_min', { amt: money(minimum - subtotal, cur) }) }}</div>
            <div v-else-if="nextPromo" class="small" style="color:var(--brand-dark)">{{ t('more_for_promo', { amt: money(nextPromo.threshold - subtotal, cur), name: promoLabel(nextPromo, cur) }) }}</div>
            <div class="row between sum"><span>{{ t('subtotal') }}</span><b class="price">{{ money(subtotal, cur) }}</b></div>
            <button class="btn primary block big" data-testid="go-checkout" :disabled="!canOrder || hasUnavailable || minimum > subtotal" @click="checkout">{{ t('checkout') }}</button>
          </template>
        </div>
      </aside>
      </div>

      <!-- cart bar -->
      <div class="bottombar" v-if="count > 0">
        <div>
          <div v-if="minimum > subtotal" class="small muted" style="margin-bottom:6px">{{ t('below_min', { amt: money(minimum - subtotal, cur) }) }}</div>
          <div v-else-if="nextPromo" class="small" style="margin-bottom:6px;color:var(--brand-dark)">{{ t('more_for_promo', { amt: money(nextPromo.threshold - subtotal, cur), name: promoLabel(nextPromo, cur) }) }}</div>
          <div class="row between">
            <button class="btn" @click="showCart = true">{{ t('cart') }} · {{ count }}</button>
            <span class="price bold grow right">{{ money(subtotal, cur) }}</span>
            <button class="btn primary" :disabled="!canOrder || hasUnavailable || minimum > subtotal"
              @click="checkout">{{ t('checkout') }}</button>
          </div>
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

      <!-- cart (C06) -->
      <div class="sheet-mask" v-if="showCart" @click.self="showCart = false">
        <div class="sheet">
          <div class="sheet-head row between"><h2>{{ t('cart') }}</h2><button class="btn link" @click="clearAll">{{ t('clear') }}</button></div>
          <div class="sheet-body stack">
            <div v-for="(l, i) in cart.lines" :key="l.key" class="row top between">
              <div class="grow">
                <div class="bold">{{ items.get(l.item_id) ? pick(items.get(l.item_id)!.name, items.get(l.item_id)!.name_en) : '—' }}</div>
                <div class="muted small" v-if="optionNames(l)">{{ optionNames(l) }}</div>
                <div class="muted small" v-if="l.note">{{ t('note') }}: {{ l.note }}</div>
                <div v-if="!items.get(l.item_id) || !orderable(items.get(l.item_id)!) || linePrice(l, items) === null" class="small" style="color:var(--bad)">{{ t('unavailable') }}</div>
                <div v-else class="price small">{{ money(linePrice(l, items)! * l.qty, cur) }}</div>
              </div>
              <span class="stepper"><button @click="changeQty(i, -1)">−</button><span>{{ l.qty }}</span><button class="fill" @click="changeQty(i, 1)">+</button></span>
            </div>
          </div>
          <div class="sheet-foot row between">
            <span>{{ t('subtotal') }} <b class="price">{{ money(subtotal, cur) }}</b></span>
            <button class="btn primary" :disabled="!canOrder || hasUnavailable || minimum > subtotal" @click="router.push(`/s/${slug}/checkout`)">{{ t('checkout') }}</button>
          </div>
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
.promos { display: flex; flex-wrap: wrap; gap: 6px; padding-bottom: 0; }
.opt { display: flex; align-items: center; gap: 10px; padding: 10px 0; border-bottom: 1px solid var(--line); cursor: pointer; }
.opt.off { opacity: .5; }
.sidecart { display: none; }
@media (min-width: 1024px) {
  .sidecart { display: block; position: sticky; top: 12px; align-self: start; padding: 12px 0; }
  .sidecart .card { display: grid; gap: 12px; padding: 16px; }
  .sidecart .lines { max-height: calc(100vh - 300px); overflow-y: auto; }
  .sidecart .empty { padding: 28px 0; text-align: center; }
  .sidecart .sum { border-top: 1px solid var(--line); padding-top: 12px; font-size: 17px; }
}
</style>
