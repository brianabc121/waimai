<script setup lang="ts">
// Customer login: a mobile number and a texted code, nothing else. A number
// that has no account yet gets one when the code checks out.
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ApiError, get, post } from '../api'
import { loadCustomer, prettyPhone } from '../customer'
import { extend, locale, pick, setLocale, t } from '../i18n'
import { defaultTemplate, themeVars } from '../theme'
import type { Storefront } from '../types'

extend({
  'al.title': ['手机号登录 / 注册', 'Log in or sign up'],
  'al.hint': ['没有账号的手机号，验证通过后自动注册', 'A new number gets an account once it is verified'],
  'al.continue': ['登录后继续下单', 'Log in to continue your order'],
  'al.phone_ph': ['手机号', 'Mobile number'], 'al.code_ph': ['请输入验证码', 'Enter the code'],
  'al.get_code': ['获取验证码', 'Get code'], 'al.resend_in': ['{n} 秒后可重发', 'Resend in {n}s'], 'al.resend': ['重新发送', 'Resend'],
  'al.sent': ['验证码已发到 {phone}，5 分钟内有效。', 'We texted a code to {phone}. It is valid for 5 minutes.'],
  'al.agree': ['我已阅读并同意', 'I have read and agree to the '], 'al.terms': ['《用户服务协议》', 'Terms of Service'],
  'al.and': ['和', ' and '], 'al.privacy': ['《隐私政策》', 'Privacy Policy'],
  'al.need_agree': ['请先阅读并勾选同意协议', 'Please tick the box to agree first'],
  'al.login': ['登录', 'Log in'], 'al.logging': ['登录中…', 'Logging in…'], 'al.clear': ['清空', 'Clear'],
  'al.e.phone': ['请输入正确的 +63 手机号，例如 917 555 0123', 'Enter a valid +63 mobile number, e.g. 917 555 0123'],
  'al.e.code': ['验证码不对或已过期，请重新获取', 'That code is wrong or has expired. Get a new one.'],
  'al.e.wait': ['发送太频繁，请一分钟后再试', 'Please wait a minute before asking for another code'],
  'al.e.daily': ['今天获取验证码的次数已用完，请明天再试', 'No more codes today. Please try again tomorrow.'],
  'al.e.sms': ['暂时发不了验证码，请联系门店', 'We cannot send a code right now. Please contact the store.'],
  'al.e.busy': ['操作太频繁，请稍后再试', 'Too many attempts. Please try again shortly.'],
})

const route = useRoute(), router = useRouter()
const next = computed(() => typeof route.query.next === 'string' && route.query.next.startsWith('/') && !route.query.next.startsWith('//') ? route.query.next : '')
const slug = computed(() => typeof route.query.store === 'string' ? route.query.store : '')
const store = ref<Storefront | null>(null)
const tpl = computed(() => store.value?.templates?.delivery ?? defaultTemplate('delivery'))
const name = computed(() => store.value ? pick(store.value.branch.name, store.value.branch.name_en) : '')

const phone = ref((localStorage.getItem('phone') || '').replace(/^\+63/, ''))
const code = ref('')
const agreed = ref(false)
const sentTo = ref(''), devCode = ref(''), error = ref('')
const wait = ref(0), busy = ref(false)
let timer = 0

// What is sent: the national number without spaces or a leading zero.
const local = computed(() => phone.value.replace(/[^0-9]/g, '').replace(/^63(?=9\d{9}$)/, '').replace(/^0/, ''))
const phoneOk = computed(() => /^9\d{9}$/.test(local.value))
const full = computed(() => '+63' + local.value)

function describe(e: unknown): string {
  if (!(e instanceof ApiError)) return String(e)
  switch (e.code) {
    case 'NETWORK': return t('offline')
    case 'PHONE_INVALID': case 'VALIDATION': return t('al.e.phone')
    case 'OTP_INVALID': return t('al.e.code')
    case 'OTP_DAILY_LIMIT': return t('al.e.daily')
    case 'SMS_FAILED': return t('al.e.sms')
    case 'OTP_WAIT': return t('al.e.wait')
    case 'RATE_LIMITED': return t('al.e.busy')
    default: return e.message
  }
}

function countdown(n: number) {
  wait.value = n
  clearInterval(timer)
  timer = window.setInterval(() => { if (--wait.value <= 0) clearInterval(timer) }, 1000)
}

async function sendCode() {
  error.value = ''
  if (!phoneOk.value) { error.value = t('al.e.phone'); return }
  if (!agreed.value) { error.value = t('al.need_agree'); return }
  if (wait.value > 0 || busy.value) return
  busy.value = true
  try {
    const r = await post<{ dev_code?: string }>('/otp/request', { phone: full.value })
    sentTo.value = full.value
    localStorage.setItem('phone', full.value)
    countdown(60)
    devCode.value = r.dev_code ?? ''
    code.value = r.dev_code ?? ''
  } catch (e) {
    error.value = describe(e)
    // A code sent less than a minute ago is still good: let them type it.
    if (e instanceof ApiError && e.code === 'OTP_WAIT') { sentTo.value = full.value; countdown(60) }
  } finally { busy.value = false }
}

async function login() {
  if (busy.value || code.value.length !== 6 || !phoneOk.value) return
  if (!agreed.value) { error.value = t('al.need_agree'); return }
  busy.value = true; error.value = ''
  try {
    await post('/otp/verify', { phone: full.value, code: code.value })
    await loadCustomer(true)
    router.replace(next.value || (slug.value ? `/s/${slug.value}` : '/'))
  } catch (e) {
    error.value = describe(e)
    code.value = ''
  } finally { busy.value = false }
}
// Six digits is the whole code: no need to press the button.
watch(code, c => { if (c.length === 6 && sentTo.value) void login() })

function back() {
  if (slug.value) router.replace(`/s/${slug.value}`)
  else router.back()
}

onMounted(async () => {
  if (await loadCustomer(true)) { router.replace(next.value || (slug.value ? `/s/${slug.value}` : '/')); return }
  if (slug.value) store.value = await get<Storefront>(`/stores/${slug.value}`).catch(() => null)
})
onBeforeUnmount(() => clearInterval(timer))
</script>

<template>
  <div class="alogin" :style="themeVars(tpl)">
    <div class="acard">
      <div class="hero">
        <div class="row between">
          <button class="backbtn" :aria-label="t('back')" data-testid="login-back" @click="back">‹</button>
          <button class="lang" @click="setLocale(locale === 'zh' ? 'en' : 'zh')">{{ locale === 'zh' ? 'EN' : '中文' }}</button>
        </div>
        <div class="shop" v-if="store">
          <img v-if="tpl.logo_url" :src="tpl.logo_url" alt="" class="logo" />
          <div v-else class="logo ph">{{ name.slice(0, 1) }}</div>
          <div><div class="sname">{{ name }}</div><div class="small muted">{{ t('al.continue') }}</div></div>
        </div>
      </div>
      <form class="body" @submit.prevent="login">
        <h1>{{ t('al.title') }}</h1>
        <div class="small muted">{{ t('al.hint') }}</div>
        <label class="inp">
          <span class="cc">+63</span>
          <input v-model="phone" type="tel" inputmode="tel" autocomplete="tel-national" maxlength="16" :placeholder="t('al.phone_ph')" data-testid="login-phone" />
          <button v-if="phone" type="button" class="x" :aria-label="t('al.clear')" @click="phone = ''">×</button>
        </label>
        <label class="inp">
          <input v-model="code" inputmode="numeric" maxlength="6" autocomplete="one-time-code" :placeholder="t('al.code_ph')" data-testid="login-code"
            @input="code = code.replace(/[^0-9]/g, '')" />
          <button type="button" class="act" :class="{ wait: wait > 0 }" :disabled="wait > 0 || busy" data-testid="login-send" @click="sendCode">
            {{ wait > 0 ? t('al.resend_in', { n: wait }) : sentTo ? t('al.resend') : t('al.get_code') }}</button>
        </label>
        <div v-if="error" class="banner bad" data-testid="login-error">{{ error }}</div>
        <div v-else-if="sentTo" class="banner info small" data-testid="login-sent">{{ t('al.sent', { phone: prettyPhone(sentTo) }) }}</div>
        <div v-if="devCode" class="banner info small">{{ t('dev_code') }}: {{ devCode }}</div>
        <div class="grow"></div>
        <label class="agree"><input v-model="agreed" type="checkbox" data-testid="login-agree" />
          <span>{{ t('al.agree') }}<a href="/terms.html" target="_blank" rel="noopener">{{ t('al.terms') }}</a>{{ t('al.and') }}<a href="/privacy.html" target="_blank" rel="noopener">{{ t('al.privacy') }}</a></span></label>
        <button class="btn primary block big" :disabled="busy || code.length !== 6 || !phoneOk" data-testid="login-submit">{{ busy ? t('al.logging') : t('al.login') }}</button>
      </form>
    </div>
  </div>
</template>

<style scoped>
.alogin { min-height: 100vh; background: var(--bg); display: flex; justify-content: center; }
.acard { width: 100%; max-width: 520px; background: #fff; display: flex; flex-direction: column; min-height: 100vh; }
.hero { background: linear-gradient(160deg, var(--brand-soft), #fff 90%); padding: 14px 16px 40px; min-height: 190px; display: flex; flex-direction: column; justify-content: space-between; }
.backbtn { border: 0; background: none; font-size: 30px; line-height: 1; width: 40px; height: 40px; cursor: pointer; color: var(--ink); }
.shop { display: flex; gap: 14px; align-items: center; padding: 0 12px; }
.logo { width: 60px; height: 60px; border-radius: 16px; object-fit: cover; flex: none; }
.logo.ph { background: var(--brand); color: #fff; display: grid; place-items: center; font-size: 26px; font-weight: 800; }
.sname { font-size: 19px; font-weight: 700; }
.body { flex: 1; background: #fff; border-radius: 20px 20px 0 0; margin-top: -22px; padding: 28px 28px 32px; display: flex; flex-direction: column; gap: 12px; }
.body h1 { font-size: 22px; margin: 0; }
.inp { display: flex; align-items: center; gap: 10px; border: 1px solid #cfd4da; border-radius: 12px; height: 54px; padding: 0 14px; background: #fff; }
.inp:focus-within { border-color: var(--brand); box-shadow: 0 0 0 3px var(--brand-soft); }
.inp input { border: 0; outline: 0; flex: 1; min-width: 0; font-size: 16px; padding: 0; background: none; height: 100%; }
.cc { background: #f3f4f6; border-radius: 8px; padding: 6px 10px; font-weight: 600; }
.x { border: 0; background: none; color: #9aa1ab; font-size: 22px; cursor: pointer; padding: 4px; }
.act { border: 0; background: none; color: var(--brand); font-weight: 600; font-size: 15px; white-space: nowrap; cursor: pointer; padding: 6px 0; }
.act.wait, .act:disabled { color: #9aa1ab; font-weight: 400; cursor: default; }
.grow { flex: 1; min-height: 12px; }
.agree { display: flex; gap: 8px; align-items: flex-start; font-size: 13px; color: var(--muted); }
@media (min-width: 768px) {
  .alogin { align-items: center; padding: 32px 16px; }
  .acard { min-height: 640px; border-radius: 20px; overflow: hidden; box-shadow: 0 10px 40px rgba(0, 0, 0, .08); max-width: 460px; }
}
</style>
