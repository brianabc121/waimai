<script setup lang="ts">
// The back office's pager (office.css): ‹ 1 … 4 5 6 … 20 ›, as on the menu page.
import { computed } from 'vue'

const props = defineProps<{ page: number; pages: number; testid?: string }>()
const emit = defineEmits<{ go: [page: number] }>()

const list = computed(() => {
  const n = props.pages, p = props.page
  const set = new Set([1, n, p - 1, p, p + 1].filter(x => x >= 1 && x <= n))
  const out: number[] = []
  let last = 0
  for (const x of [...set].sort((a, b) => a - b)) { if (x - last > 1) out.push(0); out.push(x); last = x }
  return out
})
</script>

<template>
  <nav v-if="pages > 1" class="ox-pager" :data-testid="testid">
    <button type="button" :disabled="page <= 1" aria-label="‹" data-testid="pager-prev" @click="emit('go', page - 1)"><span class="rtl-flip">‹</span></button>
    <template v-for="(p, i) in list" :key="i">
      <span v-if="p === 0" class="gap">…</span>
      <button v-else type="button" :class="{ on: p === page }" @click="emit('go', p)">{{ p }}</button>
    </template>
    <button type="button" :disabled="page >= pages" aria-label="›" data-testid="pager-next" @click="emit('go', page + 1)"><span class="rtl-flip">›</span></button>
  </nav>
</template>
