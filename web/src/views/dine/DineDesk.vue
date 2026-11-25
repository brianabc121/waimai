<script setup lang="ts">
// 点单 · 堂食下单, the dine-in desk (docs/DINE_IN_ORDER_DESIGN.md):
// staff at a table or the counter, on a tablet or a phone, open tables and
// take orders by tapping only. This is the table map (§5), opening a table
// (§6), moving it and changing the guests; the order screen and the bill are
// DineOrder and DineBill.
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { ApiError, get, patch, post } from '../../api'
import { money, t } from '../../i18n'
import { useLive } from '../../live'
import { can, currentRole, session } from '../../session'
import type { Bill, DiningTable, DinePresets, Storefront } from '../../types'
import DineOrder, { type DeskLine } from './DineOrder.vue'
import DineBill from './DineBill.vue'
import DineSettings from './DineSettings.vue'
import { longMinutes, presetList, sinceText } from './strings'
import './dine.css'

const props = defineProps<{ store: Storefront | null }>()
const router = useRouter()
const route = useRoute()

const bid = session.branchId!
const base = `/merchant/branches/${bid}`
const cur = computed(() => props.store?.branch.currency ?? currentRole()?.currency ?? 'PHP')
const m = (v: number) => money(v, cur.value)
const manager = can('table.manage')

const tables = ref<DiningTable[]>([])
const presets = ref<DinePresets | null>(null)
const top = ref<number[]>([])
// free tables kept for a called queue number (docs/QUEUE_DISPLAY_DESIGN.md §6.4)
const holds = ref<Record<string, string>>({})
const loaded = ref(false)
const error = ref('')
const toast = ref('')
let toastTimer = 0
function flash(s: string) { toast.value = s; clearTimeout(toastTimer); toastTimer = window.setTimeout(() => (toast.value = ''), 2600) }

const now = ref(Date.now())
const tick = window.setInterval(() => (now.value = Date.now()), 30000)
onBeforeUnmount(() => clearInterval(tick))

// phone: one column at a time
const phoneQ = window.matchMedia('(max-width: 760px)')
const phone = ref(phoneQ.matches)
const onPhone = () => (phone.value = phoneQ.matches)
onMounted(() => phoneQ.addEventListener('change', onPhone))
onBeforeUnmount(() => phoneQ.removeEventListener('change', onPhone))

// ---- what is on screen ----
type Screen = 'tables' | 'order' | 'bill'
const screen = ref<Screen>('tables')
const sel = ref<number | null>(null)          // selected table id
const bill = ref<Bill | null>(null)            // the selected table's bill
const again = ref<DeskLine | null>(null)       // "one more" from the bill
const payFirst = ref(false)

async function loadBill(sessionId: number) {
  const b = await get<Bill>(`/merchant/table-sessions/${sessionId}`)
  if (bill.value?.session_id === sessionId && b.order.version < bill.value.order.version) return
  bill.value = b
}
const { online, refreshNow } = useLive(() => `${base}/events`, async () => {
  const r = await get<{ tables: DiningTable[] }>(`${base}/tables`)
  tables.value = r.tables
  loaded.value = true
  const tb = tables.value.find(x => x.id === sel.value)
  if (tb?.session && bill.value?.session_id === tb.session.id && bill.value.status === 'open') await loadBill(tb.session.id).catch(() => {})
  else if (tb?.session && bill.value?.session_id !== tb.session.id) await loadBill(tb.session.id).catch(() => {})
  else if (!tb?.session && screen.value === 'tables') bill.value = null
})
async function loadDesk() {
  try {
    const r = await get<{ presets: DinePresets; top: number[]; holds?: Record<string, string> }>(`${base}/dine-desk`)
    presets.value = r.presets; top.value = r.top; holds.value = r.holds ?? {}
  } catch (e) { error.value = (e as Error).message }
}
onMounted(loadDesk)

// ---- areas ----
const live = computed(() => tables.value.filter(x => x.active || x.session))
const areas = computed(() => {
  const names: string[] = []
  for (const x of live.value) if (!names.includes(x.area)) names.push(x.area)
  return names.length > 1 || names[0] ? names : []
})
const area = ref<string | null>(null)
const shown = computed(() => area.value === null ? live.value : live.value.filter(x => x.area === area.value))
const areaName = (a: string) => a || t('dd.no_area')
const count = (a: string | null) => a === null ? live.value.length : live.value.filter(x => x.area === a).length

// ---- a table's state ----
const mins = (iso: string) => { void now.value; return (now.value - new Date(iso).getTime()) / 60000 }
function state(tb: DiningTable) {
  const s = tb.session
  if (!s) return 'free'
  if (mins(s.opened_at) > longMinutes(presets.value)) return 'long'
  if (s.unserved_qty > 0) return 'wait'
  return 'busy'
}
const selTable = computed(() => tables.value.find(x => x.id === sel.value) ?? null)

async function tap(tb: DiningTable) {
  error.value = ''
  sel.value = tb.id
  if (!tb.session) { startOpen(tb); return }
  try {
    await loadBill(tb.session.id)
    if (phone.value) { again.value = null; screen.value = 'order' }
  } catch (e) { error.value = (e as Error).message }
}

// ---- opening ----
const opening = reactive<{ table: DiningTable | null; guests: number; more: boolean; tags: string[]; extra: string[]; writing: boolean; text: string; busy: boolean }>(
  { table: null, guests: 2, more: false, tags: [], extra: [], writing: false, text: '', busy: false })
function startOpen(tb: DiningTable) {
  Object.assign(opening, { table: tb, guests: Math.min(tb.seats || 2, 99), more: (tb.seats || 2) > 11, tags: [], extra: [], writing: false, text: '', busy: false })
}
const tableTags = computed(() => [...presetList(presets.value, 'table_tags'), ...opening.extra])
function toggleTag(list: string[], x: string) { const i = list.indexOf(x); if (i >= 0) list.splice(i, 1); else list.push(x) }
function addWritten() {
  const s = opening.text.trim().slice(0, 20)
  if (s && !tableTags.value.includes(s)) opening.extra.push(s)
  if (s && !opening.tags.includes(s)) opening.tags.push(s)
  opening.text = ''; opening.writing = false
}
async function openTable(order: boolean) {
  const tb = opening.table!
  opening.busy = true
  try {
    bill.value = await post<Bill>(`/merchant/tables/${tb.id}/open`, { guests: opening.guests, tags: opening.tags })
    sel.value = tb.id
    opening.table = null
    again.value = null
    if (order) screen.value = 'order'
    await refreshNow()
  } catch (e) { error.value = errText(e); opening.table = null }
  opening.busy = false
}

// ---- guests, moving ----
const guestsFor = reactive<{ open: boolean; n: number; more: boolean }>({ open: false, n: 2, more: false })
function startGuests() { if (!bill.value) return; Object.assign(guestsFor, { open: true, n: bill.value.guests, more: bill.value.guests > 11 }) }
async function saveGuests() {
  try { bill.value = await patch<Bill>(`/merchant/table-sessions/${bill.value!.session_id}`, { guests: guestsFor.n }); guestsFor.open = false; void refreshNow() }
  catch (e) { error.value = errText(e) }
}
const moving = ref(false)
const freeTables = computed(() => live.value.filter(x => !x.session && x.active))
async function moveTo(tb: DiningTable) {
  try {
    bill.value = await post<Bill>(`/merchant/table-sessions/${bill.value!.session_id}/move`, { table_id: tb.id })
    moving.value = false; sel.value = tb.id
    flash(t('dd.moved', { name: tb.name }))
    void refreshNow()
  } catch (e) { error.value = errText(e); moving.value = false; void refreshNow() }
}

function errText(e: unknown) {
  if (e instanceof ApiError && e.status === 0) return t('submit_failed')
  return (e as Error).message
}

// ---- moving between screens ----
function goOrder() { again.value = null; screen.value = 'order' }
function goBill(pay = false) { payFirst.value = pay; screen.value = 'bill' }
function back() { screen.value = 'tables'; again.value = null; void refreshNow() }
function onSent(b: Bill, n: number) {
  bill.value = b
  flash(t('dd.sent_ok', { t: b.table_name, n }))
  if (!presets.value?.stay_after_send) back()
  else void refreshNow()
}
function onAgain(l: DeskLine) { again.value = l; screen.value = 'order' }
function onClosed() { back(); sel.value = null; bill.value = null }

// deep link from the tables page: ?table=ID opens that table
onMounted(async () => {
  const id = Number(route.query.table)
  if (!id) return
  const stop = watch(loaded, async ok => {
    if (!ok) return
    stop()
    const tb = tables.value.find(x => x.id === id)
    if (!tb) return
    await tap(tb)
    if (tb.session && route.query.view === 'bill') goBill(route.query.pay === '1')
    else if (tb.session && route.query.view === 'order') goOrder()
  }, { immediate: true })
})

const settingsOpen = ref(false)
const menuItems = computed(() => new Map((props.store?.categories ?? []).flatMap(c => c.items).map(i => [i.id, i])))
const sentTotals = computed(() => {
  const items = (bill.value?.order.items ?? []).filter(i => i.prep_status !== 'cancelled')
  return { n: items.reduce((s, i) => s + i.qty, 0), amt: items.reduce((s, i) => s + i.line_total, 0) }
})
const sideItems = computed(() => {
  const items = (bill.value?.order.items ?? []).filter(i => i.prep_status !== 'cancelled')
  return [...items].sort((a, b) => Number(!!a.served_at) - Number(!!b.served_at))
})
function stText(it: { prep_status: string; served_at: string | null; batch_at?: string }) {
  if (it.prep_status === 'cancelled') return ['void', t('dd.st.void')]
  if (it.served_at) return ['served', t('dd.st.served')]
  if (it.prep_status === 'ready') return ['ready', t('dd.st.ready')]
  return ['cook', it.batch_at ? t('dd.st.cook_m', { n: Math.max(0, Math.floor(mins(it.batch_at))) }) : t('dd.st.cook')]
}
</script>

<template>
  <div class="dd warm" data-testid="dine-desk">
    <!-- the table map -->
    <template v-if="screen === 'tables'">
      <div class="dd-top">
        <button class="dd-back" @click="router.push('/merchant/orders')"><span class="rtl-flip">‹</span> {{ t('vo.back') }}</button>
        <h1 data-testid="dd-title">{{ t('pos.dine') }}</h1>
        <span class="sp"></span>
        <button v-if="manager" class="dd-gear" :title="t('dd.settings')" data-testid="dd-settings" @click="settingsOpen = true">⚙</button>
        <span v-if="!phone" class="dd-shop">{{ store?.branch.name }}</span>
      </div>
      <div v-if="!online" class="banner warn dd-banner">{{ t('dd.offline') }}</div>
      <div v-if="error" class="banner bad dd-banner" @click="error = ''">{{ error }}</div>
      <div class="dd-body">
        <div class="dd-main">
          <div v-if="areas.length || !phone" class="dd-bar">
            <template v-if="areas.length">
              <button class="dd-tab" :class="{ on: area === null }" @click="area = null">{{ t('dd.all') }}<small>{{ count(null) }}</small></button>
              <button v-for="a in areas" :key="a" class="dd-tab" :class="{ on: area === a }" data-testid="dd-area" @click="area = a">{{ areaName(a) }}<small>{{ count(a) }}</small></button>
            </template>
            <div v-if="!phone" class="dd-legend">
              <span><i class="free"></i>{{ t('dd.lg.free') }}</span><span><i class="busy"></i>{{ t('dd.lg.busy') }}</span>
              <span><i class="wait"></i>{{ t('dd.lg.wait') }}</span><span><i class="long"></i>{{ t('dd.lg.long', { n: longMinutes(presets) }) }}</span>
            </div>
          </div>
          <div v-if="loaded && !live.length" class="dd-empty">{{ t('dd.no_tables') }}
            <router-link v-if="manager" to="/merchant/tables" class="btn small" style="margin-inline-start:8px">{{ t('tables') }}</router-link></div>
          <div class="dd-tgrid">
            <button v-for="tb in shown" :key="tb.id" class="dd-tc" :class="[state(tb), { sel: tb.id === sel }]" :data-testid="`dd-table-${tb.name}`" @click="tap(tb)">
              <span v-if="tb.session && tb.session.unserved_qty" class="badge">{{ phone ? tb.session.unserved_qty : t('dd.to_serve', { n: tb.session.unserved_qty }) }}</span>
              <span class="row1"><b class="nm">{{ tb.name }}</b>
                <span v-if="tb.session" class="g">{{ phone ? t('dd.guests_n', { n: tb.session.guests }) : t('dd.guests', { g: tb.session.guests, s: tb.seats }) }}</span></span>
              <template v-if="tb.session">
                <span v-if="tb.session.item_count" class="amt">{{ m(tb.session.total) }}</span>
                <span v-else class="amt none">{{ t('dd.not_ordered') }}</span>
                <span class="tm">{{ sinceText(tb.session.opened_at, now) }}<template v-if="tb.session.rounds && !phone"> · {{ t('dd.rounds', { n: tb.session.rounds }) }}</template></span>
              </template>
              <template v-else>
                <span class="cap">{{ t('dd.seats', { n: tb.seats }) }}</span>
                <span v-if="holds[tb.id]" class="held" :data-testid="`dd-held-${tb.name}`">{{ t('qu.held', { no: holds[tb.id] }) }}</span>
                <span v-else class="free">{{ phone ? t('dd.open_only') : t('dd.tap_open') }}</span>
              </template>
            </button>
          </div>
        </div>

        <!-- the selected table, beside the map (not on a phone) -->
        <aside v-if="!phone" class="dd-side" data-testid="dd-side">
          <template v-if="selTable?.session && bill && bill.status === 'open' && bill.table_id === selTable.id">
            <div class="sh"><h2>{{ bill.table_name }} <small>{{ [bill.area, t('dd.guests', { g: bill.guests, s: bill.seats }), t('dd.opened', { t: new Date(bill.opened_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) })].filter(Boolean).join(' · ') }}</small></h2>
              <div v-if="bill.tags.length || bill.moved_from" class="dd-chiprow"><span v-for="x in bill.tags" :key="x" class="dd-tag">{{ x }}</span>
                <span v-if="bill.moved_from" class="dd-tag dim">{{ t('dd.moved_from', { name: bill.moved_from }) }}</span></div></div>
            <div class="sb">
              <div class="dd-stats"><div><b>{{ m(bill.order.totals.total) }}</b>{{ t('dd.ordered') }}</div><div><b>{{ Math.max(0, Math.floor(mins(bill.opened_at))) }}</b>{{ t('dd.minutes') }}</div>
                <div><b :class="{ warn: selTable.session.unserved_qty }">{{ selTable.session.unserved_qty }}</b>{{ t('dd.unserved') }}</div></div>
              <div v-for="it in sideItems" :key="it.id" class="dd-mini"><b>{{ it.qty }}</b><span class="grow">{{ it.name }}<template v-if="it.options.length || it.note_tags?.length"> · {{ [...it.options.map(o => o.name), ...(it.note_tags ?? []).map(x => x.name)].join(' ') }}</template></span>
                <span class="dd-st" :class="stText(it)[0]">{{ stText(it)[1] }}</span></div>
              <div v-if="!sideItems.length" class="muted small">{{ t('dd.not_ordered') }}</div>
            </div>
            <div class="sf">
              <button class="dd-big" data-testid="dd-add" @click="goOrder">{{ t('dd.add') }}</button>
              <div class="two"><button class="dd-big ghost" data-testid="dd-bill" @click="goBill(false)">{{ t('dd.bill') }}</button>
                <button class="dd-big gold" data-testid="dd-pay" @click="goBill(true)">{{ t('dd.pay') }}</button></div>
              <div class="two"><button class="dd-big ghost sm" data-testid="dd-move" @click="moving = true">{{ t('dd.move') }}</button>
                <button class="dd-big ghost sm" data-testid="dd-guests" @click="startGuests">{{ t('dd.guests_btn') }}</button></div>
            </div>
          </template>
          <div v-else class="sb muted dd-pick">{{ t('dd.pick_table') }}
            <div v-if="loaded" class="dd-stats" style="margin-top:16px">
              <div><b>{{ live.filter(x => !x.session).length }}</b>{{ t('dd.lg.free') }}</div>
              <div><b>{{ live.filter(x => x.session).length }}</b>{{ t('dd.lg.busy') }}</div>
              <div><b>{{ live.reduce((s, x) => s + (x.session?.unserved_qty ?? 0), 0) }}</b>{{ t('dd.unserved') }}</div></div>
          </div>
        </aside>
      </div>
    </template>

    <DineOrder v-else-if="screen === 'order' && bill" :key="bill.session_id" :bill="bill" :store="store" :presets="presets" :top="top" :phone="phone" :again="again"
      :sent="sentTotals" @back="back" @sent="onSent" @bill="goBill(false)" @pay="goBill(true)" @move="moving = true" @guests="startGuests" />
    <DineBill v-else-if="screen === 'bill' && bill" :bill="bill" :presets="presets" :currency="cur" :country="store?.branch.country_code ?? ''" :phone="phone" :pay-first="payFirst" :items="menuItems"
      @back="back" @update="b => (bill = b)" @again="onAgain" @closed="onClosed" @add="goOrder" />

    <!-- open a table: guests and notes -->
    <div v-if="opening.table" class="dd-mask" @click.self="opening.table = null">
      <div class="dd-modal" data-testid="dd-open">
        <div class="mh"><h3>{{ t('dd.open_title', { name: opening.table.name }) }}</h3>
          <span class="muted">{{ [opening.table.area, t('dd.seats', { n: opening.table.seats })].filter(Boolean).join(' · ') }}</span>
          <button class="x" :aria-label="t('dd.cancel')" @click="opening.table = null">✕</button></div>
        <div class="mb">
          <div class="dd-lbl">{{ t('dd.how_many') }} <small>{{ t('dd.tap_once') }}</small></div>
          <div v-if="!opening.more" class="dd-num">
            <button v-for="n in 11" :key="n" :class="{ on: opening.guests === n }" :data-testid="`dd-guests-${n}`" @click="opening.guests = n">{{ n }}</button>
            <button class="sm" @click="opening.more = true; opening.guests = Math.max(opening.guests, 12)">{{ t('dd.more') }} ＋</button>
          </div>
          <div v-else class="dd-stp big"><button @click="opening.guests = Math.max(1, opening.guests - 1)">−</button><b>{{ opening.guests }}</b>
            <button class="f" @click="opening.guests = Math.min(99, opening.guests + 1)">＋</button></div>
          <div class="dd-lbl" style="margin-top:18px">{{ t('dd.table_tags') }} <small>{{ t('dd.table_tags_hint') }}</small></div>
          <div class="dd-chs">
            <button v-for="x in tableTags" :key="x" class="dd-ch" :class="{ on: opening.tags.includes(x) }" @click="toggleTag(opening.tags, x)">{{ x }}</button>
            <button v-if="!opening.writing" class="dd-ch add" @click="opening.writing = true">{{ t('dd.other') }}</button>
            <span v-else class="dd-write"><input v-model="opening.text" maxlength="20" :placeholder="t('dd.write_ph')" @keydown.enter.prevent="addWritten" />
              <button class="dd-ch on" @click="addWritten">{{ t('dd.write_add') }}</button></span>
          </div>
        </div>
        <div class="mf"><button class="dd-big ghost" :disabled="opening.busy" data-testid="dd-open-only" @click="openTable(false)">{{ t('dd.open_only') }}</button>
          <button class="dd-big" style="flex:2" :disabled="opening.busy" data-testid="dd-open-order" @click="openTable(true)">{{ t('dd.open_order') }}</button></div>
      </div>
    </div>

    <!-- change guests -->
    <div v-if="guestsFor.open && bill" class="dd-mask" @click.self="guestsFor.open = false">
      <div class="dd-modal">
        <div class="mh"><h3>{{ t('dd.guests_title', { name: bill.table_name }) }}</h3><button class="x" @click="guestsFor.open = false">✕</button></div>
        <div class="mb">
          <div v-if="!guestsFor.more" class="dd-num">
            <button v-for="n in 11" :key="n" :class="{ on: guestsFor.n === n }" @click="guestsFor.n = n">{{ n }}</button>
            <button class="sm" @click="guestsFor.more = true; guestsFor.n = Math.max(guestsFor.n, 12)">{{ t('dd.more') }} ＋</button>
          </div>
          <div v-else class="dd-stp big"><button @click="guestsFor.n = Math.max(1, guestsFor.n - 1)">−</button><b>{{ guestsFor.n }}</b>
            <button class="f" @click="guestsFor.n = Math.min(99, guestsFor.n + 1)">＋</button></div>
        </div>
        <div class="mf"><button class="dd-big ghost" @click="guestsFor.open = false">{{ t('dd.cancel') }}</button><button class="dd-big" style="flex:2" @click="saveGuests">{{ t('dd.save') }}</button></div>
      </div>
    </div>

    <!-- move to a free table -->
    <div v-if="moving && bill" class="dd-mask" @click.self="moving = false">
      <div class="dd-modal wide" data-testid="dd-move-pick">
        <div class="mh"><h3>{{ t('dd.move_title', { name: bill.table_name }) }}</h3><span class="muted">{{ t('dd.move_hint') }}</span><button class="x" @click="moving = false">✕</button></div>
        <div class="mb"><div class="dd-tgrid small">
          <button v-for="tb in freeTables" :key="tb.id" class="dd-tc free" :data-testid="`dd-move-${tb.name}`" @click="moveTo(tb)">
            <span class="row1"><b class="nm">{{ tb.name }}</b></span><span class="cap">{{ [tb.area, t('dd.seats', { n: tb.seats })].filter(Boolean).join(' · ') }}</span></button>
        </div></div>
      </div>
    </div>

    <DineSettings v-if="settingsOpen" :presets="presets" :country="store?.branch.country_code ?? ''" @close="settingsOpen = false" @saved="p => (presets = p)" />
    <div v-if="toast" class="dd-toast" data-testid="dd-toast">{{ toast }}</div>
  </div>
</template>
