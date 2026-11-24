<script setup lang="ts">
// Ordering by picture on "order for a customer" (delivery and pickup), for
// staff who do not remember the dish codes. The menu is shown like the
// dine-in desk: categories on the left, dish tiles with pictures (a list on a
// phone). A tap adds one; a dish with a size or a choice it must have opens
// the page's options sheet first. What has been added so far is listed beside
// the tiles (in a bar at the bottom on a phone), and "Done" goes back to the
// order, where flavour notes and add-ons are set on each line as before.
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { mn, money, t } from '../i18n'
import { hasNotes } from '../notes'
import type { Category, Item } from '../types'
import './dine/dine.css'
import './dine/strings'
import './manualStrings'

export interface PicLine { key: number; item: Item; qty: number; text: string; amount: number }

const props = defineProps<{ categories: Category[]; service: 'delivery' | 'pickup'; lines: PicLine[]; currency: string; total: number }>()
const emit = defineEmits<{ pick: [it: Item]; qty: [key: number, d: number]; close: [] }>()
const m = (v: number) => money(v, props.currency)

const phoneQ = window.matchMedia('(max-width: 760px)')
const phone = ref(phoneQ.matches)
const onPhone = () => (phone.value = phoneQ.matches)
phoneQ.addEventListener('change', onPhone)
onBeforeUnmount(() => phoneQ.removeEventListener('change', onPhone))

// only the dishes this service offers
const cats = computed(() => props.categories
  .map(c => ({ ...c, items: c.items.filter(i => props.service === 'pickup' ? i.pickup_available !== false : i.delivery_available !== false) }))
  .filter(c => c.items.length))
const catId = ref<number | null>(null)
watch(cats, c => { if (catId.value === null || !c.some(x => x.id === catId.value)) catId.value = c[0]?.id ?? null }, { immediate: true })
const shown = computed(() => cats.value.find(c => c.id === catId.value)?.items ?? [])

const qtyOf = (it: Item) => props.lines.filter(l => l.item.id === it.id).reduce((s, l) => s + l.qty, 0)
const catCount = (c: { items: Item[] }) => c.items.reduce((s, i) => s + qtyOf(i), 0)
const count = computed(() => props.lines.reduce((s, l) => s + l.qty, 0))
const needsSheet = (it: Item) => it.groups.some(g => g.max_select === 1 || g.min_select > 0)
const fromPrice = (it: Item) => it.groups.some(g => g.min_select > 0 && g.options.some(o => o.price_delta > 0))
const priceText = (it: Item) => fromPrice(it) ? t('dd.from_price', { p: m(it.price) }) : m(it.price)
function tap(it: Item) { if (!it.sold_out) emit('pick', it) }
// "−" on a phone row takes one off the last line of that dish
function less(it: Item) {
  const l = [...props.lines].reverse().find(x => x.item.id === it.id)
  if (l) emit('qty', l.key, -1)
}
const listOpen = ref(false)
function onKey(e: KeyboardEvent) { if (e.key === 'Escape' && !document.querySelector('.sheet-mask')) emit('close') }
window.addEventListener('keydown', onKey)
onBeforeUnmount(() => window.removeEventListener('keydown', onKey))
</script>

<template>
  <div class="pp-mask" @click.self="emit('close')">
    <div class="dd pp" data-testid="mo-pic">
      <div class="pp-head">
        <h3>{{ t('vo.pic_title') }}</h3>
        <span class="pp-svc">{{ service === 'pickup' ? t('vo.pickup') : t('vo.delivery') }}</span>
        <span v-if="!phone" class="muted small">{{ t('vo.pic_hint') }}</span>
        <span class="sp"></span>
        <button class="pp-x" data-testid="mo-pic-close" :aria-label="t('vo.pic_done')" @click="emit('close')">✕</button>
      </div>
      <div class="dd-obody">
        <nav class="dd-cats">
          <button v-for="c in cats" :key="c.id" :class="{ on: c.id === catId }" :data-testid="`mo-pic-cat-${c.id}`" @click="catId = c.id">
            {{ mn(c) }}<i v-if="catCount(c)">{{ catCount(c) }}</i></button>
        </nav>
        <section class="dd-dishes">
          <div v-if="!shown.length" class="dd-empty">{{ t('vo.pic_none') }}</div>
          <!-- a phone: a list -->
          <div v-if="phone" class="dd-plist">
            <div v-for="it in shown" :key="it.id" class="dd-pi" :class="{ out: it.sold_out }" :data-testid="`mo-pic-dish-${it.id}`" @click="tap(it)">
              <img v-if="it.image_url" class="th" :src="it.image_url" alt="" loading="lazy" /><span v-else class="th none">{{ mn(it).slice(0, 2) }}</span>
              <div class="tx"><div><span v-if="it.code" class="code">{{ it.code }}</span> <b>{{ mn(it) }}</b></div>
                <div class="pr">{{ priceText(it) }}
                  <span v-if="it.groups.length" class="mk">{{ t('dd.spec') }}</span><span v-if="hasNotes(it)" class="mk">{{ t('dd.notes') }}</span></div></div>
              <span v-if="it.sold_out" class="muted small r">{{ t('vo.sold_out') }}</span>
              <span v-else class="dd-stp r" @click.stop>
                <button v-if="qtyOf(it)" :data-testid="`mo-pic-less-${it.id}`" @click="less(it)">−</button>
                <b v-if="qtyOf(it)">{{ qtyOf(it) }}</b>
                <button class="f" :data-testid="`mo-pic-add-${it.id}`" @click="tap(it)">＋</button></span>
            </div>
          </div>
          <!-- a tablet or a computer: tiles -->
          <div v-else class="dd-dgrid">
            <button v-for="it in shown" :key="it.id" class="dd-dt" :class="{ on: qtyOf(it), out: it.sold_out }" :disabled="it.sold_out" :data-testid="`mo-pic-dish-${it.id}`" @click="tap(it)">
              <span v-if="qtyOf(it)" class="n">{{ qtyOf(it) }}</span>
              <span class="pic" :class="{ none: !it.image_url }" :style="it.image_url ? { backgroundImage: `url(${it.image_url})` } : {}"><i v-if="!it.image_url">{{ mn(it).slice(0, 2) }}</i></span>
              <span v-if="it.code" class="code">{{ it.code }}</span>
              <span class="tx"><b class="nm">{{ mn(it) }}</b>
                <span class="ft"><span class="pr">{{ priceText(it) }}</span>
                  <span v-if="it.groups.length" class="mk">{{ t('dd.spec') }}</span><span v-if="hasNotes(it)" class="mk">{{ t('dd.notes') }}</span>
                  <span v-if="it.sold_out" class="so">{{ t('vo.sold_out') }}</span>
                  <span v-else-if="it.stock_qty !== null && it.stock_qty <= 5" class="left">{{ t('dd.left', { n: it.stock_qty }) }}</span></span></span>
            </button>
          </div>
        </section>

        <!-- what has been added: beside the tiles, or a sheet from the bottom bar on a phone -->
        <aside v-if="!phone || listOpen" class="dd-cart" :class="{ sheet: phone }" data-testid="mo-pic-lines">
          <div class="chd"><h3>{{ t('vo.pic_added') }}</h3><span class="sp"></span>
            <button v-if="phone" class="lnk" @click="listOpen = false">✕</button></div>
          <div class="lines">
            <div v-if="!lines.length" class="muted dd-empty sm">{{ t('vo.pic_empty') }}</div>
            <div v-for="l in lines" :key="l.key" class="dd-ln" data-testid="mo-pic-line">
              <img v-if="l.item.image_url" class="th" :src="l.item.image_url" alt="" />
              <div class="lt"><b>{{ mn(l.item) }}</b><div v-if="l.text" class="opt">{{ l.text }}</div></div>
              <div class="lp">{{ m(l.amount) }}
                <span class="dd-stp s"><button @click="emit('qty', l.key, -1)">−</button><b>{{ l.qty }}</b><button class="f" @click="emit('qty', l.key, 1)">＋</button></span></div>
            </div>
          </div>
          <div class="cfoot">
            <div class="tt"><span>{{ t('vo.pic_sum', { n: count }) }}</span><b>{{ m(total) }}</b></div>
            <button class="dd-big" data-testid="mo-pic-done" @click="emit('close')">{{ t('vo.pic_done') }}</button>
          </div>
        </aside>
      </div>
      <div v-if="phone && !listOpen" class="dd-pbar">
        <button class="l" data-testid="mo-pic-open-lines" @click="listOpen = true">{{ t('vo.pic_added') }} <span class="n">{{ count }}</span><b>{{ m(total) }}</b></button>
        <button class="go" data-testid="mo-pic-done-bar" @click="emit('close')">{{ t('vo.pic_done') }}</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.pp-mask { position: fixed; inset: 0; z-index: 45; background: rgba(40, 20, 10, .5); display: grid; place-items: center; padding: 20px; }
.dd.pp { height: min(860px, calc(100dvh - 40px)); width: min(1320px, 100%); border-radius: 16px; box-shadow: 0 24px 60px rgba(0, 0, 0, .35); }
.pp-head { background: var(--red-bg); color: #fff; display: flex; align-items: center; gap: 12px; padding: 10px 14px 10px 18px; flex: none; }
.pp-head h3 { margin: 0; font-size: 18px; }
.pp-head .muted { color: var(--gold-soft); }
.pp-svc { background: var(--gold); color: var(--red-deep); border-radius: 999px; padding: 2px 10px; font-weight: 700; font-size: 13px; }
.pp-x { width: 38px; height: 38px; border-radius: 50%; border: 1px solid rgba(244, 207, 115, .5); background: rgba(255, 255, 255, .12); color: var(--gold) !important; font-size: 16px; }
.dd-dt:disabled { cursor: default; }
.dd-pi { cursor: pointer; }
.dd-ln { cursor: default; }
@media (max-width: 1000px) {
  .pp-mask { padding: 10px; }
  .dd.pp { height: calc(100dvh - 20px); }
}
/* a phone: the whole screen */
@media (max-width: 760px) {
  .pp-mask { padding: 0; }
  .dd.pp { height: 100dvh; width: 100%; border-radius: 0; }
  .pp-head { padding: 8px 10px 8px 14px; }
}
</style>
