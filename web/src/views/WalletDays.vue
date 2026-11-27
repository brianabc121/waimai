<script setup lang="ts">
// The daily trial balance (docs/BALANCE_CHECK.md W19): each kind of account
// on a closed day, opening + in − out = closing, every currency adding up
// to 0.
import { onMounted, ref } from 'vue'
import { loadDays, type TrialBalance } from '../walletLedger'
import { money, t } from '../i18n'

const props = defineProps<{ branch: number | null }>()
const tb = ref<TrialBalance | null>(null)
const day = ref('')
const store = ref('')
const error = ref('')
async function load() {
  error.value = ''
  try {
    const q: Record<string, string | boolean> = { day: day.value }
    if (props.branch === null) { if (store.value.trim()) q.branch = store.value.trim(); else q.all = true }
    tb.value = await loadDays(props.branch, q)
    if (!day.value && tb.value.day) day.value = tb.value.day
  } catch (e) { error.value = (e as Error).message }
}
onMounted(load)
</script>

<template>
  <div class="stack" data-testid="wallet-days">
    <div class="row wrap gap">
      <label class="field"><span>{{ t('wl.d.day') }}</span><input v-model="day" type="date" @change="load" /></label>
      <label v-if="branch === null" class="field"><span>{{ t('wl.f.store') }}</span><input v-model="store" inputmode="numeric" style="width:90px" @keyup.enter="load" /></label>
      <button class="btn" @click="load">{{ t('wl.search') }}</button>
    </div>
    <div v-if="error" class="banner bad">{{ error }}</div>
    <template v-if="tb">
      <div v-if="!tb.last_day" class="muted">{{ t('wl.d.none') }}</div>
      <template v-else>
        <div v-if="tb.unclosed" class="banner warn">{{ t('wl.d.open', { d: tb.last_day }) }}</div>
        <div v-else class="banner" :class="tb.balanced ? 'ok' : 'bad'" data-testid="wl-balanced">{{ t(tb.balanced ? 'wl.d.ok' : 'wl.d.bad') }}
          <span v-if="branch !== null" class="muted small"> · {{ t('wl.d.tz', { tz: tb.timezone }) }}</span></div>
        <div class="scroll">
          <table class="data">
            <thead><tr><th>{{ t('wl.c.account') }}</th><th class="num">{{ t('wl.d.accounts') }}</th><th class="num">{{ t('wl.d.opening') }}</th>
              <th class="num">{{ t('wl.d.credits') }}</th><th class="num">{{ t('wl.d.debits') }}</th><th class="num">{{ t('wl.d.closing') }}</th></tr></thead>
            <tbody>
              <tr v-for="r in tb.rows" :key="r.currency + r.account_kind" data-testid="wl-day-row">
                <td>{{ t('wl.a.' + r.account_kind) }} <span class="muted small">{{ r.currency }}</span></td>
                <td class="num">{{ r.accounts }}</td>
                <td class="num">{{ money(r.opening, r.currency) }}</td><td class="num">{{ money(r.credits, r.currency) }}</td>
                <td class="num">{{ money(r.debits, r.currency) }}</td><td class="num"><b>{{ money(r.closing, r.currency) }}</b></td>
              </tr>
            </tbody>
          </table>
        </div>
      </template>
    </template>
  </div>
</template>

<style scoped>
.scroll { overflow-x: auto; }
.gap { gap: 10px; align-items: flex-end; }
.wrap { flex-wrap: wrap; }
.field { margin: 0; }
</style>
