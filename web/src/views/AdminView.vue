<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { get, post } from '../api'
import { t } from '../i18n'
import { logout } from '../session'

const router = useRouter()
const merchants = ref<{ merchant_id: number; merchant_name: string; branch_name: string; slug: string }[]>([])
const error = ref('')
const f = reactive({ merchant_name: '', branch_name: '', branch_name_en: '', slug: '', timezone: 'Asia/Manila', currency: 'PHP',
  owner: { username: '', password: '', display_name: '' } })

async function load() { merchants.value = (await get('/admin/merchants')).merchants }
onMounted(() => load().catch(e => (error.value = e.message)))

async function create() {
  error.value = ''
  try {
    await post('/admin/merchants', f)
    Object.assign(f, { merchant_name: '', branch_name: '', branch_name_en: '', slug: '', owner: { username: '', password: '', display_name: '' } })
    await load()
  } catch (e) { error.value = (e as Error).message }
}
async function out() { await logout(); router.replace('/login') }
</script>

<template>
  <div>
    <nav class="staffbar"><b>{{ t('merchants') }}</b><span class="grow"></span><button class="btn link" @click="out">{{ t('sign_out') }}</button></nav>
    <div class="page stack" style="max-width:900px">
      <div v-if="error" class="banner bad">{{ error }}</div>
      <div class="card" style="padding:0">
        <table class="data">
          <thead><tr><th>{{ t('merchant_name') }}</th><th>{{ t('branch_name') }}</th><th>{{ t('share_link') }}</th></tr></thead>
          <tbody><tr v-for="m in merchants" :key="m.slug"><td>{{ m.merchant_name }}</td><td>{{ m.branch_name }}</td>
            <td><a :href="'/s/' + m.slug" target="_blank">/s/{{ m.slug }}</a></td></tr></tbody>
        </table>
      </div>
      <form class="card" @submit.prevent="create">
        <h2 style="margin-bottom:12px">{{ t('new_merchant') }}</h2>
        <div style="display:grid;grid-template-columns:1fr 1fr;gap:0 12px">
          <label class="field"><span>{{ t('merchant_name') }}</span><input v-model="f.merchant_name" required /></label>
          <label class="field"><span>{{ t('slug') }} (a-z, 0-9, -)</span><input v-model="f.slug" required pattern="[a-z0-9][a-z0-9-]{1,40}" /></label>
          <label class="field"><span>{{ t('branch_name') }}</span><input v-model="f.branch_name" required /></label>
          <label class="field"><span>{{ t('name_en') }}</span><input v-model="f.branch_name_en" /></label>
          <label class="field"><span>Timezone</span><input v-model="f.timezone" required /></label>
          <label class="field"><span>Currency</span><input v-model="f.currency" required maxlength="3" /></label>
          <label class="field"><span>{{ t('owner_account') }} · {{ t('username') }}</span><input v-model="f.owner.username" required autocapitalize="none" /></label>
          <label class="field"><span>{{ t('display_name') }}</span><input v-model="f.owner.display_name" required /></label>
          <label class="field"><span>{{ t('password') }} (8+)</span><input v-model="f.owner.password" type="password" required minlength="8" autocomplete="new-password" /></label>
        </div>
        <button class="btn primary">{{ t('create') }}</button>
      </form>
    </div>
  </div>
</template>
