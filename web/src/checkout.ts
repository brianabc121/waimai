// Dine-in and delivery promotions, discounts at checkout, payment types and
// the reconciliation page (docs/CHECKOUT_DISCOUNT_DESIGN.md).
import { extend, isZh, locale, t } from './i18n'
import type { PayChannel } from './types'

extend({
  'ck.tab_delivery': ['外卖活动', 'Delivery offers'],
  'ck.tab_dine': ['堂食活动', 'Dine-in offers'],
  'ck.tab_coupons': ['优惠券（外卖）', 'Coupons (delivery)'],
  'ck.new_dine': ['新建堂食活动', 'New dine-in promotion'],
  'ck.delivery_hint': ['外卖活动只用于外卖和自取订单，不会减堂食账单。', 'Delivery offers apply to delivery and pickup orders only, never to dine-in bills.'],
  'ck.dine_hint': ['堂食活动只减堂食账单（店员开台、顾客扫码点菜），不会出现在外卖店铺页，外卖活动也不会减堂食账单。一张账单最多用一个活动，自动挑省得最多的；名额在结账时才扣。',
    'Dine-in offers only reduce table bills (opened by staff or ordered by QR code) and never show on the delivery store page; delivery offers never reduce a table bill. One offer per bill, the one that saves the most; its quota is used when the bill is paid.'],
  'ck.dine_time_hint': ['每天时段按开台时间算：开台时在时段内，结账晚了也照样减。', 'Daily hours count from when the table was opened: a table opened inside them keeps the offer even if it pays later.'],
  'ck.dine_offers': ['堂食优惠', 'Dine-in offers'],
})

extend({
  // discount at checkout (§5)
  'ck.adj': ['结账优惠', 'Discount at checkout'],
  'ck.adj_none': ['不参与', 'None'],
  'ck.adj_pct': ['打折', 'Percentage off'],
  'ck.adj_round': ['抹零', 'Round off'],
  'ck.adj_set': ['改实收', 'Set amount to pay'],
  'ck.other': ['其他', 'Other'],
  'ck.pct_hint': ['输入折扣，例如 92 就是 92 折', 'Type the percentage off, for example 8 for 8% off'],
  'ck.round_to': ['→ {to}（抹 {off}）', '→ {to} ({off} off)'],
  'ck.round_hint': ['输入抹掉多少', 'Type how much to take off'],
  'ck.set_hint': ['输入顾客实际付多少，输入 0 就是免单', 'Type what the customer actually pays; 0 makes it free'],
  'ck.free': ['免单', 'On the house'],
  'ck.reason': ['原因', 'Reason'],
  'ck.reason_req': ['改实收和免单必须选一个原因', 'Pick a reason when you set the amount to pay'],
  'ck.reason_opt': ['可不选', 'Optional'],
  'ck.r.regular': ['熟客', 'Regular'],
  'ck.r.friend': ['老板朋友', "Owner's friend"],
  'ck.r.staff': ['员工餐', 'Staff meal'],
  'ck.r.complaint': ['投诉补偿', 'Complaint'],
  'ck.r.slow': ['上菜慢', 'Slow service'],
  'ck.r.other': ['其他', 'Other'],
  'ck.summary': ['应付 {from} → {to}，优惠 {off}', 'To pay {from} → {to}, {off} off'],
  'ck.clear': ['不参与', 'No discount'],
  'ck.apply': ['确定', 'Apply'],
  'ck.cancel': ['取消', 'Cancel'],
  'ck.staff_disc': ['门店优惠', 'Store discount'],
  'ck.round_off': ['抹零', 'Rounded off'],
  'ck.set_off': ['改价让利', 'Price lowered'],
  'ck.approve_what': ['结账优惠', 'a discount at checkout'],
  'ck.by': ['{name} 设', 'by {name}'],
  'ck.by_ok': ['{name} 设，{boss} 授权', 'by {name}, approved by {boss}'],
  'ck.approved': ['{name} 已授权，这一单可以设结账优惠', '{name} approved a discount on this order'],
  'ck.approval_used': ['授权已经用过或过期了，请再授权一次', 'That approval was used or expired. Ask again.'],
  'ck.changed': ['金额变了，请看一下新的应付再确认', 'The amount changed. Check the new total and confirm again.'],
  // payment types (§6)
  'ck.pay_type': ['付款', 'Payment'],
  'ck.pt.cash': ['现金', 'Cash'],
  'ck.pt.card': ['刷卡', 'Card'],
  'ck.pt.online': ['在线支付', 'Online'],
  'ck.no_channels': ['这家店还没有可用的在线方式，请在门店设置的“收款方式”里打开', 'No online methods are on. Turn them on under Settings > Payment methods.'],
  'ck.no_types': ['收款方式都关了，请在门店设置里打开', 'Every payment method is off. Turn one on in Settings.'],
  'ck.cod': ['货到付款', 'Cash on delivery'],
  'ck.pay_store': ['到店付', 'Pay at the store'],
  'ck.paid_online': ['已在线付款', 'Paid online'],
  // orders.payment_method of a staff order the customer paid online before it was placed
  prepaid: ['已在线付款', 'Paid online'],
  'ck.rider_cash': ['收现金', 'Cash collected'],
  'ck.rider_online': ['顾客在线付', 'Paid online'],
  'ck.record_pay': ['记录付款方式', 'Record payment'],
  'ck.edit_adj': ['改结账优惠', 'Change discount'],
  // store settings (§6.1)
  'ck.set_title': ['收款方式', 'Payment methods'],
  'ck.pay_set_hint': ['结账时只出现打开的方式。在线方式由平台按国家配置，只是记录付款类型，不扫码，钱到没到账请在对账页核对。',
    'Only methods that are on show at checkout. The platform sets the online methods per country; they only record how a bill was paid (no QR scanning), so check the money arrived on the Reconciliation page.'],
  'ck.set_cash': ['收现金', 'Cash'],
  'ck.set_card': ['刷卡', 'Card'],
  'ck.set_online': ['在线方式（{country}）', 'Online methods ({country})'],
  'ck.set_reasons': ['结账优惠原因', 'Discount reasons'],
  'ck.set_reasons_hint': ['一行一个，最多 12 个；留空就用默认的（熟客、老板朋友、员工餐、投诉补偿、上菜慢、其他）', 'One per line, up to 12. Leave empty for the defaults.'],
  'ck.save': ['保存', 'Save'],
  'ck.saved': ['已保存', 'Saved'],
  // role permissions
  'rp.p.checkout.discount': ['结账优惠', 'Discounts at checkout'],
  'rp.d.checkout.discount': ['结账时打折、抹零、改实收；没有这项要经理授权码', 'Percentage off, round-off and setting the amount to pay; without it a manager approves'],
  'rp.p.reconcile': ['对账', 'Reconciliation'],
  'rp.d.reconcile': ['收款汇总、在线支付核对、现金盘点、让利记录', 'Takings by method, online payment checks, cash count, discount log'],
})

// The discount reasons to tap: the store's own, else the built-in ones.
export const defaultReasons = () => ['regular', 'friend', 'staff', 'complaint', 'slow', 'other'].map(k => t('ck.r.' + k))
export function reasonList(own: string[] | null | undefined): string[] {
  return own?.length ? own : defaultReasons()
}

// pctText shows a percentage as Chinese readers say it ("9 折") or as "10% off".
export function pctText(bp: number): string {
  return t('pr.percent_off_sp', { zhe: bp / 1000, pct: Math.round(10000 - bp) / 100 })
}

// channelName shows an online method in the page's language, else its own name.
export function channelName(c: PayChannel | undefined, fallback = ''): string {
  if (!c) return fallback
  return c.names?.[locale.value] || (isZh() ? c.names?.zh : '') || c.name
}

// payText says how an order was paid: "现金", "在线支付 · GCash".
export function payText(o: { pay_type?: string; pay_method?: string; payment_method?: string }): string {
  if (o.pay_type === 'online') return t('ck.pt.online') + (o.pay_method ? ' · ' + o.pay_method : '')
  if (o.pay_type === 'cash' || o.pay_type === 'card') return t('ck.pt.' + o.pay_type)
  return o.pay_method ?? ''
}
