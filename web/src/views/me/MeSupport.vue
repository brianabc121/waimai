<script setup lang="ts">
// Who to turn to: the platform for the account, the store for an order.
import { onMounted, ref, watch } from 'vue'
import { pick, t } from '../../i18n'
import MeIntro from './MeIntro.vue'
import MePage from './MePage.vue'
import { me, myStores, platformInfo, type MyStore, type PlatformInfo } from './me'
import { chatEnabled, customerUnread } from '../../chat'

const info = ref<PlatformInfo | null>(null)
const stores = ref<MyStore[]>([])
// chatting with platform support, where this installation has chat
const online = ref(false), answers = ref(0)
onMounted(async () => {
  info.value = await platformInfo().catch(() => null)
  online.value = await chatEnabled()
  if (online.value) answers.value = (await customerUnread().catch(() => ({ support: 0 }))).support
})
watch(() => me.profile?.id, async id => { if (id) stores.value = (await myStores().catch(() => [])).filter(s => s.phone) }, { immediate: true })
const chatLink = (v: string) => /^https?:\/\//i.test(v) ? v : ''
</script>

<template>
  <MePage :title="t('ms.title')" open>
    <MeIntro :title="t('mi.support')" :sub="t('mi.support_sub')" :icon="'M5 13v-1a7 7 0 0 1 14 0v1M5 13h2.5v5H6a1 1 0 0 1-1-1zM19 13h-2.5v5H18a1 1 0 0 0 1-1zM16.5 18c0 1.7-1.5 2.5-3.5 2.5'" />
    <div class="card">
      <h2>{{ t('ms.platform') }}</h2>
      <router-link v-if="online" to="/me/support/chat" class="btn primary block sp-online" data-testid="support-online">{{ t('sp.online') }}
        <i v-if="answers" data-testid="support-online-unread">{{ answers > 99 ? '99+' : answers }}</i></router-link>
      <div v-if="info && !online && !info.support_phone && !info.support_email && !info.support_chat" class="note" style="margin-top:8px" data-testid="support-none">{{ t('ms.none') }}</div>
      <div v-else-if="info" class="me-rows" style="padding:0">
        <a v-if="info.support_phone" :href="'tel:' + info.support_phone.replace(/[^+\d]/g, '')" data-testid="support-phone"><span>{{ info.support_phone }}</span><span class="v"><span class="btn small primary">{{ t('ms.call') }}</span></span></a>
        <a v-if="info.support_email" :href="'mailto:' + info.support_email"><span>{{ t('ms.email') }}</span><span class="v">{{ info.support_email }}</span></a>
        <a v-if="info.support_chat && chatLink(info.support_chat)" :href="chatLink(info.support_chat)" target="_blank" rel="noopener"><span>{{ t('ms.chat') }}</span><span class="v"><span class="btn small">{{ t('ms.open') }}</span></span></a>
        <div v-else-if="info.support_chat" style="cursor:default"><span>{{ t('ms.chat') }}</span><span class="v">{{ info.support_chat }}</span></div>
        <div v-if="info.support_hours" style="cursor:default"><span>{{ t('ms.hours') }}</span><span class="v">{{ info.support_hours }}</span></div>
      </div>
      <router-link to="/me/feedback" class="btn block" style="margin-top:12px" data-testid="support-feedback">{{ t('ms.to_feedback') }}</router-link>
    </div>
    <div v-if="stores.length" class="card">
      <h2>{{ t('ms.stores') }}</h2>
      <p class="note">{{ t('ms.stores_hint') }}</p>
      <div class="me-rows" style="padding:0">
        <a v-for="s in stores" :key="s.slug" :href="'tel:' + s.phone.replace(/[^+\d]/g, '')" data-testid="support-store">
          <span class="grow"><b>{{ pick(s.name, s.name_en) }}</b><br /><span class="small muted">{{ s.phone }}</span></span>
          <span class="v"><span class="btn small">{{ t('ms.call') }}</span></span></a>
      </div>
    </div>
  </MePage>
</template>

<style scoped>
.sp-online { margin: 10px 0 4px; display: flex; align-items: center; justify-content: center; gap: 8px; }
.sp-online i { display: inline-grid; place-items: center; min-width: 18px; height: 18px; padding: 0 5px; border-radius: 9px; background: #fff; color: #e5352b; font-size: 11px; font-style: normal; font-weight: 700; }
</style>
