import { reactive } from 'vue'
import { ApiError, apiScope, get, post } from './api'
import type { BranchRole, Entry, User } from './types'
import { signOut } from './retry'
import { t } from './i18n'

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
  const v = session.user?.entry === 'admin' ? adminView() : null
  if (v && session.user) { session.user.roles = [v]; session.branchId = v.branch_id }
}

export const session = reactive<{ user: User | null; loaded: boolean; branchId: number | null }>({
  user: null, loaded: false, branchId: Number(localStorage.getItem('branch_id')) || null,
})

const USER_KEY = 'session_user'
function remember() { if (session.user) localStorage.setItem(USER_KEY, JSON.stringify(session.user)) }
function remembered(): User | null { try { return JSON.parse(localStorage.getItem(USER_KEY) || 'null') } catch { return null } }

export async function loadSession(): Promise<User | null> {
  if (!session.loaded) {
    try { session.user = await get<User>('/auth/me'); remember() } catch (e) {
      // Without signal the workbench still opens as the person who last used
      // this phone; the server checks who they are on every request anyway.
      session.user = e instanceof ApiError && e.code === 'NETWORK' ? remembered() : null
      if (!session.user) localStorage.removeItem(USER_KEY)
    }
    session.loaded = true
    applyAdminView()
    pickBranch()
  }
  return session.user
}

// The stores where this person has a back-office role.
export const officeRoles = (u: User | null = session.user) => (u?.roles ?? []).filter(r => r.role !== 'staff')

// In the back office the current store is one where the person has a role there.
function pickBranch() {
  const all = session.user?.roles ?? []
  const roles = session.user?.entry === 'office' && officeRoles().length ? officeRoles() : all
  if (!roles.some(r => r.branch_id === session.branchId)) session.branchId = roles[0]?.branch_id ?? null
}

// Which sign-in a page is under: the platform's pages and its read-only view
// of a store use the platform's own (docs/STAFF_ACCESS_DESIGN.md §5.5).
export function scopeFor(path: string): 'admin' | 'staff' {
  if (path === '/admin' || path.startsWith('/admin/')) return 'admin'
  if (path.startsWith('/login')) return 'staff'
  return adminView() ? 'admin' : 'staff'
}
export function useScope(scope: 'admin' | 'staff') {
  if (apiScope.admin === (scope === 'admin')) return
  apiScope.admin = scope === 'admin'
  session.user = null
  session.loaded = false
}

export function setBranch(id: number) {
  session.branchId = id
  localStorage.setItem('branch_id', String(id))
}

export function currentRole() {
  return session.user?.roles.find(r => r.branch_id === session.branchId) ?? null
}

// can tells whether the signed-in person may do something in the current
// store (docs/STAFF_ACCESS_DESIGN.md §6). The platform's read-only view of a
// store sees every page; the server still refuses its changes.
export function can(...perms: string[]): boolean {
  const r = currentRole()
  if (!r) return false
  if (session.user?.is_platform_admin && adminView()) return true
  if (r.role === 'owner') return true
  return perms.some(p => p === 'kitchen' ? r.posts.some(x => x.kind !== 'rider') : (r.perms ?? []).includes(p))
}
export const isOwner = () => currentRole()?.role === 'owner' || (!!session.user?.is_platform_admin && !!adminView())

// The tab last used on the sign-in page of this device.
export const ENTRY_KEY = 'login_entry'

// login signs in to the store's back office or the staff workbench. Someone
// without a back-office role who asks for the back office gets the workbench.
export async function login(username: string, password: string, entry: 'office' | 'work' = 'office') {
  localStorage.removeItem(VIEW_KEY)
  useScope('staff')
  session.user = await post<User>('/auth/login', { username, password, entry })
  localStorage.setItem(ENTRY_KEY, entry)
  remember()
  session.loaded = true
  pickBranch()
}

// The platform's own sign-in.
export async function adminLogin(username: string, password: string) {
  localStorage.removeItem(VIEW_KEY)
  useScope('admin')
  session.user = await post<User>('/admin/auth/login', { username, password })
  session.loaded = true
}

// switchEntry moves between the back office (asks for the password again
// when coming from the workbench) and the workbench.
export async function switchEntry(entry: 'office' | 'work', password = '') {
  session.user = await post<User>('/auth/entry', { entry, password })
  remember()
  pickBranch()
}

// logout answers whether the person is signed out. When the request was
// lost or the server failed, the session still works: nothing is forgotten
// here, the person is told, and can try again (CODE_REVIEW_FIX_PLAN M15).
export async function logout(): Promise<boolean> {
  if (!(await signOut(() => post(apiScope.admin ? '/admin/auth/logout' : '/auth/logout')))) {
    window.alert(t('logout_failed'))
    return false
  }
  localStorage.removeItem(VIEW_KEY)
  localStorage.removeItem(USER_KEY)
  session.user = null
  return true
}

// Where a sign-in lands: the workbench, the one store's back office, or the
// page that asks which store and role when there are several.
export function homeFor(u: User): string {
  const entry: Entry = u.entry ?? (u.is_platform_admin ? 'admin' : 'office')
  if (entry === 'admin') return '/admin'
  if (entry === 'work' || !officeRoles(u).length) return '/work'
  return officeRoles(u).length > 1 ? '/login/pick' : '/merchant/orders'
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
  try { session.user = await get<User>('/auth/me'); pickBranch(); remember() } catch { /* keep what we have */ }
}
