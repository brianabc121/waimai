<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ApiError, get, newKey, post } from '../api'
import { clearLines, useCart } from '../cart'
import { money, pick, t, whenText } from '../i18n'
import type { Quote, Storefront } from '../types'
import TotalsTable from './TotalsTable.vue'

const props = defineProps<{ slug: string }>()
const router = useRouter()
const cart = useCart(props.slug)

const quote = ref<Quote | null>(null)
const error = ref('')
const notice = ref('')
const busy = ref(false)

// Scheduling: '' means as soon as possible. When the store is closed right
// now, a time must be chosen.
const store = ref<Storefront | null>(null)
const scheduled = ref('')
const tz = computed(() => store.value?.branch.timezone)
const slots = computed(() => store.value?.slots ?? [])
const mustSchedule = computed(() => !!store.value && !store.value.branch.open_now)
async function loadStore() {
  store.value = await get<Storefront>(`/stores/${props.slug}`).catch(() => null)
  if (scheduled.value && !slots.value.includes(scheduled.value)) scheduled.value = ''
}

// phone verification
const verified = ref(false)
const phone = ref(localStorage.getItem('phone') || '')
const code = ref('')
const codeSent = ref(false)
const devCode = ref('')
const otpError = ref('')

// The idempotency key belongs to one confirmed quote. It is kept across a
// reload so that a retry after a timeout finds the original order.
let idemKey = ''
function keyFor(quoteId: string) {
  const saved = sessionStorage.getItem('order-key')
  if (saved?.startsWith(quoteId + '|')) return saved.slice(quoteId.length + 1)
  const k = newKey()
  sessionStorage.setItem('order-key', quoteId + '|' + k)
  return k
}

function describe(e: unknown): string {
  if (!(e instanceof ApiError)) return String(e)
  switch (e.code) {
    case 'NETWORK': return t('offline')
    case 'ITEM_UNAVAILABLE': return t('unavailable')
    case 'OUT_OF_ZONE': return t('choose_zone')
    case 'STORE_CLOSED': return t('closed')
    case 'SCHEDULE_INVALID': return t('schedule_invalid')
    case 'BELOW_MINIMUM': return t('below_min', { amt: money((e.details?.minimum ?? 0) - (e.details?.subtotal ?? 0), quote.value?.currency) })
    default: return e.message
  }
}

async function requote() {
  error.value = ''
  if (cart.lines.length === 0) { router.replace(`/s/${props.slug}`); return }
  if (mustSchedule.value && !scheduled.value) { quote.value = null; error.value = slots.value.length ? t('pick_time') : t('closed'); return }
  try {
    quote.value = await post<Quote>(`/stores/${props.slug}/quotes`, {
      service_type: cart.service,
      zone_id: cart.service === 'delivery' ? cart.zoneId : null,
      items: cart.lines.map(l => ({ item_id: l.item_id, qty: l.qty, option_ids: l.option_ids, note: l.note })),
      scheduled_for: scheduled.value || null,
    })
    idemKey = keyFor(quote.value.id)
  } catch (e) {
    quote.value = null
    error.value = describe(e)
    // The offered times move on; fetch fresh ones when the chosen one is gone.
    if (e instanceof ApiError && (e.code === 'SCHEDULE_INVALID' || e.code === 'STORE_CLOSED')) await loadStore()
  }
}

onMounted(async () => {
  const s = await get('/customer/session').catch(() => ({ verified: false }))
  verified.value = s.verified
  if (s.phone) phone.value = s.phone
  await loadStore()
  await requote()
})

async function sendCode() {
  otpError.value = ''
  try {
    const r = await post('/otp/request', { phone: phone.value })
    codeSent.value = true
    devCode.value = r.dev_code ?? ''
    if (r.dev_code) code.value = r.dev_code
    localStorage.setItem('phone', phone.value)
  } catch (e) { otpError.value = (e as Error).message }
}

async function verify() {
  otpError.value = ''
  try {
    await post('/otp/verify', { phone: phone.value, code: code.value })
    verified.value = true
  } catch (e) { otpError.value = (e as Error).message }
}

const ready = computed(() => !!quote.value && verified.value && cart.name.trim() !== ''
  && (cart.service === 'pickup' || cart.addressLine.trim() !== ''))

async function submit() {
  if (!quote.value || busy.value) return
  busy.value = true
  error.value = ''; notice.value = ''
  try {
    const r = await post(`/stores/${props.slug}/orders`, {
      quote_id: quote.value.id, customer_name: cart.name, address_line: cart.addressLine,
      address_unit: cart.addressUnit, address_note: cart.addressNote, note: cart.note,
    }, { 'Idempotency-Key': idemKey })
    clearLines()
    sessionStorage.removeItem('order-key')
    localStorage.setItem('last-order:' + props.slug, r.access_token)
    router.replace(`/o/${r.access_token}`)
  } catch (e) {
    if (e instanceof ApiError && (e.code === 'QUOTE_CHANGED' || e.code === 'QUOTE_EXPIRED' || e.code === 'QUOTE_USED')) {
      // Show the new amount first; the customer confirms again.
      await requote()
      notice.value = t('amount_changed')
    } else if (e instanceof ApiError && e.code === 'PHONE_NOT_VERIFIED') {
      verified.value = false
    } else {
      // On a network error the key is kept, so pressing again is a safe retry.
      error.value = describe(e)
    }
  } finally { busy.value = false }
}
</script>

<template>
  <div class="phone">
    <div class="topbar row"><router-link :to="`/s/${slug}`">‹ {{ t('back_to_menu') }}</router-link></div>
    <div class="section stack">
      <div v-if="error" class="banner bad">{{ error }} <button class="btn link" @click="requote">{{ t('retry') }}</button></div>
      <div v-if="notice" class="banner warn">{{ notice }}</div>

      <div class="card stack">
        <div class="row between"><h2>{{ t(cart.service) }}</h2><span class="tag brand" v-if="quote?.zone_name">{{ quote.zone_name }}</span></div>
        <label class="field"><span>{{ t('your_name') }}</span><input v-model="cart.name" maxlength="80" autocomplete="name" /></label>
        <template v-if="cart.service === 'delivery'">
          <label class="field"><span>{{ t('street') }}</span><input v-model="cart.addressLine" maxlength="300" autocomplete="street-address" /></label>
          <label class="field"><span>{{ t('unit') }}</span><input v-model="cart.addressUnit" maxlength="100" /></label>
          <label class="field"><span>{{ t('address_note') }}</span><input v-model="cart.addressNote" maxlength="300" /></label>
        </template>
        <label class="field" style="margin:0"><span>{{ t('order_note') }}</span><input v-model="cart.note" maxlength="300" /></label>
      </div>

      <div class="card stack" v-if="slots.length || mustSchedule">
        <h2>{{ t(cart.service === 'delivery' ? 'deliver_at' : 'pickup_at') }}</h2>
        <select v-model="scheduled" data-testid="slot" :aria-label="t('when')" @change="requote">
          <option value="" :disabled="mustSchedule">{{ mustSchedule ? t('pick_time') : t('asap') }}</option>
          <option v-for="s in slots" :key="s" :value="s">{{ whenText(s, tz) }}</option>
        </select>
      </div>

      <div class="card stack">
        <h2>{{ t('verify_phone') }}</h2>
        <div v-if="verified" class="banner ok">{{ t('verified_as') }} {{ phone }}</div>
        <template v-else>
          <p class="muted small">{{ t('verify_hint') }}</p>
          <div class="row"><input v-model="phone" type="tel" inputmode="tel" :placeholder="t('phone_number')" autocomplete="tel" />
            <button class="btn" @click="sendCode" :disabled="phone.length < 7">{{ t('send_code') }}</button></div>
          <div class="row" v-if="codeSent"><input v-model="code" inputmode="numeric" maxlength="6" :placeholder="t('code')" autocomplete="one-time-code" />
            <button class="btn primary" @click="verify" :disabled="code.length !== 6">{{ t('verify') }}</button></div>
          <div v-if="devCode" class="banner info small">{{ t('dev_code') }}: {{ devCode }}</div>
          <div v-if="otpError" class="banner bad">{{ otpError }}</div>
        </template>
      </div>

      <div class="card stack" v-if="quote">
        <h2>{{ t('order_summary') }}</h2>
        <div v-for="(l, i) in quote.lines" :key="i" class="row top between">
          <div class="grow"><b>{{ l.qty }} ×</b> {{ pick(l.name, l.name_en) }}
            <div class="muted small" v-if="l.options.length">{{ l.options.map(o => pick(o.name, o.name_en)).join(' · ') }}</div>
            <div class="muted small" v-if="l.note">{{ t('note') }}: {{ l.note }}</div></div>
          <span class="price">{{ money(l.line_total, quote.currency) }}</span>
        </div>
        <TotalsTable :totals="quote.totals" :currency="quote.currency" :delivery="cart.service === 'delivery'" />
        <div class="row between"><span class="muted">{{ t('pay_method') }}</span><span>{{ t(cart.service === 'delivery' ? 'cod' : 'pay_at_pickup') }}</span></div>
      </div>
    </div>

    <div class="bottombar"><div>
      <button class="btn primary block big" :disabled="!ready || busy" @click="submit">
        {{ busy ? t('placing') : quote ? `${t('place_order')} · ${money(quote.totals.total, quote.currency)}` : t('place_order') }}
      </button>
    </div></div>
  </div>
</template>
