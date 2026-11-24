<script setup lang="ts">
// Cooking progress (docs/COOKING_PROGRESS_DESIGN.md): every open order's
// dishes, by order or by dish, and what was finished today. It only reads;
// a dish can be rushed, the kitchen still starts and finishes it.
import '../progress'
import '../addon'
import './dine/strings'
import KitchenNote from './KitchenNote.vue'
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { get, post } from '../api'
import { both, minutesSince, t, whenText } from '../i18n'
import { useLive } from '../live'
import { can, session } from '../session'
import { batchSource, batchTitle, itemsByBatch, pendingAddon } from '../addon'
import { column, counts, kitchenLines, timerClass, type Column } from '../progress'
import type { Order, OrderBatch, OrderItem } from '../types'

interface DoneBatch {
  order_id: number; public_no: string; service_type: string; table_name: string; batch_seq: number; batches: number
  qty: number; kitchen_at: string; done_at: string; minutes: number; late: boolean; cooks: string[]
  acceptance_status: string; fulfillment_status: string; packing_status: string; rider_name: string; picked_up_at: string | null; served: boolean
}
interface Summary { count: number; avg_minutes: number; median_minutes: number; late: number }
interface DoneDay { date: string; timezone: string; target_minutes: number; summary: Summary; batches: DoneBatch[] }

const bid = session.branchId!
const route = useRoute(), router = useRouter()
const orders = ref<Order[]>([])
const target = ref(15)
const tz = ref<string | undefined>(undefined)
const today = ref<Summary>({ count: 0, avg_minutes: 0, median_minutes: 0, late: 0 })
const done = ref<DoneDay | null>(null)
const error = ref('')
const flash = ref('')
const loaded = ref(false)

type Tab = 'orders' | 'dishes' | 'done'
const tab = ref<Tab>((['orders', 'dishes', 'done'] as const).find(x => x === route.query.tab) ?? 'orders')
const type = ref<'all' | 'delivery' | 'pickup' | 'dine_in'>('all')
const station = ref<string>('all')
const sort = ref<'wait' | 'placed'>('wait')
const onlyLate = ref(false)
const split = ref(true)
const lane = ref<Column>('cook') // the phone shows one column at a time
const doneType = ref<'all' | 'delivery' | 'pickup' | 'dine_in'>('all')
type Svc = 'all' | 'delivery' | 'pickup' | 'dine_in'
const setType = (s: string) => { type.value = s as Svc }
const setDoneType = (s: string) => { doneType.value = s as Svc }
const doneLate = ref(false)
const selectedId = ref<number | null>(Number(route.query.order) || null)
const laterOpen = ref(false)

const now = ref(Date.now())
const tick = window.setInterval(() => (now.value = Date.now()), 20000)
onBeforeUnmount(() => window.clearInterval(tick))

async function load() {
  const p = await get<{ orders: Order[]; target_minutes: number; timezone: string; today: Summary }>(`/merchant/branches/${bid}/progress`)
  orders.value = p.orders; target.value = p.target_minutes; tz.value = p.timezone; today.value = p.today
  loaded.value = true
  if (tab.value === 'done') await loadDone()
}
async function loadDone() {
  done.value = await get<DoneDay>(`/merchant/branches/${bid}/progress/done`)
}
const { online, lastSync, refreshNow } = useLive(() => `/merchant/branches/${bid}/events`, load)
watch(tab, v => {
  flash.value = ''
  void router.replace({ query: { ...route.query, tab: v === 'orders' ? undefined : v } })
  if (v === 'done') void loadDone().catch(e => { error.value = (e as Error).message })
})

const clock = (iso: string | null | undefined) => iso
  ? new Date(iso).toLocaleTimeString('en-GB', { timeZone: tz.value, hour: '2-digit', minute: '2-digit' }) : ''
const since = (iso: string | null | undefined) => { void now.value; return iso ? minutesSince(iso) : 0 }
const tc = (m: number) => timerClass(m, target.value)
const typeTag = (s: string) => s === 'delivery' ? 'del' : s === 'pickup' ? 'pick' : 'dine'

// stations seen on today's dishes, unassigned last
const stationKey = (i: OrderItem) => i.station_id == null ? 'none' : String(i.station_id)
const stationName = (i: OrderItem) => i.station_id == null ? t('unassigned') : i.station_name
const stations = computed(() => {
  const m = new Map<string, { key: string; name: string; n: number }>()
  for (const o of live.value) for (const i of kitchenLines(o)) {
    const k = stationKey(i)
    if (!m.has(k)) m.set(k, { key: k, name: stationName(i), n: 0 })
    if (i.prep_status !== 'ready') m.get(k)!.n += i.qty
  }
  return [...m.values()].sort((a, b) => (a.key === 'none' ? 1 : 0) - (b.key === 'none' ? 1 : 0) || a.name.localeCompare(b.name))
})

const released = (o: Order) => { void now.value; return !o.release_at || new Date(o.release_at).getTime() <= Date.now() }
const live = computed(() => orders.value.filter(o => o.acceptance_status === 'accepted' && released(o)))
const later = computed(() => orders.value.filter(o => o.acceptance_status === 'accepted' && !released(o)))
const cancelled = computed(() => orders.value.filter(o => o.acceptance_status === 'cancelled'))

interface Card { o: Order; lines: OrderItem[]; col: Column; minutes: number; free: number }
function card(o: Order): Card | null {
  const all = kitchenLines(o)
  if (o.service_type === 'dine_in' && !all.length) return null // everything is at the table
  const lines = station.value === 'all' ? all : all.filter(i => stationKey(i) === station.value)
  if (station.value !== 'all' && !lines.length) return null
  const starts = (lines.length ? lines : all).map(i => i.kitchen_at).filter(Boolean) as string[]
  const start = starts.sort()[0] ?? o.release_at ?? o.accepted_at ?? o.created_at
  const free = o.items.filter(i => !i.needs_prep && i.prep_status !== 'cancelled' && !i.served_at).reduce((n, i) => n + i.qty, 0)
  return { o, lines, col: column(lines), minutes: since(start), free }
}
const allCards = computed(() => live.value.map(card).filter((c): c is Card => !!c))
const typeCount = (s: string) => allCards.value.filter(c => c.o.service_type === s).length
const cards = computed(() => {
  let l = allCards.value.filter(c => type.value === 'all' || c.o.service_type === type.value)
  if (onlyLate.value) l = l.filter(c => c.minutes > target.value)
  return [...l].sort(sort.value === 'wait' ? (a, b) => b.minutes - a.minutes
    : (a, b) => (a.o.accepted_at ?? '').localeCompare(b.o.accepted_at ?? ''))
})
const byCol = (c: Column) => cards.value.filter(x => x.col === c)
const lateCount = computed(() => allCards.value.filter(c => c.minutes > target.value).length)
const readyCount = computed(() => allCards.value.filter(c => c.col === 'done').length)
const cols: Column[] = ['wait', 'cook', 'done']
const colColor = { wait: '#9ca3af', cook: '#3b82f6', done: '#22a05a' }

function lineText(o: Order, i: OrderItem): { text: string; cls: string } {
  if (!i.needs_prep) return { text: t('no_prep'), cls: '' }
  if (i.served_at) return { text: t('kp.d.served'), cls: 'd' }
  const st = i.station_id != null || stations.value.length > 1 ? stationName(i) + ' · ' : ''
  if (i.prep_status === 'ready') return { text: st + t('kp.d.ready_at', { t: clock(i.ready_at) }), cls: 'd' }
  if (i.prep_status === 'preparing') return { text: st + t('kp.d.cook_min', { n: since(i.prep_started_at) }), cls: 'p' }
  const m = since(i.kitchen_at)
  return { text: st + (m >= 1 ? t('kp.d.wait_min', { n: m }) : t('kp.d.wait')), cls: m > target.value ? 'r' : '' }
}
const dot = (i: OrderItem) => !i.needs_prep ? 'x' : i.prep_status === 'ready' || i.served_at ? 'd' : i.prep_status === 'preparing' ? 'p' : ''
const optText = (i: OrderItem) => i.options.map(x => both(x.name, x.name_en)).join(' · ')
const title = (o: Order) => o.service_type === 'dine_in' ? `${t('table')} ${o.table_name}` : o.public_no
function outStatus(o: Order): string {
  if (o.service_type === 'dine_in') return t('kp.st.to_serve')
  if (o.fulfillment_status === 'awaiting_handoff') return t('kp.st.to_rider')
  if (o.fulfillment_status === 'awaiting_pickup') return t('kp.st.to_collect')
  return o.service_type === 'pickup' && o.packing_status === 'packed' ? t('kp.st.to_collect') : t('kp.st.to_pack')
}

// the card's dishes by batch; the original order has no header unless it has add-ons
function groups(c: Card) {
  const ids = new Set(c.lines.map(i => i.id))
  const free = station.value === 'all' ? c.o.items.filter(i => !i.needs_prep && i.prep_status !== 'cancelled' && !i.served_at) : []
  const keep = (i: OrderItem) => ids.has(i.id) || free.includes(i)
  return itemsByBatch(c.o).map(g => ({ ...g, items: g.items.filter(keep) })).filter(g => g.items.length)
}
const hasHeads = (c: Card) => c.o.service_type === 'dine_in' || groups(c).some(g => g.seq > 1) || !!pendingAddon(c.o)
function batchHead(o: Order, seq: number, b: OrderBatch | null): string {
  const name = o.service_type === 'dine_in' ? t('kp.round', { n: seq }) : batchTitle(seq)
  const parts = [b ? clock(b.at) : '']
  if (b?.round_tags?.length) parts.push(b.round_tags.join(' · '))
  return `${name} ${parts.filter(Boolean).join(' · ')}`
}
function batchMinutes(items: OrderItem[]): number | null {
  const open = items.filter(i => i.needs_prep && i.prep_status !== 'ready' && !i.served_at)
  if (!open.length) return null
  return since(open.map(i => i.kitchen_at ?? '').sort()[0] || null)
}

// ---- the drawer ----
const selected = computed(() => allCards.value.find(c => c.o.id === selectedId.value) ?? null)
function pick(id: number | null) {
  selectedId.value = id
  void router.replace({ query: { ...route.query, order: id ?? undefined } })
}
const drawerGroups = computed(() => selected.value
  ? itemsByBatch(selected.value.o).map(g => ({ ...g, items: g.items.filter(i => i.prep_status !== 'cancelled') })).filter(g => g.items.length) : [])
const canRush = computed(() => can('order.handle'))
// who started and finished a dish, and when it was rushed
function timeline(i: OrderItem): string {
  const made = [i.prep_started_at ? t('kp.tl.start', { who: i.started_by || '—', t: clock(i.prep_started_at) }) : '',
    i.ready_at ? t('kp.tl.ready', { who: i.ready_by || '—', t: clock(i.ready_at) }) : ''].filter(Boolean).join(' → ')
  const rushed = i.rush_at && i.prep_status !== 'ready' ? t('kp.tl.rush', { t: clock(i.rush_at) }) : ''
  return [made, rushed].filter(Boolean).join(' · ')
}
async function rush(i: OrderItem) {
  error.value = ''; flash.value = ''
  try {
    await post(`/merchant/order-items/${i.id}/rush`); flash.value = t('kp.rushed')
    window.setTimeout(() => { flash.value = '' }, 5000)
  } catch (e) { error.value = (e as Error).message }
  await refreshNow()
}
function openOrder(o: Order) {
  void router.push(o.service_type === 'dine_in' ? '/merchant/orders/new?mode=dine' : `/merchant/orders?order=${o.id}`)
}

// ---- by dish ----
interface DishRow { key: string; name: string; flavour: string; wait: number; cook: number; ready: number; longest: number; orders: { id: number; label: string; late: boolean }[] }
const dishGroups = computed(() => {
  const st = new Map<string, { key: string; name: string; cook: number; wait: number; rows: Map<string, DishRow> }>()
  for (const c of allCards.value) {
    if (type.value !== 'all' && c.o.service_type !== type.value) continue
    for (const i of c.lines) {
      const sk = stationKey(i)
      if (!st.has(sk)) st.set(sk, { key: sk, name: stationName(i), cook: 0, wait: 0, rows: new Map() })
      const g = st.get(sk)!
      const flavour = split.value ? [optText(i), ...(i.note_tags ?? []).map(x => x.name), i.note].filter(Boolean).join(' · ') : ''
      const rk = i.item_id + '|' + flavour
      if (!g.rows.has(rk)) g.rows.set(rk, { key: rk, name: both(i.name, i.name_en), flavour, wait: 0, cook: 0, ready: 0, longest: 0, orders: [] })
      const r = g.rows.get(rk)!
      if (i.prep_status === 'ready') r.ready += i.qty
      else if (i.prep_status === 'preparing') { r.cook += i.qty; g.cook += i.qty }
      else { r.wait += i.qty; g.wait += i.qty }
      if (i.prep_status !== 'ready') r.longest = Math.max(r.longest, since(i.kitchen_at))
      const label = title(c.o) + (i.batch_seq > 1 && c.o.service_type !== 'dine_in' ? ' ' + batchTitle(i.batch_seq) : '')
      if (!r.orders.some(x => x.id === c.o.id)) r.orders.push({ id: c.o.id, label, late: c.minutes > target.value })
    }
  }
  const out = [...st.values()].map(g => ({ ...g, rows: [...g.rows.values()].sort((a, b) => b.longest - a.longest) }))
    .sort((a, b) => (a.key === 'none' ? 1 : 0) - (b.key === 'none' ? 1 : 0) || (b.cook + b.wait) - (a.cook + a.wait))
  return out
})
const maxLoad = computed(() => Math.max(1, ...dishGroups.value.map(g => g.cook + g.wait)))

// ---- finished ----
const doneRows = computed(() => (done.value?.batches ?? [])
  .filter(b => (doneType.value === 'all' || b.service_type === doneType.value) && (!doneLate.value || b.late)))
function doneTitle(b: DoneBatch): string {
  if (b.service_type === 'dine_in') return `${t('table')} ${b.table_name} · ${t('kp.round', { n: b.batch_seq })}`
  return b.batches > 1 ? `${b.public_no} · ${batchTitle(b.batch_seq)}` : b.public_no
}
function doneNow(b: DoneBatch): string {
  if (b.acceptance_status === 'cancelled') return t('kp.done.cancelled')
  if (b.service_type === 'dine_in') return b.fulfillment_status === 'dined' ? t('kp.done.closed') : b.served ? t('kp.done.served') : t('kp.st.to_serve')
  if (b.picked_up_at) return t('kp.done.picked', { who: b.rider_name, t: clock(b.picked_up_at) })
  if (b.fulfillment_status === 'delivered') return t('kp.done.delivered')
  if (b.fulfillment_status === 'picked_up' || b.fulfillment_status === 'completed') return t('kp.done.collected')
  if (['none', 'awaiting_handoff', 'awaiting_pickup'].includes(b.fulfillment_status)) return t('kp.done.waiting')
  return t('kp.done.out')
}
</script>

<template>
  <div class="page cp">
    <div class="row between wrap cp-top">
      <div class="seg light">
        <button :class="{ on: tab === 'orders' }" data-testid="cp-tab-orders" @click="tab = 'orders'">{{ t('kp.tab.orders') }}</button>
        <button :class="{ on: tab === 'dishes' }" data-testid="cp-tab-dishes" @click="tab = 'dishes'">{{ t('kp.tab.dishes') }}</button>
        <button :class="{ on: tab === 'done' }" data-testid="cp-tab-done" @click="tab = 'done'">{{ t('kp.tab.done_n', { n: today.count }) }}</button>
      </div>
      <span class="muted small" v-if="lastSync">{{ t('kp.live', { t: lastSync.toLocaleTimeString() }) }}</span>
    </div>
    <div v-if="!online" class="banner warn">{{ t('offline') }}</div>
    <div v-if="error" class="banner bad">{{ error }}</div>
    <div v-else-if="flash" class="banner ok" data-testid="cp-flash">{{ flash }}</div>

    <template v-if="tab !== 'done'">
      <div class="cp-stats">
        <div class="cp-stat"><b data-testid="cp-active">{{ allCards.length }}</b><small>{{ t('kp.s.active') }}</small></div>
        <button class="cp-stat" :class="{ red: lateCount > 0, on: onlyLate }" data-testid="cp-late" @click="onlyLate = !onlyLate"><b>{{ lateCount }}</b><small>{{ t('kp.s.late', { n: target }) }}</small></button>
        <div class="cp-stat green"><b data-testid="cp-ready">{{ readyCount }}</b><small>{{ t('kp.s.ready') }}</small></div>
        <div class="cp-stat"><b>{{ today.count ? t('kp.mins', { n: today.avg_minutes }) : '—' }}</b><small>{{ t('kp.s.avg') }}</small></div>
        <div class="cp-stat gray" v-if="later.length"><b>{{ later.length }}</b><small>{{ t('kp.s.later') }}</small></div>
      </div>
      <div v-for="o in cancelled" :key="o.id" class="banner bad" data-testid="cp-cancelled">
        {{ t('kp.cancelled', { no: title(o), t: clock(o.closed_at), s: kitchenLines(o).filter(i => i.prep_status !== 'ready').map(i => `${i.qty} × ${both(i.name, i.name_en)}`).join('、') || '—' }) }}</div>
      <div class="row wrap cp-filters">
        <button class="btn small" :class="{ primary: type === 'all' }" @click="type = 'all'">{{ t('kp.all', { n: allCards.length }) }}</button>
        <button v-for="s in ['delivery', 'pickup', 'dine_in']" :key="s" class="btn small" :class="{ primary: type === s }" @click="setType(s)">{{ t('kp.type_n', { s: t(s), n: typeCount(s) }) }}</button>
        <template v-if="tab === 'orders' && stations.length > 1">
          <span class="cp-sep"></span>
          <button class="btn small" :class="{ primary: station === 'all' }" @click="station = 'all'">{{ t('kp.all_stations') }}</button>
          <button v-for="s in stations" :key="s.key" class="btn small" :class="{ primary: station === s.key }" @click="station = s.key">{{ s.name }} {{ s.n }}</button>
        </template>
        <span class="cp-sep"></span>
        <select v-if="tab === 'orders'" v-model="sort" class="cp-sel" :aria-label="t('kp.sort.wait')">
          <option value="wait">{{ t('kp.sort.wait') }}</option><option value="placed">{{ t('kp.sort.placed') }}</option>
        </select>
        <template v-else>
          <button class="btn small" :class="{ primary: split }" @click="split = true">{{ t('kp.dish.split') }}</button>
          <button class="btn small" :class="{ primary: !split }" @click="split = false">{{ t('kp.dish.merge') }}</button>
        </template>
      </div>
    </template>

    <!-- by order: three columns (one at a time on a phone) -->
    <template v-if="tab === 'orders'">
      <div class="seg light cp-lanes">
        <button v-for="c in cols" :key="c" :class="{ on: lane === c }" @click="lane = c">{{ t('kp.col.' + c) }} {{ byCol(c).length }}</button>
      </div>
      <div v-if="loaded && !allCards.length" class="card muted">{{ t('kp.empty') }}</div>
      <div class="cp-board">
        <div v-for="c in cols" :key="c" class="cp-col" :class="{ shown: lane === c }" :data-testid="'cp-col-' + c">
          <div class="cp-colh"><span class="dot" :style="{ background: colColor[c] }"></span>{{ t('kp.col.' + c) }} · {{ byCol(c).length }}<small>{{ t('kp.col.' + c + '_hint') }}</small></div>
          <button v-for="x in byCol(c)" :key="x.o.id" class="cp-card" :class="{ late: x.minutes > target, sel: x.o.id === selectedId }" :data-order="x.o.public_no" @click="pick(x.o.id)">
            <div class="cp-ch">
              <b class="cp-no">{{ title(x.o) }}</b>
              <span class="tag" :class="'t-' + typeTag(x.o.service_type)">{{ t(x.o.service_type) }}</span>
              <span v-if="x.o.service_type !== 'dine_in' && (x.o.batches ?? []).some(b => b.seq > 1 && b.status === 'accepted')" class="tag t-add">{{ t('ao.tag') }}</span>
              <span class="cp-timer" :class="tc(x.minutes)">{{ t('kp.mins', { n: x.minutes }) }}</span>
            </div>
            <div class="cp-bar"><i class="g" :style="{ width: (counts(x.lines).total ? counts(x.lines).ready / counts(x.lines).total * 100 : 100) + '%' }"></i><i class="b" :style="{ width: (counts(x.lines).total ? counts(x.lines).cooking / counts(x.lines).total * 100 : 0) + '%' }"></i></div>
            <div class="cp-barl"><span data-testid="cp-count">{{ t('kp.progress', { a: counts(x.lines).ready, b: counts(x.lines).total }) }}<template v-if="counts(x.lines).cooking"> · {{ t('kp.progress_cook', { n: counts(x.lines).cooking }) }}</template><template v-if="x.free && station === 'all'"> · {{ t('kp.free', { n: x.free }) }}</template></span>
              <b v-if="c === 'done'" class="cp-out">{{ outStatus(x.o) }}</b></div>
            <template v-for="g in groups(x)" :key="g.seq">
              <div v-if="hasHeads(x)" class="cp-bh" :class="{ add: g.seq > 1 }"><b>{{ batchHead(x.o, g.seq, g.batch) }}</b>
                <span v-if="g.seq > 1 && batchMinutes(g.items) !== null" class="r">{{ t('kp.mins', { n: batchMinutes(g.items)! }) }}</span></div>
              <div v-for="i in g.items" :key="i.id" class="cp-dl" :class="{ muted: !i.needs_prep }">
                <span class="cp-dot" :class="dot(i)"></span>
                <span class="n">{{ i.qty }} × {{ both(i.name, i.name_en) }}<span v-if="i.rush_at && i.prep_status !== 'ready'" class="tag bad cp-rush">{{ t('kp.rush_tag') }}</span></span>
                <span class="m" :class="lineText(x.o, i).cls">{{ lineText(x.o, i).text }}</span>
              </div>
            </template>
            <div v-if="pendingAddon(x.o)" class="cp-bh pend"><b>{{ batchTitle(pendingAddon(x.o)!.seq) }}</b> {{ clock(pendingAddon(x.o)!.at) }} · {{ t('kp.pending_batch') }}</div>
          </button>
          <div v-if="!byCol(c).length" class="muted small cp-none">{{ t('kp.col.empty') }}</div>
          <div v-if="c === 'wait' && later.length" class="cp-later">
            <button class="btn link small" @click="laterOpen = !laterOpen">{{ laterOpen ? '▾' : '▸' }} {{ t('kp.later', { n: later.length }) }}</button>
            <div v-if="laterOpen" class="small muted">
              <div v-for="o in later" :key="o.id">{{ title(o) }} · {{ t('kp.later_at', { t: whenText(o.release_at!, o.timezone) }) }}</div>
            </div>
          </div>
        </div>
      </div>
      <p class="small muted">{{ t('kp.foot') }}</p>
    </template>

    <!-- by dish -->
    <template v-if="tab === 'dishes'">
      <div v-if="!dishGroups.length" class="card muted">{{ t('kp.dish.empty') }}</div>
      <div v-for="g in dishGroups" :key="g.key" class="card cp-dg" data-testid="cp-dish-group">
        <div class="cp-dgh"><h3>{{ g.name }}</h3><small class="muted">{{ t('kp.dish.station', { a: g.cook, b: g.wait }) }}</small>
          <span class="cp-load"><i :style="{ width: (g.cook + g.wait) / maxLoad * 100 + '%' }"></i></span>
          <small v-if="dishGroups.length > 1 && g === dishGroups[0] && g.key !== 'none' && g.cook + g.wait > 0" class="cp-busy">{{ t('kp.dish.busiest') }}</small></div>
        <div class="cp-table">
          <div class="cp-tr head"><span class="nm">{{ t('kp.dish.col.name') }}</span><span>{{ t('kp.dish.col.wait') }}</span><span>{{ t('kp.dish.col.cook') }}</span><span>{{ t('kp.dish.col.ready') }}</span><span>{{ t('kp.dish.col.longest') }}</span><span class="os">{{ t('kp.dish.col.orders') }}</span></div>
          <div v-for="r in g.rows" :key="r.key" class="cp-tr" :class="{ late: r.longest > target }" data-testid="cp-dish-row">
            <span class="nm"><b>{{ r.name }}</b><small v-if="r.flavour" class="cp-fl"> · {{ r.flavour }}</small>
              <small class="cp-short">{{ t('kp.dish.short', { a: r.wait, b: r.cook, c: r.ready }) }}</small></span>
            <span class="num" :class="{ zero: !r.wait }">{{ r.wait }}</span>
            <span class="num" :class="{ zero: !r.cook }">{{ r.cook }}</span>
            <span class="num" :class="{ zero: !r.ready }">{{ r.ready }}</span>
            <span class="lg" :class="tc(r.longest)">{{ r.wait + r.cook ? t('kp.mins', { n: r.longest }) : '—' }}</span>
            <span class="os"><button v-for="o in r.orders" :key="o.id" class="ono" :class="{ r: o.late }" @click="tab = 'orders'; pick(o.id)">{{ o.label }}</button></span>
          </div>
        </div>
      </div>
    </template>

    <!-- finished today -->
    <template v-if="tab === 'done'">
      <div class="cp-stats" v-if="done">
        <div class="cp-stat"><b data-testid="cp-done-count">{{ done.summary.count }}</b><small>{{ t('kp.done.count') }}</small></div>
        <div class="cp-stat green"><b>{{ done.summary.count ? t('kp.mins', { n: done.summary.avg_minutes }) : '—' }}</b><small>{{ t('kp.done.avg') }}</small></div>
        <button class="cp-stat" :class="{ red: done.summary.late > 0, on: doneLate }" @click="doneLate = !doneLate"><b>{{ done.summary.late }}</b><small>{{ t('kp.done.late', { n: done.target_minutes }) }}</small></button>
        <div class="cp-stat"><b>{{ done.summary.count ? t('kp.mins', { n: done.summary.median_minutes }) : '—' }}</b><small>{{ t('kp.done.median') }}</small></div>
      </div>
      <div class="row wrap cp-filters">
        <button class="btn small" :class="{ primary: doneType === 'all' }" @click="doneType = 'all'">{{ t('kp.all', { n: done?.batches.length ?? 0 }) }}</button>
        <button v-for="s in ['delivery', 'pickup', 'dine_in']" :key="s" class="btn small" :class="{ primary: doneType === s }" @click="setDoneType(s)">{{ t(s) }}</button>
        <router-link v-if="can('staff.reports')" class="small cp-report" to="/merchant/staff/kitchen">{{ t('kp.done.report') }}</router-link>
      </div>
      <div class="card cp-done">
        <div v-if="!doneRows.length" class="muted">{{ t('kp.done.empty') }}</div>
        <table v-else>
          <thead><tr><th>{{ t('kp.done.col.no') }}</th><th>{{ t('kp.done.col.type') }}</th><th>{{ t('kp.done.col.qty') }}</th><th>{{ t('kp.done.col.in') }}</th><th>{{ t('kp.done.col.done') }}</th><th>{{ t('kp.done.col.mins') }}</th><th class="hide-sm">{{ t('kp.done.col.cooks') }}</th><th class="hide-sm">{{ t('kp.done.col.now') }}</th></tr></thead>
          <tbody>
            <tr v-for="b in doneRows" :key="b.order_id + '-' + b.batch_seq" :class="{ late: b.late }" data-testid="cp-done-row">
              <td><b>{{ doneTitle(b) }}</b></td>
              <td><span class="tag" :class="'t-' + typeTag(b.service_type)">{{ t(b.service_type) }}</span></td>
              <td>{{ t('kp.done.qty', { n: b.qty }) }}</td>
              <td>{{ clock(b.kitchen_at) }}</td>
              <td>{{ clock(b.done_at) }}</td>
              <td :class="b.late ? 'cp-lt' : 'bold'">{{ b.late ? t('kp.done.late_tag', { n: b.minutes }) : t('kp.mins', { n: b.minutes }) }}</td>
              <td class="hide-sm">{{ b.cooks.join('、') || '—' }}</td>
              <td class="hide-sm muted">{{ doneNow(b) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>

    <!-- one order's dishes, who made them and when -->
    <div v-if="selected" class="cp-shade" @click="pick(null)"></div>
    <aside v-if="selected" class="cp-drawer" data-testid="cp-drawer">
      <div class="cp-ch">
        <b class="cp-no big">{{ title(selected.o) }}</b>
        <span class="tag" :class="'t-' + typeTag(selected.o.service_type)">{{ t(selected.o.service_type) }}</span>
        <span class="cp-timer" :class="tc(selected.minutes)">{{ t('kp.mins', { n: selected.minutes }) }}</span>
        <button class="btn link cp-x" :aria-label="t('kp.close')" @click="pick(null)">×</button>
      </div>
      <div class="small muted">{{ [selected.o.customer_name, t('kp.into_kitchen', { t: clock(selected.lines.map(i => i.kitchen_at ?? '').sort()[0] || selected.o.accepted_at), n: target })].filter(Boolean).join(' · ') }}</div>
      <div class="cp-bar big"><i class="g" :style="{ width: (counts(selected.lines).total ? counts(selected.lines).ready / counts(selected.lines).total * 100 : 100) + '%' }"></i><i class="b" :style="{ width: (counts(selected.lines).total ? counts(selected.lines).cooking / counts(selected.lines).total * 100 : 0) + '%' }"></i></div>
      <div class="cp-barl"><span>{{ t('kp.progress', { a: counts(selected.lines).ready, b: counts(selected.lines).total }) }}<template v-if="counts(selected.lines).cooking"> · {{ t('kp.progress_cook', { n: counts(selected.lines).cooking }) }}</template></span>
        <span v-if="selected.free">{{ t('kp.free', { n: selected.free }) }}</span></div>
      <div class="cp-scroll">
        <template v-for="g in drawerGroups" :key="g.seq">
          <div class="cp-bh" :class="{ add: g.seq > 1 }"><b>{{ batchHead(selected.o, g.seq, g.batch) }}</b><span v-if="g.batch" class="muted"> · {{ batchSource(selected.o, g.batch) }}</span>
            <span v-if="batchMinutes(g.items) !== null" class="r">{{ t('kp.mins', { n: batchMinutes(g.items)! }) }}</span></div>
          <div v-for="i in g.items" :key="i.id" class="cp-dline" data-testid="cp-dline">
            <div class="cp-dl" :class="{ muted: !i.needs_prep }">
              <span class="cp-dot" :class="dot(i)"></span>
              <span class="n">{{ i.qty }} × {{ both(i.name, i.name_en) }}<span v-if="i.rush_at && i.prep_status !== 'ready'" class="tag bad cp-rush">{{ t('kp.rush_tag') }}</span></span>
              <span class="m" :class="lineText(selected.o, i).cls">{{ lineText(selected.o, i).text }}</span>
              <button v-if="canRush && i.needs_prep && !i.rush_at && (i.prep_status === 'pending' || i.prep_status === 'preparing')" class="btn small cp-rushbtn" data-testid="cp-rush" @click="rush(i)">{{ t('kp.rush') }}</button>
            </div>
            <div v-if="optText(i)" class="cp-tl">{{ optText(i) }}</div>
            <div class="cp-tl"><KitchenNote :it="i" /></div>
            <div class="cp-tl" v-if="timeline(i)">{{ timeline(i) }}</div>
          </div>
        </template>
        <template v-if="pendingAddon(selected.o)">
          <div class="cp-bh pend"><b>{{ batchTitle(pendingAddon(selected.o)!.seq) }}</b> {{ clock(pendingAddon(selected.o)!.at) }} · {{ t('kp.pending_batch') }}</div>
          <div v-for="(l, n) in pendingAddon(selected.o)!.lines ?? []" :key="n" class="cp-dl muted"><span class="cp-dot x"></span><span class="n">{{ l.qty }} × {{ both(l.name, l.name_en) }}</span><span class="m">{{ t('kp.pending_note') }}</span></div>
        </template>
      </div>
      <div class="row cp-actions">
        <button class="btn primary grow" data-testid="cp-open" @click="openOrder(selected.o)">{{ t('kp.open') }}</button>
        <a class="btn" :href="`/ticket/${selected.o.id}`" target="_blank">{{ t('kp.print') }}</a>
      </div>
      <p class="small muted">{{ t('kp.only_board') }}</p>
    </aside>
  </div>
</template>

<style scoped>
.cp { display: flex; flex-direction: column; gap: 12px; }
.cp-stats { display: flex; gap: 10px; flex-wrap: wrap; }
.cp-stat { background: #fff; border: 1px solid var(--line, #e5e7eb); border-radius: 12px; padding: 10px 16px; min-width: 140px; text-align: start; font: inherit; color: inherit; }
button.cp-stat { cursor: pointer; }
.cp-stat b { font-size: 22px; display: block; } .cp-stat small { color: #6b7280; }
.cp-stat.red { border-color: #f3b4a8; background: #fff3f0; } .cp-stat.red b { color: #b42318; }
.cp-stat.on { outline: 2px solid #d92d20; }
.cp-stat.green b { color: #1f7a45; } .cp-stat.gray b { color: #6b7280; }
.cp-filters { gap: 6px; align-items: center; }
.cp-sep { width: 1px; height: 22px; background: #d1d5db; margin: 0 4px; }
.cp-sel { width: auto; padding: 5px 8px; }
.cp-lanes { display: none; }
.cp-board { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 12px; align-items: start; }
.cp-col { background: #efede9; border-radius: 14px; padding: 10px; display: flex; flex-direction: column; gap: 10px; min-width: 0; }
.cp-colh { display: flex; align-items: center; gap: 8px; font-weight: 800; font-size: 15px; padding: 2px 4px; }
.cp-colh .dot { width: 10px; height: 10px; border-radius: 50%; }
.cp-colh small { color: #6b7280; font-weight: 500; margin-inline-start: auto; font-size: 12px; }
.cp-none { padding: 4px 6px; }
.cp-card { display: block; width: 100%; text-align: start; font: inherit; color: inherit; background: #fff; border: 1px solid #e5e7eb; border-radius: 12px; padding: 10px 12px; cursor: pointer; }
.cp-card.late { border-color: #f3b4a8; box-shadow: inset 4px 0 0 #d92d20; }
.cp-card.sel { outline: 2px solid var(--brand); }
.cp-ch { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
.cp-no { font-size: 16px; } .cp-no.big { font-size: 22px; }
.tag.t-del { background: #fde9cf; color: #c2650a; } .tag.t-pick { background: #e0f2fe; color: #0369a1; } .tag.t-dine { background: #ede9fe; color: #6d28d9; }
.tag.t-add { background: #fff1e2; color: #a2510a; border: 1px solid #f6c58f; }
.cp-timer { margin-inline-start: auto; font-weight: 800; font-size: 13px; border-radius: 999px; padding: 2px 10px; background: #eceef0; color: #374151; white-space: nowrap; }
.cp-timer.warn { background: #fef3c7; color: #a15c00; } .cp-timer.bad { background: #d92d20; color: #fff; }
.cp-bar { height: 6px; background: #eceef0; border-radius: 999px; margin: 8px 0 2px; overflow: hidden; display: flex; }
.cp-bar.big { height: 8px; }
.cp-bar i { display: block; height: 100%; } .cp-bar .g { background: #22a05a; } .cp-bar .b { background: #3b82f6; }
.cp-barl { display: flex; justify-content: space-between; gap: 8px; font-size: 12px; color: #6b7280; }
.cp-out { color: #1f7a45; white-space: nowrap; }
.cp-bh { font-size: 12px; color: #7b6657; background: #f6efe4; border-radius: 6px; padding: 3px 8px; margin-top: 7px; display: flex; gap: 6px; align-items: center; }
.cp-bh b { color: #3b1f14; font-weight: 600; } .cp-bh.add { background: #fff1e2; color: #a2510a; } .cp-bh .r { margin-inline-start: auto; font-weight: 700; }
.cp-bh.pend { background: #fafafa; border: 1px dashed #c7c9cc; color: #6b7280; }
.cp-dl { display: flex; align-items: center; gap: 8px; padding: 5px 2px; font-size: 13.5px; }
.cp-dl .n { flex: 1; min-width: 0; overflow-wrap: anywhere; }
.cp-dl .m { color: #6b7280; font-size: 12px; text-align: end; }
.cp-dl .m.p { color: #1d4ed8; font-weight: 600; } .cp-dl .m.d { color: #1f7a45; } .cp-dl .m.r { color: #b42318; font-weight: 700; }
.cp-dl.muted { color: #9ca3af; }
.cp-dot { width: 14px; height: 14px; border-radius: 50%; flex: none; border: 2px solid #9ca3af; }
.cp-dot.p { border-color: #3b82f6; background: linear-gradient(90deg, #3b82f6 50%, transparent 50%); }
.cp-dot.d { border-color: #22a05a; background: #22a05a; }
.cp-dot.x { border-style: dashed; border-color: #c7c9cc; }
.cp-rush { margin-inline-start: 6px; font-size: 11px; }
.cp-later { border: 1px dashed #c7c9cc; border-radius: 12px; padding: 6px 10px; }
.cp-dg { padding: 10px 14px; }
.cp-dgh { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.cp-dgh h3 { margin: 0; font-size: 16px; }
.cp-load { width: 160px; height: 8px; background: #eceef0; border-radius: 999px; overflow: hidden; display: inline-block; }
.cp-load i { display: block; height: 100%; background: var(--brand); }
.cp-busy { color: #b42318; font-weight: 700; }
.cp-table { margin-top: 6px; }
.cp-tr { display: grid; grid-template-columns: minmax(0, 2.4fr) 70px 70px 90px 100px minmax(0, 2.4fr); gap: 8px; align-items: center; padding: 8px 6px; border-bottom: 1px solid #f0f0f0; }
.cp-tr.head { font-size: 12.5px; color: #6b7280; font-weight: 600; border-bottom-color: #e5e7eb; }
.cp-tr.late { background: #fff6f4; }
.cp-tr .num { font-weight: 800; font-size: 18px; } .cp-tr .num.zero { color: #d1d5db; font-weight: 500; }
.cp-tr .lg.warn { color: #a15c00; font-weight: 700; } .cp-tr .lg.bad { color: #b42318; font-weight: 800; }
.cp-fl { color: #9a6b3a; }
.cp-short { display: none; }
.ono { font: inherit; font-size: 12px; background: #f3f4f6; border: 0; border-radius: 6px; padding: 1px 6px; margin: 1px 2px; cursor: pointer; }
.ono.r { background: #fde2dd; color: #b42318; font-weight: 700; }
.cp-report { margin-inline-start: auto; font-weight: 600; }
.cp-done table { width: 100%; border-collapse: collapse; font-size: 14px; }
.cp-done th { text-align: start; font-size: 12.5px; color: #6b7280; font-weight: 600; padding: 8px; border-bottom: 1px solid #e5e7eb; }
.cp-done td { padding: 9px 8px; border-bottom: 1px solid #f0f0f0; }
.cp-done tr.late td { background: #fff6f4; }
.cp-lt { color: #b42318; font-weight: 800; }
.cp-shade { position: fixed; inset: 0; background: rgba(20, 20, 20, .25); z-index: 60; }
.cp-drawer { position: fixed; inset-inline-end: 0; top: 0; bottom: 0; width: 470px; max-width: 100vw; background: #fff; z-index: 61; box-shadow: -10px 0 30px rgba(0, 0, 0, .14); padding: 18px 20px; display: flex; flex-direction: column; gap: 8px; }
.cp-x { font-size: 24px; padding: 0 4px; }
.cp-scroll { flex: 1; overflow-y: auto; min-height: 0; }
.cp-tl { font-size: 12.5px; color: #6b7280; margin: 0 0 2px 22px; }
.cp-rushbtn { color: #b42318; border-color: #d92d20; padding: 2px 8px; }
.cp-actions { gap: 10px; }
@media (max-width: 1100px) {
  .cp-board { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .cp-col:last-child { grid-column: 1 / -1; }
}
@media (max-width: 700px) {
  .cp-stats { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px; }
  .cp-stat { min-width: 0; padding: 6px 10px; } .cp-stat b { font-size: 18px; }
  .cp-lanes { display: flex; } .cp-lanes button { flex: 1; }
  .cp-board { grid-template-columns: 1fr; }
  .cp-col { display: none; background: none; padding: 0; } .cp-col.shown { display: flex; } .cp-colh { display: none; }
  .cp-col:last-child { grid-column: auto; }
  .cp-tr.head, .cp-tr .num, .cp-tr .os { display: none; }
  .cp-tr { grid-template-columns: minmax(0, 1fr) auto; }
  .cp-short { display: block; color: #6b7280; }
  .hide-sm { display: none; }
  .cp-drawer { width: 100vw; }
}
</style>
