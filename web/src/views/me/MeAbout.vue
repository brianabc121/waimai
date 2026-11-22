<script setup lang="ts">
// What the platform says about itself, and the papers.
import { onMounted, ref } from 'vue'
import { pick, t } from '../../i18n'
import MePage from './MePage.vue'
import { platformInfo, type PlatformInfo } from './me'

const info = ref<PlatformInfo | null>(null)
onMounted(async () => { info.value = await platformInfo().catch(() => null) })
</script>

<template>
  <MePage :title="t('mb.title')" open>
    <div class="card stack">
      <h2 v-if="info?.name">{{ info.name }}</h2>
      <p style="white-space:pre-wrap;line-height:1.7" data-testid="about-text">{{ (info && pick(info.about, info.about_en)) || t('mb.default') }}</p>
    </div>
    <div class="card me-rows">
      <a href="/terms.html" target="_blank"><span>{{ t('mp.terms') }}</span><span class="v chev">›</span></a>
      <a href="/privacy.html" target="_blank"><span>{{ t('mp.privacy') }}</span><span class="v chev">›</span></a>
      <router-link to="/me/support"><span>{{ t('me.support') }}</span><span class="v chev">›</span></router-link>
    </div>
  </MePage>
</template>
