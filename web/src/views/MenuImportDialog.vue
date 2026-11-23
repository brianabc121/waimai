<script setup lang="ts">
// Excel import of dishes: download the template (or the current dishes),
// upload, wait for the background parse, review row by row, then confirm.
// The store's back office and the platform admin use the same dialog with
// their own API base.
import { reactive } from 'vue'
import { get, post, upload } from '../api'
import { importError, money, t } from '../i18n'
import type { ImportRow, MenuImport } from '../types'

const props = defineProps<{ base: string; cur: string; title?: string }>()
const emit = defineEmits<{ close: []; imported: [] }>()

const imp = reactive<{ busy: boolean; error: string; data: MenuImport | null; done: string; file: string }>({ busy: false, error: '', data: null, done: '', file: '' })
let open = true

async function pickFile(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  Object.assign(imp, { busy: true, error: '', data: null, done: '', file: file.name })
  try {
    const { id } = await upload<{ id: number }>(`${props.base}/menu-imports?filename=${encodeURIComponent(file.name)}`, file)
    for (let i = 0; i < 60 && open; i++) {
      const m = await get<MenuImport>(`${props.base}/menu-imports/${id}`)
      if (m.status !== 'pending') { imp.data = m; break }
      await new Promise(r => setTimeout(r, 1000))
    }
    if (!imp.data && open) imp.error = t('import_parsing')
  } catch (err) { imp.error = (err as Error).message }
  imp.busy = false
}

async function apply() {
  if (!imp.data) return
  imp.busy = true; imp.error = ''
  try {
    const m = await post<MenuImport>(`${props.base}/menu-imports/${imp.data.id}/apply`)
    imp.data = m
    imp.done = t('import_done', { c: m.created_count, u: m.updated_count })
    emit('imported')
  } catch (err) { imp.error = (err as Error).message }
  imp.busy = false
}

function close() { open = false; emit('close') }

const priceBad = (r: ImportRow) => r.errors.includes('price_invalid') || r.errors.includes('price_required')
const specText = (r: ImportRow) => r.specs.map(g => `${g.name}（${g.options.map(o => o.name).join('/')}）`).join(' ')
// For a new dish a blank yes/no cell means yes; for an existing one, unchanged.
function channel(r: ImportRow, v: boolean | null): 'on' | 'off' | 'keep' {
  if (v === null) return r.action === 'update' ? 'keep' : 'on'
  return v ? 'on' : 'off'
}
const actionLabel = (r: ImportRow) => r.action === 'create' && r.published !== true ? t('act.create_draft') : t('act.' + r.action)
const template = (menu: boolean) => `/api/v1${props.base}/menu-imports/template${menu ? '?with=menu' : ''}`
</script>

<template>
  <div class="sheet-mask" @click.self="close">
    <div class="sheet center imp-sheet" data-testid="import-dialog">
      <div class="sheet-head"><h2>{{ title || t('import_excel') }}</h2></div>
      <div class="sheet-body stack">
        <p class="muted small">{{ t('import_hint') }}</p>
        <div class="row wrap">
          <a class="btn" :href="template(false)" download data-testid="import-template">{{ t('download_template') }}</a>
          <a class="btn" :href="template(true)" download :title="t('import_export_hint')" data-testid="import-export">{{ t('import_export') }}</a>
          <label class="btn primary" :class="{ disabled: imp.busy }">{{ t('upload') }} .xlsx
            <input type="file" accept=".xlsx,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" hidden :disabled="imp.busy" data-testid="import-file" @change="pickFile" /></label>
          <span v-if="imp.busy" class="muted">{{ t('import_parsing') }}</span>
        </div>
        <details class="small muted"><summary>{{ t('import_syntax') }}</summary><p style="margin:6px 0 0">{{ t('import_syntax_body') }}</p></details>
        <div v-if="imp.error" class="banner bad">{{ imp.error }}</div>
        <div v-if="imp.done" class="banner ok" data-testid="import-done">{{ imp.done }}</div>
        <template v-if="imp.data">
          <div v-if="imp.data.status === 'failed'" class="banner bad">{{ t('import_failed') }}: {{ imp.data.error }}</div>
          <template v-else>
            <div class="banner" :class="imp.data.error_rows ? 'warn' : 'info'" data-testid="import-summary">
              <b>{{ imp.file || imp.data.filename }}</b> · {{ t('import_summary', { total: imp.data.total_rows, valid: imp.data.valid_rows, bad: imp.data.error_rows }) }}</div>
            <div class="imp-table">
              <table class="data">
                <thead><tr><th>{{ t('import_line') }}</th><th>{{ t('category') }}</th><th>{{ t('import_dish') }}</th><th class="right">{{ t('price') }}</th>
                  <th>{{ t('import_specs') }}</th><th>{{ t('import_channels') }}</th><th>{{ t('import_result') }}</th></tr></thead>
                <tbody><tr v-for="r in imp.data.rows" :key="r.line" :class="{ badrow: r.errors.length }" :data-line="r.line">
                  <td class="num">{{ r.line }}</td>
                  <td>{{ r.category }}<span v-if="r.new_category" class="tag brand imp-newcat">{{ t('import_new_cat') }}</span></td>
                  <td><div class="imp-dish">
                    <img v-if="r.image_url" :src="r.image_url" alt="" class="imp-thumb" loading="lazy" referrerpolicy="no-referrer" />
                    <span v-else class="imp-thumb empty">{{ r.name.slice(0, 1) || '—' }}</span>
                    <div><b>{{ r.name || '—' }}</b><span v-if="r.photos.length" class="muted small"> +{{ r.photos.length }}🖼</span>
                      <div class="muted small">{{ r.name_en }}</div></div>
                  </div></td>
                  <td class="price right"><span v-if="r.action === 'update' && r.old_price !== r.price" class="muted strike" style="margin-right:4px">{{ money(r.old_price ?? 0, cur) }}</span>{{ priceBad(r) ? '—' : money(r.price, cur) }}</td>
                  <td class="small imp-specs"><template v-if="r.specs.length">{{ specText(r) }}</template>
                    <span v-else-if="r.specs_set && r.action === 'update'" class="muted">{{ t('import_none') }}</span>
                    <span v-else class="muted">{{ r.action === 'update' ? t('import_keep') : '—' }}</span></td>
                  <td><span class="imp-ch">
                    <i v-for="c in (['dine_in', 'delivery', 'pickup'] as const)" :key="c" :class="channel(r, r[c])">{{ t('ich.' + c) }}</i></span></td>
                  <td><span v-if="r.errors.length" class="small" style="color:var(--bad)">{{ r.errors.map(importError).join('；') }}</span>
                    <span v-else class="tag" :class="r.action === 'create' ? 'ok' : 'info'">{{ actionLabel(r) }}</span></td>
                </tr></tbody>
              </table>
            </div>
          </template>
        </template>
      </div>
      <div class="sheet-foot row">
        <button type="button" class="btn grow" @click="close">{{ imp.done ? t('back') : t('cancel') }}</button>
        <button v-if="imp.data?.status === 'parsed'" class="btn primary grow" :disabled="imp.busy || imp.data.valid_rows === 0" data-testid="import-apply" @click="apply">
          {{ t('import_apply', { n: imp.data.valid_rows }) }}</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.imp-sheet { max-width: 980px; width: 100%; }
.imp-table { max-height: 50vh; overflow: auto; }
.imp-table th { position: sticky; top: 0; background: var(--card, #fff); z-index: 1; }
.badrow td { background: var(--bad-soft); }
label.btn.disabled { opacity: .5; pointer-events: none; }
.imp-dish { display: flex; gap: 8px; align-items: center; min-width: 150px; }
.imp-thumb { width: 36px; height: 36px; border-radius: 6px; object-fit: cover; flex: none; }
.imp-thumb.empty { display: grid; place-items: center; background: var(--line, #eee); color: var(--muted, #888); font-size: 14px; }
.imp-specs { max-width: 220px; }
.imp-newcat { margin-left: 4px; font-size: 11px; }
.imp-ch { display: inline-flex; gap: 3px; }
.imp-ch i { font-style: normal; font-size: 11px; padding: 1px 4px; border-radius: 4px; border: 1px solid var(--line, #ddd); }
.imp-ch i.on { background: var(--ok-soft, #e7f6ec); border-color: transparent; color: var(--ok, #1a7f37); }
.imp-ch i.off { color: var(--muted, #999); text-decoration: line-through; }
.imp-ch i.keep { color: var(--muted, #999); border-style: dashed; }
details summary { cursor: pointer; }
</style>
