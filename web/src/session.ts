import { reactive } from 'vue'
import { get, post } from './api'
import type { BranchRole, User } from './types'

// A platform administrator can open one store's back office read-only. The
// store is remembered here and shown to the staff screens as if it were a role.
const VIEW_KEY = 'admin_view'

export function adminView(): BranchRole | null {
  try { return JSON.parse(localStorage.getItem(VIEW_KEY) || 'null') } catch { return null }
}

export function setAdminView(b: { id: number; name: string; slug: string; currency: string } | null) {
  if (!b) { localStorage.removeItem(VIEW_KEY); return }
  const role: BranchRole = { branch_id: b.id, branch_name: b.name, slug: b.slug, currency: b.currency, role: 'owner', posts: [] }
  localStorage.setItem(VIEW_KEY, JSON.stringify(role))
  localStorage.setItem('branch_id', String(b.id))
  session.loaded = false
}

function applyAdminView() {
  const v = session.user?.is_platform_admin ? adminView() : null
  if (v && session.user) { session.user.roles = [v]; session.branchId = v.branch_id }
}

export const session = reactive<{ user: User | null; loaded: boolean; branchId: number | null }>({
  user: null, loaded: false, branchId: Number(localStorage.getItem('branch_id')) || null,
})

export async function loadSession(): Promise<User | null> {
  if (!session.loaded) {
    try { session.user = await get<User>('/auth/me') } catch { session.user = null }
    session.loaded = true
    applyAdminView()
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
  localStorage.removeItem(VIEW_KEY)
  session.user = await post<User>('/auth/login', { username, password })
  session.loaded = true
  pickBranch()
}

export async function logout() {
  await post('/auth/logout').catch(() => {})
  localStorage.removeItem(VIEW_KEY)
  session.user = null
}

// Where each role lands after signing in.
export function homeFor(u: User): string {
  if (u.is_platform_admin) return '/admin'
  const role = u.roles.find(r => r.branch_id === session.branchId)?.role ?? u.roles[0]?.role
  return role === 'staff' ? '/work' : '/merchant/orders'
}

// The identities a person can work under in the current branch, from their posts:
// "store" for a station or the packing counter, "rider" for deliveries.
export type Identity = 'store' | 'rider'
export function identities(): Identity[] {
  const posts = currentRole()?.posts ?? []
  const out: Identity[] = []
  if (posts.some(p => p.kind !== 'rider')) out.push('store')
  if (posts.some(p => p.kind === 'rider')) out.push('rider')
  return out
}

// Re-read the signed-in user, e.g. after a manager changed their posts.
export async function reloadUser() {
  if (session.user?.is_platform_admin) return
  try { session.user = await get<User>('/auth/me'); pickBranch() } catch { /* keep what we have */ }
}
