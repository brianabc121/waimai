<script setup lang="ts">
// The frame of every page under "me": a bar with a way back and the title.
// A page that needs an account sends whoever is not logged in to the login
// page, and shows nothing until the profile is there.
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { loginRoute } from '../../customer'
import { t } from '../../i18n'
import { loadProfile, me } from './me'

const props = withDefaults(defineProps<{ title: string; open?: boolean; back?: string }>(), { open: false, back: '/me' })
const route = useRoute(), router = useRouter()
const ready = ref(props.open)

onMounted(async () => {
  const p = await loadProfile()
  if (!p && !props.open) { await router.replace(loginRoute(route.fullPath)); return }
  ready.value = true
})
function goBack() { if (window.history.state?.back) router.back(); else void router.replace(props.back) }
</script>

<template>
  <div class="phone mepage">
    <header class="mp-bar">
      <button :aria-label="t('back')" data-testid="me-back" @click="goBack">
        <svg viewBox="0 0 24 24" width="24" height="24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m15 5-7 7 7 7" /></svg></button>
      <b>{{ title }}</b>
      <span class="right"><slot name="right" /></span>
    </header>
    <div v-if="ready" class="mp-body" :data-ready="!!me.profile || open"><slot /></div>
    <div v-else class="mp-body muted">{{ t('loading') }}</div>
  </div>
</template>

<style>
.mepage { background: #f4f4f4; padding-bottom: 32px; }
.mp-bar { position: sticky; top: 0; z-index: 10; display: grid; grid-template-columns: 48px 1fr 48px; align-items: center; height: 50px; background: #fff; border-bottom: 1px solid #eee; }
.mp-bar > button { border: 0; background: none; height: 50px; display: grid; place-items: center; cursor: pointer; color: var(--ink); }
.mp-bar > b { text-align: center; font-size: 17px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.mp-bar .right { display: flex; justify-content: flex-end; padding-right: 8px; white-space: nowrap; }
.mp-body { padding: 12px; display: grid; gap: 12px; align-content: start; }
.mepage .card { border: 0; border-radius: 14px; }
.mepage .empty { text-align: center; color: var(--muted); padding: 48px 20px; font-size: 14px; }
.mepage .note { font-size: 13px; color: var(--muted); line-height: 1.6; }
.me-rows { padding: 0 14px; }
.me-rows > * { display: flex; align-items: center; gap: 10px; min-height: 52px; padding: 8px 0; border-bottom: 1px solid #f0f1f3; color: var(--ink); width: 100%; background: none; border-left: 0; border-right: 0; border-top: 0; text-align: left; cursor: pointer; }
.me-rows > *:last-child { border-bottom: 0; }
.me-rows .v { margin-left: auto; color: var(--muted); display: flex; align-items: center; gap: 6px; min-width: 0; text-align: right; }
.me-rows .chev { color: #b8bec7; flex: none; }
.me-logo { width: 46px; height: 46px; border-radius: 10px; object-fit: cover; flex: none; background: var(--brand-soft); color: var(--brand-dark); display: grid; place-items: center; font-size: 20px; font-weight: 700; }
.me-toast { position: fixed; left: 50%; bottom: calc(var(--tabbar-h) + 32px); transform: translateX(-50%); background: rgba(0,0,0,.8); color: #fff; padding: 8px 16px; border-radius: 999px; font-size: 14px; z-index: 90; white-space: nowrap; }
.me-stars i { font-style: normal; color: #d5d8dd; font-size: 16px; } .me-stars i.on { color: #f5a623; }
</style>
