<script setup lang="ts">
// "Report this conversation": a reason and a few words, for the platform.
import { ref } from 'vue'
import { post } from '../../api'
import { t } from '../../i18n'

const props = defineProps<{ path: string; title: string }>()
const emit = defineEmits<{ close: []; done: [] }>()
const reasons = ['harassment', 'spam', 'fraud', 'abuse', 'other']
const reason = ref(''), detail = ref(''), error = ref(''), busy = ref(false)
async function send() {
  error.value = ''; busy.value = true
  try { await post(props.path, { reason: reason.value, detail: detail.value }); emit('done') }
  catch (e) { error.value = (e as Error).message } finally { busy.value = false }
}
</script>

<template>
  <div class="sheet-mask" data-testid="chat-report" @click.self="emit('close')">
    <form class="sheet" @submit.prevent="send">
      <div class="sheet-head row between"><b>{{ title }}</b><button type="button" class="btn link" @click="emit('close')">{{ t('close') }}</button></div>
      <div class="sheet-body" style="display:grid;gap:10px">
        <div v-if="error" class="banner bad">{{ error }}</div>
        <label v-for="r in reasons" :key="r" class="row" style="gap:8px"><input v-model="reason" type="radio" :value="r" :data-testid="`chat-report-${r}`" /> {{ t('ch.rp_' + r) }}</label>
        <textarea v-model="detail" rows="3" maxlength="500" :placeholder="t('ch.rp_detail')" data-testid="chat-report-detail"></textarea>
        <p class="small muted" style="margin:0">{{ t('ch.rp_note') }}</p>
      </div>
      <div class="sheet-foot"><button class="btn primary block" :disabled="busy || !reason || (reason === 'other' && !detail.trim())" data-testid="chat-report-send">{{ t('ch.rp_send') }}</button></div>
    </form>
  </div>
</template>
