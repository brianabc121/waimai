<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { ApiError, get, post } from '../api'
import { both, minutesSince, t, whenText } from '../i18n'
import { useLive } from '../live'
import { session } from '../session'
import type { Order, OrderItem, Station } from '../types'

const bid = session.branchId!
const orders = ref<Order[]>([])
const stations = ref<Station[]>([])
// Station filter for the cooking tab: 'all', 'none' (unassigned) or a station id.
// Remembered per device, since a screen usually sits at one station.
const station = ref<'all' | 'none' | number>(JSON.parse(localStorage.getItem(`kds-station-${bid}`) || '"all"'))
function setStation(v: 'all' | 'none' | number) { station.value = v; localStorage.setItem(`kds-station-${bid}`, JSON.stringify(v)) }
const tab = ref<'cook' | 'pack' | 'serve'>('cook')
const error = ref('')
const now = ref(Date.now())
setInterval(() => (now.value = Date.now()), 30000)

// Per-order packing check state: which lines the packer ticked, and bag count.
// It is tied to the manifest version, so any change to the order clears it.
const checks = reactive<Record<number, { manifest: number; ticked: Set<number>; bags: number }>>({})
function check(o: Order) {
  const c = checks[o.id]
  if (!c || c.manifest !== o.manifest_version) checks[o.id] = { manifest: o.manifest_version, ticked: new Set(), bags: 1 }
  return checks[o.id]
}

const { online, lastSync, refreshNow } = useLive(() => `/merchant/branches/${bid}/events`, async () => {
  const [b, s] = await Promise.all([get<{ orders: Order[] }>(`/kitchen/branches/${bid}/board`), get<{ stations: Station[] }>(`/merchant/branches/${bid}/stations`)])
  orders.value = b.orders; stations.value = s.stations
  if (typeof station.value === 'number' && !s.stations.some(x => x.id === station.value)) setStation('all')
})

// A table's bill stays open for the whole meal: dishes already at the table drop off the screen.
const live = (o: Order) => o.items.filter(i => i.prep_status !== 'cancelled' && !i.served_at)
const dine = (o: Order) => o.service_type === 'dine_in'
const cancelled = computed(() => orders.value.filter(o => o.acceptance_status === 'cancelled'))
const open = (i: OrderItem) => i.needs_prep && (i.prep_status === 'pending' || i.prep_status === 'preparing')
const cooking = computed(() => orders.value.filter(o => o.acceptance_status === 'accepted' && (dine(o) ? live(o).some(open) : o.packing_status === 'waiting_items')))
const packing = computed(() => orders.value.filter(o => o.acceptance_status === 'accepted' && !dine(o) && o.packing_status !== 'none'))
// Dine-in dishes that are ready and waiting to be carried to the table.
const toServe = (o: Order) => live(o).filter(i => i.prep_status === 'ready')
const serving = computed(() => orders.value.filter(o => o.acceptance_status === 'accepted' && dine(o) && toServe(o).length > 0))
const serveCount = computed(() => serving.value.reduce((n, o) => n + toServe(o).length, 0))
const atStation = (i: OrderItem) => station.value === 'all' || (station.value === 'none' ? i.needs_prep && i.station_id === null : i.station_id === station.value)
// Lines shown on a ticket for the chosen station; the "all" view shows every line.
const lines = (o: Order) => live(o).filter(atStation)
const shown = computed(() => station.value === 'all' ? cooking.value : cooking.value.filter(o => lines(o).some(open)))
const openCount = (v: 'none' | number) => cooking.value.reduce((n, o) => n + live(o).filter(i => open(i) && (v === 'none' ? i.station_id === null : i.station_id === v)).length, 0)
// Stations to show as tabs: active ones, plus disabled ones that still hold work.
const tabs = computed(() => stations.value.filter(s => s.active || openCount(s.id) > 0))
const unassigned = computed(() => stations.value.length ? openCount('none') : 0)
function move(it: OrderItem, e: Event) {
  const v = (e.target as HTMLSelectElement).value
  void run(() => post(`/kitchen/items/${it.id}/station`, { station_id: v === '' ? null : Number(v), reason: 'kitchen screen' }))
}
const missing = (o: Order) => live(o).filter(i => i.prep_status !== 'ready')
const waited = (o: Order) => { void now.value; return minutesSince(o.release_at ?? o.created_at) }

async function run(fn: () => Promise<unknown>) {
  error.value = ''
  try { await fn() } catch (e) {
    error.value = e instanceof ApiError && e.code === 'MANIFEST_CHANGED' ? t('conflict') : (e as Error).message
  }
  await refreshNow()
}
const itemAction = (it: OrderItem, action: string) => run(() => post(`/kitchen/items/${it.id}/${action}`))
const pack = (o: Order) => run(() => post(`/kitchen/orders/${o.id}/pack`, { bags: check(o).bags, manifest_version: o.manifest_version }))
const serve = (it: OrderItem) => run(() => post(`/merchant/bill-items/${it.id}/serve`))
const handoff = (o: Order) => run(() => post(`/merchant/orders/${o.id}/handoff`))

function tick(o: Order, it: OrderItem) {
  const c = check(o)
  c.ticked.has(it.id) ? c.ticked.delete(it.id) : c.ticked.add(it.id)
}
const allTicked = (o: Order) => live(o).every(i => check(o).ticked.has(i.id))
</script>

<template>
  <div class="page">
    <div class="row between wrap" style="margin-bottom:12px">
      <div class="seg light">
        <button :class="{ on: tab === 'cook' }" @click="tab = 'cook'">{{ t('to_cook') }} · {{ cooking.length }}</button>
        <button :class="{ on: tab === 'pack' }" @click="tab = 'pack'">{{ t('to_pack') }} · {{ packing.length }}</button>
        <button :class="{ on: tab === 'serve' }" data-test="tab-serve" @click="tab = 'serve'">{{ t('to_serve') }} · {{ serveCount }}</button>
      </div>
      <span class="muted small" v-if="lastSync">{{ t('synced') }} {{ lastSync.toLocaleTimeString() }}</span>
    </div>
    <div v-if="!online" class="banner warn" style="margin-bottom:12px">{{ t('offline') }}</div>
    <div v-if="error" class="banner bad" style="margin-bottom:12px">{{ error }}</div>

    <div class="cols" v-if="cancelled.length" style="margin-bottom:14px">
      <div v-for="o in cancelled" :key="o.id" class="card" style="border-color:var(--bad);background:var(--bad-soft)">
        <h2>{{ o.public_no }}</h2>
        <p class="bold" style="color:var(--bad)">{{ t('cancelled_notice') }}</p>
        <div v-for="it in o.items" :key="it.id" class="strike">{{ it.qty }} × {{ both(it.name, it.name_en) }}</div>
      </div>
    </div>

    <!-- cooking: per-item progress, grouped by order (K01) -->
    <template v-if="tab === 'cook'">
      <div class="row wrap" v-if="stations.length" style="margin-bottom:12px;gap:6px">
        <button class="btn small" :class="{ primary: station === 'all' }" @click="setStation('all')">{{ t('all_stations') }}</button>
        <button v-for="s in tabs" :key="s.id" class="btn small" :class="{ primary: station === s.id }" @click="setStation(s.id)">
          {{ both(s.name, s.name_en) }} · {{ openCount(s.id) }}<span v-if="!s.active"> ({{ t('disabled') }})</span></button>
        <button class="btn small" :class="{ primary: station === 'none', danger: unassigned > 0 && station !== 'none' }" @click="setStation('none')">{{ t('unassigned') }} · {{ unassigned }}</button>
      </div>
      <div v-if="unassigned > 0" class="banner warn" style="margin-bottom:12px">{{ t('unassigned_warn', { n: unassigned }) }}</div>
      <div v-if="shown.length === 0" class="card muted">{{ t('kitchen_empty') }}</div>
      <div class="cols">
        <div v-for="o in shown" :key="o.id" class="card stack">
          <div class="row between">
            <h2><template v-if="dine(o)">{{ t('table') }} {{ o.table_name }}</template><template v-else>{{ o.public_no }}</template>
              <span class="tag brand" style="margin-left:4px">{{ t(o.service_type) }}</span>
              <span class="tag info" v-if="o.scheduled_for" style="margin-left:4px">{{ t('scheduled') }} {{ whenText(o.scheduled_for, o.timezone) }}</span></h2>
            <span v-if="!dine(o)" class="tag" :class="waited(o) >= 20 ? 'bad' : waited(o) >= 10 ? 'warn' : ''">{{ waited(o) }} {{ t('min') }}</span>
            <span v-else class="small muted">{{ o.public_no }}</span>
          </div>
          <div v-if="o.note" class="banner warn small">{{ t('order_note') }}: {{ o.note }}</div>
          <div v-for="it in lines(o)" :key="it.id" class="kline" :class="it.prep_status">
            <div class="grow">
              <div class="kname">{{ it.qty }} × {{ both(it.name, it.name_en) }}<span v-if="dine(o)" class="small muted" style="font-weight:400"> · {{ t('batch_n', { n: it.batch_seq }) }}</span></div>
              <div class="small" v-if="stations.length && it.needs_prep">
                <span class="tag" :class="it.station_id === null ? 'warn' : 'info'">{{ it.station_name || t('unassigned') }}</span>
                <select v-if="open(it)" class="mv" :value="it.station_id ?? ''" :title="t('move_to')" :aria-label="t('move_to')" @change="move(it, $event)">
                  <option value="">{{ t('unassigned') }}</option>
                  <option v-for="s in stations.filter(s => s.active || s.id === it.station_id)" :key="s.id" :value="s.id">{{ t('move_to') }} {{ s.name }}</option>
                </select>
              </div>
              <div class="small" v-if="it.options.length">{{ it.options.map(x => both(x.name, x.name_en)).join(' · ') }}</div>
              <div class="small bold" v-if="it.note" style="color:var(--warn)">{{ t('note') }}: {{ it.note }}</div>
            </div>
            <span v-if="!it.needs_prep" class="tag">{{ t('no_prep') }}</span>
            <template v-else>
              <button v-if="it.prep_status === 'pending'" class="btn small" @click="itemAction(it, 'start')">{{ t('start') }}</button>
              <button v-if="it.prep_status !== 'ready'" class="btn small ok" @click="itemAction(it, 'ready')">{{ t('ready') }}</button>
              <button v-else class="btn small" @click="itemAction(it, 'undo')">{{ t('undo') }}</button>
            </template>
          </div>
        </div>
      </div>
    </template>

    <!-- serving: dine-in dishes ready for the table -->
    <template v-else-if="tab === 'serve'">
      <div v-if="serving.length === 0" class="card muted">{{ t('kitchen_empty') }}</div>
      <div class="cols">
        <div v-for="o in serving" :key="o.id" class="card stack">
          <div class="row between"><h2>{{ t('table') }} {{ o.table_name }}</h2><span class="small muted">{{ o.public_no }}</span></div>
          <div v-for="it in toServe(o)" :key="it.id" class="kline ready">
            <div class="grow">
              <div class="kname">{{ it.qty }} × {{ both(it.name, it.name_en) }}</div>
              <div class="small" v-if="it.options.length">{{ it.options.map(x => both(x.name, x.name_en)).join(' · ') }}</div>
              <div class="small bold" v-if="it.note" style="color:var(--warn)">{{ t('note') }}: {{ it.note }}</div>
            </div>
            <button class="btn small ok" @click="serve(it)">{{ t('serve') }}</button>
          </div>
        </div>
      </div>
    </template>

    <!-- packing: whole-order check and handoff (K03/K04) -->
    <template v-else>
      <div v-if="packing.length === 0" class="card muted">{{ t('kitchen_empty') }}</div>
      <div class="cols">
        <div v-for="o in packing" :key="o.id" class="card stack">
          <div class="row between">
            <h2>{{ o.public_no }} <span class="tag brand">{{ t(o.service_type) }}</span>
              <span class="tag info" v-if="o.scheduled_for" style="margin-left:4px">{{ t('scheduled') }} {{ whenText(o.scheduled_for, o.timezone) }}</span></h2>
            <span class="tag" :class="{ ok: o.packing_status === 'packed', warn: o.packing_status === 'ready_to_pack' }">{{ t('p.' + o.packing_status) }}</span>
          </div>
          <div v-if="o.note" class="banner warn small">{{ t('order_note') }}: {{ o.note }}</div>

          <template v-if="o.packing_status === 'waiting_items'">
            <div v-for="it in live(o)" :key="it.id" class="row between" :class="{ muted: it.prep_status === 'ready' }">
              <span>{{ it.qty }} × {{ both(it.name, it.name_en) }}</span>
              <span class="tag" :class="it.prep_status === 'ready' ? 'ok' : 'bad'">{{ it.prep_status === 'ready' ? t('i.ready') : t('missing') }}</span>
            </div>
            <div class="banner warn small">{{ t('missing') }}: {{ missing(o).map(i => i.name).join('、') }}</div>
          </template>

          <template v-else-if="o.packing_status === 'ready_to_pack'">
            <label v-for="it in live(o)" :key="it.id" class="check top">
              <input type="checkbox" :checked="check(o).ticked.has(it.id)" @change="tick(o, it)" />
              <span><b>{{ it.qty }} × {{ both(it.name, it.name_en) }}</b>
                <span class="small muted" v-if="it.options.length"><br />{{ it.options.map(x => both(x.name, x.name_en)).join(' · ') }}</span>
                <span class="small bold" v-if="it.note" style="color:var(--warn)"><br />{{ t('note') }}: {{ it.note }}</span></span>
            </label>
            <div class="row between"><span>{{ t('bags') }}</span>
              <span class="stepper"><button @click="check(o).bags = Math.max(1, check(o).bags - 1)">−</button><b>{{ check(o).bags }}</b><button class="fill" @click="check(o).bags++">+</button></span></div>
            <button class="btn primary block" :disabled="!allTicked(o)" @click="pack(o)">{{ allTicked(o) ? t('confirm_pack') : t('check_all') }}</button>
          </template>

          <template v-else>
            <div class="row between"><span class="muted">{{ t('bags') }}</span><b>{{ o.bags }}</b></div>
            <template v-if="o.service_type === 'pickup'">
              <div class="row between"><span class="muted">{{ t('pickup_code') }}</span><b style="font-size:22px">{{ o.pickup_code }}</b></div>
              <button class="btn primary block" @click="handoff(o)">{{ t('handoff_customer') }}</button>
            </template>
            <template v-else>
              <div class="row between"><span class="muted">{{ t('rider') }}</span><b>{{ o.delivery?.rider_name ?? '—' }}</b></div>
              <button class="btn primary block" :disabled="!o.delivery" @click="handoff(o)">{{ t('handoff_rider') }}</button>
            </template>
          </template>
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
.kline { display: flex; align-items: center; gap: 8px; padding: 8px 10px; border-radius: 8px; background: #f6f6f4; border-left: 5px solid var(--line); }
.kline.preparing { border-left-color: var(--warn); background: var(--warn-soft); }
.kline.ready { border-left-color: var(--ok); background: var(--ok-soft); }
.kname { font-size: 17px; font-weight: 700; }
.mv { width: auto; padding: 2px 6px; font-size: 12px; margin-left: 6px; }
.check.top { align-items: flex-start; }
</style>
