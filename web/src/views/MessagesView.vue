<script setup lang="ts">
// The store's messages: every customer's conversation in one shared inbox.
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { money, t } from '../i18n'
import { useLive } from '../live'
import { can, session } from '../session'
import { addQuickReply, chat, chatCustomerOfOrder, chatSetup, connect, day, deleteQuickReply, describeChat, lastLine, listen, setAutoReply, storeChat, storeChatBlock, storeChatRead, storeChats, type ChatOrder, type ChatSetup, type ChatThread, type StoreChat } from '../chat'
import { chatPush, disableChatPush, enableChatPush, setupChatPush } from '../webpush'
import '../officeStrings'
import ChatPane from './chat/ChatPane.vue'
import ChatReport from './chat/ChatReport.vue'

const route = useRoute(), router = useRouter()
const bid = session.branchId!
const filter = reactive({ q: '', unread: false, active: false })
const chats = ref<ChatThread[] | null>(null), unread = ref(0), enabled = ref(true)
const error = ref(''), readOnly = ref(false), note = ref(''), reporting = ref(false)
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
  current.value = null; error.value = ''; note.value = ''
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

async function block() {
  const c = current.value
  if (!c || (!c.blocked && !confirm(t('ch.block_confirm')))) return
  await storeChatBlock(bid, c.customer_id!, !c.blocked).then(() => { c.blocked = !c.blocked }, e => { error.value = e.message })
}

// ---- quick replies and the automatic answer ----
const pane = ref<{ put: (text: string) => void } | null>(null)
const setup = ref<ChatSetup | null>(null), managing = ref(false), newQuick = ref(''), setupError = ref('')
const auto = reactive({ on: true, text: '' })
const manager = computed(() => can('chat.settings'))
async function loadSetup() {
  try { setup.value = await chatSetup(bid); auto.on = setup.value.auto_reply_on; auto.text = setup.value.auto_reply } catch { /* the page works without them */ }
}
const quickDefaults = () => [t('ch.q1'), t('ch.q2'), t('ch.q3'), t('ch.q4')]
async function addQuick(text: string) {
  setupError.value = ''
  try { setup.value!.quick_replies.push(await addQuickReply(bid, text)); newQuick.value = '' } catch (e) { setupError.value = (e as Error).message }
}
async function addDefaults() { for (const q of quickDefaults()) await addQuick(q) }
async function removeQuick(id: number) {
  setupError.value = ''
  try { await deleteQuickReply(bid, id); setup.value!.quick_replies = setup.value!.quick_replies.filter(q => q.id !== id) } catch (e) { setupError.value = (e as Error).message }
}
async function saveAuto() {
  setupError.value = ''
  try { await setAutoReply(bid, auto.on, auto.text); managing.value = false } catch (e) { setupError.value = (e as Error).message }
}
function openOrder(o: ChatOrder) { void router.push({ path: '/merchant/orders', query: { order: o.id } }) }

// "message the customer" on the order desk leads here with ?order=<id>
async function fromOrder(orderId: number) {
  try {
    const r = await chatCustomerOfOrder(bid, orderId)
    await router.replace({ query: { c: r.customer_id } })
  } catch (e) {
    const code = (e as { code?: string }).code ?? ''
    error.value = code === 'CHAT_NO_ACCOUNT' ? describeChat(code) : code === 'NOT_FOUND' ? t('not_found') : (e as Error).message
    await router.replace({ query: {} })
  }
}

let stop = () => {}
onMounted(async () => {
  void loadSetup()
  void setupChatPush('staff')
  if (Number(route.query.order)) void fromOrder(Number(route.query.order))
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
  <div class="ox warm">
  <div class="page ox-page msg-page" :class="{ picked: !!selectedId }">
    <div class="ox-head ms-pagehead">
      <div><h1>{{ t('ch.nav') }}</h1><p class="ox-sub">{{ t('ox.ch.sub') }}</p></div>
      <div class="ox-acts"><span v-if="unread" class="ox-pill brand">{{ t('ox.ch.unread', { n: unread }) }}</span></div>
    </div>
  <div class="msgs" :class="{ picked: !!selectedId, info: !!current }" data-testid="messages-page">
    <aside class="ms-list">
      <div class="ms-search"><label class="ox-search"><input v-model="filter.q" type="search" :placeholder="t('ch.search')" data-testid="chat-search" /></label></div>
      <div class="ms-filters ox-chips">
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
      <p v-if="chatPush.supported && !readOnly" class="ms-shared ms-push" data-testid="desk-push">
        <template v-if="chatPush.on">{{ t('ch.push_is_on') }} <button class="btn link small" :disabled="chatPush.busy" @click="disableChatPush('staff')">{{ t('ch.push_off') }}</button></template>
        <template v-else-if="chatPush.denied">{{ t('ch.push_denied') }}</template>
        <button v-else class="btn link small" :disabled="chatPush.busy" data-testid="desk-push-on" @click="enableChatPush('staff')">{{ t('ch.push_ask_desk') }}</button>
      </p>
    </aside>

    <section class="ms-chat">
      <div v-if="error" class="banner bad" style="margin:12px" data-testid="chat-page-error">{{ error }}</div>
      <div v-if="!selectedId" class="ms-pick">{{ t('ch.pick') }}</div>
      <template v-else-if="current">
        <header class="ms-head">
          <button class="btn link ms-back" data-testid="chat-back" @click="router.push({ query: {} })">‹ {{ t('back') }}</button>
          <b data-testid="chat-with">{{ name(current) }} · {{ t('ch.tail', { n: current.phone_tail ?? '' }) }}</b>
          <span class="small muted">{{ t('ch.orders_here', { n: current.order_count }) }}</span>
          <span v-if="!readOnly" class="ms-acts">
            <button class="btn link small" data-testid="chat-block" :data-on="current.blocked || null" @click="block">{{ t(current.blocked ? 'ch.unblock' : 'ch.block') }}</button>
            <button v-if="current.id" class="btn link small" data-testid="chat-report-open" @click="reporting = true">{{ t('ch.report_customer') }}</button>
          </span>
        </header>
        <div v-if="current.blocked" class="banner warn" style="margin:10px 12px 0" data-testid="chat-blocked-note">{{ t('ch.blocked_note') }}</div>
        <div v-if="note" class="banner ok" style="margin:10px 12px 0" data-testid="chat-note">{{ note }}</div>
        <ChatPane ref="pane" :key="current.peer" :peer="current.peer" self="store" :branch-id="bid" :staff="me" :peer-read-at="peerReadAt"
          :blocked="blocked" desk :upload-path="`/merchant/branches/${bid}/chats/${current.customer_id}/images`" :orders="current.orders"
          @seen="seen" @activity="activity" @order="openOrder">
          <template #above>
            <div v-if="setup && !blocked" class="ms-quick" data-testid="chat-quick">
              <button v-for="q in setup.quick_replies" :key="q.id" type="button" data-testid="chat-quick-item" @click="pane?.put(q.body)">{{ q.body }}</button>
              <button type="button" class="more" data-testid="chat-quick-manage" @click="managing = true">{{ t(setup.quick_replies.length ? 'ch.quick_manage' : 'ch.quick') }}</button>
            </div>
          </template>
        </ChatPane>
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

    <ChatReport v-if="reporting && current" :path="`/merchant/branches/${bid}/chats/${current.customer_id}/report`" :title="t('ch.report_customer')"
      @close="reporting = false" @done="reporting = false; note = t('ch.reported')" />
    <div v-if="managing && setup" class="sheet-mask" data-testid="chat-setup" @click.self="managing = false">
      <div class="sheet ms-setup">
        <div class="sheet-head row between"><b>{{ t('ch.quick') }}</b><button class="btn link" @click="managing = false">{{ t('close') }}</button></div>
        <div class="ms-setup-body">
          <div v-if="setupError" class="banner bad">{{ setupError }}</div>
          <p v-if="!setup.quick_replies.length" class="small muted">{{ t('ch.quick_none') }} <button class="btn link small" data-testid="chat-quick-defaults" @click="addDefaults">{{ t('ch.quick_defaults') }}</button></p>
          <div v-for="q in setup.quick_replies" :key="q.id" class="ms-q"><span>{{ q.body }}</span><button class="btn link small bad" :aria-label="t('delete')" data-testid="chat-quick-del" @click="removeQuick(q.id)">×</button></div>
          <form class="ms-q" @submit.prevent="newQuick.trim() && addQuick(newQuick)">
            <input v-model="newQuick" maxlength="200" :placeholder="t('ch.quick_add')" data-testid="chat-quick-new" /><button class="btn small" :disabled="!newQuick.trim()" data-testid="chat-quick-add">{{ t('ch.add') }}</button>
          </form>
          <h4>{{ t('ch.auto_title') }}</h4>
          <p class="small muted">{{ t('ch.auto_hint') }}<template v-if="!manager"> {{ t('ch.auto_manager') }}</template></p>
          <label class="row" style="gap:8px"><input v-model="auto.on" type="checkbox" :disabled="!manager" data-testid="chat-auto-on" /> {{ t('ch.auto_title') }}</label>
          <textarea v-model="auto.text" rows="2" maxlength="200" :disabled="!manager || !auto.on" :placeholder="t('ch.auto_placeholder')" data-testid="chat-auto-text"></textarea>
          <button v-if="manager" class="btn primary" data-testid="chat-auto-save" @click="saveAuto">{{ t('save') }}</button>
        </div>
      </div>
    </div>
  </div>
  </div>
  </div>
</template>

<style>
.ms-quick { display: flex; gap: 6px; padding: 6px 12px; background: #fff; border-top: 1px solid #eee; overflow-x: auto; scrollbar-width: none; }
.ms-quick button { flex: none; max-width: 220px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; border: 1px solid #d6dae0; border-radius: 14px; background: #fff; padding: 3px 10px; font: inherit; font-size: 13px; cursor: pointer; }
.ms-quick button.more { border-style: dashed; color: var(--muted); }
.ms-setup-body { padding: 14px 16px 18px; display: grid; gap: 8px; overflow-y: auto; }
.ms-setup-body h4 { margin: 10px 0 0; } .ms-setup-body textarea, .ms-q input { font: inherit; padding: 8px 10px; border: 1px solid #d6dae0; border-radius: 8px; width: 100%; }
.ms-q { display: flex; gap: 8px; align-items: center; } .ms-q span { flex: 1; min-width: 0; overflow-wrap: anywhere; background: #f6f7f8; border-radius: 8px; padding: 6px 10px; }
.msg-page { grid-template-rows: auto minmax(0, 1fr); height: calc(100dvh - var(--shell-top, 0px)); }
.msgs { display: grid; grid-template-columns: 320px minmax(0, 1fr); gap: 14px; min-height: 0; }
.msgs.info { grid-template-columns: 320px minmax(0, 1fr) 260px; }
.msgs > * { background: var(--card); border: 1px solid var(--line); border-radius: 12px; overflow: hidden; }
.ms-list { overflow-y: auto; min-width: 0; }
.ms-search { padding: 12px 12px 8px; } .ms-search .ox-search { max-width: none; display: block; }
.ms-filters { padding: 2px 12px 10px; border-bottom: 1px solid var(--line); }
.ms-row { display: flex; gap: 12px; padding: 12px; border: 0; border-bottom: 1px solid #f0e6d8; background: none; width: 100%; text-align: start; cursor: pointer; align-items: flex-start; color: var(--ink); font: inherit;
  border-inline-start: 3px solid transparent; }
.ms-row:hover { background: #fbf4ea; }
.ms-row.on { background: var(--pink); border-inline-start-color: var(--brand); }
.ms-row .grow { min-width: 0; display: grid; gap: 2px; } .ms-row .tag { justify-self: start; }
.ms-row .last { color: var(--muted); font-size: 14px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; min-width: 0; }
.ms-row .num { display: inline-grid; place-items: center; min-width: 18px; height: 18px; padding: 0 5px; border-radius: 9px; background: #e5352b; color: #fff; font-size: 11px; font-style: normal; font-weight: 700; flex: none; }
.ms-ava { width: 42px; height: 42px; border-radius: 50%; background: #c8641e; color: #fff; display: grid; place-items: center; font-weight: 700; flex: none; }
.ms-empty, .ms-pick { color: var(--muted); text-align: center; padding: 48px 20px; font-size: 14px; }
.ms-shared { font-size: 12px; color: #a8927f; text-align: center; padding: 14px; }
.ms-chat { display: flex; flex-direction: column; min-width: 0; min-height: 0; background: #f3ece1; }
.ms-head { background: var(--card); border-bottom: 1px solid var(--line); padding: 12px 16px; display: flex; gap: 12px; align-items: baseline; flex: none; }
.ms-back { display: none; }
.ms-acts { margin-inline-start: auto; display: flex; gap: 12px; white-space: nowrap; } .ms-head { flex-wrap: wrap; }
.ms-push { padding-top: 0; }
.ms-info { padding: 4px 16px 16px; overflow-y: auto; display: grid; gap: 6px; align-content: start; }
.ms-info h4 { margin: 14px 0 2px; font-size: 14px; color: #2f1810; font-weight: 700; }
.ms-order { background: #fff; border: 1px solid var(--line); border-radius: 12px; padding: 10px 12px; display: grid; gap: 4px; }
.ms-info .lnk { color: var(--brand); font-size: 14px; }
@media (max-width: 1199px) { .msgs, .msgs.info { grid-template-columns: 300px minmax(0, 1fr); } .ms-info { display: none; } }
@media (max-width: 767px) {
  .msgs, .msgs.info { grid-template-columns: minmax(0, 1fr); }
  .msg-page { padding: 0; gap: 0; } .msg-page .ms-pagehead { padding: 12px 12px 10px; }
  .msg-page.picked .ms-pagehead { display: none; }
  .msgs > * { border-radius: 0; border-width: 1px 0 0; }
  .msgs.picked .ms-list { display: none; } .msgs:not(.picked) .ms-chat { display: none; }
  .ms-back { display: inline-flex; }
}
</style>
