<script setup lang="ts">
// Reconciliation (docs/CHECKOUT_DISCOUNT_DESIGN.md §8): what came in by payment
// method, the owner's check of each online payment against the wallet or bank
// app, the cash counted in the drawer, and who gave which discounts.
import { computed, onMounted, reactive, ref, watch } from 'vue'
import '../officeStrings'
import { useRoute, useRouter } from 'vue-router'
import { api, get } from '../api'
import '../bills'
import { channelName, pctText } from '../checkout'
import { locale, money, pick, t, toMinor } from '../i18n'
import { session } from '../session'
import type { PayChannel } from '../types'
import { addDays, addMonths, clock, dayOf, monthEnd, monthStart } from './staff/time'
import ReconPlatforms, { type ReconPlatform, type ReconType } from './ReconPlatforms.vue'
import { platformName } from '../platforms'
import { brandsHere, multiBrand, useBrandPick } from '../brands'
import BrandSeg from './brands/BrandSeg.vue'
import '../stored'

interface Row { pay_type: string; channel_id: number; platform_id: number; name: string; names: Record<string, string>; count: number; amount: number
  dine: number; takeaway: number; ok: number; problem: number; unchecked: number }
interface CashCount { day: string; expected: number; counted: number; note: string; counted_by: string; at: string }
interface Summary { timezone: string; currency: string; count: number; total: number; rows: Row[]; problems: number
  cash: { counter: number; rider_collected: number; rider_returned: number; rider_pending: number; refunded: number; expected: number; top_up?: number; late_cash?: number; waived?: number; rider_short?: number
    stored_in?: number; stored_out?: number }
  refunds: { count: number; total: number; cash: number; to_credit?: number; to_stored?: number }; net: number
  stored?: { count: number; paid: number; bonus: number; cash: number; card: number; online: number; revoked: number; revoked_cash: number; revoked_bonus: number
    spent_principal: number; spent_bonus: number; cashouts: number; paid_out: number; cash_out?: number; cashout_cash: number; clawback: number }
  discounts: { promotion: number; coupon: number; staff: number; round_off: number; platform_merchant: number; platform_subsidy: number; total: number }
  cash_count: CashCount | null; platforms: ReconPlatform[]; types: ReconType[]
  whole: boolean; brands: { brand_id: number; name: string; code: string; count: number; amount: number; refunds: number; platform: number; commission: number }[] }
interface Payment { order_id: number; public_no: string; paid_at: string; service_type: string; table_name: string; channel_id: number
  method: string; names: Record<string, string>; amount: number; staff: string; status: '' | 'ok' | 'problem'; note: string; checked_by: string; checked_at: string | null }
interface Entry { order_id: number; public_no: string; at: string; service_type: string; table_name: string; percent_bp: number; mode: string
  staff: number; round_off: number; total: number; reason: string; by: string; approved_by: string }
interface DiscLog { entries: Entry[]; by_staff: { name: string; count: number; amount: number }[]
  by_promo: { id: number; name: string; name_en: string; scope: string; count: number; amount: number }[] }

const route = useRoute(), router = useRouter()
const bid = session.branchId!
const tz = ref('Asia/Manila')
const today = computed(() => dayOf(Date.now(), tz.value))
type Preset = 'today' | 'yesterday' | 'week' | 'month' | 'last_month' | 'custom'
const preset = ref<Preset>('today')
const from = ref(''), to = ref('')
type Tab = 'sum' | 'online' | 'disc'
const tab = computed<Tab>(() => route.query.tab === 'online' || route.query.tab === 'disc' ? route.query.tab : 'sum')
const setTab = (k: Tab) => router.replace({ query: k === 'sum' ? {} : { tab: k } })
const error = ref('')
const sum = ref<Summary | null>(null)
const pays = ref<Payment[] | null>(null)
const disc = ref<DiscLog | null>(null)
const filt = reactive({ channel: 0, status: '' })

function choose(p: Preset) {
  const d = today.value
  preset.value = p
  if (p === 'today') { from.value = to.value = d }
  else if (p === 'yesterday') { from.value = to.value = addDays(d, -1) }
  else if (p === 'week') { from.value = addDays(d, -6); to.value = d }
  else if (p === 'month') { from.value = monthStart(d); to.value = d }
  else if (p === 'last_month') { from.value = addMonths(d, -1); to.value = monthEnd(addMonths(d, -1)) }
}
// one brand, or every brand together (docs/MULTI_BRAND_DESIGN.md §12)
const { pick: brandPick } = useBrandPick('reconcile', brandsHere, true)
const q = () => `from=${from.value}&to=${to.value}` + (brandPick.value ? `&brand=${brandPick.value}` : '')
async function load() {
  if (!from.value || !to.value || from.value > to.value) return
  error.value = ''
  try {
    const s = await get<Summary>(`/merchant/branches/${bid}/reconcile?${q()}`)
    if (s.timezone !== tz.value) { tz.value = s.timezone; if (preset.value !== 'custom') { choose(preset.value); return } }
    sum.value = s
    if (s.cash_count) Object.assign(count, { text: String(s.cash_count.counted / 100), note: s.cash_count.note })
    else Object.assign(count, { text: '', note: '' })
    if (tab.value === 'online') await loadOnline()
    if (tab.value === 'disc') disc.value = await get<DiscLog>(`/merchant/branches/${bid}/reconcile/discounts?${q()}`)
  } catch (e) { error.value = (e as Error).message }
}
async function loadOnline() {
  pays.value = (await get<{ payments: Payment[] }>(`/merchant/branches/${bid}/reconcile/online?${q()}&channel=${filt.channel}&status=${filt.status}`)).payments
}
onMounted(() => { choose('today'); void load() })
watch([from, to, tab, brandPick], () => { void load() })
watch(filt, () => { void loadOnline().catch(e => (error.value = (e as Error).message)) })

const cur = computed(() => sum.value?.currency ?? 'PHP')
const m = (v: number) => money(v, cur.value)
const exportHref = computed(() => `/api/v1/merchant/branches/${bid}/reconcile?${q()}&format=xlsx&lang=${locale.value}`)
const asChannel = (x: { name: string; names: Record<string, string> }) => channelName({ name: x.name, names: x.names } as PayChannel)
const rowName = (r: Row) => r.pay_type === 'online' ? `${t('ck.pt.online')} · ${asChannel(r)}`
  : r.pay_type === 'platform' ? `${t('ck.pt.platform')} · ${platformName(r)}` : t('ck.pt.' + r.pay_type)
const onlineRows = computed(() => (sum.value?.rows ?? []).filter(r => r.pay_type === 'online'))
const when = (iso: string) => `${dayOf(iso, tz.value).slice(5)} ${clock(iso, tz.value)}`
const where = (x: { service_type: string; table_name: string }) => x.service_type === 'dine_in' ? t('rc.table', { t: x.table_name }) : t('bl.' + x.service_type)

// ---- online payments: checked one by one ----
const writing = ref<number | null>(null)
const badNote = ref('')
async function mark(ids: number[], status: '' | 'ok' | 'problem', note = '') {
  error.value = ''
  try {
    await api('PUT', `/merchant/branches/${bid}/reconcile/checks`, { order_ids: ids, status, note })
    writing.value = null; badNote.value = ''
    await load()
  } catch (e) { error.value = (e as Error).message }
}
const unchecked = computed(() => (pays.value ?? []).filter(p => !p.status).map(p => p.order_id))

// ---- the cash count (one day) ----
const count = reactive({ text: '', note: '', busy: false })
const counted = computed(() => count.text === '' ? null : toMinor(count.text))
async function saveCount() {
  if (counted.value == null) return
  count.busy = true; error.value = ''
  try {
    await api('PUT', `/merchant/branches/${bid}/reconcile/cash-count`, { day: from.value, counted: counted.value, note: count.note })
    await load()
  } catch (e) { error.value = (e as Error).message }
  count.busy = false
}
const diffText = (d: number) => (d > 0 ? '+' : d < 0 ? '−' : '') + money(Math.abs(d), cur.value)

const kindText = (e: Entry) => [e.percent_bp ? pctText(e.percent_bp) : '', e.mode === 'round' ? t('ck.round_off') : '',
  e.mode === 'set_total' ? (e.total === 0 ? t('ck.free') : t('ck.adj_set')) : ''].filter(Boolean).join(' + ')
</script>

<template>
  <div class="ox warm">
  <div class="page ox-page rc" data-testid="reconcile">
    <div class="ox-head">
      <div><h1>{{ t('rc.nav') }}</h1><p class="ox-sub">{{ t('ox.rc.sub') }}</p></div>
      <div class="ox-acts"><router-link class="btn" to="/merchant/cash">{{ t('cash') }}</router-link><a class="btn" data-testid="rc-export" :href="exportHref" download>{{ t('rc.export') }}</a></div>
    </div>
    <div class="ox-tabs" role="tablist">
      <button type="button" role="tab" :class="{ on: tab === 'sum' }" data-testid="rc-tab-sum" @click="setTab('sum')">{{ t('rc.tab_sum') }}</button>
      <button type="button" role="tab" :class="{ on: tab === 'online' }" data-testid="rc-tab-online" @click="setTab('online')">{{ t('rc.tab_online') }}
        <span v-if="sum?.problems" class="badge bad">{{ sum.problems }}</span></button>
      <button type="button" role="tab" :class="{ on: tab === 'disc' }" data-testid="rc-tab-disc" @click="setTab('disc')">{{ t('rc.tab_disc') }}</button>
    </div>
    <div class="ox-tools ox-chips" style="margin:0">
      <button v-for="p in (['today', 'yesterday', 'week', 'month', 'last_month', 'custom'] as Preset[])" :key="p" :class="{ on: preset === p }"
        :data-preset="p" @click="p === 'custom' ? preset = 'custom' : choose(p)">{{ t('bl.p.' + p) }}</button>
      <span v-if="preset === 'custom'" class="custom"><input v-model="from" type="date" :max="to"> ~ <input v-model="to" type="date" :min="from" :max="today"></span>
    </div>
    <div v-if="multiBrand()" class="ox-tools" style="margin:0"><BrandSeg v-model="brandPick" :brands="brandsHere()" all data-testid="rc-brand" /></div>
    <div v-if="error" class="banner bad">{{ error }}</div>
    <div v-if="sum?.problems" class="banner bad" data-testid="rc-problems">{{ t('rc.c.problems', { n: sum.problems }) }}
      <a href="#" @click.prevent="setTab('online'); filt.status = 'problem'">›</a></div>

    <!-- takings -->
    <template v-if="tab === 'sum' && sum">
      <p class="muted small" style="margin:0">{{ t('rc.hint') }}</p>
      <div class="ox-stats" data-testid="rc-cards">
        <div class="ox-stat"><span class="l">{{ t('rc.c.total') }}</span><b class="v red" data-testid="rc-total">{{ m(sum.total) }}</b><small class="s">{{ t('rc.c.count', { n: sum.count }) }}</small></div>
        <div v-for="r in sum.rows" :key="r.pay_type + r.channel_id + '-' + r.platform_id" class="ox-stat"><span class="l">{{ rowName(r) }}</span><b class="v">{{ m(r.amount) }}</b>
          <small class="s">{{ t('rc.c.count', { n: r.count }) }}</small></div>
        <div class="ox-stat"><span class="l">{{ t('rc.c.discount') }}</span><b class="v neg">{{ m(sum.discounts.total) }}</b>
          <small class="s">{{ t('rc.d.promo') }} {{ m(sum.discounts.promotion) }} · {{ t('rc.d.staff') }} {{ m(sum.discounts.staff) }} · {{ t('rc.d.round') }} {{ m(sum.discounts.round_off) }}<template v-if="sum.discounts.platform_merchant">
            · {{ t('po.merchant') }} {{ m(sum.discounts.platform_merchant) }}</template></small></div>
        <div v-if="sum.refunds.count" class="ox-stat" data-testid="rc-refunds"><span class="l">{{ t('rc.c.refunds') }}</span><b class="v neg">-{{ m(sum.refunds.total) }}</b>
          <small class="s">{{ t('rc.c.count', { n: sum.refunds.count }) }} · {{ t('rc.c.net', { v: m(sum.net) }) }}</small>
          <small v-if="sum.refunds.to_credit" class="s" data-testid="rc-to-credit">{{ t('rc.c.to_credit', { v: m(sum.refunds.to_credit) }) }}</small>
          <small v-if="sum.refunds.to_stored" class="s" data-testid="rc-to-stored">{{ t('sv.rc.to_stored', { v: m(sum.refunds.to_stored) }) }}</small></div>
        <div v-if="sum.stored && (sum.stored.count || sum.stored.cashouts || sum.stored.spent_principal + sum.stored.spent_bonus)" class="ox-stat" data-testid="rc-stored">
          <span class="l">{{ t('sv.rc.title') }} · {{ t('sv.rc.topups', { n: sum.stored.count }) }}</span><b class="v">{{ m(sum.stored.paid - sum.stored.revoked) }}</b>
          <small class="s">{{ t('ck.pt.cash') }} {{ m(sum.stored.cash) }} · {{ t('ck.pt.card') }} {{ m(sum.stored.card) }} · {{ t('ck.pt.online') }} {{ m(sum.stored.online) }}<template v-if="sum.stored.revoked">
            · {{ t('sv.rc.revoked') }} -{{ m(sum.stored.revoked) }}</template></small>
          <small class="s">{{ t('sv.rc.given') }} {{ m(sum.stored.bonus - sum.stored.revoked_bonus) }} · {{ t('sv.rc.spent') }} {{ t('sv.rc.spent_s', { p: m(sum.stored.spent_principal), b: m(sum.stored.spent_bonus) }) }}</small>
          <small v-if="sum.stored.cashouts" class="s">{{ t('sv.rc.cashouts', { n: sum.stored.cashouts }) }} -{{ m(sum.stored.paid_out) }}<template v-if="sum.stored.clawback"> · {{ t('sv.rc.clawback', { v: m(sum.stored.clawback) }) }}</template></small>
          <small class="s muted">{{ t('sv.rc.hint') }}</small></div>
      </div>
      <div class="cols">
        <div class="ox-panel ox-scroll">
          <table class="data" data-testid="rc-rows">
            <thead><tr><th>{{ t('rc.col.method') }}</th><th class="r">{{ t('rc.col.count') }}</th><th class="r">{{ t('rc.col.amount') }}</th>
              <th class="r">{{ t('rc.col.dine') }}</th><th class="r">{{ t('rc.col.takeaway') }}</th><th>{{ t('rc.col.check') }}</th></tr></thead>
            <tbody>
              <tr v-for="r in sum.rows" :key="r.pay_type + r.channel_id + '-' + r.platform_id" :data-method="r.pay_type === 'online' || r.pay_type === 'platform' ? r.name : r.pay_type">
                <td><b>{{ rowName(r) }}</b></td><td class="r">{{ r.count }}</td><td class="r"><b>{{ m(r.amount) }}</b></td>
                <td class="r">{{ m(r.dine) }}</td><td class="r">{{ m(r.takeaway) }}</td>
                <td class="small"><a v-if="r.pay_type === 'online'" href="#" :class="{ neg: r.problem }" @click.prevent="filt.channel = r.channel_id; filt.status = ''; setTab('online')">
                  {{ t('rc.check_n', { ok: r.ok, bad: r.problem, un: r.unchecked }) }}</a></td>
              </tr>
              <tr v-if="!sum.rows.length"><td colspan="6" class="muted">{{ t('rc.none') }}</td></tr>
            </tbody>
          </table>
        </div>
        <div v-if="!sum.whole" class="ox-panel stack" data-testid="rc-cash-store">
          <h2>{{ t('rc.cash') }}</h2>
          <p class="muted small" style="margin:0">{{ t('br.cash_store') }}</p>
          <button v-if="multiBrand() && brandPick" class="btn" @click="brandPick = 0">{{ t('br.all') }}</button>
        </div>
        <div v-else class="ox-panel stack">
          <h2>{{ t('rc.cash') }}</h2>
          <div class="kv"><span>{{ t('rc.cash_counter') }}</span><b>{{ m(sum.cash.counter) }}</b></div>
          <div class="kv"><span>{{ t('rc.cash_rider') }}</span><b>{{ m(sum.cash.rider_collected) }}</b></div>
          <div class="kv sub"><span>{{ t('rc.cash_returned') }}</span><span>{{ m(sum.cash.rider_returned) }}</span></div>
          <div class="kv sub"><span>{{ t('rc.cash_pending') }}</span><span :class="{ neg: sum.cash.rider_pending }">{{ m(sum.cash.rider_pending) }}</span></div>
          <div v-if="sum.cash.rider_short" class="kv sub"><span>{{ t('rc.cash_short') }}</span><span class="neg" data-testid="rc-cash-short">{{ m(sum.cash.rider_short) }}</span></div>
          <div v-if="sum.cash.top_up" class="kv sub"><span>{{ t('rc.cash_top_up') }}</span><span data-testid="rc-cash-topup">{{ m(sum.cash.top_up) }}</span></div>
          <div v-if="sum.cash.late_cash" class="kv sub"><span>{{ t('rc.cash_late') }}</span><span>{{ m(sum.cash.late_cash) }}</span></div>
          <div v-if="sum.cash.waived" class="kv sub"><span>{{ t('rc.cash_waived') }}</span><span class="neg">{{ m(sum.cash.waived) }}</span></div>
          <div v-if="sum.cash.refunded" class="kv"><span>{{ t('rc.cash_refunded') }}</span><b class="neg" data-testid="rc-cash-refunded">-{{ m(sum.cash.refunded) }}</b></div>
          <div v-if="sum.cash.stored_in" class="kv"><span>{{ t('sv.rc.cash_in') }}</span><b data-testid="rc-stored-in">{{ m(sum.cash.stored_in) }}</b></div>
          <div v-if="sum.cash.stored_out" class="kv"><span>{{ t('sv.rc.cash_out') }}</span><b class="neg" data-testid="rc-stored-out">-{{ m(sum.cash.stored_out) }}</b></div>
          <router-link class="small" to="/merchant/cash">{{ t('rc.to_cash') }} ›</router-link>
          <h2 style="margin-top:8px">{{ t('rc.count') }}</h2>
          <template v-if="from === to">
            <p class="muted small" style="margin:0">{{ t('rc.count_hint') }}</p>
            <div class="kv"><span>{{ t('rc.expected') }}</span><b data-testid="rc-expected">{{ m(sum.cash.expected) }}</b></div>
            <label class="field" style="margin:4px 0 0"><span>{{ t('rc.counted') }}</span>
              <input v-model="count.text" inputmode="decimal" data-testid="rc-counted" /></label>
            <div v-if="counted != null" class="kv"><span>{{ t('rc.diff') }}</span>
              <b data-testid="rc-diff" :class="{ neg: counted !== sum.cash.expected }">{{ diffText(counted - sum.cash.expected) }}</b></div>
            <input v-model="count.note" maxlength="200" :placeholder="t('rc.count_note')" />
            <button class="btn primary" :disabled="counted == null || count.busy" data-testid="rc-count-save" @click="saveCount">{{ t('rc.count_save') }}</button>
            <div v-if="sum.cash_count" class="muted small" data-testid="rc-counted-by">{{ t('rc.counted_by', { name: sum.cash_count.counted_by || t('rc.someone'), at: when(sum.cash_count.at) }) }}
              · {{ t('rc.counted') }} {{ m(sum.cash_count.counted) }} · {{ t('rc.diff') }} {{ diffText(sum.cash_count.counted - sum.cash_count.expected) }}</div>
          </template>
          <p v-else class="muted small" style="margin:0">{{ t('rc.count_day_only') }}</p>
        </div>
      </div>
      <div v-if="sum.brands.length > 1" class="ox-panel ox-scroll" data-testid="rc-brands">
        <div class="ox-panel-h"><h2>{{ t('br.by_brand') }}</h2><span class="muted small">{{ t('br.by_brand_hint') }}</span></div>
        <table class="data">
          <thead><tr><th>{{ t('br.title') }}</th><th class="r">{{ t('rc.col.count') }}</th><th class="r">{{ t('rc.col.amount') }}</th>
            <th class="r">{{ t('rc.c.refunds') }}</th><th class="r">{{ t('br.platform_owe') }}</th><th class="r">{{ t('po.commission') }}</th></tr></thead>
          <tbody>
            <tr v-for="x in sum.brands" :key="x.brand_id" :data-brand="x.code">
              <td><b>{{ x.name }}</b> <span class="muted small">{{ x.code }}</span></td><td class="r">{{ x.count }}</td><td class="r"><b>{{ m(x.amount) }}</b></td>
              <td class="r">{{ x.refunds ? '−' + m(x.refunds) : '—' }}</td><td class="r">{{ x.platform ? m(x.platform) : '—' }}</td>
              <td class="r">{{ x.commission ? '−' + m(x.commission) : '—' }}</td>
            </tr>
            <tr class="sum"><td>{{ t('rc.c.total') }}</td><td class="r">{{ sum.count }}</td><td class="r"><b>{{ m(sum.total) }}</b></td>
              <td class="r">{{ sum.refunds.total ? '−' + m(sum.refunds.total) : '—' }}</td><td></td><td></td></tr>
          </tbody>
        </table>
      </div>
      <ReconPlatforms :several="multiBrand()" :bid="bid" :from="from" :to="to" :tz="tz" :currency="cur" :total="sum.total" :platforms="sum.platforms ?? []" :types="sum.types ?? []" @changed="load" />
    </template>

    <!-- online payments -->
    <template v-if="tab === 'online' && pays">
      <div class="ox-panel">
      <div class="ox-tools">
        <select v-model.number="filt.channel" data-testid="rc-f-channel">
          <option :value="0">{{ t('rc.f.all_methods') }}</option>
          <option v-for="r in onlineRows" :key="r.channel_id" :value="r.channel_id">{{ asChannel(r) }}</option></select>
        <span class="ox-chips"><button v-for="s in ['', 'unchecked', 'ok', 'problem']" :key="s" :class="{ on: filt.status === s }" :data-status="s" @click="filt.status = s">{{ t('rc.f.' + (s || 'all')) }}</button></span>
        <span class="grow"></span>
        <button v-if="unchecked.length" class="btn" data-testid="rc-mark-all" @click="mark(unchecked, 'ok')">{{ t('rc.mark_all', { n: unchecked.length }) }}</button>
      </div>
      <div class="ox-scroll">
        <table class="data" data-testid="rc-online">
          <thead><tr><th>{{ t('rc.col.time') }}</th><th>{{ t('rc.col.order') }}</th><th>{{ t('rc.col.method') }}</th><th class="r">{{ t('rc.col.amount') }}</th>
            <th>{{ t('rc.col.staff') }}</th><th>{{ t('rc.col.check') }}</th><th></th></tr></thead>
          <tbody>
            <template v-for="p in pays" :key="p.order_id">
              <tr :data-order="p.public_no" :class="'st-' + (p.status || 'none')">
                <td class="nowrap">{{ when(p.paid_at) }}</td>
                <td><router-link :to="`/merchant/orders?order=${p.order_id}`">{{ p.public_no }}</router-link><div class="small muted">{{ where(p) }}</div></td>
                <td>{{ asChannel({ name: p.method, names: p.names }) }}</td>
                <td class="r"><b>{{ m(p.amount) }}</b></td>
                <td>{{ p.staff }}</td>
                <td><span class="tag ox-pill" :class="p.status === 'ok' ? 'ok' : p.status === 'problem' ? 'bad' : ''" data-testid="rc-status">{{ t('rc.st.' + (p.status || 'none')) }}</span>
                  <div v-if="p.note" class="small neg">{{ p.note }}</div>
                  <div v-if="p.checked_by" class="small muted">{{ t('rc.checked_by', { name: p.checked_by, at: when(p.checked_at!) }) }}</div></td>
                <td class="nowrap r">
                  <template v-if="!p.status">
                    <button class="btn small primary" data-testid="rc-ok" @click="mark([p.order_id], 'ok')">{{ t('rc.mark_ok') }}</button>
                    <button class="btn small" data-testid="rc-bad" @click="writing = p.order_id; badNote = ''">{{ t('rc.mark_bad') }}</button>
                  </template>
                  <button v-else class="btn small" data-testid="rc-undo" @click="mark([p.order_id], '')">{{ t('rc.mark_undo') }}</button>
                </td>
              </tr>
              <tr v-if="writing === p.order_id"><td colspan="7">
                <form class="row" style="gap:8px" @submit.prevent="mark([p.order_id], 'problem', badNote)">
                  <input v-model="badNote" class="grow" maxlength="200" :placeholder="t('rc.bad_note')" data-testid="rc-bad-note" />
                  <button class="btn danger" :disabled="!badNote.trim()" data-testid="rc-bad-save">{{ t('rc.mark_bad') }}</button>
                  <button type="button" class="btn" @click="writing = null">{{ t('ck.cancel') }}</button>
                </form></td></tr>
            </template>
            <tr v-if="!pays.length"><td colspan="7" class="ox-empty">{{ t('rc.no_online') }}</td></tr>
          </tbody>
        </table>
      </div>
      </div>
    </template>

    <!-- discounts -->
    <template v-if="tab === 'disc' && disc">
      <div class="cols">
        <div class="ox-panel ox-scroll">
          <table class="data" data-testid="rc-disc">
            <thead><tr><th>{{ t('rc.col.time') }}</th><th>{{ t('rc.col.order') }}</th><th>{{ t('rc.col.kind') }}</th><th class="r">{{ t('rc.col.off') }}</th>
              <th class="r">{{ t('rc.col.paid') }}</th><th>{{ t('rc.col.reason') }}</th><th>{{ t('rc.col.by') }}</th><th>{{ t('rc.col.approved') }}</th></tr></thead>
            <tbody>
              <tr v-for="e in disc.entries" :key="e.order_id" :data-order="e.public_no">
                <td class="nowrap">{{ when(e.at) }}</td>
                <td><router-link :to="`/merchant/orders?order=${e.order_id}`">{{ e.public_no }}</router-link><div class="small muted">{{ where(e) }}</div></td>
                <td>{{ kindText(e) }}</td><td class="r neg">−{{ m(e.staff + e.round_off) }}</td><td class="r">{{ m(e.total) }}</td>
                <td>{{ e.reason }}</td><td>{{ e.by || t('rc.someone') }}</td><td>{{ e.approved_by }}</td>
              </tr>
              <tr v-if="!disc.entries.length"><td colspan="8" class="muted">{{ t('rc.no_disc') }}</td></tr>
            </tbody>
          </table>
        </div>
        <div class="stack">
          <div class="ox-panel stack" data-testid="rc-by-staff">
            <h2>{{ t('rc.by_staff') }}</h2>
            <div v-for="s in disc.by_staff" :key="s.name" class="kv"><span>{{ s.name || t('rc.someone') }}</span><b>{{ t('rc.staff_line', { n: s.count, amt: m(s.amount) }) }}</b></div>
            <div v-if="!disc.by_staff.length" class="muted small">—</div>
          </div>
          <div class="ox-panel stack" data-testid="rc-by-promo">
            <h2>{{ t('rc.by_promo') }}</h2>
            <div v-for="p in disc.by_promo" :key="p.id" class="kv"><span><span class="tag">{{ t(p.scope === 'dine_in' ? 'ck.tab_dine' : 'ck.tab_delivery') }}</span> {{ pick(p.name, p.name_en) }}</span>
              <b>{{ t('rc.staff_line', { n: p.count, amt: m(p.amount) }) }}</b></div>
            <div v-if="!disc.by_promo.length" class="muted small">—</div>
          </div>
        </div>
      </div>
    </template>
  </div>
  </div>
</template>

<style scoped>
.rc .cols h2 { color: #2f1810; }
.ox-stat .v.neg { color: #b8392a; }
.custom input { width: auto; }
.r { text-align: end; white-space: nowrap; font-variant-numeric: tabular-nums; }
.nowrap { white-space: nowrap; }
.neg { color: #b8392a; }
.cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(170px, 1fr)); gap: 10px; }
.cards .card { display: flex; flex-direction: column; gap: 2px; padding: 12px 14px; }
.cards span { font-size: 12.5px; color: var(--muted); } .cards b { font-size: 21px; font-variant-numeric: tabular-nums; } .cards small { font-size: 12px; color: var(--muted); }
.cols { display: grid; gap: 14px; } @media (min-width: 900px) { .cols { grid-template-columns: minmax(0, 3fr) minmax(0, 2fr); align-items: start; } }
.cols h2 { margin: 0; font-size: 16px; }
.kv { display: flex; justify-content: space-between; gap: 10px; }
.kv.sub { padding-inline-start: 14px; color: var(--muted); font-size: 13px; }
.badge.bad { display: inline-block; min-width: 18px; padding: 0 5px; border-radius: 9px; background: #b8392a; color: #fff; font-size: 11px; line-height: 18px; margin-inline-start: 4px; }
tr.st-problem td { background: #fff3f1; }
tr.sum td { font-weight: 700; border-top: 2px solid var(--line); }
@media (max-width: 600px) { .cards { grid-template-columns: 1fr 1fr; } }
</style>
