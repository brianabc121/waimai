<script setup lang="ts">
import KitchenNote from './KitchenNote.vue'
import { computed, ref } from 'vue'
import { ApiError, api, get, post } from '../api'
import { both, has, minutesSince, money, reasonText, t, whenText } from '../i18n'
import { useLive } from '../live'
import { can, session } from '../session'
import type { Adjust, Order, PayChoice, PayOptions, RiderInfo, Totals } from '../types'
import AdjustSheet from './checkout/AdjustSheet.vue'
import PayPicker from './checkout/PayPicker.vue'
import { payText, pctText } from '../checkout'
import { contactApps, contactName, contactPhone } from '../addresses'
import './work/strings'
import './translateStrings'
import '../orders'
import TotalsTable from './TotalsTable.vue'
import ApproveDialog from './ApproveDialog.vue'
import '../approve'
import { chatEnabled } from '../chat'
import './manualStrings'
import { readyTag } from '../progress'
import { useRoute } from 'vue-router'
import { addons, batchSource, batchTitle, itemsByBatch, pendingAddon, addonErr, presetSplit } from '../addon'
import VoidSheet from './VoidSheet.vue'
import RefundSheet from './RefundSheet.vue'
import MoneyLog from './MoneyLog.vue'
import { copyText, voidMessage, voidNote, voidsOf } from '../voids'
import type { OrderItem, OrderVoid } from '../types'

const srcText = (s: string) => has(`vo.src.${s}`) ? t(`vo.src.${s}`) : s

// "message the customer" shows only where chat is set up
const chatOn = ref(false)
void chatEnabled().then(v => { chatOn.value = v })

// store name, notice or introduction in another language the merchant has not
// confirmed yet (only managers can see it; the others get nothing)
const textsPending = ref(0)
void get<{ pending: number }>(`/merchant/branches/${session.branchId}/store-texts`).then(v => { textsPending.value = v.pending }, () => {})

const bid = session.branchId!

// The discount at checkout and how a paid order was paid (docs/CHECKOUT_DISCOUNT_DESIGN.md §5.4, §6.2).
const payOpts = ref<PayOptions | null>(null)
void get<PayOptions>(`/merchant/branches/${bid}/pay-options`).then(o => (payOpts.value = o)).catch(() => null)
const adjFor = ref<Order | null>(null)
const adjToken = ref('')
const canAdjust = (o: Order) => o.service_type !== 'dine_in' && o.payment_status !== 'paid' && !!o.addon?.open && can('order.handle')
function adjustOrder(o: Order) {
  if (can('checkout.discount')) { adjToken.value = ''; adjFor.value = o; return }
  askApproval('checkout.discount', t('ck.approve_what'), h => { adjToken.value = h['X-Approval']; adjFor.value = o })
}
const adjPreview = (a: Adjust) => post<Totals>(`/merchant/orders/${adjFor.value!.id}/adjust-preview`, a)
const adjInitial = (o: Order): Adjust | null => o.totals.adj_percent_bp || o.totals.adj_mode
  ? { percent_bp: o.totals.adj_percent_bp ?? 0, mode: o.totals.adj_mode ?? '', amount: o.totals.adj_mode === 'round' ? o.totals.round_off ?? 0 : o.totals.adj_mode === 'set_total' ? o.totals.total : 0, reason: o.adj_reason ?? '' }
  : null
async function saveAdjust(a: Adjust | null) {
  const o = adjFor.value!
  adjFor.value = null; error.value = ''
  try {
    await api('PUT', `/merchant/orders/${o.id}/adjust`, { expected_version: o.version, adjust: a ?? { percent_bp: 0, mode: '', amount: 0, reason: '' } },
      adjToken.value ? { 'X-Approval': adjToken.value } : {})
  } catch (e) {
    error.value = e instanceof ApiError && e.code === 'VERSION_CONFLICT' ? t('conflict')
      : e instanceof ApiError && e.code === 'APPROVAL_INVALID' ? t('ck.approval_used') : (e as Error).message
  }
  adjToken.value = ''
  await refreshNow()
}
const adjText = (o: Order) => {
  const x = o.totals, parts: string[] = []
  if (x.adj_percent_bp) parts.push(pctText(x.adj_percent_bp))
  if (x.adj_mode === 'round') parts.push(t('ck.round_off'))
  if (x.adj_mode === 'set_total') parts.push(o.totals.total === 0 ? t('ck.free') : t('ck.adj_set'))
  if (o.adj_reason) parts.push(o.adj_reason)
  if (o.adj_by) parts.push(o.adj_approved_by ? t('ck.by_ok', { name: o.adj_by, boss: o.adj_approved_by }) : t('ck.by', { name: o.adj_by }))
  return parts.join(' · ')
}
const payFor = ref<Order | null>(null)
const payPick = ref<PayChoice>({ type: 'cash', channel_id: 0 })
function recordPay(o: Order) {
  payPick.value = { type: (o.pay_type || 'cash') as PayChoice['type'], channel_id: o.pay_channel_id ?? 0 }
  payFor.value = o
}
async function savePay() {
  const o = payFor.value!
  payFor.value = null; error.value = ''
  try { await api('PUT', `/merchant/orders/${o.id}/payment`, payPick.value) } catch (e) { error.value = (e as Error).message }
  await refreshNow()
}
const scope = ref<'active' | 'history'>('active')
const orders = ref<Order[]>([])
const riders = ref<RiderInfo[]>([])
// ?order=ID opens that order (from the cooking progress page)
const selectedId = ref<number | null>(Number(useRoute().query.order) || null)
const error = ref('')
const sound = ref(localStorage.getItem('order-sound') !== 'off')
const now = ref(Date.now())
setInterval(() => (now.value = Date.now()), 15000)

let knownPending = new Set<number | string>()
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
    // a new order, or a customer's add-on waiting to be confirmed, rings
    const pending = new Set([...r.orders.filter(o => o.acceptance_status === 'pending').map(o => o.id),
      ...r.orders.flatMap(o => (o.batches ?? []).filter(b => b.status === 'pending').map(b => `${o.id}:${b.seq}`))])
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

async function act(o: Order, action: string, body: Record<string, unknown> = {}, headers?: Record<string, string>) {
  error.value = ''
  try {
    await post(`/merchant/orders/${o.id}/${action}`, { expected_version: o.version, ...body }, headers)
  } catch (e) {
    error.value = e instanceof ApiError && e.code === 'VERSION_CONFLICT' ? t('conflict')
      : e instanceof ApiError && e.code === 'APPROVAL_INVALID' ? t('ap.used') : (e as Error).message
  }
  await refreshNow()
}

function withReason(o: Order, action: 'reject' | 'cancel', headers?: Record<string, string>) {
  const reason = prompt(t('reason'))
  if (reason === null) return
  if (!reason.trim()) { error.value = t('reason_required'); return }
  void act(o, action, { reason }, headers)
}

// What this role may not do, a manager approves on the spot (docs/STAFF_ACCESS_DESIGN.md §7).
const approving = ref<{ perm: string; what: string; then: (headers: Record<string, string>) => void } | null>(null)
const approvedBy = ref('')
function askApproval(perm: string, what: string, then: (headers: Record<string, string>) => void) {
  approvedBy.value = ''
  approving.value = { perm, what, then }
}
function approved(token: string, name: string) {
  const a = approving.value!
  approving.value = null
  approvedBy.value = t('ap.done', { name })
  a.then({ 'X-Approval': token })
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
function aftersale(o: Order, status: 'resolved' | 'declined', headers?: Record<string, string>) {
  const reply = prompt(t(status === 'resolved' ? 'as.d.how' : 'as.d.why'))
  if (reply === null) return
  if (status === 'declined' && !reply.trim()) { error.value = t('reason_required'); return }
  void act(o, 'aftersale', { status, reply }, headers)
}
const canCancel = (o: Order) => o.acceptance_status === 'accepted' && ['none', 'awaiting_handoff', 'awaiting_pickup'].includes(o.fulfillment_status)
const canAssign = (o: Order) => can('order.handle') && o.service_type === 'delivery' && o.acceptance_status === 'accepted' && ['none', 'awaiting_handoff'].includes(o.fulfillment_status)
const waited = (o: Order) => { void now.value; return minutesSince(o.created_at) }
const accTag = (s: string) => ({ pending: 'warn', accepted: 'info', rejected: 'bad', cancelled: 'bad' }[s] ?? '')

// ---- add-ons (docs/ADD_ON_ORDER_DESIGN.md) ----
const grouped = (o: Order) => (o.batches?.length ?? 0) > 1 && o.service_type !== 'dine_in'
const lineCount = (lines: { qty: number }[] | undefined) => (lines ?? []).reduce((n, l) => n + l.qty, 0)
const pendingSum = (o: Order) => { const b = pendingAddon(o); return b ? (b.total_after ?? 0) - (b.total_before ?? 0) : 0 }
const leftMin = (iso: string | null) => { void now.value; return iso ? Math.max(0, Math.ceil((new Date(iso).getTime() - Date.now()) / 60000)) : 0 }
const rejecting = ref(false)

// ---- voids and refunds by dish (docs/VOID_ITEM_DESIGN.md §8.1, §7) ----
const voiding = ref<OrderItem | null>(null)
const voidDone = ref<{ order: Order; v: OrderVoid; text: string; copied: boolean } | null>(null)
const refunding = ref(false)
const refundDone = ref('')
const canVoid = (o: Order, it: OrderItem) => !!o.void_open && it.prep_status !== 'cancelled' && can('order.handle')
const voidClosed = (o: Order) => o.acceptance_status === 'accepted' && o.service_type !== 'dine_in' && !o.void_open && o.fulfillment_status === 'out_for_delivery'
async function voided(r: { order: Order; access_token?: string }) {
  voiding.value = null
  const v = (r.order.voids ?? []).slice(-1)[0]
  await refreshNow()
  if (!v) return
  const text = voidMessage(r.order, v, r.access_token ?? '', contactName(r.order))
  voidDone.value = { order: r.order, v, text, copied: false }
  try { await copyText(text); voidDone.value.copied = true } catch { /* the button copies it */ }
}
async function copyVoid() { if (voidDone.value) { await copyText(voidDone.value.text); voidDone.value.copied = true } }
async function refunded(_: Order, amount: number) {
  refunding.value = false
  refundDone.value = t('vd.r.done', { s: money(amount, selected.value?.currency) })
  await refreshNow()
}
async function decideAddon(o: Order, seq: number, action: 'accept' | 'reject', reason = '') {
  error.value = ''; rejecting.value = false
  try { await post(`/merchant/orders/${o.id}/batches/${seq}/${action}`, action === 'reject' ? { reason } : {}) }
  catch (e) { error.value = addonErr(e) }
  await refreshNow()
}
</script>

<template>
  <div class="page">
    <div class="row between wrap" style="margin-bottom:12px">
      <div class="seg light">
        <button :class="{ on: scope === 'active' }" @click="setScope('active')">{{ t('active') }}<span v-if="scope === 'active' && pendingCount"> · {{ pendingCount }}</span></button>
        <button :class="{ on: scope === 'history' }" @click="setScope('history')">{{ t('history') }}</button>
      </div>
      <div class="row wrap">
        <label class="check" style="margin:0"><input type="checkbox" :checked="sound" @change="toggleSound" />{{ t('new_order_sound') }}</label>
        <router-link v-if="can('order.manual')" class="btn primary small" to="/merchant/order/delivery" data-testid="manual-order-new">{{ t('pos.new_delivery') }}</router-link>
      </div>
    </div>
    <div v-if="textsPending" class="banner warn" style="margin-bottom:12px" data-testid="store-texts-banner">{{ t('mtr.store_banner', { n: textsPending }) }}
      <router-link to="/merchant/settings">{{ t('mtr.store_go') }}</router-link></div>
    <div v-if="!online" class="banner warn" style="margin-bottom:12px">{{ t('offline') }}</div>
    <div v-if="error" class="banner bad" style="margin-bottom:12px">{{ error }}</div>
    <div v-else-if="approvedBy" class="banner ok" style="margin-bottom:12px" data-testid="approved-by">{{ approvedBy }}</div>

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
            <span v-if="o.order_source" class="tag info" data-testid="by-staff-tag">{{ t('vo.tag', { s: srcText(o.order_source) }) }}</span>
            <span class="tag brand" v-if="o.scheduled_for">{{ t('scheduled') }} {{ whenText(o.scheduled_for, o.timezone) }}</span>
            <span class="tag" :class="accTag(o.acceptance_status)">{{ t('a.' + o.acceptance_status) }}</span>
            <span v-if="unread(o)" class="tag bad" :data-unread="o.public_no">{{ t('rd.unread') }}</span>
            <span class="tag" v-if="o.acceptance_status === 'accepted'">{{ t('p.' + o.packing_status) }}</span>
            <span v-if="scope === 'active' && readyTag(o)" class="tag" :class="{ ok: readyTag(o)!.ready === readyTag(o)!.total }" data-testid="cp-order-tag">{{ t('kp.order_tag', { a: readyTag(o)!.ready, b: readyTag(o)!.total }) }}</span>
            <span class="tag" v-if="o.acceptance_status === 'accepted'">{{ t('f.' + o.fulfillment_status) }}</span>
            <span class="tag" :class="o.payment_status === 'paid' ? 'ok' : ''">{{ t(o.payment_status) }}</span>
            <span v-if="o.aftersale" class="tag" :class="o.aftersale.status === 'open' ? 'bad' : ''" data-testid="aftersale-tag">{{ t('as.d.' + o.aftersale.status) }}</span>
            <span v-if="addons(o).length && o.service_type !== 'dine_in'" class="tag addon" data-testid="addon-tag">{{ t('ao.tag') }}<template v-if="addons(o).length > 1"> ×{{ addons(o).length }}</template></span>
          </div>
          <div v-if="pendingAddon(o)" class="banner warn small" style="margin-top:8px" data-testid="addon-pending-tag">
            {{ t('ao.pending_head', { n: lineCount(pendingAddon(o)!.lines), s: money(pendingSum(o), o.currency) }) }}</div>
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
            <router-link v-if="readyTag(selected) && can('order.progress') && scope === 'active'" class="tag cp-tag" :class="{ ok: readyTag(selected)!.ready === readyTag(selected)!.total }" :to="`/merchant/progress?order=${selected.id}`" data-testid="cp-order-link">{{ t('kp.order_tag', { a: readyTag(selected)!.ready, b: readyTag(selected)!.total }) }} ›</router-link>
          </div>
          <div v-if="selected.close_reason" class="banner bad">{{ t('reason') }}: {{ reasonText(selected.close_reason) }}</div>
          <div v-if="selected.aftersale" class="banner stack" :class="selected.aftersale.status === 'open' ? 'warn' : 'info'" data-testid="aftersale-box">
            <div><b>{{ t('as.d.' + selected.aftersale.status) }}</b> · {{ t('as.r.' + selected.aftersale.reason) }} · {{ whenText(selected.aftersale.created_at, selected.timezone) }}</div>
            <div v-if="selected.aftersale.detail" style="white-space:pre-wrap">{{ selected.aftersale.detail }}</div>
            <div v-if="selected.aftersale.reply">{{ t('as.d.reply') }}: {{ selected.aftersale.reply }}</div>
            <template v-if="selected.aftersale.status === 'open'">
              <div class="small">{{ t('as.d.hint') }}</div>
              <div class="row wrap">
                <template v-if="can('order.aftersale')">
                <button class="btn primary small" data-testid="aftersale-resolve" @click="aftersale(selected, 'resolved')">{{ t('as.d.resolve') }}</button>
                <button class="btn danger small" data-testid="aftersale-decline" @click="aftersale(selected, 'declined')">{{ t('as.d.decline') }}</button>
                </template>
                <template v-else>
                  <button class="btn small need" :title="t('rp.need')" data-testid="aftersale-locked" @click="askApproval('order.aftersale', t('as.d.resolve'), h => aftersale(selected!, 'resolved', h))">🔒 {{ t('as.d.resolve') }} · {{ t('ap.ask') }}</button>
                  <button class="btn small need" :title="t('rp.need')" @click="askApproval('order.aftersale', t('as.d.decline'), h => aftersale(selected!, 'declined', h))">🔒 {{ t('as.d.decline') }} · {{ t('ap.ask') }}</button>
                </template>
              </div>
            </template>
          </div>
          <div v-if="selected.scheduled_for" class="banner info">{{ t(selected.service_type === 'delivery' ? 'deliver_at' : 'pickup_at') }} <b>{{ whenText(selected.scheduled_for, selected.timezone) }}</b>
            <span v-if="selected.release_at"> · {{ t('kitchen_from') }} {{ whenText(selected.release_at, selected.timezone) }}</span></div>

          <div>
            <div class="muted small">{{ t('customer') }}</div>
            <div data-testid="order-contact">{{ contactName(selected) }} · <a :href="'tel:' + contactPhone(selected)">{{ contactPhone(selected) }}</a>
              <router-link v-if="chatOn && selected.service_type !== 'dine_in'" class="btn link small" style="margin-inline-start:6px" data-testid="order-chat" :to="{ path: '/merchant/messages', query: { order: selected.id } }">{{ t('ch.contact_customer') }}</router-link>
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
            <div v-if="selected.order_source" class="small" style="margin-top:6px" data-testid="by-staff">
              <span class="tag info">{{ t('vo.tag', { s: srcText(selected.order_source) }) }}</span>
              <template v-if="selected.member_no"> · {{ t('vo.member_no') }} <b>{{ String(selected.member_no).padStart(6, '0') }}</b></template>
              <template v-if="selected.placed_by"> · {{ t('vo.placed_by') }} {{ selected.placed_by }}</template></div>
            <div v-if="selected.staff_note" class="banner info small" style="margin-top:6px">{{ t('vo.d.staff_note') }}: {{ selected.staff_note }}</div>
          </div>

          <div v-if="pendingAddon(selected)" class="banner warn stack" data-testid="addon-pending">
            <div class="row between wrap"><b>{{ t('ao.pending_head', { n: lineCount(pendingAddon(selected)!.lines), s: money(pendingSum(selected), selected.currency) }) }}</b>
              <span class="small">⏱ {{ t('ao.pending_left', { n: leftMin(pendingAddon(selected)!.expires_at) }) }}</span></div>
            <div v-for="(l, i) in pendingAddon(selected)!.lines" :key="i" class="row between small">
              <span><b>{{ l.qty }} ×</b> {{ both(l.name, l.name_en) }}<span v-if="l.options.length" class="muted"> · {{ l.options.map(o => both(o.name, o.name_en)).join(' · ') }}</span>
                <span v-if="l.note_tags?.length || l.note" class="muted"> · {{ [...(l.note_tags ?? []).map(x => x.name), l.note].filter(Boolean).join(' · ') }}</span></span>
              <span>{{ money(l.line_total, selected.currency) }}</span></div>
            <div class="small">{{ money(pendingAddon(selected)!.total_before ?? 0, selected.currency) }} → <b>{{ money(pendingAddon(selected)!.total_after ?? 0, selected.currency) }}</b></div>
            <div v-if="can('order.handle')" class="row wrap">
              <button class="btn primary grow" data-testid="addon-accept" @click="decideAddon(selected, pendingAddon(selected)!.seq, 'accept')">{{ t('ao.accept') }}</button>
              <button class="btn danger" data-testid="addon-reject" @click="rejecting = !rejecting">{{ t('ao.reject') }}</button>
            </div>
            <div v-if="rejecting" class="stack" data-testid="addon-reasons"><span class="small">{{ t('ao.reject_title') }}</span>
              <div class="row wrap"><button v-for="r in presetSplit('ao.reasons')" :key="r" class="btn small" @click="decideAddon(selected, pendingAddon(selected)!.seq, 'reject', r)">{{ r }}</button></div></div>
          </div>
          <div v-if="selected.addon && can('order.manual') && (selected.addon.open || addons(selected).length)" class="addonbar" data-testid="addon-box">
            <template v-if="selected.addon.open">
              <router-link class="btn primary small" data-testid="addon-new" :to="{ path: '/merchant/order/delivery', query: { append: selected.id } }">{{ t('ao.btn') }}</router-link>
              <span class="small muted">{{ selected.addon.repack ? t('ao.repack') : t(selected.service_type === 'pickup' ? 'ao.until_pickup' : 'ao.until_delivery') }}</span>
            </template>
            <span v-else class="small muted" data-testid="addon-closed">{{ t('ao.closed') }}</span>
          </div>

          <div class="stack" style="border-top:1px solid var(--line);padding-top:10px">
            <template v-for="g in itemsByBatch(selected)" :key="g.seq">
              <div v-if="grouped(selected)" class="bhead" :class="{ add: g.seq > 1 }" data-testid="batch-head">
                <b>{{ batchTitle(g.seq) }}</b>
                <span v-if="g.batch"> {{ whenText(g.batch.at, selected.timezone).split(' ').pop() }} · {{ batchSource(selected, g.batch) }}</span>
                <span v-if="g.batch?.round_tags?.length"> · {{ g.batch.round_tags.join(' · ') }}</span>
                <span v-if="g.seq > 1 && g.batch?.total_before != null" class="muted"> · {{ money(g.batch.total_before, selected.currency) }} → {{ money(g.batch.total_after ?? 0, selected.currency) }}</span>
                <a v-if="g.seq > 1" class="small" style="margin-inline-start:auto" :href="`/ticket/${selected.id}?batch=${g.seq}`" target="_blank" data-testid="batch-print">{{ t('ao.print_batch') }}</a>
              </div>
              <div v-for="it in g.items" :key="it.id" class="row top between" :class="{ voidline: it.prep_status === 'cancelled' }" data-testid="order-line">
                <div class="grow"><span v-if="baskets(selected)" class="tag info" style="margin-inline-end:4px">{{ t('basket_short', { n: it.basket }) }}</span><b>{{ it.qty }} ×</b> {{ both(it.name, it.name_en) }}
                  <div class="muted small" v-if="it.options.length">{{ it.options.map(o => both(o.name, o.name_en)).join(' · ') }}</div>
                  <KitchenNote :it="it" />
                  <div v-for="v in voidsOf(selected, it)" :key="v.id" class="small voidnote" data-testid="void-note">{{ voidNote(v, selected.timezone) }}
                    <a v-if="v.wasted" :href="`/ticket/${selected.id}?void=${v.id}`" target="_blank" rel="noopener">{{ t('vd.print') }}</a></div></div>
                <span class="tag" v-if="selected.acceptance_status === 'accepted'" :class="it.prep_status === 'ready' ? 'ok' : ''">{{ t('i.' + it.prep_status) }}</span>
                <button v-if="canVoid(selected, it)" class="btn small voidbtn" data-testid="void-btn" @click="voiding = it">{{ t('vd.btn') }}</button>
                <span class="price">{{ money(it.line_total, selected.currency) }}</span>
              </div>
            </template>
            <div v-for="b in (selected.batches ?? []).filter(b => b.seq > 1 && b.status !== 'accepted' && b.status !== 'pending')" :key="'x' + b.seq" class="small muted" data-testid="batch-dropped">
              {{ batchTitle(b.seq) }} · {{ t('ao.src.customer') }} · {{ t('ao.st.' + b.status) }}<template v-if="b.reject_reason">（{{ b.reject_reason }}）</template></div>
          </div>
          <div v-if="voidClosed(selected)" class="small muted" data-testid="void-closed">{{ t('vd.closed') }}</div>
          <TotalsTable :totals="selected.totals" :currency="selected.currency" :delivery="selected.service_type === 'delivery'" :discounts="selected.discounts" />
          <div v-if="selected.refunds?.length || selected.refund_open" class="stack refunds" data-testid="refunds">
            <div v-for="r in selected.refunds" :key="r.id" class="small">{{ t('vd.r.row', { s: money(r.amount, selected.currency), m: r.method, r: r.reason, at: whenText(r.at, selected.timezone) }) }}
              <span class="muted"> · {{ r.lines.map(l => `${l.name} ×${l.qty}`).join('、') }}</span></div>
            <div v-if="refundDone" class="banner ok small">{{ refundDone }}</div>
            <button v-if="selected.refund_open" class="btn small" style="align-self:flex-start" data-testid="refund-btn" @click="refundDone = ''; refunding = true">{{ t('vd.r.btn') }}</button>
          </div>
          <MoneyLog v-if="can('order.view')" :key="'ml' + selected.id" :order-id="selected.id" :version="selected.version" :currency="selected.currency" :timezone="selected.timezone" />
          <div v-if="selected.totals.adj_percent_bp || selected.totals.adj_mode" class="row between adjrow" data-testid="adj-row">
            <span class="muted">{{ t('ck.adj') }}</span><span>{{ adjText(selected) }}</span></div>
          <div v-if="selected.payment_status === 'paid' && selected.pay_type" class="row between" data-testid="pay-row">
            <span class="muted">{{ t('ck.pay_type') }}</span><span>{{ payText(selected) }}
              <span v-if="selected.pay_check" class="tag" :class="selected.pay_check === 'ok' ? 'ok' : 'bad'" data-testid="pay-check">{{ t('rc.check_tag', { s: t('rc.st.' + selected.pay_check) }) }}<template v-if="selected.pay_check_note"> · {{ selected.pay_check_note }}</template></span></span></div>
          <div v-if="canAdjust(selected) || (selected.payment_status === 'paid' && selected.service_type !== 'dine_in' && can('order.handle'))" class="row wrap" style="gap:8px">
            <button v-if="canAdjust(selected)" class="btn small" data-testid="adj-edit" @click="adjustOrder(selected)">{{ t('ck.edit_adj') }}</button>
            <button v-if="selected.payment_status === 'paid' && selected.service_type !== 'dine_in' && can('order.handle')" class="btn small" data-testid="pay-record" @click="recordPay(selected)">{{ t('ck.record_pay') }}</button>
          </div>

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

          <div class="row wrap" v-if="selected.acceptance_status === 'pending' && can('order.handle')">
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
            <button v-if="canCancel(selected) && can('order.cancel')" class="btn danger" data-testid="cancel-order" @click="withReason(selected, 'cancel')">{{ t('cancel_order') }}</button>
            <button v-else-if="canCancel(selected)" class="btn need" :title="t('rp.need')" data-testid="cancel-locked" @click="askApproval('order.cancel', t('cancel_order'), h => withReason(selected!, 'cancel', h))">🔒 {{ t('cancel_order') }} · {{ t('ap.ask') }}</button>
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
    <AdjustSheet v-if="adjFor" :currency="adjFor.currency" :reasons="payOpts?.reasons" :initial="adjInitial(adjFor)" :preview="adjPreview"
      @close="adjFor = null" @apply="saveAdjust" />
    <div v-if="payFor" class="drawer-mask" style="position:fixed;inset:0;background:rgba(0,0,0,.35);z-index:40" @click="payFor = null"></div>
    <div v-if="payFor" class="card stack paybox" data-testid="pay-dialog">
      <h2 style="margin:0;font-size:17px">{{ t('ck.record_pay') }} · {{ payFor.public_no }}</h2>
      <PayPicker v-model="payPick" :options="payOpts" />
      <div class="row" style="justify-content:flex-end;gap:8px">
        <button class="btn" @click="payFor = null">{{ t('ck.cancel') }}</button>
        <button class="btn primary" :disabled="payPick.type === 'online' && !payPick.channel_id" data-testid="pay-save" @click="savePay">{{ t('ck.save') }}</button>
      </div>
    </div>
    <VoidSheet v-if="voiding && selected" :item="voiding" :branch-id="bid" :currency="selected.currency" @done="voided" @close="voiding = null" />
    <RefundSheet v-if="refunding && selected" :order="selected" :branch-id="bid" @done="refunded" @close="refunding = false" />
    <div v-if="voidDone" class="vdone-mask" @click.self="voidDone = null">
      <div class="card stack vdone" data-testid="void-done">
        <h3 style="margin:0">{{ t('vd.done') }} · {{ voidDone.v.name }} ×{{ voidDone.v.qty }}</h3>
        <div class="small muted">{{ t('vd.done_hint') }}</div>
        <textarea v-model="voidDone.text" rows="5" data-testid="void-message"></textarea>
        <div class="row" style="justify-content:flex-end;gap:8px">
          <a v-if="voidDone.v.wasted" class="btn" :href="`/ticket/${voidDone.order.id}?void=${voidDone.v.id}`" target="_blank" rel="noopener">{{ t('vd.print') }}</a>
          <button class="btn" @click="copyVoid">{{ voidDone.copied ? t('vd.copied') : t('vd.copy') }}</button>
          <button class="btn primary" data-testid="void-done-ok" @click="voidDone = null">{{ t('vd.close') }}</button></div>
      </div>
    </div>
    <ApproveDialog v-if="approving" :branch-id="bid" :perm="approving.perm" :what="approving.what" @approved="approved" @close="approving = null" />
  </div>
</template>

<style scoped>
.paybox { position: fixed; left: 50%; top: 16vh; transform: translateX(-50%); width: min(440px, calc(100vw - 32px)); z-index: 41; }
.adjrow span:last-child { color: #8a5a12; text-align: end; }
.voidline > .grow > b, .voidline > .grow { text-decoration: line-through; color: var(--muted); }
.voidline .voidnote, .voidline :deep(*) { text-decoration: none; }
.voidnote { color: #b42318; text-decoration: none; display: inline-block; width: 100%; }
.voidnote a { margin-inline-start: 6px; }
.voidbtn { color: #b42318; border-color: #f1b4ae; }
.refunds { border-top: 1px dashed var(--line); padding-top: 8px; }
.vdone-mask { position: fixed; inset: 0; background: rgba(0,0,0,.35); z-index: 30; display: flex; align-items: center; justify-content: center; padding: 16px; }
.vdone { width: min(440px, 100%); }
.vdone textarea { width: 100%; font: inherit; }
.cp-tag { cursor: pointer; border: 1px solid currentColor; }
.desk { display: grid; grid-template-columns: minmax(280px, 380px) 1fr; gap: 14px; align-items: start; }
.list { display: flex; flex-direction: column; gap: 10px; }
.ord { text-align: start; cursor: pointer; width: 100%; }
.ord.sel { border-color: var(--brand); box-shadow: 0 0 0 2px var(--brand-soft); }
.ord.pend { border-inline-start: 5px solid var(--warn); }
.detail { position: sticky; top: calc(var(--shell-top, 0px) + 14px); max-width: 640px; }
.nopick { text-align: center; padding: 48px 16px; }
.tag.addon { background: #fff1e0; color: #b45309; border-color: #f5c48a; }
.addonbar { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.bhead { display: flex; flex-wrap: wrap; align-items: baseline; gap: 2px 6px; font-size: 12.5px; color: var(--muted); padding: 4px 0 2px; border-bottom: 1px dashed var(--line); }
.bhead.add b { color: #b45309; }
.backlist { display: none; align-self: flex-start; }
/* on a phone the list and the order take turns on the screen */
@media (max-width: 767px) {
  .desk { grid-template-columns: 1fr; } .detail { position: static; max-width: none; }
  .desk.picked .list, .nopick { display: none; }
  .backlist { display: inline-flex; margin: -4px 0 -6px -4px; }
}
</style>
