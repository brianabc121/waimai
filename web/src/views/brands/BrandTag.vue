<script setup lang="ts">
// A brand's coloured tag on an order (docs/MULTI_BRAND_DESIGN.md §12). Shown
// only when the store has more than one brand.
import { multiBrand } from '../../brands'
defineProps<{ name?: string; color?: string; big?: boolean }>()
</script>

<template>
  <span v-if="name && multiBrand()" class="brtag" :class="{ big }" :style="{ background: color || '#a02c20' }" data-testid="brand-tag">{{ name }}</span>
</template>

<style scoped>
.brtag { display: inline-flex; align-items: center; color: #fff; border-radius: 5px; padding: 0 6px; font-size: 11.5px; font-weight: 700; line-height: 18px; white-space: nowrap; vertical-align: middle; }
.brtag.big { font-size: 14px; line-height: 24px; padding: 0 8px; }
</style>
