<script setup lang="ts">
// What the platform says about itself, and the papers.
import { onMounted, ref } from 'vue'
import { pick, t } from '../../i18n'
import MeIntro from './MeIntro.vue'
import MePage from './MePage.vue'
import { platformInfo, type PlatformInfo } from './me'

const info = ref<PlatformInfo | null>(null)
onMounted(async () => { info.value = await platformInfo().catch(() => null) })
</script>

<template>
  <MePage :title="t('mb.title')" open layout="split">
    <MeIntro :title="info?.name || t('mb.title')" :sub="t('mi.about_sub')" :icon="'M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18zM12 11v5.5M12 7.8h.01'" />
    <div class="card stack main">
      <p style="white-space:pre-wrap;line-height:1.7" data-testid="about-text">{{ (info && pick(info.about, info.about_en)) || t('mb.default') }}</p>
    </div>
    <div class="card me-rows side">
      <a href="/terms.html" target="_blank"><span>{{ t('mp.terms') }}</span><span class="v chev">›</span></a>
      <a href="/privacy.html" target="_blank"><span>{{ t('mp.privacy') }}</span><span class="v chev">›</span></a>
      <router-link to="/me/support"><span>{{ t('me.support') }}</span><span class="v chev">›</span></router-link>
    </div>
  </MePage>
</template>
