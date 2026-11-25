<script setup lang="ts">
// What goes with a dish on the customer's cart, checkout and order pages:
// the specs in grey (with the price before toppings when there are some),
// then the toppings (加料) under one label beside a line, one per row with
// its price on the right, then the flavour notes (口味) and the note (备注).
import { money, t } from '../i18n'
import '../toppings'

defineProps<{ specs: string[]; toppings: { name: string; price: number }[]; flavors: string[]; note: string; cur: string; tid: string; base?: number | null }>()
</script>

<template>
  <div class="lx">
    <div v-if="specs.length || (toppings.length && base != null)" class="lx-spec">{{ [...specs, toppings.length && base != null ? t('tp.c.unit', { p: money(base, cur) }) : ''].filter(Boolean).join(' · ') }}</div>
    <div v-if="toppings.length" class="lx-row" :data-testid="`${tid}-toppings`"><span class="lx-lab add">{{ t('tp.c.topping') }}</span>
      <div class="lx-adds">
        <div v-for="(x, i) in toppings" :key="i" class="lx-add"><span class="k">+ {{ x.name }}</span><span v-if="x.price" class="p">{{ money(x.price, cur) }}</span></div>
      </div></div>
    <div v-if="flavors.length" class="lx-row" :data-testid="`${tid}-notes`"><span class="lx-lab fl">{{ t('tp.c.flavor') }}</span>
      <span class="lx-v">{{ flavors.join(' · ') }}</span></div>
    <div v-if="note" class="lx-row" :data-testid="`${tid}-memo`"><span class="lx-lab nt">{{ t('tp.c.note') }}</span>
      <span class="lx-v">{{ note }}</span></div>
  </div>
</template>

<style scoped>
.lx { min-width: 0; }
.lx-spec { color: var(--muted); font-size: 12.5px; margin-top: 2px; overflow-wrap: anywhere; }
.lx-row { display: flex; gap: 6px; align-items: flex-start; margin-top: 4px; font-size: 12.5px; line-height: 1.45; }
.lx-lab { flex: none; font-size: 11px; font-weight: 700; line-height: 16px; border-radius: 4px; padding: 1px 5px; margin-top: 1px; white-space: nowrap; }
.lx-lab.add { background: var(--red, #a02c20); color: #fff; }
.lx-lab.fl { background: var(--gold-soft, #fbe7b3); color: var(--red-deep, #8f251b); }
.lx-lab.nt { background: #efe6da; color: var(--muted); }
.lx-v { min-width: 0; overflow-wrap: anywhere; color: var(--ink); }
.lx-adds { flex: 1; min-width: 0; border-inline-start: 2px solid #f0c9a6; padding-inline-start: 8px; }
.lx-add { display: flex; justify-content: space-between; gap: 8px; line-height: 20px; }
.lx-add .k { color: var(--red-deep, #8f251b); font-weight: 600; min-width: 0; overflow-wrap: anywhere; }
.lx-add .p { flex: none; color: var(--ink); font-variant-numeric: tabular-nums; }
</style>
