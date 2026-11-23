// Thin fetch wrapper. Every write carries X-Requested-With (the API's CSRF check).
import { extend, t } from './i18n'
import { checkZawgyi } from './zawgyi'

extend({
  zawgyi_confirm: ['这段缅甸文看起来是旧的 Zawgyi 编码：“{text}”。其他手机上会显示成乱码，翻译也会出错。建议换成缅甸文 Unicode 输入法重新输入。\n\n点“确定”照样保存，点“取消”回去修改。',
    'This Burmese looks like it was typed in the old Zawgyi encoding: "{text}". It shows as gibberish on other phones and cannot be translated. Please type it again with a Unicode Burmese keyboard.\n\nOK saves it anyway; Cancel goes back.'],
  zawgyi_stopped: ['已取消保存：请用 Unicode 输入法重新输入缅甸文。', 'Not saved: please type the Burmese again with a Unicode keyboard.'],
})

export class ApiError extends Error {
  constructor(public status: number, public code: string, message: string, public details?: any) {
    super(message)
  }
}

export async function api<T = any>(method: string, path: string, body?: unknown, headers: Record<string, string> = {}): Promise<T> {
  if (method !== 'GET' && body !== undefined &&
    !checkZawgyi(path, body, found => window.confirm(t('zawgyi_confirm', { text: found.slice(0, 40) })))) {
    throw new ApiError(0, 'ZAWGYI', t('zawgyi_stopped'))
  }
  let res: Response
  try {
    res = await fetch('/api/v1' + path, {
      method,
      credentials: 'same-origin',
      headers: { 'X-Requested-With': 'waimai-web', ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}), ...headers },
      body: body !== undefined ? JSON.stringify(body) : undefined,
    })
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
      headers: { 'X-Requested-With': 'waimai-web', 'Content-Type': 'application/octet-stream' }, body: file })
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
