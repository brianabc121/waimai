import { reactive } from 'vue'
import { get, post } from './api'

// The logged-in customer (a phone number confirmed by a texted code). One
// account works in every store.
export const customer = reactive<{ loaded: boolean; verified: boolean; phone: string }>({ loaded: false, verified: false, phone: '' })

export async function loadCustomer(force = false): Promise<boolean> {
  if (!customer.loaded || force) {
    const s = await get<{ verified: boolean; phone?: string }>('/customer/session').catch(() => ({ verified: false, phone: '' }))
    Object.assign(customer, { loaded: true, verified: s.verified, phone: s.phone ?? '' })
  }
  return customer.verified
}

export async function customerLogout() {
  await post('/customer/logout').catch(() => null)
  Object.assign(customer, { loaded: true, verified: false, phone: '' })
}

// Where to send someone who has to log in first; they come back to `next`.
export function loginRoute(next: string, slug?: string) {
  return { path: '/account/login', query: { next, ...(slug ? { store: slug } : {}) } }
}

export { prettyPhone } from './dial'
