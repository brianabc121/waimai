// Stored value, "top up X, get Y free" (docs/STORED_VALUE_DESIGN.md): the
// store's marketing page and counter, the customer's offers page and top-up
// code. Texts are [zh, en].
import { ApiError, get, post, put } from './api'
import { extend, has, money, t } from './i18n'
import type { CreditCodeSent } from './credit'

export interface StoredTier {
  id: number; pay: number; bonus: number; coupon_kind: '' | 'store_free' | 'store_threshold'; coupon_value: number; coupon_min: number
  coupon_days: number; coupon_qty: number; label: string; per_customer: number | null; first_only: boolean; quota: number | null
  taken: number; mine?: number; closed?: '' | 'first_only' | 'per_customer' | 'quota'
}
export interface StoredCampaign {
  id: number; name: string; name_en: string; starts_at: string | null; ends_at: string | null; status: 'active' | 'paused'
  state: 'running' | 'upcoming' | 'ended' | 'paused'; custom_ok: boolean; custom_min: number; brand_ids: number[]; note: string
  tiers: StoredTier[]; count: number; paid: number; given: number
}
export interface StoredOverview {
  currency: string; enabled: boolean; cap: number; terms_at: string | null; approve_over: number; principal: number; bonus: number
  holders: number; new_holders: number; month_count: number; month_paid: number; month_given: number
  month_spent_principal: number; month_spent_bonus: number; campaigns: StoredCampaign[]
}
export interface StoredBalance { credit: number; principal: number; bonus: number; total: number; frozen: boolean }
export interface Topup {
  id: number; branch_id: number; store_name: string; slug: string; brand_name: string; customer_id: number; phone: string; currency: string
  pay: number; bonus: number; coupon_kind: string; coupon_value: number; coupon_min: number; coupon_qty: number; source: 'customer' | 'staff'
  status: 'pending' | 'paid' | 'cancelled' | 'revoked' | 'expired'; code?: string; token?: string; expires_at: string
  pay_type: string; pay_method: string; received_by: string; paid_at: string | null; revoked_at: string | null; created_at: string; revocable: boolean
}
export interface StoredOffers { currency: string; enabled: boolean; campaign: StoredCampaign | null; balance: StoredBalance | null; pending: Topup | null }
export interface TopupStarted { topup: Topup; verify: CreditCodeSent; balance: StoredBalance }
export interface CashoutQuote {
  customer_id: number; phone: string; currency: string; reason: 'customer' | 'store'; topped_up: number; bonus_given: number
  principal: number; bonus: number; bonus_used: number; clawback: number; paid_out: number
}
export interface StoredHolder { customer_id: number; phone: string; principal: number; bonus: number; frozen: boolean; last_topup: string | null }
export interface StoredEntry {
  id: number; at: string; kind: string; principal: number; bonus: number; order_id: number; public_no: string; topup_id: number
  phone?: string; by?: string; note?: string
}
export interface BranchStored {
  currency: string; principal: number; bonus: number; month_cashouts: number; month_paid_out: number; month_void: number
  holders: StoredHolder[]; entries: StoredEntry[]
}
export interface StoredCountry { country_code: string; enabled: boolean; cap: number; note: string; currency: string; stores: number; principal: number; bonus: number }
export interface AdminStoredBranch {
  branch_id: number; name: string; country_code: string; currency: string; principal: number; bonus: number
  month_topup: number; month_paid_out: number; frozen: boolean; closed: boolean; watch: boolean
}

const B = (bid: number) => `/merchant/branches/${bid}`
export const storedOverview = (bid: number) => get<StoredOverview>(`${B(bid)}/marketing`)
export const acceptStoredTerms = (bid: number) => post(`${B(bid)}/marketing/terms`)
export const saveCampaign = (bid: number, id: number, body: unknown) =>
  id ? put<{ id: number }>(`${B(bid)}/stored-campaigns/${id}`, body) : post<{ id: number }>(`${B(bid)}/stored-campaigns`, body)
export const setCampaignStatus = (bid: number, id: number, status: 'active' | 'paused') => post(`${B(bid)}/stored-campaigns/${id}/status`, { status })
export const branchTopups = (bid: number, q: { status?: string; code?: string } = {}) =>
  get<{ items: Topup[] }>(`${B(bid)}/topups?status=${q.status ?? ''}&code=${q.code ?? ''}`).then(r => r.items)
export const startStaffTopup = (bid: number, body: { phone: string; tier_id?: number; amount?: number; brand_id?: number }) =>
  post<TopupStarted>(`${B(bid)}/topups`, body)
export const confirmTopup = (bid: number, id: number, body: { pay_type: string; pay_channel_id: number; verify?: { id: number; code: string } },
  headers?: Record<string, string>) => post<Topup>(`${B(bid)}/topups/${id}/confirm`, body, headers)
export const revokeTopup = (bid: number, id: number, note: string, headers?: Record<string, string>) =>
  post(`${B(bid)}/topups/${id}/revoke`, { note }, headers)
export const branchStored = (bid: number, customer = 0) => get<BranchStored>(`${B(bid)}/stored${customer ? '?customer=' + customer : ''}`)
export const cashoutQuote = (bid: number, cid: number, reason: string) => get<CashoutQuote>(`${B(bid)}/stored/${cid}/cashout?reason=${reason}`)
export const storedCashout = (bid: number, cid: number, body: unknown, headers?: Record<string, string>) =>
  post<CashoutQuote>(`${B(bid)}/stored/${cid}/cashout`, body, headers)

export const storeStored = (slug: string) => get<StoredOffers>(`/stores/${encodeURIComponent(slug)}/stored`)
export const createTopup = (slug: string, body: { tier_id?: number; amount?: number }) => post<Topup>(`/stores/${encodeURIComponent(slug)}/topups`, body)
export const myTopup = (id: number) => get<Topup>(`/customer/topups/${id}`)
export const cancelMyTopup = (id: number) => post(`/customer/topups/${id}/cancel`)
export const myStoredEntries = (bid: number) => get<{ items: StoredEntry[] }>(`/customer/store-credits/${bid}/stored`).then(r => r.items)

export function storedErr(e: unknown): string {
  if (e instanceof ApiError) {
    const vars = { amt: String(e.details?.limit ?? ''), cap: String(e.details?.cap ?? '') }
    if (has(`sv.err.${e.code}`)) return t(`sv.err.${e.code}`, vars)
    if (has(`sc.err.${e.code}`)) return t(`sc.err.${e.code}`, vars)
  }
  return (e as Error).message
}

// couponText is what comes with a tier, e.g. "2 × 50 off".
export function tierCoupon(x: { coupon_kind: string; coupon_value: number; coupon_min: number; coupon_qty: number }, cur: string): string {
  if (!x.coupon_qty) return ''
  const v = money(x.coupon_value, cur)
  return x.coupon_kind === 'store_threshold' ? t('sv.t.coupon_th', { n: x.coupon_qty, min: money(x.coupon_min, cur), v }) : t('sv.t.coupon', { n: x.coupon_qty, v })
}
// the limits of a tier, e.g. "first top-up only · 50 left"
export function tierLimits(x: StoredTier): string[] {
  const out: string[] = []
  if (x.first_only) out.push(t('sv.t.first'))
  if (x.per_customer) out.push(t('sv.t.per', { n: x.per_customer }))
  if (x.quota) out.push(t('sv.t.quota', { left: Math.max(0, x.quota - x.taken) }))
  return out
}
// the bonus as a share of what is paid, e.g. "+20%"
export const bonusRate = (x: { pay: number; bonus: number }) => x.pay > 0 ? Math.round(x.bonus * 100 / x.pay) : 0

extend({
  // the menu and permissions
  'mk.nav': ['营销', 'Marketing'],
  'mk.campaigns': ['营销活动', 'Campaigns'],
  'sv.desk': ['会员充值', 'Top up'],
  'rp.p.stored.topup': ['会员充值', 'Top up stored value'],
  'rp.d.stored.topup': ['在“会员充值”收款确认充值；超过店主设的金额要经理授权', 'Confirm top-ups at the counter; above the owner\'s limit a manager approves'],
  'rp.p.stored.cashout': ['退储值、撤销充值', 'Pay back stored value'],
  'rp.d.stored.cashout': ['把顾客的储值退成现金，撤销当天收错的充值', 'Pay a customer\'s stored value back in money, undo a top-up taken by mistake today'],
  // the marketing page
  'sv.m.hint': ['充值送：顾客先充钱，本店送赠送金和券。充的钱是预收款，顾客消费时才算营业额。', 'Top up and get more: customers pay in advance and get a bonus and coupons. The money is paid in advance; it becomes takings as they order.'],
  'sv.m.off': ['平台还没有在你的国家或地区开通储值', 'Stored value is not available in your country yet'],
  'sv.m.terms_t': ['开通充值送之前请看清楚', 'Before you turn on top-ups'],
  'sv.m.terms': ['顾客充的钱是门店欠顾客的，要能随时兑现；储值只能在本店用；顾客要退，按“顾客原因退本金扣已用赠送、门店原因全退本金”处理；还有顾客储值时门店不能删除；每位顾客在本店最多存 {cap}。',
    'Money customers top up is owed to them and has to be honoured; it is spent at this store only; refunds follow the rules (the customer\'s reasons: the principal left less the bonus used; the store\'s: all the principal left); a store cannot be deleted while customers hold stored value; one customer holds at most {cap} here.'],
  'sv.m.terms_ok': ['我已了解，开通充值送', 'I understand: turn on top-ups'],
  'sv.m.terms_owner': ['只有店主能开通，请找店主', 'Only the owner can turn this on'],
  'sv.m.owed': ['顾客储值余额', 'Customers hold'],
  'sv.m.parts': ['本金 {p} · 赠送 {b}', 'Principal {p} · bonus {b}'],
  'sv.m.holders': ['{n} 位顾客', '{n} customers'],
  'sv.m.month_in': ['本月充值', 'Topped up this month'],
  'sv.m.month_n': ['{n} 笔 · 送出 {b}', '{n} top-ups · {b} given'],
  'sv.m.month_spent': ['本月储值消费', 'Spent this month'],
  'sv.m.month_spent_s': ['其中赠送 {b}', '{b} of it bonus'],
  'sv.m.campaigns': ['充值送活动', 'Top-up offers'],
  'sv.m.new': ['新建活动', 'New offer'],
  'sv.m.edit': ['编辑', 'Edit'], 'sv.m.pause': ['暂停', 'Pause'], 'sv.m.resume': ['恢复', 'Resume'],
  'sv.m.none': ['还没有充值送活动', 'No top-up offers yet'],
  'sv.m.always': ['长期有效', 'No end date'],
  'sv.m.period': ['{from} 至 {to}', '{from} to {to}'],
  'sv.m.stats': ['已充 {n} 笔 · 实收 {paid} · 送出 {given}', '{n} top-ups · {paid} taken · {given} given'],
  'sv.m.promos': ['满减、菜品活动和优惠券在“优惠活动”里设置', 'Order offers, dish deals and coupons are under Promotions'],
  'sv.m.topups': ['最近充值', 'Recent top-ups'],
  'sv.m.approve_over': ['单笔充值超过这个金额要经理授权', 'A top-up above this needs a manager'],
  'sv.m.saved': ['已保存', 'Saved'],
  'sv.st.running': ['进行中', 'Running'], 'sv.st.upcoming': ['未开始', 'Upcoming'], 'sv.st.ended': ['已结束', 'Ended'], 'sv.st.paused': ['已暂停', 'Paused'],
  'sv.t.pay': ['充 {amt}', 'Pay {amt}'],
  'sv.t.give': ['送 {amt}', 'Get {amt} free'],
  'sv.t.coupon': ['再送 {n} 张 {v} 无门槛券', '+ {n} coupons of {v} off'],
  'sv.t.coupon_th': ['再送 {n} 张满 {min} 减 {v} 券', '+ {n} coupons of {v} off {min}'],
  'sv.t.first': ['首充专享', 'First top-up only'],
  'sv.t.per': ['每人限 {n} 次', '{n} per customer'],
  'sv.t.quota': ['剩 {left} 份', '{left} left'],
  'sv.t.custom': ['其他金额 {min} 起，不送', 'Any amount from {min}, nothing extra'],
  'sv.t.rate': ['多 {pct}%', '+{pct}%'],
  // top-ups
  'sv.c.time': ['时间', 'Time'], 'sv.c.customer': ['顾客', 'Customer'], 'sv.c.pay': ['实收', 'Paid'], 'sv.c.bonus': ['赠送', 'Bonus'],
  'sv.c.way': ['收款方式', 'Paid by'], 'sv.c.by': ['经手人', 'By'], 'sv.c.status': ['状态', 'Status'],
  'sv.ts.pending': ['待收款', 'Waiting'], 'sv.ts.paid': ['已到账', 'Paid'], 'sv.ts.cancelled': ['已取消', 'Cancelled'],
  'sv.ts.revoked': ['已撤销', 'Undone'], 'sv.ts.expired': ['已过期', 'Expired'],
  'sv.revoke': ['撤销', 'Undo'],
  'sv.revoke_ask': ['撤销这笔充值？钱从钱箱退还顾客，赠送和券一起收回。只有当天、一分没用过的才能撤销。', 'Undo this top-up? The money goes back to the customer from the drawer, and the bonus and coupons are taken back. Only today\'s, and only if none of it was spent.'],
  'sv.revoked': ['已撤销', 'Undone'],
  'sv.revoke_what': ['撤销充值', 'undoing a top-up'],
  // the campaign editor
  'sv.e.new': ['新建充值送活动', 'New top-up offer'], 'sv.e.edit': ['编辑充值送活动', 'Edit top-up offer'],
  'sv.e.name': ['活动名称', 'Name'], 'sv.e.name_en': ['英文名称（可不填）', 'English name (optional)'],
  'sv.e.starts': ['开始', 'Starts'], 'sv.e.ends': ['结束（不填就是长期）', 'Ends (empty: no end)'],
  'sv.e.tiers': ['充值档位', 'Tiers'], 'sv.e.pay': ['充值金额', 'Pay'], 'sv.e.bonus': ['赠送金额', 'Bonus'],
  'sv.e.coupon': ['送券', 'Coupons'], 'sv.e.coupon_none': ['不送券', 'None'], 'sv.e.coupon_free': ['无门槛券', 'Any order'],
  'sv.e.coupon_th': ['满减券', 'With a minimum'], 'sv.e.coupon_value': ['每张减', 'Off'], 'sv.e.coupon_min': ['满多少可用', 'Minimum'],
  'sv.e.coupon_qty': ['张数', 'How many'], 'sv.e.coupon_days': ['有效天数', 'Valid days'],
  'sv.e.label': ['角标（如“最划算”）', 'Badge (e.g. "Best value")'],
  'sv.e.first': ['首充专享', 'First top-up only'], 'sv.e.per': ['每人限次', 'Per customer'], 'sv.e.quota': ['总份数', 'Quota'],
  'sv.e.add': ['加一档', 'Add a tier'], 'sv.e.remove': ['删掉这一档', 'Remove tier'],
  'sv.e.custom': ['也可以充其他金额（不送）', 'Also any amount (nothing extra)'], 'sv.e.custom_min': ['最少充', 'At least'],
  'sv.e.brands': ['哪些品牌的页面显示（不选就是全部）', 'Shown on which brands (none: all)'],
  'sv.e.note': ['活动说明（顾客看得到）', 'Note (customers see it)'],
  'sv.e.rate_warn': ['有一档赠送超过 30%，请确认毛利扛得住', 'A tier gives more than 30%: make sure the margin can take it'],
  'sv.e.save': ['保存', 'Save'], 'sv.e.cancel': ['取消', 'Cancel'],
  // the counter
  'sv.d.hint': ['顾客出示充值码，或者按手机号给顾客充值（顾客要报短信验证码）。收到钱再确认。', 'The customer shows their top-up code, or top up by phone number (the customer reads out a texted code). Confirm once you have the money.'],
  'sv.d.by_code': ['顾客的充值码', 'Customer\'s top-up code'], 'sv.d.code_ph': ['6 位数字', '6 digits'], 'sv.d.find': ['查找', 'Find'],
  'sv.d.not_found': ['没有这个待收款的充值码', 'No waiting top-up with this code'],
  'sv.d.by_phone': ['按手机号充值', 'Top up by phone number'], 'sv.d.phone': ['顾客手机号', 'Customer\'s mobile'],
  'sv.d.other': ['其他金额', 'Other amount'],
  'sv.d.confirm': ['收款确认', 'Take the money'],
  'sv.d.get': ['收 {pay}，到账 {total}', 'Take {pay}, they get {total}'],
  'sv.d.ok': ['已收到 {amt}，确认充值', 'Got {amt}: confirm'],
  'sv.d.done': ['充值成功：{p} 到账 {total}', 'Topped up: {p} now has {total} more'],
  'sv.d.today': ['今天的充值', 'Today\'s top-ups'],
  'sv.d.approve': ['大额充值', 'a large top-up'],
  'sv.d.off': ['本店还没有进行中的充值送活动，请先在“营销活动”里建一个', 'No top-up offer is running. Create one under Marketing first.'],
  // stored value on the store balances page
  'sv.p.tab': ['储值', 'Stored value'], 'sv.p.principal': ['本金', 'Principal'], 'sv.p.bonus': ['赠送', 'Bonus'],
  'sv.p.last': ['最后充值', 'Last top-up'], 'sv.p.cashout': ['退储值', 'Pay back'],
  'sv.p.month': ['本月退储值 {n} 笔，退出 {amt}', '{n} paid back this month, {amt} in all'],
  'sv.k.topup': ['充值', 'Top-up'], 'sv.k.topup_revoke': ['撤销充值', 'Top-up undone'], 'sv.k.stored_spend': ['下单使用', 'Spent'],
  'sv.k.stored_return': ['订单退回', 'Returned'], 'sv.k.stored_refund': ['退款退回', 'Refunded'], 'sv.k.stored_cashout': ['退储值', 'Paid back'],
  'sv.o.reason': ['原因', 'Reason'],
  'sv.o.r.customer': ['顾客原因', 'The customer\'s'], 'sv.o.r.store': ['门店原因（停业、换老板等）', 'The store\'s (closing, new owner…)'],
  'sv.o.hint_customer': ['退剩余本金，扣回已经用掉的赠送，剩余赠送作废。', 'Pays back the principal left less the bonus already used; the bonus left is void.'],
  'sv.o.hint_store': ['剩余本金全部退还，剩余赠送作废。', 'Pays back all the principal left; the bonus left is void.'],
  'sv.o.left': ['剩余本金', 'Principal left'], 'sv.o.bonus_left': ['剩余赠送（作废）', 'Bonus left (void)'],
  'sv.o.used': ['已用赠送（扣回）', 'Bonus used (kept back)'], 'sv.o.pay': ['退给顾客', 'Pay back'],
  'sv.o.way': ['怎么退', 'Paid back by'],
  'sv.o.w.cash': ['现金', 'Cash'], 'sv.o.w.transfer': ['转账', 'Transfer'], 'sv.o.w.online': ['在线', 'Online'], 'sv.o.w.other': ['其他', 'Other'],
  'sv.o.note': ['备注', 'Note'], 'sv.o.go': ['确认退 {amt}', 'Pay back {amt}'], 'sv.o.done': ['已退还 {amt}', '{amt} paid back'],
  'sv.o.what': ['退储值', 'paying back stored value'],
  // the customer's offers page
  'sv.c.title': ['优惠活动', 'Offers'],
  'sv.c.bar': ['充 {pay} 送 {bonus}', 'Pay {pay}, get {bonus} free'],
  'sv.c.bal': ['我在本店的余额', 'My balance here'],
  'sv.c.parts': ['本金 {p} · 赠送 {b}', 'Principal {p} · bonus {b}'],
  'sv.c.credit': ['退款余额 {c}', 'Refunds {c}'],
  'sv.c.login': ['登录后查看余额和充值', 'Sign in to see your balance and top up'],
  'sv.c.records': ['明细', 'History'],
  'sv.c.topup': ['充值送', 'Top up and get more'],
  'sv.c.go': ['去充值', 'Top up'],
  'sv.c.closed.first_only': ['已充过', 'Already used'], 'sv.c.closed.per_customer': ['已达上限', 'Limit reached'], 'sv.c.closed.quota': ['已抢完', 'All gone'],
  'sv.c.amount': ['其他金额', 'Other amount'],
  'sv.c.rules': ['充值说明', 'How it works'],
  'sv.c.rule1': ['只能在本店使用，不能转给别人，不能提现。', 'Only at this store; it cannot be passed on or cashed out.'],
  'sv.c.rule2': ['付款时先用退款余额，再按比例用本金和赠送。', 'Paying uses refunds first, then principal and bonus in proportion.'],
  'sv.c.rule3': ['长期有效。确实要退，请联系门店：顾客原因退款会扣回已用的赠送。', 'It does not expire. To get money back, ask the store: for your own reasons the bonus you used is kept back.'],
  'sv.c.coupons': ['领券', 'Coupons'],
  'sv.c.promos': ['店铺活动', 'Store offers'],
  'sv.c.none': ['本店暂时没有优惠活动', 'No offers at this store right now'],
  'sv.q.title': ['出示充值码', 'Show your top-up code'],
  'sv.q.hint': ['把这个码给店员看，付钱后马上到账。30 分钟内有效。', 'Show this code to the staff; it goes in as soon as you pay. Valid 30 minutes.'],
  'sv.q.amount': ['付 {pay}，到账 {total}', 'Pay {pay}, get {total}'],
  'sv.q.until': ['{at} 前有效', 'Valid until {at}'],
  'sv.q.cancel': ['不充了', 'Cancel'],
  'sv.q.paid': ['充值成功，到账 {total}', 'Done: {total} added'],
  'sv.q.waiting': ['等待店员确认收款…', 'Waiting for the staff to take the money…'],
  'sv.q.expired': ['充值码已过期，请重新生成', 'The code expired. Make a new one.'],
  'sv.q.back': ['回到店铺', 'Back to the store'],
  'sv.co.upsell': ['充 {pay} 送 {bonus}，充值后这一单就能用余额付', 'Pay {pay}, get {bonus} free, and use it on this order'],
  'sv.r.split': ['其中 {s} 是储值付的，退回顾客的储值（本金和赠送按原比例）', '{s} of it was paid from stored value and goes back to it (principal and bonus as paid)'],
  'ck.pt.stored_value': ['储值', 'Stored value'],
  // reconciliation and bills
  'sv.rc.title': ['储值', 'Stored value'],
  'sv.rc.hint': ['充值是预收款，不算营业额；顾客用储值下单时才算。', 'Top-ups are money taken in advance, not takings; orders paid with them are.'],
  'sv.rc.topups': ['充值 {n} 笔', '{n} top-ups'], 'sv.rc.given': ['送出赠送', 'Bonus given'],
  'sv.rc.revoked': ['撤销', 'Undone'], 'sv.rc.spent': ['下单用掉', 'Spent on orders'], 'sv.rc.spent_s': ['本金 {p} · 赠送 {b}', 'principal {p} · bonus {b}'],
  'sv.rc.cashouts': ['退储值 {n} 笔', '{n} paid back'], 'sv.rc.clawback': ['扣回赠送 {v}', '{v} bonus kept back'],
  'sv.rc.cash_in': ['储值现金充值', 'Stored value in cash'], 'sv.rc.cash_out': ['退储值付出现金', 'Stored value paid back in cash'],
  'sv.rc.to_stored': ['其中 {v} 退回顾客储值', '{v} of it went back into stored value'],
  'sv.bl.stored': ['储值', 'Stored value'],
  'sv.bl.stored_s': ['本金收款 {p} · 赠送抵扣 {b}', 'principal {p} · bonus used {b}'],
  'sv.bl.topups': ['本期充值本金 {p} · 送出 {b}', 'Topped up {p} · bonus given {b}'],
  // the customer's balance page
  'sv.me.entries': ['储值明细', 'Stored value history'],
  // errors
  'sv.err.STORED_OFF': ['平台还没有在这个国家或地区开通储值', 'Stored value is not available in this country'],
  'sv.err.STORED_TERMS': ['请店主先在“营销活动”页开通充值送', 'The owner has to turn on top-ups on the Marketing page first'],
  'sv.err.STORED_CAMPAIGN_OVERLAP': ['同一时间只能有一个进行中的充值送活动', 'Only one top-up offer can run at a time'],
  'sv.err.STORED_NO_CAMPAIGN': ['本店现在没有充值送活动', 'The store has no top-up offer now'],
  'sv.err.STORED_TIER_GONE': ['这一档已经下架了，请刷新', 'That tier is gone. Refresh the page.'],
  'sv.err.STORED_TIER_CLOSED': ['这一档不能再充了（首充专享、次数或份数用完）', 'That tier is not available (first top-up only, or its limit is reached)'],
  'sv.err.STORED_AMOUNT': ['金额不对，请选一档或者输入不低于最低金额的数', 'Pick a tier, or an amount at least the minimum'],
  'sv.err.STORED_CAP': ['超过每位顾客在本店能存的上限 {cap}', 'This is more than one customer may hold here ({cap})'],
  'sv.err.TOPUP_NOT_PENDING': ['这笔充值已经确认过、取消了或者过期了', 'This top-up was already confirmed, cancelled or has expired'],
  'sv.err.STORED_NEEDS_APPROVAL': ['单笔超过 {amt}，要经理授权', 'Above {amt}: a manager has to approve'],
  'sv.err.TOPUP_NOT_REVOCABLE': ['只有当天到账的充值能撤销', 'Only a top-up paid today can be undone'],
  'sv.err.TOPUP_SPENT': ['这笔储值已经用过了，不能撤销，请用“退储值”', 'Some of it was already spent: use "Pay back" instead'],
  'sv.err.STORED_NONE': ['这位顾客在本店没有储值', 'This customer holds no stored value here'],
  'sv.err.STORED_CHANGED': ['顾客的储值刚变过，请重新看一下金额', 'Their stored value just changed. Check the amount again.'],
  'sv.err.STORED_CASHOUT_OWNER': ['金额较大，要店主授权', 'This much needs the owner to approve'],
})
