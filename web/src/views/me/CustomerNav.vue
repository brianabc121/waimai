<script setup lang="ts">
// The customer's top bar on an iPad or a computer, in the marketplace's red:
// the way home, to the orders and to "me", the messages and the language.
// A phone has the bottom navigation instead, so it does not show this.
import { computed } from 'vue'
import { locale, setLocale, t } from '../../i18n'
import { avatarUrl, displayName, me } from './me'

defineProps<{ active?: 'home' | 'orders' | 'me' }>()
const unread = computed(() => me.profile ? me.profile.unread + me.profile.chat_unread : 0)
</script>

<template>
  <header class="cnav" data-testid="customer-nav">
    <div class="cnav-in">
      <router-link to="/" class="brand">{{ t('cn.brand') }}</router-link>
      <nav class="links">
        <router-link to="/" :class="{ on: active === 'home' }">{{ t('me.home') }}</router-link>
        <router-link to="/orders" :class="{ on: active === 'orders' }" data-testid="nav-orders">{{ t('cn.orders') }}</router-link>
        <router-link to="/me" :class="{ on: active === 'me' }" data-testid="nav-me">{{ t('cn.center') }}</router-link>
      </nav>
      <span class="grow"></span>
      <button class="lang" @click="setLocale(locale === 'zh' ? 'en' : 'zh')">{{ locale === 'zh' ? 'EN' : '中文' }}</button>
      <router-link to="/me/messages" class="msg" :aria-label="t('me.messages')" data-testid="nav-messages">
        <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M12 4.500c4.7 0 8.5 3.1 8.5 7s-3.8 7-8.5 7c-1 0-2-.150-2.9-.400L5 20l.9-3.300C4.400 15.400 3.500 13.600 3.500 11.500c0-3.900 3.800-7 8.500-7z" /><path d="M8.5 11.500h.01M12 11.500h.01M15.5 11.500h.01" /></svg>
        <i v-if="unread">{{ unread > 99 ? '99+' : unread }}</i>
      </router-link>
      <router-link to="/me" class="avatar" :aria-label="t('me.me')">
        <img v-if="me.profile && avatarUrl(me.profile)" :src="avatarUrl(me.profile)" alt="" />
        <template v-else-if="me.profile?.nickname">{{ displayName(me.profile).slice(0, 1) }}</template>
        <svg v-else viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><circle cx="12" cy="8.5" r="3.8" /><path d="M4.5 20c1.2-3.6 4-5.4 7.5-5.400s6.3 1.8 7.5 5.4" /></svg>
      </router-link>
    </div>
  </header>
</template>

<style scoped>
.cnav { display: none; }
@media (min-width: 768px) {
  .cnav { display: block; background: var(--red-bg); color: #fff; }
  .cnav-in { max-width: 1240px; margin: 0 auto; display: flex; align-items: center; gap: 28px; height: 60px; padding: 0 24px; }
  .brand { font-size: 20px; font-weight: 700; color: #fff3da; white-space: nowrap; letter-spacing: 1px; }
  .links { display: flex; gap: 6px; }
  .links a { color: #fff; opacity: .88; padding: 6px 12px; border-radius: 9px; white-space: nowrap; font-size: 14px; }
  .links a:hover { opacity: 1; background: rgba(255, 255, 255, .12); }
  .links a.on { opacity: 1; background: var(--gold); color: var(--red); font-weight: 600; }
  .grow { flex: 1; }
  .lang { border: 1px solid rgba(255, 255, 255, .7); background: none; color: #fff; border-radius: 6px; padding: 1px 8px; font-size: 12px; cursor: pointer; margin-right: -12px; }
  .msg { position: relative; width: 36px; height: 36px; border-radius: 8px; background: rgba(255, 255, 255, .16); color: #fff; display: grid; place-items: center; }
  .msg i { position: absolute; right: -6px; top: -6px; min-width: 16px; height: 16px; padding: 0 3px; border-radius: 8px; background: var(--gold); color: var(--red); font-style: normal; font-size: 11px; font-weight: 700; display: grid; place-items: center; border: 1.5px solid var(--red); }
  .avatar { width: 36px; height: 36px; border-radius: 50%; background: var(--gold); color: var(--red); font-weight: 700; display: grid; place-items: center; overflow: hidden; margin-left: -12px; }
  .avatar img { width: 100%; height: 100%; object-fit: cover; }
}
@media (min-width: 768px) and (max-width: 1023px) {
  .cnav-in { gap: 16px; padding: 0 20px; }
}
</style>
