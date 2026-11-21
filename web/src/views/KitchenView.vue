<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { ApiError, get, post } from '../api'
import { both, minutesSince, t } from '../i18n'
import { useLive } from '../live'
import { session } from '../session'
import type { Order, OrderItem } from '../types'

const bid = session.branchId!
const orders = ref<Order[]>([])
const tab = ref<'cook' | 'pack'>('cook')
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
  orders.value = (await get<{ orders: Order[] }>(`/kitchen/branches/${bid}/board`)).orders
})

const live = (o: Order) => o.items.filter(i => i.prep_status !== 'cancelled')
const cancelled = computed(() => orders.value.filter(o => o.acceptance_status === 'cancelled'))
const cooking = computed(() => orders.value.filter(o => o.acceptance_status === 'accepted' && o.packing_status === 'waiting_items'))
const packing = computed(() => orders.value.filter(o => o.acceptance_status === 'accepted' && o.packing_status !== 'none'))
const missing = (o: Order) => live(o).filter(i => i.prep_status !== 'ready')
const waited = (o: Order) => { void now.value; return minutesSince(o.created_at) }

async function run(fn: () => Promise<unknown>) {
  error.value = ''
  try { await fn() } catch (e) {
    error.value = e instanceof ApiError && e.code === 'MANIFEST_CHANGED' ? t('conflict') : (e as Error).message
  }
  await refreshNow()
}
const itemAction = (it: OrderItem, action: string) => run(() => post(`/kitchen/items/${it.id}/${action}`))
const pack = (o: Order) => run(() => post(`/kitchen/orders/${o.id}/pack`, { bags: check(o).bags, manifest_version: o.manifest_version }))
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
      <div v-if="cooking.length === 0" class="card muted">{{ t('kitchen_empty') }}</div>
      <div class="cols">
        <div v-for="o in cooking" :key="o.id" class="card stack">
          <div class="row between">
            <h2>{{ o.public_no }} <span class="tag brand">{{ t(o.service_type) }}</span></h2>
            <span class="tag" :class="waited(o) >= 20 ? 'bad' : waited(o) >= 10 ? 'warn' : ''">{{ waited(o) }} {{ t('min') }}</span>
          </div>
          <div v-if="o.note" class="banner warn small">{{ t('order_note') }}: {{ o.note }}</div>
          <div v-for="it in live(o)" :key="it.id" class="kline" :class="it.prep_status">
            <div class="grow">
              <div class="kname">{{ it.qty }} × {{ both(it.name, it.name_en) }}</div>
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

    <!-- packing: whole-order check and handoff (K03/K04) -->
    <template v-else>
      <div v-if="packing.length === 0" class="card muted">{{ t('kitchen_empty') }}</div>
      <div class="cols">
        <div v-for="o in packing" :key="o.id" class="card stack">
          <div class="row between">
            <h2>{{ o.public_no }} <span class="tag brand">{{ t(o.service_type) }}</span></h2>
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
.check.top { align-items: flex-start; }
</style>
