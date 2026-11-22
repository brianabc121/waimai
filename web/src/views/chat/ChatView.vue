<script setup lang="ts">
// The customer's chat with one store.
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { loadCustomer, loginRoute } from '../../customer'
import { pick, t } from '../../i18n'
import { customerChat, customerChatRead, describeChat, type CustomerChat } from '../../chat'
import { loadProfile } from '../me/me'
import ChatPane from './ChatPane.vue'

const props = defineProps<{ slug: string }>()
const route = useRoute(), router = useRouter()
const info = ref<CustomerChat | null>(null)
const error = ref('')

async function load() {
  try { info.value = await customerChat(props.slug) } catch (e) { error.value = (e as { code?: string }).code === 'NOT_FOUND' ? t('not_found') : (e as Error).message }
}
onMounted(async () => {
  if (!(await loadCustomer())) { await router.replace(loginRoute(route.fullPath, props.slug)); return }
  await load()
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
function goBack() { if (window.history.state?.back) router.back(); else void router.replace('/me/messages') }
</script>

<template>
  <div class="phone chatpage" data-testid="chat-page">
    <header class="ch-bar">
      <button :aria-label="t('back')" data-testid="chat-back" @click="goBack">
        <svg viewBox="0 0 24 24" width="24" height="24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m15 5-7 7 7 7" /></svg></button>
      <b v-if="info" class="ch-ttl"><router-link :to="`/s/${info.slug}`">{{ pick(info.store_name, info.store_name_en) }}</router-link>
        <small :class="{ on: info.open_now }" data-testid="chat-open">{{ t(info.open_now ? 'ch.open' : 'ch.closed') }}</small></b>
      <b v-else>{{ t('ch.title') }}</b>
      <span class="right"><a v-if="info?.phone" class="ch-call" :href="`tel:${info.phone}`" :aria-label="t('ch.call')" :title="t('ch.call')">
        <svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M6.6 3.5h2.6l1.3 3.6-1.8 1.4a12 12 0 0 0 5.800 5.800l1.4-1.800 3.600 1.300v2.600a2 2 0 0 1-2.200 2A16.500 16.500 0 0 1 4.600 5.700a2 2 0 0 1 2-2.200z" /></svg></a></span>
    </header>
    <div v-if="error" class="banner bad" style="margin:12px">{{ error }}</div>
    <ChatPane v-if="info" :peer="info.peer" self="customer" :peer-read-at="info.peer_read_at" :blocked="blocked" @seen="seen">
      <template #top>
        <div class="ch-tip" data-testid="chat-privacy">{{ t('ch.privacy') }}</div>
        <div v-if="!info.open_now" class="ch-tip warn" data-testid="chat-closed-note">{{ t('ch.closed_note') }}</div>
      </template>
    </ChatPane>
  </div>
</template>

<style>
.chatpage { display: flex; flex-direction: column; height: 100dvh; min-height: 0; padding-bottom: 0; background: #f4f4f4; }
.ch-bar { flex: none; display: flex; align-items: center; gap: 4px; height: 52px; padding: 0 6px; background: #fff; border-bottom: 1px solid #eee; }
.ch-bar > button { border: 0; background: none; width: 40px; height: 40px; display: grid; place-items: center; color: var(--ink); cursor: pointer; flex: none; }
.ch-bar > b { flex: 1; min-width: 0; font-size: 16px; }
.ch-bar .right { flex: none; }
.ch-ttl { display: grid; line-height: 1.2; } .ch-ttl a { color: inherit; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ch-ttl small { font-size: 12px; font-weight: 400; color: var(--muted); } .ch-ttl small.on { color: var(--ok, #18794e); }
.ch-call { color: var(--ink); display: grid; place-items: center; width: 40px; height: 40px; }
.ch-tip { align-self: center; background: #eceef1; color: #4b5563; font-size: 12px; border-radius: 10px; padding: 6px 12px; max-width: 92%; text-align: center; }
.ch-tip.warn { background: #fff8e1; color: #9a6700; }
</style>
