<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { get, post } from '../api'
import { locale, setLocale, t } from '../i18n'
import { currentRole, logout, session, setAdminView, setBranch } from '../session'
import { useShell } from '../shell'

const router = useRouter()
const shell = useShell('store')
const role = computed(() => currentRole()?.role ?? '')
const desk = computed(() => ['owner', 'manager', 'cashier'].includes(role.value))
const manager = computed(() => ['owner', 'manager'].includes(role.value))
const posts = computed(() => currentRole()?.posts ?? [])
const inStore = computed(() => posts.value.some(p => p.kind !== 'rider'))

// Clocking on and off from the back office: the same shift as in the workbench.
const shift = ref<{ on_duty: boolean } | null>(null)
onMounted(async () => { if (role.value) shift.value = await get<{ on_duty: boolean }>('/work/shift').catch(() => null) })
async function toggleShift() {
  if (!shift.value) return
  shift.value = await post<{ on_duty: boolean }>('/work/shift', { on_duty: !shift.value.on_duty, branch_id: session.branchId }).catch(() => shift.value)
}

async function out() { await logout(); session.loaded = true; router.replace('/login') }
const viewing = computed(() => !!session.user?.is_platform_admin)
function backToAdmin() { setAdminView(null); location.href = '/admin/merchants' }
function change(e: Event) { setBranch(Number((e.target as HTMLSelectElement).value)); location.reload() }
</script>

<template>
  <div class="shell" :class="{ drawer: shell.drawer.value, open: shell.open.value }">
    <header class="shell-top no-print">
      <button class="shell-menu" data-testid="nav-open" :aria-label="t('nav_menu')" @click="shell.show()"><i></i><i></i><i></i></button>
      <b class="grow shell-name">{{ currentRole()?.branch_name }}</b>
      <button v-if="shift" class="shiftbtn" :class="{ on: shift.on_duty }" :title="t(shift.on_duty ? 'shift_end' : 'shift_start')" @click="toggleShift">
        <i></i>{{ t(shift.on_duty ? 'shift_on' : 'shift_start') }}</button>
    </header>
    <div class="shell-mask no-print" @click="shell.open.value = false"></div>
    <nav class="staffbar no-print">
      <div class="shell-brand">
        <select v-if="(session.user?.roles.length ?? 0) > 1" :value="session.branchId ?? undefined" @change="change">
          <option v-for="r in session.user!.roles" :key="r.branch_id" :value="r.branch_id">{{ r.branch_name }}</option>
        </select>
        <b v-else class="grow">{{ currentRole()?.branch_name }}</b>
        <button v-if="!shell.narrow.value" class="shell-fold" data-testid="nav-fold" :title="t('nav_fold')" :aria-label="t('nav_fold')" @click="shell.fold()">«</button>
        <button v-else class="shell-fold" :aria-label="t('close')" @click="shell.open.value = false">×</button>
      </div>
      <div class="shell-links">
        <router-link v-if="desk" to="/merchant/orders">{{ t('orders') }}</router-link>
        <router-link v-if="desk" to="/merchant/tables">{{ t('tables') }}</router-link>
        <router-link v-if="desk || inStore" to="/kitchen">{{ t('kitchen') }}</router-link>
        <router-link v-if="desk || inStore" to="/merchant/menu">{{ t('menu_admin') }}</router-link>
        <router-link v-if="manager" to="/merchant/templates">{{ t('templates') }}</router-link>
        <router-link v-if="manager" to="/merchant/media">{{ t('promo_media') }}</router-link>
        <router-link v-if="manager" to="/merchant/promos">{{ t('promos') }}</router-link>
        <router-link v-if="manager" to="/merchant/cash">{{ t('cash') }}</router-link>
        <router-link v-if="desk" to="/merchant/riders">{{ t('riders') }}</router-link>
        <router-link v-if="manager" to="/merchant/delivery-map">{{ t('delivery_map') }}</router-link>
        <router-link v-if="manager" to="/merchant/staff">{{ t('staff') }}</router-link>
        <router-link v-if="manager" to="/merchant/settings">{{ t('settings') }}</router-link>
        <router-link v-if="posts.length" to="/work">{{ t('my_workbench') }}</router-link>
      </div>
      <div class="shell-foot">
        <button v-if="shift" class="shiftbtn" :class="{ on: shift.on_duty }" data-testid="office-shift" :data-on="shift.on_duty" :title="t(shift.on_duty ? 'shift_end' : 'shift_start')" @click="toggleShift">
          <i></i>{{ t(shift.on_duty ? 'shift_on' : 'shift_start') }}</button>
        <div class="row between">
          <span class="small grow who">{{ session.user?.display_name }}</span>
          <button class="lang" @click="setLocale(locale === 'zh' ? 'en' : 'zh')">{{ locale === 'zh' ? 'EN' : '中文' }}</button>
        </div>
        <button class="btn link" @click="out">{{ t('sign_out') }}</button>
      </div>
    </nav>
    <main class="shell-main">
      <div v-if="viewing" class="banner info no-print" style="border-radius:0" data-testid="admin-viewing">
        {{ t('admin_viewing') }} <button class="btn small" style="margin-left:8px" @click="backToAdmin">{{ t('admin_back') }}</button></div>
      <div v-if="!session.branchId" class="page"><div class="banner warn">{{ t('not_found') }}</div></div>
      <router-view v-else :key="session.branchId" />
    </main>
  </div>
</template>

<style>
.shiftbtn { background: none; border: 1px solid #8b949e; color: #fff; border-radius: 999px; padding: 4px 12px; font-size: 12.5px; cursor: pointer; white-space: nowrap; }
.shiftbtn i { display: inline-block; width: 7px; height: 7px; border-radius: 50%; background: #8b949e; margin-right: 5px; }
.shiftbtn.on { border-color: #3fb950; } .shiftbtn.on i { background: #3fb950; }
</style>
