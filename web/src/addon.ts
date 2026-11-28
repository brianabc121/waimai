// Adding dishes to a delivery or pickup order before it leaves the store
// (docs/ADD_ON_ORDER_DESIGN.md): texts and helpers shared by the store's
// order pages, the order desk, the kitchen, the rider and the customer.
import { extend, has, t } from './i18n'
import type { Order, OrderBatch, OrderItem } from './types'

extend({
  'ao.orig': ['原单', 'Original order'],
  'ao.n': ['加单 {n}', 'Add-on {n}'],
  'ao.k': ['加单 {n}', 'add-on {n}'],
  'ao.tag': ['加单', 'Add-on'],
  'ao.src.customer': ['顾客加单', 'Added by the customer'],
  'ao.src.web': ['顾客下单', 'Ordered by the customer'],
  'ao.src.desk': ['代客下单', 'Placed by the store'],
  'ao.src.staff': ['店员 {name}', 'Staff {name}'],
  'ao.src.staff0': ['店员', 'Staff'],
  'ao.btn': ['＋加单', '＋ Add items'],
  'ao.until_delivery': ['骑手取餐前都能加', 'Items can be added until the rider collects it'],
  'ao.until_pickup': ['顾客取走前都能加', 'Items can be added until the customer collects it'],
  'ao.repack': ['已打包，加单后要重新打包', 'Packed: adding items means packing it again'],
  'ao.closed': ['已经出店，不能再加单', 'The order has left the store; no more items can be added'],
  'ao.pending_head': ['顾客加单 {n} 份 +{s} · 等确认', 'Customer added {n} more +{s} · to confirm'],
  'ao.pending_left': ['{n} 分钟内不处理自动失效', 'Drops by itself in {n} min'],
  'ao.accept': ['确认加单', 'Accept'],
  'ao.reject': ['拒绝', 'Decline'],
  'ao.reasons': ['商品卖完了|来不及做|骑手已出发|其他', 'Sold out|No time to make it|The rider has left|Other'],
  'ao.reject_title': ['选一个原因（顾客看得到）', 'Pick a reason (the customer sees it)'],
  'ao.st.pending': ['等商家确认', 'Waiting for the store'],
  'ao.st.accepted': ['已确认', 'Accepted'],
  'ao.st.rejected': ['商家未接受', 'Declined by the store'],
  'ao.st.expired': ['商家未及时确认，已失效', 'Not confirmed in time'],
  'ao.st.cancelled': ['已撤回', 'Withdrawn'],
  'ao.print_batch': ['打印这一批', 'Print this batch'],
  'ao.was': ['原 {s}', 'was {s}'],
  'ao.changed': ['加单后应收 {b}（原 {a}）', 'Collect {b} after the add-on (was {a})'],
  // the order desk in add-on mode
  'ao.m.title': ['给 {no} 加单', 'Add items to {no}'],
  'ao.m.this': ['这次加的商品', 'Items to add'],
  'ao.m.ro': ['加单沿用原单的顾客、地址和配送费，这里不能改。', 'The customer, address and delivery fee stay as on the order.'],
  'ao.m.fee_same': ['{s} · 不另收', '{s} · no extra charge'],
  'ao.m.before': ['原来', 'Was'],
  'ao.m.after': ['加单后', 'Now'],
  'ao.m.extra': ['多收', 'Extra'],
  'ao.m.promo_up': ['满减升档：{s}', 'Better offer: {s}'],
  'ao.m.confirm': ['确认加单 · +{s}', 'Add to the order · +{s}'],
  'ao.m.done': ['已给 {no} 加单', 'Items added to {no}'],
  'ao.m.done_hint': ['给顾客的消息已复制，可以直接粘贴发送。', 'The message for the customer is copied; paste it to send.'],
  'ao.m.back': ['回到订单', 'Back to orders'],
  'ao.m.missing': ['找不到这张订单', 'This order was not found'],
  'ao.m.round_tags': ['整轮要求（后厨看得到）', 'For the whole batch (the kitchen sees it)'],
  'ao.err.ADDON_CLOSED': ['这张单已经出店，不能再加单', 'This order has left the store; no more items can be added'],
  'ao.err.QUOTE_CHANGED': ['金额变了，请看一下新金额再确认', 'The amount changed: check it and confirm again'],
  'ao.err.ADDON_PENDING': ['上一次加的商品商家还没确认', 'The store has not answered your last add-on yet'],
  'ao.err.ADDON_LIMIT': ['这张单不能再加单了', 'This order cannot take more add-ons'],
  'ao.err.ADDON_DECIDED': ['这次加单已经处理过或已失效', 'This add-on was already answered or has expired'],
  'ao.msg.head': ['{name}，已为您在订单 {no} 加了：', '{name}, we added to order {no}:'],
  'ao.msg.head0': ['已为您在订单 {no} 加了：', 'We added to order {no}:'],
  'ao.msg.total_d': ['订单合计改为 {s}（原 {o}），和原来的商品一起送，不另收配送费。', 'The order is now {s} (was {o}), delivered together with no extra delivery fee.'],
  'ao.msg.total_p': ['订单合计改为 {s}（原 {o}），和原来的商品一起取。', 'The order is now {s} (was {o}); collect it all together.'],
  'ao.msg.link': ['查看进度：{s}', 'Track it: {s}'],
  // the customer
  'ao.c.add': ['＋ 加单', '＋ Add items'],
  'ao.c.hint_d': ['骑手取餐前都能加，不另收配送费', 'Until the rider collects it, with no extra delivery fee'],
  'ao.c.hint_p': ['取餐前都能加', 'Until you collect it'],
  'ao.c.banner': ['正在给订单 {no} 加单 · 和原来的商品一起送', 'Adding to order {no} · comes with your order'],
  'ao.c.banner_p': ['正在给订单 {no} 加单 · 和原来的商品一起取', 'Adding to order {no} · collect it all together'],
  'ao.c.exit': ['退出加单', 'Stop adding'],
  'ao.c.go': ['去确认加单', 'Review add-on'],
  'ao.c.title': ['确认加单', 'Confirm add-on'],
  'ao.c.orig': ['原单 {n} 份（已下单）', 'Original order: {n} items (ordered)'],
  'ao.c.this': ['这次加 {n} 份', 'Adding {n} items'],
  'ao.c.to': ['送到：{s}', 'Deliver to: {s}'],
  'ao.c.pickup': ['到店自取 · 取餐号 {s}', 'Pickup · code {s}'],
  'ao.c.fee_paid': ['原单已含，不另收', 'On your order already, no extra charge'],
  'ao.c.no_coupon': ['这次加单不能再用券，原单的券照常有效。', 'Coupons cannot be used on an add-on; the ones on your order still apply.'],
  'ao.c.wait_note': ['商家确认后才会制作，{n} 分钟内没确认会自动失效。', 'The store makes them once it accepts; unanswered after {n} min they drop.'],
  'ao.c.wait_note0': ['和原来的商品一起制作。', 'They are made together with your order.'],
  'ao.c.confirm': ['确认加单 · 多付 {s}', 'Add · {s} more'],
  'ao.c.withdraw': ['撤回', 'Withdraw'],
  'ao.c.left': ['还能加 {n} 次', '{n} more add-ons allowed'],
  'ao.c.new_order': ['另下一单', 'Place a new order'],
  'ao.c.empty': ['还没选要加的商品', 'No items chosen yet'],
  'ao.c.more': ['继续选商品', 'Choose more'],
  'ao.c.total_now': ['加单后合计', 'New total'],
  // settings, tickets, packing
  'ao.set.minutes': ['顾客加单等商家确认（分钟）', 'Minutes to answer a customer’s add-on'],
  'ao.set.minutes_hint': ['超过这个时间不处理，顾客这次加的商品自动失效', 'Unanswered for longer, the customer’s add-on drops'],
  'ao.t.head': ['加单 {n}', 'Add-on {n}'],
  'ao.t.with': ['和原单 {no} 一起出', 'Goes out with order {no}'],
  'ao.p.repack': ['加了商品：已打包的袋子要再装', 'Items were added: pack it again'],
})

// addonNo is how an add-on batch is called: the order is batch 1, so batch 3 is "add-on 2".
export const addonNo = (seq: number) => seq - 1

export function batchTitle(seq: number): string {
  return seq <= 1 ? t('ao.orig') : t('ao.n', { n: addonNo(seq) })
}

// batchSource is who put a batch in.
export function batchSource(o: Order, b: Pick<OrderBatch, 'seq' | 'source' | 'by'>): string {
  if (b.seq <= 1) return o.order_source ? t('ao.src.desk') : t('ao.src.web')
  if (b.source === 'customer') return t('ao.src.customer')
  return b.by ? t('ao.src.staff', { name: b.by }) : t('ao.src.staff0')
}

// addons are the add-on batches the store has (accepted), batch 1 left out.
export const addons = (o: Order) => (o.batches ?? []).filter(b => b.seq > 1 && b.status === 'accepted')
export const pendingAddon = (o: Order) => (o.batches ?? []).find(b => b.status === 'pending') ?? null

// itemsByBatch groups an order's dishes by batch, in order.
export function itemsByBatch(o: Order): { seq: number; batch: OrderBatch | null; items: OrderItem[] }[] {
  const out = new Map<number, OrderItem[]>()
  for (const it of o.items) {
    if (!out.has(it.batch_seq)) out.set(it.batch_seq, [])
    out.get(it.batch_seq)!.push(it)
  }
  return [...out.entries()].sort((a, b) => a[0] - b[0])
    .map(([seq, items]) => ({ seq, batch: (o.batches ?? []).find(b => b.seq === seq) ?? null, items }))
}

// The first total an order had before its add-ons, when it had any.
export function totalBefore(o: Order): number | null {
  const first = addons(o)[0]
  return first?.total_before ?? null
}

export function addonErr(e: unknown): string {
  const code = (e as { code?: string })?.code
  const k = `ao.err.${code}`
  return code && has(k) ? t(k) : (e as Error).message
}

// presetSplit is a "a|b|c" text as its choices.
export const presetSplit = (key: string) => t(key).split('|').map(x => x.trim()).filter(Boolean)
