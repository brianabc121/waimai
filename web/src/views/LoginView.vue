<script setup lang="ts">
// The one sign-in page of the stores: the back office and the staff workbench
// are two tabs of it (docs/STAFF_ACCESS_DESIGN.md §5.1). The platform signs in
// at /admin/login.
import LangButton from './LangButton.vue'
import './loginStrings'
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ApiError } from '../api'
import { t } from '../i18n'
import { ENTRY_KEY, login, officeRoles, session, setBranch } from '../session'
import { native } from './work/native'

const router = useRouter()
const route = useRoute()
const next = typeof route.query.next === 'string' && route.query.next.startsWith('/') ? route.query.next : ''
const toWork = (p: string) => p === '/work' || p.startsWith('/work/')
// The app is the workbench; in a browser the tab last used on this device.
const saved = (() => { try { return localStorage.getItem(ENTRY_KEY) } catch { return null } })()
const entry = ref<'office' | 'work'>(native || toWork(next) || (saved === 'work' && !next) ? 'work' : 'office')
const username = ref('')
const password = ref('')
const error = ref('')
const busy = ref(false)
const staffOnly = ref(false)

async function submit() {
  busy.value = true; error.value = ''
  try {
    await login(username.value, password.value, entry.value)
    const u = session.user!
    if (u.entry === 'work') {
      if (entry.value === 'office') { staffOnly.value = true; return }
      router.replace(next && toWork(next) ? next : '/work')
      return
    }
    const roles = officeRoles(u)
    if (roles.length > 1) { router.replace({ path: '/login/pick', query: next ? { next } : {} }); return }
    setBranch(roles[0].branch_id)
    router.replace(next && !toWork(next) ? next : '/merchant/orders')
  } catch (e) {
    error.value = e instanceof ApiError && e.code === 'BAD_CREDENTIALS' ? t('bad_login') : (e as Error).message
  } finally { busy.value = false }
}
</script>

<template>
  <div class="phone login">
    <div v-if="staffOnly" class="section stack loginbox" data-testid="login-staff-only">
      <div class="banner warn">{{ t('lg.staff_only') }}</div>
      <button class="btn primary block big" data-testid="login-go-work" @click="router.replace('/work')">{{ t('lg.go_work') }}</button>
    </div>
    <form v-else class="section stack" @submit.prevent="submit">
      <div class="row between"><h1>{{ t('sign_in') }}</h1>
        <LangButton class="lang" /></div>
      <div v-if="!native" class="logintabs" role="tablist">
        <button type="button" role="tab" :aria-selected="entry === 'office'" :class="{ on: entry === 'office' }" data-testid="login-tab-office" @click="entry = 'office'">{{ t('lg.office') }}</button>
        <button type="button" role="tab" :aria-selected="entry === 'work'" :class="{ on: entry === 'work' }" data-testid="login-tab-work" @click="entry = 'work'">{{ t('lg.work') }}</button>
      </div>
      <div v-if="error" class="banner bad">{{ error }}</div>
      <label class="field"><span>{{ t('username') }}</span><input v-model="username" autocomplete="username" autocapitalize="none" required /></label>
      <label class="field"><span>{{ t('password') }}</span><input v-model="password" type="password" autocomplete="current-password" required /></label>
      <button class="btn primary block big" :disabled="busy">{{ t('sign_in') }}</button>
      <p v-if="!native" class="muted small" style="margin:4px 0 0">{{ t('lg.hint') }}</p>
    </form>
  </div>
</template>

<style scoped>
.logintabs { display: flex; background: #f1f2f4; border-radius: 10px; padding: 3px; gap: 3px; }
.logintabs button { flex: 1; border: 0; background: transparent; padding: 9px 6px; border-radius: 8px; font-weight: 600; color: var(--muted); cursor: pointer; font-size: 14px; }
.logintabs button.on { background: #fff; color: var(--text, #1f2328); box-shadow: 0 1px 3px rgba(0,0,0,.12); }
@media (min-width: 768px) { .loginbox { background: #fff; border: 1px solid var(--line); border-radius: 14px; padding: 28px; } }
</style>
