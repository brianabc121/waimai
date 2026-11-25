<script setup lang="ts">
// Attendance: who clocked on and off, by day and by month. A manager can
// correct, add or void a shift; a reason is required and the times as first
// recorded are kept.
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { ApiError, get, patch, post } from '../../api'
import { pick, t } from '../../i18n'
import { session } from '../../session'
import type { Post } from '../../types'
import ExportLink from './ExportLink.vue'
import StaffTabs from './StaffTabs.vue'
import { addDays, addMonths, clock, dayOf, fromInput, hm, hoursInto, metres, monthEnd, monthStart, toInput, weekday } from './time'
import '../work/strings'
import './strings'

interface Shift {
  id: number; user_id: number; day: string; started_at: string; ended_at: string | null; minutes: number
  start_source: string; end_reason: string; start_distance_m: number | null; end_distance_m: number | null
  orig_started_at: string | null; orig_ended_at: string | null; edited: boolean; edited_by: string; edit_reason: string
  voided: boolean; last_action_at: string | null
}
interface Person { id: number; display_name: string; username: string; role: string; active: boolean; gone: boolean; posts: Post[] }
interface Attendance { timezone: string; now: string; away_meters: number; staff: Person[]; shifts: Shift[] }

const bid = session.branchId!
const tz = ref('Asia/Manila')
const mode = ref<'day' | 'month'>('day')
const day = ref('')
const data = ref<Attendance | null>(null)
const error = ref('')
const notice = ref('')
const today = computed(() => dayOf(Date.now(), tz.value))

const range = computed(() => !day.value ? ['', ''] : mode.value === 'day' ? [day.value, day.value] : [monthStart(day.value), monthEnd(day.value)])
async function load() {
  if (!day.value) day.value = today.value
  const [from, to] = range.value
  try {
    const d = await get<Attendance>(`/merchant/branches/${bid}/attendance?from=${from}&to=${to}`)
    if (d.timezone !== tz.value) { tz.value = d.timezone }
    data.value = d; error.value = ''
  } catch (e) { error.value = (e as Error).message }
}
onMounted(load)
watch([mode, day], load)
// People at work keep counting: refresh every minute.
const timer = setInterval(load, 60000)
onBeforeUnmount(() => clearInterval(timer))

const step = (n: number) => { day.value = mode.value === 'day' ? addDays(day.value, n) : addMonths(day.value, n) }
const wd = (d: string) => t('weekday.' + weekday(d))
const title = computed(() => mode.value === 'day' ? `${day.value} ${wd(day.value)}` : day.value.slice(0, 7))

const awayM = computed(() => data.value?.away_meters ?? 200)
const forgot = (s: Shift) => !s.voided && !s.edited && s.end_reason === 'auto'
const away = (s: Shift) => !s.voided && !s.edited && s.start_distance_m !== null && s.start_distance_m > awayM.value
const todo = (s: Shift) => forgot(s) || away(s)
const live = (list: Shift[]) => list.filter(s => !s.voided)
const minutes = (list: Shift[]) => live(list).reduce((n, s) => n + s.minutes, 0)
const byUser = computed(() => {
  const m = new Map<number, Shift[]>()
  for (const s of data.value?.shifts ?? []) (m.get(s.user_id) ?? m.set(s.user_id, []).get(s.user_id)!).push(s)
  return m
})
const postName = (p: Post) => p.kind === 'station' ? pick(p.station_name ?? '', p.station_name_en ?? '') : p.kind === 'packing' ? t('wk.packing') : t('rider')
const postsText = (p: Person) => p.posts.length ? p.posts.map(postName).join(' · ') : p.role !== 'staff' ? t('role.' + p.role) : t('st.no_posts')
const colors = ['#d9480f', '#1d4ed8', '#18794e', '#7c3aed', '#b45309', '#0e7490', '#be185d']

// ---- by day ----
const rows = computed(() => {
  const all = (data.value?.staff ?? []).map(p => ({ p, shifts: byUser.value.get(p.id) ?? [] }))
  return [...all.filter(r => r.shifts.length), ...all.filter(r => !r.shifts.length && r.p.active && !r.p.gone)]
})
const worked = computed(() => rows.value.filter(r => live(r.shifts).length))
const absent = computed(() => rows.value.filter(r => !live(r.shifts).length).map(r => r.p.display_name))
const onNow = computed(() => (data.value?.shifts ?? []).filter(s => !s.voided && !s.ended_at).length)
const activeCount = computed(() => (data.value?.staff ?? []).filter(p => p.active && !p.gone).length)
const allShifts = computed(() => data.value?.shifts ?? [])
const nowH = computed(() => day.value === today.value && data.value ? hoursInto(data.value.now, day.value, tz.value) : null)
const pct = (h: number) => Math.max(0, Math.min(100, h / 24 * 100))
function bar(s: Shift) {
  const a = hoursInto(s.started_at, day.value, tz.value), b = hoursInto(s.ended_at ?? data.value!.now, day.value, tz.value)
  return { left: pct(a) + '%', width: Math.max(0.6, pct(b) - pct(a)) + '%' }
}
// A time on another calendar day than the one shown says so.
function at(iso: string, base: string) {
  const d = dayOf(iso, tz.value)
  return (d > base ? t('att.next_day') + ' ' : d < base ? t('att.prev_day') + ' ' : '') + clock(iso, tz.value)
}
const where = (s: Shift) => s.start_distance_m === null ? t('att.no_pos') : s.start_distance_m > awayM.value ? t('att.away', { d: metres(s.start_distance_m) }) : t('att.in_store')

// ---- by month ----
const days = computed(() => { const out: string[] = []; for (let d = monthStart(day.value); d <= monthEnd(day.value); d = addDays(d, 1)) out.push(d); return out })
function cell(p: Person, d: string) {
  const list = live((byUser.value.get(p.id) ?? []).filter(s => s.day === d))
  if (!list.length) return { text: '', cls: 'none' }
  const h = Math.round(minutes(list) / 6) / 10
  return { text: String(h), cls: list.some(todo) ? 'todo' : list.some(s => s.edited) ? 'edited' : h > 10 ? 'long' : 'ok' }
}
const monthRows = computed(() => (data.value?.staff ?? []).filter(p => (p.active && !p.gone) || byUser.value.has(p.id)).map(p => {
  const list = live(byUser.value.get(p.id) ?? [])
  return { p, days: new Set(list.map(s => s.day)).size, hours: (minutes(list) / 60).toFixed(1) }
}))
function openDay(d: string) { day.value = d; mode.value = 'day' }

// ---- correct, add, void ----
const f = reactive({ open: false, shift: null as Shift | null, user: 0, start: '', end: '', reason: '', error: '', busy: false })
function edit(s: Shift) {
  Object.assign(f, { open: true, shift: s, user: s.user_id, start: toInput(s.started_at, tz.value), end: s.ended_at ? toInput(s.ended_at, tz.value) : '', reason: '', error: '' })
  notice.value = ''
}
function add(p: Person | null) {
  Object.assign(f, { open: true, shift: null, user: p?.id ?? 0, start: `${day.value}T09:00`, end: `${day.value}T18:00`, reason: '', error: '' })
  notice.value = ''
}
const person = (id: number) => data.value?.staff.find(p => p.id === id)
const span = computed(() => {
  const a = fromInput(f.start, tz.value), b = f.end ? fromInput(f.end, tz.value) : null
  return a && b ? (Date.parse(b) - Date.parse(a)) / 60000 : null
})
const errText = (e: unknown) => {
  const code = e instanceof ApiError ? e.code : ''
  if (code === 'VALIDATION') return t('att.e.times')
  const k = 'att.e.' + code
  return t(k) !== k ? t(k) : (e as Error).message
}
async function send(fn: () => Promise<unknown>) {
  if (!f.reason.trim()) { f.error = t('att.e.reason'); return }
  f.error = ''; f.busy = true
  try { await fn(); f.open = false; notice.value = t('att.saved'); await load() } catch (e) { f.error = errText(e) }
  f.busy = false
}
function save() {
  const body: Record<string, unknown> = { reason: f.reason, started_at: fromInput(f.start, tz.value) }
  if (f.end) body.ended_at = fromInput(f.end, tz.value)
  return send(() => f.shift ? patch(`/merchant/branches/${bid}/attendance/${f.shift.id}`, body)
    : post(`/merchant/branches/${bid}/attendance`, { ...body, user_id: f.user }))
}
function voidShift() {
  if (!f.reason.trim()) { f.error = t('att.e.reason'); return }
  if (confirm(t('att.f.void_confirm'))) void send(() => patch(`/merchant/branches/${bid}/attendance/${f.shift!.id}`, { void: true, reason: f.reason }))
}
</script>

<template>
  <div class="ox warm"><div class="page att">
    <StaffTabs />
    <div class="row wrap" style="margin-bottom:14px">
      <span class="datepick"><button data-act="prev" @click="step(-1)"><span class="rtl-flip">‹</span></button><b data-testid="att-title">{{ title }}</b><button data-act="next" @click="step(1)"><span class="rtl-flip">›</span></button></span>
      <button class="btn" @click="day = today">{{ t('att.today') }}</button>
      <span class="pills"><button :class="{ on: mode === 'day' }" data-mode="day" @click="mode = 'day'">{{ t('att.by_day') }}</button>
        <button :class="{ on: mode === 'month' }" data-mode="month" @click="mode = 'month'">{{ t('att.by_month') }}</button></span>
      <span class="grow"></span>
      <ExportLink list="attendance" :from="range[0]" :to="range[1]" />
      <button class="btn" data-testid="att-add" @click="add(null)">＋ {{ t('att.add') }}</button>
    </div>
    <div v-if="error" class="banner bad" style="margin-bottom:12px">{{ error }}</div>
    <div v-if="notice" class="banner ok" style="margin-bottom:12px" data-testid="att-notice">{{ notice }}</div>

    <template v-if="data && mode === 'day'">
      <div class="kpis">
        <div class="kpi" data-kpi="worked"><div class="l">{{ t('att.k.worked') }}</div><div class="v">{{ worked.length }}<small>{{ t('att.k.worked_of', { n: activeCount }) }}</small></div>
          <div class="d">{{ absent.length ? t('att.k.absent', { names: absent.slice(0, 4).join('、') + (absent.length > 4 ? '…' : '') }) : t('att.k.all_in') }}</div></div>
        <div class="kpi" data-kpi="on"><div class="l">{{ t('att.k.on_now') }}</div><div class="v">{{ onNow }}<small>{{ t('att.k.people') }}</small></div></div>
        <div class="kpi" data-kpi="hours"><div class="l">{{ t('att.k.hours') }}</div><div class="v">{{ (minutes(allShifts) / 60).toFixed(1) }}<small>{{ t('att.k.hours_unit') }}</small></div>
          <div v-if="onNow" class="d">{{ t('att.k.still') }}</div></div>
        <div class="kpi" :class="{ warn: allShifts.some(todo) }" data-kpi="todo"><div class="l">{{ t('att.k.todo') }}</div><div class="v">{{ allShifts.filter(todo).length }}<small>{{ t('att.k.items') }}</small></div>
          <div class="d">{{ allShifts.some(todo) ? t('att.k.todo_detail', { a: allShifts.filter(forgot).length, b: allShifts.filter(away).length }) : t('att.k.todo_none') }}</div></div>
      </div>

      <div class="card" style="padding:0;overflow-x:auto">
        <table class="data atttable">
          <thead><tr><th>{{ t('att.col.staff') }}</th><th>{{ t('att.col.on') }}</th><th>{{ t('att.col.off') }}</th><th class="num">{{ t('att.col.hours') }}</th>
            <th style="min-width:240px"><div class="axis"><span>0</span><span>6</span><span>12</span><span>18</span><span>24</span></div></th>
            <th>{{ t('att.col.where') }}</th><th>{{ t('att.col.status') }}</th><th></th></tr></thead>
          <tbody>
            <tr v-for="r in rows" :key="r.p.id" :data-att="r.p.username" :class="{ off: !r.shifts.length }">
              <td><span class="av" :style="{ background: colors[r.p.id % colors.length] }">{{ r.p.display_name.slice(0, 1) }}</span>
                <span class="who"><b>{{ r.p.display_name }}</b><span class="sub">{{ postsText(r.p) }}</span></span></td>
              <template v-if="r.shifts.length">
                <td><div v-for="s in r.shifts" :key="s.id" class="ln" :class="{ void: s.voided }" data-col="on">{{ at(s.started_at, day) }}</div></td>
                <td><div v-for="s in r.shifts" :key="s.id" class="ln" :class="{ void: s.voided }" data-col="off">
                  <span v-if="!s.ended_at" class="tag ok">{{ t('att.at_work') }}</span><template v-else>{{ at(s.ended_at, day) }}</template></div></td>
                <td class="num" data-col="hours"><b :class="{ warnt: r.shifts.some(forgot) }">{{ hm(minutes(r.shifts)) }}</b>
                  <div v-if="live(r.shifts).length > 1" class="sub">{{ t('att.segments', { n: live(r.shifts).length }) }}</div></td>
                <td><div class="tl"><i v-for="s in live(r.shifts)" :key="s.id" :class="{ now: !s.ended_at, auto: forgot(s) }" :style="bar(s)"></i>
                  <b v-if="nowH !== null" :style="{ left: pct(nowH) + '%' }"></b></div></td>
                <td><div v-for="s in r.shifts" :key="s.id" class="ln" :class="{ warnt: away(s), void: s.voided }">{{ where(s) }}</div></td>
                <td data-col="status"><div v-for="s in r.shifts" :key="s.id" class="ln">
                  <span v-if="s.voided" class="tag">{{ t('att.voided') }}</span>
                  <template v-else>
                    <span v-if="forgot(s)" class="tag warn">{{ t('att.forgot') }}</span>
                    <span v-if="away(s)" class="tag warn">{{ t('att.away_flag') }}</span>
                    <span v-if="!todo(s) && !s.edited" class="okt">{{ t('att.ok') }}</span>
                    <span v-if="s.edited" class="tag info" :title="s.edit_reason">{{ s.start_source === 'manager' ? t('att.by_manager') : t('att.edited') }}</span>
                    <span v-if="s.start_source === 'auto'" class="sub"> {{ t('att.auto_start') }}</span>
                    <span v-if="s.end_reason === 'logout'" class="sub"> {{ t('att.logout') }}</span>
                    <span v-if="forgot(s) && s.last_action_at" class="sub"> {{ t('att.last_action', { t: at(s.last_action_at, day) }) }}</span>
                  </template></div></td>
                <td class="right"><div v-for="s in r.shifts" :key="s.id" class="ln"><button v-if="!s.voided" class="btn small" :class="{ primary: todo(s) }" data-act="edit" @click="edit(s)">{{ t('att.edit') }}</button></div></td>
              </template>
              <template v-else>
                <td class="muted">{{ t('att.no_record') }}</td><td class="muted">—</td><td class="num muted">—</td>
                <td><div class="tl"><b v-if="nowH !== null" :style="{ left: pct(nowH) + '%' }"></b></div></td>
                <td class="muted">—</td><td class="muted">{{ t('att.none_today') }}</td>
                <td class="right"><button class="btn small" data-act="fill" @click="add(r.p)">{{ t('att.fill') }}</button></td>
              </template>
            </tr>
          </tbody>
        </table>
      </div>
      <div class="legend"><span><i style="background:#7fb89b"></i>{{ t('att.legend.shift') }}</span><span><i class="hatch"></i>{{ t('att.legend.auto') }}</span>
        <span v-if="nowH !== null">{{ t('att.legend.now') }} {{ clock(data.now, tz) }}</span><span>{{ t('att.tz', { tz }) }}</span></div>
    </template>

    <template v-if="data && mode === 'month'">
      <div class="card" style="padding:0;overflow-x:auto">
        <table class="month" data-testid="att-month">
          <thead><tr><th class="name">{{ t('att.col.staff') }}</th>
            <th v-for="d in days" :key="d" :class="{ we: weekday(d) === 0 || weekday(d) === 6 }">{{ +d.slice(8) }}</th>
            <th class="sum">{{ t('att.m.days') }}</th><th class="sum">{{ t('att.m.hours') }}</th></tr></thead>
          <tbody>
            <tr v-for="r in monthRows" :key="r.p.id" :data-att="r.p.username">
              <td class="name">{{ r.p.display_name }}</td>
              <td v-for="d in days" :key="d" :class="cell(r.p, d).cls" :data-day="d" @click="openDay(d)">{{ cell(r.p, d).text }}</td>
              <td class="sum" data-col="days">{{ r.days }}</td><td class="sum" data-col="hours">{{ r.hours }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <div class="legend"><span><i style="background:#e6f6ed"></i>{{ t('att.m.legend.cell') }}</span><span><i style="background:#c9ecd8"></i>{{ t('att.m.legend.long') }}</span>
        <span><i style="background:var(--warn-soft)"></i>{{ t('att.m.legend.todo') }}</span><span><i style="background:var(--info-soft)"></i>{{ t('att.m.legend.edited') }}</span></div>
    </template>

    <template v-if="f.open">
      <div class="drawer-mask" @click="f.open = false"></div>
      <form class="drawer" data-testid="att-form" @submit.prevent="save">
        <div class="row between"><h2 style="font-size:18px">{{ f.shift ? t('att.f.edit_title', { name: person(f.user)?.display_name ?? '' }) : t('att.f.add_title') }}</h2>
          <button type="button" class="btn link" @click="f.open = false">✕</button></div>
        <div v-if="f.error" class="banner bad" data-testid="att-error">{{ f.error }}</div>
        <label v-if="!f.shift" class="field"><span>{{ t('att.f.who') }}</span>
          <select v-model.number="f.user" name="user" required>
            <option :value="0" disabled>{{ t('att.f.pick') }}</option>
            <option v-for="p in data?.staff.filter(x => !x.gone)" :key="p.id" :value="p.id">{{ p.display_name }}</option>
          </select></label>
        <div v-if="f.shift" class="old">
          <b>{{ f.shift.edited && f.shift.start_source !== 'manager' ? t('att.f.original') : t('att.f.current') }}</b>
          <span>{{ t('att.col.on') }} {{ at(f.shift.orig_started_at ?? f.shift.started_at, f.shift.day) }}
            <template v-if="f.shift.start_source === 'auto'"> · {{ t('att.auto_start') }}</template>
            <template v-if="f.shift.start_distance_m !== null"> · {{ where(f.shift) }}</template></span>
          <span v-if="f.shift.edited ? f.shift.orig_ended_at : f.shift.ended_at">{{ t('att.col.off') }} {{ at((f.shift.edited ? f.shift.orig_ended_at : f.shift.ended_at)!, f.shift.day) }}
            <template v-if="f.shift.end_reason === 'auto'"> · <span class="warnt">{{ t('att.auto_end') }}</span></template></span>
          <span v-else class="muted">{{ t('att.f.end_open') }}</span>
          <span v-if="f.shift.last_action_at" class="muted">{{ t('att.last_action', { t: at(f.shift.last_action_at, f.shift.day) }) }}</span>
          <span v-if="f.shift.edited" class="muted">{{ t('att.f.reason_was', { who: f.shift.edited_by, why: f.shift.edit_reason }) }}</span>
        </div>
        <div class="two">
          <label class="field"><span>{{ t('att.f.start') }}</span><input v-model="f.start" name="start" type="datetime-local" required /></label>
          <label class="field"><span>{{ t('att.f.end') }}</span><input v-model="f.end" name="end" type="datetime-local" :required="!f.shift || !!f.shift.ended_at" /></label>
        </div>
        <div v-if="span !== null && span > 0" class="banner info" data-testid="att-result">{{ f.shift && f.shift.ended_at ? t('att.f.result', { a: hm(span), b: hm(f.shift.minutes) }) : t('att.f.result_new', { a: hm(span) }) }}</div>
        <label class="field"><span>{{ t('att.f.reason') }}</span><textarea v-model="f.reason" name="reason" rows="3"></textarea></label>
        <div class="muted small">{{ t('att.tz', { tz }) }}</div>
        <div class="foot">
          <button v-if="f.shift" type="button" class="btn link" style="color:var(--bad)" :disabled="f.busy" data-testid="att-void" @click="voidShift">{{ t('att.f.void') }}</button>
          <span class="grow"></span>
          <button type="button" class="btn" @click="f.open = false">{{ t('cancel') }}</button>
          <button class="btn primary" :disabled="f.busy" data-testid="att-save">{{ t('att.f.save') }}</button>
        </div>
      </form>
    </template>
  </div>
  </div>
</template>

<style scoped>
.datepick { display: inline-flex; align-items: center; background: #fff; border: 1px solid var(--line); border-radius: 8px; overflow: hidden; }
.datepick b { padding: 7px 12px; border-inline-start: 1px solid var(--line); border-inline-end: 1px solid var(--line); min-width: 128px; text-align: center; }
.datepick button { border: 0; background: none; padding: 7px 12px; cursor: pointer; font-size: 15px; }
.pills { display: inline-flex; background: #eceef0; border-radius: 8px; padding: 3px; }
.pills button { border: 0; background: none; padding: 5px 13px; border-radius: 6px; font-weight: 600; font-size: 13px; color: var(--muted); cursor: pointer; }
.pills button.on { background: #fff; box-shadow: 0 1px 2px rgba(0,0,0,.12); color: var(--brand-dark); }
.kpis { display: grid; grid-template-columns: repeat(auto-fit, minmax(210px, 1fr)); gap: 12px; margin-bottom: 14px; }
.kpi { background: #fff; border: 1px solid var(--line); border-radius: 12px; padding: 12px 14px; }
.kpi .l, .kpi .d { font-size: 12.5px; color: var(--muted); } .kpi .v { font-size: 24px; font-weight: 700; font-variant-numeric: tabular-nums; }
.kpi .v small { font-size: 13px; font-weight: 500; color: var(--muted); margin-inline-start: 4px; }
.kpi.warn { border-color: #f0d9a8; background: #fffaf0; } .kpi.warn .v { color: var(--warn); }
.atttable td { white-space: nowrap; vertical-align: top; } .atttable tr.off td { color: var(--muted); vertical-align: middle; }
.atttable th.num, .atttable td.num { text-align: end; font-variant-numeric: tabular-nums; }
.av { width: 32px; height: 32px; border-radius: 50%; display: inline-flex; align-items: center; justify-content: center; color: #fff; font-weight: 700; margin-inline-end: 10px; vertical-align: top; }
.who { display: inline-grid; } .sub { font-size: 12px; color: var(--muted); font-weight: 400; }
.ln { min-height: 26px; display: flex; align-items: center; gap: 4px; font-variant-numeric: tabular-nums; } .ln.void { text-decoration: line-through; color: var(--muted); }
td.right .ln { justify-content: flex-end; }
.warnt { color: var(--warn); font-weight: 600; } .okt { color: var(--ok); font-weight: 600; }
.axis { display: flex; justify-content: space-between; font-size: 11px; font-weight: 400; }
.tl { position: relative; height: 16px; background: #f1f2f4; border-radius: 4px; margin-top: 5px; }
.tl i { position: absolute; top: 0; bottom: 0; border-radius: 4px; background: #7fb89b; } .tl i.now { background: var(--ok); }
.tl i.auto, .legend i.hatch { background: repeating-linear-gradient(45deg, #e9b9b3, #e9b9b3 4px, #f6dcd9 4px, #f6dcd9 8px); }
.tl b { position: absolute; top: -3px; bottom: -3px; width: 2px; background: var(--ink); }
.legend { display: flex; gap: 16px; flex-wrap: wrap; font-size: 12.5px; color: var(--muted); margin-top: 10px; }
.legend i { display: inline-block; width: 12px; height: 12px; border-radius: 3px; margin-inline-end: 5px; vertical-align: -1px; }
table.month { border-collapse: collapse; background: #fff; width: 100%; font-size: 12px; }
table.month th { background: #fafafa; color: var(--muted); font-weight: 600; padding: 6px 2px; border-bottom: 1px solid var(--line); text-align: center; min-width: 30px; }
table.month th.we { color: var(--brand-dark); } table.month th.name, table.month td.name { text-align: start; padding: 0 10px; white-space: nowrap; }
table.month td { border-bottom: 1px solid var(--line); border-inline-start: 1px solid #f1f2f4; text-align: center; height: 34px; font-variant-numeric: tabular-nums; cursor: pointer; }
table.month td.name { font-size: 13.5px; font-weight: 600; border-inline-start: 0; cursor: default; }
table.month .sum { font-weight: 700; padding: 0 8px; background: #fafafa; cursor: default; white-space: nowrap; }
table.month td.ok { background: #e6f6ed; color: #14603f; } table.month td.long { background: #c9ecd8; color: #0f4e33; }
table.month td.todo { background: var(--warn-soft); color: var(--warn); font-weight: 700; } table.month td.edited { background: var(--info-soft); color: var(--info); }
.drawer-mask { position: fixed; inset: 0; background: rgba(0,0,0,.35); z-index: 40; }
.drawer { position: fixed; top: 0; inset-inline-end: 0; bottom: 0; width: min(440px, 100vw); background: #fff; z-index: 41; box-shadow: -8px 0 24px rgba(0,0,0,.15);
  padding: 18px 20px; display: flex; flex-direction: column; gap: 12px; overflow-y: auto; }
.drawer .old { background: #f6f6f4; border-radius: 10px; padding: 10px 12px; font-size: 13px; display: grid; gap: 3px; }
.drawer textarea { font: inherit; width: 100%; border: 1px solid var(--line); border-radius: 8px; padding: 7px 10px; resize: vertical; }
.two { display: grid; gap: 10px; } .two .field { margin-bottom: 0; }
.foot { margin-top: auto; display: flex; gap: 8px; align-items: center; padding-top: 6px; }
</style>
