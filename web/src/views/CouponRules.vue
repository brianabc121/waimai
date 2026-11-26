<script setup lang="ts">
// The store's coupons: the welcome-back coupon given after a customer's first
// completed order (docs/COUPON_DESIGN.md §3).
import { computed, reactive, ref, watch } from 'vue'
import { get, put } from '../api'
import { couponStub, couponTitle } from '../coupons'
import ClaimBatches from './ClaimBatches.vue'
import { money, t, toMajor, toMinor } from '../i18n'
import { session } from '../session'
import type { CouponKind, RepurchaseRule } from '../types'


// brand: each brand has its own rule and coupons (docs/MULTI_BRAND_DESIGN.md §10); 0 = the main brand
const props = defineProps<{ brand?: number }>()
const bid = session.branchId!
const bq = () => props.brand ? `?brand=${props.brand}` : ''
const rule = ref<RepurchaseRule | null>(null)
const f = reactive({ enabled: false, kind: 'store_free' as CouponKind, value: 20, threshold: 0, valid_days: 30, min_order: 0, monthly_cap: '' as number | '' })
const error = ref(''), saved = ref(false), busy = ref(false)

function fill(r: RepurchaseRule) {
  rule.value = r
  Object.assign(f, { enabled: r.enabled, kind: r.kind, value: toMajor(r.value), threshold: toMajor(r.threshold), valid_days: r.valid_days,
    min_order: toMajor(r.min_order), monthly_cap: r.monthly_cap ?? '' })
}
watch(() => props.brand, async () => {
  saved.value = false
  try { fill(await get<RepurchaseRule>(`/merchant/branches/${bid}/coupons/repurchase${bq()}`)) } catch (e) { error.value = (e as Error).message }
}, { immediate: true })

async function save(enabled = f.enabled) {
  error.value = ''; saved.value = false; busy.value = true
  try {
    fill(await put<RepurchaseRule>(`/merchant/branches/${bid}/coupons/repurchase`, {
      brand_id: props.brand ?? 0, enabled, kind: f.kind, value: toMinor(f.value), threshold: f.kind === 'store_free' ? 0 : toMinor(f.threshold),
      valid_days: Number(f.valid_days), min_order: toMinor(f.min_order), monthly_cap: f.monthly_cap === '' ? null : Number(f.monthly_cap),
    }))
    saved.value = true
  } catch (e) { error.value = (e as Error).message } finally { busy.value = false }
}
const cur = computed(() => rule.value?.currency ?? 'PHP')
const sample = computed(() => ({ kind: f.kind, value: toMinor(f.value), threshold: f.kind === 'store_free' ? 0 : toMinor(f.threshold) }))
const rate = computed(() => rule.value?.stats.issued ? Math.round(rule.value.stats.used * 100 / rule.value.stats.issued) : 0)
</script>

<template>
  <div class="cr" v-if="rule">
    <form class="card stack" data-testid="repurchase-form" @submit.prevent="save()">
      <div class="row between" style="align-items:flex-start">
        <div><h2 style="margin:0">{{ t('cr.title') }}</h2><div class="muted small">{{ t('cr.hint') }}</div></div>
        <button type="button" class="sw-row" data-testid="repurchase-switch" :aria-pressed="f.enabled" @click="save(!f.enabled)" :disabled="busy">
          <b :class="f.enabled ? 'on' : ''">{{ t(f.enabled ? 'cr.on' : 'cr.off') }}</b><i class="sw" :class="{ on: f.enabled }"></i></button>
      </div>
      <div v-if="error" class="banner bad">{{ error }}</div>
      <div v-if="saved" class="banner ok" data-testid="repurchase-saved">{{ t('cr.saved') }}</div>
      <div class="cr-form">
        <span class="l">{{ t('cr.who') }}</span><div><span class="pill on">{{ t('cr.who_v') }}</span><div class="h">{{ t('cr.who_h') }}</div></div>
        <span class="l">{{ t('cr.min') }}</span><div><input v-model="f.min_order" type="number" min="0" step="0.01" data-testid="repurchase-min" /> <span class="h">{{ t('cr.min_h') }}</span></div>
        <span class="l">{{ t('cr.kind') }}</span>
        <div class="kinds">
          <button v-for="k in (['store_free', 'store_threshold', 'store_delivery'] as const)" :key="k" type="button" :class="{ on: f.kind === k }" :data-testid="'repurchase-kind-' + k" @click="f.kind = k">{{ t('cr.k.' + k) }}</button>
        </div>
        <template v-if="f.kind !== 'store_free'">
          <span class="l">{{ t('cr.threshold') }}</span><div><input v-model="f.threshold" type="number" min="0" step="0.01" data-testid="repurchase-threshold" /></div>
        </template>
        <span class="l">{{ t('cr.value') }}</span><div><input v-model="f.value" type="number" min="0" step="0.01" data-testid="repurchase-value" /> <span v-if="f.kind === 'store_delivery'" class="h">{{ t('cr.value_fee_h') }}</span></div>
        <span class="l">{{ t('cr.days') }}</span><div><input v-model="f.valid_days" type="number" min="1" max="90" data-testid="repurchase-days" /> <span class="h">{{ t('cr.days_h') }}</span></div>
        <span class="l">{{ t('cr.cap') }}</span><div><input v-model="f.monthly_cap" type="number" min="1" :placeholder="'—'" /> <span class="h">{{ t('cr.cap_h') }}</span></div>
        <span class="l">{{ t('cr.tell') }}</span><div class="h">{{ t('cr.tell_v') }}</div>
      </div>
      <div class="row"><button class="btn primary" :disabled="busy" data-testid="repurchase-save">{{ t('save') }}</button></div>
    </form>
    <div class="stack side">
      <div class="card"><div class="muted small" style="margin-bottom:8px">{{ t('cr.preview') }}</div>
        <div class="ticket" data-testid="repurchase-preview"><div class="stub"><b>{{ couponStub(sample, cur)[0] }}</b><small>{{ couponStub(sample, cur)[1] }}</small></div>
          <div class="body"><b>{{ t('cp.repurchase') }}</b><span>{{ couponTitle(sample, cur) }}</span><span>{{ t('cr.days_valid', { n: f.valid_days }) }}</span></div></div></div>
      <div class="card" data-testid="repurchase-stats"><div class="row between"><h2 style="margin:0">{{ t('cr.month') }}</h2></div>
        <div class="stats">
          <div><span>{{ t('cr.issued') }}</span><b>{{ rule.stats.issued }}</b></div>
          <div><span>{{ t('cr.used') }}</span><b>{{ rule.stats.used }}</b></div>
          <div><span>{{ t('cr.rate') }}</span><b class="ok">{{ rate }}%</b></div>
          <div><span>{{ t('cr.sales') }}</span><b>{{ money(rule.stats.sales, cur) }}</b></div>
        </div>
        <div class="muted small">{{ t('cr.rest', { open: rule.stats.open, expired: rule.stats.expired, cost: money(rule.stats.cost, cur) }) }}</div>
      </div>
    </div>
    <ClaimBatches class="wide" :currency="cur" :brand="props.brand ?? 0" />
  </div>
</template>

<style scoped>
.cr { display: grid; gap: 14px; } .cr .wide { grid-column: 1 / -1; }
@media (min-width: 900px) { .cr { grid-template-columns: minmax(0, 1fr) 340px; align-items: start; } }
.cr-form { display: grid; grid-template-columns: 150px minmax(0, 1fr); gap: 12px 14px; align-items: center; }
@media (max-width: 560px) { .cr-form { grid-template-columns: 1fr; gap: 4px; } .cr-form .l { margin-top: 8px; } }
.cr-form .l { font-weight: 600; } .cr-form input { width: 120px; } .h { font-size: 12px; color: var(--muted); }
.pill { display: inline-block; border: 1px solid var(--line); border-radius: 8px; padding: 5px 12px; }
.pill.on { background: var(--brand-soft); color: var(--brand-dark, var(--brand)); border-color: #f0b9a0; font-weight: 700; }
.kinds { display: inline-flex; flex-wrap: wrap; border: 1px solid var(--line); border-radius: 8px; overflow: hidden; justify-self: start; }
.kinds button { border: 0; border-inline-start: 1px solid var(--line); background: #fff; padding: 6px 14px; font: inherit; cursor: pointer; }
.kinds button:first-child { border-inline-start: 0; } .kinds button.on { background: var(--brand-soft); color: var(--brand-dark, var(--brand)); font-weight: 700; }
.sw-row { display: inline-flex; align-items: center; gap: 8px; border: 0; background: none; cursor: pointer; font: inherit; padding: 4px; }
.sw-row b { color: var(--muted); } .sw-row b.on { color: var(--ok, #18794e); }
.sw { width: 42px; height: 24px; border-radius: 12px; background: #d6dae0; position: relative; display: inline-block; }
.sw::after { content: ""; position: absolute; top: 3px; inset-inline-start: 3px; width: 18px; height: 18px; border-radius: 50%; background: #fff; transition: left .15s; }
.sw.on { background: var(--brand); } .sw.on::after { inset-inline-start: 21px; }
.ticket { display: flex; border-radius: 12px; overflow: hidden; background: #fffaf2; border: 1px solid #ecd9b8; }
.ticket .stub { width: 100px; flex: none; background: linear-gradient(160deg, #b8392a, #8f251b); color: #fff; display: grid; place-items: center; align-content: center; padding: 10px 4px; text-align: center; }
.ticket .stub b { font-size: 24px; line-height: 1.1; } .ticket .stub small { font-size: 11.5px; opacity: .9; }
.ticket .body { padding: 10px 12px; display: flex; flex-direction: column; gap: 2px; color: #3b1f14; min-width: 0; }
.ticket .body b { font-size: 15px; } .ticket .body span { font-size: 12px; color: #7b6657; }
.stats { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; margin: 10px 0 8px; }
.stats span { display: block; font-size: 12.5px; color: var(--muted); } .stats b { font-size: 21px; font-variant-numeric: tabular-nums; } .stats b.ok { color: var(--ok, #18794e); }
</style>
