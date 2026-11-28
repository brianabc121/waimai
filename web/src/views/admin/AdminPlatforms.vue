<script setup lang="ts">
// Delivery platforms (docs/ORDER_TYPES_DESIGN.md §6.1): Grab, foodpanda and
// the ones of each country. Stores in those countries turn them on in their
// settings; platform orders are numbered "<prefix> <the platform's number>".
// A platform is never deleted, only stopped, so old orders keep their name.
import { onMounted, reactive, ref } from 'vue'
import { get, post, put } from '../../api'
import { extend, t } from '../../i18n'
import { LANGS } from '../../langs'
import { type DeliveryPlatform, bpToPct, pctToBp, platformName } from '../../platforms'

extend({
  'ad.pf.hint': ['商家在“设置 → 外卖平台”里勾选上架的平台，商品里给每件商品设平台价；店员在“点单 → 平台下单”录入平台订单，单号是“前缀 + 平台单号”。佣金、加价是默认值，商家可以改成自己谈的。停用后老订单照常显示，不能删除。',
    'Merchants turn platforms on in Settings → Delivery platforms and set platform prices on the catalog; staff enter platform orders under Order → Platform order, numbered "prefix + the platform\'s number". Commission and markup are defaults a merchant may change. A stopped platform still shows on old orders; platforms cannot be deleted.'],
  'ad.pf.name': ['名字', 'Name'], 'ad.pf.names': ['各语言名字（不填就显示名字）', 'Names by language (empty shows the name)'],
  'ad.pf.prefix': ['单号前缀', 'Order prefix'], 'ad.pf.countries': ['国家（两位代码，逗号分开，空 = 所有国家）', 'Countries (2-letter codes, comma separated; empty = all)'],
  'ad.pf.ccol': ['国家', 'Countries'], 'ad.pf.color': ['颜色', 'Colour'], 'ad.pf.logo': ['图标链接', 'Logo link'],
  'ad.pf.no_hint': ['平台单号示例', 'Order number example'], 'ad.pf.no_pattern': ['平台单号格式（正则，可空）', 'Order number pattern (regular expression, optional)'],
  'ad.pf.commission': ['默认佣金 %', 'Default commission %'], 'ad.pf.base': ['佣金按', 'Commission on'],
  'ad.pf.base_after': ['商家优惠后的金额', 'the amount after the merchant\'s discount'], 'ad.pf.base_orig': ['原价（优惠前）', 'the original amount'],
  'ad.pf.markup': ['默认商品加价 %', 'Default price markup %'],
  'ad.pf.active': ['启用', 'On'], 'ad.pf.stores': ['上架的店', 'Stores'],
  'ad.pf.new': ['新增平台', 'Add a platform'], 'ad.pf.edit': ['编辑', 'Edit'], 'ad.pf.save': ['保存', 'Save'], 'ad.pf.cancel': ['取消', 'Cancel'],
  'ad.pf.all': ['所有国家', 'All countries'], 'ad.pf.none': ['还没有外卖平台', 'No delivery platforms yet'],
  'ad.pf.dup': ['已经有平台用这个前缀', 'Another platform already uses this prefix'],
})

const list = ref<DeliveryPlatform[]>([])
const error = ref('')
const editing = ref<DeliveryPlatform | null>(null)
const OTHER = LANGS.map(l => l.code)
const f = reactive({ name: '', names: {} as Record<string, string>, prefix: '', countries: '', color: '#00b14f', logo_url: '', no_hint: '', no_pattern: '',
  commission: '', commission_base: 'after_merchant' as 'after_merchant' | 'original', markup: '', active: true, sort: 0 })

async function run(fn: () => Promise<unknown>) {
  error.value = ''
  try { await fn() } catch (e) { error.value = (e as Error & { code?: string }).code === 'DUPLICATE' ? t('ad.pf.dup') : (e as Error).message }
}
const load = async () => { list.value = (await get<{ platforms: DeliveryPlatform[] }>('/admin/platforms')).platforms }
onMounted(() => run(load))

function edit(p: DeliveryPlatform | null) {
  editing.value = p ?? { id: 0, name: '', names: {}, prefix: '', countries: [], color: '#00b14f', logo_url: '', no_hint: '', no_pattern: '',
    commission_bp: 0, commission_base: 'after_merchant', markup_bp: 0, active: true, sort: 0 }
  const x = editing.value
  Object.assign(f, { name: x.name, names: { ...x.names }, prefix: x.prefix, countries: x.countries.join(', '), color: x.color, logo_url: x.logo_url,
    no_hint: x.no_hint, no_pattern: x.no_pattern, commission: bpToPct(x.commission_bp), commission_base: x.commission_base,
    markup: bpToPct(x.markup_bp), active: x.active, sort: x.sort })
}
const body = (p: DeliveryPlatform, active = f.active) => ({ name: f.name, names: f.names, prefix: f.prefix, color: f.color, logo_url: f.logo_url,
  countries: f.countries.split(/[\s,，]+/).filter(Boolean), no_hint: f.no_hint, no_pattern: f.no_pattern,
  commission_bp: pctToBp(f.commission) ?? 0, commission_base: f.commission_base, markup_bp: pctToBp(f.markup) ?? 0, active, sort: p.sort })
const save = () => run(async () => {
  const p = editing.value!
  if (p.id) await put(`/admin/platforms/${p.id}`, body(p))
  else await post('/admin/platforms', body(p))
  editing.value = null
  await load()
})
const toggle = (p: DeliveryPlatform) => run(async () => {
  edit(p); editing.value = null
  await put(`/admin/platforms/${p.id}`, body(p, !p.active))
  await load()
})
</script>

<template>
  <div class="adm-page">
    <p class="muted small" style="margin-top:0">{{ t('ad.pf.hint') }}</p>
    <div v-if="error" class="banner bad">{{ error }}</div>
    <div class="row between" style="margin-bottom:10px"><span></span>
      <button class="btn primary small" data-testid="pf-new" @click="edit(null)">{{ t('ad.pf.new') }}</button></div>
    <div class="card flush">
      <table class="data" data-testid="platforms">
        <thead><tr><th>{{ t('ad.pf.name') }}</th><th>{{ t('ad.pf.prefix') }}</th><th>{{ t('ad.pf.ccol') }}</th>
          <th>{{ t('ad.pf.commission') }}</th><th>{{ t('ad.pf.markup') }}</th><th>{{ t('ad.pf.stores') }}</th><th>{{ t('ad.pf.active') }}</th><th></th></tr></thead>
        <tbody>
          <tr v-if="!list.length"><td colspan="8" class="muted">{{ t('ad.pf.none') }}</td></tr>
          <tr v-for="p in list" :key="p.id" :data-platform="p.prefix" :class="{ off: !p.active }">
            <td><span class="pf-dot" :style="{ background: p.color }"></span><b>{{ p.name }}</b>
              <div v-if="platformName(p) !== p.name" class="muted small">{{ platformName(p) }}</div></td>
            <td class="mono">{{ p.prefix }} <span v-if="p.no_hint" class="muted small">{{ p.no_hint }}</span></td>
            <td class="small">{{ p.countries.length ? p.countries.join(' ') : t('ad.pf.all') }}</td>
            <td class="num">{{ bpToPct(p.commission_bp) }}%</td>
            <td class="num">{{ bpToPct(p.markup_bp) }}%</td>
            <td class="num">{{ p.stores ?? 0 }}</td>
            <td><label class="check"><input type="checkbox" :checked="p.active" @change="toggle(p)" /></label></td>
            <td><button class="btn small" data-testid="pf-edit" @click="edit(p)">{{ t('ad.pf.edit') }}</button></td>
          </tr>
        </tbody>
      </table>
    </div>
    <form v-if="editing" class="card stack" style="margin-top:12px" data-testid="pf-form" @submit.prevent="save">
      <div class="pf-grid">
        <label class="field"><span>{{ t('ad.pf.name') }}</span><input v-model="f.name" maxlength="40" required data-testid="pf-name" /></label>
        <label class="field"><span>{{ t('ad.pf.prefix') }}</span><input v-model.trim="f.prefix" maxlength="10" pattern="[A-Za-z0-9]{1,10}" required class="mono" data-testid="pf-prefix" /></label>
        <label class="field"><span>{{ t('ad.pf.color') }}</span><input v-model="f.color" type="color" /></label>
        <label class="field"><span>{{ t('ad.pf.countries') }}</span><input v-model="f.countries" placeholder="PH, SG, MY" data-testid="pf-countries" /></label>
        <label class="field"><span>{{ t('ad.pf.commission') }}</span><input v-model="f.commission" type="number" min="0" max="100" step="0.01" /></label>
        <label class="field"><span>{{ t('ad.pf.markup') }}</span><input v-model="f.markup" type="number" min="0" max="100" step="0.01" data-testid="pf-markup" /></label>
        <label class="field"><span>{{ t('ad.pf.base') }}</span><select v-model="f.commission_base">
          <option value="after_merchant">{{ t('ad.pf.base_after') }}</option><option value="original">{{ t('ad.pf.base_orig') }}</option></select></label>
        <label class="field"><span>{{ t('ad.pf.no_hint') }}</span><input v-model="f.no_hint" maxlength="60" placeholder="GF-123" /></label>
        <label class="field"><span>{{ t('ad.pf.no_pattern') }}</span><input v-model="f.no_pattern" maxlength="200" class="mono" /></label>
      </div>
      <label class="field"><span>{{ t('ad.pf.logo') }}</span><input v-model.trim="f.logo_url" maxlength="500" placeholder="https://…" /></label>
      <div class="muted small">{{ t('ad.pf.names') }}</div>
      <div class="pf-names">
        <label v-for="l in OTHER" :key="l" class="row" style="gap:6px"><span class="muted small" style="width:56px">{{ l }}</span>
          <input v-model="f.names[l]" maxlength="40" :placeholder="f.name" /></label>
      </div>
      <label class="check"><input v-model="f.active" type="checkbox" />{{ t('ad.pf.active') }}</label>
      <div class="row" style="justify-content:flex-end;gap:8px">
        <button type="button" class="btn" @click="editing = null">{{ t('ad.pf.cancel') }}</button>
        <button class="btn primary" data-testid="pf-save">{{ t('ad.pf.save') }}</button>
      </div>
    </form>
  </div>
</template>

<style scoped>
.pf-dot { display: inline-block; width: 10px; height: 10px; border-radius: 50%; margin-inline-end: 6px; }
tr.off td { opacity: .5; }
.pf-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); gap: 0 12px; }
.pf-names { display: grid; grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); gap: 6px 12px; }
</style>
