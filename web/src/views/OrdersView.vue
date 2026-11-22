<script setup lang="ts">
import { computed, ref } from 'vue'
import { ApiError, get, post } from '../api'
import { both, minutesSince, money, reasonText, t, whenText } from '../i18n'
import { useLive } from '../live'
import { session } from '../session'
import type { Order, RiderInfo } from '../types'
import { contactApps, contactName, contactPhone } from '../addresses'
import './work/strings'
import '../orders'
import TotalsTable from './TotalsTable.vue'

const bid = session.branchId!
const scope = ref<'active' | 'history'>('active')
const orders = ref<Order[]>([])
const riders = ref<RiderInfo[]>([])
const selectedId = ref<number | null>(null)
const error = ref('')
const sound = ref(localStorage.getItem('order-sound') !== 'off')
const now = ref(Date.now())
setInterval(() => (now.value = Date.now()), 15000)

let knownPending = new Set<number>()
let first = true

// A short two-tone chime for new orders, generated with Web Audio so that no
// audio file is needed. Browsers allow it after the first click on the page.
function chime() {
  if (!sound.value) return
  try {
    const ctx = new (window.AudioContext || (window as any).webkitAudioContext)()
    ;[880, 1175].forEach((f, i) => {
      const o = ctx.createOscillator(), g = ctx.createGain()
      o.frequency.value = f; o.connect(g); g.connect(ctx.destination)
      g.gain.setValueAtTime(0.25, ctx.currentTime + i * 0.25)
      g.gain.exponentialRampToValueAtTime(0.001, ctx.currentTime + i * 0.25 + 0.4)
      o.start(ctx.currentTime + i * 0.25); o.stop(ctx.currentTime + i * 0.25 + 0.45)
    })
  } catch { /* audio not available */ }
}

async function load() {
  const r = await get<{ orders: Order[] }>(`/merchant/branches/${bid}/orders?scope=${scope.value}`)
  orders.value = r.orders
  if (scope.value === 'active') {
    const pending = new Set(r.orders.filter(o => o.acceptance_status === 'pending').map(o => o.id))
    if (!first && [...pending].some(id => !knownPending.has(id))) chime()
    knownPending = pending; first = false
  }
  // Riders come in the order to pick from: on duty and near the store first.
  riders.value = (await get(`/merchant/branches/${bid}/riders`)).riders
}
const { online, refreshNow } = useLive(() => `/merchant/branches/${bid}/events`, load)

// Orders split into baskets show which basket each dish goes in.
const baskets = (o: Order) => o.items.some(i => i.basket > 1)
const km = (m: number) => m < 1000 ? `${Math.round(m / 10) * 10} m` : `${(m / 1000).toFixed(1)} km`
function riderLabel(r: RiderInfo) {
  const parts = [r.display_name, !r.on_duty ? t('st.now.off_duty') : r.contact === 'lost' ? t('rd.lost') : r.contact === 'none' ? t('rd.no_position') : t('st.now.on_duty')]
  if (r.distance_m != null && r.contact === 'ok') parts.push(km(r.distance_m))
  if (r.active_deliveries) parts.push(t('rd.carrying', { n: r.active_deliveries }))
  return parts.join(' · ')
}
// Two minutes after assigning, a delivery the rider has not had on screen is flagged.
const unread = (o: Order) => !!o.delivery && o.delivery.status === 'assigned' && !o.delivery.seen_at &&
  o.acceptance_status === 'accepted' && now.value - new Date(o.delivery.assigned_at).getTime() > 120000

const selected = computed(() => orders.value.find(o => o.id === selectedId.value) ?? null)
const pendingCount = computed(() => orders.value.filter(o => o.acceptance_status === 'pending').length)

function toggleSound() { sound.value = !sound.value; localStorage.setItem('order-sound', sound.value ? 'on' : 'off'); if (sound.value) chime() }
function setScope(s: 'active' | 'history') { scope.value = s; selectedId.value = null; first = true; void refreshNow() }

async function act(o: Order, action: string, body: Record<string, unknown> = {}) {
  error.value = ''
  try {
    await post(`/merchant/orders/${o.id}/${action}`, { expected_version: o.version, ...body })
  } catch (e) {
    error.value = e instanceof ApiError && e.code === 'VERSION_CONFLICT' ? t('conflict') : (e as Error).message
  }
  await refreshNow()
}

function withReason(o: Order, action: 'reject' | 'cancel') {
  const reason = prompt(t('reason'))
  if (reason === null) return
  if (!reason.trim()) { error.value = t('reason_required'); return }
  void act(o, action, { reason })
}

function assign(o: Order, e: Event) {
  const id = Number((e.target as HTMLSelectElement).value)
  if (id) void act(o, 'assign-rider', { rider_id: id })
}

const failed = (o: Order) => o.acceptance_status === 'accepted' && o.fulfillment_status === 'delivery_failed'
const redeliverTo = ref(0)
function redeliver(o: Order) {
  const id = redeliverTo.value || o.delivery?.rider_id || 0
  if (id) void act(o, 'redeliver', { rider_id: id })
}
function closeFailed(o: Order) {
  const reason = prompt(t('reason'))
  if (reason === null) return
  if (!reason.trim()) { error.value = t('reason_required'); return }
  void act(o, 'close-failed', { reason })
}
// The customer's after-sales request: the desk settles it with the customer
// (orders are paid in cash) and records what was agreed.
function aftersale(o: Order, status: 'resolved' | 'declined') {
  const reply = prompt(t(status === 'resolved' ? 'as.d.how' : 'as.d.why'))
  if (reply === null) return
  if (status === 'declined' && !reply.trim()) { error.value = t('reason_required'); return }
  void act(o, 'aftersale', { status, reply })
}
const canCancel = (o: Order) => o.acceptance_status === 'accepted' && ['none', 'awaiting_handoff', 'awaiting_pickup'].includes(o.fulfillment_status)
const canAssign = (o: Order) => o.service_type === 'delivery' && o.acceptance_status === 'accepted' && ['none', 'awaiting_handoff'].includes(o.fulfillment_status)
const waited = (o: Order) => { void now.value; return minutesSince(o.created_at) }
const accTag = (s: string) => ({ pending: 'warn', accepted: 'info', rejected: 'bad', cancelled: 'bad' }[s] ?? '')
</script>

<template>
  <div class="page">
    <div class="row between wrap" style="margin-bottom:12px">
      <div class="seg light">
        <button :class="{ on: scope === 'active' }" @click="setScope('active')">{{ t('active') }}<span v-if="scope === 'active' && pendingCount"> · {{ pendingCount }}</span></button>
        <button :class="{ on: scope === 'history' }" @click="setScope('history')">{{ t('history') }}</button>
      </div>
      <label class="check" style="margin:0"><input type="checkbox" :checked="sound" @change="toggleSound" />{{ t('new_order_sound') }}</label>
    </div>
    <div v-if="!online" class="banner warn" style="margin-bottom:12px">{{ t('offline') }}</div>
    <div v-if="error" class="banner bad" style="margin-bottom:12px">{{ error }}</div>

    <div class="desk" :class="{ picked: !!selected }">
      <div class="list">
        <div v-if="orders.length === 0" class="card muted">{{ t('no_orders') }}</div>
        <button v-for="o in orders" :key="o.id" class="card ord" :class="{ sel: o.id === selectedId, pend: o.acceptance_status === 'pending' }" @click="selectedId = o.id">
          <div class="row between"><b>{{ o.public_no }}</b><span class="price bold">{{ money(o.totals.total, o.currency) }}</span></div>
          <div class="row between small" style="margin-top:4px">
            <span>{{ t(o.service_type) }} · {{ o.customer_name }}</span>
            <span class="muted" v-if="scope === 'active'">{{ t('waiting') }} {{ waited(o) }} {{ t('min') }}</span>
          </div>
          <div class="row wrap" style="margin-top:8px;gap:6px">
            <span class="tag brand" v-if="o.scheduled_for">{{ t('scheduled') }} {{ whenText(o.scheduled_for, o.timezone) }}</span>
            <span class="tag" :class="accTag(o.acceptance_status)">{{ t('a.' + o.acceptance_status) }}</span>
            <span v-if="unread(o)" class="tag bad" :data-unread="o.public_no">{{ t('rd.unread') }}</span>
            <span class="tag" v-if="o.acceptance_status === 'accepted'">{{ t('p.' + o.packing_status) }}</span>
            <span class="tag" v-if="o.acceptance_status === 'accepted'">{{ t('f.' + o.fulfillment_status) }}</span>
            <span class="tag" :class="o.payment_status === 'paid' ? 'ok' : ''">{{ t(o.payment_status) }}</span>
            <span v-if="o.aftersale" class="tag" :class="o.aftersale.status === 'open' ? 'bad' : ''" data-testid="aftersale-tag">{{ t('as.d.' + o.aftersale.status) }}</span>
          </div>
        </button>
      </div>

      <div class="detail card" v-if="selected">
        <div class="stack">
          <button class="btn link backlist" data-testid="order-back" @click="selectedId = null">‹ {{ t('back') }}</button>
          <div class="row between">
            <h2>{{ selected.public_no }} · {{ t(selected.service_type) }}</h2>
            <a class="btn small" :href="`/ticket/${selected.id}`" target="_blank">{{ t('print') }}</a>
          </div>
          <div class="row wrap" style="gap:6px">
            <span class="tag" :class="accTag(selected.acceptance_status)">{{ t('a.' + selected.acceptance_status) }}</span>
            <span class="tag">{{ t('p.' + selected.packing_status) }}</span>
            <span class="tag">{{ t('f.' + selected.fulfillment_status) }}</span>
            <span class="tag" :class="selected.payment_status === 'paid' ? 'ok' : 'warn'">{{ t(selected.payment_method) }} · {{ t(selected.payment_status) }}</span>
          </div>
          <div v-if="selected.close_reason" class="banner bad">{{ t('reason') }}: {{ reasonText(selected.close_reason) }}</div>
          <div v-if="selected.aftersale" class="banner stack" :class="selected.aftersale.status === 'open' ? 'warn' : 'info'" data-testid="aftersale-box">
            <div><b>{{ t('as.d.' + selected.aftersale.status) }}</b> · {{ t('as.r.' + selected.aftersale.reason) }} · {{ whenText(selected.aftersale.created_at, selected.timezone) }}</div>
            <div v-if="selected.aftersale.detail" style="white-space:pre-wrap">{{ selected.aftersale.detail }}</div>
            <div v-if="selected.aftersale.reply">{{ t('as.d.reply') }}: {{ selected.aftersale.reply }}</div>
            <template v-if="selected.aftersale.status === 'open'">
              <div class="small">{{ t('as.d.hint') }}</div>
              <div class="row wrap">
                <button class="btn primary small" data-testid="aftersale-resolve" @click="aftersale(selected, 'resolved')">{{ t('as.d.resolve') }}</button>
                <button class="btn danger small" data-testid="aftersale-decline" @click="aftersale(selected, 'declined')">{{ t('as.d.decline') }}</button>
              </div>
            </template>
          </div>
          <div v-if="selected.scheduled_for" class="banner info">{{ t(selected.service_type === 'delivery' ? 'deliver_at' : 'pickup_at') }} <b>{{ whenText(selected.scheduled_for, selected.timezone) }}</b>
            <span v-if="selected.release_at"> · {{ t('kitchen_from') }} {{ whenText(selected.release_at, selected.timezone) }}</span></div>

          <div>
            <div class="muted small">{{ t('customer') }}</div>
            <div data-testid="order-contact">{{ contactName(selected) }} · <a :href="'tel:' + contactPhone(selected)">{{ contactPhone(selected) }}</a>
              <span v-if="selected.contact_phone && selected.contact_phone !== selected.customer_phone" class="small muted"> · {{ t('oc.ordered_by') }} {{ selected.customer_phone }}</span></div>
            <div v-if="contactApps(selected).length" class="small" data-testid="order-im">
              <span v-for="(c, i) in contactApps(selected)" :key="c.field"><template v-if="i"> · </template>{{ c.label }} <b>{{ c.value }}</b></span></div>
            <div v-if="selected.service_type === 'delivery'">{{ selected.zone_name }} · {{ selected.address_line }} {{ selected.address_unit }}
              <span v-if="selected.zone_distance_m" class="tag warn" data-testid="outside-zone">{{ t('outside_zone', { n: selected.zone_distance_m }) }}</span>
              <a v-if="selected.dest_lat != null" class="small" target="_blank" rel="noopener" :href="`https://www.google.com/maps/search/?api=1&query=${selected.dest_lat},${selected.dest_lng}`">{{ t('view_on_map') }}</a></div>
            <div v-else>{{ t('pickup_code') }} <b>{{ selected.pickup_code }}</b></div>
            <div v-if="selected.address_note" class="small">{{ t('address_note') }}: {{ selected.address_note }}</div>
            <div v-if="selected.address_photo" class="small" data-testid="door-photo">{{ t('oc.door_photo') }} <a :href="`/api/v1/merchant/orders/${selected.id}/address-photo`" target="_blank" rel="noopener">{{ t('oc.view') }}</a></div>
            <div v-if="selected.note" class="banner warn small" style="margin-top:6px">{{ t('order_note') }}: {{ selected.note }}</div>
          </div>

          <div class="stack" style="border-top:1px solid var(--line);padding-top:10px">
            <div v-for="it in selected.items" :key="it.id" class="row top between">
              <div class="grow"><span v-if="baskets(selected)" class="tag info" style="margin-right:4px">{{ t('basket_short', { n: it.basket }) }}</span><b>{{ it.qty }} ×</b> {{ both(it.name, it.name_en) }}
                <div class="muted small" v-if="it.options.length">{{ it.options.map(o => both(o.name, o.name_en)).join(' · ') }}</div>
                <div class="small" v-if="it.note" style="color:var(--warn)">{{ t('note') }}: {{ it.note }}</div></div>
              <span class="tag" v-if="selected.acceptance_status === 'accepted'" :class="it.prep_status === 'ready' ? 'ok' : ''">{{ t('i.' + it.prep_status) }}</span>
              <span class="price">{{ money(it.line_total, selected.currency) }}</span>
            </div>
          </div>
          <TotalsTable :totals="selected.totals" :currency="selected.currency" :delivery="selected.service_type === 'delivery'" />

          <div v-if="selected.delivery" class="row between">
            <span class="muted">{{ t('rider') }}</span>
            <span>{{ selected.delivery.rider_name }} <span class="tag">{{ selected.delivery.status }}</span>
              <span v-if="unread(selected)" class="tag bad" data-testid="rider-unread">{{ t('rd.unread') }}</span></span>
          </div>
          <div v-if="selected.delivery && unread(selected)" class="banner warn small">{{ t('rd.unread_hint', { name: selected.delivery.rider_name }) }}
            <a v-if="selected.delivery.rider_phone" :href="'tel:' + selected.delivery.rider_phone">{{ selected.delivery.rider_phone }}</a></div>
          <div v-if="selected.delivery?.proof_at" class="row between" data-testid="proof-row"><span class="muted">{{ t('rd.proof') }}</span>
            <a :href="`/api/v1/merchant/orders/${selected.id}/proof`" target="_blank" rel="noopener">{{ t('rd.proof_view') }} · {{ whenText(selected.delivery.proof_at, selected.timezone) }}</a></div>
          <div v-if="selected.delivery?.fail_reason" class="banner bad">{{ selected.delivery.fail_reason }}</div>
          <div v-if="selected.bags" class="row between"><span class="muted">{{ t('bags') }}</span><b>{{ selected.bags }}</b></div>
          <div v-if="selected.cash" class="row between"><span class="muted">{{ t('collected') }}</span>
            <span>{{ money(selected.cash.amount_collected, selected.currency) }} · <span class="tag">{{ t('c.' + selected.cash.reconcile_status) }}</span></span></div>

          <div class="row wrap" v-if="selected.acceptance_status === 'pending'">
            <button class="btn primary grow big" @click="act(selected, 'accept')">{{ t('accept') }}</button>
            <button class="btn danger big" @click="withReason(selected, 'reject')">{{ t('reject') }}</button>
          </div>
          <div class="stack" v-else-if="selected.acceptance_status === 'accepted'">
            <label class="field" v-if="canAssign(selected)" style="margin:0">
              <span>{{ selected.delivery ? t('reassign') : t('assign_rider') }}</span>
              <select :value="selected.delivery?.rider_id ?? 0" @change="assign(selected, $event)">
                <option :value="0">—</option>
                <option v-for="r in riders" :key="r.id" :value="r.id" :data-rider="r.username">{{ riderLabel(r) }}</option>
              </select>
            </label>
            <button v-if="selected.fulfillment_status === 'awaiting_handoff' && selected.delivery" class="btn primary" @click="act(selected, 'handoff')">{{ t('handoff_rider') }}</button>
            <button v-if="selected.fulfillment_status === 'awaiting_pickup'" class="btn primary" @click="act(selected, 'handoff')">{{ t('handoff_customer') }}</button>
            <button v-if="canCancel(selected)" class="btn danger" @click="withReason(selected, 'cancel')">{{ t('cancel_order') }}</button>
            <template v-if="failed(selected)">
              <div class="banner warn">{{ t('failed_hint') }}</div>
              <div class="row wrap">
                <select :value="redeliverTo || selected.delivery?.rider_id || 0" :aria-label="t('choose_rider')" style="flex:1 1 160px" @change="redeliverTo = Number(($event.target as HTMLSelectElement).value)">
                  <option v-for="r in riders" :key="r.id" :value="r.id" :data-rider="r.username">{{ riderLabel(r) }}</option>
                </select>
                <button class="btn primary" @click="redeliver(selected)">{{ t('redeliver') }}</button>
              </div>
              <button class="btn danger" @click="closeFailed(selected)">{{ t('close_failed') }}</button>
            </template>
          </div>
        </div>
      </div>
      <div v-else-if="orders.length" class="detail card muted nopick">{{ t('pick_order') }}</div>
    </div>
  </div>
</template>

<style scoped>
.desk { display: grid; grid-template-columns: minmax(280px, 380px) 1fr; gap: 14px; align-items: start; }
.list { display: flex; flex-direction: column; gap: 10px; }
.ord { text-align: left; cursor: pointer; width: 100%; }
.ord.sel { border-color: var(--brand); box-shadow: 0 0 0 2px var(--brand-soft); }
.ord.pend { border-left: 5px solid var(--warn); }
.detail { position: sticky; top: calc(var(--shell-top, 0px) + 14px); max-width: 640px; }
.nopick { text-align: center; padding: 48px 16px; }
.backlist { display: none; align-self: flex-start; }
/* on a phone the list and the order take turns on the screen */
@media (max-width: 767px) {
  .desk { grid-template-columns: 1fr; } .detail { position: static; max-width: none; }
  .desk.picked .list, .nopick { display: none; }
  .backlist { display: inline-flex; margin: -4px 0 -6px -4px; }
}
</style>
