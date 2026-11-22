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
        <svg viewBox="0 0 24 24" width="24" height="24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M4 11 12 4l8 7v8a1 1 0 0 1-1 1h-4v-6h-6v6H5a1 1 0 0 1-1-1z" /></svg>
        <span>{{ t('me.home') }}</span>
      </router-link>
      <router-link to="/orders" :class="{ on: active === 'orders' }" data-testid="tab-orders">
        <svg viewBox="0 0 24 24" width="24" height="24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect x="5" y="4.5" width="14" height="16" rx="2.5" /><path d="M9 3v3M15 3v3M9 11h6M9 15h4" /></svg>
        <span>{{ t('me.orders') }}</span>
      </router-link>
      <router-link to="/me" :class="{ on: active === 'me' }" data-testid="tab-me">
        <span class="ic"><svg viewBox="0 0 24 24" width="24" height="24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><circle cx="12" cy="8.5" r="3.8" /><path d="M4.5 20c1.2-3.6 4-5.4 7.5-5.4s6.3 1.8 7.5 5.4" /></svg>
          <i v-if="me.profile?.unread" class="dot" data-testid="tab-unread"></i></span>
        <span>{{ t('me.me') }}</span>
      </router-link>
    </div>
  </nav>
</template>

<style scoped>
.ctabs { position: fixed; left: 0; right: 0; bottom: 0; z-index: 25; pointer-events: none; }
.ctabs > div { pointer-events: auto; max-width: 520px; margin: 0 auto; height: calc(var(--tabbar-h) + env(safe-area-inset-bottom)); padding-bottom: env(safe-area-inset-bottom);
  display: grid; grid-template-columns: repeat(3, 1fr); background: #fff; border-top: 1px solid var(--line); }
.ctabs a { display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 1px; color: #8b919a; font-size: 12px; }
.ctabs a.on { color: var(--ink); font-weight: 700; }
.ctabs a.on svg { color: var(--brand); }
.ic { position: relative; display: grid; }
.dot { position: absolute; top: -1px; right: -4px; width: 9px; height: 9px; border-radius: 50%; background: #e5352b; border: 1.5px solid #fff; }
</style>
