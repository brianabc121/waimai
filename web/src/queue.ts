// The queue display (docs/QUEUE_DISPLAY_DESIGN.md): the staff's queue page,
// the paired screens and the guest's "take a number" pages share these.
import { ApiError } from './api'
import { extend, has, isZh, t, tIn, type Locale } from './i18n'
import { langInfo } from './langs'

export interface QueueSettings {
  tea: boolean; restaurant: boolean; paused: boolean
  upgrade_minutes: number; call_alert_minutes: number; requeue_after: number; dine_minutes: number
  ready_stay_minutes: number; show_delivery: boolean; show_dine_in: boolean; sms: boolean; media: boolean
}
export interface QueueGroup {
  id: number; letter: string; preset: string; label: string; label_en: string
  min_people: number; max_people: number; table_ids: number[]; sort: number
  tables: number[]; dine_minutes: number; from_history: boolean
}
export interface QueueTicket {
  id: number; number: string; group_id: number; letter: string; people: number; source: 'qr' | 'paper' | 'staff'
  phone: string; tags: string[]; status: string; calls: number; called_at: string | null
  called_table_id: number | null; called_table: string; seated_table_id: number | null; seated_table: string
  seated_at: string | null; session_id: number | null; jump_reason: string; created_at: string; closed_at: string | null
  position: number; est_minutes: number; token?: string
}
export interface QueueTable { id: number; name: string; area: string; seats: number; group_id: number
  state: 'free' | 'busy' | 'held'; held_by?: string; opened_at?: string; guests?: number; soon?: boolean }
export interface QueueSuggestion { table_id: number; ticket_id: number; upgrade: boolean }
export interface QueueDesk {
  settings: QueueSettings; groups: QueueGroup[]; tickets: QueueTicket[]; tables: QueueTable[]
  suggestions: QueueSuggestion[]
  stats: { waiting: number; called: number; seated: number; skipped: number; cancelled: number; avg_wait: number; by_source: Record<string, number> }
  timezone: string; now: string
}
export interface PublicGroup { id: number; letter: string; preset: string; label: string; label_en: string
  min_people: number; max_people: number; waiting: number; est_minutes: number; current: string; next: string[] }
export interface QueueCall { number: string; people: number; table: string; calls: number; at: string }
export interface QueuePublic {
  slug: string; store: string; store_en: string; logo: string; open: boolean; paused: boolean
  groups: PublicGroup[]; calling: QueueCall[]; skipped: string[]
  settings: { call_alert_minutes: number; requeue_after: number }; now: string
}
export interface GuestTicket {
  number: string; letter: string; people: number; source: string; status: string; ahead: number; est_minutes: number
  calls: number; called_at: string | null; table: string; created_at: string; group: PublicGroup; queue: QueuePublic
}
export interface PickupNumber { order_id: number; code: string; service_type: string; stage: 'wait' | 'cook' | 'ready'
  since: string; stamp?: string; calls: number; items?: string; leaves_at?: string; can_hand_over: boolean }
export interface TeaBoard { wait: PickupNumber[]; cook: PickupNumber[]; ready: PickupNumber[]; now: string }
export interface QueueScreen { id: number; name: string; mode: 'tea' | 'restaurant'; orientation: 'landscape' | 'portrait'
  lang: string; lang2: string; paired: boolean; paired_at: string | null; last_seen_at: string | null
  pair_expires_at: string | null; created_at: string }

extend({
  'qu.nav': ['排队叫号', 'Queue'],
  'qu.sub': ['发号、叫号、入座、插队；有空桌时自动推荐该叫谁', 'Give numbers, call, seat and reorder; free tables suggest whom to call'],
  'qu.tab.line': ['排队叫号', 'Queue'],
  'qu.tab.pickup': ['取餐叫号', 'Pickup numbers'],
  'qu.tab.log': ['排队记录', 'Queue log'],
  'qu.tab.settings': ['设置', 'Settings'],
  'qu.take': ['+ 取号', '+ New number'],
  'qu.pause': ['暂停取号', 'Pause the queue'],
  'qu.resume': ['开始取号', 'Resume the queue'],
  'qu.paused_banner': ['已暂停取号：顾客扫码不能取号，已经取的号照常叫。', 'The queue is paused: guests cannot take numbers by scanning. Numbers already given are still called.'],
  'qu.open_screen': ['打开叫号屏', 'Open the screen'],
  'qu.s.waiting': ['等待中', 'Waiting'],
  'qu.s.called': ['已叫未到', 'Called, not here yet'],
  'qu.s.seated': ['今天已入座', 'Seated today'],
  'qu.s.avg': ['平均等待', 'Average wait'],
  'qu.n_tables': ['{n} 桌', '{n} parties'],
  'qu.mins': ['{n} 分钟', '{n} min'],
  'qu.about': ['约 {n} 分钟', 'About {n} min'],
  'qu.all': ['全部', 'All'],
  'qu.soon_v': ['很快', 'Soon'],
  'qu.skipped': ['过号', 'Missed'],
  'qu.preset.small': ['小桌', 'Small table'],
  'qu.preset.medium': ['中桌', 'Medium table'],
  'qu.preset.large': ['大桌', 'Large table'],
  'qu.preset.room': ['包间', 'Private room'],
  'qu.people': ['{n} 人', '{n} guests'],
  'qu.range': ['{a}–{b} 人', '{a}–{b} guests'],
  'qu.range_up': ['{a} 人以上', '{a}+ guests'],
  'qu.src.qr': ['扫码', 'QR'],
  'qu.src.paper': ['纸质', 'Paper'],
  'qu.src.staff': ['店员', 'Staff'],
  'qu.tag.baby': ['婴儿椅', 'High chair'],
  'qu.tag.wheelchair': ['轮椅', 'Wheelchair'],
  'qu.tag.member': ['会员', 'Member'],
  'qu.st.called': ['已叫 {n} 次', 'Called {n}×'],
  'qu.st.skipped': ['过号', 'Missed'],
  'qu.ago': ['{n} 分钟前', '{n} min ago'],
  'qu.act.call': ['叫号', 'Call'],
  'qu.act.recall': ['重叫', 'Call again'],
  'qu.act.seat': ['入座', 'Seat'],
  'qu.act.skip': ['过号', 'Missed'],
  'qu.act.requeue': ['复号', 'Back in line'],
  'qu.act.jump': ['插队', 'Move up'],
  'qu.act.people': ['改人数', 'Change party size'],
  'qu.act.cancel': ['取消', 'Cancel'],
  'qu.more': ['更多', 'More'],
  'qu.empty': ['现在没有人排队', 'Nobody is waiting'],
  'qu.free': ['空桌', 'Free tables'],
  'qu.soon': ['快空的桌', 'Freeing up soon'],
  'qu.no_free': ['现在没有空桌', 'No free tables now'],
  'qu.seats': ['{n} 座', '{n} seats'],
  'qu.held': ['留给 {no}', 'Held for {no}'],
  'qu.dining': ['用餐 {n} 分钟', 'Dining {n} min'],
  'qu.sug': ['推荐 {no}：{n} 人，等了 {m} 分钟', 'Suggested {no}: {n} guests, waited {m} min'],
  'qu.sug_up': ['可升桌：{no} 等了 {m} 分钟，这桌型没人等', 'Upgrade: {no} waited {m} min and nobody waits for this size'],
  'qu.call_to': ['叫号到这桌', 'Call to this table'],
  'qu.priority': ['优先', 'Priority'],
  'qu.reason': ['原因：{r}', 'Reason: {r}'],
  'qu.done': ['已完成', 'Done'],
  'qu.t.title': ['取号', 'New number'],
  'qu.t.people': ['人数', 'Party size'],
  'qu.t.more': ['更多', 'More'],
  'qu.t.goes': ['排 {g} · 前面 {n} 桌', 'Line {g} · {n} ahead'],
  'qu.t.phone': ['手机号', 'Phone'],
  'qu.t.phone_hint': ['选填，叫号时发短信', 'Optional, for a text when called'],
  'qu.t.tags': ['标签', 'Tags'],
  'qu.t.custom': ['自定义', 'Other'],
  'qu.t.priority': ['优先（插到最前）', 'Priority (first in line)'],
  'qu.t.ok': ['取号并打印', 'Give number and print'],
  'qu.t.ok_np': ['取号，不打印', 'Give number, no slip'],
  'qu.t.done': ['已取号', 'Number given'],
  'qu.t.ahead': ['前面 {n} 桌 · 约 {m} 分钟', '{n} ahead · about {m} min'],
  'qu.t.print': ['打印号码条', 'Print slip'],
  'qu.r.title': ['原因', 'Reason'],
  'qu.r.member': ['会员', 'Member'],
  'qu.r.booking': ['预约到店', 'Booked ahead'],
  'qu.r.elder': ['老人孕妇', 'Elderly or pregnant'],
  'qu.r.staff': ['店内安排', 'Store decision'],
  'qu.r.other': ['其他', 'Other'],
  'qu.j.title': ['插队 {no}', 'Move up {no}'],
  'qu.j.first': ['插到最前', 'To the front'],
  'qu.j.pos': ['插到第几位', 'To position'],
  'qu.j.now': ['现在排第 {n} 位', 'Now at position {n}'],
  'qu.p.title': ['改人数 {no}', 'Party size of {no}'],
  'qu.p.hint': ['人数换了桌型时，号码不变，排到新桌型里', 'If the size needs another table size, the number moves to that line'],
  'qu.seat.title': ['{no} 入座', 'Seat {no}'],
  'qu.seat.hint': ['推荐的桌放在最前，也可以选任何空桌', 'The suggested table comes first; any free table works'],
  'qu.seat.rec': ['推荐', 'Suggested'],
  'qu.seat.small': ['这桌只有 {n} 个座位', 'This table has only {n} seats'],
  'qu.seat.held_q': ['这桌留给 {no}，仍然让 {me} 入座？', 'This table is held for {no}. Seat {me} anyway?'],
  'qu.seat.done': ['{no} 已入座 {t}', '{no} seated at {t}'],
  'qu.seat.order': ['去点菜', 'Take the order'],
  'qu.seat.none': ['没有空桌', 'No free tables'],
  'qu.cancel_q': ['取消 {no}？', 'Cancel {no}?'],
  'qu.slip.at': ['取号 {t}', 'Taken {t}'],
  'qu.slip.scan': ['扫码看排队进度', 'Scan to follow the line'],
  'qu.slip.skip': ['过号请到前台，顺延 {n} 位', 'Missed your call? See the front desk; you go back {n} places'],
  'qu.pk.ready': ['请取餐', 'Ready'],
  'qu.pk.cook': ['制作中', 'Preparing'],
  'qu.pk.wait': ['等待制作', 'In line'],
  'qu.pk.recall': ['重叫', 'Call again'],
  'qu.pk.done': ['已取餐', 'Picked up'],
  'qu.pk.empty': ['没有待取的号', 'Nothing waiting to be picked up'],
  'qu.pk.called': ['已叫 {n} 次', 'Called {n}×'],
  'qu.pk.leaves': ['{m} 分钟后从屏上撤下', 'Leaves the screen in {m} min'],
  'qu.log.none': ['这天没有排队记录', 'No queue activity that day'],
  'qu.log.time': ['时间', 'Time'],
  'qu.log.no': ['号码', 'Number'],
  'qu.log.what': ['操作', 'Action'],
  'qu.log.by': ['操作人', 'By'],
  'qu.log.guest': ['顾客', 'Guest'],
  'qu.log.a.take': ['取号', 'Took a number'],
  'qu.log.a.call': ['叫号', 'Called'],
  'qu.log.a.recall': ['重叫', 'Called again'],
  'qu.log.a.seat': ['入座', 'Seated'],
  'qu.log.a.skip': ['过号', 'Missed'],
  'qu.log.a.requeue': ['复号', 'Back in line'],
  'qu.log.a.jump': ['插队', 'Moved up'],
  'qu.log.a.people': ['改人数', 'Party size changed'],
  'qu.log.a.cancel': ['取消', 'Cancelled'],
  'qu.log.d.table': ['到 {t}', 'to {t}'],
  'qu.log.d.jump': ['第 {a} 位 → 第 {b} 位', 'position {a} → {b}'],
  'qu.log.d.pos': ['排到第 {n} 位', 'to position {n}'],
  'qu.log.d.people': ['{a} 人 → {b} 人', '{a} → {b} guests'],
  'qu.set.modes': ['模式', 'Modes'],
  'qu.set.tea': ['奶茶店取餐屏', 'Pickup screen (tea shop)'],
  'qu.set.tea_d': ['屏上显示等待制作、制作中、请取餐的取餐号', 'Shows pickup numbers: in line, preparing, ready'],
  'qu.set.rest': ['饭店排队叫号', 'Restaurant queue'],
  'qu.set.rest_d': ['扫码或纸质取号，按桌型排队、叫号、入座', 'Numbers by QR or paper, lines by table size, calling and seating'],
  'qu.set.groups': ['桌型分组', 'Table sizes'],
  'qu.set.groups_d': ['按人数分组，每组一个字母。桌台按座位数自动归组，也可以手动指定。', 'Parties are grouped by size, one letter per group. Tables join a group by their seats unless you pick them.'],
  'qu.set.letter': ['字母', 'Letter'],
  'qu.set.name': ['名称', 'Name'],
  'qu.set.range': ['人数', 'Guests'],
  'qu.set.tables': ['桌台', 'Tables'],
  'qu.set.auto': ['按座位自动', 'By seats'],
  'qu.set.dine': ['平均用餐 {n} 分钟', 'Dining {n} min on average'],
  'qu.set.add_group': ['加一组', 'Add a group'],
  'qu.set.save': ['保存', 'Save'],
  'qu.set.saved': ['已保存', 'Saved'],
  'qu.set.rules': ['排队规则', 'Queue rules'],
  'qu.set.upgrade': ['等多久可以升大桌（分钟）', 'Upgrade to a bigger table after (min)'],
  'qu.set.alert': ['叫号多久没到标红（分钟）', 'Mark a call red after (min)'],
  'qu.set.requeue': ['过号复号顺延几位', 'A missed number goes back this many places'],
  'qu.set.dine_min': ['默认用餐时长（分钟，没有历史数据时用）', 'Default dining time (min, until there is history)'],
  'qu.set.sms': ['叫号时给留了手机号的顾客发短信', 'Text guests who left a phone number when called'],
  'qu.set.media': ['叫号屏下方放宣传物料', 'Show promo media on the screens'],
  'qu.set.tea_rules': ['取餐屏', 'Pickup screen'],
  'qu.set.ready_stay': ['请取餐停留多久（分钟）', 'Keep ready numbers for (min)'],
  'qu.set.dine_in': ['显示堂食柜台单', 'Show dine-in counter orders'],
  'qu.set.delivery': ['显示外卖单', 'Show delivery orders'],
  'qu.set.screens': ['叫号屏', 'Screens'],
  'qu.set.screens_d': ['电视或平板的浏览器打开 {url}，输入配对码。一家店可以有多块屏。', 'Open {url} in the TV or tablet browser and enter the pairing code. A store can have several screens.'],
  'qu.set.add_screen': ['添加屏幕', 'Add a screen'],
  'qu.set.screen_name': ['屏幕名称', 'Screen name'],
  'qu.set.mode.tea': ['取餐屏', 'Pickup'],
  'qu.set.mode.restaurant': ['排队叫号', 'Queue'],
  'qu.set.landscape': ['横屏', 'Landscape'],
  'qu.set.portrait': ['竖屏', 'Portrait'],
  'qu.set.lang': ['语言', 'Language'],
  'qu.set.lang2': ['第二语言', 'Second language'],
  'qu.set.lang2_none': ['不显示', 'None'],
  'qu.set.code': ['配对码 {code}，10 分钟内有效', 'Pairing code {code}, valid for 10 minutes'],
  'qu.set.online': ['在线', 'Online'],
  'qu.set.seen': ['最后在线 {t}', 'Last seen {t}'],
  'qu.set.unpaired': ['未绑定', 'Not paired'],
  'qu.set.revoke': ['解除绑定', 'Unpair'],
  'qu.set.revoke_q': ['解除“{name}”的绑定？那块屏会回到配对页。', 'Unpair "{name}"? That screen goes back to the pairing page.'],
  'qu.set.voice': ['试播', 'Test voice'],
  'qu.set.qr': ['扫码取号二维码', 'Take-a-number QR code'],
  'qu.set.qr_d': ['打印出来贴在门口，顾客扫码就能取号', 'Print it for the door; guests scan it to take a number'],
  'qu.sc.pair_title': ['绑定叫号屏', 'Pair this screen'],
  'qu.sc.pair_hint': ['在商家后台“排队叫号 > 设置 > 叫号屏”添加屏幕，把 6 位配对码输在这里。', 'In the back office, add a screen under Queue > Settings > Screens and enter its 6-digit code here.'],
  'qu.sc.pair_go': ['绑定', 'Pair'],
  'qu.sc.wait_d': ['已下单，排队中', 'Order placed, in line'],
  'qu.sc.cook_d': ['正在为您制作', 'Being prepared'],
  'qu.sc.ready_d': ['请到取餐台出示取餐号', 'Show your number at the counter'],
  'qu.sc.more': ['还有 {n} 单', '{n} more'],
  'qu.sc.say_ready': ['请 {no} 号取餐', 'Number {no}, your order is ready'],
  'qu.sc.tea_sub': ['请留意您的取餐号', 'Watch for your pickup number'],
  'qu.sc.rest_sub': ['请留意叫号和手机上的提醒', 'Watch for your number here and on your phone'],
  'qu.sc.calling': ['正在叫号', 'Now calling'],
  'qu.sc.to_table': ['请到 {t} 号桌', 'Please go to table {t}'],
  'qu.sc.to_place': ['请到 {t}', 'Please go to {t}'],
  'qu.sc.to_desk': ['请到前台', 'Please come to the front desk'],
  'qu.sc.nth': ['第 {n} 次叫号', 'Call {n}'],
  'qu.sc.recent': ['刚刚叫过', 'Just called'],
  'qu.sc.group': ['桌型', 'Table'],
  'qu.sc.current': ['当前叫到', 'Now serving'],
  'qu.sc.waiting': ['等待', 'Waiting'],
  'qu.sc.est': ['预计等待', 'Est. wait'],
  'qu.sc.waitlist': ['等待中', 'Up next'],
  'qu.sc.missed_d': ['过号请到前台找店员复号（顺延 {n} 位）', 'Missed? See the front desk to rejoin ({n} places back)'],
  'qu.sc.scan': ['手机扫码取号', 'Scan to take a number'],
  'qu.sc.scan_d': ['不用在门口等，叫到号会提醒你', 'No need to wait at the door; we will alert you'],
  'qu.sc.paused': ['暂停取号，请到前台咨询', 'Not taking numbers now; please ask at the front desk'],
  'qu.sc.say_table': ['请 {no} 号顾客到 {t} 号桌', 'Number {no}, please go to table {t}'],
  'qu.sc.say_place': ['请 {no} 号顾客到 {t}', 'Number {no}, please go to {t}'],
  'qu.sc.say_desk': ['请 {no} 号顾客到前台', 'Number {no}, please come to the front desk'],
  'qu.sc.sound_on': ['声音已打开', 'Sound on'],
  'qu.sc.sound_off': ['点一下屏幕打开声音', 'Tap the screen to turn on sound'],
  'qu.sc.offline': ['连接中断，正在重连', 'Connection lost, reconnecting'],
  'qu.sc.tip': ['取餐提示', 'Pickup tips'],
  'qu.sc.tip_d': ['号码做好会放大并播报；{n} 分钟没取会撤下，请到柜台询问', 'A ready number grows and is announced; after {n} min it leaves the screen, so ask at the counter'],
  'qu.sc.none': ['暂无', 'None'],
  'qu.sc.off': ['这块屏的模式没有开启，请在后台设置里打开', 'This screen’s mode is turned off in the back office settings'],
  'qu.g.title': ['排队取号', 'Take a number'],
  'qu.g.open': ['营业中，可以取号', 'Open, taking numbers'],
  'qu.g.closed': ['现在不能取号，请到前台咨询', 'Not taking numbers now; please ask at the front desk'],
  'qu.g.lines': ['现在排队', 'The line now'],
  'qu.g.waiting_n': ['等 {n} 桌', '{n} waiting'],
  'qu.g.line_none': ['不用等', 'No wait'],
  'qu.g.people': ['几位用餐？', 'How many guests?'],
  'qu.g.phone_hint': ['选填，叫到号时短信提醒', 'Optional, we text you when called'],
  'qu.g.agree': ['我已了解：过号顺延 {n} 位', 'I understand a missed call puts me {n} places back'],
  'qu.g.take': ['立即取号', 'Take a number'],
  'qu.g.ahead': ['前面还有', 'Ahead of you'],
  'qu.g.est': ['预计等待', 'Est. wait'],
  'qu.g.current': ['当前叫到', 'Now serving'],
  'qu.g.menu': ['先看看菜单', 'See the menu'],
  'qu.g.cancel': ['取消排队', 'Leave the line'],
  'qu.g.cancel_q': ['确定取消排队？号码不能恢复。', 'Leave the line? Your number cannot be restored.'],
  'qu.g.notify': ['打开提醒', 'Turn on alerts'],
  'qu.g.notify_on': ['提醒已打开', 'Alerts on'],
  'qu.g.called': ['到你了！', 'It’s your turn!'],
  'qu.g.come': ['请在 {n} 分钟内到店', 'Please come within {n} min'],
  'qu.g.skipped': ['已过号', 'Missed'],
  'qu.g.skipped_d': ['请到前台找店员复号', 'Please see the front desk to rejoin the line'],
  'qu.g.seated': ['已入座，用餐愉快', 'Seated. Enjoy your meal'],
  'qu.g.cancelled': ['已取消排队', 'You left the line'],
  'qu.g.void': ['这个号已经失效', 'This number is no longer valid'],
  'qu.g.again': ['重新取号', 'Take a new number'],
  'qu.g.notfound': ['找不到这个排队号', 'This queue number was not found'],
  'qu.g.keep': ['页面开着会自动刷新，叫到号时手机会震动', 'Keep this page open; it updates by itself and vibrates when called'],
  'qu.g.notify_title': ['{store}：到你了', '{store}: it’s your turn'],
  'qu.g.unit_tables': ['桌', 'parties'],
  'qu.g.unit_min': ['分钟', 'min'],
  'qu.err.QUEUE_CLOSED': ['现在不能取号', 'Not taking numbers now'],
  'qu.err.QUEUE_NO_GROUP': ['没有适合这个人数的桌型，请找店员', 'No table fits this party size; please ask the staff'],
  'qu.err.QUEUE_HAS_TICKET': ['你已经有一个排队号了', 'You already have a number'],
  'qu.err.TABLE_HELD': ['这桌留给 {number}', 'This table is held for {number}'],
  'qu.err.PAIR_CODE_INVALID': ['配对码不对或已过期', 'The code is wrong or has expired'],
  'qu.err.INVALID_STATE': ['状态已经变了，已刷新', 'It changed meanwhile; refreshed'],
})

// qErr is an error for people: the codes this area knows, else the server's words.
export function qErr(e: unknown): string {
  if (e instanceof ApiError) {
    const k = 'qu.err.' + e.code
    if (has(k)) return t(k, { number: e.details?.number ?? '' })
    return e.message
  }
  return (e as Error)?.message ?? String(e)
}

type Txt = (key: string, vars?: Record<string, string | number>) => string

// groupName is a group's name in a language: the store's own, else the preset's.
export function groupName(g: { preset: string; label: string; label_en: string; letter: string }, tx: Txt = t, lang?: string): string {
  const zh = lang ? lang === 'zh' || lang === 'zh-Hant' : isZh()
  const own = zh ? (g.label || g.label_en) : (g.label_en || g.label)
  if (own) return own
  return has('qu.preset.' + g.preset) ? tx('qu.preset.' + g.preset) : g.letter
}
export function groupRange(g: { min_people: number; max_people: number }, tx: Txt = t): string {
  if (g.max_people >= 99) return tx('qu.range_up', { a: g.min_people })
  if (g.min_people === g.max_people) return tx('qu.people', { n: g.min_people })
  return tx('qu.range', { a: g.min_people, b: g.max_people })
}
// estText: "about 20 min", or "soon" when a table is about to be free.
export const estText = (m: number, tx: Txt = t) => m > 0 ? tx('qu.about', { n: m }) : tx('qu.soon_v')
export const tagText = (s: string) => has('qu.tag.' + s) ? t('qu.tag.' + s) : s
export const TAGS = ['baby', 'wheelchair', 'member']
export const REASONS = ['member', 'booking', 'elder', 'staff']

// Where a called guest should go: a table ("05" reads "table 05") or the desk.
export function placeText(table: string, tx: Txt = t): string {
  if (!table) return tx('qu.sc.to_desk')
  return tx(/^[A-Za-z]?\d+$/.test(table) ? 'qu.sc.to_table' : 'qu.sc.to_place', { t: table })
}
export function sayCall(no: string, table: string, tx: Txt = t): string {
  if (!table) return tx('qu.sc.say_desk', { no })
  return tx(/^[A-Za-z]?\d+$/.test(table) ? 'qu.sc.say_table' : 'qu.sc.say_place', { no, t: table })
}

export const minsSince = (iso: string | null | undefined, now = Date.now()) => iso ? Math.max(0, Math.floor((now - new Date(iso).getTime()) / 60000)) : 0

// A chime: two soft notes. Browsers allow sound only after a tap on the page.
let ctx: AudioContext | null = null
export function unlockSound(): boolean {
  try {
    ctx ??= new AudioContext()
    if (ctx.state === 'suspended') void ctx.resume()
    return ctx.state !== 'suspended'
  } catch { return false }
}
export const soundReady = () => !!ctx && ctx.state === 'running'
export function chime() {
  if (!ctx) return
  const now = ctx.currentTime
  for (const [f, at] of [[880, 0], [660, 0.28]] as const) {
    const o = ctx.createOscillator(), g = ctx.createGain()
    o.frequency.value = f
    o.type = 'sine'
    g.gain.setValueAtTime(0.0001, now + at)
    g.gain.exponentialRampToValueAtTime(0.35, now + at + 0.03)
    g.gain.exponentialRampToValueAtTime(0.0001, now + at + 0.6)
    o.connect(g).connect(ctx.destination)
    o.start(now + at)
    o.stop(now + at + 0.65)
  }
}

// say reads a line aloud in the screen's language, when the device has such
// a voice (some TV boxes have none: then only the chime sounds).
export function say(text: string, lang: Locale) {
  try {
    const s = window.speechSynthesis
    if (!s) return
    const tag = langInfo(lang).tag
    const base = tag.split('-')[0]
    const voices = s.getVoices()
    const v = voices.find(x => x.lang.toLowerCase() === tag.toLowerCase()) ?? voices.find(x => x.lang.toLowerCase().startsWith(base))
    if (voices.length && !v) return
    const u = new SpeechSynthesisUtterance(text)
    u.lang = tag
    if (v) u.voice = v
    u.rate = 0.9
    s.speak(u)
  } catch { /* no voice */ }
}

// announce: chime, then the words twice, like a restaurant's caller.
export function announce(text: string, lang: Locale) {
  chime()
  window.setTimeout(() => { say(text, lang); say(text, lang) }, 900)
}

export { tIn }
