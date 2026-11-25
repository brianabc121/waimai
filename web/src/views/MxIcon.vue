<script setup lang="ts">
// Line icons of the menu page (MenuView, NoteTemplates, ToppingTemplates).
defineProps<{ name: 'folder' | 'search' | 'dots' | 'grip' | 'list' | 'grid' | 'eye' | 'trash' | 'plus' | 'info' | 'sliders'; size?: number }>()
</script>

<template>
  <svg class="mxi" :width="size ?? 18" :height="size ?? 18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"
    stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
    <path v-if="name === 'folder'" d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" />
    <template v-else-if="name === 'search'"><circle cx="11" cy="11" r="6.5" /><path d="m20 20-4.2-4.2" /></template>
    <template v-else-if="name === 'dots'"><circle cx="5" cy="12" r="1.3" fill="currentColor" /><circle cx="12" cy="12" r="1.3" fill="currentColor" /><circle cx="19" cy="12" r="1.3" fill="currentColor" /></template>
    <template v-else-if="name === 'grip'"><circle cx="9" cy="6" r="1.2" fill="currentColor" /><circle cx="15" cy="6" r="1.2" fill="currentColor" /><circle cx="9" cy="12" r="1.2" fill="currentColor" />
      <circle cx="15" cy="12" r="1.2" fill="currentColor" /><circle cx="9" cy="18" r="1.2" fill="currentColor" /><circle cx="15" cy="18" r="1.2" fill="currentColor" /></template>
    <path v-else-if="name === 'list'" d="M5 7h14M5 12h14M5 17h14" />
    <template v-else-if="name === 'grid'"><rect x="4" y="4" width="6.5" height="6.5" rx="1.5" /><rect x="13.5" y="4" width="6.5" height="6.5" rx="1.5" />
      <rect x="4" y="13.5" width="6.5" height="6.5" rx="1.5" /><rect x="13.5" y="13.5" width="6.5" height="6.5" rx="1.5" /></template>
    <template v-else-if="name === 'eye'"><path d="M2 12s3.6-6.5 10-6.5S22 12 22 12s-3.6 6.5-10 6.5S2 12 2 12z" /><circle cx="12" cy="12" r="3" /></template>
    <path v-else-if="name === 'trash'" d="M4 7h16M9 7V4.5h6V7M6.5 7l1 13h9l1-13M10 11v5M14 11v5" />
    <path v-else-if="name === 'plus'" d="M12 5v14M5 12h14" />
    <template v-else-if="name === 'info'"><circle cx="12" cy="12" r="9" /><path d="M12 11v5M12 8h.01" /></template>
    <path v-else-if="name === 'sliders'" d="M4 7h10M18 7h2M4 17h4M12 17h8M16 5v4M10 15v4" />
  </svg>
</template>

<style scoped>
.mxi { display: inline-block; vertical-align: middle; flex: none; }
</style>
