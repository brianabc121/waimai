<script setup lang="ts">
// Every translation of a store in one table (docs/I18N_SIMPLE_DESIGN.md §4.7).
// Not part of the daily work: it is for finding a sentence a customer quoted.
// The search looks in the original and in every language; "change" opens the
// language window of the record the text belongs to. Shared by the merchant's
// settings page and the platform's item page.
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { get, post } from '../api'
import { t } from '../i18n'
import { LANGS } from '../langs'
import { noteLangStatus, type LangsMeta } from '../langStatus'
import LangsSheet from './LangsSheet.vue'
import './langsStrings'
import './translateStrings'

const props = defineProps<{ base: string; readonly?: boolean }>()
const emit = defineEmits<{ (e: 'close'): void }>()

interface Field { entity: string; entity_id: number; field: string; text: string; owner: string }
interface Tr { entity: string; entity_id: number; field: string; lang: string; text: string; status: string }
interface View extends LangsMeta { fields: Field[]; translations: Tr[] }

const view = ref<View | null>(null)
const error = ref('')
const busy = ref(false)
const q = ref('')
const shown = ref<string[]>([])
const limit = ref(100)
const editing = ref<Field | null>(null)
let timer = 0

function take(v: View) {
  view.value = v
  noteLangStatus(props.base, v)
  if (!shown.value.length) shown.value = [...new Set([v.default_lang, 'en'])].filter(l => l !== v.menu_lang && !v.menu_langs_off.includes(l))
  clearTimeout(timer)
  if (v.pending) timer = window.setTimeout(() => { void load() }, 3000)
}
async function load() {
  try { take(await get<View>(`${props.base}/translations`)) } catch (e) { error.value = (e as Error).message }
}
onMounted(load)
onUnmounted(() => clearTimeout(timer))

// ---- which rows ----
const kinds = ['item', 'description', 'details', 'category', 'option', 'note', 'promotion', 'brand', 'store'] as const
type Kind = typeof kinds[number]
const kind = ref<Kind | ''>('')
const kindOf = (f: Field): Kind => {
  if (f.entity === 'item') return f.field === 'name' ? 'item' : f.field === 'description' ? 'description' : 'details'
  if (['group', 'option', 'topping_template', 'topping_option'].includes(f.entity)) return 'option'
  if (f.entity === 'note_group' || f.entity === 'note_tag') return 'note'
  if (f.entity === 'category') return 'category'
  if (['promotion', 'stored_campaign', 'stored_tier', 'coupon_batch'].includes(f.entity)) return 'promotion'
  if (f.entity.startsWith('brand')) return 'brand'
  return 'store'
}
const byKey = computed(() => {
  const m = new Map<string, Tr>()
  for (const x of view.value?.translations ?? []) m.set(`${x.entity}/${x.entity_id}/${x.field}/${x.lang}`, x)
  return m
})
const id = (f: Field) => `${f.entity}/${f.entity_id}/${f.field}`
const cell = (f: Field, lang: string) => byKey.value.get(`${id(f)}/${lang}`)
// every text of a row, in every language, to search in
const haystack = computed(() => {
  const m = new Map<string, string>()
  for (const f of view.value?.fields ?? []) m.set(id(f), f.text.toLowerCase())
  for (const x of view.value?.translations ?? []) {
    const k = `${x.entity}/${x.entity_id}/${x.field}`
    m.set(k, (m.get(k) ?? '') + '\n' + x.text.toLowerCase())
  }
  return m
})
const rows = computed(() => {
  const needle = q.value.trim().toLowerCase()
  return (view.value?.fields ?? []).filter(f => (!kind.value || kindOf(f) === kind.value) && (!needle || (haystack.value.get(id(f)) ?? '').includes(needle)))
})
watch([q, kind], () => { limit.value = 100 })
// a language that only matches the search is shown too, so the hit is visible
const hitLangs = computed(() => {
  const needle = q.value.trim().toLowerCase()
  if (!needle) return [] as string[]
  const out = new Set<string>()
  for (const f of rows.value.slice(0, limit.value)) for (const l of targets.value) {
    if (cell(f, l.code)?.text.toLowerCase().includes(needle)) out.add(l.code)
  }
  return [...out]
})
const targets = computed(() => LANGS.filter(l => l.code !== view.value?.menu_lang && !(view.value?.menu_langs_off ?? []).includes(l.code)))
const cols = computed(() => targets.value.filter(l => shown.value.includes(l.code) || hitLangs.value.includes(l.code)))
function toggleCol(code: string) {
  shown.value = shown.value.includes(code) ? shown.value.filter(x => x !== code) : [...shown.value, code]
}

// the record a text belongs to, by name: an option shows its item
const ownerName = computed(() => {
  const m = new Map<string, string>()
  for (const f of view.value?.fields ?? []) if (f.field === 'name' && f.owner === `${f.entity}/${f.entity_id}`) m.set(f.owner, f.text)
  return m
})
const where = (f: Field) => {
  const label = t(`lg.f.${f.entity}.${f.field}`)
  const owner = ownerName.value.get(f.owner)
  return owner && owner !== f.text ? `${label} · ${owner}` : label
}
const parts = (text: string) => {
  const needle = q.value.trim()
  const at = needle ? text.toLowerCase().indexOf(needle.toLowerCase()) : -1
  return at < 0 ? [text, '', ''] : [text.slice(0, at), text.slice(at, at + needle.length), text.slice(at + needle.length)]
}

async function retranslate() {
  if (busy.value || !confirm(t('mtr.retranslate_confirm'))) return
  busy.value = true; error.value = ''
  try { await post(`${props.base}/translations/retranslate`); await load() } catch (e) { error.value = (e as Error).message }
  busy.value = false
}
</script>

<template>
  <div class="at-mask" @click.self="emit('close')">
    <div class="at-box" data-testid="all-langs" role="dialog" :aria-label="t('mtr.title')">
      <div class="at-head">
        <div><h2>{{ t('mtr.title') }}</h2><p>{{ t('lg.all.sub') }}</p></div>
        <button type="button" class="at-x" :aria-label="t('close')" data-testid="all-langs-close" @click="emit('close')">✕</button>
      </div>
      <div v-if="error" class="banner bad">{{ error }}</div>
      <template v-if="view">
        <div v-if="!view.provider" class="banner info">{{ t('mtr.no_provider') }}</div>
        <div v-if="view.error" class="banner bad" data-testid="all-langs-error">{{ t('lg.set.failed') }}
          <button v-if="!readonly" type="button" class="btn small" :disabled="busy" @click="retranslate">{{ t('mtr.retry') }}</button></div>
        <div v-else-if="view.pending" class="banner info">{{ t('mtr.pending') }}</div>

        <div class="at-bar">
          <input v-model="q" type="search" class="at-q" :placeholder="t('lg.all.search')" data-testid="all-langs-q" />
          <select v-model="kind" data-testid="all-langs-kind">
            <option value="">{{ t('lg.all.kind_all') }}</option>
            <option v-for="k in kinds" :key="k" :value="k">{{ t('mtr.k_' + k) }}</option>
          </select>
          <span class="muted small" data-testid="all-langs-count">{{ t('lg.all.found', { n: rows.length }) }}</span>
          <button v-if="!readonly && view.provider" type="button" class="btn small" :disabled="busy" data-testid="all-langs-retranslate" @click="retranslate">{{ t('mtr.retranslate') }}</button>
        </div>
        <div class="at-langs">
          <span class="small muted">{{ t('mtr.columns') }}</span>
          <button v-for="l in targets" :key="l.code" type="button" :class="{ on: cols.some(c => c.code === l.code) }" :data-testid="'all-langs-col-' + l.code" @click="toggleCol(l.code)">{{ l.name }}</button>
        </div>

        <div class="at-wrap">
          <table class="data at-grid">
            <thead><tr><th>{{ t('lg.all.where') }}</th><th>{{ t('mtr.original') }}</th><th v-for="l in cols" :key="l.code">{{ l.name }}</th><th></th></tr></thead>
            <tbody>
              <tr v-for="f in rows.slice(0, limit)" :key="id(f)" :data-testid="`all-langs-row-${f.entity}-${f.entity_id}-${f.field}`">
                <td class="small muted">{{ where(f) }}</td>
                <td class="tx"><template v-for="(p, i) in parts(f.text)" :key="i"><mark v-if="i === 1 && p">{{ p }}</mark><template v-else>{{ p }}</template></template></td>
                <td v-for="l in cols" :key="l.code" class="tx" :dir="l.code === 'ar' ? 'rtl' : undefined" :data-testid="`all-langs-cell-${f.entity}-${f.entity_id}-${f.field}-${l.code}`">
                  <template v-if="cell(f, l.code)">
                    <template v-for="(p, i) in parts(cell(f, l.code)!.text)" :key="i"><mark v-if="i === 1 && p">{{ p }}</mark><template v-else>{{ p }}</template></template>
                    <small>{{ cell(f, l.code)!.status === 'human' ? t('lg.mine') : t('lg.auto') }}</small>
                  </template>
                  <span v-else class="muted">—</span>
                </td>
                <td class="right"><button type="button" class="btn small primary" data-testid="all-langs-edit" @click="editing = f">{{ t('lg.all.edit') }}</button></td>
              </tr>
              <tr v-if="!rows.length"><td :colspan="cols.length + 3" class="muted">{{ t('mtr.empty') }}</td></tr>
            </tbody>
          </table>
          <button v-if="rows.length > limit" type="button" class="btn small" style="margin-top:10px" data-testid="all-langs-more" @click="limit += 200">{{ t('lg.all.more') }}</button>
        </div>
        <div class="at-hint">{{ t('lg.all.hint') }}</div>
      </template>
    </div>
    <LangsSheet v-if="editing" :base="base" :scope="editing.owner" :name="ownerName.get(editing.owner) ?? editing.text" :readonly="readonly"
      @close="editing = null; load()" />
  </div>
</template>

<style scoped>
.at-mask { position: fixed; inset: 0; z-index: 900; background: rgba(30, 20, 10, .42); display: flex; align-items: center; justify-content: center; padding: 12px; }
.at-box { background: #fffdf9; color: #2b211c; border: 1px solid #eadfce; border-radius: 14px; padding: 16px; width: min(1180px, 100%); max-height: 100%; overflow: auto;
  display: flex; flex-direction: column; gap: 10px; text-align: start; font-size: 14px; }
.at-head { display: flex; justify-content: space-between; align-items: flex-start; gap: 12px; }
.at-head h2 { margin: 0; font-size: 20px; }
.at-head p { margin: 3px 0 0; color: #8a7b6f; font-size: 13px; }
.at-x { border: 0; background: none; font-size: 16px; color: #8a7b6f; padding: 4px 6px; cursor: pointer; }
.at-bar { display: flex; flex-wrap: wrap; gap: 10px; align-items: center; }
.at-bar .at-q { flex: 1 1 220px; min-width: 0; width: auto; }
.at-bar select { flex: 0 0 auto; width: auto; max-width: 100%; }
.at-langs { display: flex; flex-wrap: wrap; gap: 6px; align-items: center; }
.at-langs button { border: 1px solid #e3d6c3; background: #fff; border-radius: 16px; padding: 3px 10px; font: inherit; font-size: 12px; color: #a59a90; cursor: pointer; }
.at-langs button.on { border-color: #a02c20; color: #a02c20; }
.at-wrap { overflow: auto; }
.at-grid th, .at-grid td { vertical-align: top; min-width: 120px; }
.at-grid .tx { white-space: pre-wrap; overflow-wrap: anywhere; }
.at-grid small { display: block; color: #8a7b6f; font-size: 12px; }
.at-grid mark { background: #ffe9a8; padding: 1px 2px; border-radius: 3px; }
.at-hint { background: #f6f1ea; border-radius: 8px; padding: 9px 12px; color: #6b5a4e; font-size: 13px; }
.at-box .btn.primary { background: #a02c20; border-color: #a02c20; color: #fff; }
@media (max-width: 640px) { .at-mask { padding: 0; } .at-box { border-radius: 0; border: 0; height: 100%; } }
</style>
