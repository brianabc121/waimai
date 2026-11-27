<script setup lang="ts">
// The platform's balance ledger and the state of the books
// (docs/BALANCE_CHECK.md §5): the last check, every movement of every
// balance, and the closed days.
import { onMounted, ref } from 'vue'
import { get } from '../../api'
import { t } from '../../i18n'
import { fmtDateTime } from './util'
import type { WalletCheck } from '../../walletLedger'
import WalletLedger from '../WalletLedger.vue'
import WalletDays from '../WalletDays.vue'

const tab = ref<'ledger' | 'days'>('ledger')
const check = ref<WalletCheck | null>(null)
onMounted(() => { void get<{ last: WalletCheck | null }>('/admin/wallet-check').then(r => { check.value = r.last }).catch(() => {}) })
const warn = (w: string) => { const k = w.split(' ')[0]; return k === 'APP_ROLE' ? t('wl.warn.APP_ROLE') : w }
</script>

<template>
  <div class="page stack" data-testid="admin-ledger">
    <div v-if="check?.problems?.length" class="banner bad" data-testid="wl-check-bad">
      {{ t('wl.chk.bad', { n: check.problems.length, at: fmtDateTime(check.ran_at) }) }}
      <div v-for="(p, i) in check.problems.slice(0, 30)" :key="i" class="small mono">{{ p }}</div>
    </div>
    <div v-else-if="check" class="banner ok" data-testid="wl-check-ok">{{ t('wl.chk.ok', { at: fmtDateTime(check.ran_at), full: fmtDateTime(check.last_full) }) }}
      <div class="small mono muted">{{ t('wl.chk.digest') }} {{ check.digest.slice(0, 16) }}</div></div>
    <div v-for="(w, i) in check?.warnings ?? []" :key="i" class="banner warn"><b>{{ t('wl.chk.warn') }}</b> {{ warn(w) }}</div>
    <div class="row gap">
      <button class="btn" :class="{ primary: tab === 'ledger' }" @click="tab = 'ledger'">{{ t('wl.tab') }}</button>
      <button class="btn" :class="{ primary: tab === 'days' }" data-testid="wl-tab-days" @click="tab = 'days'">{{ t('wl.days') }}</button>
    </div>
    <div class="card">
      <WalletLedger v-if="tab === 'ledger'" :branch="null" />
      <WalletDays v-else :branch="null" />
    </div>
  </div>
</template>

<style scoped>
.gap { gap: 8px; }
</style>
