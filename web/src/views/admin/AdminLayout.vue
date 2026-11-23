<script setup lang="ts">
import './strings'
import '../../bills'
import { useRoute, useRouter } from 'vue-router'
import { computed, onMounted, onUnmounted } from 'vue'
import { locale, setLocale, t } from '../../i18n'
import { logout, session } from '../../session'
import { loadSupportUnread, loadWorkerStatus, monitor, poll } from './monitor'
import { useShell } from '../../shell'

const router = useRouter()
const route = useRoute()
const shell = useShell('admin')
const title = computed(() => t(String(route.meta.title ?? 'ad.title')))
// the failed-job badge stays current on every page; errors here are shown by the page itself
// and so does the number of support messages nobody has read
let stop = () => {}, stopSupport = () => {}
onMounted(() => {
  if (!session.user?.is_platform_admin) return
  stop = poll(() => loadWorkerStatus().catch(() => {}), 60)
  stopSupport = poll(() => loadSupportUnread().catch(() => {}), 20)
})
onUnmounted(() => { stop(); stopSupport() })
async function out() { await logout(); session.loaded = true; router.replace('/login') }
</script>

<template>
  <div class="adm" :class="{ drawer: shell.drawer.value, open: shell.open.value }">
    <div class="shell-mask" @click="shell.open.value = false"></div>
    <nav class="adm-side">
      <div class="brand row between"><span>{{ t('ad.product') }}<small>{{ t('ad.title') }}</small></span>
        <button v-if="!shell.narrow.value" class="shell-fold" :title="t('nav_fold')" :aria-label="t('nav_fold')" @click="shell.fold()">«</button>
        <button v-else class="shell-fold" :aria-label="t('close')" @click="shell.open.value = false">×</button></div>
      <div class="grp">{{ t('ad.g_monitor') }}</div>
      <router-link to="/admin/dashboard">{{ t('ad.dashboard') }}</router-link>
      <router-link to="/admin/api">{{ t('ad.api') }}</router-link>
      <router-link to="/admin/worker">{{ t('ad.worker') }}<span v-if="monitor.failedJobs" class="badge" data-testid="failed-badge">{{ monitor.failedJobs }}</span></router-link>
      <div class="grp">{{ t('ad.g_merchants') }}</div>
      <router-link to="/admin/merchants">{{ t('ad.merchants') }}</router-link>
      <router-link to="/admin/items">{{ t('ad.items') }}</router-link>
      <router-link to="/admin/customers">{{ t('ad.customers') }}</router-link>
      <router-link to="/admin/mall">{{ t('ad.mall') }}</router-link>
      <router-link to="/admin/coupons" data-testid="nav-coupons">{{ t('ad.coupons') }}</router-link>
      <router-link to="/admin/bills" data-testid="nav-bills">{{ t('bl.nav') }}</router-link>
      <router-link to="/admin/support" data-testid="nav-support">{{ t('ad.support') }}<span v-if="monitor.supportUnread" class="badge" data-testid="support-badge">{{ monitor.supportUnread > 99 ? '99+' : monitor.supportUnread }}</span></router-link>
      <router-link to="/admin/chats" data-testid="nav-chats">{{ t('ad.chats') }}</router-link>
      <div class="grp">{{ t('ad.g_system') }}</div>
      <router-link to="/admin/settings">{{ t('ad.settings') }}</router-link>
      <router-link to="/admin/audit">{{ t('ad.audit') }}</router-link>
    </nav>
    <div class="adm-main">
      <div class="adm-top">
        <button class="shell-menu" data-testid="nav-open" :aria-label="t('nav_menu')" @click="shell.show()"><i></i><i></i><i></i></button>
        <h1 class="grow">{{ title }}</h1>
        <button class="lang" @click="setLocale(locale === 'zh' ? 'en' : 'zh')">{{ locale === 'zh' ? 'EN' : '中文' }}</button>
        <span class="who">{{ session.user?.display_name }}</span>
        <button class="btn link" @click="out">{{ t('sign_out') }}</button>
      </div>
      <div v-if="!session.user?.is_platform_admin" class="adm-page"><div class="banner bad">{{ t('not_found') }}</div></div>
      <router-view v-else />
    </div>
  </div>
</template>
