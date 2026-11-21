<script setup lang="ts">
// "Where to deliver": the customer moves the map under a fixed pin; the
// server says which delivery area that point belongs to. Loaded only when a
// customer opens it, so the menu itself never loads the map.
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { post } from '../api'
import { extend, money, pick, t } from '../i18n'
import { cart } from '../cart'
import { mapsKey, type Place } from '../maps'
import type { LatLng, View } from '../geo'
import type { Located, Storefront } from '../types'
import MapCanvas from './MapCanvas.vue'

extend({
  'dp.title': ['送到哪里', 'Deliver to'],
  'dp.search': ['搜索地址', 'Search an address'],
  'dp.no_hits': ['没有找到这个地址', 'No results'],
  'dp.hint': ['拖动地图，把图钉对准你家或公司门口。', 'Move the map until the pin sits on your door.'],
  'dp.ok': ['可以配送 · {name}', 'We deliver here · {name}'],
  'dp.inside': ['范围内', 'In area'], 'dp.near': ['边界附近', 'Near the edge'], 'dp.outside': ['范围外', 'Out of range'],
  'dp.fee': ['配送费 {fee} · 起送 {min}', 'Fee {fee} · min. {min}'], 'dp.eta': [' · 约 {n} 分钟', ' · ~{n} min'],
  'dp.near_hint': ['你的位置离“{name}”配送区很近，按这个区域配送。', 'Your pin is just outside "{name}"; that area\'s terms apply.'],
  'dp.out': ['这个位置不在配送范围', 'We do not deliver to this spot'],
  'dp.out_near': ['离最近的配送区“{name}”约 {d} 米，超出可配送距离', 'The nearest area, "{name}", is about {d} m away'],
  'dp.out_hint': ['可以把图钉移到彩色范围里的地点，或者改成到店自取。', 'Move the pin into a coloured area, or switch to pickup.'],
  'dp.to_pickup': ['改为到店自取', 'Switch to pickup'],
  'dp.confirm': ['确认送到这里', 'Deliver here'],
  'dp.checking': ['正在判断配送区域…', 'Checking the delivery area…'],
  'dp.failed': ['暂时无法判断，请稍后再试', 'Could not check right now, please try again'],
  'dp.me': ['用我现在的位置', 'Use my current location'],
  'dp.me_denied': ['没有拿到定位，请在地图上手动选位置', 'Location is unavailable; please place the pin by hand'],
  'dp.unit': ['楼层 / 门牌', 'Floor / unit'], 'dp.note': ['给骑手的备注（选填）', 'Note for the rider (optional)'],
})

const props = defineProps<{ store: Storefront }>()
const emit = defineEmits<{ close: []; pickup: [] }>()

const COLORS = ['#2a78d6', '#18794e', '#7c3aed', '#c2255c', '#e0a100', '#0b7285']
const key = ref<string | null>(null)
const map = ref<InstanceType<typeof MapCanvas>>()
const cur = computed(() => props.store.branch.currency)
const zones = computed(() => props.store.zones.filter(z => z.polygon.length >= 3))
const shop = computed<LatLng | null>(() => props.store.branch.lat != null && props.store.branch.lng != null ? [props.store.branch.lat, props.store.branch.lng] : null)
const initial = computed<View>(() => ({
  center: cart.lat != null && cart.lng != null ? [cart.lat, cart.lng] : shop.value ?? zones.value[0]?.polygon[0] ?? [0, 0], zoom: 16 }))

const at = ref<LatLng | null>(null)
const result = ref<Located | null>(null)
const busy = ref(false), failed = ref(false), note = ref('')
const unit = ref(cart.addressUnit), riderNote = ref(cart.addressNote)
const label = ref(cart.placeLabel)
let timer = 0, seq = 0

onMounted(async () => { key.value = await mapsKey() })
onBeforeUnmount(() => clearTimeout(timer))

// The pin is the centre of the map: every time the map settles, ask again.
function settle() {
  const c = map.value?.view.center
  if (!c) return
  at.value = [c[0], c[1]]
  busy.value = true
  clearTimeout(timer)
  timer = window.setTimeout(check, 350)
}
watch(() => map.value?.view, settle)
async function check() {
  const p = at.value, n = ++seq
  if (!p) return
  try {
    const r = await post<Located>(`/stores/${props.store.branch.slug}/locate`, { lat: p[0], lng: p[1] })
    if (n !== seq) return
    result.value = r; failed.value = false
  } catch {
    if (n !== seq) return
    result.value = null; failed.value = true
  }
  busy.value = false
}

const q = ref(''), hits = ref<Place[] | null>(null)
async function search() {
  if (!q.value.trim()) { hits.value = null; return }
  hits.value = await map.value!.search(q.value)
  if (hits.value.length === 1) go(hits.value[0])
}
function go(p: Place) {
  hits.value = null; label.value = p.label; q.value = p.label
  map.value!.setView({ center: p.at, zoom: Math.max(17, map.value!.view.zoom) })
}
function locateMe() {
  note.value = ''
  if (!navigator.geolocation) { note.value = t('dp.me_denied'); return }
  navigator.geolocation.getCurrentPosition(
    p => map.value?.setView({ center: [p.coords.latitude, p.coords.longitude], zoom: Math.max(17, map.value.view.zoom) }),
    () => { note.value = t('dp.me_denied') }, { enableHighAccuracy: true, timeout: 10000 })
}

const zone = computed(() => result.value?.zone ?? null)
function confirm() {
  if (!zone.value || !at.value || busy.value) return
  Object.assign(cart, { lat: at.value[0], lng: at.value[1], zoneId: zone.value.id, placeLabel: label.value,
    addressUnit: unit.value.trim(), addressNote: riderNote.value.trim() })
  // A searched address is a good first guess for the street line.
  if (!cart.addressLine && label.value) cart.addressLine = label.value
  emit('close')
}
const poly = (ps: LatLng[], px: (l: LatLng) => { x: number; y: number }) => ps.map(p => { const s = px(p); return `${s.x.toFixed(1)},${s.y.toFixed(1)}` }).join(' ')
const colorOf = (i: number, c: string) => c || COLORS[i % COLORS.length]
</script>

<template>
  <div class="sheet-mask" data-testid="picker">
    <div class="sheet picker">
      <div class="sheet-head row"><button class="btn link" style="font-size:20px;padding:0 10px 0 0" :aria-label="t('back')" @click="emit('close')">‹</button><h2 style="margin:0">{{ t('dp.title') }}</h2></div>
      <div class="sheet-body stack">
        <MapCanvas v-if="key" ref="map" class="pinmap" :maps-key="key" :initial="initial" data-testid="pin-map" @ready="settle">
          <template #default="{ px }">
            <g v-for="(z, i) in [...zones].reverse()" :key="z.id">
              <polygon :points="poly(z.polygon, px)" :fill="colorOf(zones.indexOf(z), z.color)" fill-opacity="0.18" :stroke="colorOf(zones.indexOf(z), z.color)" stroke-width="2" stroke-linejoin="round" :data-i="i" />
            </g>
            <g v-if="shop" :transform="`translate(${px(shop).x} ${px(shop).y})`">
              <path d="M0 0C-3 -9 -11 -13 -11 -22A11 11 0 1 1 11 -22C11 -13 3 -9 0 0Z" fill="#d9480f" stroke="#fff" stroke-width="2" />
              <text y="15" text-anchor="middle" font-size="11.5" font-weight="700" fill="#b93a08" stroke="#fff" stroke-width="3" paint-order="stroke">{{ pick(store.branch.name, store.branch.name_en || store.branch.name) }}</text>
            </g>
          </template>
          <template #ui>
            <svg class="pinmap-pin" width="34" height="44" viewBox="-17 -40 34 44">
              <ellipse cx="0" cy="1" rx="9" ry="3" fill="#000" opacity="0.25" />
              <path d="M0 0C-4 -12 -15 -17 -15 -26A15 15 0 1 1 15 -26C15 -17 4 -12 0 0Z" :fill="result && !zone ? '#b42318' : '#1d4ed8'" stroke="#fff" stroke-width="2" /><circle cy="-26" r="5" fill="#fff" />
            </svg>
            <div class="mapc-float pinmap-search">
              <form @submit.prevent="search"><span>🔍</span><input v-model="q" :placeholder="t('dp.search')" data-testid="pin-search" /></form>
              <div v-if="hits" class="hits"><div v-for="(h, i) in hits" :key="i" @click="go(h)">{{ h.label }}</div><div v-if="!hits.length" class="muted">{{ t('dp.no_hits') }}</div></div>
            </div>
            <button type="button" class="mapc-float pinmap-me" :title="t('dp.me')" :aria-label="t('dp.me')" @click="locateMe">◎</button>
          </template>
        </MapCanvas>
        <div v-else class="pinmap muted" style="display:grid;place-items:center">{{ t('dp.checking') }}</div>

        <div v-if="note" class="banner warn">{{ note }}</div>
        <div v-if="failed" class="banner bad">{{ t('dp.failed') }} <button class="btn link" @click="check">{{ t('retry') }}</button></div>
        <div v-else-if="!result" class="locresult muted">{{ t('dp.checking') }}</div>
        <template v-else-if="zone">
          <div class="locresult in" :style="{ opacity: busy ? 0.55 : 1 }" data-testid="pin-result" :data-zone="zone.name" :data-state="result.distance_m ? 'near' : 'in'">
            <div class="row between"><b>{{ t('dp.ok', { name: pick(zone.name, zone.name_en || zone.name) }) }}</b>
              <span class="tag" :class="result.distance_m ? 'warn' : 'ok'">{{ t(result.distance_m ? 'dp.near' : 'dp.inside') }}</span></div>
            <div class="small muted" style="margin-top:4px">{{ t('dp.fee', { fee: money(zone.fee, cur), min: money(zone.min_order, cur) }) }}<template v-if="zone.eta_minutes">{{ t('dp.eta', { n: zone.eta_minutes }) }}</template></div>
          </div>
          <p class="small muted" style="margin:0">{{ result.distance_m ? t('dp.near_hint', { name: pick(zone.name, zone.name_en || zone.name) }) : t('dp.hint') }}</p>
          <label class="field" style="margin:0"><span>{{ t('dp.unit') }}</span><input v-model="unit" maxlength="100" /></label>
          <label class="field" style="margin:0"><span>{{ t('dp.note') }}</span><input v-model="riderNote" maxlength="300" /></label>
        </template>
        <template v-else>
          <div class="locresult out" :style="{ opacity: busy ? 0.55 : 1 }" data-testid="pin-result" data-state="out">
            <div class="row between"><b>{{ t('dp.out') }}</b><span class="tag">{{ t('dp.outside') }}</span></div>
            <div v-if="result.nearest" class="small" style="margin-top:4px">{{ t('dp.out_near', { name: pick(result.nearest.name, result.nearest.name_en || result.nearest.name), d: result.nearest.distance_m }) }}</div>
          </div>
          <p class="small muted" style="margin:0">{{ t('dp.out_hint') }}</p>
          <button v-if="store.branch.pickup_enabled" type="button" class="btn block" data-testid="pin-pickup" @click="emit('pickup')">{{ t('dp.to_pickup') }}</button>
        </template>
        <button type="button" class="btn primary block" :disabled="!zone || busy" data-testid="pin-confirm" @click="confirm">{{ t('dp.confirm') }}</button>
      </div>
    </div>
  </div>
</template>
