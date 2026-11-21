<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { locale, setLocale, t } from '../i18n'
import { currentRole, logout, session, setAdminView, setBranch } from '../session'

const router = useRouter()
const role = computed(() => currentRole()?.role ?? '')
const desk = computed(() => ['owner', 'manager', 'cashier'].includes(role.value))
const manager = computed(() => ['owner', 'manager'].includes(role.value))
const posts = computed(() => currentRole()?.posts ?? [])
const inStore = computed(() => posts.value.some(p => p.kind !== 'rider'))

async function out() { await logout(); session.loaded = true; router.replace('/login') }
const viewing = computed(() => !!session.user?.is_platform_admin)
function backToAdmin() { setAdminView(null); location.href = '/admin/merchants' }
function change(e: Event) { setBranch(Number((e.target as HTMLSelectElement).value)); location.reload() }
</script>

<template>
  <div>
    <nav class="staffbar no-print">
      <select v-if="(session.user?.roles.length ?? 0) > 1" :value="session.branchId ?? undefined" @change="change">
        <option v-for="r in session.user!.roles" :key="r.branch_id" :value="r.branch_id">{{ r.branch_name }}</option>
      </select>
      <b v-else>{{ currentRole()?.branch_name }}</b>
      <router-link v-if="desk" to="/merchant/orders">{{ t('orders') }}</router-link>
      <router-link v-if="desk" to="/merchant/tables">{{ t('tables') }}</router-link>
      <router-link v-if="desk || inStore" to="/kitchen">{{ t('kitchen') }}</router-link>
      <router-link v-if="desk || inStore" to="/merchant/menu">{{ t('menu_admin') }}</router-link>
      <router-link v-if="manager" to="/merchant/templates">{{ t('templates') }}</router-link>
      <router-link v-if="manager" to="/merchant/promos">{{ t('promos') }}</router-link>
      <router-link v-if="manager" to="/merchant/cash">{{ t('cash') }}</router-link>
      <router-link v-if="manager" to="/merchant/delivery-map">{{ t('delivery_map') }}</router-link>
      <router-link v-if="manager" to="/merchant/staff">{{ t('staff') }}</router-link>
      <router-link v-if="manager" to="/merchant/settings">{{ t('settings') }}</router-link>
      <router-link v-if="posts.length" to="/work">{{ t('my_workbench') }}</router-link>
      <span class="grow"></span>
      <span class="small">{{ session.user?.display_name }}</span>
      <button class="lang" @click="setLocale(locale === 'zh' ? 'en' : 'zh')">{{ locale === 'zh' ? 'EN' : '中文' }}</button>
      <button class="btn link" @click="out">{{ t('sign_out') }}</button>
    </nav>
    <div v-if="viewing" class="banner info no-print" style="border-radius:0" data-testid="admin-viewing">
      {{ t('admin_viewing') }} <button class="btn small" style="margin-left:8px" @click="backToAdmin">{{ t('admin_back') }}</button></div>
    <div v-if="!session.branchId" class="page"><div class="banner warn">{{ t('not_found') }}</div></div>
    <router-view v-else :key="session.branchId" />
  </div>
</template>
