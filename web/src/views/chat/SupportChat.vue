<script setup lang="ts">
// The customer's chat with platform support.
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { loadCustomer, loginRoute } from '../../customer'
import { t } from '../../i18n'
import { customerSupport, customerSupportRead, type SupportChat } from '../../chat'
import { setupChatPush } from '../../webpush'
import { loadProfile, platformInfo, type PlatformInfo } from '../me/me'
import ChatPane from './ChatPane.vue'
import './chatpage.css'

const route = useRoute(), router = useRouter()
const info = ref<SupportChat | null>(null), platform = ref<PlatformInfo | null>(null)
const error = ref('')

async function load() {
  try { info.value = await customerSupport() } catch (e) { error.value = (e as Error).message }
}
let timer = 0
onMounted(async () => {
  if (!(await loadCustomer())) { await router.replace(loginRoute(route.fullPath)); return }
  document.title = t('sp.title')
  void platformInfo().then(p => { platform.value = p }, () => null)
  await load()
  void setupChatPush('customer')
  // asked again every few seconds: whether the platform has read (the chat
  // server's own receipt can be missed)
  timer = window.setInterval(() => { if (document.visibilityState === 'visible' && info.value) void load() }, 6000)
})
onBeforeUnmount(() => clearInterval(timer))
const phone = computed(() => platform.value?.support_phone?.replace(/[^+\d]/g, '') ?? '')
async function seen() { await customerSupportRead().catch(() => null); void loadProfile() }
function goBack() { if (window.history.state?.back) router.back(); else void router.replace('/me/support') }
</script>

<template>
  <div class="phone chatpage" data-testid="support-page">
    <header class="ch-bar">
      <button :aria-label="t('back')" data-testid="chat-back" @click="goBack">
        <svg viewBox="0 0 24 24" width="24" height="24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m15 5-7 7 7 7" /></svg></button>
      <b class="ch-ttl">{{ t('sp.title') }}<small v-if="platform?.support_hours">{{ t('sp.hours', { h: platform.support_hours }) }}</small></b>
      <span class="right row"><a v-if="phone" class="ch-call" :href="`tel:${phone}`" :aria-label="t('sp.call')" :title="t('sp.call')" data-testid="support-call">
        <svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M6.6 3.5h2.6l1.3 3.6-1.8 1.4a12 12 0 0 0 5.800 5.800l1.4-1.800 3.600 1.300v2.600a2 2 0 0 1-2.200 2A16.500 16.500 0 0 1 4.600 5.700a2 2 0 0 1 2-2.200z" /></svg></a></span>
    </header>
    <div v-if="error" class="banner bad" style="margin:12px">{{ error }}</div>
    <ChatPane v-if="info" :peer="info.peer" self="customer" :peer-read-at="info.peer_read_at" :blocked="info.enabled ? '' : t('ch.off')"
      upload-path="/customer/support/images" :orders="info.orders" :empty-text="t('sp.start')" @seen="seen" @order="o => o.token && router.push(`/o/${o.token}`)">
      <template #top>
        <div class="ch-tip" data-testid="support-tip">{{ t('sp.tip') }}</div>
      </template>
    </ChatPane>
  </div>
</template>
