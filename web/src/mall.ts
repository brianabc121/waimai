// The marketplace: where the customer is, and the stores that deliver there.
import { reactive } from 'vue'
import { get } from './api'
import { listAddresses, type Address } from './addresses'
import { loadCustomer } from './customer'
import { extend, pick, t } from './i18n'
import { mapsKey, placeName } from './maps'

export interface MallCategory { id: number; code: string; name: string; name_en: string; icon: string; sort: number; active: boolean }
export interface MallBanner { id: number; title: string; title_en: string; subtitle: string; subtitle_en: string; link_slug: string; link_url: string; image_url: string; sort: number; active: boolean }
export interface MallHome { name: string; categories: MallCategory[]; banners: MallBanner[] }
export interface MallPromo { kind: string; threshold: number; value: number; max_discount: number | null; name: string; name_en: string }
export interface MallDish { name: string; name_en: string; price: number; image_url: string }
export interface MallStore {
  slug: string; name: string; name_en: string; logo_url: string; cover_url: string; tagline: string; categories: string[]
  currency: string; city: string; district: string; rating: number | null; monthly_sales: number
  status: 'open' | 'closed' | 'paused'; opens_at: string; opens_tomorrow: boolean; scheduling: boolean
  delivery: { fee: number; min_order: number; eta_minutes: number | null } | null; pickup: boolean
  min_order_from: number; distance_m: number | null; promos: MallPromo[]; dishes: MallDish[]
}
export interface RegionRef { country_code: string; country?: string; province?: string; city: string; district: string }
export interface MallList { region: RegionRef | null; stores: MallStore[]; pickup: MallStore[]; frequent: string[] }
export interface MallCountry { country_code: string; country: string; stores: number
  cities: { province: string; city: string; stores: number; districts: { district: string; stores: number }[] }[] }

// Where the customer is. `gps` is what the device says; `address` one of
// their saved addresses; `place` a spot they searched or pinned; `region` a
// city they browse without giving a spot. `manual` is set when they chose it
// themselves: such a choice is kept until they ask to be located again.
export interface MallLoc {
  source: 'gps' | 'address' | 'place' | 'region'
  lat?: number; lng?: number; name: string; address_id?: number
  region?: RegionRef; manual: boolean
}

const KEY = 'mall_loc'
function read(): MallLoc | null {
  try {
    const l = JSON.parse(localStorage.getItem(KEY) || 'null')
    return l && typeof l.source === 'string' && (l.source === 'region' ? !!l.region : typeof l.lat === 'number' && typeof l.lng === 'number') ? l : null
  } catch { return null }
}

// locating: asking the device; denied: it would not say, and nothing else was known.
export const mall = reactive<{ loc: MallLoc | null; locating: boolean; denied: boolean }>({ loc: read(), locating: false, denied: false })

export function setLoc(l: MallLoc | null) {
  mall.loc = l
  if (l) { localStorage.setItem(KEY, JSON.stringify(l)); mall.denied = false } else localStorage.removeItem(KEY)
}
// The spot a new address or a map should start from, if one is known.
export const mallSpot = (): { lat: number; lng: number } | null => {
  const l = read()
  return l && l.lat != null && l.lng != null ? { lat: l.lat, lng: l.lng } : null
}
// The saved address the customer chose in the marketplace, if that is what they chose.
export const mallAddressId = (): number | null => { const l = read(); return l?.source === 'address' ? l.address_id ?? null : null }

function position(): Promise<{ lat: number; lng: number }> {
  return new Promise((resolve, reject) => {
    if (!navigator.geolocation) { reject(new Error('unsupported')); return }
    navigator.geolocation.getCurrentPosition(p => resolve({ lat: p.coords.latitude, lng: p.coords.longitude }), reject,
      { enableHighAccuracy: false, timeout: 8000, maximumAge: 5 * 60 * 1000 })
  })
}

export const fromAddress = (a: Address, manual: boolean): MallLoc =>
  ({ source: 'address', lat: a.lat!, lng: a.lng!, name: a.place_name, address_id: a.id, manual })

// The name of a located spot: the street the map knows there, when there is a
// map; otherwise it is named after the region the server finds (regionName).
async function nameOf(lat: number, lng: number): Promise<string> {
  const key = await mapsKey()
  return key ? placeName(key, [lat, lng]).catch(() => '') : ''
}
export const regionName = (r: RegionRef | null | undefined) => r ? [r.district, r.city].filter(Boolean).join(', ') || r.country || '' : ''

// locate asks the device where it is. When it will not say: someone logged in
// goes to their default address; anyone else keeps what was known before, or
// is asked to choose (mall.denied).
export async function locate(): Promise<void> {
  mall.locating = true
  try {
    const p = await position()
    const before = mall.loc
    // The same spot as last time keeps its name while the new one is looked up.
    const same = before?.source === 'gps' && Math.abs(before.lat! - p.lat) < 0.0005 && Math.abs(before.lng! - p.lng) < 0.0005
    setLoc({ source: 'gps', lat: p.lat, lng: p.lng, name: same ? before!.name : '', manual: false })
    void nameOf(p.lat, p.lng).then(name => { if (name && mall.loc?.source === 'gps' && mall.loc.lat === p.lat) setLoc({ ...mall.loc, name }) })
  } catch {
    const fallback = await defaultAddress()
    if (fallback) setLoc(fromAddress(fallback, false))
    else if (!mall.loc) mall.denied = true
  } finally { mall.locating = false }
}

async function defaultAddress(): Promise<Address | null> {
  if (!(await loadCustomer().catch(() => false))) return null
  const r = await listAddresses().catch(() => null)
  const usable = (r?.addresses ?? []).filter(a => a.lat != null && a.lng != null)
  return usable.find(a => a.is_default) ?? usable[0] ?? null
}

// start is what opening the marketplace does: keep a place the customer chose,
// otherwise find out where they are.
export async function start(): Promise<void> {
  if (mall.loc?.manual) return
  await locate()
}

export const mallHome = () => get<MallHome>('/mall/home')
export const mallRegions = () => get<{ countries: MallCountry[] }>('/mall/regions').then(r => r.countries)
export interface MallAsk { category?: string; q?: string; sort?: string; filters?: string[] }
export function mallStores(loc: MallLoc, ask: MallAsk = {}): Promise<MallList> {
  const p = new URLSearchParams()
  if (loc.source === 'region' && loc.region) {
    p.set('country', loc.region.country_code); p.set('city', loc.region.city)
    if (loc.region.district) p.set('district', loc.region.district)
  } else { p.set('lat', String(loc.lat)); p.set('lng', String(loc.lng)) }
  if (ask.category) p.set('category', ask.category)
  if (ask.q) p.set('q', ask.q)
  if (ask.sort) p.set('sort', ask.sort)
  if (ask.filters?.length) p.set('filter', ask.filters.join(','))
  return get<MallList>('/mall/stores?' + p.toString())
}

// How many dishes wait in this device's cart for a store.
export function cartCount(slug: string): number {
  try {
    const c = JSON.parse(localStorage.getItem('cart:' + slug) || 'null')
    return Array.isArray(c?.lines) ? c.lines.reduce((n: number, l: { qty?: number }) => n + (Number(l.qty) || 0), 0) : 0
  } catch { return 0 }
}

export const distanceText = (m: number) => m < 1000 ? `${Math.max(10, Math.round(m / 10) * 10)} m` : `${(m / 1000).toFixed(1)} km`
export const catName = (c: MallCategory) => pick(c.name, c.name_en)
export const mallName = (h: MallHome | null) => h?.name || t('ml.brand')

extend({
  'ml.brand': ['外卖商城', 'Food Market'], 'ml.search_ph': ['搜索店铺或菜品', 'Search stores or dishes'], 'ml.search': ['搜索', 'Search'],
  'ml.messages': ['消息', 'Messages'], 'ml.locating': ['正在定位…', 'Finding you…'], 'ml.choose_loc': ['选择位置', 'Choose a place'],
  'ml.here': ['当前位置', 'Current location'], 'ml.all': ['全部', 'All'],
  'ml.frequent': ['常点的店', 'Your stores'], 'ml.nearby': ['附近的店', 'Stores near you'], 'ml.nearby_n': ['共 {n} 家 · 都能送到这个位置', '{n} deliver here'],
  'ml.in_city': ['{city}的店', 'Stores in {city}'], 'ml.in_city_n': ['共 {n} 家', '{n} stores'],
  'ml.region_note': ['这里列的是开在这个城市的店。能不能送到你家，要选了收货地址才知道。', 'These are the stores in this city. Whether one delivers to you is known once you choose an address.'],
  'ml.pickup_title': ['附近可自取', 'Pickup nearby'], 'ml.pickup_hint': ['不送到这里，可以到店取', 'No delivery here; collect at the store'],
  'ml.sort_default': ['综合', 'Best match'], 'ml.sort_distance': ['距离近', 'Nearest'], 'ml.sort_rating': ['评分高', 'Top rated'], 'ml.sort_fee': ['配送费低', 'Lowest fee'],
  'ml.filter': ['筛选', 'Filter'], 'ml.f_open': ['只看营业中', 'Open now'], 'ml.f_promo': ['有优惠', 'With offers'], 'ml.f_schedule': ['可预约', 'Takes scheduled orders'], 'ml.f_pickup': ['可自取', 'Pickup'],
  'ml.monthly': ['月售 {n}', '{n} sold this month'], 'ml.new': ['新店', 'New'], 'ml.minutes': ['{n} 分钟', '{n} min'],
  'ml.min': ['起送 {amt}', 'Min. {amt}'], 'ml.min_from': ['起送 {amt} 起', 'Min. from {amt}'], 'ml.fee': ['配送 {amt}', 'Delivery {amt}'], 'ml.fee_free': ['免配送费', 'Free delivery'],
  'ml.pickup_only': ['仅到店自取', 'Pickup only'], 'ml.can_schedule': ['可预约', 'Schedule ahead'],
  'ml.closed': ['休息中', 'Closed'], 'ml.paused': ['暂停接单', 'Not taking orders'],
  'ml.opens_today': ['今天 {t} 营业', 'Opens {t} today'], 'ml.opens_tomorrow': ['明天 {t} 营业', 'Opens {t} tomorrow'],
  'ml.found': ['找到 {n} 家店有“{q}”', '{n} stores with "{q}"'], 'ml.found_none': ['没有找到“{q}”', 'Nothing found for "{q}"'],
  'ml.none_title': ['这个位置暂时没有能送到的店', 'No store delivers here yet'], 'ml.none_hint': ['可以换个地址，或者看看附近能自取的店', 'Try another address, or see the stores you can collect from'],
  'ml.none_filtered': ['没有符合条件的店', 'No store matches'], 'ml.clear_filters': ['清除条件', 'Clear filters'],
  'ml.change': ['换个地址', 'Change address'], 'ml.browse': ['按城市逛逛', 'Browse by city'],
  'ml.apply': ['想在这里开店？', 'Want to open a store here?'], 'ml.apply_link': ['商家入驻', 'Apply as a merchant'], 'ml.staff_login': ['商家登录', 'Merchant login'],
  'ml.sheet_title': ['送到哪里', 'Deliver to'], 'ml.sheet_search': ['搜索大楼、小区、街道', 'Search a building or street'],
  'ml.gps': ['当前位置（自动定位）', 'Current location'], 'ml.relocate': ['重新定位', 'Locate me'], 'ml.need_permission': ['需要允许定位', 'Needs location access'],
  'ml.denied': ['没有拿到你的定位。请搜索地址，或选一个已保存的地址。', 'We could not find where you are. Search for an address or choose a saved one.'],
  'ml.denied_again': ['还是没有拿到定位，请在浏览器设置里允许定位', 'Still no location. Allow location access in your browser settings.'],
  'ml.my_addresses': ['我的收货地址', 'My addresses'], 'ml.map_pick': ['在地图上选位置', 'Pick on the map'],
  'ml.cities': ['按城市逛', 'Browse by city'], 'ml.city_all': ['全部 {city}', 'All of {city}'], 'ml.city_n': ['{n} 家店', '{n} stores'],
  'ml.cities_note': ['只列有店的城市。按城市看的时候不显示配送费，下单选了收货地址才知道能不能送。', 'Only cities with stores are listed. Delivery fees are shown once you choose an address.'],
  'ml.cities_none': ['还没有商家', 'No stores yet'],
  'ml.first_title': ['正在找你附近的店…', 'Looking for stores near you…'], 'ml.first_hint': ['需要用一下你现在的位置，只用来判断哪些店能送到', 'Your location is used only to find the stores that deliver to you'],
  'ml.back_mall': ['返回商城', 'Back to the market'], 'ml.banner': ['活动', 'Promotion'],
  'ml.load_failed': ['加载失败', 'Could not load'], 'ml.orders': ['订单', 'Orders'], 'ml.favorites': ['收藏', 'Favourites'],
})
