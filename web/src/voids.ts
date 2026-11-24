// Voiding dishes and refunds by dish (docs/VOID_ITEM_DESIGN.md): texts and
// helpers shared by the store's order pages, the table bill, the kitchen,
// tickets, the void log and the customer's order page.
import { ApiError } from './api'
import { extend, has, money, t } from './i18n'
import type { Order, OrderItem, OrderVoid } from './types'

extend({
  'vd.btn': ['退菜', 'Void'],
  'vd.title': ['退菜 · {name}', 'Void {name}'],
  'vd.qty': ['退几份', 'How many'],
  'vd.reason': ['原因', 'Reason'],
  'vd.required': ['必选', 'Required'],
  'vd.write': ['手写', 'Write one'],
  'vd.kind.customer': ['顾客原因', 'Customer’s reason'],
  'vd.kind.store': ['店铺原因', 'Store’s reason'],
  'vd.kind_hint.customer': ['满减按剩下的菜重新算，可能降档', 'The offer is worked out again on what is left and may drop'],
  'vd.kind_hint.store': ['店里的原因，满减照旧不降', 'The store’s fault: the offer stays as it was'],
  'vd.reasons.customer': ['点错了|顾客不要了', 'Wrong order|Guest changed mind'],
  'vd.reasons.store': ['上太慢|菜品有问题|沽清了', 'Too slow|Quality issue|Ran out'],
  'vd.st.free': ['还没开始做，退了库存退回', 'Not started: the stock goes back'],
  'vd.st.preparing': ['厨房已在做 {n} 分钟，退了算损耗', 'The kitchen started it {n} min ago: it counts as waste'],
  'vd.st.preparing0': ['厨房已在做，退了算损耗', 'The kitchen started it: it counts as waste'],
  'vd.st.ready': ['已经做好，退了算损耗', 'Already made: it counts as waste'],
  'vd.st.served': ['已上桌，退了算损耗', 'Already served: it counts as waste'],
  'vd.st.packed': ['已打包，退了算损耗', 'Already packed: it counts as waste'],
  'vd.before': ['应付', 'Due'],
  'vd.less': ['少收 {s}', '{s} less'],
  'vd.promo_down': ['满减从{a}降到{b}（顾客原因）', 'The offer drops from {a} to {b} (customer’s reason)'],
  'vd.promo_none': ['没有满减', 'no offer'],
  'vd.rider': ['会通知骑手 {name}，收款改为 {s}', 'Rider {name} is told to collect {s}'],
  'vd.packed': ['已打包，退菜后要从袋里拿出来再确认', 'Already packed: take it out of the bag and confirm packing again'],
  'vd.need': ['退已做的菜要经理授权', 'Voiding a dish already made needs a manager'],
  'vd.ask': ['请经理授权', 'Ask a manager'],
  'vd.ok': ['确认退菜', 'Void'],
  'vd.ok_less': ['确认退菜 · 少收 {s}', 'Void · {s} less'],
  'vd.cancel': ['取消', 'Cancel'],
  'vd.what': ['退已做的菜：{name}', 'Void a dish already made: {name}'],
  'vd.line': ['已退 {n} 份 · {r} · {at}', 'Voided {n} · {r} · {at}'],
  'vd.line_by': ['已退 {n} 份 · {r} · {at} {by}', 'Voided {n} · {r} · {at} by {by}'],
  'vd.closed': ['已经出店，不能再退菜；送达或取走后可以按菜退款', 'The order has left the store: no more voids. Once received, refund by dish.'],
  'vd.done': ['已退菜', 'Voided'],
  'vd.done_hint': ['给顾客的消息已复制，可以直接粘贴发送（可以先改）。', 'The message for the customer is copied: paste it to send (edit it first if you like).'],
  'vd.copy': ['复制', 'Copy'],
  'vd.copied': ['已复制', 'Copied'],
  'vd.close': ['好的', 'OK'],
  'vd.print': ['打印退菜单', 'Print void slip'],
  'vd.msg.head': ['{name}，您的订单 {no} 退了：', '{name}, we took off your order {no}:'],
  'vd.msg.head0': ['您的订单 {no} 退了：', 'We took off your order {no}:'],
  'vd.msg.line': ['{dish} ×{n}（{r}，抱歉）', '{dish} ×{n} ({r}, sorry)'],
  'vd.msg.total': ['订单合计改为 {s}（原 {o}），其他菜照常{go}。', 'The order is now {s} (was {o}); the rest comes as usual.'],
  'vd.msg.go_d': ['送出', 'sent'],
  'vd.msg.go_p': ['可取', 'ready'],
  'vd.msg.link': ['查看订单：{s}', 'See the order: {s}'],
  'vd.err.VOID_CLOSED': ['这张单已经出店，不能再退菜', 'This order has left the store: dishes can no longer be voided'],
  'vd.err.LAST_DISH': ['这是最后一道菜，请直接取消订单', 'This is the last dish: cancel the order instead'],
  'vd.err.VOID_NEEDS_APPROVAL': ['厨房已经开始做了，要经理授权', 'The kitchen started it: a manager has to approve'],
  'vd.err.VERSION_CONFLICT': ['订单刚刚变了，请再看一下', 'The order just changed: check it again'],
  'vd.err.APPROVAL_INVALID': ['授权已用过或过期，请重新授权', 'The approval was used or has expired. Ask again.'],
  // refunds by dish
  'vd.r.btn': ['按菜退款', 'Refund by dish'],
  'vd.r.title': ['按菜退款 · {no}', 'Refund by dish · {no}'],
  'vd.r.hint': ['顾客已经付过钱。选要退的菜，金额按退菜规则算好，钱在系统外退给顾客，这里记一笔。', 'The customer has paid. Pick the dishes; the amount follows the void rules. Give the money back outside the system and record it here.'],
  'vd.r.pick': ['退哪些菜', 'Which dishes'],
  'vd.r.amount': ['退款金额', 'Amount'],
  'vd.r.suggested': ['建议 {s}（最多）', 'Suggested {s} (at most)'],
  'vd.r.method': ['退款方式', 'How it is given back'],
  'vd.r.methods': ['现金|转账|电子钱包|其他', 'Cash|Transfer|E-wallet|Other'],
  'vd.r.ok': ['记录退款 {s}', 'Record refund {s}'],
  'vd.r.done': ['已记录退款 {s}', 'Refund of {s} recorded'],
  'vd.r.row': ['已退款 {s} · {m} · {r} · {at}', 'Refunded {s} · {m} · {r} · {at}'],
  'vd.r.total': ['已退款合计 {s}', 'Refunded in all {s}'],
  'vd.r.need': ['按菜退款要售后权限', 'Refunds need the after-sales permission'],
  'vd.err.REFUND_NOT_YET': ['顾客还没收到，先退菜就好', 'The customer has not received it yet: void the dish instead'],
  // the kitchen
  'vd.k.alert': ['退菜 {n} 份：{name}（{r}）', 'Void {n}: {name} ({r})'],
  'vd.k.ack': ['知道了', 'Got it'],
  'vd.k.voided': ['已退', 'Voided'],
  // the ticket
  'vd.t.head': ['退菜', 'VOID'],
  'vd.t.no': ['单号 {no}', 'Order {no}'],
  'vd.t.reason': ['原因：{r}', 'Reason: {r}'],
  'vd.t.by': ['退菜人：{by}', 'Voided by: {by}'],
  'vd.t.approved': ['授权：{by}', 'Approved by: {by}'],
  // the customer
  'vd.c.banner': ['商家退了 {n} 份菜：{dish}（{r}）· 少收 {s}', 'The store took off {n}: {dish} ({r}) · {s} less'],
  'vd.c.struck': ['商家已退', 'Taken off by the store'],
  'vd.c.was': ['原 {s}', 'was {s}'],
  'vd.c.refund': ['商家已退款 {s}', 'The store refunded {s}'],
  // the void log
  'vd.log.title': ['退菜记录', 'Voids'],
  'vd.log.count': ['退菜次数', 'Voids'],
  'vd.log.amount': ['退菜金额', 'Voided'],
  'vd.log.wasted': ['其中损耗', 'Of which waste'],
  'vd.log.refund': ['按菜退款', 'Refunds'],
  'vd.log.refund_n': ['{n} 单', '{n} orders'],
  'vd.log.by_reason': ['按原因', 'By reason'],
  'vd.log.by_staff': ['按员工', 'By staff'],
  'vd.log.by_dish': ['按菜品', 'By dish'],
  'vd.log.rows': ['明细', 'Details'],
  'vd.log.none': ['这一天没有退菜', 'No voids on this day'],
  'vd.log.time': ['时间', 'Time'],
  'vd.log.order': ['单号', 'Order'],
  'vd.log.dish': ['菜', 'Dish'],
  'vd.log.qty': ['份数', 'Qty'],
  'vd.log.money': ['金额', 'Amount'],
  'vd.log.reason': ['原因', 'Reason'],
  'vd.log.stage': ['做到哪一步', 'Stage'],
  'vd.log.who': ['谁退的', 'By'],
  'vd.log.approved': ['授权', 'Approved'],
  'vd.log.ack': ['厨房确认', 'Kitchen saw'],
  'vd.log.refund_row': ['退款', 'Refund'],
  'vd.log.times': ['{n} 次', '{n}×'],
  'vd.stage.pending': ['未开始', 'Not started'],
  'vd.stage.preparing': ['在做', 'Cooking'],
  'vd.stage.ready': ['已做好', 'Made'],
  'vd.stage.served': ['已上桌', 'Served'],
  'vd.stage.packed': ['已打包', 'Packed'],
})

export const splitList = (key: string) => t(key).split('|').map(s => s.trim()).filter(Boolean)
// A tapped reason is the customer's when it is one of the customer reasons.
export const customerReasons = () => splitList('vd.reasons.customer')
export const kindOf = (reason: string): 'store' | 'customer' => customerReasons().includes(reason) ? 'customer' : 'store'

export function voidErr(e: unknown): string {
  if (e instanceof ApiError && has(`vd.err.${e.code}`)) return t(`vd.err.${e.code}`)
  return (e as Error).message
}

// The voids of one line: the line itself when it was cancelled whole, or the
// lines split off it.
export const voidsOf = (o: Order, it: OrderItem): OrderVoid[] => (o.voids ?? []).filter(v => v.item_id === it.id)

const clock = (iso: string, tz?: string) => new Date(iso).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', timeZone: tz })
export function voidNote(v: OrderVoid, tz?: string): string {
  return v.by ? t('vd.line_by', { n: v.qty, r: v.reason, at: clock(v.at, tz), by: v.by }) : t('vd.line', { n: v.qty, r: v.reason, at: clock(v.at, tz) })
}

// What the desk sends the customer after a void.
export function voidMessage(o: Order, v: OrderVoid, token: string, contact: string): string {
  const head = contact ? t('vd.msg.head', { name: contact, no: o.public_no }) : t('vd.msg.head0', { no: o.public_no })
  const lines = [head, t('vd.msg.line', { dish: v.name, n: v.qty, r: v.reason }),
    t('vd.msg.total', { s: money(v.total_after, o.currency), o: money(v.total_before, o.currency), go: t(o.service_type === 'pickup' ? 'vd.msg.go_p' : 'vd.msg.go_d') })]
  if (token) lines.push(t('vd.msg.link', { s: `${location.origin}/o/${token}` }))
  return lines.join('\n')
}

export async function copyText(text: string) {
  try { await navigator.clipboard.writeText(text); return } catch { /* fall back below */ }
  const ta = document.createElement('textarea')
  ta.value = text; document.body.appendChild(ta); ta.select()
  try { document.execCommand('copy') } finally { ta.remove() }
}
