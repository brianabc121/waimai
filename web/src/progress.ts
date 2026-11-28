// Cooking progress in the back office (docs/COOKING_PROGRESS_DESIGN.md): texts
// and the rules that put an order in "not started", "cooking" or "ready".
// The order page, the kitchen screen and the progress page share them.
import { extend } from './i18n'
import type { Order, OrderItem } from './types'

extend({
  'kp.nav': ['烹饪进度', 'Cooking progress'],
  'kp.tab.orders': ['按订单', 'By order'],
  'kp.tab.dishes': ['按商品', 'By item'],
  'kp.tab.done': ['已完成', 'Finished'],
  'kp.tab.done_n': ['已完成 · 今天 {n}', 'Finished · today {n}'],
  'kp.live': ['实时 · 同步于 {t}', 'Live · synced {t}'],
  'kp.s.active': ['在做的单', 'Orders in the kitchen'],
  'kp.s.late': ['超时（目标 {n} 分钟）', 'Late (target {n} min)'],
  'kp.s.ready': ['做好待出', 'Ready, not out yet'],
  'kp.s.avg': ['今天平均出餐', 'Average today'],
  'kp.s.later': ['预约未到时间', 'Scheduled, not due'],
  'kp.mins': ['{n} 分钟', '{n} min'],
  'kp.col.wait': ['还没开始', 'Not started'],
  'kp.col.wait_hint': ['一道都没开始', 'Nothing started'],
  'kp.col.cook': ['正在做', 'Cooking'],
  'kp.col.cook_hint': ['有商品开始或做好了', 'Some items started or ready'],
  'kp.col.done': ['做好了', 'Ready'],
  'kp.col.done_hint': ['待打包 / 待取 / 待上桌', 'To pack / collect / serve'],
  'kp.col.empty': ['没有', 'None'],
  'kp.all': ['全部 {n}', 'All {n}'],
  'kp.type_n': ['{s} {n}', '{s} {n}'],
  'kp.all_stations': ['全部工位', 'All stations'],
  'kp.only_late': ['只看超时', 'Late only'],
  'kp.sort.wait': ['等得最久在前', 'Longest wait first'],
  'kp.sort.placed': ['下单先后', 'By order time'],
  'kp.progress': ['{a}/{b} 份做好', '{a}/{b} ready'],
  'kp.progress_cook': ['{n} 份在做', '{n} cooking'],
  'kp.free': ['另有 {n} 份免制作', '{n} more need no cooking'],
  'kp.d.wait': ['待做', 'To do'],
  'kp.d.wait_min': ['待做 {n} 分钟', 'Waiting {n} min'],
  'kp.d.cook_min': ['在做 {n} 分钟', 'Cooking {n} min'],
  'kp.d.ready_at': ['{t} 做好', 'Ready {t}'],
  'kp.d.served': ['已上桌', 'Served'],
  'kp.rush_tag': ['催', 'Rush'],
  'kp.rush': ['催单', 'Rush'],
  'kp.rushed': ['已催单，后厨屏这件商品标了红色“催”', 'Rushed: the kitchen screen marks this item'],
  'kp.st.to_pack': ['待打包', 'To pack'],
  'kp.st.to_rider': ['等骑手取餐', 'Waiting for the rider'],
  'kp.st.to_collect': ['等顾客来取', 'Waiting for the customer'],
  'kp.st.to_serve': ['等上桌', 'To serve'],
  'kp.pending_batch': ['顾客加单 · 等确认', 'Customer add-on · to confirm'],
  'kp.pending_note': ['不算进度，确认后才进厨房', 'Not counted; goes to the kitchen once accepted'],
  'kp.round': ['第 {n} 轮', 'Round {n}'],
  'kp.cancelled': ['{no} 刚被取消（{t}）：{s} 不用做了。', '{no} was just cancelled ({t}): stop making {s}.'],
  'kp.later': ['预约未到时间 {n} 单', '{n} scheduled, not due yet'],
  'kp.later_at': ['{t} 开做', 'starts {t}'],
  'kp.foot': ['厨师在后厨屏点了“开始”才会显示“在做”；直接点“备好”的商品会从待做直接变成做好。',
    'An item shows as cooking once the cook taps Start; one marked Ready straight away goes from waiting to ready.'],
  'kp.empty': ['厨房现在没有要做的单', 'Nothing in the kitchen right now'],
  'kp.open': ['打开订单', 'Open order'],
  'kp.print': ['打印小票', 'Print ticket'],
  'kp.close': ['关闭', 'Close'],
  'kp.into_kitchen': ['{t} 进厨房 · 目标 {n} 分钟', 'In the kitchen {t} · target {n} min'],
  'kp.tl.start': ['{who} {t} 开始', '{who} started {t}'],
  'kp.tl.ready': ['{who} {t} 备好', '{who} ready {t}'],
  'kp.tl.rush': ['{t} 催过单', 'Rushed {t}'],
  'kp.only_board': ['“开始”“备好”只在后厨屏和员工工作台点，这里只能催单。', 'Start and Ready are tapped on the kitchen screen or the workbench; here you can only rush.'],
  'kp.dish.station': ['在做 {a} 份 · 待做 {b} 份', '{a} cooking · {b} to do'],
  'kp.dish.busiest': ['最忙', 'Busiest'],
  'kp.dish.col.name': ['商品', 'Item'],
  'kp.dish.col.wait': ['待做', 'To do'],
  'kp.dish.col.cook': ['在做', 'Cooking'],
  'kp.dish.col.ready': ['做好待出', 'Ready'],
  'kp.dish.col.longest': ['最久等了', 'Longest wait'],
  'kp.dish.col.orders': ['哪几单', 'Orders'],
  'kp.dish.split': ['口味分开', 'Split by flavour'],
  'kp.dish.merge': ['只按商品名合计', 'By name only'],
  'kp.dish.empty': ['没有要做的商品', 'No items to make'],
  'kp.dish.short': ['待 {a} · 在 {b} · 好 {c}', 'to do {a} · cooking {b} · ready {c}'],
  'kp.done.count': ['今天做好的单（含加单、堂食每轮）', 'Finished today (add-ons and rounds counted apart)'],
  'kp.done.avg': ['平均出餐', 'Average time'],
  'kp.done.late': ['超时（超过 {n} 分钟）', 'Late (over {n} min)'],
  'kp.done.median': ['一半的单在这之内做好', 'Half were done within'],
  'kp.done.report': ['按员工、按天看 → 员工 › 工作汇总 › 后厨', 'By person and day → Staff › Work summary › Kitchen'],
  'kp.done.col.no': ['单号', 'Order'],
  'kp.done.col.type': ['类型', 'Type'],
  'kp.done.col.qty': ['商品', 'Items'],
  'kp.done.col.in': ['进厨房', 'In kitchen'],
  'kp.done.col.done': ['全部做好', 'All ready'],
  'kp.done.col.mins': ['出餐用时', 'Time taken'],
  'kp.done.col.cooks': ['谁做的', 'Cooked by'],
  'kp.done.col.now': ['现在', 'Now'],
  'kp.done.qty': ['{n} 份', '{n}'],
  'kp.done.late_tag': ['{n} 分钟 · 超时', '{n} min · late'],
  'kp.done.empty': ['这一天还没有做好的单', 'Nothing finished on this day'],
  'kp.done.picked': ['骑手 {who} 已取 {t}', 'Rider {who} collected {t}'],
  'kp.done.out': ['已出店', 'Left the store'],
  'kp.done.delivered': ['已送达', 'Delivered'],
  'kp.done.collected': ['顾客已取', 'Collected'],
  'kp.done.served': ['已上桌', 'Served'],
  'kp.done.closed': ['已结账', 'Paid and closed'],
  'kp.done.cancelled': ['已取消', 'Cancelled'],
  'kp.done.waiting': ['还在店里', 'Still in the store'],
  'kp.order_tag': ['{a}/{b} 份做好', '{a}/{b} ready'],
  'kp.set.target': ['出餐目标时间（分钟）', 'Cooking target (minutes)'],
  'kp.set.target_hint': ['烹饪进度和后厨屏的计时到这个时间的 2/3 变黄，超过变红', 'Timers on the progress page and the kitchen screen turn yellow at two thirds of it and red after it'],
})

// The dishes of an order the kitchen makes, as the page counts them: not
// cancelled, needing the kitchen, and (dine-in) not at the table yet.
export function kitchenLines(o: Order): OrderItem[] {
  return o.items.filter(i => i.needs_prep && i.prep_status !== 'cancelled' && !i.served_at)
}

export type Column = 'wait' | 'cook' | 'done'

export function column(lines: OrderItem[]): Column {
  if (lines.every(i => i.prep_status === 'ready')) return 'done'
  if (lines.some(i => i.prep_status === 'preparing' || i.prep_status === 'ready')) return 'cook'
  return 'wait'
}

const qty = (l: OrderItem[]) => l.reduce((n, i) => n + i.qty, 0)

// counts is how many servings are ready, cooking and to make.
export function counts(lines: OrderItem[]) {
  return {
    total: qty(lines),
    ready: qty(lines.filter(i => i.prep_status === 'ready')),
    cooking: qty(lines.filter(i => i.prep_status === 'preparing')),
  }
}

// The order-page tag: how much of an accepted order the kitchen has made.
// Null when there is nothing to make.
export function readyTag(o: Order): { ready: number; total: number } | null {
  if (o.acceptance_status !== 'accepted') return null
  const lines = o.items.filter(i => i.needs_prep && i.prep_status !== 'cancelled')
  if (!lines.length) return null
  const c = counts(lines)
  return { ready: c.ready, total: c.total }
}

// timerClass colours a timer by the store's target: yellow from two thirds.
export function timerClass(minutes: number, target: number): '' | 'warn' | 'bad' {
  if (minutes > target) return 'bad'
  if (minutes * 3 >= target * 2) return 'warn'
  return ''
}
