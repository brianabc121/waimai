<script setup lang="ts">
// The cropping tool of the promo media page. The merchant moves and zooms the
// picture behind a 16:9 frame; for a video they also choose the part to play
// and the frame used as its cover. The file itself is not cut: the frame is
// saved with it and applied wherever it is shown.
import { computed, onBeforeUnmount, reactive, ref } from 'vue'
import { t } from '../i18n'
import { MEDIA_RATIO, clock, cropStyle, largestCrop } from '../media'
import type { Crop, PromoMedia } from '../types'

export interface CropResult { crop: Crop; title: string; trim_start: number; trim_end: number; poster: Blob | null }

const props = defineProps<{ kind: PromoMedia['kind']; src: string; initial?: PromoMedia | null; busy?: boolean; error?: string }>()
const emit = defineEmits<{ save: [r: CropResult]; cancel: [] }>()

const stage = ref<HTMLElement | null>(null)
const el = ref<HTMLImageElement | HTMLVideoElement | null>(null)
const ready = ref(false)
const failed = ref(false)
const title = ref(props.initial?.title ?? '')
const crop = reactive<Crop>({ x: 0, y: 0, w: 1, h: 1 })
let full: Crop = { x: 0, y: 0, w: 1, h: 1 }  // the largest crop of the frame's shape
const zoom = ref(1)

// ---- the picture is loaded: start from the saved crop, or the middle ----
function loaded() {
  const e = el.value!
  const w = e instanceof HTMLVideoElement ? e.videoWidth : e.naturalWidth
  const h = e instanceof HTMLVideoElement ? e.videoHeight : e.naturalHeight
  if (!w || !h) { failed.value = true; return }
  full = largestCrop(w, h)
  const c = props.initial?.crop
  // A saved crop of another shape (the whole of a picture that is not 16:9) starts over.
  const same = c && Math.abs(c.w / c.h - full.w / full.h) < 0.02
  Object.assign(crop, same ? c : full)
  zoom.value = Math.min(4, Math.max(1, full.w / crop.w))
  if (e instanceof HTMLVideoElement) {
    // A recording made in a browser does not say how long it is until it was
    // played to its end once: jumping far ahead makes the browser find out.
    if (!isFinite(e.duration)) {
      e.addEventListener('durationchange', () => { if (isFinite(e.duration)) timed(e) }, { once: true })
      e.currentTime = 1e9
      return
    }
    timed(e)
    return
  }
  ready.value = true
}
function timed(v: HTMLVideoElement) {
  video.duration = v.duration || 0
  video.start = Math.min(props.initial?.trim_start ?? 0, video.duration)
  video.end = props.initial?.trim_end ?? 0
  video.cover = video.start
  v.currentTime = video.start
  ready.value = true
}

const clamp = (v: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, v))
function setZoom(z: number) {
  z = clamp(z, 1, 4)
  const cx = crop.x + crop.w / 2, cy = crop.y + crop.h / 2
  crop.w = full.w / z; crop.h = full.h / z
  crop.x = clamp(cx - crop.w / 2, 0, 1 - crop.w); crop.y = clamp(cy - crop.h / 2, 0, 1 - crop.h)
  zoom.value = z
}
function wheel(e: WheelEvent) { if (ready.value) setZoom(zoom.value * (e.deltaY < 0 ? 1.08 : 1 / 1.08)) }

// ---- dragging, and pinching with two fingers ----
const pointers = new Map<number, { x: number; y: number }>()
let pinch = 0
function down(e: PointerEvent) {
  if (!ready.value) return
  stage.value!.setPointerCapture(e.pointerId)
  pointers.set(e.pointerId, { x: e.clientX, y: e.clientY })
  pinch = 0
}
function move(e: PointerEvent) {
  const p = pointers.get(e.pointerId)
  if (!p) return
  const box = stage.value!.getBoundingClientRect()
  if (pointers.size === 1) {
    crop.x = clamp(crop.x - (e.clientX - p.x) / box.width * crop.w, 0, 1 - crop.w)
    crop.y = clamp(crop.y - (e.clientY - p.y) / box.height * crop.h, 0, 1 - crop.h)
  }
  p.x = e.clientX; p.y = e.clientY
  if (pointers.size === 2) {
    const [a, b] = [...pointers.values()]
    const d = Math.hypot(a.x - b.x, a.y - b.y)
    if (pinch) setZoom(zoom.value * d / pinch)
    pinch = d
  }
}
function up(e: PointerEvent) { pointers.delete(e.pointerId); pinch = 0 }
function key(e: KeyboardEvent) {
  const step = e.shiftKey ? 0.1 : 0.02
  const d: Record<string, [number, number]> = { ArrowLeft: [-1, 0], ArrowRight: [1, 0], ArrowUp: [0, -1], ArrowDown: [0, 1] }
  if (e.key === '+' || e.key === '=') setZoom(zoom.value * 1.1)
  else if (e.key === '-') setZoom(zoom.value / 1.1)
  else if (d[e.key]) {
    crop.x = clamp(crop.x + d[e.key][0] * step * crop.w, 0, 1 - crop.w)
    crop.y = clamp(crop.y + d[e.key][1] * step * crop.h, 0, 1 - crop.h)
  } else return
  e.preventDefault()
}

// ---- video: play, the part to play, the cover ----
const video = reactive({ duration: 0, at: 0, playing: false, start: 0, end: 0, cover: 0 })
const vid = () => el.value as HTMLVideoElement
const stopAt = computed(() => video.end || video.duration)
function play() {
  const v = vid()
  if (video.playing) { v.pause(); return }
  if (v.currentTime < video.start || v.currentTime >= stopAt.value - 0.05) v.currentTime = video.start
  void v.play()
}
function tick() {
  const v = vid()
  video.at = v.currentTime
  if (video.playing && v.currentTime >= stopAt.value) { v.pause(); v.currentTime = video.start }
}
function seek(e: Event) { vid().currentTime = Number((e.target as HTMLInputElement).value) }
function startHere() {
  video.at = vid().currentTime
  video.start = Math.min(video.at, Math.max(0, stopAt.value - 0.5))
  if (video.cover < video.start) video.cover = video.start
}
function endHere() {
  video.at = vid().currentTime
  video.end = video.at >= video.duration - 0.05 ? 0 : Math.max(video.at, video.start + 0.5)
  if (video.end && video.cover > video.end) video.cover = video.start
}
function whole() { video.start = 0; video.end = 0 }

// The cover is the cropped frame at the chosen moment, drawn at 960 × 540.
async function cover(): Promise<Blob | null> {
  const v = vid()
  v.pause()
  if (Math.abs(v.currentTime - video.cover) > 0.03) {
    await new Promise<void>(done => { v.addEventListener('seeked', () => done(), { once: true }); v.currentTime = video.cover })
  }
  const c = document.createElement('canvas')
  c.width = 960; c.height = Math.round(960 / MEDIA_RATIO)
  try {
    c.getContext('2d')!.drawImage(v, crop.x * v.videoWidth, crop.y * v.videoHeight, crop.w * v.videoWidth, crop.h * v.videoHeight, 0, 0, c.width, c.height)
    return await new Promise(done => c.toBlob(done, 'image/jpeg', 0.8))
  } catch { return null }
}

const saving = ref(false)
async function save() {
  if (!ready.value || saving.value) return
  saving.value = true
  const poster = props.kind === 'video' ? await cover() : null
  saving.value = false
  emit('save', { crop: { ...crop }, title: title.value.trim(), poster,
    trim_start: props.kind === 'video' ? Math.round(video.start * 10) / 10 : 0,
    trim_end: props.kind === 'video' ? Math.round(video.end * 10) / 10 : 0 })
}
onBeforeUnmount(() => { if (props.kind === 'video') (el.value as HTMLVideoElement | null)?.pause() })
</script>

<template>
  <div class="sheet-mask" @click.self="emit('cancel')">
    <form class="sheet center cropper" data-testid="cropper" @submit.prevent="save">
      <div class="sheet-head"><h2>{{ t('pm.crop_title') }}</h2><div class="muted small">{{ t('pm.crop_hint') }}</div></div>
      <div class="sheet-body stack">
        <div v-if="error" class="banner bad">{{ error }}</div>
        <div v-if="failed" class="banner bad">{{ t(kind === 'video' ? 'pm.cant_play' : 'pm.bad_type') }}</div>
        <div ref="stage" class="stage" :class="{ ready }" tabindex="0" data-testid="crop-stage" :data-crop="[crop.x, crop.y, crop.w, crop.h].map(v => v.toFixed(3)).join(',')"
          @pointerdown="down" @pointermove="move" @pointerup="up" @pointercancel="up" @wheel.prevent="wheel" @keydown="key">
          <video v-if="kind === 'video'" ref="el" :src="src" :style="cropStyle(crop)" playsinline preload="auto"
            @loadedmetadata="loaded" @error="failed = true" @timeupdate="tick" @play="video.playing = true" @pause="video.playing = false"></video>
          <img v-else ref="el" :src="src" alt="" :style="cropStyle(crop)" draggable="false" @load="loaded" @error="failed = true" />
          <div class="thirds"></div>
        </div>
        <label class="row zoom"><span class="small muted">{{ t('pm.zoom') }}</span>
          <input type="range" min="1" max="4" step="0.01" :value="zoom" :disabled="!ready" data-testid="crop-zoom" @input="setZoom(Number(($event.target as HTMLInputElement).value))" />
          <span class="small muted num">{{ zoom.toFixed(1) }}×</span></label>

        <template v-if="kind === 'video' && ready">
          <div class="row player">
            <button type="button" class="btn small" data-testid="crop-play" @click="play">{{ t(video.playing ? 'pm.pause' : 'pm.play') }}</button>
            <input type="range" min="0" :max="video.duration" step="0.05" :value="video.at" class="grow" data-testid="crop-seek" @input="seek" />
            <span class="small muted num">{{ clock(video.at) }} / {{ clock(video.duration) }}</span>
          </div>
          <div class="row wrap trim">
            <span class="small"><b>{{ t('pm.trim') }}</b> <span data-testid="crop-trim">{{ clock(video.start) }} – {{ clock(stopAt) }}</span></span>
            <button type="button" class="btn small" data-testid="trim-start" @click="startHere">{{ t('pm.from_here') }}</button>
            <button type="button" class="btn small" data-testid="trim-end" @click="endHere">{{ t('pm.to_here') }}</button>
            <button type="button" class="btn small link" v-if="video.start || video.end" @click="whole">{{ t('pm.whole') }}</button>
          </div>
          <div class="row wrap trim">
            <span class="small muted">{{ t('pm.cover_at', { t: clock(video.cover) }) }}</span>
            <button type="button" class="btn small" data-testid="cover-here" @click="video.cover = vid().currentTime">{{ t('pm.cover_here') }}</button>
          </div>
        </template>
        <p v-if="kind === 'gif'" class="muted small" style="margin:0">{{ t('pm.gif_note') }}</p>
        <label class="field" style="margin:0"><span>{{ t('pm.title') }}</span><input v-model="title" maxlength="40" :placeholder="t('pm.title_ph')" data-testid="crop-title" /></label>
      </div>
      <div class="sheet-foot row">
        <button type="button" class="btn grow" @click="emit('cancel')">{{ t('cancel') }}</button>
        <button class="btn primary grow" data-testid="crop-save" :disabled="!ready || busy || saving">{{ busy ? t('pm.uploading') : t('save') }}</button>
      </div>
    </form>
  </div>
</template>

<style scoped>
.cropper { max-width: 680px; width: 100%; }
.stage { position: relative; width: 100%; aspect-ratio: 16 / 9; overflow: hidden; background: #1c1c1c; border-radius: 10px; touch-action: none; cursor: grab; outline-offset: 2px; }
.stage:active { cursor: grabbing; }
.stage > img, .stage > video { position: absolute; max-width: none; object-fit: fill; display: block; user-select: none; pointer-events: none; }
.stage:not(.ready) > img, .stage:not(.ready) > video { visibility: hidden; }
/* lines at the thirds help to place the subject */
.thirds { position: absolute; inset: 0; pointer-events: none; border: 2px solid rgba(255, 255, 255, .9); border-radius: 10px;
  background: linear-gradient(to right, transparent calc(33.33% - .5px), rgba(255,255,255,.35) calc(33.33% - .5px) calc(33.33% + .5px), transparent calc(33.33% + .5px) calc(66.66% - .5px), rgba(255,255,255,.35) calc(66.66% - .5px) calc(66.66% + .5px), transparent calc(66.66% + .5px)),
    linear-gradient(to bottom, transparent calc(33.33% - .5px), rgba(255,255,255,.35) calc(33.33% - .5px) calc(33.33% + .5px), transparent calc(33.33% + .5px) calc(66.66% - .5px), rgba(255,255,255,.35) calc(66.66% - .5px) calc(66.66% + .5px), transparent calc(66.66% + .5px)); }
.zoom { gap: 10px; align-items: center; } .zoom input { flex: 1; }
.player, .trim { gap: 8px; align-items: center; }
.num { font-variant-numeric: tabular-nums; white-space: nowrap; }
</style>
