// The balance ledger (docs/BALANCE_CHECK.md §5): every movement of every
// balance, the other side of it, and the closed days of the books. The
// platform sees everything, a store its own accounts. Texts are [zh, en].
import { get } from './api'
import { extend } from './i18n'

export interface LedgerCounter { account_kind: string; amount: number }
export interface LedgerRow {
  posting_id: number; journal_id: number; at: string; kind: string; account_id: number; account_kind: string; seq: number
  branch_id: number; store_name: string; customer_id: number; phone: string; currency: string; amount: number; balance: number
  order_id: number; public_no: string; refund_id: number; topup_id: number; cashout_id: number
  actor_type: string; by: string; approved_by: string; note: string; reverses: number; counter: LedgerCounter[]
}
export interface LedgerTotal { account_kind: string; currency: string; in: number; out: number; count: number }
export interface WalletCheck { ran_at: string; full: boolean; problems: string[]; warnings: string[]; digest: string; last_full: string | null }
export interface Ledger { rows: LedgerRow[]; totals: LedgerTotal[]; more: boolean; check?: WalletCheck | null }
export interface TrialRow { account_kind: string; currency: string; accounts: number; opening: number; credits: number; debits: number; closing: number }
export interface TrialBalance { day: string; last_day: string; rows: TrialRow[]; balanced: boolean; unclosed: boolean; timezone: string }

const qs = (q: Record<string, string | number | boolean>) => {
  const s = new URLSearchParams(Object.entries(q).filter(([, v]) => v !== '' && v !== 0 && v !== false).map(([k, v]) => [k, v === true ? '1' : String(v)])).toString()
  return s ? '?' + s : ''
}
export const ledgerURL = (branchID: number | null) => branchID ? `/merchant/branches/${branchID}/wallet-ledger` : '/admin/wallet-ledger'
export const daysURL = (branchID: number | null) => branchID ? `/merchant/branches/${branchID}/wallet-days` : '/admin/wallet-days'
export const loadLedger = (branchID: number | null, q: Record<string, string | number | boolean>) => get<Ledger>(ledgerURL(branchID) + qs(q))
export const loadDays = (branchID: number | null, q: Record<string, string | number | boolean>) => get<TrialBalance>(daysURL(branchID) + qs(q))

export const journalKinds = ['refund', 'revoke', 'spend', 'return', 'topup', 'topup_revoke', 'stored_spend', 'stored_return',
  'stored_refund', 'stored_cashout', 'reclass', 'platform_adjust']
export const accountKinds = ['store_credit', 'stored_principal', 'stored_bonus', 'store_refund', 'store_sales', 'store_topup',
  'store_bonus', 'store_stored_sales', 'store_cashout', 'store_clawback', 'store_bonus_void', 'platform_balance', 'platform_bonus', 'platform_issued']

/** A ledger page as CSV, for a spreadsheet. */
export function ledgerCSV(rows: LedgerRow[], label: (k: string) => string): string {
  const head = ['posting', 'journal', 'time', 'kind', 'account', 'store', 'customer', 'phone', 'currency', 'amount', 'balance_after',
    'order', 'refund', 'topup', 'cashout', 'by', 'approved_by', 'note', 'other_side']
  const esc = (v: string | number) => { const s = String(v); return /[",\n]/.test(s) ? '"' + s.replace(/"/g, '""') + '"' : s }
  const lines = rows.map(r => [r.posting_id, r.journal_id, r.at, label('wl.k.' + r.kind), label('wl.a.' + r.account_kind), r.store_name,
    r.customer_id || '', r.phone, r.currency, (r.amount / 100).toFixed(2), (r.balance / 100).toFixed(2), r.public_no || r.order_id || '',
    r.refund_id || '', r.topup_id || '', r.cashout_id || '', r.by, r.approved_by, r.note,
    r.counter.map(c => `${label('wl.a.' + c.account_kind)} ${(c.amount / 100).toFixed(2)}`).join('; ')].map(esc).join(','))
  return '﻿' + [head.join(','), ...lines].join('\n')
}

extend({
  'wl.title': ['余额流水', 'Balance ledger'],
  'wl.hint': ['每一笔余额变动一行：哪个账户、变了多少、变动后余额、对方科目、哪张单、谁做的。数字来自只能追加的复式账本。',
    'One row per change of a balance: which account, by how much, the balance after, the other side, the order and who did it. From the append-only double-entry books.'],
  'wl.tab': ['全部流水', 'Ledger'], 'wl.days': ['日终试算', 'Daily trial balance'],
  'wl.f.phone': ['顾客手机号（可只填尾号）', 'Customer phone (or its last digits)'], 'wl.f.order': ['订单号', 'Order no.'],
  'wl.f.kind': ['业务', 'Business'], 'wl.f.account': ['账户', 'Account'], 'wl.f.all': ['全部', 'All'],
  'wl.f.store': ['门店 ID', 'Store ID'], 'wl.f.platform': ['只看平台余额', 'Platform balance only'],
  'wl.f.from': ['从', 'From'], 'wl.f.to': ['到', 'To'], 'wl.search': ['查询', 'Search'], 'wl.more': ['更早的', 'Older'],
  'wl.csv': ['导出表格', 'Export CSV'],
  'wl.c.time': ['时间', 'Time'], 'wl.c.what': ['业务', 'Business'], 'wl.c.account': ['账户', 'Account'],
  'wl.c.amount': ['变动', 'Change'], 'wl.c.balance': ['变动后余额', 'Balance after'], 'wl.c.other': ['对方科目', 'Other side'],
  'wl.c.ref': ['单号', 'Record'], 'wl.c.by': ['操作人', 'By'], 'wl.none': ['这段时间没有余额变动', 'No balance moved in this period'],
  'wl.totals': ['按账户合计（所选范围）', 'By account (this selection)'], 'wl.t.in': ['增加', 'In'], 'wl.t.out': ['减少', 'Out'],
  'wl.ref.order': ['订单 {n}', 'Order {n}'], 'wl.ref.refund': ['退款 #{n}', 'Refund #{n}'], 'wl.ref.topup': ['充值 #{n}', 'Top-up #{n}'],
  'wl.ref.cashout': ['退储值 #{n}', 'Payback #{n}'], 'wl.ref.reverses': ['冲回凭证 #{n}', 'Reverses #{n}'],
  'wl.approved': ['授权：{n}', 'Approved by {n}'], 'wl.system': ['系统', 'System'],
  'wl.d.day': ['日期', 'Day'], 'wl.d.opening': ['期初', 'Opening'], 'wl.d.credits': ['增加', 'In'], 'wl.d.debits': ['减少', 'Out'],
  'wl.d.closing': ['期末', 'Closing'], 'wl.d.accounts': ['账户数', 'Accounts'],
  'wl.d.ok': ['平衡：每种币种所有账户期末相加为 0', 'Balanced: every currency adds up to 0'],
  'wl.d.bad': ['不平衡，请马上联系平台', 'Not balanced: tell the platform now'],
  'wl.d.none': ['还没有结过账的日子（每天过了午夜自动结账）', 'No day closed yet (each day closes after midnight)'],
  'wl.d.open': ['这一天还没结账，最近结账的是 {d}', 'This day is not closed yet; the last closed is {d}'],
  'wl.d.tz': ['按 {tz} 时间', 'In {tz} time'],
  'wl.chk.ok': ['账本核对正常：{at}（每小时核对新增的，每天全量核对一次；最近全量 {full}）', 'The books check out: {at} (new entries hourly, everything daily; last full check {full})'],
  'wl.chk.bad': ['账本核对发现 {n} 个问题（{at}），请马上处理：', 'The check of the books found {n} problems ({at}). Act now:'],
  'wl.chk.warn': ['安全提醒', 'Safety'], 'wl.chk.digest': ['账本指纹', 'Fingerprint'],
  'wl.warn.APP_ROLE': ['应用用的是数据库超级用户或账本表的主人，账本只靠触发器保护。请按部署文档改用 waimai_app 账号连接。',
    'The app connects as a superuser or the owner of the books, so only triggers protect them. Connect as waimai_app (see the deploy guide).'],
  // journal kinds
  'wl.k.refund': ['退款存入余额', 'Refund into balance'], 'wl.k.revoke': ['撤回（改退现金）', 'Revoked (cash instead)'],
  'wl.k.spend': ['用退款余额付款', 'Paid from refund balance'], 'wl.k.return': ['订单变小或取消，退回余额', 'Order went down: back to balance'],
  'wl.k.topup': ['充值', 'Top-up'], 'wl.k.topup_revoke': ['撤销充值', 'Top-up undone'],
  'wl.k.stored_spend': ['用储值付款', 'Paid from stored value'], 'wl.k.stored_return': ['订单变小或取消，退回储值', 'Order went down: back to stored value'],
  'wl.k.stored_refund': ['按商品退款回储值', 'Refund back to stored value'], 'wl.k.stored_cashout': ['退储值', 'Stored value paid back'],
  'wl.k.reclass': ['科目调整（金额不变）', 'Reclassified (no balance moved)'], 'wl.k.platform_adjust': ['平台调整', 'Platform adjustment'],
  // account kinds
  'wl.a.store_credit': ['顾客·退款余额', 'Customer · refund balance'], 'wl.a.stored_principal': ['顾客·储值本金', 'Customer · stored principal'],
  'wl.a.stored_bonus': ['顾客·赠送金', 'Customer · bonus'], 'wl.a.store_refund': ['店铺·退进余额', 'Store · refunded into balances'],
  'wl.a.store_sales': ['店铺·退款余额消费', 'Store · paid from refund balances'], 'wl.a.store_topup': ['店铺·充值收款', 'Store · top-ups taken'],
  'wl.a.store_bonus': ['店铺·送出赠送', 'Store · bonus given'], 'wl.a.store_stored_sales': ['店铺·储值消费', 'Store · paid from stored value'],
  'wl.a.store_cashout': ['店铺·退储值付现', 'Store · paid back in money'], 'wl.a.store_clawback': ['店铺·扣回已用赠送', 'Store · used bonus kept back'],
  'wl.a.store_bonus_void': ['店铺·赠送作废', 'Store · bonus made void'], 'wl.a.platform_balance': ['顾客·平台余额', 'Customer · platform balance'],
  'wl.a.platform_bonus': ['顾客·平台奖励金', 'Customer · platform rewards'], 'wl.a.platform_issued': ['平台·发放', 'Platform · issued'],
})
