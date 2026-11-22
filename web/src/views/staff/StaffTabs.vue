<script setup lang="ts">
// The row of sub-pages under "Staff".
import { t } from '../../i18n'
import './strings'
</script>

<template>
  <nav class="stafftabs no-print" data-testid="staff-tabs">
    <router-link to="/merchant/staff" exact-active-class="on" data-tab="list">{{ t('sr.tab.list') }}</router-link>
    <router-link to="/merchant/staff/attendance" active-class="on" data-tab="attendance">{{ t('sr.tab.attendance') }}</router-link>
    <router-link to="/merchant/staff/kitchen" active-class="on" data-tab="kitchen">{{ t('sr.tab.kitchen') }}</router-link>
  </nav>
</template>

<style scoped>
.stafftabs { display: flex; gap: 4px; border-bottom: 1px solid var(--line); margin: -4px 0 16px; }
.stafftabs a { padding: 9px 14px; font-weight: 600; color: var(--muted); border-bottom: 3px solid transparent; margin-bottom: -1px; text-decoration: none; }
.stafftabs a.on { color: var(--brand-dark); border-bottom-color: var(--brand); }
</style>
