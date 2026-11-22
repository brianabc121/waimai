<script setup lang="ts">
// A note to the platform, and what it answered.
import { reactive, ref, watch } from 'vue'
import { t } from '../../i18n'
import MePage from './MePage.vue'
import { dateTime, describe, me, myFeedback, sendFeedback, type Feedback } from './me'

const kinds = ['suggestion', 'problem', 'complaint', 'account', 'other']
const list = ref<Feedback[]>([])
const f = reactive({ kind: 'suggestion', content: '', contact: '' })
const error = ref(''), sent = ref(false), busy = ref(false)
watch(() => me.profile?.id, async id => { if (id) list.value = await myFeedback().catch(() => []) }, { immediate: true })

async function submit() {
  error.value = ''; sent.value = false
  if (!f.content.trim()) { error.value = t('mk.e.content'); return }
  busy.value = true
  try {
    list.value.unshift(await sendFeedback(f))
    f.content = ''
    sent.value = true
  } catch (e) { error.value = describe(e) }
  busy.value = false
}
</script>

<template>
  <MePage :title="t('mk.title')">
    <form class="card" data-testid="feedback-form" @submit.prevent="submit">
      <div v-if="sent" class="banner ok" style="margin-bottom:12px" data-testid="feedback-sent">{{ t('mk.sent') }}</div>
      <div v-if="error" class="banner bad" style="margin-bottom:12px" data-testid="feedback-error">{{ error }}</div>
      <div class="field"><span class="small muted">{{ t('mk.kind') }}</span>
        <div class="chips"><button v-for="k in kinds" :key="k" type="button" :class="{ on: f.kind === k }" :data-kind="k" @click="f.kind = k">{{ t('mk.' + k) }}</button></div></div>
      <label class="field"><span>{{ t('mk.content') }}</span><textarea v-model="f.content" rows="5" maxlength="1000" :placeholder="t('mk.content_ph')" data-testid="feedback-content"></textarea></label>
      <label class="field"><span>{{ t('mk.contact') }}</span><input v-model="f.contact" maxlength="100" :placeholder="t('mk.contact_ph')" data-testid="feedback-contact" /></label>
      <button class="btn primary block big" :disabled="busy" data-testid="feedback-submit">{{ t('mk.submit') }}</button>
    </form>
    <template v-if="list.length">
      <h3 style="padding:4px 6px 0">{{ t('mk.mine') }}</h3>
      <div v-for="x in list" :key="x.id" class="card stack" data-testid="feedback-item">
        <div class="row between"><span class="tag">{{ t('mk.' + x.kind) }}</span><span class="small muted">{{ dateTime(x.created_at) }}</span></div>
        <p style="white-space:pre-wrap">{{ x.content }}</p>
        <div v-if="x.reply" class="reply" data-testid="feedback-reply"><b>{{ t('mk.replied') }}</b>：{{ x.reply }}</div>
        <div v-else class="small muted">{{ t('mk.waiting') }}</div>
      </div>
    </template>
  </MePage>
</template>

<style scoped>
.field { margin-bottom: 12px; }
.chips { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 6px; }
.chips button { border: 1px solid var(--line); background: #fff; border-radius: 999px; padding: 6px 14px; cursor: pointer; font-size: 14px; }
.chips button.on { background: var(--brand-soft); border-color: var(--brand); color: var(--brand-dark); font-weight: 600; }
.reply { background: #f6f7f8; border-radius: 10px; padding: 10px 12px; font-size: 14px; white-space: pre-wrap; }
</style>
