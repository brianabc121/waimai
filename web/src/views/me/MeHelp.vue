<script setup lang="ts">
// Questions about the account, answered; the rest goes to feedback.
import { ref } from 'vue'
import { t } from '../../i18n'
import MePage from './MePage.vue'

const open = ref(1)
</script>

<template>
  <MePage :title="t('mh.title')" open>
    <div class="card faq">
      <div v-for="n in 6" :key="n" class="qa" :class="{ on: open === n }">
        <button data-testid="help-q" @click="open = open === n ? 0 : n"><span>{{ t(`mh.q${n}`) }}</span><i>›</i></button>
        <p v-if="open === n" data-testid="help-a">{{ t(`mh.a${n}`) }}</p>
      </div>
    </div>
    <div class="card row between"><span>{{ t('mh.still') }}</span>
      <router-link to="/me/feedback" class="btn small primary" data-testid="help-feedback">{{ t('mh.ask') }}</router-link></div>
  </MePage>
</template>

<style scoped>
.faq { padding: 2px 14px; }
.qa { border-bottom: 1px solid #f0f1f3; } .qa:last-child { border-bottom: 0; }
.qa button { display: flex; align-items: center; justify-content: space-between; gap: 10px; width: 100%; border: 0; background: none; padding: 14px 0; text-align: left; cursor: pointer; font-weight: 600; }
.qa i { font-style: normal; color: #b8bec7; transition: transform .15s; } .qa.on i { transform: rotate(90deg); }
.qa p { color: #4b5563; font-size: 14px; line-height: 1.7; padding-bottom: 14px; }
</style>
