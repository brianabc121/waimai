import { reactive } from 'vue'
import { get, post } from './api'
import type { User } from './types'

export const session = reactive<{ user: User | null; loaded: boolean; branchId: number | null }>({
  user: null, loaded: false, branchId: Number(localStorage.getItem('branch_id')) || null,
})

export async function loadSession(): Promise<User | null> {
  if (!session.loaded) {
    try { session.user = await get<User>('/auth/me') } catch { session.user = null }
    session.loaded = true
    pickBranch()
  }
  return session.user
}

function pickBranch() {
  const roles = session.user?.roles ?? []
  if (!roles.some(r => r.branch_id === session.branchId)) session.branchId = roles[0]?.branch_id ?? null
}

export function setBranch(id: number) {
  session.branchId = id
  localStorage.setItem('branch_id', String(id))
}

export function currentRole() {
  return session.user?.roles.find(r => r.branch_id === session.branchId) ?? null
}

export async function login(username: string, password: string) {
  session.user = await post<User>('/auth/login', { username, password })
  session.loaded = true
  pickBranch()
}

export async function logout() {
  await post('/auth/logout').catch(() => {})
  session.user = null
}

// Where each role lands after signing in.
export function homeFor(u: User): string {
  if (u.is_platform_admin) return '/admin'
  const role = u.roles.find(r => r.branch_id === session.branchId)?.role ?? u.roles[0]?.role
  if (role === 'rider') return '/rider'
  if (role === 'kitchen') return '/kitchen'
  return '/merchant/orders'
}
