<script setup lang="ts">
// What is in the cart: the baskets, their dishes and the fees. Used in the
// sheet over the cart bar and beside the menu on a computer.
import { computed, ref } from 'vue'
import { cart, linePrice, MAX_BASKETS, moveLine, type CartLine } from '../cart'
import { money, pick, t } from '../i18n'
import type { Item, Storefront } from '../types'

const props = defineProps<{ store: Storefront; items: Map<number, Item>; currency: string; orderable: (it: Item) => boolean }>()
const emit = defineEmits<{ empty: [] }>()

const baskets = computed(() => {
  if (!props.store.branch.baskets_enabled) return []
  const top = Math.max(cart.basket, ...cart.lines.map(l => l.basket))
  return Array.from({ length: Math.min(MAX_BASKETS, Math.max(2, top + 1)) }, (_, i) => i + 1)
})
// With baskets off everything is shown as one list.
const lines = computed(() => baskets.value.length ? cart.lines.filter(l => l.basket === cart.basket) : cart.lines)
const sum = (ls: CartLine[]) => ls.reduce((s, l) => s + (linePrice(l, props.items) ?? 0) * l.qty, 0)
const used = computed(() => new Set(cart.lines.map(l => l.basket)).size)
const bad = (l: CartLine) => { const it = props.items.get(l.item_id); return !it || !props.orderable(it) || linePrice(l, props.items) === null }
const name = (l: CartLine) => { const it = props.items.get(l.item_id); return it ? pick(it.name, it.name_en) : '—' }
function optionNames(l: CartLine) {
  const it = props.items.get(l.item_id)
  if (!it) return ''
  const all = new Map(it.groups.flatMap(g => g.options).map(o => [o.id, o]))
  return l.option_ids.map(id => all.get(id)).filter(Boolean).map(o => pick(o!.name, o!.name_en)).join(' · ')
}

function change(l: CartLine, d: number) {
  l.qty = Math.min(99, l.qty + d)
  if (l.qty <= 0) cart.lines.splice(cart.lines.indexOf(l), 1)
  if (cart.lines.length === 0) emit('empty')
}
function clear() {
  if (!confirm(t(baskets.value.length ? 'sf.clear_basket_confirm' : 'clear_confirm'))) return
  cart.lines = baskets.value.length ? cart.lines.filter(l => l.basket !== cart.basket) : []
  if (cart.lines.length === 0) { cart.basket = 1; emit('empty') }
}

// Moving: with two baskets the dish simply goes to the other one; with more, the customer picks.
const moving = ref('')
function move(l: CartLine) {
  const others = baskets.value.filter(b => b !== l.basket)
  if (others.length === 1) moveLine(l, others[0])
  else moving.value = moving.value === l.key ? '' : l.key
}
function moveTo(l: CartLine, b: number) { moving.value = ''; moveLine(l, b) }
</script>

<template>
  <div class="cartpanel">
    <div class="baskets">
      <label v-for="b in baskets" :key="b" class="bk" :data-basket="b">
        <input type="radio" name="basket" :value="b" v-model="cart.basket" /><span>{{ t('sf.basket_n', { n: b }) }}</span>
      </label>
      <span class="grow"></span>
      <button v-if="lines.length" class="plain" data-testid="clear-basket" @click="clear">{{ baskets.length ? t('sf.clear_basket') : t('clear') }}</button>
    </div>
    <div class="clines">
      <p v-if="!lines.length" class="muted none">{{ cart.lines.length ? t('sf.basket_empty') : t('cart_empty') }}</p>
      <div v-for="l in lines" :key="l.key" class="cline" :data-line="name(l)">
        <img v-if="items.get(l.item_id)?.image_url" :src="items.get(l.item_id)!.image_url" alt="" />
        <div v-else class="ph">{{ name(l).slice(0, 1) }}</div>
        <div class="grow">
          <div class="nm">{{ name(l) }}</div>
          <div class="muted small" v-if="optionNames(l)">{{ optionNames(l) }}</div>
          <div class="muted small" v-if="l.note">{{ t('note') }}: {{ l.note }}</div>
          <div v-if="bad(l)" class="small" style="color:var(--bad)">{{ t('unavailable') }}</div>
          <div v-else class="pr">{{ money(linePrice(l, items)!, currency) }}</div>
        </div>
        <div class="acts">
          <button v-if="baskets.length" class="plain" data-testid="move-basket" @click="move(l)">{{ t('sf.move_basket') }}</button>
          <div v-if="moving === l.key" class="targets">
            <button v-for="b in baskets.filter(x => x !== l.basket)" :key="b" class="plain" @click="moveTo(l, b)">{{ t('sf.basket_n', { n: b }) }}</button>
          </div>
          <span class="stepper"><button @click="change(l, -1)" aria-label="-">−</button><span>{{ l.qty }}</span><button class="fill" @click="change(l, 1)" aria-label="+">+</button></span>
        </div>
      </div>
    </div>
    <div class="fees" v-if="cart.lines.length">
      <div><span>{{ t('packaging_fee') }}</span><b>{{ money(store.branch.packaging_fee, currency) }}</b></div>
      <div v-if="baskets.length" data-testid="basket-subtotal"><span>{{ t('sf.basket_subtotal') }}</span><b>{{ money(sum(lines), currency) }}</b></div>
      <div v-if="!baskets.length || used > 1" data-testid="cart-subtotal"><span>{{ baskets.length ? t('sf.all_baskets') : t('subtotal') }}</span><b>{{ money(sum(cart.lines), currency) }}</b></div>
    </div>
  </div>
</template>

<style scoped>
.cartpanel { display: flex; flex-direction: column; min-height: 0; }
.baskets { display: flex; flex-wrap: wrap; align-items: center; gap: 6px 14px; padding: 12px 16px; border-bottom: 1px solid #f0f0f0; }
.bk { display: inline-flex; align-items: center; gap: 6px; cursor: pointer; font-size: 15px; }
.bk input { appearance: none; width: 22px; height: 22px; margin: 0; border: 1.5px solid #cfd3d9; border-radius: 50%; display: grid; place-items: center; cursor: pointer; }
.bk input:checked { border-color: var(--brand); }
.bk input:checked::after { content: ''; width: 12px; height: 12px; border-radius: 50%; background: var(--brand); }
.plain { border: 0; background: none; padding: 2px 0; color: #9a9ea5; font-size: 13px; cursor: pointer; }
.plain:hover { color: var(--ink); }
.clines { overflow-y: auto; padding: 4px 16px; min-height: 120px; flex: 1; }
.none { text-align: center; padding: 28px 0; margin: 0; }
.cline { display: flex; gap: 12px; padding: 10px 0; }
.cline img, .cline .ph { width: 56px; height: 56px; border-radius: 8px; object-fit: cover; flex: none; }
.ph { display: grid; place-items: center; background: var(--brand-soft); color: var(--brand-dark); font-size: 22px; font-weight: 700; }
.nm { font-weight: 700; font-size: 15px; line-height: 1.3; }
.pr { color: #e5462b; font-weight: 700; margin-top: 2px; font-variant-numeric: tabular-nums; }
.acts { display: flex; flex-direction: column; align-items: flex-end; justify-content: space-between; gap: 6px; flex: none; }
.targets { display: flex; gap: 10px; }
.targets .plain { color: var(--brand-dark); font-weight: 600; }
.fees { border-top: 1px solid #f0f0f0; margin: 0 16px; padding: 10px 0 12px; display: grid; gap: 8px; }
.fees > div { display: flex; justify-content: space-between; align-items: baseline; font-size: 15px; color: #444; }
.fees b { color: #e5462b; font-size: 17px; font-weight: 500; font-variant-numeric: tabular-nums; }
</style>
