<script setup lang="ts">
// One conversation: the messages and the box to write in. The customer's chat
// page and the store's message page both use it.
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { money, t } from '../../i18n'
import { chat, clock, connect, history, listen, sendImage, sendOrder, sendText, type ChatMsg, type ChatOrder } from '../../chat'

const props = defineProps<{
  peer: string
  self: 'customer' | 'store'
  branchId?: number // set on the store's side
  staff?: { uid: number; name: string }
  peerReadAt: string | null
  blocked?: string // why nothing can be sent, when it cannot
  desk?: boolean // Enter sends
  uploadPath: string // where this conversation's pictures are uploaded
  orders?: ChatOrder[] // the customer's orders with the store: what a card shows and what can be sent
}>()
const emit = defineEmits<{ seen: []; activity: []; order: [order: ChatOrder] }>()

const list = ref<ChatMsg[]>([])
const end = ref(true), loading = ref(true), error = ref('')
const draft = ref(''), box = ref<HTMLElement | null>(null)
const readAt = ref(0)
watch(() => props.peerReadAt, v => { readAt.value = Math.max(readAt.value, v ? Date.parse(v) : 0) }, { immediate: true })

function toBottom() { void nextTick(() => { if (box.value) box.value.scrollTop = box.value.scrollHeight }) }
function seen() { if (document.visibilityState === 'visible') emit('seen') }

async function load() {
  loading.value = true; error.value = ''; list.value = []; readAt.value = props.peerReadAt ? Date.parse(props.peerReadAt) : 0
  const peer = props.peer
  try {
    const r = await history(props.branchId, peer)
    if (peer !== props.peer) return
    list.value = r.list; end.value = r.end
    toBottom()
    if (r.list.some(m => !m.mine)) seen()
  } catch (e) { error.value = (e as Error).message }
  loading.value = false
}
async function earlier() {
  const first = list.value[0]
  if (!first) return
  const el = box.value, from = el ? el.scrollHeight : 0
  try {
    const r = await history(props.branchId, props.peer, first.id)
    list.value = [...r.list, ...list.value]; end.value = r.end
    void nextTick(() => { if (el) el.scrollTop = el.scrollHeight - from })
  } catch (e) { error.value = (e as Error).message }
}

async function deliver(m: ChatMsg) {
  m.state = 'sending'; m.error = ''
  try {
    const sent = await (m.retry ? m.retry() : sendText(props.branchId, props.peer, m.text, props.staff))
    Object.assign(m, sent, { state: 'sent' })
    emit('activity')
  } catch (e) { m.state = 'failed'; m.error = (e as Error).message }
}
function send() {
  const text = draft.value.trim()
  if (!text || props.blocked) return
  const m: ChatMsg = { id: 'local-' + Date.now() + Math.random(), mine: true, kind: 'text', text, at: Date.now(), state: 'sending', staff: props.staff?.name }
  list.value.push(m)
  draft.value = ''
  toBottom()
  void deliver(list.value[list.value.length - 1])
}
function local(kind: 'image' | 'order', more: Partial<ChatMsg>): ChatMsg {
  list.value.push({ id: 'local-' + Date.now() + Math.random(), mine: true, kind, text: '', at: Date.now(), state: 'sending', staff: props.staff?.name, ...more })
  toBottom()
  return list.value[list.value.length - 1]
}
// a picture: shown at once from the device, then uploaded and sent
function pickImage(e: Event) {
  const input = e.target as HTMLInputElement, file = input.files?.[0]
  input.value = ''
  if (!file || props.blocked) return
  const peer = props.peer, branch = props.branchId, path = props.uploadPath, staff = props.staff
  void deliver(local('image', { image: URL.createObjectURL(file), retry: () => sendImage(branch, peer, path, file, staff) }))
}
const picking = ref(false)
function pickOrder(o: ChatOrder) {
  picking.value = false
  const peer = props.peer, branch = props.branchId, staff = props.staff
  void deliver(local('order', { orderId: o.id, retry: () => sendOrder(branch, peer, o.id, staff) }))
}
const orderOf = (id?: number) => props.orders?.find(o => o.id === id)
// put fills the box (a quick reply), ready to be changed or sent
const input = ref<HTMLTextAreaElement | null>(null)
function put(text: string) { draft.value = text; void nextTick(() => input.value?.focus()) }
defineExpose({ put })
function key(e: KeyboardEvent) {
  if (e.key === 'Enter' && !e.shiftKey && !e.isComposing && props.desk) { e.preventDefault(); send() }
}

let stop = () => {}
const onVisible = () => { if (document.visibilityState === 'visible' && list.value.some(m => !m.mine)) seen() }
onMounted(() => {
  stop = listen((peer, m) => {
    if (peer !== props.peer || list.value.some(x => x.id === m.id)) return
    list.value.push(m)
    toBottom()
    if (!m.mine) seen()
    emit('activity')
  }, peer => { if (peer === props.peer) readAt.value = Date.now() })
  document.addEventListener('visibilitychange', onVisible)
  void load()
})
onBeforeUnmount(() => { stop(); document.removeEventListener('visibilitychange', onVisible) })
watch(() => props.peer, () => void load())
// back on line after a drop: read again what may have been missed
watch(() => chat.state, (now, was) => { if (now === 'on' && (was === 'down' || was === 'kicked')) void load() })

// the last message of mine the other side has read
const lastRead = computed(() => {
  for (let i = list.value.length - 1; i >= 0; i--) {
    const m = list.value[i]
    if (m.mine && m.state === 'sent') return m.at <= readAt.value ? m.id : ''
  }
  return ''
})
const stamp = (i: number) => i === 0 || list.value[i].at - list.value[i - 1].at > 5 * 60 * 1000
</script>

<template>
  <div class="cpane" data-testid="chat-pane" :data-state="chat.state">
    <div v-if="chat.state === 'down' || chat.state === 'kicked'" class="cp-line" data-testid="chat-down">
      {{ t(chat.state === 'kicked' ? 'ch.kicked' : 'ch.down') }}
      <button class="btn link small" @click="connect(branchId).then(load).catch(() => null)">{{ t('ch.retry') }}</button>
    </div>
    <div ref="box" class="cp-list" data-testid="chat-list">
      <slot name="top" />
      <button v-if="!end" class="btn link small cp-more" data-testid="chat-earlier" @click="earlier">{{ t('ch.earlier') }}</button>
      <div v-if="loading" class="cp-note">{{ t('ch.connecting') }}</div>
      <div v-else-if="error" class="banner bad" data-testid="chat-error">{{ error }}</div>
      <div v-else-if="!list.length" class="cp-note" data-testid="chat-empty">{{ t(self === 'store' ? 'ch.start_desk' : 'ch.start') }}</div>
      <template v-for="(m, i) in list" :key="m.id">
        <div v-if="stamp(i)" class="cp-day">{{ clock(m.at) }}</div>
        <div class="cp-msg" :class="{ me: m.mine, failed: m.state === 'failed' }" data-testid="chat-msg" :data-mine="m.mine" :data-state="m.state">
          <div class="cp-bub" @click="m.state === 'failed' && deliver(m)">
            <span v-if="m.kind === 'text'">{{ m.text }}</span>
            <a v-else-if="m.kind === 'image'" class="cp-img" :href="m.image" target="_blank" rel="noopener" data-testid="chat-image" @click="m.state === 'failed' && $event.preventDefault()"><img :src="m.image" alt="" @load="toBottom" /></a>
            <button v-else-if="m.kind === 'order'" type="button" class="cp-order" data-testid="chat-order-card" :disabled="!orderOf(m.orderId)" @click.stop="orderOf(m.orderId) && emit('order', orderOf(m.orderId)!)">
              <template v-if="orderOf(m.orderId)">
                <b>{{ t('ch.order_card', { no: orderOf(m.orderId)!.public_no }) }}</b><i class="tag warn">{{ t('s.' + orderOf(m.orderId)!.status) }}</i>
                <small>{{ clock(orderOf(m.orderId)!.created_at) }} · {{ money(orderOf(m.orderId)!.total, orderOf(m.orderId)!.currency) }}</small>
                <small class="go">{{ t('ch.view_order') }}</small>
              </template>
              <b v-else>{{ t('ch.order_gone') }}</b>
            </button>
            <span v-else class="muted">{{ t('ch.other') }}</span>
            <em v-if="m.auto">{{ t('ch.auto') }}</em>
            <em v-else-if="m.mine && self === 'store' && m.staff">{{ m.staff }}</em>
          </div>
        </div>
        <div v-if="m.state === 'failed'" class="cp-state bad" data-testid="chat-failed" @click="deliver(m)">{{ m.error || t('ch.failed') }} · {{ t('ch.failed') }}</div>
        <div v-else-if="m.state === 'sending'" class="cp-state">{{ t('ch.sending') }}</div>
        <div v-else-if="m.id === lastRead" class="cp-state" data-testid="chat-read">{{ t('ch.read') }}</div>
      </template>
    </div>
    <slot name="above" />
    <div v-if="blocked" class="cp-blocked" data-testid="chat-blocked">{{ blocked }}</div>
    <div v-if="picking && !blocked" class="cp-orders" data-testid="chat-order-picker">
      <div class="row between"><b>{{ t('ch.pick_order') }}</b><button type="button" class="btn link small" @click="picking = false">{{ t('close') }}</button></div>
      <div v-if="!orders?.length" class="small muted">{{ t('ch.no_orders_here') }}</div>
      <button v-for="o in orders ?? []" :key="o.id" type="button" data-testid="chat-order-pick" @click="pickOrder(o)">
        <b>#{{ o.public_no }}</b><span class="tag warn">{{ t('s.' + o.status) }}</span><span class="small muted">{{ clock(o.created_at) }} · {{ money(o.total, o.currency) }}</span>
      </button>
    </div>
    <form v-if="!blocked" class="cp-box" @submit.prevent="send">
      <label class="cp-tool" :title="t('ch.pic')" :aria-label="t('ch.pic')">
        <input type="file" accept="image/*" hidden data-testid="chat-file" @change="pickImage" />
        <svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="5" width="18" height="14" rx="2.500" /><circle cx="8.500" cy="10" r="1.600" /><path d="m4 17 5-4.500 4 3.500 3-2.500 4 3.500" /></svg>
      </label>
      <button type="button" class="cp-tool" :title="t('ch.send_order')" :aria-label="t('ch.send_order')" data-testid="chat-order-btn" @click="picking = !picking">
        <svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M6 3.500h12v17l-3-2-3 2-3-2-3 2z" /><path d="M9 8.500h6M9 12.500h6" /></svg>
      </button>
      <textarea ref="input" v-model="draft" rows="1" maxlength="1000" :placeholder="t(desk ? 'ch.input_desk' : 'ch.input')" data-testid="chat-input" @keydown="key"></textarea>
      <button class="btn primary" :disabled="!draft.trim()" data-testid="chat-send">{{ t('ch.send') }}</button>
    </form>
  </div>
</template>

<style>
.cpane { display: flex; flex-direction: column; min-height: 0; flex: 1; background: #f4f4f4; }
.cp-line { background: #fff8e1; color: #9a6700; font-size: 13px; padding: 6px 12px; text-align: center; }
.cp-list { flex: 1; min-height: 0; overflow-y: auto; padding: 12px; display: flex; flex-direction: column; gap: 8px; overscroll-behavior: contain; }
.cp-more { align-self: center; }
.cp-note { text-align: center; color: var(--muted); font-size: 13px; padding: 24px 12px; }
.cp-day { text-align: center; font-size: 12px; color: #9aa1ab; margin-top: 4px; }
.cp-msg { display: flex; max-width: 82%; }
.cp-msg.me { align-self: flex-end; }
.cp-bub { background: #fff; border-radius: 14px 14px 14px 4px; padding: 8px 12px; white-space: pre-wrap; overflow-wrap: anywhere; box-shadow: 0 1px 1px rgba(0,0,0,.04); font-size: 15px; line-height: 1.45; }
.cp-msg.me .cp-bub { background: var(--brand); color: #fff; border-radius: 14px 14px 4px 14px; }
.cp-msg.failed .cp-bub { opacity: .6; cursor: pointer; }
.cp-bub em { display: block; font-style: normal; font-size: 11px; opacity: .8; text-align: right; margin-top: 2px; }
.cp-bub:has(.cp-img) { padding: 4px; background: #fff !important; }
.cp-img { display: block; } .cp-img img { display: block; max-width: min(220px, 60vw); max-height: 260px; border-radius: 10px; object-fit: cover; }
.cp-bub:has(.cp-order) { padding: 0; background: #fff !important; color: var(--ink) !important; }
.cp-order { display: grid; grid-template-columns: 1fr auto; gap: 2px 10px; align-items: center; border: 0; background: none; font: inherit; text-align: left; padding: 10px 12px; min-width: 200px; cursor: pointer; color: inherit; }
.cp-order small { grid-column: 1 / -1; color: var(--muted); font-size: 12px; } .cp-order small.go { color: var(--brand-dark); } .cp-order i { font-style: normal; }
.cp-bub:has(.cp-order) em, .cp-bub:has(.cp-img) em { color: var(--muted); padding: 0 8px 4px; }
.cp-tool { flex: none; width: 34px; height: 38px; display: grid; place-items: center; border: 0; background: none; color: #5b6470; cursor: pointer; padding: 0; }
.cp-orders { background: #fff; border-top: 1px solid #eee; padding: 10px 12px; display: grid; gap: 6px; max-height: 240px; overflow-y: auto; }
.cp-orders > button { display: flex; gap: 8px; align-items: center; border: 1px solid #e5e7eb; border-radius: 10px; background: #fff; padding: 8px 10px; font: inherit; cursor: pointer; text-align: left; }
.cp-state { align-self: flex-end; font-size: 11px; color: #9aa1ab; margin-top: -4px; }
.cp-state.bad { color: var(--bad, #b42318); cursor: pointer; }
.cp-blocked { background: #fff; border-top: 1px solid #eee; padding: 12px 16px calc(12px + env(safe-area-inset-bottom)); text-align: center; color: var(--muted); font-size: 14px; }
.cp-box { display: flex; gap: 4px; align-items: flex-end; padding: 8px 12px calc(8px + env(safe-area-inset-bottom)); background: #fff; border-top: 1px solid #eee; }
.cp-box textarea { flex: 1; min-width: 0; border: 0; background: #f1f3f5; border-radius: 18px; padding: 9px 14px; font: inherit; resize: none; max-height: 120px; field-sizing: content; }
.cp-box .btn { border-radius: 18px; min-height: 38px; padding: 0 16px; }
@media (min-width: 768px) { .cp-msg { max-width: 62%; } .cp-list { padding: 16px 20px; } }
</style>
