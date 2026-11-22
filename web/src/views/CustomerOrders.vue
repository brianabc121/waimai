<script setup lang="ts">
// The logged-in customer's orders in every store: tabs by state, a search
// box, one card per order with what can be done about it.
import { onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { get } from '../api'
import { loadCustomer, loginRoute } from '../customer'
import { locale, money, pick, setLocale, t } from '../i18n'
import { cardStatus, closedStatus, finished, orderAgain, stamp } from '../orders'
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
  <div class="ol" data-testid="order-list" :data-state="state" @click="menu = ''">
    <header class="ol-head">
      <div class="ol-bar">
        <button class="ol-back" :aria-label="t('back')" data-testid="orders-back" @click="back">
          <svg viewBox="0 0 24 24" width="24" height="24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M15 5l-7 7 7 7" /></svg></button>
        <label class="ol-search">
          <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="11" cy="11" r="6.5" /><path d="M16 16l4.5 4.5" /></svg>
          <input v-model="q" type="search" maxlength="50" :placeholder="t('ol.search')" data-testid="orders-search" />
        </label>
        <button class="ol-lang" @click="setLocale(locale === 'zh' ? 'en' : 'zh')">{{ locale === 'zh' ? 'EN' : '中文' }}</button>
      </div>
      <nav class="ol-tabs" role="tablist">
        <button v-for="k in tabs" :key="k" role="tab" :aria-selected="tab === k" :class="{ on: tab === k }" :data-tab="k" @click="tab = k">{{ t('ol.t_' + k) }}</button>
      </nav>
    </header>

    <main class="ol-list">
      <div v-if="state === 'loading'" class="ol-note">{{ t('loading') }}</div>
      <div v-else-if="state === 'error'" class="ol-note"><p>{{ t('offline') }}</p><button class="btn small" style="margin-top:10px" @click="load()">{{ t('retry') }}</button></div>
      <div v-else-if="!orders.length" class="ol-note" data-testid="orders-empty">{{ t(q.trim() ? 'ol.empty_search' : tab === 'all' ? 'ol.empty' : 'ol.empty_tab') }}</div>

      <article v-for="o in orders" :key="o.token" class="ol-card" :data-order="o.public_no" @click="open(o)">
        <div class="ol-store">
          <a class="ol-name" :href="`/s/${o.store.slug}`" @click.prevent.stop="router.push(`/s/${o.store.slug}`)">
            <img v-if="o.store.logo_url" :src="o.store.logo_url" alt="" /><span v-else class="ph">{{ pick(o.store.name, o.store.name_en).slice(0, 1) }}</span>
            <b>{{ pick(o.store.name, o.store.name_en) }}</b>
            <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 5l7 7-7 7" /></svg>
          </a>
          <span class="ol-status" :class="tone(o)" data-testid="card-status">{{ cardStatus(o) }}</span>
        </div>
        <div class="ol-body">
          <div class="ol-thumbs">
            <template v-for="(it, i) in o.items.slice(0, 3)" :key="i">
              <img v-if="it.image_url" :src="it.image_url" :alt="pick(it.name, it.name_en)" loading="lazy" />
              <span v-else class="ph" :title="pick(it.name, it.name_en)">{{ pick(it.name, it.name_en) }}</span>
            </template>
          </div>
          <div class="ol-sum">
            <div class="ol-total">{{ money(o.total, o.currency) }}</div>
            <div class="muted small">{{ t('ol.n_items', { n: o.item_count }) }}</div>
          </div>
        </div>
        <div class="muted small">{{ t('ol.ordered_at') }}: {{ stamp(o.created_at, o.timezone) }} · {{ o.public_no }}</div>
        <div class="ol-acts" @click.stop>
          <div class="ol-more">
            <button class="ol-morebtn" :aria-expanded="menu === o.token" data-testid="card-more" @click.stop="menu = menu === o.token ? '' : o.token">{{ t('ol.more') }}</button>
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
        <span v-else class="muted small">{{ t('ol.end') }}</span>
      </div>
    </main>
    <CustomerTabs active="orders" />
  </div>
</template>

<style scoped>
.ol { min-height: 100vh; padding-bottom: calc(var(--tabbar-h, 0px) + env(safe-area-inset-bottom) + 24px); }
.ol-head { position: sticky; top: 0; z-index: 10; background: var(--bg); }
.ol-bar, .ol-tabs, .ol-list { max-width: 760px; margin: 0 auto; }
.ol-bar { display: flex; align-items: center; gap: 8px; padding: 10px 12px 6px; }
.ol-back { border: 0; background: none; padding: 6px; margin-left: -6px; cursor: pointer; display: flex; border-radius: 50%; }
.ol-search { flex: 1; min-width: 0; display: flex; align-items: center; gap: 8px; background: #fff; border-radius: 999px; padding: 0 14px; color: var(--muted); border: 1px solid var(--line); }
.ol-search input { border: 0; padding: 9px 0; background: none; color: var(--ink); outline: none; min-width: 0; }
.ol-search:focus-within { border-color: var(--brand); }
.ol-lang { border: 1px solid var(--line); background: #fff; border-radius: 999px; padding: 6px 12px; font-size: 13px; font-weight: 600; cursor: pointer; white-space: nowrap; }
.ol-tabs { display: flex; gap: 22px; padding: 4px 16px 0; overflow-x: auto; scrollbar-width: none; }
.ol-tabs::-webkit-scrollbar { display: none; }
.ol-tabs button { border: 0; background: none; padding: 8px 0 10px; font-size: 16px; color: #4b5563; cursor: pointer; white-space: nowrap; position: relative; }
.ol-tabs button.on { color: var(--ink); font-weight: 700; }
.ol-tabs button.on::after { content: ""; position: absolute; left: 0; right: 0; bottom: 4px; height: 4px; border-radius: 2px; background: var(--brand); }
.ol-list { padding: 8px 12px 0; display: grid; gap: 10px; }
.ol-note { text-align: center; color: var(--muted); padding: 64px 16px; }
.ol-card { background: #fff; border-radius: 12px; padding: 14px; cursor: pointer; display: grid; gap: 10px; min-width: 0; }
.ol-store { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
.ol-name { display: flex; align-items: center; gap: 8px; color: var(--ink); min-width: 0; font-size: 16px; }
.ol-name b { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ol-name svg { flex: none; color: var(--muted); }
.ol-name img, .ol-name .ph { width: 24px; height: 24px; border-radius: 6px; object-fit: cover; flex: none; }
.ol-name .ph { background: var(--brand); color: #fff; font-size: 13px; font-weight: 700; display: grid; place-items: center; }
.ol-status { color: var(--muted); white-space: nowrap; font-size: 14px; }
.ol-status.live { color: var(--brand-dark); font-weight: 600; } .ol-status.bad { color: var(--muted); }
.ol-body { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.ol-thumbs { display: flex; gap: 8px; min-width: 0; overflow: hidden; }
.ol-thumbs img, .ol-thumbs .ph { width: 76px; height: 76px; border-radius: 8px; object-fit: cover; flex: none; }
.ol-thumbs .ph { background: var(--brand-soft); color: var(--brand-dark); font-size: 13px; font-weight: 600; padding: 6px; display: flex; align-items: center; justify-content: center; overflow: hidden; line-height: 1.3; text-align: center; word-break: break-word; }
.ol-sum { text-align: right; flex: none; }
.ol-total { font-size: 19px; font-weight: 700; font-variant-numeric: tabular-nums; }
.ol-acts { display: flex; align-items: center; gap: 8px; }
.ol-more { position: relative; }
.ol-morebtn { border: 0; background: none; color: var(--muted); padding: 8px 4px; cursor: pointer; }
.ol-menu { position: absolute; left: 0; bottom: calc(100% + 4px); z-index: 5; background: #fff; border: 1px solid var(--line); border-radius: 10px; box-shadow: 0 8px 24px rgba(0,0,0,.12); padding: 4px; min-width: 132px; display: grid; }
.ol-menu button, .ol-menu a { border: 0; background: none; text-align: left; padding: 9px 12px; border-radius: 6px; color: var(--ink); cursor: pointer; white-space: nowrap; }
.ol-menu button:hover, .ol-menu a:hover { background: var(--bg); }
.pill { border: 1px solid #d1d5db; background: #fff; border-radius: 999px; padding: 7px 16px; cursor: pointer; white-space: nowrap; min-height: 36px; }
.pill.brand { border-color: var(--brand); color: var(--brand-dark); }
.pill:disabled { opacity: .5; }
.ol-foot { text-align: center; padding: 10px 0 6px; }
@media (max-width: 380px) { .ol-thumbs img, .ol-thumbs .ph { width: 64px; height: 64px; } .pill { padding: 7px 12px; } }
@media (min-width: 768px) { .ol-bar { padding-top: 16px; } .ol-list { gap: 12px; } }
/* a computer shows two orders side by side */
@media (min-width: 1024px) {
  .ol-bar, .ol-tabs, .ol-list { max-width: 1100px; }
  .ol-list { grid-template-columns: repeat(2, minmax(0, 1fr)); align-items: start; }
  .ol-note, .ol-foot { grid-column: 1 / -1; }
  .ol-search { max-width: 420px; } .ol-bar { justify-content: space-between; } .ol-bar .ol-search { margin-right: auto; }
}
</style>
