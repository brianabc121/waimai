<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ApiError, get, post, put } from '../../api'
import { t } from '../../i18n'
import { fmtDateTime } from './util'

// Keys of outside services. A secret never comes back from the server: the
// page shows a masked hint and an empty box that replaces it when filled in.
interface Setting {
  key: string; group: string; secret: boolean; source: 'admin' | 'env' | 'none'
  value: string; unreadable: boolean; updated_at: string | null; updated_by: string
}
interface View {
  items: Setting[]
  active: { maps: string; routes: string; web_push: boolean; app_push: boolean; sms: string; translate: string; site?: string }
  otp_sent_today?: number
  weak_secret: boolean
}

const view = ref<View | null>(null)
const otpToday = ref<number | null>(null)
const draft = reactive<Record<string, string>>({})
const error = ref('')
const failed = ref('') // the group whose save was refused
const saved = ref('')
const busy = ref(false)
const groups = ['maps', 'web_push', 'app_push', 'sms', 'translate', 'site'] as const
type Group = typeof groups[number]

function take(v: View) {
  view.value = v
  if (v.otp_sent_today != null) otpToday.value = v.otp_sent_today
  for (const s of v.items) draft[s.key] = s.secret ? '' : s.value
}
onMounted(async () => {
  try { take(await get<View>('/admin/settings')) } catch (e) { error.value = (e as Error).message }
})

const byKey = computed(() => Object.fromEntries((view.value?.items ?? []).map(s => [s.key, s])))
// the SMS boxes follow the provider picked in the form, before it is saved
function shown(g: Group): Setting[] {
  const all = (view.value?.items ?? []).filter(s => s.group === g)
  if (g !== 'sms') return all
  const p = draft.SMS_PROVIDER || 'log'
  return all.filter(s => s.key === 'SMS_PROVIDER' || (p === 'webhook' && s.key.startsWith('SMS_WEBHOOK')) || (p === 'twilio' && s.key.startsWith('TWILIO')))
}
// what the form changes: a filled-in secret, or another value of a plain field
function changes(g: Group): Record<string, string> {
  const out: Record<string, string> = {}
  for (const s of shown(g)) {
    const v = (draft[s.key] ?? '').trim()
    if (s.secret ? v !== '' : v !== s.value) out[s.key] = v
  }
  return out
}
const dirty = (g: Group) => Object.keys(changes(g)).length > 0

async function send(g: Group, fn: () => Promise<View>) {
  if (busy.value) return
  busy.value = true
  saved.value = ''
  try { take(await fn()); error.value = ''; failed.value = ''; saved.value = g } catch (e) {
    const refused = e instanceof ApiError && (e.code === 'SETTINGS_INCOMPLETE' || e.code === 'VALIDATION')
    error.value = (refused ? t('ad.set.refused') : '') + (e as Error).message
    failed.value = g
  }
  busy.value = false
}
const save = (g: Group) => send(g, () => put<View>('/admin/settings', { values: changes(g) }))
function clear(s: Setting) {
  if (!confirm(t('ad.set.clear_confirm', { name: t('ad.set.k.' + s.key) }))) return
  void send(s.group as Group, () => put<View>('/admin/settings', { values: { [s.key]: '' } }))
}
function newPair() {
  if (view.value?.active.web_push && !confirm(t('ad.set.vapid_confirm'))) return
  void send('web_push', () => post<View>('/admin/settings/vapid'))
}

function state(g: Group): { on: boolean; text: string } {
  const a = view.value!.active
  if (g === 'maps') return { on: a.maps !== 'off', text: t('ad.set.maps_' + a.maps) + ' · ' + t('ad.set.routes_' + a.routes) }
  if (g === 'web_push') return { on: a.web_push, text: t(a.web_push ? 'ad.set.on' : 'ad.set.off') }
  if (g === 'app_push') return { on: a.app_push, text: t(a.app_push ? 'ad.set.on' : 'ad.set.off') }
  if (g === 'translate') return { on: a.translate !== 'off', text: t('ad.set.tr_' + a.translate) }
  if (g === 'site') return { on: !!a.site, text: a.site ? t('ad.set.site_on', { d: a.site }) : t('ad.set.off') }
  return { on: a.sms !== 'log', text: t('ad.set.sms_' + a.sms) }
}
</script>

<template>
  <div class="adm-page adm-set">
    <div class="banner info">{{ t('ad.set.intro') }}</div>
    <div v-if="view?.weak_secret" class="banner warn" data-testid="weak-secret">{{ t('ad.set.weak_secret') }}</div>
    <div v-if="error && !failed" class="banner bad">{{ error }}</div>
    <template v-if="view">
      <div v-for="g in groups" :key="g" class="card" :data-group="g">
        <div class="head">
          <div><h2>{{ t('ad.set.g.' + g) }}</h2><div class="small muted">{{ t('ad.set.g.' + g + '_hint') }}</div></div>
          <span class="tag" :class="state(g).on ? 'ok' : ''" data-testid="state">{{ state(g).text }}</span>
        </div>
        <div v-if="g === 'sms' && otpToday != null" class="small muted" data-testid="otp-today" style="margin-bottom:8px">{{ t('ad.set.otp_today', { n: otpToday }) }}</div>
        <div v-if="failed === g" class="banner bad" data-testid="set-error">{{ error }}</div>
        <div v-if="saved === g" class="banner ok" data-testid="set-saved">{{ t('ad.set.saved') }}</div>
        <div v-for="s in shown(g)" :key="s.key" class="set-row" :data-key="s.key">
          <div class="set-name">
            <div><b>{{ t('ad.set.k.' + s.key) }}</b></div>
            <div class="mono muted">{{ s.key }}</div>
          </div>
          <div class="set-now">
            <span v-if="s.unreadable" class="tag bad">{{ t('ad.set.unreadable') }}</span>
            <span v-else class="tag" :class="{ ok: s.source === 'admin', info: s.source === 'env' }" data-testid="source">{{ t('ad.set.src_' + s.source) }}</span>
            <span v-if="s.secret && s.value" class="mono" data-testid="mask">{{ s.value }}</span>
            <div v-if="s.source === 'admin' && s.updated_at" class="small muted">{{ t('ad.set.updated', { who: s.updated_by || '—', at: fmtDateTime(s.updated_at) }) }}</div>
          </div>
          <div class="set-edit">
            <select v-if="s.key === 'SMS_PROVIDER'" v-model="draft[s.key]">
              <option value="log">{{ t('ad.set.sms_log') }}</option>
              <option value="webhook">{{ t('ad.set.sms_webhook') }}</option>
              <option value="twilio">{{ t('ad.set.sms_twilio') }}</option>
            </select>
            <select v-else-if="s.key === 'TRANSLATE_PROVIDER'" v-model="draft[s.key]" data-testid="tr-provider">
              <option value="">{{ t('ad.set.tr_auto') }}</option>
              <option v-for="p in ['claude-haiku-5-5', 'claude-sonnet-5-5', 'google', 'off']" :key="p" :value="p">{{ t('ad.set.tr_' + p) }}</option>
            </select>
            <textarea v-else-if="s.key === 'FCM_SERVICE_ACCOUNT'" v-model="draft[s.key]" rows="3" spellcheck="false" :placeholder="t('ad.set.fcm_ph')"></textarea>
            <input v-else v-model="draft[s.key]" :type="s.secret ? 'password' : 'text'" autocomplete="new-password" spellcheck="false"
              :placeholder="s.secret ? t(s.source === 'none' ? 'ad.set.enter' : 'ad.set.replace') : s.key === 'STORE_SITE_DOMAIN' ? 'abcfood.com' : ''" />
            <button v-if="s.source === 'admin' || s.unreadable" class="btn small danger" :disabled="busy" data-testid="clear" @click="clear(s)">{{ t('ad.set.clear') }}</button>
          </div>
        </div>
        <div class="row" style="justify-content:flex-end;margin-top:12px">
          <button v-if="g === 'web_push'" class="btn" :disabled="busy" data-testid="new-pair" @click="newPair">{{ t('ad.set.new_pair') }}</button>
          <button class="btn primary" :disabled="busy || !dirty(g)" data-testid="save" @click="save(g)">{{ t('save') }}</button>
        </div>
      </div>
      <div class="small muted">{{ t('ad.set.not_here') }}</div>
    </template>
  </div>
</template>
