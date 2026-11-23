import { reactive } from 'vue'
import { get, post, put } from './api'
import { chosen, locale, onLocaleChosen, setLocale } from './i18n'
import { isLocale } from './langs'

// The logged-in customer (a phone number confirmed by a texted code). One
// account works in every store.
export const customer = reactive<{ loaded: boolean; verified: boolean; phone: string }>({ loaded: false, verified: false, phone: '' })

export async function loadCustomer(force = false): Promise<boolean> {
  if (!customer.loaded || force) {
    const s = await get<{ verified: boolean; phone?: string; lang?: string }>('/customer/session').catch(() => ({ verified: false, phone: '', lang: '' }))
    Object.assign(customer, { loaded: true, verified: s.verified, phone: s.phone ?? '' })
    // A new phone or browser opens in the language the account chose, unless
    // this browser already has a choice of its own; a choice made here before
    // signing in goes to an account that has none.
    if (s.verified && !chosen.value && isLocale(s.lang)) void setLocale(s.lang)
    else if (s.verified && chosen.value && !s.lang) void put('/customer/lang', { lang: locale.value }).catch(() => null)
  }
  return customer.verified
}

// The signed-in customer's account remembers what they pick.
onLocaleChosen(l => {
  if (customer.verified) void put('/customer/lang', { lang: l ?? '' }).catch(() => null)
})

export async function customerLogout() {
  await post('/customer/logout').catch(() => null)
  Object.assign(customer, { loaded: true, verified: false, phone: '' })
}

// Where to send someone who has to log in first; they come back to `next`.
export function loginRoute(next: string, slug?: string) {
  return { path: '/account/login', query: { next, ...(slug ? { store: slug } : {}) } }
}

export { prettyPhone } from './dial'
