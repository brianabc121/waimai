<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { get, post } from '../api'
import { locale, money, pick, setLocale, t, toMajor, toMinor, whenText } from '../i18n'
import { useLive } from '../live'
import { logout, session } from '../session'
import type { Order } from '../types'

const router = useRouter()
const tasks = ref<Order[]>([])
const error = ref('')
const collected = reactive<Record<number, string>>({})

const { online, refreshNow } = useLive(() => '/rider/events', async () => {
  tasks.value = (await get<{ tasks: Order[] }>('/rider/tasks')).tasks
})

const current = computed(() => tasks.value.filter(o => ['assigned', 'picked_up', 'arrived'].includes(o.delivery?.status ?? '')))
const done = computed(() => tasks.value.filter(o => ['delivered', 'failed'].includes(o.delivery?.status ?? '')))

async function act(o: Order, action: string, body: Record<string, unknown> = {}) {
  error.value = ''
  try { await post(`/rider/tasks/${o.delivery!.id}/${action}`, body) } catch (e) { error.value = (e as Error).message }
  await refreshNow()
}

function amount(o: Order) {
  if (collected[o.id] === undefined) collected[o.id] = String(toMajor(o.totals.total))
  return collected[o.id]
}

function deliver(o: Order) {
  const due = o.payment_status === 'unpaid'
  void act(o, 'deliver', { collected_amount: due ? toMinor(collected[o.id] ?? toMajor(o.totals.total)) : 0 })
}

function problem(o: Order) {
  const reason = prompt(t('reason'))
  if (reason?.trim()) void act(o, 'fail', { reason })
}

// The customer's pin when there is one: an exact point beats a typed address.
const mapUrl = (o: Order) => 'https://www.google.com/maps/dir/?api=1&destination=' +
  encodeURIComponent(o.dest_lat != null && o.dest_lng != null ? `${o.dest_lat},${o.dest_lng}` : `${o.address_line} ${o.zone_name}`)
async function out() { await logout(); router.replace('/login') }
</script>

<template>
  <div class="phone" style="padding-bottom:24px">
    <div class="topbar row between">
      <h1>{{ t('my_tasks') }}</h1>
      <span class="row"><button class="lang" @click="setLocale(locale === 'zh' ? 'en' : 'zh')">{{ locale === 'zh' ? 'EN' : '中文' }}</button>
        <button class="btn link" @click="out">{{ t('sign_out') }}</button></span>
    </div>
    <div class="section stack">
      <div class="muted small">{{ session.user?.display_name }}</div>
      <div v-if="!online" class="banner warn">{{ t('offline') }}</div>
      <div v-if="error" class="banner bad">{{ error }}</div>
      <div v-if="current.length === 0" class="card muted">{{ t('no_tasks') }}</div>

      <div v-for="o in current" :key="o.id" class="card stack">
        <div class="row between"><h2>{{ o.public_no }} <span class="tag info" v-if="o.scheduled_for">{{ whenText(o.scheduled_for, o.timezone) }}</span></h2><span class="tag brand">{{ t('f.' + (o.fulfillment_status === 'none' ? 'none' : o.fulfillment_status)) }}</span></div>

        <!-- R01: pick up at the store -->
        <template v-if="o.delivery!.status === 'assigned'">
          <div v-if="o.packing_status !== 'packed'" class="banner warn">{{ t('not_packed') }}</div>
          <div v-else class="banner ok">{{ t('p.packed') }} · {{ t('bags') }} <b>{{ o.bags }}</b></div>
          <div class="muted small">{{ o.items.map(i => `${i.qty}× ${pick(i.name, i.name_en)}`).join(', ') }}</div>
          <button class="btn primary block big" :disabled="o.packing_status !== 'packed'" @click="act(o, 'pickup')">{{ t('confirm_pickup') }}</button>
        </template>

        <!-- R02/R03: on the way, arrived, delivered -->
        <template v-else>
          <div>
            <div class="bold">{{ o.customer_name }}</div>
            <div>{{ o.zone_name }} · {{ o.address_line }} {{ o.address_unit }}
              <span v-if="o.zone_distance_m" class="tag warn">{{ t('outside_zone', { n: o.zone_distance_m }) }}</span></div>
            <div v-if="o.address_note" class="banner warn small" style="margin-top:6px">{{ o.address_note }}</div>
          </div>
          <div class="row">
            <a class="btn grow" :href="mapUrl(o)" target="_blank" rel="noopener">{{ t('navigate') }}</a>
            <a class="btn grow" :href="'tel:' + o.customer_phone">{{ t('call') }}</a>
          </div>
          <div class="row between"><span class="muted">{{ t('bags') }}</span><b>{{ o.bags }}</b></div>
          <button v-if="o.delivery!.status === 'picked_up'" class="btn block big" @click="act(o, 'arrive')">{{ t('arrived') }}</button>
          <template v-if="o.payment_status === 'unpaid'">
            <div class="row between"><span>{{ t('collect') }}</span><b class="price" style="font-size:22px">{{ money(o.totals.total, o.currency) }}</b></div>
            <label class="field" style="margin:0"><span>{{ t('collected') }}</span>
              <input type="number" inputmode="decimal" min="0" step="0.01" :value="amount(o)" @input="collected[o.id] = ($event.target as HTMLInputElement).value" /></label>
          </template>
          <div v-else class="banner ok">{{ t('paid') }}</div>
          <button class="btn primary block big" @click="deliver(o)">{{ t('confirm_delivered') }}</button>
          <button class="btn danger block" @click="problem(o)">{{ t('report_problem') }}</button>
        </template>
      </div>

      <template v-if="done.length">
        <h3 class="muted">{{ t('finished') }}</h3>
        <div v-for="o in done" :key="o.id" class="card row between">
          <span><b>{{ o.public_no }}</b> · {{ o.zone_name }}</span>
          <span class="tag" :class="o.delivery!.status === 'delivered' ? 'ok' : 'bad'">{{ t('f.' + o.fulfillment_status) }}</span>
        </div>
      </template>
    </div>
  </div>
</template>
