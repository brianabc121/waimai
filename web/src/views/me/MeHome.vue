<script setup lang="ts">
// "Me": who is logged in, what they hold, and the way into everything that
// belongs to the customer rather than to one store.
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { loginRoute } from '../../customer'
import { money, pick, promoLabel, t } from '../../i18n'
import AddressSheet from '../AddressSheet.vue'
import CustomerTabs from './CustomerTabs.vue'
import { avatarUrl, displayName, homeStore, loadProfile, me, myOffers, plain, type StoreOffers } from './me'

const router = useRouter()
const ready = ref(false)
const offers = ref<StoreOffers[]>([])
const showAddresses = ref(false)
const toast = ref('')
const home = homeStore()

const profile = computed(() => me.profile)
// One wallet is shown: the one in the currency of the store "home" leads to.
const wallet = computed(() => {
  const ws = profile.value?.wallets ?? []
  return ws.find(w => w.currency === home?.currency) ?? ws[0] ?? { currency: home?.currency ?? 'PHP', balance: 0, bonus: 0 }
})
const symbol = computed(() => money(0, wallet.value.currency).replace(/0$/, '').trim())
const offerCount = computed(() => offers.value.reduce((n, o) => n + o.promotions.length, 0))
const firstOffers = computed(() => offers.value.flatMap(o => o.promotions.map(p => ({ p, o }))).slice(0, 2))

onMounted(async () => {
  const p = await loadProfile()
  ready.value = true
  if (p) offers.value = await myOffers(home?.slug).catch(() => [])
})

// Everything here belongs to an account: without one, the way leads through login.
function go(path: string, open = false) {
  if (!profile.value && !open) void router.push(loginRoute(path))
  else void router.push(path)
}
function addresses() {
  if (!profile.value) void router.push(loginRoute('/me'))
  else showAddresses.value = true
}
async function share() {
  const url = location.origin + (home ? `/s/${home.slug}` : '/me')
  try {
    if (navigator.share) await navigator.share({ title: home?.name ?? document.title, url })
    else { await navigator.clipboard.writeText(url); toast.value = t('me.link_copied'); setTimeout(() => { toast.value = '' }, 2000) }
  } catch { /* closed without sharing */ }
}

const entries = computed(() => [
  { key: 'favorites', to: '/me/favorites', icon: 'M12 3.5l2.6 5.3 5.9.9-4.3 4.1 1 5.8-5.2-2.8-5.2 2.8 1-5.8-4.3-4.1 5.9-.9z' },
  { key: 'support', to: '/me/support', open: true, icon: 'M5 13v-1a7 7 0 0 1 14 0v1M5 13h2.5v5H6a1 1 0 0 1-1-1zM19 13h-2.5v5H18a1 1 0 0 0 1-1zM16.5 18c0 1.7-1.5 2.5-3.5 2.5' },
  { key: 'reviews', to: '/me/reviews', icon: 'M5 20V5h9M5 20h14v-8M9 10h4M9 14h6M15.5 9.5l5-5 1.5 1.5-5 5-2 .5z' },
  { key: 'messages', to: '/me/messages', dot: !!profile.value && profile.value.unread + profile.value.chat_unread > 0, icon: 'M4.5 6.5A2.5 2.5 0 0 1 7 4h10a2.5 2.5 0 0 1 2.5 2.5v7A2.5 2.5 0 0 1 17 16h-5l-4.5 4v-4H7a2.5 2.5 0 0 1-2.5-2.5zM8.5 10h.01M12 10h.01M15.5 10h.01' },
  { key: 'apply', to: '/me/apply', icon: 'M6 21V4M6 5h11l-2 3.5 2 3.5H6M4 21h5' },
  { key: 'addresses', icon: 'M12 13a2.6 2.6 0 1 0 0-5.200001 2.6 2.6 0 0 0 0 5.2zM12 20.5s6-5.4 6-10a6 6 0 0 0-12 0c0 4.6 6 10 6 10z' },
  { key: 'about', to: '/me/about', open: true, icon: 'M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18zM12 11v5.500M12 7.800h.01' },
  { key: 'feedback', to: '/me/feedback', icon: 'M4.5 5.500h15v11h-9l-4 3.500v-3.500h-2zM8.5 9.500h7M8.5 12.500h4.5' },
  { key: 'help', to: '/me/help', open: true, icon: 'M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18zM9.5 9.500a2.5 2.5 0 1 1 3.7 2.200c-.800.5-1.2 1-1.2 1.900M12 16.800h.01' },
])
</script>

<template>
  <div class="phone mehome" :data-ready="ready || null">
    <header class="hero">
      <div class="tools">
        <button :aria-label="t('me.settings')" data-testid="me-settings" @click="go('/me/profile')">
          <svg viewBox="0 0 24 24" width="24" height="24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3" /><path d="M12 3.500l1.5 2.2 2.6-.500 1 2.5 2.5 1-.500 2.6 2.2 1.5-2.2 1.5.5 2.6-2.5 1-1 2.5-2.6-.500L12 21.500l-1.5-2.2-2.6.5-1-2.5-2.5-1 .500-2.600L2.5 12.200l2.2-1.5-.500-2.6 2.5-1 1-2.5 2.6.500z" transform="translate(-.3 -.5)" /></svg></button>
        <button :aria-label="t('me.share')" data-testid="me-share" @click="share">
          <svg viewBox="0 0 24 24" width="24" height="24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><circle cx="18" cy="5" r="2.6" /><circle cx="6" cy="12" r="2.6" /><circle cx="18" cy="19" r="2.6" /><path d="m8.3 10.7 7.4-4.400M8.3 13.300l7.4 4.4" /></svg></button>
      </div>
      <button class="who" data-testid="me-who" @click="go('/me/profile')">
        <img v-if="profile && avatarUrl(profile)" :src="avatarUrl(profile)" alt="" class="avatar" data-testid="me-avatar" />
        <span v-else class="avatar ph"><svg viewBox="0 0 24 24" width="40" height="40" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"><circle cx="12" cy="9" r="4" /><path d="M4 21c1.3-4 4.3-6 8-6s6.7 2 8 6" /></svg></span>
        <span v-if="profile" class="names">
          <b data-testid="me-name">{{ displayName(profile) }}</b>
          <span data-testid="me-member">{{ t('me.member_no', { no: profile.member_no }) }}</span>
        </span>
        <span v-else class="names">
          <b data-testid="me-login">{{ t('me.login') }}</b>
          <span>{{ t('me.login_hint') }}</span>
        </span>
      </button>
    </header>

    <div class="body">
      <button class="strip" data-testid="me-offer-strip" @click="go('/me/coupons')">
        <span class="badge">{{ t('me.offer_strip', { n: offerCount }) }}</span>
        <span class="grow">{{ t('me.offer_strip_hint') }}</span>
        <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="m9 5 7 7-7 7" /></svg>
      </button>

      <div class="card stats">
        <button data-testid="me-balance" @click="go('/me/wallet/balance')"><b>{{ plain(wallet.balance) }}</b><span>{{ t('me.balance') }}({{ symbol }})</span></button>
        <button data-testid="me-coupons" @click="go('/me/coupons')"><b>{{ offerCount }}</b><span>{{ t('me.coupons') }}</span></button>
        <button data-testid="me-bonus" @click="go('/me/wallet/bonus')"><b>{{ plain(wallet.bonus) }}</b><span>{{ t('me.bonus') }}({{ symbol }})</span></button>
      </div>

      <div class="card offers">
        <div class="row between"><h2>{{ t('me.my_offers') }}</h2>
          <button class="more" @click="go('/me/coupons')">{{ t('me.more') }}
            <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="m9 5 7 7-7 7" /></svg></button></div>
        <div v-if="!firstOffers.length" class="none">{{ t('me.no_offers') }}</div>
        <router-link v-for="x in firstOffers" :key="x.p.id" :to="`/s/${x.o.store.slug}`" class="offer">
          <b>{{ promoLabel(x.p, x.o.currency) }}</b>
          <span class="small muted">{{ pick(x.o.store.name, x.o.store.name_en) }}</span>
        </router-link>
      </div>

      <div class="card grid">
        <button v-for="e in entries" :key="e.key" :data-testid="'me-' + e.key" @click="e.to ? go(e.to, e.open) : addresses()">
          <span class="ic"><svg viewBox="0 0 24 24" width="30" height="30" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path :d="e.icon" /></svg>
            <i v-if="e.dot" class="dot" data-testid="me-unread"></i></span>
          <span>{{ t('me.' + e.key) }}</span>
        </button>
      </div>
    </div>

    <AddressSheet v-if="showAddresses" manage :slug="home?.slug" :currency="home?.currency" @close="showAddresses = false" />
    <div v-if="toast" class="me-toast">{{ toast }}</div>
    <CustomerTabs active="me" />
  </div>
</template>

<style scoped>
.mehome { background: #f4f4f4; padding-bottom: calc(var(--tabbar-h) + env(safe-area-inset-bottom) + 20px); }
.hero { padding: 10px 16px 64px; color: var(--ink);
  background: radial-gradient(120% 90% at 0% 0%, var(--brand-soft) 0%, transparent 60%), linear-gradient(180deg, #fff7ef 0%, #f4f4f4 100%); }
.tools { display: flex; justify-content: flex-end; gap: 4px; }
.tools button { width: 42px; height: 42px; border: 0; background: none; border-radius: 50%; display: grid; place-items: center; cursor: pointer; color: var(--ink); }
.who { display: flex; align-items: center; gap: 14px; width: 100%; border: 0; background: none; padding: 8px 6px 0; text-align: left; cursor: pointer; }
.avatar { width: 76px; height: 76px; border-radius: 50%; object-fit: cover; border: 2px solid #fff; background: #fff; flex: none; box-shadow: 0 2px 8px rgba(0, 0, 0, .08); }
.avatar.ph { display: grid; place-items: center; color: #b8bec7; }
.names { display: grid; gap: 4px; min-width: 0; }
.names b { font-size: 21px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.names span { font-size: 13px; font-weight: 600; color: #4b5563; }

.body { margin-top: -50px; padding: 0 12px; display: grid; gap: 12px; }
.strip { display: flex; align-items: center; gap: 10px; width: 100%; border: 0; padding: 10px 14px 24px; margin-bottom: -26px; border-radius: 16px 16px 0 0; cursor: pointer; text-align: left;
  color: #fff; font-size: 14px; background: linear-gradient(90deg, var(--brand) 0%, #f08c3a 60%, #f7c35f 100%); }
.strip .badge { background: #fff; color: var(--brand-dark); font-weight: 700; padding: 2px 10px; border-radius: 6px; white-space: nowrap; }
.strip .grow { text-align: right; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.card { border: 0; border-radius: 16px; position: relative; }
.stats { display: grid; grid-template-columns: repeat(3, 1fr); padding: 18px 6px; }
.stats button { border: 0; background: none; display: grid; gap: 2px; justify-items: center; cursor: pointer; }
.stats b { font-size: 26px; line-height: 1.1; font-variant-numeric: tabular-nums; }
.stats span { color: #5b6470; font-size: 14px; }
.offers h2 { font-size: 18px; }
.more { border: 0; background: none; display: flex; align-items: center; gap: 2px; font-size: 13px; cursor: pointer; }
.none { text-align: center; color: #7d8794; font-size: 13px; padding: 26px 0 20px; }
.offer { display: flex; align-items: baseline; justify-content: space-between; gap: 10px; margin-top: 10px; padding: 10px 12px; border-radius: 10px; background: var(--brand-soft); color: var(--brand-dark); }
.grid { display: grid; grid-template-columns: repeat(4, 1fr); row-gap: 18px; padding: 20px 4px; }
.grid button { border: 0; background: none; display: grid; gap: 6px; justify-items: center; cursor: pointer; font-size: 14px; color: #2b3138; padding: 0; }
.grid .ic { position: relative; display: grid; color: #2b3138; }
.dot { position: absolute; top: -2px; right: -4px; width: 10px; height: 10px; border-radius: 50%; background: #e5352b; border: 1.5px solid #fff; }
@media (min-width: 768px) { .mehome { max-width: 620px; } }
</style>
