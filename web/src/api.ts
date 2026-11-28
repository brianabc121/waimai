// Thin fetch wrapper. Every write carries X-Requested-With (the API's CSRF check).
import { extend, t } from './i18n'
import { fixZawgyi, notice } from './zawgyi'

extend({
  'lim.order': ['“{name}”每单限购 {n} 件', '"{name}": at most {n} per order'],
  'lim.day': ['“{name}”每人每天限购 {n} 件，今天还可以买 {left} 件', '"{name}": at most {n} per person per day; {left} more today'],
  zawgyi_converted: ['检测到旧的 Zawgyi 编码缅甸文，已自动转换成 Unicode 再保存：“{text}”', 'Burmese in the old Zawgyi encoding was converted to Unicode before saving: "{text}"'],
})

export class ApiError extends Error {
  constructor(public status: number, public code: string, message: string, public details?: any) {
    super(message)
  }
}

// The platform's back office signs in apart from the stores
// (docs/STAFF_ACCESS_DESIGN.md §5.5); its requests say so.
export const apiScope = { admin: false }

export async function api<T = any>(method: string, path: string, body?: unknown, headers: Record<string, string> = {}): Promise<T> {
  if (method !== 'GET' && body !== undefined) {
    const z = await fixZawgyi(path, body)
    body = z.body
    if (z.converted.length) notice(t('zawgyi_converted', { text: z.converted[0].slice(0, 40) }))
  }
  let res: Response
  try {
    res = await fetch('/api/v1' + path, {
      method,
      credentials: 'same-origin',
      headers: { 'X-Requested-With': 'waimai-web', 'X-Session': apiScope.admin ? 'admin' : 'staff', ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}), ...headers },
      body: body !== undefined ? JSON.stringify(body) : undefined,
    })
  } catch {
    throw new ApiError(0, 'NETWORK', 'network error')
  }
  const data = await res.json().catch(() => null)
  if (!res.ok) {
    const e = data?.error ?? {}
    throw new ApiError(res.status, e.code ?? 'UNKNOWN', limitText(e) ?? e.message ?? res.statusText, e.details)
  }
  return data as T
}

// limitText is what a purchase limit that refused the order says (ITEM_LIMIT),
// so that every page shows it without a case of its own.
function limitText(e: { code?: string; details?: any }): string | undefined {
  if (e.code !== 'ITEM_LIMIT' || !e.details) return undefined
  const d = e.details
  return t(d.kind === 'day' ? 'lim.day' : 'lim.order', { name: d.name ?? '', n: d.limit ?? 0, left: d.left ?? 0 })
}

export const get = <T = any>(path: string) => api<T>('GET', path)
export const post = <T = any>(path: string, body: unknown = {}, headers?: Record<string, string>) => api<T>('POST', path, body, headers)
export const put = <T = any>(path: string, body: unknown) => api<T>('PUT', path, body)
export const patch = <T = any>(path: string, body: unknown) => api<T>('PATCH', path, body)
export const del = <T = any>(path: string, body?: unknown) => api<T>('DELETE', path, body)

// upload sends a file as the raw request body (the API reads it with a size cap).
export async function upload<T = any>(path: string, file: Blob): Promise<T> {
  let res: Response
  try {
    res = await fetch('/api/v1' + path, { method: 'POST', credentials: 'same-origin',
      headers: { 'X-Requested-With': 'waimai-web', 'X-Session': apiScope.admin ? 'admin' : 'staff', 'Content-Type': 'application/octet-stream' }, body: file })
  } catch {
    throw new ApiError(0, 'NETWORK', 'network error')
  }
  const data = await res.json().catch(() => null)
  if (!res.ok) {
    const e = data?.error ?? {}
    throw new ApiError(res.status, e.code ?? 'UNKNOWN', e.message ?? res.statusText, e.details)
  }
  return data as T
}

export function newKey(): string {
  return crypto.randomUUID ? crypto.randomUUID() : String(Date.now()) + Math.random().toString(16).slice(2)
}
