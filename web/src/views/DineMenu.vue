<script setup lang="ts">
// Menu, cart and "send to kitchen" for one table. Shared by the scanned phone,
// the table tablet and the staff desk; the parent decides where the batch goes.
import { computed, reactive, ref, watch } from 'vue'
import { newKey } from '../api'
import { linePrice, type CartLine } from '../cart'
import { locale, money, pick, t } from '../i18n'
import type { BatchLine, Category, Item } from '../types'

const props = defineProps<{ categories: Category[]; currency: string; busy: boolean; storageKey: string; wide?: boolean }>()
const emit = defineEmits<{ submit: [lines: BatchLine[], expectedTotal: number, key: string] }>()

// The cart and its idempotency key survive a reload, so a batch whose answer
// was lost can be sent again under the same key. Any edit makes a new key.
interface Saved { lines: CartLine[]; key: string }
function restore(): Saved {
  try {
    const v = JSON.parse(sessionStorage.getItem(props.storageKey) || 'null')
    if (v && Array.isArray(v.lines) && typeof v.key === 'string') return v
  } catch { /* ignore */ }
  return { lines: [], key: newKey() }
}
const state = reactive<Saved>(restore())
watch(() => state.lines, () => { state.key = newKey() }, { deep: true })
watch(state, s => sessionStorage.setItem(props.storageKey, JSON.stringify(s)), { deep: true })

const cats = computed(() => props.categories.filter(c => c.items.length))
const items = computed(() => new Map(props.categories.flatMap(c => c.items).map(i => [i.id, i])))
const count = computed(() => state.lines.reduce((n, l) => n + l.qty, 0))
const total = computed(() => state.lines.reduce((s, l) => s + (linePrice(l, items.value) ?? 0) * l.qty, 0))
const bad = (l: CartLine) => { const it = items.value.get(l.item_id); return !it || it.sold_out || linePrice(l, items.value) === null }
const hasBad = computed(() => state.lines.some(bad))
const review = ref(false)

const sheet = reactive<{ item: Item | null; chosen: Set<number>; qty: number; note: string }>({ item: null, chosen: new Set(), qty: 1, note: '' })

function add(it: Item, optionIds: number[], qty: number, note: string) {
  const ids = [...optionIds].sort((a, b) => a - b)
  const key = `${it.id}:${ids.join(',')}:${note.trim()}`
  const ex = state.lines.find(l => l.key === key)
  if (ex) ex.qty = Math.min(99, ex.qty + qty)
  else state.lines.push({ key, item_id: it.id, qty, option_ids: ids, note: note.trim() })
}
function open(it: Item) {
  if (it.sold_out) return
  if (it.groups.length === 0) { add(it, [], 1, ''); return }
  sheet.item = it; sheet.chosen = new Set(); sheet.qty = 1; sheet.note = ''
}
function toggle(gi: number, id: number) {
  const g = sheet.item!.groups[gi]
  const inGroup = g.options.filter(o => sheet.chosen.has(o.id)).map(o => o.id)
  if (sheet.chosen.has(id)) { sheet.chosen.delete(id); return }
  if (g.max_select === 1) inGroup.forEach(x => sheet.chosen.delete(x))
  else if (inGroup.length >= g.max_select) return
  sheet.chosen.add(id)
}
const sheetMissing = computed(() => sheet.item?.groups.find(g => g.options.filter(o => sheet.chosen.has(o.id)).length < g.min_select) ?? null)
const sheetPrice = computed(() => {
  if (!sheet.item) return 0
  const extra = sheet.item.groups.flatMap(g => g.options).filter(o => sheet.chosen.has(o.id)).reduce((s, o) => s + o.price_delta, 0)
  return (sheet.item.price + extra) * sheet.qty
})
function confirmSheet() {
  if (!sheet.item || sheetMissing.value) return
  add(sheet.item, [...sheet.chosen], sheet.qty, sheet.note)
  sheet.item = null
}
function groupHint(g: { min_select: number; max_select: number }) {
  if (g.min_select > 0 && g.min_select === g.max_select) return `${t('required')} · ${t('pick_n', { n: g.min_select })}`
  if (g.min_select > 0) return `${t('required')} · ${t('up_to_n', { n: g.max_select })}`
  return `${t('optional')} · ${t('up_to_n', { n: g.max_select })}`
}
function optionNames(l: CartLine) {
  const it = items.value.get(l.item_id)
  if (!it) return ''
  const all = new Map(it.groups.flatMap(g => g.options).map(o => [o.id, o]))
  return l.option_ids.map(id => all.get(id)).filter(Boolean).map(o => pick(o!.name, o!.name_en)).join(' · ')
}
function changeQty(i: number, d: number) {
  const l = state.lines[i]
  l.qty = Math.min(99, l.qty + d)
  if (l.qty <= 0) state.lines.splice(i, 1)
  if (state.lines.length === 0) review.value = false
}
const qtyOf = (it: Item) => state.lines.filter(l => l.item_id === it.id).reduce((n, l) => n + l.qty, 0)
function scrollTo(id: number) { document.getElementById('dcat-' + id)?.scrollIntoView({ behavior: 'smooth', block: 'start' }) }

function send() {
  if (props.busy || hasBad.value || count.value === 0) return
  emit('submit', state.lines.map(l => ({ item_id: l.item_id, qty: l.qty, option_ids: l.option_ids, note: l.note })), total.value, state.key)
}
// Called by the parent once the server has confirmed the batch.
function clear() { state.lines = []; review.value = false }
defineExpose({ clear, count })
</script>

<template>
  <div>
    <div class="cats">
      <button v-for="c in cats" :key="c.id" @click="scrollTo(c.id)">{{ pick(c.name, c.name_en) }}</button>
    </div>
    <div class="section stack">
      <div v-for="c in cats" :key="c.id" :id="'dcat-' + c.id" class="stack" style="scroll-margin-top:60px">
        <h2>{{ pick(c.name, c.name_en) }}</h2>
        <div :class="wide ? 'grid' : 'stack'">
          <div v-for="it in c.items" :key="it.id" class="card item" :class="{ off: it.sold_out }">
            <img v-if="it.image_url" :src="it.image_url" alt="" />
            <div class="grow">
              <div class="bold">{{ pick(it.name, it.name_en) }}</div>
              <div class="muted small" v-if="locale === 'zh' && it.name_en">{{ it.name_en }}</div>
              <div class="muted small" v-if="it.description">{{ it.description }}</div>
              <div class="row between" style="margin-top:8px">
                <span class="price bold">{{ money(it.price, currency) }}</span>
                <span v-if="it.sold_out" class="tag">{{ t('sold_out') }}</span>
                <button v-else-if="it.groups.length" class="btn small primary" @click="open(it)">
                  {{ t('choose') }}<span v-if="qtyOf(it)"> · {{ qtyOf(it) }}</span></button>
                <span v-else class="stepper">
                  <template v-if="qtyOf(it)">
                    <button @click="changeQty(state.lines.findIndex(l => l.item_id === it.id), -1)" aria-label="-">−</button>
                    <span>{{ qtyOf(it) }}</span>
                  </template>
                  <button class="fill" @click="open(it)" aria-label="+">+</button>
                </span>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>

    <div class="dbar" v-if="count > 0">
      <button class="btn" @click="review = true">{{ t('cart') }} · {{ count }}</button>
      <span class="price bold grow right">{{ money(total, currency) }}</span>
      <button class="btn primary" @click="review = true">{{ t('send_to_kitchen') }}</button>
    </div>

    <div class="sheet-mask" v-if="sheet.item" @click.self="sheet.item = null">
      <div class="sheet">
        <div class="sheet-head">
          <h2>{{ pick(sheet.item.name, sheet.item.name_en) }}</h2>
          <div class="muted small" v-if="sheet.item.description">{{ sheet.item.description }}</div>
        </div>
        <div class="sheet-body stack">
          <div v-for="(g, gi) in sheet.item.groups" :key="g.id">
            <div class="row between"><h3>{{ pick(g.name, g.name_en) }}</h3><span class="muted small">{{ groupHint(g) }}</span></div>
            <label v-for="o in g.options" :key="o.id" class="opt" :class="{ off: o.sold_out }">
              <input :type="g.max_select === 1 ? 'radio' : 'checkbox'" :checked="sheet.chosen.has(o.id)" :disabled="o.sold_out" @click.prevent="toggle(gi, o.id)" />
              <span class="grow">{{ pick(o.name, o.name_en) }}<span v-if="o.sold_out" class="muted small"> · {{ t('sold_out') }}</span></span>
              <span class="price muted" v-if="o.price_delta">+{{ money(o.price_delta, currency) }}</span>
            </label>
          </div>
          <label class="field"><span>{{ t('note') }}</span><input v-model="sheet.note" maxlength="200" :placeholder="t('note_ph')" /></label>
        </div>
        <div class="sheet-foot row between">
          <span class="stepper"><button @click="sheet.qty = Math.max(1, sheet.qty - 1)">−</button><span>{{ sheet.qty }}</span><button class="fill" @click="sheet.qty = Math.min(99, sheet.qty + 1)">+</button></span>
          <button class="btn primary grow" :disabled="!!sheetMissing" @click="confirmSheet">
            {{ sheetMissing ? `${t('required')}: ${pick(sheetMissing.name, sheetMissing.name_en)}` : `${t('add_to_cart')} · ${money(sheetPrice, currency)}` }}
          </button>
        </div>
      </div>
    </div>

    <div class="sheet-mask" v-if="review" @click.self="review = false">
      <div class="sheet">
        <div class="sheet-head row between"><h2>{{ t('this_round') }}</h2><button class="btn link" @click="clear">{{ t('clear') }}</button></div>
        <div class="sheet-body stack">
          <div v-for="(l, i) in state.lines" :key="l.key" class="row top between">
            <div class="grow">
              <div class="bold">{{ items.get(l.item_id) ? pick(items.get(l.item_id)!.name, items.get(l.item_id)!.name_en) : '—' }}</div>
              <div class="muted small" v-if="optionNames(l)">{{ optionNames(l) }}</div>
              <div class="muted small" v-if="l.note">{{ t('note') }}: {{ l.note }}</div>
              <div v-if="bad(l)" class="small" style="color:var(--bad)">{{ t('unavailable') }}</div>
              <div v-else class="price small">{{ money(linePrice(l, items)! * l.qty, currency) }}</div>
            </div>
            <span class="stepper"><button @click="changeQty(i, -1)">−</button><span>{{ l.qty }}</span><button class="fill" @click="changeQty(i, 1)">+</button></span>
          </div>
          <slot name="notice" />
        </div>
        <div class="sheet-foot row between">
          <span>{{ t('subtotal') }} <b class="price">{{ money(total, currency) }}</b></span>
          <button class="btn primary" data-test="send-batch" :disabled="busy || hasBad" @click="send">{{ busy ? t('placing') : t('confirm_send') }}</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.cats { display: flex; gap: 8px; overflow-x: auto; padding: 10px 14px; position: sticky; top: 0; background: var(--bg); z-index: 10; }
.cats button { border: 1px solid var(--line); background: #fff; border-radius: 999px; padding: 6px 14px; white-space: nowrap; cursor: pointer; }
.grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(280px, 1fr)); gap: 12px; }
.item { display: flex; gap: 12px; }
.item img { width: 84px; height: 84px; object-fit: cover; border-radius: 8px; flex: none; }
.item.off { opacity: .55; }
.opt { display: flex; align-items: center; gap: 10px; padding: 10px 0; border-bottom: 1px solid var(--line); cursor: pointer; }
.opt.off { opacity: .5; }
.dbar { position: sticky; bottom: 0; display: flex; align-items: center; gap: 10px; padding: 12px 14px calc(12px + env(safe-area-inset-bottom));
  background: #fff; border-top: 1px solid var(--line); z-index: 20; }
</style>
