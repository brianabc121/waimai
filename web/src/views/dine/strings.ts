// Texts of the dine-in desk (docs/DINE_IN_ORDER_DESIGN.md), [zh, en].
import { extend, t } from '../../i18n'
import type { DinePresets } from '../../types'

extend({
  'dd.mode_delivery': ['外卖 / 自取', 'Delivery / pickup'], 'dd.mode_dine': ['堂食', 'Dine in'],
  'dd.no_tables': ['还没有桌台。请店主或经理先在“桌台”页添加。', 'No tables yet. The owner or a manager adds them on the Tables page.'],
  'dd.all': ['全部', 'All'], 'dd.no_area': ['其他', 'Other'],
  'dd.go_desk': ['去堂食点餐', 'Open the dine-in desk'], 'dd.area': ['区域', 'Area'], 'dd.area_ph': ['如 大厅、包间、露台', 'e.g. Hall, Private room, Terrace'],
  'dd.paid': ['已收', 'Paid'],
  'dd.seats_lbl': ['座位数', 'Seats'], 'dd.edit_table': ['桌台设置', 'Table settings'],
  'dd.lg.free': ['空闲', 'Free'], 'dd.lg.busy': ['用餐中', 'Eating'], 'dd.lg.wait': ['有菜没上', 'Dishes to serve'],
  'dd.lg.long': ['超 {n} 分钟', 'Over {n} min'],
  'dd.seats': ['{n} 人桌', 'Seats {n}'], 'dd.guests': ['{g}/{s} 人', '{g}/{s} guests'], 'dd.guests_n': ['{n} 人', '{n} guests'],
  'dd.tap_open': ['点一下开台', 'Tap to open'], 'dd.not_ordered': ['还没点菜', 'Nothing ordered'],
  'dd.min': ['{n} 分', '{n} min'], 'dd.hm': ['{h} 时 {m} 分', '{h} h {m} min'], 'dd.rounds': ['{n} 轮', '{n} rounds'],
  'dd.to_serve': ['{n} 道没上', '{n} to serve'], 'dd.opened': ['开台 {t}', 'Opened {t}'],
  'dd.ordered': ['已点', 'Ordered'], 'dd.minutes': ['分钟', 'Minutes'], 'dd.unserved': ['道没上', 'To serve'],
  'dd.add': ['＋ 加菜', '+ Add dishes'], 'dd.bill': ['账单 / 退菜', 'Bill / void'], 'dd.pay': ['结账', 'Pay'],
  'dd.move': ['换桌', 'Move'], 'dd.guests_btn': ['改人数', 'Guests'], 'dd.pick_table': ['点一张桌台，这里会显示它的菜和操作。', 'Tap a table to see its dishes and actions.'],
  'dd.open_title': ['开台 · {name}', 'Open {name}'], 'dd.how_many': ['几位客人', 'How many guests'], 'dd.tap_once': ['点一下就选好', 'One tap'],
  'dd.more': ['12 以上', '12+'], 'dd.table_tags': ['这桌备注', 'Table notes'],
  'dd.table_tags_hint': ['选填，点选，后厨和小票都看得到', 'Optional; the kitchen and receipts show them'],
  'dd.other': ['＋ 其他', '+ Other'], 'dd.write': ['＋ 手写一条', '+ Write one'], 'dd.write_ph': ['写好按“添加”', 'Type, then Add'], 'dd.write_add': ['添加', 'Add'],
  'dd.open_only': ['只开台', 'Open only'], 'dd.open_order': ['开台并点菜 →', 'Open and order →'],
  'dd.round_n': ['第 {n} 轮加菜', 'Round {n}'], 'dd.by_code': ['🔢 按编号点', '🔢 By code'], 'dd.code_hint': ['点编号的字母和数字', 'Tap the letters and digits of a code'],
  'dd.this_round': ['这一轮', 'This round'], 'dd.not_sent': ['还没送厨', 'Not sent yet'], 'dd.clear': ['清空', 'Clear'],
  'dd.round_tags': ['整轮要求（点选，可多选）', 'For the whole round (tap any)'],
  'dd.sent_summary': ['已送厨 {r} 轮 · {n} 份 {amt}', 'Sent: {r} rounds · {n} · {amt}'], 'dd.view': ['查看 ›', 'View ›'],
  'dd.round_total': ['这一轮 {n} 份', 'This round: {n}'], 'dd.cancel': ['取消', 'Cancel'], 'dd.send': ['送厨（{n} 份）', 'Send ({n})'],
  'dd.sent_ok': ['{t} 第 {n} 轮已送厨', '{t}: round {n} sent'], 'dd.empty_round': ['点左边的菜加进来', 'Tap dishes to add them'],
  'dd.discard': ['清空这一轮，回到桌台？', 'Clear this round and go back?'], 'dd.top': ['常点', 'Popular'],
  'dd.from_price': ['{p} 起', 'from {p}'], 'dd.spec': ['规格', 'Options'], 'dd.notes': ['口味', 'Notes'], 'dd.left': ['剩 {n}', '{n} left'],
  'dd.required': ['必选', 'Required'], 'dd.paid_multi': ['可多选，加价', 'Pick any, extra charge'], 'dd.paid_single': ['单选', 'Pick one'],
  'dd.tpl_single': ['口味 · 单选，不加钱', 'Notes · pick one, free'], 'dd.tpl_multi': ['可多选', 'Pick any'],
  'dd.add_round': ['加入这一轮 {amt}', 'Add · {amt}'], 'dd.update_round': ['改好了 {amt}', 'Update · {amt}'], 'dd.choose_first': ['先选：{g}', 'Choose: {g}'],
  'dd.remove': ['删掉', 'Remove'], 'dd.price_changed': ['价格有变化，这一轮现在是 {amt}。', 'Prices changed; this round is now {amt}.'],
  'dd.send_new': ['按新价格送厨', 'Send at the new price'],
  'dd.round_head': ['第 {n} 轮', 'Round {n}'], 'dd.src.staff': ['店员 {name}', 'Staff {name}'], 'dd.src.staff0': ['店员', 'Staff'],
  'dd.src.qr': ['顾客扫码', 'Guest (QR)'], 'dd.src.ipad': ['平板', 'Tablet'], 'dd.src.web': ['网页', 'Web'],
  'dd.st.cook': ['制作中', 'Cooking'], 'dd.st.cook_m': ['制作中 {n} 分', 'Cooking {n} min'], 'dd.st.ready': ['已备好', 'Ready'],
  'dd.st.served': ['已上桌', 'Served'], 'dd.st.void': ['已退', 'Voided'], 'dd.st.rush': ['催', 'Rush'],
  'dd.serve': ['✓ 上桌', '✓ Serve'], 'dd.again': ['再来一份', 'One more'], 'dd.rush': ['催一下', 'Rush'], 'dd.void': ['退菜', 'Void'],
  'dd.rushed': ['已催', 'Rushed'], 'dd.void_title': ['退菜 · {name}', 'Void {name}'], 'dd.void_qty': ['退几份', 'How many'],
  'dd.reason': ['原因', 'Reason'], 'dd.void_note': ['已开始做的菜退掉，后厨屏会显示“已退”；金额从账单扣掉。', 'The kitchen screen shows it as voided; the amount comes off the bill.'],
  'dd.void_ok': ['确认退菜', 'Void'], 'dd.voided': ['退菜：{r}', 'Voided: {r}'],
  'dd.pay_title': ['结账关台', 'Pay and close'], 'dd.items_n': ['菜品 {n} 份', '{n} dishes'], 'dd.service': ['服务费', 'Service charge'],
  'dd.tax': ['税', 'Tax'], 'dd.tax_incl': ['含税', 'Tax included'], 'dd.due': ['应付', 'Due'], 'dd.received': ['实收', 'Received'],
  'dd.exact': ['刚好', 'Exact'], 'dd.change': ['找零', 'Change'], 'dd.close': ['收款并关台', 'Take payment and close'],
  'dd.cooking_block': ['还有 {n} 道菜在做，先上桌或退菜', '{n} dishes are still cooking: serve or void them first'],
  'dd.short': ['实收少于应付', 'Less than the amount due'], 'dd.closed_title': ['{name} 已关台', '{name} is closed'],
  'dd.print': ['打印小票', 'Print receipt'], 'dd.done': ['完成', 'Done'],
  'dd.move_title': ['把 {name} 换到', 'Move {name} to'], 'dd.move_hint': ['只能换到空桌，账单一起带过去', 'Free tables only; the bill moves along'],
  'dd.moved_from': ['从 {name} 换来', 'Moved from {name}'], 'dd.moved': ['已换到 {name}', 'Moved to {name}'],
  'dd.guests_title': ['改人数 · {name}', 'Guests at {name}'], 'dd.save': ['保存', 'Save'], 'dd.back_tables': ['桌台', 'Tables'],
  'dd.settings': ['堂食设置', 'Dine-in settings'], 'dd.set.long': ['坐多久变红', 'Mark red after'],
  'dd.set.after': ['送厨后', 'After sending'], 'dd.set.after_tables': ['回到桌台', 'Back to tables'], 'dd.set.after_stay': ['留在这一桌', 'Stay at the table'],
  'dd.set.photos': ['菜品格显示图片', 'Dish photos'], 'dd.on': ['显示', 'On'], 'dd.off': ['不显示', 'Off'],
  'dd.set.table_tags': ['这桌备注的选项', 'Table note choices'], 'dd.set.round_tags': ['整轮要求的选项', 'Whole-round choices'],
  'dd.set.void': ['退菜原因的选项', 'Void reasons'], 'dd.set.pay': ['付款方式', 'Payment methods'],
  'dd.set.hint': ['点标签删掉；“＋ 其他”加一个。', 'Tap a tag to remove it; “+ Other” adds one.'],
  'dd.set.reset': ['恢复默认', 'Reset'], 'dd.saved': ['已保存', 'Saved'],
  'dd.offline': ['网络断了，正在重连…', 'Offline, reconnecting…'], 'dd.no_perm': ['你的角色不能用堂食。', 'Your role cannot use dine-in.'],
  'dd.dish': ['菜品', 'Dishes'], 'dd.no_items': ['这个分类没有可以堂食的菜', 'No dine-in dishes here'],
  // defaults, separated by |
  'dd.def.table_tags': ['有小孩|有老人|赶时间|生日|VIP', 'Children|Elderly|In a hurry|Birthday|VIP'],
  'dd.def.round_tags': ['先上饮料|菜齐再上|全部少辣|分小碗|加急', 'Drinks first|All together|Less spicy|Small bowls|Urgent'],
  'dd.def.void': ['点错了|顾客不要了|上太慢|菜品有问题|沽清了', 'Wrong order|Guest changed mind|Too slow|Quality issue|Ran out'],
  'dd.def.pay': ['现金|刷卡|转账', 'Cash|Card|Transfer'], 'dd.wallet': ['电子钱包', 'E-wallet'],
  // kitchen and receipts
  'dd.k.rush': ['催菜', 'Rush'], 'dd.k.table': ['桌 {name}', 'Table {name}'], 'dd.pay_method': ['付款方式', 'Paid by'],
})

const split = (k: string) => t(k).split('|').map(s => s.trim()).filter(Boolean)

// Wallets people pay with, by the store's country, put after cash and card.
const WALLETS: Record<string, string> = {
  PH: 'GCash|Maya', TH: 'PromptPay|TrueMoney', VN: 'MoMo|ZaloPay', MY: "Touch 'n Go|GrabPay",
  SG: 'PayNow|GrabPay', ID: 'GoPay|OVO', MM: 'KBZPay|WavePay', KH: 'ABA|Wing', LA: 'BCEL One',
  CN: '微信支付|支付宝', HK: '八达通|PayMe', TW: 'LINE Pay|街口支付', JP: 'PayPay',
}

export function presetList(p: DinePresets | null, which: 'table_tags' | 'round_tags' | 'void_reasons' | 'pay_methods', country = ''): string[] {
  const own = p?.[which]
  if (own) return own
  if (which === 'table_tags') return split('dd.def.table_tags')
  if (which === 'round_tags') return split('dd.def.round_tags')
  if (which === 'void_reasons') return split('dd.def.void')
  const [cash, card, transfer] = split('dd.def.pay')
  return [cash, card, ...(WALLETS[country.toUpperCase()]?.split('|') ?? [t('dd.wallet')]), transfer].filter(Boolean)
}

export const longMinutes = (p: DinePresets | null) => p?.long_minutes || 90

export function sinceText(iso: string, now: number): string {
  const m = Math.max(0, Math.floor((now - new Date(iso).getTime()) / 60000))
  return m < 60 ? t('dd.min', { n: m }) : t('dd.hm', { h: Math.floor(m / 60), m: String(m % 60).padStart(2, '0') })
}
