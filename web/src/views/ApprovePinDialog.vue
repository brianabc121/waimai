<script setup lang="ts">
// The manager's own approval code (docs/STAFF_ACCESS_DESIGN.md §7).
import { ref } from 'vue'
import { ApiError, put } from '../api'
import { t } from '../i18n'
import { reloadUser, session } from '../session'
import '../approve'
import './loginStrings'

const emit = defineEmits<{ (e: 'close'): void }>()
const pin = ref('')
const password = ref('')
const error = ref('')
const notice = ref('')
const busy = ref(false)

async function save() {
  busy.value = true; error.value = ''; notice.value = ''
  try {
    await put('/auth/approve-pin', { pin: pin.value, password: password.value })
    notice.value = t('ap.saved'); pin.value = ''; password.value = ''
    await reloadUser()
  } catch (e) {
    error.value = e instanceof ApiError && e.code === 'BAD_PASSWORD' ? t('lg.bad_password') : (e as Error).message
  } finally { busy.value = false }
}
</script>

<template>
  <div class="drawer-mask" @click="emit('close')"></div>
  <form class="approvebox card stack" data-testid="pin-dialog" @submit.prevent="save">
    <div class="row between"><h2>{{ t('ap.my') }}</h2>
      <span class="tag" :class="session.user?.has_approve_pin ? 'ok' : ''" data-testid="pin-state">{{ t(session.user?.has_approve_pin ? 'ap.is_set' : 'ap.not_set') }}</span></div>
    <p class="muted small" style="margin:0">{{ t('ap.my_hint') }}</p>
    <label class="field"><span>{{ t('ap.new_pin') }}</span>
      <input v-model="pin" class="pininput" inputmode="numeric" autocomplete="off" maxlength="6" pattern="[0-9]{6}" required data-testid="pin-new" /></label>
    <label class="field"><span>{{ t('ap.password') }}</span>
      <input v-model="password" type="password" autocomplete="current-password" required data-testid="pin-password" /></label>
    <div v-if="error" class="banner bad">{{ error }}</div>
    <div v-if="notice" class="banner ok" data-testid="pin-saved">{{ notice }}</div>
    <div class="row" style="justify-content:flex-end;gap:8px">
      <button type="button" class="btn" @click="emit('close')">{{ t('ap.cancel') }}</button>
      <button class="btn primary" :disabled="busy || pin.length !== 6 || !password" data-testid="pin-save">{{ t('ap.save') }}</button>
    </div>
  </form>
</template>

<style scoped>
.drawer-mask { position: fixed; inset: 0; background: rgba(0,0,0,.35); z-index: 40; }
.approvebox { position: fixed; left: 50%; top: 14vh; transform: translateX(-50%); width: min(400px, calc(100vw - 32px)); z-index: 41; }
.approvebox h2 { margin: 0; font-size: 17px; }
.pininput { letter-spacing: 10px; font-size: 22px; text-align: center; font-variant-numeric: tabular-nums; }
</style>
