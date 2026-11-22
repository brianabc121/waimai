// The customer's own pages ("me"): what they ask the server and the words they use.
import { reactive } from 'vue'
import { del, get, post, put, upload } from '../../api'
import { customer, loadCustomer } from '../../customer'
import { extend, locale, t } from '../../i18n'
import type { Promotion } from '../../types'

export interface Wallet { currency: string; balance: number; bonus: number }
export interface Profile {
  id: number; member_no: string; phone: string; nickname: string; avatar_at: string | null
  wallets: Wallet[]; unread: number; favorites: number
}
export interface StoreRef { slug: string; name: string; name_en: string; logo_url: string }
export interface MyStore extends StoreRef { phone: string; address: string; currency: string; favorite: boolean; last_order_at: string | null }
export interface StoreOffers { store: StoreRef; currency: string; promotions: Promotion[] }
export interface MyReview {
  id: number; rating: number; taste: number; packaging: number; delivery: number | null; comment: string
  created_at: string; display_name: string; items: { name: string; name_en: string }[]; store: StoreRef; token: string
}
export interface Message { id: number; kind: 'order' | 'service' | 'wallet'; title: string; title_en: string; body: string; body_en: string; link: string; created_at: string; read: boolean }
export interface Feedback { id: number; kind: string; content: string; contact: string; reply: string; replied_at: string | null; created_at: string }
export interface Application {
  id: number; store_name: string; contact_name: string; phone: string; city: string; cuisine: string; note: string
  status: 'pending' | 'approved' | 'rejected'; reply: string; decided_at: string | null; created_at: string
}
export interface WalletEntry { id: number; account: string; currency: string; amount: number; note: string; created_at: string }
export type PlatformInfo = Record<'name' | 'about' | 'about_en' | 'support_phone' | 'support_email' | 'support_hours' | 'support_chat', string>

// The profile is shared by the pages so that the header and the red dot stay in step.
export const me = reactive<{ profile: Profile | null }>({ profile: null })

export async function loadProfile(): Promise<Profile | null> {
  if (!(await loadCustomer())) { me.profile = null; return null }
  try { me.profile = await get<Profile>('/customer/me') }
  catch { await loadCustomer(true); if (!customer.verified) me.profile = null }
  return me.profile
}
export const saveNickname = async (nickname: string) => { me.profile = await put<Profile>('/customer/me', { nickname }) }
export const saveAvatar = async (photo: Blob) => { me.profile = await upload<Profile>('/customer/avatar', new File([photo], 'me.jpg', { type: 'image/jpeg' })) }
export const removeAvatar = async () => { me.profile = await del<Profile>('/customer/avatar') }
export const avatarUrl = (p: Profile) => p.avatar_at ? `/api/v1/customer/avatar?v=${encodeURIComponent(p.avatar_at)}` : ''

export const myStores = (favorites = false) => get<{ stores: MyStore[] }>('/customer/stores' + (favorites ? '?favorites=1' : '')).then(r => r.stores)
export const isFavorite = (slug: string) => get<{ favorite: boolean }>(`/customer/favorites/${slug}`).then(r => r.favorite)
export const setFavorite = (slug: string, on: boolean) => on ? put(`/customer/favorites/${slug}`, {}) : del(`/customer/favorites/${slug}`)
export const myOffers = (slug = '') => get<{ offers: StoreOffers[] }>('/customer/offers' + (slug ? `?store=${encodeURIComponent(slug)}` : '')).then(r => r.offers)
export const myReviews = () => get<{ reviews: MyReview[] }>('/customer/reviews').then(r => r.reviews)
export const myMessages = (offset = 0) => get<{ messages: Message[]; more: boolean }>(`/customer/messages?offset=${offset}`)
export const readMessages = (id = 0) => post('/customer/messages/read', { id })
export const myFeedback = () => get<{ feedback: Feedback[] }>('/customer/feedback').then(r => r.feedback)
export const sendFeedback = (f: { kind: string; content: string; contact: string }) => post<Feedback>('/customer/feedback', f)
export const myApplications = () => get<{ applications: Application[] }>('/customer/applications').then(r => r.applications)
export const sendApplication = (a: Record<string, string>) => post<Application>('/customer/applications', a)
export const walletEntries = (account: string) => get<{ entries: WalletEntry[] }>(`/customer/wallet/${account}`).then(r => r.entries)
export const platformInfo = () => get<PlatformInfo>('/platform/info')

// ---- the store "home" goes back to ----
// The last store the customer looked at on this device.
export interface HomeStore { slug: string; name: string; currency: string }
export function homeStore(): HomeStore | null {
  try { const h = JSON.parse(localStorage.getItem('home_store') || 'null'); return h && typeof h.slug === 'string' ? h : null } catch { return null }
}
export function rememberStore(h: HomeStore) { localStorage.setItem('home_store', JSON.stringify(h)) }
export const homePath = () => { const h = homeStore(); return h ? `/s/${h.slug}` : '/me/favorites' }

// +639175550123 -> 0917 *** 0123: what stands in for a name nobody has set.
export function maskedPhone(p: string): string {
  const d = p.replace(/^\+\d{2}/, '0')
  return d.length >= 8 ? `${d.slice(0, 4)} *** ${d.slice(-4)}` : p
}
export const displayName = (p: Profile) => p.nickname || maskedPhone(p.phone)

export function dateTime(iso: string): string {
  return new Date(iso).toLocaleString(locale.value === 'zh' ? 'zh-CN' : 'en-GB', { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false })
}
// 9300 -> "93", 9350 -> "93.50": the bare number under which the currency is named.
export function plain(minor: number): string {
  const v = minor / 100
  return (Number.isInteger(v) ? String(v) : v.toFixed(2)).replace(/\B(?=(\d{3})+(?!\d))/g, ',')
}
export const describe = (e: unknown) => (e as { code?: string }).code === 'NETWORK' ? t('offline') : (e as Error).message

extend({
  'me.home': ['首页', 'Home'], 'me.orders': ['订单', 'Orders'], 'me.me': ['我的', 'Me'],
  'me.login': ['登录 / 注册', 'Log in or sign up'], 'me.login_hint': ['登录后查看订单、收藏和消息', 'Log in to see your orders, favourites and messages'],
  'me.member_no': ['会员号：{no}', 'Member no. {no}'], 'me.settings': ['设置', 'Settings'], 'me.share': ['分享', 'Share'],
  'me.link_copied': ['链接已复制', 'Link copied'],
  'me.balance': ['余额', 'Balance'], 'me.coupons': ['优惠券', 'Offers'], 'me.bonus': ['奖励金', 'Rewards'],
  'me.offer_strip': ['{n} 个优惠', '{n} offers'], 'me.offer_strip_hint': ['下单自动使用，不用领取', 'Applied by themselves at checkout'],
  'me.my_offers': ['我的优惠券 / 红包', 'My offers'], 'me.more': ['更多', 'More'],
  'me.no_offers': ['暂无可用优惠，去店铺看看吧', 'No offers right now. Have a look at a store.'],
  'me.favorites': ['我的收藏', 'Favourites'], 'me.support': ['我的客服', 'Support'], 'me.reviews': ['我的评价', 'My reviews'],
  'me.messages': ['我的消息', 'Messages'], 'me.apply': ['商家入驻', 'Open a store'], 'me.addresses': ['收货地址', 'Addresses'],
  'me.about': ['关于我们', 'About us'], 'me.feedback': ['意见反馈', 'Feedback'], 'me.help': ['账号疑问', 'Account help'],
  'me.need_login': ['请先登录', 'Please log in first'],
  // profile and settings
  'mp.title': ['个人资料和设置', 'Profile and settings'], 'mp.avatar': ['头像', 'Picture'], 'mp.change': ['更换', 'Change'], 'mp.remove': ['移除', 'Remove'],
  'mp.nickname': ['昵称', 'Nickname'], 'mp.nickname_ph': ['最多 20 个字', 'Up to 20 characters'], 'mp.save': ['保存', 'Save'], 'mp.saved': ['已保存', 'Saved'],
  'mp.phone': ['手机号', 'Mobile number'], 'mp.phone_hint': ['手机号就是你的账号，暂不支持更换', 'Your number is your account and cannot be changed yet'],
  'mp.member': ['会员号', 'Member number'], 'mp.language': ['语言', 'Language'], 'mp.terms': ['用户服务协议', 'Terms of Service'], 'mp.privacy': ['隐私政策', 'Privacy Policy'],
  'mp.logout': ['退出登录', 'Log out'], 'mp.logout_confirm': ['确定退出登录？', 'Log out?'], 'mp.photo_failed': ['图片处理失败，请换一张', 'That picture could not be used. Try another.'],
  // wallet
  'mw.balance': ['我的余额', 'My balance'], 'mw.bonus': ['我的奖励金', 'My rewards'], 'mw.records': ['变动记录', 'History'], 'mw.none': ['还没有记录', 'Nothing yet'],
  'mw.balance_note': ['余额目前只由平台记入（例如退款、补偿）。在线充值和用余额付款还没有开通，要等接入线上支付之后。',
    'For now only the platform adds to your balance (refunds, compensation). Topping up and paying from the balance come with online payment, which is not available yet.'],
  'mw.bonus_note': ['奖励金由平台在活动中发放，目前只做记录，还不能在下单时抵扣。',
    'Rewards are given by the platform during campaigns. For now they are only recorded and cannot be spent on an order yet.'],
  // offers
  'mo.title': ['我的优惠', 'My offers'], 'mo.hint': ['下面是你常去的店铺正在进行的优惠。满足条件时下单会自动使用最划算的一个，不用领取。',
    'These are the offers running at your stores. The best one that applies is used by itself at checkout.'],
  'mo.use': ['去使用', 'Order'], 'mo.until': ['{d} 前有效', 'Until {d}'], 'mo.daily': ['每天 {a}–{b}', 'Daily {a}–{b}'],
  'mo.delivery_only': ['仅外卖', 'Delivery only'], 'mo.pickup_only': ['仅自取', 'Pickup only'],
  // favourites
  'mf.title': ['我的收藏', 'Favourites'], 'mf.none': ['还没有收藏的店铺。在店铺页点右上角的星星就能收藏。', 'No favourites yet. Tap the star at the top of a store page to keep it.'],
  'mf.visited': ['点过餐的店', 'Stores you ordered from'], 'mf.unfav': ['取消收藏', 'Remove'], 'mf.fav': ['收藏', 'Keep'], 'mf.faved': ['已收藏', 'Kept'],
  'mf.fav_on': ['已收藏', 'Added to favourites'], 'mf.fav_off': ['已取消收藏', 'Removed from favourites'],
  'mf.no_store': ['还没有去过的店铺。扫店铺二维码或打开店铺链接就可以开始点餐。', 'No stores yet. Scan a store\'s code or open its link to start ordering.'],
  // support
  'ms.title': ['我的客服', 'Support'], 'ms.platform': ['平台客服', 'Platform support'], 'ms.hours': ['服务时间', 'Hours'], 'ms.call': ['拨打', 'Call'],
  'ms.email': ['邮箱', 'Email'], 'ms.chat': ['在线联系', 'Chat'], 'ms.open': ['打开', 'Open'],
  'ms.none': ['平台暂未提供客服电话，订单问题请直接联系门店，其他问题可以在“意见反馈”里留言。',
    'There is no platform hotline yet. For an order, call the store; for anything else leave us a note under Feedback.'],
  'ms.stores': ['订单问题联系门店', 'About an order: call the store'], 'ms.stores_hint': ['餐品、配送、退款都由门店处理', 'Food, delivery and refunds are handled by the store'],
  'ms.to_feedback': ['给平台留言', 'Leave the platform a note'],
  // reviews
  'mr.title': ['我的评价', 'My reviews'], 'mr.none': ['还没有评价。订单完成后可以在订单页评价。', 'No reviews yet. You can review an order once it is finished.'],
  'mr.order': ['查看订单', 'See the order'],
  // messages
  'mm.title': ['我的消息', 'Messages'], 'mm.none': ['还没有消息', 'No messages yet'], 'mm.read_all': ['全部已读', 'Mark all read'],
  'mm.kind_order': ['订单', 'Order'], 'mm.kind_service': ['客服', 'Support'], 'mm.kind_wallet': ['钱包', 'Wallet'], 'mm.load_more': ['加载更多', 'Load more'],
  // applying to open a store
  'ma.title': ['商家入驻', 'Open a store'], 'ma.intro': ['想在平台上开店接单？留下店铺信息，平台审核后会联系你开通商家账号。',
    'Want to take orders on the platform? Leave your details; after a review we will contact you to set up your account.'],
  'ma.store': ['店铺名称', 'Store name'], 'ma.contact': ['联系人', 'Contact person'], 'ma.phone': ['联系电话', 'Phone'], 'ma.city': ['所在城市 / 地区', 'City or area'],
  'ma.cuisine': ['经营品类', 'What you sell'], 'ma.cuisine_ph': ['例如：中餐、奶茶、烧烤', 'e.g. Chinese food, milk tea'], 'ma.note': ['补充说明', 'Anything else'],
  'ma.submit': ['提交申请', 'Send application'], 'ma.sent': ['申请已提交，请留意“我的消息”', 'Application sent. Watch your messages.'],
  'ma.mine': ['我的申请', 'My applications'], 'ma.s_pending': ['审核中', 'In review'], 'ma.s_approved': ['已通过', 'Approved'], 'ma.s_rejected': ['未通过', 'Not approved'],
  'ma.reply': ['平台回复', 'Reply'], 'ma.pending': ['你有一份申请正在审核，结果出来前不能再提交。', 'One application is in review. You can send another once it is decided.'],
  'ma.e.required': ['请填写店铺名称、联系人和联系电话', 'Enter the store name, contact person and phone'],
  // about
  'mb.title': ['关于我们', 'About us'], 'mb.default': ['这是一个餐饮外卖点餐平台：顾客在门店页面点餐，门店自己接单、备餐和配送。',
    'A food ordering platform: customers order from a store\'s page, and the store accepts, prepares and delivers.'],
  // feedback
  'mk.title': ['意见反馈', 'Feedback'], 'mk.kind': ['反馈类型', 'About'], 'mk.suggestion': ['功能建议', 'Suggestion'], 'mk.problem': ['使用问题', 'A problem'],
  'mk.complaint': ['投诉', 'Complaint'], 'mk.account': ['账号问题', 'My account'], 'mk.other': ['其他', 'Other'],
  'mk.content': ['反馈内容', 'Your note'], 'mk.content_ph': ['请描述你遇到的问题或建议（最多 1000 字）', 'Describe the problem or idea (up to 1000 characters)'],
  'mk.contact': ['联系方式（选填）', 'Contact (optional)'], 'mk.contact_ph': ['微信、邮箱等，方便我们回复', 'WeChat, email… so we can reach you'],
  'mk.submit': ['提交', 'Send'], 'mk.sent': ['已提交，谢谢。回复会出现在“我的消息”里。', 'Sent, thank you. A reply will show up in your messages.'],
  'mk.mine': ['我的反馈', 'My feedback'], 'mk.waiting': ['等待回复', 'Waiting for a reply'], 'mk.replied': ['平台回复', 'Reply'],
  'mk.e.content': ['请填写反馈内容', 'Write your note first'],
  // account help
  'mh.title': ['账号疑问', 'Account help'], 'mh.still': ['没有找到答案？', 'Still stuck?'], 'mh.ask': ['给平台留言', 'Leave us a note'],
  'mh.q1': ['收不到验证码怎么办？', 'I do not get the code'], 'mh.a1': ['先确认手机号填写正确、信号正常，等 60 秒后重新获取。同一号码每天获取次数有上限，用完要等第二天。还是收不到请联系门店或给平台留言。',
    'Check the number and your signal, then ask again after 60 seconds. There is a daily limit per number. If it still does not arrive, contact the store or leave us a note.'],
  'mh.q2': ['可以更换手机号吗？', 'Can I change my number?'], 'mh.a2': ['目前手机号就是账号，不能直接更换。用新手机号登录会得到一个新账号，原账号的订单、地址和余额不会自动转过去。需要转移请给平台留言。',
    'Your number is your account and cannot be changed directly. Logging in with a new number makes a new account; orders, addresses and balance do not move by themselves. Leave us a note if you need them moved.'],
  'mh.q3': ['没有密码，账号安全吗？', 'There is no password. Is that safe?'], 'mh.a3': ['登录只靠发到你手机的验证码，验证码 5 分钟内有效。不要把验证码告诉别人。在别人的设备上用完请到“设置”里退出登录。',
    'You log in only with a code texted to your phone, valid for 5 minutes. Never share it. On someone else\'s device, log out under Settings when you are done.'],
  'mh.q4': ['登录状态能保持多久？', 'How long do I stay logged in?'], 'mh.a4': ['在同一台设备上保持 30 天，期间下单会自动续期。', '30 days on the same device, extended whenever you order.'],
  'mh.q5': ['怎么注销账号、删除我的资料？', 'How do I delete my account?'], 'mh.a5': ['在“意见反馈”里选择“账号问题”并说明要注销，平台核实后处理。已完成的订单记录门店需要依法保留。',
    'Under Feedback choose "My account" and say you want it deleted; the platform handles it after checking. Stores must keep the records of finished orders as the law requires.'],
  'mh.q6': ['余额和奖励金怎么用？', 'How do I use my balance and rewards?'], 'mh.a6': ['目前只做记录：余额由平台记入（例如退款），奖励金由活动发放，都还不能在下单时使用。接入线上支付后开通。',
    'For now they are only recorded: the platform adds to the balance (refunds, for example) and rewards come from campaigns. Neither can be spent on an order until online payment is available.'],
})
