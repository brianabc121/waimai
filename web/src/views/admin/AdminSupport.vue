<script setup lang="ts">
// Platform support: customers and stores write to the platform, and every
// administrator answers from this one shared inbox (the chat server's p_1).
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { extend, money, t } from '../../i18n'
import { session } from '../../session'
import { adminSupport, adminSupportChat, adminSupportRead, adminSupportResolve, connect, day, describeChat, listen, type SupportDetail, type SupportRow } from '../../chat'
import ChatPane from '../chat/ChatPane.vue'
import Pager from './Pager.vue'
import { fmtDate, fmtDateTime } from './util'
import { monitor } from './monitor'
import '../../brands'
import { latest } from '../../retry'

extend({
  'as.search': ['搜索顾客、门店、商户、手机号、消息内容', 'Search customer, store, merchant, phone or message text'],
  'as.all': ['全部', 'All'], 'as.unread': ['未读', 'Unread'], 'as.open': ['待处理', 'Open'],
  'as.customers': ['顾客', 'Customers'], 'as.stores': ['商家', 'Stores'],
  'as.none': ['还没有人联系平台客服', 'Nobody has contacted support yet'], 'as.pick': ['点左边的会话开始回复', 'Choose a conversation on the left to answer'],
  'as.customer': ['顾客', 'Customer'], 'as.store': ['商家', 'Store'], 'as.guest': ['顾客', 'Customer'],
  'as.resolved': ['已解决', 'Resolved'], 'as.resolve': ['标记为已解决', 'Mark as resolved'], 'as.reopen': ['重新打开', 'Reopen'],
  'as.resolved_by': ['{who} 于 {d} 标记已解决', 'Resolved by {who} on {d}'], 'as.resolve_hint': ['对方再发消息会自动回到“待处理”。', 'A new message from them opens it again.'],
  'as.you': ['客服：', 'Support: '], 'as.image': ['[图片]', '[Picture]'], 'as.order': ['[订单]', '[Order]'],
  'as.shared': ['所有平台管理员共用这个收件箱。对方只看到“平台客服”，看不到是哪位管理员回复的。', 'All administrators share this inbox. The other side sees "Platform support", not who answered.'],
  'as.about': ['这个会话', 'This conversation'], 'as.started': ['开始于 {d}', 'Started {d}'], 'as.not_started': ['还没有消息，可以先发一条', 'No messages yet; you can write first'],
  'as.count': ['共 {n} 条消息', '{n} messages'], 'as.orders': ['最近订单', 'Recent orders'], 'as.since': ['注册于 {d}', 'Joined {d}'],
  'as.muted': ['已被禁止给商家发消息', 'Cannot message stores (muted)'], 'as.store_off': ['这家店的聊天已关闭', 'This store\'s chat is closed'],
  'as.merchant': ['商户：{n}', 'Merchant: {n}'], 'as.to_merchant': ['查看商户 ›', 'Open merchant ›'], 'as.to_store': ['打开店铺页 ›', 'Open store page ›'],
  'as.off': ['聊天服务没有开通，暂时不能回复（见部署教程第 19 节）', 'Chat is not set up, so answers cannot be sent (see the deployment guide, section 19)'],
})

const SIZE = 30
const route = useRoute(), router = useRouter()
const ask = reactive({ q: '', who: '', unread: false, open: false, page: 1 })
const list = ref<{ chats: SupportRow[]; total: number; unread: number; open: number } | null>(null), enabled = ref(true)
const error = ref(''), loaded = ref<SupportDetail | null>(null)
const party = computed(() => /^[cb]_\d+$/.test(String(route.query.party ?? '')) ? String(route.query.party) : '')
// What is on screen is the conversation picked in the list, or nothing: a
// late answer for the one picked before is never shown, replied to or closed.
const detail = computed(() => loaded.value?.party === party.value ? loaded.value : null)
const turn = latest()
const me = computed(() => ({ uid: session.user!.id, name: session.user!.display_name || session.user!.username }))

async function load() {
  try {
    const r = await adminSupport({ ...ask, page_size: SIZE })
    list.value = r.list; enabled.value = r.enabled
    // keep the "read" mark of the open conversation current
    const row = r.list.chats.find(c => c.party === party.value)
    if (row && loaded.value?.party === row.party) loaded.value.peer_read_at = row.peer_read_at
  } catch (e) { error.value = (e as Error).message }
}
let typing = 0
watch(() => ask.q, () => { clearTimeout(typing); typing = window.setTimeout(() => { ask.page = 1; void load() }, 300) })
watch(() => [ask.who, ask.unread, ask.open], () => { ask.page = 1; void load() })

async function open(p: string) {
  const mine = turn.next()
  loaded.value = null
  if (!p) return
  try {
    const d = await adminSupportChat(p)
    if (mine()) loaded.value = d
  } catch (e) { if (mine()) error.value = (e as { code?: string }).code === 'NOT_FOUND' ? t('not_found') : (e as Error).message }
}
watch(party, p => void open(p), { immediate: true })

async function seen() {
  if (!party.value) return
  await adminSupportRead(party.value).catch(() => null)
  monitor.supportUnread = Math.max(0, monitor.supportUnread - (list.value?.chats.find(c => c.party === party.value)?.unread ?? 0))
  void load()
}
async function activity() {
  const d = detail.value
  if (d) {
    const mine = turn.next()
    const fresh = await adminSupportChat(d.party).catch(() => null)
    if (fresh && mine()) loaded.value = fresh
  }
  void load()
}
async function resolve() {
  const d = detail.value
  if (!d) return
  await adminSupportResolve(d.party, !d.resolved_at).then(() => (party.value === d.party ? open(d.party) : Promise.resolve()).then(load), e => { error.value = e.message })
}

let stop = () => {}, timer = 0
onMounted(async () => {
  void load()
  // a message from anyone moves their conversation up
  stop = listen(() => void load())
  timer = window.setInterval(() => { if (document.visibilityState === 'visible') void load() }, 20000)
  try { await connect('platform') } catch (e) { error.value = describeChat((e as { code?: string }).code ?? 'CHAT_DOWN') }
})
onBeforeUnmount(() => { stop(); clearInterval(timer) })

const name = (r: { name: string; kind: string }) => r.name || t('as.guest')
const last = (r: SupportRow) => (r.last_sender === 'platform' ? t('as.you') : '') +
  (r.last_kind === 'image' ? t('as.image') : r.last_kind === 'order' ? t('as.order') : r.last_text)
const blocked = computed(() => enabled.value ? '' : t('as.off'))
</script>

<template>
  <div class="asp" :class="{ picked: !!party }" data-testid="admin-support">
    <aside class="asp-list">
      <div class="asp-search"><input v-model="ask.q" type="search" :placeholder="t('as.search')" data-testid="as-search" /></div>
      <div class="asp-filters">
        <button :class="{ on: !ask.unread && !ask.open }" @click="ask.unread = ask.open = false">{{ t('as.all') }}<template v-if="list"> {{ list.total }}</template></button>
        <button :class="{ on: ask.unread }" data-testid="as-unread" @click="ask.unread = !ask.unread">{{ t('as.unread') }}<template v-if="list?.unread"> {{ list.unread }}</template></button>
        <button :class="{ on: ask.open }" data-testid="as-open" @click="ask.open = !ask.open">{{ t('as.open') }}<template v-if="list?.open"> {{ list.open }}</template></button>
        <select v-model="ask.who" data-testid="as-who">
          <option value="">{{ t('as.customers') }} + {{ t('as.stores') }}</option>
          <option value="customer">{{ t('as.customers') }}</option>
          <option value="store">{{ t('as.stores') }}</option>
        </select>
      </div>
      <div v-if="error" class="banner bad" style="margin:10px" data-testid="as-error">{{ error }}</div>
      <div v-if="!enabled" class="banner warn" style="margin:10px">{{ t('as.off') }}</div>
      <div v-if="list && !list.chats.length" class="asp-empty" data-testid="as-empty">{{ t('as.none') }}</div>
      <button v-for="r in list?.chats ?? []" :key="r.id" class="asp-row" :class="{ on: r.party === party }" data-testid="as-row" :data-party="r.party"
        @click="router.push({ query: { party: r.party } })">
        <span class="asp-ava" :class="r.kind">{{ name(r).slice(0, 1) }}</span>
        <span class="grow">
          <span class="row between"><b>{{ name(r) }}<template v-if="r.kind === 'customer' && r.phone_tail"> · {{ r.phone_tail }}</template></b><span class="small muted">{{ day(r.last_at) }}</span></span>
          <span class="row between"><span class="last">{{ last(r) }}</span><i v-if="r.unread" class="num" data-testid="as-row-unread">{{ r.unread > 99 ? '99+' : r.unread }}</i></span>
          <span class="row" style="gap:6px">
            <span class="tag" :class="r.kind === 'store' ? 'info' : ''">{{ t(r.kind === 'store' ? 'as.store' : 'as.customer') }}</span>
            <span v-if="r.brand_name" class="asp-brand" data-testid="as-row-brand" :style="{ background: r.brand_color || '#a02c20' }">{{ r.brand_name }}</span>
            <span v-if="r.kind === 'customer' && r.store_name && r.store_name !== r.brand_name" class="small muted">{{ r.store_name }}</span>
            <span v-if="r.merchant" class="small muted">{{ r.merchant }}</span>
            <span v-if="r.resolved_at" class="tag ok">{{ t('as.resolved') }}</span>
          </span>
        </span>
      </button>
      <Pager v-if="list && list.total > SIZE" style="padding:10px 12px" :total="list.total" :page="ask.page" :size="SIZE" @go="p => { ask.page = p; load() }" />
      <p class="asp-shared">{{ t('as.shared') }}</p>
    </aside>

    <section class="asp-chat">
      <div v-if="!party" class="asp-empty">{{ t('as.pick') }}</div>
      <template v-else-if="detail">
        <header class="asp-head">
          <button class="btn link asp-back" @click="router.push({ query: {} })">‹ {{ t('back') }}</button>
          <b data-testid="as-title">{{ name(detail) }}</b>
          <span class="tag" :class="detail.kind === 'store' ? 'info' : ''">{{ t(detail.kind === 'store' ? 'as.store' : 'as.customer') }}</span>
          <span v-if="detail.brand_name" class="asp-brand" data-testid="as-brand" :style="{ background: detail.brand_color || '#a02c20' }">{{ detail.brand_name }}</span>
          <span v-if="detail.resolved_at" class="tag ok">{{ t('as.resolved') }}</span>
          <button class="btn small asp-resolve" data-testid="as-resolve" :disabled="!detail.id" @click="resolve">{{ t(detail.resolved_at ? 'as.reopen' : 'as.resolve') }}</button>
        </header>
        <ChatPane :key="detail.party" :peer="detail.party" self="platform" branch-id="platform" :staff="me" :peer-read-at="detail.peer_read_at"
          :blocked="blocked" desk :upload-path="`/admin/support/${detail.party}/images`" :orders="detail.orders" :empty-text="t('as.not_started')"
          :brands="detail.brands" :brand-note="t('as.about_brand')"
          @seen="seen" @activity="activity" />
      </template>
    </section>

    <aside v-if="detail" class="asp-info" data-testid="as-info">
      <h4>{{ t(detail.kind === 'store' ? 'as.store' : 'as.customer') }}</h4>
      <div>{{ name(detail) }}</div>
      <div class="small muted" data-testid="as-phone">{{ detail.phone || '—' }}</div>
      <template v-if="detail.kind === 'store'">
        <div class="small muted">{{ t('as.merchant', { n: detail.merchant ?? '' }) }}</div>
        <router-link v-if="detail.merchant_id" class="lnk" :to="`/admin/merchants/${detail.merchant_id}`">{{ t('as.to_merchant') }}</router-link>
        <a v-if="detail.slug" class="lnk" :href="`/s/${detail.slug}`" target="_blank" rel="noopener">{{ t('as.to_store') }}</a>
        <div v-if="!detail.chat_enabled" class="small bad">{{ t('as.store_off') }}</div>
        <template v-if="detail.brands.length > 1">
          <h4>{{ t('as.brands') }}</h4>
          <div class="asp-brands" data-testid="as-brands"><span v-for="b in detail.brands" :key="b.id" class="asp-brand" :style="{ background: b.color || '#a02c20' }">{{ b.name }}</span></div>
        </template>
      </template>
      <template v-else>
        <div v-if="detail.muted" class="small bad">{{ t('as.muted') }}</div>
      </template>
      <div class="small muted">{{ t('as.since', { d: fmtDate(detail.since) }) }}</div>
      <h4>{{ t('as.about') }}</h4>
      <div v-if="detail.started_at" class="small">{{ t('as.started', { d: fmtDate(detail.started_at) }) }} · {{ t('as.count', { n: detail.count }) }}</div>
      <div v-else class="small muted">{{ t('as.not_started') }}</div>
      <div v-if="detail.resolved_at" class="small">{{ t('as.resolved_by', { who: detail.resolved_by || '—', d: fmtDateTime(detail.resolved_at) }) }}</div>
      <div class="small muted">{{ t('as.resolve_hint') }}</div>
      <h4>{{ t('as.orders') }}</h4>
      <div v-for="o in detail.orders" :key="o.id" class="small" data-testid="as-order">#{{ o.public_no }}<template v-if="detail.kind === 'customer'"> · {{ o.store_name }}</template><template v-if="o.brand_name && (detail.kind === 'customer' ? o.brand_name !== o.store_name : detail.brands.length > 1)"> · <span class="asp-brand" :style="{ background: o.brand_color || '#a02c20' }">{{ o.brand_name }}</span></template> · {{ fmtDate(o.created_at) }} · {{ money(o.total, o.currency) }} · {{ t('s.' + o.status) }}</div>
      <div v-if="!detail.orders.length" class="small muted">—</div>
    </aside>
  </div>
</template>

<style>
.asp { display: grid; grid-template-columns: 360px minmax(0, 1fr) 260px; height: calc(100dvh - 52px); background: #fff; }
.asp-list { border-inline-end: 1px solid var(--line); overflow-y: auto; min-width: 0; }
.asp-search { padding: 12px 12px 6px; } .asp-search input { width: 100%; border: 0; background: #f1f3f5; border-radius: 10px; padding: 9px 12px; font: inherit; }
.asp-filters { display: flex; gap: 8px; padding: 4px 12px 10px; border-bottom: 1px solid var(--line); flex-wrap: wrap; align-items: center; }
.asp-filters button, .asp-filters select { border: 1px solid #d6dae0; border-radius: 16px; padding: 3px 12px; font-size: 13px; background: #fff; cursor: pointer; white-space: nowrap; }
.asp-filters button.on { border-color: var(--brand); background: var(--brand-soft); color: var(--brand-dark); font-weight: 600; }
.asp-row { display: flex; gap: 12px; padding: 12px; border: 0; border-bottom: 1px solid #f0f1f3; background: none; width: 100%; text-align: start; cursor: pointer; align-items: flex-start; color: var(--ink); font: inherit; }
.asp-row.on { background: var(--brand-soft); }
.asp-row .grow { min-width: 0; display: grid; gap: 3px; }
.asp-row .grow > .row { min-width: 0; }
.asp-row .last { flex: 1; color: var(--muted); font-size: 14px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; min-width: 0; }
.asp-row .num { display: inline-grid; place-items: center; min-width: 18px; height: 18px; padding: 0 5px; border-radius: 9px; background: #e5352b; color: #fff; font-size: 11px; font-style: normal; font-weight: 700; flex: none; }
.asp-ava { width: 42px; height: 42px; border-radius: 50%; background: #1971c2; color: #fff; display: grid; place-items: center; font-weight: 700; flex: none; }
.asp-ava.store { border-radius: 12px; background: var(--brand); }
.asp-empty { color: var(--muted); text-align: center; padding: 48px 20px; font-size: 14px; }
.asp-shared { font-size: 12px; color: #a3a9b2; text-align: center; padding: 14px; }
.asp-chat { display: flex; flex-direction: column; min-width: 0; min-height: 0; background: #f4f4f4; }
.asp-head { background: #fff; border-bottom: 1px solid var(--line); padding: 10px 16px; display: flex; gap: 10px; align-items: center; flex: none; flex-wrap: wrap; }
.asp-resolve { margin-inline-start: auto; }
.asp-back { display: none; }
.asp-info { border-inline-start: 1px solid var(--line); padding: 4px 16px 16px; overflow-y: auto; display: grid; gap: 6px; align-content: start; }
.asp-info h4 { margin: 12px 0 2px; font-size: 12px; color: var(--muted); font-weight: 600; }
.asp-brand { display: inline-flex; align-items: center; color: #fff; border-radius: 5px; padding: 0 6px; font-size: 11.5px; font-weight: 700; line-height: 18px; white-space: nowrap; }
.asp-brands { display: flex; flex-wrap: wrap; gap: 6px; }
.asp-info .lnk { color: var(--brand); font-size: 13px; } .asp-info .bad { color: var(--bad, #b42318); }
@media (max-width: 1199px) { .asp { grid-template-columns: 320px minmax(0, 1fr); } .asp-info { display: none; } }
@media (max-width: 767px) {
  .asp { grid-template-columns: minmax(0, 1fr); }
  .asp.picked .asp-list { display: none; } .asp:not(.picked) .asp-chat { display: none; }
  .asp-back { display: inline-flex; }
}
</style>
