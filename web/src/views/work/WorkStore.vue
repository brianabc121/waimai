<script setup lang="ts">
// In-store identity: the dishes waiting at the person's own stations, and the
// orders to pack when they hold the packing post. It reads the same kitchen
// board as the big kitchen screen, so both always agree.
import { computed, reactive, ref, watch } from 'vue'
import { ApiError, get, post } from '../../api'
import { both, minutesSince, pick, t, whenText } from '../../i18n'
import { useLive } from '../../live'
import { currentRole, reloadUser, session } from '../../session'
import type { Order, OrderItem } from '../../types'
import { duty, loadDuty, setDuty } from './duty'
import '../staff/strings'

const props = defineProps<{ tab: 'tasks' | 'done'; riderTasks: number }>()
const emit = defineEmits<{ count: [n: number]; switch: [] }>()

const bid = session.branchId!
const orders = ref<Order[]>([])
const error = ref('')
const now = ref(Date.now())
setInterval(() => (now.value = Date.now()), 30000)

const posts = computed(() => currentRole()?.posts ?? [])
const myStations = computed(() => posts.value.filter(p => p.kind === 'station'))
const packer = computed(() => posts.value.some(p => p.kind === 'packing'))
// Filter: 'all', 'packing', or a station id.
const filter = ref<'all' | 'packing' | number>('all')

const { online, refreshNow } = useLive(() => `/merchant/branches/${bid}/events`, async () => {
  await reloadUser() // posts may have been changed by a manager
  orders.value = (await get<{ orders: Order[] }>(`/kitchen/branches/${bid}/board`)).orders
  void loadDuty() // working without clocking on clocks you on
})

// ---- clocking on and off: the same shift as the rider's switch ----
const shiftBusy = ref(false)
async function toggleShift() {
  shiftBusy.value = true; error.value = ''
  try { await setDuty(!duty.on) } catch (e) { error.value = (e as Error).message }
  shiftBusy.value = false
}
const sinceText = computed(() => duty.since ? new Date(duty.since).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false }) : '')

const live = (o: Order) => o.items.filter(i => i.prep_status !== 'cancelled' && !i.served_at)
const accepted = computed(() => orders.value.filter(o => o.acceptance_status === 'accepted'))
const mine = (i: OrderItem) => i.needs_prep && myStations.value.some(p => p.station_id === i.station_id)
const since = (o: Order) => o.release_at ?? o.created_at
type Task = { o: Order; it: OrderItem; waited: number }
const tasksOf = (status: string[]) => {
  void now.value
  const out: Task[] = []
  for (const o of accepted.value) for (const it of live(o)) if (mine(it) && status.includes(it.prep_status)) out.push({ o, it, waited: minutesSince(since(o)) })
  return out.sort((a, b) => b.waited - a.waited)
}
const cooking = computed(() => tasksOf(['pending', 'preparing']))
const ready = computed(() => tasksOf(['ready']))
const packing = computed(() => packer.value ? accepted.value.filter(o => o.service_type !== 'dine_in' && o.packing_status !== 'none' && ['none', 'awaiting_handoff', 'awaiting_pickup'].includes(o.fulfillment_status)) : [])
const toPack = computed(() => packing.value.filter(o => o.packing_status !== 'packed'))
const stationCount = (id: number | null) => cooking.value.filter(x => x.it.station_id === id).length
const shownCooking = computed(() => filter.value === 'packing' ? [] : filter.value === 'all' ? cooking.value : cooking.value.filter(x => x.it.station_id === filter.value))
const showPacking = computed(() => filter.value === 'all' || filter.value === 'packing')
watch(() => cooking.value.length + toPack.value.length, n => emit('count', n), { immediate: true })
watch(posts, () => { if (typeof filter.value === 'number' && !myStations.value.some(p => p.station_id === filter.value)) filter.value = 'all' })

const source = (o: Order) => o.service_type === 'dine_in' ? `${t('wk.table', { n: o.table_name })} ${t('wk.dine_in')}` : `${o.public_no} ${t(o.service_type)}`
const readyCount = (o: Order) => live(o).filter(i => i.prep_status === 'ready').length
const missingCount = (o: Order) => live(o).length - readyCount(o)

async function run(fn: () => Promise<unknown>) {
  error.value = ''
  try { await fn() } catch (e) {
    error.value = e instanceof ApiError && e.code === 'MANIFEST_CHANGED' ? t('conflict') : (e as Error).message
  }
  await refreshNow()
}
const itemAction = (it: OrderItem, action: string) => run(() => post(`/kitchen/items/${it.id}/${action}?via=work`))

// ---- packing sheet: tick every line, count the bags, confirm ----
const packId = ref(0)
const pk = reactive({ manifest: 0, ticked: new Set<number>(), bags: 1 })
const packOrder = computed(() => packing.value.find(o => o.id === packId.value) ?? null)
function openPack(o: Order) { packId.value = o.id; Object.assign(pk, { manifest: o.manifest_version, ticked: new Set<number>(), bags: 1 }) }
// A change to the order clears the ticks: the packer must check it again.
watch(packOrder, o => { if (o && o.manifest_version !== pk.manifest) { pk.manifest = o.manifest_version; pk.ticked = new Set() } })
const tick = (it: OrderItem) => { pk.ticked.has(it.id) ? pk.ticked.delete(it.id) : pk.ticked.add(it.id) }
const allTicked = computed(() => !!packOrder.value && live(packOrder.value).every(i => pk.ticked.has(i.id)))
async function confirmPack() {
  const o = packOrder.value!
  await run(() => post(`/kitchen/orders/${o.id}/pack?via=work`, { bags: pk.bags, manifest_version: o.manifest_version }))
  if (!error.value) packId.value = 0
}
const handoff = (o: Order) => run(() => post(`/merchant/orders/${o.id}/handoff`))
</script>

<template>
  <div class="section stack" data-testid="work-store">
    <div v-if="!online" class="banner warn">{{ t('offline') }}</div>
    <div v-if="error" class="banner bad">{{ error }}</div>
    <button v-if="riderTasks" class="banner info" style="border:0;text-align: start;width:100%;cursor:pointer" data-testid="rider-busy" @click="emit('switch')">{{ t('wk.rider_busy', { n: riderTasks }) }} ›</button>

    <template v-if="tab === 'tasks'">
      <div v-if="duty.loaded" class="card duty" :class="{ on: duty.on }" data-testid="shift" :data-on="duty.on">
        <div class="row between">
          <div><b>{{ duty.on ? t('sh.on') : t('sh.off') }}</b>
            <div class="small muted">{{ duty.on ? t('sh.since', { t: sinceText }) : t('sh.off_hint') }}</div></div>
          <button class="btn" :class="{ primary: !duty.on }" :disabled="shiftBusy" data-act="shift" @click="toggleShift">{{ duty.on ? t('sh.end') : t('sh.start') }}</button>
        </div>
      </div>
      <div v-if="myStations.length + (packer ? 1 : 0) > 1" class="chips">
        <button :class="{ on: filter === 'all' }" @click="filter = 'all'">{{ t('wk.all') }} {{ cooking.length + toPack.length }}</button>
        <button v-for="p in myStations" :key="p.station_id!" :class="{ on: filter === p.station_id }" :data-station="p.station_name" @click="filter = p.station_id!">{{ pick(p.station_name ?? '', p.station_name_en ?? '') }} {{ stationCount(p.station_id) }}</button>
        <button v-if="packer" :class="{ on: filter === 'packing' }" @click="filter = 'packing'">{{ t('wk.packing') }} {{ toPack.length }}</button>
      </div>

      <div v-for="x in shownCooking" :key="x.it.id" class="card task" :class="x.it.prep_status" :data-task="x.it.name">
        <div><span class="q">{{ x.it.qty }} × {{ both(x.it.name, x.it.name_en) }}</span>
          <span v-if="x.it.prep_status === 'preparing'" class="tag info" style="margin-inline-start:6px">{{ t('i.preparing') }}</span>
          <div v-if="x.it.options.length" class="small">{{ x.it.options.map(o => both(o.name, o.name_en)).join(' · ') }}</div>
          <div v-if="x.it.note" class="small bold" style="color:var(--warn)">{{ t('note') }}：{{ x.it.note }}</div>
          <div class="src">{{ x.it.station_name }} · {{ source(x.o) }} · <span :class="{ late: x.waited >= 8 }">{{ t('wk.waited', { n: x.waited }) }}</span></div></div>
        <button v-if="x.it.prep_status === 'pending'" class="btn" data-act="start" @click="itemAction(x.it, 'start')">{{ t('wk.start') }}</button>
        <button v-else class="btn ok" data-act="ready" @click="itemAction(x.it, 'ready')">{{ t('wk.done') }}</button>
      </div>

      <template v-if="showPacking">
        <div v-for="o in packing" :key="o.id" class="card task" :data-pack="o.public_no">
          <div><span class="q">{{ o.public_no }}</span> <span class="tag" :class="{ ok: o.packing_status === 'packed', warn: o.packing_status === 'ready_to_pack' }">{{ t('p.' + o.packing_status) }}</span>
            <span v-if="o.scheduled_for" class="tag info" style="margin-inline-start:4px">{{ whenText(o.scheduled_for, o.timezone) }}</span>
            <div class="src">{{ t(o.service_type) }} ·
              <template v-if="o.packing_status === 'packed'">{{ t('bags') }} {{ o.bags }}<template v-if="o.service_type === 'pickup'"> · {{ t('pickup_code') }} <b>{{ o.pickup_code }}</b></template><template v-else> · {{ t('rider') }} {{ o.delivery?.rider_name ?? '—' }}</template></template>
              <template v-else-if="missingCount(o)">{{ t('wk.items_short', { n: missingCount(o) }) }}</template>
              <template v-else>{{ t('wk.items_ready', { n: readyCount(o) }) }}</template></div></div>
          <button v-if="o.packing_status === 'ready_to_pack'" class="btn primary" data-act="pack" @click="openPack(o)">{{ t('wk.go_pack') }}</button>
          <button v-else-if="o.packing_status === 'packed' && o.service_type === 'pickup'" class="btn" data-act="handoff" @click="handoff(o)">{{ t('handoff_customer') }}</button>
          <button v-else-if="o.packing_status === 'packed'" class="btn" :disabled="!o.delivery" data-act="handoff" @click="handoff(o)">{{ t('handoff_rider') }}</button>
        </div>
      </template>
      <div v-if="!shownCooking.length && !(showPacking && packing.length)" class="card muted" data-testid="store-empty">{{ t('wk.store_empty') }}</div>
    </template>

    <template v-else>
      <div class="muted small">{{ t('wk.ready_list') }}</div>
      <div v-for="x in ready" :key="x.it.id" class="card task" :data-ready="x.it.name">
        <div><span class="q">{{ x.it.qty }} × {{ both(x.it.name, x.it.name_en) }}</span><div class="src">{{ x.it.station_name }} · {{ source(x.o) }}</div></div>
        <span class="tag ok">{{ t('i.ready') }}</span>
      </div>
      <div v-if="!ready.length" class="card muted">{{ t('wk.done_empty') }}</div>
    </template>

    <div v-if="packOrder" class="sheet-mask" @click.self="packId = 0">
      <div class="sheet" data-testid="pack-sheet">
        <div class="sheet-head row between"><h2>{{ packOrder.public_no }} <span class="tag brand">{{ t(packOrder.service_type) }}</span></h2>
          <button class="btn link" @click="packId = 0">✕</button></div>
        <div class="sheet-body stack">
          <div v-if="packOrder.note" class="banner warn small">{{ t('order_note') }}: {{ packOrder.note }}</div>
          <label v-for="it in live(packOrder)" :key="it.id" class="check" style="align-items:flex-start">
            <input type="checkbox" :checked="pk.ticked.has(it.id)" @change="tick(it)" />
            <span><span v-if="packOrder.items.some(x => x.basket > 1)" class="tag info" style="margin-inline-end:4px">{{ t('basket_short', { n: it.basket }) }}</span><b>{{ it.qty }} × {{ both(it.name, it.name_en) }}</b>
              <span v-if="it.options.length" class="small muted"><br />{{ it.options.map(x => both(x.name, x.name_en)).join(' · ') }}</span>
              <span v-if="it.note" class="small bold" style="color:var(--warn)"><br />{{ t('note') }}: {{ it.note }}</span></span>
          </label>
          <div class="row between"><span>{{ t('bags') }}</span>
            <span class="stepper"><button @click="pk.bags = Math.max(1, pk.bags - 1)">−</button><b>{{ pk.bags }}</b><button class="fill" @click="pk.bags++">+</button></span></div>
        </div>
        <div class="sheet-foot"><button class="btn primary block big" :disabled="!allTicked || packOrder.packing_status !== 'ready_to_pack'" data-act="confirm-pack" @click="confirmPack">{{ allTicked ? t('confirm_pack') : t('check_all') }}</button></div>
      </div>
    </div>
  </div>
</template>
