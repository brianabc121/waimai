<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { get, post } from '../api'
import { money, t, toMajor, toMinor } from '../i18n'
import { currentRole, session } from '../session'
import type { Cash } from '../types'
import '../officeStrings'
import '../checkout'

const bid = session.branchId!
const cur = currentRole()?.currency ?? 'PHP'
const rows = ref<Cash[]>([])
const input = reactive<Record<number, string>>({})
const error = ref('')

async function load() {
  rows.value = (await get<{ collections: Cash[] }>(`/merchant/branches/${bid}/cash-collections`)).collections
  for (const r of rows.value) if (input[r.id] === undefined) input[r.id] = String(toMajor(r.amount_collected))
}
onMounted(load)

// a status in the rail, and numbers for the tiles (just the rows added up)
const STATES = ['pending', 'reconciled', 'discrepancy'] as const
const st = ref('')
const shown = computed(() => rows.value.filter(r => !st.value || r.reconcile_status === st.value))
const pending = computed(() => rows.value.filter(r => r.reconcile_status === 'pending'))
const sum = (xs: Cash[], f: (r: Cash) => number) => xs.reduce((n, r) => n + f(r), 0)

async function reconcile(r: Cash) {
  error.value = ''
  try { await post(`/merchant/cash-collections/${r.id}/reconcile`, { amount_returned: toMinor(input[r.id]) }) }
  catch (e) { error.value = (e as Error).message }
  await load()
}

// An order the rider collected too little for stays unpaid until the store
// says what happened to the rest: it came in afterwards, or it is let go.
const settling = ref<{ row: Cash; action: 'collected' | 'waived' } | null>(null), settleNote = ref(''), busy = ref(false)
function ask(r: Cash, action: 'collected' | 'waived') { settling.value = { row: r, action }; settleNote.value = ''; error.value = '' }
async function settle() {
  const s = settling.value
  if (!s || busy.value) return
  if (s.action === 'waived' && !settleNote.value.trim()) { error.value = t('cs.short_why'); return }
  busy.value = true; error.value = ''
  try {
    await post(`/merchant/cash-collections/${s.row.id}/settle-short`, { action: s.action, note: settleNote.value.trim() })
    settling.value = null
  } catch (e) { error.value = (e as Error).message }
  finally { busy.value = false }
  await load()
}
</script>

<template>
  <div class="ox warm">
  <div class="page ox-page">
    <div class="ox-head">
      <div><h1>{{ t('cash') }}</h1><p class="ox-sub">{{ t('ox.cs.sub') }}</p></div>
      <div class="ox-acts"><router-link class="btn" to="/merchant/reconcile">{{ t('rc.nav') }}</router-link></div>
    </div>
    <div v-if="error" class="banner bad">{{ error }}</div>

    <div class="ox-stats">
      <div class="ox-stat"><span class="l">{{ t('c.pending') }}</span><b class="v" :class="{ red: pending.length > 0 }">{{ pending.length }}</b>
        <span class="s">{{ t('ox.cs.pending_due', { m: money(sum(pending, r => r.amount_collected), cur) }) }}</span></div>
      <div class="ox-stat"><span class="l">{{ t('due') }}</span><b class="v">{{ money(sum(rows, r => r.amount_due), cur) }}</b><span class="s">{{ t('ox.cs.listed', { n: rows.length }) }}</span></div>
      <div class="ox-stat"><span class="l">{{ t('collected') }}</span><b class="v">{{ money(sum(rows, r => r.amount_collected), cur) }}</b></div>
      <div class="ox-stat"><span class="l">{{ t('returned') }}</span><b class="v">{{ money(sum(rows, r => r.amount_returned ?? 0), cur) }}</b></div>
    </div>

    <div class="ox-body">
      <aside class="ox-rail">
        <div class="ox-rail-h">{{ t('ox.pr.state') }}</div>
        <div class="ox-rail-list">
          <button class="ox-cat" :class="{ on: st === '' }" @click="st = ''"><span class="nm">{{ t('ox.cs.all') }}</span><span class="n">{{ rows.length }}</span></button>
          <button v-for="k in STATES" :key="k" class="ox-cat" :class="{ on: st === k }" @click="st = k"><span class="nm">{{ t('c.' + k) }}</span><span class="n">{{ rows.filter(r => r.reconcile_status === k).length }}</span></button>
        </div>
        <p class="ox-rail-tip">{{ t('ox.cs.tip') }}</p>
      </aside>
      <section class="ox-panel">
        <div v-if="rows.length === 0" class="ox-empty">{{ t('no_cash') }}</div>
        <div v-else class="ox-scroll">
          <table class="data">
            <thead><tr><th>{{ t('order_no') }}</th><th>{{ t('rider') }}</th><th class="num">{{ t('due') }}</th><th class="num">{{ t('collected') }}</th><th>{{ t('returned') }}</th><th class="right">{{ t('ox.pr.state') }}</th></tr></thead>
            <tbody>
              <tr v-for="r in shown" :key="r.id">
                <td><b class="ox-code">{{ r.public_no }}</b><div class="muted small">{{ new Date(r.created_at).toLocaleString() }}</div></td>
                <td>{{ r.rider_name }}</td>
                <td class="num"><span class="ox-money">{{ money(r.amount_due, cur) }}</span></td>
                <td class="num"><span class="ox-money">{{ money(r.amount_collected, cur) }}</span>
                  <div v-if="r.amount_collected !== r.amount_due" class="small" style="color:var(--bad)">{{ t('diff') }} {{ money(r.amount_collected - r.amount_due, cur) }}</div>
                  <div v-if="r.short" class="cs-short" data-testid="cash-short">
                    <span class="small">{{ t('cs.short', { m: money(r.short, cur) }) }}</span>
                    <button class="btn small" data-testid="cash-short-collected" @click="ask(r, 'collected')">{{ t('cs.short_collected') }}</button>
                    <button class="btn small" data-testid="cash-short-waived" @click="ask(r, 'waived')">{{ t('cs.short_waived') }}</button>
                  </div>
                  <div v-else-if="r.late_cash" class="small muted">{{ t('cs.late_done', { m: money(r.late_cash, cur) }) }}</div>
                  <div v-else-if="r.waived" class="small muted">{{ t('cs.waived_done', { m: money(r.waived, cur) }) }}</div></td>
                <td style="width:170px">
                  <input v-if="r.reconcile_status === 'pending'" type="number" min="0" step="0.01" v-model="input[r.id]" />
                  <span v-else class="ox-money">{{ money(r.amount_returned ?? 0, cur) }}</span>
                </td>
                <td class="right">
                  <button v-if="r.reconcile_status === 'pending'" class="btn small primary" @click="reconcile(r)">{{ t('reconcile') }}</button>
                  <span v-else class="tag ox-pill" :class="r.reconcile_status === 'reconciled' ? 'ok' : 'bad'">{{ t('c.' + r.reconcile_status) }}</span>
                </td>
              </tr>
              <tr v-if="!shown.length"><td colspan="6" class="ox-empty">{{ t('no_cash') }}</td></tr>
            </tbody>
          </table>
        </div>
      </section>
    </div>
  </div>
  <div v-if="settling" class="sheet-mask" @click.self="settling = null">
    <form class="sheet center" data-testid="cash-short-sheet" @submit.prevent="settle">
      <div class="sheet-head"><h2>{{ t(settling.action === 'collected' ? 'cs.short_collected' : 'cs.short_waived') }}</h2></div>
      <div class="sheet-body stack">
        <p class="small" style="margin:0">{{ t(settling.action === 'collected' ? 'cs.short_collected_tip' : 'cs.short_waived_tip', { no: settling.row.public_no, m: money(settling.row.short ?? 0, cur) }) }}</p>
        <label class="field"><span>{{ t(settling.action === 'waived' ? 'cs.short_why' : 'cs.short_note') }}</span><input v-model="settleNote" maxlength="200" data-testid="cash-short-note" /></label>
        <div v-if="error" class="banner bad">{{ error }}</div>
      </div>
      <div class="sheet-foot row">
        <button type="button" class="btn" @click="settling = null">{{ t('cancel') }}</button>
        <button class="btn primary" :disabled="busy" data-testid="cash-short-ok">{{ t('cs.ok') }}</button>
      </div>
    </form>
  </div>
  </div>
</template>

<style scoped>
.cs-short { display: flex; flex-wrap: wrap; gap: 6px; align-items: center; justify-content: flex-end; margin-top: 4px; color: var(--bad); }
</style>
