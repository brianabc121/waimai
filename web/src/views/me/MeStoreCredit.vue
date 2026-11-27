<script setup lang="ts">
// Store balances: what each store refunded into the customer's balance there,
// and every change to one (docs/REFUND_BALANCE_DESIGN.md §8.1). A balance is
// spent only at its own store.
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { money, t } from '../../i18n'
import { storePath } from '../../site'
import MePage from './MePage.vue'
import { dateTime, describe, me, myStoreCreditEntries, myStoreCredits, type CreditEntry, type StoreCredit } from './me'

const props = defineProps<{ bid?: string }>()
const router = useRouter()
const items = ref<StoreCredit[] | null>(null)
const entries = ref<CreditEntry[] | null>(null)
const error = ref('')
const branchID = computed(() => Number(props.bid) || 0)
const current = computed(() => items.value?.find(c => c.branch_id === branchID.value) ?? null)

watch(() => [branchID.value, me.profile?.id], async ([bid, id]) => {
  if (!id) return
  error.value = ''
  try {
    items.value ??= await myStoreCredits()
    entries.value = bid ? await myStoreCreditEntries(bid as number) : null
  } catch (e) { error.value = describe(e) }
}, { immediate: true })

const open = (c: StoreCredit) => router.push(`/me/store-credit/${c.branch_id}`)
</script>

<template>
  <MePage :title="current ? current.store_name : t('sc.title')" :back="branchID ? '/me/store-credit' : '/me'" layout="split">
    <div class="side">
      <div v-if="current" class="card total" data-testid="credit-total">
        <span class="small">{{ current.store_name }}</span>
        <b>{{ money(current.balance, current.currency) }}</b>
        <span v-if="current.frozen" class="tag">{{ t('sc.frozen') }}</span>
        <span v-else-if="current.closed" class="tag">{{ t('sc.closed') }}</span>
        <router-link v-else-if="current.balance > 0" class="use" :to="storePath(current.slug)">{{ t('sc.use') }}</router-link>
      </div>
      <p class="note" style="padding:0 6px" data-testid="credit-note">{{ t('sc.note') }}</p>
    </div>
    <div v-if="error" class="banner bad">{{ error }}</div>
    <div v-if="!branchID" class="card main" data-testid="credit-list">
      <h2 style="margin-bottom:6px">{{ t('sc.title') }}</h2>
      <div v-if="items && !items.length" class="empty" data-testid="credit-empty">{{ t('sc.none') }}</div>
      <button v-for="c in items ?? []" :key="c.account_id" class="entry row-btn" data-testid="credit-store" @click="open(c)">
        <div class="grow"><div>{{ c.store_name }}</div>
          <div v-if="c.frozen || c.closed" class="small muted">{{ c.frozen ? t('sc.frozen') : t('sc.closed') }}</div></div>
        <b class="in">{{ money(c.balance, c.currency) }}</b>
        <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="m9 5 7 7-7 7" /></svg>
      </button>
    </div>
    <div v-else class="card main" data-testid="credit-entries">
      <h2 style="margin-bottom:6px">{{ t('mw.records') }}</h2>
      <div v-if="entries && !entries.length" class="empty">{{ t('mw.none') }}</div>
      <div v-for="e in entries ?? []" :key="e.id" class="entry" data-testid="credit-entry">
        <div class="grow"><div>{{ t('sc.k_' + e.kind) }}<span v-if="e.public_no" class="muted"> · {{ t('sc.order', { no: e.public_no }) }}</span></div>
          <div class="small muted">{{ dateTime(e.at) }} · {{ t('sc.after', { amt: money(e.balance, current?.currency) }) }}</div></div>
        <b :class="e.amount > 0 ? 'in' : 'out'">{{ e.amount > 0 ? '+' : '−' }}{{ money(Math.abs(e.amount), current?.currency) }}</b>
      </div>
    </div>
  </MePage>
</template>

<style scoped>
.total { display: grid; gap: 2px; padding: 16px; color: #fff; background: var(--motif) 0 0 / 56px 56px, linear-gradient(120deg, var(--red-deep) 0%, #c0503a 100%); }
.total b { font-size: 26px; font-weight: 600; line-height: 1.15; font-variant-numeric: tabular-nums; }
.total .tag { justify-self: start; margin-top: 6px; background: rgba(255, 255, 255, .2); color: #fff; }
.total .use { justify-self: start; margin-top: 8px; padding: 4px 14px; border-radius: 999px; background: #fff; color: var(--red-deep); font-weight: 600; text-decoration: none; }
.entry { display: flex; align-items: center; gap: 10px; padding: 10px 0; border-bottom: 1px solid #f0f1f3; }
.entry:last-child { border-bottom: 0; }
.row-btn { width: 100%; background: none; border: 0; border-bottom: 1px solid #f0f1f3; text-align: left; font: inherit; color: inherit; cursor: pointer; }
.entry b { font-weight: 600; font-variant-numeric: tabular-nums; white-space: nowrap; } .entry .in { color: var(--brand-dark); } .entry .out { color: var(--ink); }
</style>
