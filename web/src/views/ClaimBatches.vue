<script setup lang="ts">
// Coupons customers claim on the store's page (docs/COUPON_DESIGN.md §3.5):
// the store makes a batch, sets how many and how many per person, and can
// pause or end it. Coupons already claimed stay usable until they expire.
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { get, post, put } from '../api'
import { couponTitle } from '../coupons'
import { money, t, toMajor, toMinor } from '../i18n'
import LangsButton from './LangsButton.vue'
import { session } from '../session'
import type { CouponBatch, CouponKind } from '../types'

const props = defineProps<{ currency: string; brand?: number }>()
const bid = session.branchId!
const list = ref<CouponBatch[]>([])
const error = ref(''), busy = ref(false), editing = ref(false)
const blank = () => ({ id: 0, name: '', kind: 'store_threshold' as CouponKind, value: 15 as number | string, threshold: 100 as number | string,
  valid_days: 7 as number | string, total_quota: 100 as number | string, per_customer: 1 as number | string })
const f = reactive(blank())

async function load() {
  try {
    const all = (await get<{ batches: CouponBatch[] }>(`/merchant/branches/${bid}/coupons/batches`)).batches
    // one brand's batches (docs/MULTI_BRAND_DESIGN.md §10)
    list.value = props.brand ? all.filter(b => !b.brand_id || b.brand_id === props.brand) : all
  } catch (e) { error.value = (e as Error).message }
}
watch(() => props.brand, load)
onMounted(load)
function edit(b: CouponBatch | null) {
  Object.assign(f, b ? { id: b.id, name: b.name, kind: b.kind, value: toMajor(b.value), threshold: toMajor(b.threshold), valid_days: b.valid_days,
    total_quota: b.total_quota ?? '', per_customer: b.per_customer ?? 1 } : blank())
  editing.value = true
}
async function run(fn: () => Promise<unknown>) {
  error.value = ''; busy.value = true
  try { await fn(); await load(); return true } catch (e) { error.value = (e as Error).message; return false } finally { busy.value = false }
}
async function save() {
  const body = { name: f.name, kind: f.kind, value: toMinor(f.value), threshold: f.kind === 'store_free' ? 0 : toMinor(f.threshold),
    valid_days: Number(f.valid_days), total_quota: f.total_quota === '' ? null : Number(f.total_quota), per_customer: Number(f.per_customer) || 1,
    brand_id: props.brand ?? 0 }
  const url = `/merchant/branches/${bid}/coupons/batches`
  if (await run(() => f.id ? put(`${url}/${f.id}`, body) : post(url, body))) editing.value = false
}
function status(b: CouponBatch, to: string) {
  if (to === 'ended' && !confirm(t('cb.end_confirm'))) return
  void run(() => post(`/merchant/branches/${bid}/coupons/batches/${b.id}/status`, { status: to }))
}
const sample = computed(() => ({ kind: f.kind, value: toMinor(f.value), threshold: f.kind === 'store_free' ? 0 : toMinor(f.threshold) }))
</script>

<template>
  <div class="card stack" data-testid="claim-batches">
    <div class="row between" style="align-items:flex-start">
      <div><h2 style="margin:0">{{ t('cb.title') }}</h2><div class="muted small">{{ t('cb.hint') }}</div></div>
      <button class="btn primary" data-testid="claim-new" @click="edit(null)">{{ t('cb.new') }}</button>
    </div>
    <div v-if="error" class="banner bad">{{ error }}</div>
    <form v-if="editing" class="cb-form" data-testid="claim-form" @submit.prevent="save">
      <label class="field"><span>{{ t('cb.name') }}</span><input v-model="f.name" maxlength="60" :placeholder="couponTitle(sample, props.currency)" data-testid="claim-name" /></label>
      <label class="field"><span>{{ t('cr.kind') }}</span>
        <select v-model="f.kind" data-testid="claim-kind"><option v-for="k in (['store_threshold', 'store_free', 'store_delivery'] as const)" :key="k" :value="k">{{ t('cr.k.' + k) }}</option></select></label>
      <label v-if="f.kind !== 'store_free'" class="field"><span>{{ t('cr.threshold') }}</span><input v-model="f.threshold" type="number" min="0" step="0.01" data-testid="claim-threshold" /></label>
      <label class="field"><span>{{ t('cr.value') }}</span><input v-model="f.value" type="number" min="0" step="0.01" data-testid="claim-value" /></label>
      <label class="field"><span>{{ t('cb.days') }}</span><input v-model="f.valid_days" type="number" min="1" max="365" data-testid="claim-days" /></label>
      <label class="field"><span>{{ t('cb.quota') }}</span><input v-model="f.total_quota" type="number" min="1" placeholder="—" data-testid="claim-quota" /></label>
      <label class="field"><span>{{ t('cb.per') }}</span><input v-model="f.per_customer" type="number" min="1" /></label>
      <div class="small muted full">{{ t('cr.preview') }}：{{ couponTitle(sample, props.currency) }}</div>
      <div class="row full" style="gap:8px"><button class="btn primary" :disabled="busy" data-testid="claim-save">{{ t('save') }}</button>
        <LangsButton v-if="f.name" :base="`/merchant/branches/${bid}`" :scope="f.id ? `coupon_batch/${f.id}` : null" :name="f.name" />
        <button type="button" class="btn" @click="editing = false">{{ t('cancel') }}</button></div>
    </form>
    <div v-if="!list.length && !editing" class="muted small">{{ t('cb.none') }}</div>
    <div v-for="b in list" :key="b.id" class="cb-row" :class="b.status" data-testid="claim-row">
      <div class="grow"><b>{{ b.name || couponTitle(b, b.currency) }}</b>
        <div class="small muted">{{ couponTitle(b, b.currency) }} · {{ t('cb.days_n', { n: b.valid_days }) }} ·
          {{ b.total_quota ? t('cb.left', { n: b.total_quota - b.issued_count, of: b.total_quota }) : t('cb.no_quota') }}</div>
        <div class="small">{{ t('cb.stats', { issued: b.stats.issued, used: b.stats.used, spent: money(b.stats.spent, b.currency) }) }}</div></div>
      <span class="tag" :class="{ ok: b.status === 'active' }">{{ t('cb.st.' + b.status) }}</span>
      <div v-if="b.status !== 'ended'" class="acts">
        <button class="btn small" @click="edit(b)">{{ t('edit') }}</button>
        <button v-if="b.status === 'active'" class="btn small" data-testid="claim-pause" @click="status(b, 'paused')">{{ t('cb.pause') }}</button>
        <button v-else class="btn small" @click="status(b, 'active')">{{ t('cb.resume') }}</button>
        <button class="btn small danger" @click="status(b, 'ended')">{{ t('cb.end') }}</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.cb-form { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 0 12px; border-top: 1px solid var(--line); padding-top: 12px; }
.cb-form .full { grid-column: 1 / -1; margin-bottom: 8px; }
.cb-row { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; border-top: 1px solid var(--line); padding-top: 10px; }
.cb-row.ended { opacity: .6; } .cb-row .grow { flex: 1 1 220px; min-width: 0; }
.acts { display: flex; gap: 6px; }
</style>
