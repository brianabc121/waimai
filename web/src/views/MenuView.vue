<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { get, patch, post, put } from '../api'
import { both, money, t, toMajor, toMinor } from '../i18n'
import { currentRole, session } from '../session'
import type { Category, Item, Station } from '../types'

const bid = session.branchId!
const cur = currentRole()?.currency ?? 'PHP'
const manager = computed(() => ['owner', 'manager'].includes(currentRole()?.role ?? ''))
const cats = ref<Category[]>([])
const stations = ref<Station[]>([])
const error = ref('')

async function load() {
  const m = await get(`/merchant/branches/${bid}/menu`)
  cats.value = m.categories; stations.value = m.stations
}
const stationName = (id: number | null) => { const x = stations.value.find(s => s.id === id); return x ? both(x.name, x.name_en) : '' }
onMounted(load)

async function run(fn: () => Promise<unknown>) {
  error.value = ''
  try { await fn(); await load(); return true } catch (e) { error.value = (e as Error).message; return false }
}

const quick = (it: Item, body: Record<string, unknown>) => run(() => patch(`/merchant/branches/${bid}/items/${it.id}`, body))

function setStock(it: Item) {
  const v = prompt(`${t('stock')} (${t('unlimited')}: ${t('clear')})`, it.stock_qty === null ? '' : String(it.stock_qty))
  if (v === null) return
  void quick(it, { stock_set: true, stock_qty: v.trim() === '' ? null : Math.max(0, Math.floor(Number(v))) })
}

function archive(it: Item) { if (confirm(`${t('archive')}: ${it.name}?`)) void quick(it, { archived: true }) }

async function newCategory() {
  const name = prompt(t('name_zh'))
  if (!name?.trim()) return
  const en = prompt(t('name_en')) ?? ''
  await run(() => post(`/merchant/branches/${bid}/categories`, { name, name_en: en, sort: cats.value.length }))
}

// ---- item editor ----
interface OptForm { name: string; name_en: string; extra: number; sold_out: boolean }
interface GroupForm { name: string; name_en: string; min_select: number; max_select: number; options: OptForm[] }
interface Form { id: number; category_id: number; name: string; name_en: string; description: string; image_url: string
  price: number; published: boolean; sold_out: boolean; limited: boolean; stock: number; needs_prep: boolean
  delivery_available: boolean; pickup_available: boolean; sort: number; station_id: number | null; groups: GroupForm[] }

const form = reactive<{ open: boolean; f: Form }>({ open: false, f: blank() })

function blank(): Form {
  return { id: 0, category_id: 0, name: '', name_en: '', description: '', image_url: '', price: 0, published: false, sold_out: false,
    limited: false, stock: 0, needs_prep: true, delivery_available: true, pickup_available: true, sort: 0, station_id: null, groups: [] }
}

function edit(it: Item | null, categoryId = 0) {
  form.f = it ? {
    id: it.id, category_id: it.category_id, name: it.name, name_en: it.name_en, description: it.description, image_url: it.image_url,
    price: toMajor(it.price), published: it.published, sold_out: it.sold_out, limited: it.stock_qty !== null, stock: it.stock_qty ?? 0,
    needs_prep: it.needs_prep, delivery_available: it.delivery_available, pickup_available: it.pickup_available, sort: it.sort, station_id: it.station_id,
    groups: it.groups.map(g => ({ name: g.name, name_en: g.name_en, min_select: g.min_select, max_select: g.max_select,
      options: g.options.map(o => ({ name: o.name, name_en: o.name_en, extra: toMajor(o.price_delta), sold_out: o.sold_out })) })),
  } : { ...blank(), category_id: categoryId || cats.value[0]?.id || 0 }
  form.open = true
}

async function save() {
  const f = form.f
  const body = {
    category_id: f.category_id, name: f.name, name_en: f.name_en, description: f.description, image_url: f.image_url,
    price: toMinor(f.price), published: f.published, sold_out: f.sold_out, stock_qty: f.limited ? Math.max(0, Math.floor(f.stock)) : null,
    needs_prep: f.needs_prep, delivery_available: f.delivery_available, pickup_available: f.pickup_available, sort: f.sort, station_id: f.needs_prep ? f.station_id : null,
    groups: f.groups.map(g => ({ name: g.name, name_en: g.name_en, min_select: Number(g.min_select), max_select: Number(g.max_select),
      options: g.options.map(o => ({ name: o.name, name_en: o.name_en, price_delta: toMinor(o.extra), sold_out: o.sold_out })) })),
  }
  const ok = await run(() => f.id ? put(`/merchant/branches/${bid}/items/${f.id}`, body) : post(`/merchant/branches/${bid}/items`, body))
  if (ok) form.open = false
}
</script>

<template>
  <div class="page stack">
    <div class="row between">
      <h1>{{ t('menu_admin') }}</h1>
      <div class="row" v-if="manager">
        <button class="btn" @click="newCategory">{{ t('new_category') }}</button>
        <button class="btn primary" :disabled="cats.length === 0" @click="edit(null)">{{ t('new_item') }}</button>
      </div>
    </div>
    <div v-if="error && !form.open" class="banner bad">{{ error }}</div>

    <div v-for="c in cats" :key="c.id" class="card" style="padding:0;overflow:auto">
      <div class="row between" style="padding:10px 12px"><h2>{{ both(c.name, c.name_en) }}</h2>
        <button v-if="manager" class="btn small" @click="edit(null, c.id)">+ {{ t('new_item') }}</button></div>
      <table class="data">
        <tbody>
          <tr v-for="it in c.items" :key="it.id">
            <td><b>{{ it.name }}</b><div class="muted small">{{ it.name_en }}</div></td>
            <td class="price">{{ money(it.price, cur) }}</td>
            <td>
              <span class="tag" :class="it.published ? 'ok' : ''">{{ it.published ? t('published') : t('draft') }}</span>
              <span class="tag bad" v-if="it.sold_out" style="margin-left:4px">{{ t('sold_out') }}</span>
              <span class="tag info" v-if="it.stock_qty !== null" style="margin-left:4px">{{ t('stock') }} {{ it.stock_qty }}</span>
              <span class="tag" v-if="!it.needs_prep" style="margin-left:4px">{{ t('no_prep') }}</span>
              <span class="tag info" v-else-if="it.station_id" style="margin-left:4px">{{ stationName(it.station_id) }}</span>
              <span class="tag warn" v-else-if="stations.length" style="margin-left:4px">{{ t('unassigned') }}</span>
            </td>
            <td class="right">
              <div class="row wrap" style="justify-content:flex-end;gap:6px">
                <button class="btn small" @click="quick(it, { sold_out: !it.sold_out })">{{ it.sold_out ? t('restore') : t('mark_sold_out') }}</button>
                <button class="btn small" @click="setStock(it)">{{ t('stock') }}</button>
                <template v-if="manager">
                  <button class="btn small" @click="quick(it, { published: !it.published })">{{ it.published ? t('unpublish') : t('publish') }}</button>
                  <button class="btn small" @click="edit(it)">{{ t('edit') }}</button>
                  <button class="btn small danger" @click="archive(it)">{{ t('archive') }}</button>
                </template>
              </div>
            </td>
          </tr>
          <tr v-if="c.items.length === 0"><td class="muted">—</td></tr>
        </tbody>
      </table>
    </div>

    <div class="sheet-mask" v-if="form.open" @click.self="form.open = false">
      <form class="sheet center" @submit.prevent="save">
        <div class="sheet-head"><h2>{{ form.f.id ? t('edit') : t('new_item') }}</h2></div>
        <div class="sheet-body">
          <div v-if="error" class="banner bad" style="margin-bottom:12px">{{ error }}</div>
          <div class="grid2">
            <label class="field"><span>{{ t('name_zh') }}</span><input v-model="form.f.name" required /></label>
            <label class="field"><span>{{ t('name_en') }}</span><input v-model="form.f.name_en" /></label>
            <label class="field"><span>{{ t('category') }}</span><select v-model="form.f.category_id">
              <option v-for="c in cats" :key="c.id" :value="c.id">{{ c.name }}</option></select></label>
            <label class="field"><span>{{ t('price') }} ({{ cur }})</span><input type="number" min="0" step="0.01" v-model.number="form.f.price" required /></label>
          </div>
          <label class="field"><span>{{ t('description') }}</span><input v-model="form.f.description" /></label>
          <label class="field"><span>{{ t('image_url') }}</span><input v-model="form.f.image_url" placeholder="https://" /></label>
          <div class="grid2">
            <label class="check"><input type="checkbox" v-model="form.f.published" />{{ t('published') }}</label>
            <label class="check"><input type="checkbox" v-model="form.f.needs_prep" />{{ t('needs_prep') }}</label>
            <label class="check"><input type="checkbox" v-model="form.f.delivery_available" />{{ t('delivery') }}</label>
            <label class="check"><input type="checkbox" v-model="form.f.pickup_available" />{{ t('pickup') }}</label>
            <label class="check"><input type="checkbox" v-model="form.f.limited" />{{ t('stock') }}</label>
            <input v-if="form.f.limited" type="number" min="0" step="1" v-model.number="form.f.stock" />
          </div>

          <label class="field" v-if="form.f.needs_prep && stations.length" style="margin-top:10px"><span>{{ t('station') }}</span>
            <select v-model="form.f.station_id">
              <option :value="null">{{ t('unassigned') }}</option>
              <option v-for="x in stations.filter(x => x.active || x.id === form.f.station_id)" :key="x.id" :value="x.id">{{ both(x.name, x.name_en) }}</option>
            </select></label>

          <h3 style="margin:14px 0 8px">{{ t('option_groups') }}</h3>
          <div v-for="(g, gi) in form.f.groups" :key="gi" class="card stack" style="margin-bottom:10px;background:#fafafa">
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
              <button type="button" class="btn small danger" @click="form.f.groups.splice(gi, 1)">{{ t('delete') }}</button>
            </div>
          </div>
          <button type="button" class="btn small" @click="form.f.groups.push({ name: '', name_en: '', min_select: 1, max_select: 1, options: [{ name: '', name_en: '', extra: 0, sold_out: false }] })">+ {{ t('add_group') }}</button>
        </div>
        <div class="sheet-foot row">
          <button type="button" class="btn grow" @click="form.open = false">{{ t('cancel') }}</button>
          <button class="btn primary grow">{{ t('save') }}</button>
        </div>
      </form>
    </div>
  </div>
</template>

<style scoped>
.grid2 { display: grid; grid-template-columns: 1fr 1fr; gap: 0 12px; align-items: center; }
</style>
