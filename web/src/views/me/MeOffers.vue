<script setup lang="ts">
// The customer's coupons (usable / used / expired) and, under the usable
// ones, the promotions their stores run, which apply by themselves.
import { computed, ref, watch } from 'vue'
import { get } from '../../api'
import { couponName, couponStub, validity, whereUsable, lastDay } from '../../coupons'
import { locale, pick, promoLabel, t } from '../../i18n'
import type { CustomerCoupon, Promotion } from '../../types'
import MeIntro from './MeIntro.vue'
import MePage from './MePage.vue'
import { bare, describe, homeStore, me, myOffers, offerParts, type StoreOffers } from './me'

const offers = ref<StoreOffers[] | null>(null)
const coupons = ref<CustomerCoupon[] | null>(null)
const error = ref('')
const tab = ref<'available' | 'used' | 'expired'>('available')
watch(() => me.profile?.id, async id => {
  if (!id) return
  try {
    const [o, c] = await Promise.all([myOffers(homeStore()?.slug), get<{ coupons: CustomerCoupon[] }>('/customer/coupons')])
    offers.value = o; coupons.value = c.coupons
  } catch (e) { error.value = describe(e) }
}, { immediate: true })
const shown = computed(() => (coupons.value ?? []).filter(c => c.status === tab.value))
const openCount = computed(() => (coupons.value ?? []).filter(c => c.status === 'available').length)
// A store coupon leads to its store; a platform coupon to the marketplace.
const useLink = (c: CustomerCoupon) => c.store_slug ? `/s/${c.store_slug}` : '/'

// Who the offer is for, when it is not for every order.
function only(p: Promotion): string {
  if (p.applies_delivery && !p.applies_pickup) return t('mo.delivery_only')
  if (p.applies_pickup && !p.applies_delivery) return t('mo.pickup_only')
  return ''
}
// The name the store gave it, unless it only repeats what the offer is.
const named = (p: Promotion, currency: string) => { const n = pick(p.name, p.name_en); return n && bare(n, currency) !== bare(promoLabel(p, currency), currency) ? n : '' }
function valid(p: Promotion): string {
  const out: string[] = []
  out.push(p.ends_at ? t('mo.until', { d: new Date(p.ends_at).toLocaleDateString(locale.value === 'zh' ? 'zh-CN' : 'en-GB') }) : t('mo.no_end'))
  if (p.daily_start !== p.daily_end) out.push(t('mo.daily', { a: p.daily_start, b: p.daily_end }))
  return t('mo.valid', { d: out.join(' · ') })
}
</script>

<template>
  <MePage :title="t('mo.title')" class="offers" layout="two">
    <MeIntro :title="t('mo.auto')" :sub="t('mo.no_claim')">
      <template #icon><svg viewBox="0 0 28 24" width="40" height="34" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M3.5 6h21v3.500a2.5 2.5 0 0 0 0 5V18h-21v-3.500a2.5 2.5 0 0 0 0-5z" /><path d="M10 6.500v11" stroke-dasharray="1.6 2.2" /><path d="m15.5 15 4.5-6M15.8 9.600h.01M19.7 14.400h.01" /></svg></template>
    </MeIntro>
    <div v-if="error" class="banner bad">{{ error }}</div>
    <div class="cp-tabs" role="tablist">
      <button v-for="k in (['available', 'used', 'expired'] as const)" :key="k" type="button" role="tab" :class="{ on: tab === k }"
        :aria-selected="tab === k" :data-testid="'coupon-tab-' + k" @click="tab = k">{{ k === 'available' ? t('mo.tab_open', { n: openCount }) : t(k === 'used' ? 'mo.tab_used' : 'mo.tab_expired') }}</button>
    </div>
    <div v-if="coupons && !shown.length" class="card empty" data-testid="coupons-empty">{{ t(tab === 'available' ? 'mo.no_coupons' : tab === 'used' ? 'mo.none_used' : 'mo.none_expired') }}</div>
    <div v-for="c in shown" :key="c.id" class="coupon mine cell" :class="[c.kind, { gone: tab !== 'available' }]" data-testid="my-coupon">
      <div class="stub"><b>{{ couponStub(c, c.currency)[0] }}</b><span class="small">{{ couponStub(c, c.currency)[1] }}</span></div>
      <div class="l">
        <b>{{ couponName(c) }}</b>
        <span class="store"><span>{{ whereUsable(c) }}</span><em v-if="c.kind === 'store_delivery'">{{ t('mo.delivery_only') }}</em></span>
        <span v-if="tab === 'used' && c.used_at" class="small">{{ t('mo.used_on', { d: lastDay(c.used_at) }) }}</span>
        <span v-else class="small" :class="{ soon: tab === 'available' && validity(c).soon }">{{ validity(c).text }}</span>
      </div>
      <router-link v-if="tab === 'available'" :to="useLink(c)" class="use">{{ t('mo.use') }}
        <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="2.6" stroke-linecap="round" stroke-linejoin="round"><path d="m9 5 7 7-7 7" /></svg></router-link>
    </div>
    <template v-if="tab === 'available' && offers?.length">
      <div class="cp-head">{{ t('mo.store_offers') }}</div>
    </template>
    <template v-for="o in tab === 'available' ? offers ?? [] : []" :key="o.store.slug">
      <div v-for="p in o.promotions" :key="p.id" class="coupon cell" :class="{ ship: p.kind === 'free_delivery' }" data-testid="offer">
        <div class="stub">
          <svg v-if="p.kind === 'free_delivery'" viewBox="0 0 28 24" width="44" height="36" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M9 6.500h9.500v10M18.5 9.500h4l3 3.500v3.500h-2M9 16.500h6M2.5 9.500h5M4.5 12.500h4" /><circle cx="12" cy="17.5" r="1.9" /><circle cx="21" cy="17.5" r="1.9" /></svg>
          <svg v-else viewBox="0 0 28 24" width="38" height="32" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M3.5 6h21v3.500a2.5 2.5 0 0 0 0 5V18h-21v-3.500a2.5 2.5 0 0 0 0-5z" /><path d="M10 6.500v11" stroke-dasharray="1.6 2.2" /><path d="m15.5 15 4.5-6M15.8 9.600h.01M19.7 14.400h.01" /></svg>
          <b>{{ offerParts(p, o.currency).gives }}</b>
        </div>
        <div class="l">
          <b>{{ offerParts(p, o.currency).when }}</b>
          <span class="store"><span>{{ pick(o.store.name, o.store.name_en) }}</span><em v-if="only(p)">{{ only(p) }}</em></span>
          <span v-if="named(p, o.currency)" class="small nm">{{ named(p, o.currency) }}</span>
          <span class="small">{{ valid(p) }}</span>
        </div>
        <router-link :to="`/s/${o.store.slug}`" class="use">{{ t('mo.use') }}
          <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="2.6" stroke-linecap="round" stroke-linejoin="round"><path d="m9 5 7 7-7 7" /></svg></router-link>
      </div>
    </template>
  </MePage>
</template>

<style scoped>
.cp-tabs { display: flex; background: var(--paper); border-radius: 14px; padding: 4px; gap: 4px; box-shadow: 0 3px 12px rgba(90, 40, 15, .06); }
.cp-tabs button { flex: 1; border: 0; background: none; border-radius: 10px; padding: 6px 4px; font: inherit; font-size: 14px; color: var(--muted); cursor: pointer; }
.cp-tabs button.on { background: var(--red); color: #fff; font-weight: 600; }
.cp-head { font-size: 12px; color: var(--muted); font-weight: 500; margin: 6px 4px -2px; }
.coupon.mine .stub b { font-size: 20px; }
.coupon.mine.store_delivery .stub { background: linear-gradient(135deg, #fdf1d6, #fbe3ad); border-right-color: #dcbf7c; color: #a8641a; }
.coupon.mine.platform_free .stub { background: linear-gradient(135deg, #efe6ff, #e2d3fb); border-right-color: #b9a1e6; color: #5b21b6; }
.coupon.mine.gone { opacity: .6; filter: grayscale(.6); }
.l .soon { color: #c2410c; font-weight: 500; }
.coupon { position: relative; display: flex; align-items: stretch; background: var(--paper); border-radius: 12px; box-shadow: 0 3px 12px rgba(90, 40, 15, .08); min-width: 0; }
.stub { flex: none; width: 28%; min-width: 92px; max-width: 150px; display: grid; justify-items: center; align-content: center; gap: 2px; padding: 12px 6px; border-radius: 12px 0 0 12px; background: linear-gradient(135deg, #fdeae4, #f9d9d3); color: var(--red); border-right: 1.500px dashed #d9a79a; position: relative; text-align: center; }
.coupon.ship .stub { background: linear-gradient(135deg, #fdf1d6, #fbe3ad); border-right-color: #dcbf7c; }
.coupon.ship .stub svg { color: #b9781e; }
/* the two notches of a ticket, where the stub tears off */
.stub::before, .stub::after { content: ""; position: absolute; right: -8px; width: 14px; height: 14px; border-radius: 50%; background: var(--bg); }
.stub::before { top: -7px; } .stub::after { bottom: -7px; }
.stub b { font-size: 19px; font-weight: 700; line-height: 1.2; }
.l { flex: 1; min-width: 0; display: grid; gap: 2px; align-content: center; padding: 10px 6px 10px 14px; }
.l > b { font-size: 15px; font-weight: 600; }
.l .store { display: flex; align-items: center; gap: 6px; color: var(--muted); font-size: 12px; min-width: 0; flex-wrap: wrap; }
.l .store em { font-style: normal; font-size: 11px; padding: 0 6px; border-radius: 4px; background: var(--gold-soft); color: #a8641a; border: 1px solid #f0d9a0; white-space: nowrap; }
.l .small { color: var(--muted); font-size: 12px; }
.use { flex: none; align-self: center; margin-right: 12px; display: inline-flex; align-items: center; gap: 2px; background: var(--red); color: #fff; font-weight: 500; font-size: 12px; border-radius: 999px; padding: 4px 6px 4px 12px; white-space: nowrap; }
.use:hover { background: var(--red-deep); }
@media (max-width: 380px) {
  .stub b { font-size: 17px; } .l { padding-left: 12px; } .l > b { font-size: 14px; } .use { padding: 4px 6px 4px 10px; margin-right: 8px; }
}
</style>
