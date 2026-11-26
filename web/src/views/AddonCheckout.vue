<script setup lang="ts">
// The customer confirms the dishes they add to an order (docs/ADD_ON_ORDER_DESIGN.md §8.2):
// no address, time or payment to choose, all of it stays as on the order. The
// page shows the order before and after, and what the add-on costs more.
import { storePath } from '../site'
import '../checkout'
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ApiError, get, newKey, post } from '../api'
import { clearLines, quoteItems, useCart } from '../cart'
import { money, mn, pick, t } from '../i18n'
import { addonErr } from '../addon'
import type { AddonQuote, Order } from '../types'
import './storeStrings'

const props = defineProps<{ token: string }>()
const router = useRouter()
const order = ref<Order | null>(null)
const quote = ref<AddonQuote | null>(null)
const error = ref('')
const busy = ref(false)
const closed = ref(false)
const slug = computed(() => order.value?.store?.slug ?? '')
let cart: ReturnType<typeof useCart> | null = null

async function requote() {
  if (!cart || !cart.lines.length) { quote.value = null; return }
  error.value = ''
  try { quote.value = await post<AddonQuote>(`/customer/orders/${props.token}/append/quote`, { items: quoteItems() }) }
  catch (e) {
    quote.value = null; error.value = addonErr(e)
    closed.value = e instanceof ApiError && e.code === 'ADDON_CLOSED'
  }
}
onMounted(async () => {
  try { order.value = await get<Order>(`/customer/orders/${props.token}`) } catch (e) { error.value = (e as Error).message; return }
  cart = useCart(`${slug.value}#add:${props.token.slice(0, 10)}`)
  await requote()
})

const count = computed(() => quote.value?.lines.reduce((n, l) => n + l.qty, 0) ?? 0)
const origCount = computed(() => order.value?.items.filter(i => i.prep_status !== 'cancelled').reduce((n, i) => n + i.qty, 0) ?? 0)
const delivery = computed(() => order.value?.service_type === 'delivery')
// a customer's add-on on an accepted order waits for the store
const waits = computed(() => order.value?.acceptance_status === 'accepted')
const confirmMins = computed(() => order.value?.addon?.confirm_minutes ?? 5)

async function send() {
  if (!quote.value || busy.value) return
  busy.value = true; error.value = ''
  try {
    await post<Order>(`/customer/orders/${props.token}/append`, { items: quoteItems(), expected_total: quote.value.after.total }, { 'Idempotency-Key': newKey() })
    if (cart) clearLines()
    router.replace(`/o/${props.token}`)
  } catch (e) {
    error.value = addonErr(e)
    if (e instanceof ApiError && e.code === 'QUOTE_CHANGED') void requote()
    closed.value = e instanceof ApiError && e.code === 'ADDON_CLOSED'
  } finally { busy.value = false }
}
// The rider took the order meanwhile: the same dishes go into a new order instead.
function newOrder() {
  if (!cart || !slug.value) return
  const lines = cart.lines.map(l => ({ ...l }))
  clearLines()
  const main = useCart(slug.value)
  for (const l of lines) main.lines.push(l)
  router.replace(storePath(slug.value))
}
</script>

<template>
  <div class="phone ac warm" data-testid="addon-checkout">
    <header class="ac-top">
      <button class="ac-back" :aria-label="t('back')" @click="router.back()">
        <svg class="rtl-flip" viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m15 5-7 7 7 7" /></svg></button>
      <h1>{{ t('ao.c.title') }}</h1><span></span>
    </header>
    <div class="ac-body stack">
      <div v-if="error" class="banner bad" data-testid="addon-error">{{ error }}
        <button v-if="closed && cart?.lines.length" class="btn small" style="margin-inline-start:8px" @click="newOrder">{{ t('ao.c.new_order') }}</button></div>
      <template v-if="order">
        <div class="card ac-card">
          <div class="ac-where">{{ delivery ? t('ao.c.to', { s: [order.address_line, order.address_unit].filter(Boolean).join(' · ') }) : t('ao.c.pickup', { s: order.pickup_code }) }}</div>
          <div class="muted small">{{ order.public_no }} · {{ t(order.payment_method) }}</div>
        </div>
        <div class="card ac-card">
          <div class="ac-row muted"><span>{{ t('ao.c.orig', { n: origCount }) }}</span><span>{{ money(order.totals.subtotal, order.currency) }}</span></div>
          <div class="ac-sep"></div>
          <div class="ac-row"><b>{{ t('ao.c.this', { n: count }) }}</b><b>{{ quote ? money(quote.subtotal, quote.currency) : '—' }}</b></div>
          <div v-for="(l, i) in quote?.lines ?? []" :key="i" class="ac-line" data-testid="addon-line">
            <span class="grow">{{ mn(l) }}<small v-if="l.options.length"> · {{ l.options.map(o => mn(o)).join(' · ') }}</small></span>
            <span>×{{ l.qty }}</span><span class="r">{{ money(l.line_total, order.currency) }}</span>
          </div>
          <div v-if="!quote && !error" class="muted">{{ t('ao.c.empty') }}</div>
          <router-link v-if="slug" class="btn small ac-more" :to="{ path: storePath(slug), query: { append: token } }">{{ t('ao.c.more') }}</router-link>
        </div>
        <div v-if="quote" class="card ac-card" data-testid="addon-sums">
          <div class="ac-row"><span>{{ t('subtotal') }}</span><span>{{ money(quote.after.subtotal, quote.currency) }}</span></div>
          <div v-if="quote.after.packaging_fee" class="ac-row"><span>{{ t('packaging_fee') }}</span><span>{{ money(quote.after.packaging_fee, quote.currency) }}</span></div>
          <div v-if="delivery" class="ac-row"><span>{{ t('delivery_fee') }}</span><span>{{ money(quote.after.delivery_fee, quote.currency) }} · {{ t('ao.c.fee_paid') }}</span></div>
          <div v-if="quote.after.discount" class="ac-row" :class="{ gold: quote.promo_up }" data-testid="addon-discount">
            <span>{{ quote.after.promotion_name ? pick(quote.after.promotion_name, quote.after.promotion_name_en) : t('discount') }}<template v-if="quote.promo_up"> ↑</template></span>
            <span>−{{ money(quote.after.discount, quote.currency) }}</span></div>
          <div class="ac-sep"></div>
          <div class="ac-row"><span>{{ t('ao.c.total_now') }}</span><span><s class="muted">{{ money(quote.before.total, quote.currency) }}</s> <b>{{ money(quote.after.total, quote.currency) }}</b></span></div>
          <p class="muted small">{{ t('ao.c.no_coupon') }}</p>
          <p class="muted small">{{ waits ? t('ao.c.wait_note', { n: confirmMins }) : t('ao.c.wait_note0') }}</p>
        </div>
      </template>
    </div>
    <div class="ac-bar">
      <button class="btn primary block big" data-testid="addon-send" :disabled="!quote || busy" @click="send">
        {{ t('ao.c.confirm', { s: quote ? money(quote.extra, quote.currency) : '—' }) }}</button>
    </div>
  </div>
</template>

<style scoped>
.ac { min-height: 100vh; background: var(--bg); padding-bottom: 96px; }
.ac-top { display: grid; grid-template-columns: 40px 1fr 40px; align-items: center; padding: 10px 12px; background: var(--red-bg, #8f251b); color: #fff; }
.ac-top h1 { margin: 0; font-size: 17px; text-align: center; }
.ac-back { background: none; border: 0; color: inherit; cursor: pointer; }
.ac-body { padding: 12px; max-width: 640px; margin: 0 auto; }
.ac-card { display: flex; flex-direction: column; gap: 6px; }
.ac-where { font-weight: 700; }
.ac-row { display: flex; justify-content: space-between; gap: 10px; font-size: 14px; }
.ac-row.gold { color: #9a6b00; font-weight: 700; background: #fff4d6; margin: 0 -8px; padding: 2px 8px; border-radius: 6px; }
.ac-line { display: flex; gap: 10px; font-size: 13.5px; } .ac-line .r { min-width: 70px; text-align: end; }
.ac-line small { color: var(--muted); }
.ac-sep { border-top: 1px dashed var(--line); margin: 4px 0; }
.ac-more { align-self: flex-start; }
.ac-bar { position: fixed; inset-inline: 0; bottom: 0; padding: 10px 12px calc(10px + env(safe-area-inset-bottom)); background: #fff; box-shadow: 0 -2px 10px rgba(0,0,0,.08); }
.ac-bar .btn { max-width: 640px; margin: 0 auto; display: block; }
.grow { flex: 1; min-width: 0; }
</style>
