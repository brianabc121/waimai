<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { get, patch, post, put } from '../api'
import { both, money, t, toMajor, toMinor } from '../i18n'
import { session } from '../session'
import type { Branch, Station, Zone } from '../types'

const bid = session.branchId!
const branch = ref<Branch | null>(null)
const zones = ref<Zone[]>([])
const stations = ref<Station[]>([])
const staff = ref<{ id: number; username: string; display_name: string; role: string; active: boolean }[]>([])
const error = ref('')
const saved = ref(false)

const s = reactive({ name: '', name_en: '', address: '', phone: '', tax: 12, tax_inclusive: true, service: 0, packaging: 0,
  min_order: 0, accept_timeout_minutes: 10, delivery_enabled: true, pickup_enabled: true, paused: false, open_time: '00:00', close_time: '00:00', notify_sms: true, scheduling_enabled: false, prep_minutes: 30 })
const st = reactive({ id: 0, name: '', name_en: '', active: true })
const z = reactive({ id: 0, name: '', name_en: '', fee: 0, min_order: 0, active: true })
const u = reactive({ username: '', password: '', display_name: '', phone: '', role: 'cashier' })

async function load() {
  const m = await get(`/merchant/branches/${bid}/menu`)
  const b: Branch = m.branch
  branch.value = b; zones.value = m.zones; stations.value = m.stations
  Object.assign(s, { name: b.name, name_en: b.name_en, address: b.address, phone: b.phone, tax: b.tax_rate_bp / 100,
    tax_inclusive: b.tax_inclusive, service: b.service_charge_bp / 100, packaging: toMajor(b.packaging_fee), min_order: toMajor(b.min_order_amount),
    accept_timeout_minutes: b.accept_timeout_minutes, delivery_enabled: b.delivery_enabled, pickup_enabled: b.pickup_enabled,
    paused: b.paused, open_time: b.open_time, close_time: b.close_time, notify_sms: b.notify_sms,
    scheduling_enabled: b.scheduling_enabled, prep_minutes: b.prep_minutes })
  staff.value = (await get(`/merchant/branches/${bid}/staff`)).staff
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
  scheduling_enabled: s.scheduling_enabled, prep_minutes: s.prep_minutes }))

function editStation(x: Station | null) {
  Object.assign(st, x ? { id: x.id, name: x.name, name_en: x.name_en, active: x.active } : { id: 0, name: '', name_en: '', active: true })
}
async function saveStation() {
  const body = { name: st.name, name_en: st.name_en, active: st.active, sort: st.id ? stations.value.find(x => x.id === st.id)?.sort ?? 0 : stations.value.length }
  if (await run(() => st.id ? put(`/merchant/branches/${bid}/stations/${st.id}`, body) : post(`/merchant/branches/${bid}/stations`, body))) editStation(null)
}

function editZone(zone: Zone | null) {
  Object.assign(z, zone ? { id: zone.id, name: zone.name, name_en: zone.name_en, fee: toMajor(zone.fee), min_order: toMajor(zone.min_order), active: zone.active }
    : { id: 0, name: '', name_en: '', fee: 0, min_order: 0, active: true })
}
async function saveZone() {
  const body = { name: z.name, name_en: z.name_en, fee: toMinor(z.fee), min_order: toMinor(z.min_order), active: z.active, sort: 0 }
  if (await run(() => z.id ? put(`/merchant/branches/${bid}/zones/${z.id}`, body) : post(`/merchant/branches/${bid}/zones`, body))) editZone(null)
}
async function addStaff() {
  if (await run(() => post(`/merchant/branches/${bid}/staff`, { ...u }))) Object.assign(u, { username: '', password: '', display_name: '', phone: '' })
}
const toggleStaff = (m: { id: number; active: boolean }) => run(() => patch(`/merchant/branches/${bid}/staff/${m.id}`, { active: !m.active }))
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
        <label class="check"><input type="checkbox" v-model="s.tax_inclusive" />{{ t('tax_inclusive') }}</label>
        <label class="check"><input type="checkbox" v-model="s.paused" />{{ t('pause_orders') }}</label>
        <label class="check"><input type="checkbox" v-model="s.delivery_enabled" />{{ t('delivery') }}</label>
        <label class="check"><input type="checkbox" v-model="s.pickup_enabled" />{{ t('pickup') }}</label>
        <label class="check"><input type="checkbox" v-model="s.notify_sms" />{{ t('notify_sms') }}</label>
        <label class="check"><input type="checkbox" v-model="s.scheduling_enabled" />{{ t('scheduling_enabled') }}</label>
      </div>
      <button class="btn primary">{{ t('save') }}</button>
    </form>

    <div class="card stack">
      <h2>{{ t('zones') }}</h2>
      <table class="data">
        <thead><tr><th>{{ t('name_zh') }}</th><th>{{ t('fee') }}</th><th>{{ t('min_order') }}</th><th></th><th></th></tr></thead>
        <tbody><tr v-for="zone in zones" :key="zone.id">
          <td>{{ both(zone.name, zone.name_en) }}</td><td class="price">{{ money(zone.fee, branch.currency) }}</td>
          <td class="price">{{ zone.min_order ? money(zone.min_order, branch.currency) : '—' }}</td>
          <td><span class="tag" :class="zone.active ? 'ok' : ''">{{ zone.active ? t('enabled') : t('disabled') }}</span></td>
          <td class="right"><button class="btn small" @click="editZone(zone)">{{ t('edit') }}</button></td>
        </tr></tbody>
      </table>
      <form class="row wrap" @submit.prevent="saveZone">
        <input v-model="z.name" :placeholder="t('name_zh')" required style="flex:1 1 140px" />
        <input v-model="z.name_en" :placeholder="t('name_en')" style="flex:1 1 140px" />
        <input type="number" min="0" step="0.01" v-model.number="z.fee" :title="t('fee')" style="flex:0 1 100px" />
        <input type="number" min="0" step="0.01" v-model.number="z.min_order" :title="t('min_order')" style="flex:0 1 100px" />
        <label class="check" style="margin:0"><input type="checkbox" v-model="z.active" />{{ t('enabled') }}</label>
        <button class="btn primary">{{ z.id ? t('save') : t('new_zone') }}</button>
        <button v-if="z.id" type="button" class="btn" @click="editZone(null)">{{ t('cancel') }}</button>
      </form>
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

    <div class="card stack">
      <h2>{{ t('staff') }}</h2>
      <table class="data">
        <thead><tr><th>{{ t('username') }}</th><th>{{ t('display_name') }}</th><th>{{ t('role') }}</th><th></th></tr></thead>
        <tbody><tr v-for="m in staff" :key="m.id">
          <td>{{ m.username }}</td><td>{{ m.display_name }}</td><td>{{ t('role.' + m.role) }}</td>
          <td class="right"><span v-if="!m.active" class="tag bad" style="margin-right:6px">{{ t('disabled') }}</span>
            <button v-if="m.id !== session.user?.id" class="btn small" @click="toggleStaff(m)">{{ m.active ? t('disable') : t('enable') }}</button></td>
        </tr></tbody>
      </table>
      <form class="row wrap" @submit.prevent="addStaff">
        <input v-model="u.username" :placeholder="t('username')" required autocapitalize="none" style="flex:1 1 120px" />
        <input v-model="u.display_name" :placeholder="t('display_name')" required style="flex:1 1 120px" />
        <input v-model="u.password" type="password" :placeholder="t('password') + ' (8+)'" required minlength="8" autocomplete="new-password" style="flex:1 1 120px" />
        <input v-model="u.phone" :placeholder="t('phone')" style="flex:1 1 120px" />
        <select v-model="u.role" style="flex:0 1 150px">
          <option v-for="r in ['manager', 'cashier', 'kitchen', 'rider']" :key="r" :value="r">{{ t('role.' + r) }}</option>
        </select>
        <button class="btn primary">{{ t('add_staff') }}</button>
      </form>
    </div>
  </div>
</template>

<style scoped>
.grid2 { display: grid; grid-template-columns: 1fr 1fr; gap: 0 12px; }
</style>
