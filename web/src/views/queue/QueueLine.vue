<script setup lang="ts">
// The restaurant's line (docs/QUEUE_DISPLAY_DESIGN.md §6, §7): groups on the
// left, numbers in the middle, free tables with whom to call on the right.
import QRCode from 'qrcode'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { post } from '../../api'
import { t } from '../../i18n'
import { can, currentRole } from '../../session'
import {
  estText, groupName, groupRange, minsSince, qErr, REASONS, TAGS, tagText,
  type QueueDesk, type QueueTable, type QueueTicket,
} from '../../queue'

const props = defineProps<{ desk: QueueDesk; base: string; take: number }>()
const emit = defineEmits<{ reload: [] }>()

const canJump = computed(() => can('queue.jump'))
const now = ref(Date.now())
let tick = 0
onMounted(() => { tick = window.setInterval(() => { now.value = Date.now() }, 20000) })
onBeforeUnmount(() => window.clearInterval(tick))

const error = ref('')
const flash = ref('')
let flashTimer = 0
function done(msg: string) { flash.value = msg; window.clearTimeout(flashTimer); flashTimer = window.setTimeout(() => { flash.value = '' }, 4000) }

// ---- the list ----
const filter = ref<number | 'all' | 'skipped'>('all')
const groupOf = (id: number) => props.desk.groups.find(g => g.id === id)
const inLine = computed(() => props.desk.tickets.filter(x => x.status === 'waiting' || x.status === 'called'))
const skipped = computed(() => props.desk.tickets.filter(x => x.status === 'skipped'))
const countIn = (gid: number) => inLine.value.filter(x => x.group_id === gid).length
const sections = computed(() => {
  if (filter.value === 'skipped') return [{ g: null, rows: skipped.value }]
  const gs = filter.value === 'all' ? props.desk.groups : props.desk.groups.filter(g => g.id === filter.value)
  return gs.map(g => ({ g, rows: inLine.value.filter(x => x.group_id === g.id)
    .sort((a, b) => (a.status === 'called' ? 0 : 1) - (b.status === 'called' ? 0 : 1) || a.position - b.position) }))
    .filter(s => s.rows.length || filter.value !== 'all')
})
const alertMin = computed(() => props.desk.settings.call_alert_minutes)
const waited = (x: QueueTicket) => minsSince(x.created_at, now.value)
const waitClass = (x: QueueTicket) => { const m = waited(x); return m >= 40 ? 'r' : m >= 20 ? 'y' : '' }
const late = (x: QueueTicket) => x.status === 'called' && minsSince(x.called_at, now.value) >= alertMin.value
const tail = (p: string) => p ? '…' + p.slice(-4) : ''

// ---- actions ----
const busy = ref(0)
async function act(x: QueueTicket, action: string, body: Record<string, unknown> = {}) {
  busy.value = x.id
  error.value = ''
  menu.value = 0
  try {
    const r = await post<{ ticket: QueueTicket; session_id?: number; table_id?: number }>(`${props.base}/tickets/${x.id}/${action}`, body)
    emit('reload')
    return r
  } catch (e) { error.value = qErr(e); emit('reload'); return null } finally { busy.value = 0 }
}
async function call(x: QueueTicket, tableId?: number) {
  const r = await act(x, 'call', tableId ? { table_id: tableId } : {})
  if (r) done(t('qu.act.call') + ' ' + x.number)
}
async function cancel(x: QueueTicket) {
  if (!confirm(t('qu.cancel_q', { no: x.number }))) return
  await act(x, 'cancel')
}
const menu = ref(0)
function toggleMenu(id: number) { menu.value = menu.value === id ? 0 : id }
const closeMenu = (e: MouseEvent) => { if (!(e.target as HTMLElement).closest('.qmenu, .qmore')) menu.value = 0 }
onMounted(() => document.addEventListener('click', closeMenu))
onBeforeUnmount(() => document.removeEventListener('click', closeMenu))

// ---- tables: free (with whom to call) and freeing up soon ----
const freeTables = computed(() => props.desk.tables.filter(x => x.state !== 'busy'))
const soonTables = computed(() => props.desk.tables.filter(x => x.state === 'busy' && x.soon))
const suggestionFor = (tb: QueueTable) => {
  const s = props.desk.suggestions.find(s => s.table_id === tb.id)
  if (!s) return null
  const tk = props.desk.tickets.find(x => x.id === s.ticket_id)
  return tk ? { ...s, ticket: tk } : null
}
const holder = (tb: QueueTable) => props.desk.tickets.find(x => x.status === 'called' && x.called_table_id === tb.id)

// ---- take a number ----
const takeOpen = ref(false)
const tk = ref({ people: 0, more: false, phone: '', tags: [] as string[], custom: '', priority: false, reason: '', other: '' })
const taken = ref<QueueTicket | null>(null)
watch(() => props.take, () => openTake())
function openTake() {
  tk.value = { people: 0, more: false, phone: '', tags: [], custom: '', priority: false, reason: '', other: '' }
  taken.value = null
  takeOpen.value = true
}
const takeGroup = computed(() => props.desk.groups.find(g => tk.value.people >= g.min_people && tk.value.people <= g.max_people))
function toggleTag(s: string) { const l = tk.value.tags; const i = l.indexOf(s); if (i >= 0) l.splice(i, 1); else l.push(s) }
const reasonText = (r: string, other: string) => r === 'other' ? other.trim() : r ? t('qu.r.' + r) : ''
async function giveNumber(print: boolean) {
  const v = tk.value
  const tags = [...v.tags, ...(v.custom.trim() ? [v.custom.trim()] : [])]
  const reason = v.priority ? reasonText(v.reason, v.other) : ''
  error.value = ''
  busy.value = -1
  try {
    taken.value = await post<QueueTicket>(`${props.base}/tickets`, { people: v.people, phone: v.phone.trim(), tags, priority: v.priority, reason, source: 'paper' })
    emit('reload')
    if (print) await printSlip(taken.value)
  } catch (e) { error.value = qErr(e) } finally { busy.value = 0 }
}

// ---- the printed slip ----
const slip = ref<{ t: QueueTicket; qr: string } | null>(null)
async function printSlip(x: QueueTicket) {
  const qr = x.token ? await QRCode.toDataURL(`${location.origin}/q/t/${x.token}`, { width: 220, margin: 1 }).catch(() => '') : ''
  slip.value = { t: x, qr }
  await nextTick()
  document.body.classList.add('print-slip')
  const off = () => { document.body.classList.remove('print-slip'); window.removeEventListener('afterprint', off) }
  window.addEventListener('afterprint', off)
  window.print()
  window.setTimeout(off, 1000)
}
const store = computed(() => currentRole()?.branch_name ?? '')
const slipTime = (iso: string) => new Date(iso).toLocaleString(undefined, { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', timeZone: props.desk.timezone || undefined })

// ---- seat ----
const seatFor = ref<QueueTicket | null>(null)
const seated = ref<{ no: string; table: string; tableId: number } | null>(null)
const seatTables = computed(() => {
  const x = seatFor.value
  if (!x) return []
  const rec = (tb: QueueTable) => tb.id === x.called_table_id || props.desk.suggestions.some(s => s.table_id === tb.id && s.ticket_id === x.id)
  return freeTables.value.map(tb => ({ tb, rec: rec(tb), otherHold: tb.state === 'held' && tb.id !== x.called_table_id }))
    .sort((a, b) => Number(b.rec) - Number(a.rec) || Number(a.otherHold) - Number(b.otherHold) || Math.abs(a.tb.seats - x.people) - Math.abs(b.tb.seats - x.people))
})
function openSeat(x: QueueTicket) { seatFor.value = x; seated.value = null; menu.value = 0 }
async function seat(tb: QueueTable, otherHold: boolean) {
  const x = seatFor.value!
  if (otherHold && !confirm(t('qu.seat.held_q', { no: tb.held_by ?? '', me: x.number }))) return
  const r = await act(x, 'seat', { table_id: tb.id, force: otherHold })
  if (r) seated.value = { no: x.number, table: tb.name, tableId: r.table_id ?? tb.id }
}

// ---- move up ----
const jumpFor = ref<QueueTicket | null>(null)
const jp = ref({ first: true, pos: 2, reason: '', other: '' })
function openJump(x: QueueTicket) { jumpFor.value = x; jp.value = { first: true, pos: Math.max(1, x.position - 1), reason: '', other: '' }; menu.value = 0 }
async function jump() {
  const x = jumpFor.value!
  const r = await act(x, 'jump', { position: jp.value.first ? 1 : jp.value.pos, reason: reasonText(jp.value.reason, jp.value.other) })
  if (r) { jumpFor.value = null; done(t('qu.j.now', { n: r.ticket.position })) }
}

// ---- party size ----
const peopleFor = ref<QueueTicket | null>(null)
const pp = ref(2)
function openPeople(x: QueueTicket) { peopleFor.value = x; pp.value = x.people; menu.value = 0 }
async function savePeople() {
  const r = await act(peopleFor.value!, 'people', { people: pp.value })
  if (r) peopleFor.value = null
}
</script>

<template>
  <div class="ql">
    <div v-if="error" class="banner bad" data-testid="ql-error">{{ error }}</div>
    <div v-else-if="flash" class="banner ok" data-testid="ql-flash">{{ flash }}</div>
    <div class="ox-stats">
      <div class="ox-stat"><span class="l">{{ t('qu.s.waiting') }}</span><span class="v red" data-testid="ql-waiting">{{ t('qu.n_tables', { n: desk.stats.waiting }) }}</span></div>
      <div class="ox-stat"><span class="l">{{ t('qu.s.called') }}</span><span class="v">{{ desk.stats.called }}</span></div>
      <div class="ox-stat"><span class="l">{{ t('qu.s.seated') }}</span><span class="v" data-testid="ql-seated">{{ desk.stats.seated }}</span></div>
      <div class="ox-stat"><span class="l">{{ t('qu.s.avg') }}</span><span class="v">{{ desk.stats.seated ? t('qu.mins', { n: desk.stats.avg_wait }) : '—' }}</span></div>
    </div>

    <div class="ql-body">
      <aside class="ox-rail">
        <div class="ox-rail-h">{{ t('qu.set.groups') }}</div>
        <div class="ox-rail-list">
          <button class="ox-cat" :class="{ on: filter === 'all' }" @click="filter = 'all'"><span class="nm">{{ t('qu.all') }}</span><span class="n">{{ inLine.length }}</span></button>
          <button v-for="g in desk.groups" :key="g.id" class="ox-cat" :class="{ on: filter === g.id }" :data-testid="'ql-g-' + g.letter" @click="filter = g.id">
            <span class="gl">{{ g.letter }}</span><span class="nm">{{ groupName(g) }}<small>{{ groupRange(g) }}</small></span><span class="n">{{ countIn(g.id) }}</span></button>
          <button class="ox-cat" :class="{ on: filter === 'skipped' }" data-testid="ql-g-skipped" @click="filter = 'skipped'"><span class="nm">{{ t('qu.skipped') }}</span><span class="n">{{ skipped.length }}</span></button>
        </div>
      </aside>

      <section class="ox-panel ql-list">
        <div v-if="!sections.some(s => s.rows.length)" class="ox-empty" data-testid="ql-empty">{{ t('qu.empty') }}</div>
        <template v-for="s in sections" :key="s.g?.id ?? 'skipped'">
          <div v-if="s.g && filter === 'all'" class="grp-h"><span class="l">{{ s.g.letter }}</span>{{ groupName(s.g) }} <em>{{ groupRange(s.g) }} · {{ t('qu.n_tables', { n: s.rows.length }) }}</em></div>
          <div v-for="x in s.rows" :key="x.id" class="qrow" :class="{ called: x.status === 'called', late: late(x), jump: !!x.jump_reason, missed: x.status === 'skipped' }" :data-no="x.number" data-testid="ql-row">
            <div class="no">{{ x.number }}</div>
            <div class="pp">{{ t('qu.people', { n: x.people }) }}</div>
            <div><span class="wt" :class="waitClass(x)">{{ t('qu.mins', { n: waited(x) }) }}</span></div>
            <div class="info">
              <span class="src">{{ t('qu.src.' + x.source) }}</span>
              <span v-for="g in x.tags" :key="g" class="ox-tag gold">{{ tagText(g) }}</span>
              <span v-if="x.phone">📱{{ tail(x.phone) }}</span>
              <span v-if="x.jump_reason" class="ox-tag blue">{{ t('qu.reason', { r: x.jump_reason }) }}</span>
              <span v-if="x.status === 'called'" class="ox-pill" :class="late(x) ? 'bad' : 'warn'">{{ t('qu.st.called', { n: x.calls }) }} · {{ t('qu.ago', { n: minsSince(x.called_at, now) }) }}<template v-if="x.called_table"> · {{ x.called_table }}</template></span>
              <span v-else-if="x.status === 'skipped'" class="ox-pill bad">{{ t('qu.st.skipped') }}</span>
              <span v-else class="muted">#{{ x.position }} · {{ estText(x.est_minutes) }}</span>
            </div>
            <div class="ops">
              <template v-if="x.status !== 'skipped'">
                <button class="btn small" :class="x.status === 'called' ? '' : 'primary'" :disabled="busy === x.id" data-testid="ql-call" @click="call(x)">{{ t(x.status === 'called' ? 'qu.act.recall' : 'qu.act.call') }}</button>
                <button class="btn small ok" :disabled="busy === x.id" data-testid="ql-seat" @click="openSeat(x)">{{ t('qu.act.seat') }}</button>
              </template>
              <button v-else class="btn small primary" :disabled="busy === x.id" data-testid="ql-requeue" @click="act(x, 'requeue')">{{ t('qu.act.requeue') }}</button>
              <button class="btn small qmore" :aria-label="t('qu.more')" data-testid="ql-more" @click="toggleMenu(x.id)">⋯</button>
              <div v-if="menu === x.id" class="qmenu">
                <button v-if="x.status === 'called'" data-testid="ql-skip" @click="act(x, 'skip')">{{ t('qu.act.skip') }}</button>
                <button v-if="x.status === 'skipped'" @click="call(x)">{{ t('qu.act.call') }}</button>
                <button v-if="x.status === 'waiting'" :disabled="!canJump" :title="canJump ? '' : t('rp.need')" data-testid="ql-jump" @click="openJump(x)">{{ t('qu.act.jump') }}</button>
                <button data-testid="ql-people" @click="openPeople(x)">{{ t('qu.act.people') }}</button>
                <button v-if="x.token" @click="printSlip(x); menu = 0">{{ t('qu.t.print') }}</button>
                <button class="bad" data-testid="ql-cancel" @click="cancel(x)">{{ t('qu.act.cancel') }}</button>
              </div>
            </div>
          </div>
        </template>
      </section>

      <aside class="ox-panel ql-tables" data-testid="ql-tables">
        <div class="ox-panel-h"><h2>{{ t('qu.free') }} <span class="n">{{ freeTables.length }}</span></h2></div>
        <div v-if="!freeTables.length" class="muted small">{{ t('qu.no_free') }}</div>
        <div v-for="tb in freeTables" :key="tb.id" class="tcard free" :data-table="tb.name" data-testid="ql-free">
          <div class="th"><b>{{ tb.name }}</b><small>{{ t('qu.seats', { n: tb.seats }) }}<template v-if="tb.area"> · {{ tb.area }}</template></small>
            <span v-if="tb.state === 'held'" class="ox-pill warn">{{ t('qu.held', { no: tb.held_by ?? '' }) }}</span></div>
          <div v-if="tb.state === 'held' && holder(tb)" class="sug">
            <span class="no">{{ holder(tb)!.number }}</span><span class="why">{{ t('qu.people', { n: holder(tb)!.people }) }} · {{ t('qu.st.called', { n: holder(tb)!.calls }) }}</span>
            <button class="btn small ok" @click="openSeat(holder(tb)!)">{{ t('qu.act.seat') }}</button>
          </div>
          <div v-else-if="suggestionFor(tb)" class="sug" :class="{ up: suggestionFor(tb)!.upgrade }" data-testid="ql-sug">
            <span class="no">{{ suggestionFor(tb)!.ticket.number }}</span>
            <span class="why">{{ suggestionFor(tb)!.upgrade
              ? t('qu.sug_up', { no: suggestionFor(tb)!.ticket.number, m: waited(suggestionFor(tb)!.ticket) })
              : t('qu.sug', { no: suggestionFor(tb)!.ticket.number, n: suggestionFor(tb)!.ticket.people, m: waited(suggestionFor(tb)!.ticket) }) }}</span>
            <button class="btn small primary" :disabled="busy === suggestionFor(tb)!.ticket.id" data-testid="ql-call-to" @click="call(suggestionFor(tb)!.ticket, tb.id)">{{ t('qu.call_to') }}</button>
          </div>
        </div>
        <template v-if="soonTables.length">
          <div class="ox-panel-h soonh"><h2>{{ t('qu.soon') }} <span class="n">{{ soonTables.length }}</span></h2></div>
          <div v-for="tb in soonTables" :key="tb.id" class="tcard soon">
            <div class="th"><b>{{ tb.name }}</b><small>{{ t('qu.seats', { n: tb.seats }) }}</small>
              <span class="ox-pill plain">{{ t('qu.dining', { n: minsSince(tb.opened_at, now) }) }}</span></div>
          </div>
        </template>
      </aside>
    </div>

    <!-- take a number -->
    <div v-if="takeOpen" class="sheet-mask" @click.self="takeOpen = false">
      <div class="sheet center ox-sheet" data-testid="qt-sheet">
        <div class="sheet-head"><h2>{{ taken ? t('qu.t.done') : t('qu.t.title') }}</h2></div>
        <div v-if="!taken" class="sheet-body">
          <div class="lab2">{{ t('qu.t.people') }}</div>
          <div class="pgrid">
            <button v-for="n in 12" :key="n" :class="{ on: tk.people === n && !tk.more }" :data-testid="'qt-p' + n" @click="tk.people = n; tk.more = false">{{ n }}</button>
          </div>
          <div class="morep"><button class="btn small" :class="{ primary: tk.more }" @click="tk.more = true; if (tk.people <= 12) tk.people = 13">{{ t('qu.t.more') }}</button>
            <input v-if="tk.more" v-model.number="tk.people" type="number" min="13" max="99" /></div>
          <div v-if="tk.people" class="grpbar" data-testid="qt-group">
            <template v-if="takeGroup"><b>{{ takeGroup.letter }}</b> {{ groupName(takeGroup) }} · {{ t('qu.t.goes', { g: takeGroup.letter, n: countIn(takeGroup.id) }) }}</template>
            <template v-else>{{ t('qu.err.QUEUE_NO_GROUP') }}</template>
          </div>
          <div class="lab2">{{ t('qu.t.phone') }} <small>{{ t('qu.t.phone_hint') }}</small></div>
          <input v-model="tk.phone" type="tel" inputmode="tel" data-testid="qt-phone" />
          <div class="lab2">{{ t('qu.t.tags') }}</div>
          <div class="ox-chips">
            <button v-for="g in TAGS" :key="g" :class="{ on: tk.tags.includes(g) }" @click="toggleTag(g)">{{ tagText(g) }}</button>
            <input v-model="tk.custom" class="cust" maxlength="20" :placeholder="t('qu.t.custom')" />
          </div>
          <template v-if="canJump">
            <label class="lab2 chk"><input v-model="tk.priority" type="checkbox" data-testid="qt-priority" /> {{ t('qu.t.priority') }}</label>
            <div v-if="tk.priority" class="ox-chips">
              <button v-for="r in [...REASONS, 'other']" :key="r" :class="{ on: tk.reason === r }" @click="tk.reason = r">{{ t('qu.r.' + r) }}</button>
              <input v-if="tk.reason === 'other'" v-model="tk.other" class="cust" maxlength="60" />
            </div>
          </template>
          <div v-if="error" class="banner bad">{{ error }}</div>
        </div>
        <div v-else class="sheet-body taken" data-testid="qt-taken">
          <b class="bign">{{ taken.number }}</b>
          <div>{{ groupName(groupOf(taken.group_id) ?? { preset: '', label: '', label_en: '', letter: taken.letter }) }} · {{ t('qu.people', { n: taken.people }) }}</div>
          <div class="muted">{{ t('qu.t.ahead', { n: Math.max(0, taken.position - 1), m: taken.est_minutes }) }}</div>
        </div>
        <div class="sheet-foot row">
          <template v-if="!taken">
            <button class="btn" @click="takeOpen = false">{{ t('qu.act.cancel') }}</button>
            <span class="grow"></span>
            <button class="btn" :disabled="!tk.people || !takeGroup || busy === -1 || (tk.priority && !reasonText(tk.reason, tk.other))" data-testid="qt-ok-np" @click="giveNumber(false)">{{ t('qu.t.ok_np') }}</button>
            <button class="btn primary" :disabled="!tk.people || !takeGroup || busy === -1 || (tk.priority && !reasonText(tk.reason, tk.other))" data-testid="qt-ok" @click="giveNumber(true)">{{ t('qu.t.ok') }}</button>
          </template>
          <template v-else>
            <button class="btn" @click="printSlip(taken)">{{ t('qu.t.print') }}</button>
            <span class="grow"></span>
            <button class="btn" @click="openTake">{{ t('qu.take') }}</button>
            <button class="btn primary" data-testid="qt-close" @click="takeOpen = false">{{ t('qu.done') }}</button>
          </template>
        </div>
      </div>
    </div>

    <!-- seat -->
    <div v-if="seatFor" class="sheet-mask" @click.self="seatFor = null">
      <div class="sheet center ox-sheet" data-testid="qs-sheet">
        <div class="sheet-head"><h2>{{ t('qu.seat.title', { no: seatFor.number }) }} · {{ t('qu.people', { n: seatFor.people }) }}</h2><p class="muted small">{{ t('qu.seat.hint') }}</p></div>
        <div v-if="!seated" class="sheet-body">
          <div v-if="!seatTables.length" class="muted">{{ t('qu.seat.none') }}</div>
          <div class="tmini">
            <button v-for="s in seatTables" :key="s.tb.id" :class="{ rec: s.rec, hold: s.otherHold }" :disabled="busy === seatFor.id" :data-table="s.tb.name" data-testid="qs-table" @click="seat(s.tb, s.otherHold)">
              <i v-if="s.rec">{{ t('qu.seat.rec') }}</i>{{ s.tb.name }}
              <small>{{ t('qu.seats', { n: s.tb.seats }) }}</small>
              <small v-if="s.otherHold" class="warnt">{{ t('qu.held', { no: s.tb.held_by ?? '' }) }}</small>
              <small v-else-if="s.tb.seats < seatFor.people" class="warnt">{{ t('qu.seat.small', { n: s.tb.seats }) }}</small>
            </button>
          </div>
          <div v-if="error" class="banner bad">{{ error }}</div>
        </div>
        <div v-else class="sheet-body taken" data-testid="qs-done"><b class="bign">{{ seated.no }}</b><div>{{ t('qu.seat.done', { no: seated.no, t: seated.table }) }}</div></div>
        <div class="sheet-foot row">
          <button class="btn" @click="seatFor = null">{{ seated ? t('qu.done') : t('qu.act.cancel') }}</button>
          <span class="grow"></span>
          <router-link v-if="seated" class="btn primary" :to="`/merchant/order/dine?table=${seated.tableId}&view=order`" data-testid="qs-order">{{ t('qu.seat.order') }}</router-link>
        </div>
      </div>
    </div>

    <!-- move up -->
    <div v-if="jumpFor" class="sheet-mask" @click.self="jumpFor = null">
      <div class="sheet center ox-sheet" data-testid="qj-sheet">
        <div class="sheet-head"><h2>{{ t('qu.j.title', { no: jumpFor.number }) }}</h2><p class="muted small">{{ t('qu.j.now', { n: jumpFor.position }) }}</p></div>
        <div class="sheet-body">
          <div class="ox-chips">
            <button :class="{ on: jp.first }" @click="jp.first = true">{{ t('qu.j.first') }}</button>
            <button :class="{ on: !jp.first }" @click="jp.first = false">{{ t('qu.j.pos') }}</button>
            <input v-if="!jp.first" v-model.number="jp.pos" type="number" min="1" class="cust" />
          </div>
          <div class="lab2">{{ t('qu.r.title') }}</div>
          <div class="ox-chips">
            <button v-for="r in [...REASONS, 'other']" :key="r" :class="{ on: jp.reason === r }" :data-testid="'qj-r-' + r" @click="jp.reason = r">{{ t('qu.r.' + r) }}</button>
            <input v-if="jp.reason === 'other'" v-model="jp.other" class="cust" maxlength="60" />
          </div>
          <div v-if="error" class="banner bad">{{ error }}</div>
        </div>
        <div class="sheet-foot row"><button class="btn" @click="jumpFor = null">{{ t('qu.act.cancel') }}</button><span class="grow"></span>
          <button class="btn primary" :disabled="!reasonText(jp.reason, jp.other) || busy === jumpFor.id" data-testid="qj-ok" @click="jump">{{ t('qu.act.jump') }}</button></div>
      </div>
    </div>

    <!-- party size -->
    <div v-if="peopleFor" class="sheet-mask" @click.self="peopleFor = null">
      <div class="sheet center ox-sheet">
        <div class="sheet-head"><h2>{{ t('qu.p.title', { no: peopleFor.number }) }}</h2><p class="muted small">{{ t('qu.p.hint') }}</p></div>
        <div class="sheet-body">
          <div class="pgrid"><button v-for="n in 12" :key="n" :class="{ on: pp === n }" @click="pp = n">{{ n }}</button></div>
          <div class="morep"><input v-model.number="pp" type="number" min="1" max="99" /></div>
          <div v-if="error" class="banner bad">{{ error }}</div>
        </div>
        <div class="sheet-foot row"><button class="btn" @click="peopleFor = null">{{ t('qu.act.cancel') }}</button><span class="grow"></span>
          <button class="btn primary" :disabled="!pp || pp === peopleFor.people" @click="savePeople">{{ t('qu.set.save') }}</button></div>
      </div>
    </div>

    <!-- printed only -->
    <div v-if="slip" class="qslip">
      <div class="s">{{ store }}</div>
      <div class="n">{{ slip.t.number }}</div>
      <div class="s">{{ groupName(groupOf(slip.t.group_id) ?? { preset: '', label: '', label_en: '', letter: slip.t.letter }) }} · {{ t('qu.people', { n: slip.t.people }) }}</div>
      <div class="s">{{ t('qu.t.ahead', { n: Math.max(0, slip.t.position - 1), m: slip.t.est_minutes }) }}</div>
      <div class="m">{{ t('qu.slip.at', { t: slipTime(slip.t.created_at) }) }}</div>
      <hr />
      <img v-if="slip.qr" :src="slip.qr" alt="" />
      <div class="m">{{ t('qu.slip.scan') }}</div>
      <div class="m">{{ t('qu.slip.skip', { n: desk.settings.requeue_after }) }}</div>
    </div>
  </div>
</template>

<style scoped>
.ql { display: grid; gap: 12px; }
.ql-body { display: grid; grid-template-columns: 200px minmax(0, 1fr) 340px; gap: 12px; align-items: start; }
.ql-body > * { min-width: 0; }
.ox-cat .gl { width: 24px; height: 24px; border-radius: 7px; background: #f4ebde; color: var(--brand); font-weight: 900; display: grid; place-items: center; font-size: 13px; flex: none; }
.ox-cat .nm small { display: block; color: var(--muted); font-size: 12px; font-weight: 500; }
.grp-h { display: flex; align-items: center; gap: 8px; margin: 8px 2px 6px; font-weight: 800; color: #5b4636; font-size: 13.5px; }
.grp-h:first-child { margin-top: 0; }
.grp-h .l { width: 22px; height: 22px; border-radius: 6px; background: var(--brand); color: #fff; display: grid; place-items: center; font-size: 12px; }
.grp-h em { font-style: normal; color: var(--muted); font-weight: 500; }
.qrow { display: grid; grid-template-columns: 86px 64px 76px minmax(0, 1fr) auto; align-items: center; gap: 10px; padding: 9px 10px; border-radius: 10px; border: 1px solid #f0e6d7; margin-bottom: 7px; background: #fff; position: relative; }
.qrow.called { background: #fff8e6; border-color: #f1d58e; }
.qrow.late { background: #fdecea; border-color: #f2b8b5; }
.qrow.jump { box-shadow: inset 3px 0 0 #2556a8; }
.qrow .no { font-size: 22px; font-weight: 900; color: var(--brand); font-variant-numeric: tabular-nums; }
.qrow .pp { font-weight: 700; font-size: 15px; }
.qrow .wt { font-weight: 800; font-size: 13px; border-radius: 999px; padding: 2px 9px; background: #eeeae4; white-space: nowrap; }
.qrow .wt.y { background: #fef3c7; color: #a15c00; } .qrow .wt.r { background: #d92d20; color: #fff; }
.qmore { font-size: 18px; line-height: 1; padding-inline: 10px; }
.qrow .info { display: flex; gap: 6px; flex-wrap: wrap; align-items: center; color: var(--muted); font-size: 12.5px; min-width: 0; }
.src { font-size: 12px; border: 1px solid #e5d6c0; border-radius: 6px; padding: 0 6px; color: #6b5a4c; background: #fffaf2; }
.ops { display: flex; gap: 6px; align-items: center; position: relative; }
.qmenu { position: absolute; top: 100%; inset-inline-end: 0; z-index: 20; background: #fff; border: 1px solid var(--line); border-radius: 10px; box-shadow: 0 10px 30px rgba(0,0,0,.15); display: grid; min-width: 150px; padding: 4px; }
.qmenu button { border: 0; background: none; text-align: start; padding: 9px 12px; font: inherit; border-radius: 8px; cursor: pointer; color: var(--ink); }
.qmenu button:hover { background: #fbf4ea; } .qmenu button.bad { color: var(--bad); } .qmenu button:disabled { opacity: .45; cursor: default; }
.ql-tables { position: sticky; top: 12px; }
.soonh { margin-top: 14px; }
.tcard { border: 1px solid var(--line); border-radius: 12px; padding: 10px 12px; margin-bottom: 10px; background: #fff; }
.tcard.free { border-top: 4px solid #8cc9a2; }
.tcard.soon { border-top: 4px solid #f1d58e; background: #fffdf6; }
.tcard .th { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; } .tcard .th b { font-size: 18px; } .tcard .th small { color: var(--muted); }
.tcard .th .ox-pill { margin-inline-start: auto; }
.sug { margin-top: 8px; border-radius: 10px; background: #fbf4ea; padding: 8px 10px; display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.sug.up { background: #eef3ff; border: 1px dashed #9db5f0; }
.sug .no { font-size: 18px; font-weight: 900; color: var(--brand); }
.sug .why { font-size: 12.5px; color: var(--muted); flex: 1; min-width: 120px; line-height: 1.35; }
.lab2 { font-weight: 700; margin: 14px 0 8px; font-size: 14px; display: flex; align-items: center; gap: 8px; } .lab2 small { color: var(--muted); font-weight: 500; }
.lab2:first-child { margin-top: 0; }
.lab2.chk { cursor: pointer; }
.pgrid { display: grid; grid-template-columns: repeat(6, 1fr); gap: 8px; }
.pgrid button { border: 1.5px solid var(--line); border-radius: 12px; height: 50px; font-size: 20px; font-weight: 800; background: #fff; cursor: pointer; color: var(--ink); }
.pgrid button.on { background: var(--brand); color: #fff; border-color: var(--brand); }
.morep { display: flex; gap: 8px; margin-top: 8px; align-items: center; } .morep input { width: 100px; }
.grpbar { display: flex; align-items: center; gap: 8px; background: #fbf4ea; border-radius: 10px; padding: 8px 12px; margin-top: 10px; font-size: 13.5px; } .grpbar b { color: var(--brand); }
.cust { width: 140px; padding: 5px 10px; border-radius: 999px; }
.taken { text-align: center; display: grid; gap: 6px; }
.bign { font-size: 64px; font-weight: 900; color: var(--brand); letter-spacing: 3px; font-variant-numeric: tabular-nums; }
.tmini { display: grid; grid-template-columns: repeat(auto-fill, minmax(120px, 1fr)); gap: 10px; }
.tmini button { border: 1.5px solid var(--line); border-radius: 12px; padding: 12px 10px 10px; background: #fff; font-weight: 800; font-size: 17px; position: relative; cursor: pointer; color: var(--ink); text-align: start; font-family: inherit; }
.tmini small { display: block; font-weight: 500; color: var(--muted); font-size: 12px; }
.tmini small.warnt { color: #a2510a; }
.tmini .rec { border-color: var(--brand); background: #fbeae3; color: var(--brand); }
.tmini .rec i { position: absolute; top: -9px; inset-inline-end: 8px; font-style: normal; font-size: 11px; background: var(--brand); color: #fff; border-radius: 999px; padding: 0 7px; }
.tmini .hold { border-color: #e5b94c; background: #fff8e2; }
.qslip { display: none; }
@media (max-width: 1180px) { .ql-body { grid-template-columns: 180px minmax(0, 1fr); } .ql-tables { grid-column: 1 / -1; position: static; } }
@media (max-width: 860px) {
  .ql-body { grid-template-columns: minmax(0, 1fr); }
  .qrow { grid-template-columns: auto auto 1fr; }
  .qmore { font-size: 18px; line-height: 1; padding-inline: 10px; }
.qrow .info { grid-column: 1 / -1; }
  .qrow .ops { grid-column: 1 / -1; justify-content: flex-end; }
}
</style>

<style>
/* printing a number slip prints only the slip (58/80 mm thermal paper) */
@media print {
  body.print-slip * { visibility: hidden !important; }
  body.print-slip .qslip, body.print-slip .qslip * { visibility: visible !important; }
  body.print-slip .qslip { display: block !important; position: fixed; left: 0; top: 0; width: 72mm; padding: 4mm; text-align: center; font-family: monospace; color: #000; }
  .qslip .n { font-size: 40pt; font-weight: 900; letter-spacing: 2pt; margin: 2mm 0; }
  .qslip .s { font-size: 11pt; } .qslip .m { font-size: 9pt; line-height: 1.5; }
  .qslip hr { border: 0; border-top: 1px dashed #000; margin: 2mm 0; }
  .qslip img { width: 30mm; height: 30mm; }
}
</style>
