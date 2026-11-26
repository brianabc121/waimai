<script setup lang="ts">
// The left column of the customer's pages on a computer: who is logged in,
// and every page of "me" in one list, the current one marked. A phone and an
// iPad do without it (they reach the pages from "me").
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { t } from '../../i18n'
import { avatarUrl, displayName, me } from './me'
import { onOwnSite, ownSite, ownSlug } from '../../site'

const route = useRoute()
interface Link { to: string; key: string; icon: string; dot?: boolean }
// a store's own page keeps the pages of that store; its messages are the chat with it
const groups = computed(() => allGroups.value
  .map(g => ({ ...g, links: g.links.map(l => l.to === '/me/messages' && ownSite() ? { ...l, to: `/chat/${ownSlug()}` } : l).filter(l => onOwnSite(l.to)) }))
  .filter(g => g.links.length))
const allGroups = computed<{ key: string; links: Link[] }[]>(() => [
  { key: '', links: [
    { to: '/me', key: 'cn.center', icon: 'M4 11 12 4l8 7v8a1 1 0 0 1-1 1h-4v-6h-6v6H5a1 1 0 0 1-1-1z' },
    { to: '/orders', key: 'cn.orders', icon: 'M6.5 4.5h11a1.5 1.5 0 0 1 1.5 1.500v13a1.5 1.5 0 0 1-1.500 1.500h-11A1.5 1.5 0 0 1 5 19V6a1.5 1.5 0 0 1 1.500-1.500zM9 9.500h6M9 13.500h4' },
  ] },
  { key: 'cn.g_wallet', links: [
    { to: '/me/coupons', key: 'mo.title', icon: 'M3.5 6.500h17v3a2.5 2.5 0 0 0 0 5v3h-17v-3a2.5 2.5 0 0 0 0-5zM9.5 7v10' },
    { to: '/me/wallet/balance', key: 'me.balance', icon: 'M4 7.500A2.5 2.5 0 0 1 6.500 5H18v3M4 7.500V17a2 2 0 0 0 2 2h14V8H6.500A2.5 2.5 0 0 1 4 7.500zM16 13.500h.01' },
    { to: '/me/wallet/bonus', key: 'me.bonus', icon: 'M12 3.500l2.500 5 5.500.8-4 3.900.9 5.500L12 16.100l-4.900 2.600.9-5.500-4-3.900 5.500-.8z' },
  ] },
  { key: 'cn.g_daily', links: [
    { to: '/me/addresses', key: 'me.addresses', icon: 'M12 13a2.6 2.6 0 1 0 0-5.2 2.6 2.6 0 0 0 0 5.2zM12 21s6.5-5.8 6.5-10.6a6.5 6.5 0 0 0-13 0C5.5 15.2 12 21 12 21z' },
    { to: '/me/favorites', key: 'me.favorites', icon: 'M12 20s-7.5-4.6-7.5-10.1A4.3 4.3 0 0 1 12 7.2a4.3 4.3 0 0 1 7.5 2.7C19.5 15.4 12 20 12 20z' },
    { to: '/me/messages', key: 'me.messages', dot: !!me.profile && me.profile.unread + me.profile.chat_unread > 0, icon: 'M4.5 6.5A2.5 2.5 0 0 1 7 4h10a2.5 2.5 0 0 1 2.5 2.5v7A2.5 2.5 0 0 1 17 16h-5l-4.5 4v-4H7a2.5 2.5 0 0 1-2.5-2.5zM8.5 10h.01M12 10h.01M15.5 10h.01' },
    { to: '/me/support', key: 'me.support', icon: 'M5 13v-1a7 7 0 0 1 14 0v1M5 13h2.5v5H6a1 1 0 0 1-1-1zM19 13h-2.5v5H18a1 1 0 0 0 1-1zM16.5 18c0 1.7-1.5 2.5-3.5 2.5' },
  ] },
  { key: 'cn.g_more', links: [
    { to: '/me/reviews', key: 'me.reviews', icon: 'M19 12v5.5a2 2 0 0 1-2 2H6.5a2 2 0 0 1-2-2v-11a2 2 0 0 1 2-2H14M8 10h4M8 14h3M14.5 13.5l6-6-2-2-6 6-.5 2.5z' },
    { to: '/me/feedback', key: 'me.feedback', icon: 'M4 6.5h16v11H4zM4.5 7l7.5 6 7.5-6' },
    { to: '/me/help', key: 'me.help', icon: 'M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18zM9.5 9.5a2.5 2.5 0 1 1 3.7 2.2c-.8.5-1.2 1-1.2 1.9M12 16.8h.01' },
    { to: '/me/about', key: 'me.about', icon: 'M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18zM12 11v5.5M12 7.8h.01' },
    { to: '/me/apply', key: 'me.apply', icon: 'M5 10.5V19h14v-8.5M3.5 7 5 4h14l1.5 3v1a2.2 2.2 0 0 1-4.25.8 2.2 2.2 0 0 1-4.25 0 2.2 2.2 0 0 1-4.25 0A2.2 2.2 0 0 1 3.5 8zM9.5 19v-4.5h5V19' },
    { to: '/me/profile', key: 'me.settings', icon: 'M12 8.5a3.5 3.5 0 1 1 0 7 3.5 3.5 0 0 1 0-7zM12 2.5v3M12 18.5v3M2.5 12h3M18.5 12h3M5.3 5.3l2.1 2.1M16.6 16.6l2.1 2.1M5.3 18.7l2.1-2.1M16.6 7.4l2.1-2.1' },
  ] },
])
// /orders lights "my orders"; /me lights only the overview, not every page under it
const on = (to: string) => to === '/me' ? route.path === '/me' : route.path === to || route.path.startsWith(to + '/')
</script>

<template>
  <aside class="mside" data-testid="me-side">
    <router-link :to="me.profile ? '/me/profile' : '/account/login?next=' + encodeURIComponent(route.fullPath)" class="who">
      <img v-if="me.profile && avatarUrl(me.profile)" :src="avatarUrl(me.profile)" alt="" class="ava" />
      <span v-else class="ava ph"><svg viewBox="0 0 24 24" width="26" height="26" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"><circle cx="12" cy="9" r="4" /><path d="M4.5 20.5c1.2-3.7 4-5.5 7.5-5.5s6.3 1.8 7.5 5.5z" stroke-linejoin="round" /></svg></span>
      <span class="nm">
        <b>{{ me.profile ? displayName(me.profile) : t('me.login') }}</b>
        <small>{{ me.profile ? t('me.member_no', { no: me.profile.member_no }) : t('me.login_hint') }}</small>
      </span>
    </router-link>
    <nav v-for="g in groups" :key="g.key">
      <h3 v-if="g.key">{{ t(g.key) }}</h3>
      <router-link v-for="l in g.links" :key="l.to" :to="l.to" :class="{ on: on(l.to) }">
        <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path :d="l.icon" /></svg>
        <span>{{ t(l.key) }}</span>
        <i v-if="l.dot" class="dot"></i>
      </router-link>
    </nav>
  </aside>
</template>

<style scoped>
.mside { display: none; }
@media (min-width: 1024px) {
  .mside { display: grid; gap: 4px; align-content: start; position: sticky; top: 16px; background: var(--paper); border-radius: 14px; padding: 12px 10px; box-shadow: 0 3px 12px rgba(90, 40, 15, .08); font-size: 14px; }
  .who { display: flex; align-items: center; gap: 10px; padding: 6px 8px 14px; margin-bottom: 4px; border-bottom: 1px solid var(--line); color: var(--ink); }
  .ava { width: 44px; height: 44px; border-radius: 50%; object-fit: cover; flex: none; background: var(--gold-soft); }
  .ava.ph { display: grid; place-items: center; color: var(--red); }
  .nm { display: grid; min-width: 0; }
  .nm b { font-size: 15px; font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .nm small { font-size: 12px; color: var(--muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  nav { display: grid; gap: 1px; }
  .who + nav { margin-top: 6px; }
  h3 { font-size: 12px; font-weight: 500; color: var(--muted); padding: 10px 10px 4px; }
  nav a { display: flex; align-items: center; gap: 10px; padding: 8px 10px; border-radius: 9px; color: var(--ink); }
  nav a svg { color: var(--red); flex: none; opacity: .85; }
  nav a:hover { background: var(--bg); }
  nav a.on { background: var(--gold-soft); color: var(--red); font-weight: 600; }
  nav a.on svg { opacity: 1; }
  nav a span { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .dot { width: 8px; height: 8px; border-radius: 50%; background: #e5352b; flex: none; }
}
</style>
