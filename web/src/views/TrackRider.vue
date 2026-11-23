<script setup lang="ts">
// The customer's view of the rider while the order is on its way: a small
// map when the platform has a map key, a line of text otherwise. It is only
// mounted between pickup and delivery, and asks every 15 seconds.
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { get } from '../api'
import { extend, minutesSince, t } from '../i18n'
import { mapsKey as loadMapsKey } from '../maps'
import type { LatLng, View } from '../geo'
import type { Tracking } from '../types'
import MapCanvas from './MapCanvas.vue'

extend({
  'tr.title': ['骑手位置', 'Where your rider is'],
  'tr.away': ['骑手离你约 {d}', 'Your rider is about {d} away'],
  'tr.updated': ['{n} 分钟前更新', 'updated {n} min ago'], 'tr.now': ['刚刚更新', 'updated just now'],
  'tr.unknown': ['骑手正在路上。暂时没有收到位置，不影响配送。', 'Your rider is on the way. No position right now; your delivery is not affected.'],
  'tr.you': ['你', 'You'],
})

const props = defineProps<{ token: string }>()
const tr = ref<Tracking | null>(null)
const key = ref('')
const map = ref<InstanceType<typeof MapCanvas>>()
const ready = ref(false)

async function load() { try { tr.value = await get<Tracking>(`/customer/orders/${props.token}/tracking`) } catch { /* keep the last one */ } }
void load()
const timer = window.setInterval(load, 15000)
onBeforeUnmount(() => window.clearInterval(timer))
void loadMapsKey().then(k => (key.value = k))

const rider = computed<LatLng | null>(() => tr.value?.lat != null && tr.value.lng != null ? [tr.value.lat, tr.value.lng] : null)
const dest = computed<LatLng | null>(() => tr.value?.dest_lat != null && tr.value.dest_lng != null ? [tr.value.dest_lat, tr.value.dest_lng] : null)
const initial = computed<View>(() => ({ center: rider.value ?? dest.value ?? [0, 0], zoom: 15 }))
function fit() {
  const pts = [rider.value, dest.value].filter(Boolean) as LatLng[]
  if (pts.length > 1) map.value?.fit(pts, 16)
  else if (pts.length) map.value?.setView({ center: pts[0], zoom: 16 })
}
watch([ready, rider], () => { if (ready.value) fit() })
const km = (m: number) => m < 1000 ? `${Math.max(50, Math.round(m / 50) * 50)} m` : `${(m / 1000).toFixed(1)} km`
const updated = computed(() => { if (!tr.value?.located_at) return ''; const n = minutesSince(tr.value.located_at); return n < 1 ? t('tr.now') : t('tr.updated', { n }) })
</script>

<template>
  <div v-if="tr?.active" class="card stack" data-testid="tracking" :data-has-position="!!rider">
    <h2>{{ t('tr.title') }}</h2>
    <template v-if="rider">
      <MapCanvas v-if="key" ref="map" :maps-key="key" :initial="initial" style="height:220px;border-radius:10px" data-testid="tracking-map" @ready="ready = true">
        <template #default="{ px }">
          <g v-if="dest" :transform="`translate(${px(dest).x},${px(dest).y})`">
            <circle r="13" fill="#fff" stroke="#2a78d6" stroke-width="3" /><text y="5" text-anchor="middle" font-size="12" font-weight="700" fill="#2a78d6">{{ t('tr.you') }}</text>
          </g>
          <g :transform="`translate(${px(rider).x},${px(rider).y})`" data-testid="rider-pin">
            <circle r="15" fill="#d9480f" stroke="#fff" stroke-width="2.5" /><text y="5" text-anchor="middle" font-size="13" font-weight="700" fill="#fff">{{ t('ic.rider') }}</text>
          </g>
        </template>
      </MapCanvas>
      <div class="row between small">
        <b v-if="tr.distance_m != null" data-testid="tracking-distance">{{ t('tr.away', { d: km(tr.distance_m) }) }}</b><span v-else></span>
        <span class="muted">{{ updated }}</span>
      </div>
    </template>
    <p v-else class="muted small">{{ t('tr.unknown') }}</p>
  </div>
</template>
