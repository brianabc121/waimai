<script setup lang="ts">
// Paying an order the staff entered from the customer's store balance
// (docs/REFUND_BALANCE_DESIGN.md §5.3): the customer is texted a code and
// reads it out; only then is their balance used.
import { reactive } from 'vue'
import { ApiError } from '../api'
import { creditErr, requestCreditCode, spendCreditByCode } from '../credit'
import { money, t } from '../i18n'
import '../voids'
import type { Order } from '../types'

const props = defineProps<{ order: Order }>()
const emit = defineEmits<{ (e: 'done', o: Order): void; (e: 'close'): void }>()
const s = reactive({ phone: '', needPhone: !props.order.customer_phone, id: 0, sentTo: '', balance: 0, code: '', error: '', busy: false })

async function send() {
  s.error = ''
  try {
    const r = await requestCreditCode(props.order.id, 'spend', s.phone.trim())
    s.id = r.id; s.sentTo = r.phone; s.balance = r.balance; s.code = r.dev_code ?? ''
  } catch (e) {
    if (e instanceof ApiError && e.code === 'CREDIT_PHONE') s.needPhone = true
    s.error = creditErr(e)
  }
}
async function go() {
  s.busy = true; s.error = ''
  try { emit('done', await spendCreditByCode<Order>(props.order.id, s.id, s.code)) } catch (e) { s.error = creditErr(e) }
  s.busy = false
}
</script>

<template>
  <div class="drawer-mask" @click="emit('close')"></div>
  <div class="cs-sheet" data-testid="credit-spend">
    <div class="cs-h"><h3>{{ t('sc.s.title') }}</h3><button class="x" @click="emit('close')">✕</button></div>
    <p class="small muted">{{ t('sc.s.hint') }}</p>
    <label v-if="s.needPhone" class="field"><span>{{ t('sc.r.phone') }}</span><input v-model="s.phone" inputmode="tel" data-testid="credit-spend-phone" /></label>
    <div class="row"><button class="btn" data-testid="credit-spend-send" @click="send">{{ t(s.id ? 'sc.r.resend' : 'sc.r.send') }}</button>
      <span v-if="s.sentTo" class="small muted">{{ t('sc.r.sent', { p: s.sentTo }) }}</span></div>
    <template v-if="s.id">
      <div class="small">{{ t('sc.s.balance', { s: money(s.balance, order.currency) }) }}</div>
      <label class="field"><span>{{ t('sc.r.code') }}</span><input v-model="s.code" inputmode="numeric" maxlength="6" data-testid="credit-spend-code" /></label>
    </template>
    <div v-if="s.error" class="banner bad small">{{ s.error }}</div>
    <div class="cs-f"><button class="btn" @click="emit('close')">{{ t('vd.cancel') }}</button>
      <button class="btn primary grow" :disabled="s.busy || !s.id || !/^\d{6}$/.test(s.code)" data-testid="credit-spend-ok" @click="go">{{ t('sc.s.ok') }}</button></div>
  </div>
</template>

<style scoped>
.drawer-mask { position: fixed; inset: 0; background: rgba(0,0,0,.35); z-index: 55; }
.cs-sheet { position: fixed; z-index: 60; left: 50%; top: 50%; transform: translate(-50%, -50%); width: min(420px, calc(100vw - 32px)); background: #fff; border-radius: 14px; padding: 14px 16px; display: flex; flex-direction: column; gap: 10px; box-shadow: 0 10px 40px rgba(0,0,0,.2); }
.cs-h { display: flex; align-items: center; gap: 8px; } .cs-h h3 { flex: 1; margin: 0; font-size: 17px; }
.x { background: none; border: 0; font-size: 18px; cursor: pointer; color: var(--muted); }
.row { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.cs-f { display: flex; gap: 10px; } .grow { flex: 1; }
</style>
