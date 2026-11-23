<script setup lang="ts">
// "Deliver to": where the marketplace should look for stores. The customer
// keeps the spot the device found, or takes one of their addresses, a place
// they search or pin on the map, or just a city to look around in.
import { computed, defineAsyncComponent, onMounted, ref } from 'vue'
import { listAddresses, sorted, whoLine, type Address } from '../../addresses'
import { loadCustomer } from '../../customer'
import { t } from '../../i18n'
import { fromAddress, locate, mall, mallRegions, mallSpot, regionName, setLoc, type MallCountry, type RegionRef } from '../../mall'
import { mapsKey } from '../../maps'
import type { Picked } from '../PlacePicker.vue'

const PlacePicker = defineAsyncComponent(() => import('../PlacePicker.vue'))

const props = defineProps<{ region?: RegionRef | null; startAt?: 'cities' }>()
const emit = defineEmits<{ close: [] }>()

const step = ref<'list' | 'map' | 'cities'>(props.startAt ?? 'list')
const addresses = ref<Address[]>([])
const hasMap = ref(false)
const countries = ref<MallCountry[] | null>(null)
const tried = ref(false)

onMounted(async () => {
  void mapsKey().then(k => { hasMap.value = !!k })
  if (step.value === 'cities') void loadCities()
  if (await loadCustomer().catch(() => false)) {
    const r = await listAddresses().catch(() => null)
    addresses.value = sorted(r?.addresses ?? []).filter(a => a.lat != null && a.lng != null)
  }
})

const loc = computed(() => mall.loc)
const onGps = computed(() => loc.value?.source === 'gps')
const gpsName = computed(() => onGps.value ? loc.value!.name || regionName(props.region) : '')
const hint = computed(() => regionName(loc.value?.region ?? props.region))

async function relocate() {
  tried.value = false
  const before = mall.loc
  if (before?.manual) setLoc({ ...before, manual: false })
  await locate()
  if (mall.loc?.source === 'gps') emit('close')
  else { tried.value = true; if (before && !mall.loc) setLoc(before) }
}
function useAddress(a: Address) { setLoc(fromAddress(a, true)); emit('close') }
function picked(p: Picked) { setLoc({ source: 'place', lat: p.lat, lng: p.lng, name: p.name, manual: true }); emit('close') }

async function loadCities() { countries.value = await mallRegions().catch(() => []) }
function toCities() { step.value = 'cities'; if (!countries.value) void loadCities() }
function useRegion(c: MallCountry, city: MallCountry['cities'][number], district = '') {
  const region = { country_code: c.country_code, country: c.country, province: city.province, city: city.city, district }
  setLoc({ source: 'region', name: regionName(region), region, manual: true })
  emit('close')
}
</script>

<template>
  <div class="sheet-mask" data-testid="mall-location" @click.self="emit('close')">
    <div v-if="step === 'map'" class="sheet mpick">
      <PlacePicker :start="mallSpot()" keepable @done="picked" @close="step = 'list'" />
    </div>

    <div v-else class="sheet mloc">
      <div class="mloc-head">
        <button v-if="step === 'cities' && !startAt" class="x back" :aria-label="t('back')" @click="step = 'list'"><span class="rtl-flip">‹</span></button>
        <h2>{{ step === 'cities' ? t('ml.cities') : t('ml.sheet_title') }}</h2>
        <button class="x" :aria-label="t('close')" data-testid="mall-location-close" @click="emit('close')">✕</button>
      </div>

      <div v-if="step === 'list'" class="sheet-body">
        <p v-if="mall.denied || tried" class="warn" data-testid="mall-denied">{{ tried ? t('ml.denied_again') : t('ml.denied') }}</p>
        <button v-if="hasMap" class="find" data-testid="mall-search-place" @click="step = 'map'">
          <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="10.5" cy="10.5" r="6.5" /><path d="m15.5 15.5 5 5" /></svg>
          {{ t('ml.sheet_search') }}</button>

        <div class="row gps" :class="{ on: onGps }">
          <span v-if="onGps" class="radio on"></span>
          <div class="grow">
            <b>{{ onGps ? t('ml.gps') : t('ml.relocate') }}</b>
            <small v-if="gpsName">{{ gpsName }}</small>
          </div>
          <button class="btn link" :disabled="mall.locating" data-testid="mall-relocate" @click="relocate">
            {{ mall.locating ? t('ml.locating') : onGps ? '⟳ ' + t('ml.relocate') : mall.denied || tried ? t('ml.need_permission') : '⟳ ' + t('ml.relocate') }}</button>
        </div>

        <template v-if="addresses.length">
          <p class="sec">{{ t('ml.my_addresses') }}</p>
          <button v-for="a in addresses" :key="a.id" class="row" data-testid="mall-address" @click="useAddress(a)">
            <span class="radio" :class="{ on: loc?.source === 'address' && loc.address_id === a.id }"></span>
            <div class="grow">
              <b><i v-if="a.label" class="lab">{{ t('ad.label_' + a.label) }}</i>{{ a.place_name }}</b>
              <small>{{ [a.unit, whoLine(a)].filter(Boolean).join(' · ') }}</small>
            </div>
          </button>
        </template>

        <button v-if="hasMap" class="row nav" data-testid="mall-map-pick" @click="step = 'map'">
          <span class="em">🗺️</span><b class="grow">{{ t('ml.map_pick') }}</b><span class="go"><span class="rtl-flip">›</span></span></button>
        <button class="row nav" data-testid="mall-cities" @click="toCities">
          <span class="em">🏙️</span><b class="grow">{{ t('ml.browse') }}</b><small v-if="hint">{{ hint }}</small><span class="go"><span class="rtl-flip">›</span></span></button>
      </div>

      <div v-else class="sheet-body">
        <p v-if="countries === null" class="none">…</p>
        <p v-else-if="!countries.length" class="none">{{ t('ml.cities_none') }}</p>
        <template v-for="c in countries ?? []" :key="c.country_code">
          <p class="sec">{{ c.country || c.country_code }}</p>
          <div v-for="city in c.cities" :key="city.city" class="city">
            <button class="row" data-testid="mall-city" @click="useRegion(c, city)">
              <b class="grow">{{ t('ml.city_all', { city: city.city }) }}</b><small>{{ t('ml.city_n', { n: city.stores }) }}</small><span class="go"><span class="rtl-flip">›</span></span></button>
            <div v-if="city.districts.length > 1" class="districts">
              <button v-for="d in city.districts" :key="d.district" data-testid="mall-district" @click="useRegion(c, city, d.district)">{{ d.district }} <small>{{ d.stores }}</small></button>
            </div>
          </div>
        </template>
        <p class="note">{{ t('ml.cities_note') }}</p>
      </div>
    </div>
  </div>
</template>

<style scoped>
.mloc { max-height: 86vh; }
.mpick { height: 94vh; max-height: 94vh; overflow: hidden; }
@media (min-width: 768px) { .mpick { height: min(720px, 90vh); max-width: 620px; } }
.mloc-head { display: flex; align-items: center; padding: 16px 16px 6px; }
.mloc-head h2 { flex: 1; text-align: center; font-size: 18px; }
.x { width: 34px; height: 34px; border: 0; background: none; color: #6b7280; font-size: 17px; cursor: pointer; border-radius: 50%; }
.x.back { font-size: 26px; line-height: 1; }
.mloc-head h2:first-child { padding-inline-start: 34px; }
.warn { background: var(--warn-soft); color: var(--warn); border-radius: 10px; padding: 10px 12px; font-size: 14px; margin-bottom: 12px; }
.find { width: 100%; display: flex; align-items: center; gap: 8px; padding: 11px 12px; border: 1px solid #d5d8dd; border-radius: 10px; background: #fff; color: #8b919a; cursor: pointer; text-align: start; }
.row { width: 100%; display: flex; align-items: center; gap: 12px; padding: 13px 2px; border: 0; border-bottom: 1px solid var(--line); background: none; text-align: start; cursor: pointer; }
.row.gps { cursor: default; }
.row.gps.on b { color: var(--brand); }
.row b { display: block; font-size: 15.5px; }
.row small { display: block; color: #6b7280; font-size: 13px; }
.row.nav { border-bottom: 0; padding: 12px 2px; }
.row .go { color: #6b7280; font-size: 18px; }
.row .em { font-size: 18px; }
.grow { flex: 1; min-width: 0; }
.radio { width: 20px; height: 20px; border-radius: 50%; border: 1.5px solid #c3c7ce; flex: none; }
.radio.on { border: 6px solid var(--brand); }
.lab { font-style: normal; font-size: 12px; background: var(--brand-soft); color: var(--brand); border-radius: 4px; padding: 1px 6px; margin-inline-end: 7px; font-weight: 600; }
.sec { color: #6b7280; font-size: 13px; margin: 14px 0 0; }
.city .row { padding: 12px 2px; }
.districts { display: flex; flex-wrap: wrap; gap: 8px; padding: 10px 0 12px; border-bottom: 1px solid var(--line); }
.districts button { border: 1px solid var(--line); background: #fff; border-radius: 16px; padding: 4px 12px; cursor: pointer; font-size: 14px; }
.districts small { color: #8b919a; }
.city:has(.districts) .row { border-bottom: 0; }
.note, .none { color: #8b919a; font-size: 13px; margin-top: 14px; }
.none { text-align: center; padding: 24px 0; }
</style>
