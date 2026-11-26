// Order types and order numbers (docs/ORDER_TYPES_DESIGN.md): the five types,
// their colours and texts, shared by the order list, kitchen and settings.
import { extend, t } from './i18n'

extend({
  'ot.all': ['全部', 'All'],
  'ot.t.dine_in': ['堂食', 'Dine-in'],
  'ot.t.staff': ['代客', 'By staff'],
  'ot.t.platform': ['平台', 'Platform'],
  'ot.t.mall': ['商城', 'Mall'],
  'ot.t.web': ['网店', 'Own link'],
  'ot.ds.staff': ['店员', 'staff'],
  'ot.ds.self': ['扫码', 'scan'],
  'ot.set.title': ['订单编号', 'Order numbers'],
  'ot.set.code': ['商家代码', 'Store code'],
  'ot.set.code_hint': ['2–8 位字母或数字，全平台唯一；代客单号以它开头', '2–8 letters or digits, unique on the platform; staff order numbers start with it'],
  'ot.set.reset': ['每日重置时间', 'Daily reset time'],
  'ot.set.reset_hint': ['堂食、网店单号在这个时间从 1 重新开始，之前的单算前一天', 'Dine-in and own-link numbers start from 1 at this time; earlier orders count for the day before'],
  'ot.set.preview': ['编号预览', 'Preview'],
  'ot.set.p_dine': ['店员点单、扫码自助都用它', 'For staff and scanned tables'],
  'ot.set.p_staff': ['商家代码 + 会员号；同一会员当天第二单加 -2', 'Store code + member number; -2 for the member\'s second order of the day'],
  'ot.set.p_mall': ['顾客从商城进店下的单，每天 00:00 清零', 'Orders from the mall; from 1 again at midnight'],
  'ot.set.p_web': ['顾客打开本店链接下的单', 'Orders from the store\'s own link'],
  'ot.set.note': ['改代码只影响以后的单，已有订单号不变。', 'A new code only applies to new orders.'],
})

export const ORDER_TYPES = ['dine_in', 'staff', 'platform', 'mall', 'web'] as const
export type OrderType = typeof ORDER_TYPES[number]

// typeLabel is the short tag of an order: 堂食 · 店员, 代客, 商城...
export function typeLabel(o: { order_type?: string; dine_source?: string }): string {
  const ty = o.order_type || 'web'
  const base = t('ot.t.' + ty)
  return ty === 'dine_in' && o.dine_source ? `${base} · ${t('ot.ds.' + o.dine_source)}` : base
}

export const typeColor: Record<string, string> = {
  dine_in: '#2f7d4f', staff: '#8a5a2b', platform: '#00b14f', mall: '#c2410c', web: '#2463a6',
}
