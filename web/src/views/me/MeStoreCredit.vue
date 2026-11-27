<script setup lang="ts">
// Store balances, as the customer sees them (brian 2026-11-27): two
// balances per store, the cash balance (stored principal plus refunds paid
// in, both money the customer paid) and the top-up bonus. Tapping one shows
// its history. The books keep the three accounts apart (only the principal
// can be paid back, docs/STORED_VALUE_DESIGN.md §8.3); this page only adds
// the first two up. Refunds go back in the shares the order paid (W12).
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { money, t } from '../../i18n'
import { offersPath, storePath } from '../../site'
import { myStoredEntries } from '../../stored'
import MePage from './MePage.vue'
import { dateTime, describe, me, myStoreCreditEntries, myStoreCredits, type StoreCredit } from './me'

const props = defineProps<{ bid?: string }>()
const router = useRouter()
const items = ref<StoreCredit[] | null>(null)
const rows = ref<Row[] | null>(null)
const error = ref('')
const show = ref<'all' | 'cash' | 'bonus'>('all')
const branchID = computed(() => Number(props.bid) || 0)
const current = computed(() => items.value?.find(c => c.branch_id === branchID.value) ?? null)
const cur = computed(() => current.value?.currency)

// cash: the refund balance and the principal; bonus: what top-ups gave
const cashOf = (c: StoreCredit) => c.principal === undefined && c.bonus === undefined ? c.balance : (c.credit ?? 0) + (c.principal ?? 0)
const bonusOf = (c: StoreCredit) => c.bonus ?? 0

// One row of history: the refund balance and stored value are booked apart,
// so a spend from both is two entries; the same order's entries of one kind
// within a few seconds are one row here.
interface Row { key: string; at: string; kind: string; label: string; no: string; cash: number; bonus: number }
const kindOf: Record<string, string> = { spend: 'spend', stored_spend: 'spend', return: 'return', stored_return: 'return',
  refund: 'refund', stored_refund: 'refund' }
const labelOf: Record<string, string> = { spend: 'sc.k_spend', return: 'sc.k_return', refund: 'sc.k_refund', revoke: 'sc.k_revoke',
  topup: 'sv.k.topup', topup_revoke: 'sv.k.topup_revoke', stored_cashout: 'sv.k.stored_cashout' }

function merge(list: Row[]): Row[] {
  list.sort((a, b) => b.at.localeCompare(a.at))
  const out: Row[] = []
  for (const r of list) {
    const last = out[out.length - 1]
    if (last && r.no && last.no === r.no && last.kind === r.kind && Math.abs(Date.parse(last.at) - Date.parse(r.at)) < 5000) {
      last.cash += r.cash; last.bonus += r.bonus
    } else out.push({ ...r })
  }
  return out
}

watch(() => [branchID.value, me.profile?.id], async ([bid, id]) => {
  if (!id) return
  error.value = ''
  try {
    items.value ??= await myStoreCredits()
    if (!bid) { rows.value = null; return }
    const [credit, stored] = await Promise.all([myStoreCreditEntries(bid as number), myStoredEntries(bid as number).catch(() => [])])
    const list: Row[] = credit.map(e => ({ key: 'c' + e.id, at: e.at, kind: kindOf[e.kind] ?? e.kind, label: '', no: e.public_no, cash: e.amount, bonus: 0 }))
    for (const e of stored) list.push({ key: 's' + e.id, at: e.at, kind: kindOf[e.kind] ?? e.kind, label: '', no: e.public_no, cash: e.principal, bonus: e.bonus })
    rows.value = merge(list).map(r => ({ ...r, label: t(labelOf[r.kind] ?? 'sv.k.' + r.kind) }))
  } catch (e) { error.value = describe(e) }
}, { immediate: true })

const shown = computed(() => (rows.value ?? []).filter(r => show.value === 'all' || (show.value === 'cash' ? r.cash : r.bonus) !== 0))
// a word joiner keeps the sign on the amount's line
const signed = (v: number) => (v > 0 ? '+' : v < 0 ? '−' : '') + '⁠' + money(Math.abs(v), cur.value)
const open = (c: StoreCredit) => router.push(`/me/store-credit/${c.branch_id}`)
function pick(k: 'cash' | 'bonus') {
  show.value = show.value === k ? 'all' : k
  document.querySelector('[data-testid=credit-entries]')?.scrollIntoView({ behavior: 'smooth', block: 'start' })
}
</script>

<template>
  <MePage :title="current ? current.store_name : t('sc.title')" :back="branchID ? '/me/store-credit' : '/me'" :layout="branchID ? 'split' : 'one'">
    <div v-if="error" class="banner bad">{{ error }}</div>

    <!-- every store the customer holds a balance at -->
    <template v-if="!branchID">
      <div v-if="items && !items.length" class="card empty" data-testid="credit-empty">{{ t('sc.none') }}</div>
      <button v-for="c in items ?? []" :key="c.account_id" class="card store" data-testid="credit-store" @click="open(c)">
        <div class="sh"><b class="nm">{{ c.store_name }}</b>
          <span v-if="c.frozen || c.closed" class="tag">{{ c.frozen ? t('sc.frozen') : t('sc.closed') }}</span>
          <span class="tot">{{ money(c.balance, c.currency) }}</span>
          <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="m9 5 7 7-7 7" /></svg></div>
        <div class="two">
          <span><small>{{ t('sb.cash') }}</small><b>{{ money(cashOf(c), c.currency) }}</b></span>
          <span><small>{{ t('sb.bonus') }}</small><b class="gold">{{ money(bonusOf(c), c.currency) }}</b></span>
        </div>
      </button>
    </template>

    <!-- one store: the two balances, then their history -->
    <template v-else>
      <div class="side">
        <div v-if="current" class="card total" data-testid="credit-total">
          <span class="small">{{ t('sb.total') }}</span>
          <b class="big">{{ money(current.balance, current.currency) }}</b>
          <div class="tiles">
            <button class="tile" :class="{ on: show === 'cash' }" data-testid="credit-cash" @click="pick('cash')">
              <small>{{ t('sb.cash') }}</small><b>{{ money(cashOf(current), current.currency) }}</b><span>{{ t('sb.detail') }} ›</span></button>
            <button class="tile" :class="{ on: show === 'bonus' }" data-testid="credit-bonus" @click="pick('bonus')">
              <small>{{ t('sb.bonus') }}</small><b>{{ money(bonusOf(current), current.currency) }}</b><span>{{ t('sb.detail') }} ›</span></button>
          </div>
          <span v-if="current.frozen" class="tag">{{ t('sc.frozen') }}</span>
          <span v-else-if="current.closed" class="tag">{{ t('sc.closed') }}</span>
          <div v-else class="acts">
            <router-link v-if="current.balance > 0" class="use" :to="storePath(current.slug)">{{ t('sc.use') }}</router-link>
            <router-link class="use alt" :to="offersPath(current.slug)" data-testid="credit-topup">{{ t('sv.c.go') }}</router-link>
          </div>
        </div>
        <div class="card rules" data-testid="credit-note">
          <h3>{{ t('sb.how') }}</h3>
          <dl>
            <dt>{{ t('sb.cash') }}</dt><dd>{{ t('sb.cash_tip') }}</dd>
            <dt>{{ t('sb.bonus') }}</dt><dd>{{ t('sb.bonus_tip') }}</dd>
          </dl>
          <ol class="small muted"><li>{{ t('sb.r1') }}</li><li>{{ t('sb.r2') }}</li><li>{{ t('sb.r3') }}</li><li>{{ t('sb.r4') }}</li></ol>
        </div>
      </div>
      <div class="card main" data-testid="credit-entries">
        <div class="tabs">
          <button v-for="k in (['all', 'cash', 'bonus'] as const)" :key="k" :class="{ on: show === k }" :data-testid="'credit-tab-' + k" @click="show = k">
            {{ t(k === 'all' ? 'sb.all' : 'sb.' + k) }}</button>
        </div>
        <div v-if="rows && !shown.length" class="empty">{{ t('mw.none') }}</div>
        <div v-for="r in shown" :key="r.key" class="entry" data-testid="credit-entry">
          <div class="grow"><div>{{ r.label }}<span v-if="r.no" class="muted"> · {{ t('sc.order', { no: r.no }) }}</span></div>
            <div class="small muted">{{ dateTime(r.at) }}<template v-if="show === 'all' && r.cash && r.bonus"> · {{ t('sb.split', { c: signed(r.cash), b: signed(r.bonus) }) }}</template></div></div>
          <b :class="(show === 'bonus' ? r.bonus : show === 'cash' ? r.cash : r.cash + r.bonus) > 0 ? 'in' : 'out'">
            {{ signed(show === 'bonus' ? r.bonus : show === 'cash' ? r.cash : r.cash + r.bonus) }}</b>
        </div>
      </div>
    </template>
  </MePage>
</template>

<style scoped>
.store { display: grid; gap: 10px; width: 100%; padding: 14px 16px; border: 0; text-align: left; font: inherit; color: inherit; cursor: pointer; }
.sh { display: flex; align-items: center; gap: 8px; }
.sh .nm { flex: 1; min-width: 0; font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.sh .tot { font-weight: 700; color: var(--red-deep); font-variant-numeric: tabular-nums; }
.two { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; }
.two span { display: grid; gap: 2px; padding: 8px 10px; border-radius: 10px; background: #faf5ec; }
.two small { color: var(--muted); font-size: 12px; }
.two b { font-variant-numeric: tabular-nums; font-weight: 600; }
.gold { color: #a86a12; }

.total { display: grid; gap: 4px; padding: 16px; color: #fff; background: var(--motif) 0 0 / 56px 56px, linear-gradient(120deg, var(--red-deep) 0%, #c0503a 100%); }
.total .big { font-size: 28px; font-weight: 600; line-height: 1.15; font-variant-numeric: tabular-nums; }
.tiles { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; margin-top: 8px; }
.tile { display: grid; gap: 2px; justify-items: start; padding: 10px 12px; border-radius: 12px; border: 1px solid rgba(255, 255, 255, .35);
  background: rgba(255, 255, 255, .12); color: #fff; font: inherit; text-align: left; cursor: pointer; }
.tile.on { background: #fff; color: var(--red-deep); border-color: #fff; }
.tile small { font-size: 12px; opacity: .9; }
.tile b { font-size: 18px; font-weight: 600; font-variant-numeric: tabular-nums; }
.tile span { font-size: 12px; opacity: .85; }
.total .tag { justify-self: start; margin-top: 6px; background: rgba(255, 255, 255, .2); color: #fff; }
.acts { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 8px; }
.total .use { padding: 4px 14px; border-radius: 999px; background: #fff; color: var(--red-deep); font-weight: 600; text-decoration: none; }
.total .use.alt { background: rgba(255, 255, 255, .18); color: #fff; border: 1px solid rgba(255, 255, 255, .6); }

.rules h3 { margin: 0 0 8px; font-size: 15px; }
.rules dl { display: grid; grid-template-columns: auto 1fr; gap: 6px 10px; margin: 0 0 8px; font-size: 13px; }
.rules dt { font-weight: 600; white-space: nowrap; }
.rules dd { margin: 0; color: var(--muted); }
.rules ol { margin: 0; padding-left: 18px; display: grid; gap: 4px; }

.tabs { display: flex; gap: 6px; margin-bottom: 4px; }
.tabs button { padding: 5px 14px; border-radius: 999px; border: 1px solid var(--line, #e6dccb); background: #fff; font: inherit; font-size: 13px; color: inherit; cursor: pointer; }
.tabs button.on { background: var(--red-deep); border-color: var(--red-deep); color: #fff; }
.entry { display: flex; align-items: center; gap: 10px; padding: 10px 0; border-bottom: 1px solid #f0f1f3; }
.entry:last-child { border-bottom: 0; }
.entry b { font-weight: 600; font-variant-numeric: tabular-nums; white-space: nowrap; } .entry .in { color: var(--brand-dark); } .entry .out { color: var(--ink); }
.empty { padding: 18px 0; text-align: center; color: var(--muted); }
</style>
