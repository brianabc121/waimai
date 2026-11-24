<script setup lang="ts">
// "Choose where to work" (docs/STAFF_ACCESS_DESIGN.md §5.2): each store where
// the person has a back-office role, and the staff workbench. Coming from the
// workbench, the back office asks for the password again (§5.4).
import LangButton from './LangButton.vue'
import './loginStrings'
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ApiError } from '../api'
import { t } from '../i18n'
import { logout, officeRoles, session, setBranch, switchEntry } from '../session'
import type { BranchRole } from '../types'

const router = useRouter()
const route = useRoute()
const next = typeof route.query.next === 'string' && /^\/merchant(\/|$)/.test(route.query.next) ? route.query.next : ''
const last = Number(localStorage.getItem('branch_id')) || 0
const rows = computed(() => [...officeRoles()].sort((a, b) => Number(b.branch_id === last) - Number(a.branch_id === last)))
const fromWork = computed(() => session.user?.entry === 'work')
const chosen = ref<BranchRole | null>(null)
const password = ref('')
const error = ref('')
const busy = ref(false)

async function open(r: BranchRole) {
  error.value = ''
  if (fromWork.value && chosen.value?.branch_id !== r.branch_id) { chosen.value = r; password.value = ''; return }
  busy.value = true
  try {
    if (fromWork.value) await switchEntry('office', password.value)
    setBranch(r.branch_id)
    // a full load, so that every page of the back office starts on this store
    location.href = next || '/merchant/orders'
  } catch (e) {
    error.value = e instanceof ApiError && e.code === 'BAD_PASSWORD' ? t('lg.bad_password') : (e as Error).message
  } finally { busy.value = false }
}
async function work() {
  busy.value = true; error.value = ''
  try {
    if (!fromWork.value) await switchEntry('work')
    router.replace('/work')
  } catch (e) { error.value = (e as Error).message } finally { busy.value = false }
}
async function out() { await logout(); session.loaded = true; router.replace('/login') }
</script>

<template>
  <div class="phone login">
    <div class="section stack pickbox">
      <div class="row between"><h1>{{ t('lg.pick') }}</h1><LangButton class="lang" /></div>
      <p class="muted small" style="margin:0">{{ session.user?.display_name }} · {{ t('lg.pick_hint') }}</p>
      <div v-if="!rows.length" class="banner warn" data-testid="login-staff-only">{{ t('lg.staff_only') }}</div>
      <div v-if="error" class="banner bad">{{ error }}</div>
      <div class="picklist">
        <template v-for="r in rows" :key="r.branch_id">
          <button class="pickrow" :class="{ on: chosen?.branch_id === r.branch_id }" data-testid="pick-row" :data-branch="r.branch_id" :disabled="busy" @click="open(r)">
            <span class="grow"><b>{{ r.branch_name }}</b><small v-if="r.branch_id === last" class="muted"> · {{ t('lg.last') }}</small></span>
            <span class="rtag" :class="r.role">{{ t('role.' + r.role) }}</span><span class="rtl-flip muted">›</span>
          </button>
          <form v-if="fromWork && chosen?.branch_id === r.branch_id" class="pickpw stack" @submit.prevent="open(r)">
            <span class="small muted">{{ t('lg.again') }}</span>
            <div class="row" style="gap:8px">
              <input v-model="password" class="grow" type="password" autocomplete="current-password" required data-testid="pick-password" :aria-label="t('password')" />
              <button class="btn primary" :disabled="busy || !password" data-testid="pick-enter">{{ t('lg.enter') }}</button>
            </div>
          </form>
        </template>
        <button class="pickrow" data-testid="pick-work" :disabled="busy" @click="work">
          <span class="grow"><b>{{ t('lg.work') }}</b><small class="muted"> · {{ t('lg.work_hint') }}</small></span><span class="rtl-flip muted">›</span>
        </button>
      </div>
      <button class="btn link" @click="out">{{ t('sign_out') }}</button>
    </div>
  </div>
</template>

<style scoped>
.picklist { display: flex; flex-direction: column; border: 1px solid var(--line); border-radius: 12px; overflow: hidden; background: #fff; }
.pickrow { display: flex; align-items: center; gap: 10px; padding: 14px; border: 0; border-bottom: 1px solid var(--line); background: #fff; text-align: start; cursor: pointer; font-size: 14.5px; color: inherit; }
.pickrow:last-child { border-bottom: 0; }
.pickrow:hover, .pickrow.on { background: #fafafa; }
.pickpw { padding: 10px 14px 14px; border-bottom: 1px solid var(--line); background: #fafafa; }
.pickpw input { min-width: 0; padding: 9px 10px; border: 1px solid var(--line); border-radius: 8px; font-size: 14px; }
@media (min-width: 768px) { .pickbox { background: #fff; border: 1px solid var(--line); border-radius: 14px; padding: 28px; box-shadow: 0 6px 24px rgba(0,0,0,.06); } }
</style>
