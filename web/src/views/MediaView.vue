<script setup lang="ts">
// Promo media of the ordering page: upload, crop, order, hide and delete.
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { ApiError, del, get, patch, put, upload } from '../api'
import { t } from '../i18n'
import LangsButton from './LangsButton.vue'
import '../media'
import '../officeStrings'
import { currentRole, session } from '../session'
import type { PromoMedia } from '../types'
import MediaCropper, { type CropResult } from './MediaCropper.vue'
import MediaFrame from './MediaFrame.vue'

const bid = session.branchId!
const base = `/merchant/branches/${bid}/media`
const slug = currentRole()?.slug
const list = ref<PromoMedia[]>([])
const max = ref(8)
const error = ref('')
const loading = ref(true)

async function load() {
  const r = await get<{ media: PromoMedia[]; max: number }>(base)
  list.value = r.media; max.value = r.max; loading.value = false
}
onMounted(() => load().catch(e => { error.value = (e as Error).message; loading.value = false }))

async function run(fn: () => Promise<unknown>) {
  error.value = ''
  try { await fn(); await load() } catch (e) { error.value = (e as Error).message }
}

// ---- picking a file ----
const LIMIT = { image: 2, gif: 8, video: 20 } as const
const picker = ref<HTMLInputElement | null>(null)
const KINDS = ['image', 'gif', 'video'] as const
const kind = ref('')
const show = ref<'' | 'on' | 'off'>('')
const shownList = computed(() => list.value.filter(m => (!kind.value || m.kind === kind.value) && (!show.value || m.active === (show.value === 'on'))))
const crop = reactive<{ open: boolean; kind: PromoMedia['kind']; src: string; file: Blob | null; item: PromoMedia | null; busy: boolean; error: string }>(
  { open: false, kind: 'image', src: '', file: null, item: null, busy: false, error: '' })

function kindOf(f: File): PromoMedia['kind'] | null {
  const name = f.name.toLowerCase()
  if (f.type === 'image/gif' || name.endsWith('.gif')) return 'gif'
  if (/^image\/(jpeg|png|webp)$/.test(f.type)) return 'image'
  if (/^video\/(mp4|webm|quicktime)$/.test(f.type) || /\.(mp4|m4v|mov|webm)$/.test(name)) return 'video'
  return null
}

// A photo is brought down to 1920 px and saved as JPEG before it is sent;
// GIFs and videos go as they are.
function shrink(file: File): Promise<Blob> {
  return new Promise((resolve, reject) => {
    const img = new Image(), url = URL.createObjectURL(file)
    img.onload = () => {
      URL.revokeObjectURL(url)
      const k = Math.min(1, 1920 / Math.max(img.width, img.height))
      if (k === 1 && file.type === 'image/jpeg' && file.size <= 1.2e6) { resolve(file); return }
      const c = document.createElement('canvas')
      c.width = Math.round(img.width * k); c.height = Math.round(img.height * k)
      const g = c.getContext('2d')!
      g.fillStyle = '#fff'; g.fillRect(0, 0, c.width, c.height)
      g.drawImage(img, 0, 0, c.width, c.height)
      c.toBlob(b => b ? resolve(b) : reject(new Error(t('pm.bad_type'))), 'image/jpeg', 0.85)
    }
    img.onerror = () => { URL.revokeObjectURL(url); reject(new Error(t('pm.bad_type'))) }
    img.src = url
  })
}

async function picked(e: Event) {
  const input = e.target as HTMLInputElement
  const f = input.files?.[0]
  input.value = ''
  if (!f) return
  error.value = ''
  const kind = kindOf(f)
  if (!kind) { error.value = t('pm.bad_type'); return }
  if (kind !== 'image' && f.size > LIMIT[kind] * 1024 * 1024) {
    error.value = t('pm.too_big', { kind: t('pm.' + kind), n: LIMIT[kind] }); return
  }
  try {
    const file = kind === 'image' ? await shrink(f) : f
    closeCrop()
    Object.assign(crop, { open: true, kind, file, src: URL.createObjectURL(file), item: null, busy: false, error: '' })
  } catch (err) { error.value = (err as Error).message }
}
function edit(m: PromoMedia) {
  closeCrop()
  Object.assign(crop, { open: true, kind: m.kind, file: null, src: m.url, item: m, busy: false, error: '' })
}
function closeCrop() {
  if (crop.file && crop.src) URL.revokeObjectURL(crop.src)
  Object.assign(crop, { open: false, src: '', file: null, item: null })
}
onBeforeUnmount(closeCrop)

async function save(r: CropResult) {
  crop.busy = true; crop.error = ''
  try {
    const meta = { title: r.title, crop: r.crop, trim_start: r.trim_start, trim_end: r.trim_end }
    const m = crop.item
      ? await patch<PromoMedia>(`${base}/${crop.item.id}`, meta)
      : await upload<PromoMedia>(`${base}?meta=${encodeURIComponent(JSON.stringify(meta))}`, crop.file!)
    if (r.poster) await upload(`${base}/${m.id}/poster`, r.poster)
    closeCrop()
    await load()
  } catch (e) {
    const err = e as ApiError
    crop.error = err.code === 'MEDIA_FULL' ? t('pm.full', { n: max.value }) : err.code === 'FILE_TOO_LARGE' || err.status === 413
      ? t('pm.too_big', { kind: t('pm.' + crop.kind), n: LIMIT[crop.kind] }) : err.message
  }
  crop.busy = false
}

// ---- the list ----
const toggle = (m: PromoMedia) => run(() => patch(`${base}/${m.id}`, { active: !m.active }))
const remove = (m: PromoMedia) => { if (confirm(t('pm.delete_confirm'))) void run(() => del(`${base}/${m.id}`)) }
function shift(i: number, by: number) {
  const ids = list.value.map(m => m.id)
  const [id] = ids.splice(i, 1)
  ids.splice(i + by, 0, id)
  void run(() => put(`${base}-order`, { ids }))
}
const mb = (n: number) => n >= 1048576 ? (n / 1048576).toFixed(1) + ' MB' : Math.max(1, Math.round(n / 1024)) + ' KB'
// The cover address stays the same when the cover changes; the list asks for it anew.
const stamp = ref(Date.now())
const poster = (m: PromoMedia) => m.poster_url ? `${m.poster_url}?v=${stamp.value}` : ''
async function saved(r: CropResult) { await save(r); stamp.value = Date.now() }
</script>

<template>
  <div class="ox warm">
  <div class="page ox-page">
    <div class="ox-head">
      <div><h1>{{ t('promo_media') }}</h1><p class="ox-sub">{{ t('ox.pm.sub') }}</p></div>
      <div class="ox-acts">
        <a v-if="slug" class="btn" :href="'/s/' + slug" target="_blank" rel="noopener">{{ t('pm.preview') }}</a>
        <button class="btn primary" data-testid="media-upload" :disabled="list.length >= max" @click="picker?.click()">＋ {{ t('pm.upload') }}</button>
        <input ref="picker" type="file" hidden data-testid="media-file" accept="image/jpeg,image/png,image/webp,image/gif,video/mp4,video/webm,video/quicktime" @change="picked" />
      </div>
    </div>
    <div v-if="error" class="banner bad" data-testid="media-error">{{ error }}</div>

    <div class="ox-stats">
      <div class="ox-stat"><span class="l">{{ t('ox.pm.total') }}</span><b class="v">{{ list.length }} <small class="muted">/ {{ max }}</small></b></div>
      <div class="ox-stat"><span class="l">{{ t('pm.shown') }}</span><b class="v">{{ list.filter(m => m.active).length }}</b></div>
      <div class="ox-stat"><span class="l">{{ t('pm.hidden') }}</span><b class="v">{{ list.filter(m => !m.active).length }}</b></div>
      <div class="ox-stat"><span class="l">{{ t('ox.pm.left') }}</span><b class="v" :class="{ red: list.length >= max }">{{ Math.max(0, max - list.length) }}</b></div>
    </div>

    <div class="ox-body">
      <aside class="ox-rail">
        <div class="ox-rail-h">{{ t('ox.pm.kinds') }}</div>
        <div class="ox-rail-list">
          <button class="ox-cat" :class="{ on: kind === '' }" @click="kind = ''"><span class="nm">{{ t('ox.pm.all') }}</span><span class="n">{{ list.length }}</span></button>
          <button v-for="k in KINDS" :key="k" class="ox-cat" :class="{ on: kind === k }" @click="kind = k"><span class="nm">{{ t('pm.' + k) }}</span><span class="n">{{ list.filter(m => m.kind === k).length }}</span></button>
        </div>
        <p class="ox-rail-tip">{{ t('pm.limits', { n: max }) }}</p>
      </aside>

      <section class="ox-panel">
        <div class="ox-tools">
          <div class="ox-chips">
            <button :class="{ on: show === '' }" @click="show = ''">{{ t('ox.tb.all_states') }}</button>
            <button :class="{ on: show === 'on' }" @click="show = 'on'">{{ t('pm.shown') }}</button>
            <button :class="{ on: show === 'off' }" @click="show = 'off'">{{ t('pm.hidden') }}</button>
          </div>
          <span class="grow"></span><span class="muted small">{{ t('ox.pm.order') }}</span>
        </div>
        <div v-if="!loading && !list.length" class="ox-empty" data-testid="media-none">{{ t('pm.none') }}</div>
        <div v-else-if="!shownList.length" class="ox-empty">{{ t('ox.pm.no_match') }}</div>
        <div class="mlist">
          <div v-for="m in shownList" :key="m.id" class="mcard" :class="{ off: !m.active }" data-testid="media-card" :data-kind="m.kind" :data-active="m.active">
            <div class="thumb">
              <MediaFrame :kind="m.kind" :src="m.url" :crop="m.crop" :poster="poster(m)" :still="!!m.poster_url" lazy />
              <span class="no">{{ list.indexOf(m) + 1 }}</span>
              <span class="tag ox-pill st" :class="m.active ? 'ok' : ''">{{ t(m.active ? 'pm.shown' : 'pm.hidden') }}</span>
            </div>
            <div class="info">
              <div class="bold name ox-strong">{{ m.title || '—' }}</div>
              <div class="row wrap" style="gap:6px">
                <span class="ox-tag" :class="{ gold: m.kind === 'gif', blue: m.kind === 'video' }">{{ t('pm.' + m.kind) }}</span>
                <span class="muted small">{{ mb(m.size) }}</span>
              </div>
              <div class="ox-ops acts">
                <LangsButton v-if="m.title" small :base="`/merchant/branches/${bid}`" :scope="`media/${m.id}`" :name="m.title" />
                <button class="op" :disabled="list.indexOf(m) === 0" :aria-label="t('pm.up')" :title="t('pm.up')" data-testid="media-up" @click="shift(list.indexOf(m), -1)">↑</button>
                <button class="op" :disabled="list.indexOf(m) === list.length - 1" :aria-label="t('pm.down')" :title="t('pm.down')" data-testid="media-down" @click="shift(list.indexOf(m), 1)">↓</button>
                <button class="op red" data-testid="media-crop" @click="edit(m)">{{ t('pm.crop') }}</button>
                <button class="op" data-testid="media-toggle" @click="toggle(m)">{{ t(m.active ? 'pm.hide' : 'pm.show') }}</button>
                <button class="op bad" data-testid="media-delete" @click="remove(m)">{{ t('delete') }}</button>
              </div>
            </div>
          </div>
        </div>
      </section>
    </div>

    <MediaCropper v-if="crop.open" :key="crop.src" :kind="crop.kind" :src="crop.src" :initial="crop.item" :busy="crop.busy" :error="crop.error"
      @save="saved" @cancel="closeCrop" />
  </div>
  </div>
</template>

<style scoped>
.mlist { display: grid; gap: 12px; grid-template-columns: repeat(auto-fill, minmax(min(100%, 260px), 1fr)); }
.mcard { display: flex; flex-direction: column; background: #fff; border: 1px solid var(--line); border-radius: 12px; overflow: hidden; }
.mcard.off .thumb { opacity: .45; }
.thumb { position: relative; background: #f3e9da; }
.no { position: absolute; inset-inline-start: 8px; top: 8px; min-width: 24px; height: 24px; padding: 0 7px; border-radius: 999px; background: rgba(47, 24, 16, .7); color: #fff; font-size: 12.5px; line-height: 24px; text-align: center; font-weight: 700; }
.st { position: absolute; inset-inline-end: 4px; top: 8px; }
.info { display: grid; gap: 6px; min-width: 0; padding: 10px 12px; }
.name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.acts { justify-content: flex-start; margin-inline-start: -10px; }
.ox-stat .v small { font-size: 14px; font-weight: 500; }
</style>
