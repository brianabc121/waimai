<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { get, patch, post, put } from '../api'
import { both, money, t } from '../i18n'
import { can, currentRole, session } from '../session'
import type { Category, Item, Station } from '../types'
import ItemForm from './ItemForm.vue'
import MenuImportDialog from './MenuImportDialog.vue'
import MenuTranslations from './MenuTranslations.vue'
import './translateStrings'
import '../notes'
import NoteTemplates from './NoteTemplates.vue'
import '../toppings'
import ToppingTemplates from './ToppingTemplates.vue'
import { useRoute, useRouter } from 'vue-router'

const bid = session.branchId!
const cur = currentRole()?.currency ?? 'PHP'
const manager = computed(() => can('menu.edit'))
const translator = computed(() => can('menu.translate'))
const soldOut = computed(() => can('menu.soldout', 'menu.edit', 'kitchen'))
const cats = ref<Category[]>([])
// the two tabs: the dishes, and the flavour note templates (docs/NOTE_TEMPLATE_DESIGN.md)
const route = useRoute(), router = useRouter()
// plus the topping (加料) templates
type Tab = 'items' | 'notes' | 'toppings'
const tab = computed<Tab>(() => route.query.tab === 'notes' ? 'notes' : route.query.tab === 'toppings' ? 'toppings' : 'items')
const setTab = (x: Tab) => router.replace({ query: { ...route.query, tab: x === 'items' ? undefined : x } })
const toppingName = (id: number) => toppings.value.find(x => x.id === id)?.name ?? ''
const toppings = ref<{ id: number; name: string }[]>([])
async function loadToppings() { toppings.value = (await get<{ templates: { id: number; name: string }[] }>(`/merchant/branches/${bid}/topping-templates`).catch(() => ({ templates: [] }))).templates }
onMounted(loadToppings)
const tplName = (id: number) => tpls.value.find(x => x.id === id)?.name ?? ''
const tpls = ref<{ id: number; name: string }[]>([])
async function loadTpls() { tpls.value = (await get<{ templates: { id: number; name: string }[] }>(`/merchant/branches/${bid}/note-templates`).catch(() => ({ templates: [] }))).templates }
onMounted(loadTpls)
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

// ---- the menu in other languages (MenuTranslations.vue) ----
const langs = reactive<{ open: boolean; item: Item | null }>({ open: false, item: null })
const showLangs = (it: Item | null) => Object.assign(langs, { open: true, item: it })
// a failed translation run shows on the page until a later one works
const trFailed = ref(false)
async function checkTranslations() {
  trFailed.value = !!(await get<{ error: string }>(`/merchant/branches/${bid}/translations`).catch(() => ({ error: '' }))).error
}
onMounted(() => { if (translator.value) void checkTranslations() })
function closeLangs() { langs.open = false; void checkTranslations() }

// ---- Excel import (MenuImportDialog.vue) ----
const importing = ref(false)

// ---- item editor (ItemForm.vue) ----
const form = reactive<{ open: boolean; item: Item | null; categoryId: number }>({ open: false, item: null, categoryId: 0 })
function edit(it: Item | null, categoryId = 0) { Object.assign(form, { open: true, item: it, categoryId }) }
async function saved() { form.open = false; await load() }
</script>

<template>
  <div class="page stack">
    <div class="row between wrap">
      <h1>{{ t('menu_admin') }}</h1>
      <div class="row wrap" v-if="manager || translator">
        <button v-if="translator" class="btn" :title="t('mtr.open_hint')" data-testid="open-langs" @click="showLangs(null)">{{ t('mtr.open') }}</button>
        <template v-if="manager">
        <button class="btn" data-testid="open-import" @click="importing = true">{{ t('import_excel') }}</button>
        <button class="btn" @click="newCategory">{{ t('new_category') }}</button>
        <button class="btn primary" :disabled="cats.length === 0" @click="edit(null)">{{ t('new_item') }}</button>
        </template>
      </div>
    </div>
    <div class="subtabs" data-testid="menu-tabs">
      <button :class="{ on: tab === 'items' }" data-testid="tab-items" @click="setTab('items')">{{ t('nt.tab_items') }}</button>
      <button :class="{ on: tab === 'notes' }" data-testid="tab-notes" @click="setTab('notes')">{{ t('nt.tab_templates') }}</button>
      <button :class="{ on: tab === 'toppings' }" data-testid="tab-toppings" @click="setTab('toppings')">{{ t('tp.tab') }}</button>
    </div>
    <div v-if="error" class="banner bad">{{ error }}</div>
    <div v-if="trFailed && translator" class="banner bad row between" data-testid="tr-failed-banner">{{ t('mtr.failed', { at: '' }) }}
      <button class="btn small" @click="showLangs(null)">{{ t('mtr.open') }}</button></div>

    <NoteTemplates v-if="tab === 'notes'" :base="`/merchant/branches/${bid}`" :cats="cats" :can-edit="manager" @changed="load(); loadTpls()" />
    <ToppingTemplates v-else-if="tab === 'toppings'" :base="`/merchant/branches/${bid}`" :cats="cats" :can-edit="manager" :cur="cur" @changed="load(); loadToppings()" />

    <template v-else>
    <div v-for="c in cats" :key="c.id" class="card" style="padding:0;overflow:auto">
      <div class="row between wrap" style="padding:10px 12px"><h2>{{ both(c.name, c.name_en) }}
          <span v-if="c.required" class="tag brand" :title="t('cat_required_hint')" data-testid="cat-required">{{ t('cat_required') }}</span></h2>
        <div class="row" v-if="manager">
          <button class="btn small" :title="t('cat_required_hint')" data-testid="toggle-required" @click="setRequired(c)">{{ c.required ? t('cat_required_off') : t('cat_required_on') }}</button>
          <button class="btn small" @click="edit(null, c.id)">+ {{ t('new_item') }}</button></div></div>
      <table class="data">
        <tbody>
          <tr v-for="it in c.items" :key="it.id">
            <td><span v-if="it.code" class="tag brand" style="margin-inline-end:6px" data-testid="item-code-tag">{{ it.code }}</span><b>{{ it.name }}</b>
              <span v-if="it.note_template_ids?.length" class="tag warn" style="margin-inline-start:6px" data-testid="item-notes-tag">{{ t('nt.on', { names: it.note_template_ids.map(tplName).join('、') }) }}</span>
              <span v-if="it.topping_template_ids?.length" class="tag info" style="margin-inline-start:6px" data-testid="item-toppings-tag">{{ t('tp.on', { names: it.topping_template_ids.map(toppingName).join('、') }) }}</span>
              <div class="muted small">{{ it.name_en }}</div></td>
            <td class="price">{{ money(it.price, cur) }}</td>
            <td>
              <span class="tag" :class="it.published ? 'ok' : ''">{{ it.published ? t('published') : t('draft') }}</span>
              <span class="tag bad" v-if="it.sold_out" style="margin-inline-start:4px">{{ t('sold_out') }}</span>
              <span class="tag info" v-if="it.stock_qty !== null" style="margin-inline-start:4px">{{ t('stock') }} {{ it.stock_qty }}</span>
              <span class="tag" v-if="!it.needs_prep" style="margin-inline-start:4px">{{ t('no_prep') }}</span>
              <span class="tag info" v-else-if="it.station_id" style="margin-inline-start:4px">{{ stationName(it.station_id) }}</span>
              <span class="tag warn" v-else-if="stations.length" style="margin-inline-start:4px">{{ t('unassigned') }}</span>
            </td>
            <td class="right">
              <div class="row wrap" style="justify-content:flex-end;gap:6px">
                <template v-if="soldOut">
                <button class="btn small" @click="quick(it, { sold_out: !it.sold_out })">{{ it.sold_out ? t('restore') : t('mark_sold_out') }}</button>
                <button class="btn small" @click="setStock(it)">{{ t('stock') }}</button>
                </template>
                <button v-if="translator && !manager" class="btn small" :data-testid="'item-langs-' + it.id" @click="showLangs(it)">{{ t('mtr.open') }}</button>
                <template v-if="manager">
                  <button class="btn small" @click="quick(it, { published: !it.published })">{{ it.published ? t('unpublish') : t('publish') }}</button>
                  <button class="btn small" @click="edit(it)">{{ t('edit') }}</button>
                  <button class="btn small" :data-testid="'item-langs-' + it.id" @click="showLangs(it)">{{ t('mtr.open') }}</button>
                  <button class="btn small danger" @click="archive(it)">{{ t('archive') }}</button>
                </template>
              </div>
            </td>
          </tr>
          <tr v-if="c.items.length === 0"><td class="muted">—</td></tr>
        </tbody>
      </table>
    </div>
    </template>

    <MenuImportDialog v-if="importing" :base="`/merchant/branches/${bid}`" :cur="cur" @imported="load" @close="importing = false" />

    <MenuTranslations v-if="langs.open" :base="`/merchant/branches/${bid}`" :cats="cats" :item="langs.item" @close="closeLangs" />

    <ItemForm v-if="form.open" :item="form.item" :category-id="form.categoryId" :cats="cats" :stations="stations" :cur="cur"
      :base="`/merchant/branches/${bid}`" @saved="saved" @close="form.open = false" @manage-notes="form.open = false; setTab('notes')" @manage-toppings="form.open = false; setTab('toppings')" />
  </div>
</template>

<style scoped>
</style>
