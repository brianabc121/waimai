<script setup lang="ts">
// How the customer's own pages sit on a wider screen. A phone sees only the
// page. An iPad adds the red top bar and centres the page. A computer adds the
// list of every page of "me" on the left, and the page fills the rest.
import { onMounted } from 'vue'
import CustomerNav from './CustomerNav.vue'
import MeSide from './MeSide.vue'
import { loadProfile, me } from './me'

defineProps<{ active: 'home' | 'orders' | 'me' }>()
// the bar and the side show who is logged in, also on pages that do not load it themselves
onMounted(() => { if (!me.profile && window.matchMedia('(min-width: 768px)').matches) void loadProfile().catch(() => null) })
</script>

<template>
  <CustomerNav :active="active" />
  <div class="cframe">
    <MeSide />
    <div class="cf-main"><slot /></div>
  </div>
</template>

<style>
.cf-main { min-width: 0; }
@media (min-width: 768px) {
  .cframe { max-width: 760px; margin: 0 auto; padding: 18px 20px 0; }
}
@media (min-width: 1024px) {
  .cframe { max-width: 1240px; padding: 20px 24px 0; display: grid; grid-template-columns: 220px minmax(0, 1fr); gap: 24px; align-items: start; }
}
</style>
