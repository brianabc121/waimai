<script setup lang="ts">
// Who to turn to: the platform for the account, the store for an order.
import { onMounted, ref, watch } from 'vue'
import { pick, t } from '../../i18n'
import MePage from './MePage.vue'
import { me, myStores, platformInfo, type MyStore, type PlatformInfo } from './me'

const info = ref<PlatformInfo | null>(null)
const stores = ref<MyStore[]>([])
onMounted(async () => { info.value = await platformInfo().catch(() => null) })
watch(() => me.profile?.id, async id => { if (id) stores.value = (await myStores().catch(() => [])).filter(s => s.phone) }, { immediate: true })
const chatLink = (v: string) => /^https?:\/\//i.test(v) ? v : ''
</script>

<template>
  <MePage :title="t('ms.title')" open>
    <div class="card">
      <h2>{{ t('ms.platform') }}</h2>
      <div v-if="info && !info.support_phone && !info.support_email && !info.support_chat" class="note" style="margin-top:8px" data-testid="support-none">{{ t('ms.none') }}</div>
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
