<script setup lang="ts">
// The message box. "Chats" are the conversations with stores; "notices" are
// order updates, answers from the platform and wallet changes.
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { customerChats, day, lastLine, listen, connect, type ChatThread } from '../../chat'
import { pick, t } from '../../i18n'
import MePage from './MePage.vue'
import { dateTime, describe, me, myMessages, readMessages, type Message } from './me'

const route = useRoute(), router = useRouter()

// ---- chats ----
const tab = ref<'chats' | 'notices'>(route.query.tab === 'notices' ? 'notices' : 'chats')
const chats = ref<ChatThread[] | null>(null), chatOn = ref(true)
async function loadChats() {
  try {
    const r = await customerChats()
    chats.value = r.chats; chatOn.value = r.enabled
    if (me.profile) me.profile.chat_unread = r.chats.reduce((n, c) => n + c.unread, 0)
    // Open on the notices where chat is not set up, and where only they have something new.
    const unread = r.chats.some(c => c.unread > 0), chats0 = first
    first = false
    if (!route.query.tab && !tabPicked && chats0 && ((!r.enabled && !r.chats.length) || (!unread && (me.profile?.unread ?? 0) > 0))) tab.value = 'notices'
  } catch (e) { error.value = describe(e) }
}
let tabPicked = false, first = true
function pickTab(v: 'chats' | 'notices') { tabPicked = true; tab.value = v; void router.replace({ query: { tab: v } }) }
// A message that arrives while the list is open moves its store to the top.
let stop = () => {}, timer = 0
onMounted(() => {
  stop = listen(() => void loadChats())
  timer = window.setInterval(() => { if (document.visibilityState === 'visible') void loadChats() }, 30000)
})
onBeforeUnmount(() => { stop(); clearInterval(timer) })

// ---- notices ----
const list = ref<Message[] | null>(null)
const more = ref(false), error = ref('')
async function load(reset = false) {
  try {
    const r = await myMessages(reset ? 0 : list.value?.length ?? 0)
    list.value = reset ? r.messages : [...(list.value ?? []), ...r.messages]
    more.value = r.more
  } catch (e) { error.value = describe(e) }
}
watch(() => me.profile?.id, async id => {
  if (!id) return
  void load(true)
  await loadChats()
  if (chatOn.value) void connect().catch(() => null)
}, { immediate: true })

async function open(m: Message) {
  if (!m.read) {
    m.read = true
    if (me.profile) me.profile.unread = Math.max(0, me.profile.unread - 1)
    await readMessages(m.id).catch(() => null)
  }
  if (m.link.startsWith('/') && !m.link.startsWith('//')) void router.push(m.link)
}
async function readAll() {
  await readMessages().catch(() => null)
  list.value?.forEach(m => { m.read = true })
  if (me.profile) me.profile.unread = 0
}
</script>

<template>
  <MePage :title="t('mm.title')">
    <template #right><button v-if="tab === 'notices' && me.profile?.unread" class="btn link small" data-testid="msg-read-all" @click="readAll">{{ t('mm.read_all') }}</button></template>
    <div class="mtabs" role="tablist">
      <button role="tab" :class="{ on: tab === 'chats' }" data-testid="msg-tab-chats" @click="pickTab('chats')">{{ t('ch.tab_chats') }}
        <i v-if="me.profile?.chat_unread" data-testid="msg-chats-unread">{{ me.profile.chat_unread > 99 ? '99+' : me.profile.chat_unread }}</i></button>
      <button role="tab" :class="{ on: tab === 'notices' }" data-testid="msg-tab-notices" @click="pickTab('notices')">{{ t('ch.tab_notices') }}
        <i v-if="me.profile?.unread" data-testid="msg-notices-unread">{{ me.profile.unread > 99 ? '99+' : me.profile.unread }}</i></button>
    </div>
    <div v-if="error" class="banner bad">{{ error }}</div>
    <template v-if="tab === 'chats'">
      <div v-if="chats && !chats.length" class="card empty" data-testid="chats-empty">{{ t(chatOn ? 'ch.none' : 'ch.off') }}</div>
      <div v-if="chats?.length" class="card clist" data-testid="chats">
        <router-link v-for="c in chats" :key="c.id" :to="`/chat/${c.slug}`" class="crow" data-testid="chat-row" :data-slug="c.slug">
          <span class="me-logo">{{ (c.store_name ?? '').slice(0, 1) }}</span>
          <span class="grow">
            <span class="row between"><b>{{ c.store_name }}</b><span class="small muted">{{ day(c.last_at) }}</span></span>
            <span class="row between"><span class="last">{{ lastLine(c, 'customer') }}</span><i v-if="c.unread" class="num" data-testid="chat-row-unread">{{ c.unread > 99 ? '99+' : c.unread }}</i></span>
            <span v-if="c.order" class="tag warn">#{{ c.order.public_no }} {{ t('s.' + c.order.status) }}</span>
          </span>
        </router-link>
      </div>
      <p v-if="chats?.length" class="note" style="text-align:center">{{ t('ch.keep') }}</p>
    </template>
    <template v-else>
      <div v-if="list && !list.length" class="card empty" data-testid="msg-empty">{{ t('mm.none') }}</div>
      <button v-for="m in list ?? []" :key="m.id" class="card msg" :class="{ unread: !m.read }" data-testid="msg" :data-kind="m.kind" @click="open(m)">
        <div class="row between"><span class="tag" :class="m.kind === 'order' ? 'brand' : m.kind === 'wallet' ? 'ok' : 'info'">{{ t('mm.kind_' + m.kind) }}</span>
          <span class="small muted">{{ dateTime(m.created_at) }}</span></div>
        <b><i v-if="!m.read" class="dot"></i>{{ pick(m.title, m.title_en) }}</b>
        <p v-if="m.body" class="small muted" style="white-space:pre-wrap">{{ pick(m.body, m.body_en) }}</p>
      </button>
      <button v-if="more" class="btn block" @click="load()">{{ t('mm.load_more') }}</button>
    </template>
  </MePage>
</template>

<style scoped>
.msg { display: grid; gap: 6px; text-align: left; width: 100%; cursor: pointer; }
.msg b { font-size: 15px; } .msg:not(.unread) b { font-weight: 500; color: #4b5563; }
.mtabs { display: flex; background: #fff; border-radius: 14px; overflow: hidden; }
.mtabs button { flex: 1; border: 0; background: none; padding: 12px 0; color: var(--muted); font-size: 15px; cursor: pointer; }
.mtabs button.on { color: var(--ink); font-weight: 700; box-shadow: inset 0 -3px 0 var(--brand); }
.mtabs i, .crow .num { display: inline-grid; place-items: center; min-width: 18px; height: 18px; padding: 0 5px; border-radius: 9px; background: #e5352b; color: #fff; font-size: 11px; font-style: normal; font-weight: 700; flex: none; vertical-align: 1px; }
.clist { padding: 0 14px; }
.crow { display: flex; gap: 12px; padding: 13px 0; border-bottom: 1px solid #f0f1f3; color: var(--ink); align-items: flex-start; }
.crow:last-child { border-bottom: 0; }
.crow .grow { min-width: 0; display: grid; gap: 2px; }
.crow .last { color: var(--muted); font-size: 14px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; min-width: 0; }
.crow .tag { justify-self: start; }
.dot { display: inline-block; width: 8px; height: 8px; border-radius: 50%; background: #e5352b; margin-right: 6px; vertical-align: 1px; }
</style>
