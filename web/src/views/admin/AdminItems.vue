<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { del, get, patch, post, put } from '../../api'
import { money, t } from '../../i18n'
import type { Category, Item, Station } from '../../types'
import ItemForm from '../ItemForm.vue'
import MenuImportDialog from '../MenuImportDialog.vue'
import MenuTranslations from '../MenuTranslations.vue'
import NoteTemplates from '../NoteTemplates.vue'
import '../translateStrings'
import Pager from './Pager.vue'
import type { AdminBranch, AdminItemRow } from './types'
import { fmtDateTime, qs } from './util'

const SIZE = 20
const route = useRoute()
const branches = ref<AdminBranch[]>([])
const merchantId = ref(Number(route.query.merchant_id) || 0)
const branchId = ref(Number(route.query.branch_id) || 0)
const q = ref('')
const status = ref('')
const page = ref(1)
const list = reactive<{ items: AdminItemRow[]; total: number; counts: Record<string, number> }>({ items: [], total: 0, counts: {} })
const error = ref('')
const picked = ref<number[]>([])
// Excel import for the selected store; ?import=1 (from the merchant page) opens it
const importing = ref(false)

const merchants = computed(() => {
  const seen = new Map<number, string>()
  for (const b of branches.value) seen.set(b.merchant_id, b.merchant_name)
  return [...seen].map(([id, name]) => ({ id, name }))
})
const stores = computed(() => branches.value.filter(b => !merchantId.value || b.merchant_id === merchantId.value))
const store = computed(() => branches.value.find(b => b.id === branchId.value) ?? null)

async function load() {
  try {
    Object.assign(list, await get('/admin/items' + qs({ q: q.value, merchant_id: merchantId.value, branch_id: branchId.value, status: status.value, page: page.value, page_size: SIZE })))
    picked.value = picked.value.filter(id => list.items.some(i => i.id === id))
    error.value = ''
  } catch (e) { error.value = (e as Error).message }
}
onMounted(async () => {
  try { branches.value = (await get('/admin/branches')).branches } catch (e) { error.value = (e as Error).message }
  await load()
  if (route.query.import === '1' && store.value) importing.value = true
})
function reload() { page.value = 1; picked.value = []; void load() }
function pickMerchant() { if (store.value && store.value.merchant_id !== merchantId.value) branchId.value = 0; reload() }
function pickStore() { if (store.value) merchantId.value = store.value.merchant_id; reload() }
let timer = 0
watch(q, () => { clearTimeout(timer); timer = window.setTimeout(reload, 250) })
function tab(s: string) { status.value = s; reload() }
function go(p: number) { page.value = p; void load() }

async function run(fn: () => Promise<unknown>) {
  error.value = ''
  try { await fn(); await load(); return true } catch (e) { error.value = (e as Error).message; return false }
}
const base = (bid: number) => `/admin/branches/${bid}`
const quick = (it: AdminItemRow, body: Record<string, unknown>) => run(() => patch(`${base(it.branch_id)}/items/${it.id}`, body))
function remove(it: AdminItemRow) { if (confirm(t('ad.delete_item_confirm', { name: it.name }))) void quick(it, { archived: true }) }
function setStock(it: AdminItemRow) {
  const v = prompt(`${t('stock')} (${t('unlimited')}: ${t('clear')})`, it.stock_qty === null ? '' : String(it.stock_qty))
  if (v === null) return
  void quick(it, { stock_set: true, stock_qty: v.trim() === '' ? null : Math.max(0, Math.floor(Number(v))) })
}
const bulk = (action: string) => run(async () => { await post(`${base(branchId.value)}/items/bulk`, { ids: picked.value, action }); picked.value = [] })
const allPicked = computed(() => list.items.length > 0 && picked.value.length === list.items.length)
function toggleAll() { picked.value = allPicked.value ? [] : list.items.map(i => i.id) }

// ---- item editor: needs the branch's categories, stations and the full item ----
const form = reactive<{ open: boolean; bid: number; cur: string; item: Item | null; cats: Category[]; stations: Station[] }>({ open: false, bid: 0, cur: 'PHP', item: null, cats: [], stations: [] })
async function edit(row: AdminItemRow | null) {
  const bid = row?.branch_id ?? branchId.value
  error.value = ''
  try {
    const m = await get<{ branch: { currency: string }; categories: Category[]; stations: Station[] }>(`${base(bid)}/menu`)
    const item = row ? m.categories.flatMap(c => c.items).find(i => i.id === row.id) ?? null : null
    if (row && !item) { await load(); return }
    Object.assign(form, { open: true, bid, cur: m.branch.currency, item, cats: m.categories, stations: m.stations })
  } catch (e) { error.value = (e as Error).message }
}
async function saved() { form.open = false; await load() }

// ---- the store's menu in other languages ----
// The store's flavour note templates (docs/NOTE_TEMPLATE_DESIGN.md 5.4), edited as the platform.
const notes = reactive<{ open: boolean; cats: Category[] }>({ open: false, cats: [] })
async function openNotes() {
  try { notes.cats = (await get(`${base(branchId.value)}/menu`)).categories; notes.open = true } catch (e) { error.value = (e as Error).message }
}
const langs = reactive<{ open: boolean; cats: Category[] }>({ open: false, cats: [] })
async function openLangs() {
  try { langs.cats = (await get(`${base(branchId.value)}/menu`)).categories; langs.open = true } catch (e) { error.value = (e as Error).message }
}

// ---- categories of the selected store ----
const cats = reactive<{ open: boolean; error: string; rows: Category[]; name: string; name_en: string }>({ open: false, error: '', rows: [], name: '', name_en: '' })
async function loadCats() { cats.rows = (await get(`${base(branchId.value)}/menu`)).categories }
async function openCats() { Object.assign(cats, { open: true, error: '', name: '', name_en: '' }); await catRun(async () => {}) }
async function catRun(fn: () => Promise<unknown>) {
  cats.error = ''
  try { await fn(); await loadCats(); await load() } catch (e) { cats.error = (e as Error).message }
}
const addCat = () => catRun(async () => {
  await post(`${base(branchId.value)}/categories`, { name: cats.name, name_en: cats.name_en, sort: cats.rows.length })
  cats.name = ''; cats.name_en = ''
})
const saveCat = (c: Category) => catRun(() => put(`${base(branchId.value)}/categories/${c.id}`, { name: c.name, name_en: c.name_en, sort: Number(c.sort) }))
function removeCat(c: Category) { if (confirm(t('ad.cat_delete_confirm', { name: c.name }))) void catRun(() => del(`${base(branchId.value)}/categories/${c.id}`)) }
</script>

<template>
  <div class="adm-page">
    <div class="row wrap">
      <select v-model.number="merchantId" style="width:170px" data-testid="pick-merchant" @change="pickMerchant">
        <option :value="0">{{ t('ad.all_merchants') }}</option>
        <option v-for="m in merchants" :key="m.id" :value="m.id">{{ m.name }}</option>
      </select>
      <select v-model.number="branchId" style="width:170px" data-testid="pick-store" @change="pickStore">
        <option :value="0">{{ t('ad.all_stores') }}</option>
        <option v-for="b in stores" :key="b.id" :value="b.id">{{ merchantId ? b.name : b.merchant_name + ' · ' + b.name }}</option>
      </select>
      <input v-model="q" :placeholder="t('ad.search_items')" style="width:240px" data-testid="item-search" />
      <div class="tabs">
        <button :class="{ on: status === '' }" @click="tab('')">{{ t('ad.all') }} {{ list.counts.all ?? 0 }}</button>
        <button :class="{ on: status === 'on_sale' }" @click="tab('on_sale')">{{ t('ad.i.on_sale') }} {{ list.counts.on_sale ?? 0 }}</button>
        <button :class="{ on: status === 'off' }" @click="tab('off')">{{ t('ad.i.off') }} {{ list.counts.off ?? 0 }}</button>
        <button :class="{ on: status === 'sold_out' }" @click="tab('sold_out')">{{ t('ad.i.sold_out') }} {{ list.counts.sold_out ?? 0 }}</button>
      </div>
      <span class="grow"></span>
      <button class="btn" :disabled="!store" data-testid="open-import" :title="store ? '' : t('ad.pick_store')" @click="importing = true">{{ t('import_excel') }}</button>
      <button class="btn" :disabled="!store" data-testid="open-langs" :title="store ? t('mtr.open_hint') : t('ad.pick_store')" @click="openLangs">{{ t('mtr.open') }}</button>
      <button class="btn" :disabled="!store" data-testid="open-notes" @click="openNotes">{{ t('nt.tab_templates') }}</button>
      <button class="btn" :disabled="!store" data-testid="manage-cats" @click="openCats">{{ t('ad.manage_cats') }}</button>
      <button class="btn primary" :disabled="!store" data-testid="new-item" @click="edit(null)">＋ {{ t('new_item') }}</button>
    </div>
    <div v-if="store" class="banner warn" data-testid="editing-banner">{{ t('ad.editing_as_admin', { name: store.merchant_name + ' · ' + store.name }) }}</div>
    <div v-else class="banner info">{{ t('ad.pick_store') }}</div>
    <div v-if="error" class="banner bad">{{ error }}</div>

    <div class="card flush">
      <table class="data manycols">
        <thead><tr>
          <th style="width:32px"><input v-if="store" type="checkbox" :checked="allPicked" @change="toggleAll" /></th>
          <th>{{ t('ad.item') }}</th><th v-if="!store">{{ t('ad.branch') }}</th><th>{{ t('category') }}</th><th>{{ t('station') }}</th>
          <th class="right">{{ t('price') }}</th><th>{{ t('ad.on_shelf') }}</th><th>{{ t('ad.i.sold_out') }}</th><th class="right">{{ t('stock') }}</th><th>{{ t('ad.updated') }}</th><th></th>
        </tr></thead>
        <tbody>
          <tr v-for="it in list.items" :key="it.id" :data-item="it.name">
            <td><input v-if="store" type="checkbox" :value="it.id" v-model="picked" /></td>
            <td><div class="row">
              <img v-if="it.image_url" class="thumb" :src="it.image_url" alt="" /><span v-else class="thumb">{{ it.name.slice(0, 1) }}</span>
              <div><b :class="{ muted: !it.published }">{{ it.name }}</b> <span v-if="!it.published" class="tag">{{ t('ad.i.off') }}</span>
                <div class="small muted">{{ it.name_en }}<template v-if="it.group_count"> · {{ t('ad.n_groups', { n: it.group_count }) }}</template></div></div>
            </div></td>
            <td v-if="!store">{{ it.merchant_name }}<div class="small muted">{{ it.branch_name }}</div></td>
            <td>{{ it.category_name }}</td>
            <td :class="{ muted: !it.needs_prep || !it.station_name }">{{ !it.needs_prep ? t('no_prep') : it.station_name || '—' }}</td>
            <td class="right num">{{ money(it.price, it.currency) }}</td>
            <td><button class="sw" :class="{ on: it.published }" data-testid="sw-published" :aria-pressed="it.published" :aria-label="t('ad.on_shelf')" @click="quick(it, { published: !it.published })"></button></td>
            <td><button class="sw warn" :class="{ on: it.sold_out }" data-testid="sw-sold-out" :aria-pressed="it.sold_out" :aria-label="t('ad.i.sold_out')" @click="quick(it, { sold_out: !it.sold_out })"></button></td>
            <td class="right num"><button class="btn link" @click="setStock(it)">{{ it.stock_qty === null ? t('unlimited') : it.stock_qty }}</button></td>
            <td class="num">{{ fmtDateTime(it.updated_at) }}</td>
            <td><div class="acts"><button class="btn small" data-testid="edit-item" @click="edit(it)">{{ t('edit') }}</button>
              <button class="btn small danger" data-testid="delete-item" @click="remove(it)">{{ t('delete') }}</button></div></td>
          </tr>
          <tr v-if="list.items.length === 0"><td colspan="11" class="muted">{{ t('ad.empty') }}</td></tr>
        </tbody>
      </table>
    </div>
    <Pager :total="list.total" :page="page" :size="SIZE" @go="go">
      <template v-if="store">
        <span data-testid="picked">{{ t('ad.selected_n', { n: picked.length }) }}</span>
        <button class="btn small" :disabled="!picked.length" data-testid="bulk-publish" @click="bulk('publish')">{{ t('ad.bulk_publish') }}</button>
        <button class="btn small" :disabled="!picked.length" data-testid="bulk-unpublish" @click="bulk('unpublish')">{{ t('ad.bulk_unpublish') }}</button>
        <button class="btn small" :disabled="!picked.length" @click="bulk('sold_out')">{{ t('ad.bulk_sold_out') }}</button>
        <button class="btn small" :disabled="!picked.length" @click="bulk('in_stock')">{{ t('ad.bulk_in_stock') }}</button>
      </template>
    </Pager>
    <p class="small muted">{{ t('ad.items_foot') }} {{ t('ad.excel_hint') }}</p>

    <MenuTranslations v-if="langs.open && store" :base="base(store.id)" :cats="langs.cats" @close="langs.open = false" />
    <MenuImportDialog v-if="importing && store" :base="base(store.id)" :cur="store.currency"
      :title="t('import_excel') + ' · ' + store.merchant_name + ' · ' + store.name" @imported="load" @close="importing = false" />

    <template v-if="notes.open && store">
      <div class="adm-mask" @click="notes.open = false"></div>
      <div class="adm-drawer" style="width:min(1100px,100vw);background:var(--bg)" data-testid="notes-drawer">
        <div class="dh row between"><h2>{{ t('nt.tab_templates') }} · {{ store.name }}</h2><button class="btn small" @click="notes.open = false">✕</button></div>
        <div class="db"><NoteTemplates :base="base(store.id)" :cats="notes.cats" :can-edit="true" @changed="load" /></div>
      </div>
    </template>

    <ItemForm v-if="form.open" :item="form.item" :cats="form.cats" :stations="form.stations" :cur="form.cur" :base="base(form.bid)"
      @saved="saved" @close="form.open = false" @manage-notes="form.open = false; openNotes()" />

    <template v-if="cats.open && store">
      <div class="adm-mask" @click="cats.open = false"></div>
      <div class="adm-drawer" data-testid="cats-drawer">
        <div class="dh"><h2>{{ t('ad.manage_cats') }} · {{ store.name }}</h2><div class="small muted">{{ t('ad.cat_hint') }}</div></div>
        <div class="db">
          <div v-if="cats.error" class="banner bad" style="margin-bottom:12px">{{ cats.error }}</div>
          <div v-for="c in cats.rows" :key="c.id" class="row" style="margin-bottom:8px" :data-cat="c.name">
            <input v-model="c.name" :placeholder="t('name_zh')" />
            <input v-model="c.name_en" :placeholder="t('name_en')" />
            <input v-model.number="c.sort" type="number" style="width:64px" :title="t('ad.sort')" />
            <span class="small muted num" style="width:28px;text-align: end">{{ c.items.length }}</span>
            <button class="btn small" @click="saveCat(c)">{{ t('save') }}</button>
            <button class="btn small danger" data-testid="delete-cat" @click="removeCat(c)">×</button>
          </div>
          <form class="row" style="margin-top:14px" @submit.prevent="addCat">
            <input v-model="cats.name" name="cat_name" :placeholder="t('name_zh')" required />
            <input v-model="cats.name_en" :placeholder="t('name_en')" />
            <button class="btn primary">＋ {{ t('new_category') }}</button>
          </form>
        </div>
        <div class="df"><button type="button" class="btn" @click="cats.open = false">{{ t('back') }}</button></div>
      </div>
    </template>
  </div>
</template>
