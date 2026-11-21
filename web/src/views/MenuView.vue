<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { get, patch, post, upload } from '../api'
import { both, importError, money, t } from '../i18n'
import { currentRole, session } from '../session'
import type { Category, Item, MenuImport, Station } from '../types'
import ItemForm from './ItemForm.vue'

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

// ---- Excel import: upload, wait for the background parse, review, confirm ----
const imp = reactive<{ open: boolean; busy: boolean; error: string; data: MenuImport | null; done: string }>({ open: false, busy: false, error: '', data: null, done: '' })
function openImport() { Object.assign(imp, { open: true, busy: false, error: '', data: null, done: '' }) }
async function pickFile(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  Object.assign(imp, { busy: true, error: '', data: null, done: '' })
  try {
    const { id } = await upload<{ id: number }>(`/merchant/branches/${bid}/menu-imports?filename=${encodeURIComponent(file.name)}`, file)
    for (let i = 0; i < 60 && imp.open; i++) {
      const m = await get<MenuImport>(`/merchant/branches/${bid}/menu-imports/${id}`)
      if (m.status !== 'pending') { imp.data = m; break }
      await new Promise(r => setTimeout(r, 1000))
    }
    if (!imp.data) imp.error = t('import_parsing')
  } catch (err) { imp.error = (err as Error).message }
  imp.busy = false
}
async function applyImport() {
  if (!imp.data) return
  imp.busy = true; imp.error = ''
  try {
    const m = await post<MenuImport>(`/merchant/branches/${bid}/menu-imports/${imp.data.id}/apply`)
    imp.data = m
    imp.done = t('import_done', { c: m.created_count, u: m.updated_count })
    await load()
  } catch (err) { imp.error = (err as Error).message }
  imp.busy = false
}

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
        <button class="btn" @click="openImport">{{ t('import_excel') }}</button>
        <button class="btn" @click="newCategory">{{ t('new_category') }}</button>
        <button class="btn primary" :disabled="cats.length === 0" @click="edit(null)">{{ t('new_item') }}</button>
      </div>
    </div>
    <div v-if="error" class="banner bad">{{ error }}</div>

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

    <div class="sheet-mask" v-if="imp.open" @click.self="imp.open = false">
      <div class="sheet center wide">
        <div class="sheet-head"><h2>{{ t('import_excel') }}</h2></div>
        <div class="sheet-body stack">
          <p class="muted small">{{ t('import_hint') }}</p>
          <div class="row wrap">
            <a class="btn" :href="`/api/v1/merchant/branches/${bid}/menu-imports/template`" download>{{ t('download_template') }}</a>
            <label class="btn primary" :class="{ disabled: imp.busy }">{{ t('upload') }} .xlsx
              <input type="file" accept=".xlsx" hidden :disabled="imp.busy" data-testid="import-file" @change="pickFile" /></label>
            <span v-if="imp.busy" class="muted">{{ t('import_parsing') }}</span>
          </div>
          <div v-if="imp.error" class="banner bad">{{ imp.error }}</div>
          <div v-if="imp.done" class="banner ok">{{ imp.done }}</div>
          <template v-if="imp.data">
            <div v-if="imp.data.status === 'failed'" class="banner bad">{{ t('import_failed') }}: {{ imp.data.error }}</div>
            <template v-else>
              <div class="banner" :class="imp.data.error_rows ? 'warn' : 'info'" data-testid="import-summary">
                {{ t('import_summary', { total: imp.data.total_rows, valid: imp.data.valid_rows, bad: imp.data.error_rows }) }}</div>
              <div style="max-height:46vh;overflow:auto">
                <table class="data">
                  <thead><tr><th>{{ t('import_line') }}</th><th>{{ t('category') }}</th><th>{{ t('name_zh') }}</th><th>{{ t('price') }}</th><th>{{ t('import_result') }}</th></tr></thead>
                  <tbody><tr v-for="r in imp.data.rows" :key="r.line" :class="{ badrow: r.errors.length }">
                    <td>{{ r.line }}</td><td>{{ r.category }}</td><td>{{ r.name }}<div class="muted small">{{ r.name_en }}</div></td>
                    <td class="price"><span v-if="r.action === 'update' && r.old_price !== r.price" class="muted strike">{{ money(r.old_price ?? 0, cur) }} </span>{{ r.errors.includes('price_invalid') || r.errors.includes('price_required') ? '—' : money(r.price, cur) }}</td>
                    <td><span v-if="r.errors.length" class="small" style="color:var(--bad)">{{ r.errors.map(importError).join('；') }}</span>
                      <span v-else class="tag" :class="r.action === 'create' ? 'ok' : 'info'">{{ t('act.' + r.action) }}</span></td>
                  </tr></tbody>
                </table>
              </div>
            </template>
          </template>
        </div>
        <div class="sheet-foot row">
          <button type="button" class="btn grow" @click="imp.open = false">{{ imp.done ? t('back') : t('cancel') }}</button>
          <button v-if="imp.data?.status === 'parsed'" class="btn primary grow" :disabled="imp.busy || imp.data.valid_rows === 0" data-testid="import-apply" @click="applyImport">
            {{ t('import_apply', { n: imp.data.valid_rows }) }}</button>
        </div>
      </div>
    </div>

    <ItemForm v-if="form.open" :item="form.item" :category-id="form.categoryId" :cats="cats" :stations="stations" :cur="cur"
      :base="`/merchant/branches/${bid}`" @saved="saved" @close="form.open = false" />
  </div>
</template>

<style scoped>
.sheet.wide { max-width: 860px; width: 100%; }
.badrow td { background: var(--bad-soft); }
label.btn.disabled { opacity: .5; pointer-events: none; }
</style>
