<script setup lang="ts">
import './strings'
import { useRoute, useRouter } from 'vue-router'
import { computed } from 'vue'
import { locale, setLocale, t } from '../../i18n'
import { logout, session } from '../../session'

const router = useRouter()
const route = useRoute()
const title = computed(() => t(String(route.meta.title ?? 'ad.title')))
async function out() { await logout(); session.loaded = true; router.replace('/login') }
</script>

<template>
  <div class="adm">
    <nav class="adm-side">
      <div class="brand">{{ t('ad.product') }}<small>{{ t('ad.title') }}</small></div>
      <div class="grp">{{ t('ad.g_merchants') }}</div>
      <router-link to="/admin/merchants">{{ t('ad.merchants') }}</router-link>
      <router-link to="/admin/items">{{ t('ad.items') }}</router-link>
      <div class="grp">{{ t('ad.g_system') }}</div>
      <router-link to="/admin/audit">{{ t('ad.audit') }}</router-link>
    </nav>
    <div class="adm-main">
      <div class="adm-top">
        <h1>{{ title }}</h1><span class="grow"></span>
        <button class="lang" @click="setLocale(locale === 'zh' ? 'en' : 'zh')">{{ locale === 'zh' ? 'EN' : '中文' }}</button>
        <span>{{ session.user?.display_name }}</span>
        <button class="btn link" @click="out">{{ t('sign_out') }}</button>
      </div>
      <div v-if="!session.user?.is_platform_admin" class="adm-page"><div class="banner bad">{{ t('not_found') }}</div></div>
      <router-view v-else />
    </div>
  </div>
</template>
