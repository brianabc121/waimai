import { locale } from '../../i18n'

const pad = (n: number) => String(n).padStart(2, '0')

export function fmtDate(iso: string | null): string {
  if (!iso) return '—'
  const d = new Date(iso)
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

export function fmtDateTime(iso: string | null): string {
  if (!iso) return '—'
  const d = new Date(iso)
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

export function fmtTime(iso: string | null): string {
  if (!iso) return '—'
  const d = new Date(iso)
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

// ago is a short relative time: "3 秒前", "6 分钟前"; older than a day falls back to the date and time.
export function ago(iso: string | null, now = Date.now()): string {
  if (!iso) return '—'
  const s = Math.max(0, Math.round((now - new Date(iso).getTime()) / 1000))
  const zh = locale.value === 'zh'
  if (s < 60) return zh ? `${s} 秒前` : `${s}s ago`
  if (s < 3600) return zh ? `${Math.floor(s / 60)} 分钟前` : `${Math.floor(s / 60)}m ago`
  if (s < 86400) return zh ? `${Math.floor(s / 3600)} 小时前` : `${Math.floor(s / 3600)}h ago`
  return fmtDateTime(iso)
}

export const pages = (total: number, size: number) => Math.max(1, Math.ceil(total / size))

export function qs(params: Record<string, string | number | null | undefined>): string {
  const u = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) if (v !== null && v !== undefined && v !== '' && v !== 0) u.set(k, String(v))
  const s = u.toString()
  return s ? '?' + s : ''
}
