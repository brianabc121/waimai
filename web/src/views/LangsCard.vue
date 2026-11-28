<script setup lang="ts">
// The store's language settings (docs/I18N_SIMPLE_DESIGN.md §4.6): the language
// the merchant writes in, the languages customers get, whether translating
// works, and the way to the table of all translations.
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { get, post, put } from '../api'
import { t } from '../i18n'
import { LANGS } from '../langs'
import { fmtDateTime } from './admin/util'
import { noteLangStatus, type LangsMeta } from '../langStatus'
import AllTranslations from './AllTranslations.vue'
import './langsStrings'
import './translateStrings'

const props = defineProps<{ base: string }>()
const meta = ref<LangsMeta | null>(null)
const error = ref('')
const busy = ref(false)
const all = ref(false)
let timer = 0

function take(m: LangsMeta) {
  meta.value = m
  noteLangStatus(props.base, m)
  clearTimeout(timer)
  if (m.pending) timer = window.setTimeout(() => { void load() }, 3000)
}
async function load() {
  try { take(await get<LangsMeta>(`${props.base}/translations?scope=none`)) } catch (e) { error.value = (e as Error).message }
}
onMounted(load)
onUnmounted(() => clearTimeout(timer))
defineExpose({ load })

async function send(fn: () => Promise<unknown>) {
  if (busy.value) return
  busy.value = true; error.value = ''
  try { await fn(); await load() } catch (e) { error.value = (e as Error).message }
  busy.value = false
}
const targets = computed(() => LANGS.filter(l => l.code !== meta.value?.menu_lang))
const off = (code: string) => meta.value?.menu_langs_off.includes(code) ?? false
function setLang(code: string) {
  const m = meta.value!
  if (code === m.menu_lang || !confirm(t('mtr.menu_lang_confirm'))) { void load(); return }
  void send(() => put(`${props.base}/menu-langs`, { menu_lang: code, menu_langs_off: m.menu_langs_off }))
}
function toggle(code: string) {
  const m = meta.value!
  const next = off(code) ? m.menu_langs_off.filter(x => x !== code) : [...m.menu_langs_off, code]
  void send(() => put(`${props.base}/menu-langs`, { menu_lang: m.menu_lang, menu_langs_off: next }))
}
const retry = () => send(() => post(`${props.base}/translations/retranslate`))
</script>

<template>
  <section class="ox-panel stack" data-testid="langs-card">
    <div class="ox-panel-h" style="margin:0"><h2>{{ t('lg.set.title') }}</h2></div>
    <p class="muted small">{{ t('lg.set.hint') }}</p>
    <div v-if="error" class="banner bad">{{ error }}</div>
    <template v-if="meta">
      <div v-if="!meta.provider" class="banner info small">{{ t('mtr.no_provider') }}</div>
      <div class="grid2">
        <label class="field"><span>{{ t('lg.set.lang') }}</span>
          <select :value="meta.menu_lang" :disabled="busy" data-testid="langs-menu-lang" @change="setLang(($event.target as HTMLSelectElement).value)">
            <option v-for="l in LANGS" :key="l.code" :value="l.code">{{ l.name }}</option></select></label>
        <div class="field"><span>{{ t('lg.set.status') }}</span>
          <div class="lc-status" :class="{ bad: !!meta.error }" data-testid="langs-status">
            <template v-if="meta.error"><i class="lc-dot"></i>{{ t('lg.set.failed') }}
              <button type="button" class="btn small" :disabled="busy" data-testid="langs-card-retry" @click="retry">{{ t('mtr.retry') }}</button></template>
            <template v-else-if="meta.pending"><i class="lc-dot wait"></i>{{ t('lg.set.pending') }}</template>
            <template v-else-if="meta.updated_at"><i class="lc-dot"></i>{{ t('lg.set.ok', { at: fmtDateTime(meta.updated_at) }) }}</template>
            <template v-else>{{ t('lg.set.never') }}</template>
          </div></div>
      </div>
      <div class="field" style="margin:0"><span>{{ t('lg.set.langs') }}</span>
        <div class="lc-chips">
          <button v-for="l in targets" :key="l.code" type="button" :class="{ off: off(l.code) }" :disabled="busy || l.code === 'en'" :aria-pressed="!off(l.code)"
            :title="l.code === 'en' ? '' : t(off(l.code) ? 'mtr.lang_on_hint' : 'mtr.lang_off_hint')" :data-testid="'langs-toggle-' + l.code" @click="toggle(l.code)">{{ l.name }}</button>
        </div></div>
      <div class="lc-all"><button type="button" class="btn link" data-testid="langs-all" @click="all = true">{{ t('lg.set.all') }} ›</button>
        <span class="muted small">{{ t('lg.set.all_hint') }}</span></div>
    </template>
    <Teleport v-if="all" to="body"><AllTranslations :base="base" @close="all = false; load()" /></Teleport>
  </section>
</template>

<style scoped>
.lc-status { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; min-height: 38px; }
.lc-status.bad { color: #b3261e; }
.lc-dot { width: 8px; height: 8px; border-radius: 50%; background: #2f8f3e; flex: none; }
.lc-status.bad .lc-dot { background: #d0342c; }
.lc-dot.wait { background: #d9a441; }
.lc-chips { display: flex; flex-wrap: wrap; gap: 6px; }
.lc-chips button { border: 1px solid #b9d6b8; background: #eef7ed; color: #24642f; border-radius: 16px; padding: 3px 11px; font: inherit; font-size: 13px; cursor: pointer; }
.lc-chips button.off { border-color: #e3d6c3; background: #f6f1ea; color: #a59a90; text-decoration: line-through; }
.lc-chips button:disabled { cursor: default; }
.lc-all { display: flex; flex-wrap: wrap; align-items: baseline; gap: 4px 12px; }
.lc-all .btn.link { color: var(--brand, #a02c20); font-weight: 600; padding: 0; }
</style>
