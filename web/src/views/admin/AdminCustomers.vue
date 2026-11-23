<script setup lang="ts">
// The platform's side of the customer pages: answer feedback, decide
// applications to open a store, keep the wallet ledger, and say who the
// platform is and how to reach it.
import { onMounted, reactive, ref, watch } from 'vue'
import { get, post, put } from '../../api'
import { extend, money, t } from '../../i18n'
import Pager from './Pager.vue'
import { fmtDateTime, qs } from './util'

extend({
  'ac.feedback': ['意见反馈', 'Feedback'], 'ac.applications': ['入驻申请', 'Applications'], 'ac.customers': ['顾客和钱包', 'Customers & wallets'], 'ac.info': ['平台信息', 'Platform info'],
  'ac.open': ['待回复', 'Open'], 'ac.replied': ['已回复', 'Replied'], 'ac.pending': ['待审核', 'Pending'], 'ac.approved': ['已通过', 'Approved'], 'ac.rejected': ['未通过', 'Rejected'],
  'ac.customer': ['顾客', 'Customer'], 'ac.content': ['内容', 'Note'], 'ac.contact': ['联系方式', 'Contact'], 'ac.reply': ['回复', 'Reply'], 'ac.send': ['发送回复', 'Send reply'],
  'ac.reply_ph': ['回复会出现在顾客的“我的消息”里', 'The reply shows up in the customer\'s messages'],
  'ac.p.country': ['国家/地区', 'Country'], 'ac.p.province': ['省份', 'Province'], 'ac.p.area': ['地区', 'Area'],
  'ac.p.address': ['详细地址', 'Address'], 'ac.p.point': ['经纬度', 'Coordinates'], 'ac.p.map': ['在谷歌地图查看', 'Open in Google Maps'],
  'ac.p.city': ['城市（手填）', 'City (typed)'], 'ac.p.postal': ['邮编', 'Postal code'], 'ac.p.cuisine': ['经营品类', 'What they sell'],
  'ac.store': ['店铺', 'Store'], 'ac.who': ['联系人', 'Contact'], 'ac.city': ['城市 / 品类', 'City / food'], 'ac.approve': ['通过', 'Approve'], 'ac.reject': ['不通过', 'Turn down'],
  'ac.decide_ph': ['给申请人的话（不通过时必填）', 'A word to the applicant (required when turning down)'],
  'ac.approve_hint': ['通过只是通知申请人，商户账号还要到“商户”里新建。', 'Approving only tells the applicant. Create the merchant under Merchants.'],
  'ac.search': ['搜索手机号、昵称或会员号', 'Search phone, nickname or member no.'], 'ac.member': ['会员号', 'Member no.'], 'ac.nickname': ['昵称', 'Nickname'],
  'ac.orders': ['订单数', 'Orders'], 'ac.balance': ['余额', 'Balance'], 'ac.bonus': ['奖励金', 'Rewards'], 'ac.adjust': ['调整', 'Adjust'], 'ac.joined': ['注册时间', 'Joined'],
  'ac.adjust_title': ['调整 {who} 的钱包', 'Adjust the wallet of {who}'], 'ac.account': ['账户', 'Account'], 'ac.currency': ['币种', 'Currency'],
  'ac.amount': ['金额（负数为扣减）', 'Amount (negative takes away)'], 'ac.note': ['原因（顾客能看到）', 'Reason (the customer sees it)'], 'ac.save': ['保存', 'Save'],
  'ac.wallet_hint': ['这里只是记账：不会真的收款或付款，顾客目前也不能用余额或奖励金下单。', 'This is bookkeeping only: no money moves, and customers cannot pay orders from it yet.'],
  'ac.i.name': ['平台名称', 'Platform name'], 'ac.i.about': ['关于我们（中文）', 'About us (Chinese)'], 'ac.i.about_en': ['关于我们（英文）', 'About us (English)'],
  'ac.i.support_phone': ['客服电话', 'Support phone'], 'ac.i.support_email': ['客服邮箱', 'Support email'], 'ac.i.support_hours': ['服务时间', 'Support hours'],
  'ac.i.support_chat': ['在线客服（链接或账号，例如 WhatsApp、Telegram、微信号）', 'Chat (a link or a handle: WhatsApp, Telegram, WeChat…)'],
  'ac.info_hint': ['显示在顾客“我的”页面的“我的客服”和“关于我们”里。没填的项目不显示。', 'Shown under Support and About us on the customer\'s pages. Empty fields are left out.'],
  'ac.saved': ['已保存', 'Saved'], 'ac.none': ['暂无', 'Nothing here'], 'ac.go': ['搜索', 'Search'],
  'mk.suggestion': ['功能建议', 'Suggestion'], 'mk.problem': ['使用问题', 'A problem'], 'mk.complaint': ['投诉', 'Complaint'], 'mk.account': ['账号问题', 'My account'], 'mk.other': ['其他', 'Other'],
})

interface Fb { id: number; kind: string; content: string; contact: string; reply: string; replied_at: string | null; created_at: string; phone: string; member_no: string }
interface App { id: number; store_name: string; contact_name: string; phone: string; city: string; cuisine: string; note: string; status: string; reply: string; created_at: string; account_phone: string
  country_code: string; country: string; province: string; district: string; postal_code: string; place_name: string; place_address: string; unit: string; lat: number | null; lng: number | null }
interface Wallet { currency: string; balance: number; bonus: number }
interface Cust { id: number; member_no: string; phone: string; nickname: string; orders: number; created_at: string; wallets: Wallet[] }

const SIZE = 20
const tab = ref<'feedback' | 'applications' | 'customers' | 'info'>('feedback')
const error = ref('')
const fail = (e: unknown) => { error.value = (e as Error).message }

// ---- feedback ----
const fb = reactive({ items: [] as Fb[], total: 0, open: 0, status: 'open', page: 1 })
const replies = reactive<Record<number, string>>({})
async function loadFb() {
  try { Object.assign(fb, await get('/admin/feedback' + qs({ status: fb.status, page: fb.page, page_size: SIZE }))); error.value = '' } catch (e) { fail(e) }
}
async function sendReply(x: Fb) {
  try { await post(`/admin/feedback/${x.id}/reply`, { reply: replies[x.id] ?? '' }); delete replies[x.id]; await loadFb() } catch (e) { fail(e) }
}

// ---- applications ----
const ap = reactive({ items: [] as App[], total: 0, pending: 0, status: 'pending', page: 1 })
async function loadAp() {
  try { Object.assign(ap, await get('/admin/applications' + qs({ status: ap.status, page: ap.page, page_size: SIZE }))); error.value = '' } catch (e) { fail(e) }
}
// Where the store is, one line per part; an application from before the map shows the city typed by hand.
function placeRows(x: App): [string, string][] {
  if (x.lat == null || x.lng == null) return [['ac.p.city', x.city]].filter(r => r[1]) as [string, string][]
  const street = [x.place_name, x.place_address !== x.place_name ? x.place_address : '', x.unit].filter(Boolean).join(' · ')
  const rows: [string, string][] = [
    ['ac.p.country', x.country ? `${x.country}${x.country_code ? ` (${x.country_code})` : ''}` : x.country_code],
    ['ac.p.province', x.province], ['ac.p.area', [x.city, x.district].filter(Boolean).join(' · ')],
    ['ac.p.address', street],
  ]
  if (x.postal_code) rows.push(['ac.p.postal', x.postal_code])
  return rows.map(([k, v]) => [k, v || '—'])
}
const mapLink = (x: App) => `https://www.google.com/maps/search/?api=1&query=${x.lat},${x.lng}`

async function decide(x: App, status: string) {
  try { await post(`/admin/applications/${x.id}/decide`, { status, reply: replies[-x.id] ?? '' }); delete replies[-x.id]; await loadAp() } catch (e) { fail(e) }
}

// ---- customers and wallets ----
const cu = reactive({ items: [] as Cust[], total: 0, q: '', page: 1 })
const adj = reactive({ who: null as Cust | null, account: 'balance', currency: 'PHP', amount: '', note: '' })
async function loadCu() {
  try { Object.assign(cu, await get('/admin/customers' + qs({ q: cu.q, page: cu.page, page_size: SIZE }))); error.value = '' } catch (e) { fail(e) }
}
function adjust(c: Cust) { Object.assign(adj, { who: c, account: 'balance', currency: c.wallets[0]?.currency ?? 'PHP', amount: '', note: '' }) }
async function saveAdjust() {
  if (!adj.who) return
  try {
    await post(`/admin/customers/${adj.who.id}/wallet`, { account: adj.account, currency: adj.currency, amount: Math.round(Number(adj.amount) * 100), note: adj.note })
    adj.who = null
    await loadCu()
  } catch (e) { fail(e) }
}
const walletText = (c: Cust, k: 'balance' | 'bonus') => c.wallets.filter(w => w[k]).map(w => money(w[k], w.currency)).join(' / ') || '0'

// ---- platform info ----
const infoKeys = ['name', 'about', 'about_en', 'support_phone', 'support_email', 'support_hours', 'support_chat']
const info = reactive<Record<string, string>>({})
const saved = ref(false)
async function loadInfo() { try { Object.assign(info, await get('/admin/platform-info')); error.value = '' } catch (e) { fail(e) } }
async function saveInfo() {
  try { Object.assign(info, await put('/admin/platform-info', { ...info })); saved.value = true; error.value = ''; setTimeout(() => { saved.value = false }, 2000) } catch (e) { fail(e) }
}

const loaders = { feedback: loadFb, applications: loadAp, customers: loadCu, info: loadInfo }
watch(tab, v => { error.value = ''; void loaders[v]() })
onMounted(() => { void loadFb(); void loadAp() })
</script>

<template>
  <div class="adm-page">
    <div class="row wrap tabs">
      <button :class="{ on: tab === 'feedback' }" data-testid="ac-tab-feedback" @click="tab = 'feedback'">{{ t('ac.feedback') }}<span v-if="fb.open" class="badge">{{ fb.open }}</span></button>
      <button :class="{ on: tab === 'applications' }" data-testid="ac-tab-applications" @click="tab = 'applications'">{{ t('ac.applications') }}<span v-if="ap.pending" class="badge">{{ ap.pending }}</span></button>
      <button :class="{ on: tab === 'customers' }" data-testid="ac-tab-customers" @click="tab = 'customers'">{{ t('ac.customers') }}</button>
      <button :class="{ on: tab === 'info' }" data-testid="ac-tab-info" @click="tab = 'info'">{{ t('ac.info') }}</button>
    </div>
    <div v-if="error" class="banner bad" data-testid="ac-error">{{ error }}</div>

    <template v-if="tab === 'feedback'">
      <select v-model="fb.status" style="width:160px" @change="fb.page = 1; loadFb()">
        <option value="open">{{ t('ac.open') }}</option><option value="replied">{{ t('ac.replied') }}</option><option value="">{{ t('ad.all') }}</option>
      </select>
      <div v-if="!fb.items.length" class="card muted">{{ t('ac.none') }}</div>
      <div v-for="x in fb.items" :key="x.id" class="card stack" data-testid="ac-feedback">
        <div class="row wrap small muted"><span class="tag">{{ t('mk.' + x.kind) }}</span><span>{{ x.member_no }} · {{ x.phone }}</span>
          <span v-if="x.contact">{{ t('ac.contact') }}: {{ x.contact }}</span><span class="grow"></span><span>{{ fmtDateTime(x.created_at) }}</span></div>
        <p style="white-space:pre-wrap">{{ x.content }}</p>
        <div v-if="x.reply" class="reply"><b>{{ t('ac.reply') }}</b> · {{ fmtDateTime(x.replied_at) }}<br />{{ x.reply }}</div>
        <form v-else class="row top" @submit.prevent="sendReply(x)">
          <textarea v-model="replies[x.id]" rows="2" maxlength="1000" :placeholder="t('ac.reply_ph')" data-testid="ac-reply"></textarea>
          <button class="btn primary" data-testid="ac-reply-send">{{ t('ac.send') }}</button>
        </form>
      </div>
      <Pager :total="fb.total" :page="fb.page" :size="SIZE" @go="p => { fb.page = p; loadFb() }" />
    </template>

    <template v-else-if="tab === 'applications'">
      <div class="row wrap"><select v-model="ap.status" style="width:160px" @change="ap.page = 1; loadAp()">
        <option value="pending">{{ t('ac.pending') }}</option><option value="approved">{{ t('ac.approved') }}</option><option value="rejected">{{ t('ac.rejected') }}</option><option value="">{{ t('ad.all') }}</option>
      </select><span class="small muted">{{ t('ac.approve_hint') }}</span></div>
      <div v-if="!ap.items.length" class="card muted">{{ t('ac.none') }}</div>
      <div v-for="x in ap.items" :key="x.id" class="card stack" data-testid="ac-application">
        <div class="row wrap"><b>{{ x.store_name }}</b><span class="tag" :class="{ ok: x.status === 'approved', bad: x.status === 'rejected', warn: x.status === 'pending' }">{{ t('ac.' + x.status) }}</span>
          <span class="grow"></span><span class="small muted">{{ fmtDateTime(x.created_at) }}</span></div>
        <div class="small">{{ t('ac.who') }}: {{ x.contact_name }} · {{ x.phone }}<template v-if="x.cuisine"> · {{ x.cuisine }}</template>
          <span class="muted"> · {{ t('ac.customer') }} {{ x.account_phone }}</span></div>
        <dl class="ac-place" data-testid="ac-place">
          <template v-for="[k, v] in placeRows(x)" :key="k"><dt>{{ t(k) }}</dt><dd :data-testid="k.replace(/\./g, '-')">{{ v }}</dd></template>
          <template v-if="x.lat != null && x.lng != null"><dt>{{ t('ac.p.point') }}</dt>
            <dd data-testid="ac-p-point">{{ x.lat.toFixed(6) }}, {{ x.lng!.toFixed(6) }}
              <a :href="mapLink(x)" target="_blank" rel="noopener" class="small">{{ t('ac.p.map') }}</a></dd></template>
        </dl>
        <p v-if="x.note" style="white-space:pre-wrap">{{ x.note }}</p>
        <div v-if="x.status !== 'pending'" class="reply" v-show="x.reply">{{ x.reply }}</div>
        <div v-else class="row top wrap">
          <textarea v-model="replies[-x.id]" rows="2" maxlength="1000" :placeholder="t('ac.decide_ph')" class="grow" style="min-width:220px" data-testid="ac-decide-note"></textarea>
          <button class="btn ok" data-testid="ac-approve" @click="decide(x, 'approved')">{{ t('ac.approve') }}</button>
          <button class="btn danger" data-testid="ac-reject" @click="decide(x, 'rejected')">{{ t('ac.reject') }}</button>
        </div>
      </div>
      <Pager :total="ap.total" :page="ap.page" :size="SIZE" @go="p => { ap.page = p; loadAp() }" />
    </template>

    <template v-else-if="tab === 'customers'">
      <form class="row" @submit.prevent="cu.page = 1; loadCu()">
        <input v-model="cu.q" :placeholder="t('ac.search')" style="max-width:320px" data-testid="ac-search" /><button class="btn">{{ t('ac.go') }}</button>
      </form>
      <div class="small muted">{{ t('ac.wallet_hint') }}</div>
      <div class="card flush">
        <table class="data">
          <thead><tr><th>{{ t('ac.member') }}</th><th>{{ t('phone') }}</th><th>{{ t('ac.nickname') }}</th><th>{{ t('ac.orders') }}</th><th>{{ t('ac.balance') }}</th><th>{{ t('ac.bonus') }}</th><th>{{ t('ac.joined') }}</th><th></th></tr></thead>
          <tbody>
            <tr v-for="c in cu.items" :key="c.id" data-testid="ac-customer">
              <td class="mono">{{ c.member_no }}</td><td>{{ c.phone }}</td><td>{{ c.nickname || '—' }}</td><td class="num">{{ c.orders }}</td>
              <td class="num" data-testid="ac-balance">{{ walletText(c, 'balance') }}</td><td class="num">{{ walletText(c, 'bonus') }}</td><td class="num">{{ fmtDateTime(c.created_at) }}</td>
              <td><button class="btn small" data-testid="ac-adjust" @click="adjust(c)">{{ t('ac.adjust') }}</button></td>
            </tr>
            <tr v-if="!cu.items.length"><td colspan="8" class="muted">{{ t('ac.none') }}</td></tr>
          </tbody>
        </table>
      </div>
      <Pager :total="cu.total" :page="cu.page" :size="SIZE" @go="p => { cu.page = p; loadCu() }" />
      <div v-if="adj.who" class="sheet-mask" @click.self="adj.who = null">
        <form class="sheet center" data-testid="ac-adjust-form" @submit.prevent="saveAdjust">
          <div class="sheet-head"><h2>{{ t('ac.adjust_title', { who: adj.who.phone }) }}</h2></div>
          <div class="sheet-body">
            <div class="banner info" style="margin-bottom:12px">{{ t('ac.wallet_hint') }}</div>
            <div v-if="error" class="banner bad" style="margin-bottom:12px">{{ error }}</div>
            <label class="field"><span>{{ t('ac.account') }}</span><select v-model="adj.account" data-testid="ac-adjust-account"><option value="balance">{{ t('ac.balance') }}</option><option value="bonus">{{ t('ac.bonus') }}</option></select></label>
            <label class="field"><span>{{ t('ac.currency') }}</span><input v-model="adj.currency" maxlength="3" style="text-transform:uppercase" /></label>
            <label class="field"><span>{{ t('ac.amount') }}</span><input v-model="adj.amount" inputmode="decimal" data-testid="ac-adjust-amount" /></label>
            <label class="field"><span>{{ t('ac.note') }}</span><input v-model="adj.note" maxlength="200" data-testid="ac-adjust-note" /></label>
          </div>
          <div class="sheet-foot row"><button type="button" class="btn grow" @click="adj.who = null">{{ t('cancel') }}</button><button class="btn primary grow" data-testid="ac-adjust-save">{{ t('ac.save') }}</button></div>
        </form>
      </div>
    </template>

    <form v-else class="card" style="max-width:760px" data-testid="ac-info-form" @submit.prevent="saveInfo">
      <p class="small muted" style="margin-bottom:12px">{{ t('ac.info_hint') }}</p>
      <label v-for="k in infoKeys" :key="k" class="field"><span>{{ t('ac.i.' + k) }}</span>
        <textarea v-if="k.startsWith('about')" v-model="info[k]" rows="5" maxlength="4000" :data-testid="'ac-info-' + k"></textarea>
        <input v-else v-model="info[k]" maxlength="200" :data-testid="'ac-info-' + k" /></label>
      <button class="btn primary" data-testid="ac-info-save">{{ t(saved ? 'ac.saved' : 'ac.save') }}</button>
    </form>
  </div>
</template>

<style scoped>
.tabs button { border: 1px solid var(--line); background: #fff; border-radius: 999px; padding: 7px 16px; cursor: pointer; font-weight: 600; display: inline-flex; align-items: center; gap: 6px; }
.tabs button.on { background: var(--brand); border-color: var(--brand); color: #fff; }
.badge { background: #e5352b; color: #fff; border-radius: 999px; font-size: 12px; padding: 0 7px; line-height: 18px; }
.ac-place { display: grid; grid-template-columns: max-content 1fr; gap: 4px 14px; margin: 0; padding: 10px 12px; background: var(--bg); border-radius: 10px; font-size: 14px; }
.ac-place:empty { display: none; }
.ac-place dt { color: var(--muted); }
.ac-place dd { margin: 0; overflow-wrap: anywhere; }
.ac-place dd a { margin-inline-start: 8px; }
.reply { background: #f6f7f8; border-radius: 10px; padding: 10px 12px; font-size: 14px; white-space: pre-wrap; }
</style>
