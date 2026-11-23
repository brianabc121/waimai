<script setup lang="ts">
// Choosing where to deliver. The pin stays in the middle of the map; the
// customer either types and picks a suggestion, or moves the map and picks one
// of the places around the pin. What is kept is the pin's position: the name
// is only what people read. The store's delivery areas are never drawn here;
// the customer is told whether the spot can be delivered to and what it costs.
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { termsAt, type DeliveryTerms } from '../addresses'
import { money, t } from '../i18n'
import { mapsKey, storeScope, type Place, type Region } from '../maps'
import type { LatLng, View } from '../geo'
import MapCanvas from './MapCanvas.vue'

// region: the country and region at the point (in English), when it was asked for
export interface Picked { lat: number; lng: number; name: string; address: string; region?: Region | null }

const props = defineProps<{
  // the store whose delivery is asked about; without one nothing is said about delivery
  slug?: string; currency?: string
  // where to start: a saved pin, else the store, else wherever the map opens
  start?: { lat: number; lng: number } | null; name?: string; shop?: { lat: number; lng: number; name: string; country?: string } | null
  // embedded: shown beside a form, with no header of its own
  embedded?: boolean; pickup?: boolean
  // keepable: an out-of-range spot may still be chosen (for the address book)
  keepable?: boolean
  // region: also say which country and region the point is in
  region?: boolean
}>()
const emit = defineEmits<{ done: [p: Picked, terms: DeliveryTerms | null]; close: []; pickup: [] }>()

const key = ref<string | null>(null)
const map = ref<InstanceType<typeof MapCanvas>>()
const initial = computed<View>(() => ({
  center: props.start ? [props.start.lat, props.start.lng] : props.shop ? [props.shop.lat, props.shop.lng] : [14.5547, 121.0244], zoom: 17 }))

const at = ref<LatLng | null>(null)
const terms = ref<DeliveryTerms | null>(null)
const checking = ref(false), failed = ref(false), note = ref('')
const q = ref(''), typing = ref(false)
const list = ref<Place[]>([]), looking = ref(false)
const chosen = ref<Place | null>(null)
// A place the customer picked by name: when the map settles on it, it stays chosen.
let kept: Place | null = props.name ? { label: props.name } : null
let settleTimer = 0, typeTimer = 0, seq = 0, listSeq = 0

onMounted(async () => { key.value = await mapsKey() })
onBeforeUnmount(() => { clearTimeout(settleTimer); clearTimeout(typeTimer) })

// Searches stay in the store's country and look near the store first: nobody
// orders from a store in Manila to an address in New York.
async function mapReady() {
  moved()
  if (!props.shop || !map.value) return
  const s = await storeScope(props.shop, p => map.value?.resolve(p) ?? Promise.resolve(null))
  map.value?.scope(s)
}

// Every time the map comes to rest, the pin is somewhere new.
function moved() {
  const c = map.value?.view.center
  if (!c) return
  if (at.value && Math.abs(at.value[0] - c[0]) < 1e-7 && Math.abs(at.value[1] - c[1]) < 1e-7) return
  at.value = [c[0], c[1]]
  if (props.slug) checking.value = true
  clearTimeout(settleTimer)
  settleTimer = window.setTimeout(settled, 500)
}
watch(() => map.value?.view, moved)

async function settled() {
  const p = at.value
  if (!p) return
  void check(p)
  if (!typing.value) await around(p)
}
async function check(p: LatLng) {
  if (!props.slug) return
  const n = ++seq
  try {
    const r = await termsAt(props.slug, p[0], p[1])
    if (n !== seq) return
    terms.value = r; failed.value = false
  } catch {
    if (n !== seq) return
    terms.value = null; failed.value = true
  }
  checking.value = false
}
async function around(p: LatLng) {
  const n = ++listSeq
  looking.value = true
  const found = await map.value!.nearby(p).catch(() => [] as Place[])
  if (n !== listSeq) return
  looking.value = false
  const places = found.filter(x => x.distance_m !== 0), street = found.find(x => x.distance_m === 0)
  const pin: Place = { label: t('pp.pin_only'), detail: street?.label ?? `${p[0].toFixed(5)}, ${p[1].toFixed(5)}`, at: p, distance_m: 0 }
  const head = kept && !places.some(x => x.label === kept!.label) ? [{ ...kept, at: p }] : []
  list.value = [...head, ...places, pin]
  chosen.value = (kept && list.value.find(x => x.label === kept!.label)) || list.value[0]
  kept = null
}

function onType() {
  clearTimeout(typeTimer)
  const text = q.value.trim()
  if (text.length < 2) { typing.value = false; if (at.value) void around(at.value); return }
  typing.value = true
  typeTimer = window.setTimeout(async () => {
    const n = ++listSeq
    looking.value = true
    const found = await map.value!.suggest(text).catch(() => [] as Place[])
    if (n !== listSeq) return
    looking.value = false
    list.value = found; chosen.value = null
  }, 300)
}
async function pick(p: Place) {
  if (!typing.value) { chosen.value = p; return }
  // a suggestion: go there, then offer what is around it with this one chosen
  const where = p.at ?? await p.locate?.() ?? null
  if (!where) return
  kept = { label: p.label, detail: p.detail }
  typing.value = false; q.value = ''
  list.value = []; chosen.value = null
  map.value!.setView({ center: where, zoom: Math.max(17, map.value!.view.zoom) })
}
function enter() { if (typing.value && list.value.length) void pick(list.value[0]) }

function locateMe() {
  note.value = ''
  if (!navigator.geolocation) { note.value = t('pp.me_denied'); return }
  navigator.geolocation.getCurrentPosition(
    p => map.value?.setView({ center: [p.coords.latitude, p.coords.longitude], zoom: Math.max(17, map.value.view.zoom) }),
    () => { note.value = t('pp.me_denied') }, { enableHighAccuracy: true, timeout: 10000 })
}

const blocked = computed(() => !!props.slug && !props.keepable && (!terms.value || !terms.value.deliverable))
const ready = computed(() => !!at.value && !!chosen.value && !typing.value && !checking.value && !blocked.value)
const resolving = ref(false)
async function done() {
  if (!ready.value || !at.value || !chosen.value || resolving.value) return
  const c = chosen.value, p = at.value
  const pinOnly = c.label === t('pp.pin_only')
  const out: Picked = { lat: p[0], lng: p[1], name: pinOnly ? (c.detail || c.label) : c.label, address: pinOnly ? '' : (c.detail ?? '') }
  if (props.region) {
    resolving.value = true
    out.region = await (map.value?.resolve(p) ?? Promise.resolve(null)).catch(() => null)
    resolving.value = false
  }
  emit('done', out, terms.value)
}
const state = computed(() => checking.value || !terms.value ? 'checking' : terms.value.deliverable ? 'in' : 'out')
</script>

<template>
  <div class="ppick" :class="{ embedded }" data-testid="picker">
    <div v-if="!embedded" class="pp-head"><button class="pp-back" :aria-label="t('back')" data-testid="pin-back" @click="emit('close')"><span class="rtl-flip">‹</span></button><b>{{ t('pp.title') }}</b></div>
    <MapCanvas v-if="key" ref="map" class="pp-map" :maps-key="key" :initial="initial" data-testid="pin-map" @ready="mapReady">
      <template #default="{ px }">
        <g v-if="shop" :transform="`translate(${px([shop.lat, shop.lng]).x} ${px([shop.lat, shop.lng]).y})`">
          <path d="M0 0C-3 -9 -11 -13 -11 -22A11 11 0 1 1 11 -22C11 -13 3 -9 0 0Z" fill="#d9480f" stroke="#fff" stroke-width="2" />
          <text y="15" text-anchor="middle" font-size="11.5" font-weight="700" fill="#b93a08" stroke="#fff" stroke-width="3" paint-order="stroke">{{ shop.name }}</text>
        </g>
      </template>
      <template #ui>
        <svg class="pinmap-pin" width="34" height="44" viewBox="-17 -40 34 44">
          <ellipse cx="0" cy="1" rx="9" ry="3" fill="#000" opacity="0.25" />
          <path d="M0 0C-4 -12 -15 -17 -15 -26A15 15 0 1 1 15 -26C15 -17 4 -12 0 0Z" :fill="state === 'out' ? '#b42318' : '#1d4ed8'" stroke="#fff" stroke-width="2" /><circle cy="-26" r="5" fill="#fff" />
        </svg>
        <div class="pp-tip">{{ t('pp.drag') }}</div>
        <button type="button" class="mapc-float pp-me" :title="t('pp.me')" :aria-label="t('pp.me')" @click="locateMe">◎</button>
      </template>
    </MapCanvas>
    <div v-else class="pp-map muted" style="display:grid;place-items:center">{{ t('loading') }}</div>

    <div class="pp-body">
      <form class="pp-search" @submit.prevent="enter">
        <span>🔍</span><input v-model="q" :placeholder="t('pp.search')" data-testid="pin-search" enterkeyhint="search" @input="onType" />
        <button v-if="q" type="button" class="pp-x" @click="q = ''; onType()">×</button>
      </form>
      <div v-if="note" class="banner warn small">{{ note }}</div>
      <template v-if="slug">
        <div v-if="failed" class="banner bad small">{{ t('pp.failed') }} <button class="btn link" @click="at && check(at)">{{ t('retry') }}</button></div>
        <div v-else-if="!terms" class="pp-terms muted">{{ t('pp.checking') }}</div>
        <div v-else-if="terms.deliverable" class="pp-terms in" :style="{ opacity: checking ? 0.55 : 1 }" data-testid="pin-result" :data-state="state">
          <b>{{ t('pp.can') }}</b>
          <div class="small muted">{{ t('pp.terms', { fee: money(terms.fee, currency), min: money(terms.min_order, currency) }) }}<template v-if="terms.eta_minutes">{{ t('pp.eta', { n: terms.eta_minutes }) }}</template></div>
        </div>
        <div v-else class="pp-terms out" :style="{ opacity: checking ? 0.55 : 1 }" data-testid="pin-result" :data-state="state">
          <b>{{ t('pp.cannot') }}</b>
          <div class="small">{{ t(keepable ? 'pp.cannot_keep' : 'pp.cannot_hint') }}</div>
          <button v-if="pickup" type="button" class="btn small" style="margin-top:8px" data-testid="pin-pickup" @click="emit('pickup')">{{ t('pp.to_pickup') }}</button>
        </div>
      </template>
      <div class="pp-listhd">
        <span class="muted">📍 {{ t(typing ? 'pp.results' : 'pp.nearby') }}</span>
        <button type="button" class="btn primary small" :disabled="!ready || resolving" data-testid="pin-confirm" @click="done">{{ t('pp.done') }}</button>
      </div>
      <div class="pp-list" data-testid="pin-list">
        <div v-if="looking && !list.length" class="muted pp-empty">{{ t('pp.looking') }}</div>
        <div v-else-if="typing && !list.length" class="muted pp-empty">{{ t('pp.none') }}</div>
        <button v-for="(p, i) in list" :key="i" type="button" class="pp-place" :class="{ on: chosen === p }" data-testid="pin-place" @click="pick(p)">
          <span class="grow">
            <span class="n">{{ p.label }}</span>
            <span class="d" v-if="p.detail || p.distance_m">{{ p.detail }}<template v-if="p.distance_m"><template v-if="p.detail"> · </template>{{ t('pp.metres', { n: p.distance_m }) }}</template></span>
          </span>
          <span v-if="chosen === p" class="tick">✓</span>
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.ppick { display: flex; flex-direction: column; background: #fff; height: 100%; min-height: 0; }
.pp-head { display: flex; align-items: center; padding: 12px 14px; border-bottom: 1px solid var(--line); font-size: 17px; flex: none; }
.pp-head b { flex: 1; text-align: center; margin-inline-end: 34px; }
.pp-back { border: 0; background: none; font-size: 28px; line-height: 1; width: 34px; cursor: pointer; color: var(--ink); }
.pp-map { height: 38vh; min-height: 220px; max-height: 360px; flex: none; position: relative; }
.ppick.embedded .pp-map { height: 300px; }
.pp-tip { position: absolute; left: 50%; top: 12px; transform: translateX(-50%); background: rgba(31, 35, 40, .88); color: #fff; font-size: 12.5px; padding: 5px 12px; border-radius: 16px; white-space: nowrap; pointer-events: none; z-index: 2; }
.pp-me { inset-inline-end: 12px; bottom: 30px; width: 40px; height: 40px; border-radius: 50%; border: 0; cursor: pointer; font-size: 18px; color: var(--brand); }
.pp-body { flex: 1; min-height: 0; display: flex; flex-direction: column; gap: 8px; padding: 12px 12px 0; }
.pp-search { display: flex; align-items: center; gap: 8px; background: #f1f2f4; border-radius: 22px; height: 44px; padding: 0 14px; flex: none; }
.pp-search:focus-within { background: #fff; outline: 2px solid var(--brand); }
.pp-search input { border: 0; outline: 0 !important; background: none; flex: 1; min-width: 0; padding: 0; font-size: 15px; height: 100%; }
.pp-x { border: 0; background: none; color: #9aa1ab; font-size: 22px; cursor: pointer; }
.pp-terms { border: 1px solid var(--line); border-radius: 10px; padding: 9px 12px; font-size: 14px; flex: none; }
.pp-terms.in { border-color: #2a78d6; background: #f3f8ff; }
.pp-terms.out { border-color: #f3c4bf; background: var(--bad-soft); color: var(--bad); }
.pp-listhd { display: flex; align-items: center; justify-content: space-between; font-size: 14px; flex: none; }
.pp-listhd .btn { min-height: 34px; padding: 0 18px; font-size: 15px; }
.pp-list { flex: 1; min-height: 80px; overflow-y: auto; margin: 0 -12px; }
.pp-empty { padding: 14px 16px; font-size: 14px; }
.pp-place { display: flex; align-items: center; gap: 10px; width: 100%; text-align: start; border: 0; border-top: 1px solid #f0f1f3; background: #fff; padding: 11px 16px; cursor: pointer; font: inherit; color: inherit; }
.pp-place .grow { flex: 1; min-width: 0; display: block; }
.pp-place .n { display: block; font-size: 15.5px; font-weight: 600; }
.pp-place .d { display: block; font-size: 13px; color: #7b828c; }
.pp-place .tick { color: var(--brand); font-size: 18px; font-weight: 700; }
.pp-place.on { background: var(--brand-soft); }
</style>
