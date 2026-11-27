// Chat between a customer and a store. The messages go through an OpenIM
// server (docs/CHAT_DESIGN.md): this module signs the browser in to it, and
// is the only place that touches its SDK. The lists and the unread counts
// come from this system's own API.
import { reactive } from 'vue'
import { toUnicode } from './zawgyi'
import { ApiError, del, get, post, put, upload } from './api'
import { shrink } from './photo'
import { extend, t } from './i18n'
import type { BrandRef } from './types'

export interface ChatLogin { user_id: string; token: string; ws_url: string; api_url: string; platform_id: number }
export interface OrderTag { id: number; public_no: string; status: string }
export interface ChatThread {
  id: number; branch_id: number; slug?: string; store_name?: string; customer_id?: number; customer?: string; phone_tail?: string
  peer: string; last_kind: string; last_text: string; last_sender: 'customer' | 'store' | 'system'; last_at: string
  unread: number; count: number; peer_read_at: string | null; order: OrderTag | null
  brand_id?: number; brand_name?: string; brand_color?: string
}
export interface ChatOrder { id: number; public_no: string; status: string; service_type: string; total: number; currency: string; created_at: string; open: boolean; token?: string; store_name?: string
  brand_id?: number; brand_name?: string; brand_color?: string }
export interface CustomerChat {
  branch_id: number; slug: string; store_name: string; store_name_en: string; phone: string; open_now: boolean; open_time: string
  peer: string; enabled: boolean; muted: boolean; thread_id: number; peer_read_at: string | null; orders: ChatOrder[]; quiet: boolean
  brand_id?: number
}
export interface StoreChat extends ChatThread { phone_masked: string; orders: ChatOrder[]; order_count: number; blocked: boolean }

// ChatEx travels with a message: the store's side says which member of staff
// is writing; the customer's side which brand's page they wrote from; a store
// writing to platform support which of its brands it writes about.
export interface ChatEx { uid?: number; name?: string; brand?: number }

// One message as the pages show it.
export interface ChatMsg {
  id: string; mine: boolean; kind: 'text' | 'image' | 'order' | 'other'; text: string; at: number
  state: 'sent' | 'sending' | 'failed'; staff?: string; error?: string
  image?: string   // the picture's address
  orderId?: number
  auto?: boolean   // the answer a closed store gives by itself
  brand?: number   // the brand a customer wrote to (docs/MULTI_BRAND_DESIGN.md §9)
  retry?: () => Promise<ChatMsg>   // how a picture or a card that failed is sent again
}
export interface QuickReply { id: number; body: string }
export interface ChatSetup { quick_replies: QuickReply[]; auto_reply_on: boolean; auto_reply: string }
export const chatImageUrl = (token: string) => `/api/v1/chat-images/${token}`

// ---- this system's API ----
export const customerChats = () => get<{ chats: ChatThread[]; support: ChatThread | null; enabled: boolean }>('/customer/chats')
export const customerChat = (slug: string) => get<CustomerChat>(`/customer/chats/${slug}`)
export const customerChatRead = (slug: string) => post(`/customer/chats/${slug}/read`)
export const customerUnread = () => get<{ chats: number; support: number; notices: number; total: number }>('/customer/unread')
export const storeChats = (bid: number, q: { q?: string; unread?: boolean; active?: boolean; brand?: number } = {}) =>
  get<{ chats: ChatThread[]; unread: number; support_unread: number; enabled: boolean }>(`/merchant/branches/${bid}/chats?q=${encodeURIComponent(q.q ?? '')}&unread=${q.unread ? 1 : ''}&active=${q.active ? 1 : ''}${q.brand ? '&brand=' + q.brand : ''}`)
export const storeChat = (bid: number, customerId: number) => get<StoreChat>(`/merchant/branches/${bid}/chats/${customerId}`)
export const chatSetup = (bid: number) => get<ChatSetup>(`/merchant/branches/${bid}/chat-setup`)
export const addQuickReply = (bid: number, body: string) => post<QuickReply>(`/merchant/branches/${bid}/chat-quick-replies`, { body })
export const deleteQuickReply = (bid: number, id: number) => del(`/merchant/branches/${bid}/chat-quick-replies/${id}`)
export const setAutoReply = (bid: number, on: boolean, text: string) => put(`/merchant/branches/${bid}/chat-auto-reply`, { on, text })
export const chatCustomerOfOrder = (bid: number, orderId: number) => get<{ customer_id: number }>(`/merchant/branches/${bid}/chat-order/${orderId}`)
export const customerChatQuiet = (slug: string, quiet: boolean) => post(`/customer/chats/${slug}/quiet`, { quiet })
export const customerChatHide = (slug: string) => post(`/customer/chats/${slug}/hide`)
export const storeChatBlock = (bid: number, customerId: number, blocked: boolean) => post(`/merchant/branches/${bid}/chats/${customerId}/block`, { blocked })
export const storeChatRead = (bid: number, customerId: number) => post(`/merchant/branches/${bid}/chats/${customerId}/read`)

// ---- platform support (the platform's shared inbox, p_1) ----
export interface SupportChat { enabled: boolean; peer: string; thread_id: number; unread: number; peer_read_at: string | null; orders: ChatOrder[] }
export interface SupportRow {
  id: number; party: string; kind: 'customer' | 'store'; customer_id?: number; branch_id?: number; name: string; phone: string; phone_tail: string; merchant?: string
  last_kind: string; last_text: string; last_sender: 'customer' | 'store' | 'platform'; last_at: string; unread: number; count: number
  peer_read_at: string | null; resolved_at: string | null; resolved_by?: string
  brand_id?: number; brand_name?: string; brand_color?: string; store_name?: string // the brand the latest message was about
}
export interface SupportDetail extends SupportRow { started_at: string | null; orders: ChatOrder[]; slug?: string; merchant_id?: number; muted: boolean; since: string; chat_enabled: boolean
  brands: BrandRef[] }
export const customerSupport = () => get<SupportChat>('/customer/support')
export const customerSupportRead = () => post('/customer/support/read')
export const storeSupport = (bid: number) => get<SupportChat>(`/merchant/branches/${bid}/support`)
export const storeSupportRead = (bid: number) => post(`/merchant/branches/${bid}/support/read`)
export const adminSupport = (q: { q?: string; who?: string; unread?: boolean; open?: boolean; page?: number; page_size?: number }) =>
  get<{ list: { chats: SupportRow[]; total: number; unread: number; open: number }; enabled: boolean }>(`/admin/support?q=${encodeURIComponent(q.q ?? '')}&who=${q.who ?? ''}&unread=${q.unread ? 1 : ''}&open=${q.open ? 1 : ''}&page=${q.page ?? 1}&page_size=${q.page_size ?? 30}`)
export const adminSupportUnread = () => get<{ unread: number }>('/admin/support/unread')
export const adminSupportChat = (party: string) => get<SupportDetail>(`/admin/support/${party}`)
export const adminSupportRead = (party: string) => post(`/admin/support/${party}/read`)
export const adminSupportResolve = (party: string, resolved: boolean) => post(`/admin/support/${party}/resolve`, { resolved })

// chatOn says whether this installation has chat at all (the entry points hide without it).
let onPromise: Promise<boolean> | null = null
export const chatEnabled = () => onPromise ??= get<{ chat?: boolean }>('/config').then(c => !!c.chat, () => false)

// ---- the connection ----
// state is what the pages show about the line to the chat server.
export const chat = reactive<{ state: 'off' | 'connecting' | 'on' | 'down' | 'kicked'; user: string }>({ state: 'off', user: '' })

type Sdk = any
let sdk: Sdk = null
let wanted = '' // the login url the current connection was made with
let pending: Promise<Sdk> | null = null

type Incoming = (peer: string, m: ChatMsg) => void
const onMessage = new Set<Incoming>()
const onRead = new Set<(peer: string) => void>()

function absolute(u: string, ws: boolean): string {
  if (!u.startsWith('/')) return u
  const scheme = ws ? (location.protocol === 'https:' ? 'wss:' : 'ws:') : location.protocol
  return `${scheme}//${location.host}${u}`
}

// toMsg turns the SDK's message into ours. What is not a text shows as a placeholder.
function toMsg(m: any, self: string): ChatMsg {
  let staff = '', auto = false, brand = 0
  try { const ex = JSON.parse(m.ex || '{}'); staff = ex.name ?? ''; auto = !!ex.auto; brand = Number(ex.brand) || 0 } catch { /* no extra */ }
  const out: ChatMsg = {
    id: m.clientMsgID, mine: m.sendID === self, kind: 'other', text: '',
    at: m.sendTime || m.createTime || Date.now(), state: m.status === 3 ? 'failed' : 'sent', staff, auto, brand: brand || undefined,
  }
  if (m.contentType === 101) { out.kind = 'text'; out.text = m.textElem?.content ?? '' }
  else if (m.contentType === 110) {
    // this system's own cards: a picture uploaded here, or an order
    try {
      const card = JSON.parse(m.customElem?.data || '{}')
      if (card.t === 'image' && /^[0-9a-f]+$/.test(card.token ?? '')) { out.kind = 'image'; out.image = chatImageUrl(card.token) }
      else if (card.t === 'order' && Number(card.order) > 0) { out.kind = 'order'; out.orderId = Number(card.order) }
    } catch { /* shown as "cannot be shown" */ }
  }
  return out
}

async function open(loginPath: string): Promise<Sdk> {
  chat.state = 'connecting'
  const login = await post<ChatLogin>(loginPath)
  const mod = await import('@openim/client-sdk')
  const s: Sdk = mod.getSDK()
  if (sdk && chat.user && chat.user !== login.user_id) await s.logout().catch(() => null)
  if (!sdk) {
    const E = mod.CbEvents
    s.on(E.OnRecvNewMessages, ({ data }: any) => {
      for (const m of data ?? []) {
        if (m.sessionType !== 1) continue
        const peer = m.sendID === chat.user ? m.recvID : m.sendID
        const msg = toMsg(m, chat.user)
        onMessage.forEach(f => f(peer, msg))
      }
    })
    s.on(E.OnRecvC2CReadReceipt, ({ data }: any) => { for (const r of data ?? []) onRead.forEach(f => f(r.userID)) })
    s.on(E.OnConnectSuccess, () => { chat.state = 'on' })
    s.on(E.OnConnecting, () => { if (chat.state !== 'kicked') chat.state = 'connecting' })
    s.on(E.OnConnectFailed, () => { chat.state = 'down' })
    s.on(E.OnKickedOffline, () => { chat.state = 'kicked' })
    const expired = () => { pending = null; wanted = ''; chat.state = 'down' }
    s.on(E.OnUserTokenExpired, expired)
    s.on(E.OnUserTokenInvalid, expired)
  }
  sdk = s
  chat.user = login.user_id
  try {
    await s.login({ userID: login.user_id, token: login.token, platformID: login.platform_id, logLevel: 4,
      wsAddr: absolute(login.ws_url, true), apiAddr: absolute(login.api_url, false) })
  } catch (e) { chat.state = 'down'; throw chatError(e) }
  chat.state = 'on'
  return s
}

// Side is whose account a page signs in to the chat server as: the customer
// (undefined), a store (its branch id) or the platform's support inbox.
export type Side = number | 'platform' | undefined

// connect signs in (once) as the given side.
export function connect(side?: Side): Promise<Sdk> {
  const path = side === 'platform' ? '/admin/support/login' : side ? `/merchant/branches/${side}/chats/login` : '/customer/chats/login'
  if (pending && wanted === path) return pending
  wanted = path
  pending = open(path).catch(e => { pending = null; wanted = ''; throw e })
  return pending
}

export async function disconnect() {
  pending = null; wanted = ''
  if (sdk) await sdk.logout().catch(() => null)
  chat.state = 'off'; chat.user = ''
}

export function listen(message: Incoming, read: (peer: string) => void = () => {}): () => void {
  onMessage.add(message); onRead.add(read)
  return () => { onMessage.delete(message); onRead.delete(read) }
}

// chatError turns what the SDK throws into an ApiError. When this system's
// rules refused a message, errMsg carries its code (CHAT_MUTED and the like).
export function chatError(e: any): ApiError {
  if (e instanceof ApiError) return e
  const code = typeof e?.errMsg === 'string' && /^[A-Z_]+$/.test(e.errMsg) ? e.errMsg : 'CHAT_DOWN'
  return new ApiError(0, code, describeChat(code))
}
export function describeChat(code: string): string {
  const key = 'ch.err_' + code
  const s = t(key)
  return s === key ? t('ch.err_CHAT_DOWN') : s
}

async function conversationId(s: Sdk, peer: string): Promise<string> {
  return (await s.getOneConversation({ sourceID: peer, sessionType: 1 })).data.conversationID
}

// history returns one page of a conversation, oldest first; `before` is the
// id of the oldest message already shown.
export async function history(branchId: Side, peer: string, before = ''): Promise<{ list: ChatMsg[]; end: boolean }> {
  const s = await connect(branchId)
  try {
    const r = (await s.getAdvancedHistoryMessageList({ conversationID: await conversationId(s, peer), startClientMsgID: before, count: 30, viewType: 0 })).data
    const list = (r.messageList ?? []).filter((m: any) => m.contentType < 1000).map((m: any) => toMsg(m, chat.user))
    return { list, end: !!r.isEnd || !(r.messageList ?? []).length }
  } catch (e) { throw chatError(e) }
}

// sendText sends one text. extra travels with the message: the store's side
// says which member of staff is writing.
export async function sendText(branchId: Side, peer: string, text: string, extra?: ChatEx): Promise<ChatMsg> {
  const s = await connect(branchId)
  text = (await toUnicode(text).catch(() => ({ body: text }))).body // Zawgyi Burmese goes out as Unicode
  try {
    const m = (await s.createTextMessage(text)).data
    if (extra) m.ex = JSON.stringify(extra)
    const sent = (await s.sendMessage({ recvID: peer, groupID: '', message: m })).data
    return toMsg({ ...m, ...sent, ex: m.ex }, chat.user)
  } catch (e) { throw chatError(e) }
}

async function sendCard(branchId: Side, peer: string, card: object, extra?: ChatEx): Promise<ChatMsg> {
  const s = await connect(branchId)
  try {
    const m = (await s.createCustomMessage({ data: JSON.stringify(card), extension: '', description: '' })).data
    if (extra) m.ex = JSON.stringify(extra)
    const sent = (await s.sendMessage({ recvID: peer, groupID: '', message: m })).data
    return toMsg({ ...m, ...sent, ex: m.ex }, chat.user)
  } catch (e) { throw chatError(e) }
}

// sendImage shrinks a picture, uploads it to this system (uploadPath is the
// conversation's own address for that) and sends its token as a card.
export async function sendImage(branchId: Side, peer: string, uploadPath: string, file: File, extra?: ChatEx): Promise<ChatMsg> {
  let token: string
  try { token = (await upload<{ token: string }>(uploadPath, await shrink(file))).token } catch (e) {
    if (e instanceof ApiError) throw new ApiError(e.status, e.code, describeChat(e.code === 'FILE_TOO_LARGE' || e.code === 'VALIDATION' ? 'BAD_IMAGE' : e.code))
    throw new ApiError(0, 'BAD_IMAGE', describeChat('BAD_IMAGE'))
  }
  return sendCard(branchId, peer, { t: 'image', token }, extra)
}

export const sendOrder = (branchId: Side, peer: string, orderId: number, extra?: ChatEx) =>
  sendCard(branchId, peer, { t: 'order', order: orderId }, extra)

// ---- words ----
export function clock(at: number | string): string {
  const d = new Date(at), now = new Date()
  const hm = `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
  if (d.toDateString() === now.toDateString()) return hm
  const md = `${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
  return d.getFullYear() === now.getFullYear() ? `${md} ${hm}` : `${d.getFullYear()}-${md} ${hm}`
}
export function day(at: number | string): string {
  const d = new Date(at), now = new Date()
  if (d.toDateString() === now.toDateString()) return clock(at)
  const md = `${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
  return d.getFullYear() === now.getFullYear() ? md : `${d.getFullYear()}-${md}`
}
export function lastLine(c: ChatThread, self: 'customer' | 'store'): string {
  const body = c.last_kind === 'image' ? t('ch.kind_image') : c.last_kind === 'order' ? t('ch.kind_order') : c.last_text
  return c.last_sender === self ? t('ch.you') + body : body
}

extend({
  'ch.title': ['消息', 'Messages'],
  'sp.title': ['平台客服', 'Platform support'], 'sp.online': ['在线客服', 'Chat with support'], 'sp.ava': ['客', 'S'],
  'sp.start': ['有什么问题，直接发消息给平台客服。', 'Write to platform support about anything.'],
  'sp.tip': ['平台客服会尽快回复。订单的问题也可以先联系商家。', 'Platform support will answer soon. For an order, you can also ask the store first.'],
  'sp.hours': ['服务时间：{h}', 'Hours: {h}'], 'sp.call': ['打电话给客服', 'Call support'],
  'sp.store_tip': ['这里是平台客服，帮你处理账号、结算、上架、投诉等问题。门店的店长和前台共用这个会话。', 'Platform support helps with your account, payouts, listings and complaints. Managers and the desk share this conversation.'],
  'sp.store_start': ['还没有消息。有问题可以直接问平台。', 'No messages yet. Ask the platform anything.'],
  'sp.you_store': ['平台客服：', 'Support: '], 'ch.tab_chats': ['聊天', 'Chats'], 'ch.tab_notices': ['通知', 'Notices'],
  'ch.none': ['还没有聊天。在店铺页或订单页点“联系商家”就可以开始。', 'No chats yet. Tap "Contact store" on a store or an order to start.'],
  'ch.keep': ['聊天记录保留 180 天', 'Chats are kept for 180 days'],
  'ch.off': ['聊天功能暂时没有开通', 'Chat is not available yet'],
  'ch.contact_store': ['联系商家', 'Contact store'], 'ch.contact_customer': ['联系顾客', 'Message customer'],
  'ch.input': ['输入消息…', 'Type a message…'], 'ch.input_desk': ['输入消息，回车发送…', 'Type a message, Enter to send…'],
  'ch.send': ['发送', 'Send'], 'ch.read': ['已读', 'Read'], 'ch.sending': ['发送中…', 'Sending…'], 'ch.failed': ['没发出去，点这里重发', 'Not sent. Tap to retry'],
  'ch.earlier': ['查看更早的消息', 'Earlier messages'], 'ch.start': ['有什么想问商家的，直接发消息。', 'Ask the store anything.'],
  'ch.start_desk': ['还没有消息。可以先给这位顾客发一条。', 'No messages yet. You can write first.'],
  'ch.you': ['你：', 'You: '], 'ch.kind_image': ['[图片]', '[Picture]'], 'ch.kind_order': ['[订单]', '[Order]'], 'ch.other': ['[这条消息在这里看不了]', '[This message cannot be shown here]'],
  'ch.open': ['营业中', 'Open'], 'ch.closed': ['已打烊', 'Closed'], 'ch.closed_note': ['商家现在不在营业时间，可能要晚一些才回复。急事请打电话。', 'The store is closed now and may answer later. Call for anything urgent.'],
  'ch.call': ['打电话给商家', 'Call the store'], 'ch.privacy': ['为了处理纠纷和保障安全，平台可以查看聊天内容。', 'To settle disputes and keep people safe, the platform can read chats.'],
  'ch.connecting': ['正在连接…', 'Connecting…'], 'ch.down': ['聊天连接断开了，正在重试', 'Chat is disconnected, retrying'], 'ch.retry': ['重新连接', 'Reconnect'],
  'ch.kicked': ['这个账号在别处登录，这里已下线', 'Signed in elsewhere; this page is offline'],
  'ch.err_CHAT_DOWN': ['聊天暂时不可用，请稍后再试，急事请打电话', 'Chat is unavailable right now. Try again later, or call.'],
  'ch.err_CHAT_OFF': ['聊天功能暂时没有开通', 'Chat is not available yet'],
  'ch.err_CHAT_MUTED': ['你暂时不能发消息，如有疑问请联系平台客服', 'You cannot send messages for now. Contact support if this is a mistake.'],
  'ch.err_CHAT_CLOSED': ['暂时无法联系这家店，请打电话', 'This store cannot be messaged right now. Please call.'],
  'ch.err_CHAT_BLOCKED': ['暂时无法联系这家店，请打电话', 'This store cannot be messaged right now. Please call.'],
  'ch.err_CHAT_NO_ORDER': ['只能主动联系 7 天内在本店下过单的顾客', 'You can only write first to customers who ordered in the last 7 days'],
  'ch.err_CHAT_TOO_FAST': ['发得太快了，请稍等一下', 'Too many messages. Wait a moment.'],
  'ch.err_CHAT_TOO_MANY': ['今天新开的聊天太多了，明天再试', 'Too many new chats today. Try again tomorrow.'],
  'ch.err_VALIDATION': ['这条消息发不了（太长或格式不支持）', 'This message cannot be sent (too long or not supported)'],
  'ch.err_FORBIDDEN': ['你没有回复顾客的权限', 'You are not allowed to answer customers'],
  'ch.err_NOT_FOUND': ['找不到对方', 'The other side was not found'],
  'ch.err_BAD_IMAGE': ['这张图片发不了，请换一张（支持常见照片格式）', 'This picture cannot be sent. Try another one.'],
  'ch.err_CHAT_NO_ACCOUNT': ['这位顾客下单时没有登录账号，不能发消息，请打电话', 'This customer ordered without an account. Please call instead.'],
  'ch.pic': ['发图片', 'Send a picture'], 'ch.send_order': ['发订单', 'Send an order'], 'ch.pick_order': ['选择要发送的订单', 'Choose an order to send'],
  'ch.no_orders_here': ['在这家店还没有订单', 'No orders with this store yet'], 'ch.order_card': ['订单 #{no}', 'Order #{no}'], 'ch.order_gone': ['订单', 'Order'],
  'ch.view_order': ['查看订单 ›', 'View order ›'], 'ch.auto': ['自动回复', 'Automatic reply'],
  'ch.quick': ['快捷回复', 'Quick replies'], 'ch.quick_manage': ['设置', 'Manage'], 'ch.quick_add': ['添加一条快捷回复', 'Add a quick reply'], 'ch.quick_none': ['还没有快捷回复。把常说的话加进来，回复时点一下就能填好。', 'No quick replies yet. Add what you say often and fill it in with one tap.'],
  'ch.quick_defaults': ['加入几条常用的', 'Add some common ones'], 'ch.auto_title': ['打烊自动回复', 'Automatic reply while closed'],
  'ch.auto_hint': ['不在营业时间收到顾客消息时，自动回这一句（同一位顾客 6 小时内只回一次）。', 'Sent when a customer writes outside opening hours (once in 6 hours per customer).'],
  'ch.auto_placeholder': ['留空就用默认的：现在不在营业时间，营业后会尽快回复您。', 'Leave empty for the standard sentence.'], 'ch.auto_manager': ['只有店长能改', 'Only a manager can change this'],
  'ch.new_msg': ['有新的顾客消息', 'New message from a customer'], 'ch.new_msg_go': ['去查看', 'Open'],
  'ch.add': ['添加', 'Add'],
  'ch.q1': ['您好，已经收到您的消息，马上为您处理。', 'Hello, we got your message and are on it.'], 'ch.q2': ['您的餐正在制作中，请稍等。', 'Your order is being prepared.'],
  'ch.q3': ['骑手已经出发，很快送到。', 'The rider is on the way.'], 'ch.q4': ['不好意思让您久等了。', 'Sorry to keep you waiting.'],
  'ch.more': ['更多', 'More'], 'ch.push_ask': ['开启提醒，商家回复时通知你', 'Turn on reminders to hear when the store answers'], 'ch.push_on': ['开启', 'Turn on'],
  'ch.push_denied': ['浏览器不允许这个网站发通知，要在浏览器设置里打开', 'This browser blocks notifications from this site. Allow them in its settings.'],
  'ch.push_ask_desk': ['开启新消息提醒（页面关了也能收到）', 'Turn on reminders for new messages (even with the page closed)'], 'ch.push_is_on': ['新消息提醒已开启', 'Reminders are on'], 'ch.push_off': ['关闭', 'Turn off'],
  'ch.quiet': ['不再提醒这家店的消息', 'No reminders about this store'], 'ch.unquiet': ['恢复这家店的消息提醒', 'Remind me about this store again'], 'ch.quiet_on': ['已关闭这家店的消息提醒', 'Reminders about this store are off'],
  'ch.report_store': ['举报这个商家', 'Report this store'], 'ch.report_customer': ['举报这位顾客', 'Report this customer'], 'ch.reported': ['举报已提交，平台会查看这个聊天', 'Report sent. The platform will look at this chat.'],
  'ch.hide': ['删除这个聊天', 'Remove this chat'], 'ch.hide_confirm': ['只在你这边不再显示，商家那边还在。对方再发消息时会重新出现。确定吗？', 'It disappears on your side only, and comes back when the store writes again. Continue?'],
  'ch.block': ['拉黑这位顾客', 'Block this customer'], 'ch.unblock': ['取消拉黑', 'Unblock'], 'ch.block_confirm': ['拉黑后这位顾客不能再给本店发消息，下单不受影响。确定吗？', 'This customer will not be able to message the store; ordering is not affected. Continue?'],
  'ch.blocked_note': ['已拉黑：这位顾客不能给本店发消息。', 'Blocked: this customer cannot message the store.'],
  'ch.rp_harassment': ['骚扰', 'Harassment'], 'ch.rp_spam': ['广告或刷屏', 'Advertising or spam'], 'ch.rp_fraud': ['欺诈', 'Fraud'], 'ch.rp_abuse': ['辱骂或不当内容', 'Abuse or improper content'], 'ch.rp_other': ['其他', 'Other'],
  'ch.rp_detail': ['补充说明（选“其他”时必填）', 'Details (needed for "Other")'], 'ch.rp_note': ['提交后平台会查看这个聊天的内容。', 'The platform will read this chat after you send the report.'], 'ch.rp_send': ['提交举报', 'Send report'],
  'ch.img_gone': ['图片已过期', 'Picture expired'],
  // the store's side
  'ch.nav': ['消息', 'Messages'], 'ch.search': ['搜索顾客、手机尾号、订单号', 'Search name, phone ending, order no.'],
  'ch.f_all': ['全部', 'All'], 'ch.f_unread': ['未读', 'Unread'], 'ch.f_active': ['进行中订单', 'Open orders'],
  'ch.pick': ['点左边的聊天查看内容', 'Choose a chat on the left'], 'ch.none_desk': ['还没有顾客发来消息', 'No messages from customers yet'],
  'ch.guest': ['顾客', 'Customer'], 'ch.tail': ['尾号 {n}', 'ending {n}'], 'ch.orders_here': ['在本店下过 {n} 单', '{n} orders here'],
  'ch.customer': ['顾客', 'Customer'], 'ch.current_order': ['当前订单', 'Current order'], 'ch.recent_orders': ['最近订单', 'Recent orders'],
  'ch.open_order': ['打开订单 ›', 'Open order ›'], 'ch.no_orders': ['还没有订单', 'No orders yet'], 'ch.shared': ['全店共用一个收件箱，顾客只看到店名。', 'The whole store shares this inbox; customers see the store name only.'],
})
