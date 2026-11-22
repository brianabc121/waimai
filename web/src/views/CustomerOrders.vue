<script setup lang="ts">
// The logged-in customer's orders in every store: tabs by state, a search
// box, one card per order with what can be done about it.
import { onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { get } from '../api'
import { loadCustomer, loginRoute } from '../customer'
import { locale, money, pick, setLocale, t } from '../i18n'
import { cardStatus, closedStatus, finished, orderAgain, shortStamp } from '../orders'
import type { OrderCard } from '../types'
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
    <header class="ol-head">
      <div class="ol-bar">
        <button class="ol-back" :aria-label="t('back')" data-testid="orders-back" @click="back">
          <svg viewBox="0 0 24 24" width="26" height="26" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><path d="M15 5l-7 7 7 7" /></svg></button>
        <h1>{{ t('ol.title') }}</h1>
        <button class="ol-lang" @click="setLocale(locale === 'zh' ? 'en' : 'zh')">{{ locale === 'zh' ? 'EN' : '中文' }}</button>
        <label class="ol-search">
          <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="11" cy="11" r="6.5" /><path d="M16 16l4.5 4.5" /></svg>
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
            <span v-else class="ph"><svg viewBox="0 0 24 24" width="26" height="26" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"><path d="M9 3.500c-.8.9.8 1.7 0 2.600M12 3.500c-.8.9.8 1.7 0 2.600M15 3.500c-.8.9.8 1.7 0 2.6" /><path d="M4 11h16a8 8 0 0 1-5 7.400V20H9v-1.600A8 8 0 0 1 4 11z" fill="currentColor" stroke="none" /></svg></span>
            <b>{{ pick(o.store.name, o.store.name_en) }}</b>
            <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2.6" stroke-linecap="round" stroke-linejoin="round"><path d="M9 5l7 7-7 7" /></svg>
          </a>
          <span class="ol-status" :class="tone(o)" data-testid="card-status">{{ cardStatus(o) }}</span>
        </div>
        <div class="ol-body">
          <ul class="ol-items">
            <li v-for="(it, i) in o.items.slice(0, 3)" :key="i">
              <img v-if="it.image_url" :src="it.image_url" alt="" loading="lazy" />
              <span v-else class="ph"><svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"><path d="M4 11h16a8 8 0 0 1-5 7.400V20H9v-1.600A8 8 0 0 1 4 11z" /><path d="M10 4c-.8.9.8 1.7 0 2.600M14 4c-.8.9.8 1.7 0 2.6" /></svg></span>
              <span class="nm">{{ pick(it.name, it.name_en) }}</span>
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
    <CustomerTabs active="orders" />
  </div>
</template>

<style scoped>
.ol { min-height: 100vh; padding-bottom: calc(var(--tabbar-h, 0px) + env(safe-area-inset-bottom) + 24px); }
.ol-head { position: sticky; top: 0; z-index: 10; background: var(--red); color: #fff; }
.ol-bar, .ol-tabs, .ol-list { max-width: 760px; margin: 0 auto; }
.ol-bar { display: grid; grid-template-columns: 44px minmax(0, 1fr) auto; grid-template-areas: 'back title lang' 'search search search'; align-items: center; gap: 8px 8px; padding: 10px 16px 14px; }
.ol-back { grid-area: back; border: 0; background: none; color: #fff; padding: 6px; margin-left: -8px; cursor: pointer; display: flex; border-radius: 50%; justify-self: start; }
.ol-bar h1 { grid-area: title; text-align: center; font-size: 22px; font-weight: 800; letter-spacing: 2px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ol-lang { grid-area: lang; border: 1.5px solid rgba(255, 255, 255, .85); background: none; color: #fff; border-radius: 7px; padding: 2px 9px; font-size: 13.5px; font-weight: 700; cursor: pointer; white-space: nowrap; min-width: 44px; }
.ol-search { grid-area: search; min-width: 0; display: flex; align-items: center; gap: 10px; background: var(--paper); border-radius: 12px; padding: 0 14px; color: var(--muted); }
.ol-search input { border: 0; padding: 10px 0; background: none; color: var(--ink); outline: none; min-width: 0; font-size: 16px; }
.ol-search input::placeholder { color: var(--muted); }
.ol-search:focus-within { box-shadow: 0 0 0 2px var(--gold); }
.ol-tabs { display: flex; justify-content: space-between; gap: 6px; padding: 14px 14px 6px; overflow-x: auto; scrollbar-width: none; }
.ol-tabs::-webkit-scrollbar { display: none; }
.ol-tabs button { flex: none; border: 0; background: none; padding: 7px 18px; border-radius: 999px; font-size: 16.5px; color: var(--ink); cursor: pointer; white-space: nowrap; }
.ol-tabs button.on { background: var(--gold); font-weight: 800; }
.ol-list { padding: 8px 12px 0; display: grid; gap: 12px; }
.ol-note { text-align: center; color: var(--muted); padding: 64px 16px; }
.ol-card { background: var(--paper); border: 1px solid var(--line); border-radius: 16px; padding: 12px 14px; cursor: pointer; display: grid; gap: 10px; min-width: 0; }
.ol-store { display: flex; align-items: center; justify-content: space-between; gap: 10px; padding-bottom: 10px; border-bottom: 1px solid var(--line); }
.ol-name { display: flex; align-items: center; gap: 12px; color: var(--ink); min-width: 0; font-size: 19px; }
.ol-name b { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-weight: 800; }
.ol-name > svg { flex: none; }
.ol-name img, .ol-name .ph { width: 40px; height: 40px; border-radius: 10px; object-fit: cover; flex: none; }
.ol-name .ph { background: #c2401c; color: #fff3da; display: grid; place-items: center; }
.ol-status { white-space: nowrap; font-size: 14px; padding: 3px 12px; border-radius: 999px; background: #e7e2d9; color: #6d6259; }
.ol-status.live { background: var(--gold-soft); color: var(--red); font-weight: 700; }
.ol-body { display: flex; align-items: stretch; gap: 12px; }
.ol-items { list-style: none; margin: 0; padding: 0; flex: 1; min-width: 0; display: grid; gap: 8px; align-content: center; }
.ol-items li { display: flex; align-items: center; gap: 12px; min-width: 0; }
.ol-items img, .ol-items .ph { width: 100px; height: 52px; border-radius: 9px; object-fit: cover; flex: none; }
.ol-items .ph { background: var(--gold-soft); color: #b98a3a; display: grid; place-items: center; }
.ol-items .nm { font-size: 16.5px; font-weight: 700; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; min-width: 0; }
.ol-items .qty { color: var(--muted); font-size: 13.5px; flex: none; }
.ol-items .rest { color: var(--muted); font-size: 13.5px; }
.ol-sum { flex: none; display: grid; align-content: center; justify-items: center; gap: 2px; padding-left: 14px; border-left: 1px solid var(--line); min-width: 92px; }
.ol-total { font-size: 23px; font-weight: 800; color: var(--red); font-variant-numeric: tabular-nums; white-space: nowrap; }
.ol-count { color: var(--muted); font-size: 14.5px; }
.ol-meta { display: flex; flex-wrap: wrap; gap: 0 10px; color: var(--muted); font-size: 14px; padding-bottom: 10px; border-bottom: 1px solid var(--line); }
.ol-meta i { font-style: normal; }
.ol-acts { display: flex; align-items: center; gap: 8px; }
.ol-more { position: relative; }
.ol-morebtn { border: 0; background: none; color: var(--muted); padding: 8px 4px; cursor: pointer; display: flex; align-items: center; gap: 10px; font-size: 15px; }
.ol-morebtn b { letter-spacing: 1px; color: var(--ink); font-size: 13px; }
.ol-menu { position: absolute; left: 0; bottom: calc(100% + 4px); z-index: 5; background: #fff; border: 1px solid var(--line); border-radius: 10px; box-shadow: 0 8px 24px rgba(60, 30, 10, .16); padding: 4px; min-width: 132px; display: grid; }
.ol-menu button, .ol-menu a { border: 0; background: none; text-align: left; padding: 9px 12px; border-radius: 6px; color: var(--ink); cursor: pointer; white-space: nowrap; }
.ol-menu button:hover, .ol-menu a:hover { background: var(--bg); }
.pill { border: 1px solid #cdb9a0; background: none; color: var(--ink); border-radius: 10px; padding: 7px 14px; cursor: pointer; white-space: nowrap; min-height: 38px; }
.pill.brand { border-color: var(--red); background: var(--red); color: #fff; font-weight: 800; padding: 7px 24px; letter-spacing: 1px; }
.pill.brand:hover { background: var(--red-deep); }
.pill:disabled { opacity: .5; }
.ol-foot { text-align: center; padding: 12px 0 6px; }
.ol-end { display: flex; align-items: center; justify-content: center; gap: 22px; color: var(--muted); font-size: 14px; }
.ol-end::before, .ol-end::after { content: ""; width: 64px; height: 1px; background: #cdbda8; }
@media (max-width: 380px) {
  .ol-tabs button { padding: 7px 12px; font-size: 15.5px; }
  .ol-items img, .ol-items .ph { width: 76px; height: 46px; } .ol-items .nm { font-size: 15.5px; }
  .ol-sum { min-width: 76px; padding-left: 10px; } .ol-total { font-size: 20px; }
  .pill { padding: 7px 10px; } .pill.brand { padding: 7px 16px; }
}
@media (min-width: 768px) {
  .ol-bar { padding: 14px 20px 16px; }
  .ol-tabs { justify-content: flex-start; gap: 12px; padding: 16px 20px 8px; } .ol-list { padding: 8px 20px 0; }
}
/* a computer shows two orders side by side, and the search beside the title */
@media (min-width: 1024px) {
  .ol-bar, .ol-tabs, .ol-list { max-width: 1100px; }
  .ol-bar { grid-template-columns: 44px auto minmax(0, 1fr) auto; grid-template-areas: 'back title search lang'; gap: 16px; }
  .ol-bar h1 { text-align: left; } .ol-search { max-width: 420px; justify-self: end; width: 100%; }
  .ol-list { grid-template-columns: repeat(2, minmax(0, 1fr)); align-items: start; }
  .ol-note, .ol-foot { grid-column: 1 / -1; }
}
</style>
