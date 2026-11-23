<script setup lang="ts">
// The offers running at the customer's stores. They apply by themselves at
// checkout, so this page only says what there is and leads to the store.
import { ref, watch } from 'vue'
import { locale, pick, promoLabel, t } from '../../i18n'
import type { Promotion } from '../../types'
import MeIntro from './MeIntro.vue'
import MePage from './MePage.vue'
import { bare, describe, homeStore, me, myOffers, offerParts, type StoreOffers } from './me'

const offers = ref<StoreOffers[] | null>(null)
const error = ref('')
watch(() => me.profile?.id, async id => {
  if (!id) return
  try { offers.value = await myOffers(homeStore()?.slug) } catch (e) { error.value = describe(e) }
}, { immediate: true })

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
  <MePage :title="t('mo.title')" class="offers">
    <MeIntro :title="t('mo.auto')" :sub="t('mo.no_claim')">
      <template #icon><svg viewBox="0 0 28 24" width="40" height="34" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M3.5 6h21v3.500a2.5 2.5 0 0 0 0 5V18h-21v-3.500a2.5 2.5 0 0 0 0-5z" /><path d="M10 6.500v11" stroke-dasharray="1.6 2.2" /><path d="m15.5 15 4.5-6M15.8 9.600h.01M19.7 14.400h.01" /></svg></template>
    </MeIntro>
    <div v-if="error" class="banner bad">{{ error }}</div>
    <div v-if="offers && !offers.length" class="card empty" data-testid="offers-empty">{{ t('me.no_offers') }}</div>
    <template v-for="o in offers ?? []" :key="o.store.slug">
      <div v-for="p in o.promotions" :key="p.id" class="coupon" :class="{ ship: p.kind === 'free_delivery' }" data-testid="offer">
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
.coupon { position: relative; display: flex; align-items: stretch; background: var(--paper); border-radius: 16px; box-shadow: 0 3px 12px rgba(90, 40, 15, .08); min-width: 0; }
.stub { flex: none; width: 31%; min-width: 106px; max-width: 170px; display: grid; justify-items: center; align-content: center; gap: 4px; padding: 14px 6px; border-radius: 16px 0 0 16px; background: linear-gradient(135deg, #fdeae4, #f9d9d3); color: var(--red); border-right: 1.500px dashed #d9a79a; position: relative; text-align: center; }
.coupon.ship .stub { background: linear-gradient(135deg, #fdf1d6, #fbe3ad); border-right-color: #dcbf7c; }
.coupon.ship .stub svg { color: #b9781e; }
/* the two notches of a ticket, where the stub tears off */
.stub::before, .stub::after { content: ""; position: absolute; right: -10px; width: 18px; height: 18px; border-radius: 50%; background: var(--bg); }
.stub::before { top: -9px; } .stub::after { bottom: -9px; }
.stub b { font-size: 23px; font-weight: 900; line-height: 1.2; }
.l { flex: 1; min-width: 0; display: grid; gap: 3px; align-content: center; padding: 14px 6px 14px 16px; }
.l > b { font-size: 18.500px; font-weight: 800; }
.l .store { display: flex; align-items: center; gap: 8px; color: var(--muted); font-size: 15px; min-width: 0; flex-wrap: wrap; }
.l .store em { font-style: normal; font-size: 12.500px; padding: 1px 8px; border-radius: 6px; background: var(--gold-soft); color: #a8641a; border: 1px solid #f0d9a0; white-space: nowrap; }
.l .small { color: var(--muted); font-size: 12.5px; }
.use { flex: none; align-self: center; margin-right: 12px; display: inline-flex; align-items: center; gap: 2px; background: var(--red); color: #fff; font-weight: 700; font-size: 14.5px; border-radius: 999px; padding: 7px 8px 7px 14px; white-space: nowrap; }
.use:hover { background: var(--red-deep); }
@media (max-width: 380px) {
  .stub b { font-size: 19px; } .l { padding-left: 14px; } .l > b { font-size: 16.500px; } .use { padding: 6px 8px 6px 12px; font-size: 14px; margin-right: 8px; }
}
</style>
