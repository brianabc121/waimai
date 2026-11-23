<script setup lang="ts">
// The menu in other languages (docs/I18N_DESIGN.md §6.1, mock-up ⑤): which
// language the menu is written in, which languages customers get, and every
// translation with its state. Opened for the whole menu (checking view) or for
// one dish. Shared by the merchant's menu page and the platform's item page.
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { get, post, put } from '../api'
import { t } from '../i18n'
import { fmtDateTime } from './admin/util'
import { LANGS, type Locale } from '../langs'
import type { Category, Item } from '../types'
import './translateStrings'

const props = defineProps<{ base: string; cats: Category[]; item?: Item | null; readonly?: boolean }>()
const emit = defineEmits<{ (e: 'close'): void }>()

interface Field { entity: string; entity_id: number; field: string; text: string }
interface Tr { entity: string; entity_id: number; field: string; lang: string; text: string; status: 'machine' | 'converted' | 'human' | 'stale'; provider: string }
interface View {
  menu_lang: Locale; menu_langs_off: string[]; provider: string; pending: boolean
  error: string; error_at: string | null; fields: Field[]; translations: Tr[]
}

const view = ref<View | null>(null)
const error = ref('')
const busy = ref(false)
let timer = 0

function take(v: View) {
  view.value = v
  clearTimeout(timer)
  // a run is waiting or going: look again shortly
  if (v.pending) timer = window.setTimeout(() => { void load() }, 3000)
}
async function load() {
  try { take(await get<View>(`${props.base}/translations`)) } catch (e) { error.value = (e as Error).message }
}
onMounted(load)
onUnmounted(() => clearTimeout(timer))

async function send(fn: () => Promise<View>) {
  if (busy.value) return
  busy.value = true; error.value = ''
  try { take(await fn()) } catch (e) { error.value = (e as Error).message }
  busy.value = false
}

// ---- which rows ----
type Kind = 'item' | 'category' | 'option' | 'description' | 'details' | 'promotion'
const kinds: Kind[] = ['item', 'category', 'option', 'description', 'details', 'promotion']
const kind = ref<Kind>('item')
const ofKind = (f: Field, k: Kind) => {
  switch (k) {
    case 'item': return f.entity === 'item' && f.field === 'name'
    case 'description': return f.entity === 'item' && f.field === 'description'
    case 'details': return f.entity === 'item' && f.field === 'details'
    case 'option': return f.entity === 'group' || f.entity === 'option'
    default: return f.entity === k
  }
}
// one dish: its own texts, its option groups and options
const mine = computed(() => {
  const it = props.item
  if (!it) return null
  const keys = new Set([`item/${it.id}`])
  for (const g of it.groups) { keys.add(`group/${g.id}`); for (const o of g.options) keys.add(`option/${o.id}`) }
  return keys
})
const rows = computed(() => (view.value?.fields ?? []).filter(f =>
  mine.value ? mine.value.has(`${f.entity}/${f.entity_id}`) : ofKind(f, kind.value)))
const count = (k: Kind) => (view.value?.fields ?? []).filter(f => ofKind(f, k)).length
// the option groups' dish, to tell "spice" of one dish from another's
const dishOf = computed(() => {
  const m = new Map<string, string>()
  for (const c of props.cats) for (const it of c.items) for (const g of it.groups) {
    m.set(`group/${g.id}`, it.name)
    for (const o of g.options) m.set(`option/${o.id}`, `${it.name} · ${g.name}`)
  }
  return m
})

// ---- which columns ----
const targets = computed(() => LANGS.filter(l => l.code !== view.value?.menu_lang))
const hidden = ref<Set<string>>(new Set())
const cols = computed(() => targets.value.filter(l => !hidden.value.has(l.code)))
function toggleCol(code: string) {
  const s = new Set(hidden.value)
  if (s.has(code)) s.delete(code); else s.add(code)
  hidden.value = s
}

const byKey = computed(() => {
  const m = new Map<string, Tr>()
  for (const x of view.value?.translations ?? []) m.set(`${x.entity}/${x.entity_id}/${x.field}/${x.lang}`, x)
  return m
})
const cell = (f: Field, lang: string) => byKey.value.get(`${f.entity}/${f.entity_id}/${f.field}/${lang}`)
const off = (lang: string) => view.value?.menu_langs_off.includes(lang) ?? false

const stats = computed(() => {
  const s = { machine: 0, human: 0, stale: 0, missing: 0 }
  for (const f of view.value?.fields ?? []) for (const l of targets.value) {
    if (off(l.code)) continue
    const c = cell(f, l.code)
    if (!c) s.missing++
    else if (c.status === 'stale') s.stale++
    else if (c.status === 'human') s.human++
    else s.machine++
  }
  return s
})

// ---- editing ----
const editing = ref<{ f: Field; lang: string; text: string } | null>(null)
function edit(f: Field, lang: string) {
  if (props.readonly) return
  editing.value = { f, lang, text: cell(f, lang)?.text ?? '' }
}
const body = (f: Field, lang: string, extra: Record<string, unknown>) =>
  ({ entity: f.entity, entity_id: f.entity_id, field: f.field, lang, ...extra })
function save() {
  const e = editing.value!
  editing.value = null
  void send(() => put<View>(`${props.base}/translations`, body(e.f, e.lang, { text: e.text })))
}
// "" hands the text back to the translation service
function revert() {
  const e = editing.value!
  editing.value = null
  void send(() => put<View>(`${props.base}/translations`, body(e.f, e.lang, { text: '' })))
}
const keep = (f: Field, lang: string) => send(() => put<View>(`${props.base}/translations`, body(f, lang, { keep: true })))

function setMenuLang(code: string) {
  if (code === view.value!.menu_lang || !confirm(t('mtr.menu_lang_confirm'))) return
  void send(() => put<View>(`${props.base}/menu-langs`, { menu_lang: code, menu_langs_off: view.value!.menu_langs_off }))
}
function toggleOff(code: string) {
  const v = view.value!
  const next = off(code) ? v.menu_langs_off.filter(x => x !== code) : [...v.menu_langs_off, code]
  void send(() => put<View>(`${props.base}/menu-langs`, { menu_lang: v.menu_lang, menu_langs_off: next }))
}
function retranslate() {
  if (!confirm(t('mtr.retranslate_confirm'))) return
  void send(() => post<View>(`${props.base}/translations/retranslate`))
}
const badge = (c: Tr | undefined) => c ? t('mtr.st_' + c.status) : t('mtr.st_missing')
</script>

<template>
  <div class="dim" @click.self="emit('close')">
    <div class="tr-box tr-sheet" data-testid="tr-dialog">
      <div class="row between">
        <h2>{{ item ? t('mtr.title_item', { name: item.name }) : t('mtr.title') }}</h2>
        <button class="btn small" data-testid="tr-close" @click="emit('close')">✕</button>
      </div>
      <div v-if="error" class="banner bad">{{ error }}</div>
      <template v-if="view">
        <div v-if="!view.provider" class="banner info" data-testid="tr-no-provider">{{ t('mtr.no_provider') }}</div>
        <div v-if="view.error" class="banner bad" data-testid="tr-failed">
          {{ t('mtr.failed', { at: view.error_at ? fmtDateTime(view.error_at) : '' }) }}
          <button v-if="!readonly" class="btn small" :disabled="busy" data-testid="tr-retry" @click="send(() => post(`${base}/translations/retranslate`))">{{ t('mtr.retry') }}</button>
        </div>
        <div v-if="view.pending" class="banner info" data-testid="tr-pending">{{ t('mtr.pending') }}</div>

        <div v-if="!item" class="tr-head">
          <label class="row" style="gap:6px">{{ t('mtr.menu_lang') }}
            <select :value="view.menu_lang" :disabled="readonly || busy" data-testid="tr-menu-lang" @change="setMenuLang(($event.target as HTMLSelectElement).value)">
              <option v-for="l in LANGS" :key="l.code" :value="l.code">{{ l.name }}</option>
            </select></label>
          <span class="small muted">{{ t('mtr.stats', stats) }}</span>
          <button v-if="!readonly && view.provider" class="btn small" :disabled="busy" data-testid="tr-retranslate" @click="retranslate">{{ t('mtr.retranslate') }}</button>
        </div>
        <div v-if="!item" class="small muted">{{ t('mtr.menu_lang_hint') }}</div>

        <div v-if="!item" class="tr-kinds">
          <button v-for="k in kinds" :key="k" class="chip" :class="{ on: kind === k }" :data-testid="'tr-kind-' + k" @click="kind = k">
            {{ t('mtr.k_' + k) }} <span class="muted">{{ count(k) }}</span></button>
        </div>
        <div class="tr-langs">
          <span class="small muted">{{ t('mtr.columns') }}</span>
          <span v-for="l in targets" :key="l.code" class="lang-chip" :class="{ on: !hidden.has(l.code), off: off(l.code) }">
            <button type="button" :data-testid="'tr-col-' + l.code" @click="toggleCol(l.code)">{{ l.name }}</button>
            <button v-if="!readonly && l.code !== 'en'" type="button" class="sw" :title="t(off(l.code) ? 'mtr.lang_on_hint' : 'mtr.lang_off_hint')"
              :data-testid="'tr-off-' + l.code" :disabled="busy" @click="toggleOff(l.code)">{{ off(l.code) ? t('mtr.lang_off') : t('mtr.lang_on') }}</button>
          </span>
        </div>

        <div class="tr-grid-wrap">
          <table class="data tr-grid">
            <thead><tr><th class="orig">{{ t('mtr.original') }}</th><th v-for="l in cols" :key="l.code" :class="{ off: off(l.code) }">{{ l.name }}</th></tr></thead>
            <tbody>
              <tr v-for="f in rows" :key="`${f.entity}/${f.entity_id}/${f.field}`" :data-testid="`tr-row-${f.entity}-${f.entity_id}-${f.field}`">
                <td class="orig"><div>{{ f.text }}</div>
                  <div v-if="dishOf.get(`${f.entity}/${f.entity_id}`)" class="small muted">{{ dishOf.get(`${f.entity}/${f.entity_id}`) }}</div>
                  <div v-if="item" class="small muted">{{ t('mtr.k_' + (f.entity === 'item' ? (f.field === 'name' ? 'item' : f.field) : f.entity === 'group' || f.entity === 'option' ? 'option' : f.entity)) }}</div></td>
                <td v-for="l in cols" :key="l.code" class="tr-cell" :class="[cell(f, l.code)?.status ?? 'missing', { off: off(l.code) }]"
                  :data-testid="`tr-cell-${f.entity}-${f.entity_id}-${f.field}-${l.code}`" :dir="l.code === 'ar' ? 'rtl' : undefined" @click="edit(f, l.code)">
                  <div class="tx">{{ cell(f, l.code)?.text ?? '—' }}</div>
                  <div class="st"><span class="tag" :class="{ ok: cell(f, l.code)?.status === 'human', warn: cell(f, l.code)?.status === 'stale' }">{{ badge(cell(f, l.code)) }}</span>
                    <button v-if="!readonly && cell(f, l.code)?.status === 'stale'" class="btn small" data-testid="tr-keep" @click.stop="keep(f, l.code)">{{ t('mtr.keep') }}</button></div>
                </td>
              </tr>
              <tr v-if="!rows.length"><td :colspan="cols.length + 1" class="muted">{{ t('mtr.empty') }}</td></tr>
            </tbody>
          </table>
        </div>
      </template>

      <div v-if="editing" class="dim" @click.self="editing = null">
        <div class="tr-box tr-edit" data-testid="tr-edit">
          <h3>{{ LANGS.find(l => l.code === editing!.lang)?.name }}</h3>
          <div class="small muted">{{ t('mtr.original') }}：{{ editing.f.text }}</div>
          <textarea v-model="editing.text" rows="3" :dir="editing.lang === 'ar' ? 'rtl' : undefined" data-testid="tr-text"></textarea>
          <div class="row" style="justify-content:flex-end;gap:6px">
            <button class="btn" @click="revert">{{ t('mtr.revert') }}</button>
            <button class="btn primary" :disabled="!editing.text.trim()" data-testid="tr-save" @click="save">{{ t('mtr.save') }}</button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.dim { position: fixed; inset: 0; z-index: 900; background: rgba(0, 0, 0, .4); display: flex; align-items: center; justify-content: center; padding: 12px; }
.tr-box { background: #fff; border-radius: 12px; padding: 16px; display: flex; flex-direction: column; gap: 10px; max-height: 100%; overflow: auto; }
.tr-sheet { width: min(1180px, 100%); }
.tr-edit { width: min(480px, 100%); }
.tr-edit textarea { width: 100%; }
.tr-head { display: flex; flex-wrap: wrap; gap: 12px; align-items: center; }
.tr-kinds, .tr-langs { display: flex; flex-wrap: wrap; gap: 6px; align-items: center; }
.chip { border: 1px solid #ddd; background: #fff; border-radius: 16px; padding: 4px 10px; cursor: pointer; }
.chip.on { border-color: var(--brand, #b8322a); color: var(--brand, #b8322a); }
.lang-chip { display: inline-flex; border: 1px solid #ddd; border-radius: 16px; overflow: hidden; }
.lang-chip button { border: 0; background: #fff; padding: 3px 8px; cursor: pointer; font-size: 12px; }
.lang-chip:not(.on) > button:first-child { color: #aaa; text-decoration: line-through; }
.lang-chip .sw { border-inline-start: 1px solid #ddd; color: #2a7a3b; }
.lang-chip.off .sw { color: #b8322a; }
.tr-grid-wrap { overflow: auto; }
.tr-grid th, .tr-grid td { min-width: 140px; vertical-align: top; }
.tr-grid .orig { position: sticky; inset-inline-start: 0; background: #faf7f2; min-width: 160px; }
.tr-cell { cursor: pointer; }
.tr-cell:hover { background: #fdf6ec; }
.tr-cell .tx { white-space: pre-wrap; overflow-wrap: anywhere; }
.tr-cell.missing .tx, .off { color: #aaa; }
.tr-cell .st { margin-top: 4px; display: flex; gap: 4px; align-items: center; }
</style>
