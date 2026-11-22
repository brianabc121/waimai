<script setup lang="ts">
// One trip on the map: the route the rider rode and the route the system
// would take, with the drops, the comparison and what happened when.
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { get } from '../../api'
import { t } from '../../i18n'
import { mapsKey as loadMapsKey } from '../../maps'
import { session } from '../../session'
import type { LatLng, Pt, View } from '../../geo'
import MapCanvas from '../MapCanvas.vue'
import { clock } from './time'
import { km, signedKm, type Trip, tripState } from './trips'
import './strings'
import './report.css'

interface Detail extends Trip { timezone: string; rider_name: string; store: LatLng | null; actual_path: [number, number, number][]; track_gaps: number[]
  planned_path: LatLng[]; planned_order: number[]; path_forgotten: boolean; path_days: number; detour_percent: number; detour_meters: number }

const bid = session.branchId!
const route = useRoute()
const d = ref<Detail | null>(null)
const error = ref('')
const key = ref<string | null>(null)
const map = ref<InstanceType<typeof MapCanvas>>()
const ready = ref(false)
const showPlanned = ref(true)
const showActual = ref(true)
const at = ref(0) // seconds since the start, for the slider
const playing = ref(false)

async function load() {
  try { d.value = await get<Detail>(`/merchant/branches/${bid}/trips/${route.params.id}`); error.value = ''; at.value = duration.value }
  catch (e) { error.value = (e as Error).message }
}
void load()
void loadMapsKey().then(k => (key.value = k))
// A trip under way keeps moving.
const timer = window.setInterval(() => { if (d.value && d.value.status !== 'done') void load() }, 30000)
let player = 0
onBeforeUnmount(() => { window.clearInterval(timer); window.clearInterval(player) })

const tz = computed(() => d.value?.timezone ?? 'Asia/Manila')
const start = computed(() => d.value ? Date.parse(d.value.started_at) : 0)
const path = computed(() => d.value?.actual_path ?? [])
const duration = computed(() => {
  if (!d.value) return 0
  const end = d.value.ended_at ? (Date.parse(d.value.ended_at) - start.value) / 1000 : 0
  return Math.max(1, Math.round(end), path.value.length ? path.value[path.value.length - 1][2] : 0)
})
const stops = computed(() => (d.value?.orders ?? []).filter(o => o.lat != null && o.lng != null))
const points = computed<LatLng[]>(() => [...path.value.map(p => [p[0], p[1]] as LatLng), ...(d.value?.planned_path ?? []),
  ...stops.value.map(o => [o.lat!, o.lng!] as LatLng), ...(d.value?.store ? [d.value.store] : [])])
const initial = computed<View>(() => ({ center: d.value?.store ?? points.value[0] ?? [14.5995, 120.9842], zoom: 14 }))
let fitted = false
watch([ready, points], () => { if (ready.value && !fitted && points.value.length) { fitted = true; map.value?.fit(points.value, 17) } })

// The ridden path in runs: a recorded run is a solid line, a gap a dotted one.
const runs = computed(() => {
  const gaps = new Set(d.value?.track_gaps ?? [])
  const out: { gap: boolean; pts: LatLng[] }[] = []
  let cur: LatLng[] = []
  path.value.forEach((p, i) => {
    cur.push([p[0], p[1]])
    if (gaps.has(i) && i + 1 < path.value.length) {
      out.push({ gap: false, pts: cur })
      out.push({ gap: true, pts: [[p[0], p[1]], [path.value[i + 1][0], path.value[i + 1][1]]] })
      cur = []
    }
  })
  if (cur.length) out.push({ gap: false, pts: cur })
  return out
})
const line = (px: (l: LatLng) => Pt, pts: LatLng[]) => pts.map(p => { const q = px(p); return `${q.x.toFixed(1)},${q.y.toFixed(1)}` }).join(' ')
// where the rider was at the slider's time
const marker = computed<LatLng | null>(() => {
  const p = path.value
  if (!p.length) return null
  if (at.value <= p[0][2]) return [p[0][0], p[0][1]]
  for (let i = 1; i < p.length; i++) {
    if (at.value <= p[i][2]) {
      const span = p[i][2] - p[i - 1][2], f = span > 0 ? (at.value - p[i - 1][2]) / span : 1
      return [p[i - 1][0] + (p[i][0] - p[i - 1][0]) * f, p[i - 1][1] + (p[i][1] - p[i - 1][1]) * f]
    }
  }
  return [p[p.length - 1][0], p[p.length - 1][1]]
})
function play() {
  window.clearInterval(player)
  playing.value = !playing.value
  if (!playing.value) return
  if (at.value >= duration.value) at.value = 0
  const stepS = Math.max(1, duration.value / 200)
  player = window.setInterval(() => {
    at.value = Math.min(duration.value, at.value + stepS)
    if (at.value >= duration.value) { playing.value = false; window.clearInterval(player) }
  }, 50)
}
const time = (iso: string | null) => iso ? clock(iso, tz.value) : ''
const timeAt = (sec: number) => clock(start.value + sec * 1000, tz.value)

const overPct = computed(() => d.value && d.value.over_m !== null && d.value.compare_m ? Math.round(d.value.over_m * 100 / d.value.compare_m) : null)
const minutes = computed(() => d.value?.ended_at ? Math.round((Date.parse(d.value.ended_at) - start.value) / 60000) : null)
const no = (id: number) => d.value?.orders.find(o => o.order_id === id)?.public_no ?? '#' + id
const actualOrder = computed(() => (d.value?.orders ?? []).filter(o => o.drop_seq !== null).sort((a, b) => a.drop_seq! - b.drop_seq!))
const sameOrder = computed(() => !d.value?.planned_order.length || actualOrder.value.length !== d.value.planned_order.length
  || actualOrder.value.every((o, i) => o.order_id === d.value!.planned_order[i])
  // straight lines there and back are as long in either direction
  || (d.value.planned_source === 'straight' && actualOrder.value.every((o, i) => o.order_id === d.value!.planned_order[d.value!.planned_order.length - 1 - i])))
const plannedShown = computed(() => {
  const p = d.value?.planned_order ?? []
  const reversed = d.value?.planned_source === 'straight' && actualOrder.value.length === p.length && p.length > 1
    && actualOrder.value.every((o, i) => o.order_id === p[p.length - 1 - i])
  return reversed ? [...p].reverse() : p
})
const missingPlace = computed(() => (d.value?.orders ?? []).some(o => o.outcome !== 'removed' && (o.lat == null || o.lng == null)))

interface Ev { at: number; title: string; sub: string; warn?: boolean }
const events = computed<Ev[]>(() => {
  const x = d.value
  if (!x) return []
  const ev: Ev[] = [{ at: start.value, title: t('rr.ev.start'), sub: t('rr.ev.start_d', { n: x.orders.length }) }]
  if (x.left_store_at) ev.push({ at: Date.parse(x.left_store_at), title: t('rr.ev.left'), sub: t('rr.ev.left_d') })
  for (const o of x.orders) {
    if (!o.done_at) continue
    ev.push({ at: Date.parse(o.done_at), title: `${o.public_no} ${t('rr.ev.' + o.outcome)}`, sub: o.outcome === 'failed' ? o.fail_reason ?? '' : o.address ?? '', warn: o.outcome !== 'delivered' })
  }
  for (const g of x.track_gaps) {
    if (g + 1 >= path.value.length) continue
    const from = path.value[g][2], len = path.value[g + 1][2] - from
    ev.push({ at: start.value + from * 1000, title: t('rr.ev.gap', { n: Math.max(1, Math.round(len / 60)) }), sub: t('rr.ev.gap_d'), warn: true })
  }
  if (x.status === 'done') {
    const back = x.end_reason === 'returned' || (x.end_reason === 'next_trip' && x.returned_at)
    ev.push({ at: Date.parse(x.ended_at!), title: back ? t('rr.ev.back') : t('rr.end.' + x.end_reason), sub: t('rr.end_d.' + x.end_reason), warn: !back })
  }
  return ev.sort((a, b) => a.at - b.at)
})
</script>

<template>
  <div class="srp trip" data-testid="trip-page">
    <div class="triphead">
      <router-link class="btn small" to="/merchant/staff/riders" data-act="back">‹ {{ t('rr.back') }}</router-link>
      <template v-if="d">
        <h2>{{ d.rider_name }} · {{ d.day.slice(5) }}</h2>
        <span class="sub" data-testid="trip-sub">{{ t('rr.head_out', { t: time(d.left_store_at ?? d.started_at) }) }} · {{ d.returned_at ? t('rr.head_back', { t: time(d.returned_at) }) : tripState(d) }} · {{ t('rr.head_orders', { n: d.order_count }) }}</span>
      </template>
    </div>
    <div v-if="error" class="banner bad" style="margin:12px">{{ error }}</div>
    <div v-if="d" class="tripbody">
      <div class="tripmap">
        <MapCanvas v-if="key" ref="map" :maps-key="key" :initial="initial" data-testid="trip-map" @ready="ready = true">
          <template #default="{ px }">
            <polyline v-if="showPlanned && d.planned_path.length" :points="line(px, d.planned_path)" class="planned" data-testid="planned-line" />
            <template v-if="showActual">
              <polyline v-for="(r, i) in runs" :key="i" :points="line(px, r.pts)" :class="r.gap ? 'gapline' : 'actual'" :data-testid="r.gap ? 'gap-line' : 'actual-line'" />
            </template>
            <g v-if="d.store" :transform="`translate(${px(d.store).x},${px(d.store).y})`" data-testid="store-pin">
              <circle r="14" fill="#d9480f" stroke="#fff" stroke-width="2.5" /><text y="5" text-anchor="middle" font-size="13" font-weight="700" fill="#fff">店</text>
            </g>
            <g v-for="o in stops" :key="o.order_id" :transform="`translate(${px([o.lat!, o.lng!]).x},${px([o.lat!, o.lng!]).y})`" class="stoppin" :class="o.outcome || 'pending'" :data-stop="o.public_no">
              <circle r="13" /><text y="5" text-anchor="middle" class="n">{{ o.drop_seq ?? '·' }}</text>
              <text x="18" y="5" class="lbl">{{ o.public_no }}{{ o.done_at ? ' · ' + time(o.done_at) : '' }}</text>
            </g>
            <g v-if="showActual && marker" :transform="`translate(${px(marker).x},${px(marker).y})`" data-testid="trip-marker">
              <circle r="9" fill="#fff" stroke="#d9480f" stroke-width="3" /><circle r="3.5" fill="#d9480f" />
            </g>
          </template>
          <template #ui>
            <div v-if="path.length > 1" class="slider">
              <button type="button" class="btn small" data-act="play" @click="play">{{ playing ? '❚❚' : '▶' }}</button>
              <b data-testid="slider-time">{{ timeAt(at) }}</b>
              <input v-model.number="at" type="range" min="0" :max="duration" step="1" name="at">
              <span class="sub">{{ timeAt(duration) }}</span>
            </div>
          </template>
        </MapCanvas>
        <div v-else-if="key === ''" class="nomap" data-testid="trip-nomap">{{ t('rr.no_map') }}</div>
      </div>

      <aside class="tripside">
        <section>
          <h3>{{ t('rr.two_routes') }}</h3>
          <label class="rt"><input v-model="showPlanned" type="checkbox" name="planned" :disabled="!d.planned_path.length"><i class="sw planned"></i>
            <span>{{ t('rr.planned') }}<small v-if="d.planned_source === 'straight'" class="sub"> {{ t('rr.straight') }}</small>
              <small v-else-if="d.planned_source === 'google'" class="sub" data-testid="planned-by"> {{ t(d.planned_mode === 'DRIVE' ? 'rr.by_car' : 'rr.by_bike') }}</small></span>
            <b data-testid="planned-km">{{ d.compare_m === null ? '—' : km(d.compare_m) }}</b></label>
          <label class="rt"><input v-model="showActual" type="checkbox" name="actual" :disabled="!path.length"><i class="sw actual"></i>
            <span>{{ t('rr.actual') }}</span><b data-testid="actual-km">{{ d.actual_m === null ? '—' : km(d.actual_m) }}</b></label>
          <div v-if="d.over_m !== null" class="tiles">
            <div :class="{ warn: d.detour }" data-testid="over-km"><b>{{ signedKm(d.over_m) }}</b><span>{{ d.over_m > -50 ? t('rr.over') : t('rr.under') }}</span></div>
            <div :class="{ warn: d.detour }"><b>{{ overPct === null ? '—' : (overPct > 0 ? '+' : '') + overPct + '%' }}</b><span>{{ t('rr.vs_planned') }}</span></div>
            <div><b>{{ minutes === null ? '—' : t('rr.n_min', { n: minutes }) }}</b><span>{{ t('rr.took') }}</span></div>
          </div>
          <p v-if="d.detour" class="banner warn small" data-testid="detour-note">{{ t('rr.detour_note', { p: d.detour_percent, km: d.detour_meters / 1000 }) }}</p>
          <p v-if="!d.planned_path.length" class="sub" data-testid="no-planned">{{ !d.store ? t('rr.np.no_store') : missingPlace ? t('rr.np.no_place') : t('rr.np.not_yet') }}</p>
          <p v-if="d.planned_source === 'google'" class="sub" data-testid="road-note">{{ d.planned_seconds ? t('rr.planned_min', { n: Math.round(d.planned_seconds / 60) }) : '' }}{{ t(d.planned_mode === 'DRIVE' ? 'rr.car_note' : 'rr.bike_note') }}</p>
          <p v-if="d.planned_path.length && d.status === 'done' && !d.returned_at" class="sub">{{ t('rr.no_return_note') }}</p>
          <p v-if="d.gap_seconds" class="sub" data-testid="gap-note">{{ t('rr.gap_note') }}</p>
        </section>
        <section v-if="actualOrder.length">
          <h3>{{ t('rr.order_title') }}</h3>
          <div v-if="plannedShown.length" class="seq"><span class="sub">{{ t('rr.seq.planned') }}</span><span v-for="(id, i) in plannedShown" :key="id" class="chip p"><i>{{ i + 1 }}</i>{{ no(id) }}</span></div>
          <div class="seq"><span class="sub">{{ t('rr.seq.actual') }}</span><span v-for="o in actualOrder" :key="o.order_id" class="chip a"><i>{{ o.drop_seq }}</i>{{ o.public_no }}</span></div>
          <p v-if="!sameOrder" class="banner warn small" data-testid="order-differs">{{ t('rr.order_differs') }}</p>
        </section>
        <section>
          <h3>{{ t('rr.events') }}</h3>
          <ol class="events" data-testid="trip-events">
            <li v-for="(e, i) in events" :key="i" :class="{ warn: e.warn }"><time>{{ clock(e.at, tz) }}</time><span><b>{{ e.title }}</b><template v-if="e.sub"> · {{ e.sub }}</template></span></li>
          </ol>
        </section>
        <section class="sub">
          <template v-if="d.path_forgotten">{{ t('rr.forgotten', { n: d.path_days }) }}</template>
          <template v-else-if="!d.track_points">{{ t('rr.no_track') }}</template>
          <template v-else>{{ t('rr.track_note', { n: d.track_points }) }}</template>
        </section>
      </aside>
    </div>
  </div>
</template>

<style scoped>
.trip { display: flex; flex-direction: column; height: calc(100vh - var(--shell-top, 0px)); min-height: 520px; }
.triphead { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; padding: 10px 16px; background: #fff; border-bottom: 1px solid var(--line); }
.triphead h2 { margin: 0; font-size: 17px; }
.tripbody { flex: 1; display: grid; grid-template-columns: minmax(0, 1fr) 380px; min-height: 0; }
.tripmap { position: relative; min-height: 320px; background: #eef0f2; } .tripmap :deep(.mapc) { position: absolute; inset: 0; }
.nomap { position: absolute; inset: 0; display: flex; align-items: center; justify-content: center; padding: 24px; text-align: center; color: var(--muted); }
.tripside { background: #fff; border-left: 1px solid var(--line); overflow-y: auto; }
.tripside section { padding: 14px 18px; border-bottom: 1px solid var(--line); } .tripside section:last-child { border-bottom: 0; }
@media (max-width: 900px) { .trip { height: auto; } .tripbody { grid-template-columns: 1fr; } .tripmap { height: 60vh; } .tripside { border-left: 0; } }
.planned { fill: none; stroke: #1d4ed8; stroke-width: 4; stroke-dasharray: 10 7; stroke-linejoin: round; }
.actual { fill: none; stroke: #d9480f; stroke-width: 4.5; stroke-linejoin: round; stroke-linecap: round; }
.gapline { fill: none; stroke: #d9480f; stroke-width: 3.5; stroke-dasharray: 1 8; stroke-linecap: round; }
.stoppin circle { fill: #1f2328; stroke: #fff; stroke-width: 2.5; } .stoppin.failed circle { fill: #b45309; } .stoppin.pending circle { fill: #6b7280; }
.stoppin .n { fill: #fff; font-size: 13px; font-weight: 700; }
.stoppin .lbl { font-size: 13px; font-weight: 700; fill: #1f2328; paint-order: stroke; stroke: #fff; stroke-width: 4px; stroke-linejoin: round; }
.slider { position: absolute; left: 12px; right: 64px; bottom: 26px; display: flex; align-items: center; gap: 10px; background: #fff; border-radius: 10px; padding: 7px 12px; box-shadow: 0 2px 10px rgba(0,0,0,.18); z-index: 2; }
.slider input { flex: 1; accent-color: var(--brand); } .slider b { font-variant-numeric: tabular-nums; }
.rt { display: flex; align-items: center; gap: 9px; padding: 5px 0; cursor: pointer; } .rt span { flex: 1; } .rt b { font-variant-numeric: tabular-nums; }
.sw { width: 30px; height: 0; border-top: 4px solid #d9480f; } .sw.planned { border-top: 4px dashed #1d4ed8; }
.tiles { display: grid; grid-template-columns: repeat(3, 1fr); gap: 8px; margin: 10px 0; }
.tiles div { background: #f6f6f4; border-radius: 10px; padding: 9px 6px; text-align: center; } .tiles b { display: block; font-size: 16px; font-variant-numeric: tabular-nums; }
.tiles span { font-size: 12px; color: var(--muted); } .tiles .warn b { color: var(--warn); }
.seq { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; margin-bottom: 7px; }
.chip { display: inline-flex; align-items: center; gap: 5px; border: 1px solid var(--line); border-radius: 999px; padding: 1px 9px 1px 2px; font-size: 13px; }
.chip i { font-style: normal; width: 18px; height: 18px; border-radius: 50%; color: #fff; font-size: 11px; font-weight: 700; display: inline-flex; align-items: center; justify-content: center; }
.chip.p i { background: #1d4ed8; } .chip.a i { background: #d9480f; }
.events { list-style: none; margin: 0; padding: 0; } .events li { display: flex; gap: 12px; padding: 4px 0; font-size: 14px; }
.events time { color: var(--muted); font-variant-numeric: tabular-nums; flex: none; width: 42px; } .events li.warn b { color: var(--warn); }
p.small { margin: 8px 0 0; }
</style>
