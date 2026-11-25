<script setup lang="ts">
import './dine/strings'
import '../officeStrings'
import KitchenNote from './KitchenNote.vue'
import { computed, reactive, ref } from 'vue'
import { ApiError, get, post } from '../api'
import { both, minutesSince, t, whenText } from '../i18n'
import { useLive } from '../live'
import { session } from '../session'
import type { Order, OrderItem, Station } from '../types'
import { timerClass } from '../progress'
import { addonNo, addons } from '../addon'
import '../voids'

// Voids (docs/VOID_ITEM_DESIGN.md §8.3): a dish already started stays on top
// in red until a cook taps "got it"; one not started shows struck for a minute.
const unacked = computed(() => orders.value.flatMap(o => (o.voids ?? []).filter(v => v.needs_ack && !v.ack_at).map(v => ({ o, v }))))
const hasUnacked = (o: Order) => (o.voids ?? []).some(v => v.needs_ack && !v.ack_at)
const recentVoids = (o: Order) => { void now.value; return (o.voids ?? []).filter(v => !v.needs_ack && Date.now() - new Date(v.at).getTime() < 60000) }
const ack = (id: number) => run(() => post(`/kitchen/voids/${id}/ack`))
let knownVoids = new Set<number>()
let firstLoad = true
function voidBeep() {
  try {
    const ctx = new (window.AudioContext || (window as any).webkitAudioContext)()
    ;[660, 440].forEach((f, i) => {
      const o = ctx.createOscillator(), g = ctx.createGain()
      o.frequency.value = f; o.connect(g); g.connect(ctx.destination)
      g.gain.setValueAtTime(0.25, ctx.currentTime + i * 0.3)
      g.gain.exponentialRampToValueAtTime(0.001, ctx.currentTime + i * 0.3 + 0.4)
      o.start(ctx.currentTime + i * 0.3); o.stop(ctx.currentTime + i * 0.3 + 0.45)
    })
  } catch { /* audio not available */ }
}

const bid = session.branchId!
const orders = ref<Order[]>([])
// the store's cooking target colours the timers (docs/COOKING_PROGRESS_DESIGN.md §8)
const target = ref(15)
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
  const [b, s] = await Promise.all([get<{ orders: Order[]; target_minutes?: number }>(`/kitchen/branches/${bid}/board`), get<{ stations: Station[] }>(`/merchant/branches/${bid}/stations`)])
  orders.value = b.orders; stations.value = s.stations
  if (b.target_minutes) target.value = b.target_minutes
  if (typeof station.value === 'number' && !s.stations.some(x => x.id === station.value)) setStation('all')
  const ids = new Set(unacked.value.map(x => x.v.id))
  if (!firstLoad && [...ids].some(id => !knownVoids.has(id))) voidBeep()
  knownVoids = ids; firstLoad = false
  // a minute after a void not started, its struck line goes
  if (orders.value.some(o => recentVoids(o).length)) setTimeout(() => (now.value = Date.now()), 61000)
})

// A table's bill stays open for the whole meal: dishes already at the table drop off the screen.
const live = (o: Order) => o.items.filter(i => i.prep_status !== 'cancelled' && !i.served_at)
const dine = (o: Order) => o.service_type === 'dine_in'
// An order with dishes added in the last 3 minutes has an orange edge (docs/ADD_ON_ORDER_DESIGN.md §7.1).
const addedTo = (o: Order) => !dine(o) && addons(o).length > 0
const freshAdd = (o: Order) => { void now.value; const list = addons(o), b = list[list.length - 1]; return !dine(o) && !!b && Date.now() - new Date(b.at).getTime() < 180000 }
// Orders split into baskets show which basket each dish goes in.
const baskets = (o: Order) => o.items.some(i => i.basket > 1)
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
const itemAction = (it: OrderItem, action: string) => run(() => post(`/kitchen/items/${it.id}/${action}?via=board`))
const pack = (o: Order) => run(() => post(`/kitchen/orders/${o.id}/pack?via=board`, { bags: check(o).bags, manifest_version: o.manifest_version }))
const serve = (it: OrderItem) => run(() => post(`/merchant/bill-items/${it.id}/serve`))
const handoff = (o: Order) => run(() => post(`/merchant/orders/${o.id}/handoff`))

function tick(o: Order, it: OrderItem) {
  const c = check(o)
  c.ticked.has(it.id) ? c.ticked.delete(it.id) : c.ticked.add(it.id)
}
const allTicked = (o: Order) => live(o).every(i => check(o).ticked.has(i.id))
</script>

<template>
  <div class="ox warm">
  <div class="page ox-page kpage">
    <div class="ox-head">
      <div><h1>{{ t('kitchen') }}</h1><p class="ox-sub">{{ t('ox.kt.sub') }}</p></div>
      <span v-if="lastSync" class="ksync"><i :class="{ off: !online }"></i>{{ t('synced') }} {{ lastSync.toLocaleTimeString() }}</span>
    </div>
    <nav class="ox-tabs seg">
      <button :class="{ on: tab === 'cook' }" @click="tab = 'cook'">{{ t('to_cook') }} <span class="kn">{{ cooking.length }}</span></button>
      <button :class="{ on: tab === 'pack' }" @click="tab = 'pack'">{{ t('to_pack') }} <span class="kn">{{ packing.length }}</span></button>
      <button :class="{ on: tab === 'serve' }" data-test="tab-serve" @click="tab = 'serve'">{{ t('to_serve') }} <span class="kn">{{ serveCount }}</span></button>
    </nav>
    <div v-if="!online" class="banner warn">{{ t('offline') }}</div>
    <div v-if="error" class="banner bad">{{ error }}</div>

    <div v-if="unacked.length" class="stack" data-testid="k-voids">
      <div v-for="x in unacked" :key="x.v.id" class="kvoidalert" data-testid="k-void-alert">
        <span class="grow"><b>{{ t('vd.k.alert', { n: x.v.qty, name: x.v.name, r: x.v.reason }) }}</b>
          <span class="small"> · {{ dine(x.o) ? t('table') + ' ' + x.o.table_name : x.o.public_no }} · {{ whenText(x.v.at, x.o.timezone).split(' ').pop() }}</span></span>
        <button class="btn" data-testid="k-void-ack" @click="ack(x.v.id)">{{ t('vd.k.ack') }}</button>
      </div>
    </div>

    <div class="cols" v-if="cancelled.length">
      <div v-for="o in cancelled" :key="o.id" class="card" style="border-color:var(--bad);background:var(--bad-soft)">
        <h2>{{ o.public_no }}</h2>
        <p class="bold" style="color:var(--bad)">{{ t('cancelled_notice') }}</p>
        <div v-for="it in o.items" :key="it.id" class="strike">{{ it.qty }} × {{ both(it.name, it.name_en) }}</div>
      </div>
    </div>

    <!-- cooking: per-item progress, grouped by order (K01) -->
    <div v-if="tab === 'cook'" class="ox-body" :class="{ nostations: !stations.length }">
      <aside v-if="stations.length" class="ox-rail">
        <div class="ox-rail-h">{{ t('ox.kt.stations') }}</div>
        <div class="ox-rail-list">
          <button class="ox-cat" :class="{ on: station === 'all' }" @click="setStation('all')"><span class="nm">{{ t('all_stations') }}</span><span class="n">{{ cooking.length }}</span></button>
          <button v-for="s in tabs" :key="s.id" class="ox-cat" :class="{ on: station === s.id }" @click="setStation(s.id)">
            <span class="nm">{{ both(s.name, s.name_en) }}<span v-if="!s.active"> ({{ t('disabled') }})</span></span><span class="n">{{ openCount(s.id) }}</span></button>
          <button class="ox-cat" :class="{ on: station === 'none', warnc: unassigned > 0 }" @click="setStation('none')"><span class="nm">{{ t('unassigned') }}</span><span class="n">{{ unassigned }}</span></button>
        </div>
        <p class="ox-rail-tip">{{ t('ox.kt.tip') }}</p>
      </aside>
      <section class="kmain">
      <div v-if="unassigned > 0" class="banner warn">{{ t('unassigned_warn', { n: unassigned }) }}</div>
      <div v-if="shown.length === 0" class="card muted ox-empty">{{ t('kitchen_empty') }}</div>
      <div class="cols">
        <div v-for="o in shown" :key="o.id" class="card stack" :class="{ kfresh: freshAdd(o), kvoid: hasUnacked(o) }">
          <div class="row between">
            <h2><template v-if="dine(o)">{{ t('table') }} {{ o.table_name }}<span v-for="g in o.table_tags ?? []" :key="g" class="tag warn" style="margin-inline-start:4px">{{ g }}</span></template><template v-else>{{ o.public_no }}</template>
              <span class="tag brand" style="margin-inline-start:4px">{{ t(o.service_type) }}</span>
              <span v-if="addedTo(o)" class="tag kaddtag" style="margin-inline-start:4px" data-testid="k-addon">{{ t('ao.tag') }}</span>
              <span class="tag info" v-if="o.scheduled_for" style="margin-inline-start:4px">{{ t('scheduled') }} {{ whenText(o.scheduled_for, o.timezone) }}</span></h2>
            <span v-if="!dine(o)" class="tag" :class="timerClass(waited(o), target)">{{ waited(o) }} {{ t('min') }}</span>
            <span v-else class="small muted">{{ o.public_no }}</span>
          </div>
          <div v-if="o.note" class="banner warn small">{{ t('order_note') }}: {{ o.note }}</div>
          <div v-for="it in lines(o)" :key="it.id" class="kline" :class="it.prep_status">
            <div class="grow">
              <div class="kname"><span v-if="baskets(o)" class="tag info" style="margin-inline-end:4px;vertical-align:middle">{{ t('basket_short', { n: it.basket }) }}</span><span v-if="it.rush_at && it.prep_status !== 'ready'" class="tag bad krush" data-testid="k-rush">{{ t('dd.k.rush') }}</span>{{ it.qty }} × {{ both(it.name, it.name_en) }}<span v-if="dine(o)" class="small muted" style="font-weight:400"> · {{ t('batch_n', { n: it.batch_seq }) }}</span><span v-else-if="it.batch_seq > 1" class="small kadd" data-testid="k-addon-line"> · {{ t('ao.k', { n: addonNo(it.batch_seq) }) }}</span></div>
              <div v-if="it.round_tags?.length" class="small kround" data-testid="k-round">※ {{ it.round_tags.join(' · ') }}</div>
              <div class="small" v-if="stations.length && it.needs_prep">
                <span class="tag" :class="it.station_id === null ? 'warn' : 'info'">{{ it.station_name || t('unassigned') }}</span>
                <select v-if="open(it)" class="mv" :value="it.station_id ?? ''" :title="t('move_to')" :aria-label="t('move_to')" @change="move(it, $event)">
                  <option value="">{{ t('unassigned') }}</option>
                  <option v-for="s in stations.filter(s => s.active || s.id === it.station_id)" :key="s.id" :value="s.id">{{ t('move_to') }} {{ s.name }}</option>
                </select>
              </div>
              <div class="small" v-if="it.options.length">{{ it.options.map(x => both(x.name, x.name_en)).join(' · ') }}</div>
              <KitchenNote :it="it" />
            </div>
            <span v-if="!it.needs_prep" class="tag">{{ t('no_prep') }}</span>
            <template v-else>
              <button v-if="it.prep_status === 'pending'" class="btn small" @click="itemAction(it, 'start')">{{ t('start') }}</button>
              <button v-if="it.prep_status !== 'ready'" class="btn small ok" @click="itemAction(it, 'ready')">{{ t('ready') }}</button>
              <button v-else class="btn small" @click="itemAction(it, 'undo')">{{ t('undo') }}</button>
            </template>
          </div>
          <div v-for="v in recentVoids(o)" :key="'v' + v.id" class="kline" data-testid="k-voided"><span class="grow kname strike">{{ v.qty }} × {{ v.name }}</span><span class="tag bad">{{ t('vd.k.voided') }}</span></div>
        </div>
      </div>
      </section>
    </div>

    <!-- serving: dine-in dishes ready for the table -->
    <template v-else-if="tab === 'serve'">
      <div v-if="serving.length === 0" class="card muted ox-empty">{{ t('kitchen_empty') }}</div>
      <div class="cols">
        <div v-for="o in serving" :key="o.id" class="card stack">
          <div class="row between"><h2>{{ t('table') }} {{ o.table_name }}</h2><span class="small muted">{{ o.public_no }}</span></div>
          <div v-for="it in toServe(o)" :key="it.id" class="kline ready">
            <div class="grow">
              <div class="kname"><span v-if="it.rush_at" class="tag bad krush">{{ t('dd.k.rush') }}</span>{{ it.qty }} × {{ both(it.name, it.name_en) }}</div>
              <div class="small" v-if="it.options.length">{{ it.options.map(x => both(x.name, x.name_en)).join(' · ') }}</div>
              <KitchenNote :it="it" />
            </div>
            <button class="btn small ok" @click="serve(it)">{{ t('serve') }}</button>
          </div>
        </div>
      </div>
    </template>

    <!-- packing: whole-order check and handoff (K03/K04) -->
    <template v-else>
      <div v-if="packing.length === 0" class="card muted ox-empty">{{ t('kitchen_empty') }}</div>
      <div class="cols">
        <div v-for="o in packing" :key="o.id" class="card stack">
          <div class="row between">
            <h2>{{ o.public_no }} <span class="tag brand">{{ t(o.service_type) }}</span>
              <span class="tag info" v-if="o.scheduled_for" style="margin-inline-start:4px">{{ t('scheduled') }} {{ whenText(o.scheduled_for, o.timezone) }}</span></h2>
            <span class="tag" :class="{ ok: o.packing_status === 'packed', warn: o.packing_status === 'ready_to_pack' }">{{ t('p.' + o.packing_status) }}</span>
          </div>
          <div v-if="o.note" class="banner warn small">{{ t('order_note') }}: {{ o.note }}</div>

          <div v-if="addedTo(o) && o.bags && o.packing_status !== 'packed'" class="banner warn small" data-testid="k-repack">{{ t('ao.p.repack') }} · {{ t('bags') }} {{ o.bags }}</div>
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
                <template v-if="it.note || it.note_tags?.length"><br /><KitchenNote :it="it" inline /></template></span>
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
  </div>
</template>

<style scoped>
.kvoidalert { display: flex; align-items: center; gap: 10px; background: #fde8e6; color: #9b1c12; border: 2px solid #d92d20; border-radius: 10px; padding: 10px 12px; font-size: 16px; }
.kvoidalert .btn { background: #d92d20; color: #fff; border-color: #d92d20; min-height: 44px; }
.kvoid { border: 2px solid #d92d20 !important; }

.kfresh { border: 2px solid #f59e0b; }
.kaddtag { background: #fff1e0; color: #b45309; border-color: #f5c48a; }
.kadd { color: #b45309; font-weight: 600; }
.kpage .cols > .card { background: #fff; }
.kpage .cols > .card h2 { font-size: 18px; color: #2f1810; }
.kline { display: flex; align-items: center; gap: 8px; padding: 8px 10px; border-radius: 8px; background: #f7f1e7; border-inline-start: 5px solid var(--line); }
.kpage .ox-tabs.seg { display: flex; background: none; border-radius: 0; padding: 0; }
.kpage .ox-tabs.seg button { border-radius: 0; color: #5b4636; padding: 10px 18px; }
.kpage .ox-tabs.seg button.on { background: none; box-shadow: none; color: var(--brand); }
.kmain { display: grid; gap: 12px; min-width: 0; }
.ox-body.nostations { grid-template-columns: minmax(0, 1fr); }
.ox-cat.warnc:not(.on) .n { color: var(--bad); font-weight: 700; }
.kn { display: inline-grid; place-items: center; min-width: 22px; height: 22px; padding: 0 6px; border-radius: 11px; background: #efe5d6; font-size: 13px; color: #5b4636; }
.ox-tabs > .on .kn { background: var(--brand); color: #fff; }
.ksync { display: inline-flex; align-items: center; gap: 6px; color: var(--muted); font-size: 13px; align-self: center; }
.ksync i { width: 8px; height: 8px; border-radius: 50%; background: #2f7a2a; } .ksync i.off { background: var(--bad); }
.kline.preparing { border-inline-start-color: var(--warn); background: var(--warn-soft); }
.kline.ready { border-inline-start-color: var(--ok); background: var(--ok-soft); }
.kname { font-size: 17px; font-weight: 700; }
.mv { width: auto; padding: 2px 6px; font-size: 12px; margin-inline-start: 6px; }
.check.top { align-items: flex-start; }
.krush { margin-inline-end: 4px; vertical-align: middle; }
.kround { color: var(--warn, #b26a00); font-weight: 700; }
</style>
