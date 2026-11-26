<script setup lang="ts">
// The brand picker at the top of a page: one button per brand, and
// "all brands together" where the page can show them merged.
import { t } from '../../i18n'
import type { BrandRef } from '../../types'
defineProps<{ brands: BrandRef[]; all?: boolean }>()
const pick = defineModel<number>({ required: true })
</script>

<template>
  <div class="brseg" role="tablist" data-testid="brand-seg">
    <button v-if="all" type="button" role="tab" :class="{ on: pick === 0 }" :aria-selected="pick === 0" data-testid="brand-all" @click="pick = 0">{{ t('br.all') }}</button>
    <button v-for="b in brands" :key="b.id" type="button" role="tab" :class="{ on: pick === b.id }" :aria-selected="pick === b.id"
      :data-testid="'brand-' + b.id" @click="pick = b.id"><i :style="{ background: b.color }"></i>{{ b.name }}</button>
  </div>
</template>

<style scoped>
.brseg { display: inline-flex; flex-wrap: wrap; border: 1px solid var(--edge, #e6d8c4); border-radius: 10px; overflow: hidden; background: #fff; max-width: 100%; }
.brseg button { border: 0; border-inline-end: 1px solid var(--edge, #e6d8c4); background: none; padding: 6px 12px; font: inherit; font-size: 13px; font-weight: 600; display: inline-flex; align-items: center; gap: 6px; cursor: pointer; color: inherit; }
.brseg button:last-child { border-inline-end: 0; }
.brseg button.on { background: var(--brand, #a02c20); color: #fff; }
.brseg i { width: 9px; height: 9px; border-radius: 50%; display: inline-block; }
.brseg button.on i { box-shadow: 0 0 0 1.5px #fff; }
</style>
