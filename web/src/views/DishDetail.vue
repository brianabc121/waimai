<script setup lang="ts">
// The dish page on the customer's store page: every picture of the dish, the
// short description and the longer details, and the same buttons as on the
// menu. On a phone it rises from the bottom and fills the screen; from tablet
// width up it is a dialog with the pictures beside the text.
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { locale, money, pick, t, mn, mt } from '../i18n'
import type { Item } from '../types'
import './storeStrings'

const props = defineProps<{ item: Item; currency: string; canOrder: boolean; qty: number }>()
const emit = defineEmits<{ add: []; dec: []; close: [] }>()

const pictures = computed(() => [props.item.image_url, ...(props.item.photos ?? [])].filter(Boolean))
const name = computed(() => mn(props.item))

const track = ref<HTMLElement | null>(null)
const index = ref(0)
function onScroll() {
  const el = track.value
  if (el) index.value = Math.min(Math.max(Math.round(el.scrollLeft / el.clientWidth), 0), pictures.value.length - 1)
}
function go(i: number) {
  const el = track.value
  const n = pictures.value.length
  if (el && n) el.scrollTo({ left: ((i % n + n) % n) * el.clientWidth, behavior: 'smooth' })
}

// Escape closes it, as the back gesture would.
const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') emit('close') }
onMounted(() => addEventListener('keydown', onKey))
onBeforeUnmount(() => removeEventListener('keydown', onKey))
</script>

<template>
  <div class="sheet-mask dishmask" @click.self="emit('close')">
    <div class="dish" role="dialog" :aria-label="name" data-testid="dish-detail">
      <button class="x" :aria-label="t('close')" data-testid="dish-close" @click="emit('close')">
        <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round"><path d="M6 6l12 12M18 6 6 18" /></svg></button>
      <div class="gallery">
        <div v-if="pictures.length" ref="track" class="track" @scroll.passive="onScroll">
          <img v-for="(p, i) in pictures" :key="i + p" :src="p" alt="" :loading="i ? 'lazy' : 'eager'" draggable="false" />
        </div>
        <div v-else class="ph">{{ name.slice(0, 1) }}</div>
        <template v-if="pictures.length > 1">
          <button class="arrow prev" :aria-label="t('pm.prev')" @click="go(index - 1)"><span class="rtl-flip">‹</span></button>
          <button class="arrow next" :aria-label="t('pm.next')" data-testid="dish-next" @click="go(index + 1)"><span class="rtl-flip">›</span></button>
          <span class="count" data-testid="dish-count">{{ index + 1 }}/{{ pictures.length }}</span>
          <!-- the pictures in small: which one is showing, and a way to jump -->
          <div class="thumbs">
            <button v-for="(p, i) in pictures" :key="'t' + i" :class="{ on: i === index }" :aria-label="`${i + 1}/${pictures.length}`" @click="go(i)"><img :src="p" alt="" loading="lazy" /></button>
          </div>
        </template>
      </div>
      <div class="info">
        <div class="body">
          <div class="head">
            <h2>{{ name }}</h2>
            <div class="en" v-if="locale === 'zh' && item.name_en">{{ item.name_en }}</div>
            <div class="stats">
              <span>{{ t('sold_n', { n: item.sold ?? 0 }) }}</span>
              <span v-if="item.stock_qty != null">{{ t('stock_n', { n: item.stock_qty }) }}</span>
            </div>
            <div class="bigprice">{{ money(item.price, currency) }}</div>
          </div>
          <p v-if="item.description" class="desc" data-testid="dish-desc">{{ mt(item.description, item.description_tr) }}</p>
          <section v-if="item.groups.length" class="block">
            <h3>{{ t('dd.options') }}</h3>
            <div v-for="g in item.groups" :key="g.id" class="opts">
              <div class="gname">{{ mn(g) }}</div>
              <div class="ochips"><span v-for="o in g.options" :key="o.id" :class="{ off: o.sold_out }">{{ mn(o) }}<small v-if="o.price_delta">+{{ money(o.price_delta, currency) }}</small></span></div>
            </div>
          </section>
          <section class="block">
            <h3>{{ t('dd.details') }}</h3>
            <p class="details" :class="{ muted: !item.details }" data-testid="dish-details">{{ mt(item.details, item.details_tr) || t('dd.none') }}</p>
          </section>
        </div>
        <div class="foot">
          <div class="fprice"><span class="price">{{ money(item.price, currency) }}</span></div>
          <span v-if="!canOrder" class="tag">{{ t('sold_out') }}</span>
          <button v-else-if="item.groups.length" class="btn primary" data-testid="dish-choose" @click="emit('add')">
            {{ t('choose') }}<span v-if="qty"> · {{ qty }}</span></button>
          <span v-else-if="qty" class="stepper">
            <button @click="emit('dec')" aria-label="-">−</button><span>{{ qty }}</span>
            <button class="fill" data-testid="dish-add" @click="emit('add')" aria-label="+">+</button></span>
          <button v-else class="btn primary" data-testid="dish-add" @click="emit('add')">{{ t('add_to_cart') }}</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* The dish page in the marketplace's colours: the pictures on top, a cream
   card rising over their lower edge, the price in deep red. */
.dishmask { z-index: 65; }
.dish { position: relative; background: var(--paper, #fffaf2); width: 100%; max-width: 520px; height: 100%; max-height: 100%; display: flex; flex-direction: column; overflow: hidden; }
.x { position: absolute; top: 12px; inset-inline-end: 12px; z-index: 3; width: 34px; height: 34px; border: 0; border-radius: 50%; background: rgba(255, 255, 255, .92); color: var(--ink); display: grid; place-items: center; cursor: pointer; box-shadow: 0 2px 8px rgba(0, 0, 0, .18); }
.x:hover { background: #fff; }
.gallery { position: relative; flex: none; aspect-ratio: 1; max-height: 52vh; background: #efe4d3; }
.track { display: flex; height: 100%; overflow-x: auto; scroll-snap-type: x mandatory; scrollbar-width: none; overscroll-behavior-x: contain; }
.track::-webkit-scrollbar { display: none; }
.track img { flex: 0 0 100%; width: 100%; height: 100%; object-fit: cover; scroll-snap-align: start; user-select: none; }
.gallery .ph { height: 100%; display: grid; place-items: center; background: var(--brand-soft); color: var(--brand-dark); font-size: 64px; font-weight: 700; }
.count { position: absolute; inset-inline-end: 12px; bottom: 26px; padding: 1px 9px; border-radius: 999px; background: rgba(0, 0, 0, .45); color: #fff; font-size: 12px; }
.arrow { display: none; position: absolute; top: 50%; width: 36px; height: 36px; margin-top: -18px; border: 0; border-radius: 50%; background: rgba(255, 255, 255, .9); color: var(--ink); font-size: 24px; line-height: 1; cursor: pointer; padding: 0 0 3px; box-shadow: 0 2px 8px rgba(0, 0, 0, .15); }
.arrow.prev { inset-inline-start: 12px; } .arrow.next { inset-inline-end: 12px; }
@media (hover: hover) { .gallery:hover .arrow { display: block; } }
.thumbs { display: none; }
.info { flex: 1; min-height: 0; display: flex; flex-direction: column; position: relative; margin-top: -16px; border-radius: 18px 18px 0 0; background: var(--paper, #fffaf2); }
.body { flex: 1; overflow-y: auto; padding: 18px 16px 16px; display: flex; flex-direction: column; gap: 10px; }
.head { background: #fff; border: 1px solid #efe3d1; border-radius: 14px; padding: 14px 16px; }
.head h2 { font-size: 19px; line-height: 1.3; font-weight: 700; color: var(--ink); overflow-wrap: anywhere; }
.en { color: var(--muted); font-size: 13px; margin-top: 2px; }
.stats { display: flex; gap: 6px; margin-top: 8px; }
.stats span { font-size: 11px; line-height: 18px; padding: 0 7px; border-radius: 5px; background: #f3e6d2; color: #6f5a49; }
.bigprice { display: none; margin-top: 10px; color: var(--red, #a02c20); font-size: 22px; font-weight: 700; font-variant-numeric: tabular-nums; }
.desc { margin: 0; padding: 12px 14px; border-radius: 12px; background: #fbf1e2; color: #5d4637; font-size: 14px; line-height: 1.6; overflow-wrap: anywhere; }
.block { background: #fff; border: 1px solid #efe3d1; border-radius: 14px; padding: 14px 16px; }
.block h3 { position: relative; font-size: 15px; font-weight: 700; color: var(--ink); margin-bottom: 10px; padding-inline-start: 10px; }
.block h3::before { content: ''; position: absolute; inset-inline-start: 0; top: 3px; bottom: 3px; width: 3px; border-radius: 2px; background: var(--red, #a02c20); }
.details { margin: 0; color: #5d4637; font-size: 14px; white-space: pre-wrap; overflow-wrap: anywhere; line-height: 1.75; }
.details.muted { color: #a8968a; }
.opts + .opts { margin-top: 10px; }
.gname { font-size: 13px; color: var(--muted); margin-bottom: 6px; }
.ochips { display: flex; flex-wrap: wrap; gap: 6px; }
.ochips span { font-size: 13px; line-height: 28px; padding: 0 10px; border-radius: 8px; background: #faf3e8; border: 1px solid #efe3d1; color: var(--ink); }
.ochips span.off { opacity: .5; text-decoration: line-through; }
.ochips small { margin-inline-start: 4px; color: var(--red, #a02c20); font-size: 12px; }
.foot { flex: none; display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 10px 16px calc(10px + env(safe-area-inset-bottom)); background: #fff; border-top: 1px solid #efe3d1; box-shadow: 0 -4px 14px rgba(59, 31, 20, .05); }
.foot .price { color: var(--red, #a02c20); font-size: 22px; font-weight: 700; font-variant-numeric: tabular-nums; }
.foot .btn { min-width: 140px; min-height: 44px; border-radius: 12px; font-weight: 700; background: var(--red, #a02c20); border-color: var(--red, #a02c20); }
.foot .stepper button { width: 34px; height: 34px; border-radius: 9px; background: #fff; color: var(--red, #a02c20); border: 1.5px solid var(--red, #a02c20); }
.foot .stepper > span { min-width: 36px; text-align: center; font-size: 16px; }
@media (min-width: 768px) {
  .dish { flex-direction: row; max-width: 920px; height: min(580px, calc(100vh - 48px)); border-radius: 18px; box-shadow: 0 20px 56px rgba(40, 20, 10, .3); }
  .gallery { width: 50%; height: 100%; max-height: none; aspect-ratio: auto; }
  .count { bottom: 84px; }
  /* the small pictures along the bottom of the large one */
  .thumbs { position: absolute; inset-inline-start: 0; inset-inline-end: 0; bottom: 0; display: flex; gap: 8px; padding: 12px 14px; overflow-x: auto; scrollbar-width: none; background: linear-gradient(transparent, rgba(0, 0, 0, .45)); }
  .thumbs button { flex: none; width: 52px; height: 52px; padding: 0; border: 2px solid rgba(255, 255, 255, .6); border-radius: 9px; overflow: hidden; cursor: pointer; background: none; opacity: .75; }
  .thumbs button.on { border-color: #fff; opacity: 1; }
  .thumbs img { width: 100%; height: 100%; object-fit: cover; display: block; }
  .info { margin: 0; border-radius: 0; }
  .x { background: #f3e6d2; box-shadow: none; }
  .body { padding: 24px 24px 16px; }
  .head { background: none; border: 0; padding: 0 0 12px; padding-inline-end: 44px; border-radius: 0; border-bottom: 1px solid #efe3d1; }
  .head h2 { font-size: 22px; }
  .bigprice { font-size: 26px; }
  .foot { padding: 14px 24px; box-shadow: none; }
  .foot .fprice { visibility: hidden; }
  .bigprice { display: block; }
}
</style>
