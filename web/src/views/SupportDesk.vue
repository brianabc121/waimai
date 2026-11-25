<script setup lang="ts">
// The store's conversation with platform support. Everyone at the desk shares it.
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { t } from '../i18n'
import { useLive } from '../live'
import { session } from '../session'
import { connect, describeChat, storeSupport, storeSupportRead, type ChatOrder, type SupportChat } from '../chat'
import '../officeStrings'
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
  <div class="ox warm">
  <div class="page ox-page spd-page">
    <div class="ox-head">
      <div><h1>{{ t('sp.title') }}</h1><p class="ox-sub">{{ t('ox.sp.sub') }}</p></div>
    </div>
  <div class="spd-body">
  <div class="spd" data-testid="store-support">
    <header class="spd-head">
      <span class="spd-ava">{{ t('sp.ava') }}</span>
      <span class="grow"><b>{{ t('sp.title') }}</b><span class="small muted">{{ t('ox.sp.shared') }}</span></span>
    </header>
    <div v-if="error" class="banner bad" style="margin:12px" data-testid="support-error">{{ error }}</div>
    <ChatPane v-if="info" :peer="info.peer" self="store" :branch-id="bid" :staff="me" :peer-read-at="info.peer_read_at" :blocked="blocked" desk
      :upload-path="`/merchant/branches/${bid}/support/images`" :orders="info.orders" :empty-text="t('sp.store_start')"
      @seen="seen" @activity="load" @order="openOrder" />
  </div>
  <aside class="spd-side">
    <div class="ox-panel">
      <h3>{{ t('ox.sp.can') }}</h3>
      <p class="small muted">{{ t('sp.store_tip') }}</p>
      <ul class="spd-list"><li v-for="k in ['account', 'money', 'menu', 'other']" :key="k">{{ t('ox.sp.k.' + k) }}</li></ul>
    </div>
    <div class="ox-tip"><b>{{ t('ox.sp.tip_h') }}</b>{{ t('ox.sp.tip') }}</div>
  </aside>
  </div>
  </div>
  </div>
</template>

<style>
.spd-page { grid-template-rows: auto minmax(0, 1fr); height: calc(100dvh - var(--shell-top, 0px)); }
.spd-body { display: grid; grid-template-columns: minmax(0, 1fr) 280px; gap: 14px; min-height: 0; }
.spd { display: flex; flex-direction: column; min-height: 0; background: #f3ece1; border: 1px solid var(--line); border-radius: 12px; overflow: hidden; }
.spd-head { flex: none; display: flex; gap: 12px; align-items: center; background: var(--card); border-bottom: 1px solid var(--line); padding: 12px 16px; }
.spd-head .grow { display: grid; gap: 2px; min-width: 0; }
.spd-side { display: grid; gap: 14px; align-content: start; }
.spd-side h3 { margin-top: 0; }
.spd-list { margin: 8px 0 0; padding-inline-start: 18px; display: grid; gap: 4px; font-size: 14px; }
@media (max-width: 1023px) { .spd-body { grid-template-columns: minmax(0, 1fr); } .spd-side { display: none; } }
@media (max-width: 767px) { .spd-page { padding: 0; gap: 0; } .spd-page .ox-head { padding: 12px; } .spd { border-radius: 0; border-width: 1px 0 0; } }
.spd-ava { width: 40px; height: 40px; border-radius: 12px; background: var(--brand); color: #fff; display: grid; place-items: center; font-weight: 700; flex: none; }
</style>
