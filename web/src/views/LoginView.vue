<script setup lang="ts">
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ApiError } from '../api'
import { locale, setLocale, t } from '../i18n'
import { homeFor, login, session } from '../session'

const router = useRouter()
const route = useRoute()
const username = ref('')
const password = ref('')
const error = ref('')
const busy = ref(false)

async function submit() {
  busy.value = true; error.value = ''
  try {
    await login(username.value, password.value)
    const next = typeof route.query.next === 'string' && route.query.next.startsWith('/') ? route.query.next : homeFor(session.user!)
    router.replace(next)
  } catch (e) {
    error.value = e instanceof ApiError && e.code === 'BAD_CREDENTIALS' ? t('bad_login') : (e as Error).message
  } finally { busy.value = false }
}
</script>

<template>
  <div class="phone login">
    <form class="section stack" @submit.prevent="submit">
      <div class="row between"><h1>{{ t('sign_in') }}</h1>
        <button type="button" class="lang" @click="setLocale(locale === 'zh' ? 'en' : 'zh')">{{ locale === 'zh' ? 'EN' : '中文' }}</button></div>
      <div v-if="error" class="banner bad">{{ error }}</div>
      <label class="field"><span>{{ t('username') }}</span><input v-model="username" autocomplete="username" autocapitalize="none" required /></label>
      <label class="field"><span>{{ t('password') }}</span><input v-model="password" type="password" autocomplete="current-password" required /></label>
      <button class="btn primary block big" :disabled="busy">{{ t('sign_in') }}</button>
    </form>
  </div>
</template>
