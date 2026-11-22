<script setup lang="ts">
// The store's messages: every customer's conversation in one shared inbox.
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { money, t } from '../i18n'
import { useLive } from '../live'
import { session } from '../session'
import { chat, connect, day, describeChat, lastLine, listen, storeChat, storeChatRead, storeChats, type ChatThread, type StoreChat } from '../chat'
import ChatPane from './chat/ChatPane.vue'

const route = useRoute(), router = useRouter()
const bid = session.branchId!
const filter = reactive({ q: '', unread: false, active: false })
const chats = ref<ChatThread[] | null>(null), unread = ref(0), enabled = ref(true)
const error = ref(''), readOnly = ref(false)
const selectedId = computed(() => Number(route.query.c) || 0)
const current = ref<StoreChat | null>(null)
const me = computed(() => ({ uid: session.user!.id, name: session.user!.display_name || session.user!.username }))

async function load() {
  const r = await storeChats(bid, filter)
  chats.value = r.chats; unread.value = r.unread; enabled.value = r.enabled
}
const live = useLive(() => `/merchant/branches/${bid}/events`, load)
let typing = 0
watch(() => filter.q, () => { clearTimeout(typing); typing = window.setTimeout(() => void live.refreshNow(), 250) })
watch(() => [filter.unread, filter.active], () => void live.refreshNow())

async function openChat(id: number) {
  current.value = null; error.value = ''
  if (!id) return
  try { current.value = await storeChat(bid, id) } catch (e) { error.value = (e as { code?: string }).code === 'NOT_FOUND' ? t('not_found') : (e as Error).message }
}
watch(selectedId, id => void openChat(id), { immediate: true })
function pick(c: ChatThread) { void router.push({ query: { c: c.customer_id } }) }
async function seen() {
  if (!current.value || readOnly.value) return
  await storeChatRead(bid, current.value.customer_id!).catch(() => null)
  void live.refreshNow()
}
async function activity() {
  // the order beside the conversation may have moved on
  if (current.value) current.value = await storeChat(bid, current.value.customer_id!).catch(() => current.value)
  void live.refreshNow()
}

let stop = () => {}
onMounted(async () => {
  stop = listen(() => void live.refreshNow())
  try { await connect(bid) } catch (e) {
    const code = (e as { code?: string }).code
    if (code === 'READ_ONLY') readOnly.value = true
    else error.value = describeChat(code ?? 'CHAT_DOWN')
  }
})
onBeforeUnmount(() => stop())

// The list is refreshed on every event of the store, so it knows first when
// the customer has read: the chat server's own receipt can be missed.
const peerReadAt = computed(() => chats.value?.find(c => c.customer_id === selectedId.value)?.peer_read_at ?? current.value?.peer_read_at ?? null)
const name = (c: { customer?: string }) => c.customer || t('ch.guest')
const blocked = computed(() => readOnly.value ? t('admin_viewing') : '')
</script>

<template>
  <div class="msgs" :class="{ picked: !!selectedId }" data-testid="messages-page">
    <aside class="ms-list">
      <div class="ms-search"><input v-model="filter.q" type="search" :placeholder="t('ch.search')" data-testid="chat-search" /></div>
      <div class="ms-filters">
        <button :class="{ on: !filter.unread && !filter.active }" data-testid="chat-f-all" @click="filter.unread = filter.active = false">{{ t('ch.f_all') }}</button>
        <button :class="{ on: filter.unread }" data-testid="chat-f-unread" @click="filter.unread = !filter.unread">{{ t('ch.f_unread') }}<template v-if="unread"> {{ unread }}</template></button>
        <button :class="{ on: filter.active }" data-testid="chat-f-active" @click="filter.active = !filter.active">{{ t('ch.f_active') }}</button>
      </div>
      <div v-if="!enabled" class="banner warn" style="margin:10px">{{ t('ch.off') }}</div>
      <div v-if="chats && !chats.length" class="ms-empty" data-testid="chats-empty">{{ t('ch.none_desk') }}</div>
      <button v-for="c in chats ?? []" :key="c.id" class="ms-row" :class="{ on: c.customer_id === selectedId }" data-testid="chat-row" :data-customer="c.customer_id" @click="pick(c)">
        <span class="ms-ava">{{ name(c).slice(0, 1) }}</span>
        <span class="grow">
          <span class="row between"><b>{{ name(c) }} · {{ t('ch.tail', { n: c.phone_tail ?? '' }) }}</b><span class="small muted">{{ day(c.last_at) }}</span></span>
          <span class="row between"><span class="last">{{ lastLine(c, 'store') }}</span><i v-if="c.unread" class="num" data-testid="chat-row-unread">{{ c.unread > 99 ? '99+' : c.unread }}</i></span>
          <span v-if="c.order" class="tag warn">#{{ c.order.public_no }} {{ t('s.' + c.order.status) }}</span>
        </span>
      </button>
      <p class="ms-shared">{{ t('ch.shared') }}</p>
    </aside>

    <section class="ms-chat">
      <div v-if="error" class="banner bad" style="margin:12px" data-testid="chat-page-error">{{ error }}</div>
      <div v-if="!selectedId" class="ms-pick">{{ t('ch.pick') }}</div>
      <template v-else-if="current">
        <header class="ms-head">
          <button class="btn link ms-back" data-testid="chat-back" @click="router.push({ query: {} })">‹ {{ t('back') }}</button>
          <b data-testid="chat-with">{{ name(current) }} · {{ t('ch.tail', { n: current.phone_tail ?? '' }) }}</b>
          <span class="small muted">{{ t('ch.orders_here', { n: current.order_count }) }}</span>
        </header>
        <ChatPane :key="current.peer" :peer="current.peer" self="store" :branch-id="bid" :staff="me" :peer-read-at="peerReadAt"
          :blocked="blocked" desk @seen="seen" @activity="activity" />
      </template>
    </section>

    <aside v-if="current" class="ms-info" data-testid="chat-info">
      <h4>{{ t('ch.customer') }}</h4>
      <div>{{ name(current) }}</div><div class="small muted">{{ current.phone_masked }}</div>
      <template v-if="current.order">
        <h4>{{ t('ch.current_order') }}</h4>
        <div class="ms-order">
          <div class="row between"><b>#{{ current.order.public_no }}</b><span class="tag warn">{{ t('s.' + current.order.status) }}</span></div>
          <router-link class="lnk" :to="{ path: '/merchant/orders', query: { order: current.order.id } }">{{ t('ch.open_order') }}</router-link>
        </div>
      </template>
      <h4>{{ t('ch.recent_orders') }}</h4>
      <div v-if="!current.orders.length" class="small muted">{{ t('ch.no_orders') }}</div>
      <div v-for="o in current.orders" :key="o.id" class="small" data-testid="chat-order">#{{ o.public_no }} · {{ day(o.created_at) }} · {{ money(o.total, o.currency) }} · {{ t('s.' + o.status) }}</div>
    </aside>
  </div>
</template>

<style>
.msgs { display: grid; grid-template-columns: 320px minmax(0, 1fr) 240px; height: calc(100dvh - var(--shell-top, 0px)); background: #fff; }
.ms-list { border-right: 1px solid var(--line); overflow-y: auto; min-width: 0; }
.ms-search { padding: 12px 12px 6px; } .ms-search input { width: 100%; border: 0; background: #f1f3f5; border-radius: 10px; padding: 9px 12px; font: inherit; }
.ms-filters { display: flex; gap: 8px; padding: 4px 12px 10px; border-bottom: 1px solid var(--line); flex-wrap: wrap; }
.ms-filters button { border: 1px solid #d6dae0; border-radius: 16px; padding: 3px 12px; font-size: 13px; background: #fff; cursor: pointer; white-space: nowrap; }
.ms-filters button.on { border-color: var(--brand); background: var(--brand-soft); color: var(--brand-dark); font-weight: 600; }
.ms-row { display: flex; gap: 12px; padding: 12px; border: 0; border-bottom: 1px solid #f0f1f3; background: none; width: 100%; text-align: left; cursor: pointer; align-items: flex-start; color: var(--ink); font: inherit; }
.ms-row.on { background: var(--brand-soft); }
.ms-row .grow { min-width: 0; display: grid; gap: 2px; } .ms-row .tag { justify-self: start; }
.ms-row .last { color: var(--muted); font-size: 14px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; min-width: 0; }
.ms-row .num { display: inline-grid; place-items: center; min-width: 18px; height: 18px; padding: 0 5px; border-radius: 9px; background: #e5352b; color: #fff; font-size: 11px; font-style: normal; font-weight: 700; flex: none; }
.ms-ava { width: 42px; height: 42px; border-radius: 50%; background: #1971c2; color: #fff; display: grid; place-items: center; font-weight: 700; flex: none; }
.ms-empty, .ms-pick { color: var(--muted); text-align: center; padding: 48px 20px; font-size: 14px; }
.ms-shared { font-size: 12px; color: #a3a9b2; text-align: center; padding: 14px; }
.ms-chat { display: flex; flex-direction: column; min-width: 0; min-height: 0; background: #f4f4f4; }
.ms-head { background: #fff; border-bottom: 1px solid var(--line); padding: 12px 16px; display: flex; gap: 12px; align-items: baseline; flex: none; }
.ms-back { display: none; }
.ms-info { border-left: 1px solid var(--line); padding: 4px 16px 16px; overflow-y: auto; display: grid; gap: 6px; align-content: start; }
.ms-info h4 { margin: 12px 0 2px; font-size: 12px; color: var(--muted); font-weight: 600; }
.ms-order { background: #fafafa; border: 1px solid var(--line); border-radius: 12px; padding: 10px 12px; display: grid; gap: 4px; }
.ms-info .lnk { color: var(--brand); font-size: 14px; }
@media (max-width: 1199px) { .msgs { grid-template-columns: 300px minmax(0, 1fr); } .ms-info { display: none; } }
@media (max-width: 767px) {
  .msgs { grid-template-columns: minmax(0, 1fr); }
  .msgs.picked .ms-list { display: none; } .msgs:not(.picked) .ms-chat { display: none; }
  .ms-back { display: inline-flex; }
}
</style>
