<script setup lang="ts">
// Where deliveries went in a period, on a map: a dot per address (merged
// when they crowd), a heat map weighted by deliveries, the stores and (for one
// store) its delivery zones. docs/DELIVERY_ADDRESS_STATS_DESIGN.md §3.3.
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { pick, t } from '../../i18n'
import type { LatLng, Pt } from '../../geo'
import MapCanvas from '../MapCanvas.vue'
import { cluster, dotColor, dotRadius, HEAT_GRADIENT, paintHeat } from './heat'
import '../mapStrings'
import './deliveryStrings'

export interface MapPoint { key: string; lat: number; lng: number; n: number; c: number; line: string; last_at: string; b?: number }
export interface MapBrand { id: number; name: string; color: string; n: number }
export interface MapStore { id: number; name: string; lat: number; lng: number }
export interface MapZone { id: number; name: string; name_en: string; color: string; polygon: LatLng[] }
export interface MapData { points: MapPoint[]; too_many: boolean; orders: number; unpinned: number; stores: MapStore[]; zones: MapZone[]; brands?: MapBrand[] }

const props = defineProps<{ mapsKey: string; data: MapData; focus?: string; admin?: boolean }>()
const emit = defineEmits<{ rank: [key: string] }>()

const COLORS = ['#2a78d6', '#18794e', '#7c3aed', '#c2255c', '#e0a100', '#0b7285']
const DEFAULT_CENTER: LatLng = [14.5995, 120.9842]

const map = ref<InstanceType<typeof MapCanvas>>()
const ready = ref(false)
const mode = ref<'pins' | 'heat' | 'both'>('both')
const showStores = ref(true)
const showZones = ref(true)
const radius = ref(30)
const mapType = ref<'roadmap' | 'satellite'>('roadmap')
const sel = ref<MapPoint | null>(null)
// Several brands shown together: a dot takes the colour of the brand that
// delivered most there (docs/MULTI_BRAND_DESIGN.md §12); the heat stays all of them.
const brandList = computed(() => props.data.brands ?? [])
const byBrand = ref(true)
const colorBy = computed(() => byBrand.value && brandList.value.length > 1)
const brandColor = (id?: number) => brandList.value.find(b => b.id === id)?.color || '#a02c20'
function fill(c: { n: number; items: { n: number; p: MapPoint }[] }) {
  if (!colorBy.value) return dotColor(c.n)
  const by = new Map<number, number>()
  for (const d of c.items) by.set(d.p.b ?? 0, (by.get(d.p.b ?? 0) ?? 0) + d.n)
  return brandColor([...by.entries()].sort((a, b) => b[1] - a[1])[0][0])
}

const pts = computed(() => props.data.points)
const all = computed<LatLng[]>(() => [...pts.value.map(p => [p.lat, p.lng] as LatLng), ...props.data.stores.map(s => [s.lat, s.lng] as LatLng)])
const initial = computed(() => ({ center: all.value[0] ?? DEFAULT_CENTER, zoom: 13 }))
const top5 = computed(() => pts.value.slice(0, 5))
const pinnedOrders = computed(() => pts.value.reduce((s, p) => s + p.n, 0))
const zname = (z: MapZone) => pick(z.name, z.name_en || z.name)
const zcolor = (z: MapZone, i: number) => z.color || COLORS[i % COLORS.length]

function fitAll() {
  if (all.value.length > 1) map.value?.fit(all.value, 16)
  else if (all.value.length === 1) map.value?.setView({ center: all.value[0], zoom: 15 })
}
function onReady() { ready.value = true; fitAll(); focusKey(props.focus); void repaint() }
watch(() => props.data, () => { sel.value = null; fitAll(); void repaint() })
watch(() => props.focus, k => focusKey(k))
function focusKey(k?: string) {
  const p = k ? pts.value.find(x => x.key === k) : undefined
  if (!p || !map.value) return
  map.value.setView({ center: [p.lat, p.lng], zoom: Math.max(16, map.value.view.zoom) })
  sel.value = p
}

// The box of the map, in pixels of the drawing layer.
function origin(): Pt { const m = map.value!; return m.px(m.at({ x: 0, y: 0 })) }

// Dots, merged by a 44-pixel grid until the map is close in.
type Dot = { x: number; y: number; n: number; p: MapPoint }
function dots(px: (l: LatLng) => Pt) {
  const ds: Dot[] = pts.value.map(p => { const q = px([p.lat, p.lng]); return { x: q.x, y: q.y, n: p.n, p } })
  const z = map.value?.view.zoom ?? 0
  return cluster(ds, z >= 17 ? 1 : 56)
}
function openCluster(c: { items: Dot[]; x: number; y: number }) {
  if (c.items.length === 1) { sel.value = c.items[0].p; return }
  const m = map.value!
  const ls = c.items.map(d => [d.p.lat, d.p.lng] as LatLng)
  m.fit(ls, 19)
}

// The heat: painted for the box on screen, after the map settles.
const heat = ref<{ href: string; x: number; y: number; w: number; h: number; zoom: number } | null>(null)
let timer: ReturnType<typeof setTimeout> | undefined
function repaint() {
  clearTimeout(timer)
  timer = setTimeout(() => {
    const m = map.value
    if (!m || !ready.value || mode.value === 'pins') { heat.value = null; return }
    const o = origin(), { w, h } = m.size
    const r = radius.value
    const ds = pts.value.map(p => { const q = m.px([p.lat, p.lng]); return { x: q.x - o.x, y: q.y - o.y, n: p.n } })
    const href = paintHeat(ds, w, h, r)
    heat.value = href ? { href, x: o.x, y: o.y, w, h, zoom: m.view.zoom } : null
  }, 120)
}
watch(() => [map.value?.view.zoom, map.value?.view.center[0], map.value?.view.center[1], map.value?.size.w, map.value?.size.h], () => void repaint())
watch([mode, radius], () => void repaint())
onBeforeUnmount(() => clearTimeout(timer))
const heatShown = computed(() => heat.value && mode.value !== 'pins' && map.value && Math.abs(heat.value.zoom - map.value.view.zoom) < 1e-6)

// Where the open card goes: beside its dot, kept inside the map.
const card = computed(() => {
  const m = map.value, p = sel.value
  if (!m || !p) return null
  const o = origin(), q = m.px([p.lat, p.lng]), x = q.x - o.x, y = q.y - o.y
  const W = Math.min(260, m.size.w - 24)
  const left = Math.max(12, Math.min(m.size.w - W - 12, x + 18 + W > m.size.w ? x - W - 18 : x + 18))
  const top = Math.max(56, Math.min(m.size.h - 210, y - 90))
  return { left: left + 'px', top: top + 'px', width: W + 'px' }
})
const two = (n: number) => String(n).padStart(2, '0')
function when(iso: string) { const d = new Date(iso); return `${two(d.getMonth() + 1)}-${two(d.getDate())} ${two(d.getHours())}:${two(d.getMinutes())}` }
const gmaps = (p: { lat: number; lng: number }) => `https://www.google.com/maps/search/?api=1&query=${p.lat},${p.lng}`
const poly = (ring: LatLng[], px: (l: LatLng) => Pt) => ring.map(l => { const q = px(l); return `${q.x},${q.y}` }).join(' ')
function setType(m: 'roadmap' | 'satellite') { mapType.value = m; map.value?.setType(m) }
</script>

<template>
  <div class="dm-wrap">
    <div class="dm-map card">
      <MapCanvas ref="map" :maps-key="mapsKey" :initial="initial" class="dm-canvas" data-testid="delivery-map" @ready="onReady" @click="sel = null">
        <template #default="{ px }">
          <image v-if="heatShown" :href="heat!.href" :x="heat!.x" :y="heat!.y" :width="heat!.w" :height="heat!.h" data-testid="heat-layer" />
          <g v-if="showZones && !admin">
            <g v-for="(z, i) in data.zones" :key="z.id">
              <polygon :points="poly(z.polygon, px)" fill="none" :stroke="zcolor(z, i)" stroke-width="2" stroke-dasharray="6 4" stroke-opacity="0.85" stroke-linejoin="round" data-testid="zone-outline" />
            </g>
          </g>
          <g v-if="mode !== 'heat'">
            <g v-for="(c, i) in dots(px)" :key="i" class="hit" :data-n="c.n" :data-count="c.items.length" data-testid="map-dot" @click.stop="openCluster(c)">
              <circle :cx="c.x" :cy="c.y" :r="c.items.length > 1 ? Math.max(14, dotRadius(c.n)) : dotRadius(c.n)" :fill="fill(c)"
                :fill-opacity="c.items.length > 1 ? 0.78 : 0.9" stroke="#fff" :stroke-width="c.items.length > 1 ? 3 : 1.5" />
              <text v-if="c.n >= 10 || c.items.length > 1" :x="c.x" :y="c.y + 4" text-anchor="middle" font-size="11.5" font-weight="700" fill="#fff">{{ c.n }}</text>
            </g>
            <circle v-if="sel" :cx="px([sel.lat, sel.lng]).x" :cy="px([sel.lat, sel.lng]).y" :r="dotRadius(sel.n) + 5" fill="none" stroke="#1f2328" stroke-width="2.5" />
          </g>
          <g v-if="showStores">
            <g v-for="s in data.stores" :key="s.id" :transform="`translate(${px([s.lat, s.lng]).x} ${px([s.lat, s.lng]).y})`" data-testid="map-store">
              <path d="M0 0C-4 -12 -15 -17 -15 -29A15 15 0 1 1 15 -29C15 -17 4 -12 0 0Z" fill="#d9480f" stroke="#fff" stroke-width="2" />
              <path d="M-8 -31h16l-2 -6h-12zM-7 -30v8h14v-8M-2 -22v-5h4v5" fill="none" stroke="#fff" stroke-width="1.8" stroke-linejoin="round" />
              <text v-if="data.stores.length <= 3" y="18" text-anchor="middle" font-size="12.5" font-weight="700" fill="#b93a08" stroke="#fff" stroke-width="3.5" paint-order="stroke">{{ s.name }}</text>
            </g>
          </g>
        </template>
        <template #ui>
          <div class="mapc-float dm-seg" role="group">
            <button v-for="m in (['pins', 'heat', 'both'] as const)" :key="m" type="button" :class="{ on: mode === m }" :data-testid="'mode-' + m" @click="mode = m">{{ t('da.mode.' + m) }}</button>
          </div>
          <div class="mapc-float dm-seg dm-type" role="group">
            <button type="button" :class="{ on: mapType === 'roadmap' }" @click="setType('roadmap')">{{ t('mz.map') }}</button>
            <button type="button" :class="{ on: mapType === 'satellite' }" @click="setType('satellite')">{{ t('mz.satellite') }}</button>
          </div>
          <div v-if="mode !== 'pins'" class="mapc-float dm-legend">
            <b class="small">{{ t('da.legend') }}</b>
            <div class="dm-grad" :style="{ background: HEAT_GRADIENT }"></div>
            <div class="row between small muted"><span>{{ t('da.less') }}</span><span>{{ t('da.more') }}</span></div>
          </div>
          <div v-if="sel && card" class="mapc-float dm-card" :style="card" data-testid="map-card" @click.stop>
            <div class="row between"><b class="grow">{{ sel.line }}</b><button type="button" class="btn link" :aria-label="t('da.close')" @click="sel = null">✕</button></div>
            <div class="row between small"><span class="muted">{{ t('da.col.orders') }}</span><b>{{ sel.n }}</b></div>
            <div class="row between small"><span class="muted">{{ t('da.col.customers') }}</span><span>{{ sel.c }}</span></div>
            <div class="row between small"><span class="muted">{{ t('da.col.last_once') }}</span><span>{{ when(sel.last_at) }}</span></div>
            <div class="row small dm-acts">
              <a :href="gmaps(sel)" target="_blank" rel="noopener">{{ t('da.open_gmaps') }}</a>
              <a href="#" @click.prevent="emit('rank', sel.key)">{{ t('da.see_top') }}</a>
            </div>
          </div>
        </template>
      </MapCanvas>
    </div>

    <aside class="dm-side">
      <div class="card">
        <h3>{{ t('da.show') }}</h3>
        <label class="row small"><input v-model="showStores" type="checkbox" data-testid="show-stores" />{{ t('da.show_stores') }}</label>
        <label v-if="!admin" class="row small"><input v-model="showZones" type="checkbox" data-testid="show-zones" />{{ t('da.show_zones') }}</label>
        <label v-if="brandList.length > 1" class="row small"><input v-model="byBrand" type="checkbox" data-testid="color-brands" />{{ t('br.map_color') }}</label>
        <div v-if="colorBy" class="dm-brands" data-testid="map-brands">
          <span v-for="b in brandList" :key="b.id" class="row small"><i class="dm-dot" :style="{ background: b.color || '#a02c20' }"></i><span class="grow ell">{{ b.name }}</span><b>{{ b.n }}</b></span>
        </div>
        <template v-if="mode !== 'pins'">
          <div class="small muted dm-gap">{{ t('da.radius') }}</div>
          <input v-model.number="radius" type="range" min="12" max="70" step="2" data-testid="heat-radius" />
          <div class="row between small muted"><span>{{ t('da.radius_small') }}</span><span>{{ t('da.radius_big') }}</span></div>
        </template>
      </div>
      <div v-if="top5.length" class="card dm-hide-narrow">
        <h3>{{ t('da.top5') }}</h3>
        <div v-for="(p, i) in top5" :key="p.key" class="dm-top row" @click="focusKey(p.key)">
          <span class="rank" :class="'r' + (i + 1)">{{ i + 1 }}</span><span class="grow ell">{{ p.line }}</span><b>{{ p.n }}</b>
        </div>
      </div>
      <div v-if="data.unpinned" class="banner warn small" data-testid="map-unpinned">{{ t('da.unpinned', { n: data.unpinned }) }}</div>
      <div v-if="!data.too_many" class="small muted" data-testid="map-sum">{{ pts.length ? t('da.map_sum', { a: pts.length, n: pinnedOrders }) : t('da.no_points') }}</div>
    </aside>
  </div>
</template>

<style scoped>
.dm-wrap { display: grid; grid-template-columns: minmax(0, 1fr) 280px; gap: 12px; align-items: start; }
.dm-map { padding: 0; overflow: hidden; }
.dm-canvas { height: min(640px, calc(100vh - 230px)); min-height: 420px; }
.dm-seg { top: 12px; inset-inline-start: 12px; display: flex; overflow: hidden; }
.dm-seg button { border: 0; background: #fff; padding: 7px 12px; font: inherit; font-size: 13px; font-weight: 600; color: var(--muted); cursor: pointer; }
.dm-seg button.on { background: var(--brand); color: #fff; }
.dm-type { inset-inline-start: auto; inset-inline-end: 12px; }
.dm-legend { inset-inline-start: 12px; bottom: 24px; padding: 8px 10px; width: 190px; }
.dm-grad { height: 10px; border-radius: 5px; margin: 5px 0 3px; }
.dm-card { padding: 10px 12px; display: grid; gap: 4px; font-size: 13px; z-index: 3; }
.dm-card b.grow { overflow-wrap: anywhere; }
.dm-card .btn.link { padding: 0 2px; min-height: 0; }
.dm-acts { gap: 14px; border-top: 1px solid var(--line); padding-top: 6px; margin-top: 2px; font-weight: 600; }
.dm-side { display: grid; gap: 12px; grid-template-columns: minmax(0, 1fr); min-width: 0; }
.dm-side .card { display: grid; gap: 6px; grid-template-columns: minmax(0, 1fr); min-width: 0; }
.dm-side h3 { font-size: 14px; margin: 0 0 2px; }
.dm-side input[type=range] { width: 100%; accent-color: var(--brand); padding: 0; border: 0; }
.dm-gap { margin-top: 6px; }
.dm-brands { display: grid; gap: 4px; padding-inline-start: 4px; }
.dm-brands .row { gap: 6px; align-items: center; }
.dm-dot { width: 10px; height: 10px; border-radius: 50%; flex: none; }
.dm-top { gap: 8px; font-size: 13px; padding: 4px 0; border-bottom: 1px solid #f3f3f3; cursor: pointer; }
.dm-top:last-child { border: 0; }
.ell { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.rank { width: 22px; height: 22px; border-radius: 50%; display: inline-flex; align-items: center; justify-content: center; font-weight: 700; font-size: 11px; background: #f1f2f4; color: var(--muted); flex: none; }
.rank.r1 { background: var(--brand); color: #fff; } .rank.r2 { background: #e8590c; color: #fff; } .rank.r3 { background: #f08c00; color: #fff; }
@media (max-width: 1023px) {
  .dm-wrap { grid-template-columns: 1fr; }
  .dm-canvas { height: 460px; }
  .dm-hide-narrow { display: none; }
}
@media (max-width: 767px) {
  .dm-canvas { height: 420px; min-height: 0; }
  .dm-seg button { padding: 6px 9px; }
  .dm-legend { width: 150px; }
}
</style>
