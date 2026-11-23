<script setup lang="ts">
// The customer's bottom navigation: the store, the orders, and "me".
import { computed } from 'vue'
import { t } from '../../i18n'
import { homePath, me } from './me'

defineProps<{ active: 'home' | 'orders' | 'me' }>()
const home = computed(() => homePath())
</script>

<template>
  <nav class="ctabs" data-testid="customer-tabs">
    <div>
      <router-link :to="home" :class="{ on: active === 'home' }" data-testid="tab-home">
        <svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M4 11 12 4l8 7v8a1 1 0 0 1-1 1h-4v-6h-6v6H5a1 1 0 0 1-1-1z" /></svg>
        <span>{{ t('me.home') }}</span>
      </router-link>
      <router-link to="/orders" :class="{ on: active === 'orders' }" data-testid="tab-orders">
        <svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect x="5" y="4.5" width="14" height="16" rx="2.5" /><path d="M9 3v3M15 3v3M9 11h6M9 15h4" /></svg>
        <span>{{ t('me.orders') }}</span>
      </router-link>
      <router-link to="/me" :class="{ on: active === 'me' }" data-testid="tab-me">
        <span class="ic"><svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><circle cx="12" cy="8.5" r="3.8" /><path d="M4.5 20c1.2-3.6 4-5.4 7.5-5.4s6.3 1.8 7.5 5.4" /></svg>
          <i v-if="me.profile?.unread" class="dot" data-testid="tab-unread"></i></span>
        <span>{{ t('me.me') }}</span>
      </router-link>
    </div>
  </nav>
</template>

<style scoped>
.ctabs { position: fixed; inset-inline-start: 0; inset-inline-end: 0; bottom: 0; z-index: 25; pointer-events: none; }
.ctabs > div { pointer-events: auto; max-width: 520px; margin: 0 auto; height: calc(var(--tabbar-h) + env(safe-area-inset-bottom)); padding-bottom: env(safe-area-inset-bottom);
  display: grid; grid-template-columns: repeat(3, 1fr); background: #fff; border-top: 1px solid var(--line); }
.ctabs a { display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 1px; color: #8b919a; font-size: 11px; }
.ctabs a.on { color: var(--ink); font-weight: 600; }
.ctabs a.on svg { color: var(--brand); }
.ic { position: relative; display: grid; }
.dot { position: absolute; top: -1px; inset-inline-end: -4px; width: 9px; height: 9px; border-radius: 50%; background: #e5352b; border: 1.5px solid #fff; }
.ctabs a > span:last-child { line-height: 1.2; }
/* the warm look: cream bar, the current tab on a gold pill */
.warm .ctabs > div { background: var(--bg); border-top-color: var(--line); align-items: center; }
.warm .ctabs a { color: var(--muted); font-size: 11px; justify-self: center; min-width: 64px; height: calc(var(--tabbar-h) - 10px); border-radius: 14px; }
.warm .ctabs a.on { background: var(--gold); color: var(--red); }
.warm .ctabs a.on svg { color: var(--red); }
/* an iPad or a computer has the top bar (CustomerNav) instead */
@media (min-width: 768px) { .ctabs { display: none; } }
</style>
