<script setup lang="ts">
// Store settings: the delivery platforms the store sells on, with its shop
// ID there and its own terms (docs/ORDER_TYPES_DESIGN.md §6). Empty rates
// follow the platform's defaults. Dish prices on each platform are set on the
// menu page.
import { onMounted, ref, watch } from 'vue'
import { get, put } from '../api'
import { t } from '../i18n'
import { session } from '../session'
import { type BranchPlatform, bpToPct, pctToBp, platformName } from '../platforms'
import { deliveryBrands, multiBrand, useBrandPick } from '../brands'
import BrandSeg from './brands/BrandSeg.vue'


const props = defineProps<{ branchId: number }>()
interface Row { p: BranchPlatform; enabled: boolean; shop_ref: string; commission: string; markup: string; round_step: number }
const rows = ref<Row[]>([])
const error = ref('')
const ok = ref(false)
const loaded = ref(false)
const STEPS = [{ v: 0, k: 'pf.set.r0' }, { v: 100, k: 'pf.set.r1' }, { v: 500, k: 'pf.set.r5' }, { v: 1000, k: 'pf.set.r10' }]

function fill(list: BranchPlatform[]) {
  rows.value = list.map(p => ({ p, enabled: p.enabled, shop_ref: p.shop_ref, commission: bpToPct(p.own_commission_bp), markup: bpToPct(p.own_markup_bp), round_step: p.round_step }))
}
// each brand has its own shops on the platforms (docs/MULTI_BRAND_DESIGN.md §10)
const { pick: brand } = useBrandPick('platforms', deliveryBrands)
async function load() {
  error.value = ''; ok.value = false
  try { fill((await get<{ platforms: BranchPlatform[] }>(`/merchant/branches/${props.branchId}/platforms?brand=${brand.value}`)).platforms) } catch (e) { error.value = (e as Error).message }
  loaded.value = true
}
onMounted(load)
watch(brand, load)
async function save() {
  error.value = ''; ok.value = false
  try {
    const body = { brand_id: brand.value, platforms: rows.value.map(r => ({ platform_id: r.p.id, enabled: r.enabled, shop_ref: r.shop_ref,
      own_commission_bp: pctToBp(r.commission), own_markup_bp: pctToBp(r.markup), round_step: r.round_step })) }
    const list = (await put<{ platforms: BranchPlatform[] }>(`/merchant/branches/${props.branchId}/platforms`, body)).platforms
    fill(list)
    // the 平台下单 menu follows at once
    const role = session.user?.roles.find(r => r.branch_id === props.branchId)
    if (role && list.some(p => p.enabled)) role.platforms = true
    ok.value = true
  } catch (e) { error.value = (e as Error).message }
}
</script>

<template>
  <form class="ox-panel stack" data-testid="platforms-card" @submit.prevent="save">
    <div class="ox-panel-h" style="margin:0"><h2>{{ t('pf.set.title') }}</h2><span class="grow"></span>
      <button v-if="rows.length" class="btn primary" data-testid="pf-save">{{ t('save') }}</button></div>
    <p class="muted small" style="margin:0">{{ t('pf.set.hint') }}</p>
    <template v-if="multiBrand() && deliveryBrands().length > 1">
      <BrandSeg v-model="brand" :brands="deliveryBrands()" />
      <p class="muted small" style="margin:0">{{ t('br.pf_hint') }}</p>
    </template>
    <div v-if="error" class="banner bad">{{ error }}</div>
    <div v-else-if="ok" class="banner ok">{{ t('saved') }}</div>
    <p v-if="loaded && !rows.length" class="muted">{{ t('pf.set.none') }}</p>
    <div v-for="r in rows" :key="r.p.id" class="pfrow" :class="{ off: !r.enabled }" :data-platform="r.p.prefix">
      <label class="check pfname"><input v-model="r.enabled" type="checkbox" :data-testid="'pf-on-' + r.p.prefix" />
        <img v-if="r.p.logo_url" :src="r.p.logo_url" alt="" /><i v-else :style="{ background: r.p.color }"></i>
        <b>{{ platformName(r.p) }}</b></label>
      <template v-if="r.enabled">
        <label class="field"><span>{{ t('pf.set.shop') }}</span><input v-model.trim="r.shop_ref" maxlength="60" /></label>
        <label class="field"><span>{{ t('pf.set.commission') }}</span>
          <input v-model="r.commission" type="number" min="0" max="100" step="0.01" :placeholder="t('pf.set.default', { v: bpToPct(r.p.commission_bp) + '%' })" :data-testid="'pf-commission-' + r.p.prefix" /></label>
        <label class="field"><span>{{ t('pf.set.markup') }}</span>
          <input v-model="r.markup" type="number" min="0" max="100" step="0.01" :placeholder="t('pf.set.default', { v: bpToPct(r.p.markup_bp) + '%' })" :data-testid="'pf-markup-' + r.p.prefix" /></label>
        <label class="field"><span>{{ t('pf.set.round') }}</span>
          <select v-model.number="r.round_step"><option v-for="s in STEPS" :key="s.v" :value="s.v">{{ t(s.k) }}</option></select></label>
      </template>
    </div>
  </form>
</template>

<style scoped>
.pfrow { display: grid; grid-template-columns: 180px repeat(4, minmax(0, 1fr)); gap: 0 12px; align-items: end; padding: 8px 0; border-top: 1px solid var(--line); }
.pfrow.off { grid-template-columns: 1fr; }
.pfrow .field { margin: 0; }
.pfname { align-self: center; gap: 8px; }
.pfname i, .pfname img { width: 18px; height: 18px; border-radius: 5px; flex: none; object-fit: cover; }
@media (max-width: 760px) { .pfrow { grid-template-columns: 1fr 1fr; } .pfname { grid-column: 1 / -1; } }
</style>
