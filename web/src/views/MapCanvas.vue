<script setup lang="ts">
// A map with an SVG layer in it. The default slot draws with `px(latLng)`, in
// pixels of that layer; the layer belongs to the map and moves with it. The
// `ui` slot holds ordinary HTML floating over the map.
import { onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import { fitView, metresPerPixel, world, type LatLng, type Pt, type View } from '../geo'
import { createMap, CURSORS, layerOf, type Layer, type MapAdapter, type Place, type Spot } from '../maps'
import { t } from '../i18n'

// cursor: 'pen' while drawing, 'pin' while placing, 'pointer', or '' for the map's own.
const props = defineProps<{ mapsKey: string; initial: View; cursor?: string }>()
const emit = defineEmits<{ click: [at: LatLng]; dblclick: [at: LatLng]; move: [at: LatLng]; ready: [] }>()

const root = ref<HTMLElement>(), base = ref<HTMLElement>(), svg = ref<SVGSVGElement>()
const adapter = shallowRef<MapAdapter | null>(null)
const view = ref<View>(props.initial)
const size = ref({ w: 0, h: 0 })
const layer = ref<Layer>(layerOf(props.initial, size.value))
const failed = ref('')
let ro: ResizeObserver | null = null

function sync() {
  const a = adapter.value
  if (!a) return
  const v = a.view(), l = a.layer(), w = view.value, k = layer.value
  // Only real changes reach Vue: this runs many times during a zoom.
  if (v.zoom !== w.zoom || v.center[0] !== w.center[0] || v.center[1] !== w.center[1]) view.value = v
  if (l.zoom !== k.zoom || l.ox !== k.ox || l.oy !== k.oy) layer.value = l
}
function measure() { if (root.value) size.value = { w: root.value.clientWidth, h: root.value.clientHeight } }

onMounted(async () => {
  measure()
  ro = new ResizeObserver(() => { measure(); sync() })
  ro.observe(root.value!)
  try {
    adapter.value = await createMap(props.mapsKey, base.value!, props.initial, {
      view: sync, click: at => emit('click', at), dblclick: at => emit('dblclick', at), move: at => emit('move', at),
    })
    adapter.value.mount(svg.value!)
    adapter.value.setCursor(CURSORS[props.cursor ?? ''] ?? '')
    sync()
    emit('ready')
  } catch (e) {
    failed.value = (e as Error).message === 'MAPS_AUTH' ? t('map_auth_failed') : t('map_load_failed')
  }
})
onBeforeUnmount(() => { ro?.disconnect(); adapter.value?.destroy() })

watch(() => props.cursor, c => adapter.value?.setCursor(CURSORS[c ?? ''] ?? ''))

const px = (l: LatLng): Pt => { const k = layer.value, p = world(l, k.zoom); return { x: p.x - k.ox, y: p.y - k.oy } }
// at takes a pixel of the map's box (not of the layer).
const at = (p: Pt): LatLng => adapter.value ? adapter.value.toLatLng(p) : props.initial.center
const mpp = () => metresPerPixel(view.value.center[0], view.value.zoom)
function setView(v: View) { adapter.value?.setView(v); sync() }
function fit(points: LatLng[], maxZoom = 17) { if (points.length && size.value.w) setView(fitView(points, size.value, maxZoom)) }
function zoomBy(d: number) { setView({ center: view.value.center, zoom: view.value.zoom + d }) }
const search = (q: string): Promise<Spot[]> => adapter.value?.search(q) ?? Promise.resolve([])
const suggest = (q: string): Promise<Place[]> => adapter.value?.suggest(q) ?? Promise.resolve([])
const nearby = (p: LatLng): Promise<Place[]> => adapter.value?.nearby(p) ?? Promise.resolve([])
const setType = (m: 'roadmap' | 'satellite') => adapter.value?.setType(m)

// drag follows the pointer from a handle's pointerdown until release and
// reports where on the map it is. The map underneath does not pan meanwhile.
function drag(e: PointerEvent, onMove: (at: LatLng) => void, onEnd?: (moved: boolean) => void) {
  e.preventDefault(); e.stopPropagation()
  adapter.value?.lock(true)
  const x0 = e.clientX, y0 = e.clientY
  let moved = false
  const move = (m: PointerEvent) => {
    if (!moved && Math.hypot(m.clientX - x0, m.clientY - y0) < 3) return
    moved = true
    const r = root.value!.getBoundingClientRect()
    onMove(at({ x: m.clientX - r.left, y: m.clientY - r.top }))
  }
  const up = () => {
    window.removeEventListener('pointermove', move); window.removeEventListener('pointerup', up)
    adapter.value?.lock(false); onEnd?.(moved)
  }
  window.addEventListener('pointermove', move); window.addEventListener('pointerup', up)
}

defineExpose({ px, at, mpp, setView, fit, search, suggest, nearby, setType, drag, view, size, adapter })
</script>

<template>
  <div class="mapc" ref="root">
    <div class="mapc-base" ref="base"></div>
    <svg ref="svg" class="mapc-layer" width="1" height="1"><slot v-if="adapter" :px="px" /></svg>
    <slot name="ui" />
    <div v-if="adapter?.kind === 'stub'" class="mapc-float mapc-zoom">
      <button type="button" data-testid="zoom-in" @click="zoomBy(1)">+</button><button type="button" data-testid="zoom-out" @click="zoomBy(-1)">−</button>
    </div>
    <div v-if="adapter?.kind === 'stub'" class="mapc-attrib">{{ t('map_stub') }}</div>
    <div v-if="failed" class="mapc-failed"><div class="banner bad">{{ failed }}</div></div>
  </div>
</template>
