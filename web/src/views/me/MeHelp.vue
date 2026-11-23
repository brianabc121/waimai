<script setup lang="ts">
// Questions about the account, answered; the rest goes to feedback.
import { ref } from 'vue'
import { t } from '../../i18n'
import MeIntro from './MeIntro.vue'
import MePage from './MePage.vue'

const open = ref(1)
</script>

<template>
  <MePage :title="t('mh.title')" open layout="split">
    <MeIntro :title="t('mi.help')" :sub="t('mi.help_sub')" :icon="'M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18zM9.5 9.5a2.5 2.5 0 1 1 3.7 2.2c-.8.5-1.2 1-1.2 1.9M12 16.8h.01'" />
    <div class="card faq main">
      <div v-for="n in 6" :key="n" class="qa" :class="{ on: open === n }">
        <button data-testid="help-q" @click="open = open === n ? 0 : n"><span>{{ t(`mh.q${n}`) }}</span><i>›</i></button>
        <p v-if="open === n" data-testid="help-a">{{ t(`mh.a${n}`) }}</p>
      </div>
    </div>
    <div class="card row between side"><b>{{ t('mh.still') }}</b>
      <router-link to="/me/feedback" class="btn small primary" data-testid="help-feedback">{{ t('mh.ask') }}</router-link></div>
  </MePage>
</template>

<style scoped>
.faq { padding: 2px 14px; }
.qa { border-bottom: 1px solid var(--line); } .qa:last-child { border-bottom: 0; }
.qa button { display: flex; align-items: center; justify-content: space-between; gap: 10px; width: 100%; border: 0; background: none; padding: 12px 0; text-align: left; cursor: pointer; font-weight: 400; font-size: 14px; color: var(--ink); }
.qa.on button { color: var(--red); font-weight: 500; }
.qa i { font-style: normal; color: #b9a793; font-size: 17px; line-height: 1; transition: transform .15s; } .qa.on i { transform: rotate(90deg); }
.qa p { color: var(--muted); background: var(--bg); border-radius: 10px; padding: 10px 12px !important; margin-bottom: 10px; font-size: 13px; line-height: 1.65; padding-bottom: 14px; }
</style>
