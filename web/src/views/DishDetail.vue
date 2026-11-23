<script setup lang="ts">
// The dish page on the customer's store page: every picture of the dish, the
// short description and the longer details, and the same buttons as on the
// menu. On a phone it rises from the bottom and fills the screen; from tablet
// width up it is a dialog with the pictures beside the text.
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { locale, money, pick, t } from '../i18n'
import type { Item } from '../types'
import './storeStrings'

const props = defineProps<{ item: Item; currency: string; canOrder: boolean; qty: number }>()
const emit = defineEmits<{ add: []; dec: []; close: [] }>()

const pictures = computed(() => [props.item.image_url, ...(props.item.photos ?? [])].filter(Boolean))
const name = computed(() => pick(props.item.name, props.item.name_en))

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
        <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round"><path d="M6 6l12 12M18 6 6 18" /></svg></button>
      <div class="gallery">
        <div v-if="pictures.length" ref="track" class="track" @scroll.passive="onScroll">
          <img v-for="(p, i) in pictures" :key="i + p" :src="p" alt="" :loading="i ? 'lazy' : 'eager'" draggable="false" />
        </div>
        <div v-else class="ph">{{ name.slice(0, 1) }}</div>
        <template v-if="pictures.length > 1">
          <button class="arrow prev" :aria-label="t('pm.prev')" @click="go(index - 1)"><span class="rtl-flip">‹</span></button>
          <button class="arrow next" :aria-label="t('pm.next')" data-testid="dish-next" @click="go(index + 1)"><span class="rtl-flip">›</span></button>
          <span class="count" data-testid="dish-count">{{ index + 1 }}/{{ pictures.length }}</span>
        </template>
      </div>
      <div class="info">
        <div class="body">
          <h2>{{ name }}</h2>
          <div class="muted" v-if="locale === 'zh' && item.name_en">{{ item.name_en }}</div>
          <div class="stats">
            <span>{{ t('sold_n', { n: item.sold ?? 0 }) }}</span>
            <span v-if="item.stock_qty != null">{{ t('stock_n', { n: item.stock_qty }) }}</span>
          </div>
          <p v-if="item.description" class="desc" data-testid="dish-desc">{{ item.description }}</p>
          <section v-if="item.groups.length" class="block">
            <h3>{{ t('dd.options') }}</h3>
            <p v-for="g in item.groups" :key="g.id" class="opts"><b>{{ pick(g.name, g.name_en) }}</b>{{ g.options.map(o => pick(o.name, o.name_en)).join(' / ') }}</p>
          </section>
          <section class="block">
            <h3>{{ t('dd.details') }}</h3>
            <p class="details" :class="{ muted: !item.details }" data-testid="dish-details">{{ item.details || t('dd.none') }}</p>
          </section>
        </div>
        <div class="foot">
          <span class="price">{{ money(item.price, currency) }}</span>
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
.dishmask { z-index: 65; }
.dish { position: relative; background: #fff; width: 100%; max-width: 520px; height: 100%; max-height: 100%; display: flex; flex-direction: column; overflow: hidden; }
.x { position: absolute; top: 10px; inset-inline-end: 10px; z-index: 3; width: 34px; height: 34px; border: 0; border-radius: 50%; background: rgba(0, 0, 0, .45); color: #fff; display: grid; place-items: center; cursor: pointer; }
.gallery { position: relative; flex: none; aspect-ratio: 1; max-height: 56vh; background: #f4f1ec; }
.track { display: flex; height: 100%; overflow-x: auto; scroll-snap-type: x mandatory; scrollbar-width: none; overscroll-behavior-x: contain; }
.track::-webkit-scrollbar { display: none; }
.track img { flex: 0 0 100%; width: 100%; height: 100%; object-fit: cover; scroll-snap-align: start; user-select: none; }
.gallery .ph { height: 100%; display: grid; place-items: center; background: var(--brand-soft); color: var(--brand-dark); font-size: 64px; font-weight: 700; }
.count { position: absolute; inset-inline-end: 12px; bottom: 12px; padding: 2px 10px; border-radius: 999px; background: rgba(0, 0, 0, .5); color: #fff; font-size: 13px; }
.arrow { display: none; position: absolute; top: 50%; width: 36px; height: 36px; margin-top: -18px; border: 0; border-radius: 50%; background: rgba(0, 0, 0, .45); color: #fff; font-size: 24px; line-height: 1; cursor: pointer; padding: 0 0 3px; }
.arrow.prev { inset-inline-start: 10px; } .arrow.next { inset-inline-end: 10px; }
@media (hover: hover) { .gallery:hover .arrow { display: block; } }
.info { flex: 1; min-height: 0; display: flex; flex-direction: column; }
.body { flex: 1; overflow-y: auto; padding: 16px 18px; }
.body h2 { font-size: 21px; line-height: 1.3; overflow-wrap: anywhere; }
.stats { display: flex; gap: 12px; color: #8a94a3; font-size: 13px; margin-top: 4px; }
.desc { margin: 10px 0 0; color: #4d5157; overflow-wrap: anywhere; }
.block { margin-top: 16px; padding-top: 14px; border-top: 1px solid #f0ebe4; }
.block h3 { font-size: 15px; margin-bottom: 6px; }
.details { margin: 0; color: #4d5157; white-space: pre-wrap; overflow-wrap: anywhere; line-height: 1.7; }
.opts { margin: 4px 0 0; color: #5d6168; font-size: 14px; overflow-wrap: anywhere; }
.opts b { font-weight: 600; color: var(--ink); margin-inline-end: 8px; }
.foot { flex: none; display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 12px 18px calc(12px + env(safe-area-inset-bottom)); border-top: 1px solid var(--line); }
.foot .price { color: #e5462b; font-size: 22px; font-weight: 700; }
.foot .btn { min-width: 132px; border-radius: 999px; }
@media (min-width: 768px) {
  .dish { flex-direction: row; max-width: 880px; height: min(560px, calc(100vh - 48px)); border-radius: 16px; box-shadow: 0 16px 48px rgba(0, 0, 0, .25); }
  .gallery { width: 50%; height: 100%; max-height: none; aspect-ratio: auto; }
  .body { padding: 22px 24px; }
  .foot { padding: 14px 24px; }
}
</style>
