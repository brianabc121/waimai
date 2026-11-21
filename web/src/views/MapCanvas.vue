<script setup lang="ts">
// A map with an SVG layer on top. The default slot draws in screen pixels
// using `px(latLng)`; the `ui` slot holds ordinary HTML floating over the map.
import { onBeforeUnmount, onMounted, ref, shallowRef } from 'vue'
import { fitView, fromScreen, metresPerPixel, toScreen, type LatLng, type Pt, type View } from '../geo'
import { createMap, type MapAdapter, type Place } from '../maps'
import { t } from '../i18n'

const props = defineProps<{ mapsKey: string; initial: View }>()
const emit = defineEmits<{ click: [at: LatLng]; dblclick: [at: LatLng]; move: [at: LatLng]; ready: [] }>()

const root = ref<HTMLElement>(), base = ref<HTMLElement>()
const adapter = shallowRef<MapAdapter | null>(null)
const view = ref<View>(props.initial)
const size = ref({ w: 0, h: 0 })
const failed = ref('')
let ro: ResizeObserver | null = null

function sync() { if (adapter.value) view.value = adapter.value.view() }
function measure() { if (root.value) size.value = { w: root.value.clientWidth, h: root.value.clientHeight } }

onMounted(async () => {
  measure()
  ro = new ResizeObserver(() => { measure(); sync() })
  ro.observe(root.value!)
  try {
    adapter.value = await createMap(props.mapsKey, base.value!, props.initial, {
      view: sync, click: at => emit('click', at), dblclick: at => emit('dblclick', at), move: at => emit('move', at),
    })
    sync()
    emit('ready')
  } catch (e) {
    failed.value = (e as Error).message === 'MAPS_AUTH' ? t('map_auth_failed') : t('map_load_failed')
  }
})
onBeforeUnmount(() => { ro?.disconnect(); adapter.value?.destroy() })

const px = (l: LatLng): Pt => toScreen(l, view.value, size.value)
const at = (p: Pt): LatLng => fromScreen(p, view.value, size.value)
const mpp = () => metresPerPixel(view.value.center[0], view.value.zoom)
function setView(v: View) { adapter.value?.setView(v); sync() }
function fit(points: LatLng[], maxZoom = 17) { if (points.length && size.value.w) setView(fitView(points, size.value, maxZoom)) }
function zoomBy(d: number) { setView({ center: view.value.center, zoom: view.value.zoom + d }) }
const search = (q: string): Promise<Place[]> => adapter.value?.search(q) ?? Promise.resolve([])
const setType = (m: 'roadmap' | 'satellite') => adapter.value?.setType(m)

// drag follows the pointer from a handle's pointerdown until release and
// reports where on the map it is. The map underneath does not pan meanwhile.
function drag(e: PointerEvent, onMove: (at: LatLng) => void, onEnd?: (moved: boolean) => void) {
  e.preventDefault(); e.stopPropagation()
  const x0 = e.clientX, y0 = e.clientY
  let moved = false
  const move = (m: PointerEvent) => {
    if (!moved && Math.hypot(m.clientX - x0, m.clientY - y0) < 3) return
    moved = true
    const r = root.value!.getBoundingClientRect()
    onMove(at({ x: m.clientX - r.left, y: m.clientY - r.top }))
  }
  const up = () => { window.removeEventListener('pointermove', move); window.removeEventListener('pointerup', up); onEnd?.(moved) }
  window.addEventListener('pointermove', move); window.addEventListener('pointerup', up)
}

defineExpose({ px, at, mpp, setView, fit, search, setType, drag, view, size, adapter })
</script>

<template>
  <div class="mapc" ref="root">
    <div class="mapc-base" ref="base"></div>
    <svg v-if="adapter" class="mapc-layer" :width="size.w" :height="size.h" :viewBox="`0 0 ${size.w} ${size.h}`"><slot :px="px" /></svg>
    <slot name="ui" />
    <div v-if="adapter?.kind === 'stub'" class="mapc-float mapc-zoom">
      <button type="button" data-testid="zoom-in" @click="zoomBy(1)">+</button><button type="button" data-testid="zoom-out" @click="zoomBy(-1)">−</button>
    </div>
    <div v-if="adapter?.kind === 'stub'" class="mapc-attrib">{{ t('map_stub') }}</div>
    <div v-if="failed" class="mapc-failed"><div class="banner bad">{{ failed }}</div></div>
  </div>
</template>
