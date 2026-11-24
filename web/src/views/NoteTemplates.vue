<script setup lang="ts">
// The menu page's "flavour notes" tab (docs/NOTE_TEMPLATE_DESIGN.md §5): the
// store's templates on the left, the chosen one on the right with its groups
// and tags, the ready-made templates and the "apply to dishes" dialog. The
// platform back office uses it too, with its own `base`.
import { computed, nextTick, onMounted, reactive, ref, watch } from 'vue'
import { ApiError, del, get, post, put } from '../api'
import { t } from '../i18n'
import '../notes'
import type { Category, NoteTemplate } from '../types'
import NotePicker from './NotePicker.vue'

const props = defineProps<{ base: string; cats: Category[]; canEdit: boolean }>()
const emit = defineEmits<{ changed: [] }>()

const ICONS = ['🧋', '☕', '🌶️', '🍜', '🍚', '🥗', '🍲', '🍗', '🥟', '🍰', '🍺', '🥤', '🍦', '🧂', '📦', '⭐']

interface TagForm { id: number; name: string; active: boolean }
interface GroupForm { id: number; name: string; multi: boolean; tags: TagForm[]; draft: string }
interface Form { id: number; name: string; icon: string; groups: GroupForm[] }

const list = ref<NoteTemplate[]>([])
const loaded = ref(false)
const error = ref('')
const sel = ref<number>(0)
const form = reactive<Form>({ id: 0, name: '', icon: '', groups: [] })
const dirty = ref(false)
const busy = ref(false)

async function load(keep = sel.value) {
  try {
    list.value = (await get<{ templates: NoteTemplate[] }>(`${props.base}/note-templates`)).templates
    loaded.value = true
    const pickId = list.value.some(x => x.id === keep) ? keep : list.value[0]?.id ?? 0
    pick(pickId)
  } catch (e) { error.value = (e as Error).message }
}
onMounted(() => load())

function pick(id: number) {
  sel.value = id
  const tpl = list.value.find(x => x.id === id)
  if (!tpl) { Object.assign(form, { id: 0, name: '', icon: '', groups: [] }); dirty.value = false; return }
  Object.assign(form, { id: tpl.id, name: tpl.name, icon: tpl.icon, groups: tpl.groups.map(g => ({ id: g.id, name: g.name, multi: g.multi, draft: '',
    tags: g.tags.map(x => ({ id: x.id, name: x.name, active: x.active })) })) })
  error.value = ''
  void nextTick(() => { dirty.value = false })
}
watch(form, () => { dirty.value = true }, { deep: true })

function newTemplate() {
  sel.value = -1
  Object.assign(form, { id: 0, name: '', icon: '', groups: [{ id: 0, name: '', multi: true, tags: [], draft: '' }] })
  error.value = ''
}

const current = computed(() => list.value.find(x => x.id === form.id))
const tagCount = (tpl: NoteTemplate) => tpl.groups.reduce((n, g) => n + g.tags.length, 0)

function addTag(g: GroupForm) {
  for (const part of g.draft.split(/[,，、;；\n]/)) {
    const name = part.trim().slice(0, 20)
    if (name && !g.tags.some(x => x.name === name)) g.tags.push({ id: 0, name, active: true })
  }
  g.draft = ''
}
function moveGroup(i: number, d: number) {
  const j = i + d
  if (j < 0 || j >= form.groups.length) return
  const [g] = form.groups.splice(i, 1)
  form.groups.splice(j, 0, g)
}
// dragging a tag within its group
const drag = ref<{ g: GroupForm; i: number } | null>(null)
function drop(g: GroupForm, i: number) {
  const d = drag.value
  drag.value = null
  if (!d || d.g !== g || d.i === i) return
  const [x] = g.tags.splice(d.i, 1)
  g.tags.splice(i, 0, x)
}

// what customers will see
const preview = ref<number[]>([])
const previewGroups = computed(() => form.groups.map((g, gi) => ({ id: gi + 1, name: g.name, name_en: '', multi: g.multi,
  tags: g.tags.filter(x => x.active).map((x, xi) => ({ id: (gi + 1) * 1000 + xi, name: x.name, name_en: '', active: true })) })).filter(g => g.tags.length))

async function save() {
  for (const g of form.groups) if (g.draft.trim()) addTag(g)
  error.value = ''
  if (form.groups.some(g => !g.tags.length)) { error.value = t('nt.need_tag'); return }
  busy.value = true
  const body = { name: form.name, icon: form.icon, groups: form.groups.map(g => ({ id: g.id, name: g.name, multi: g.multi,
    tags: g.tags.map(x => ({ id: x.id, name: x.name, active: x.active })) })) }
  try {
    const r = form.id ? await put<{ id: number }>(`${props.base}/note-templates/${form.id}`, body) : await post<{ id: number }>(`${props.base}/note-templates`, body)
    await load(r.id)
    emit('changed')
  } catch (e) {
    error.value = e instanceof ApiError && e.code === 'NOTE_TEMPLATE_TAKEN' ? t('nt.taken')
      : e instanceof ApiError && e.code === 'NOTE_TAG_TWICE' ? t('nt.tag_twice', { name: String(e.details?.name ?? '') }) : (e as Error).message
  } finally { busy.value = false }
}

async function remove() {
  const tpl = current.value
  if (!tpl || !confirm(t('nt.delete_confirm', { name: tpl.name, n: tpl.item_ids.length }))) return
  try { await del(`${props.base}/note-templates/${tpl.id}`); await load(0); emit('changed') } catch (e) { error.value = (e as Error).message }
}

// ---- ready-made templates ----
interface Starter { key: string; icon: string; name: string; groups: NoteTemplate['groups']; added: boolean }
const starters = reactive<{ open: boolean; list: Starter[]; chosen: string[] }>({ open: false, list: [], chosen: [] })
async function openStarters() {
  try {
    starters.list = (await get<{ starters: Starter[] }>(`${props.base}/note-templates/starters`)).starters
    starters.chosen = starters.list.filter(s => !s.added && ['milk_tea', 'stir_fry', 'general'].includes(s.key)).map(s => s.key)
    starters.open = true
  } catch (e) { error.value = (e as Error).message }
}
async function addStarters() {
  try {
    const r = await post<{ ids: number[] }>(`${props.base}/note-templates/starters`, { keys: starters.chosen })
    starters.open = false
    await load(r.ids[0] ?? sel.value)
    emit('changed')
  } catch (e) { error.value = (e as Error).message }
}

// ---- apply to dishes ----
const apply = reactive<{ open: boolean; q: string; chosen: Set<number>; error: string }>({ open: false, q: '', chosen: new Set(), error: '' })
function openApply() { Object.assign(apply, { open: true, q: '', chosen: new Set(current.value?.item_ids ?? []), error: '' }) }
const applyCats = computed(() => {
  const q = apply.q.trim().toLowerCase()
  return props.cats.map(c => ({ c, items: c.items.filter(it => !q || it.name.toLowerCase().includes(q) || (it.code ?? '').toLowerCase().includes(q)) }))
    .filter(x => x.items.length)
})
const nameOf = (id: number) => list.value.find(x => x.id === id)?.name ?? ''
function toggleCat(items: { id: number }[], on: boolean) { for (const it of items) on ? apply.chosen.add(it.id) : apply.chosen.delete(it.id) }
async function saveApply() {
  if (!current.value) return
  apply.error = ''
  try {
    await put(`${props.base}/note-templates/${current.value.id}/items`, { item_ids: [...apply.chosen] })
    apply.open = false
    await load(current.value.id)
    emit('changed')
  } catch (e) {
    apply.error = e instanceof ApiError && e.code === 'NOTE_TEMPLATES_FULL'
      ? t('nt.full', { names: ((e.details?.names as string[] | undefined) ?? []).join('、') }) : (e as Error).message
  }
}
</script>

<template>
  <div class="nt-page" data-testid="note-templates">
    <div v-if="loaded && !list.length && sel !== -1" class="card nt-empty">
      <div class="nt-empty-ico">🧋🌶️📦</div>
      <h2>{{ t('nt.empty') }}</h2>
      <p class="muted">{{ t('nt.empty_hint') }}</p>
      <div v-if="canEdit" class="row" style="justify-content:center">
        <button class="btn primary" data-testid="nt-starters" @click="openStarters">{{ t('nt.starters') }}</button>
        <button class="btn" data-testid="nt-new" @click="newTemplate">+ {{ t('nt.new') }}</button>
      </div>
      <div class="nt-why" style="text-align:start;margin-top:16px"><b>{{ t('nt.why_title') }}</b><br />{{ t('nt.why') }}</div>
    </div>

    <div v-else-if="loaded" class="nt-wrap">
      <div>
        <div class="nt-list">
          <button v-for="x in list" :key="x.id" class="nt-item" :class="{ on: x.id === sel }" :data-testid="'nt-item-' + x.name" @click="pick(x.id)">
            <span class="nt-ico">{{ x.icon || '🏷️' }}</span>
            <span class="grow"><b>{{ x.name }}</b><small>{{ t('nt.groups_tags', { g: x.groups.length, n: tagCount(x) }) }}</small></span>
            <span class="nt-use">{{ t('nt.used_on', { n: x.item_ids.length }) }}</span>
          </button>
          <button v-if="sel === -1" class="nt-item on"><span class="nt-ico">🏷️</span><span class="grow"><b>{{ form.name || t('nt.new') }}</b></span></button>
          <div v-if="canEdit" class="nt-foot">
            <button class="btn primary" data-testid="nt-new" @click="newTemplate">+ {{ t('nt.new') }}</button>
            <button class="btn" data-testid="nt-starters" @click="openStarters">{{ t('nt.starters') }}</button>
          </div>
        </div>
        <div class="nt-why"><b>{{ t('nt.why_title') }}</b><br />{{ t('nt.why') }}</div>
      </div>

      <form v-if="sel !== 0" class="nt-editor" data-testid="nt-editor" @submit.prevent="save">
        <div class="nt-eh">
          <details class="nt-icons">
            <summary :title="t('nt.name')">{{ form.icon || '🏷️' }}</summary>
            <div class="nt-iconlist"><button v-for="i in ICONS" :key="i" type="button" :class="{ on: form.icon === i }" @click="form.icon = i">{{ i }}</button></div>
          </details>
          <input v-model="form.name" class="nt-name" maxlength="20" required :placeholder="t('nt.name')" :disabled="!canEdit" data-testid="nt-name" />
          <span class="muted small nt-hide-sm">{{ t('nt.name_hint') }}</span>
          <span class="grow"></span>
          <template v-if="canEdit && current">
            <button type="button" class="btn" data-testid="nt-apply" @click="openApply">{{ t('nt.apply', { n: current.item_ids.length }) }}</button>
            <button type="button" class="btn danger" data-testid="nt-delete" @click="remove">{{ t('delete') }}</button>
          </template>
        </div>
        <div class="nt-eb">
          <div v-if="error" class="banner bad">{{ error }}</div>
          <div v-for="(g, gi) in form.groups" :key="gi" class="nt-grp" data-testid="nt-group">
            <div class="nt-gh">
              <input v-model="g.name" maxlength="20" :placeholder="t('nt.group_ph')" :disabled="!canEdit" data-testid="nt-group-name" />
              <span class="seg light nt-seg">
                <button type="button" :class="{ on: !g.multi }" :disabled="!canEdit" data-testid="nt-single" @click="g.multi = false">{{ t('nt.single') }}</button>
                <button type="button" :class="{ on: g.multi }" :disabled="!canEdit" data-testid="nt-multi" @click="g.multi = true">{{ t('nt.multi') }}</button>
              </span>
              <span class="muted small nt-hide-sm">{{ g.multi ? t('nt.multi_hint') : t('nt.single_hint') }}</span>
              <span v-if="canEdit" class="nt-gops">
                <button type="button" :disabled="gi === 0" aria-label="↑" @click="moveGroup(gi, -1)">↑</button>
                <button type="button" :disabled="gi === form.groups.length - 1" aria-label="↓" @click="moveGroup(gi, 1)">↓</button>
                <button type="button" :disabled="form.groups.length === 1" aria-label="✕" @click="form.groups.splice(gi, 1)">✕</button>
              </span>
            </div>
            <div class="nt-tags">
              <span v-for="(x, xi) in g.tags" :key="xi + x.name" class="nt-tg" :class="{ off: !x.active }" :draggable="canEdit"
                @dragstart="drag = { g, i: xi }" @dragover.prevent @drop="drop(g, xi)">
                <button type="button" class="nt-tn" :disabled="!canEdit" :title="t('nt.tag_tip')" @click="x.active = !x.active">{{ x.name }}</button>
                <button v-if="canEdit" type="button" class="x" aria-label="✕" @click="g.tags.splice(xi, 1)">✕</button>
              </span>
              <input v-if="canEdit" v-model="g.draft" class="nt-add" maxlength="200" :placeholder="t('nt.tag_ph')" data-testid="nt-tag-input"
                @keydown.enter.prevent="addTag(g)" @blur="addTag(g)" />
            </div>
          </div>
          <div class="muted small">{{ t('nt.tag_tip') }}</div>
          <button v-if="canEdit && form.groups.length < 5" type="button" class="btn" style="justify-self:start" data-testid="nt-add-group"
            @click="form.groups.push({ id: 0, name: '', multi: false, tags: [], draft: '' })">+ {{ t('nt.add_group') }}</button>
          <div v-if="previewGroups.length">
            <div class="muted small" style="margin-bottom:6px">{{ t('nt.preview') }}</div>
            <div class="nt-preview warm"><NotePicker v-model="preview" :groups="previewGroups" /></div>
          </div>
          <div v-if="canEdit" class="row wrap" style="justify-content:flex-end;gap:10px">
            <span v-if="current?.item_ids.length" class="muted small">{{ t('nt.save_hint', { n: current.item_ids.length }) }}</span>
            <button type="button" class="btn" @click="sel === -1 ? pick(list[0]?.id ?? 0) : pick(sel)">{{ t('cancel') }}</button>
            <button class="btn primary" :disabled="busy || (!dirty && !!form.id)" data-testid="nt-save">{{ t('save') }}</button>
          </div>
        </div>
      </form>
    </div>

    <!-- ready-made templates -->
    <div v-if="starters.open" class="sheet-mask" @click.self="starters.open = false">
      <div class="sheet center" data-testid="nt-starter-sheet">
        <div class="sheet-head"><h2>{{ t('nt.starters') }}</h2><div class="muted small">{{ t('nt.starters_hint') }}</div></div>
        <div class="sheet-body nt-pick">
          <label v-for="s in starters.list" :key="s.key" :class="{ on: starters.chosen.includes(s.key), off: s.added }">
            <input v-model="starters.chosen" type="checkbox" :value="s.key" :disabled="s.added" />
            <div><b>{{ s.icon }} {{ s.name }}</b><span v-if="s.added" class="tag" style="margin-inline-start:6px">{{ t('nt.added') }}</span>
              <small v-for="g in s.groups" :key="g.name">{{ g.name ? g.name + '（' + (g.multi ? t('nt.multi') : t('nt.single')) + '）：' : '' }}{{ g.tags.map(x => x.name).join('、') }}</small></div>
          </label>
        </div>
        <div class="sheet-foot row">
          <button class="btn grow" @click="starters.open = false">{{ t('cancel') }}</button>
          <button class="btn primary grow" :disabled="!starters.chosen.length" data-testid="nt-starters-add" @click="addStarters">{{ t('nt.add_n', { n: starters.chosen.length }) }}</button>
        </div>
      </div>
    </div>

    <!-- apply to dishes -->
    <div v-if="apply.open && current" class="sheet-mask" @click.self="apply.open = false">
      <div class="sheet center nt-apply" data-testid="nt-apply-sheet">
        <div class="sheet-head"><h2>{{ t('nt.apply_title', { name: current.name }) }}</h2><div class="muted small">{{ t('nt.apply_hint') }}</div></div>
        <div style="padding:10px 16px"><input v-model="apply.q" :placeholder="t('nt.search')" style="width:100%" /></div>
        <div class="sheet-body" style="padding:0">
          <div v-if="apply.error" class="banner bad" style="margin:8px 16px">{{ apply.error }}</div>
          <template v-for="x in applyCats" :key="x.c.id">
            <label class="nt-cat">
              <input type="checkbox" :checked="x.items.every(it => apply.chosen.has(it.id))"
                :indeterminate.prop="x.items.some(it => apply.chosen.has(it.id)) && !x.items.every(it => apply.chosen.has(it.id))"
                :data-testid="'nt-cat-' + x.c.name" @change="toggleCat(x.items, ($event.target as HTMLInputElement).checked)" />
              {{ x.c.name }} <span class="muted small">{{ t('nt.cat_count', { k: x.items.filter(it => apply.chosen.has(it.id)).length, n: x.items.length }) }}</span>
            </label>
            <label v-for="it in x.items" :key="it.id" class="nt-it">
              <input type="checkbox" :checked="apply.chosen.has(it.id)" :data-testid="'nt-dish-' + it.name"
                @change="($event.target as HTMLInputElement).checked ? apply.chosen.add(it.id) : apply.chosen.delete(it.id)" />
              <span class="grow">{{ it.name }}</span>
              <span v-if="it.note_template_ids?.length" class="muted small">{{ t('nt.using', { names: it.note_template_ids.map(nameOf).join('、') }) }}</span>
            </label>
          </template>
        </div>
        <div class="sheet-foot row">
          <span class="muted small grow">{{ t('nt.total', { n: apply.chosen.size }) }}</span>
          <button class="btn" @click="apply.open = false">{{ t('cancel') }}</button>
          <button class="btn primary" data-testid="nt-apply-save" @click="saveApply">{{ t('save') }}</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.nt-wrap { display: grid; grid-template-columns: 300px minmax(0, 1fr); gap: 14px; align-items: start; }
.nt-list { background: #fff; border: 1px solid var(--line); border-radius: 12px; overflow: hidden; }
.nt-item { width: 100%; border: 0; border-bottom: 1px solid var(--line); background: #fff; padding: 12px 14px; display: flex; gap: 10px; align-items: center;
  text-align: start; font: inherit; cursor: pointer; color: var(--ink); }
.nt-item.on { background: var(--brand-soft); box-shadow: inset 3px 0 0 var(--brand); }
.nt-item b { display: block; overflow-wrap: anywhere; } .nt-item small { color: var(--muted); font-size: 12.5px; }
.nt-use { font-size: 12px; color: var(--muted); white-space: nowrap; }
.nt-ico { width: 34px; height: 34px; border-radius: 9px; display: grid; place-items: center; font-size: 17px; flex: none; background: #f6efe4; }
.nt-foot { padding: 12px 14px; display: grid; gap: 8px; }
.nt-why { margin-top: 12px; background: #fff8ec; border: 1px solid #f5ddb0; color: #7a4b05; border-radius: 10px; padding: 10px 12px; font-size: 13px; }
.nt-editor { background: #fff; border: 1px solid var(--line); border-radius: 12px; min-width: 0; }
.nt-eh { padding: 14px 18px; border-bottom: 1px solid var(--line); display: flex; flex-wrap: wrap; gap: 10px; align-items: center; }
.nt-name { font-size: 17px; font-weight: 700; width: 220px; max-width: 100%; }
.nt-icons { position: relative; }
.nt-icons summary { list-style: none; cursor: pointer; width: 38px; height: 38px; border-radius: 9px; display: grid; place-items: center; font-size: 19px; background: #f6efe4; }
.nt-icons summary::-webkit-details-marker { display: none; }
.nt-iconlist { position: absolute; z-index: 5; top: 44px; inset-inline-start: 0; background: #fff; border: 1px solid var(--line); border-radius: 10px; padding: 6px;
  display: grid; grid-template-columns: repeat(8, 32px); gap: 4px; box-shadow: 0 8px 24px rgba(0,0,0,.15); }
.nt-iconlist button { border: 1px solid transparent; background: none; border-radius: 6px; font-size: 18px; height: 32px; cursor: pointer; }
.nt-iconlist button.on { border-color: var(--brand); background: var(--brand-soft); }
.nt-eb { padding: 16px 18px; display: grid; gap: 12px; }
.nt-grp { border: 1px solid var(--line); border-radius: 10px; padding: 12px 14px; background: #fcfcfb; }
.nt-gh { display: flex; flex-wrap: wrap; align-items: center; gap: 10px; margin-bottom: 10px; }
.nt-gh input { width: 160px; max-width: 100%; }
.nt-seg button { padding: 5px 12px; }
.nt-gops { margin-inline-start: auto; display: flex; gap: 4px; }
.nt-gops button { border: 1px solid var(--line); background: #fff; border-radius: 6px; width: 30px; height: 30px; color: #57606a; cursor: pointer; }
.nt-gops button:disabled { opacity: .4; cursor: default; }
.nt-tags { display: flex; flex-wrap: wrap; gap: 6px; align-items: center; border: 1px solid var(--line); border-radius: 8px; background: #fff; padding: 6px; }
.nt-tg { display: inline-flex; align-items: center; background: #f2f3f5; border: 1px solid #e1e4e8; border-radius: 6px; max-width: 100%; }
.nt-tg .nt-tn { border: 0; background: none; font: inherit; font-size: 13.5px; padding: 3px 4px 3px 9px; cursor: pointer; color: var(--ink); overflow-wrap: anywhere; text-align: start; }
.nt-tg .x { border: 0; background: none; color: #8b949e; font-size: 12px; padding: 3px 7px; cursor: pointer; }
.nt-tg.off .nt-tn { color: #9aa1a9; text-decoration: line-through; }
.nt-tg.off { background: #fafafa; }
.nt-add { border: 0 !important; outline: 0; font-size: 13.5px; padding: 4px; min-width: 140px; flex: 1; background: none; }
.nt-preview { border: 1px dashed #d0c3ae; border-radius: 10px; padding: 12px; background: var(--paper); }
.nt-empty { text-align: center; padding: 40px 20px; max-width: 640px; margin: 20px auto; }
.nt-empty-ico { font-size: 34px; letter-spacing: 6px; }
.nt-pick { display: grid; gap: 8px; }
.nt-pick label { display: flex; align-items: flex-start; gap: 10px; border: 1px solid var(--line); border-radius: 10px; padding: 9px 12px; cursor: pointer; }
.nt-pick label.on { border-color: var(--brand); background: #fffaf6; }
.nt-pick label.off { opacity: .6; cursor: default; }
.nt-pick small { display: block; color: var(--muted); font-size: 12.5px; margin-top: 2px; }
.nt-apply { max-width: 560px; }
.nt-cat { padding: 8px 16px; background: #f6f6f4; display: flex; gap: 10px; align-items: center; font-weight: 700; border-top: 1px solid var(--line); cursor: pointer; }
.nt-it { padding: 8px 16px 8px 44px; display: flex; gap: 10px; align-items: center; border-top: 1px solid #f0f0f0; font-size: 14px; cursor: pointer; }
@media (max-width: 900px) {
  .nt-wrap { grid-template-columns: 1fr; }
  .nt-hide-sm { display: none; }
  .nt-eh, .nt-eb { padding-inline: 12px; }
}
</style>
