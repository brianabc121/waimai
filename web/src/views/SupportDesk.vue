<script setup lang="ts">
// The store's conversation with platform support. Everyone at the desk shares it.
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { t } from '../i18n'
import { useLive } from '../live'
import { session } from '../session'
import { connect, describeChat, storeSupport, storeSupportRead, type ChatOrder, type SupportChat } from '../chat'
import ChatPane from './chat/ChatPane.vue'

const router = useRouter()
const bid = session.branchId!
const info = ref<SupportChat | null>(null)
const error = ref(''), readOnly = ref(false)
const me = computed(() => ({ uid: session.user!.id, name: session.user!.display_name || session.user!.username }))

async function load() {
  try { info.value = await storeSupport(bid) } catch (e) { error.value = (e as Error).message }
}
// the store's events also say when the platform has read or answered
useLive(() => `/merchant/branches/${bid}/events`, load)
onMounted(async () => {
  try { await connect(bid) } catch (e) {
    const code = (e as { code?: string }).code
    if (code === 'READ_ONLY') readOnly.value = true
    else error.value = describeChat(code ?? 'CHAT_DOWN')
  }
})
async function seen() { if (!readOnly.value) await storeSupportRead(bid).catch(() => null) }
const blocked = computed(() => readOnly.value ? t('admin_viewing') : info.value && !info.value.enabled ? t('ch.off') : '')
function openOrder(o: ChatOrder) { void router.push({ path: '/merchant/orders', query: { order: o.id } }) }
</script>

<template>
  <div class="spd" data-testid="store-support">
    <header class="spd-head">
      <span class="spd-ava">{{ t('sp.ava') }}</span>
      <span class="grow"><b>{{ t('sp.title') }}</b><span class="small muted">{{ t('sp.store_tip') }}</span></span>
    </header>
    <div v-if="error" class="banner bad" style="margin:12px" data-testid="support-error">{{ error }}</div>
    <ChatPane v-if="info" :peer="info.peer" self="store" :branch-id="bid" :staff="me" :peer-read-at="info.peer_read_at" :blocked="blocked" desk
      :upload-path="`/merchant/branches/${bid}/support/images`" :orders="info.orders" :empty-text="t('sp.store_start')"
      @seen="seen" @activity="load" @order="openOrder" />
  </div>
</template>

<style>
.spd { display: flex; flex-direction: column; height: calc(100dvh - var(--shell-top, 0px)); background: #f4f4f4; }
.spd-head { flex: none; display: flex; gap: 12px; align-items: center; background: #fff; border-bottom: 1px solid var(--line); padding: 12px 16px; }
.spd-head .grow { display: grid; gap: 2px; min-width: 0; }
.spd-ava { width: 40px; height: 40px; border-radius: 12px; background: #7c3aed; color: #fff; display: grid; place-items: center; font-weight: 700; flex: none; }
</style>
