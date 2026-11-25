<script setup lang="ts">
// The menu page's "flavour notes" tab (docs/NOTE_TEMPLATE_DESIGN.md §5), in
// three columns: the store's templates, the chosen one with its groups and
// tags, and what customers will see with the dishes that use it. Also the
// ready-made templates and the "apply to dishes" dialog. The platform back
// office uses it too, with its own `base`; the columns stack when the room
// is narrow (a container query, so the back office's drawer works too).
import { computed, nextTick, onMounted, reactive, ref, watch } from 'vue'
import { ApiError, del, get, post, put } from '../api'
import { t } from '../i18n'
import '../notes'
import '../menuPage'
import MxIcon from './MxIcon.vue'
import type { Category, NoteTemplate } from '../types'
import NotePicker from './NotePicker.vue'
import MenuTranslations from './MenuTranslations.vue'
import './translateStrings'

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
// the template's group names and tags in the customers' languages
const langsOpen = ref(false)

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
  preview.value = []
  void nextTick(() => { dirty.value = false })
}
watch(form, () => { dirty.value = true }, { deep: true })

function newTemplate() {
  sel.value = -1
  Object.assign(form, { id: 0, name: '', icon: '', groups: [{ id: 0, name: '', multi: true, tags: [], draft: '' }] })
  error.value = ''
}

const current = computed(() => list.value.find(x => x.id === form.id))
const search = ref('')
const shownList = computed(() => { const q = search.value.trim().toLowerCase(); return list.value.filter(x => !q || x.name.toLowerCase().includes(q)) })
const allItems = computed(() => props.cats.flatMap(c => c.items))
const linked = computed(() => (current.value?.item_ids ?? []).map(id => allItems.value.find(i => i.id === id)).filter(i => !!i))
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

const previewNames = computed(() => previewGroups.value.flatMap(g => g.tags).filter(x => preview.value.includes(x.id)).map(x => x.name))

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
    <p class="nt-intro">{{ t('mx.nt_intro') }}</p>
    <div v-if="loaded && !list.length && sel !== -1" class="nt-card nt-empty">
      <div class="nt-empty-ico">🧋🌶️📦</div>
      <h2>{{ t('nt.empty') }}</h2>
      <p class="muted">{{ t('nt.empty_hint') }}</p>
      <div v-if="canEdit" class="row" style="justify-content:center">
        <button class="btn primary" data-testid="nt-starters" @click="openStarters">{{ t('nt.starters') }}</button>
        <button class="btn" data-testid="nt-new" @click="newTemplate">+ {{ t('nt.new') }}</button>
      </div>
      <div class="nt-why" style="text-align:start;margin-top:16px"><b><MxIcon name="info" /> {{ t('nt.why_title') }}</b><br />{{ t('nt.why') }}</div>
    </div>

    <div v-else-if="loaded" class="nt-wrap">
      <!-- the templates -->
      <div class="nt-side">
        <div class="nt-card nt-list">
          <div class="nt-lh"><h2>{{ t('mx.nt_list') }}</h2><span class="muted">{{ t('mx.n_tpl', { n: list.length }) }}</span></div>
          <label class="nt-search"><MxIcon name="search" class="si" /><input v-model="search" :placeholder="t('mx.search_tpl')" /></label>
          <button v-for="x in shownList" :key="x.id" class="nt-item" :class="{ on: x.id === sel }" :data-testid="'nt-item-' + x.name" @click="pick(x.id)">
            <span class="nt-ico">{{ x.icon || '🏷️' }}</span>
            <span class="grow"><b>{{ x.name }}</b><small>{{ t('mx.nt_line', { g: x.groups.length, n: tagCount(x) }) }}</small>
              <small class="nt-use">{{ x.item_ids.length ? t('mx.nt_used', { n: x.item_ids.length }) : t('mx.nt_unused') }}</small></span>
          </button>
          <button v-if="sel === -1" class="nt-item on"><span class="nt-ico">{{ form.icon || '🏷️' }}</span><span class="grow"><b>{{ form.name || t('nt.new') }}</b></span></button>
          <div v-if="canEdit" class="nt-foot">
            <button class="btn nt-newbtn" data-testid="nt-new" @click="newTemplate">＋ {{ t('nt.new') }}</button>
            <button class="btn link" data-testid="nt-starters" @click="openStarters">{{ t('mx.nt_starters') }}</button>
          </div>
        </div>
        <div class="nt-why"><b><MxIcon name="info" /> {{ t('nt.why_title') }}</b><br />{{ t('nt.why') }}</div>
      </div>

      <!-- the chosen template -->
      <form v-if="sel !== 0" class="nt-card nt-editor" data-testid="nt-editor" @submit.prevent="save">
        <div class="nt-eh">
          <h2>{{ form.id ? t('mx.nt_edit') : t('mx.nt_new_title') }}</h2>
          <span class="grow"></span>
          <button v-if="canEdit && current" type="button" class="btn small" data-testid="nt-langs" :title="t('mtr.open_hint')" @click="langsOpen = true">{{ t('mtr.open') }}</button>
          <span v-if="form.id && !dirty" class="nt-saved" data-testid="nt-saved">✓ {{ t('mx.saved') }}</span>
          <span v-else-if="canEdit" class="nt-dirty">{{ t('mx.unsaved') }}</span>
        </div>
        <div class="nt-eb">
          <div v-if="error" class="banner bad">{{ error }}</div>
          <div class="nt-namerow">
            <span class="lbl">{{ t('nt.name') }}</span>
            <details class="nt-icons">
              <summary :title="t('mx.icon')">{{ form.icon || '🏷️' }}</summary>
              <div class="nt-iconlist"><button v-for="i in ICONS" :key="i" type="button" :class="{ on: form.icon === i }" :disabled="!canEdit" @click="form.icon = i">{{ i }}</button></div>
            </details>
            <input v-model="form.name" class="nt-name" maxlength="20" required :placeholder="t('nt.name')" :disabled="!canEdit" data-testid="nt-name" />
            <span class="muted small">{{ t('mx.only_store') }}</span>
          </div>
          <div class="nt-sec"><h3>{{ t('mx.nt_groups') }}</h3><span class="muted small">{{ t('mx.nt_groups_hint') }}</span></div>
          <div v-for="(g, gi) in form.groups" :key="gi" class="nt-grp" data-testid="nt-group">
            <div class="nt-gh">
              <span class="nt-handle" aria-hidden="true"><MxIcon name="grip" /></span>
              <input v-model="g.name" class="nt-gname" maxlength="20" :placeholder="t('nt.group_ph')" :disabled="!canEdit" data-testid="nt-group-name" />
              <span class="nt-seg">
                <button type="button" :class="{ on: !g.multi }" :disabled="!canEdit" data-testid="nt-single" @click="g.multi = false">{{ t('nt.single') }}</button>
                <button type="button" :class="{ on: g.multi }" :disabled="!canEdit" data-testid="nt-multi" @click="g.multi = true">{{ t('nt.multi') }}</button>
              </span>
              <span class="grow"></span>
              <template v-if="canEdit">
                <button type="button" class="nt-gbtn" :disabled="form.groups.length === 1" :title="t('delete')" :aria-label="t('delete')" data-testid="nt-group-delete" @click="form.groups.splice(gi, 1)"><MxIcon name="trash" :size="19" /></button>
                <details class="nt-gmore">
                  <summary :aria-label="t('mx.more')"><MxIcon name="dots" :size="20" /></summary>
                  <div class="nt-gmenu">
                    <button type="button" :disabled="gi === 0" @click="moveGroup(gi, -1); ($event.target as HTMLElement).closest('details')?.removeAttribute('open')">{{ t('mx.move_up') }}</button>
                    <button type="button" :disabled="gi === form.groups.length - 1" @click="moveGroup(gi, 1); ($event.target as HTMLElement).closest('details')?.removeAttribute('open')">{{ t('mx.move_down') }}</button>
                  </div>
                </details>
              </template>
            </div>
            <div class="nt-tags">
              <span v-for="(x, xi) in g.tags" :key="xi + x.name" class="nt-tg" :class="{ off: !x.active }" :draggable="canEdit"
                @dragstart="drag = { g, i: xi }" @dragover.prevent @drop="drop(g, xi)">
                <button type="button" class="nt-tn" :disabled="!canEdit" :title="t('nt.tag_tip')" @click="x.active = !x.active">{{ x.name }}</button>
                <button v-if="canEdit" type="button" class="x" aria-label="✕" @click="g.tags.splice(xi, 1)">✕</button>
              </span>
              <input v-if="canEdit" v-model="g.draft" class="nt-add" maxlength="200" :placeholder="'＋ ' + t('mx.nt_add_tag')" :title="t('nt.tag_ph')" data-testid="nt-tag-input"
                @keydown.enter.prevent="addTag(g)" @blur="addTag(g)" />
            </div>
            <div class="nt-ghint">{{ g.multi ? t('mx.nt_any') : t('mx.nt_one') }}</div>
          </div>
          <div class="muted small">{{ t('nt.tag_tip') }}</div>
          <button v-if="canEdit && form.groups.length < 5" type="button" class="nt-addgrp" data-testid="nt-add-group"
            @click="form.groups.push({ id: 0, name: '', multi: false, tags: [], draft: '' })">＋ {{ t('nt.add_group') }}</button>
        </div>
        <div v-if="canEdit" class="nt-ef">
          <div class="grow">
            <button v-if="current" type="button" class="nt-del" data-testid="nt-delete" @click="remove">{{ t('delete') }}</button>
            <div class="muted small">{{ current?.item_ids.length ? t('nt.save_hint', { n: current.item_ids.length }) : t('mx.sync_hint') }}</div>
          </div>
          <button v-if="sel === -1" type="button" class="btn" @click="pick(list[0]?.id ?? 0)">{{ t('cancel') }}</button>
          <button v-if="current" type="button" class="btn" :disabled="dirty" data-testid="nt-apply" @click="openApply">{{ t('mx.apply_items') }}</button>
          <button class="btn primary" :disabled="busy || (!dirty && !!form.id)" data-testid="nt-save">{{ t('mx.save_changes') }}</button>
        </div>
      </form>

      <!-- what customers see, and the dishes using it -->
      <div v-if="sel !== 0" class="nt-right">
        <div class="nt-card nt-pv">
          <div class="nt-pvh"><b class="nt-eye"><MxIcon name="eye" :size="20" /> {{ t('mx.preview') }}</b><span class="muted small">{{ t('mx.live') }}</span></div>
          <div class="nt-pvbox">
            <div class="nt-pvt"><b>{{ t('mx.nt_pv_title') }}</b><span class="muted small">{{ t('mx.nt_pv_free') }}</span></div>
            <div v-if="previewGroups.length" class="nt-preview warm"><NotePicker v-model="preview" :groups="previewGroups" /></div>
            <div v-else class="muted small" style="padding:10px 0">—</div>
            <div class="nt-pvsel">{{ previewNames.length ? t('mx.nt_pv_chosen', { names: previewNames.join('、') }) : t('mx.nt_pv_none') }}</div>
          </div>
        </div>
        <div v-if="current" class="nt-card nt-linked" data-testid="nt-linked">
          <div class="nt-pvh"><b>{{ t('mx.linked') }} <span class="muted" style="font-weight:500">{{ t('mx.linked_n', { n: linked.length }) }}</span></b>
            <button v-if="canEdit" class="btn link" @click="openApply">{{ t('mx.manage') }}</button></div>
          <div v-for="it in linked.slice(0, 6)" :key="it.id" class="nt-li">
            <img v-if="it.image_url" :src="it.image_url" alt="" loading="lazy" /><span v-else class="ph">🍽</span><span>{{ it.name }}</span></div>
          <div v-if="linked.length > 6" class="muted small" style="padding:6px 0">… +{{ linked.length - 6 }}</div>
          <div v-if="!linked.length" class="muted small" style="padding:10px 0">{{ t('mx.no_linked') }}</div>
          <button v-if="canEdit" class="btn block nt-adjust" @click="openApply">{{ t('mx.adjust') }}</button>
        </div>
      </div>
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

    <MenuTranslations v-if="langsOpen && current" :base="base" :cats="cats" :tpl="current" :readonly="!canEdit" @close="langsOpen = false" />

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
/* shared look of the two template tabs (the topping tab has the same rules) */
.nt-page { container-type: inline-size; --pink: #fbeae3; --pink-line: #efb9a8; --card: #fffdf9; --line: #eadfce; }
.nt-intro { color: #5b4636; margin: 0 0 12px; font-size: 14.5px; }
.nt-wrap { display: grid; grid-template-columns: 280px minmax(0, 1fr) 330px; gap: 16px; align-items: start; }
.nt-card { background: var(--card); border: 1px solid var(--line); border-radius: 12px; min-width: 0; }
.nt-side, .nt-right { display: grid; gap: 14px; min-width: 0; }
.nt-list { padding: 14px 0 10px; }
.nt-lh { display: flex; align-items: baseline; justify-content: space-between; padding: 0 16px 10px; }
.nt-lh h2, .nt-eh h2 { font-size: 19px; color: #2f1810; }
.nt-search { position: relative; display: block; margin: 0 14px 10px; }
.nt-search .si { position: absolute; inset-inline-start: 10px; top: 50%; transform: translateY(-50%); color: #8a7464; font-size: 16px; pointer-events: none; }
.nt-search input { padding-inline-start: 32px; background: #fff; }
.nt-item { width: 100%; border: 0; border-bottom: 1px solid #f0e6d8; background: none; padding: 12px 16px; display: flex; gap: 12px; align-items: flex-start;
  text-align: start; font: inherit; cursor: pointer; color: var(--ink); border-inline-start: 3px solid transparent; }
.nt-item:hover { background: #fbf4ea; }
.nt-item.on { background: var(--pink); border-inline-start-color: var(--brand); }
.nt-item.on b { color: var(--brand); }
.nt-item b { display: block; overflow-wrap: anywhere; font-size: 15.5px; } .nt-item small { display: block; color: #6f5a4b; font-size: 13px; margin-top: 2px; }
.nt-ico { width: 38px; height: 38px; border-radius: 10px; display: grid; place-items: center; font-size: 20px; flex: none; background: #f6ecdd; }
.nt-foot { padding: 12px 14px 4px; display: grid; gap: 4px; justify-items: center; }
.nt-newbtn { width: 100%; color: var(--brand); border-color: var(--pink-line); background: #fff; }
.nt-why { background: #fdf3e2; border: 1px solid #f0dcb8; color: #5b3a12; border-radius: 12px; padding: 12px 14px; font-size: 13.5px; line-height: 1.6; }
.nt-why b { font-size: 15px; color: #3b2418; }
.nt-eh { padding: 16px 20px 12px; display: flex; flex-wrap: wrap; gap: 10px; align-items: center; }
.nt-saved { background: #e6f2e2; color: #2f7a2a; border-radius: 999px; padding: 3px 12px; font-size: 13px; font-weight: 700; }
.nt-dirty { background: #fdf0d8; color: #9a5b06; border-radius: 999px; padding: 3px 12px; font-size: 13px; font-weight: 700; }
.nt-eb { padding: 0 20px 16px; display: grid; gap: 12px; }
.nt-namerow { display: flex; flex-wrap: wrap; align-items: center; gap: 10px; padding-bottom: 14px; border-bottom: 1px solid var(--line); }
.nt-namerow .lbl { font-weight: 600; min-width: 64px; }
.nt-name { font-size: 15px; width: 300px; max-width: 100%; flex: 0 1 300px; background: #fff; }
.nt-icons { position: relative; }
.nt-icons summary { list-style: none; cursor: pointer; width: 40px; height: 40px; border-radius: 9px; display: grid; place-items: center; font-size: 20px; background: #f6ecdd; }
.nt-icons summary::-webkit-details-marker { display: none; }
.nt-iconlist { position: absolute; z-index: 5; top: 46px; inset-inline-start: 0; background: #fff; border: 1px solid var(--line); border-radius: 10px; padding: 6px;
  display: grid; grid-template-columns: repeat(8, 32px); gap: 4px; box-shadow: 0 8px 24px rgba(0,0,0,.15); }
.nt-iconlist button { border: 1px solid transparent; background: none; border-radius: 6px; font-size: 18px; height: 32px; cursor: pointer; }
.nt-iconlist button.on { border-color: var(--brand); background: var(--pink); }
.nt-sec { display: flex; align-items: baseline; gap: 12px; flex-wrap: wrap; }
.nt-sec h3 { font-size: 17px; color: #2f1810; }
.nt-grp { border: 1px solid var(--line); border-radius: 12px; padding: 12px 14px; background: #fff; }
.nt-gh { display: flex; flex-wrap: wrap; align-items: center; gap: 10px; margin-bottom: 10px; }
.nt-handle { color: #a8927f; display: inline-flex; }
.nt-gname { width: 200px; max-width: 100%; flex: 0 1 200px; }
.nt-seg { display: inline-flex; border: 1px solid var(--line); border-radius: 8px; overflow: hidden; }
.nt-seg button { border: 0; background: #fff; padding: 7px 18px; font: inherit; font-weight: 600; color: #5b4636; cursor: pointer; }
.nt-seg button + button { border-inline-start: 1px solid var(--line); }
.nt-seg button.on { background: var(--pink); color: var(--brand); }
.nt-gbtn { border: 0; background: none; width: 32px; height: 32px; border-radius: 6px; cursor: pointer; color: #5b4636; display: inline-grid; place-items: center; }
.nt-eye { display: inline-flex; align-items: center; gap: 8px; }
.nt-gbtn:disabled { opacity: .3; cursor: default; }
.nt-gmore { position: relative; }
.nt-gmore summary { list-style: none; cursor: pointer; width: 32px; height: 32px; display: grid; place-items: center; font-size: 18px; color: #5b4636; border-radius: 6px; }
.nt-gmore summary::-webkit-details-marker { display: none; }
.nt-gmenu { position: absolute; z-index: 6; top: 34px; inset-inline-end: 0; background: #fff; border: 1px solid var(--line); border-radius: 10px; padding: 4px; display: grid;
  min-width: 120px; box-shadow: 0 8px 24px rgba(0,0,0,.15); }
.nt-gmenu button { border: 0; background: none; font: inherit; text-align: start; padding: 8px 10px; border-radius: 6px; cursor: pointer; }
.nt-gmenu button:hover:not(:disabled) { background: #fbf4ea; } .nt-gmenu button:disabled { opacity: .4; }
.nt-tags { display: flex; flex-wrap: wrap; gap: 10px; align-items: center; }
.nt-tg { display: inline-flex; align-items: center; background: #fbf6ef; border: 1px solid #e6d8c4; border-radius: 8px; max-width: 100%; }
.nt-tg .nt-tn { border: 0; background: none; font: inherit; font-size: 14.5px; padding: 6px 4px 6px 14px; cursor: pointer; color: var(--ink); overflow-wrap: anywhere; text-align: start; }
.nt-tg .x { border: 0; background: none; color: #6f5a4b; font-size: 13px; padding: 6px 12px 6px 8px; cursor: pointer; }
.nt-tg.off .nt-tn { color: #9aa1a9; text-decoration: line-through; }
.nt-tg.off { background: #fafafa; }
.nt-add { border: 1px dashed #cdb89f !important; border-radius: 8px; outline: 0; font-size: 14px; padding: 6px 12px; width: 140px; background: #fff; color: var(--ink); }
.nt-add::placeholder { color: #6f5a4b; }
.nt-add:focus { border-style: solid !important; border-color: var(--brand) !important; width: 200px; }
.nt-ghint { color: #6f5a4b; font-size: 13px; margin-top: 10px; }
.nt-addgrp { border: 1px dashed #cdb89f; background: #fffaf3; border-radius: 12px; padding: 14px; font: inherit; font-weight: 700; color: var(--brand); cursor: pointer; }
.nt-addgrp:hover { background: var(--pink); }
.nt-ef { display: flex; flex-wrap: wrap; align-items: center; gap: 10px; padding: 14px 20px; border-top: 1px solid var(--line); }
.nt-del { border: 0; background: none; padding: 0; color: var(--brand); font: inherit; font-weight: 700; cursor: pointer; margin-bottom: 4px; }
.nt-ef .btn { background: #fff; }
.nt-ef .btn.primary { background: var(--brand); color: #fff; }
.nt-pv, .nt-linked { padding: 14px 16px; }
.nt-pvh { display: flex; align-items: center; justify-content: space-between; gap: 8px; padding-bottom: 12px; border-bottom: 1px solid var(--line); margin-bottom: 12px; font-size: 16px; }
.nt-pvbox { background: #f8f1e6; border-radius: 10px; padding: 12px 14px; }
.nt-pvt { display: flex; justify-content: space-between; align-items: baseline; margin-bottom: 10px; font-size: 16px; }
.nt-preview { background: transparent; }
.nt-preview :deep(.np-gn) { justify-content: space-between; font-size: 15px; }
.nt-preview :deep(.np-chips) { display: grid; grid-template-columns: repeat(auto-fill, minmax(84px, 1fr)); gap: 8px; }
.nt-preview :deep(.np-chip) { text-align: center; background: #fff; }
.nt-preview :deep(.np-chip.on) { background: var(--pink); border-color: var(--brand); color: var(--brand); }
.nt-pvsel { border-top: 1px solid var(--line); margin-top: 12px; padding-top: 10px; color: #6f5a4b; font-size: 13.5px; }
.nt-li { display: flex; align-items: center; gap: 14px; padding: 8px 0; border-bottom: 1px solid #f0e6d8; font-weight: 600; }
.nt-li img, .nt-li .ph { width: 64px; height: 56px; border-radius: 8px; object-fit: cover; flex: none; }
.nt-li .ph { background: #f3e9da; display: grid; place-items: center; font-size: 20px; }
.nt-adjust { margin-top: 12px; background: #fff; }
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
/* iPad and narrower rooms: the preview goes under the editor */
@container (max-width: 1150px) {
  .nt-wrap { grid-template-columns: 250px minmax(0, 1fr); }
  .nt-right { grid-column: 2; grid-template-columns: repeat(auto-fit, minmax(260px, 1fr)); align-items: start; }
}
@container (max-width: 760px) {
  .nt-wrap { grid-template-columns: 1fr; }
  .nt-right { grid-column: auto; grid-template-columns: 1fr; }
  .nt-list { display: flex; flex-wrap: wrap; gap: 8px; padding: 10px; }
  .nt-lh, .nt-search { flex-basis: 100%; margin: 0; padding: 0; }
  .nt-item { width: auto; flex: 1 1 200px; border: 1px solid var(--line); border-radius: 10px; padding: 8px 10px; }
  .nt-item .nt-use { display: none; }
  .nt-foot { flex-basis: 100%; padding: 4px 0 0; }
  .nt-eh, .nt-eb { padding-inline: 12px; }
  .nt-ef { padding: 12px; }
  .nt-ef .btn { flex: 1 1 auto; }
  .nt-ef > .grow { flex-basis: 100%; }
  .nt-why { display: none; }
}
</style>
