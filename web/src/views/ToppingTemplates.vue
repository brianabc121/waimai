<script setup lang="ts">
// The menu page's "toppings" tab (加料模板): the store's topping templates on
// the left; the chosen one on the right as a table of code, name and price,
// with how it looks when ordering and the "apply to dishes" dialog. A dish
// that uses a template gets its toppings as an option group, kept in step.
import { computed, nextTick, onMounted, reactive, ref, watch } from 'vue'
import { ApiError, del, get, post, put } from '../api'
import { money, t, toMajor, toMinor } from '../i18n'
import '../toppings'
import type { Category, ToppingTemplate } from '../types'

const props = defineProps<{ base: string; cats: Category[]; canEdit: boolean; cur: string }>()
const emit = defineEmits<{ changed: [] }>()

interface Row { id: number; code: string; name: string; name_en: string; price: number | null; sold_out: boolean }
interface Form { id: number; name: string; title: string; max_select: number; rows: Row[] }

const list = ref<ToppingTemplate[]>([])
const loaded = ref(false)
const error = ref('')
const notice = ref('')
const sel = ref(0)
const form = reactive<Form>({ id: 0, name: '', title: '', max_select: 0, rows: [] })
const dirty = ref(false)
const busy = ref(false)
const blankRow = (): Row => ({ id: 0, code: '', name: '', name_en: '', price: null, sold_out: false })

async function load(keep = sel.value) {
  try {
    list.value = (await get<{ templates: ToppingTemplate[] }>(`${props.base}/topping-templates`)).templates
    loaded.value = true
    pick(list.value.some(x => x.id === keep) ? keep : list.value[0]?.id ?? 0)
  } catch (e) { error.value = (e as Error).message }
}
onMounted(() => load())

function pick(id: number) {
  sel.value = id
  const tpl = list.value.find(x => x.id === id)
  if (!tpl) Object.assign(form, { id: 0, name: '', title: '', max_select: 0, rows: [] })
  else Object.assign(form, { id: tpl.id, name: tpl.name, title: tpl.title, max_select: tpl.max_select,
    rows: [...tpl.options.map(o => ({ id: o.id, code: o.code, name: o.name, name_en: o.name_en, price: toMajor(o.price), sold_out: o.sold_out })), blankRow()] })
  error.value = ''
  void nextTick(() => { dirty.value = false })
}
watch(form, () => {
  dirty.value = true
  // there is always one empty row at the end to type into
  const last = form.rows[form.rows.length - 1]
  if (props.canEdit && (sel.value !== 0) && (!last || last.name.trim() || last.code.trim())) form.rows.push(blankRow())
}, { deep: true })

function choose(id: number) {
  if (dirty.value && sel.value !== id && !confirm(t('tp.unsaved'))) return
  pick(id)
}
function newTemplate() {
  if (dirty.value && !confirm(t('tp.unsaved'))) return
  sel.value = -1
  Object.assign(form, { id: 0, name: '', title: '', max_select: 0, rows: [blankRow(), blankRow()] })
  error.value = ''; notice.value = ''
  void nextTick(() => document.querySelector<HTMLInputElement>('[data-testid="tp-name"]')?.focus())
}

const filled = computed(() => form.rows.filter(r => r.name.trim() || r.code.trim()))
const maxChoices = computed(() => Math.max(1, filled.value.length))

function errText(e: unknown) {
  if (e instanceof ApiError) {
    if (e.code === 'TOPPING_CODE_TWICE') return t('tp.code_twice', { code: e.details?.code ?? '' })
    if (e.code === 'TOPPING_NAME_TWICE') return t('tp.name_twice', { name: e.details?.name ?? '' })
    if (e.code === 'TOPPING_TEMPLATE_NAME_TAKEN') return t('tp.name_taken')
    if (e.code === 'TOPPING_TEMPLATES_FULL') return t('tp.full', { names: (e.details?.names ?? []).join('、') })
  }
  return (e as Error).message
}

async function save() {
  error.value = ''; notice.value = ''
  if (!filled.value.length || filled.value.some(r => !r.name.trim())) { error.value = t('tp.need_rows'); return }
  busy.value = true
  const body = { name: form.name, title: form.title, max_select: Math.min(form.max_select, filled.value.length),
    options: filled.value.map(r => ({ id: r.id, code: r.code, name: r.name, name_en: r.name_en, price: toMinor(r.price ?? 0), sold_out: r.sold_out })) }
  try {
    const r = form.id ? await put<{ id: number }>(`${props.base}/topping-templates/${form.id}`, body)
      : await post<{ id: number }>(`${props.base}/topping-templates`, body)
    dirty.value = false
    await load(r.id)
    notice.value = t('tp.saved')
    emit('changed')
  } catch (e) { error.value = errText(e) } finally { busy.value = false }
}

async function remove() {
  const tpl = list.value.find(x => x.id === form.id)
  if (!tpl || !confirm(t('tp.delete_confirm', { name: tpl.name, n: tpl.item_ids.length }))) return
  try { await del(`${props.base}/topping-templates/${tpl.id}`); dirty.value = false; await load(0); emit('changed') } catch (e) { error.value = errText(e) }
}

// ---- apply to dishes ----
const apply = reactive<{ open: boolean; ids: number[]; busy: boolean; error: string }>({ open: false, ids: [], busy: false, error: '' })
const current = computed(() => list.value.find(x => x.id === form.id) ?? null)
function startApply() { Object.assign(apply, { open: true, ids: [...(current.value?.item_ids ?? [])], busy: false, error: '' }) }
function toggleItem(id: number) { const i = apply.ids.indexOf(id); if (i >= 0) apply.ids.splice(i, 1); else apply.ids.push(id) }
function toggleCat(c: Category) {
  const ids = c.items.map(i => i.id)
  const all = ids.every(id => apply.ids.includes(id))
  apply.ids = all ? apply.ids.filter(id => !ids.includes(id)) : [...new Set([...apply.ids, ...ids])]
}
async function saveApply() {
  apply.busy = true; apply.error = ''
  try {
    await put(`${props.base}/topping-templates/${form.id}/items`, { item_ids: apply.ids })
    apply.open = false
    await load(form.id)
    emit('changed')
  } catch (e) { apply.error = errText(e) } finally { apply.busy = false }
}
const itemName = (id: number) => props.cats.flatMap(c => c.items).find(i => i.id === id)?.name ?? ''
const m = (minor: number) => money(minor, props.cur)
</script>

<template>
  <div class="tp-page" data-testid="topping-templates">
    <p class="muted small" style="margin:0 0 12px">{{ t('tp.intro') }}</p>
    <div class="tp-wrap">
      <aside class="tp-list">
        <button v-for="x in list" :key="x.id" class="tp-li" :class="{ on: sel === x.id }" :data-testid="`tp-item-${x.name}`" @click="choose(x.id)">
          <b>{{ x.name }}</b>
          <small>{{ t('tp.n_items', { n: x.options.length }) }} · {{ x.item_ids.length ? t('tp.used_by', { n: x.item_ids.length }) : t('tp.unused') }}</small>
        </button>
        <div v-if="loaded && !list.length" class="muted small" style="padding:8px">{{ t('tp.none') }}</div>
        <button v-if="canEdit" class="btn block" data-testid="tp-new" @click="newTemplate">{{ t('tp.new') }}</button>
      </aside>

      <section v-if="sel !== 0" class="card tp-edit" data-testid="tp-editor">
        <div class="grid3">
          <label class="field"><span>{{ t('tp.name') }}</span><input v-model="form.name" maxlength="30" :disabled="!canEdit" data-testid="tp-name" />
            <small class="muted">{{ t('tp.name_hint') }}</small></label>
          <label class="field"><span>{{ t('tp.title') }}</span><input v-model="form.title" maxlength="20" :placeholder="t('tp.title_ph')" :disabled="!canEdit" data-testid="tp-title" /></label>
          <label class="field"><span>{{ t('tp.max') }}</span>
            <select v-model.number="form.max_select" :disabled="!canEdit" data-testid="tp-max">
              <option :value="0">{{ t('tp.max_any') }}</option>
              <option v-for="n in maxChoices" :key="n" :value="n">{{ t('tp.max_n', { n }) }}</option>
            </select></label>
        </div>

        <table class="data tp-rows">
          <thead><tr><th style="width:110px">{{ t('tp.col_code') }}</th><th>{{ t('tp.col_name') }}</th><th style="width:130px">{{ t('tp.col_price') }} ({{ cur }})</th>
            <th style="width:70px">{{ t('tp.col_sold') }}</th><th style="width:40px"></th></tr></thead>
          <tbody>
            <tr v-for="(r, i) in form.rows" :key="i" data-testid="tp-row">
              <td><input v-model="r.code" maxlength="12" placeholder="AC02" :disabled="!canEdit" data-testid="tp-code" @input="r.code = r.code.toUpperCase().replace(/\s/g, '')" /></td>
              <td><input v-model="r.name" maxlength="30" :disabled="!canEdit" data-testid="tp-row-name" /></td>
              <td><input v-model.number="r.price" type="number" min="0" step="0.01" placeholder="0" :disabled="!canEdit" data-testid="tp-price" /></td>
              <td class="center"><input v-model="r.sold_out" type="checkbox" :disabled="!canEdit || !r.name.trim()" /></td>
              <td><button v-if="canEdit && (r.name || r.code)" class="btn link small" :title="t('delete')" @click="form.rows.splice(i, 1)">✕</button></td>
            </tr>
          </tbody>
        </table>

        <div v-if="filled.length" class="tp-preview warm">
          <div class="muted small">{{ t('tp.preview') }}</div>
          <div class="tp-ph"><b>{{ form.title.trim() || t('tp.title_ph') }}</b>
            <small>{{ form.max_select ? t('tp.max') + ' ' + t('tp.max_n', { n: form.max_select }) : t('tp.max_any') }}</small></div>
          <div class="tp-chips"><span v-for="(r, i) in filled" :key="i" class="tp-chip" :class="{ off: r.sold_out }">
            <em v-if="r.code">{{ r.code }}</em>{{ r.name }}<small v-if="r.price">+{{ m(toMinor(r.price)) }}</small></span></div>
        </div>

        <div v-if="current" class="tp-use small">
          <template v-if="current.item_ids.length">{{ t('tp.used_by', { n: current.item_ids.length }) }}：{{ current.item_ids.map(itemName).join('、') }}</template>
          <template v-else>{{ t('tp.unused') }}</template>
        </div>
        <div v-if="error" class="banner bad small" data-testid="tp-error">{{ error }}</div>
        <div v-if="notice && !dirty" class="banner ok small" data-testid="tp-notice">{{ notice }}</div>
        <div v-if="canEdit" class="row wrap tp-foot">
          <button v-if="form.id" class="btn danger" @click="remove">{{ t('tp.delete') }}</button>
          <span class="grow"></span>
          <button v-if="form.id" class="btn" :disabled="dirty" data-testid="tp-apply" @click="startApply">{{ t('tp.apply') }}</button>
          <button class="btn primary" :disabled="busy || !form.name.trim()" data-testid="tp-save" @click="save">{{ t('tp.save') }}</button>
        </div>
      </section>
    </div>

    <div v-if="apply.open && current" class="sheet-mask" @click.self="apply.open = false">
      <div class="sheet center tp-apply" data-testid="tp-apply-sheet">
        <div class="sheet-head row between"><h2>{{ t('tp.apply_title', { name: current.name }) }}</h2><button class="btn link" @click="apply.open = false">✕</button></div>
        <div class="sheet-body">
          <p class="muted small" style="margin-top:0">{{ t('tp.apply_hint') }}</p>
          <div v-for="c in cats" :key="c.id" class="tp-cat">
            <label class="tp-ch head"><input type="checkbox" :checked="c.items.length > 0 && c.items.every(i => apply.ids.includes(i.id))" @change="toggleCat(c)" /><b>{{ c.name }}</b></label>
            <label v-for="it in c.items" :key="it.id" class="tp-ch" :data-testid="`tp-dish-${it.name}`">
              <input type="checkbox" :checked="apply.ids.includes(it.id)" @change="toggleItem(it.id)" />
              <span><em v-if="it.code">{{ it.code }}</em>{{ it.name }}</span></label>
          </div>
          <div v-if="apply.error" class="banner bad small">{{ apply.error }}</div>
        </div>
        <div class="sheet-foot row"><button class="btn grow" @click="apply.open = false">{{ t('cancel') }}</button>
          <button class="btn primary grow" :disabled="apply.busy" data-testid="tp-apply-save" @click="saveApply">{{ t('tp.save') }}</button></div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.tp-wrap { display: grid; grid-template-columns: 240px 1fr; gap: 14px; align-items: start; }
.tp-list { display: grid; gap: 6px; }
.tp-li { text-align: start; border: 1px solid var(--line); background: #fff; border-radius: 10px; padding: 9px 12px; cursor: pointer; font: inherit; }
.tp-li.on { border-color: var(--brand); background: var(--brand-soft); }
.tp-li small { display: block; color: var(--muted); margin-top: 2px; }
.tp-edit { display: grid; gap: 12px; }
.grid3 { display: grid; grid-template-columns: 1.4fr 1fr 120px; gap: 0 12px; align-items: start; }
.tp-rows input:not([type=checkbox]) { width: 100%; min-width: 0; }
.tp-rows td { padding: 4px 6px; }
.center { text-align: center; }
.tp-preview { border: 1px dashed var(--line); border-radius: 10px; padding: 10px 12px; background: #fffaf2; }
.tp-ph { margin: 6px 0; } .tp-ph small { color: var(--muted); margin-inline-start: 6px; }
.tp-chips { display: flex; flex-wrap: wrap; gap: 6px; }
.tp-chip { border: 1px solid #d9c8b0; border-radius: 8px; padding: 5px 10px; background: #fff; }
.tp-chip.off { opacity: .45; text-decoration: line-through; }
.tp-chip em, .tp-ch em { font-style: normal; font-size: 11px; font-weight: 700; background: #3b2418; color: #f4dfa8; border-radius: 4px; padding: 0 4px; margin-inline-end: 5px; }
.tp-chip small { color: #a8321e; font-weight: 700; margin-inline-start: 4px; }
.tp-use { color: var(--muted); overflow-wrap: anywhere; }
.tp-foot { gap: 8px; }
.tp-apply { max-width: 620px; }
.tp-cat { margin-bottom: 10px; display: flex; flex-wrap: wrap; gap: 6px; }
.tp-ch { display: inline-flex; align-items: center; gap: 6px; border: 1px solid var(--line); border-radius: 8px; padding: 6px 10px; cursor: pointer; }
.tp-ch.head { flex-basis: 100%; border: 0; padding: 4px 0; }
@media (max-width: 760px) {
  .tp-wrap { grid-template-columns: 1fr; }
  .grid3 { grid-template-columns: 1fr; }
}
</style>
