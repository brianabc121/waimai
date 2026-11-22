// What the customer's order list and order page share.
import type { Router } from 'vue-router'
import { get } from './api'
import { replaceCart } from './cart'
import { extend, locale, pick, t } from './i18n'
import type { Reorder } from './types'

extend({
  'ol.title': ['我的订单', 'My orders'], 'ol.search': ['搜索我的订单', 'Search my orders'],
  'ol.t_all': ['全部', 'All'], 'ol.t_active': ['进行中', 'In progress'], 'ol.t_review': ['待评价', 'To review'], 'ol.t_aftersale': ['退款/售后', 'Refunds'],
  'ol.empty': ['还没有订单', 'No orders yet'], 'ol.empty_search': ['没有找到相关订单', 'No orders match'], 'ol.empty_tab': ['这里暂时没有订单', 'Nothing here right now'],
  'ol.ordered_at': ['下单时间', 'Ordered'], 'ol.n_items': ['共 {n} 件', '{n} items'],
  'ol.more': ['更多', 'More'], 'ol.detail': ['订单详情', 'Order details'],
  'ol.review': ['评价', 'Review'], 'ol.reviewed': ['已评价', 'Reviewed'], 'ol.again': ['再来一单', 'Order again'],
  'ol.to_review': ['待评价', 'To review'], 'ol.done': ['已完成', 'Completed'],
  'ol.load_more': ['加载更多', 'Load more'], 'ol.end': ['没有更多了', 'That is all'],
  'ol.login_first': ['登录后查看订单', 'Log in to see your orders'],
  'ol.gone_all': ['这一单里的商品现在都已下架，去店里看看别的吧。', 'Nothing from this order is sold any more. Have a look at the menu.'],
  'ol.gone_some': ['这些商品已下架或规格有变化，没有加入购物车：{names}', 'Not added, no longer sold or changed: {names}'],
  'as.open': ['售后处理中', 'Request sent'], 'as.resolved': ['售后已处理', 'Request settled'], 'as.declined': ['售后未通过', 'Request declined'],
  'as.apply': ['申请退款', 'Ask for a refund'], 'as.title': ['申请退款 / 售后', 'Refund or after-sales request'],
  'as.cod_note': ['本店是货到付款，系统不会自动退钱。提交后商家会收到申请，并和你联系处理。', 'Orders are paid in cash, so nothing is refunded automatically. The store receives your request and settles it with you.'],
  'as.reason': ['原因', 'Reason'], 'as.detail_ph': ['补充说明（选“其他”时必填）', 'More details (needed for "Other")'], 'as.submit': ['提交申请', 'Send request'],
  'as.r.wrong_item': ['送错了商品', 'Wrong item'], 'as.r.missing_item': ['少送了商品', 'Something is missing'], 'as.r.quality': ['商品质量有问题', 'Quality problem'],
  'as.r.late': ['等太久了', 'Took too long'], 'as.r.not_received': ['没有收到', 'Never arrived'], 'as.r.changed_mind': ['不想要了', 'Changed my mind'], 'as.r.other': ['其他', 'Other'],
  'as.yours': ['你的申请', 'Your request'], 'as.reply': ['商家回复', 'The store says'],
  'as.wait': ['商家还没有处理，着急可以直接联系商家。', 'The store has not answered yet. You can also call the store.'],
  'as.d.open': ['顾客申请售后', 'After-sales request'], 'as.d.resolved': ['售后已处理', 'Request settled'], 'as.d.declined': ['售后已拒绝', 'Request declined'],
  'as.d.hint': ['订单是货到付款，请和顾客联系后线下处理（退现金、补送等），再在这里记录结果。', 'Orders are paid in cash: settle it with the customer (cash back, send the missing dish), then record the outcome here.'],
  'as.d.resolve': ['已处理', 'Settled'], 'as.d.decline': ['拒绝', 'Decline'], 'as.d.reply': ['给顾客的回复', 'Reply to the customer'],
  'as.d.how': ['怎么处理的？（顾客会看到，可以留空）', 'How was it settled? (shown to the customer, may be empty)'], 'as.d.why': ['拒绝的原因（顾客会看到）', 'Why is it declined? (shown to the customer)'],
  'od.done_title': ['订单已完成', 'Order completed'], 'od.thanks': ['感谢惠顾，欢迎再来', 'Thank you. See you again.'],
  'od.h.pending_acceptance': ['商家确认后就开始准备', 'The store starts on it once it accepts'],
  'od.h.preparing': ['商家正在准备你的餐', 'The store is preparing your order'], 'od.h.scheduled': ['到预约时间前商家会备好', 'It will be ready for the time you chose'],
  'od.h.awaiting_rider': ['骑手取餐后就出发', 'The rider leaves as soon as it is picked up'], 'od.h.out_for_delivery': ['骑手正在送来，请保持电话畅通', 'Your rider is on the way. Keep your phone at hand.'],
  'od.h.rider_arrived': ['骑手已到，请准备好现金', 'Your rider is here. Please have the cash ready.'], 'od.h.ready_for_pickup': ['到店报取餐号取餐', 'Give your pickup code at the store'],
  'od.h.closed': ['这张订单已经关闭', 'This order is closed'],
  'od.call_rider': ['致电骑手', 'Call rider'], 'od.call_store': ['联系商家', 'Call store'],
  'od.satisfied': ['你对本单满意吗？', 'Happy with this order?'],
  'od.fees': ['商品费用', 'Items and fees'], 'od.info': ['订单信息', 'Order details'],
  'od.address': ['配送地址', 'Deliver to'], 'od.pickup_at_store': ['取餐门店', 'Pick up at'], 'od.no': ['订单号', 'Order no.'], 'od.copy': ['复制', 'Copy'], 'od.copied': ['已复制', 'Copied'],
  'od.service': ['配送方式', 'Service'], 'od.store_delivery': ['门店配送', 'Store delivery'], 'od.self_pickup': ['到店自取', 'Pickup'],
  'od.wanted': ['期望时间', 'Wanted for'], 'od.asap': ['尽快送达', 'As soon as possible'], 'od.asap_pickup': ['尽快取餐', 'As soon as possible'],
  'od.done_at': ['完成时间', 'Completed'], 'od.all_orders': ['全部订单', 'All orders'],
})

// "2026-11-18 17:41" on the store's clock.
export function stamp(iso: string, timeZone?: string): string {
  const d = new Date(iso)
  try {
    return d.toLocaleDateString('en-CA', { timeZone }) + ' ' + d.toLocaleTimeString('en-GB', { timeZone, hour: '2-digit', minute: '2-digit' })
  } catch { return d.toLocaleString(locale.value === 'zh' ? 'zh-CN' : 'en-US') }
}

export const finished = (status: string) => status === 'delivered' || status === 'picked_up' || status === 'completed'
export const closedStatus = (status: string) => ['rejected', 'cancelled', 'delivery_failed'].includes(status)

// The short status of a card in the list.
export function cardStatus(c: { display_status: string; can_review: boolean; aftersale: string }): string {
  if (c.aftersale) return t('as.' + c.aftersale)
  if (finished(c.display_status)) return t(c.can_review ? 'ol.to_review' : 'ol.done')
  return t('s.' + c.display_status)
}

// orderAgain puts an earlier order's dishes into that store's cart (replacing
// what was there) and opens the store, where prices and stock are today's.
export async function orderAgain(router: Router, token: string): Promise<string> {
  const r = await get<Reorder>(`/customer/orders/${token}/reorder`)
  if (r.lines.length) replaceCart(r.slug, r.service_type, r.lines)
  const note = !r.lines.length ? t('ol.gone_all')
    : r.missing.length ? t('ol.gone_some', { names: r.missing.map(m => pick(m.name, m.name_en)).join('、') }) : ''
  await router.push(`/s/${r.slug}`)
  return note
}
