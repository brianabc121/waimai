<script setup lang="ts">
// One table's bill, grouped by ordering round. Staff also get serve and void.
import KitchenNote from './KitchenNote.vue'
import { computed } from 'vue'
import { both, money, pick, t, mn } from '../i18n'
import type { Order, OrderItem } from '../types'

const props = defineProps<{ order: Order; staff?: boolean; readonly?: boolean }>()
defineEmits<{ serve: [item: OrderItem]; void: [item: OrderItem] }>()

const batches = computed(() => {
  const m = new Map<number, { seq: number; source: string; items: OrderItem[] }>()
  for (const it of props.order.items) {
    if (!m.has(it.batch_seq)) m.set(it.batch_seq, { seq: it.batch_seq, source: it.batch_source, items: [] })
    m.get(it.batch_seq)!.items.push(it)
  }
  return [...m.values()].sort((a, b) => a.seq - b.seq)
})
function state(it: OrderItem): [string, string] {
  if (it.prep_status === 'cancelled') return [t('voided'), '']
  if (it.served_at) return [t('served'), 'ok']
  if (it.prep_status === 'ready') return [t('i.ready'), 'info']
  return [t('i.' + it.prep_status), it.prep_status === 'preparing' ? 'warn' : '']
}
const name = (it: OrderItem) => props.staff ? both(it.name, it.name_en) : mn(it)
</script>

<template>
  <div class="stack">
    <div v-if="batches.length === 0" class="muted">{{ t('empty_bill') }}</div>
    <div v-for="b in batches" :key="b.seq" class="batch" data-test="batch">
      <div class="row between small muted" style="margin-bottom:4px">
        <span class="bold">{{ t('batch_n', { n: b.seq }) }}</span><span class="tag">{{ t('src.' + b.source) }}</span>
      </div>
      <div v-for="it in b.items" :key="it.id" class="bline" :class="{ strike: it.prep_status === 'cancelled' }">
        <div class="grow">
          <div>{{ it.qty }} × {{ name(it) }}</div>
          <div class="small muted" v-if="it.options.length">{{ it.options.map(x => props.staff ? pick(x.name, x.name_en) : mn(x)).join(' · ') }}</div>
          <KitchenNote :it="it" />
          <div class="small voidr" v-if="it.void_reason">{{ t('reason') }}: {{ it.void_reason }}</div>
        </div>
        <span class="tag" :class="state(it)[1]">{{ state(it)[0] }}</span>
        <span class="price">{{ money(it.line_total, order.currency) }}</span>
        <template v-if="staff && !readonly && it.prep_status !== 'cancelled'">
          <button v-if="it.prep_status === 'ready' && !it.served_at" class="btn small ok" @click="$emit('serve', it)">{{ t('serve') }}</button>
          <button class="btn small" @click="$emit('void', it)">{{ t('void_item') }}</button>
        </template>
      </div>
    </div>
  </div>
</template>

<style scoped>
.batch { border: 1px solid var(--line); border-radius: 10px; padding: 8px 10px; background: #fff; }
.bline { display: flex; align-items: center; gap: 8px; padding: 6px 0; border-top: 1px dashed var(--line); }
.bline.strike > .grow, .bline.strike > .price { text-decoration: line-through; color: var(--muted); }
.voidr { color: #b42318; }
.price { font-variant-numeric: tabular-nums; white-space: nowrap; }
</style>
