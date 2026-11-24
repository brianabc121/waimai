<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { get, post, put } from '../api'
import { money, promoLabel, t, toMajor, toMinor } from '../i18n'
import { currentRole, session } from '../session'
import type { Promotion, Totals } from '../types'
import { useRoute, useRouter } from 'vue-router'
import CouponRules from './CouponRules.vue'
import '../coupons'
import '../checkout'

// Three tabs: delivery promotions, dine-in promotions (the two never mix,
// docs/CHECKOUT_DISCOUNT_DESIGN.md §4) and coupons. The tab is kept in the address.
const route = useRoute(), router = useRouter()
const tab = computed(() => route.query.tab === 'coupons' ? 'coupons' : route.query.tab === 'dine' ? 'dine' : 'promos')
const setTab = (k: string) => { result.value = null; trial.service_type = k === 'dine' ? 'dine_in' : 'delivery'; router.replace({ query: k === 'promos' ? {} : { tab: k } }) }
const scope = computed(() => tab.value === 'dine' ? 'dine_in' : 'delivery')
const dine = computed(() => scope.value === 'dine_in')

const bid = session.branchId!
const cur = currentRole()?.currency ?? 'PHP'
const promos = ref<Promotion[]>([])
const error = ref('')

const all = ref<Promotion[]>([])
async function load() { all.value = (await get<{ promotions: Promotion[] }>(`/merchant/branches/${bid}/promotions`)).promotions }
watch([all, scope], () => { promos.value = all.value.filter(p => (p.scope ?? 'delivery') === scope.value) }, { immediate: true })
onMounted(load)

async function run(fn: () => Promise<unknown>) {
  error.value = ''
  try { await fn(); await load(); return true } catch (e) { error.value = (e as Error).message; return false }
}

// Amounts are typed in major units and percentages as "10" for 10% off.
// Dates use the browser's time zone, which is normally the store's.
interface Form { id: number; scope: 'delivery' | 'dine_in'; name: string; name_en: string; kind: Promotion['kind']; threshold: number; value: number
  cap: number | ''; applies_delivery: boolean; applies_pickup: boolean; starts: string; ends: string
  daily_start: string; daily_end: string; quota: number | ''; per_phone: number | '' }
const blank = (): Form => ({ id: 0, scope: scope.value, name: '', name_en: '', kind: 'amount_off', threshold: 0, value: 0, cap: '', applies_delivery: true,
  applies_pickup: true, starts: '', ends: '', daily_start: '00:00', daily_end: '00:00', quota: '', per_phone: '' })
const form = reactive<{ open: boolean; f: Form }>({ open: false, f: blank() })

const pad = (n: number) => String(n).padStart(2, '0')
function toLocal(iso: string | null) {
  if (!iso) return ''
  const d = new Date(iso)
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}
const toISO = (v: string) => (v ? new Date(v).toISOString() : null)
const num = (v: number | '') => (v === '' || v === null ? null : Number(v))

function edit(p: Promotion | null) {
  form.f = p ? { id: p.id, scope: p.scope ?? 'delivery', name: p.name, name_en: p.name_en, kind: p.kind, threshold: toMajor(p.threshold),
    value: p.kind === 'percent_off' ? p.value / 100 : toMajor(p.value), cap: p.max_discount === null ? '' : toMajor(p.max_discount),
    applies_delivery: p.applies_delivery, applies_pickup: p.applies_pickup, starts: toLocal(p.starts_at), ends: toLocal(p.ends_at),
    daily_start: p.daily_start, daily_end: p.daily_end, quota: p.total_quota ?? '', per_phone: p.per_phone_limit ?? '' } : blank()
  error.value = ''
  form.open = true
}

async function save() {
  const f = form.f
  const dineIn = f.scope === 'dine_in'
  const body = { scope: f.scope, name: f.name, name_en: f.name_en, kind: f.kind, threshold: toMinor(f.threshold),
    value: f.kind === 'percent_off' ? Math.round(Number(f.value) * 100) : f.kind === 'amount_off' ? toMinor(f.value) : 0,
    max_discount: f.kind === 'percent_off' && num(f.cap) ? toMinor(f.cap as number) : null,
    applies_delivery: !dineIn && f.applies_delivery, applies_pickup: !dineIn && f.kind !== 'free_delivery' && f.applies_pickup,
    starts_at: toISO(f.starts), ends_at: toISO(f.ends), daily_start: f.daily_start || '00:00', daily_end: f.daily_end || '00:00',
    total_quota: num(f.quota), per_phone_limit: dineIn ? null : num(f.per_phone) }
  if (await run(() => f.id ? put(`/merchant/branches/${bid}/promotions/${f.id}`, body) : post(`/merchant/branches/${bid}/promotions`, body))) form.open = false
}

function setStatus(p: Promotion, status: string) {
  if (status === 'ended' && !confirm(t('promo_end_confirm'))) return
  void run(() => post(`/merchant/branches/${bid}/promotions/${p.id}/status`, { status }))
}

const statusTag = (p: Promotion) => p.status === 'active' ? (p.live_now ? 'ok' : 'warn') : p.status === 'paused' ? 'warn' : p.status === 'ended' ? 'bad' : ''
const statusText = (p: Promotion) => p.status === 'active' && !p.live_now ? t('promo.not_live') : t('promo.' + p.status)
const when = (p: Promotion) => {
  const parts: string[] = []
  if (p.starts_at || p.ends_at) parts.push(`${p.starts_at ? new Date(p.starts_at).toLocaleString() : '…'} – ${p.ends_at ? new Date(p.ends_at).toLocaleString() : '…'}`)
  if (p.daily_start !== p.daily_end) parts.push(`${t('daily')} ${p.daily_start}–${p.daily_end}`)
  return parts.join(' · ') || t('always')
}

// ---- trial calculation ----
const trial = reactive({ service_type: 'delivery', subtotal: 300, delivery_fee: 49 })
const result = ref<{ rows: { id: number; name: string; status: string; discount: number; reason: string; wins: boolean }[]; totals: Totals } | null>(null)
async function runTrial() {
  error.value = ''
  try {
    result.value = await post(`/merchant/branches/${bid}/promotion-trial`, { service_type: trial.service_type,
      subtotal: toMinor(trial.subtotal), delivery_fee: toMinor(trial.delivery_fee) })
  } catch (e) { error.value = (e as Error).message }
}
</script>

<template>
  <div class="page stack" style="max-width:1000px">
    <div class="row between">
      <h1>{{ t('cr.page') }}</h1>
      <button v-if="tab !== 'coupons'" class="btn primary" data-testid="promo-new" @click="edit(null)">{{ dine ? t('ck.new_dine') : t('new_promo') }}</button>
    </div>
    <div class="subtabs" role="tablist">
      <button type="button" role="tab" :class="{ on: tab === 'promos' }" data-testid="promo-tab" @click="setTab('promos')">{{ t('ck.tab_delivery') }}</button>
      <button type="button" role="tab" :class="{ on: tab === 'dine' }" data-testid="promo-dine-tab" @click="setTab('dine')">{{ t('ck.tab_dine') }}</button>
      <button type="button" role="tab" :class="{ on: tab === 'coupons' }" data-testid="coupon-tab" @click="setTab('coupons')">{{ t('ck.tab_coupons') }}</button>
    </div>
    <CouponRules v-if="tab === 'coupons'" />
    <template v-else>
    <p v-if="dine" class="banner warn small" data-testid="promo-dine-hint">{{ t('ck.dine_hint') }}</p>
    <p v-else class="muted small">{{ t('promos_hint') }} {{ t('ck.delivery_hint') }}</p>
    <div v-if="error && !form.open" class="banner bad">{{ error }}</div>

    <div class="card" style="padding:0;overflow:auto">
      <table class="data">
        <thead><tr><th>{{ t('promo_name') }}</th><th>{{ t('promo_rule') }}</th><th>{{ t('promo_when') }}</th><th>{{ t('promo_usage') }}</th><th></th><th></th></tr></thead>
        <tbody>
          <tr v-for="p in promos" :key="p.id">
            <td><b>{{ p.name }}</b><div class="muted small">{{ p.name_en }}</div></td>
            <td>{{ promoLabel(p, cur) }}
              <div class="muted small" v-if="!dine">{{ [p.applies_delivery && t('delivery'), p.applies_pickup && t('pickup')].filter(Boolean).join(' · ') }}</div></td>
            <td class="small">{{ when(p) }}</td>
            <td>{{ p.used_count }}<span v-if="p.total_quota"> / {{ p.total_quota }}</span>
              <div class="muted small" v-if="p.per_phone_limit">{{ t('per_customer_n', { n: p.per_phone_limit }) }}</div></td>
            <td><span class="tag" :class="statusTag(p)">{{ statusText(p) }}</span></td>
            <td class="right">
              <div class="row wrap" style="justify-content:flex-end;gap:6px" v-if="p.status !== 'ended'">
                <button v-if="p.status === 'draft'" class="btn small primary" @click="setStatus(p, 'active')">{{ t('publish') }}</button>
                <button v-if="p.status === 'active'" class="btn small" @click="setStatus(p, 'paused')">{{ t('pause') }}</button>
                <button v-if="p.status === 'paused'" class="btn small primary" @click="setStatus(p, 'active')">{{ t('resume') }}</button>
                <button class="btn small" @click="edit(p)">{{ t('edit') }}</button>
                <button class="btn small danger" @click="setStatus(p, 'ended')">{{ t('end') }}</button>
              </div>
            </td>
          </tr>
          <tr v-if="promos.length === 0"><td colspan="6" class="muted">{{ t('no_promos') }}</td></tr>
        </tbody>
      </table>
    </div>

    <form class="card stack" @submit.prevent="runTrial">
      <h2>{{ t('trial') }}</h2>
      <p class="muted small">{{ t('trial_hint') }}</p>
      <div class="row wrap">
        <select v-if="!dine" v-model="trial.service_type" style="flex:0 1 120px" :aria-label="t('delivery')">
          <option value="delivery">{{ t('delivery') }}</option><option value="pickup">{{ t('pickup') }}</option>
        </select>
        <label class="row" style="gap:6px">{{ t('subtotal') }}<input type="number" min="0" step="0.01" v-model.number="trial.subtotal" style="width:110px" /></label>
        <label class="row" style="gap:6px" v-if="trial.service_type === 'delivery'">{{ t('delivery_fee') }}<input type="number" min="0" step="0.01" v-model.number="trial.delivery_fee" style="width:110px" /></label>
        <button class="btn primary">{{ t('trial_run') }}</button>
      </div>
      <template v-if="result">
        <table class="data">
          <tbody><tr v-for="r in result.rows" :key="r.id">
            <td>{{ r.name }} <span class="tag" v-if="r.status !== 'active'">{{ t('promo.' + r.status) }}</span></td>
            <td class="price">{{ r.reason ? '—' : '−' + money(r.discount, cur) }}</td>
            <td><span v-if="r.wins" class="tag ok">{{ t('trial_wins') }}</span><span v-else-if="r.reason" class="muted small">{{ t('why.' + r.reason) }}</span></td>
          </tr></tbody>
        </table>
        <div class="row between bold"><span>{{ t('total') }}</span>
          <span>{{ money(result.totals.total, cur) }} <span class="muted small" v-if="result.totals.discount">(−{{ money(result.totals.discount, cur) }})</span></span></div>
      </template>
    </form>

    <div class="sheet-mask" v-if="form.open" @click.self="form.open = false">
      <form class="sheet center" @submit.prevent="save">
        <div class="sheet-head"><h2>{{ form.f.id ? t('edit') : form.f.scope === 'dine_in' ? t('ck.new_dine') : t('new_promo') }}
          <span v-if="form.f.scope === 'dine_in'" class="tag">{{ t('dine_in') }}</span></h2></div>
        <div class="sheet-body">
          <div v-if="error" class="banner bad" style="margin-bottom:12px">{{ error }}</div>
          <div class="grid2">
            <label class="field"><span>{{ t('name_zh') }}</span><input v-model="form.f.name" required :placeholder="t('promo_name_ph')" /></label>
            <label class="field"><span>{{ t('name_en') }}</span><input v-model="form.f.name_en" /></label>
            <label class="field"><span>{{ t('promo_kind') }}</span><select v-model="form.f.kind">
              <option value="amount_off">{{ t('kind.amount_off') }}</option>
              <option value="percent_off">{{ t('kind.percent_off') }}</option>
              <option v-if="form.f.scope !== 'dine_in'" value="free_delivery">{{ t('kind.free_delivery') }}</option></select></label>
            <label class="field"><span>{{ t('promo_threshold') }} ({{ cur }})</span><input type="number" min="0" step="0.01" v-model.number="form.f.threshold" /></label>
            <label class="field" v-if="form.f.kind === 'amount_off'"><span>{{ t('promo_amount') }} ({{ cur }})</span><input type="number" min="0.01" step="0.01" v-model.number="form.f.value" required /></label>
            <label class="field" v-if="form.f.kind === 'percent_off'"><span>{{ t('promo_percent') }}</span><input type="number" min="1" max="99" step="0.5" v-model.number="form.f.value" required /></label>
            <label class="field" v-if="form.f.kind === 'percent_off'"><span>{{ t('promo_cap') }} ({{ cur }})</span><input type="number" min="0" step="0.01" v-model="form.f.cap" :placeholder="t('unlimited')" /></label>
          </div>
          <div class="grid2" v-if="form.f.scope !== 'dine_in'">
            <label class="check"><input type="checkbox" v-model="form.f.applies_delivery" />{{ t('delivery') }}</label>
            <label class="check" v-if="form.f.kind !== 'free_delivery'"><input type="checkbox" v-model="form.f.applies_pickup" />{{ t('pickup') }}</label>
          </div>
          <div class="grid2">
            <label class="field"><span>{{ t('promo_starts') }}</span><input type="datetime-local" v-model="form.f.starts" /></label>
            <label class="field"><span>{{ t('promo_ends') }}</span><input type="datetime-local" v-model="form.f.ends" /></label>
            <label class="field"><span>{{ t('promo_daily_start') }}</span><input type="time" v-model="form.f.daily_start" /></label>
            <label class="field"><span>{{ t('promo_daily_end') }}</span><input type="time" v-model="form.f.daily_end" /></label>
            <label class="field"><span>{{ t('promo_quota') }}</span><input type="number" min="1" step="1" v-model="form.f.quota" :placeholder="t('unlimited')" /></label>
            <label class="field" v-if="form.f.scope !== 'dine_in'"><span>{{ t('promo_per_phone') }}</span><input type="number" min="1" step="1" v-model="form.f.per_phone" :placeholder="t('unlimited')" /></label>
          </div>
          <p class="muted small">{{ t('promo_time_hint') }}</p>
          <p v-if="form.f.scope === 'dine_in'" class="muted small">{{ t('ck.dine_time_hint') }}</p>
        </div>
        <div class="sheet-foot row">
          <button type="button" class="btn grow" @click="form.open = false">{{ t('cancel') }}</button>
          <button class="btn primary grow">{{ t('save') }}</button>
        </div>
      </form>
    </div>
    </template>
  </div>
</template>

<style scoped>
.grid2 { display: grid; grid-template-columns: 1fr 1fr; gap: 0 12px; align-items: center; }
</style>
