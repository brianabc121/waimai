<script setup lang="ts">
import { computed, ref } from 'vue'
import { ApiError, get, post } from '../api'
import { both, minutesSince, money, reasonText, t } from '../i18n'
import { useLive } from '../live'
import { session } from '../session'
import type { Order } from '../types'
import TotalsTable from './TotalsTable.vue'

const bid = session.branchId!
const scope = ref<'active' | 'history'>('active')
const orders = ref<Order[]>([])
const riders = ref<{ id: number; display_name: string }[]>([])
const selectedId = ref<number | null>(null)
const error = ref('')
const sound = ref(localStorage.getItem('order-sound') !== 'off')
const now = ref(Date.now())
setInterval(() => (now.value = Date.now()), 30000)

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
  if (riders.value.length === 0) riders.value = (await get(`/merchant/branches/${bid}/riders`)).riders
}
const { online, refreshNow } = useLive(() => `/merchant/branches/${bid}/events`, load)

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

    <div class="desk">
      <div class="list">
        <div v-if="orders.length === 0" class="card muted">{{ t('no_orders') }}</div>
        <button v-for="o in orders" :key="o.id" class="card ord" :class="{ sel: o.id === selectedId, pend: o.acceptance_status === 'pending' }" @click="selectedId = o.id">
          <div class="row between"><b>{{ o.public_no }}</b><span class="price bold">{{ money(o.totals.total, o.currency) }}</span></div>
          <div class="row between small" style="margin-top:4px">
            <span>{{ t(o.service_type) }} · {{ o.customer_name }}</span>
            <span class="muted" v-if="scope === 'active'">{{ t('waiting') }} {{ waited(o) }} {{ t('min') }}</span>
          </div>
          <div class="row wrap" style="margin-top:8px;gap:6px">
            <span class="tag" :class="accTag(o.acceptance_status)">{{ t('a.' + o.acceptance_status) }}</span>
            <span class="tag" v-if="o.acceptance_status === 'accepted'">{{ t('p.' + o.packing_status) }}</span>
            <span class="tag" v-if="o.acceptance_status === 'accepted'">{{ t('f.' + o.fulfillment_status) }}</span>
            <span class="tag" :class="o.payment_status === 'paid' ? 'ok' : ''">{{ t(o.payment_status) }}</span>
          </div>
        </button>
      </div>

      <div class="detail card" v-if="selected">
        <div class="stack">
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

          <div>
            <div class="muted small">{{ t('customer') }}</div>
            <div>{{ selected.customer_name }} · <a :href="'tel:' + selected.customer_phone">{{ selected.customer_phone }}</a></div>
            <div v-if="selected.service_type === 'delivery'">{{ selected.zone_name }} · {{ selected.address_line }} {{ selected.address_unit }}</div>
            <div v-else>{{ t('pickup_code') }} <b>{{ selected.pickup_code }}</b></div>
            <div v-if="selected.address_note" class="small">{{ t('address_note') }}: {{ selected.address_note }}</div>
            <div v-if="selected.note" class="banner warn small" style="margin-top:6px">{{ t('order_note') }}: {{ selected.note }}</div>
          </div>

          <div class="stack" style="border-top:1px solid var(--line);padding-top:10px">
            <div v-for="it in selected.items" :key="it.id" class="row top between">
              <div class="grow"><b>{{ it.qty }} ×</b> {{ both(it.name, it.name_en) }}
                <div class="muted small" v-if="it.options.length">{{ it.options.map(o => both(o.name, o.name_en)).join(' · ') }}</div>
                <div class="small" v-if="it.note" style="color:var(--warn)">{{ t('note') }}: {{ it.note }}</div></div>
              <span class="tag" v-if="selected.acceptance_status === 'accepted'" :class="it.prep_status === 'ready' ? 'ok' : ''">{{ t('i.' + it.prep_status) }}</span>
              <span class="price">{{ money(it.line_total, selected.currency) }}</span>
            </div>
          </div>
          <TotalsTable :totals="selected.totals" :currency="selected.currency" :delivery="selected.service_type === 'delivery'" />

          <div v-if="selected.delivery" class="row between">
            <span class="muted">{{ t('rider') }}</span>
            <span>{{ selected.delivery.rider_name }} <span class="tag">{{ selected.delivery.status }}</span></span>
          </div>
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
                <option v-for="r in riders" :key="r.id" :value="r.id">{{ r.display_name }}</option>
              </select>
            </label>
            <button v-if="selected.fulfillment_status === 'awaiting_handoff' && selected.delivery" class="btn primary" @click="act(selected, 'handoff')">{{ t('handoff_rider') }}</button>
            <button v-if="selected.fulfillment_status === 'awaiting_pickup'" class="btn primary" @click="act(selected, 'handoff')">{{ t('handoff_customer') }}</button>
            <button v-if="canCancel(selected)" class="btn danger" @click="withReason(selected, 'cancel')">{{ t('cancel_order') }}</button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.desk { display: grid; grid-template-columns: minmax(280px, 380px) 1fr; gap: 14px; align-items: start; }
.list { display: flex; flex-direction: column; gap: 10px; }
.ord { text-align: left; cursor: pointer; width: 100%; }
.ord.sel { border-color: var(--brand); box-shadow: 0 0 0 2px var(--brand-soft); }
.ord.pend { border-left: 5px solid var(--warn); }
.detail { position: sticky; top: 66px; max-width: 640px; }
@media (max-width: 760px) { .desk { grid-template-columns: 1fr; } .detail { position: static; } }
</style>
