<script setup lang="ts">
import LangButton from '../LangButton.vue'
// Profile and settings: the picture, the nickname, the language, logging out.
import { ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { customerLogout, prettyPhone } from '../../customer'
import { locale, setLocale, t } from '../../i18n'
import { shrink } from '../../photo'
import MePage from './MePage.vue'
import { avatarUrl, describe, me, removeAvatar, saveAvatar, saveNickname } from './me'

const router = useRouter()
const nickname = ref('')
const error = ref(''), saved = ref(false), busy = ref(false)
watch(() => me.profile, p => { if (p) nickname.value = p.nickname }, { immediate: true })

async function run(fn: () => Promise<void>) {
  error.value = ''; saved.value = false; busy.value = true
  try { await fn(); saved.value = true; setTimeout(() => { saved.value = false }, 2000) } catch (e) { error.value = describe(e) }
  busy.value = false
}
async function pickPhoto(ev: Event) {
  const input = ev.target as HTMLInputElement, file = input.files?.[0]
  input.value = ''
  if (!file) return
  let photo: Blob
  try { photo = await shrink(file) } catch { error.value = t('mp.photo_failed'); return }
  await run(() => saveAvatar(photo))
}
async function logout() {
  if (!confirm(t('mp.logout_confirm'))) return
  if (!(await customerLogout())) return
  me.profile = null
  await router.replace('/me')
}
</script>

<template>
  <MePage :title="t('mp.title')" layout="narrow">
    <template v-if="me.profile">
      <div v-if="error" class="banner bad" data-testid="profile-error">{{ error }}</div>
      <div class="card me-rows">
        <label data-testid="profile-avatar">
          <span>{{ t('mp.avatar') }}</span>
          <span class="v">
            <img v-if="avatarUrl(me.profile)" :src="avatarUrl(me.profile)" alt="" class="pic" data-testid="profile-avatar-img" />
            <span v-else class="pic ph"></span>
            <button v-if="me.profile.avatar_at" type="button" class="btn link small" data-testid="profile-avatar-remove" @click.prevent="run(removeAvatar)">{{ t('mp.remove') }}</button>
            <span class="btn small">{{ t('mp.change') }}</span>
            <input type="file" accept="image/*" hidden @change="pickPhoto" />
          </span>
        </label>
        <form style="cursor:default" @submit.prevent="run(() => saveNickname(nickname))">
          <span style="white-space:nowrap">{{ t('mp.nickname') }}</span>
          <input v-model="nickname" maxlength="20" :placeholder="t('mp.nickname_ph')" data-testid="profile-nickname" />
          <button class="btn small primary" :disabled="busy || nickname.trim() === me.profile.nickname" data-testid="profile-save">{{ t(saved ? 'mp.saved' : 'mp.save') }}</button>
        </form>
        <div style="cursor:default"><span>{{ t('mp.phone') }}</span><span class="v">{{ prettyPhone(me.profile.phone) }}</span></div>
        <div style="cursor:default"><span>{{ t('mp.member') }}</span><span class="v">{{ me.profile.member_no }}</span></div>
      </div>
      <p class="note" style="padding:0 6px">{{ t('mp.phone_hint') }}</p>

      <div class="card me-rows">
        <div style="cursor:default"><span>{{ t('mp.language') }}</span>
          <span class="v"><LangButton class="btn small" /></span></div>
        <a href="/terms.html" target="_blank"><span>{{ t('mp.terms') }}</span><span class="v chev"><span class="rtl-flip">›</span></span></a>
        <a href="/privacy.html" target="_blank"><span>{{ t('mp.privacy') }}</span><span class="v chev"><span class="rtl-flip">›</span></span></a>
      </div>
      <button class="btn block big" style="color:var(--bad)" data-testid="profile-logout" @click="logout">{{ t('mp.logout') }}</button>
    </template>
  </MePage>
</template>

<style scoped>
.pic { width: 44px; height: 44px; border-radius: 50%; object-fit: cover; background: #eceef1; }
.me-rows form input { flex: 1; min-width: 0; }
</style>
