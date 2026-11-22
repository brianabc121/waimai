<script setup lang="ts">
// Every conversation between a customer and a store, for the platform to read.
// Nothing can be written from here, and opening a conversation is written
// down in the audit trail.
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { get, post } from '../../api'
import { extend, money, t } from '../../i18n'
import { day } from '../../chat'
import Pager from './Pager.vue'
import { fmtDate, fmtDateTime, qs } from './util'

extend({
  'ax.search': ['搜索店名、顾客手机号、订单号、聊天内容', 'Search store, customer phone, order no. or message text'],
  'ax.all': ['全部', 'All'], 'ax.today': ['今天', 'Today'], 'ax.all_stores': ['全部门店', 'All stores'],
  'ax.none': ['还没有会话', 'No conversations yet'], 'ax.pick': ['点左边的会话查看内容', 'Choose a conversation on the left'],
  'ax.n_msgs': ['{n} 条', '{n} messages'], 'ax.read_only': ['只读', 'Read-only'],
  'ax.note': ['平台只能查看，不能代替任何一方发消息。每次打开会话都会记进“操作记录”。', 'The platform can only read. Every time a conversation is opened it is written to the audit trail.'],
  'ax.customer': ['顾客', 'Customer'], 'ax.store': ['门店', 'Store'], 'ax.merchant': ['商户：{n}', 'Merchant: {n}'], 'ax.guest': ['顾客', 'Customer'],
  'ax.about': ['这个会话', 'This conversation'], 'ax.started': ['开始于 {d}', 'Started {d}'], 'ax.count': ['共 {n} 条消息', '{n} messages'],
  'ax.orders': ['最近订单', 'Recent orders'], 'ax.actions': ['处理', 'Actions'],
  'ax.mute': ['禁言这位顾客', 'Stop this customer from writing'], 'ax.unmute': ['解除禁言', 'Let this customer write again'], 'ax.muted': ['已禁言', 'Muted'],
  'ax.close_store': ['关闭这家店的聊天功能', 'Close this store\'s chat'], 'ax.open_store': ['重新开通这家店的聊天', 'Reopen this store\'s chat'], 'ax.store_off': ['聊天已关闭', 'Chat closed'],
  'ax.mute_confirm': ['禁言后，这位顾客在所有店铺都不能发消息。确定吗？', 'This customer will not be able to write to any store. Continue?'],
  'ax.close_confirm': ['关闭后，顾客和这家店都不能再互相发消息。确定吗？', 'Customers and this store will not be able to message each other. Continue?'],
  'ax.from_customer': ['顾客', 'Customer'], 'ax.from_store': ['门店', 'Store'], 'ax.earlier': ['查看更早的消息', 'Earlier messages'],
  'ax.image': ['[图片]', '[Picture]'], 'ax.order': ['[订单]', '[Order]'], 'ax.you': ['', ''],
})

interface Tag { id: number; public_no: string; status: string }
interface Row {
  id: number; branch_id: number; slug: string; store_name: string; merchant: string; customer_id: number; customer: string; phone: string; phone_tail: string
  last_kind: string; last_text: string; last_sender: string; last_at: string; count: number; order: Tag | null; muted: boolean; chat_enabled: boolean
}
interface Line { id: number; sender: 'customer' | 'store' | 'system'; staff?: string; kind: string; body: string; sent_at: string }
interface Order { id: number; public_no: string; status: string; total: number; currency: string; created_at: string }
interface Detail extends Row { started_at: string; messages: Line[]; orders: Order[] }

const SIZE = 30
const route = useRoute(), router = useRouter()
const ask = reactive({ q: '', today: false, branch_id: 0, page: 1 })
const list = ref<{ chats: Row[]; total: number; today: number } | null>(null)
const stores = ref<{ id: number; name: string }[]>([])
const error = ref('')
const selectedId = computed(() => Number(route.query.id) || 0)
const detail = ref<Detail | null>(null), more = ref(false)

async function load() {
  try { list.value = await get('/admin/chats' + qs({ q: ask.q, today: ask.today ? 1 : '', branch_id: ask.branch_id || '', page: ask.page, page_size: SIZE })) }
  catch (e) { error.value = (e as Error).message }
}
let typing = 0
watch(() => ask.q, () => { clearTimeout(typing); typing = window.setTimeout(() => { ask.page = 1; void load() }, 300) })
watch(() => [ask.today, ask.branch_id], () => { ask.page = 1; void load() })

async function open(id: number) {
  detail.value = null
  if (!id) return
  try {
    const d = await get<Detail>(`/admin/chats/${id}`)
    detail.value = d; more.value = d.messages.length < d.count
  } catch (e) { error.value = (e as Error).message }
}
watch(selectedId, id => void open(id), { immediate: true })
async function earlier() {
  const d = detail.value
  if (!d || !d.messages.length) return
  const older = await get<Detail>(`/admin/chats/${d.id}?before=${d.messages[0].id}`)
  d.messages = [...older.messages, ...d.messages]
  more.value = older.messages.length > 0 && d.messages.length < d.count
}
async function mute() {
  const d = detail.value!
  if (!d.muted && !confirm(t('ax.mute_confirm'))) return
  await post(`/admin/chat-customers/${d.customer_id}/mute`, { muted: !d.muted }).then(() => { d.muted = !d.muted }, e => { error.value = e.message })
}
async function toggleStore() {
  const d = detail.value!
  if (d.chat_enabled && !confirm(t('ax.close_confirm'))) return
  await post(`/admin/chat-stores/${d.branch_id}`, { enabled: !d.chat_enabled }).then(() => { d.chat_enabled = !d.chat_enabled }, e => { error.value = e.message })
}

onMounted(async () => {
  void load()
  stores.value = await get<{ branches: { id: number; name: string }[] }>('/admin/branches').then(r => r.branches, () => [])
})
const who = (r: { customer: string }) => r.customer || t('ax.guest')
const last = (r: Row) => (r.last_sender === 'store' ? t('ax.from_store') : t('ax.from_customer')) + '：' + (r.last_kind === 'image' ? t('ax.image') : r.last_kind === 'order' ? t('ax.order') : r.last_text)
</script>

<template>
  <div class="axc" :class="{ picked: !!selectedId }" data-testid="admin-chats">
    <aside class="ax-list">
      <div class="ax-search"><input v-model="ask.q" type="search" :placeholder="t('ax.search')" data-testid="ax-search" /></div>
      <div class="ax-filters">
        <button :class="{ on: !ask.today }" @click="ask.today = false">{{ t('ax.all') }}<template v-if="list"> {{ list.total }}</template></button>
        <button :class="{ on: ask.today }" data-testid="ax-today" @click="ask.today = !ask.today">{{ t('ax.today') }}<template v-if="list"> {{ list.today }}</template></button>
        <select v-model.number="ask.branch_id" data-testid="ax-store">
          <option :value="0">{{ t('ax.all_stores') }}</option>
          <option v-for="s in stores" :key="s.id" :value="s.id">{{ s.name }}</option>
        </select>
      </div>
      <div v-if="error" class="banner bad" style="margin:10px">{{ error }}</div>
      <div v-if="list && !list.chats.length" class="ax-empty" data-testid="ax-empty">{{ t('ax.none') }}</div>
      <button v-for="r in list?.chats ?? []" :key="r.id" class="ax-row" :class="{ on: r.id === selectedId }" data-testid="ax-row" :data-id="r.id" @click="router.push({ query: { id: r.id } })">
        <span class="ax-ava">{{ r.store_name.slice(0, 1) }}</span>
        <span class="grow">
          <span class="row between"><b>{{ r.store_name }} ↔ {{ who(r) }} {{ r.phone_tail }}</b><span class="small muted">{{ day(r.last_at) }}</span></span>
          <span class="last">{{ last(r) }}</span>
          <span class="row" style="gap:6px">
            <span v-if="r.order" class="tag warn">#{{ r.order.public_no }} {{ t('s.' + r.order.status) }}</span>
            <span class="tag">{{ t('ax.n_msgs', { n: r.count }) }}</span>
            <span v-if="r.muted" class="tag bad">{{ t('ax.muted') }}</span><span v-if="!r.chat_enabled" class="tag bad">{{ t('ax.store_off') }}</span>
          </span>
        </span>
      </button>
      <Pager v-if="list && list.total > SIZE" style="padding:10px 12px" :total="list.total" :page="ask.page" :size="SIZE" @go="p => { ask.page = p; load() }" />
    </aside>

    <section class="ax-chat">
      <div v-if="!selectedId" class="ax-empty">{{ t('ax.pick') }}</div>
      <template v-else-if="detail">
        <header class="ax-head">
          <button class="btn link ax-back" @click="router.push({ query: {} })">‹ {{ t('back') }}</button>
          <b data-testid="ax-title">{{ detail.store_name }} ↔ {{ who(detail) }} {{ detail.phone_tail }}</b><span class="tag">{{ t('ax.read_only') }}</span>
        </header>
        <div class="ax-msgs" data-testid="ax-messages">
          <button v-if="more" class="btn link small" style="align-self:center" data-testid="ax-earlier" @click="earlier">{{ t('ax.earlier') }}</button>
          <div v-for="m in detail.messages" :key="m.id" class="ax-msg" :class="m.sender" data-testid="ax-msg" :data-sender="m.sender">
            <div class="bub"><span v-if="m.kind === 'text' || m.kind === 'system'">{{ m.body }}</span><span v-else class="muted">{{ m.kind === 'image' ? t('ax.image') : t('ax.order') }}</span>
              <em>{{ m.sender === 'store' ? t('ax.from_store') + (m.staff ? ' · ' + m.staff : '') : t('ax.from_customer') }} · {{ fmtDateTime(m.sent_at) }}</em></div>
          </div>
        </div>
        <div class="ax-note">{{ t('ax.note') }}</div>
      </template>
    </section>

    <aside v-if="detail" class="ax-info" data-testid="ax-info">
      <h4>{{ t('ax.store') }}</h4>
      <div>{{ detail.store_name }}</div><div class="small muted">{{ t('ax.merchant', { n: detail.merchant }) }}</div>
      <a class="lnk" :href="`/s/${detail.slug}`" target="_blank" rel="noopener">/s/{{ detail.slug }}</a>
      <h4>{{ t('ax.customer') }}</h4>
      <div>{{ who(detail) }}</div><div class="small muted">{{ detail.phone }}</div>
      <h4>{{ t('ax.about') }}</h4>
      <div class="small">{{ t('ax.started', { d: fmtDate(detail.started_at) }) }}</div>
      <div class="small">{{ t('ax.count', { n: detail.count }) }}</div>
      <h4>{{ t('ax.orders') }}</h4>
      <div v-for="o in detail.orders" :key="o.id" class="small">#{{ o.public_no }} · {{ fmtDate(o.created_at) }} · {{ money(o.total, o.currency) }} · {{ t('s.' + o.status) }}</div>
      <div v-if="!detail.orders.length" class="small muted">—</div>
      <h4>{{ t('ax.actions') }}</h4>
      <button class="btn link bad" data-testid="ax-mute" :data-on="detail.muted || null" @click="mute">{{ t(detail.muted ? 'ax.unmute' : 'ax.mute') }}</button>
      <button class="btn link bad" data-testid="ax-store-toggle" @click="toggleStore">{{ t(detail.chat_enabled ? 'ax.close_store' : 'ax.open_store') }}</button>
    </aside>
  </div>
</template>

<style>
.axc { display: grid; grid-template-columns: 380px minmax(0, 1fr) 250px; height: calc(100dvh - 52px); background: #fff; }
.ax-list { border-right: 1px solid var(--line); overflow-y: auto; min-width: 0; }
.ax-search { padding: 12px 12px 6px; } .ax-search input { width: 100%; border: 0; background: #f1f3f5; border-radius: 10px; padding: 9px 12px; font: inherit; }
.ax-filters { display: flex; gap: 8px; padding: 4px 12px 10px; border-bottom: 1px solid var(--line); flex-wrap: wrap; align-items: center; }
.ax-filters button, .ax-filters select { border: 1px solid #d6dae0; border-radius: 16px; padding: 3px 12px; font-size: 13px; background: #fff; cursor: pointer; white-space: nowrap; max-width: 150px; }
.ax-filters button.on { border-color: var(--brand); background: var(--brand-soft); color: var(--brand-dark); font-weight: 600; }
.ax-row { display: flex; gap: 12px; padding: 12px; border: 0; border-bottom: 1px solid #f0f1f3; background: none; width: 100%; text-align: left; cursor: pointer; align-items: flex-start; color: var(--ink); font: inherit; }
.ax-row.on { background: var(--brand-soft); }
.ax-row .grow { min-width: 0; display: grid; gap: 3px; }
.ax-row .last { color: var(--muted); font-size: 14px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.ax-ava { width: 42px; height: 42px; border-radius: 12px; background: var(--brand); color: #fff; display: grid; place-items: center; font-weight: 700; flex: none; }
.ax-empty { color: var(--muted); text-align: center; padding: 48px 20px; font-size: 14px; }
.ax-chat { display: flex; flex-direction: column; min-width: 0; min-height: 0; background: #f4f4f4; }
.ax-head { background: #fff; border-bottom: 1px solid var(--line); padding: 12px 16px; display: flex; gap: 10px; align-items: center; flex: none; }
.ax-back { display: none; }
.ax-msgs { flex: 1; min-height: 0; overflow-y: auto; padding: 16px 20px; display: flex; flex-direction: column; gap: 10px; }
.ax-msg { display: flex; max-width: 66%; } .ax-msg.store { align-self: flex-end; }
.ax-msg .bub { background: #fff; border-radius: 14px 14px 14px 4px; padding: 8px 12px; white-space: pre-wrap; overflow-wrap: anywhere; }
.ax-msg.store .bub { background: var(--brand); color: #fff; border-radius: 14px 14px 4px 14px; }
.ax-msg em { display: block; font-style: normal; font-size: 11px; opacity: .75; margin-top: 3px; }
.ax-note { background: #fff8e1; color: #9a6700; font-size: 13px; padding: 10px 16px; border-top: 1px solid var(--line); flex: none; }
.ax-info { border-left: 1px solid var(--line); padding: 4px 16px 16px; overflow-y: auto; display: grid; gap: 6px; align-content: start; }
.ax-info h4 { margin: 12px 0 2px; font-size: 12px; color: var(--muted); font-weight: 600; }
.ax-info .lnk { color: var(--brand); font-size: 13px; } .ax-info .btn.bad { color: var(--bad); justify-self: start; padding: 0; min-height: 28px; }
.tag.bad { background: var(--bad-soft, #fdecea); color: var(--bad, #b42318); }
@media (max-width: 1199px) { .axc { grid-template-columns: 320px minmax(0, 1fr); } .ax-info { display: none; } }
@media (max-width: 767px) {
  .axc { grid-template-columns: minmax(0, 1fr); }
  .axc.picked .ax-list { display: none; } .axc:not(.picked) .ax-chat { display: none; }
  .ax-back { display: inline-flex; }
}
</style>
