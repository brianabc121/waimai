<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { get } from '../../api'
import { t } from '../../i18n'
import Pager from './Pager.vue'
import Chart from './charts/Chart.vue'
import { errorRateSeries, latencySeries, requestSeries } from './charts/series'
import { poll } from './monitor'
import type { ApiError, ApiMetrics, ApiRoute } from './types'
import { count, fmtDateTime, fmtTime, ms, pct, qs, rate } from './util'

const SIZE = 20
const ranges = ['1h', '6h', '24h', '7d']
const range = ref('1h')
const m = ref<ApiMetrics | null>(null)
const error = ref('')
const loading = ref(false)
const filter = ref('')
type SortKey = 'count' | 'c4' | 'c5' | 'p50' | 'p95' | 'max_ms'
const sortBy = ref<SortKey>('count')
const errStatus = ref(0)
const errRoute = ref('')
const errPage = ref(1)
const errors = reactive<{ items: ApiError[]; total: number }>({ items: [], total: 0 })

async function load() {
  try {
    const [metrics, errs] = await Promise.all([
      get<ApiMetrics>('/admin/metrics/api' + qs({ range: range.value })),
      get<{ items: ApiError[]; total: number }>('/admin/metrics/errors' + qs({ range: range.value, status: errStatus.value, route: errRoute.value, page: errPage.value, page_size: SIZE })),
    ])
    m.value = metrics
    Object.assign(errors, errs)
    error.value = ''
  } catch (e) { error.value = (e as Error).message }
  loading.value = false
}
let stop = () => {}
onMounted(() => { stop = poll(load, 10) })
onUnmounted(() => stop())
// while the new range loads the charts keep their last picture, dimmed
function pick(r: string) { range.value = r; errPage.value = 1; loading.value = true; void load() }
function reloadErrors() { errPage.value = 1; void load() }
function go(p: number) { errPage.value = p; void load() }

const points = computed(() => m.value?.series ?? [])
const step = computed(() => m.value?.step_seconds ?? 60)
const times = computed(() => points.value.map(p => p.t))
const requests = computed(() => requestSeries(points.value, step.value))
const errorRate = computed(() => errorRateSeries(points.value))
const latency = computed(() => latencySeries(points.value))
const totalRate = computed(() => (m.value && m.value.totals.requests ? m.value.totals.c5 / m.value.totals.requests : 0))
const streams = computed(() => { const s = m.value?.streams; return s ? s.customer + s.table + s.store + s.rider : 0 })

const cols: { key: SortKey; label: string }[] = [
  { key: 'count', label: 'ad.r.count' }, { key: 'c4', label: '4xx' }, { key: 'c5', label: '5xx' },
  { key: 'p50', label: 'P50' }, { key: 'p95', label: 'P95' }, { key: 'max_ms', label: 'ad.r.slowest' },
]
const routes = computed<ApiRoute[]>(() => {
  const q = filter.value.trim().toLowerCase()
  const k = sortBy.value
  return (m.value?.routes ?? []).filter(r => !q || (r.method + ' ' + r.route).toLowerCase().includes(q)).slice().sort((a, b) => b[k] - a[k] || b.count - a.count)
})
const routeNames = computed(() => [...new Set((m.value?.routes ?? []).map(r => r.route))].sort())
function showErrors(r: ApiRoute) { errRoute.value = r.route; errStatus.value = 0; reloadErrors() }
</script>

<template>
  <div class="adm-page">
    <div v-if="error" class="banner bad">{{ error }}</div>
    <div class="row wrap">
      <div class="tabs"><button v-for="r in ranges" :key="r" :class="{ on: range === r }" :data-range="r" @click="pick(r)">{{ t('ad.range.' + r) }}</button></div>
      <span class="grow"></span>
      <template v-if="m">
        <span class="small muted" data-testid="streams">{{ t('ad.streams', { n: streams, c: m.streams.customer + m.streams.table, s: m.streams.store, r: m.streams.rider }) }}</span>
        <span class="small muted" data-testid="limited">{{ t('ad.limited', { n: count(m.totals.limited) }) }}</span>
      </template>
    </div>
    <template v-if="m">
      <div class="adm-grid5 stat">
        <div class="card"><div class="muted small">{{ t('ad.r.count') }}</div><div class="v" data-testid="total-requests">{{ count(m.totals.requests) }}</div></div>
        <div class="card"><div class="muted small">4xx</div><div class="v">{{ count(m.totals.c4) }}</div></div>
        <div class="card"><div class="muted small">{{ t('ad.error_rate') }}</div><div class="v" :style="m.totals.c5 ? 'color:var(--bad)' : ''">{{ pct(totalRate) }}</div><div class="small muted">{{ t('ad.d.times', { n: count(m.totals.c5) }) }}</div></div>
        <div class="card"><div class="muted small">P50</div><div class="v">{{ ms(m.totals.p50) }}</div></div>
        <div class="card"><div class="muted small">P95</div><div class="v">{{ ms(m.totals.p95) }}</div><div class="small muted">{{ t('ad.r.slowest') }} {{ ms(m.totals.max_ms) }}</div></div>
      </div>
      <div class="adm-grid3" :style="loading ? 'opacity:.5' : ''">
        <div class="card"><div class="head"><h2>{{ t('ad.c.requests') }}</h2></div>
          <Chart kind="bars" :times="times" :step-seconds="step" :series="requests" :format="rate" :label="t('ad.c.requests')" :min-max="4" /></div>
        <div class="card"><div class="head"><h2>{{ t('ad.error_rate') }}</h2></div>
          <Chart kind="lines" :times="times" :step-seconds="step" :series="errorRate" :format="pct" :label="t('ad.error_rate')" :min-max="0.05" /></div>
        <div class="card"><div class="head"><h2>{{ t('ad.c.latency') }}</h2></div>
          <Chart kind="lines" :times="times" :step-seconds="step" :series="latency" :format="ms" :label="t('ad.c.latency')" :min-max="100" /></div>
      </div>

      <div class="card flush">
        <div class="head"><h2>{{ t('ad.r.title') }}</h2><input v-model="filter" :placeholder="t('ad.r.filter')" style="width:220px" data-testid="route-filter" /></div>
        <table class="data">
          <thead><tr><th>{{ t('ad.r.method') }}</th><th>{{ t('ad.r.route') }}</th>
            <th v-for="c in cols" :key="c.key" class="right sort" :data-sort="c.key" @click="sortBy = c.key">{{ c.label.startsWith('ad.') ? t(c.label) : c.label }}{{ sortBy === c.key ? ' ▾' : '' }}</th></tr></thead>
          <tbody>
            <tr v-for="r in routes" :key="r.method + r.route" class="click" :data-route="r.method + ' ' + r.route" :title="t('ad.r.show_errors')" @click="showErrors(r)">
              <td><span class="tag" :class="r.method === 'GET' ? 'info' : ''">{{ r.method }}</span></td>
              <td class="mono">{{ r.route }}</td>
              <td class="right num">{{ count(r.count) }}</td>
              <td class="right num">{{ count(r.c4) }}</td>
              <td class="right num"><b v-if="r.c5" style="color:var(--bad)">{{ count(r.c5) }}</b><template v-else>0</template></td>
              <td class="right num">{{ ms(r.p50) }}</td>
              <td class="right num">{{ ms(r.p95) }}</td>
              <td class="right num">{{ ms(r.max_ms) }}</td>
            </tr>
            <tr v-if="routes.length === 0"><td colspan="8" class="muted">{{ t('ad.no_traffic') }}</td></tr>
          </tbody>
        </table>
      </div>

      <div class="card flush">
        <div class="head"><h2>{{ t('ad.e.title') }}</h2>
          <div class="row">
            <select v-model.number="errStatus" style="width:130px" data-testid="error-status" @change="reloadErrors">
              <option :value="0">{{ t('ad.e.all_status') }}</option><option :value="5">5xx</option><option :value="429">429</option>
            </select>
            <select v-model="errRoute" style="width:300px" data-testid="error-route" @change="reloadErrors">
              <option value="">{{ t('ad.e.all_routes') }}</option>
              <option v-for="r in routeNames" :key="r" :value="r">{{ r }}</option>
            </select>
          </div>
        </div>
        <table class="data">
          <thead><tr><th>{{ t('ad.time') }}</th><th>{{ t('ad.endpoint') }}</th><th>{{ t('ad.status') }}</th><th class="right">{{ t('ad.duration') }}</th><th>request_id</th><th>{{ t('ad.e.internal') }}</th></tr></thead>
          <tbody>
            <tr v-for="e in errors.items" :key="e.id" :data-error="e.status">
              <td class="num" :title="e.at">{{ fmtDateTime(e.at) }}{{ fmtTime(e.at).slice(5) }}</td>
              <td class="mono">{{ e.method }} {{ e.route }}</td>
              <td><span class="tag" :class="e.status >= 500 ? 'bad' : 'warn'">{{ e.status }} {{ e.code }}</span></td>
              <td class="right num">{{ ms(e.duration_ms) }}</td>
              <td class="mono">{{ e.request_id }}</td>
              <td class="small muted" style="max-width:340px;word-break:break-word">{{ e.message || '—' }}</td>
            </tr>
            <tr v-if="errors.items.length === 0"><td colspan="6" class="muted">{{ t('ad.d.no_errors') }}</td></tr>
          </tbody>
        </table>
      </div>
      <Pager :total="errors.total" :page="errPage" :size="SIZE" @go="go"><span>{{ t('ad.auto_refresh') }}</span></Pager>
      <p class="small muted">{{ t('ad.api_foot') }}</p>
    </template>
  </div>
</template>
