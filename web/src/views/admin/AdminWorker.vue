<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { get, post } from '../../api'
import { shown, t } from '../../i18n'
import Pager from './Pager.vue'
import { loadWorkerStatus, poll } from './monitor'
import type { JobRow, JobStatus, QueueKind, WorkerStatus, WorkerTask } from './types'
import { ago, fmtDateTime, fmtTime, qs } from './util'

const SIZE = 30
const status = ref<WorkerStatus | null>(null)
const tab = ref<'' | JobStatus>('')
const kind = ref('')
const page = ref(1)
const jobs = reactive<{ items: JobRow[]; total: number }>({ items: [], total: 0 })
const detail = ref<JobRow | null>(null)
const error = ref('')
const notice = ref('')
const busy = ref(false)
const tabs: ('' | JobStatus)[] = ['', 'pending', 'running', 'failed', 'done', 'discarded']
const kinds = ['sms', 'menu_import']

// "now" is the server's clock, so a wrong clock on this computer does not
// turn "3 seconds ago" into nonsense
const now = computed(() => (status.value ? new Date(status.value.now).getTime() : Date.now()))

async function load() {
  try {
    const [s, l] = await Promise.all([loadWorkerStatus(),
      get<{ items: JobRow[]; total: number }>('/admin/jobs' + qs({ status: tab.value, kind: kind.value, page: page.value, page_size: SIZE }))])
    status.value = s
    Object.assign(jobs, l)
    error.value = ''
  } catch (e) { error.value = (e as Error).message }
}
let stop = () => {}
onMounted(() => { stop = poll(load, 10) })
onUnmounted(() => stop())

function pick(s: '' | JobStatus) { tab.value = s; page.value = 1; void load() }
function reload() { page.value = 1; void load() }
function go(p: number) { page.value = p; void load() }

async function act(fn: () => Promise<unknown>) {
  if (busy.value) return
  busy.value = true
  notice.value = ''
  try { await fn(); error.value = '' } catch (e) { error.value = (e as Error).message }
  busy.value = false
  await load()
}
const retry = (j: JobRow) => act(async () => { await post(`/admin/jobs/${j.id}/retry`); detail.value = null })
function discard(j: JobRow) {
  if (!confirm(t('ad.discard_confirm', { id: j.id }))) return
  void act(async () => { await post(`/admin/jobs/${j.id}/discard`); detail.value = null })
}
function retryAll() {
  const n = kind.value ? (status.value?.queue.by_kind.find(k => k.kind === kind.value)?.failed ?? 0) : (status.value?.queue.failed ?? 0)
  if (!confirm(t('ad.retry_all_confirm', { n }))) return
  void act(async () => {
    const out = await post<{ retried: number }>('/admin/jobs/retry-failed', { kind: kind.value })
    notice.value = t('ad.retried_n', { n: out.retried })
  })
}
async function open(j: JobRow) {
  try { detail.value = await get<JobRow>(`/admin/jobs/${j.id}`); error.value = '' } catch (e) { error.value = (e as Error).message }
}

const kindName = (k: string) => (kinds.includes(k) ? t('ad.kind.' + k) : k)
const every = (s: number) => (s >= 60 ? t('ad.n_minutes', { n: Math.round(s / 60) }) : t('ad.n_seconds', { n: s }))
function wait(s: number): string {
  if (s < 60) return t('ad.n_seconds', { n: s })
  if (s < 3600) return t('ad.n_minutes', { n: Math.floor(s / 60) })
  return shown.value === 'zh' ? `${Math.floor(s / 3600)} 小时` : `${Math.floor(s / 3600)} h`
}
// "短信 3 · 菜单导入 1" for the kinds that have any
function split(field: keyof Omit<QueueKind, 'kind'>): string {
  return (status.value?.queue.by_kind ?? []).filter(k => k[field] > 0).map(k => `${kindName(k.kind)} ${k[field]}`).join(' · ')
}
function counted(task: WorkerTask): string {
  return task.task === 'cleanup' || !task.last_started_at ? '' : t('ad.count.' + task.task, { n: task.last_count })
}
const tagClass: Record<JobStatus, string> = { pending: '', running: 'info', done: 'ok', failed: 'bad', discarded: '' }
const mark: Record<JobStatus, string> = { pending: '', running: '', done: '✓ ', failed: '✕ ', discarded: '' }
const anyOnline = computed(() => status.value?.instances.some(i => i.online) ?? true)
</script>

<template>
  <div class="adm-page">
    <div v-if="error" class="banner bad">{{ error }}</div>
    <div v-if="notice" class="banner" data-testid="notice">{{ notice }}</div>
    <div v-if="status && !anyOnline" class="banner bad" data-testid="no-worker">{{ t('ad.no_worker') }}</div>
    <template v-if="status">
      <div class="adm-grid2">
        <div class="card flush">
          <div class="head"><h2>{{ t('ad.instances') }}</h2><span class="small muted">{{ t('ad.offline_hint') }}</span></div>
          <table class="data">
            <thead><tr><th>{{ t('ad.instance') }}</th><th>{{ t('ad.status') }}</th><th>{{ t('ad.started') }}</th><th>{{ t('ad.last_beat') }}</th><th class="right">{{ t('ad.loop_ms') }}</th></tr></thead>
            <tbody>
              <tr v-for="w in status.instances" :key="w.instance" :data-instance="w.online ? 'online' : 'lost'">
                <td><span class="mono">{{ w.instance }}</span><div class="small muted">{{ t(w.embedded ? 'ad.embedded' : 'ad.standalone') }} · {{ w.hostname }}</div></td>
                <td><span class="tag" :class="w.online ? 'ok' : 'bad'">{{ w.online ? '✓ ' + t('ad.online') : '✕ ' + t('ad.lost') }}</span></td>
                <td class="num">{{ fmtDateTime(w.started_at) }}</td>
                <td class="num" :title="w.last_beat_at">{{ w.online ? ago(w.last_beat_at, now) : fmtDateTime(w.last_beat_at) }}</td>
                <td class="right num">{{ w.online ? w.last_loop_ms + ' ms' : '—' }}</td>
              </tr>
              <tr v-if="status.instances.length === 0"><td colspan="5" class="muted">{{ t('ad.empty') }}</td></tr>
            </tbody>
          </table>
        </div>
        <div class="card flush">
          <div class="head"><h2>{{ t('ad.tasks') }}</h2></div>
          <table class="data">
            <thead><tr><th>{{ t('ad.task') }}</th><th>{{ t('ad.every') }}</th><th>{{ t('ad.last_run') }}</th><th class="right">{{ t('ad.duration') }}</th><th>{{ t('ad.result') }}</th></tr></thead>
            <tbody>
              <tr v-for="k in status.tasks" :key="k.task" :data-task="k.task">
                <td>{{ t('ad.task.' + k.task) }}</td>
                <td class="num">{{ every(k.interval_seconds) }}</td>
                <td class="num" :title="k.last_started_at ?? ''">{{ ago(k.last_started_at, now) }}</td>
                <td class="right num">{{ k.last_started_at ? k.last_duration_ms + ' ms' : '—' }}</td>
                <td>
                  <span v-if="!k.last_started_at" class="muted small">{{ t('ad.not_run') }}</span>
                  <template v-else-if="k.ok"><span class="tag ok">✓ {{ t('ad.ok') }}</span> <span class="small muted">{{ counted(k) }}</span></template>
                  <template v-else>
                    <span class="tag bad">✕ {{ t('ad.failed') }}</span> <span class="small muted">{{ t('ad.failures_n', { n: k.failures }) }}</span>
                    <div class="small" style="color:var(--bad);word-break:break-word">{{ k.last_error }}</div>
                  </template>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <div class="adm-grid4 stat">
        <div class="card"><div class="muted small">{{ t('ad.q.pending') }}</div><div class="v" data-testid="q-pending">{{ status.queue.pending }}</div>
          <div class="small muted">{{ status.queue.oldest_wait_seconds > 0 ? t('ad.oldest_wait', { t: wait(status.queue.oldest_wait_seconds) }) : t('ad.none_waiting') }}</div></div>
        <div class="card"><div class="muted small">{{ t('ad.q.running') }}</div><div class="v">{{ status.queue.running }}</div><div class="small muted">{{ split('running') || '—' }}</div></div>
        <div class="card"><div class="muted small">{{ t('ad.q.failed_long') }}</div><div class="v" data-testid="q-failed" :style="status.queue.failed ? 'color:var(--bad)' : ''">{{ status.queue.failed }}</div>
          <div class="small muted">{{ split('failed') || '—' }}</div></div>
        <div class="card"><div class="muted small">{{ t('ad.q.done_24h') }}</div><div class="v">{{ status.queue.done_24h }}</div><div class="small muted">{{ split('done_24h') || '—' }}</div></div>
      </div>

      <div class="card flush">
        <div class="head">
          <div class="row wrap"><h2>{{ t('ad.jobs') }}</h2>
            <div class="tabs">
              <button v-for="s in tabs" :key="s" :class="{ on: tab === s }" :data-tab="s || 'all'" @click="pick(s)">
                {{ s ? t('ad.q.' + s) : t('ad.all') }}<template v-if="s === 'failed' && status.queue.failed"> {{ status.queue.failed }}</template>
              </button>
            </div>
          </div>
          <div class="row">
            <select v-model="kind" style="width:150px" data-testid="job-kind" @change="reload">
              <option value="">{{ t('ad.all_kinds') }}</option>
              <option v-for="k in kinds" :key="k" :value="k">{{ kindName(k) }}</option>
            </select>
            <button class="btn small" :disabled="busy || !status.queue.failed" data-testid="retry-all" @click="retryAll">{{ t('ad.retry_all') }}</button>
          </div>
        </div>
        <table class="data">
          <thead><tr><th>ID</th><th>{{ t('ad.kind') }}</th><th>{{ t('ad.subject') }}</th><th>{{ t('ad.status') }}</th><th class="right">{{ t('ad.attempts') }}</th>
            <th>{{ t('ad.created') }}</th><th>{{ t('ad.last_error') }}</th><th></th></tr></thead>
          <tbody>
            <tr v-for="j in jobs.items" :key="j.id" :data-job="j.id" :data-status="j.status">
              <td class="num">#{{ j.id }}</td>
              <td>{{ kindName(j.kind) }}</td>
              <td class="mono">{{ j.subject || '—' }}</td>
              <td><span class="tag" :class="tagClass[j.status]">{{ mark[j.status] }}{{ t('ad.q.' + j.status) }}</span></td>
              <td class="right num">{{ j.attempts }} / {{ j.max_attempts }}</td>
              <td class="num" :title="j.created_at">{{ fmtDateTime(j.created_at) }}</td>
              <td class="small muted" style="max-width:360px;word-break:break-word">{{ j.last_error }}</td>
              <td><div class="acts">
                <button class="btn small" data-act="details" @click="open(j)">{{ t('ad.details') }}</button>
                <button v-if="j.status === 'failed' || j.status === 'discarded'" class="btn small" :disabled="busy" data-act="retry" @click="retry(j)">{{ t('ad.retry') }}</button>
                <button v-if="j.status === 'failed' || j.status === 'pending'" class="btn small danger" :disabled="busy" data-act="discard" @click="discard(j)">{{ t('ad.discard') }}</button>
              </div></td>
            </tr>
            <tr v-if="jobs.items.length === 0"><td colspan="8" class="muted">{{ t('ad.empty') }}</td></tr>
          </tbody>
        </table>
      </div>
      <Pager :total="jobs.total" :page="page" :size="SIZE" @go="go"><span>{{ t('ad.auto_refresh') }}</span></Pager>
      <p class="small muted">{{ t('ad.jobs_foot') }}</p>
    </template>

    <template v-if="detail">
      <div class="adm-mask" @click="detail = null"></div>
      <div class="adm-drawer" data-testid="job-detail">
        <div class="dh"><h2>{{ t('ad.job_n', { id: detail.id }) }}</h2></div>
        <div class="db">
          <dl class="kv">
            <dt>{{ t('ad.kind') }}</dt><dd>{{ kindName(detail.kind) }} <span class="mono muted">{{ detail.kind }}</span></dd>
            <dt>{{ t('ad.subject') }}</dt><dd class="mono">{{ detail.subject || '—' }}</dd>
            <dt>{{ t('ad.status') }}</dt><dd><span class="tag" :class="tagClass[detail.status]">{{ mark[detail.status] }}{{ t('ad.q.' + detail.status) }}</span></dd>
            <dt>{{ t('ad.attempts') }}</dt><dd class="num">{{ detail.attempts }} / {{ detail.max_attempts }}</dd>
            <dt>{{ t('ad.created') }}</dt><dd class="num">{{ fmtDateTime(detail.created_at) }}{{ fmtTime(detail.created_at).slice(5) }}</dd>
            <template v-if="detail.status === 'pending'"><dt>{{ t('ad.next_run') }}</dt><dd class="num">{{ fmtDateTime(detail.run_at) }}{{ fmtTime(detail.run_at).slice(5) }}</dd></template>
            <template v-if="detail.finished_at"><dt>{{ t('ad.finished') }}</dt><dd class="num">{{ fmtDateTime(detail.finished_at) }}{{ fmtTime(detail.finished_at).slice(5) }}</dd></template>
            <template v-if="detail.last_error"><dt>{{ t('ad.last_error') }}</dt><dd style="color:var(--bad);word-break:break-word">{{ detail.last_error }}</dd></template>
            <template v-if="detail.message"><dt>{{ t('ad.message') }}</dt><dd data-testid="job-message" style="word-break:break-word">{{ detail.message }}</dd></template>
          </dl>
        </div>
        <div class="df">
          <button class="btn" @click="detail = null">{{ t('ad.close') }}</button>
          <button v-if="detail.status === 'failed' || detail.status === 'pending'" class="btn danger" :disabled="busy" @click="discard(detail)">{{ t('ad.discard') }}</button>
          <button v-if="detail.status === 'failed' || detail.status === 'discarded'" class="btn primary" :disabled="busy" @click="retry(detail)">{{ t('ad.retry') }}</button>
        </div>
      </div>
    </template>
  </div>
</template>
