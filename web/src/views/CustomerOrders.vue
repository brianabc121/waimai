<script setup lang="ts">
// The logged-in customer's orders in every store: tabs by state, a search
// box, one card per order with what can be done about it.
import LangButton from './LangButton.vue'
import { onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { get } from '../api'
import { loadCustomer, loginRoute } from '../customer'
import { locale, money, pick, setLocale, t, mn } from '../i18n'
import { cardStatus, closedStatus, finished, orderAgain, shortStamp } from '../orders'
import type { OrderCard } from '../types'
import CustomerFrame from './me/CustomerFrame.vue'
import CustomerTabs from './me/CustomerTabs.vue'

const router = useRouter()
type Tab = 'all' | 'active' | 'review' | 'aftersale'
const tabs: Tab[] = ['all', 'active', 'review', 'aftersale']
const saved = sessionStorage.getItem('orders-tab') as Tab | null
const tab = ref<Tab>(saved && tabs.includes(saved) ? saved : 'all')
const q = ref('')
const orders = ref<OrderCard[]>([])
const next = ref(0)
const state = ref<'loading' | 'ready' | 'error'>('loading')
const more = ref(false)
const busy = ref('')
const menu = ref('')

// Each load carries a number; only the latest one may fill the list.
let seq = 0
async function load(append = false) {
  const mine = ++seq
  if (append) more.value = true
  else state.value = 'loading'
  try {
    const p = new URLSearchParams({ tab: tab.value, q: q.value.trim() })
    if (append && next.value) p.set('before', String(next.value))
    const r = await get<{ orders: OrderCard[]; next: number }>(`/customer/orders?${p}`)
    if (mine !== seq) return
    orders.value = append ? [...orders.value, ...r.orders] : r.orders
    next.value = r.next
    state.value = 'ready'
  } catch {
    if (mine === seq) state.value = append ? 'ready' : 'error'
  }
  if (mine === seq) more.value = false
}

onMounted(async () => {
  if (!(await loadCustomer())) { router.replace(loginRoute('/orders')); return }
  void load()
})
watch(tab, v => { sessionStorage.setItem('orders-tab', v); menu.value = ''; void load() })
let timer = 0
watch(q, () => { window.clearTimeout(timer); timer = window.setTimeout(() => void load(), 300) })

// Back goes where the customer came from; someone who opened the list
// directly goes to the store of the latest order.
function back() {
  if (window.history.state?.back) router.back()
  else router.replace(orders.value[0] ? `/s/${orders.value[0].store.slug}` : '/')
}
const open = (o: OrderCard, query: Record<string, string> = {}) => router.push({ path: `/o/${o.token}`, query })
async function again(o: OrderCard) {
  busy.value = o.token
  try { const note = await orderAgain(router, o.token); if (note) window.alert(note) }
  catch (e) { window.alert((e as Error).message) }
  busy.value = ''
}
const tone = (o: OrderCard) => closedStatus(o.display_status) ? 'bad' : finished(o.display_status) ? '' : 'live'
</script>

<template>
  <div class="ol warm" data-testid="order-list" :data-state="state" @click="menu = ''">
   <CustomerFrame active="orders">
    <header class="ol-head">
      <div class="ol-bar">
        <button class="ol-back" :aria-label="t('back')" data-testid="orders-back" @click="back">
          <svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M15 5l-7 7 7 7" /></svg></button>
        <h1>{{ t('ol.title') }}</h1>
        <LangButton class="ol-lang" />
        <label class="ol-search">
          <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="11" cy="11" r="6.5" /><path d="M16 16l4.5 4.5" /></svg>
          <input v-model="q" type="search" maxlength="50" :placeholder="t('ol.search')" data-testid="orders-search" />
        </label>
      </div>
    </header>
    <nav class="ol-tabs" role="tablist">
      <button v-for="k in tabs" :key="k" role="tab" :aria-selected="tab === k" :class="{ on: tab === k }" :data-tab="k" @click="tab = k">{{ t('ol.t_' + k) }}</button>
    </nav>

    <main class="ol-list">
      <div v-if="state === 'loading'" class="ol-note">{{ t('loading') }}</div>
      <div v-else-if="state === 'error'" class="ol-note"><p>{{ t('offline') }}</p><button class="btn small" style="margin-top:10px" @click="load()">{{ t('retry') }}</button></div>
      <div v-else-if="!orders.length" class="ol-note" data-testid="orders-empty">{{ t(q.trim() ? 'ol.empty_search' : tab === 'all' ? 'ol.empty' : 'ol.empty_tab') }}</div>

      <article v-for="o in orders" :key="o.token" class="ol-card" :data-order="o.public_no" @click="open(o)">
        <div class="ol-store">
          <a class="ol-name" :href="`/s/${o.store.slug}`" @click.prevent.stop="router.push(`/s/${o.store.slug}`)">
            <img v-if="o.store.logo_url" :src="o.store.logo_url" alt="" />
            <span v-else class="ph"><svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"><path d="M9 3.500c-.8.9.8 1.7 0 2.600M12 3.500c-.8.9.8 1.7 0 2.600M15 3.500c-.8.9.8 1.7 0 2.6" /><path d="M4 11h16a8 8 0 0 1-5 7.400V20H9v-1.600A8 8 0 0 1 4 11z" fill="currentColor" stroke="none" /></svg></span>
            <b>{{ pick(o.store.name, o.store.name_en) }}</b>
            <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><path d="M9 5l7 7-7 7" /></svg>
          </a>
          <span class="ol-status" :class="tone(o)" data-testid="card-status">{{ cardStatus(o) }}</span>
        </div>
        <div class="ol-body">
          <ul class="ol-items">
            <li v-for="(it, i) in o.items.slice(0, 3)" :key="i">
              <img v-if="it.image_url" :src="it.image_url" alt="" loading="lazy" />
              <span v-else class="ph"><svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"><path d="M4 11h16a8 8 0 0 1-5 7.400V20H9v-1.600A8 8 0 0 1 4 11z" /><path d="M10 4c-.8.9.8 1.7 0 2.600M14 4c-.8.9.8 1.7 0 2.6" /></svg></span>
              <span class="nm">{{ mn(it) }}</span>
              <span v-if="it.qty > 1" class="qty">×{{ it.qty }}</span>
            </li>
            <li v-if="o.items.length > 3" class="rest">{{ t('ol.n_more', { n: o.items.length - 3 }) }}</li>
          </ul>
          <div class="ol-sum">
            <div class="ol-total">{{ money(o.total, o.currency) }}</div>
            <div class="ol-count">{{ t('ol.n_items', { n: o.item_count }) }}</div>
          </div>
        </div>
        <div class="ol-meta"><span>{{ shortStamp(o.created_at, o.timezone) }}</span><i>•</i><span>{{ t('od.no') }}: {{ o.public_no }}</span></div>
        <div class="ol-acts" @click.stop>
          <div class="ol-more">
            <button class="ol-morebtn" :aria-expanded="menu === o.token" data-testid="card-more" @click.stop="menu = menu === o.token ? '' : o.token"><b>•••</b>{{ t('ol.more') }}</button>
            <div v-if="menu === o.token" class="ol-menu">
              <button @click="open(o)">{{ t('ol.detail') }}</button>
              <a v-if="o.store.phone" :href="'tel:' + o.store.phone">{{ t('od.call_store') }}</a>
              <button v-if="!o.aftersale && !closedStatus(o.display_status) && o.display_status !== 'pending_acceptance'" @click="open(o, { aftersale: '1' })">{{ t('as.apply') }}</button>
            </div>
          </div>
          <span class="grow"></span>
          <button v-if="o.aftersale" class="pill" @click="open(o)">{{ t('as.' + o.aftersale) }}</button>
          <button v-if="o.can_review" class="pill" data-testid="card-review" @click="open(o, { review: '1' })">{{ t('ol.review') }}</button>
          <button class="pill brand" data-testid="card-again" :disabled="busy === o.token" @click="again(o)">{{ t('ol.again') }}</button>
        </div>
      </article>

      <div v-if="state === 'ready' && orders.length" class="ol-foot">
        <button v-if="next" class="btn small" data-testid="orders-more" :disabled="more" @click="load(true)">{{ t('ol.load_more') }}</button>
        <span v-else class="ol-end">{{ t('ol.end') }}</span>
      </div>
    </main>
   </CustomerFrame>
    <CustomerTabs active="orders" />
  </div>
</template>

<style scoped>
.ol { font-size: 14px; min-height: 100vh; padding-bottom: calc(var(--tabbar-h, 0px) + env(safe-area-inset-bottom) + 24px); }
.ol-head { position: sticky; top: 0; z-index: 10; background: var(--red-bg); color: #fff; }
.ol-bar, .ol-tabs, .ol-list { max-width: 760px; margin: 0 auto; }
.ol-bar { display: grid; grid-template-columns: 44px minmax(0, 1fr) auto; grid-template-areas: 'back title lang' 'search search search'; align-items: center; gap: 6px 8px; padding: 6px 12px 10px; }
.ol-back { grid-area: back; border: 0; background: none; color: #fff; padding: 6px; margin-inline-start: -6px; cursor: pointer; display: flex; border-radius: 50%; justify-self: start; }
.ol-bar h1 { grid-area: title; text-align: center; font-size: 17px; font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ol-lang { grid-area: lang; border: 1px solid rgba(255, 255, 255, .8); background: none; color: #fff; border-radius: 6px; padding: 1px 7px; font-size: 12px; font-weight: 500; cursor: pointer; white-space: nowrap; min-width: 36px; justify-self: end; }
.ol-search { grid-area: search; min-width: 0; display: flex; align-items: center; gap: 8px; background: var(--paper); border-radius: 999px; padding: 0 12px; color: var(--muted); }
.ol-search input { border: 0; padding: 7px 0; background: none; color: var(--ink); outline: none; min-width: 0; font-size: 14px; }
.ol-search input::placeholder { color: var(--muted); }
.ol-search:focus-within { box-shadow: 0 0 0 2px var(--gold); }
.ol-tabs { display: flex; justify-content: space-between; gap: 4px; padding: 10px 12px 4px; overflow-x: auto; scrollbar-width: none; }
.ol-tabs::-webkit-scrollbar { display: none; }
.ol-tabs button { flex: none; border: 0; background: none; padding: 4px 14px; border-radius: 999px; font-size: 14px; color: var(--muted); cursor: pointer; white-space: nowrap; }
.ol-tabs button.on { background: var(--gold); color: var(--ink); font-weight: 600; }
.ol-list { padding: 6px 12px 0; display: grid; gap: 10px; }
.ol-note { text-align: center; color: var(--muted); padding: 64px 16px; font-size: 13px; }
.ol-card { background: var(--paper); border: 1px solid var(--line); border-radius: 12px; padding: 10px 12px; cursor: pointer; display: grid; gap: 8px; min-width: 0; }
.ol-store { display: flex; align-items: center; justify-content: space-between; gap: 10px; padding-bottom: 8px; border-bottom: 1px solid var(--line); }
.ol-name { display: flex; align-items: center; gap: 8px; color: var(--ink); min-width: 0; font-size: 15px; }
.ol-name b { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-weight: 600; }
.ol-name > svg { flex: none; color: var(--muted); }
.ol-name img, .ol-name .ph { width: 28px; height: 28px; border-radius: 7px; object-fit: cover; flex: none; }
.ol-name .ph { background: #c2401c; color: #fff3da; display: grid; place-items: center; }
.ol-status { white-space: nowrap; font-size: 12px; padding: 1px 8px; border-radius: 999px; background: #e7e2d9; color: #6d6259; }
.ol-status.live { background: var(--gold-soft); color: var(--red); font-weight: 600; }
.ol-body { display: flex; align-items: stretch; gap: 10px; }
.ol-items { list-style: none; margin: 0; padding: 0; flex: 1; min-width: 0; display: grid; gap: 6px; align-content: center; }
.ol-items li { display: flex; align-items: center; gap: 10px; min-width: 0; }
.ol-items img, .ol-items .ph { width: 64px; height: 40px; border-radius: 6px; object-fit: cover; flex: none; }
.ol-items .ph { background: var(--gold-soft); color: #b98a3a; display: grid; place-items: center; }
.ol-items .nm { font-size: 14px; font-weight: 500; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; min-width: 0; }
.ol-items .qty { color: var(--muted); font-size: 12px; flex: none; }
.ol-items .rest { color: var(--muted); font-size: 12px; }
.ol-sum { flex: none; display: grid; align-content: center; justify-items: center; gap: 0; padding-inline-start: 12px; border-inline-start: 1px solid var(--line); min-width: 76px; }
.ol-total { font-size: 17px; font-weight: 700; color: var(--red); font-variant-numeric: tabular-nums; white-space: nowrap; }
.ol-count { color: var(--muted); font-size: 12px; }
.ol-meta { display: flex; flex-wrap: wrap; gap: 0 8px; color: var(--muted); font-size: 12px; padding-bottom: 8px; border-bottom: 1px solid var(--line); }
.ol-meta i { font-style: normal; }
.ol-acts { display: flex; align-items: center; gap: 8px; }
.ol-more { position: relative; }
.ol-morebtn { border: 0; background: none; color: var(--muted); padding: 6px 4px; cursor: pointer; display: flex; align-items: center; gap: 6px; font-size: 13px; }
.ol-morebtn b { letter-spacing: 1px; color: var(--ink); font-size: 11px; }
.ol-menu { position: absolute; inset-inline-start: 0; bottom: calc(100% + 4px); z-index: 5; background: #fff; border: 1px solid var(--line); border-radius: 10px; box-shadow: 0 8px 24px rgba(60, 30, 10, .16); padding: 4px; min-width: 120px; display: grid; font-size: 13px; }
.ol-menu button, .ol-menu a { border: 0; background: none; text-align: start; padding: 8px 12px; border-radius: 6px; color: var(--ink); cursor: pointer; white-space: nowrap; }
.ol-menu button:hover, .ol-menu a:hover { background: var(--bg); }
.pill { border: 1px solid #cdb9a0; background: none; color: var(--ink); border-radius: 999px; padding: 4px 14px; font-size: 13px; cursor: pointer; white-space: nowrap; min-height: 30px; }
.pill.brand { border-color: var(--red); background: var(--red); color: #fff; font-weight: 500; padding: 4px 16px; }
.pill.brand:hover { background: var(--red-deep); }
.pill:disabled { opacity: .5; }
.ol-foot { text-align: center; padding: 8px 0 6px; }
.ol-end { display: flex; align-items: center; justify-content: center; gap: 14px; color: var(--muted); font-size: 12px; }
.ol-end::before, .ol-end::after { content: ""; width: 48px; height: 1px; background: #cdbda8; }
@media (max-width: 380px) {
  .ol-tabs button { padding: 4px 10px; font-size: 13.5px; }
  .ol-items img, .ol-items .ph { width: 54px; height: 36px; } .ol-items .nm { font-size: 13.5px; }
  .ol-sum { min-width: 66px; padding-inline-start: 10px; } .ol-total { font-size: 16px; }
  .pill { padding: 4px 10px; } .pill.brand { padding: 4px 12px; }
}
/* an iPad or a computer: the red bar is the top bar above; title, search and
   tabs head the list */
@media (min-width: 768px) {
  .ol { padding-bottom: 40px; }
  .ol-head { position: static; background: none; color: var(--ink); }
  .ol-bar, .ol-tabs, .ol-list { max-width: none; }
  .ol-bar { grid-template-columns: 36px auto minmax(0, 1fr); grid-template-areas: 'back title search'; gap: 4px 12px; padding: 0 0 12px; }
  .ol-back { color: var(--ink); margin-inline-start: -8px; }
  .ol-back:hover { background: var(--gold-soft); }
  .ol-bar h1 { text-align: start; font-size: 20px; }
  .ol-lang { display: none; }
  .ol-search { max-width: 360px; width: 100%; justify-self: end; border: 1px solid var(--line); }
  .ol-tabs { justify-content: flex-start; gap: 8px; padding: 0 0 12px; }
  .ol-list { padding: 0; gap: 12px; }
  .ol-card { padding: 12px 16px; }
}
/* a computer: the side list is the way back; two orders side by side */
@media (min-width: 1024px) {
  .ol-bar { grid-template-columns: auto minmax(0, 1fr); grid-template-areas: 'title search'; }
  .ol-back { display: none; }
  .ol-list { grid-template-columns: repeat(2, minmax(0, 1fr)); align-items: start; }
  .ol-note, .ol-foot { grid-column: 1 / -1; }
}
</style>
