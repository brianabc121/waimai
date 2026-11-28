<script setup lang="ts">
// One delivery address, the way every screen shows it: the place's name and
// the unit first and bold, the street on the line below.
import { computed } from 'vue'
import { addrParts, type Addressed } from '../place'

const props = defineProps<{ o: Addressed; lead?: string }>()
const p = computed(() => addrParts(props.o))
</script>

<template>
  <span class="addr-text" data-testid="addr-text">
    <span class="addr-head">{{ lead }}<b class="addr-name">{{ p.name }}</b><b v-if="p.unit" class="addr-unit"> · {{ p.unit }}</b></span>
    <span v-if="p.street" class="addr-street">{{ p.street }}</span>
  </span>
</template>

<style scoped>
.addr-street { display: block; font-size: .92em; opacity: .75; font-weight: 400; }
.addr-unit { color: var(--brand-dark, inherit); }
</style>
