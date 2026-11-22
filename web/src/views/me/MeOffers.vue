<script setup lang="ts">
// The offers running at the customer's stores. They apply by themselves at
// checkout, so this page only says what there is and leads to the store.
import { ref, watch } from 'vue'
import { locale, pick, promoLabel, t } from '../../i18n'
import type { Promotion } from '../../types'
import MePage from './MePage.vue'
import { describe, homeStore, me, myOffers, type StoreOffers } from './me'

const offers = ref<StoreOffers[] | null>(null)
const error = ref('')
watch(() => me.profile?.id, async id => {
  if (!id) return
  try { offers.value = await myOffers(homeStore()?.slug) } catch (e) { error.value = describe(e) }
}, { immediate: true })

function terms(p: Promotion): string {
  const out: string[] = []
  if (p.applies_delivery && !p.applies_pickup) out.push(t('mo.delivery_only'))
  if (p.applies_pickup && !p.applies_delivery) out.push(t('mo.pickup_only'))
  if (p.daily_start !== p.daily_end) out.push(t('mo.daily', { a: p.daily_start, b: p.daily_end }))
  if (p.ends_at) out.push(t('mo.until', { d: new Date(p.ends_at).toLocaleDateString(locale.value === 'zh' ? 'zh-CN' : 'en-GB') }))
  return out.join(' · ')
}
</script>

<template>
  <MePage :title="t('mo.title')">
    <p class="note" style="padding:0 6px">{{ t('mo.hint') }}</p>
    <div v-if="error" class="banner bad">{{ error }}</div>
    <div v-if="offers && !offers.length" class="card empty" data-testid="offers-empty">{{ t('me.no_offers') }}</div>
    <template v-for="o in offers ?? []" :key="o.store.slug">
      <div v-for="p in o.promotions" :key="p.id" class="coupon" data-testid="offer">
        <div class="l"><b>{{ promoLabel(p, o.currency) }}</b>
          <span>{{ pick(p.name, p.name_en) }}</span>
          <span class="small">{{ pick(o.store.name, o.store.name_en) }}<template v-if="terms(p)"> · {{ terms(p) }}</template></span></div>
        <router-link :to="`/s/${o.store.slug}`" class="btn small primary">{{ t('mo.use') }}</router-link>
      </div>
    </template>
  </MePage>
</template>

<style scoped>
.coupon { display: flex; align-items: center; gap: 12px; background: #fff; border-radius: 14px; padding: 14px; border-left: 5px solid var(--brand); }
.coupon .l { flex: 1; min-width: 0; display: grid; gap: 2px; }
.coupon b { font-size: 18px; color: var(--brand-dark); }
.coupon .small { color: var(--muted); }
</style>
