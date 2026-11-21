<script setup lang="ts">
// Rider identity: the deliveries assigned to this person. A list of task
// cards, and one page per delivery with a single main button for the next
// step: pick up, arrived, delivered (with the cash collected).
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ApiError, get, post } from '../../api'
import { both, money, t, toMajor, toMinor, whenText } from '../../i18n'
import { useLive } from '../../live'
import { currentRole, reloadUser, session } from '../../session'
import type { Order } from '../../types'
import { duty, flush, loadDuty, openAppSettings, setDuty, track } from './duty'
import { native } from './native'
import { askNotifications } from './push'
import { enqueue, overlay, queue, send } from './offline'

const props = defineProps<{ tab: 'tasks' | 'done'; active: boolean }>()
const emit = defineEmits<{ count: [n: number] }>()
const route = useRoute()
const router = useRouter()

// The last list the server gave, kept on the phone so that it is still there
// when the app is opened without signal.
const CACHE = `work-tasks-${session.user?.id}`
const cached = (): Order[] => { try { return JSON.parse(localStorage.getItem(CACHE) || '[]') } catch { return [] } }
const fetched = ref<Order[]>(cached())
// Steps taken offline are shown as done while they wait to be sent.
const tasks = computed(() => overlay(fetched.value))
const error = ref('')
const notice = ref('')
const busy = ref(false)

const { online, refreshNow } = useLive(() => '/rider/events', async () => {
  for (const r of await send()) {
    notice.value = t(r.code === 'NOT_ASSIGNED' ? 'wk.op_reassigned' : 'wk.op_refused', { no: r.no, why: r.message })
  }
  await reloadUser() // posts may have been changed by a manager
  fetched.value = (await get<{ tasks: Order[] }>('/rider/tasks')).tasks
  localStorage.setItem(CACHE, JSON.stringify(fetched.value))
  void loadDuty(); void flush()
})
window.addEventListener('online', refreshNow)
onBeforeUnmount(() => { window.removeEventListener('online', refreshNow); track(false) })

const st = (o: Order) => o.delivery?.status ?? ''
const current = computed(() => tasks.value.filter(o => ['assigned', 'picked_up', 'arrived'].includes(st(o))))
const done = computed(() => tasks.value.filter(o => ['delivered', 'failed'].includes(st(o))).reverse())
watch(() => current.value.length, n => emit('count', n), { immediate: true })

// The open delivery lives in the URL (?o=12) so the phone's back button returns to the list.
const openId = computed(() => props.active ? Number(route.query.o) || 0 : 0)
const cur = computed(() => current.value.find(o => o.id === openId.value) ?? null)
const open = (o: Order) => router.push({ query: { o: o.id } })
const back = () => router.replace({ query: {} })
// The delivery was reassigned, cancelled or finished while its page was open.
const loaded = ref(false)
watch(fetched, () => (loaded.value = true))
watch([openId, tasks], () => {
  if (openId.value && loaded.value && !cur.value && !busy.value) {
    if (!done.value.some(o => o.id === openId.value)) notice.value = t('wk.reassigned')
    void back()
  }
})

const due = (o: Order) => o.payment_status === 'unpaid'
const itemsText = (o: Order) => o.items.map(i => `${i.qty}× ${both(i.name, i.name_en)}`).join('、')
// The customer's pin when there is one: an exact point beats a typed address.
const mapUrl = (o: Order) => 'https://www.google.com/maps/dir/?api=1&destination=' +
  encodeURIComponent(o.dest_lat != null && o.dest_lng != null ? `${o.dest_lat},${o.dest_lng}` : `${o.address_line} ${o.zone_name}`)

async function act(o: Order, action: string, body: Record<string, unknown> = {}) {
  error.value = ''; notice.value = ''; busy.value = true
  let ok = true
  try {
    if (queue.ops.length) throw new ApiError(0, 'NETWORK', '') // keep the order of steps already waiting
    await post(`/rider/tasks/${o.delivery!.id}/${action}`, body)
  } catch (e) {
    const code = e instanceof ApiError ? e.code : ''
    // No signal: keep the step on the phone and move on. A pickup needs the
    // store to have packed the order, which only the server knows for sure.
    if (code === 'NETWORK') { enqueue(o, action, body); busy.value = false; void refreshNow(); return true }
    ok = false
    error.value = code === 'NOT_ASSIGNED' ? t('wk.reassigned') : code === 'NOT_RIDER' ? t('wk.only_rider') : (e as Error).message
  }
  await refreshNow()
  busy.value = false
  return ok
}

// ---- cash on delivery ----
const cash = reactive<{ id: number; value: string }>({ id: 0, value: '' })
watch(cur, o => { if (o && cash.id !== o.id) { cash.id = o.id; cash.value = String(toMajor(o.totals.total)) } }, { immediate: true })
const quick = computed(() => {
  const d = cur.value ? toMajor(cur.value.totals.total) : 0
  const ups = [100, 500, 1000].map(n => Math.ceil((d + 0.001) / n) * n)
  return [...new Set(ups)].filter(v => v > d).slice(0, 3)
})
const got = computed(() => toMinor(cash.value))
const diff = computed(() => cur.value ? got.value - cur.value.totals.total : 0)
async function deliver(o: Order) {
  if (await act(o, 'deliver', { collected_amount: due(o) ? got.value : 0 })) void back()
}

// ---- problem sheet ----
const prob = reactive({ open: false, reason: '' })
const reasons = ['wk.r.unreachable', 'wk.r.address', 'wk.r.refused', 'wk.r.vehicle']
async function fail(o: Order) {
  if (!prob.reason.trim()) return
  if (await act(o, 'fail', { reason: prob.reason.trim() })) { prob.open = false; prob.reason = ''; void back() }
}
const branchName = computed(() => currentRole()?.branch_name ?? '')

// Tell the store which new assignments this rider has had on screen.
const seen = new Set<number>()
watch([current, () => props.active], () => {
  if (!props.active || document.visibilityState !== 'visible') return
  const ids = current.value.filter(o => st(o) === 'assigned' && !o.delivery!.seen_at && !seen.has(o.delivery!.id)).map(o => o.delivery!.id)
  if (!ids.length) return
  ids.forEach(id => seen.add(id))
  post('/rider/seen', { ids }).catch(() => ids.forEach(id => seen.delete(id)))
}, { immediate: true })

// ---- shift and position ----
// The position is read only on duty and only while the rider identity is in front.
watch([() => props.active, () => duty.on], ([active, on]) => track(active && on, () => current.value.length > 0), { immediate: true })
const dutyBusy = ref(false)
async function toggleDuty() {
  dutyBusy.value = true; error.value = ''
  try {
    await setDuty(!duty.on)
    if (duty.on) void askNotifications()
  } catch (e) { error.value = (e as Error).message }
  dutyBusy.value = false
}
const geoText = computed(() => !duty.on ? t('wk.duty.off_hint') : !props.active ? t('wk.geo.paused')
  : duty.geo === 'ok' ? t('wk.geo.ok') : duty.geo === 'denied' ? t(native ? 'wk.geo.denied_app' : 'wk.geo.denied')
  : duty.geo === 'unsupported' ? t('wk.geo.unsupported') : t('wk.geo.searching'))
</script>

<template>
  <div data-testid="work-rider">
    <!-- one delivery -->
    <template v-if="cur">
      <div class="subbar row between" :data-task-page="st(cur)">
        <button class="back" data-testid="task-back" @click="back">‹ {{ cur.public_no }}</button>
        <span class="tag" :class="st(cur) === 'assigned' ? 'brand' : 'info'">{{ st(cur) === 'assigned' ? t('wk.to_pickup') : t('f.' + cur.fulfillment_status) }}</span>
      </div>
      <div class="section stack">
        <div v-if="!online" class="banner warn">{{ t('offline') }}</div>
        <div v-if="queue.ops.length" class="banner warn" data-testid="ops-waiting">{{ t('wk.ops_waiting', { n: queue.ops.length }) }}</div>
        <div v-if="error" class="banner bad" data-testid="task-error">{{ error }}</div>

        <!-- pick up at the store -->
        <template v-if="st(cur) === 'assigned'">
          <div v-if="cur.packing_status !== 'packed'" class="banner warn">{{ t('not_packed') }}</div>
          <div v-else class="banner ok">{{ t('wk.packed_n', { n: cur.bags ?? 0 }) }}</div>
          <div class="card"><div class="k">{{ t('wk.pickup_at') }}</div><b>{{ branchName }}</b></div>
          <div class="card"><div class="k">{{ t('wk.items') }}</div>
            <div v-for="i in cur.items" :key="i.id" style="padding:3px 0">{{ i.qty }} × {{ both(i.name, i.name_en) }}</div></div>
          <div class="card"><div class="k">{{ t('wk.payment') }}</div>
            <div class="row between"><span>{{ due(cur) ? t('wk.cod') : t('wk.prepaid') }}</span>
              <b v-if="due(cur)" class="amt">{{ money(cur.totals.total, cur.currency) }}</b><b v-else style="color:var(--ok)">{{ t('wk.no_cash') }}</b></div></div>
          <button class="btn primary block big" :disabled="cur.packing_status !== 'packed' || busy" data-act="pickup" @click="act(cur, 'pickup')">{{ t('wk.confirm_pickup_n', { n: cur.bags ?? 0 }) }}</button>
        </template>

        <!-- on the way, then at the door -->
        <template v-else>
          <div class="card stack">
            <div><b style="font-size:16px">{{ cur.customer_name }}</b>
              <div>{{ cur.zone_name }} · {{ cur.address_line }} <b>{{ cur.address_unit }}</b>
                <span v-if="cur.zone_distance_m" class="tag warn">{{ t('outside_zone', { n: cur.zone_distance_m }) }}</span></div>
              <div v-if="cur.address_note" class="banner warn small" style="margin-top:6px">{{ t('wk.customer_note') }}：{{ cur.address_note }}</div></div>
            <div class="row">
              <a class="btn grow" :href="mapUrl(cur)" target="_blank" rel="noopener" data-act="navigate">{{ t('navigate') }}</a>
              <a class="btn grow" :href="'tel:' + cur.customer_phone">{{ t('call') }}</a>
            </div>
            <div class="row between"><span class="muted">{{ t('bags') }} {{ cur.bags }} · {{ due(cur) ? t('wk.cod_short') : t('wk.prepaid') }}</span>
              <b v-if="due(cur)" class="amt">{{ money(cur.totals.total, cur.currency) }}</b></div>
          </div>

          <button v-if="st(cur) === 'picked_up'" class="btn primary block big" :disabled="busy" data-act="arrive" @click="act(cur, 'arrive')">{{ t('wk.i_arrived') }}</button>

          <template v-else>
            <div v-if="due(cur)" class="card stack cash">
              <div class="row between"><span class="k">{{ t('wk.due') }}</span><b class="amt" style="font-size:28px">{{ money(cur.totals.total, cur.currency) }}</b></div>
              <label class="field" style="margin:0"><span>{{ t('wk.received') }}</span>
                <input v-model="cash.value" type="number" inputmode="decimal" min="0" step="0.01" name="collected" /></label>
              <div class="quick">
                <button :class="{ on: diff === 0 }" @click="cash.value = String(toMajor(cur.totals.total))">{{ t('wk.exact', { amt: money(cur.totals.total, cur.currency) }) }}</button>
                <button v-for="v in quick" :key="v" :class="{ on: got === toMinor(v) }" :data-quick="v" @click="cash.value = String(v)">{{ money(toMinor(v), cur.currency) }}</button>
              </div>
              <div v-if="diff > 0" class="muted small" data-testid="change">{{ t('wk.change', { got: money(got, cur.currency), change: money(diff, cur.currency) }) }}</div>
              <div v-else-if="diff < 0" class="small" style="color:var(--warn)">{{ t('wk.short', { amt: money(-diff, cur.currency) }) }}</div>
            </div>
            <div v-else class="banner ok">{{ t('wk.prepaid') }} · {{ t('wk.no_cash') }}</div>
            <button class="btn primary block big" :disabled="busy" data-act="deliver" @click="deliver(cur)">
              {{ due(cur) ? t('wk.confirm_deliver_cash', { amt: money(cur.totals.total, cur.currency) }) : t('wk.confirm_deliver') }}</button>
          </template>
          <button class="btn link block" style="color:var(--bad)" data-act="problem" @click="prob.open = true">{{ t('wk.cannot_deliver') }}</button>
        </template>
      </div>

      <div v-if="prob.open" class="sheet-mask" @click.self="prob.open = false">
        <div class="sheet" data-testid="problem-sheet">
          <div class="sheet-body stack">
            <h2>{{ t('wk.problem') }}</h2><p class="muted small">{{ t('wk.problem_hint') }}</p>
            <div class="quick"><button v-for="r in reasons" :key="r" :class="{ on: prob.reason === t(r) }" @click="prob.reason = t(r)">{{ t(r) }}</button></div>
            <textarea v-model="prob.reason" rows="2" name="reason" :placeholder="t('reason')"></textarea>
            <button class="btn primary block big" :disabled="!prob.reason.trim() || busy" data-act="submit-problem" @click="fail(cur)">{{ t('wk.submit') }}</button>
          </div>
        </div>
      </div>
    </template>

    <!-- the list -->
    <div v-else class="section stack">
      <div v-if="!online" class="banner warn">{{ t('offline') }}</div>
      <div v-if="queue.ops.length" class="banner warn" data-testid="ops-waiting">{{ t('wk.ops_waiting', { n: queue.ops.length }) }}</div>
      <div v-if="notice" class="banner warn" data-testid="rider-notice">{{ notice }}</div>
      <div v-if="error" class="banner bad">{{ error }}</div>

      <div v-if="tab === 'tasks' && duty.loaded" class="card duty" :class="{ on: duty.on }" data-testid="duty" :data-on="duty.on" :data-geo="duty.geo">
        <div class="row between">
          <div><b>{{ duty.on ? t('wk.duty.on') : t('wk.duty.off') }}</b>
            <div class="small muted" data-testid="geo-text">{{ geoText }}</div></div>
          <button class="btn" :class="{ primary: !duty.on }" :disabled="dutyBusy" data-act="duty" @click="toggleDuty">{{ duty.on ? t('wk.duty.end') : t('wk.duty.start') }}</button>
        </div>
        <div v-if="duty.on && active && duty.geo !== 'denied' && duty.geo !== 'unsupported'" class="small muted">{{ t(native ? 'wk.geo.app' : 'wk.geo.screen') }}</div>
        <button v-if="native && duty.on && duty.geo === 'denied'" class="btn small" @click="openAppSettings">{{ t('wk.app_settings') }}</button>
      </div>

      <template v-if="tab === 'tasks'">
        <div v-if="!current.length" class="card muted" data-testid="rider-empty">{{ t('no_tasks') }}</div>
        <div v-for="o in current" :key="o.id" class="card stack" :class="{ fresh: st(o) === 'assigned' }" :data-delivery="o.public_no">
          <div class="row between"><b class="no">{{ o.public_no }} <span v-if="o.scheduled_for" class="tag info">{{ whenText(o.scheduled_for, o.timezone) }}</span></b>
            <span class="tag" :class="st(o) === 'assigned' ? 'brand' : 'info'">{{ st(o) === 'assigned' ? t('wk.to_pickup') : t('f.' + o.fulfillment_status) }}</span></div>
          <div v-if="st(o) === 'assigned'" class="line"><span class="pt s"></span><span>{{ branchName }} ·
            <b :style="{ color: o.packing_status === 'packed' ? 'var(--ok)' : 'var(--warn)' }">{{ o.packing_status === 'packed' ? `${t('p.packed')} ${o.bags ?? ''} ${t('bags')}` : t('not_packed') }}</b></span></div>
          <div class="line"><span class="pt"></span><span>{{ o.zone_name }} · {{ o.address_line }} {{ o.address_unit }}</span></div>
          <div class="row between small muted"><span>{{ itemsText(o) }}</span>
            <span style="white-space:nowrap">{{ due(o) ? t('wk.cod_short') : t('wk.prepaid') }} <b v-if="due(o)" style="color:var(--brand-dark);font-size:16px">{{ money(o.totals.total, o.currency) }}</b></span></div>
          <button class="btn block" :class="{ primary: st(o) === 'assigned' }" data-act="open" @click="open(o)">{{ st(o) === 'assigned' ? t('wk.go_pickup') : t('wk.continue') }}</button>
        </div>
      </template>

      <template v-else>
        <div class="card row" style="text-align:center">
          <div class="grow"><b style="font-size:22px" data-testid="n-delivered">{{ done.filter(o => st(o) === 'delivered').length }}</b><div class="small muted">{{ t('wk.delivered_n') }}</div></div>
          <div class="grow"><b style="font-size:22px">{{ done.filter(o => st(o) === 'failed').length }}</b><div class="small muted">{{ t('wk.failed_n') }}</div></div>
        </div>
        <div class="muted small">{{ t('wk.today') }}</div>
        <div v-for="o in done" :key="o.id" class="card row between" :data-done="o.public_no">
          <span><b>{{ o.public_no }}</b> · {{ o.zone_name }}</span>
          <span class="tag" :class="st(o) === 'delivered' ? 'ok' : 'bad'">{{ t('f.' + o.fulfillment_status) }}</span>
        </div>
        <div v-if="!done.length" class="card muted">{{ t('wk.done_empty') }}</div>
      </template>
    </div>
  </div>
</template>
