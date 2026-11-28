<script setup lang="ts">
// One record in the other languages (docs/I18N_SIMPLE_DESIGN.md §4.3): what the
// merchant wrote on the left, one language's translation on the right, to be
// changed in place. Opened by LangsButton; the same window everywhere.
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { get, post, put } from '../api'
import { t } from '../i18n'
import { LANGS } from '../langs'
import { noteLangStatus, type LangsMeta } from '../langStatus'
import './langsStrings'
import './translateStrings'

const props = defineProps<{ base: string; scope: string; name: string; readonly?: boolean }>()
const emit = defineEmits<{ (e: 'close'): void }>()

interface Field { entity: string; entity_id: number; field: string; text: string }
interface Tr { entity: string; entity_id: number; field: string; lang: string; text: string; status: 'machine' | 'converted' | 'human' | 'stale'; prev?: string }
interface View extends LangsMeta { fields: Field[]; translations: Tr[] }

const view = ref<View | null>(null)
const error = ref('')
const busy = ref(false)
const saved = ref(false)
const lang = ref('')
// what the merchant typed and has not saved: key -> text
const dirty = reactive<Record<string, string>>({})
let timer = 0

const url = computed(() => `${props.base}/translations?scope=${encodeURIComponent(props.scope)}`)
function take(v: View) {
  view.value = v
  noteLangStatus(props.base, v)
  if (!lang.value || !langs.value.some(l => l.code === lang.value)) {
    lang.value = langs.value.some(l => l.code === v.default_lang) ? v.default_lang : langs.value[0]?.code ?? 'en'
  }
  clearTimeout(timer)
  // a run is waiting or going: look again shortly (what is being typed stays)
  if (v.pending) timer = window.setTimeout(() => { void load() }, 3000)
}
async function load() {
  try { take(await get<View>(url.value)) } catch (e) { error.value = (e as Error).message }
}
onMounted(load)
onUnmounted(() => clearTimeout(timer))

// the languages customers get, without the one the merchant writes in
const langs = computed(() => LANGS.filter(l => l.code !== view.value?.menu_lang && !(view.value?.menu_langs_off ?? []).includes(l.code)))
const langName = (code: string) => LANGS.find(l => l.code === code)?.name ?? code
const key = (f: Field, l: string) => `${f.entity}/${f.entity_id}/${f.field}/${l}`
const byKey = computed(() => {
  const m = new Map<string, Tr>()
  for (const x of view.value?.translations ?? []) m.set(`${x.entity}/${x.entity_id}/${x.field}/${x.lang}`, x)
  return m
})
const cell = (f: Field) => byKey.value.get(key(f, lang.value))
const value = (f: Field) => dirty[key(f, lang.value)] ?? cell(f)?.text ?? ''
function type(f: Field, text: string) {
  saved.value = false
  const k = key(f, lang.value)
  if (text === (cell(f)?.text ?? '')) delete dirty[k]
  else dirty[k] = text
}
const changed = computed(() => Object.keys(dirty).length)
const mine = (f: Field) => key(f, lang.value) in dirty || cell(f)?.status === 'human'
// the merchant's own translation of the earlier wording, offered back
const prev = (f: Field) => { const c = cell(f); return c && c.status !== 'human' && c.prev && !(key(f, lang.value) in dirty) ? c.prev : '' }
// a name goes into three languages by itself; the others read English until written
const nameOnly = (f: Field) => (f.entity === 'branch' || f.entity === 'brand') && f.field === 'name' && !['zh', 'en', 'zh-Hant'].includes(lang.value)
const label = (f: Field) => t(`lg.f.${f.entity}.${f.field}`)
const long = (f: Field) => f.field !== 'name' && f.field !== 'label' && f.field !== 'title'

async function send(fn: () => Promise<View>) {
  if (busy.value) return false
  busy.value = true; error.value = ''
  try { take(await fn()); return true } catch (e) { error.value = (e as Error).message; return false } finally { busy.value = false }
}
async function save() {
  const edits = Object.entries(dirty).filter(([, text]) => text.trim()).map(([k, text]) => {
    const [entity, id, field, l] = k.split('/')
    return { entity, entity_id: Number(id), field, lang: l, text }
  })
  if (await send(() => put<View>(url.value, { edits }))) {
    for (const k of Object.keys(dirty)) delete dirty[k]
    saved.value = true
  }
}
// "" hands the text back to the translation service
function revert(f: Field) {
  delete dirty[key(f, lang.value)]
  if (cell(f)?.status !== 'human') return
  void send(() => put<View>(url.value, { edits: [{ entity: f.entity, entity_id: f.entity_id, field: f.field, lang: lang.value, text: '' }] }))
}
const retry = () => send(async () => { await post(`${props.base}/translations/retranslate`); return get<View>(url.value) })
function close() {
  if (changed.value && !confirm(t('lg.leave'))) return
  emit('close')
}
</script>

<template>
  <div class="lg-mask" @click.self="close">
    <div class="lg-sheet" data-testid="langs-sheet" role="dialog" :aria-label="t('lg.title', { name })">
      <div class="lg-head">
        <div>
          <h2><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true"><circle cx="12" cy="12" r="9" /><path d="M3 12h18M12 3c2.8 3 2.8 15 0 18M12 3c-2.8 3-2.8 15 0 18" /></svg>{{ t('lg.title', { name }) }}</h2>
          <p>{{ t('lg.sub') }}</p>
        </div>
        <button type="button" class="lg-x" :aria-label="t('close')" data-testid="langs-close" @click="close">✕</button>
      </div>
      <div class="lg-body">
        <div v-if="error" class="banner bad">{{ error }}</div>
        <template v-if="view">
          <div v-if="!view.provider" class="banner info" data-testid="langs-no-provider">{{ t('mtr.no_provider') }}</div>
          <div v-if="view.error" class="banner bad" data-testid="langs-error">{{ t('lg.set.failed') }}
            <button v-if="!readonly" type="button" class="btn small" :disabled="busy" data-testid="langs-retry" @click="retry">{{ t('mtr.retry') }}</button></div>
          <div v-else-if="view.pending" class="banner info" data-testid="langs-pending">{{ t('mtr.pending') }}</div>

          <div class="lg-tabs" role="tablist">
            <button v-for="l in langs" :key="l.code" type="button" role="tab" :aria-selected="lang === l.code" :class="{ on: lang === l.code }"
              :data-testid="'langs-tab-' + l.code" @click="lang = l.code">{{ l.name }}</button>
          </div>
          <div class="lg-cols"><span></span><span>{{ t('lg.col_src', { lang: langName(view.menu_lang) }) }}</span><span>{{ t('lg.col_tr', { lang: langName(lang) }) }}</span><span></span></div>
          <p v-if="!view.fields.length" class="muted">{{ t('mtr.empty') }}</p>
          <div v-for="f in view.fields" :key="`${f.entity}/${f.entity_id}/${f.field}`" class="lg-row" :data-testid="`langs-row-${f.entity}-${f.entity_id}-${f.field}`">
            <div class="lg-label">{{ label(f) }}</div>
            <div class="lg-src">{{ f.text }}</div>
            <div class="lg-tr">
              <textarea v-if="long(f)" :value="value(f)" rows="2" :readonly="readonly" :class="{ mine: mine(f) }" :dir="lang === 'ar' ? 'rtl' : undefined"
                data-testid="langs-text" @input="type(f, ($event.target as HTMLTextAreaElement).value)"></textarea>
              <input v-else :value="value(f)" :readonly="readonly" :class="{ mine: mine(f) }" :dir="lang === 'ar' ? 'rtl' : undefined"
                :placeholder="nameOnly(f) ? t('lg.name_hint') : ''" data-testid="langs-text" @input="type(f, ($event.target as HTMLInputElement).value)" />
              <div v-if="prev(f)" class="lg-prev" data-testid="langs-prev">{{ t('lg.prev', { text: prev(f) }) }}
                <button v-if="!readonly" type="button" data-testid="langs-use-prev" @click="type(f, prev(f))">{{ t('lg.use_prev') }}</button></div>
            </div>
            <div class="lg-state">
              <template v-if="mine(f)"><b data-testid="langs-mine">{{ t('lg.mine') }}</b>
                <button v-if="!readonly" type="button" data-testid="langs-revert" @click="revert(f)">{{ t('lg.revert') }}</button></template>
              <span v-else-if="cell(f)">{{ t('lg.auto') }}</span>
              <span v-else-if="!nameOnly(f)">{{ t('lg.none') }}</span>
            </div>
          </div>
        </template>
      </div>
      <div class="lg-foot">
        <span class="lg-note">{{ saved ? t('saved') : t('lg.foot') }}</span>
        <button type="button" class="btn" @click="close">{{ t('close') }}</button>
        <button v-if="!readonly" type="button" class="btn primary" :disabled="busy || !changed" data-testid="langs-save" @click="save">{{ t('lg.save') }}</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.lg-mask { position: fixed; inset: 0; z-index: 950; background: rgba(30, 20, 10, .42); display: flex; align-items: center; justify-content: center; padding: 16px; }
.lg-sheet { background: #fffdf9; color: #2b211c; border: 1px solid #eadfce; border-radius: 14px; width: min(920px, 100%); max-height: 100%; display: flex; flex-direction: column;
  box-shadow: 0 16px 40px rgba(60, 30, 10, .25); text-align: start; font-size: 14px; }
.lg-head { display: flex; justify-content: space-between; align-items: flex-start; gap: 12px; padding: 16px 16px 10px; }
.lg-head h2 { margin: 0; font-size: 18px; display: flex; align-items: center; gap: 7px; overflow-wrap: anywhere; }
.lg-head h2 svg { width: 19px; height: 19px; flex: none; color: #a02c20; }
.lg-head p { margin: 3px 0 0; color: #8a7b6f; font-size: 13px; }
.lg-x { border: 0; background: none; font-size: 16px; color: #8a7b6f; padding: 4px 6px; cursor: pointer; }
.lg-body { padding: 0 16px; overflow: auto; }
.lg-body .banner { margin-bottom: 10px; }
.lg-tabs { display: flex; flex-wrap: wrap; gap: 6px; margin-bottom: 12px; }
.lg-tabs button { border: 1px solid #e3d6c3; background: #fff; border-radius: 16px; padding: 4px 12px; font: inherit; font-size: 13px; color: #6b5a4e; cursor: pointer; white-space: nowrap; }
.lg-tabs button.on { background: #a02c20; border-color: #a02c20; color: #fff; }
.lg-cols, .lg-row { display: grid; grid-template-columns: 76px minmax(0, 1fr) minmax(0, 1.25fr) 124px; gap: 10px; align-items: center; }
.lg-cols { font-size: 12px; color: #8a7b6f; padding-bottom: 6px; border-bottom: 1px solid #eadfce; }
.lg-row { padding: 8px 0; border-bottom: 1px solid #f1e8da; }
.lg-label, .lg-state { font-size: 12px; color: #8a7b6f; }
.lg-src { white-space: pre-wrap; overflow-wrap: anywhere; color: #4a3d35; }
.lg-tr input, .lg-tr textarea { width: 100%; border: 1px solid #e3d6c3; border-radius: 8px; background: #fff; padding: 8px 11px; font: inherit; color: inherit; }
.lg-tr textarea { resize: vertical; min-height: 58px; }
.lg-tr .mine { border-color: #a02c20; }
.lg-prev { margin-top: 4px; font-size: 12px; color: #8a7b6f; overflow-wrap: anywhere; }
.lg-prev button, .lg-state button { border: 0; background: none; padding: 0; font: inherit; font-size: 12px; color: #6b5a4e; text-decoration: underline; cursor: pointer; }
.lg-state { display: flex; flex-direction: column; align-items: flex-start; gap: 2px; }
.lg-state b { color: #a02c20; }
.lg-foot { display: flex; align-items: center; gap: 8px; padding: 12px 16px; }
.lg-note { margin-inline-end: auto; color: #8a7b6f; font-size: 12px; }
.lg-foot .btn.primary { background: #a02c20; border-color: #a02c20; color: #fff; }
@media (max-width: 640px) {
  .lg-mask { padding: 0; align-items: stretch; }
  .lg-sheet { border-radius: 0; border: 0; }
  .lg-tabs { flex-wrap: nowrap; overflow-x: auto; padding-bottom: 4px; }
  .lg-cols { display: none; }
  .lg-row { grid-template-columns: minmax(0, 1fr); gap: 4px; padding: 10px 0; }
  .lg-src { background: #f6f1ea; border-radius: 8px; padding: 7px 10px; }
  .lg-state { flex-direction: row; gap: 10px; }
  .lg-note { display: none; }
  .lg-foot { padding-bottom: calc(12px + env(safe-area-inset-bottom)); }
  .lg-foot .btn { flex: 1; }
}
</style>
