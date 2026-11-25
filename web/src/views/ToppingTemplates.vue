<script setup lang="ts">
// The menu page's "toppings" tab (加料模板), in three columns like the
// flavour tab: the store's topping templates; the chosen one with its name,
// the title customers see, how many they may pick and a table of code, name,
// price and sold out; and a live preview of how customers pick them (with the
// toppings' total) above the dishes that use it. A dish that uses a template
// gets its toppings as an option group, kept in step.
import { computed, nextTick, onMounted, reactive, ref, watch } from 'vue'
import { ApiError, del, get, post, put } from '../api'
import { currencySymbol, money, t, toMajor, toMinor } from '../i18n'
import '../toppings'
import '../menuPage'
import MxIcon from './MxIcon.vue'
import type { Category, ToppingTemplate } from '../types'

const props = defineProps<{ base: string; cats: Category[]; canEdit: boolean; cur: string }>()
const emit = defineEmits<{ changed: [] }>()

const ICONS = ['🧋', '🍟', '🍜', '🍚', '🥩', '🥬', '🍳', '🧀', '🌶️', '🍢', '🥟', '🍦', '🍰', '🥤', '➕', '⭐']

interface Row { id: number; code: string; name: string; name_en: string; price: number | string | null; sold_out: boolean }
interface Form { id: number; name: string; icon: string; title: string; max_select: number; rows: Row[] }

const list = ref<ToppingTemplate[]>([])
const loaded = ref(false)
const error = ref('')
const notice = ref('')
const sel = ref(0)
const form = reactive<Form>({ id: 0, name: '', icon: '', title: '', max_select: 0, rows: [] })
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
  if (!tpl) Object.assign(form, { id: 0, name: '', icon: '', title: '', max_select: 0, rows: [] })
  else Object.assign(form, { id: tpl.id, name: tpl.name, icon: tpl.icon ?? '', title: tpl.title, max_select: tpl.max_select,
    rows: tpl.options.map(o => ({ id: o.id, code: o.code, name: o.name, name_en: o.name_en, price: toMajor(o.price).toFixed(2), sold_out: o.sold_out })) })
  error.value = ''
  pv.value = []
  void nextTick(() => { dirty.value = false })
}
watch(form, () => { dirty.value = true }, { deep: true })

function choose(id: number) {
  if (dirty.value && sel.value !== id && !confirm(t('tp.unsaved'))) return
  pick(id)
}
function newTemplate() {
  if (dirty.value && !confirm(t('tp.unsaved'))) return
  sel.value = -1
  Object.assign(form, { id: 0, name: '', icon: '', title: '', max_select: 0, rows: [blankRow(), blankRow()] })
  error.value = ''; notice.value = ''; pv.value = []
  void nextTick(() => document.querySelector<HTMLInputElement>('[data-testid="tp-name"]')?.focus())
}
function addRow() {
  form.rows.push(blankRow())
  void nextTick(() => { const all = document.querySelectorAll<HTMLInputElement>('[data-testid="tp-code"]'); all[all.length - 1]?.focus() })
}
// Enter in the last row's price starts another row
function enterRow(i: number) { if (i === form.rows.length - 1) addRow() }
function removeRow(i: number) { form.rows.splice(i, 1); pv.value = [] }

const filled = computed(() => form.rows.filter(r => r.name.trim() || r.code.trim()))
const maxChoices = computed(() => Math.max(1, filled.value.length))

const search = ref('')
const shownList = computed(() => { const q = search.value.trim().toLowerCase(); return list.value.filter(x => !q || x.name.toLowerCase().includes(q)) })
const lineOf = (x: ToppingTemplate) => x.max_select ? t('mx.tp_line_max', { n: x.options.length, m: x.max_select }) : t('mx.tp_line', { n: x.options.length })

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
  const body = { name: form.name, icon: form.icon, title: form.title, max_select: Math.min(form.max_select, filled.value.length),
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

// ---- the customer's preview: pick toppings and see their total ----
// (only a picture of the dish's option sheet; the real price is worked out by
// the order, from the dish's option group, as before)
const pv = ref<number[]>([])
const pvMax = computed(() => Math.min(form.max_select, filled.value.length))
function togglePv(i: number) {
  const r = filled.value[i]
  if (!r || r.sold_out) return
  if (pv.value.includes(i)) pv.value = pv.value.filter(x => x !== i)
  else if (!pvMax.value || pv.value.length < pvMax.value) pv.value = [...pv.value, i]
}
watch(() => filled.value.map(r => r.sold_out).join(), () => { pv.value = pv.value.filter(i => filled.value[i] && !filled.value[i].sold_out) })
const pvSum = computed(() => pv.value.reduce((s, i) => s + toMinor(filled.value[i]?.price ?? 0), 0))

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
const allItems = computed(() => props.cats.flatMap(c => c.items))
const linked = computed(() => (current.value?.item_ids ?? []).map(id => allItems.value.find(i => i.id === id)).filter(i => !!i))
const m = (minor: number) => money(minor, props.cur)
const sym = computed(() => currencySymbol(props.cur))
</script>

<template>
  <div class="tp-page" data-testid="topping-templates">
    <p class="tp-intro">{{ t('mx.tp_intro') }}</p>
    <div class="tp-wrap">
      <!-- the templates -->
      <div class="tp-side">
        <aside class="tp-card tp-list">
          <div class="tp-lh"><h2>{{ t('mx.tp_list') }}</h2><span class="muted">{{ t('mx.n_tpl', { n: list.length }) }}</span></div>
          <label class="tp-search"><MxIcon name="search" class="si" /><input v-model="search" :placeholder="t('mx.search_tpl')" /></label>
          <button v-for="x in shownList" :key="x.id" class="tp-li" :class="{ on: sel === x.id }" :data-testid="`tp-item-${x.name}`" @click="choose(x.id)">
            <span class="tp-ico">{{ x.icon || '➕' }}</span>
            <span class="grow"><b>{{ x.name }}</b><small>{{ lineOf(x) }}</small>
              <small class="tp-useline">{{ x.item_ids.length ? t('mx.nt_used', { n: x.item_ids.length }) : t('tp.unused') }}</small></span>
          </button>
          <button v-if="sel === -1" class="tp-li on"><span class="tp-ico">{{ form.icon || '➕' }}</span><span class="grow"><b>{{ form.name || t('mx.tp_new_title') }}</b></span></button>
          <div v-if="loaded && !list.length && sel !== -1" class="muted small" style="padding:12px 16px">{{ t('tp.none') }}</div>
          <div v-if="canEdit" class="tp-foot"><button class="btn tp-newbtn" data-testid="tp-new" @click="newTemplate">{{ t('tp.new') }}</button></div>
        </aside>
        <div class="tp-why"><b><MxIcon name="info" /> {{ t('mx.tp_sync_title') }}</b><br />{{ t('mx.tp_sync') }}</div>
      </div>

      <!-- the chosen template -->
      <section v-if="sel !== 0" class="tp-card tp-edit" data-testid="tp-editor">
        <div class="tp-eh"><h2>{{ form.id ? t('mx.tp_edit') : t('mx.tp_new_title') }}</h2><span class="grow"></span>
          <span v-if="form.id && !dirty" class="tp-saved">✓ {{ t('mx.saved') }}</span>
          <span v-else-if="canEdit" class="tp-dirty">{{ t('mx.unsaved') }}</span></div>
        <div class="tp-eb">
          <div class="grid3">
            <label class="field"><span>{{ t('tp.name') }}</span>
              <div class="tp-namebox">
                <details class="tp-icons">
                  <summary :title="t('mx.icon')">{{ form.icon || '➕' }}</summary>
                  <div class="tp-iconlist"><button v-for="i in ICONS" :key="i" type="button" :class="{ on: form.icon === i }" :disabled="!canEdit"
                    @click="form.icon = i; ($event.target as HTMLElement).closest('details')?.removeAttribute('open')">{{ i }}</button></div>
                </details>
                <input v-model="form.name" maxlength="30" :disabled="!canEdit" data-testid="tp-name" /></div>
              <small class="muted">{{ t('mx.only_store') }}</small></label>
            <label class="field"><span>{{ t('tp.title') }}</span><input v-model="form.title" maxlength="20" :placeholder="t('tp.title_ph')" :disabled="!canEdit" data-testid="tp-title" /></label>
            <label class="field"><span>{{ t('tp.max') }}</span>
              <select v-model.number="form.max_select" :disabled="!canEdit" data-testid="tp-max">
                <option :value="0">{{ t('tp.max_any') }}</option>
                <option v-for="n in maxChoices" :key="n" :value="n">{{ t('tp.max_n', { n }) }}</option>
              </select></label>
          </div>

          <div class="tp-sec"><h3>{{ t('mx.tp_rows') }}</h3><span class="muted small">{{ t('mx.tp_rows_n', { n: filled.length }) }}</span></div>
          <div class="tp-tablewrap">
            <table class="tp-rows">
              <thead><tr><th class="h"></th><th class="cd">{{ t('tp.col_code') }}</th><th>{{ t('tp.col_name') }}</th><th class="pr">{{ t('tp.col_price') }} ({{ cur }})</th>
                <th class="so">{{ t('tp.col_sold') }}</th><th class="x"></th></tr></thead>
              <tbody>
                <tr v-for="(r, i) in form.rows" :key="i" :class="{ sold: r.sold_out }" data-testid="tp-row">
                  <td class="h"><MxIcon name="grip" /></td>
                  <td class="cd"><input v-model="r.code" maxlength="12" placeholder="T01" :disabled="!canEdit" data-testid="tp-code" @input="r.code = r.code.toUpperCase().replace(/\s/g, '')" /></td>
                  <td><input v-model="r.name" maxlength="30" :disabled="!canEdit" data-testid="tp-row-name" /></td>
                  <td class="pr"><span class="tp-money"><em>{{ sym }}</em><input v-model.number="r.price" type="number" min="0" step="0.01" placeholder="0.00" :disabled="!canEdit"
                    data-testid="tp-price" @keydown.enter.prevent="enterRow(i)" /></span></td>
                  <td class="so"><input v-model="r.sold_out" type="checkbox" :disabled="!canEdit || !r.name.trim()" :aria-label="t('tp.col_sold')" data-testid="tp-sold" /></td>
                  <td class="x"><button v-if="canEdit" class="tp-rm" :title="t('delete')" :aria-label="t('delete')" @click="removeRow(i)">✕</button></td>
                </tr>
              </tbody>
            </table>
          </div>
          <button v-if="canEdit && form.rows.length < 50" type="button" class="tp-addrow" data-testid="tp-add-row" @click="addRow">＋ {{ t('mx.tp_add') }}</button>
          <div class="muted small">{{ t('mx.tp_zero') }}</div>

          <div v-if="current" class="tp-use">
            <div class="tp-sec"><h3>{{ t('mx.linked') }}</h3><span class="tp-count">{{ t('mx.linked_n', { n: current.item_ids.length }) }}</span><span class="grow"></span>
              <button v-if="canEdit" class="btn link" :disabled="dirty" @click="startApply">{{ t('mx.tp_adjust') }}</button></div>
            <div class="tp-chiprow" data-testid="tp-use">
              <span v-for="it in linked" :key="it.id" class="tp-dish">{{ it.name }}</span>
              <span v-if="!linked.length" class="muted small">{{ t('tp.unused') }}</span>
            </div>
          </div>
          <div v-if="error" class="banner bad small" data-testid="tp-error">{{ error }}</div>
          <div v-if="notice && !dirty" class="banner ok small" data-testid="tp-notice">{{ notice }}</div>
        </div>
        <div v-if="canEdit" class="tp-ef">
          <div class="grow">
            <button v-if="form.id" class="tp-del" @click="remove">{{ t('tp.delete') }}</button>
            <div class="muted small">{{ t('mx.sync_hint') }}</div>
          </div>
          <button v-if="form.id" class="btn" :disabled="dirty" data-testid="tp-apply" @click="startApply">{{ t('mx.apply_items') }}</button>
          <button class="btn primary" :disabled="busy || !form.name.trim()" data-testid="tp-save" @click="save">{{ t('mx.save_changes') }}</button>
        </div>
      </section>

      <!-- what customers see, and the dishes using it -->
      <div v-if="sel !== 0" class="tp-right">
        <div class="tp-card tp-pv" data-testid="tp-preview">
          <div class="tp-pvh"><b class="tp-eye"><MxIcon name="eye" :size="20" /> {{ t('mx.preview') }}</b><span class="muted small">{{ t('mx.live') }}</span></div>
          <div class="tp-pvbox warm">
            <div class="tp-pvt"><b>{{ form.title.trim() || t('tp.title_ph') }}</b>
              <span class="muted small">{{ pvMax ? t('mx.tp_pv_max', { n: pvMax }) : t('mx.tp_pv_opt') }}</span></div>
            <div class="tp-chips">
              <button v-for="(r, i) in filled" :key="i" type="button" class="tp-chip" :class="{ on: pv.includes(i), off: r.sold_out }" :disabled="r.sold_out"
                :aria-pressed="pv.includes(i)" data-testid="tp-pv-opt" @click="togglePv(i)">
                <span class="ck" aria-hidden="true">{{ pv.includes(i) ? '✓' : '' }}</span>
                <span class="nm"><em v-if="r.code">{{ r.code }}</em>{{ r.name || '—' }}</span>
                <small v-if="r.price">+{{ m(toMinor(r.price)) }}</small><small v-else>+{{ m(0) }}</small>
                <span v-if="r.sold_out" class="so">{{ t('mx.sold_out') }}</span>
              </button>
              <div v-if="!filled.length" class="muted small" style="padding:8px 0">—</div>
            </div>
            <div class="tp-pvsum">
              <span>{{ t('mx.tp_pv_picked', { n: pv.length }) }}</span><span class="grow"></span>
              <span>{{ t('mx.tp_pv_sum') }} <b data-testid="tp-pv-sum">{{ m(pvSum) }}</b></span>
            </div>
          </div>
        </div>
        <div v-if="current" class="tp-card tp-linked">
          <div class="tp-pvh"><b>{{ t('mx.linked') }} <span class="muted" style="font-weight:500">{{ t('mx.linked_n', { n: linked.length }) }}</span></b>
            <button v-if="canEdit" class="btn link" :disabled="dirty" @click="startApply">{{ t('mx.manage') }}</button></div>
          <div v-for="it in linked.slice(0, 6)" :key="it.id" class="tp-lk">
            <img v-if="it.image_url" :src="it.image_url" alt="" loading="lazy" /><span v-else class="ph">🍽</span><span>{{ it.name }}</span></div>
          <div v-if="linked.length > 6" class="muted small" style="padding:6px 0">… +{{ linked.length - 6 }}</div>
          <div v-if="!linked.length" class="muted small" style="padding:10px 0">{{ t('mx.no_linked') }}</div>
          <button v-if="canEdit" class="btn block tp-adjust" :disabled="dirty" @click="startApply">{{ t('mx.adjust') }}</button>
        </div>
      </div>
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
/* the same three columns as the flavour tab (NoteTemplates.vue) */
.tp-page { container-type: inline-size; --pink: #fbeae3; --pink-line: #efb9a8; --card: #fffdf9; --line: #eadfce; }
.tp-intro { color: #5b4636; margin: 0 0 12px; font-size: 14.5px; }
.tp-wrap { display: grid; grid-template-columns: 280px minmax(0, 1fr) 330px; gap: 16px; align-items: start; }
.tp-card { background: var(--card); border: 1px solid var(--line); border-radius: 12px; min-width: 0; }
.tp-side, .tp-right { display: grid; gap: 14px; min-width: 0; }
.tp-list { padding: 14px 0 10px; }
.tp-lh { display: flex; align-items: baseline; justify-content: space-between; padding: 0 16px 10px; }
.tp-lh h2, .tp-eh h2 { font-size: 19px; color: #2f1810; }
.tp-search { position: relative; display: block; margin: 0 14px 10px; }
.tp-search .si { position: absolute; inset-inline-start: 10px; top: 50%; transform: translateY(-50%); color: #8a7464; font-size: 16px; pointer-events: none; }
.tp-search input { padding-inline-start: 32px; background: #fff; }
.tp-li { width: 100%; border: 0; border-bottom: 1px solid #f0e6d8; background: none; padding: 12px 16px; display: flex; gap: 12px; align-items: flex-start;
  text-align: start; font: inherit; cursor: pointer; color: var(--ink); border-inline-start: 3px solid transparent; }
.tp-li:hover { background: #fbf4ea; }
.tp-li.on { background: var(--pink); border-inline-start-color: var(--brand); }
.tp-li.on b { color: var(--brand); }
.tp-li b { display: block; overflow-wrap: anywhere; font-size: 15.5px; } .tp-li small { display: block; color: #6f5a4b; font-size: 13px; margin-top: 2px; }
.tp-ico { width: 38px; height: 38px; border-radius: 10px; display: grid; place-items: center; font-size: 20px; flex: none; background: #f6ecdd; }
.tp-foot { padding: 12px 14px 4px; }
.tp-newbtn { width: 100%; color: var(--brand); border-color: var(--pink-line); background: #fff; }
.tp-why { background: #fdf3e2; border: 1px solid #f0dcb8; color: #5b3a12; border-radius: 12px; padding: 12px 14px; font-size: 13.5px; line-height: 1.6; }
.tp-why b { font-size: 15px; color: #3b2418; }
.tp-eh { padding: 16px 20px 6px; display: flex; flex-wrap: wrap; gap: 10px; align-items: center; }
.tp-saved { background: #e6f2e2; color: #2f7a2a; border-radius: 999px; padding: 3px 12px; font-size: 13px; font-weight: 700; }
.tp-dirty { background: #fdf0d8; color: #9a5b06; border-radius: 999px; padding: 3px 12px; font-size: 13px; font-weight: 700; }
.tp-eb { padding: 6px 20px 16px; display: grid; gap: 10px; }
.grid3 { display: grid; grid-template-columns: 1.3fr 1.2fr 150px; gap: 0 14px; align-items: start; padding-bottom: 4px; border-bottom: 1px solid var(--line); }
.grid3 .field > span:first-child { color: #3b2418; font-weight: 600; font-size: 14px; }
.grid3 input, .grid3 select { background: #fff; }
.tp-namebox { display: flex; gap: 8px; align-items: center; }
.tp-icons { position: relative; flex: none; }
.tp-icons summary { list-style: none; cursor: pointer; width: 40px; height: 40px; border-radius: 9px; display: grid; place-items: center; font-size: 19px; background: #f6ecdd; }
.tp-icons summary::-webkit-details-marker { display: none; }
.tp-iconlist { position: absolute; z-index: 5; top: 46px; inset-inline-start: 0; background: #fff; border: 1px solid var(--line); border-radius: 10px; padding: 6px;
  display: grid; grid-template-columns: repeat(8, 32px); gap: 4px; box-shadow: 0 8px 24px rgba(0,0,0,.15); }
.tp-iconlist button { border: 1px solid transparent; background: none; border-radius: 6px; font-size: 18px; height: 32px; cursor: pointer; }
.tp-iconlist button.on { border-color: var(--brand); background: var(--pink); }
.tp-sec { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.tp-sec h3 { font-size: 17px; color: #2f1810; }
.tp-count { background: var(--pink); color: var(--brand); border-radius: 999px; padding: 1px 10px; font-size: 13px; font-weight: 700; }
.tp-tablewrap { border: 1px solid var(--line); border-radius: 10px; overflow: hidden; }
.tp-rows { width: 100%; border-collapse: collapse; }
.tp-rows th { background: #f4ebde; font-size: 13.5px; font-weight: 600; color: #3b2418; text-align: start; padding: 8px 6px; }
.tp-rows td { padding: 6px; border-top: 1px solid #f0e6d8; }
.tp-rows input:not([type=checkbox]) { width: 100%; min-width: 0; background: #fff; padding: 6px 9px; }
.tp-rows .h { width: 26px; color: #a8927f; text-align: center; } .tp-rows .cd { width: 92px; } .tp-rows .pr { width: 140px; }
.tp-rows .so { width: 56px; text-align: center; } .tp-rows .x { width: 40px; text-align: center; }
.tp-rows tr.sold td { background: #f7f2ea; }
.tp-rows tr.sold input:not([type=checkbox]), .tp-rows tr.sold .tp-money { color: #a8927f; background: #f7f2ea; }
.tp-money { display: flex; align-items: center; border: 1px solid var(--line); border-radius: 8px; background: #fff; padding-inline-start: 8px; }
.tp-money em { font-style: normal; color: #5b4636; font-weight: 600; }
.tp-money input { border: 0 !important; text-align: end; outline: 0; }
.tp-money:focus-within { outline: 2px solid var(--brand); outline-offset: -1px; }
.tp-eye { display: inline-flex; align-items: center; gap: 8px; }
.tp-rm { border: 0; background: none; color: #5b4636; font-size: 15px; cursor: pointer; padding: 4px 8px; }
.tp-addrow { border: 1px dashed #cdb89f; background: #fffaf3; border-radius: 10px; padding: 12px; font: inherit; font-weight: 700; color: var(--brand); cursor: pointer; }
.tp-addrow:hover { background: var(--pink); }
.tp-use { border-top: 1px solid var(--line); padding-top: 12px; display: grid; gap: 8px; }
.tp-chiprow { display: flex; flex-wrap: wrap; gap: 8px; }
.tp-dish { border: 1px solid var(--line); background: #fff; border-radius: 8px; padding: 5px 14px; font-weight: 600; }
.tp-ef { display: flex; flex-wrap: wrap; align-items: center; gap: 10px; padding: 14px 20px; border-top: 1px solid var(--line); }
.tp-del { border: 0; background: none; padding: 0; color: var(--brand); font: inherit; font-weight: 700; cursor: pointer; margin-bottom: 4px; }
.tp-ef .btn { background: #fff; } .tp-ef .btn.primary { background: var(--brand); color: #fff; }
.tp-pv, .tp-linked { padding: 14px 16px; }
.tp-pvh { display: flex; align-items: center; justify-content: space-between; gap: 8px; padding-bottom: 12px; border-bottom: 1px solid var(--line); margin-bottom: 12px; font-size: 16px; }
.tp-pvbox { background: #f8f1e6; border-radius: 10px; padding: 12px 14px; }
.tp-pvt { display: flex; justify-content: space-between; align-items: baseline; gap: 8px; margin-bottom: 10px; font-size: 16px; }
.tp-chips { display: grid; gap: 8px; }
.tp-chip { display: flex; align-items: center; gap: 10px; border: 1px solid #e3d3bd; border-radius: 8px; background: #fff; padding: 8px 12px; font: inherit; cursor: pointer; text-align: start; color: var(--ink); }
.tp-chip .ck { width: 18px; height: 18px; border: 1.5px solid #b9a48e; border-radius: 4px; display: grid; place-items: center; font-size: 12px; color: #fff; flex: none; }
.tp-chip .nm { flex: 1; min-width: 0; overflow-wrap: anywhere; font-weight: 600; }
.tp-chip small { color: var(--brand); font-weight: 700; white-space: nowrap; }
.tp-chip.on { background: var(--pink); border-color: var(--brand); }
.tp-chip.on .ck { background: var(--brand); border-color: var(--brand); }
.tp-chip.off { border-style: dashed; background: #f7f2ea; cursor: not-allowed; }
.tp-chip.off .nm, .tp-chip.off small { color: #a8927f; }
.tp-chip.off .ck { visibility: hidden; }
.tp-chip .so { background: #e6dccd; color: #6f5a4b; border-radius: 999px; padding: 1px 8px; font-size: 12px; font-weight: 700; }
.tp-chip em, .tp-ch em { font-style: normal; font-size: 11px; font-weight: 700; background: #3b2418; color: #f4dfa8; border-radius: 4px; padding: 0 4px; margin-inline-end: 6px; }
.tp-pvsum { display: flex; align-items: baseline; gap: 6px; border-top: 1px solid var(--line); margin-top: 12px; padding-top: 10px; color: #6f5a4b; font-size: 14px; }
.tp-pvsum b { color: var(--brand); font-size: 19px; margin-inline-start: 6px; }
.tp-lk { display: flex; align-items: center; gap: 14px; padding: 8px 0; border-bottom: 1px solid #f0e6d8; font-weight: 600; }
.tp-lk img, .tp-lk .ph { width: 64px; height: 56px; border-radius: 8px; object-fit: cover; flex: none; }
.tp-lk .ph { background: #f3e9da; display: grid; place-items: center; font-size: 20px; }
.tp-adjust { margin-top: 12px; background: #fff; }
.tp-apply { max-width: 620px; }
.tp-cat { margin-bottom: 10px; display: flex; flex-wrap: wrap; gap: 6px; }
.tp-ch { display: inline-flex; align-items: center; gap: 6px; border: 1px solid var(--line); border-radius: 8px; padding: 6px 10px; cursor: pointer; }
.tp-ch.head { flex-basis: 100%; border: 0; padding: 4px 0; }
@container (max-width: 1150px) {
  .tp-wrap { grid-template-columns: 250px minmax(0, 1fr); }
  .tp-right { grid-column: 2; grid-template-columns: repeat(auto-fit, minmax(260px, 1fr)); align-items: start; }
  .grid3 { grid-template-columns: 1fr 1fr; }
}
@container (max-width: 760px) {
  .tp-wrap { grid-template-columns: 1fr; }
  .tp-right { grid-column: auto; grid-template-columns: 1fr; }
  .tp-list { display: flex; flex-wrap: wrap; gap: 8px; padding: 10px; }
  .tp-lh, .tp-search { flex-basis: 100%; margin: 0; padding: 0; }
  .tp-li { width: auto; flex: 1 1 200px; border: 1px solid var(--line); border-radius: 10px; padding: 8px 10px; }
  .tp-li .tp-useline { display: none; }
  .tp-foot { flex-basis: 100%; padding: 4px 0 0; }
  .tp-why { display: none; }
  .tp-eh, .tp-eb { padding-inline: 12px; }
  .tp-ef { padding: 12px; } .tp-ef .btn { flex: 1 1 auto; } .tp-ef > .grow { flex-basis: 100%; }
  .grid3 { grid-template-columns: 1fr; }
  .tp-rows .h { display: none; }
  .tp-rows .cd { width: 70px; } .tp-rows .pr { width: 96px; } .tp-rows .so { width: 44px; } .tp-rows .x { width: 30px; }
  .tp-rows th, .tp-rows td { padding: 5px 3px; }
  .tp-rows input:not([type=checkbox]) { padding: 6px 5px; }
}
</style>
