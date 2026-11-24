<script setup lang="ts">
// The item editor, shared by the merchant's menu page and the platform back
// office. `base` is the API prefix of the branch, for example
// /merchant/branches/3 or /admin/branches/3.
import { computed, onMounted, reactive, ref } from 'vue'
import { ApiError, get, post, put } from '../api'
import { both, t, toMajor, toMinor } from '../i18n'
import type { Item, NoteTemplate, Station, ToppingTemplate } from '../types'
import '../notes'
import '../toppings'
import ImageUpload from './ImageUpload.vue'
import PhotoList from './PhotoList.vue'
import './manualStrings'

const props = defineProps<{ item: Item | null; categoryId?: number; cats: { id: number; name: string }[]; stations: Station[]; cur: string; base: string }>()
const emit = defineEmits<{ saved: []; close: []; 'manage-notes': []; 'manage-toppings': [] }>()

interface OptForm { name: string; name_en: string; extra: number; sold_out: boolean }
interface GroupForm { name: string; name_en: string; min_select: number; max_select: number; options: OptForm[] }
interface Form { id: number; category_id: number; code: string; name: string; name_en: string; description: string; image_url: string
  photos: string[]; details: string
  price: number; published: boolean; sold_out: boolean; limited: boolean; stock: number; needs_prep: boolean
  delivery_available: boolean; pickup_available: boolean; dine_in_available: boolean; sort: number; station_id: number | null; groups: GroupForm[]
  note_template_ids: number[]; topping_template_ids: number[] }

function blank(): Form {
  return { id: 0, category_id: 0, code: '', name: '', name_en: '', description: '', image_url: '', photos: [], details: '', price: 0, published: false, sold_out: false,
    limited: false, stock: 0, needs_prep: true, delivery_available: true, pickup_available: true, dine_in_available: true, sort: 0, station_id: null, groups: [],
    note_template_ids: [], topping_template_ids: [] }
}

const it = props.item
const f = reactive<Form>(it ? {
  id: it.id, category_id: it.category_id, code: it.code ?? '', name: it.name, name_en: it.name_en, description: it.description, image_url: it.image_url,
  photos: [...(it.photos ?? [])], details: it.details ?? '',
  price: toMajor(it.price), published: it.published, sold_out: it.sold_out, limited: it.stock_qty !== null, stock: it.stock_qty ?? 0,
  needs_prep: it.needs_prep, delivery_available: it.delivery_available, pickup_available: it.pickup_available, dine_in_available: it.dine_in_available ?? true, sort: it.sort, station_id: it.station_id,
  // groups that come from a topping template are edited on the toppings page, not here
  groups: it.groups.filter(g => !g.topping_template_id).map(g => ({ name: g.name, name_en: g.name_en, min_select: g.min_select, max_select: g.max_select,
    options: g.options.map(o => ({ name: o.name, name_en: o.name_en, extra: toMajor(o.price_delta), sold_out: o.sold_out })) })),
  note_template_ids: [...(it.note_template_ids ?? [])],
  topping_template_ids: [...(it.topping_template_ids ?? [])],
} : { ...blank(), category_id: props.categoryId || props.cats[0]?.id || 0 })
const error = ref('')
const busy = ref(false)

// The store's flavour note templates (docs/NOTE_TEMPLATE_DESIGN.md §5.2): up to three per dish.
const MAX_NOTES = 3
const templates = ref<NoteTemplate[]>([])
const showAllNotes = ref(false)
onMounted(async () => {
  templates.value = (await get<{ templates: NoteTemplate[] }>(`${props.base}/note-templates`).catch(() => ({ templates: [] }))).templates
})
const shownTemplates = computed(() => showAllNotes.value || templates.value.length <= 4 ? templates.value
  : templates.value.filter((x, i) => i < 3 || f.note_template_ids.includes(x.id)))
function toggleNote(id: number) {
  const i = f.note_template_ids.indexOf(id)
  if (i >= 0) f.note_template_ids.splice(i, 1)
  else if (f.note_template_ids.length < MAX_NOTES) f.note_template_ids.push(id)
}
// The store's topping (加料) templates: up to three per dish. null when the
// list could not be loaded (the platform back office has no such endpoint), in
// which case the dish keeps the templates it has.
const MAX_TOPPINGS = 3
const toppings = ref<ToppingTemplate[] | null>(null)
onMounted(async () => {
  toppings.value = (await get<{ templates: ToppingTemplate[] }>(`${props.base}/topping-templates`).catch(() => null))?.templates ?? null
})
function toggleTopping(id: number) {
  const i = f.topping_template_ids.indexOf(id)
  if (i >= 0) f.topping_template_ids.splice(i, 1)
  else if (f.topping_template_ids.length < MAX_TOPPINGS) f.topping_template_ids.push(id)
}
const toppingSummary = (x: ToppingTemplate) => x.options.map(o => (o.code ? o.code + ' ' : '') + o.name).join('、')
const tplSummary = (x: NoteTemplate) => x.groups.map(g => (g.name ? g.name + '：' : '') + g.tags.filter(t => t.active).map(t => t.name).join('、')).join(' · ')

async function save() {
  const body = {
    category_id: f.category_id, code: f.code.trim().toUpperCase(), name: f.name, name_en: f.name_en, description: f.description, image_url: f.image_url, photos: f.photos, details: f.details,
    price: toMinor(f.price), published: f.published, sold_out: f.sold_out, stock_qty: f.limited ? Math.max(0, Math.floor(f.stock)) : null,
    needs_prep: f.needs_prep, delivery_available: f.delivery_available, pickup_available: f.pickup_available, dine_in_available: f.dine_in_available, sort: f.sort, station_id: f.needs_prep ? f.station_id : null,
    groups: f.groups.map(g => ({ name: g.name, name_en: g.name_en, min_select: Number(g.min_select), max_select: Number(g.max_select),
      options: g.options.map(o => ({ name: o.name, name_en: o.name_en, price_delta: toMinor(o.extra), sold_out: o.sold_out })) })),
    note_template_ids: f.note_template_ids,
    ...(toppings.value ? { topping_template_ids: f.topping_template_ids } : {}),
  }
  error.value = ''; busy.value = true
  try {
    await (f.id ? put(`${props.base}/items/${f.id}`, body) : post(`${props.base}/items`, body))
    emit('saved')
  } catch (e) {
    error.value = e instanceof ApiError && e.code === 'CODE_TAKEN' ? t('vo.code_taken', { name: e.details?.name ?? '' }) : (e as Error).message
  } finally { busy.value = false }
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
          <label class="field"><span>{{ t('vo.code') }}</span><input v-model="f.code" maxlength="12" data-testid="item-code" :placeholder="t('vo.code_ph')" /></label>
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

        <section class="notes" data-testid="item-notes">
          <h3>{{ t('nt.tab_templates') }}</h3>
          <div class="muted small" style="margin-bottom:8px">{{ t('nt.form_hint') }}</div>
          <div v-if="!templates.length" class="muted small">{{ t('nt.none_yet') }} <a href="#" @click.prevent="emit('manage-notes')">{{ t('nt.manage') }}</a></div>
          <div class="tplpick">
            <label v-for="x in shownTemplates" :key="x.id" :class="{ on: f.note_template_ids.includes(x.id), off: !f.note_template_ids.includes(x.id) && f.note_template_ids.length >= MAX_NOTES }">
              <input type="checkbox" :checked="f.note_template_ids.includes(x.id)" :disabled="!f.note_template_ids.includes(x.id) && f.note_template_ids.length >= MAX_NOTES"
                :data-testid="'item-note-' + x.name" @change="toggleNote(x.id)" />
              <div><b>{{ x.icon }} {{ x.name }}</b><small>{{ tplSummary(x) }}</small></div>
            </label>
          </div>
          <div v-if="templates.length" class="row between small" style="margin-top:6px">
            <a href="#" @click.prevent="emit('manage-notes')">{{ t('nt.manage') }}</a>
            <a v-if="shownTemplates.length < templates.length" href="#" @click.prevent="showAllNotes = true">+{{ templates.length - shownTemplates.length }}</a>
          </div>
        </section>

        <section v-if="toppings" class="notes" data-testid="item-toppings">
          <h3>{{ t('tp.form') }}</h3>
          <div class="muted small" style="margin-bottom:8px">{{ t('tp.form_hint') }}</div>
          <div v-if="!toppings.length" class="muted small">{{ t('tp.none') }} <a href="#" @click.prevent="emit('manage-toppings')">{{ t('tp.manage') }}</a></div>
          <div class="tplpick">
            <label v-for="x in toppings" :key="x.id" :class="{ on: f.topping_template_ids.includes(x.id), off: !f.topping_template_ids.includes(x.id) && f.topping_template_ids.length >= MAX_TOPPINGS }">
              <input type="checkbox" :checked="f.topping_template_ids.includes(x.id)" :disabled="!f.topping_template_ids.includes(x.id) && f.topping_template_ids.length >= MAX_TOPPINGS"
                :data-testid="'item-topping-' + x.name" @change="toggleTopping(x.id)" />
              <div><b>{{ x.name }}</b><small>{{ toppingSummary(x) }}</small></div>
            </label>
          </div>
          <div v-if="toppings.length" class="small" style="margin-top:6px"><a href="#" @click.prevent="emit('manage-toppings')">{{ t('tp.manage') }}</a></div>
        </section>

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
.notes { margin-top: 14px; }
.notes h3 { margin: 0 0 4px; }
.tplpick { display: grid; gap: 8px; }
.tplpick label { display: flex; align-items: flex-start; gap: 10px; border: 1px solid var(--line); border-radius: 10px; padding: 9px 12px; cursor: pointer; }
.tplpick label.on { border-color: var(--brand); background: var(--brand-soft); }
.tplpick label.off { opacity: .55; cursor: default; }
.tplpick small { display: block; color: var(--muted); font-size: 12.5px; margin-top: 2px; overflow-wrap: anywhere; }
</style>
