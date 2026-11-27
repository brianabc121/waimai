<script setup lang="ts">
// Rider identity: the deliveries assigned to this person. A list of task
// cards, and one page per delivery with a single main button for the next
// step: pick up, arrived, delivered (with the cash collected).
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ApiError, get, post, upload } from '../../api'
import { both, money, t, toMajor, toMinor, whenText } from '../../i18n'
import { addons, totalBefore } from '../../addon'
import { useLive } from '../../live'
import { currentRole, reloadUser, session } from '../../session'
import { multiBrand } from '../../brands'
import type { Order, PayChannel, PayOptions } from '../../types'
import { channelName } from '../../checkout'
import { contactApps, contactName, contactPhone } from '../../addresses'
import { shrink } from '../../photo'
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

// what the rider collects: the total less what the store balance paid (docs/REFUND_BALANCE_DESIGN.md §5)
const collect = (o: Order) => o.due ?? o.totals.total
const due = (o: Order) => o.payment_status === 'unpaid' && collect(o) > 0
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
// an add-on changes what is due: the box follows the new total
const cashTotal = ref(0)
watch(cur, o => { if (o && (cash.id !== o.id || cashTotal.value !== collect(o))) { cash.id = o.id; cashTotal.value = collect(o); cash.value = String(toMajor(collect(o))) } }, { immediate: true })
// "was ₱379" next to the amount of an order with dishes added (docs/ADD_ON_ORDER_DESIGN.md §7.3)
const was = (o: Order) => { const b = totalBefore(o); return b != null && b !== o.totals.total ? t('ao.was', { s: money(b, o.currency) }) : '' }
const quick = computed(() => {
  const d = cur.value ? toMajor(collect(cur.value)) : 0
  const ups = [100, 500, 1000].map(n => Math.ceil((d + 0.001) / n) * n)
  return [...new Set(ups)].filter(v => v > d).slice(0, 3)
})
const got = computed(() => toMinor(cash.value))
const diff = computed(() => cur.value ? got.value - collect(cur.value) : 0)
// a customer may pay the rider online at the door (docs/CHECKOUT_DISCOUNT_DESIGN.md §6.2)
const door = reactive<{ online: boolean; channel: number; list: PayChannel[] | null }>({ online: false, channel: 0, list: null })
watch(() => cur.value?.id, () => { door.online = false; door.channel = 0 })
async function payOnline() {
  door.online = true
  if (!door.list && cur.value) {
    try { door.list = (await get<PayOptions>(`/merchant/branches/${cur.value.branch_id}/pay-options`)).channels } catch { door.list = [] }
    if (door.list.length === 1) door.channel = door.list[0].id
  }
}
async function deliver(o: Order) {
  const online = due(o) && door.online
  if (online && !door.channel) return
  if (await act(o, 'deliver', online ? { collected_amount: 0, pay: { type: 'online', channel_id: door.channel } } : { collected_amount: due(o) ? got.value : 0 })) void back()
}

// ---- photo at the door (optional) ----
const photo = reactive({ busy: false, error: '' })
async function addPhoto(o: Order, e: Event) {
  const input = e.target as HTMLInputElement, file = input.files?.[0]
  input.value = ''
  if (!file) return
  photo.busy = true; photo.error = ''
  try {
    await upload(`/rider/tasks/${o.delivery!.id}/proof`, new File([await shrink(file)], 'proof.jpg', { type: 'image/jpeg' }))
    await refreshNow()
  } catch (err) {
    photo.error = err instanceof ApiError && err.code === 'NETWORK' ? t('wk.photo_offline') : t('wk.photo_failed')
  }
  photo.busy = false
}

// ---- problem sheet ----
const prob = reactive({ open: false, reason: '' })
const reasons = ['wk.r.unreachable', 'wk.r.address', 'wk.r.refused', 'wk.r.vehicle']
async function fail(o: Order) {
  if (!prob.reason.trim()) return
  if (await act(o, 'fail', { reason: prob.reason.trim() })) { prob.open = false; prob.reason = ''; void back() }
}
const branchName = computed(() => currentRole()?.branch_name ?? '')
// which brand a delivery is (docs/MULTI_BRAND_DESIGN.md §16.8): the bag and
// the customer know it by the brand. Shown when the store has several brands,
// or the order is a brand other than the store's own name.
const brandOf = (o: Order) => o.brand_name && (multiBrand() || o.brand_name !== branchName.value)
  ? { name: o.brand_name, color: o.brand_color || '#a02c20' } : null

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
        <button class="back" data-testid="task-back" @click="back">‹ {{ cur.public_no }} <i v-if="brandOf(cur)" class="wbrand" :style="{ background: brandOf(cur)!.color }">{{ brandOf(cur)!.name }}</i></button>
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
          <div class="card"><div class="k">{{ t('wk.pickup_at') }}</div><b>{{ branchName }}</b>
            <div v-if="brandOf(cur)" class="small" data-testid="rider-pickup-brand">{{ t('wk.brand_bag') }} <i class="wbrand" :style="{ background: brandOf(cur)!.color }">{{ brandOf(cur)!.name }}</i></div></div>
          <div class="card"><div class="k">{{ t('wk.items') }} <span v-if="addons(cur).length" class="tag addon" data-testid="rider-addon">{{ t('ao.tag') }}</span></div>
            <div v-for="i in cur.items" :key="i.id" style="padding:3px 0">{{ i.qty }} × {{ both(i.name, i.name_en) }}<span v-if="i.batch_seq > 1" class="small" style="color:#b45309"> · {{ t('ao.n', { n: i.batch_seq - 1 }) }}</span></div></div>
          <div class="card"><div class="k">{{ t('wk.payment') }}</div>
            <div class="row between"><span>{{ due(cur) ? t('wk.cod') : t('wk.prepaid') }}</span>
              <span v-if="due(cur)"><span v-if="was(cur)" class="small muted" data-testid="rider-was">{{ was(cur) }} </span><b class="amt">{{ money(collect(cur), cur.currency) }}</b></span><b v-else style="color:var(--ok)">{{ t('wk.no_cash') }}</b></div></div>
          <button class="btn primary block big" :disabled="cur.packing_status !== 'packed' || busy" data-act="pickup" @click="act(cur, 'pickup')">{{ t('wk.confirm_pickup_n', { n: cur.bags ?? 0 }) }}</button>
        </template>

        <!-- on the way, then at the door -->
        <template v-else>
          <div class="card stack">
            <div><b style="font-size:16px">{{ contactName(cur) }}</b>
              <div>{{ cur.zone_name }} · {{ cur.address_line }} <b>{{ cur.address_unit }}</b>
                <span v-if="cur.zone_distance_m" class="tag warn">{{ t('outside_zone', { n: cur.zone_distance_m }) }}</span></div>
              <div v-if="cur.address_note" class="banner warn small" style="margin-top:6px">{{ t('wk.customer_note') }}：{{ cur.address_note }}</div></div>
            <div class="row">
              <a class="btn grow" :href="mapUrl(cur)" target="_blank" rel="noopener" data-act="navigate">{{ t('navigate') }}</a>
              <a class="btn grow" :href="'tel:' + contactPhone(cur)">{{ t('call') }}</a>
            </div>
            <div v-if="contactApps(cur).length || cur.address_photo" class="small" data-testid="rider-contact">
              <span v-for="c in contactApps(cur)" :key="c.field">{{ c.label }} <b>{{ c.value }}</b>　</span>
              <a v-if="cur.address_photo" :href="`/api/v1/rider/tasks/${cur.delivery!.id}/address-photo`" target="_blank" rel="noopener" data-testid="rider-door-photo">{{ t('oc.door_photo') }} ›</a>
            </div>
            <div class="row between"><span class="muted">{{ t('bags') }} {{ cur.bags }} · {{ due(cur) ? t('wk.cod_short') : t('wk.prepaid') }}</span>
              <b v-if="due(cur)" class="amt">{{ money(collect(cur), cur.currency) }}</b></div>
          </div>

          <button v-if="st(cur) === 'picked_up'" class="btn primary block big" :disabled="busy" data-act="arrive" @click="act(cur, 'arrive')">{{ t('wk.i_arrived') }}</button>

          <template v-else>
            <div v-if="due(cur)" class="card stack cash">
              <div class="row between"><span class="k">{{ t('wk.due') }}</span><b class="amt" style="font-size:28px">{{ money(collect(cur), cur.currency) }}</b></div>
              <div class="seg" data-testid="door-pay">
                <button :class="{ on: !door.online }" data-testid="door-cash" @click="door.online = false">{{ t('ck.rider_cash') }}</button>
                <button :class="{ on: door.online }" data-testid="door-online" @click="payOnline">{{ t('ck.rider_online') }}</button>
              </div>
              <div v-if="door.online" class="quick">
                <button v-for="c in door.list ?? []" :key="c.id" :class="{ on: door.channel === c.id }" data-testid="door-channel" @click="door.channel = c.id">{{ channelName(c) }}</button>
                <span v-if="door.list && !door.list.length" class="small muted">{{ t('ck.no_channels') }}</span>
              </div>
              <template v-else>
              <label class="field" style="margin:0"><span>{{ t('wk.received') }}</span>
                <input v-model="cash.value" type="number" inputmode="decimal" min="0" step="0.01" name="collected" /></label>
              <div class="quick">
                <button :class="{ on: diff === 0 }" @click="cash.value = String(toMajor(collect(cur)))">{{ t('wk.exact', { amt: money(collect(cur), cur.currency) }) }}</button>
                <button v-for="v in quick" :key="v" :class="{ on: got === toMinor(v) }" :data-quick="v" @click="cash.value = String(v)">{{ money(toMinor(v), cur.currency) }}</button>
              </div>
              <div v-if="diff > 0" class="muted small" data-testid="change">{{ t('wk.change', { got: money(got, cur.currency), change: money(diff, cur.currency) }) }}</div>
              <div v-else-if="diff < 0" class="small" style="color:var(--warn)">{{ t('wk.short', { amt: money(-diff, cur.currency) }) }}</div>
              </template>
            </div>
            <div v-else class="banner ok">{{ t('wk.prepaid') }} · {{ t('wk.no_cash') }}</div>
            <div class="card stack" data-testid="proof">
              <div class="row between">
                <span>{{ t('wk.photo') }} <span v-if="cur.delivery?.proof_at" class="tag ok" data-testid="proof-done">{{ t('wk.photo_done') }}</span></span>
                <label class="btn small" :class="{ disabled: photo.busy }">{{ cur.delivery?.proof_at ? t('wk.photo_again') : t('wk.photo_take') }}
                  <input type="file" accept="image/*" capture="environment" name="proof" hidden :disabled="photo.busy" @change="addPhoto(cur, $event)" /></label>
              </div>
              <div class="small muted">{{ t('wk.photo_hint') }}</div>
              <div v-if="photo.error" class="banner bad small">{{ photo.error }}</div>
            </div>
            <button class="btn primary block big" :disabled="busy || (due(cur) && door.online && !door.channel)" data-act="deliver" @click="deliver(cur)">
              {{ due(cur) && door.online ? t('wk.confirm_deliver') : due(cur) ? t('wk.confirm_deliver_cash', { amt: money(collect(cur), cur.currency) }) : t('wk.confirm_deliver') }}</button>
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
          <div class="row between"><b class="no">{{ o.public_no }} <i v-if="brandOf(o)" class="wbrand" data-testid="rider-brand" :style="{ background: brandOf(o)!.color }">{{ brandOf(o)!.name }}</i> <span v-if="o.scheduled_for" class="tag info">{{ whenText(o.scheduled_for, o.timezone) }}</span>
            <span v-if="addons(o).length" class="tag addon">{{ t('ao.tag') }}</span></b>
            <span class="tag" :class="st(o) === 'assigned' ? 'brand' : 'info'">{{ st(o) === 'assigned' ? t('wk.to_pickup') : t('f.' + o.fulfillment_status) }}</span></div>
          <div v-if="st(o) === 'assigned'" class="line"><span class="pt s"></span><span>{{ branchName }} ·
            <b :style="{ color: o.packing_status === 'packed' ? 'var(--ok)' : 'var(--warn)' }">{{ o.packing_status === 'packed' ? `${t('p.packed')} ${o.bags ?? ''} ${t('bags')}` : t('not_packed') }}</b></span></div>
          <div class="line"><span class="pt"></span><span>{{ o.zone_name }} · {{ o.address_line }} {{ o.address_unit }}</span></div>
          <div class="row between small muted"><span>{{ itemsText(o) }}</span>
            <span style="white-space:nowrap">{{ due(o) ? t('wk.cod_short') : t('wk.prepaid') }} <span v-if="due(o) && was(o)" class="small muted">{{ was(o) }} </span><b v-if="due(o)" style="color:var(--brand-dark);font-size:16px">{{ money(collect(o), o.currency) }}</b></span></div>
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
          <span><b>{{ o.public_no }}</b> <i v-if="brandOf(o)" class="wbrand" :style="{ background: brandOf(o)!.color }">{{ brandOf(o)!.name }}</i> · {{ o.zone_name }}</span>
          <span class="tag" :class="st(o) === 'delivered' ? 'ok' : 'bad'">{{ t('f.' + o.fulfillment_status) }}</span>
        </div>
        <div v-if="!done.length" class="card muted">{{ t('wk.done_empty') }}</div>
      </template>
    </div>
  </div>
</template>

<style scoped>
.wbrand { font-style: normal; color: #fff; font-weight: 700; font-size: 12px; border-radius: 5px; padding: 0 6px; line-height: 18px; display: inline-block; vertical-align: 1px; white-space: nowrap; }
.tag.addon { background: #fff1e0; color: #b45309; border-color: #f5c48a; }
</style>
