// Coupons: words and helpers shared by checkout, the order page, "my offers"
// and the merchant's coupon settings (docs/COUPON_DESIGN.md).
import { extend, intlTag, money, pick, t } from './i18n'
import type { CouponKind, CustomerCoupon, OrderDiscount } from './types'

extend({
  'cp.kind.store_threshold': ['商家满减券', 'Store spend-and-save coupon'],
  'cp.kind.store_free': ['商家无门槛券', 'Store coupon, no minimum'],
  'cp.kind.store_delivery': ['满减配送费券', 'Delivery fee coupon'],
  'cp.kind.platform_free': ['平台无门槛券', 'Platform coupon, no minimum'],
  'cp.kind.promotion': ['门店活动', 'Store promotion'],
  'cp.kind.dish_promotion': ['菜品活动', 'Dish promotion'],
  'cp.short.store_threshold': ['店', 'S'], 'cp.short.store_free': ['店', 'S'], 'cp.short.store_delivery': ['运', 'D'],
  'cp.short.platform_free': ['平台', 'P'], 'cp.short.promotion': ['活动', 'Promo'], 'cp.short.dish_promotion': ['菜', 'Dish'],
  'cp.no_min': ['无门槛', 'No minimum'], 'cp.over': ['满{amt}', 'Over {amt}'], 'cp.fee': ['配送费', 'Delivery'],
  'cp.off': ['减{amt}', '{amt} off'], 'cp.over_off': ['满{th}减{amt}', '{amt} off over {th}'],
  'cp.fee_off': ['配送费减{amt}', '{amt} off delivery'], 'cp.fee_over_off': ['满{th}配送费减{amt}', '{amt} off delivery over {th}'],
  'cp.fee_free': ['免配送费', 'Free delivery'], 'cp.fee_over_free': ['满{th}免配送费', 'Free delivery over {th}'],
  'cp.free_off': ['无门槛减{amt}', '{amt} off, no minimum'],
  'cp.all_stores': ['全平台通用', 'Any store'], 'cp.in_region': ['{r} 店铺通用', 'Any store in {r}'], 'cp.platform_pays': ['平台出钱', 'Paid by the platform'],
  'cp.until': ['{d} 前有效', 'Valid until {d}'], 'cp.soon': ['即将过期 · {d}', 'Expires soon · {d}'],
  'cp.tomorrow': ['明天过期', 'Expires tomorrow'], 'cp.today': ['今天过期', 'Expires today'],
  'cp.delivery_only': ['仅外卖可用', 'Delivery only'], 'cp.short_by': ['还差 {amt} 可用', 'Add {amt} more to use'],
  'cp.no_saving': ['这单用不上', 'Saves nothing on this order'],
  'cp.coupons': ['优惠券', 'Coupons'], 'cp.picked': ['已选 {n} 张', '{n} chosen'], 'cp.saves': ['共减 {amt}', '{amt} off'],
  'cp.none_usable': ['{n} 张，这单用不了', '{n}, none fit this order'], 'cp.none_picked': ['不使用', 'None used'],
  'cp.pick_title': ['选择优惠券', 'Choose coupons'], 'cp.pick_sub': ['每种券可以各用一张，一起叠加', 'Use one of each kind together'],
  'cp.n_usable': ['{n} 张可用', '{n} usable'], 'cp.confirm': ['确定，共减 {amt}', 'Done, {amt} off'], 'cp.confirm_none': ['不使用优惠券', 'Use no coupon'],
  'cp.total_off': ['共优惠', 'Total savings'],
  'cp.platform_note': ['平台券由平台出钱，不影响商家的营业额和税。', 'The platform pays for its coupon; the store\'s takings and tax are unchanged.'],
  'cp.gift': ['送你一张{what}', 'A coupon for you: {what}'],
  'cp.gift_sub': ['{store} · {d}前下单可用', '{store} · use it by {d}'],
  'cp.repurchase': ['复购礼券', 'Welcome-back coupon'],
})

extend({
  'cr.page': ['优惠', 'Offers'],
  'cr.tab_promos': ['活动', 'Promotions'],
  'cr.title': ['复购礼券', 'Welcome-back coupon'],
  'cr.hint': ['顾客在本店第一次完成订单后，自动送一张券，下次点餐可以用。每人只送一次。', 'After a customer\'s first completed order here, they get a coupon for next time. Once per customer.'],
  'cr.on': ['已开启', 'On'], 'cr.off': ['未开启', 'Off'],
  'cr.who': ['送给谁', 'Who gets it'], 'cr.who_v': ['第一次在本店完成订单的顾客', 'Customers whose first order here is completed'],
  'cr.who_h': ['外卖送达、自取已取餐才算完成。取消、拒单的不送。', 'Delivered, or picked up. Cancelled and rejected orders get nothing.'],
  'cr.min': ['首单菜品至少', 'First order\'s food at least'], 'cr.min_h': ['0 表示不限。可以防止有人下很小的单来换券', '0 means any amount. Stops tiny orders placed just for the coupon'],
  'cr.kind': ['送什么券', 'Coupon'],
  'cr.k.store_free': ['无门槛券', 'No minimum'], 'cr.k.store_threshold': ['满减券', 'Spend and save'], 'cr.k.store_delivery': ['满减配送费券', 'Delivery fee'],
  'cr.value': ['减多少', 'Amount off'], 'cr.value_fee_h': ['填 0 表示免配送费', '0 means free delivery'],
  'cr.threshold': ['菜品满多少可用', 'Usable when food is at least'],
  'cr.days': ['有效期（天）', 'Valid for (days)'], 'cr.days_h': ['送出那天算第 1 天，可填 1 到 90 天', 'The day it is given is day 1; 1 to 90 days'],
  'cr.cap': ['每月最多送（张）', 'At most per month'], 'cr.cap_h': ['空着表示不限。送满后暂停，下个月 1 号自动恢复', 'Empty means no limit. Pauses when reached, resumes on the 1st'],
  'cr.tell': ['顾客收到时', 'The customer is told'], 'cr.tell_v': ['站内消息通知顾客，到期前 3 天再提醒一次。', 'A message when given, and a reminder 3 days before it expires.'],
  'cr.saved': ['已保存。改了面额或有效期，只影响以后送出的券。', 'Saved. Changes apply to coupons given from now on.'],
  'cr.preview': ['顾客看到的样子', 'What the customer sees'],
  'cr.days_valid': ['送出后 {n} 天内有效', 'Valid for {n} days'],
  'cr.month': ['本月效果', 'This month'], 'cr.issued': ['送出', 'Given'], 'cr.used': ['已使用', 'Used'], 'cr.rate': ['用券率', 'Used rate'],
  'cr.sales': ['用券订单菜品金额', 'Food sold with them'],
  'cr.rest': ['未到期 {open} 张 · 已过期 {expired} 张 · 商家承担 {cost}', '{open} still valid · {expired} expired · cost you {cost}'],
})

extend({
  'cb.title': ['领券活动', 'Coupons to claim'],
  'cb.hint': ['顾客在店铺页面点“领取”拿到券，下单时自动使用。可以限制总数和每人张数。', 'Customers tap "Claim" on your page; the coupon applies by itself at checkout. Limit how many in all and per person.'],
  'cb.new': ['新建领券活动', 'New coupon'], 'cb.name': ['名称（可不填）', 'Name (optional)'], 'cb.days': ['领取后有效天数', 'Valid for (days) after claiming'],
  'cb.quota': ['总共发多少张', 'How many in all'], 'cb.per': ['每人最多领', 'Per customer'],
  'cb.none': ['还没有领券活动。', 'No coupons to claim yet.'], 'cb.days_n': ['领取后 {n} 天内有效', 'valid {n} days'],
  'cb.left': ['剩 {n} / {of} 张', '{n} of {of} left'], 'cb.no_quota': ['不限张数', 'no limit'],
  'cb.stats': ['已领 {issued} · 已用 {used} · 商家承担 {spent}', '{issued} claimed · {used} used · cost you {spent}'],
  'cb.st.active': ['领取中', 'Running'], 'cb.st.paused': ['已暂停', 'Paused'], 'cb.st.ended': ['已结束', 'Ended'],
  'cb.pause': ['暂停', 'Pause'], 'cb.resume': ['继续', 'Resume'], 'cb.end': ['结束', 'End'],
  'cb.end_confirm': ['结束后顾客不能再领，已领的券到期前仍可用。确定结束？', 'Customers can no longer claim it; coupons already claimed stay usable. End it?'],
  'cb.claim': ['领取', 'Claim'], 'cb.claimed': ['已领取', 'Claimed'], 'cb.gone': ['已领完', 'All gone'],
  'cb.strip': ['店铺优惠券', 'Store coupons'], 'cb.got': ['已领取，下单时自动使用', 'Claimed. It applies by itself at checkout'],
})

export const kindName = (k: CouponKind | 'promotion' | 'dish_promotion') => t('cp.kind.' + k)
export const kindBadge = (k: CouponKind | 'promotion' | 'dish_promotion') => t('cp.short.' + k)

// title says what the coupon takes off: "满₱400减₱40", "配送费减₱30".
export function couponTitle(c: { kind: string; value: number; threshold: number }, currency: string): string {
  const amt = money(c.value, currency), th = money(c.threshold, currency)
  switch (c.kind) {
    case 'store_threshold': return t('cp.over_off', { th, amt })
    case 'store_delivery':
      if (c.value === 0) return c.threshold > 0 ? t('cp.fee_over_free', { th }) : t('cp.fee_free')
      return c.threshold > 0 ? t('cp.fee_over_off', { th, amt }) : t('cp.fee_off', { amt })
  }
  return t('cp.free_off', { amt })
}

// stub is the big amount on the left of a coupon and the line under it.
export function couponStub(c: { kind: string; value: number; threshold: number }, currency: string): [string, string] {
  const big = c.kind === 'store_delivery' && c.value === 0 ? t('cp.fee_free') : money(c.value, currency)
  if (c.kind === 'store_delivery') return [big, c.threshold > 0 ? t('cp.over', { amt: money(c.threshold, currency) }) : t('cp.fee')]
  return [big, c.threshold > 0 ? t('cp.over', { amt: money(c.threshold, currency) }) : t('cp.no_min')]
}

export const couponName = (c: { name: string; name_en: string; kind: CouponKind }) => pick(c.name, c.name_en) || kindName(c.kind)
// countryName is a country in the reader's language ("PH" → "菲律宾"), else the name the map gave.
export function countryName(code: string, fallback = ''): string {
  try { return (code && new Intl.DisplayNames([intlTag()], { type: 'region' }).of(code.toUpperCase())) || fallback || code }
  catch { return fallback || code }
}
// regionName is "菲律宾 · Metro Manila", or the country alone.
export const regionName = (c: { country_code: string; country: string; province: string }) =>
  [countryName(c.country_code, c.country), c.province].filter(Boolean).join(' · ')

// where: a store's name, or for a platform coupon the country (and province) whose stores take it
export const whereUsable = (c: CustomerCoupon) => c.issuer !== 'platform' ? pick(c.store_name, c.store_name_en)
  : c.country_code ? t('cp.in_region', { r: regionName(c) }) : t('cp.all_stores')

function day(iso: string): string {
  // the last moment it is valid, in the device's own words
  const d = new Date(new Date(iso).getTime() - 1000)
  return d.toLocaleDateString(intlTag() === 'en' ? 'en-GB' : intlTag(), { month: 'short', day: 'numeric' })
}
export const lastDay = day

// validity is "11月29日 前有效", or a warning when three days or less are left.
export function validity(c: CustomerCoupon): { text: string; soon: boolean } {
  const left = new Date(c.expires_at).getTime() - Date.now()
  if (left <= 0) return { text: t('cp.until', { d: day(c.expires_at) }), soon: false }
  const days = left / 86400000
  const endOfToday = new Date(); endOfToday.setHours(24, 0, 0, 0)
  if (new Date(c.expires_at).getTime() <= endOfToday.getTime()) return { text: t('cp.today'), soon: true }
  if (new Date(c.expires_at).getTime() <= endOfToday.getTime() + 86400000) return { text: t('cp.tomorrow'), soon: true }
  if (days <= 3) return { text: t('cp.soon', { d: day(c.expires_at) }), soon: true }
  return { text: t('cp.until', { d: day(c.expires_at) }), soon: false }
}

// discountLabel names one line of an order's discounts.
export function discountLabel(d: OrderDiscount): string {
  return pick(d.name, d.name_en) || kindName(d.kind)
}
