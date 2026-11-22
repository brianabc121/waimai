<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { get } from '../../api'
import { t } from '../../i18n'
import Chart from './charts/Chart.vue'
import { latencySeries, requestSeries } from './charts/series'
import { loadWorkerStatus, poll } from './monitor'
import type { Health, Overview, WorkerStatus, WorkerTask } from './types'
import { ago, count, fmtTime, ms, pct, rate } from './util'

const o = ref<Overview | null>(null)
const worker = ref<WorkerStatus | null>(null)
const error = ref('')

async function load() {
  try {
    const [ov, ws] = await Promise.all([get<Overview>('/admin/overview'), loadWorkerStatus().catch(() => null)])
    o.value = ov
    worker.value = ws
    error.value = ''
  } catch (e) { error.value = (e as Error).message }
}
let stop = () => {}
onMounted(() => { stop = poll(load, 10) })
onUnmounted(() => stop())

const now = computed(() => (o.value ? new Date(o.value.now).getTime() : Date.now()))
const mark: Record<Health, string> = { ok: '✓', warn: '!', bad: '✕' }
function wait(s: number): string {
  return s < 60 ? t('ad.n_seconds', { n: s }) : t('ad.n_minutes', { n: Math.floor(s / 60) })
}
// each card says in one line what its verdict is based on
const cards = computed(() => {
  const v = o.value
  if (!v) return []
  const dbDown = v.db.state === 'bad'
  return [
    { key: 'api', name: 'API', to: '/admin/api', state: v.api.state,
      why: dbDown ? t('ad.h.unknown') : v.api.requests === 0 ? t('ad.h.api_idle') : t('ad.h.api', { rate: pct(v.api.error_rate), p95: ms(v.api.p95) }) },
    { key: 'worker', name: 'Worker', to: '/admin/worker', state: v.worker.state,
      why: dbDown ? t('ad.h.unknown') : v.worker.online === 0 ? t('ad.h.worker_none')
        : v.worker.failing_tasks.length ? t('ad.h.worker_failing', { tasks: v.worker.failing_tasks.map(k => t('ad.task.' + k)).join('、') })
          : t('ad.h.worker', { n: v.worker.online }) },
    { key: 'db', name: t('ad.h.db_name'), to: '', state: v.db.state,
      why: dbDown ? t('ad.h.db_down') : t('ad.h.db', { used: v.db.in_use, max: v.db.max, ms: v.db.ping_ms }) },
    { key: 'queue', name: t('ad.h.queue_name'), to: '/admin/worker', state: v.queue.state,
      why: dbDown ? t('ad.h.unknown') : v.queue.oldest_wait_seconds >= 60 ? t('ad.oldest_wait', { t: wait(v.queue.oldest_wait_seconds) })
        : v.queue.failed ? t('ad.h.queue_failed', { n: v.queue.failed }) : t('ad.none_waiting') },
    // only on an installation that has chat
    ...(v.chat?.on ? [{ key: 'chat', name: t('ad.h.chat_name'), to: '/admin/chats', state: v.chat.state,
      why: v.chat.state === 'ok' ? t('ad.h.chat_ok') : t('ad.h.chat_down') }] : []),
  ]
})
const dayRate = computed(() => (o.value && o.value.day.requests ? o.value.day.c5 / o.value.day.requests : 0))
const times = computed(() => (o.value?.hour ?? []).map(p => p.t))
const requests = computed(() => requestSeries(o.value?.hour ?? [], 60))
const latency = computed(() => latencySeries(o.value?.hour ?? []))
function counted(task: WorkerTask): string {
  return task.task === 'cleanup' || !task.last_started_at ? '' : t('ad.count.' + task.task, { n: task.last_count })
}
</script>

<template>
  <div class="adm-page">
    <div v-if="error" class="banner bad">{{ error }}</div>
    <template v-if="o">
      <div class="adm-grid4 health">
        <component :is="c.to ? 'router-link' : 'div'" v-for="c in cards" :key="c.key" :to="c.to || undefined" class="card" :data-health="c.key" :data-state="c.state">
          <div class="muted small">{{ c.name }}</div>
          <div class="state"><span class="dot" :class="c.state">{{ mark[c.state] }}</span>{{ t('ad.h.' + c.state) }}</div>
          <div class="small muted">{{ c.why }}</div>
        </component>
      </div>

      <div class="adm-grid5 stat">
        <router-link to="/admin/api" class="card"><div class="muted small">{{ t('ad.d.requests') }}</div><div class="v" data-testid="day-requests">{{ count(o.day.requests) }}</div>
          <div class="small muted">{{ t('ad.d.c4', { n: count(o.day.c4) }) }}</div></router-link>
        <router-link to="/admin/api" class="card"><div class="muted small">{{ t('ad.d.error_rate') }}</div><div class="v" :style="o.day.c5 ? 'color:var(--bad)' : ''">{{ pct(dayRate) }}</div>
          <div class="small muted">{{ t('ad.d.times', { n: count(o.day.c5) }) }}</div></router-link>
        <router-link to="/admin/api" class="card"><div class="muted small">{{ t('ad.d.p95') }}</div><div class="v">{{ ms(o.day.p95) }}</div>
          <div class="small muted">P50 {{ ms(o.day.p50) }}</div></router-link>
        <router-link to="/admin/merchants" class="card"><div class="muted small">{{ t('ad.orders_today') }}</div><div class="v">{{ count(o.orders_today) }}</div>
          <div class="small muted">{{ t('ad.d.orders_hint') }}</div></router-link>
        <router-link to="/admin/merchants" class="card"><div class="muted small">{{ t('ad.d.stores_open') }}</div>
          <div class="v">{{ o.stores_open }} <span class="small muted" style="font-weight:400">/ {{ o.stores_total }}</span></div>
          <div class="small muted">{{ t('ad.d.stores_hint') }}</div></router-link>
      </div>

      <div class="adm-grid2">
        <div class="card"><div class="head"><h2>{{ t('ad.c.requests') }} · {{ t('ad.c.last_hour') }}</h2></div>
          <Chart kind="bars" :times="times" :step-seconds="60" :series="requests" :format="rate" :label="t('ad.c.requests')" :min-max="4" /></div>
        <div class="card"><div class="head"><h2>{{ t('ad.c.latency') }} · {{ t('ad.c.last_hour') }}</h2><router-link to="/admin/api" class="small">{{ t('ad.api') }} ›</router-link></div>
          <Chart kind="lines" :times="times" :step-seconds="60" :series="latency" :format="ms" :label="t('ad.c.latency')" :min-max="100" /></div>
      </div>

      <div class="adm-grid2">
        <div class="card flush">
          <div class="head"><h2>{{ t('ad.d.recent_errors') }}</h2><router-link to="/admin/api" class="small">{{ t('ad.all') }} ›</router-link></div>
          <table class="data">
            <thead><tr><th>{{ t('ad.time') }}</th><th>{{ t('ad.endpoint') }}</th><th>{{ t('ad.status') }}</th><th>request_id</th></tr></thead>
            <tbody>
              <tr v-for="e in o.errors" :key="e.id">
                <td class="num" :title="e.at">{{ ago(e.at, now) }}</td>
                <td class="mono">{{ e.method }} {{ e.route.replace('/api/v1', '') }}</td>
                <td><span class="tag bad">{{ e.status }} {{ e.code }}</span></td>
                <td class="mono">{{ e.request_id }}</td>
              </tr>
              <tr v-if="o.errors.length === 0"><td colspan="4" class="muted">{{ t('ad.d.no_errors') }}</td></tr>
            </tbody>
          </table>
        </div>
        <div class="card flush">
          <div class="head"><h2>{{ t('ad.tasks') }}</h2><router-link to="/admin/worker" class="small">Worker ›</router-link></div>
          <table class="data">
            <thead><tr><th>{{ t('ad.task') }}</th><th>{{ t('ad.last_run') }}</th><th>{{ t('ad.result') }}</th></tr></thead>
            <tbody>
              <tr v-for="k in worker?.tasks ?? []" :key="k.task">
                <td>{{ t('ad.task.' + k.task) }}</td>
                <td class="num" :title="k.last_started_at ?? ''">{{ ago(k.last_started_at, now) }}</td>
                <td>
                  <span v-if="!k.last_started_at" class="muted small">{{ t('ad.not_run') }}</span>
                  <template v-else-if="k.ok"><span class="tag ok">✓ {{ t('ad.ok') }}</span> <span class="small muted">{{ counted(k) }}</span></template>
                  <span v-else class="tag bad">✕ {{ t('ad.failed') }}</span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
      <p class="small muted">{{ t('ad.d.foot', { time: fmtTime(o.now) }) }}</p>
    </template>
  </div>
</template>
