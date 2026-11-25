<script setup lang="ts">
// The row of sub-pages under "Staff".
import { t } from '../../i18n'
import { can, isOwner } from '../../session'
import '../../perms'
import './strings'
import '../../officeStrings'
</script>

<template>
  <div class="ox-head st-head no-print">
    <div><h1>{{ t('staff') }}</h1><p class="ox-sub">{{ t('ox.st.sub') }}</p></div>
    <div class="ox-acts"><slot /></div>
  </div>
  <nav class="ox-tabs stafftabs no-print" data-testid="staff-tabs">
    <router-link v-if="can('staff.view')" to="/merchant/staff" exact-active-class="on" data-tab="list">{{ t('sr.tab.list') }}</router-link>
    <router-link v-if="can('staff.attendance')" to="/merchant/staff/attendance" active-class="on" data-tab="attendance">{{ t('sr.tab.attendance') }}</router-link>
    <router-link v-if="can('staff.reports')" to="/merchant/staff/kitchen" active-class="on" data-tab="kitchen">{{ t('sr.tab.kitchen') }}</router-link>
    <router-link v-if="can('staff.reports')" to="/merchant/staff/riders" active-class="on" data-tab="riders">{{ t('sr.tab.riders') }}</router-link>
    <router-link v-if="isOwner()" to="/merchant/staff/roles" active-class="on" data-tab="roles">{{ t('rp.nav') }}</router-link>
  </nav>
</template>

<style scoped>
.st-head { margin-bottom: 14px; }
.stafftabs { margin-bottom: 16px; }
</style>
