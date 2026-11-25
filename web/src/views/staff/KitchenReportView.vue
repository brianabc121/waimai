<script setup lang="ts">
// In-store work summary: pick a person on the left, see how many dishes
// they made day by day and every dish on the right.
import { computed, onMounted, ref, watch } from 'vue'
import { get } from '../../api'
import { pick, t } from '../../i18n'
import { session } from '../../session'
import type { Post } from '../../types'
import ExportLink from './ExportLink.vue'
import StaffTabs from './StaffTabs.vue'
import { addDays, clock, dayOf, hm, monthStart, weekday } from './time'
import '../work/strings'
import './strings'
import './report.css'

interface Person { id: number; display_name: string; username: string; role: string; posts: Post[]; gone: boolean
  qty: number; dishes: number; orders: number; packed: number; shift_minutes: number }
interface Report { timezone: string; slow_minutes: number; total_qty: number; board_qty: number; people: Person[] }
interface Day { day: string; qty: number; dishes: number; packed: number; worked: boolean }
interface Count { id?: number; name: string; name_en?: string; qty: number }
interface Dish { id: number; ready_at: string; name: string; name_en: string; options: { name: string; name_en: string }[]; note: string; qty: number
  station_id: number | null; station_name: string; public_no: string; service_type: string; table_name: string; cancelled: boolean
  started_by: string; seconds: number | null; slow: boolean; via: string; ready_by: string; undo_count: number }
interface Detail { slow_minutes: number; qty: number; dishes: number; orders: number; packed: number; avg_seconds: number | null; timed: number
  slow_qty: number; slow_dishes: number; undo_count: number; shift_minutes: number; days: Day[]; top: Count[]; stations: Count[]
  items: Dish[]; item_total: number; page: number; page_size: number }

const bid = session.branchId!
const tz = ref('Asia/Manila')
const today = computed(() => dayOf(Date.now(), tz.value))
type Preset = 'today' | 'yesterday' | 'week' | 'month' | 'custom'
const preset = ref<Preset>('today')
const from = ref('')
const to = ref('')
const report = ref<Report | null>(null)
const detail = ref<Detail | null>(null)
const who = ref<number | null>(null)
const station = ref<number | null>(null)
const slowOnly = ref(false)
const page = ref(1)
const error = ref('')

function choose(p: Preset) {
  const d = today.value
  preset.value = p
  if (p === 'today') { from.value = to.value = d }
  else if (p === 'yesterday') { from.value = to.value = addDays(d, -1) }
  else if (p === 'week') { from.value = addDays(d, -6); to.value = d }
  else if (p === 'month') { from.value = monthStart(d); to.value = d }
}
const single = computed(() => from.value === to.value)
const span = computed(() => Math.round((Date.parse(to.value) - Date.parse(from.value)) / 86400000) + 1)
function step(n: number) {
  const days = span.value * n
  from.value = addDays(from.value, days); to.value = addDays(to.value, days)
  preset.value = single.value && to.value === today.value ? 'today' : single.value && to.value === addDays(today.value, -1) ? 'yesterday' : 'custom'
}
function openDay(d: string) { from.value = to.value = d; preset.value = d === today.value ? 'today' : 'custom' }
const wd = (d: string) => t('weekday.' + weekday(d))
const title = computed(() => single.value ? `${from.value} ${wd(from.value)}` : `${from.value.slice(5)} ~ ${to.value.slice(5)}`)
const periodText = computed(() => preset.value === 'today' ? t('att.today') : preset.value === 'yesterday' ? t('kr.yesterday') : single.value ? from.value.slice(5) : t('kr.n_days', { n: span.value }))

const range = () => `from=${from.value}&to=${to.value}`
let seq = 0
async function load() {
  if (!from.value) choose('today')
  if (from.value > to.value) return
  const mine = ++seq
  try {
    const r = await get<Report>(`/merchant/branches/${bid}/reports/kitchen?${range()}`)
    if (mine !== seq) return
    if (r.timezone !== tz.value) { tz.value = r.timezone; choose(preset.value === 'custom' ? 'today' : preset.value); return }
    report.value = r; error.value = ''
    if (who.value === null || !r.people.some(p => p.id === who.value)) who.value = r.people.find(p => p.id !== 0)?.id ?? r.people[0]?.id ?? null
    await loadDetail()
  } catch (e) { if (mine === seq) error.value = (e as Error).message }
}
let dseq = 0
async function loadDetail() {
  if (who.value === null) { detail.value = null; return }
  const mine = ++dseq
  try {
    const q = range() + (station.value !== null ? `&station=${station.value}` : '') + (slowOnly.value ? '&slow=1' : '') + `&page=${page.value}`
    const d = await get<Detail>(`/merchant/branches/${bid}/reports/kitchen/${who.value}?${q}`)
    if (mine === dseq) { detail.value = d; error.value = '' }
  } catch (e) { if (mine === dseq) error.value = (e as Error).message }
}
onMounted(load)
watch([from, to], () => { page.value = 1; station.value = null; load() })
watch(who, () => { page.value = 1; station.value = null; slowOnly.value = false; loadDetail() })
watch([station, slowOnly], () => { if (page.value === 1) loadDetail(); else page.value = 1 })
watch(page, loadDetail)

const colors = ['#d9480f', '#1d4ed8', '#18794e', '#7c3aed', '#b45309', '#0e7490', '#be185d']
const postName = (p: Post) => p.kind === 'station' ? pick(p.station_name ?? '', p.station_name_en ?? '') : p.kind === 'packing' ? t('wk.packing') : t('rider')
const name = (p: Person) => p.id === 0 ? t('kr.board') : p.display_name
const sub = (p: Person) => p.id === 0 ? t('kr.board_sub') : p.gone ? t('kr.gone')
  : p.posts.length ? p.posts.map(postName).join(' · ') : p.role !== 'staff' ? t('role.' + p.role) : t('st.no_posts')
const selected = computed(() => report.value?.people.find(p => p.id === who.value) ?? null)
const boardShare = computed(() => report.value && report.value.total_qty ? Math.round(report.value.board_qty * 100 / report.value.total_qty) : 0)

const mmss = (s: number) => `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`
const perHour = computed(() => detail.value && detail.value.shift_minutes >= 10 ? Math.round(detail.value.qty * 60 / detail.value.shift_minutes) : null)
const maxDay = computed(() => Math.max(1, ...(detail.value?.days ?? []).map(d => d.qty)))
const chartDays = computed(() => detail.value?.days ?? [])
const inRange = (d: string) => d >= from.value && d <= to.value
const dayLabel = (d: string) => d === today.value ? t('att.today') : chartDays.value.length > 10 ? String(+d.slice(8)) : t('kr.day_n', { n: +d.slice(8) })
const top = computed(() => {
  const all = detail.value?.top ?? []
  const head = all.slice(0, 5), rest = all.slice(5)
  return { head, rest: rest.length ? { n: rest.length, qty: rest.reduce((n, c) => n + c.qty, 0) } : null, max: Math.max(1, ...all.map(c => c.qty), rest.reduce((n, c) => n + c.qty, 0)) }
})
const stationName = (c: Count) => c.id ? pick(c.name, c.name_en ?? '') : t('kr.no_station')
const orderText = (d: Dish) => d.service_type === 'dine_in' ? `${d.table_name || d.public_no} ${t('dine_in')}` : `${d.public_no} ${t(d.service_type)}`
const viaText = (d: Dish) => d.via === 'work' ? t('kr.via.work') : d.via === 'board' ? t('kr.via.board', { name: d.ready_by }) : t('kr.via.unknown')
const pages = computed(() => detail.value ? Math.max(1, Math.ceil(detail.value.item_total / detail.value.page_size)) : 1)
</script>

<template>
  <div class="ox warm"><div class="page srp" data-testid="kitchen-report">
    <StaffTabs />
    <div class="row wrap" style="margin-bottom:14px">
      <span class="datepick"><button data-act="prev" @click="step(-1)"><span class="rtl-flip">‹</span></button><b data-testid="kr-title">{{ title }}</b><button data-act="next" :disabled="to >= today" @click="step(1)"><span class="rtl-flip">›</span></button></span>
      <span class="pills">
        <button v-for="p in (['today', 'yesterday', 'week', 'month'] as Preset[])" :key="p" :class="{ on: preset === p }" :data-preset="p" @click="choose(p)">{{ t('kr.p.' + p) }}</button>
        <button :class="{ on: preset === 'custom' }" data-preset="custom" @click="preset = 'custom'">{{ t('kr.p.custom') }}</button>
      </span>
      <span v-if="preset === 'custom'" class="custom"><input v-model="from" type="date" name="from" :max="to"> ~ <input v-model="to" type="date" name="to" :min="from" :max="today"></span>
      <span class="grow"></span>
      <ExportLink list="reports/kitchen" :from="from" :to="to" />
    </div>
    <div v-if="error" class="banner bad" style="margin-bottom:12px">{{ error }}</div>

    <div v-if="report" class="split">
      <div>
        <div class="card people" data-testid="kr-people">
          <div class="head" data-testid="kr-total">{{ t('kr.total', { when: periodText, n: report.total_qty }) }}</div>
          <button v-for="p in report.people" :key="p.id" class="p" :class="{ on: p.id === who }" :data-person="p.id === 0 ? 'board' : p.username" @click="who = p.id">
            <span class="av" :style="{ background: p.id === 0 ? '#6b7280' : colors[p.id % colors.length] }">{{ p.id === 0 ? t('kr.board_av') : p.display_name.slice(0, 1) }}</span>
            <span class="grow"><b>{{ name(p) }}</b><br><span class="sub">{{ sub(p) }}</span></span>
            <span class="n"><b data-col="qty">{{ p.qty }}</b>
              <span class="sub">{{ p.packed ? t('kr.qty_packed', { n: p.packed }) : p.qty || p.id === 0 || p.shift_minutes ? t('kr.qty_unit') : t('kr.not_in') }}</span></span>
          </button>
          <div v-if="!report.people.length" class="empty">{{ t('kr.nobody') }}</div>
        </div>
        <p v-if="report.board_qty" class="sub" style="margin:10px 2px" data-testid="kr-board-share">{{ t('kr.board_share', { p: boardShare }) }}</p>
      </div>

      <div v-if="detail && selected" data-testid="kr-detail">
        <div v-if="who === 0" class="banner info" style="margin-bottom:12px">{{ t('kr.board_note') }}</div>
        <div class="kpis">
          <div class="kpi" data-kpi="qty"><div class="l">{{ t('kr.k.made') }}</div><div class="v">{{ detail.qty }}<small>{{ t('kr.qty_unit') }}</small></div>
            <div class="d">{{ t('kr.k.made_d', { dishes: detail.dishes, orders: detail.orders }) }}</div></div>
          <div class="kpi" data-kpi="avg"><div class="l">{{ t('kr.k.avg') }}</div><div class="v">{{ detail.avg_seconds === null ? '—' : mmss(detail.avg_seconds) }}</div>
            <div class="d">{{ detail.timed < detail.dishes ? t('kr.k.avg_part', { n: detail.dishes - detail.timed }) : t('kr.k.avg_d') }}</div></div>
          <div class="kpi" :class="{ warn: detail.slow_qty > 0 }" data-kpi="slow"><div class="l">{{ t('kr.k.slow', { n: detail.slow_minutes }) }}</div><div class="v">{{ detail.slow_qty }}<small>{{ t('kr.qty_unit') }}</small></div>
            <div class="d">{{ detail.qty ? t('kr.k.slow_d', { p: Math.round(detail.slow_qty * 100 / detail.qty) }) : '' }}</div></div>
          <div class="kpi" data-kpi="undo"><div class="l">{{ t('kr.k.undo') }}</div><div class="v">{{ detail.undo_count }}<small>{{ t('kr.times') }}</small></div>
            <div class="d">{{ t('kr.k.undo_d') }}</div></div>
          <div v-if="who !== 0" class="kpi" data-kpi="shift"><div class="l">{{ t('kr.k.shift') }}</div><div class="v">{{ hm(detail.shift_minutes) }}</div>
            <div class="d">{{ perHour === null ? t('kr.k.shift_none') : t('kr.k.per_hour', { n: perHour }) }}</div></div>
          <div v-if="detail.packed" class="kpi" data-kpi="packed"><div class="l">{{ t('kr.k.packed') }}</div><div class="v">{{ detail.packed }}<small>{{ t('kr.orders_unit') }}</small></div></div>
        </div>

        <div class="two">
          <div class="card">
            <h3>{{ t('kr.per_day') }}<small>{{ t('kr.per_day_d') }}</small></h3>
            <div class="bars" data-testid="kr-bars">
              <button v-for="d in chartDays" :key="d.day" :class="{ on: inRange(d.day) }" :data-day="d.day" :title="`${d.day} ${wd(d.day)}`" @click="openDay(d.day)">
                <span class="q">{{ d.qty || (who !== 0 && !d.worked && d.day <= today ? t('kr.off') : d.qty) }}</span>
                <i :style="{ height: (d.qty * 100 / maxDay) + '%' }"></i>
                <span class="d">{{ dayLabel(d.day) }}</span>
              </button>
            </div>
          </div>
          <div class="card">
            <h3>{{ t('kr.top') }}<small>{{ periodText }}</small></h3>
            <div v-for="c in top.head" :key="c.id" class="hbar"><span>{{ pick(c.name, c.name_en ?? '') }}</span><i><u :style="{ width: (c.qty * 100 / top.max) + '%' }"></u></i><b>{{ c.qty }}</b></div>
            <div v-if="top.rest" class="hbar rest"><span>{{ t('kr.others', { n: top.rest.n }) }}</span><i><u :style="{ width: (top.rest.qty * 100 / top.max) + '%' }"></u></i><b>{{ top.rest.qty }}</b></div>
            <div v-if="!top.head.length" class="sub">{{ t('kr.none') }}</div>
            <p v-if="detail.stations.length" class="sub" style="margin:10px 0 0" data-testid="kr-stations">{{ t('kr.by_station') }}{{ detail.stations.map(c => `${stationName(c)} ${c.qty}`).join(' · ') }}</p>
          </div>
        </div>

        <div class="card" style="padding:0;overflow-x:auto">
          <div class="row wrap" style="padding:12px 14px">
            <h3 style="margin:0">{{ t('kr.items') }}<small data-testid="kr-item-total">{{ t('kr.items_d', { n: detail.item_total }) }}</small></h3>
            <span class="grow"></span>
            <span class="pills" data-testid="kr-filter">
              <button :class="{ on: station === null && !slowOnly }" data-f="all" @click="station = null; slowOnly = false">{{ t('kr.f.all') }}</button>
              <button v-for="c in detail.stations" :key="c.id" :class="{ on: station === (c.id ?? 0) }" :data-f="'s' + (c.id ?? 0)" @click="station = c.id ?? 0">{{ stationName(c) }}</button>
              <button :class="{ on: slowOnly }" data-f="slow" @click="slowOnly = !slowOnly">{{ t('kr.f.slow') }}</button>
            </span>
          </div>
          <table class="data">
            <thead><tr><th>{{ t('kr.c.ready') }}</th><th>{{ t('kr.c.dish') }}</th><th class="num">{{ t('kr.c.qty') }}</th><th>{{ t('kr.c.station') }}</th>
              <th>{{ t('kr.c.order') }}</th><th class="num">{{ t('kr.c.took') }}</th><th>{{ t('kr.c.via') }}</th></tr></thead>
            <tbody>
              <tr v-for="d in detail.items" :key="d.id" :data-dish="d.name">
                <td class="num" style="text-align: start;white-space:nowrap">{{ single ? '' : dayOf(d.ready_at, tz).slice(5) + ' ' }}{{ clock(d.ready_at, tz) }}</td>
                <td><b>{{ pick(d.name, d.name_en) }}</b> <span v-if="d.cancelled" class="tag">{{ t('kr.cancelled') }}</span> <span v-if="d.undo_count" class="tag warn">{{ t('kr.undone', { n: d.undo_count }) }}</span>
                  <div v-if="d.options.length || d.note" class="sub">{{ [d.options.map(o => pick(o.name, o.name_en)).join('、'), d.note && t('kr.note', { s: d.note })].filter(Boolean).join(' · ') }}</div></td>
                <td class="num">{{ d.qty }}</td>
                <td><span v-if="d.station_name" class="tag info">{{ d.station_name }}</span><span v-else class="muted">—</span></td>
                <td style="white-space:nowrap">{{ orderText(d) }}</td>
                <td class="num" data-col="took"><span v-if="d.seconds === null" class="muted">{{ t('kr.no_start') }}</span><span v-else :class="{ slowt: d.slow }">{{ mmss(d.seconds) }}</span>
                  <div v-if="d.started_by && d.started_by !== d.ready_by" class="sub">{{ t('kr.started_by', { name: d.started_by }) }}</div></td>
                <td data-col="via">{{ viaText(d) }}</td>
              </tr>
            </tbody>
          </table>
          <div v-if="!detail.items.length" class="empty">{{ t('kr.none') }}</div>
          <div v-if="pages > 1" class="pager"><button class="btn small" :disabled="page <= 1" data-act="page-prev" @click="page--"><span class="rtl-flip">‹</span></button>
            <span class="sub">{{ page }} / {{ pages }}</span><button class="btn small" :disabled="page >= pages" data-act="page-next" @click="page++"><span class="rtl-flip">›</span></button></div>
        </div>
      </div>
    </div>
  </div>
  </div>
</template>
