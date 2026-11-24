<script setup lang="ts">
// The platform's own sign-in page, apart from the stores' (docs/STAFF_ACCESS_DESIGN.md §5.5).
import LangButton from '../LangButton.vue'
import '../loginStrings'
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ApiError } from '../../api'
import { t } from '../../i18n'
import { adminLogin } from '../../session'

const router = useRouter()
const route = useRoute()
const username = ref('')
const password = ref('')
const error = ref('')
const busy = ref(false)

async function submit() {
  busy.value = true; error.value = ''
  try {
    await adminLogin(username.value, password.value)
    const next = typeof route.query.next === 'string' && route.query.next.startsWith('/admin/') ? route.query.next : '/admin'
    router.replace(next)
  } catch (e) {
    error.value = e instanceof ApiError && e.code === 'BAD_CREDENTIALS' ? t('bad_login') : (e as Error).message
  } finally { busy.value = false }
}
</script>

<template>
  <div class="admlogin">
    <form class="stack" data-testid="admin-login" @submit.prevent="submit">
      <div class="row between"><div><h1>{{ t('lg.admin_title') }}</h1><p class="small">{{ t('lg.admin_hint') }}</p></div>
        <LangButton class="lang" /></div>
      <div v-if="error" class="banner bad">{{ error }}</div>
      <label class="field"><span>{{ t('username') }}</span><input v-model="username" autocomplete="username" autocapitalize="none" required /></label>
      <label class="field"><span>{{ t('password') }}</span><input v-model="password" type="password" autocomplete="current-password" required /></label>
      <button class="btn primary block big" :disabled="busy">{{ t('sign_in') }}</button>
    </form>
  </div>
</template>

<style scoped>
.admlogin { min-height: 100vh; background: #12203a; display: flex; align-items: flex-start; justify-content: center; padding: 14vh 16px 40px; }
.admlogin form { width: min(400px, 100%); background: #fff; border-radius: 14px; padding: 28px; box-shadow: 0 10px 40px rgba(0,0,0,.35); border-top: 4px solid #2f5fd0; }
.admlogin h1 { font-size: 20px; color: #12203a; margin: 0; }
.admlogin p { margin: 2px 0 0; color: #5b6b85; }
.admlogin .btn.primary { background: #2f5fd0; border-color: #2f5fd0; }
</style>
