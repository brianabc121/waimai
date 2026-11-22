<script setup lang="ts">
// Asking to open a store on the platform. The platform reviews it and gets in
// touch; the answer also arrives as a message.
import { computed, reactive, ref, watch } from 'vue'
import { ApiError } from '../../api'
import { t } from '../../i18n'
import MeIntro from './MeIntro.vue'
import MePage from './MePage.vue'
import { dateTime, describe, me, myApplications, sendApplication, type Application } from './me'

const list = ref<Application[]>([])
const f = reactive({ store_name: '', contact_name: '', phone: '', city: '', cuisine: '', note: '' })
const error = ref(''), sent = ref(false), busy = ref(false)
const pending = computed(() => list.value.some(a => a.status === 'pending'))
watch(() => me.profile?.id, async id => { if (id) list.value = await myApplications().catch(() => []) }, { immediate: true })

async function submit() {
  error.value = ''; sent.value = false
  if (!f.store_name.trim() || !f.contact_name.trim() || !f.phone.trim()) { error.value = t('ma.e.required'); return }
  busy.value = true
  try {
    list.value.unshift(await sendApplication(f))
    Object.assign(f, { store_name: '', contact_name: '', phone: '', city: '', cuisine: '', note: '' })
    sent.value = true
  } catch (e) { error.value = e instanceof ApiError && e.code === 'APPLICATION_PENDING' ? t('ma.pending') : describe(e) }
  busy.value = false
}
</script>

<template>
  <MePage :title="t('ma.title')">
    <MeIntro :title="t('mi.apply')" :sub="t('mi.apply_sub')" :icon="'M5 10.5V19h14v-8.5M3.5 7 5 4h14l1.5 3v1a2.2 2.2 0 0 1-4.25.8 2.2 2.2 0 0 1-4.25 0 2.2 2.2 0 0 1-4.25 0A2.2 2.2 0 0 1 3.5 8zM9.5 19v-4.5h5V19'" />
    <div v-if="sent" class="banner ok" data-testid="apply-sent">{{ t('ma.sent') }}</div>
    <div v-if="pending" class="banner info" data-testid="apply-pending">{{ t('ma.pending') }}</div>
    <form v-else class="card" data-testid="apply-form" @submit.prevent="submit">
      <div v-if="error" class="banner bad" style="margin-bottom:12px" data-testid="apply-error">{{ error }}</div>
      <label class="field"><span>{{ t('ma.store') }} *</span><input v-model="f.store_name" maxlength="100" data-testid="apply-store" /></label>
      <label class="field"><span>{{ t('ma.contact') }} *</span><input v-model="f.contact_name" maxlength="80" data-testid="apply-contact" /></label>
      <label class="field"><span>{{ t('ma.phone') }} *</span><input v-model="f.phone" maxlength="40" inputmode="tel" data-testid="apply-phone" /></label>
      <label class="field"><span>{{ t('ma.city') }}</span><input v-model="f.city" maxlength="100" data-testid="apply-city" /></label>
      <label class="field"><span>{{ t('ma.cuisine') }}</span><input v-model="f.cuisine" maxlength="100" :placeholder="t('ma.cuisine_ph')" /></label>
      <label class="field"><span>{{ t('ma.note') }}</span><textarea v-model="f.note" rows="3" maxlength="1000"></textarea></label>
      <button class="btn primary block big" :disabled="busy" data-testid="apply-submit">{{ t('ma.submit') }}</button>
    </form>
    <template v-if="list.length">
      <h3 class="me-sec">{{ t('ma.mine') }}</h3>
      <div v-for="a in list" :key="a.id" class="card stack" data-testid="application" :data-status="a.status">
        <div class="row between"><b>{{ a.store_name }}</b>
          <span class="tag" :class="{ ok: a.status === 'approved', bad: a.status === 'rejected', warn: a.status === 'pending' }">{{ t('ma.s_' + a.status) }}</span></div>
        <div class="small muted">{{ a.contact_name }} · {{ a.phone }}<template v-if="a.city"> · {{ a.city }}</template> · {{ dateTime(a.created_at) }}</div>
        <div v-if="a.reply" class="reply"><b>{{ t('ma.reply') }}</b>：{{ a.reply }}</div>
      </div>
    </template>
  </MePage>
</template>

<style scoped>
.reply { background: var(--bg); border-radius: 10px; padding: 10px 12px; font-size: 14px; white-space: pre-wrap; }
</style>
