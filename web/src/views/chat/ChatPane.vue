<script setup lang="ts">
// One conversation: the messages and the box to write in. The customer's chat
// page and the store's message page both use it.
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { t } from '../../i18n'
import { chat, clock, connect, history, listen, sendText, type ChatMsg } from '../../chat'

const props = defineProps<{
  peer: string
  self: 'customer' | 'store'
  branchId?: number // set on the store's side
  staff?: { uid: number; name: string }
  peerReadAt: string | null
  blocked?: string // why nothing can be sent, when it cannot
  desk?: boolean // Enter sends
}>()
const emit = defineEmits<{ seen: []; activity: [] }>()

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
    const sent = await sendText(props.branchId, props.peer, m.text, props.staff)
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
            <span v-if="m.kind === 'text'">{{ m.text }}</span><span v-else class="muted">{{ t('ch.other') }}</span>
            <em v-if="m.mine && self === 'store' && m.staff">{{ m.staff }}</em>
          </div>
        </div>
        <div v-if="m.state === 'failed'" class="cp-state bad" data-testid="chat-failed" @click="deliver(m)">{{ m.error || t('ch.failed') }} · {{ t('ch.failed') }}</div>
        <div v-else-if="m.state === 'sending'" class="cp-state">{{ t('ch.sending') }}</div>
        <div v-else-if="m.id === lastRead" class="cp-state" data-testid="chat-read">{{ t('ch.read') }}</div>
      </template>
    </div>
    <slot name="above" />
    <div v-if="blocked" class="cp-blocked" data-testid="chat-blocked">{{ blocked }}</div>
    <form v-else class="cp-box" @submit.prevent="send">
      <textarea v-model="draft" rows="1" maxlength="1000" :placeholder="t(desk ? 'ch.input_desk' : 'ch.input')" data-testid="chat-input" @keydown="key"></textarea>
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
.cp-state { align-self: flex-end; font-size: 11px; color: #9aa1ab; margin-top: -4px; }
.cp-state.bad { color: var(--bad, #b42318); cursor: pointer; }
.cp-blocked { background: #fff; border-top: 1px solid #eee; padding: 12px 16px calc(12px + env(safe-area-inset-bottom)); text-align: center; color: var(--muted); font-size: 14px; }
.cp-box { display: flex; gap: 8px; align-items: flex-end; padding: 8px 12px calc(8px + env(safe-area-inset-bottom)); background: #fff; border-top: 1px solid #eee; }
.cp-box textarea { flex: 1; min-width: 0; border: 0; background: #f1f3f5; border-radius: 18px; padding: 9px 14px; font: inherit; resize: none; max-height: 120px; field-sizing: content; }
.cp-box .btn { border-radius: 18px; min-height: 38px; padding: 0 16px; }
@media (min-width: 768px) { .cp-msg { max-width: 62%; } .cp-list { padding: 16px 20px; } }
</style>
