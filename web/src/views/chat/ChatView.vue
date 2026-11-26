<script setup lang="ts">
// The customer's chat with one store.
import { storePath } from '../../site'
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { loadCustomer, loginRoute } from '../../customer'
import { pick, t } from '../../i18n'
import { customerChat, customerChatHide, customerChatQuiet, customerChatRead, describeChat, type CustomerChat } from '../../chat'
import { chatPush, enableChatPush, setupChatPush } from '../../webpush'
import { loadProfile } from '../me/me'
import ChatPane from './ChatPane.vue'
import ChatReport from './ChatReport.vue'
import './chatpage.css'

const props = defineProps<{ slug: string }>()
const route = useRoute(), router = useRouter()
const info = ref<CustomerChat | null>(null)
const error = ref(''), note = ref('')
const menu = ref(false), reporting = ref(false)

async function load() {
  try { info.value = await customerChat(props.slug) } catch (e) { error.value = (e as { code?: string }).code === 'NOT_FOUND' ? t('not_found') : (e as Error).message }
}
onMounted(async () => {
  if (!(await loadCustomer())) { await router.replace(loginRoute(route.fullPath, props.slug)); return }
  await load()
  void setupChatPush('customer')
  if (info.value) document.title = pick(info.value.store_name, info.value.store_name_en)
  // Asked again every few seconds: whether the store has read (the chat
  // server's own receipt can be missed), and whether writing is still allowed.
  timer = window.setInterval(() => {
    if (document.visibilityState === 'visible' && info.value) void customerChat(props.slug).then(v => { info.value = v }, () => null)
  }, 6000)
})
let timer = 0
onBeforeUnmount(() => clearInterval(timer))
const blocked = computed(() => !info.value ? '' : info.value.muted ? describeChat('CHAT_MUTED') : !info.value.enabled ? describeChat('CHAT_CLOSED') : '')
async function seen() { await customerChatRead(props.slug).catch(() => null); void loadProfile() }
async function quiet() {
  menu.value = false
  if (!info.value) return
  await customerChatQuiet(props.slug, !info.value.quiet).then(() => { info.value!.quiet = !info.value!.quiet }, e => { error.value = e.message })
}
async function hide() {
  menu.value = false
  if (!confirm(t('ch.hide_confirm'))) return
  await customerChatHide(props.slug).then(() => router.replace('/me/messages'), e => { error.value = e.message })
}
function goBack() { if (window.history.state?.back) router.back(); else void router.replace('/me/messages') }
</script>

<template>
  <div class="phone chatpage" data-testid="chat-page">
    <header class="ch-bar">
      <button :aria-label="t('back')" data-testid="chat-back" @click="goBack">
        <svg viewBox="0 0 24 24" width="24" height="24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m15 5-7 7 7 7" /></svg></button>
      <b v-if="info" class="ch-ttl"><router-link :to="storePath(info.slug)">{{ pick(info.store_name, info.store_name_en) }}</router-link>
        <small :class="{ on: info.open_now }" data-testid="chat-open">{{ t(info.open_now ? 'ch.open' : 'ch.closed') }}</small></b>
      <b v-else>{{ t('ch.title') }}</b>
      <span class="right row"><a v-if="info?.phone" class="ch-call" :href="`tel:${info.phone}`" :aria-label="t('ch.call')" :title="t('ch.call')">
        <svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M6.6 3.5h2.6l1.3 3.6-1.8 1.4a12 12 0 0 0 5.800 5.800l1.4-1.800 3.600 1.300v2.600a2 2 0 0 1-2.200 2A16.500 16.500 0 0 1 4.600 5.700a2 2 0 0 1 2-2.200z" /></svg></a>
        <button v-if="info" class="ch-call" :aria-label="t('ch.more')" :title="t('ch.more')" data-testid="chat-more" @click="menu = !menu">
          <svg viewBox="0 0 24 24" width="22" height="22" fill="currentColor"><circle cx="5" cy="12" r="1.8" /><circle cx="12" cy="12" r="1.8" /><circle cx="19" cy="12" r="1.8" /></svg></button></span>
      <div v-if="menu && info" class="ch-menu" data-testid="chat-menu">
        <a v-if="info.phone" :href="`tel:${info.phone}`" @click="menu = false">{{ t('ch.call') }}</a>
        <button v-if="info.thread_id" data-testid="chat-quiet" @click="quiet">{{ t(info.quiet ? 'ch.unquiet' : 'ch.quiet') }}</button>
        <button v-if="info.thread_id" data-testid="chat-report-open" @click="menu = false; reporting = true">{{ t('ch.report_store') }}</button>
        <button v-if="info.thread_id" class="bad" data-testid="chat-hide" @click="hide">{{ t('ch.hide') }}</button>
      </div>
    </header>
    <div v-if="menu" class="ch-menu-mask" @click="menu = false"></div>
    <div v-if="error" class="banner bad" style="margin:12px">{{ error }}</div>
    <div v-if="note" class="banner ok" style="margin:12px" data-testid="chat-note">{{ note }}</div>
    <ChatPane v-if="info" :peer="info.peer" self="customer" :peer-read-at="info.peer_read_at" :blocked="blocked"
      :upload-path="`/customer/chats/${slug}/images`" :orders="info.orders" @seen="seen" @order="o => o.token && router.push(`/o/${o.token}`)">
      <template #top>
        <div class="ch-tip" data-testid="chat-privacy">{{ t('ch.privacy') }}</div>
        <div v-if="!info.open_now" class="ch-tip warn" data-testid="chat-closed-note">{{ t('ch.closed_note') }}</div>
        <div v-if="info.quiet" class="ch-tip" data-testid="chat-quiet-note">{{ t('ch.quiet_on') }}</div>
      </template>
      <template #above>
        <!-- asked only after the first message, and only when tapped -->
        <div v-if="info.thread_id && chatPush.supported && !chatPush.on && !info.quiet" class="ch-push" data-testid="chat-push">
          <span>{{ t(chatPush.denied ? 'ch.push_denied' : 'ch.push_ask') }}</span>
          <button v-if="!chatPush.denied" class="btn small primary" :disabled="chatPush.busy" data-testid="chat-push-on" @click="enableChatPush('customer')">{{ t('ch.push_on') }}</button>
        </div>
      </template>
    </ChatPane>
    <ChatReport v-if="reporting" :path="`/customer/chats/${slug}/report`" :title="t('ch.report_store')" @close="reporting = false"
      @done="reporting = false; note = t('ch.reported')" />
  </div>
</template>
