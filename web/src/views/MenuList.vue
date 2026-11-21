<script setup lang="ts">
// The menu in one of three layouts. Only the arrangement differs: every
// layout shows the same items and raises the same add / remove events.
//   list     - category rail on the left, one item per row
//   featured - signature dishes in a large strip, then the full list
//   grid     - cards in columns with the picture on top
import { computed } from 'vue'
import { locale, money, pick, t } from '../i18n'
import type { Category, Item } from '../types'

const props = defineProps<{ categories: Category[]; currency: string; layout: string
  canOrder: (it: Item) => boolean; qtyOf: (it: Item) => number }>()
defineEmits<{ add: [item: Item]; dec: [item: Item] }>()

const cats = computed(() => props.categories.filter(c => c.items.length))
const mode = computed(() => ['list', 'featured', 'grid'].includes(props.layout) ? props.layout : 'list')
// Signature strip: the first dishes of the first category that can be ordered.
const featured = computed(() => (cats.value[0]?.items ?? []).filter(props.canOrder).slice(0, 4))
function scrollTo(id: number) { document.getElementById('cat-' + id)?.scrollIntoView({ behavior: 'smooth', block: 'start' }) }
</script>

<template>
  <div class="menu" :class="mode" :data-layout="mode">
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
        <button v-for="c in cats" :key="c.id" @click="scrollTo(c.id)">{{ pick(c.name, c.name_en) }}</button>
      </div>
      <div class="lists">
        <div v-for="c in cats" :key="c.id" :id="'cat-' + c.id" class="cat">
          <h2>{{ pick(c.name, c.name_en) }}</h2>
          <div class="items">
            <div v-for="it in c.items" :key="it.id" class="card item" :class="{ off: !canOrder(it) }">
              <img v-if="it.image_url" :src="it.image_url" alt="" />
              <div v-else-if="mode === 'grid'" class="ph">{{ pick(it.name, it.name_en).slice(0, 1) }}</div>
              <div class="grow">
                <div class="bold">{{ pick(it.name, it.name_en) }}</div>
                <div class="muted small" v-if="locale === 'zh' && it.name_en">{{ it.name_en }}</div>
                <div class="muted small desc" v-if="it.description">{{ it.description }}</div>
                <div class="row between" style="margin-top:8px">
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
.cats { display: flex; gap: 8px; overflow-x: auto; padding: 10px 14px; position: sticky; top: 0; background: var(--bg); z-index: 10; }
.cats button { border: 1px solid var(--line); background: #fff; border-radius: 999px; padding: 6px 14px; white-space: nowrap; cursor: pointer; }
.lists { padding: 4px 14px 14px; display: flex; flex-direction: column; gap: 14px; }
.cat { display: flex; flex-direction: column; gap: 10px; scroll-margin-top: 60px; }
.items { display: flex; flex-direction: column; gap: 10px; }
.item { display: flex; gap: 12px; }
.item img { width: 84px; height: 84px; object-fit: cover; border-radius: 8px; flex: none; }
.item.off { opacity: .55; }
.ph { display: grid; place-items: center; background: var(--brand-soft); color: var(--brand-dark); font-size: 28px; font-weight: 700; border-radius: 8px; }

/* list: category rail on the left */
.list .body { display: flex; align-items: flex-start; }
.list .cats { flex: none; width: 86px; flex-direction: column; gap: 0; padding: 0; height: 100vh; max-height: 100%; overflow-y: auto; background: #eceef0; align-self: flex-start; }
.list .cats button { border: 0; border-radius: 0; background: transparent; padding: 14px 8px; white-space: normal; font-size: 13px; border-left: 3px solid transparent; }
.list .cats button:hover, .list .cats button:focus { background: var(--bg); border-left-color: var(--brand); color: var(--brand-dark); }
.list .lists { flex: 1; min-width: 0; padding: 12px 12px 14px; }
.list .cat { scroll-margin-top: 8px; }

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
</style>
