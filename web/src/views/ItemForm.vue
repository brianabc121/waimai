<script setup lang="ts">
// The item editor, shared by the merchant's menu page and the platform back
// office. `base` is the API prefix of the branch, for example
// /merchant/branches/3 or /admin/branches/3.
import { reactive, ref } from 'vue'
import { post, put } from '../api'
import { both, t, toMajor, toMinor } from '../i18n'
import type { Item, Station } from '../types'
import ImageUpload from './ImageUpload.vue'
import PhotoList from './PhotoList.vue'

const props = defineProps<{ item: Item | null; categoryId?: number; cats: { id: number; name: string }[]; stations: Station[]; cur: string; base: string }>()
const emit = defineEmits<{ saved: []; close: [] }>()

interface OptForm { name: string; name_en: string; extra: number; sold_out: boolean }
interface GroupForm { name: string; name_en: string; min_select: number; max_select: number; options: OptForm[] }
interface Form { id: number; category_id: number; name: string; name_en: string; description: string; image_url: string
  photos: string[]; details: string
  price: number; published: boolean; sold_out: boolean; limited: boolean; stock: number; needs_prep: boolean
  delivery_available: boolean; pickup_available: boolean; dine_in_available: boolean; sort: number; station_id: number | null; groups: GroupForm[] }

function blank(): Form {
  return { id: 0, category_id: 0, name: '', name_en: '', description: '', image_url: '', photos: [], details: '', price: 0, published: false, sold_out: false,
    limited: false, stock: 0, needs_prep: true, delivery_available: true, pickup_available: true, dine_in_available: true, sort: 0, station_id: null, groups: [] }
}

const it = props.item
const f = reactive<Form>(it ? {
  id: it.id, category_id: it.category_id, name: it.name, name_en: it.name_en, description: it.description, image_url: it.image_url,
  photos: [...(it.photos ?? [])], details: it.details ?? '',
  price: toMajor(it.price), published: it.published, sold_out: it.sold_out, limited: it.stock_qty !== null, stock: it.stock_qty ?? 0,
  needs_prep: it.needs_prep, delivery_available: it.delivery_available, pickup_available: it.pickup_available, dine_in_available: it.dine_in_available ?? true, sort: it.sort, station_id: it.station_id,
  groups: it.groups.map(g => ({ name: g.name, name_en: g.name_en, min_select: g.min_select, max_select: g.max_select,
    options: g.options.map(o => ({ name: o.name, name_en: o.name_en, extra: toMajor(o.price_delta), sold_out: o.sold_out })) })),
} : { ...blank(), category_id: props.categoryId || props.cats[0]?.id || 0 })
const error = ref('')
const busy = ref(false)

async function save() {
  const body = {
    category_id: f.category_id, name: f.name, name_en: f.name_en, description: f.description, image_url: f.image_url, photos: f.photos, details: f.details,
    price: toMinor(f.price), published: f.published, sold_out: f.sold_out, stock_qty: f.limited ? Math.max(0, Math.floor(f.stock)) : null,
    needs_prep: f.needs_prep, delivery_available: f.delivery_available, pickup_available: f.pickup_available, dine_in_available: f.dine_in_available, sort: f.sort, station_id: f.needs_prep ? f.station_id : null,
    groups: f.groups.map(g => ({ name: g.name, name_en: g.name_en, min_select: Number(g.min_select), max_select: Number(g.max_select),
      options: g.options.map(o => ({ name: o.name, name_en: o.name_en, price_delta: toMinor(o.extra), sold_out: o.sold_out })) })),
  }
  error.value = ''; busy.value = true
  try {
    await (f.id ? put(`${props.base}/items/${f.id}`, body) : post(`${props.base}/items`, body))
    emit('saved')
  } catch (e) { error.value = (e as Error).message } finally { busy.value = false }
}
</script>

<template>
  <div class="sheet-mask" @click.self="emit('close')">
    <form class="sheet center" data-testid="item-form" @submit.prevent="save">
      <div class="sheet-head"><h2>{{ f.id ? t('edit') : t('new_item') }}</h2></div>
      <div class="sheet-body">
        <div v-if="error" class="banner bad" style="margin-bottom:12px">{{ error }}</div>
        <div class="grid2">
          <label class="field"><span>{{ t('name_zh') }}</span><input v-model="f.name" required /></label>
          <label class="field"><span>{{ t('name_en') }}</span><input v-model="f.name_en" /></label>
          <label class="field"><span>{{ t('category') }}</span><select v-model="f.category_id">
            <option v-for="c in cats" :key="c.id" :value="c.id">{{ c.name }}</option></select></label>
          <label class="field"><span>{{ t('price') }} ({{ cur }})</span><input type="number" min="0" step="0.01" v-model.number="f.price" required /></label>
        </div>
        <label class="field"><span>{{ t('description') }}</span><input v-model="f.description" /></label>
        <ImageUpload v-model="f.image_url" :base="base" :ratio="1" :width="800" :label="t('im.dish')" :hint="t('im.dish_hint')" testid="item-image" />
        <PhotoList v-model="f.photos" :base="base" :ratio="1" :width="800" :max="8" :label="t('im.more')" :hint="t('im.more_hint')" testid="item-photos" />
        <label class="field"><span>{{ t('im.details') }}</span>
          <textarea v-model="f.details" rows="4" maxlength="2000" :placeholder="t('im.details_ph')" data-testid="item-details"></textarea></label>
        <div class="field" data-testid="item-channels"><span>{{ t('it.channels') }}</span>
          <div class="chans">
            <label class="check"><input type="checkbox" v-model="f.dine_in_available" data-testid="item-dine-in" />{{ t('dine_in') }}</label>
            <label class="check"><input type="checkbox" v-model="f.delivery_available" data-testid="item-delivery" />{{ t('it.delivery') }}</label>
            <label class="check"><input type="checkbox" v-model="f.pickup_available" data-testid="item-pickup" />{{ t('it.pickup') }}</label>
          </div>
          <div v-if="!f.dine_in_available && !f.delivery_available && !f.pickup_available" class="small" style="color:var(--warn)">{{ t('it.no_channel') }}</div>
        </div>
        <div class="grid2">
          <label class="check"><input type="checkbox" v-model="f.published" data-testid="item-published" />{{ t('published') }}</label>
          <label class="check"><input type="checkbox" v-model="f.needs_prep" />{{ t('needs_prep') }}</label>
          <label class="check"><input type="checkbox" v-model="f.limited" />{{ t('stock') }}</label>
          <input v-if="f.limited" type="number" min="0" step="1" v-model.number="f.stock" />
        </div>

        <label class="field" v-if="f.needs_prep && stations.length" style="margin-top:10px"><span>{{ t('station') }}</span>
          <select v-model="f.station_id">
            <option :value="null">{{ t('unassigned') }}</option>
            <option v-for="x in stations.filter(x => x.active || x.id === f.station_id)" :key="x.id" :value="x.id">{{ both(x.name, x.name_en) }}</option>
          </select></label>

        <h3 style="margin:14px 0 8px">{{ t('option_groups') }}</h3>
        <div v-for="(g, gi) in f.groups" :key="gi" class="card stack" style="margin-bottom:10px;background:#fafafa">
          <div class="grid2">
            <label class="field"><span>{{ t('name_zh') }}</span><input v-model="g.name" required /></label>
            <label class="field"><span>{{ t('name_en') }}</span><input v-model="g.name_en" /></label>
            <label class="field"><span>{{ t('min_sel') }}</span><input type="number" min="0" v-model.number="g.min_select" /></label>
            <label class="field"><span>{{ t('max_sel') }}</span><input type="number" min="1" v-model.number="g.max_select" /></label>
          </div>
          <div v-for="(o, oi) in g.options" :key="oi" class="row">
            <input v-model="o.name" :placeholder="t('name_zh')" required />
            <input v-model="o.name_en" :placeholder="t('name_en')" />
            <input type="number" min="0" step="0.01" v-model.number="o.extra" :placeholder="t('extra')" style="width:90px" />
            <button type="button" class="btn small danger" @click="g.options.splice(oi, 1)">×</button>
          </div>
          <div class="row between">
            <button type="button" class="btn small" @click="g.options.push({ name: '', name_en: '', extra: 0, sold_out: false })">+ {{ t('add_option') }}</button>
            <button type="button" class="btn small danger" @click="f.groups.splice(gi, 1)">{{ t('delete') }}</button>
          </div>
        </div>
        <button type="button" class="btn small" @click="f.groups.push({ name: '', name_en: '', min_select: 1, max_select: 1, options: [{ name: '', name_en: '', extra: 0, sold_out: false }] })">+ {{ t('add_group') }}</button>
      </div>
      <div class="sheet-foot row">
        <button type="button" class="btn grow" @click="emit('close')">{{ t('cancel') }}</button>
        <button class="btn primary grow" :disabled="busy">{{ t('save') }}</button>
      </div>
    </form>
  </div>
</template>

<style scoped>
.chans { display: flex; flex-wrap: wrap; gap: 4px 20px; }
.grid2 { display: grid; grid-template-columns: 1fr 1fr; gap: 0 12px; align-items: center; }
</style>
