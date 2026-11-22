<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { get, patch, post, put } from '../api'
import { both, money, t, toMajor, toMinor } from '../i18n'
import { session } from '../session'
import { CONTACT_FIELDS } from '../addresses'
import type { Branch, Station, Zone } from '../types'

const bid = session.branchId!
const branch = ref<Branch | null>(null)
const zones = ref<Zone[]>([])
const stations = ref<Station[]>([])
const error = ref('')
const saved = ref(false)

const s = reactive({ name: '', name_en: '', address: '', phone: '', tax: 12, tax_inclusive: true, service: 0, packaging: 0,
  min_order: 0, accept_timeout_minutes: 10, delivery_enabled: true, pickup_enabled: true, paused: false, open_time: '00:00', close_time: '00:00', notify_sms: true, scheduling_enabled: false, prep_minutes: 30,
  kitchen_slow_minutes: 15, detour_percent: 30, detour_km: 1, notice: '', intro: '', baskets_enabled: true, contact_fields: [] as string[] })
const st = reactive({ id: 0, name: '', name_en: '', active: true })

async function load() {
  const m = await get(`/merchant/branches/${bid}/menu`)
  const b: Branch = m.branch
  branch.value = b; zones.value = m.zones; stations.value = m.stations
  Object.assign(s, { name: b.name, name_en: b.name_en, address: b.address, phone: b.phone, tax: b.tax_rate_bp / 100,
    tax_inclusive: b.tax_inclusive, service: b.service_charge_bp / 100, packaging: toMajor(b.packaging_fee), min_order: toMajor(b.min_order_amount),
    accept_timeout_minutes: b.accept_timeout_minutes, delivery_enabled: b.delivery_enabled, pickup_enabled: b.pickup_enabled,
    paused: b.paused, open_time: b.open_time, close_time: b.close_time, notify_sms: b.notify_sms,
    scheduling_enabled: b.scheduling_enabled, prep_minutes: b.prep_minutes,
    kitchen_slow_minutes: b.kitchen_slow_minutes, detour_percent: b.detour_percent, detour_km: b.detour_meters / 1000,
    notice: b.notice, intro: b.intro, baskets_enabled: b.baskets_enabled, contact_fields: [...(b.contact_fields ?? [])] })
}
onMounted(load)

async function run(fn: () => Promise<unknown>) {
  error.value = ''; saved.value = false
  try { await fn(); await load(); saved.value = true; return true } catch (e) { error.value = (e as Error).message; return false }
}

const saveSettings = () => run(() => put(`/merchant/branches/${bid}/settings`, {
  name: s.name, name_en: s.name_en, address: s.address, phone: s.phone, tax_rate_bp: Math.round(s.tax * 100), tax_inclusive: s.tax_inclusive,
  service_charge_bp: Math.round(s.service * 100), packaging_fee: toMinor(s.packaging), min_order_amount: toMinor(s.min_order),
  accept_timeout_minutes: s.accept_timeout_minutes, delivery_enabled: s.delivery_enabled, pickup_enabled: s.pickup_enabled,
  paused: s.paused, open_time: s.open_time, close_time: s.close_time, notify_sms: s.notify_sms,
  scheduling_enabled: s.scheduling_enabled, prep_minutes: s.prep_minutes,
  kitchen_slow_minutes: s.kitchen_slow_minutes, detour_percent: s.detour_percent, detour_meters: Math.round(s.detour_km * 1000),
  notice: s.notice, intro: s.intro, baskets_enabled: s.baskets_enabled, contact_fields: s.contact_fields }))

function editStation(x: Station | null) {
  Object.assign(st, x ? { id: x.id, name: x.name, name_en: x.name_en, active: x.active } : { id: 0, name: '', name_en: '', active: true })
}
async function saveStation() {
  const body = { name: st.name, name_en: st.name_en, active: st.active, sort: st.id ? stations.value.find(x => x.id === st.id)?.sort ?? 0 : stations.value.length }
  if (await run(() => st.id ? put(`/merchant/branches/${bid}/stations/${st.id}`, body) : post(`/merchant/branches/${bid}/stations`, body))) editStation(null)
}

const link = () => `${location.origin}/s/${branch.value?.slug}`
</script>

<template>
  <div class="page stack" v-if="branch" style="max-width:900px">
    <div v-if="error" class="banner bad">{{ error }}</div>
    <div v-else-if="saved" class="banner ok">{{ t('saved') }}</div>

    <div class="card stack">
      <h2>{{ t('share_link') }}</h2>
      <a :href="link()" target="_blank">{{ link() }}</a>
    </div>

    <form class="card" @submit.prevent="saveSettings">
      <h2 style="margin-bottom:12px">{{ t('store_settings') }}</h2>
      <div class="grid2">
        <label class="field"><span>{{ t('name_zh') }}</span><input v-model="s.name" required /></label>
        <label class="field"><span>{{ t('name_en') }}</span><input v-model="s.name_en" /></label>
        <label class="field"><span>{{ t('address') }}</span><input v-model="s.address" /></label>
        <label class="field"><span>{{ t('phone') }}</span><input v-model="s.phone" /></label>
        <label class="field"><span>{{ t('open_time') }}</span><input type="time" v-model="s.open_time" required /></label>
        <label class="field"><span>{{ t('close_time') }}</span><input type="time" v-model="s.close_time" required /></label>
        <label class="field"><span>{{ t('tax_rate') }}</span><input type="number" min="0" max="50" step="0.01" v-model.number="s.tax" /></label>
        <label class="field"><span>{{ t('service_rate') }}</span><input type="number" min="0" max="50" step="0.01" v-model.number="s.service" /></label>
        <label class="field"><span>{{ t('packaging_fee') }} ({{ branch.currency }})</span><input type="number" min="0" step="0.01" v-model.number="s.packaging" /></label>
        <label class="field"><span>{{ t('min_order') }} ({{ branch.currency }})</span><input type="number" min="0" step="0.01" v-model.number="s.min_order" /></label>
        <label class="field"><span>{{ t('accept_timeout') }}</span><input type="number" min="1" max="120" v-model.number="s.accept_timeout_minutes" /></label>
        <label class="field"><span>{{ t('prep_minutes') }}</span><input type="number" min="5" max="240" v-model.number="s.prep_minutes" /></label>
        <label class="field"><span>{{ t('set.slow_minutes') }}</span><input type="number" min="1" max="120" name="kitchen_slow_minutes" v-model.number="s.kitchen_slow_minutes" /></label>
        <label class="field"><span>{{ t('set.detour_percent') }}</span><input type="number" min="5" max="300" name="detour_percent" v-model.number="s.detour_percent" /></label>
        <label class="field"><span>{{ t('set.detour_km') }}</span><input type="number" min="0.1" max="20" step="0.1" name="detour_km" v-model.number="s.detour_km" /></label>
        <label class="check"><input type="checkbox" v-model="s.tax_inclusive" />{{ t('tax_inclusive') }}</label>
        <label class="check"><input type="checkbox" v-model="s.paused" />{{ t('pause_orders') }}</label>
        <label class="check"><input type="checkbox" v-model="s.delivery_enabled" />{{ t('delivery') }}</label>
        <label class="check"><input type="checkbox" v-model="s.pickup_enabled" />{{ t('pickup') }}</label>
        <label class="check"><input type="checkbox" v-model="s.notify_sms" />{{ t('notify_sms') }}</label>
        <label class="check"><input type="checkbox" v-model="s.scheduling_enabled" />{{ t('scheduling_enabled') }}</label>
        <label class="check"><input type="checkbox" name="baskets_enabled" v-model="s.baskets_enabled" />{{ t('set.baskets') }}</label>
      </div>
      <fieldset class="contact-fields" data-testid="contact-fields">
        <legend>{{ t('set.contact_fields') }}</legend>
        <p class="muted small">{{ t('set.contact_fields_hint') }}</p>
        <div class="row wrap">
          <label v-for="f in CONTACT_FIELDS" :key="f" class="check"><input type="checkbox" name="contact_fields" :value="f" v-model="s.contact_fields" />{{ t('ad.f.' + f) }}</label>
        </div>
      </fieldset>
      <label class="field"><span>{{ t('set.notice') }}</span><textarea name="notice" v-model="s.notice" rows="2" maxlength="300"></textarea></label>
      <label class="field"><span>{{ t('set.intro') }}</span><textarea name="intro" v-model="s.intro" rows="3" maxlength="500"></textarea></label>
      <button class="btn primary">{{ t('save') }}</button>
    </form>

    <div class="card stack">
      <h2>{{ t('zones') }}</h2>
      <p class="muted">{{ t('zones_moved') }}</p>
      <div><span v-for="zone in zones" :key="zone.id" class="tag" :class="zone.active ? 'ok' : ''" style="margin:0 6px 6px 0">{{ both(zone.name, zone.name_en) }} · {{ money(zone.fee, branch.currency) }}</span></div>
      <router-link class="btn" to="/merchant/delivery-map" data-testid="to-delivery-map">{{ t('delivery_map') }}</router-link>
    </div>

    <div class="card stack">
      <h2>{{ t('stations') }}</h2>
      <p class="muted small">{{ t('stations_hint') }}</p>
      <table class="data" v-if="stations.length">
        <tbody><tr v-for="x in stations" :key="x.id">
          <td>{{ both(x.name, x.name_en) }}</td>
          <td class="muted small">{{ t('items_n', { n: x.item_count }) }} · {{ t('tasks_n', { n: x.open_tasks }) }}</td>
          <td><span class="tag" :class="x.active ? 'ok' : ''">{{ x.active ? t('enabled') : t('disabled') }}</span></td>
          <td class="right"><button class="btn small" @click="editStation(x)">{{ t('edit') }}</button></td>
        </tr></tbody>
      </table>
      <form class="row wrap" @submit.prevent="saveStation">
        <input v-model="st.name" :placeholder="t('name_zh')" required style="flex:1 1 140px" />
        <input v-model="st.name_en" :placeholder="t('name_en')" style="flex:1 1 140px" />
        <label class="check" style="margin:0"><input type="checkbox" v-model="st.active" />{{ t('enabled') }}</label>
        <button class="btn primary">{{ st.id ? t('save') : t('new_station') }}</button>
        <button v-if="st.id" type="button" class="btn" @click="editStation(null)">{{ t('cancel') }}</button>
      </form>
    </div>

    <div class="card row between">
      <span><b>{{ t('staff') }}</b> <span class="muted small">{{ t('staff_moved') }}</span></span>
      <router-link class="btn" to="/merchant/staff">{{ t('staff') }}</router-link>
    </div>
  </div>
</template>

<style scoped>
.grid2 { display: grid; grid-template-columns: 1fr 1fr; gap: 0 12px; }
.contact-fields { border: 1px solid var(--line); border-radius: 10px; padding: 10px 12px 2px; margin: 0 0 12px; }
.contact-fields legend { font-weight: 600; padding: 0 6px; }
.contact-fields .row { gap: 4px 22px; margin-top: 8px; }
</style>
