<script setup lang="ts">
// The platform's language page (docs/I18N_DESIGN.md §8): every interface text
// in Chinese, English and the language being checked; corrections take effect
// at once, without a new release.
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ApiError, get, post, put } from '../../api'
import { fixTexts, t, textsOf } from '../../i18n'
import { LANGS, type Locale } from '../../langs'
import { fmtDateTime } from './util'
import './i18nStrings'

interface Src { zh: string; en: string; file: string }
interface Fix { key: string; text: string; status: 'human' | 'machine'; updated_at: string; updated_by: string }
type Status = 'human' | 'machine' | 'shipped' | 'missing'
interface Row { key: string; zh: string; en: string; text: string; shipped: string; status: Status; long: boolean; fix?: Fix }

const lang = ref<Locale>((localStorage.getItem('i18n-lang') as Locale) || 'th')
const source = ref<Record<string, Src>>({})
const sys = ref<Record<string, Record<string, string>>>({})
const shipped = ref<Record<string, string>>({})
const fixes = ref<Record<string, Fix>>({})
const error = ref('')
const note = ref('')
const busy = ref(false)
const filter = ref<'all' | 'machine' | 'missing' | 'long' | 'human'>('all')
const group = ref('')
const q = ref('')
const editing = ref('')
const draft = ref('')
const page = ref(1)
const PER = 100

async function load() {
  error.value = ''; note.value = ''; editing.value = ''
  try {
    const [src, ship, fx] = await Promise.all([
      Object.keys(source.value).length ? Promise.resolve(source.value) : import('virtual:i18n-source').then(m => {
        // the server's own texts (SMS, push reminders) are listed too, under "sys"
        sys.value = m.sys
        return { ...m.default, ...Object.fromEntries(Object.entries(m.sys).map(([k, v]) => [k, { zh: v.zh, en: v.en, file: 'server' }])) }
      }),
      textsOf(lang.value),
      get<{ fixes: Fix[] }>(`/admin/i18n/${lang.value}`),
    ])
    source.value = src
    shipped.value = { ...ship, ...Object.fromEntries(Object.entries(sys.value).filter(([, v]) => v[lang.value]).map(([k, v]) => [k, v[lang.value]])) }
    fixes.value = Object.fromEntries(fx.fixes.map(f => [f.key, f]))
  } catch (e) { error.value = (e as Error).message }
}
onMounted(load)
watch(lang, l => { try { localStorage.setItem('i18n-lang', l) } catch { /* private mode */ } page.value = 1; void load() })
watch([filter, group, q], () => { page.value = 1 })

// A label or button whose text is much wider on screen than the English may
// not fit where the English does (docs/I18N_DESIGN.md §15). Widths are
// measured in the site's font, not counted in letters: a Burmese or Thai
// letter is narrower than a Latin one, a Chinese one wider. Short texts
// (buttons, tabs, labels) get a budget against the English; sentences wrap,
// so they are left alone.
const canvas = document.createElement('canvas').getContext('2d')
const fontVer = ref(0)
function width(s: string): number {
  void fontVer.value
  if (!canvas) return [...s].length * 8
  canvas.font = `15px ${getComputedStyle(document.documentElement).fontFamily || 'sans-serif'}`
  return canvas.measureText(s).width
}
// budget is how much wider than the English a text may be: generous for a
// word or two, tighter for a short phrase
function budget(en: string): number | null {
  const n = [...en].length
  return n <= 12 ? 2 : n <= 30 ? 1.6 : n <= 50 ? 1.45 : null
}
// over is how much a text runs past its budget, in per cent of the English
// width (0 when it fits)
function over(text: string, en: string): number {
  const b = budget(en)
  if (!b || !text || !en) return 0
  const we = width(plain(en)), wt = width(plain(text))
  // a few letters more is never the problem: at least about six letters wider
  return we > 0 && wt > we * b && wt - we > 48 ? Math.round((wt / we - 1) * 100) : 0
}
// plain is a text as it shows: the "other" form of a plural, a short number
// for each {placeholder}
const plain = (s: string) => s
  .replace(/\{(\w+), plural,((?:\s*[=\w]+\s*\{[^{}]*\})+)\s*\}/g, (_, _n: string, body: string) => (/other\s*\{([^{}]*)\}/.exec(body)?.[1] ?? '').replace(/#/g, '8'))
  .replace(/\{\w+\}/g, '88')
const tooLong = (text: string, en: string) => over(text, en) > 0
watch(lang, l => { if (l === 'my' || l === 'km') import('../../fonts').then(() => document.fonts.ready).then(() => fontVer.value++) }, { immediate: true })
// while typing: how much wider than the English the draft is
const draftOver = computed(() => {
  const r = rows.value.find(x => x.key === editing.value)
  return r ? over(draft.value, r.en) : 0
})

const rows = computed<Row[]>(() => Object.entries(source.value).map(([key, s]) => {
  const ship = lang.value === 'zh' ? s.zh : lang.value === 'en' ? s.en : shipped.value[key] ?? ''
  const fix = fixes.value[key]
  const text = fix?.text ?? ship
  const has = lang.value === 'zh' || lang.value === 'en' || key in shipped.value
  const status: Status = fix ? fix.status : has ? 'shipped' : 'missing'
  return { key, zh: s.zh, en: s.en, text, shipped: ship, status, long: !!text && tooLong(text, s.en), fix }
}))
const prefix = (k: string) => k.includes('.') ? k.slice(0, k.indexOf('.')) : '-'
const groups = computed(() => {
  const m = new Map<string, number>()
  for (const r of rows.value) m.set(prefix(r.key), (m.get(prefix(r.key)) ?? 0) + 1)
  return [...m].sort((a, b) => b[1] - a[1])
})
const stats = computed(() => ({
  total: rows.value.length,
  human: rows.value.filter(r => r.status === 'human').length,
  machine: rows.value.filter(r => r.status === 'machine').length,
  missing: rows.value.filter(r => r.status === 'missing').length,
  long: rows.value.filter(r => r.long).length,
}))
const shown = computed(() => {
  const s = q.value.trim().toLowerCase()
  return rows.value.filter(r =>
    (!group.value || prefix(r.key) === group.value) &&
    (filter.value === 'all' || (filter.value === 'long' ? r.long : r.status === filter.value)) &&
    (!s || [r.key, r.zh, r.en, r.text].some(x => x.toLowerCase().includes(s))))
})
const pages = computed(() => Math.max(1, Math.ceil(shown.value.length / PER)))
const pageRows = computed(() => shown.value.slice((page.value - 1) * PER, page.value * PER))

function edit(r: Row) { editing.value = r.key; draft.value = r.text }

// corrections show in this browser at once; other visitors get them within a minute
function applyHere() {
  fixTexts(lang.value, Object.fromEntries(Object.values(fixes.value).map(f => [f.key, f.text])))
  reloadPreview()
}

async function save(edits: { key: string; text: string }[], done: string) {
  busy.value = true; error.value = ''; note.value = ''
  const to = lang.value // the language this was started for, whatever is picked meanwhile
  try {
    await put(`/admin/i18n/${to}`, { edits })
    const fx = await get<{ fixes: Fix[] }>(`/admin/i18n/${to}`)
    if (lang.value === to) {
      fixes.value = Object.fromEntries(fx.fixes.map(f => [f.key, f]))
      editing.value = ''
      note.value = done
      applyHere()
    }
  } catch (e) { error.value = (e as Error).message }
  busy.value = false
}
const saveOne = (r: Row) => save([{ key: r.key, text: draft.value.trim() === r.shipped && r.status !== 'machine' ? '' : draft.value }], t('ad.i18n.saved'))
// back to the text that ships with the app
const revert = (r: Row) => save([{ key: r.key, text: '' }], t('ad.i18n.reverted'))
// a machine text checked as it is
const approve = (r: Row) => save([{ key: r.key, text: r.text }], t('ad.i18n.saved'))

async function fill() {
  const missing = rows.value.filter(r => r.status === 'missing')
  if (!missing.length) { note.value = t('ad.i18n.none_missing'); return }
  if (!confirm(t('ad.i18n.fill_confirm', { n: missing.length }))) return
  busy.value = true; error.value = ''; note.value = ''
  // every batch goes to the language the filling was started for, even if
  // another one is picked while it runs
  const to = lang.value
  try {
    let n = 0
    for (let i = 0; i < missing.length; i += 200) {
      const r = await post<{ texts: Record<string, string> }>(`/admin/i18n/${to}/fill`,
        { texts: missing.slice(i, i + 200).map(m => ({ key: m.key, en: m.en, zh: m.zh })) })
      n += Object.keys(r.texts).length
    }
    if (lang.value === to) await load()
    note.value = t('ad.i18n.filled', { n })
    applyHere()
  } catch (e) { error.value = (e as Error).message }
  busy.value = false
}

async function exportSheet() {
  busy.value = true; error.value = ''
  try {
    const res = await fetch(`/api/v1/admin/i18n/${lang.value}/export`, {
      method: 'POST', credentials: 'same-origin',
      headers: { 'X-Requested-With': 'waimai-web', 'Content-Type': 'application/json' },
      body: JSON.stringify({ rows: shown.value.map(r => ({ key: r.key, zh: r.zh, en: r.en, text: r.text })) }),
    })
    if (!res.ok) throw new ApiError(res.status, 'EXPORT', (await res.json().catch(() => null))?.error?.message ?? res.statusText)
    const a = document.createElement('a')
    a.href = URL.createObjectURL(await res.blob())
    a.download = `texts-${lang.value}.xlsx`
    a.click()
    setTimeout(() => URL.revokeObjectURL(a.href), 10000)
  } catch (e) { error.value = (e as Error).message }
  busy.value = false
}

const fileBox = ref<HTMLInputElement | null>(null)
const pending = ref<{ key: string; text: string; before: string }[] | null>(null)
async function importSheet(ev: Event) {
  const f = (ev.target as HTMLInputElement).files?.[0]
  ;(ev.target as HTMLInputElement).value = ''
  if (!f) return
  busy.value = true; error.value = ''; note.value = ''
  try {
    const res = await fetch(`/api/v1/admin/i18n/${lang.value}/import`, {
      method: 'POST', credentials: 'same-origin',
      headers: { 'X-Requested-With': 'waimai-web', 'Content-Type': 'application/octet-stream' }, body: f,
    })
    const d = await res.json().catch(() => null)
    if (!res.ok) throw new ApiError(res.status, d?.error?.code ?? 'IMPORT', d?.error?.message ?? res.statusText)
    const byKey = Object.fromEntries(rows.value.map(r => [r.key, r]))
    pending.value = (d.edits as { key: string; text: string }[])
      .filter(e => byKey[e.key] && e.text !== byKey[e.key].text)
      .map(e => ({ ...e, before: byKey[e.key].text }))
    if (!pending.value.length) { pending.value = null; note.value = t('ad.i18n.import_same') }
  } catch (e) { error.value = (e as Error).message }
  busy.value = false
}
async function applyImport() {
  if (!pending.value) return
  const edits = pending.value.map(p => ({ key: p.key, text: p.text }))
  pending.value = null
  await save(edits, t('ad.i18n.imported', { n: edits.length }))
}

// ---- preview ----
const pages_ = [
  { path: '/', label: 'ad.i18n.p_mall' }, { path: '/orders', label: 'ad.i18n.p_orders' }, { path: '/me', label: 'ad.i18n.p_me' },
] as const
const preview = reactive({ path: '/' as string, store: '', on: true, n: 0, pseudo: false })
// "40% longer" shows the page with every text lengthened (pseudo-localisation),
// to see where a long translation would break it
const previewUrl = computed(() => {
  const p = preview.store.trim() ? `/s/${encodeURIComponent(preview.store.trim())}` : preview.path
  return `${p}?lang=${preview.pseudo ? 'en' : lang.value}&i18nfresh=${preview.n}&pseudo=${preview.pseudo ? 1 : 0}`
})
function reloadPreview() { preview.n++ }

const statusText = (s: Status) => t('ad.i18n.st_' + s)
</script>

<template>
  <div class="adm-page stack" data-testid="admin-i18n">
    <div class="card row wrap" style="gap:10px;align-items:center">
      <label class="row" style="gap:6px">{{ t('ad.i18n.lang') }}
        <select v-model="lang" :disabled="busy" data-testid="i18n-lang"><option v-for="l in LANGS" :key="l.code" :value="l.code">{{ l.name }} · {{ l.en }}</option></select></label>
      <span class="grow"></span>
      <button class="btn" :disabled="busy" data-testid="i18n-fill" @click="fill">{{ t('ad.i18n.fill') }}</button>
      <button class="btn" :disabled="busy" data-testid="i18n-export" @click="exportSheet">{{ t('ad.i18n.export') }}</button>
      <button class="btn" :disabled="busy" @click="fileBox?.click()">{{ t('ad.i18n.import') }}</button>
      <input ref="fileBox" type="file" accept=".xlsx" hidden data-testid="i18n-import" @change="importSheet" />
    </div>
    <p class="small muted">{{ t('ad.i18n.hint') }}</p>
    <div class="stats">
      <button :class="{ on: filter === 'all' }" @click="filter = 'all'"><b>{{ stats.total }}</b>{{ t('ad.i18n.f_all') }}</button>
      <button :class="{ on: filter === 'human' }" @click="filter = 'human'"><b>{{ stats.human }}</b>{{ t('ad.i18n.f_human') }}</button>
      <button :class="{ on: filter === 'machine' }" data-testid="i18n-f-machine" @click="filter = 'machine'"><b>{{ stats.machine }}</b>{{ t('ad.i18n.f_machine') }}</button>
      <button :class="{ on: filter === 'missing' }" @click="filter = 'missing'"><b>{{ stats.missing }}</b>{{ t('ad.i18n.f_missing') }}</button>
      <button :class="{ on: filter === 'long' }" data-testid="i18n-f-long" @click="filter = 'long'"><b>{{ stats.long }}</b>{{ t('ad.i18n.f_long') }}</button>
    </div>
    <div v-if="error" class="banner bad">{{ error }}</div>
    <div v-if="note" class="banner ok" data-testid="i18n-note">{{ note }}</div>
    <div v-if="pending" class="card stack" data-testid="i18n-pending">
      <b>{{ t('ad.i18n.import_review', { n: pending.length }) }}</b>
      <div class="imp">
        <div v-for="p in pending.slice(0, 50)" :key="p.key"><code>{{ p.key }}</code><s>{{ p.before }}</s><span>{{ p.text }}</span></div>
      </div>
      <div class="row"><button class="btn primary" data-testid="i18n-import-apply" @click="applyImport">{{ t('ad.i18n.import_apply') }}</button>
        <button class="btn" @click="pending = null">{{ t('cancel') }}</button></div>
    </div>

    <div class="i18n-grid">
      <aside class="card groups">
        <button :class="{ on: !group }" @click="group = ''">{{ t('ad.i18n.all_groups') }}</button>
        <button v-for="[g, n] in groups" :key="g" :class="{ on: group === g }" @click="group = g"><code>{{ g }}</code><small>{{ n }}</small></button>
      </aside>
      <section class="card stack" style="min-width:0">
        <input v-model="q" type="search" :placeholder="t('ad.i18n.search')" data-testid="i18n-search" />
        <div class="tbl-wrap">
          <table class="tbl">
            <thead><tr><th>{{ t('ad.i18n.key') }}</th><th>中文</th><th>English</th><th>{{ LANGS.find(l => l.code === lang)?.name }}</th><th>{{ t('ad.i18n.status') }}</th></tr></thead>
            <tbody>
              <tr v-for="r in pageRows" :key="r.key" :data-key="r.key" :class="{ editing: editing === r.key }">
                <td><code>{{ r.key }}</code></td>
                <td>{{ r.zh }}</td>
                <td dir="ltr">{{ r.en }}</td>
                <td :dir="lang === 'ar' ? 'rtl' : undefined" :lang="lang">
                  <template v-if="editing === r.key">
                    <textarea v-model="draft" rows="3" data-testid="i18n-draft"></textarea>
                    <div v-if="draftOver" class="small" style="color:var(--warn)" data-testid="i18n-draft-long">{{ t('ad.i18n.long_draft', { pct: draftOver }) }}</div>
                    <div class="row wrap" style="margin-top:4px">
                      <button class="btn small primary" :disabled="busy || !draft.trim()" data-testid="i18n-save" @click="saveOne(r)">{{ t('ad.i18n.save') }}</button>
                      <button class="btn small" @click="editing = ''">{{ t('cancel') }}</button>
                    </div>
                  </template>
                  <button v-else class="cell" data-testid="i18n-cell" @click="edit(r)">{{ r.text || '—' }}</button>
                  <span v-if="r.long" class="tag warn small" data-testid="i18n-long" :title="t('ad.i18n.long_hint')">{{ t('ad.i18n.long_pct', { pct: over(r.text, r.en) }) }}</span>
                </td>
                <td>
                  <span class="tag" :class="{ ok: r.status === 'human', warn: r.status === 'machine', bad: r.status === 'missing' }">{{ statusText(r.status) }}</span>
                  <div v-if="r.fix" class="small muted">{{ r.fix.updated_by }} · {{ fmtDateTime(r.fix.updated_at) }}</div>
                  <div class="row wrap" style="gap:4px;margin-top:4px">
                    <button v-if="r.status === 'machine'" class="btn small" :disabled="busy" @click="approve(r)">{{ t('ad.i18n.approve') }}</button>
                    <button v-if="r.fix" class="btn small link" :disabled="busy" data-testid="i18n-revert" @click="revert(r)">{{ t('ad.i18n.revert') }}</button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <div class="row" style="justify-content:center;gap:8px" v-if="pages > 1">
          <button class="btn small" :disabled="page <= 1" @click="page--">‹</button>
          <span>{{ page }} / {{ pages }}</span>
          <button class="btn small" :disabled="page >= pages" @click="page++">›</button>
        </div>
      </section>
      <aside class="card stack preview">
        <div class="row wrap" style="gap:6px">
          <b class="grow">{{ t('ad.i18n.preview') }}</b>
          <select v-model="preview.path" @change="preview.store = ''"><option v-for="p in pages_" :key="p.path" :value="p.path">{{ t(p.label) }}</option></select>
          <input v-model.lazy="preview.store" :placeholder="t('ad.i18n.p_store')" style="width:110px" />
        </div>
        <label class="check small" style="margin:0" :title="t('ad.i18n.pseudo_hint')"><input v-model="preview.pseudo" type="checkbox" data-testid="i18n-pseudo" />{{ t('ad.i18n.pseudo') }}</label>
        <iframe :key="previewUrl" :src="previewUrl" title="preview" data-testid="i18n-preview"></iframe>
      </aside>
    </div>
  </div>
</template>

<style scoped>
.stats { display: flex; flex-wrap: wrap; gap: 8px; }
.stats button { display: flex; flex-direction: column; align-items: flex-start; min-width: 110px; padding: 8px 12px; border: 1px solid var(--line); border-radius: 10px; background: #fff; cursor: pointer; font: inherit; }
.stats button b { font-size: 20px; }
.stats button.on { border-color: var(--brand); box-shadow: inset 0 0 0 1px var(--brand); }
.i18n-grid { display: grid; grid-template-columns: 170px minmax(0, 1fr) 380px; gap: 12px; align-items: start; }
.groups { display: flex; flex-direction: column; gap: 2px; max-height: 80vh; overflow: auto; padding: 8px; }
.groups button { display: flex; justify-content: space-between; gap: 6px; text-align: start; border: 0; background: none; padding: 5px 8px; border-radius: 6px; cursor: pointer; font: inherit; }
.groups button.on { background: var(--brand-soft, #fdeee6); }
.tbl-wrap { overflow-x: auto; }
.tbl td { vertical-align: top; font-size: 13px; overflow-wrap: anywhere; }
.tbl td:nth-child(1) { width: 150px; }
.tbl td:nth-child(5) { width: 130px; }
.tbl tr.editing td { background: #fff7ef; }
.tbl tr.editing { outline: 2px solid #f08a24; }
.cell { all: unset; cursor: text; display: block; min-height: 1.4em; border-bottom: 1px dashed #c9b8a6; white-space: pre-wrap; }
.tbl textarea { width: 100%; }
.preview { position: sticky; top: 8px; }
.preview iframe { width: 100%; height: 72vh; border: 1px solid var(--line); border-radius: 12px; background: #fff; }
.imp { display: grid; gap: 4px; max-height: 260px; overflow: auto; font-size: 13px; }
.imp div { display: grid; grid-template-columns: 160px 1fr 1fr; gap: 8px; }
.imp s { color: var(--muted); }
@media (max-width: 1200px) { .i18n-grid { grid-template-columns: minmax(0, 1fr); } .groups { flex-direction: row; flex-wrap: wrap; max-height: none; } .preview { position: static; } }
</style>
