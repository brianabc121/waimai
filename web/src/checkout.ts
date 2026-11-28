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
  'ck.dine_hint': ['堂食活动只减堂食账单（店员开台、顾客扫码点单），不会出现在外卖店铺页，外卖活动也不会减堂食账单。一张账单最多用一个活动，自动挑省得最多的；名额在结账时才扣。',
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
  'ck.r.slow': ['上得慢', 'Slow service'],
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
  'ck.pt.platform': ['平台付款', 'Paid on the platform'],
  'ck.pt.store_credit': ['店铺余额', 'Store balance'], 'ck.pt.credit': ['店铺余额', 'Store balance'],
  'rc.c.to_credit': ['其中 {v} 存入顾客店铺余额，没有付出现金', '{v} of it went into store balances; no money left the store'],
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
  'ck.set_reasons_hint': ['一行一个，最多 12 个；留空就用默认的（熟客、老板朋友、员工餐、投诉补偿、上得慢、其他）', 'One per line, up to 12. Leave empty for the defaults.'],
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
  if (o.pay_type === 'cash' || o.pay_type === 'card' || o.pay_type === 'platform' || o.pay_type === 'credit') return t('ck.pt.' + o.pay_type)
  return o.pay_method ?? ''
}

extend({
  // the reconciliation page (§8)
  'rc.nav': ['对账', 'Reconciliation'],
  'rc.tab_sum': ['收款汇总', 'Takings'],
  'rc.tab_online': ['在线支付核对', 'Online payments'],
  'rc.tab_disc': ['让利记录', 'Discounts'],
  'rc.export': ['导出 Excel', 'Export to Excel'],
  'rc.hint': ['按付款时间算，一天按门店所在时区。在线支付只是店员记下的付款方式，钱到没到账请对着 GCash、银行 App 的流水一笔笔核对。',
    "Counted by when each order was paid, by the store's own days. Online payments are only what staff recorded, so check each one against your GCash or bank app."],
  'rc.c.total': ['收款合计', 'Total taken'],
  'rc.c.count': ['{n} 单', '{n} orders'],
  'rc.c.discount': ['让利合计', 'Discounts given'],
  'rc.c.refunds': ['按商品退款', 'Refunds by item'],
  'rc.c.net': ['扣除后 {v}', 'net {v}'],
  'rc.c.problems': ['有 {n} 笔在线支付标为“有问题”', '{n} online payments are marked as a problem'],
  'rc.col.method': ['付款方式', 'Method'],
  'rc.col.count': ['笔数', 'Orders'],
  'rc.col.amount': ['金额', 'Amount'],
  'rc.col.dine': ['堂食', 'Dine-in'],
  'rc.col.takeaway': ['外卖和自取', 'Delivery and pickup'],
  'rc.col.check': ['核对', 'Checked'],
  'rc.check_n': ['已到账 {ok} · 有问题 {bad} · 未核对 {un}', '{ok} arrived · {bad} problems · {un} not checked'],
  'rc.none': ['这段时间没有已收款的订单', 'No paid orders in this period'],
  'rc.cash': ['现金', 'Cash'],
  'rc.cash_counter': ['店里收的现金（堂食、自取）', 'Cash taken at the store (tables, pickups)'],
  'rc.cash_rider': ['骑手收的现金', 'Cash riders collected'],
  'rc.cash_returned': ['骑手已交回', 'Handed in by riders'],
  'rc.cash_refunded': ['现金退款', 'Cash refunds'],
  'rc.cash_pending': ['骑手还没交回或没核款', 'Not yet handed in or checked'],
  'rc.cash_short': ['骑手少收、还没处理的', 'Collected short, not settled yet'],
  'rc.cash_top_up': ['其中：预付单加单补收的现金', 'Of which: cash for items added to prepaid orders'],
  'rc.cash_late': ['其中：少收的单后来在店里补收', 'Of which: taken later for orders collected short'],
  'rc.cash_waived': ['少收后决定不收的（不算营业额）', 'Let go after a short collection (not in takings)'],
  'cs.short': ['少收 {m}', 'Short by {m}'],
  'cs.short_collected': ['补收了', 'The rest came in'],
  'cs.short_waived': ['差额不收了', 'Let the rest go'],
  'cs.short_collected_tip': ['订单 {no} 少收的 {m} 已经在店里收到现金。确认后订单记为已付，这笔现金算进今天的钱箱。', 'The {m} missing on order {no} was taken in cash at the store. The order becomes paid and the cash counts in today\'s drawer.'],
  'cs.short_waived_tip': ['订单 {no} 少收的 {m} 不再收。确认后订单记为已付，这 {m} 不算营业额，以后退款最多退实际收到的。', 'The {m} missing on order {no} will not be collected. The order becomes paid, the {m} is not counted as takings, and refunds are limited to what was received.'],
  'cs.short_note': ['备注（可不填）', 'Note (optional)'],
  'cs.short_why': ['为什么不收（必填）', 'Why it is not collected (required)'],
  'cs.late_done': ['已在店里补收 {m}', '{m} taken later at the store'],
  'cs.waived_done': ['{m} 不收了', '{m} let go'],
  'cs.ok': ['确认', 'Confirm'],
  'rc.to_cash': ['去核款', 'Go to rider cash'],
  'rc.count': ['现金盘点', 'Cash count'],
  'rc.count_hint': ['数一下钱箱里的现金填在这里。应有 = 店里收的现金 + 骑手已交回的现金（不含备用金）。', 'Count the cash in the drawer. Expected = cash taken at the store + cash riders handed in (float not included).'],
  'rc.expected': ['应有', 'Expected'],
  'rc.counted': ['实点', 'Counted'],
  'rc.diff': ['差', 'Difference'],
  'rc.count_note': ['备注（选填）', 'Note (optional)'],
  'rc.count_save': ['保存盘点', 'Save count'],
  'rc.counted_by': ['{name} 在 {at} 盘点', 'Counted by {name} at {at}'],
  'rc.count_day_only': ['选一天才能盘点现金', 'Pick a single day to count the cash'],
  'rc.d.promo': ['活动', 'Promotions'],
  'rc.d.staff': ['结账打折', 'Percentage off at checkout'],
  'rc.d.round': ['抹零和改实收', 'Round-offs and set amounts'],
  'rc.f.all_methods': ['全部方式', 'All methods'],
  'rc.f.all': ['全部', 'All'],
  'rc.f.unchecked': ['未核对', 'Not checked'],
  'rc.f.ok': ['已到账', 'Arrived'],
  'rc.f.problem': ['有问题', 'Problem'],
  'rc.col.time': ['付款时间', 'Paid at'],
  'rc.col.order': ['订单', 'Order'],
  'rc.col.staff': ['骑手或店员', 'Rider or staff'],
  'rc.mark_ok': ['已到账', 'Arrived'],
  'rc.mark_bad': ['有问题', 'Problem'],
  'rc.mark_undo': ['改回未核对', 'Undo'],
  'rc.mark_all': ['这一页全部标为已到账（{n} 笔）', 'Mark all {n} on this page as arrived'],
  'rc.bad_note': ['有什么问题？例如：没收到，联系顾客', 'What is wrong? e.g. not received, calling the customer'],
  'rc.checked_by': ['{name} · {at}', '{name} · {at}'],
  'rc.no_online': ['这段时间没有在线支付', 'No online payments in this period'],
  'rc.st.ok': ['已到账', 'Arrived'],
  'rc.st.problem': ['有问题', 'Problem'],
  'rc.st.none': ['未核对', 'Not checked'],
  'rc.table': ['{t} 桌', 'Table {t}'],
  'rc.by_staff': ['按员工', 'By staff'],
  'rc.by_promo': ['按活动', 'By promotion'],
  'rc.staff_line': ['{n} 笔 · {amt}', '{n} · {amt}'],
  'rc.no_disc': ['这段时间没有结账优惠', 'No discounts at checkout in this period'],
  'rc.col.kind': ['类型', 'Kind'],
  'rc.col.off': ['优惠', 'Off'],
  'rc.col.paid': ['实付', 'Paid'],
  'rc.col.reason': ['原因', 'Reason'],
  'rc.col.by': ['谁设的', 'By'],
  'rc.col.approved': ['谁授权', 'Approved by'],
  'rc.someone': ['（未记录）', '(not recorded)'],
  'rc.check_tag': ['对账：{s}', 'Check: {s}'],
})
