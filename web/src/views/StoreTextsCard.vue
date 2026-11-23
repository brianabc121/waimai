<script setup lang="ts">
// The store's name, notice and introduction in Simplified Chinese, English and
// Traditional Chinese (docs/I18N_DESIGN.md §7, mock-up ⑥). The system fills in
// the languages the merchant did not write; they show to customers right away
// but stay "to confirm" until the merchant confirms or changes them.
import { computed, onMounted, reactive, ref } from 'vue'
import { get, put } from '../api'
import { t } from '../i18n'
import { LANGS } from '../langs'
import './translateStrings'

const props = defineProps<{ base: string }>()
const emit = defineEmits<{ (e: 'en-name', v: string): void }>()

interface Field { entity: string; entity_id: number; field: 'name' | 'notice' | 'intro'; text: string }
interface Tr { field: string; lang: string; text: string; status: 'machine' | 'converted' | 'human' | 'stale' }
interface View { menu_lang: string; fields: Field[]; translations: Tr[]; pending: number }

const view = ref<View | null>(null)
const draft = reactive<Record<string, string>>({})
const error = ref('')
const busy = ref(false)
const key = (f: string, l: string) => `${f}/${l}`

function take(v: View) {
  view.value = v
  for (const f of v.fields) for (const l of langs.value) draft[key(f.field, l.code)] = cell(f.field, l.code)?.text ?? ''
}
async function load() { try { take(await get<View>(`${props.base}/store-texts`)) } catch (e) { error.value = (e as Error).message } }
onMounted(load)
defineExpose({ load })

const langs = computed(() => LANGS.filter(l => ['zh', 'en', 'zh-Hant'].includes(l.code) && l.code !== view.value?.menu_lang))
const cell = (f: string, l: string) => view.value?.translations.find(x => x.field === f && x.lang === l)
const changed = (f: string, l: string) => (draft[key(f, l)] ?? '').trim() !== (cell(f, l)?.text ?? '')

async function send(f: string, l: string, body: Record<string, unknown>) {
  if (busy.value) return
  busy.value = true; error.value = ''
  try {
    take(await put<View>(`${props.base}/store-texts`, { field: f, lang: l, ...body }))
    if (f === 'name' && l === 'en' && 'text' in body) emit('en-name', String(body.text))
  } catch (e) { error.value = (e as Error).message }
  busy.value = false
}
// saving the text as it is confirms it
function confirmOne(f: string, l: string) {
  const c = cell(f, l)
  if (c?.status === 'stale' && !changed(f, l)) return send(f, l, { keep: true })
  return send(f, l, { text: (draft[key(f, l)] ?? '').trim() })
}
const label = (f: string) => t(f === 'name' ? 'mtr.store_name' : f === 'notice' ? 'mtr.store_notice' : 'mtr.store_intro')
</script>

<template>
  <div class="card stack" data-testid="store-texts">
    <div class="row between wrap">
      <h2>{{ t('mtr.store_title') }}</h2>
      <span v-if="view?.pending" class="tag warn" data-testid="store-texts-pending">{{ t('mtr.store_pending', { n: view.pending }) }}</span>
    </div>
    <p class="small muted">{{ t('mtr.store_hint') }}</p>
    <div v-if="error" class="banner bad">{{ error }}</div>
    <template v-if="view">
      <p v-if="!view.fields.length" class="muted">{{ t('mtr.empty') }}</p>
      <div v-for="f in view.fields" :key="f.field" class="st-field">
        <div class="st-orig"><b>{{ label(f.field) }}</b> <span class="small muted">{{ t('mtr.original') }}</span>
          <div class="pre">{{ f.text }}</div></div>
        <div v-for="l in langs" :key="l.code" class="st-lang" :data-testid="`store-text-${f.field}-${l.code}`">
          <div class="row between"><span>{{ l.name }}</span>
            <span class="tag" :class="{ ok: cell(f.field, l.code)?.status === 'human', warn: cell(f.field, l.code) && cell(f.field, l.code)!.status !== 'human' }">
              {{ cell(f.field, l.code) ? (cell(f.field, l.code)!.status === 'human' ? t('mtr.st_confirmed') : t('mtr.st_to_confirm')) : t('mtr.st_missing') }}</span></div>
          <textarea v-model="draft[key(f.field, l.code)]" :rows="f.field === 'name' ? 1 : 3" :maxlength="f.field === 'intro' ? 500 : 300"></textarea>
          <div class="row" style="justify-content:flex-end">
            <button v-if="changed(f.field, l.code) || (cell(f.field, l.code) && cell(f.field, l.code)!.status !== 'human')" type="button" class="btn small primary"
              :disabled="busy || !(draft[key(f.field, l.code)] ?? '').trim()" data-testid="store-text-confirm" @click="confirmOne(f.field, l.code)">
              {{ changed(f.field, l.code) ? t('mtr.save') : t('mtr.confirm') }}</button>
          </div>
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
.st-field { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 10px; padding: 10px 0; border-top: 1px solid #eee; }
.st-orig .pre { white-space: pre-wrap; overflow-wrap: anywhere; margin-top: 4px; }
.st-lang { display: flex; flex-direction: column; gap: 4px; }
.st-lang textarea { width: 100%; }
</style>
