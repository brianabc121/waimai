<script setup lang="ts">
// Rider work summary: per rider the trips, orders and distance of a period,
// and below it every trip of the rider picked.
import { computed, onMounted, ref, watch } from 'vue'
import { get } from '../../api'
import { money, t } from '../../i18n'
import { session } from '../../session'
import ExportLink from './ExportLink.vue'
import PeriodBar from './PeriodBar.vue'
import StaffTabs from './StaffTabs.vue'
import { clock, hm, weekday } from './time'
import { km, signedKm, type Trip, tripState } from './trips'
import './strings'
import './report.css'

interface Line { id: number; display_name: string; username: string; gone: boolean; shift_minutes: number; trips: number; orders: number
  delivered: number; failed: number; actual_m: number; no_track: number; compare_m: number; compared_actual_m: number; detours: number
  gaps: number; cash: number; on_trip: boolean }
interface Report { timezone: string; currency: string; detour_percent: number; detour_meters: number; store_set: boolean; road_routes: boolean; riders: Line[] }
interface Day { day: string; trips: number; delivered: number; failed: number; actual_m: number }
interface Detail { days: Day[]; trips: Trip[] }

const bid = session.branchId!
const tz = ref('Asia/Manila')
const from = ref('')
const to = ref('')
const period = ref<InstanceType<typeof PeriodBar>>()
const report = ref<Report | null>(null)
const detail = ref<Detail | null>(null)
const who = ref<number | null>(null)
const openDays = ref<Set<string>>(new Set())
const error = ref('')

let seq = 0
async function load() {
  if (!from.value || from.value > to.value) return
  const mine = ++seq
  try {
    const r = await get<Report>(`/merchant/branches/${bid}/reports/riders?from=${from.value}&to=${to.value}`)
    if (mine !== seq) return
    if (r.timezone !== tz.value) { tz.value = r.timezone; return }
    report.value = r; error.value = ''
    if (who.value === null || !r.riders.some(x => x.id === who.value)) who.value = r.riders[0]?.id ?? null
    await loadDetail()
  } catch (e) { if (mine === seq) error.value = (e as Error).message }
}
let dseq = 0
async function loadDetail() {
  if (who.value === null) { detail.value = null; return }
  const mine = ++dseq
  try {
    const d = await get<Detail>(`/merchant/branches/${bid}/reports/riders/${who.value}?from=${from.value}&to=${to.value}`)
    if (mine !== dseq) return
    detail.value = d; error.value = ''
    // one day is open; of a longer period the last day with trips
    openDays.value = new Set(d.days.length ? [d.days[d.days.length - 1].day] : [])
  } catch (e) { if (mine === dseq) error.value = (e as Error).message }
}
onMounted(load)
watch([from, to], load)
watch(who, loadDetail)

const sum = (f: (l: Line) => number) => (report.value?.riders ?? []).reduce((n, l) => n + f(l), 0)
const total = computed(() => ({ shift: sum(l => l.shift_minutes), trips: sum(l => l.trips), delivered: sum(l => l.delivered), failed: sum(l => l.failed),
  orders: sum(l => l.orders), actual: sum(l => l.actual_m), compare: sum(l => l.compare_m), compared: sum(l => l.compared_actual_m), cash: sum(l => l.cash) }))
const perTrip = (orders: number, trips: number) => trips ? (orders / trips).toFixed(1) : '—'
const overPct = (actual: number, compare: number) => compare ? Math.round((actual - compare) * 100 / compare) : null
const pctText = (p: number | null) => p === null ? '—' : (p > 0 ? '+' : '') + p + '%'
const selected = computed(() => report.value?.riders.find(l => l.id === who.value) ?? null)
const single = computed(() => from.value === to.value)
const tripsOf = (day: string) => (detail.value?.trips ?? []).filter(x => x.day === day)
const no = (x: Trip) => tripsOf(x.day).indexOf(x) + 1
const toggle = (day: string) => { const s = new Set(openDays.value); s.has(day) ? s.delete(day) : s.add(day); openDays.value = s }
const took = (x: Trip) => x.ended_at ? hm((Date.parse(x.ended_at) - Date.parse(x.started_at)) / 60000) : '—'
const dayText = (d: string) => `${d.slice(5)} ${t('weekday.' + weekday(d))}`
</script>

<template>
  <div class="ox warm"><div class="page srp" data-testid="rider-report">
    <StaffTabs />
    <div class="row wrap" style="margin-bottom:14px"><PeriodBar ref="period" v-model:from="from" v-model:to="to" :tz="tz" /><span class="grow"></span><ExportLink list="reports/riders" :from="from" :to="to" /></div>
    <div v-if="error" class="banner bad" style="margin-bottom:12px">{{ error }}</div>
    <div v-if="report && !report.store_set" class="banner warn" style="margin-bottom:12px" data-testid="rr-no-store">{{ t('rr.no_store') }}
      <router-link to="/merchant/delivery-map">{{ t('rr.go_map') }}</router-link></div>

    <template v-if="report">
      <div class="card" style="padding:0;overflow-x:auto;margin-bottom:14px">
        <table class="data rrtable" data-testid="rr-riders">
          <thead><tr><th>{{ t('rr.c.rider') }}</th><th class="num">{{ t('rr.c.shift') }}</th><th class="num">{{ t('rr.c.trips') }}</th><th class="num">{{ t('rr.c.delivered') }}</th>
            <th class="num">{{ t('rr.c.failed') }}</th><th class="num">{{ t('rr.c.per_trip') }}</th><th class="num">{{ t('rr.c.actual') }}</th>
            <th class="num">{{ t('rr.c.planned') }}</th><th class="num">{{ t('rr.c.over') }}</th><th class="num">{{ t('rr.c.cash') }}</th></tr></thead>
          <tbody>
            <tr v-for="l in report.riders" :key="l.id" :class="{ on: l.id === who }" :data-rider="l.username" @click="who = l.id">
              <td><b>{{ l.display_name }}</b> <span v-if="l.on_trip" class="tag brand">{{ t('rr.on_trip') }}</span><span v-if="l.gone" class="tag">{{ t('kr.gone') }}</span></td>
              <td class="num">{{ l.shift_minutes ? hm(l.shift_minutes) : '—' }}</td>
              <td class="num" data-col="trips">{{ l.trips }}</td><td class="num" data-col="delivered">{{ l.delivered }}</td>
              <td class="num" :class="{ warnt: l.failed }">{{ l.failed }}</td><td class="num">{{ perTrip(l.orders, l.trips) }}</td>
              <td class="num" data-col="actual">{{ l.trips > l.no_track ? km(l.actual_m) : '—' }}<span v-if="l.gaps || (l.no_track && l.trips > l.no_track)" class="sub" :title="t('rr.partial_tip')"> *</span></td>
              <td class="num" data-col="planned">{{ l.compare_m ? km(l.compare_m) : '—' }}</td>
              <td class="num" data-col="over" :class="{ warnt: l.detours }">{{ pctText(overPct(l.compared_actual_m, l.compare_m)) }}<span v-if="l.detours" class="sub"> {{ t('rr.detours', { n: l.detours }) }}</span></td>
              <td class="num">{{ l.cash ? money(l.cash, report.currency) : '—' }}</td>
            </tr>
            <tr v-if="report.riders.length > 1" class="sumrow" data-testid="rr-total">
              <td>{{ t('rr.total') }}</td><td class="num">{{ hm(total.shift) }}</td><td class="num">{{ total.trips }}</td><td class="num">{{ total.delivered }}</td>
              <td class="num">{{ total.failed }}</td><td class="num">{{ perTrip(total.orders, total.trips) }}</td><td class="num">{{ km(total.actual) }}</td>
              <td class="num">{{ total.compare ? km(total.compare) : '—' }}</td><td class="num">{{ pctText(overPct(total.compared, total.compare)) }}</td>
              <td class="num">{{ total.cash ? money(total.cash, report.currency) : '—' }}</td>
            </tr>
          </tbody>
        </table>
        <div v-if="!report.riders.length" class="empty">{{ t('rr.nobody') }}</div>
      </div>
      <p class="sub" style="margin:-4px 2px 14px">{{ t(report.road_routes ? 'rr.note_road' : 'rr.note', { p: report.detour_percent, km: report.detour_meters / 1000 }) }}</p>

      <div v-if="selected && detail" class="card" style="padding:0;overflow-x:auto" data-testid="rr-trips">
        <div style="padding:12px 14px"><h3 style="margin:0">{{ t('rr.trips_of', { name: selected.display_name }) }}<small>{{ t('rr.trips_d', { n: detail.trips.length }) }}</small></h3></div>
        <div v-if="!detail.trips.length" class="empty">{{ t('rr.no_trips') }}</div>
        <template v-for="d in detail.days" :key="d.day">
          <button v-if="!single" class="dayhead" :data-day="d.day" @click="toggle(d.day)">
            <b>{{ openDays.has(d.day) ? '▾' : '▸' }} {{ dayText(d.day) }}</b>
            <span>{{ t('rr.day_sum', { trips: d.trips, delivered: d.delivered }) }}<template v-if="d.failed"> · {{ t('rr.day_failed', { n: d.failed }) }}</template> · {{ km(d.actual_m) }}</span>
          </button>
          <table v-if="single || openDays.has(d.day)" class="data rrtable">
            <thead><tr><th>#</th><th>{{ t('rr.t.out') }}</th><th>{{ t('rr.t.back') }}</th><th class="num">{{ t('rr.t.took') }}</th><th>{{ t('rr.t.orders') }}</th>
              <th class="num">{{ t('rr.c.actual') }}</th><th class="num">{{ t('rr.c.planned') }}</th><th class="num">{{ t('rr.c.over') }}</th><th></th></tr></thead>
            <tbody>
              <tr v-for="x in tripsOf(d.day)" :key="x.id" :data-trip="x.id">
                <td class="num" style="text-align: start">{{ no(x) }}</td>
                <td style="white-space:nowrap">{{ clock(x.left_store_at ?? x.started_at, tz) }}</td>
                <td style="white-space:nowrap" data-col="back"><template v-if="x.returned_at">{{ clock(x.returned_at, tz) }}</template>
                  <span v-else class="tag" :class="x.status === 'done' ? 'warn' : 'brand'">{{ tripState(x) }}</span></td>
                <td class="num">{{ took(x) }}</td>
                <td><span v-for="o in x.orders" :key="o.order_id" class="stop" :class="o.outcome || 'pending'" :title="o.fail_reason"><i>{{ o.drop_seq ?? '·' }}</i>{{ o.public_no }}<template v-if="o.outcome === 'failed'"> {{ t('rr.failed') }}</template></span></td>
                <td class="num" data-col="actual">{{ x.actual_m === null ? '—' : km(x.actual_m) }}<span v-if="x.gap_seconds" class="sub" :title="t('rr.gap_tip')"> *</span></td>
                <td class="num" data-col="planned">{{ x.compare_m === null ? '—' : km(x.compare_m) }}</td>
                <td class="num" data-col="over" :class="{ warnt: x.detour }">{{ x.over_m === null ? '—' : signedKm(x.over_m) }}</td>
                <td class="right"><router-link class="btn small" :to="`/merchant/staff/trips/${x.id}`" data-act="route">{{ t('rr.route') }}</router-link></td>
              </tr>
            </tbody>
          </table>
        </template>
      </div>
    </template>
  </div>
  </div>
</template>

<style scoped>
.rrtable td, .rrtable th { white-space: nowrap; }
.rrtable tbody tr[data-rider] { cursor: pointer; } .rrtable tr.on td { background: var(--brand-soft); }
.rrtable tr.sumrow td { font-weight: 700; background: #fafafa; }
.warnt { color: var(--warn); font-weight: 700; }
.dayhead { display: flex; justify-content: space-between; gap: 12px; width: 100%; border: 0; border-top: 1px solid var(--line); background: #fafafa; padding: 9px 14px; font: inherit; cursor: pointer; color: inherit; }
.dayhead span { color: var(--muted); font-size: 13px; }
.stop { display: inline-flex; align-items: center; gap: 5px; border: 1px solid var(--line); border-radius: 999px; padding: 1px 9px 1px 2px; margin-inline-end: 5px; font-size: 13px; font-variant-numeric: tabular-nums; background: #fff; }
.stop i { font-style: normal; width: 18px; height: 18px; border-radius: 50%; background: var(--brand); color: #fff; font-size: 11px; font-weight: 700; display: inline-flex; align-items: center; justify-content: center; }
.stop.failed { border-color: #f0d9a8; color: var(--warn); } .stop.failed i { background: var(--warn); }
.stop.pending i { background: #9aa1ab; } .stop.removed { text-decoration: line-through; color: var(--muted); } .stop.removed i { background: #9aa1ab; }
</style>
