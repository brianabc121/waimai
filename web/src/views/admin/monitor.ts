// What the sidebar shows next to the monitoring links. Pages that load the
// same numbers anyway update it, so the badge never lags behind the page.
import { reactive } from 'vue'
import { get } from '../../api'
import type { WorkerStatus } from './types'

export const monitor = reactive({ failedJobs: 0, supportUnread: 0, newApplications: 0, walletProblems: 0 })

// loadNewApplications counts the applications to join nobody has contacted
// yet (most come from the company website), for the "Customers" link.
export async function loadNewApplications() {
  monitor.newApplications = (await get<{ new: number }>('/admin/applications?status=pending&page_size=1')).new
}

// loadSupportUnread keeps the number on "platform support" current.
export async function loadSupportUnread() {
  monitor.supportUnread = (await get<{ unread: number }>('/admin/support/unread')).unread
}

// loadWalletCheck keeps the number of balance-book problems on "Ledger":
// anything but 0 means the books need looking at now.
export async function loadWalletCheck() {
  const r = await get<{ last: { problems: string[] | null } | null }>('/admin/wallet-check')
  monitor.walletProblems = r.last?.problems?.length ?? 0
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
