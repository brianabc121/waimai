<script setup lang="ts">
// One store of the marketplace. On a phone it is a row with the store's
// picture on the left; from a tablet up it is a card with the cover on top.
// What it says about delivery is only the fee, the minimum and the time.
import { computed } from 'vue'
import { money, pick, promoLabel, t } from '../../i18n'
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
const initial = computed(() => pick(s.value.name, s.value.name_en).slice(0, 1))
</script>

<template>
  <router-link :to="`/s/${s.slug}`" class="mcard" :class="{ off: !open }" data-testid="mall-store" :data-slug="s.slug">
    <div class="cover" :style="cover ? { backgroundImage: `url(${cover})` } : {}">
      <span v-if="eta" class="eta">{{ eta }}</span>
      <span v-if="!open" class="veil">{{ shut }}<template v-if="opens"> · {{ opens }}</template></span>
    </div>
    <div class="pic">
      <img v-if="picture" :src="picture" alt="" loading="lazy" />
      <span v-else class="letter">{{ initial }}</span>
      <span v-if="!open" class="veil">{{ shut }}</span>
      <i v-if="inCart" class="n" data-testid="mall-cart-count">{{ inCart }}</i>
    </div>
    <div class="body">
      <h3>{{ pick(s.name, s.name_en) }}</h3>
      <p class="l1">
        <span v-if="s.rating != null" class="rate">★ {{ s.rating.toFixed(1) }}</span>
        <span v-if="s.monthly_sales">{{ t('ml.monthly', { n: s.monthly_sales }) }}</span>
        <span v-if="kinds" class="kinds">{{ kinds }}</span>
        <span class="right"><span v-if="eta" class="etatext">{{ eta }}<template v-if="far"> · </template></span>{{ far }}</span>
      </p>
      <p class="l2">
        <span v-if="opens" class="opens">{{ opens }}</span>
        <template v-if="s.delivery">
          <span v-if="s.delivery.min_order">{{ t('ml.min', { amt: money(s.delivery.min_order, s.currency) }) }}</span>
          <span v-if="s.delivery.fee">{{ t('ml.fee', { amt: money(s.delivery.fee, s.currency) }) }}</span>
          <span v-else class="free">{{ t('ml.fee_free') }}</span>
        </template>
        <span v-else-if="s.min_order_from">{{ t('ml.min_from', { amt: money(s.min_order_from, s.currency) }) }}</span>
      </p>
      <p v-if="s.tagline" class="tagline">{{ s.tagline }}</p>
      <div class="tags">
        <span v-if="s.rating == null && !s.monthly_sales" class="tag new">{{ t('ml.new') }}</span>
        <span v-if="!s.delivery && s.distance_m != null" class="tag only">{{ t('ml.pickup_only') }}</span>
        <span v-for="(p, i) in s.promos" :key="i" class="tag promo">{{ pick(p.name, p.name_en) || promoLabel(p, s.currency) }}</span>
        <span v-if="s.scheduling" class="tag">{{ t('ml.can_schedule') }}</span>
      </div>
      <div v-if="s.dishes.length" class="dishes">
        <div v-for="(d, i) in s.dishes" :key="i" class="dish">
          <img v-if="d.image_url" :src="d.image_url" alt="" loading="lazy" />
          <span v-else class="ph"></span>
          <span class="dn">{{ pick(d.name, d.name_en) }}</span>
          <b>{{ money(d.price, s.currency) }}</b>
        </div>
      </div>
    </div>
  </router-link>
</template>

<style scoped>
.mcard { display: grid; grid-template-columns: 84px minmax(0, 1fr); gap: 12px; padding: 12px; background: #fff; border-radius: 14px; color: var(--ink); }
.cover { display: none; }
.pic { position: relative; width: 84px; height: 84px; }
.pic img, .pic .letter { width: 100%; height: 100%; border-radius: 10px; object-fit: cover; display: block; }
.pic .letter { display: grid; place-items: center; background: var(--brand-soft); color: var(--brand); font-size: 30px; font-weight: 700; }
.pic .veil { position: absolute; inset: 0; border-radius: 10px; background: rgba(40, 44, 52, .62); color: #fff; font-size: 13px; font-weight: 700; display: grid; place-items: center; text-align: center; }
.pic .n { position: absolute; top: -6px; right: -6px; min-width: 19px; height: 19px; padding: 0 4px; border-radius: 10px; background: #e5352b; color: #fff; font-style: normal; font-size: 12px; font-weight: 700; display: grid; place-items: center; border: 1.5px solid #fff; }
.body { min-width: 0; }
h3 { font-size: 16.5px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.l1, .l2 { display: flex; flex-wrap: wrap; gap: 0 9px; font-size: 13px; color: #4b5563; margin-top: 3px; align-items: baseline; }
.l1 { flex-wrap: nowrap; white-space: nowrap; }
.l1 .rate { color: var(--brand); font-weight: 700; }
.l1 .kinds { display: none; overflow: hidden; text-overflow: ellipsis; }
.l1 .right { margin-left: auto; color: #6b7280; flex: none; }
.l2 .free { color: var(--brand); }
.l2 .opens { color: #6b7280; }
.tagline { font-size: 12.5px; color: #8b919a; margin-top: 2px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.tags { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 7px; }
.tags:empty { display: none; }
.tag { font-size: 12px; padding: 1px 7px; border-radius: 5px; background: #f1f2f4; color: #5b616b; white-space: nowrap; }
.tag.promo { background: #fff; color: var(--brand); border: 1px solid #f0a583; }
.tag.new { background: var(--ok-soft); color: var(--ok); }
.tag.only { background: var(--warn-soft); color: var(--warn); }
.dishes { display: flex; gap: 8px; margin-top: 9px; }
.dish { width: 76px; font-size: 12px; display: grid; }
.dish img, .dish .ph { width: 76px; height: 56px; border-radius: 7px; object-fit: cover; background: #f1f2f4; }
.dish .dn { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; margin-top: 2px; }
.dish b { color: var(--brand); }
.off h3, .off .l1, .off .l2, .off .rate { color: #9aa0a8; }

@media (min-width: 768px) {
  .mcard { grid-template-columns: minmax(0, 1fr); gap: 0; padding: 0; overflow: hidden; position: relative; transition: box-shadow .15s; }
  .mcard:hover { box-shadow: 0 6px 20px rgba(0, 0, 0, .1); }
  .cover { display: block; position: relative; height: 148px; background: var(--brand-soft) center / cover no-repeat; }
  .cover .eta { position: absolute; top: 10px; right: 10px; background: #fff; border-radius: 12px; padding: 1px 9px; font-size: 13px; font-weight: 700; }
  .cover .veil { position: absolute; inset: 0; background: rgba(40, 44, 52, .62); color: #fff; font-weight: 700; display: grid; place-items: center; text-align: center; padding: 0 10px; }
  .pic { width: 56px; height: 56px; margin: -34px 0 0 16px; z-index: 1; }
  .pic img, .pic .letter { border: 3px solid #fff; border-radius: 12px; }
  .pic .veil { display: none; }
  .body { padding: 6px 16px 14px; }
  .l1 .kinds { display: inline; }
  .l1 .etatext, .l2 .opens { display: none; }
  .tags { min-height: 21px; }
  .tags:empty { display: flex; }
}
</style>
