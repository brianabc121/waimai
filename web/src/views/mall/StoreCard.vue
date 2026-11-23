<script setup lang="ts">
// One store of the marketplace. On a phone it is a row with the store's
// picture on the left; from a tablet up it is a card with the cover on top.
// What it says about delivery is only the fee, the minimum and the time. A
// store near the customer that does not deliver to them is greyed and marked
// "no delivery"; opening it starts its cart on pickup when it offers that.
import { computed } from 'vue'
import { money, pick, promoLabel, t, mn, sn } from '../../i18n'
import { cart, useCart } from '../../cart'
import { cartCount, distanceText, type MallCategory, type MallStore } from '../../mall'

const props = defineProps<{ store: MallStore; categories: MallCategory[] }>()

const s = computed(() => props.store)
const open = computed(() => s.value.status === 'open')
const inCart = computed(() => cartCount(s.value.slug))
const kinds = computed(() => s.value.categories
  .map(code => props.categories.find(c => c.code === code)).filter((c): c is MallCategory => !!c).map(c => pick(c.name, c.name_en)).join(' · '))
const shut = computed(() => s.value.status === 'paused' ? t('ml.paused') : t('ml.closed'))
const opens = computed(() => s.value.status === 'closed' && s.value.opens_at
  ? t(s.value.opens_tomorrow ? 'ml.opens_tomorrow' : 'ml.opens_today', { t: s.value.opens_at }) : '')
const eta = computed(() => open.value && s.value.delivery?.eta_minutes ? t('ml.minutes', { n: s.value.delivery.eta_minutes }) : '')
const far = computed(() => s.value.distance_m != null ? distanceText(s.value.distance_m) : '')
const picture = computed(() => s.value.logo_url || s.value.cover_url)
const cover = computed(() => s.value.cover_url || s.value.logo_url)
const isNew = computed(() => s.value.rating == null && !s.value.monthly_sales)
const nodel = computed(() => s.value.no_delivery)
function opened() {
  if (nodel.value && s.value.pickup) { useCart(s.value.slug); cart.service = 'pickup' }
}
</script>

<template>
  <router-link :to="`/s/${s.slug}`" class="mcard" :class="{ off: !open || nodel, nodel }" data-testid="mall-store" :data-slug="s.slug" :data-nodelivery="nodel || undefined" @click="opened">
    <div class="cover" :style="cover ? { backgroundImage: `url(${cover})` } : {}">
      <span v-if="eta" class="eta">{{ eta }}</span>
      <span v-if="nodel" class="nodtag">{{ t('ml.no_delivery') }}</span>
      <span v-if="!open" class="veil">{{ shut }}<template v-if="opens"> · {{ opens }}</template></span>
    </div>
    <div class="pic">
      <img v-if="picture" :src="picture" alt="" loading="lazy" />
      <span v-else class="bowl"><svg viewBox="0 0 24 24" width="56%" height="56%" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round"><path d="M9 3.500c-.8.9.8 1.7 0 2.600M12 3.500c-.8.9.8 1.7 0 2.600M15 3.500c-.8.9.8 1.7 0 2.6" /><path d="M4 11h16a8 8 0 0 1-5 7.400V20H9v-1.600A8 8 0 0 1 4 11z" fill="currentColor" stroke="none" /></svg></span>
      <span v-if="!open" class="veil">{{ shut }}</span>
      <span v-if="nodel" class="nodtag" data-testid="mall-no-delivery">{{ t('ml.no_delivery') }}</span>
      <i v-if="inCart" class="n" data-testid="mall-cart-count"><svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 5h2.500l2 10h10l2-7H7" /><circle cx="9.5" cy="19" r="1.2" /><circle cx="16.5" cy="19" r="1.2" /></svg>{{ inCart }}</i>
    </div>
    <div class="body">
      <h3><span>{{ sn(s) }}</span><em v-if="isNew" class="new">{{ t('ml.new') }}</em></h3>
      <p v-if="s.rating != null || s.monthly_sales || kinds" class="l1">
        <span v-if="s.rating != null" class="rate">★ {{ s.rating.toFixed(1) }}</span>
        <span v-if="s.monthly_sales">{{ t('ml.monthly', { n: s.monthly_sales }) }}</span>
        <span v-if="kinds" class="kinds">{{ kinds }}</span>
      </p>
      <p class="l2">
        <span v-if="far" class="ic"><svg viewBox="0 0 24 24" width="17" height="17" fill="currentColor"><path fill-rule="evenodd" d="M12 22s7-6.3 7-11.800a7 7 0 1 0-14 0C5 15.7 12 22 12 22zm0-9.200a2.6 2.6 0 1 0 0-5.2 2.6 2.6 0 0 0 0 5.200z" /></svg>{{ far }}</span>
        <span v-if="eta" class="etatext">{{ eta }}</span>
        <span v-if="opens" class="opens">{{ opens }}</span>
        <span v-if="nodel" class="nod">{{ t('ml.no_delivery') }}</span>
        <template v-else-if="s.delivery">
          <span class="ic"><svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M2.5 6.500h11v10h-11zM13.5 10h4l3 3.500v3h-7" /><circle cx="7" cy="17.5" r="1.8" /><circle cx="17" cy="17.5" r="1.8" /></svg>
            <template v-if="s.delivery.fee">{{ t('ml.fee_word') }} <b>{{ money(s.delivery.fee, s.currency) }}</b></template>
            <b v-else>{{ t('ml.fee_free') }}</b></span>
          <span v-if="s.delivery.min_order">{{ t('ml.min', { amt: money(s.delivery.min_order, s.currency) }) }}</span>
        </template>
        <span v-else-if="s.min_order_from && !nodel">{{ t('ml.min_from', { amt: money(s.min_order_from, s.currency) }) }}</span>
      </p>
      <p v-if="s.tagline" class="tagline">{{ s.tagline }}</p>
      <div class="tags">
        <span v-for="(p, i) in s.promos" :key="i" class="tag promo">{{ pick(p.name, p.name_en) || promoLabel(p, s.currency) }}</span>
        <span v-if="nodel && s.pickup" class="tag pick">{{ t('ml.pickup_ok') }}</span>
        <span v-if="s.scheduling" class="tag gold">{{ t('ml.can_schedule') }}</span>
      </div>
      <div v-if="s.dishes.length" class="dishes">
        <div v-for="(d, i) in s.dishes" :key="i" class="dish">
          <img v-if="d.image_url" :src="d.image_url" alt="" loading="lazy" />
          <span v-else class="ph"></span>
          <span class="dn">{{ mn(d) }}</span>
          <b>{{ money(d.price, s.currency) }}</b>
        </div>
      </div>
    </div>
  </router-link>
</template>

<style scoped>
.mcard { display: grid; grid-template-columns: 96px minmax(0, 1fr); gap: 14px; padding: 12px 12px 12px 14px; background: var(--paper); border: 1px solid var(--line); border-radius: 14px; color: var(--ink); box-shadow: inset 4px 0 0 var(--red); overflow: hidden; }
.cover { display: none; }
.pic { position: relative; width: 96px; height: 96px; }
.pic img, .pic .bowl { width: 100%; height: 100%; border-radius: 12px; object-fit: cover; display: block; }
.bowl { display: grid !important; place-items: center; background: #9a3018; color: #fbe9c2; }
.pic .veil { position: absolute; inset: 0; border-radius: 12px; background: rgba(59, 31, 20, .62); color: #fff; font-size: 13px; font-weight: 700; display: grid; place-items: center; text-align: center; }
.pic .n { position: absolute; top: -5px; inset-inline-end: -7px; height: 24px; padding: 0 8px 0 6px; border-radius: 12px; background: var(--paper); color: var(--red); font-style: normal; font-size: 14px; font-weight: 800; display: flex; align-items: center; gap: 3px; border: 1.5px solid var(--red); }
.body { min-width: 0; }
h3 { font-size: 19px; font-weight: 800; display: flex; align-items: center; gap: 8px; min-width: 0; }
h3 span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; min-width: 0; }
h3 .new { flex: none; font-style: normal; font-size: 12.500px; font-weight: 600; padding: 1px 8px; border-radius: 6px; background: var(--gold-soft); color: var(--red-deep); }
.l1, .l2 { display: flex; flex-wrap: wrap; gap: 2px 0; font-size: 14px; color: var(--muted); margin-top: 4px; align-items: center; }
.l1 > span + span, .l2 > span + span { margin-inline-start: 10px; padding-inline-start: 10px; border-inline-start: 1px solid #cbb9a2; }
.l1 .rate { color: var(--red); font-weight: 700; }
.l1 .kinds { display: none; overflow: hidden; text-overflow: ellipsis; }
.l2 .ic { display: inline-flex; align-items: center; gap: 5px; }
.l2 .ic svg { flex: none; color: #8c7461; }
.l2 b { color: var(--red); font-weight: 800; font-size: 15.500px; }
.tagline { font-size: 12.500px; color: var(--muted); margin-top: 3px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.tags { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 8px; }
.tags:empty { display: none; }
.tag { font-size: 12.500px; padding: 2px 9px; border-radius: 7px; background: #efe5d6; color: var(--ink); white-space: nowrap; border: 1px solid transparent; }
.tag.promo { background: none; color: var(--red); border-color: #c98a78; }
.tag.gold { background: var(--gold-soft); color: var(--red-deep); }
.dishes { display: flex; gap: 8px; margin-top: 10px; }
.dish { width: 76px; font-size: 12px; display: grid; }
.dish img, .dish .ph { width: 76px; height: 56px; border-radius: 8px; object-fit: cover; background: #efe5d6; }
.dish .dn { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; margin-top: 2px; }
.dish b { color: var(--red); }
.off h3, .off .l1, .off .l2, .off .rate, .off .l2 b { color: #a6978a; }
.mcard.off { box-shadow: inset 4px 0 0 #cdbda8; }
/* near but no delivery: grey, with a ribbon across the picture */
.nodel { background: #f6f0e7; }
.nodel .pic img, .nodel .pic .bowl { filter: grayscale(1) opacity(.7); }
.nodel .dish img { filter: grayscale(1) opacity(.7); }
.pic .nodtag { position: absolute; inset-inline-start: 0; inset-inline-end: 0; bottom: 0; border-radius: 0 0 12px 12px; background: rgba(59, 31, 20, .78); color: #fff; font-size: 12px; font-weight: 700; text-align: center; padding: 2px 0; letter-spacing: 1px; }
.nodel .pic .veil { bottom: 20px; border-radius: 12px 12px 0 0; font-size: 12px; }
.l2 .nod { color: #8a4a3a; font-weight: 700; display: inline-flex; align-items: center; gap: 4px; }
.l2 .nod::before { content: ""; width: 12px; height: 12px; border-radius: 50%; border: 1.5px solid currentColor; background: linear-gradient(135deg, transparent 44%, currentColor 44%, currentColor 56%, transparent 56%); }
.tag.pick { background: #fff; color: #6d5a4b; border-color: #d6c6b1; }
.cover .nodtag { display: none; }

/* the short row */

@media (min-width: 768px) {
  .mcard { grid-template-columns: minmax(0, 1fr); gap: 0; padding: 0; position: relative; transition: box-shadow .15s; box-shadow: none; }
  .mcard.off { box-shadow: none; }
  .mcard:hover { box-shadow: 0 6px 20px rgba(90, 40, 15, .14); }
  .cover { display: block; position: relative; height: 148px; background: var(--gold-soft) center / cover no-repeat; }
  .cover .eta { position: absolute; top: 10px; inset-inline-end: 10px; background: var(--paper); border-radius: 12px; padding: 1px 9px; font-size: 13px; font-weight: 700; }
  .cover .veil { position: absolute; inset: 0; background: rgba(59, 31, 20, .62); color: #fff; font-weight: 700; display: grid; place-items: center; text-align: center; padding: 0 10px; }
  .pic { width: 56px; height: 56px; margin: -34px 0 0 16px; z-index: 1; }
  .pic img, .pic .bowl { border: 3px solid var(--paper); border-radius: 12px; }
  .pic .veil, .pic .nodtag { display: none; }
  .nodel .cover { filter: grayscale(1) opacity(.75); }
  .cover .nodtag { display: block; position: absolute; inset-inline-start: 10px; top: 10px; background: rgba(59, 31, 20, .82); color: #fff; border-radius: 7px; padding: 2px 10px; font-size: 13px; font-weight: 700; letter-spacing: 1px; }
  .body { padding: 6px 16px 14px; }
  h3 { font-size: 17.500px; }
  .l1 .kinds { display: inline; }
  .l2 .etatext, .l2 .opens { display: none; }
  .tags { min-height: 25px; }
  .tags:empty { display: flex; }
}
</style>
