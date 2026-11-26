<script setup lang="ts">
// The merchant's menu page (菜单管理), in the customer pages' deep red, paper
// and gold. Three tabs: the dishes (a category rail with counts, search and
// filters, the dishes grouped by category as a list or a grid, paging, drag
// to reorder), the flavour note templates (NoteTemplates.vue) and the topping
// templates (ToppingTemplates.vue).
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { get, patch, post, put } from '../api'
import { both, mn, money, t } from '../i18n'
import { can, currentRole, session } from '../session'
import type { Category, Item, Station } from '../types'
import type { BranchPlatform } from '../platforms'
import ItemForm from './ItemForm.vue'
import MenuImportDialog from './MenuImportDialog.vue'
import MenuTranslations from './MenuTranslations.vue'
import './translateStrings'
import '../notes'
import NoteTemplates from './NoteTemplates.vue'
import '../toppings'
import ToppingTemplates from './ToppingTemplates.vue'
import '../menuPage'
import MxIcon from './MxIcon.vue'
import { useRoute, useRouter } from 'vue-router'

const bid = session.branchId!
const base = `/merchant/branches/${bid}`
const cur = currentRole()?.currency ?? 'PHP'
const manager = computed(() => can('menu.edit'))
const translator = computed(() => can('menu.translate'))
const soldOut = computed(() => can('menu.soldout', 'menu.edit', 'kitchen'))
const cats = ref<Category[]>([])
const loaded = ref(false)
const route = useRoute(), router = useRouter()
type Tab = 'items' | 'notes' | 'toppings'
const tab = computed<Tab>(() => route.query.tab === 'notes' ? 'notes' : route.query.tab === 'toppings' ? 'toppings' : 'items')
const setTab = (x: Tab) => router.replace({ query: { ...route.query, tab: x === 'items' ? undefined : x } })
const toppingName = (id: number) => toppings.value.find(x => x.id === id)?.name ?? ''
const toppings = ref<{ id: number; name: string }[]>([])
async function loadToppings() { toppings.value = (await get<{ templates: { id: number; name: string }[] }>(`${base}/topping-templates`).catch(() => ({ templates: [] }))).templates }
onMounted(loadToppings)
const tplName = (id: number) => tpls.value.find(x => x.id === id)?.name ?? ''
const tpls = ref<{ id: number; name: string }[]>([])
async function loadTpls() { tpls.value = (await get<{ templates: { id: number; name: string }[] }>(`${base}/note-templates`).catch(() => ({ templates: [] }))).templates }
onMounted(loadTpls)
const stations = ref<Station[]>([])
const platforms = ref<BranchPlatform[]>([])
const error = ref('')

async function load() {
  const m = await get(`${base}/menu`)
  cats.value = m.categories; stations.value = m.stations; platforms.value = m.platforms ?? []; loaded.value = true
}
onMounted(load)
const stationOf = (it: Item) => stations.value.find(s => s.id === it.station_id)

async function run(fn: () => Promise<unknown>) {
  error.value = ''
  try { await fn(); await load(); return true } catch (e) { error.value = (e as Error).message; await load().catch(() => {}); return false }
}

const quick = (it: Item, body: Record<string, unknown>) => run(() => patch(`${base}/items/${it.id}`, body))

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
  await run(() => post(`${base}/categories`, { name, name_en: en, sort: cats.value.length }))
}
async function renameCategory(c: Category) {
  const name = prompt(t('name_zh'), c.name)
  if (!name?.trim()) return
  const en = prompt(t('name_en'), c.name_en) ?? c.name_en
  await run(() => put(`${base}/categories/${c.id}`, { name, name_en: en, sort: c.sort }))
}

// A required category: every delivery or pickup order must contain something from it.
const setRequired = (c: Category) => run(() => put(`${base}/categories/${c.id}`,
  { name: c.name, name_en: c.name_en, sort: c.sort, required: !c.required }))

// ---- the menu in other languages (MenuTranslations.vue) ----
const langs = reactive<{ open: boolean; item: Item | null }>({ open: false, item: null })
const showLangs = (it: Item | null) => Object.assign(langs, { open: true, item: it })
const trFailed = ref(false)
async function checkTranslations() {
  trFailed.value = !!(await get<{ error: string }>(`${base}/translations`).catch(() => ({ error: '' }))).error
}
onMounted(() => { if (translator.value) void checkTranslations() })
function closeLangs() { langs.open = false; void checkTranslations() }

// ---- Excel import (MenuImportDialog.vue) ----
const importing = ref(false)

// ---- item editor (ItemForm.vue) ----
const form = reactive<{ open: boolean; item: Item | null; categoryId: number }>({ open: false, item: null, categoryId: 0 })
function edit(it: Item | null, categoryId = 0) { Object.assign(form, { open: true, item: it, categoryId }) }
async function saved() { form.open = false; await load() }

// ---- the dish list: rail, filters, paging ----
const allItems = computed(() => cats.value.flatMap(c => c.items))
const catSel = ref(0)
const q = ref('')
const status = ref<'' | 'on' | 'draft' | 'sold' | 'limited'>('')
const station = ref<string>('')
const VIEW_KEY = 'menu-view'
const view = ref<'list' | 'grid'>((() => { try { return localStorage.getItem(VIEW_KEY) === 'grid' ? 'grid' : 'list' } catch { return 'list' } })())
watch(view, v => { try { localStorage.setItem(VIEW_KEY, v) } catch { /* private mode */ } })
const PAGE = 20
const pageNo = ref(1)
const filtering = computed(() => !!(q.value.trim() || status.value || station.value))
watch([catSel, q, status, station, view], () => { pageNo.value = 1; picked.value = new Set() })

function matches(it: Item) {
  const s = q.value.trim().toLowerCase()
  if (s && ![it.name, it.name_en, it.code ?? ''].some(x => x.toLowerCase().includes(s))) return false
  if (status.value === 'on' && !it.published) return false
  if (status.value === 'draft' && it.published) return false
  if (status.value === 'sold' && !it.sold_out) return false
  if (status.value === 'limited' && it.stock_qty === null) return false
  if (station.value === 'none' && it.needs_prep) return false
  if (station.value === 'unassigned' && !(it.needs_prep && !it.station_id)) return false
  if (/^\d+$/.test(station.value) && !(it.needs_prep && it.station_id === Number(station.value))) return false
  return true
}
const shownCats = computed(() => cats.value.filter(c => !catSel.value || c.id === catSel.value)
  .map(c => ({ c, items: c.items.filter(matches) })).filter(x => x.items.length || (!filtering.value && catSel.value)))
const shownCount = computed(() => shownCats.value.reduce((n, x) => n + x.items.length, 0))
const pages = computed(() => Math.max(1, Math.ceil(shownCount.value / PAGE)))
watch(pages, n => { if (pageNo.value > n) pageNo.value = n })
// this page's slice of the dishes, still under their category headings
const pageCats = computed(() => {
  const from = (pageNo.value - 1) * PAGE, to = from + PAGE
  let i = 0
  const out: { c: Category; items: Item[]; total: number }[] = []
  for (const x of shownCats.value) {
    const items = x.items.filter(() => { const k = i++; return k >= from && k < to })
    if (items.length || (!x.items.length && i >= from && i <= to)) out.push({ c: x.c, items, total: x.items.length })
  }
  return out
})
const pageList = computed(() => {
  const n = pages.value, p = pageNo.value
  const set = new Set([1, n, p - 1, p, p + 1].filter(x => x >= 1 && x <= n))
  const out: (number | 0)[] = []
  let last = 0
  for (const x of [...set].sort((a, b) => a - b)) { if (x - last > 1) out.push(0); out.push(x); last = x }
  return out
})
function clearFilters() { q.value = ''; status.value = ''; station.value = '' }

// ---- picking several dishes ----
const picked = ref<Set<number>>(new Set())
const pageIds = computed(() => pageCats.value.flatMap(x => x.items.map(it => it.id)))
const allPicked = computed(() => pageIds.value.length > 0 && pageIds.value.every(id => picked.value.has(id)))
function togglePick(id: number) { const s = new Set(picked.value); s.has(id) ? s.delete(id) : s.add(id); picked.value = s }
function togglePage() { picked.value = allPicked.value ? new Set() : new Set(pageIds.value) }
async function bulk(body: Record<string, unknown>) {
  const ids = [...picked.value]
  if (body.archived && !confirm(t('mx.bulk_archive_confirm', { n: ids.length }))) return
  const ok = await run(async () => { for (const id of ids) await patch(`${base}/items/${id}`, body) })
  if (ok) picked.value = new Set()
}

// ---- the little "more" menu of a dish or a category ----
const menu = reactive<{ kind: '' | 'item' | 'cat'; id: number; x: number; y: number }>({ kind: '', id: 0, x: 0, y: 0 })
function openMenu(kind: 'item' | 'cat', id: number, e: MouseEvent) {
  if (menu.kind === kind && menu.id === id) { menu.kind = ''; return }
  const r = (e.currentTarget as HTMLElement).getBoundingClientRect()
  const w = 180
  Object.assign(menu, { kind, id, x: Math.max(8, Math.min(r.right - w, window.innerWidth - w - 8)), y: r.bottom + 4 })
}
const closeMenu = () => { menu.kind = '' }
onMounted(() => { window.addEventListener('scroll', closeMenu, true); window.addEventListener('resize', closeMenu) })
onBeforeUnmount(() => { window.removeEventListener('scroll', closeMenu, true); window.removeEventListener('resize', closeMenu) })
const menuItem = computed(() => menu.kind === 'item' ? allItems.value.find(i => i.id === menu.id) ?? null : null)
const menuCat = computed(() => menu.kind === 'cat' ? cats.value.find(c => c.id === menu.id) ?? null : null)
function act(fn: () => unknown) { const r = fn(); closeMenu(); void r }

// ---- reordering: drag on a PC, "move up / down" in the menu anywhere ----
async function saveOrder() {
  await run(() => put(`${base}/menu-order`, { categories: cats.value.map(c => ({ id: c.id, item_ids: c.items.map(i => i.id) })) }))
}
const canDrag = computed(() => manager.value && !filtering.value)
const drag = ref<{ kind: 'item' | 'cat'; id: number } | null>(null)
const over = ref('')
function moveItem(id: number, toCat: number, beforeId: number | null) {
  const from = cats.value.find(c => c.items.some(i => i.id === id))
  const to = cats.value.find(c => c.id === toCat)
  if (!from || !to || id === beforeId) return false
  const [it] = from.items.splice(from.items.findIndex(i => i.id === id), 1)
  const at = beforeId === null ? -1 : to.items.findIndex(i => i.id === beforeId)
  to.items.splice(at < 0 ? to.items.length : at, 0, { ...it, category_id: to.id })
  return true
}
function moveCat(id: number, beforeId: number | null) {
  if (id === beforeId) return false
  const i = cats.value.findIndex(c => c.id === id)
  const [c] = cats.value.splice(i, 1)
  const at = beforeId === null ? -1 : cats.value.findIndex(x => x.id === beforeId)
  cats.value.splice(at < 0 ? cats.value.length : at, 0, c)
  return true
}
function dropOnItem(target: Item) {
  const d = drag.value
  drag.value = null; over.value = ''
  if (d?.kind === 'item' && moveItem(d.id, target.category_id, target.id)) void saveOrder()
}
function dropOnCat(c: Category) {
  const d = drag.value
  drag.value = null; over.value = ''
  if (d?.kind === 'item' && moveItem(d.id, c.id, null)) void saveOrder()
  else if (d?.kind === 'cat' && moveCat(d.id, c.id)) void saveOrder()
}
function step(it: Item, d: number) {
  const c = cats.value.find(x => x.id === it.category_id)
  if (!c) return
  const i = c.items.findIndex(x => x.id === it.id), j = i + d
  if (j < 0 || j >= c.items.length) return
  c.items.splice(j, 0, c.items.splice(i, 1)[0])
  void saveOrder()
}
function stepCat(c: Category, d: number) {
  const i = cats.value.findIndex(x => x.id === c.id), j = i + d
  if (j < 0 || j >= cats.value.length) return
  cats.value.splice(j, 0, cats.value.splice(i, 1)[0])
  void saveOrder()
}

const stockText = (it: Item) => it.stock_qty === null ? t('mx.unlimited') : it.stock_qty > 0 ? t('mx.left', { n: it.stock_qty }) : t('mx.none_left')
</script>

<template>
  <div class="mx warm" @click="closeMenu">
    <div class="page mx-page">
      <div class="mx-head">
        <div><h1>{{ t('mx.title') }}</h1><p class="mx-sub">{{ t('mx.sub') }}</p></div>
        <div class="mx-acts" v-if="manager || translator">
          <button v-if="translator" class="btn" :title="t('mtr.open_hint')" data-testid="open-langs" @click="showLangs(null)">{{ t('mtr.open') }}</button>
          <template v-if="manager">
            <button class="btn" data-testid="open-import" @click="importing = true">{{ t('import_excel') }}</button>
            <button class="btn" @click="newCategory">{{ t('new_category') }}</button>
            <button class="btn primary" :disabled="cats.length === 0" @click="edit(null)">＋ {{ t('new_item') }}</button>
          </template>
        </div>
      </div>
      <div class="mx-tabs" data-testid="menu-tabs">
        <button :class="{ on: tab === 'items' }" data-testid="tab-items" @click="setTab('items')">{{ t('mx.tab_items') }} <span class="n">{{ allItems.length }}</span></button>
        <button :class="{ on: tab === 'notes' }" data-testid="tab-notes" @click="setTab('notes')">{{ t('nt.tab_templates') }}</button>
        <button :class="{ on: tab === 'toppings' }" data-testid="tab-toppings" @click="setTab('toppings')">{{ t('tp.tab') }}</button>
      </div>
      <div v-if="error" class="banner bad">{{ error }}</div>
      <div v-if="trFailed && translator" class="banner bad row between" data-testid="tr-failed-banner">{{ t('mtr.failed', { at: '' }) }}
        <button class="btn small" @click="showLangs(null)">{{ t('mtr.open') }}</button></div>

      <NoteTemplates v-if="tab === 'notes'" :base="base" :cats="cats" :can-edit="manager" @changed="load(); loadTpls()" />
      <ToppingTemplates v-else-if="tab === 'toppings'" :base="base" :cats="cats" :can-edit="manager" :cur="cur" @changed="load(); loadToppings()" />

      <div v-else class="mx-body" data-testid="menu-items">
        <!-- the category rail -->
        <aside class="mx-rail" data-testid="cats-rail">
          <div class="mx-rail-h"><b>{{ t('mx.cats') }}</b>
            <button v-if="manager" class="mx-ico" :title="t('mx.new_cat')" :aria-label="t('mx.new_cat')" @click="newCategory"><MxIcon name="plus" :size="20" /></button></div>
          <div class="mx-rail-list">
            <button class="mx-cat" :class="{ on: catSel === 0 }" data-testid="cat-all" @click="catSel = 0">
              <span class="fi"><MxIcon name="folder" /></span><span class="grow">{{ t('mx.all') }}</span><span class="n">{{ allItems.length }}</span></button>
            <div v-for="c in cats" :key="c.id" class="mx-cat" :class="{ on: catSel === c.id, over: over === 'c' + c.id }" role="button" tabindex="0"
              :data-testid="'cat-' + c.name" :draggable="canDrag" @click="catSel = c.id" @keydown.enter="catSel = c.id"
              @dragstart="drag = { kind: 'cat', id: c.id }" @dragend="drag = null; over = ''"
              @dragover.prevent="over = 'c' + c.id" @dragleave="over = ''" @drop.prevent="dropOnCat(c)">
              <span class="fi"><MxIcon name="folder" /></span><span class="grow nm">{{ mn(c) }}</span>
              <span v-if="c.required" class="req" :title="t('cat_required_hint')">{{ t('cat_required') }}</span>
              <span class="n">{{ c.items.length }}</span>
              <button v-if="manager" class="mx-dots" :aria-label="t('mx.more')" :data-testid="'cat-more-' + c.name" @click.stop="openMenu('cat', c.id, $event)"><MxIcon name="dots" /></button>
            </div>
          </div>
          <template v-if="manager">
            <button class="mx-rail-add" @click="newCategory"><MxIcon name="plus" /> {{ t('mx.new_cat') }}</button>
            <div class="mx-rail-tip"><MxIcon name="sliders" :size="16" /> {{ t('mx.drag_hint') }}</div>
          </template>
        </aside>

        <section class="mx-main">
          <div class="mx-tools">
            <label class="mx-search"><MxIcon name="search" class="si" /><input v-model="q" :placeholder="t('mx.search')" data-testid="menu-search" /></label>
            <select v-model="status" data-testid="menu-status">
              <option value="">{{ t('mx.st_all') }}</option><option value="on">{{ t('mx.st_on') }}</option><option value="draft">{{ t('mx.st_draft') }}</option>
              <option value="sold">{{ t('mx.st_sold') }}</option><option value="limited">{{ t('mx.st_limited') }}</option>
            </select>
            <select v-model="station" data-testid="menu-station">
              <option value="">{{ t('mx.stn_all') }}</option>
              <option v-for="s in stations" :key="s.id" :value="String(s.id)">{{ mn(s) }}</option>
              <option value="none">{{ t('mx.stn_none') }}</option>
              <option v-if="stations.length" value="unassigned">{{ t('mx.stn_unassigned') }}</option>
            </select>
            <span class="grow"></span>
            <span class="mx-views">
              <button :class="{ on: view === 'list' }" :title="t('mx.view_list')" :aria-label="t('mx.view_list')" data-testid="view-list" @click="view = 'list'"><MxIcon name="list" :size="20" /></button>
              <button :class="{ on: view === 'grid' }" :title="t('mx.view_grid')" :aria-label="t('mx.view_grid')" data-testid="view-grid" @click="view = 'grid'"><MxIcon name="grid" :size="20" /></button>
            </span>
          </div>

          <div v-if="picked.size" class="mx-bulk" data-testid="menu-bulk">
            <b>{{ t('mx.picked', { n: picked.size }) }}</b>
            <template v-if="manager">
              <button class="btn small" @click="bulk({ published: true })">{{ t('mx.bulk_publish') }}</button>
              <button class="btn small" @click="bulk({ published: false })">{{ t('mx.bulk_draft') }}</button>
            </template>
            <template v-if="soldOut">
              <button class="btn small" data-act="sold" @click="bulk({ sold_out: true })">{{ t('mx.bulk_sold') }}</button>
              <button class="btn small" data-act="restore" @click="bulk({ sold_out: false })">{{ t('mx.bulk_restore') }}</button>
            </template>
            <button v-if="manager" class="btn small danger" @click="bulk({ archived: true })">{{ t('mx.bulk_archive') }}</button>
            <span class="grow"></span>
            <button class="btn link small" @click="picked = new Set()">{{ t('cancel') }}</button>
          </div>

          <!-- list -->
          <table v-if="view === 'list'" class="data mx-table">
            <thead><tr>
              <th class="c-h"></th>
              <th class="c-ck"><input type="checkbox" :checked="allPicked" :aria-label="t('mx.all')" @change="togglePage" /></th>
              <th class="c-info" colspan="2">{{ t('mx.col_info') }}</th><th class="c-price">{{ t('mx.col_price') }}</th>
              <th class="c-stock">{{ t('mx.col_stock') }}</th><th class="c-stn">{{ t('mx.col_station') }}</th>
              <th class="c-st">{{ t('mx.col_status') }}</th><th class="c-ops">{{ t('mx.col_ops') }}</th></tr></thead>
            <tbody v-for="x in pageCats" :key="x.c.id" class="mx-grp" :data-cat="x.c.name">
              <tr class="mx-gh" :class="{ over: over === 'g' + x.c.id }" @dragover.prevent="over = 'g' + x.c.id" @dragleave="over = ''" @drop.prevent="dropOnCat(x.c)">
                <td colspan="9"><div class="mx-gh-in">
                  <h2>{{ both(x.c.name, x.c.name_en) }} <span class="cnt">({{ x.total }})</span>
                    <span v-if="x.c.required" class="tag brand" :title="t('cat_required_hint')" data-testid="cat-required">{{ t('cat_required') }}</span></h2>
                  <span class="grow"></span>
                  <template v-if="manager">
                    <button class="btn small" :title="t('cat_required_hint')" data-testid="toggle-required" @click="setRequired(x.c)">{{ x.c.required ? t('cat_required_off') : t('cat_required_on') }}</button>
                    <button class="btn small" @click="edit(null, x.c.id)">＋ {{ t('new_item') }}</button>
                  </template>
                </div></td></tr>
              <tr v-for="it in x.items" :key="it.id" class="mx-row" :class="{ off: !it.published || it.sold_out, over: over === 'i' + it.id, picked: picked.has(it.id) }"
                :data-testid="'item-row-' + it.id" @dragover.prevent="drag?.kind === 'item' && (over = 'i' + it.id)" @dragleave="over = ''" @drop.prevent="dropOnItem(it)">
                <td class="c-h"><span v-if="manager" class="mx-handle" :class="{ dis: !canDrag }" :draggable="canDrag" :title="canDrag ? t('mx.drag') : t('mx.drag_off')"
                  @dragstart="drag = { kind: 'item', id: it.id }" @dragend="drag = null; over = ''"><MxIcon name="grip" /></span></td>
                <td class="c-ck"><input type="checkbox" :checked="picked.has(it.id)" :aria-label="it.name" @change="togglePick(it.id)" /></td>
                <td class="c-img"><img v-if="it.image_url" :src="it.image_url" alt="" loading="lazy" /><span v-else class="mx-noimg">{{ t('mx.no_pic') }}</span></td>
                <td class="c-info">
                  <div class="mx-nm"><span v-if="it.code" class="code" data-testid="item-code-tag">{{ it.code }}</span><b>{{ it.name }}</b></div>
                  <div class="mx-en">{{ it.code ? it.code + ' / ' : '' }}{{ it.name_en }}</div>
                  <div v-if="it.note_template_ids?.length || it.topping_template_ids?.length" class="mx-tpls">
                    <span v-if="it.note_template_ids?.length" class="tag warn" data-testid="item-notes-tag">{{ t('nt.on', { names: it.note_template_ids.map(tplName).join('、') }) }}</span>
                    <span v-if="it.topping_template_ids?.length" class="tag info" data-testid="item-toppings-tag">{{ t('tp.on', { names: it.topping_template_ids.map(toppingName).join('、') }) }}</span>
                  </div>
                </td>
                <td class="c-price price">{{ money(it.price, cur) }}
                  <div v-for="p in platforms" :key="p.id" class="pfline" :data-platform="p.prefix" :class="{ own: it.platform_prices?.[p.id] != null }">
                    <i :style="{ background: p.color }"></i>{{ p.prefix }} {{ money(it.platform_prices?.[p.id] ?? it.platform_auto?.[p.id] ?? it.price, cur) }}</div></td>
                <td class="c-stock"><span :class="{ lim: it.stock_qty !== null && it.stock_qty > 0, none: it.stock_qty === 0 }">{{ stockText(it) }}</span></td>
                <td class="c-stn"><span v-if="!it.needs_prep" class="muted">{{ t('no_prep') }}</span><template v-else-if="stationOf(it)">{{ mn(stationOf(it)!) }}</template>
                  <span v-else-if="stations.length" class="tag warn">{{ t('unassigned') }}</span><span v-else class="muted">—</span></td>
                <td class="c-st"><span class="pill" :class="it.sold_out ? 'sold' : it.published ? 'on' : 'draft'">{{ it.sold_out ? t('sold_out') : it.published ? t('published') : t('draft') }}</span>
                  <span v-if="it.sold_out && !it.published" class="pill draft">{{ t('draft') }}</span></td>
                <td class="c-ops"><div class="mx-ops">
                  <button v-if="manager" class="op red" @click="edit(it)">{{ t('edit') }}</button>
                  <button v-if="soldOut" class="op" @click="quick(it, { sold_out: !it.sold_out })">{{ it.sold_out ? t('restore') : t('mx.sold_out') }}</button>
                  <button v-if="translator && !manager" class="op" :data-testid="'item-langs-' + it.id" @click="showLangs(it)">{{ t('mtr.open') }}</button>
                  <button v-if="manager || soldOut" class="op dots" :aria-label="t('mx.more')" :data-testid="'item-more-' + it.id" @click.stop="openMenu('item', it.id, $event)"><MxIcon name="dots" /></button>
                </div></td>
              </tr>
              <tr v-if="!x.items.length"><td colspan="9" class="muted mx-emptyrow">{{ t('mx.empty_cat') }}</td></tr>
            </tbody>
          </table>

          <!-- grid -->
          <div v-else class="mx-gridwrap">
            <div v-if="manager" class="mx-gridall"><label class="check"><input type="checkbox" :checked="allPicked" @change="togglePage" /> {{ t('mx.all') }}</label></div>
            <template v-for="x in pageCats" :key="x.c.id">
              <div class="mx-gh-in mx-ggh"><h2>{{ both(x.c.name, x.c.name_en) }} <span class="cnt">({{ x.total }})</span>
                  <span v-if="x.c.required" class="tag brand">{{ t('cat_required') }}</span></h2><span class="grow"></span>
                <button v-if="manager" class="btn small" @click="edit(null, x.c.id)">＋ {{ t('new_item') }}</button></div>
              <div class="mx-grid">
                <div v-for="it in x.items" :key="it.id" class="mx-card" :class="{ off: !it.published || it.sold_out, picked: picked.has(it.id) }" :data-testid="'item-card-' + it.id">
                  <div class="pic"><img v-if="it.image_url" :src="it.image_url" alt="" loading="lazy" /><span v-else class="mx-noimg">{{ t('mx.no_pic') }}</span>
                    <input type="checkbox" class="ck" :checked="picked.has(it.id)" :aria-label="it.name" @change="togglePick(it.id)" />
                    <span class="pill" :class="it.sold_out ? 'sold' : it.published ? 'on' : 'draft'">{{ it.sold_out ? t('sold_out') : it.published ? t('published') : t('draft') }}</span></div>
                  <div class="bd">
                    <div class="mx-nm"><span v-if="it.code" class="code">{{ it.code }}</span><b>{{ it.name }}</b></div>
                    <div class="mx-en">{{ it.name_en }}</div>
                    <div class="row between"><span class="price">{{ money(it.price, cur) }}</span>
                      <small :class="{ lim: it.stock_qty !== null && it.stock_qty > 0, none: it.stock_qty === 0 }">{{ stockText(it) }}</small></div>
                    <div class="mx-ops">
                      <button v-if="manager" class="op red" @click="edit(it)">{{ t('edit') }}</button>
                      <button v-if="soldOut" class="op" @click="quick(it, { sold_out: !it.sold_out })">{{ it.sold_out ? t('restore') : t('mx.sold_out') }}</button>
                      <button v-if="translator && !manager" class="op" @click="showLangs(it)">{{ t('mtr.open') }}</button>
                      <button v-if="manager || soldOut" class="op dots" :aria-label="t('mx.more')" @click.stop="openMenu('item', it.id, $event)"><MxIcon name="dots" /></button>
                    </div>
                  </div>
                </div>
              </div>
            </template>
          </div>

          <div v-if="loaded && !shownCount && (filtering || !catSel)" class="mx-none">{{ t('mx.empty') }}
            <button v-if="filtering" class="btn small" @click="clearFilters">{{ t('mx.clear') }}</button></div>

          <div class="mx-foot">
            <span class="muted">{{ t('mx.total', { n: shownCount }) }}</span>
            <span class="grow"></span>
            <nav v-if="pages > 1" class="mx-pager" data-testid="menu-pager">
              <button :disabled="pageNo === 1" aria-label="‹" @click="pageNo--"><span class="rtl-flip">‹</span></button>
              <template v-for="(p, i) in pageList" :key="i">
                <span v-if="p === 0" class="gap">…</span>
                <button v-else :class="{ on: p === pageNo }" @click="pageNo = p">{{ p }}</button>
              </template>
              <button :disabled="pageNo === pages" aria-label="›" @click="pageNo++"><span class="rtl-flip">›</span></button>
            </nav>
          </div>
        </section>
      </div>
    </div>

    <!-- the "more" menu -->
    <div v-if="menuItem" class="mx-pop" :style="{ left: menu.x + 'px', top: menu.y + 'px' }" data-testid="item-menu" @click.stop>
      <button v-if="soldOut" @click="act(() => setStock(menuItem!))">{{ t('mx.stock_set') }}</button>
      <template v-if="manager">
        <button @click="act(() => quick(menuItem!, { published: !menuItem!.published }))">{{ menuItem.published ? t('unpublish') : t('publish') }}</button>
        <button :data-testid="'item-langs-' + menuItem.id" @click="act(() => showLangs(menuItem))">{{ t('mtr.open') }}</button>
        <button :disabled="filtering" data-act="up" @click="act(() => step(menuItem!, -1))">{{ t('mx.move_up') }}</button>
        <button :disabled="filtering" data-act="down" @click="act(() => step(menuItem!, 1))">{{ t('mx.move_down') }}</button>
        <button class="bad" @click="act(() => archive(menuItem!))">{{ t('archive') }}</button>
      </template>
    </div>
    <div v-if="menuCat" class="mx-pop" :style="{ left: menu.x + 'px', top: menu.y + 'px' }" data-testid="cat-menu" @click.stop>
      <button @click="act(() => renameCategory(menuCat!))">{{ t('mx.rename') }}</button>
      <button @click="act(() => setRequired(menuCat!))">{{ menuCat.required ? t('cat_required_off') : t('cat_required_on') }}</button>
      <button @click="act(() => edit(null, menuCat!.id))">＋ {{ t('new_item') }}</button>
      <button @click="act(() => stepCat(menuCat!, -1))">{{ t('mx.move_up') }}</button>
      <button @click="act(() => stepCat(menuCat!, 1))">{{ t('mx.move_down') }}</button>
    </div>

    <MenuImportDialog v-if="importing" :base="base" :cur="cur" @imported="load" @close="importing = false" />
    <MenuTranslations v-if="langs.open" :base="base" :cats="cats" :item="langs.item" @close="closeLangs" />
    <ItemForm v-if="form.open" :item="form.item" :category-id="form.categoryId" :cats="cats" :stations="stations" :cur="cur"
      :platforms="platforms" :base="base" @saved="saved" @close="form.open = false" @manage-notes="form.open = false; setTab('notes')" @manage-toppings="form.open = false; setTab('toppings')" />
  </div>
</template>

<style scoped>
.mx { --pink: #fbeae3; --pink-line: #efb9a8; --card: #fffdf9; --line: #eadfce; --bg: #f7f1e7;
  min-height: 100%; background: var(--bg); }
.mx-page { display: grid; gap: 14px; grid-template-columns: minmax(0, 1fr); }
.mx-body > *, .mx-page > * { min-width: 0; }
.mx-head { display: flex; flex-wrap: wrap; gap: 12px; align-items: flex-start; justify-content: space-between; }
.mx-head h1 { font-size: 26px; letter-spacing: .5px; color: #2f1810; }
.mx-sub { color: var(--muted); font-size: 13.5px; margin-top: 2px; }
.mx-acts { display: flex; flex-wrap: wrap; gap: 10px; }
.mx-acts .btn { background: var(--card); border-color: var(--line); }
.mx-acts .btn.primary { background: var(--brand); border-color: var(--brand); color: #fff; }
.mx-tabs { display: flex; gap: 8px; border-bottom: 1px solid var(--line); overflow-x: auto; }
.mx-tabs button { border: 0; background: none; padding: 10px 18px; font: inherit; font-size: 16px; font-weight: 600; color: #5b4636; border-bottom: 3px solid transparent;
  margin-bottom: -1px; cursor: pointer; white-space: nowrap; }
.mx-tabs button.on { color: var(--brand); border-bottom-color: var(--brand); }
.mx-tabs .n { font-variant-numeric: tabular-nums; }

.mx-body { display: grid; grid-template-columns: 230px minmax(0, 1fr); gap: 14px; align-items: start; }
.mx-rail { background: var(--card); border: 1px solid var(--line); border-radius: 12px; padding: 12px 0; position: sticky; top: 12px; }
.mx-rail-h { display: flex; align-items: center; justify-content: space-between; padding: 0 14px 10px; font-size: 16px; }
.mx-ico { border: 0; background: none; font-size: 20px; color: #5b4636; cursor: pointer; line-height: 1; padding: 2px 4px; }
.mx-rail-list { display: grid; }
.mx-cat { display: flex; align-items: center; gap: 8px; width: 100%; border: 0; background: none; font: inherit; color: var(--ink); text-align: start;
  padding: 10px 14px; cursor: pointer; border-inline-start: 3px solid transparent; }
.mx-cat:hover { background: #fbf4ea; }
.mx-cat.on { background: var(--pink); border-inline-start-color: var(--brand); color: var(--brand); font-weight: 700; }
.mx-cat.over { outline: 2px dashed var(--brand); outline-offset: -3px; }
.mx-cat .fi { color: #8a7464; display: inline-flex; } .mx-cat.on .fi { color: var(--brand); }
.mx-cat .nm { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.mx-cat .n { color: var(--muted); font-variant-numeric: tabular-nums; font-weight: 500; } .mx-cat.on .n { color: var(--brand); }
.mx-cat .req { font-size: 11px; color: var(--brand); border: 1px solid var(--pink-line); border-radius: 4px; padding: 0 4px; }
.mx-dots { border: 0; background: none; color: #8a7464; cursor: pointer; padding: 0 2px; line-height: 0; opacity: 0; }
.mx-cat:hover .mx-dots, .mx-cat.on .mx-dots, .mx-dots:focus-visible { opacity: 1; }
@media (hover: none) { .mx-dots { opacity: 1; } }
.mx-rail-add { display: flex; align-items: center; gap: 6px; width: calc(100% - 28px); margin: 8px 14px 0; padding: 12px 0 10px; border: 0; border-top: 1px solid var(--line); background: none;
  color: var(--brand); font: inherit; font-weight: 700; text-align: start; cursor: pointer; }
.mx-rail-tip { margin: 0 14px; padding-top: 10px; border-top: 1px solid var(--line); color: var(--muted); font-size: 12.5px; }

.mx-main { background: var(--card); border: 1px solid var(--line); border-radius: 12px; padding: 12px; min-width: 0; }
.mx-tools { display: flex; flex-wrap: wrap; gap: 10px; align-items: center; margin-bottom: 10px; }
.mx-tools select { width: 180px; background: #fff; }
.mx-search { position: relative; flex: 1 1 260px; max-width: 440px; }
.mx-search .si { position: absolute; inset-inline-start: 11px; top: 50%; transform: translateY(-50%); color: #8a7464; font-size: 17px; pointer-events: none; }
.mx-search input { padding-inline-start: 34px; background: #fff; }
.mx-views { display: inline-flex; border: 1px solid var(--line); border-radius: 8px; overflow: hidden; background: #fff; }
.mx-views button { border: 0; background: none; width: 42px; height: 38px; cursor: pointer; color: #5b4636; display: inline-grid; place-items: center; }
.mx-views button.on { background: var(--pink); color: var(--brand); }
.mx-bulk { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; background: var(--pink); border: 1px solid var(--pink-line); border-radius: 10px; padding: 8px 12px; margin-bottom: 10px; }
.mx-bulk b { color: var(--brand); }

.mx-table { width: 100%; border-collapse: separate; border-spacing: 0; }
.mx-table thead th { background: #f4ebde; color: #3b2418; font-weight: 600; font-size: 14px; padding: 11px 10px; text-align: start; border: 0; }
.mx-table thead th:first-child { border-start-start-radius: 8px; border-end-start-radius: 8px; }
.mx-table thead th:last-child { border-start-end-radius: 8px; border-end-end-radius: 8px; }
.mx-table td { padding: 10px; border-bottom: 1px solid #f0e6d8; vertical-align: middle; background: transparent; }
.mx-gh td { padding: 14px 10px 10px; border-bottom: 1px solid var(--line); }
.mx-gh.over td { background: var(--pink); }
.mx-gh-in { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; }
.mx-gh-in h2 { font-size: 18px; color: #2f1810; display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
.mx-gh-in .cnt { font-weight: 500; color: #5b4636; }
.mx-gh-in .btn { background: #fff; border-color: var(--line); }
.mx-row:hover td { background: #fdf8f1; }
.mx-row.over td { box-shadow: inset 0 2px 0 var(--brand); }
.mx-row.picked td { background: #fdf1ec; }
.mx-row.off .c-img img { filter: grayscale(.6); opacity: .7; }
.c-h { width: 22px; padding-inline: 4px 0 !important; } .c-ck { width: 34px; } .c-img { width: 84px; }
.c-price { width: 100px; } .c-stock { width: 110px; } .c-stn { width: 110px; } .c-st { width: 110px; } .c-ops { width: 180px; }
.pfline { font-size: 11.5px; color: var(--muted); font-weight: 400; white-space: nowrap; } .pfline i { display: inline-block; width: 7px; height: 7px; border-radius: 50%; margin-inline-end: 4px; } .pfline.own { color: var(--ink, #222); }
.mx-handle { cursor: grab; color: #a8927f; user-select: none; display: inline-flex; }
.mx-handle.dis { cursor: not-allowed; opacity: .35; }
.c-img img, .mx-noimg { width: 76px; height: 56px; border-radius: 8px; object-fit: cover; display: block; }
.mx-noimg { background: #f3e9da; color: #a8927f; font-size: 12px; display: grid; place-items: center; }
.mx-nm { display: flex; align-items: baseline; gap: 8px; flex-wrap: wrap; font-size: 16px; }
.mx-nm .code { color: var(--brand); font-weight: 700; }
.mx-nm b { color: #2f1810; overflow-wrap: anywhere; }
.mx-en { color: #6f5a4b; font-size: 13.5px; margin-top: 2px; overflow-wrap: anywhere; }
.mx-tpls { display: flex; flex-wrap: wrap; gap: 4px; margin-top: 4px; }
.price { font-weight: 700; font-size: 17px; color: #2f1810; font-variant-numeric: tabular-nums; white-space: nowrap; }
.lim { color: #c56a12; font-weight: 700; } .none { color: var(--bad); font-weight: 700; }
.pill { display: inline-flex; align-items: center; gap: 5px; border-radius: 999px; padding: 3px 11px; font-size: 13px; font-weight: 700; white-space: nowrap; margin: 1px 4px 1px 0; }
.pill::before { content: ''; width: 6px; height: 6px; border-radius: 50%; background: currentColor; }
.pill.on { background: #e6f2e2; color: #2f7a2a; } .pill.draft { background: #efe9e1; color: #7b6657; } .pill.sold { background: #fbe3df; color: #b42318; }
.mx-ops { display: flex; align-items: center; justify-content: flex-end; }
.mx-ops .op { border: 0; background: none; font: inherit; font-weight: 600; color: #6f5a4b; padding: 4px 10px; cursor: pointer; white-space: nowrap; }
.mx-ops .op + .op { border-inline-start: 1px solid var(--line); }
.mx-ops .op.red { color: var(--brand); }
.mx-ops .op.dots { line-height: 0; color: #5b4636; }
.mx-emptyrow { text-align: center; padding: 18px !important; }

.mx-gridwrap { display: grid; gap: 10px; }
.mx-gridall { color: var(--muted); }
.mx-ggh { padding: 10px 2px 2px; border-bottom: 1px solid var(--line); }
.mx-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); gap: 12px; padding-bottom: 8px; }
.mx-card { border: 1px solid var(--line); border-radius: 12px; overflow: hidden; background: #fff; display: flex; flex-direction: column; }
.mx-card.picked { border-color: var(--brand); box-shadow: 0 0 0 1px var(--brand); }
.mx-card .pic { position: relative; aspect-ratio: 4 / 3; background: #f3e9da; }
.mx-card .pic img, .mx-card .pic .mx-noimg { width: 100%; height: 100%; border-radius: 0; }
.mx-card.off .pic img { filter: grayscale(.6); opacity: .75; }
.mx-card .ck { position: absolute; top: 8px; inset-inline-start: 8px; background: #fff; }
.mx-card .pic .pill { position: absolute; top: 8px; inset-inline-end: 4px; }
.mx-card .bd { padding: 10px 12px; display: grid; gap: 6px; flex: 1; align-content: start; }
.mx-card .mx-ops { justify-content: flex-start; margin-inline-start: -10px; }

.mx-none { text-align: center; padding: 28px; color: var(--muted); display: grid; gap: 10px; justify-items: center; }
.mx-foot { display: flex; align-items: center; gap: 10px; padding: 14px 6px 4px; flex-wrap: wrap; }
.mx-pager { display: flex; gap: 8px; align-items: center; }
.mx-pager button { min-width: 38px; height: 36px; border: 1px solid var(--line); background: #fff; border-radius: 8px; font: inherit; font-weight: 600; cursor: pointer; color: #3b2418; }
.mx-pager button.on { background: var(--brand); border-color: var(--brand); color: #fff; }
.mx-pager button:disabled { opacity: .4; cursor: default; }
.mx-pager .gap { color: var(--muted); }

.mx-pop { position: fixed; z-index: 70; width: 180px; background: #fff; border: 1px solid var(--line); border-radius: 10px; box-shadow: 0 10px 28px rgba(60, 30, 10, .18); padding: 4px; display: grid; }
.mx-pop button { border: 0; background: none; font: inherit; text-align: start; padding: 9px 12px; border-radius: 7px; cursor: pointer; color: var(--ink); }
.mx-pop button:hover:not(:disabled) { background: #fbf4ea; }
.mx-pop button:disabled { opacity: .4; cursor: default; }
.mx-pop button.bad { color: var(--bad); }

/* the dish list follows the room it has (a container query): the station
   column goes first, then each dish turns into a card */
.mx-main { container-type: inline-size; }
@container (max-width: 980px) {
  .c-stn { display: none; }
  .c-ops { width: 150px; }
}
@container (max-width: 720px) {
  .mx-tools select { flex: 1 1 140px; width: auto; }
  .mx-search { flex-basis: 100%; max-width: none; }
  .mx-table thead { display: none; }
  .mx-table, .mx-table tbody { display: block; }
  .mx-gh, .mx-gh td { display: block; }
  .mx-row { display: grid; grid-template-columns: 26px 72px minmax(0, 1fr) auto; grid-template-areas: "ck img info price" "ck img stock st" "ops ops ops ops";
    column-gap: 10px; row-gap: 4px; padding: 10px 2px; border-bottom: 1px solid #f0e6d8; }
  .mx-row td { padding: 0; border: 0; width: auto; }
  .mx-row .c-h, .mx-row .c-stn { display: none; }
  .mx-row .c-ck { grid-area: ck; } .mx-row .c-img { grid-area: img; } .mx-row .c-info { grid-area: info; }
  .mx-row .c-price { grid-area: price; text-align: end; } .mx-row .c-stock { grid-area: stock; font-size: 13px; } .mx-row .c-st { grid-area: st; text-align: end; }
  .mx-row .c-ops { grid-area: ops; border-top: 1px dashed #efe3d2; padding-top: 4px; }
  .c-img img, .mx-noimg { width: 72px; height: 60px; }
  .mx-nm { font-size: 15px; } .price { font-size: 16px; }
  .mx-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px; }
  .mx-foot { justify-content: center; }
}
@media (max-width: 1180px) {
  .mx-body { grid-template-columns: 200px minmax(0, 1fr); }
}
/* phone and narrow iPad: the rail turns into a row of chips */
@media (max-width: 860px) {
  .mx-head h1 { font-size: 22px; }
  .mx-acts { width: 100%; }
  .mx-acts .btn { flex: 1 1 auto; }
  .mx-body { grid-template-columns: minmax(0, 1fr); }
  .mx-rail { position: static; padding: 8px; min-width: 0; }
  .mx-rail-h, .mx-rail-tip { display: none; }
  .mx-rail-list { display: flex; overflow-x: auto; gap: 6px; scrollbar-width: none; }
  .mx-cat { width: auto; flex: none; border: 1px solid var(--line); border-radius: 999px; padding: 6px 12px; white-space: nowrap; }
  .mx-cat.on { border-color: var(--brand); }
  .mx-rail-add { display: none; }
  .mx-main { padding: 10px; }
}
</style>
