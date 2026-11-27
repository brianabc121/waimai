<script setup lang="ts">
// "Me": who is logged in, what they hold, and the way into everything that
// belongs to the customer rather than to one store.
import { onOwnSite, ownSite, ownSlug, storePath } from '../../site'
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { get } from '../../api'
import { loginRoute } from '../../customer'
import { currencySymbol, money, pick, t, sn } from '../../i18n'
import { cardStatus, shortStamp } from '../../orders'
import type { OrderCard } from '../../types'
import CustomerFrame from './CustomerFrame.vue'
import CustomerTabs from './CustomerTabs.vue'
import { avatarUrl, displayName, homeStore, loadProfile, me, myOffers, plain, type StoreOffers } from './me'

const router = useRouter()
const ready = ref(false)
const offers = ref<StoreOffers[]>([])
const toast = ref('')
const home = homeStore()

const profile = computed(() => me.profile)
// One wallet is shown: the one in the currency of the store "home" leads to.
const wallet = computed(() => {
  const ws = profile.value?.wallets ?? []
  return ws.find(w => w.currency === home?.currency) ?? ws[0] ?? { currency: home?.currency ?? 'PHP', balance: 0, bonus: 0 }
})
const symbol = computed(() => currencySymbol(wallet.value.currency))
const offerCount = computed(() => offers.value.reduce((n, o) => n + o.promotions.length, 0))
// A computer has room for the latest orders; on a phone they are one tap away at the bottom.
const wide = window.matchMedia('(min-width: 1024px)').matches
const recent = ref<OrderCard[] | null>(null)

onMounted(async () => {
  const p = await loadProfile()
  ready.value = true
  if (!p) return
  if (wide) void get<{ orders: OrderCard[] }>('/customer/orders?tab=all').then(r => { recent.value = r.orders.slice(0, 4) }).catch(() => { recent.value = [] })
  offers.value = await myOffers(home?.slug).catch(() => [])
})

// Everything here belongs to an account: without one, the way leads through login.
function go(path: string, open = false) {
  if (!profile.value && !open) void router.push(loginRoute(path))
  else void router.push(path)
}
function addresses() { go('/me/addresses') }
async function share() {
  const url = ownSite() ? location.origin : location.origin + (home ? storePath(home.slug) : '/me')
  try {
    if (navigator.share) await navigator.share({ title: home?.name ?? document.title, url })
    else { await navigator.clipboard.writeText(url); toast.value = t('me.link_copied'); setTimeout(() => { toast.value = '' }, 2000) }
  } catch { /* closed without sharing */ }
}

interface Entry { key: string; to?: string; open?: boolean; dot?: boolean; icon: string }
// On a store's own page only that store's pages are there: its coupons, the
// addresses, the chat with it and the profile.
const mine = (es: Entry[]) => es.map(e => e.to === '/me/messages' && ownSite() ? { ...e, to: `/chat/${ownSlug()}` } : e)
  .filter(e => !e.to || onOwnSite(e.to))
// what is used most, as large icons
const common = computed<Entry[]>(() => mine(allCommon.value))
const more = computed<Entry[]>(() => mine(allMore.value))
const allCommon = computed<Entry[]>(() => [
  { key: 'addresses', icon: 'M12 13a2.6 2.6 0 1 0 0-5.2 2.6 2.6 0 0 0 0 5.2zM12 21s6.5-5.8 6.5-10.6a6.5 6.5 0 0 0-13 0C5.5 15.2 12 21 12 21z' },
  { key: 'favorites', to: '/me/favorites', icon: 'M12 20s-7.5-4.6-7.5-10.100A4.3 4.3 0 0 1 12 7.200a4.3 4.3 0 0 1 7.5 2.700C19.5 15.4 12 20 12 20z' },
  { key: 'messages', to: '/me/messages', dot: !!profile.value && profile.value.unread + profile.value.chat_unread > 0, icon: 'M4.5 6.500A2.5 2.5 0 0 1 7 4h10a2.5 2.5 0 0 1 2.5 2.500v7A2.5 2.5 0 0 1 17 16h-5l-4.5 4v-4H7a2.5 2.5 0 0 1-2.5-2.500zM8.5 10h.01M12 10h.01M15.5 10h.01' },
  { key: 'support', to: '/me/support', open: true, icon: 'M5 13v-1a7 7 0 0 1 14 0v1M5 13h2.500v5H6a1 1 0 0 1-1-1zM19 13h-2.500v5H18a1 1 0 0 0 1-1zM16.5 18c0 1.7-1.5 2.5-3.5 2.5' },
])
// the rest, as rows
const allMore = computed<Entry[]>(() => [
  { key: 'store_credit', to: '/me/store-credit', icon: 'M4 8.500h16V19H4zM4 8.500 6 5h12l2 3.500M15.500 13.500h2' },
  { key: 'reviews', to: '/me/reviews', icon: 'M19 12v5.500a2 2 0 0 1-2 2H6.500a2 2 0 0 1-2-2v-11a2 2 0 0 1 2-2H14M8 10h4M8 14h3M14.5 13.500l6-6-2-2-6 6-.5 2.500z' },
  { key: 'feedback', to: '/me/feedback', icon: 'M4 6.500h16v11H4zM4.5 7l7.5 6 7.5-6' },
  { key: 'help', to: '/me/help', open: true, icon: 'M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18zM9.5 9.500a2.5 2.5 0 1 1 3.7 2.200c-.8.5-1.2 1-1.2 1.900M12 16.800h.01' },
  { key: 'about', to: '/me/about', open: true, icon: 'M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18zM12 11v5.500M12 7.800h.01' },
  { key: 'apply', to: '/me/apply', icon: 'M5 10.500V19h14v-8.500M3.5 7 5 4h14l1.5 3v1a2.2 2.2 0 0 1-4.250.8 2.2 2.2 0 0 1-4.250 0 2.2 2.2 0 0 1-4.250 0A2.2 2.2 0 0 1 3.5 8zM9.5 19v-4.500h5V19' },
])
</script>

<template>
  <div class="phone mehome warm" :data-ready="ready || null">
   <CustomerFrame active="me">
    <header class="hero">
      <div class="tools">
        <button :aria-label="t('me.settings')" data-testid="me-settings" @click="go('/me/profile')">
          <svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3" /><path d="M12 3.500l1.5 2.2 2.6-.5 1 2.5 2.5 1-.5 2.6 2.2 1.5-2.2 1.5.5 2.6-2.5 1-1 2.5-2.6-.500L12 21.500l-1.5-2.2-2.6.5-1-2.5-2.5-1 .5-2.600L2.5 12.200l2.2-1.5-.5-2.6 2.5-1 1-2.5 2.6.500z" transform="translate(-.3 -.5)" /></svg></button>
        <button :aria-label="t('me.share')" data-testid="me-share" @click="share">
          <svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round"><circle cx="18" cy="5" r="2.6" /><circle cx="6" cy="12" r="2.6" /><circle cx="18" cy="19" r="2.6" /><path d="m8.3 10.7 7.4-4.400M8.3 13.300l7.4 4.4" /></svg></button>
      </div>
      <button class="who" data-testid="me-who" @click="go('/me/profile')">
        <img v-if="profile && avatarUrl(profile)" :src="avatarUrl(profile)" alt="" class="avatar" data-testid="me-avatar" />
        <span v-else class="avatar ph"><svg viewBox="0 0 24 24" width="30" height="30" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"><circle cx="12" cy="9" r="4" /><path d="M4.5 20.500c1.2-3.7 4-5.5 7.5-5.500s6.3 1.8 7.5 5.500z" stroke-linejoin="round" /></svg></span>
        <span v-if="profile" class="names">
          <b data-testid="me-name">{{ displayName(profile) }}</b>
          <span data-testid="me-member">{{ t('me.member_no', { no: profile.member_no }) }}</span>
        </span>
        <span v-else class="names">
          <b data-testid="me-login">{{ t('me.login') }}</b>
          <span>{{ t('me.login_hint') }}</span>
        </span>
        <svg class="chev" viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="m9 5 7 7-7 7" /></svg>
      </button>
    </header>

    <div class="body">
      <div class="stats" :class="{ one: ownSite() }">
        <button v-if="!ownSite()" data-testid="me-balance" @click="go('/me/wallet/balance')"><b>{{ symbol }}{{ plain(wallet.balance) }}</b><span>{{ t('me.balance') }}</span></button>
        <button class="hot" data-testid="me-coupons" @click="go('/me/coupons')"><b>{{ offerCount }}</b><span>{{ t('me.coupons') }}</span></button>
        <button v-if="!ownSite()" data-testid="me-bonus" @click="go('/me/wallet/bonus')"><b>{{ symbol }}{{ plain(wallet.bonus) }}</b><span>{{ t('me.bonus') }}</span></button>
      </div>

      <section>
        <h2>{{ t('me.common') }}</h2>
        <div class="panel common">
          <button v-for="e in common" :key="e.key" :data-testid="'me-' + e.key" @click="e.to ? go(e.to, e.open) : addresses()">
            <span class="ic"><svg viewBox="0 0 24 24" width="26" height="26" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path :d="e.icon" /></svg>
              <i v-if="e.dot" class="dot" data-testid="me-unread"></i></span>
            <span>{{ t('me.' + e.key) }}</span>
          </button>
        </div>
      </section>

      <section v-if="wide && profile" class="recent" data-testid="me-recent">
        <div class="row between"><h2>{{ t('cn.recent') }}</h2>
          <router-link to="/orders" class="more">{{ t('cn.all_orders') }}
            <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><path d="m9 5 7 7-7 7" /></svg></router-link></div>
        <div v-if="recent && !recent.length" class="none">{{ t('cn.no_orders') }}</div>
        <div v-else-if="recent" class="panel olist">
          <router-link v-for="o in recent" :key="o.token" :to="`/o/${o.token}`" class="orow">
            <img v-if="o.items[0]?.image_url" :src="o.items[0].image_url" alt="" />
            <span v-else class="ph">{{ sn(o.store).slice(0, 1) }}</span>
            <span class="what"><b>{{ sn(o.store) }}</b>
              <small>{{ o.items.map(i => pick(i.name, i.name_en) + (i.qty > 1 ? ' ×' + i.qty : '')).join('、') }}</small></span>
            <span class="when">{{ shortStamp(o.created_at, o.timezone) }}</span>
            <span class="st">{{ cardStatus(o) }}</span>
            <b class="amt">{{ money(o.total, o.currency) }}</b>
          </router-link>
        </div>
      </section>

      <section v-if="more.length" class="moresvc">
        <h2>{{ t('me.more_services') }}</h2>
        <div class="panel rows">
          <button v-for="e in more" :key="e.key" :data-testid="'me-' + e.key" @click="go(e.to!, e.open)">
            <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path :d="e.icon" /></svg>
            <span>{{ t('me.' + e.key) }}</span>
            <svg class="chev" viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="m9 5 7 7-7 7" /></svg>
          </button>
        </div>
      </section>
    </div>

    <div v-if="toast" class="me-toast">{{ toast }}</div>
   </CustomerFrame>
    <CustomerTabs active="me" />
  </div>
</template>

<style scoped>
.mehome { font-size: 14px; padding-bottom: calc(var(--tabbar-h) + env(safe-area-inset-bottom) + 24px); }
.hero { padding: 6px 16px 66px; color: #fff; border-radius: 0 0 50% 50% / 0 0 26px 26px;
  background: radial-gradient(90% 70% at 70% 0%, rgba(255, 160, 120, .28) 0%, transparent 60%), var(--motif) 0 0 / 56px 56px, linear-gradient(180deg, var(--red-deep) 0%, #b23a2a 100%); }
.tools { display: flex; align-items: center; justify-content: flex-end; gap: 2px; }
.tools button { width: 38px; height: 38px; border: 0; background: none; border-radius: 50%; display: grid; place-items: center; cursor: pointer; color: #fff; }
.who { display: flex; align-items: center; gap: 12px; width: 100%; border: 0; background: none; color: #fff; padding: 8px 2px 0; text-align: start; cursor: pointer; }
.avatar { width: 52px; height: 52px; border-radius: 50%; object-fit: cover; background: #fbeed3; flex: none; }
.avatar.ph { display: grid; place-items: center; color: var(--ink); }
.names { display: grid; gap: 1px; min-width: 0; flex: 1; }
.names b { font-size: 18px; font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.names span { font-size: 12px; opacity: .85; }
.who .chev { flex: none; }

.body { margin-top: -48px; padding: 0 12px; display: grid; gap: 16px; }
.stats { display: grid; grid-template-columns: repeat(3, 1fr); padding: 12px 0; background: var(--paper); border-radius: 14px; box-shadow: 0 6px 18px rgba(90, 40, 15, .12); margin: 0 -2px; }
.stats button { border: 0; background: none; display: grid; gap: 2px; justify-items: center; cursor: pointer; color: var(--ink); border-inline-start: 1px solid var(--line); padding: 2px 4px; min-width: 0; }
.stats button:first-child { border-inline-start: 0; }
.stats.one { grid-template-columns: 1fr; }
.stats b { font-size: 18px; font-weight: 600; line-height: 1.15; font-variant-numeric: tabular-nums; }
.stats .hot b { color: var(--red); }
.stats span { font-size: 12px; color: var(--muted); }
section { display: grid; gap: 8px; min-width: 0; }
h2 { font-size: 15px; font-weight: 600; }
.more { border: 0; background: none; display: flex; align-items: center; gap: 1px; font-size: 12px; color: var(--muted); cursor: pointer; padding: 0; }
.none { text-align: center; color: var(--muted); font-size: 12px; padding: 18px 0; background: var(--paper); border: 1px solid var(--line); border-radius: 14px; }
.panel { background: var(--paper); border-radius: 14px; }
.common { display: grid; grid-template-columns: repeat(4, 1fr); padding: 8px 4px; }
.common button { border: 0; background: none; border-radius: 12px; display: grid; gap: 4px; justify-items: center; cursor: pointer; font-size: 12px; color: var(--ink); padding: 8px 0; min-width: 0; }
.common button:hover, .common button:active { background: var(--gold-soft); }
.common .ic { position: relative; display: grid; color: var(--red); }
.dot { position: absolute; top: -1px; inset-inline-end: -3px; width: 10px; height: 10px; border-radius: 50%; background: #e5352b; border: 1.500px solid var(--paper); }
.rows { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); padding: 2px 12px; }
.rows button { border: 0; background: none; display: flex; align-items: center; gap: 8px; min-height: 46px; padding: 0 4px 0 6px; cursor: pointer; color: var(--ink); font-size: 14px; text-align: start; min-width: 0; border-top: 1px solid var(--line); }
.rows button:nth-child(-n+2) { border-top: 0; }
.rows button:nth-child(even) { box-shadow: -1px 0 0 var(--line); padding-inline-start: 14px; }
.rows button:last-child:nth-child(odd) { grid-column: 1 / -1; }
.rows button > svg:first-child { color: var(--red); flex: none; }
.rows button span { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.rows .chev { flex: none; color: #b9a793; }
@media (max-width: 370px) { .rows button { font-size: 13.5px; gap: 6px; padding-inline-start: 4px; } .rows button:nth-child(even) { padding-inline-start: 10px; } }
/* an iPad or a computer: the red becomes a card under the top bar */
@media (min-width: 768px) {
  .mehome.phone { max-width: none; padding-bottom: 40px; }
  .hero { border-radius: 16px; padding: 16px 22px 64px; display: flex; flex-direction: row-reverse; align-items: center; gap: 12px; }
  .who { padding: 0; flex: 1; min-width: 0; }
  .who .chev { display: none; }
  .avatar { width: 60px; height: 60px; }
  .names b { font-size: 20px; }
  .body { padding: 0 16px; gap: 20px; }
  .stats { padding: 16px 0; }
  .stats b { font-size: 20px; }
  .common { padding: 10px 8px; }
  .common button { font-size: 13px; padding: 12px 0; }
  .rows { grid-template-columns: repeat(3, minmax(0, 1fr)); padding: 4px 16px; }
  .rows button:nth-child(-n+3) { border-top: 0; }
  .rows button:nth-child(even) { box-shadow: none; padding-inline-start: 6px; }
  .rows button:not(:nth-child(3n+1)) { box-shadow: -1px 0 0 var(--line); padding-inline-start: 14px; }
  .rows button:last-child:nth-child(odd) { grid-column: auto; }
  .me-toast { bottom: 40px; }
}
/* a computer: the side list has every page, so the rows give way to the latest orders */
@media (min-width: 1024px) {
  .moresvc { display: none; }
  .olist { padding: 2px 16px; }
  .orow { display: grid; grid-template-columns: 44px minmax(0, 1fr) auto 96px 90px; align-items: center; gap: 14px; padding: 10px 0; border-top: 1px solid var(--line); color: var(--ink); }
  .orow:first-child { border-top: 0; }
  .orow:hover .what b { color: var(--red); }
  .orow img, .orow .ph { width: 44px; height: 44px; border-radius: 8px; object-fit: cover; }
  .orow .ph { background: #c2401c; color: #fff3da; display: grid; place-items: center; font-weight: 600; }
  .what { display: grid; min-width: 0; }
  .what b { font-size: 14px; font-weight: 600; }
  .what small, .when { font-size: 12px; color: var(--muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .st { justify-self: center; font-size: 12px; padding: 1px 8px; border-radius: 999px; background: var(--gold-soft); color: var(--red); white-space: nowrap; }
  .amt { justify-self: end; font-size: 15px; font-weight: 600; font-variant-numeric: tabular-nums; }
}
</style>
