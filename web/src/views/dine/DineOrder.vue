<script lang="ts">
// A dish as it goes on the round: also what "one more" hands over from the bill.
export interface DeskLine { item_id: number; option_ids: number[]; note_tags: number[]; note: string }
</script>

<script setup lang="ts">
// Taking a round at a table by tapping (docs/DINE_IN_ORDER_DESIGN.md §7):
// categories on the left, dish tiles with pictures in the middle (one tap
// adds one; a dish with a required option or flavour notes opens the choice
// sheet), this round on the right, then "send to kitchen". A phone shows the
// dishes as a list and the round in a bar at the bottom.
import { useNow } from '../../now'
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ApiError, newKey, post } from '../../api'
import { mn, money, t } from '../../i18n'
import { cartTags, hasNotes } from '../../notes'
import type { Bill, DinePresets, Item, OptionGroup, Storefront } from '../../types'
import NotePicker from '../NotePicker.vue'
import { presetList, sinceText } from './strings'

const props = defineProps<{ bill: Bill; store: Storefront | null; presets: DinePresets | null; top: number[]; phone: boolean
  again: DeskLine | null; sent: { n: number; amt: number } }>()
const emit = defineEmits<{ back: []; sent: [bill: Bill, round: number]; bill: []; pay: []; move: []; guests: []; release: [] }>()

const cur = computed(() => props.store?.branch.currency ?? props.bill.order.currency)
const m = (v: number) => money(v, cur.value)
const now = useNow(30000)

// ---- the menu: dishes offered at the table ----
const TOP = -1
const cats = computed(() => {
  const list = (props.store?.categories ?? []).map(c => ({ ...c, items: c.items.filter(i => i.dine_in_available !== false) })).filter(c => c.items.length)
  const all = new Map(list.flatMap(c => c.items).map(i => [i.id, i]))
  const top = props.top.map(id => all.get(id)).filter((x): x is Item => !!x)
  return top.length ? [{ id: TOP, name: t('dd.top'), name_en: t('dd.top'), sort: -1, items: top }, ...list] : list
})
const items = computed(() => new Map(cats.value.flatMap(c => c.items).map(i => [i.id, i])))
const catId = ref<number | null>(null)
watch(cats, c => { if (catId.value === null || !c.some(x => x.id === catId.value)) catId.value = c[0]?.id ?? null }, { immediate: true })
const shownItems = computed(() => cats.value.find(c => c.id === catId.value)?.items ?? [])
const photos = computed(() => props.presets?.show_photos !== false)

// ---- this round, kept on this device until it is sent ----
interface Line extends DeskLine { key: string; qty: number }
interface Saved { lines: Line[]; tags: string[]; key: string }
const storeKey = `dd-round:${props.bill.session_id}`
function restore(): Saved {
  try {
    const v = JSON.parse(sessionStorage.getItem(storeKey) || 'null')
    if (v && Array.isArray(v.lines) && Array.isArray(v.tags) && typeof v.key === 'string') return v
  } catch { /* ignore */ }
  return { lines: [], tags: [], key: newKey() }
}
const round = reactive<Saved>(restore())
watch(() => [round.lines, round.tags], () => { round.key = newKey() }, { deep: true })
// A sent or dropped round starts over with a new key. The screen closes right
// after, before the watcher below runs, so it is saved here at once.
function reset() {
  round.lines = []; round.tags = []; round.key = newKey()
  try { sessionStorage.removeItem(storeKey) } catch { /* private mode */ }
}
watch(round, s => { try { sessionStorage.setItem(storeKey, JSON.stringify(s)) } catch { /* private mode */ } }, { deep: true })

const lineKey = (l: DeskLine) => `${l.item_id}:${[...l.option_ids].sort((a, b) => a - b).join(',')}:${[...l.note_tags].sort((a, b) => a - b).join(',')}:${l.note.trim()}`
function addLine(l: DeskLine, qty = 1) {
  const key = lineKey(l)
  const ex = round.lines.find(x => x.key === key)
  if (ex) ex.qty = Math.min(99, ex.qty + qty)
  else round.lines.push({ ...l, option_ids: [...l.option_ids], note_tags: [...l.note_tags], key, qty })
  sel.value = key
}
const sel = ref('')
const unit = (l: DeskLine) => {
  const it = items.value.get(l.item_id)
  if (!it) return 0
  const opts = new Map(it.groups.flatMap(g => g.options).map(o => [o.id, o]))
  return it.price + l.option_ids.reduce((s, id) => s + (opts.get(id)?.price_delta ?? 0), 0)
}
const count = computed(() => round.lines.reduce((s, l) => s + l.qty, 0))
const total = computed(() => round.lines.reduce((s, l) => s + unit(l) * l.qty, 0))
const qtyOf = (it: Item) => round.lines.filter(l => l.item_id === it.id).reduce((s, l) => s + l.qty, 0)
const catCount = (c: { items: Item[]; id: number }) => c.id === TOP ? 0 : c.items.reduce((s, i) => s + qtyOf(i), 0)
function optText(l: DeskLine) {
  const it = items.value.get(l.item_id)
  if (!it) return ''
  const all = new Map(it.groups.flatMap(g => g.options).map(o => [o.id, o]))
  return l.option_ids.map(id => all.get(id)).filter(Boolean).map(o => mn(o!) + (o!.price_delta ? ` +${m(o!.price_delta)}` : '')).join(' · ')
}
const tagText = (l: DeskLine) => [...cartTags(items.value.get(l.item_id), l.note_tags), l.note].filter(Boolean)
function setQty(l: Line, d: number) {
  l.qty = Math.min(99, l.qty + d)
  if (l.qty <= 0) round.lines.splice(round.lines.indexOf(l), 1)
}
const roundNo = computed(() => new Set(props.bill.order.items.map(i => i.batch_seq)).size + 1)
const roundTags = computed(() => presetList(props.presets, 'round_tags'))
function toggleRound(x: string) { const i = round.tags.indexOf(x); if (i >= 0) round.tags.splice(i, 1); else round.tags.push(x) }

// ---- tapping a dish ----
// a dish with toppings (加料 templates) opens the sheet too, so they can be picked
const needsSheet = (it: Item) => it.groups.some(g => g.min_select > 0 || g.topping_template_id) || hasNotes(it)
function tapDish(it: Item) {
  if (it.sold_out) return
  if (needsSheet(it)) openSheet(it)
  else addLine({ item_id: it.id, option_ids: [], note_tags: [], note: '' })
}

// the choice sheet: options (paid, maybe required), flavour notes (free), how many
const sheet = reactive<{ item: Item | null; chosen: number[]; tags: number[]; note: string; qty: number; editing: string; writing: boolean; text: string }>(
  { item: null, chosen: [], tags: [], note: '', qty: 1, editing: '', writing: false, text: '' })
function openSheet(it: Item, l?: Line) {
  Object.assign(sheet, { item: it, chosen: l ? [...l.option_ids] : [], tags: l ? [...l.note_tags] : [], note: l?.note ?? '', qty: l?.qty ?? 1,
    editing: l?.key ?? '', writing: false, text: '' })
}
function editLine(l: Line) { const it = items.value.get(l.item_id); sel.value = l.key; if (it && (it.groups.length || hasNotes(it))) openSheet(it, l) }
function toggleOpt(g: OptionGroup, id: number) {
  const inGroup = g.options.filter(o => sheet.chosen.includes(o.id)).map(o => o.id)
  if (sheet.chosen.includes(id)) { sheet.chosen = sheet.chosen.filter(x => x !== id); return }
  if (g.max_select === 1) sheet.chosen = sheet.chosen.filter(x => !inGroup.includes(x))
  else if (inGroup.length >= g.max_select) return
  sheet.chosen = [...sheet.chosen, id]
}
const missing = computed(() => sheet.item?.groups.find(g => g.options.filter(o => sheet.chosen.includes(o.id)).length < g.min_select) ?? null)
const sheetUnit = computed(() => sheet.item ? unit({ item_id: sheet.item.id, option_ids: sheet.chosen, note_tags: [], note: '' }) : 0)
const sheetSummary = computed(() => {
  if (!sheet.item) return ''
  const opts = sheet.item.groups.flatMap(g => g.options).filter(o => sheet.chosen.includes(o.id)).map(o => mn(o))
  return [...opts, ...cartTags(sheet.item, sheet.tags), sheet.note].filter(Boolean).join(' · ')
})
function groupHint(g: OptionGroup) { return g.max_select === 1 ? t('dd.paid_single') : t('dd.paid_multi') }
function confirmSheet() {
  if (!sheet.item || missing.value) return
  const l = { item_id: sheet.item.id, option_ids: [...sheet.chosen], note_tags: [...sheet.tags], note: sheet.note.trim() }
  if (sheet.editing) {
    const old = round.lines.find(x => x.key === sheet.editing)
    if (old) round.lines.splice(round.lines.indexOf(old), 1)
  }
  addLine(l, sheet.qty)
  sheet.item = null
}
function removeEditing() {
  const old = round.lines.find(x => x.key === sheet.editing)
  if (old) round.lines.splice(round.lines.indexOf(old), 1)
  sheet.item = null
}
function addWritten() { sheet.note = sheet.text.trim().slice(0, 100); sheet.writing = false }
const minFrom = (it: Item) => it.groups.some(g => g.min_select > 0 && g.options.some(o => o.price_delta > 0))

// "one more" from the bill
onMounted(() => { if (props.again) addLine(props.again) })

// ---- by code: a keypad of the letters and digits the menu's codes use ----
const codeOpen = ref(false)
const code = ref('')
const codeKeys = computed(() => {
  const letters = new Set<string>()
  for (const it of items.value.values()) for (const ch of (it.code ?? '').toUpperCase()) if (/[A-Z]/.test(ch)) letters.add(ch)
  return [...[...letters].sort(), '1', '2', '3', '4', '5', '6', '7', '8', '9', '0']
})
const codeHits = computed(() => {
  const q = code.value.toUpperCase()
  if (!q) return []
  return [...items.value.values()].filter(i => (i.code ?? '').toUpperCase().startsWith(q))
    .sort((a, b) => (a.code ?? '').length - (b.code ?? '').length).slice(0, 8)
})
function pickCode(it: Item) { codeOpen.value = false; code.value = ''; tapDish(it) }

// ---- sending ----
const busy = ref(false)
const error = ref('')
const changed = ref<number | null>(null)
async function send(expected = total.value, key = round.key) {
  if (busy.value || !count.value) return
  busy.value = true; error.value = ''
  try {
    const b = await post<Bill>(`/merchant/table-sessions/${props.bill.session_id}/batches`, {
      items: round.lines.map(l => ({ item_id: l.item_id, qty: l.qty, option_ids: l.option_ids, note: l.note, note_tag_ids: l.note_tags })),
      expected_total: expected, round_tags: round.tags,
    }, { 'Idempotency-Key': key })
    const no = roundNo.value
    reset()
    changed.value = null; cartOpen.value = false
    emit('sent', b, no)
  } catch (e) {
    if (e instanceof ApiError && e.code === 'QUOTE_CHANGED') changed.value = Number(e.details?.total ?? 0)
    else error.value = e instanceof ApiError && e.status === 0 ? t('submit_failed') : (e as Error).message
  }
  busy.value = false
}
function sendNew() { if (changed.value !== null) void send(changed.value, newKey()) }
function cancel() {
  if (count.value && !confirm(t('dd.discard'))) return
  reset()
  emit('back')
}
const cartOpen = ref(false)
</script>

<template>
  <div class="dd-order">
    <div class="dd-top">
      <button class="dd-back" data-testid="dd-back" @click="emit('back')"><span class="rtl-flip">‹</span> {{ t('dd.back_tables') }}</button>
      <span class="dd-chip">{{ bill.table_name }}<small>{{ t('dd.guests_n', { n: bill.guests }) }}<template v-if="!phone"> · {{ sinceText(bill.opened_at, now) }}</template></small></span>
      <span class="dd-pill">{{ t('dd.round_n', { n: roundNo }) }}</span>
      <span class="sp"></span>
      <template v-if="!phone">
        <button class="dd-topbtn" data-testid="dd-to-bill" @click="emit('bill')">{{ t('dd.bill') }}</button>
        <button class="dd-topbtn" @click="emit('move')">{{ t('dd.move') }}</button>
        <button class="dd-topbtn" @click="emit('guests')">{{ t('dd.guests_btn') }}</button>
      </template>
      <button v-if="!bill.order.items.length" class="dd-topbtn" data-testid="dd-release-top" @click="emit('release')">{{ t('dd.release') }}</button>
      <button v-else class="dd-topbtn" data-testid="dd-to-bill" @click="emit('bill')">{{ m(bill.order.totals.total) }}</button>
    </div>
    <div class="dd-obody">
      <nav class="dd-cats">
        <button v-for="c in cats" :key="c.id" :class="{ on: c.id === catId }" :data-testid="`dd-cat-${c.id === -1 ? 'top' : c.id}`" @click="catId = c.id">
          {{ mn(c) }}<i v-if="catCount(c)">{{ catCount(c) }}</i></button>
      </nav>
      <section class="dd-dishes">
        <div class="dd-dbar"><span class="muted small">{{ mn(cats.find(c => c.id === catId) ?? { name: '' }) }}</span><span class="sp"></span>
          <button v-if="codeKeys.length > 10" class="dd-ch sm" data-testid="dd-by-code" @click="codeOpen = true">{{ t('dd.by_code') }}</button></div>
        <div v-if="!shownItems.length" class="dd-empty">{{ t('dd.no_items') }}</div>
        <!-- a phone: a list -->
        <div v-if="phone" class="dd-plist">
          <div v-for="it in shownItems" :key="it.id" class="dd-pi" :class="{ out: it.sold_out }" :data-testid="`dd-dish-${it.id}`">
            <img v-if="photos && it.image_url" class="th" :src="it.image_url" alt="" loading="lazy" /><span v-else-if="photos" class="th none">{{ mn(it).slice(0, 2) }}</span>
            <div class="tx"><div><span v-if="it.code" class="code">{{ it.code }}</span> <b>{{ mn(it) }}</b></div>
              <div class="pr">{{ minFrom(it) ? t('dd.from_price', { p: m(it.price) }) : m(it.price) }}
                <span v-if="it.groups.length" class="mk">{{ t('dd.spec') }}</span><span v-if="hasNotes(it)" class="mk">{{ t('dd.notes') }}</span></div></div>
            <span v-if="it.sold_out" class="muted small r">{{ t('sold_out') }}</span>
            <button v-else-if="needsSheet(it)" class="dd-sel r" :data-testid="`dd-add-${it.id}`" @click="openSheet(it)">{{ it.groups.length ? t('dd.spec') : t('dd.notes') }}<b v-if="qtyOf(it)">{{ qtyOf(it) }}</b></button>
            <span v-else class="dd-stp r"><button v-if="qtyOf(it)" @click="setQty(round.lines.find(l => l.item_id === it.id && !l.option_ids.length && !l.note_tags.length && !l.note)!, -1)">−</button>
              <b v-if="qtyOf(it)">{{ qtyOf(it) }}</b><button class="f" :data-testid="`dd-add-${it.id}`" @click="tapDish(it)">＋</button></span>
          </div>
        </div>
        <!-- a tablet or a computer: tiles -->
        <div v-else class="dd-dgrid">
          <button v-for="it in shownItems" :key="it.id" class="dd-dt" :class="{ on: qtyOf(it), out: it.sold_out, nopic: !photos }" :data-testid="`dd-dish-${it.id}`" @click="tapDish(it)">
            <span v-if="qtyOf(it)" class="n">{{ qtyOf(it) }}</span>
            <span v-if="photos" class="pic" :class="{ none: !it.image_url }" :style="it.image_url ? { backgroundImage: `url(${it.image_url})` } : {}"><i v-if="!it.image_url">{{ mn(it).slice(0, 2) }}</i></span>
            <span v-if="it.code" class="code">{{ it.code }}</span>
            <span class="tx"><b class="nm">{{ mn(it) }}</b>
              <span class="ft"><span class="pr">{{ minFrom(it) ? t('dd.from_price', { p: m(it.price) }) : m(it.price) }}</span>
                <span v-if="it.groups.length" class="mk">{{ t('dd.spec') }}</span><span v-if="hasNotes(it)" class="mk">{{ t('dd.notes') }}</span>
                <span v-if="it.sold_out" class="so">{{ t('sold_out') }}</span>
                <span v-else-if="it.stock_qty !== null && it.stock_qty <= 5" class="left">{{ t('dd.left', { n: it.stock_qty }) }}</span></span></span>
          </button>
        </div>
      </section>

      <!-- this round: beside the dishes, or a sheet from the bottom bar on a phone -->
      <aside v-if="!phone || cartOpen" class="dd-cart" :class="{ sheet: phone }" data-testid="dd-round">
        <div class="chd"><h3>{{ t('dd.this_round') }}</h3><span class="muted small">{{ t('dd.not_sent') }}</span><span class="sp"></span>
          <button v-if="count" class="lnk" @click="round.lines = []">{{ t('dd.clear') }}</button>
          <button v-if="phone" class="lnk" @click="cartOpen = false">✕</button></div>
        <div class="lines">
          <div v-if="!round.lines.length" class="muted dd-empty sm">{{ t('dd.empty_round') }}</div>
          <div v-for="l in round.lines" :key="l.key" class="dd-ln" :class="{ on: l.key === sel }" data-testid="dd-line" @click="editLine(l)">
            <img v-if="photos && items.get(l.item_id)?.image_url" class="th" :src="items.get(l.item_id)!.image_url" alt="" />
            <div class="lt"><b>{{ items.get(l.item_id) ? mn(items.get(l.item_id)!) : '—' }}</b>
              <div v-if="optText(l)" class="opt">{{ optText(l) }}</div>
              <div v-if="tagText(l).length" class="tags"><span v-for="x in tagText(l)" :key="x">{{ x }}</span></div></div>
            <div class="lp">{{ m(unit(l) * l.qty) }}
              <span class="dd-stp s" @click.stop><button @click="setQty(l, -1)">−</button><b>{{ l.qty }}</b><button class="f" @click="setQty(l, 1)">＋</button></span></div>
          </div>
        </div>
        <div class="whole"><div class="wl">{{ t('dd.round_tags') }}</div>
          <div class="dd-chs sm"><button v-for="x in roundTags" :key="x" class="dd-ch" :class="{ on: round.tags.includes(x) }" data-testid="dd-round-tag" @click="toggleRound(x)">{{ x }}</button></div></div>
        <button v-if="sent.n" class="sent" @click="emit('bill')">{{ t('dd.sent_summary', { r: roundNo - 1, n: sent.n, amt: m(sent.amt) }) }}<span class="sp"></span>{{ t('dd.view') }}</button>
        <div class="cfoot">
          <div v-if="error" class="banner bad small">{{ error }}</div>
          <div v-if="changed !== null" class="banner warn small">{{ t('dd.price_changed', { amt: m(changed) }) }}
            <button class="dd-ch on sm" data-testid="dd-send-new" @click="sendNew">{{ t('dd.send_new') }}</button></div>
          <div class="tt"><span>{{ t('dd.round_total', { n: count }) }}</span><b>{{ m(total) }}</b></div>
          <div class="row2"><button class="dd-big ghost" @click="cancel">{{ t('dd.cancel') }}</button>
            <button class="dd-big" :disabled="!count || busy" data-testid="dd-send" @click="send()">{{ t('dd.send', { n: count }) }}</button></div>
        </div>
      </aside>
    </div>
    <div v-if="phone && !cartOpen" class="dd-pbar">
      <button class="l" data-testid="dd-open-round" @click="cartOpen = true">{{ t('dd.this_round') }} <span class="n">{{ count }}</span><b>{{ m(total) }}</b></button>
      <button class="go" :disabled="!count || busy" data-testid="dd-send-bar" @click="send()">{{ t('send_to_kitchen') }}</button>
    </div>

    <!-- options and flavour notes of one dish -->
    <div v-if="sheet.item" class="dd-mask" :class="{ bottom: phone }" @click.self="sheet.item = null">
      <div class="dd-modal wide dish" data-testid="dd-sheet">
        <div v-if="photos && sheet.item.image_url" class="mpic" :style="{ backgroundImage: `url(${sheet.item.image_url})` }"></div>
        <div class="mh"><span v-if="sheet.item.code" class="code">{{ sheet.item.code }}</span><h3>{{ mn(sheet.item) }}</h3>
          <span class="price">{{ minFrom(sheet.item) ? t('dd.from_price', { p: m(sheet.item.price) }) : m(sheet.item.price) }}</span>
          <button class="x" @click="sheet.item = null">✕</button></div>
        <div class="mb">
          <div v-for="g in sheet.item.groups" :key="g.id" class="grp" :class="{ miss: missing?.id === g.id }">
            <div class="dd-lbl">{{ mn(g) }} <span v-if="g.min_select > 0" class="req">{{ t('dd.required') }}</span><small>{{ groupHint(g) }}</small></div>
            <div class="dd-chs">
              <button v-for="o in g.options" :key="o.id" class="dd-ch" :class="{ on: sheet.chosen.includes(o.id) }" :disabled="o.sold_out" data-testid="dd-opt" @click="toggleOpt(g, o.id)">
                <i v-if="o.code" class="dd-ocode">{{ o.code }}</i>{{ mn(o) }}<small v-if="o.price_delta">+{{ m(o.price_delta) }}</small><small v-if="o.sold_out">{{ t('sold_out') }}</small></button>
            </div>
          </div>
          <div v-if="hasNotes(sheet.item)" class="grp notes">
            <NotePicker v-model="sheet.tags" :groups="sheet.item.notes ?? []" />
          </div>
          <div class="grp">
            <div class="dd-chs">
              <button v-if="sheet.note && !sheet.writing" class="dd-ch on" @click="sheet.note = ''">{{ sheet.note }} ✕</button>
              <button v-if="!sheet.writing" class="dd-ch add" data-testid="dd-write" @click="sheet.writing = true; sheet.text = sheet.note">{{ t('dd.write') }}</button>
              <span v-else class="dd-write"><input v-model="sheet.text" maxlength="100" :placeholder="t('dd.write_ph')" @keydown.enter.prevent="addWritten" />
                <button class="dd-ch on" @click="addWritten">{{ t('dd.write_add') }}</button></span>
            </div>
          </div>
        </div>
        <div class="mf">
          <span class="dd-stp big"><button @click="sheet.qty = Math.max(1, sheet.qty - 1)">−</button><b>{{ sheet.qty }}</b><button class="f" @click="sheet.qty = Math.min(99, sheet.qty + 1)">＋</button></span>
          <span class="sum">{{ sheetSummary }}</span>
          <button v-if="sheet.editing" class="dd-big ghost sm" @click="removeEditing">{{ t('dd.remove') }}</button>
          <button class="dd-big" :disabled="!!missing" data-testid="dd-sheet-add" @click="confirmSheet">
            {{ missing ? t('dd.choose_first', { g: mn(missing) }) : t(sheet.editing ? 'dd.update_round' : 'dd.add_round', { amt: m(sheetUnit * sheet.qty) }) }}</button>
        </div>
      </div>
    </div>

    <!-- by code -->
    <div v-if="codeOpen" class="dd-mask" @click.self="codeOpen = false">
      <div class="dd-modal" data-testid="dd-code">
        <div class="mh"><h3>{{ t('dd.by_code') }}</h3><span class="muted small">{{ t('dd.code_hint') }}</span><button class="x" @click="codeOpen = false">✕</button></div>
        <div class="mb">
          <div class="dd-codeq">{{ code || '—' }}</div>
          <div class="dd-keys code"><button v-for="k in codeKeys" :key="k" @click="code += k">{{ k }}</button><button @click="code = code.slice(0, -1)">⌫</button></div>
          <div class="dd-chs" style="margin-top:12px"><button v-for="it in codeHits" :key="it.id" class="dd-ch" :disabled="it.sold_out" @click="pickCode(it)"><b>{{ it.code }}</b>&nbsp;{{ mn(it) }}</button></div>
        </div>
      </div>
    </div>
  </div>
</template>
