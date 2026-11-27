<script setup lang="ts">
// A top-up offer and its tiers (docs/STORED_VALUE_DESIGN.md §4.3): pay X get
// Y free, maybe coupons too, with limits per customer, first top-up only and
// a quota. A tier taken off stays for the top-ups that used it.
import { computed, reactive, ref } from 'vue'
import { currencySymbol, t, toMajor, toMinor } from '../../i18n'
import { brandsHere, multiBrand } from '../../brands'
import { saveCampaign, storedErr, type StoredCampaign } from '../../stored'

const props = defineProps<{ branchId: number; currency: string; campaign: StoredCampaign | null; isOwner: boolean; approveOver: number }>()
const emit = defineEmits<{ (e: 'saved'): void; (e: 'close'): void }>()

interface TierForm {
  id: number; pay: string; bonus: string; coupon_kind: '' | 'store_free' | 'store_threshold'; coupon_value: string; coupon_min: string
  coupon_qty: number; coupon_days: number; label: string; per_customer: string; first_only: boolean; quota: string
}
const day = (s: string | null) => s ? s.slice(0, 10) : ''
const c = props.campaign
const f = reactive({
  name: c?.name ?? '', name_en: c?.name_en ?? '', starts: day(c?.starts_at ?? null), ends: day(c?.ends_at ?? null),
  custom_ok: c?.custom_ok ?? false, custom_min: c?.custom_min ? String(toMajor(c.custom_min)) : '', brand_ids: [...(c?.brand_ids ?? [])],
  note: c?.note ?? '', approve_over: String(toMajor(props.approveOver)),
  tiers: (c?.tiers ?? []).map<TierForm>(x => ({
    id: x.id, pay: String(toMajor(x.pay)), bonus: String(toMajor(x.bonus)), coupon_kind: x.coupon_kind, coupon_value: x.coupon_value ? String(toMajor(x.coupon_value)) : '',
    coupon_min: x.coupon_min ? String(toMajor(x.coupon_min)) : '', coupon_qty: x.coupon_qty, coupon_days: x.coupon_days || 30, label: x.label,
    per_customer: x.per_customer ? String(x.per_customer) : '', first_only: x.first_only, quota: x.quota ? String(x.quota) : '',
  })),
})
if (!f.tiers.length) for (const [p, b] of [[500, 50], [1000, 150], [2000, 400]]) f.tiers.push(blank(p, b))
function blank(pay = 0, bonus = 0): TierForm {
  return { id: 0, pay: pay ? String(pay) : '', bonus: bonus ? String(bonus) : '', coupon_kind: '', coupon_value: '', coupon_min: '', coupon_qty: 0,
    coupon_days: 30, label: '', per_customer: '', first_only: false, quota: '' }
}
const rate = (x: TierForm) => Number(x.pay) > 0 ? Math.round(Number(x.bonus || 0) * 100 / Number(x.pay)) : 0
const rich = computed(() => f.tiers.some(x => rate(x) > 30))
const error = ref(''), busy = ref(false)
const toDate = (d: string, end = false) => d ? new Date(d + (end ? 'T23:59:59' : 'T00:00:00')).toISOString() : null
const int = (s: string) => s.trim() ? Math.round(Number(s)) : null

async function save() {
  busy.value = true; error.value = ''
  try {
    await saveCampaign(props.branchId, c?.id ?? 0, {
      name: f.name, name_en: f.name_en, starts_at: toDate(f.starts), ends_at: toDate(f.ends, true), custom_ok: f.custom_ok,
      custom_min: f.custom_ok ? toMinor(f.custom_min) : 0, brand_ids: f.brand_ids, note: f.note,
      approve_over: props.isOwner ? toMinor(f.approve_over) : undefined,
      tiers: f.tiers.map(x => ({
        id: x.id, pay: toMinor(x.pay), bonus: toMinor(x.bonus), coupon_kind: x.coupon_qty ? x.coupon_kind || 'store_free' : '',
        coupon_value: x.coupon_qty ? toMinor(x.coupon_value) : 0, coupon_min: x.coupon_qty ? toMinor(x.coupon_min) : 0,
        coupon_qty: x.coupon_qty, coupon_days: x.coupon_days, label: x.label, per_customer: int(x.per_customer), first_only: x.first_only, quota: int(x.quota),
      })),
    })
    emit('saved')
  } catch (e) { error.value = storedErr(e) } finally { busy.value = false }
}
</script>

<template>
  <div class="ce-mask" @click.self="emit('close')">
    <form class="ce" data-testid="campaign-editor" @submit.prevent="save">
      <div class="ce-h"><h2>{{ t(c ? 'sv.e.edit' : 'sv.e.new') }}</h2><button type="button" class="x" :aria-label="t('close')" @click="emit('close')">✕</button></div>
      <div class="ce-b">
        <div class="ce-g2">
          <label class="field"><span>{{ t('sv.e.name') }}</span><input v-model="f.name" maxlength="60" required data-testid="ce-name" /></label>
          <label class="field"><span>{{ t('sv.e.name_en') }}</span><input v-model="f.name_en" maxlength="60" /></label>
          <label class="field"><span>{{ t('sv.e.starts') }}</span><input v-model="f.starts" type="date" /></label>
          <label class="field"><span>{{ t('sv.e.ends') }}</span><input v-model="f.ends" type="date" /></label>
        </div>
        <div class="ce-lbl">{{ t('sv.e.tiers') }}</div>
        <div v-for="(x, i) in f.tiers" :key="i" class="ce-tier" data-testid="ce-tier">
          <div class="ce-row">
            <label class="field"><span>{{ t('sv.e.pay') }}</span><input v-model="x.pay" inputmode="decimal" required data-testid="ce-pay" /></label>
            <label class="field"><span>{{ t('sv.e.bonus') }} <b v-if="rate(x)" class="pct">{{ t('sv.t.rate', { pct: rate(x) }) }}</b></span><input v-model="x.bonus" inputmode="decimal" data-testid="ce-bonus" /></label>
            <label class="field"><span>{{ t('sv.e.label') }}</span><input v-model="x.label" maxlength="20" /></label>
            <button type="button" class="btn small rm" :title="t('sv.e.remove')" :aria-label="t('sv.e.remove')" @click="f.tiers.splice(i, 1)">✕</button>
          </div>
          <div class="ce-row">
            <label class="field"><span>{{ t('sv.e.coupon') }}</span>
              <select v-model="x.coupon_kind" @change="x.coupon_qty = x.coupon_kind ? x.coupon_qty || 1 : 0">
                <option value="">{{ t('sv.e.coupon_none') }}</option><option value="store_free">{{ t('sv.e.coupon_free') }}</option>
                <option value="store_threshold">{{ t('sv.e.coupon_th') }}</option></select></label>
            <template v-if="x.coupon_kind">
              <label class="field"><span>{{ t('sv.e.coupon_value') }}</span><input v-model="x.coupon_value" inputmode="decimal" /></label>
              <label v-if="x.coupon_kind === 'store_threshold'" class="field"><span>{{ t('sv.e.coupon_min') }}</span><input v-model="x.coupon_min" inputmode="decimal" /></label>
              <label class="field"><span>{{ t('sv.e.coupon_qty') }}</span><input v-model.number="x.coupon_qty" type="number" min="1" max="10" /></label>
              <label class="field"><span>{{ t('sv.e.coupon_days') }}</span><input v-model.number="x.coupon_days" type="number" min="1" max="365" /></label>
            </template>
          </div>
          <div class="ce-row lim">
            <label class="check"><input v-model="x.first_only" type="checkbox" />{{ t('sv.e.first') }}</label>
            <label class="field sm"><span>{{ t('sv.e.per') }}</span><input v-model="x.per_customer" inputmode="numeric" placeholder="∞" /></label>
            <label class="field sm"><span>{{ t('sv.e.quota') }}</span><input v-model="x.quota" inputmode="numeric" placeholder="∞" /></label>
          </div>
        </div>
        <button type="button" class="btn small" data-testid="ce-add" @click="f.tiers.push(blank())">＋ {{ t('sv.e.add') }}</button>
        <div v-if="rich" class="banner warn small">{{ t('sv.e.rate_warn') }}</div>
        <div class="ce-row">
          <label class="check"><input v-model="f.custom_ok" type="checkbox" />{{ t('sv.e.custom') }}</label>
          <label v-if="f.custom_ok" class="field sm"><span>{{ t('sv.e.custom_min') }}</span><input v-model="f.custom_min" inputmode="decimal" /></label>
        </div>
        <div v-if="multiBrand()">
          <div class="ce-lbl">{{ t('sv.e.brands') }}</div>
          <div class="ce-row"><label v-for="b in brandsHere()" :key="b.id" class="check"><input v-model="f.brand_ids" type="checkbox" :value="b.id" />{{ b.name }}</label></div>
        </div>
        <label class="field"><span>{{ t('sv.e.note') }}</span><textarea v-model="f.note" maxlength="500" rows="2"></textarea></label>
        <label v-if="isOwner" class="field"><span>{{ t('sv.m.approve_over') }} ({{ currencySymbol(currency) }})</span>
          <input v-model="f.approve_over" inputmode="decimal" data-testid="ce-approve-over" /></label>
        <div v-if="error" class="banner bad small" data-testid="ce-error">{{ error }}</div>
      </div>
      <div class="ce-f">
        <button type="button" class="btn" @click="emit('close')">{{ t('sv.e.cancel') }}</button>
        <button class="btn primary" :disabled="busy" data-testid="ce-save">{{ t('sv.e.save') }}</button>
      </div>
    </form>
  </div>
</template>

<style scoped>
.ce-mask { position: fixed; inset: 0; background: rgba(30, 20, 10, .35); z-index: 50; display: flex; justify-content: flex-end; }
.ce { width: min(680px, 100vw); height: 100%; background: #fff; display: flex; flex-direction: column; box-shadow: -8px 0 30px rgba(0, 0, 0, .18); }
.ce-h { display: flex; align-items: center; padding: 14px 18px; border-bottom: 1px solid var(--line); }
.ce-h h2 { margin: 0; font-size: 17px; flex: 1; }
.ce-h .x { border: 0; background: none; font-size: 18px; cursor: pointer; color: var(--muted); }
.ce-b { flex: 1; overflow: auto; padding: 14px 18px; display: grid; gap: 12px; align-content: start; }
.ce-f { display: flex; justify-content: flex-end; gap: 10px; padding: 12px 18px calc(12px + env(safe-area-inset-bottom)); border-top: 1px solid var(--line); }
.ce-g2 { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 0 12px; }
.ce-lbl { font-weight: 700; font-size: 14px; }
.ce-tier { border: 1px solid var(--line); border-radius: 10px; padding: 10px 12px 2px; background: #fffaf2; display: grid; gap: 4px; }
.ce-row { display: flex; flex-wrap: wrap; gap: 0 10px; align-items: flex-end; }
.ce-row .field { flex: 1 1 110px; min-width: 0; }
.ce-row .field.sm { flex: 0 1 110px; }
.ce-row .check { display: inline-flex; align-items: center; gap: 6px; margin-bottom: 12px; white-space: nowrap; }
.ce-row .rm { margin-bottom: 12px; }
.pct { color: var(--brand, #a02c20); font-weight: 700; margin-inline-start: 4px; }
@media (max-width: 560px) { .ce-g2 { grid-template-columns: minmax(0, 1fr); } }
</style>
