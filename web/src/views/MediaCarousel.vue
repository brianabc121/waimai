<script setup lang="ts">
// The promotional area at the top of the store page: pictures, GIFs and
// videos side by side. It moves on by itself, and customers can swipe.
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { t } from '../i18n'
import { cropStyle } from '../media'
import type { PromoMedia } from '../types'

const props = defineProps<{ media: PromoMedia[] }>()

const track = ref<HTMLElement | null>(null)
const index = ref(0)
// Which videos were started (their cover is gone) and which one plays now.
const started = reactive(new Set<number>())
const playing = ref<number | null>(null)
const muted = ref(false)
const videos = new Map<number, HTMLVideoElement>()
const setVideo = (id: number) => (el: unknown) => { if (el) videos.set(id, el as HTMLVideoElement); else videos.delete(id) }

const current = computed(() => props.media[index.value] ?? props.media[0])
const label = computed(() => {
  const m = current.value
  const name = m.title_tr || m.title || (m.kind === 'video' ? t('pm.video') : '')
  const pos = props.media.length > 1 ? `${index.value + 1}/${props.media.length}` : ''
  return [name, pos].filter(Boolean).join(' · ')
})

function go(i: number, smooth = true) {
  const el = track.value
  if (!el) return
  const n = props.media.length
  el.scrollTo({ left: ((i % n + n) % n) * el.clientWidth, behavior: smooth && !reduced ? 'smooth' : 'auto' })
}
function onScroll() {
  const el = track.value!
  const i = Math.round(el.scrollLeft / el.clientWidth)
  if (i !== index.value) index.value = Math.min(Math.max(i, 0), props.media.length - 1)
}
// A video stops when the customer moves on to something else.
watch(index, () => { stop(); rest() })

// ---- video ----
function toggle(m: PromoMedia) {
  const v = videos.get(m.id)
  if (!v) return
  if (playing.value === m.id) { v.pause(); return }
  const end = m.trim_end || Infinity
  if (!started.has(m.id) || v.currentTime < m.trim_start || v.currentTime >= end - 0.1) v.currentTime = m.trim_start
  started.add(m.id)
  v.muted = muted.value
  void v.play().catch(() => { playing.value = null })
}
function stop() { if (playing.value != null) videos.get(playing.value)?.pause() }
function onTime(m: PromoMedia, e: Event) {
  const v = e.target as HTMLVideoElement
  if (m.trim_end && v.currentTime >= m.trim_end) finished(m, v)
}
function finished(m: PromoMedia, v: HTMLVideoElement) {
  v.pause()
  v.currentTime = m.trim_start
  if (props.media.length > 1 && current.value?.id === m.id) go(index.value + 1)
}
function mute() {
  muted.value = !muted.value
  videos.forEach(v => { v.muted = muted.value })
}

// ---- moving on by itself ----
const reduced = typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches
let timer = 0
function rest() {
  clearInterval(timer)
  if (props.media.length < 2) return
  timer = window.setInterval(() => {
    if (playing.value != null || document.hidden || held) return
    go(index.value + 1)
  }, 5000)
}
// Nothing moves under a finger or a pointer that rests on it.
let held = false
const hold = (v: boolean) => { held = v; if (!v) rest() }
onMounted(rest)
onBeforeUnmount(() => clearInterval(timer))
watch(() => props.media.length, () => { index.value = 0; go(0, false); rest() })
</script>

<template>
  <div class="promo" data-testid="promo" :data-index="index" @pointerenter="hold(true)" @pointerleave="hold(false)" @touchstart.passive="hold(true)" @touchend.passive="hold(false)">
    <div ref="track" class="track" @scroll.passive="onScroll">
      <div v-for="(m, i) in media" :key="m.id" class="slide" :data-kind="m.kind">
        <template v-if="m.kind === 'video'">
          <video :ref="setVideo(m.id)" :src="m.url + (m.trim_start ? '#t=' + m.trim_start : '')" :style="cropStyle(m.crop)" playsinline
            :preload="m.poster_url ? 'none' : 'metadata'" @play="playing = m.id" @pause="playing === m.id && (playing = null)"
            @timeupdate="onTime(m, $event)" @ended="finished(m, $event.target as HTMLVideoElement)" @click="toggle(m)"></video>
          <img v-if="m.poster_url && !started.has(m.id)" :src="m.poster_url" alt="" class="whole" :loading="i ? 'lazy' : 'eager'" draggable="false" />
          <button v-if="playing !== m.id" class="play" :aria-label="t('pm.play')" data-testid="promo-play" @click="toggle(m)">
            <svg viewBox="0 0 24 24" width="30" height="30" fill="currentColor"><path d="M8 5.2v13.6a1 1 0 0 0 1.5.86l11.3-6.8a1 1 0 0 0 0-1.72L9.5 4.34A1 1 0 0 0 8 5.2z" /></svg></button>
          <button v-else class="mute" :aria-label="t(muted ? 'pm.sound_on' : 'pm.sound_off')" @click="mute">
            <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 9.5v5h3.5l4.5 4v-13l-4.5 4z" fill="currentColor" /><path v-if="muted" d="m16 9.5 5 5m0-5-5 5" /><path v-else d="M16 9a4.2 4.2 0 0 1 0 6m2.6-8.6a8 8 0 0 1 0 11.2" /></svg></button>
        </template>
        <img v-else :src="m.url" :alt="m.title_tr || m.title" :style="cropStyle(m.crop)" :loading="i ? 'lazy' : 'eager'" draggable="false" />
      </div>
    </div>
    <template v-if="media.length > 1">
      <button class="arrow prev" :aria-label="t('pm.prev')" @click="go(index - 1)"><span class="rtl-flip">‹</span></button>
      <button class="arrow next" :aria-label="t('pm.next')" data-testid="promo-next" @click="go(index + 1)"><span class="rtl-flip">›</span></button>
      <div class="dots"><i v-for="(m, i) in media" :key="m.id" :class="{ on: i === index }" @click="go(i)"></i></div>
    </template>
    <div v-if="label" class="label" data-testid="promo-label">{{ label }}</div>
  </div>
</template>

<style scoped>
.promo { position: relative; width: 100%; aspect-ratio: 16 / 9; overflow: hidden; background: #1c1c1c; color: #fff; }
.track { display: flex; height: 100%; overflow-x: auto; scroll-snap-type: x mandatory; scrollbar-width: none; overscroll-behavior-x: contain; }
.track::-webkit-scrollbar { display: none; }
.slide { position: relative; flex: 0 0 100%; height: 100%; overflow: hidden; scroll-snap-align: start; scroll-snap-stop: always; }
.slide > img, .slide > video { position: absolute; max-width: none; object-fit: fill; display: block; user-select: none; }
.slide > .whole { inset: 0; width: 100%; height: 100%; object-fit: cover; }
.slide > video { cursor: pointer; }
.play { position: absolute; left: 50%; top: 50%; width: 60px; height: 60px; margin: -30px 0 0 -30px; border: 0; border-radius: 50%; padding: 0 0 0 4px;
  background: rgba(40, 30, 24, .62); color: #fff; display: grid; place-items: center; cursor: pointer; backdrop-filter: blur(3px); }
.play:hover { background: rgba(40, 30, 24, .8); }
.mute { position: absolute; inset-inline-start: 12px; bottom: var(--promo-foot, 12px); width: 32px; height: 32px; border: 0; border-radius: 50%; background: rgba(0, 0, 0, .5); color: #fff; display: grid; place-items: center; cursor: pointer; }
.dots { position: absolute; inset-inline-start: 0; inset-inline-end: 0; bottom: calc(var(--promo-foot, 12px) + 4px); display: flex; justify-content: center; align-items: center; gap: 7px; pointer-events: none; }
.dots i { width: 6px; height: 6px; border-radius: 999px; background: rgba(255, 255, 255, .75); box-shadow: 0 0 2px rgba(0, 0, 0, .4); transition: width .2s; pointer-events: auto; cursor: pointer; }
.dots i.on { width: 20px; background: #fff; }
.label { position: absolute; inset-inline-end: 12px; bottom: var(--promo-foot, 12px); max-width: 42%; padding: 3px 10px; border-radius: 999px; background: rgba(0, 0, 0, .5); font-size: 13px; line-height: 20px;
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis; pointer-events: none; }
.arrow { display: none; position: absolute; top: 50%; width: 36px; height: 36px; margin-top: -18px; border: 0; border-radius: 50%; background: rgba(0, 0, 0, .45); color: #fff; font-size: 24px; line-height: 1; cursor: pointer; padding: 0 0 3px; }
.arrow.prev { inset-inline-start: 10px; } .arrow.next { inset-inline-end: 10px; }
@media (hover: hover) { .promo:hover .arrow { display: block; } }
</style>
