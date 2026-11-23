// What the sidebar shows next to the monitoring links. Pages that load the
// same numbers anyway update it, so the badge never lags behind the page.
import { reactive } from 'vue'
import { get } from '../../api'
import type { WorkerStatus } from './types'

export const monitor = reactive({ failedJobs: 0, supportUnread: 0 })

// loadSupportUnread keeps the number on "platform support" current.
export async function loadSupportUnread() {
  monitor.supportUnread = (await get<{ unread: number }>('/admin/support/unread')).unread
}

export async function loadWorkerStatus(): Promise<WorkerStatus> {
  const s = await get<WorkerStatus>('/admin/worker')
  monitor.failedJobs = s.queue.failed
  return s
}

// poll calls fn now and then every `seconds`, skipping turns while the tab is
// hidden and catching up as soon as it is shown again. Returns the stop function.
export function poll(fn: () => unknown, seconds: number): () => void {
  const run = () => { if (!document.hidden) void fn() }
  const timer = window.setInterval(run, seconds * 1000)
  document.addEventListener('visibilitychange', run)
  run()
  return () => { window.clearInterval(timer); document.removeEventListener('visibilitychange', run) }
}
