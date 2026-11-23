<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { get, patch, post, put } from '../api'
import { both, money, t } from '../i18n'
import { currentRole, session } from '../session'
import type { Category, Item, Station } from '../types'
import ItemForm from './ItemForm.vue'
import MenuImportDialog from './MenuImportDialog.vue'

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

// A required category: every delivery or pickup order must contain something from it.
const setRequired = (c: Category) => run(() => put(`/merchant/branches/${bid}/categories/${c.id}`,
  { name: c.name, name_en: c.name_en, sort: c.sort, required: !c.required }))

// ---- Excel import (MenuImportDialog.vue) ----
const importing = ref(false)

// ---- item editor (ItemForm.vue) ----
const form = reactive<{ open: boolean; item: Item | null; categoryId: number }>({ open: false, item: null, categoryId: 0 })
function edit(it: Item | null, categoryId = 0) { Object.assign(form, { open: true, item: it, categoryId }) }
async function saved() { form.open = false; await load() }
</script>

<template>
  <div class="page stack">
    <div class="row between">
      <h1>{{ t('menu_admin') }}</h1>
      <div class="row" v-if="manager">
        <button class="btn" data-testid="open-import" @click="importing = true">{{ t('import_excel') }}</button>
        <button class="btn" @click="newCategory">{{ t('new_category') }}</button>
        <button class="btn primary" :disabled="cats.length === 0" @click="edit(null)">{{ t('new_item') }}</button>
      </div>
    </div>
    <div v-if="error" class="banner bad">{{ error }}</div>

    <div v-for="c in cats" :key="c.id" class="card" style="padding:0;overflow:auto">
      <div class="row between wrap" style="padding:10px 12px"><h2>{{ both(c.name, c.name_en) }}
          <span v-if="c.required" class="tag brand" :title="t('cat_required_hint')" data-testid="cat-required">{{ t('cat_required') }}</span></h2>
        <div class="row" v-if="manager">
          <button class="btn small" :title="t('cat_required_hint')" data-testid="toggle-required" @click="setRequired(c)">{{ c.required ? t('cat_required_off') : t('cat_required_on') }}</button>
          <button class="btn small" @click="edit(null, c.id)">+ {{ t('new_item') }}</button></div></div>
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

    <MenuImportDialog v-if="importing" :base="`/merchant/branches/${bid}`" :cur="cur" @imported="load" @close="importing = false" />

    <ItemForm v-if="form.open" :item="form.item" :category-id="form.categoryId" :cats="cats" :stations="stations" :cur="cur"
      :base="`/merchant/branches/${bid}`" @saved="saved" @close="form.open = false" />
  </div>
</template>

<style scoped>
</style>
