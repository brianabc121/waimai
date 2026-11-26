<script setup lang="ts">
// The site's root: the mall, or on a store's own page the store itself
// (docs/ORDER_TYPES_DESIGN.md §5.2).
import { defineAsyncComponent } from 'vue'
import { ownSite, ownSlug } from '../site'

const StoreView = defineAsyncComponent(() => import('./StoreView.vue'))
const MallHome = defineAsyncComponent(() => import('./mall/MallHome.vue'))
</script>

<template>
  <StoreView v-if="ownSite()" :slug="ownSlug()" />
  <MallHome v-else />
</template>
