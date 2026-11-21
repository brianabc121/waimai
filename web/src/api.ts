// Thin fetch wrapper. Every write carries X-Requested-With (the API's CSRF check).

export class ApiError extends Error {
  constructor(public status: number, public code: string, message: string, public details?: any) {
    super(message)
  }
}

export async function api<T = any>(method: string, path: string, body?: unknown, headers: Record<string, string> = {}): Promise<T> {
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
export async function upload<T = any>(path: string, file: File): Promise<T> {
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
