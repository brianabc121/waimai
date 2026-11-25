<script setup lang="ts">
// 点单 · 堂食下单: the dine-in desk (docs/DINE_IN_ORDER_DESIGN.md) on a page of
// its own. A link with ?table=ID&view=bill|order&pay=1 opens that table.
import { onMounted, ref } from 'vue'
import { get } from '../api'
import { currentRole } from '../session'
import type { Storefront } from '../types'
import DineDesk from './dine/DineDesk.vue'
import './manualStrings'
import './dine/strings'

const store = ref<Storefront | null>(null)
onMounted(async () => {
  // the next time someone opens an old "order for a customer" link here
  try { localStorage.setItem('mo-mode', 'dine') } catch { /* private window */ }
  const slug = currentRole()?.slug
  if (slug) store.value = await get<Storefront>(`/stores/${slug}`).catch(() => null)
})
</script>

<template>
  <DineDesk :store="store" />
</template>
