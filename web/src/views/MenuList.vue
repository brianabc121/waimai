<script setup lang="ts">
// The menu in one of three layouts. Only the arrangement differs: every
// layout shows the same items and raises the same add / remove events.
//   list     - category rail on the left, one item per row
//   featured - signature dishes in a large strip, then the full list
//   grid     - cards in columns with the picture on top
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { locale, money, pick, t } from '../i18n'
import type { Category, Item } from '../types'

const props = defineProps<{ categories: Category[]; currency: string; layout: string
  canOrder: (it: Item) => boolean; qtyOf: (it: Item) => number
  // Show how many of a dish were sold and how many are left (the delivery store page).
  stats?: boolean }>()
defineEmits<{ add: [item: Item]; dec: [item: Item] }>()

const cats = computed(() => props.categories.filter(c => c.items.length))
const mode = computed(() => ['list', 'featured', 'grid'].includes(props.layout) ? props.layout : 'list')
// Signature strip: the first dishes of the first category that can be ordered.
const featured = computed(() => (cats.value[0]?.items ?? []).filter(props.canOrder).slice(0, 4))
// How many dishes of a category are in the cart, for the badge on the rail.
const inCart = (c: Category) => c.items.reduce((n, it) => n + props.qtyOf(it), 0)

// The rail marks the category that is being read.
const root = ref<HTMLElement | null>(null)
const active = ref(0)
let jumping = 0
function spy() {
  if (Date.now() < jumping || !root.value) return
  const line = (root.value.querySelector('.cats') as HTMLElement | null)?.getBoundingClientRect().top ?? 0
  let cur = cats.value[0]?.id ?? 0
  for (const c of cats.value) {
    const el = document.getElementById('cat-' + c.id)
    if (el && el.getBoundingClientRect().top <= Math.max(line, 0) + 48) cur = c.id
  }
  active.value = cur
}
function scrollTo(id: number) {
  active.value = id
  jumping = Date.now() + 700
  document.getElementById('cat-' + id)?.scrollIntoView({ behavior: 'smooth', block: 'start' })
}
// capture: the menu may sit inside a scrolling box (the preview in the back office)
onMounted(() => { addEventListener('scroll', spy, { capture: true, passive: true }); spy() })
onBeforeUnmount(() => removeEventListener('scroll', spy, { capture: true }))
defineExpose({ scrollTo })
</script>

<template>
  <div ref="root" class="menu" :class="mode" :data-layout="mode">
    <div v-if="mode === 'featured' && featured.length" class="hero">
      <h2>{{ t('signature') }}</h2>
      <div class="strip">
        <button v-for="it in featured" :key="it.id" class="hcard" @click="$emit('add', it)">
          <img v-if="it.image_url" :src="it.image_url" alt="" /><div v-else class="ph">{{ pick(it.name, it.name_en).slice(0, 1) }}</div>
          <div class="bold">{{ pick(it.name, it.name_en) }}</div>
          <div class="row between"><span class="price">{{ money(it.price, currency) }}</span><span class="plus">+<template v-if="qtyOf(it)"> {{ qtyOf(it) }}</template></span></div>
        </button>
      </div>
    </div>

    <div class="body">
      <div class="cats">
        <button v-for="c in cats" :key="c.id" :class="{ on: active === c.id }" :data-cat="c.id" @click="scrollTo(c.id)">
          {{ pick(c.name, c.name_en) }}<span v-if="c.required" class="must">{{ t('required') }}</span>
          <span v-if="inCart(c)" class="badge">{{ inCart(c) }}</span>
        </button>
      </div>
      <div class="lists">
        <div v-for="c in cats" :key="c.id" :id="'cat-' + c.id" class="cat">
          <h2>{{ pick(c.name, c.name_en) }}<span v-if="c.required" class="must">{{ t('required') }}</span></h2>
          <div class="items">
            <div v-for="it in c.items" :key="it.id" class="card item" :class="{ off: !canOrder(it) }">
              <img v-if="it.image_url" :src="it.image_url" alt="" />
              <div v-else class="ph">{{ pick(it.name, it.name_en).slice(0, 1) }}</div>
              <div class="grow">
                <div class="bold">{{ pick(it.name, it.name_en) }}</div>
                <div class="muted small" v-if="locale === 'zh' && it.name_en">{{ it.name_en }}</div>
                <div class="muted small desc" v-if="it.description">{{ it.description }}</div>
                <div class="stats" v-if="stats">
                  <span>{{ t('sold_n', { n: it.sold ?? 0 }) }}</span>
                  <span v-if="it.stock_qty != null">{{ t('stock_n', { n: it.stock_qty }) }}</span>
                </div>
                <div class="row between buy">
                  <span class="price bold">{{ money(it.price, currency) }}</span>
                  <span v-if="!canOrder(it)" class="tag">{{ t('sold_out') }}</span>
                  <button v-else-if="it.groups.length" class="btn small primary" @click="$emit('add', it)">
                    {{ t('choose') }}<span v-if="qtyOf(it)"> · {{ qtyOf(it) }}</span></button>
                  <span v-else class="stepper">
                    <template v-if="qtyOf(it)">
                      <button @click="$emit('dec', it)" aria-label="-">−</button>
                      <span>{{ qtyOf(it) }}</span>
                    </template>
                    <button class="fill" @click="$emit('add', it)" aria-label="+">+</button>
                  </span>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.cats { display: flex; gap: 8px; overflow-x: auto; padding: 10px 14px; position: sticky; top: var(--menu-top, 0px); background: var(--bg); z-index: 10; }
.cats button { position: relative; border: 1px solid var(--line); background: #fff; border-radius: 999px; padding: 6px 14px; white-space: nowrap; cursor: pointer; }
.cats button.on { border-color: var(--brand); color: var(--brand-dark); }
.badge { display: inline-block; margin-left: 5px; vertical-align: 1px; min-width: 16px; height: 16px; padding: 0 4px; border-radius: 999px; background: #e5352b; color: #fff; font-size: 11px; font-weight: 700; line-height: 16px; text-align: center; }
.must { display: inline-block; margin-left: 4px; padding: 0 5px; border-radius: 4px; background: var(--brand-soft); color: var(--brand-dark); font-size: 11px; font-weight: 600; line-height: 16px; vertical-align: middle; white-space: nowrap; }
.lists { padding: 4px 14px 14px; display: flex; flex-direction: column; gap: 14px; }
.cat { display: flex; flex-direction: column; gap: 10px; scroll-margin-top: calc(var(--menu-top, 0px) + 60px); }
.items { display: flex; flex-direction: column; gap: 10px; }
.item { display: flex; gap: 12px; }
.item img, .item .ph { width: 84px; height: 84px; object-fit: cover; border-radius: 8px; flex: none; }
.item.off { opacity: .55; }
.item > .grow { min-width: 0; display: flex; flex-direction: column; }
.buy { margin-top: auto; padding-top: 8px; }
.stats { display: flex; flex-direction: column; color: #8a94a3; font-size: 12px; line-height: 1.5; margin-top: 2px; }
.ph { display: grid; place-items: center; background: var(--brand-soft); color: var(--brand-dark); font-size: 28px; font-weight: 700; border-radius: 8px; }

/* list: a grey category rail on the left, dishes with a large picture on the right */
/* the grey of the rail runs down the whole menu, however short the rail is */
.list .body { --rail: 92px; display: flex; align-items: flex-start; background: linear-gradient(to right, #f4f4f4 var(--rail), #fff 0); }
.list .cats { flex: none; width: var(--rail); flex-direction: column; gap: 0; padding: 0 0 96px; max-height: calc(100vh - var(--menu-top, 0px)); overflow-y: auto; background: none; align-self: flex-start; scrollbar-width: none; }
.list .cats::-webkit-scrollbar { display: none; }
.list .cats button { border: 0; border-radius: 0; background: transparent; color: #8b8f96; padding: 16px 8px; white-space: normal; font-size: 13px; line-height: 1.35; }
.list .cats button.on { background: #fff; color: var(--ink); font-weight: 600; }
.list .cats .badge { position: absolute; top: 4px; right: 4px; margin: 0; }
.list .cats .must { display: block; width: fit-content; margin: 3px auto 0; }
.list .lists { flex: 1; min-width: 0; padding: 0 12px 14px; gap: 6px; }
.list .cat { scroll-margin-top: var(--menu-top, 0px); gap: 0; }
.list .cat > h2 { font-size: 13px; font-weight: 600; color: var(--muted); padding: 12px 0 2px; }
.list .items { gap: 0; }
.list .item { border: 0; border-radius: 0; box-shadow: none; background: none; padding: 10px 0; gap: 12px; }
.list .item img, .list .item .ph { width: 104px; height: 104px; }
.list .item .bold { font-size: 16px; line-height: 1.3; }
.list .item .price { color: #e5462b; font-size: 17px; }

/* featured: signature strip */
.hero { padding: 12px 14px 0; }
.strip { display: flex; gap: 10px; overflow-x: auto; padding: 10px 0 4px; }
.hcard { flex: none; width: 150px; text-align: left; border: 1px solid var(--line); background: #fff; border-radius: 12px; padding: 8px; cursor: pointer; display: flex; flex-direction: column; gap: 4px; }
.hcard img, .hcard .ph { width: 100%; height: 96px; object-fit: cover; border-radius: 8px; }
.plus { background: var(--brand); color: #fff; border-radius: 999px; padding: 0 9px; font-weight: 700; }

/* grid: cards in columns */
.grid .items { display: grid; grid-template-columns: repeat(auto-fill, minmax(158px, 1fr)); }
.grid .item { flex-direction: column; gap: 8px; }
.grid .item img, .grid .item .ph { width: 100%; height: 104px; }
.grid .desc { display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; }

/* where the menu has room (a tablet, a computer; not the phone preview in the back office):
   dishes side by side, a roomier category rail */
.menu { container: menu / inline-size; }
@container menu (min-width: 640px) {
  .featured .items { display: grid; grid-template-columns: repeat(auto-fill, minmax(300px, 1fr)); gap: 12px; }
  .list .items { display: grid; grid-template-columns: repeat(auto-fill, minmax(280px, 1fr)); gap: 4px 28px; }
  .list .body { --rail: 132px; }
  .list .cats button { font-size: 14px; padding: 15px 12px; }
  .list .cats button:hover { color: var(--ink); }
  .list .lists { padding: 0 20px 20px; }
  .list .item img, .list .item .ph { width: 120px; height: 120px; }
  .item img, .item .ph { width: 96px; height: 96px; }
  .hcard { width: 190px; } .hcard img, .hcard .ph { height: 120px; }
  .grid .items { grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); gap: 12px; }
  .grid .item img, .grid .item .ph { height: 132px; }
  .cats button:hover { border-color: var(--brand); color: var(--brand-dark); }
}
</style>
