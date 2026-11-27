<script setup lang="ts">
// A store's "offers" page (docs/STORED_VALUE_DESIGN.md §5): the customer's
// balance here, top up and get more, the coupons to claim and the store's
// order offers. Topping up makes a code the customer shows at the counter;
// it goes in once the staff take the money.
import QRCode from 'qrcode'
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { get } from '../api'
import { loadCustomer, loginRoute } from '../customer'
import { locale, money, mn, promoLabel, t, toMinor, whenText } from '../i18n'
import { storePath } from '../site'
import type { Storefront } from '../types'
import MePage from './me/MePage.vue'
import StoreCoupons from './StoreCoupons.vue'
import './storeStrings'
import {
  bonusRate, cancelMyTopup, createTopup, myTopup, storedErr, storeStored, tierCoupon, tierLimits, type StoredOffers, type Topup,
} from '../stored'

const props = defineProps<{ slug: string }>()
const router = useRouter()
const data = ref<StoredOffers | null>(null)
const store = ref<Storefront | null>(null)
const signedIn = ref(false)
const pick = ref(0), other = ref('')
const error = ref(''), busy = ref(false)
const pending = ref<Topup | null>(null), qr = ref(''), paid = ref<Topup | null>(null)
const cur = computed(() => data.value?.currency ?? store.value?.branch.currency ?? 'PHP')
const m = (v: number) => money(v, cur.value)
const camp = computed(() => data.value?.campaign ?? null)
const promos = computed(() => (store.value?.promotions ?? []).filter(p => p.live_now !== false))
const here = computed(() => storePath(props.slug))
let timer = 0

async function load() {
  error.value = ''
  signedIn.value = await loadCustomer()
  try {
    const [d, s] = await Promise.all([storeStored(props.slug), get<Storefront>(`/stores/${props.slug}?lang=${locale.value}`)])
    data.value = d; store.value = s
    if (d.pending) await show(d.pending)
    const open = d.campaign?.tiers.find(x => !x.closed)
    if (open && !pick.value) pick.value = open.id
  } catch (e) { error.value = storedErr(e) }
}
onMounted(load)
onBeforeUnmount(() => clearInterval(timer))

async function show(x: Topup) {
  pending.value = x; paid.value = null
  qr.value = x.code ? await QRCode.toDataURL(x.code, { margin: 1, width: 360 }).catch(() => '') : ''
  clearInterval(timer)
  timer = window.setInterval(poll, 3000)
}
async function poll() {
  if (!pending.value || document.visibilityState !== 'visible') return
  const x = await myTopup(pending.value.id).catch(() => null)
  if (!x || x.status === 'pending') return
  clearInterval(timer)
  if (x.status === 'paid') { paid.value = x; pending.value = null; data.value = await storeStored(props.slug).catch(() => data.value) }
  else pending.value = x
}
async function go() {
  if (!signedIn.value) { await router.push(loginRoute(router.currentRoute.value.fullPath, props.slug)); return }
  busy.value = true; error.value = ''
  try { await show(await createTopup(props.slug, pick.value ? { tier_id: pick.value } : { amount: toMinor(other.value) })) }
  catch (e) { error.value = storedErr(e) } finally { busy.value = false }
}
async function cancel() {
  if (!pending.value) return
  await cancelMyTopup(pending.value.id).catch(() => null)
  clearInterval(timer); pending.value = null
}
const chosen = computed(() => camp.value?.tiers.find(x => x.id === pick.value) ?? null)
const ready = computed(() => !!camp.value && (chosen.value ? !chosen.value.closed : !!camp.value.custom_ok && toMinor(other.value) >= camp.value.custom_min))
</script>

<template>
  <MePage :title="t('sv.c.title')" :back="here" open layout="one">
    <div class="sv" data-testid="offers-page">
      <div v-if="store" class="shop"><b>{{ mn(store.brand ?? store.branch) }}</b>
        <router-link :to="here" class="small">{{ t('sv.q.back') }} ›</router-link></div>
      <div v-if="error" class="banner bad" data-testid="offers-error">{{ error }}</div>

      <!-- the code to show at the counter -->
      <div v-if="pending" class="card qrcard" data-testid="topup-code">
        <h3>{{ t('sv.q.title') }}</h3>
        <template v-if="pending.status === 'pending'">
          <img v-if="qr" :src="qr" alt="" class="qr" />
          <div class="code" data-testid="topup-code-digits">{{ pending.code }}</div>
          <div class="amt">{{ t('sv.q.amount', { pay: m(pending.pay), total: m(pending.pay + pending.bonus) }) }}</div>
          <div v-if="pending.coupon_qty" class="small cp">{{ tierCoupon(pending, cur) }}</div>
          <p class="small muted">{{ t('sv.q.hint') }}</p>
          <p class="small muted">{{ t('sv.q.until', { at: whenText(pending.expires_at) }) }} · {{ t('sv.q.waiting') }}</p>
          <button class="btn" data-testid="topup-cancel" @click="cancel">{{ t('sv.q.cancel') }}</button>
        </template>
        <template v-else>
          <p class="muted">{{ t('sv.q.expired') }}</p>
          <button class="btn" @click="pending = null">{{ t('sv.c.go') }}</button>
        </template>
      </div>
      <div v-if="paid" class="banner ok" data-testid="topup-paid">{{ t('sv.q.paid', { total: m(paid.pay + paid.bonus) }) }}</div>

      <!-- my balance here -->
      <div class="card wallet" data-testid="offers-balance">
        <span class="small">{{ t('sv.c.bal') }}</span>
        <template v-if="data?.balance">
          <b class="bal">{{ m(data.balance.total) }}</b>
          <span class="parts">{{ t('sv.c.parts', { p: m(data.balance.principal), b: m(data.balance.bonus) }) }}<template v-if="data.balance.credit"> · {{ t('sv.c.credit', { c: m(data.balance.credit) }) }}</template></span>
          <router-link v-if="store" class="small rec" :to="`/me/store-credit/${store.branch.id}`">{{ t('sv.c.records') }} ›</router-link>
        </template>
        <router-link v-else class="small rec" :to="loginRoute($route.fullPath, slug)">{{ t('sv.c.login') }} ›</router-link>
      </div>

      <!-- top up and get more -->
      <div v-if="camp && !pending" class="card" data-testid="offers-topup">
        <h3>{{ t('sv.c.topup') }} <small v-if="camp.ends_at" class="muted">{{ t('sv.m.period', { from: '', to: camp.ends_at.slice(0, 10) }) }}</small></h3>
        <div class="tiers">
          <button v-for="x in camp.tiers" :key="x.id" class="tier" :class="{ on: pick === x.id, off: !!x.closed }" :disabled="!!x.closed"
            data-testid="offers-tier" @click="pick = x.id">
            <span v-if="x.label" class="hot">{{ x.label }}</span>
            <span class="pay">{{ t('sv.t.pay', { amt: '' }) }}<b>{{ m(x.pay) }}</b></span>
            <span class="give">{{ t('sv.t.give', { amt: m(x.bonus) }) }}<small v-if="bonusRate(x)"> {{ t('sv.t.rate', { pct: bonusRate(x) }) }}</small></span>
            <span v-if="x.coupon_qty" class="cp">{{ tierCoupon(x, cur) }}</span>
            <span v-if="x.closed" class="lim">{{ t('sv.c.closed.' + x.closed) }}</span>
            <span v-else-if="tierLimits(x).length" class="lim">{{ tierLimits(x).join(' · ') }}</span>
          </button>
          <button v-if="camp.custom_ok" class="tier" :class="{ on: !pick }" @click="pick = 0">
            <span class="pay"><b>{{ t('sv.c.amount') }}</b></span><span class="lim">{{ t('sv.t.custom', { min: m(camp.custom_min) }) }}</span></button>
        </div>
        <input v-if="!pick && camp.custom_ok" v-model="other" class="other" inputmode="decimal" :placeholder="t('sv.c.amount')" />
        <p v-if="camp.note" class="small muted">{{ camp.note }}</p>
        <button class="btn primary go" :disabled="busy || !ready" data-testid="offers-go" @click="go">{{ t('sv.c.go') }}</button>
        <div class="rules"><b class="small">{{ t('sv.c.rules') }}</b>
          <ol class="small muted"><li>{{ t('sv.c.rule1') }}</li><li>{{ t('sv.c.rule2') }}</li><li>{{ t('sv.c.rule3') }}</li></ol></div>
      </div>

      <!-- coupons and order offers -->
      <div v-if="store" class="card cpcard">
        <h3>{{ t('sv.c.coupons') }}</h3>
        <StoreCoupons :slug="slug" :currency="cur" />
      </div>
      <div v-if="promos.length" class="card" data-testid="offers-promos">
        <h3>{{ t('sv.c.promos') }}</h3>
        <div v-for="p in promos" :key="p.id" class="act"><span class="ic">{{ p.kind === 'free_delivery' ? '🛵' : '%' }}</span>
          <div><div class="tt">{{ promoLabel(p, cur) }}</div><div class="small muted">{{ mn(p) }}</div></div></div>
      </div>
      <div v-if="data && !camp && !promos.length" class="card muted" data-testid="offers-none">{{ t('sv.c.none') }}</div>
    </div>
  </MePage>
</template>

<style scoped>
.sv { display: grid; gap: 12px; max-width: 640px; margin: 0 auto; width: 100%; }
.shop { display: flex; align-items: center; justify-content: space-between; gap: 10px; padding: 0 4px; }
.card { padding: 14px; display: grid; gap: 10px; }
.card h3 { margin: 0; font-size: 15px; }
.wallet { background: linear-gradient(135deg, #fff6e2, #f8deae); border: 1px solid #ecc977; gap: 4px; }
.wallet .bal { font-size: 28px; font-weight: 700; color: var(--red-deep, #8f251b); font-variant-numeric: tabular-nums; line-height: 1.2; }
.wallet .parts { font-size: 12.5px; color: #7a4a10; }
.wallet .rec { justify-self: start; color: var(--red-deep, #8f251b); }
.tiers { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; }
.tier { position: relative; display: grid; gap: 2px; text-align: start; border: 1.5px solid var(--line); border-radius: 12px; padding: 12px 10px 10px; background: #fff;
  font: inherit; color: inherit; cursor: pointer; }
.tier.on { border-color: var(--red, #a02c20); background: #fff4ef; }
.tier.off { opacity: .55; cursor: default; }
.tier .pay { font-size: 12px; color: var(--muted); } .tier .pay b { font-size: 19px; color: var(--ink, #3b1f14); margin-inline-start: 2px; }
.tier .give { font-size: 13px; font-weight: 700; color: var(--red, #a02c20); }
.tier .give small { font-weight: 600; color: #b45309; }
.tier .cp { font-size: 11.5px; color: #7a3e00; } .tier .lim { font-size: 11.5px; color: var(--muted); }
.tier .hot { position: absolute; top: -9px; inset-inline-start: 8px; background: #f4cf73; color: #5a3200; font-size: 11px; font-weight: 700; border-radius: 6px; padding: 0 6px; }
.other { width: 100%; }
.go { min-height: 44px; border-radius: 22px; font-size: 15px; }
.rules ol { margin: 4px 0 0; padding-inline-start: 18px; line-height: 1.7; }
.qrcard { text-align: center; justify-items: center; }
.qr { width: 180px; height: 180px; }
.code { font-size: 32px; letter-spacing: 8px; font-weight: 700; font-variant-numeric: tabular-nums; }
.amt { font-weight: 700; color: var(--red-deep, #8f251b); }
.cp { color: #7a3e00; }
.act { display: flex; gap: 10px; align-items: center; padding: 8px 0; border-top: 1px dashed var(--line); }
.act:first-of-type { border-top: 0; }
.act .ic { width: 34px; height: 34px; border-radius: 9px; display: grid; place-items: center; font-weight: 700; color: #fff; background: var(--red, #a02c20); flex: none; font-size: 13px; }
.act .tt { font-weight: 600; }
/* no coupons to claim: no empty card */
.cpcard:not(:has(.scp)) { display: none; }
</style>
