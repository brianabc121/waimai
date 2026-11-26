// The mall or a store's own page (docs/ORDER_TYPES_DESIGN.md §5). One build
// serves both: the server tells from the address which one this is. On a
// store's own page (67891.abcfood.com, or the merchant's domain) the customer
// sees that store only, with no way into the mall or to other stores.
import { reactive } from 'vue'
import { get } from './api'
import { extend } from './i18n'

extend({
  'site.order': ['点餐', 'Order'],
  'sl.title': ['店铺链接', 'Store link'],
  'site.powered': ['由河马点餐系统提供', 'Powered by Hippo Order'],
})

export interface SiteStore { slug: string; store_no: number; name: string; name_en: string }
export interface SiteInfo { mode: 'mall' | 'web'; store?: SiteStore; store_domain: string }

export const site = reactive<SiteInfo>({ mode: 'mall', store_domain: '' })

// loadSite asks once, before the first page: a failure leaves the mall.
export async function loadSite() {
  try { Object.assign(site, await get<SiteInfo>('/site')) } catch { /* the mall */ }
}

export const ownSite = () => site.mode === 'web' && !!site.store
export const ownSlug = () => site.store?.slug ?? ''

// The store page and its checkout: the root of the store's own page, or the
// store inside the mall.
export const storePath = (slug: string) => ownSite() ? '/' : `/mall/s/${slug}`
export const checkoutPath = (slug: string) => ownSite() ? '/checkout' : `/mall/s/${slug}/checkout`

// storeURL is the address a merchant shares: the store's own page. On a
// development machine without the platform's domain, <no>.localhost works in
// the browser; otherwise the old link, which leads to the store in the mall.
export function storeURL(b: { store_no?: number; slug: string }): string {
  if (b.store_no && site.store_domain) return `https://${b.store_no}.${site.store_domain}`
  if (b.store_no && /^(localhost|127\.0\.0\.1)$/.test(location.hostname)) {
    return `${location.protocol}//${b.store_no}.localhost${location.port ? ':' + location.port : ''}`
  }
  return `${location.origin}/s/${b.slug}`
}

// The customer's pages of the platform rather than of one store, which a
// store's own page does not have; the messages there are the chat with the
// store.
const mallOnly = /^\/(mall(\/|$)|s\/|me\/(favorites|wallet|support|reviews|apply|about|feedback|help)(\/|$))/
export function ownSiteRedirect(path: string): string | null {
  if (!ownSite()) return null
  if (path === '/me/messages') return `/chat/${ownSlug()}`
  if (path === '/checkout') return null
  if (mallOnly.test(path)) return '/'
  if (path.startsWith('/chat/') && path !== `/chat/${ownSlug()}`) return '/'
  return null
}
export const onOwnSite = (path: string) => !ownSite() || ownSiteRedirect(path) === null
