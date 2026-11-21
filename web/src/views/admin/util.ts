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

// ms shows a duration: "182 ms", "1.9 s".
export const ms = (v: number) => (v >= 1000 ? `${(v / 1000).toFixed(1)} s` : `${Math.round(v)} ms`)
// pct shows a share between 0 and 1: "0.12%", "3.0%", "12%".
export function pct(v: number): string {
  const p = v * 100
  return `${p === 0 ? '0' : p < 1 ? p.toFixed(2) : p < 10 ? p.toFixed(1) : p.toFixed(0)}%`
}
export const count = (v: number) => Math.round(v).toLocaleString('en-US')
// rate shows requests per minute: whole numbers once they are big enough to read that way.
export const rate = (v: number) => (v >= 10 || v === 0 ? count(v) : v.toFixed(1))

// Chart colours, checked for colour-blind separation: 2xx, 4xx, 5xx and the two latency lines.
export const COLORS = { ok: '#2a78d6', c4: '#e0a100', c5: '#d03b2b', p50: '#2a78d6', p95: '#d9480f' }
