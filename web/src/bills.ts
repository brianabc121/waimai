// Words of the bill pages (docs/COUPON_DESIGN.md §8, §9), shared by the
// store's bill page and the platform's.
import { extend, t } from './i18n'
import './coupons'

extend({
  'bl.nav': ['账单', 'Bills'],
  'bl.tab_orders': ['订单明细', 'Orders'], 'bl.tab_sum': ['汇总', 'Summary'],
  'bl.p.today': ['今天', 'Today'], 'bl.p.yesterday': ['昨天', 'Yesterday'], 'bl.p.week': ['近 7 天', 'Last 7 days'],
  'bl.p.month': ['本月', 'This month'], 'bl.p.last_month': ['上月', 'Last month'], 'bl.p.custom': ['自选', 'Custom'],
  'bl.export': ['导出 Excel', 'Export to Excel'],
  'bl.hint': ['外卖和自取订单，按下单日期算。只有已完成（送达、已取餐）的订单计入合计；取消的单列出来方便对账，金额按 0 算。', 'Delivery and pickup orders by the day they were placed. Only completed orders count in the totals; cancelled ones are listed for reference at 0.'],
  'bl.f.all': ['全部订单', 'All orders'], 'bl.f.coupon': ['用了券的单', 'With a coupon'], 'bl.f.none': ['没有优惠的单', 'No discount'],
  'bl.no': ['订单号 / 时间', 'Order / time'], 'bl.type': ['方式', 'Type'], 'bl.food': ['商品原价', 'Food'], 'bl.fee': ['配送费', 'Delivery'],
  'bl.promo': ['活动', 'Promotion'], 'bl.store_coupon': ['商家券', 'Store coupons'], 'bl.fee_coupon': ['配送费券', 'Delivery coupon'],
  'bl.platform': ['平台券', 'Platform coupon'], 'bl.paid': ['顾客实付', 'Customer paid'], 'bl.takings': ['商家应收', 'Store takings'],
  'bl.state': ['状态', 'Status'], 'bl.sum': ['合计（已完成 {n} 单）', 'Total ({n} completed)'],
  'bl.st.completed': ['已完成', 'Completed'], 'bl.st.cancelled': ['已取消', 'Cancelled'], 'bl.st.failed': ['配送异常', 'Not delivered'], 'bl.st.open': ['进行中', 'In progress'],
  'bl.delivery': ['外卖', 'Delivery'], 'bl.pickup': ['自取', 'Pickup'],
  'bl.none': ['这段时间没有订单。', 'No orders in this period.'], 'bl.cut': ['只显示最近 {n} 单，全部请导出 Excel。', 'Showing the latest {n}; export to see all.'],
  'bl.src.repurchase': ['复购礼券', 'Welcome-back'], 'bl.src.claim': ['店铺领券', 'Claimed'], 'bl.src.new_customer': ['平台新人券', 'Platform new-customer'],
  'bl.src.manual': ['平台发放', 'Given by the platform'], 'bl.coupon_no': ['券号 {id}', 'Coupon #{id}'],
  'bl.c.orders': ['完成订单', 'Completed orders'], 'bl.c.food': ['商品原价合计', 'Food at full price'], 'bl.c.discount': ['优惠合计', 'Discounts'],
  'bl.c.discount_sub': ['商家 {m} · 平台 {p}', 'Store {m} · platform {p}'], 'bl.c.paid': ['顾客实付合计', 'Customers paid'],
  'bl.c.due': ['平台应补', 'Platform owes'], 'bl.c.takings': ['商家实际收入', 'Store takings'], 'bl.c.refunded': ['已退款', 'Refunded'], 'bl.c.paid_credit': ['店铺余额支付', 'Paid from store balances'], 'bl.c.paid_credit_sub': ['顾客用店铺余额付的部分，钱在退款时已经留在店里', 'Paid from balances: the money stayed in the store when it was refunded'], 'bl.c.refunded_sub': ['这些订单之后按商品退给顾客的钱，未从上面扣除', 'Given back by item on these orders later; not taken off above'], 'bl.c.cancelled': ['另有 {n} 单取消', '{n} more cancelled'],
  'bl.kinds': ['各种优惠', 'Discounts by kind'], 'bl.k.kind': ['种类', 'Kind'], 'bl.k.who': ['谁承担', 'Paid by'], 'bl.k.orders': ['用了多少单', 'Orders'],
  'bl.k.amount': ['合计减了', 'Total off'], 'bl.k.avg': ['平均每单', 'Per order'], 'bl.k.total': ['合计', 'Total'],
  'bl.who.store': ['商家', 'Store'], 'bl.who.platform': ['平台', 'Platform'],
  'bl.rep': ['复购礼券效果', 'Welcome-back coupon'], 'bl.rep_line': ['这段时间送出 {issued} 张，已用 {used} 张（{rate}%），用券订单商品 {sales}', '{issued} given in this period, {used} used ({rate}%), food sold with them {sales}'],
  'bl.settle': ['平台券补贴结算', 'Platform coupon statements'],
  'bl.settle_hint': ['每月 1 号按上个月已完成订单出结算单，平台线下转账后标记已付款。', 'On the 1st, last month\'s completed orders make a statement; the platform marks it paid after the transfer.'],
  'bl.settle_none': ['还没有结算单。', 'No statements yet.'], 'bl.month': ['月份', 'Month'], 'bl.amount': ['金额', 'Amount'],
  'bl.s.pending': ['待付款', 'To be paid'], 'bl.s.paid': ['已付款', 'Paid'], 'bl.paid_on': ['{d} 付款 · {ref}', 'Paid {d} · {ref}'],
  'bl.ask': ['有疑问', 'Question'], 'bl.view_orders': ['看订单', 'Orders'], 'bl.days': ['每天', 'By day'], 'bl.day': ['日期', 'Day'],
  // the platform's page
  'abl.stores': ['各店账单', 'Stores'], 'abl.settle': ['平台券结算单', 'Coupon statements'], 'abl.store': ['店铺', 'Store'],
  'abl.mdisc': ['商家承担优惠', 'Store discounts'], 'abl.pdisc': ['平台承担优惠', 'Platform discounts'],
  'abl.open_store': ['看账单', 'Bill'], 'abl.mark': ['标记已付款', 'Mark paid'], 'abl.paid_on': ['付款日期', 'Date paid'],
  'abl.ref': ['转账单号', 'Transfer reference'], 'abl.f.all': ['全部', 'All'],
  'abl.pending_sum': ['待付款合计', 'To be paid'], 'abl.none': ['这段时间没有完成的订单。', 'No completed orders in this period.'],
})

export const sourceName = (s: string) => s ? t('bl.src.' + s) : ''
