// Delivery platforms (docs/ORDER_TYPES_DESIGN.md §6): Grab, foodpanda and the
// ones the platform adds per country. A store turns on the platforms it sells
// on; each dish then has a platform price, set by hand or worked out by the
// server from the store's markup (the browser never computes it).
import { extend, locale } from './i18n'

extend({
  'pf.price': ['平台价', 'Platform price'],
  'pf.price_hint': ['留空 = 按加价自动算（灰字），填了就用填的。', 'Empty = worked out from the markup (grey); a value you type is used as is.'],
  'pf.auto': ['自动', 'auto'],
  // the store's settings card
  'pf.set.title': ['外卖平台', 'Delivery platforms'],
  'pf.set.hint': ['勾选本店上架的外卖平台。佣金和加价留空就用平台的默认值；菜品的平台价在“菜单”里看和改，没单独设的按“店铺价 ×（1 + 加价）”再取整自动算。',
    'Tick the platforms the store sells on. Empty rates use the platform\'s defaults. Dish prices on each platform are on the menu page; a dish without its own price costs the store price × (1 + markup), rounded.'],
  'pf.set.none': ['本店所在国家还没有外卖平台，请联系平台添加。', 'No delivery platform is offered in the store\'s country yet. Ask the platform to add one.'],
  'pf.set.on': ['上架', 'On'],
  'pf.set.shop': ['平台店铺号', 'Shop ID'],
  'pf.set.commission': ['佣金 %', 'Commission %'],
  'pf.set.markup': ['菜品加价 %', 'Price markup %'],
  'pf.set.round': ['取整', 'Round to'],
  'pf.set.r0': ['不取整', 'No rounding'],
  'pf.set.r1': ['整 1', 'Whole 1'],
  'pf.set.r5': ['整 5', 'Whole 5'],
  'pf.set.r10': ['整 10', 'Whole 10'],
  'pf.set.default': ['默认 {v}', 'default {v}'],
})

export interface DeliveryPlatform {
  id: number; name: string; names: Record<string, string>; prefix: string; countries: string[]; color: string; logo_url: string
  no_hint: string; no_pattern: string; commission_bp: number; commission_base: 'after_merchant' | 'original'; markup_bp: number
  active: boolean; sort: number; stores?: number
}
export interface BranchPlatform extends DeliveryPlatform {
  enabled: boolean; shop_ref: string; own_commission_bp: number | null; own_markup_bp: number | null; round_step: number
  effective_commission_bp: number; effective_markup_bp: number
}

// platformName is the platform's name in the page's language.
export const platformName = (p: { name: string; names?: Record<string, string> }) => p.names?.[locale.value] || p.name

// Rates are kept in basis points: 2000 is 20%.
export const bpToPct = (bp: number | null | undefined) => bp == null ? '' : String(bp / 100)
export const pctToBp = (s: string | number | null | undefined): number | null => {
  if (s === '' || s == null) return null
  const n = Number(s)
  return Number.isFinite(n) ? Math.round(n * 100) : null
}
