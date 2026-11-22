<script setup lang="ts">
// The balance or the rewards: what is held and how it came about. Both are a
// record kept by the platform; nothing here moves money.
import { computed, ref, watch } from 'vue'
import { money, t } from '../../i18n'
import MePage from './MePage.vue'
import { dateTime, describe, homeStore, me, walletEntries, type WalletEntry } from './me'

const props = defineProps<{ account: string }>()
const kind = computed(() => props.account === 'bonus' ? 'bonus' : 'balance')
const entries = ref<WalletEntry[] | null>(null)
const error = ref('')
const totals = computed(() => {
  const ws = (me.profile?.wallets ?? []).map(w => ({ currency: w.currency, amount: w[kind.value] })).filter(w => w.amount !== 0)
  return ws.length ? ws : [{ currency: homeStore()?.currency ?? 'PHP', amount: 0 }]
})
watch(() => [kind.value, me.profile?.id], async ([, id]) => {
  if (!id) return
  entries.value = null
  try { entries.value = await walletEntries(kind.value) } catch (e) { error.value = describe(e) }
}, { immediate: true })
</script>

<template>
  <MePage :title="t('mw.' + kind)">
    <div class="card total" data-testid="wallet-total">
      <span class="small">{{ t('me.' + kind) }}</span>
      <b v-for="w in totals" :key="w.currency">{{ money(w.amount, w.currency) }}</b>
    </div>
    <p class="note" style="padding:0 6px" data-testid="wallet-note">{{ t(`mw.${kind}_note`) }}</p>
    <div v-if="error" class="banner bad">{{ error }}</div>
    <div class="card">
      <h2 style="margin-bottom:6px">{{ t('mw.records') }}</h2>
      <div v-if="entries && !entries.length" class="empty" data-testid="wallet-empty">{{ t('mw.none') }}</div>
      <div v-for="e in entries ?? []" :key="e.id" class="entry" data-testid="wallet-entry">
        <div class="grow"><div>{{ e.note }}</div><div class="small muted">{{ dateTime(e.created_at) }}</div></div>
        <b :class="e.amount > 0 ? 'in' : 'out'">{{ e.amount > 0 ? '+' : '−' }}{{ money(Math.abs(e.amount), e.currency) }}</b>
      </div>
    </div>
  </MePage>
</template>

<style scoped>
.total { display: grid; gap: 4px; padding: 22px 18px; color: #fff; background: linear-gradient(120deg, var(--brand) 0%, #f08c3a 100%); }
.total b { font-size: 32px; line-height: 1.15; font-variant-numeric: tabular-nums; }
.entry { display: flex; align-items: center; gap: 10px; padding: 11px 0; border-bottom: 1px solid #f0f1f3; }
.entry:last-child { border-bottom: 0; }
.entry b { font-variant-numeric: tabular-nums; white-space: nowrap; } .entry .in { color: var(--brand-dark); } .entry .out { color: var(--ink); }
</style>
