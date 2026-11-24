<script setup lang="ts">
// An order's money history (docs/PRICING_REVIEW.md §5.3): every change to
// its amounts, written by the database, oldest first.
import { ref, watch } from 'vue'
import { get } from '../api'
import { extend, money, t, whenText } from '../i18n'

extend({
  'ml.btn': ['金额记录', 'Money history'],
  'ml.empty': ['还没有记录', 'No changes yet'],
  'ml.total': ['应收 {s}', 'Total {s}'],
  'ml.refunded': ['退回 {s}', 'Refunded {s}'],
  'ml.k.placed': ['下单', 'Placed'],
  'ml.k.addon': ['加单', 'Add-on'],
  'ml.k.void': ['退菜', 'Void'],
  'ml.k.adjust': ['结账优惠', 'Checkout discount'],
  'ml.k.table': ['加菜', 'Table round'],
  'ml.k.close': ['结账', 'Bill closed'],
  'ml.k.refund': ['退款', 'Refund'],
  'ml.k.opening': ['记录开始前', 'Before the history'],
  'ml.k.change': ['改动', 'Change'],
})

interface MoneyEvent {
  id: number
  kind: string
  actor_name: string
  total: number
  refunded: number
  delta_total: number
  created_at: string
}

const props = defineProps<{ orderId: number; version?: number; currency?: string; timezone?: string }>()
const open = ref(false)
const rows = ref<MoneyEvent[] | null>(null)
const error = ref('')

async function toggle() {
  open.value = !open.value
  if (open.value) await load()
}
// an open history follows the order as it changes
watch(() => props.version, () => { if (open.value) load() })
async function load() {
  error.value = ''
  try {
    rows.value = await get<MoneyEvent[]>(`/merchant/orders/${props.orderId}/money-events`)
  } catch (e) {
    error.value = (e as Error).message
  }
}
const kind = (k: string) => t(`ml.k.${['placed', 'addon', 'void', 'adjust', 'table', 'close', 'refund', 'opening'].includes(k) ? k : 'change'}`)
const delta = (v: number) => (v > 0 ? '+' : v < 0 ? '−' : '±') + money(Math.abs(v), props.currency)
</script>

<template>
  <div class="stack moneylog" data-testid="money-log">
    <button class="btn small" style="align-self:flex-start" data-testid="money-log-btn" @click="toggle">{{ t('ml.btn') }}</button>
    <template v-if="open">
      <div v-if="error" class="banner bad small">{{ error }}</div>
      <div v-else-if="rows && !rows.length" class="small muted">{{ t('ml.empty') }}</div>
      <div v-for="r in rows ?? []" :key="r.id" class="row between small" data-testid="money-row">
        <span>{{ kind(r.kind) }}<span class="muted"> · {{ whenText(r.created_at, timezone) }}<template v-if="r.actor_name"> · {{ r.actor_name }}</template></span></span>
        <span v-if="r.kind === 'refund'">{{ t('ml.refunded', { s: money(r.refunded, currency) }) }}</span>
        <span v-else>{{ delta(r.delta_total) }} <span class="muted">{{ t('ml.total', { s: money(r.total, currency) }) }}</span></span>
      </div>
    </template>
  </div>
</template>
