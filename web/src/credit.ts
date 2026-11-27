// Store credit on the store's side (docs/REFUND_BALANCE_DESIGN.md): a refund
// into the customer's balance at this store, the texted code for an order the
// staff entered, and the store's page of balances. Texts are [zh, en].
import { ApiError, get, post, put } from './api'
import { extend, has, t } from './i18n'

export interface CreditCodeSent { id: number; phone: string; balance: number; dev_code?: string }
export interface CreditEntry {
  id: number; at: string; kind: 'refund' | 'spend' | 'return' | 'revoke'; amount: number; balance: number
  order_id: number; public_no: string; brand_name: string; refund_id: number
  customer_id?: number; phone?: string; by?: string; note?: string; revocable?: boolean
}
export interface CreditHolder { customer_id: number; phone: string; balance: number; frozen: boolean; since: string }
export interface CreditAlert { kind: 'customer_often' | 'quick_refund' | 'staff_often'; phone?: string; by?: string; public_no?: string; order_id?: number; count?: number; amount?: number }
export interface BranchCredit {
  currency: string; owed: number; customers: number; in: number; used: number; revoked: number
  holders: CreditHolder[]; entries: CreditEntry[]; alerts: CreditAlert[]; approve_over: number; daily_limit: number
}

export const requestCreditCode = (orderID: number, purpose: 'spend' | 'refund', phone = '') =>
  post<CreditCodeSent>(`/merchant/orders/${orderID}/credit-code`, { purpose, phone })
export const spendCreditByCode = <T>(orderID: number, id: number, code: string) => post<T>(`/merchant/orders/${orderID}/credit`, { id, code })
export const branchCredit = (branchID: number, q: Record<string, string | number> = {}) => {
  const qs = new URLSearchParams(Object.entries(q).filter(([, v]) => v !== '' && v !== 0).map(([k, v]) => [k, String(v)])).toString()
  return get<BranchCredit>(`/merchant/branches/${branchID}/credit${qs ? '?' + qs : ''}`)
}
export const saveCreditLimits = (branchID: number, approveOver: number, dailyLimit: number) =>
  put(`/merchant/branches/${branchID}/credit-limits`, { approve_over: approveOver, daily_limit: dailyLimit })
export const revokeCredit = (refundID: number) => post(`/merchant/refunds/${refundID}/revoke-credit`, {})

export function creditErr(e: unknown): string {
  if (e instanceof ApiError && has(`sc.err.${e.code}`)) return t(`sc.err.${e.code}`, { amt: String(e.details?.limit ?? '') })
  return (e as Error).message
}

extend({
  // role permissions
  'rp.p.wallet.refund': ['退到店铺余额', 'Refund into store balances'],
  'rp.d.wallet.refund': ['按菜退款时把钱存进顾客在本店的余额；超过店主设的限额要经理或店主授权', "Put a refund into the customer's balance at this store; above the owner's limits a manager or the owner approves"],
  'rp.p.wallet.view': ['看店铺余额', 'See store balances'],
  'rp.d.wallet.view': ['看顾客在本店的余额、变动记录和提醒', 'See what customers hold here, the history and the alerts'],
  // the refund sheet
  'sc.r.dest': ['退到哪里', 'Where it goes'],
  'sc.r.to_money': ['退钱给顾客', 'Money back'], 'sc.r.to_credit': ['存入顾客的店铺余额', 'Into their store balance'],
  'sc.r.credit_hint': ['钱不退出去，存进顾客在本店的余额，只能在本店下次消费用。要顾客同意。', 'Nothing is paid out: it goes into the customer\'s balance here, only for their next orders at this store. The customer has to agree.'],
  'sc.r.consent': ['顾客怎么同意的', 'How the customer agreed'],
  'sc.r.c.in_person': ['当面同意', 'In person'], 'sc.r.c.phone': ['电话同意', 'On the phone'], 'sc.r.c.chat': ['聊天里同意', 'In a chat'],
  'sc.r.split': ['其中 {cash} 退回顾客，{credit} 是顾客用店铺余额付的，退回店铺余额', '{cash} goes back to the customer; {credit} was paid from their store balance and goes back to it'],
  'sc.r.split_credit': ['全部 {s} 存入顾客的店铺余额', 'All {s} goes into their store balance'],
  'sc.r.code_need': ['这张单是店员录入的，要给顾客发短信验证码，确认存进谁的余额', 'Staff entered this order: text the customer a code to confirm whose balance it is'],
  'sc.r.phone': ['顾客手机号', 'Customer\'s mobile'],
  'sc.r.send': ['发验证码', 'Text a code'], 'sc.r.resend': ['重新发送', 'Send again'],
  'sc.r.sent': ['已发到 {p}，请顾客告诉你 6 位验证码', 'Sent to {p}. Ask the customer for the 6 digits.'],
  'sc.r.code': ['验证码', 'Code'],
  'sc.r.ok': ['存入店铺余额 {s}', 'Put {s} into the balance'],
  'sc.r.approve': ['退到店铺余额', 'Refund into a store balance'],
  // spending a balance on an order the staff entered
  'sc.s.btn': ['用店铺余额', 'Use store balance'],
  'sc.s.title': ['用顾客的店铺余额付款', 'Pay from the customer\'s store balance'],
  'sc.s.hint': ['给顾客发短信验证码，顾客把验证码告诉你，才能用他的余额', 'Text the customer a code; once they read it out, their balance can be used'],
  'sc.s.balance': ['顾客在本店余额 {s}', 'Balance at this store {s}'],
  'sc.s.ok': ['用余额付款', 'Pay from the balance'], 'sc.s.done': ['已用店铺余额付 {s}', '{s} paid from the store balance'],
  // the store's page
  'sc.p.title': ['店铺余额', 'Store balances'],
  'sc.p.hint': ['顾客在本店的余额是店铺欠顾客的钱：退款存进去，顾客下单时用掉。只能在本店用，不能提现。', 'Balances here are money the store owes customers: refunds go in, orders spend them. Only at this store, never paid out.'],
  'sc.p.owed': ['顾客余额合计', 'Customers hold'], 'sc.p.customers': ['{n} 位顾客', '{n} customers'],
  'sc.p.in': ['存入', 'Refunded in'], 'sc.p.used': ['已用', 'Spent'], 'sc.p.revoked': ['撤回', 'Revoked'],
  'sc.p.holders': ['谁有余额', 'Who holds a balance'], 'sc.p.entries': ['变动记录', 'History'], 'sc.p.alerts': ['需要留意', 'Worth a look'],
  'sc.p.revoke': ['改为现金退', 'Paid in cash instead'],
  'sc.p.revoke_ask': ['顾客要现金？把这笔从余额里扣回，退款改记为现金（只有余额还够时才行）。', 'The customer wants cash? This takes it back out of the balance and records the refund as cash (only while the balance still holds it).'],
  'sc.p.revoked_ok': ['已改为现金退款', 'Changed to a cash refund'],
  'sc.p.a.customer_often': ['{p} 最近 30 天退到余额 {n} 次', '{p}: {n} refunds into the balance in 30 days'],
  'sc.p.a.quick_refund': ['订单 {no} 下单后不到 30 分钟就退到余额', 'Order {no} was refunded into a balance within 30 minutes of being placed'],
  'sc.p.a.staff_often': ['{by} 最近 30 天退到余额 {s}，比其他人多很多', '{by} refunded {s} into balances in 30 days, far more than the others'],
  'sc.p.limits': ['退到余额的限额（店主设置）', 'Limits on refunds into balances (owner)'],
  'sc.p.approve_over': ['单笔超过这个金额要经理授权', 'One refund above this needs a manager'],
  'sc.p.daily_limit': ['每人每天合计超过这个金额要店主授权', 'A person\'s total for the day above this needs the owner'],
  'sc.p.save': ['保存', 'Save'], 'sc.p.saved': ['已保存', 'Saved'],
  'sc.p.staff': ['操作人', 'By'], 'sc.p.customer': ['顾客', 'Customer'], 'sc.p.balance': ['余额', 'Balance'], 'sc.p.frozen': ['已冻结', 'Frozen'],
  'sc.k.refund': ['退款存入', 'Refund in'], 'sc.k.spend': ['下单使用', 'Spent'], 'sc.k.return': ['订单退回', 'Returned'], 'sc.k.revoke': ['改为现金', 'Paid in cash'],
  'sc.p.none': ['还没有顾客余额', 'No balances yet'],
  'sc.p.from': ['从', 'From'], 'sc.p.to': ['到', 'To'],
  // errors
  'sc.err.CREDIT_CONSENT': ['请选择顾客是怎么同意的', 'Say how the customer agreed'],
  'sc.err.CREDIT_NEEDS_APPROVAL': ['单笔超过 {amt}，要经理授权', 'Above {amt} for one refund: a manager has to approve'],
  'sc.err.CREDIT_DAILY_LIMIT': ['今天退到余额的合计超过限额，要店主授权', 'Today\'s refunds into balances are over the limit: the owner has to approve'],
  'sc.err.CREDIT_NO_CUSTOMER': ['这张单没有顾客账号，请先给顾客发短信验证码', 'This order has no customer account: text the customer a code first'],
  'sc.err.CREDIT_NONE': ['这位顾客在本店没有余额', 'This customer has no balance at this store'],
  'sc.err.CREDIT_FROZEN': ['这个余额已被平台冻结', 'This balance is frozen by the platform'],
  'sc.err.CREDIT_SPENT': ['顾客已经用掉一部分，不能再改为现金', 'The customer already spent some of it, so it cannot be changed to cash'],
  'sc.err.CREDIT_PHONE': ['请填写顾客的手机号', 'Enter the customer\'s mobile number'],
  'sc.err.CREDIT_OWED': ['还有顾客的店铺余额没结清', 'Customers still hold store balances here'],
  'sc.err.OTP_INVALID': ['验证码不对或已过期', 'The code is wrong or has expired'],
  'sc.err.OTP_WAIT': ['请稍等一分钟再发', 'Wait a minute before sending another'],
  'sc.err.OTP_DAILY_LIMIT': ['这张单今天发的验证码太多了', 'Too many codes for this order today'],
  'sc.err.SMS_FAILED': ['短信没发出去，请稍后再试', 'The text could not be sent. Try again later.'],
})
