<script setup lang="ts">
// Riders as the order desk sees them: who is on duty, where they are and what
// they carry. The list is in the order the desk should pick from; the map
// appears when the platform has a map key.
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { get } from '../api'
import { minutesSince, t } from '../i18n'
import { useLive } from '../live'
import { session } from '../session'
import { mapsKey as loadMapsKey } from '../maps'
import type { LatLng, View } from '../geo'
import type { RiderInfo } from '../types'
import MapCanvas from './MapCanvas.vue'
import './work/strings'
import '../officeStrings'

const bid = session.branchId!
const riders = ref<RiderInfo[]>([])
const store = ref<LatLng | null>(null)
const key = ref<string | null>(null)
const map = ref<InstanceType<typeof MapCanvas>>()
const picked = ref<number | null>(null)
const now = ref(Date.now())

async function load() {
  const r = await get<{ riders: RiderInfo[]; store: LatLng | null }>(`/merchant/branches/${bid}/riders`)
  riders.value = r.riders; store.value = r.store; now.value = Date.now()
}
useLive(() => `/merchant/branches/${bid}/events`, load)
// Positions change without any order changing, so also ask every 15 seconds.
const timer = window.setInterval(() => void load().catch(() => {}), 15000)
onBeforeUnmount(() => window.clearInterval(timer))
void loadMapsKey().then(k => (key.value = k))

const placed = computed(() => riders.value.filter(r => r.lat != null && r.lng != null))
const at = (r: RiderInfo): LatLng => [r.lat!, r.lng!]
const DEFAULT: LatLng = [14.5995, 120.9842]
const initial = computed<View>(() => ({ center: store.value ?? (placed.value[0] ? at(placed.value[0]) : DEFAULT), zoom: 14 }))
const ready = ref(false)
function fit() {
  const pts = [...placed.value.map(at), ...(store.value ? [store.value] : [])]
  if (pts.length > 1) map.value?.fit(pts, 16)
  else if (pts.length) map.value?.setView({ center: pts[0], zoom: 15 })
}
// Fit once when both the map and the first positions are there; after that the desk pans as it likes.
let fitted = false
watch([ready, placed], () => { if (ready.value && !fitted && (placed.value.length || store.value)) { fitted = true; fit() } })
function focus(r: RiderInfo) { picked.value = r.id; if (r.lat != null) map.value?.setView({ center: at(r), zoom: 16 }) }

const km = (m: number) => m < 1000 ? `${Math.round(m / 10) * 10} m` : `${(m / 1000).toFixed(1)} km`
const ago = (iso: string) => { void now.value; const n = minutesSince(iso); return n < 1 ? t('rd.just_now') : t('rd.min_ago', { n }) }
const stateText = (r: RiderInfo) => !r.on_duty ? t('st.now.off_duty') : r.contact === 'lost' ? t('rd.lost') : r.contact === 'none' ? t('rd.no_position') : t('st.now.on_duty')
const stateClass = (r: RiderInfo) => !r.on_duty ? '' : r.contact === 'ok' ? 'ok' : 'warn'
const onDuty = computed(() => riders.value.filter(r => r.on_duty).length)
const carrying = computed(() => riders.value.filter(r => r.active_deliveries > 0).length)
const lost = computed(() => riders.value.filter(r => r.on_duty && r.contact !== 'ok').length)
const pillClass = (r: RiderInfo) => !r.on_duty ? '' : r.contact === 'ok' ? 'ok' : 'warn'
</script>

<template>
  <div class="ox warm">
  <div class="page ox-page riders-page">
    <div class="ox-head">
      <div><h1>{{ t('riders') }}</h1><p class="ox-sub">{{ t('ox.rd.sub') }}</p></div>
      <div class="ox-acts"><button v-if="key" type="button" class="btn" @click="fit">{{ t('rd.fit') }}</button></div>
    </div>

    <div class="ox-stats" data-testid="riders-stats">
      <div class="ox-stat"><span class="l">{{ t('ox.rd.all') }}</span><b class="v">{{ riders.length }}</b></div>
      <div class="ox-stat"><span class="l">{{ t('st.now.on_duty') }}</span><b class="v">{{ onDuty }}</b></div>
      <div class="ox-stat"><span class="l">{{ t('ox.rd.carrying') }}</span><b class="v" :class="{ red: carrying > 0 }">{{ carrying }}</b></div>
      <div class="ox-stat"><span class="l">{{ t('ox.rd.lost') }}</span><b class="v" :class="{ red: lost > 0 }">{{ lost }}</b></div>
    </div>

  <div class="riders" :class="{ nomap: !key }" data-testid="riders-page">
    <MapCanvas v-if="key" ref="map" :maps-key="key" :initial="initial" class="riders-map" data-testid="riders-map" @ready="ready = true">
      <template #default="{ px }">
        <g v-if="store" :transform="`translate(${px(store).x},${px(store).y})`" data-testid="store-pin">
          <circle r="13" fill="#fff" stroke="#a02c20" stroke-width="3" /><text y="5" text-anchor="middle" font-size="13" font-weight="700" fill="#a02c20">{{ t('ic.store') }}</text>
        </g>
        <g v-for="r in placed" :key="r.id" :transform="`translate(${px(at(r)).x},${px(at(r)).y})`" class="rpin" :class="[r.contact, { on: picked === r.id }]"
          :data-rider-pin="r.username" @click.stop="picked = r.id">
          <circle r="16" /><text y="5" text-anchor="middle">{{ r.display_name.slice(0, 1) }}</text>
          <g v-if="r.active_deliveries" transform="translate(13,-13)"><circle r="9" class="badge" /><text y="4" text-anchor="middle" class="bt">{{ r.active_deliveries }}</text></g>
        </g>
      </template>
    </MapCanvas>

    <aside class="ox-panel flush riders-list">
      <div class="ox-panel-h rl-h"><h2>{{ t('rd.title') }}</h2><span class="grow"></span><span class="muted small">{{ t('rd.count', { on: onDuty, all: riders.length }) }}</span></div>
      <p v-if="!key" class="muted small rl-p">{{ t('rd.no_map') }}</p>
      <p v-else-if="!store" class="muted small rl-p">{{ t('rd.no_store') }}</p>
      <div v-if="!riders.length" class="ox-empty">{{ t('rd.empty') }}</div>
      <button v-for="r in riders" :key="r.id" type="button" class="rrow" :class="{ on: picked === r.id, off: !r.on_duty }" :data-rider="r.username" @click="focus(r)">
        <span class="av" :class="stateClass(r)">{{ r.display_name.slice(0, 1) }}</span>
        <span class="rr-b">
          <span class="rr-t"><b class="ox-strong">{{ r.display_name }}</b><span class="tag ox-pill" :class="pillClass(r)">{{ stateText(r) }}</span></span>
          <span class="small muted">
            <template v-if="r.active_deliveries">{{ t('rd.carrying', { n: r.active_deliveries }) }}</template><template v-else>{{ t('rd.free') }}</template>
            <template v-if="r.distance_m != null"> · {{ t('rd.from_store', { d: km(r.distance_m) }) }}</template>
            <template v-if="r.located_at"> · {{ ago(r.located_at) }}</template>
            <template v-if="r.battery != null"> · {{ t('rd.battery', { n: r.battery }) }}</template>
          </span>
          <span v-if="r.phone" class="small"><a :href="'tel:' + r.phone" @click.stop>{{ r.phone }}</a></span>
        </span>
      </button>
      <p class="ox-tip rl-tip">{{ t('rd.hint') }}</p>
    </aside>
  </div>
  </div>
  </div>
</template>

<style>
.riders-page { min-height: calc(100vh - var(--shell-top, 0px)); }
.riders { display: grid; grid-template-columns: minmax(0, 1fr) 360px; gap: 14px; height: calc(100vh - var(--shell-top, 0px) - 230px); min-height: 460px; }
.riders.nomap { grid-template-columns: minmax(0, 620px); height: auto; min-height: 0; }
.riders .riders-map { border-radius: 12px; overflow: hidden; border: 1px solid var(--line); }
.riders-list { overflow: auto; }
.riders .rl-h { padding: 14px 16px 4px; margin: 0; }
.riders .rl-p { padding: 0 16px; }
.riders .rl-tip { margin: 12px 14px 14px; }
.riders .rrow { display: flex; gap: 12px; align-items: flex-start; width: 100%; text-align: start; border: 0; border-top: 1px solid #f0e6d8; background: none; padding: 12px 16px; cursor: pointer; font: inherit; color: inherit;
  border-inline-start: 3px solid transparent; }
.riders .rrow:hover { background: #fbf4ea; }
.riders .rrow.on { background: var(--pink); border-inline-start-color: var(--brand); } .riders .rrow.off b { color: var(--muted); }
.riders .rrow .av { width: 38px; height: 38px; border-radius: 50%; display: inline-grid; place-items: center; color: #fff; font-weight: 700; background: #b9aa9a; flex: none; }
.riders .rrow .av.ok { background: #2f7a2a; } .riders .rrow .av.warn { background: #c8641e; }
.riders .rr-b { display: grid; gap: 3px; min-width: 0; flex: 1; }
.riders .rr-t { display: flex; justify-content: space-between; gap: 8px; align-items: center; }
.riders .rpin { cursor: pointer; } .riders .rpin circle { fill: #2f7a2a; stroke: #fff; stroke-width: 2.5; }
.riders .rpin.lost circle { fill: #8a7464; } .riders .rpin.on > circle { stroke: #2f1810; }
.riders .rpin text { fill: #fff; font-size: 14px; font-weight: 700; pointer-events: none; }
.riders .rpin .badge { fill: #a02c20; stroke: #fff; stroke-width: 1.5; } .riders .rpin .bt { font-size: 11px; }
@media (max-width: 860px) { .riders { grid-template-columns: 1fr; height: auto; } .riders .mapc { height: 50vh; } }
</style>
