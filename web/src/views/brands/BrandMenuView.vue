<script setup lang="ts">
// A brand's own menu (docs/MULTI_BRAND_DESIGN.md §5): which of the store's
// dishes the brand sells, in its own categories, under its own names,
// pictures and prices. Options, toppings, notes, stations and sold-out stay
// on the store's dish.
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute } from 'vue-router'
import { get, put } from '../../api'
import { money, t, toMajor, toMinor } from '../../i18n'
import { currentRole, session } from '../../session'
import { brandById, brandsHere } from '../../brands'
import type { Category, Item } from '../../types'
import ImageUpload from '../ImageUpload.vue'
import '../../officeStrings'

interface Menu { own_menu: boolean; categories: { id: number; name: string; name_en: string }[]
  items: { item_id: number; category_id: number | null; name: string; name_en: string; image_url: string; price: number | null; published: boolean }[] }
interface Row { on: boolean; cat: string; name: string; name_en: string; image_url: string; price: number | ''; published: boolean }

const route = useRoute()
const id = Number(route.params.id)
const bid = session.branchId!
const base = `/merchant/branches/${bid}`
const cur = computed(() => currentRole()?.currency ?? 'PHP')
const brand = computed(() => brandById(id))
const lib = ref<Category[]>([])
const own = ref(false)
const cats = ref<{ key: string; name: string; name_en: string }[]>([])
const rows = reactive<Record<number, Row>>({})
const order = ref<number[]>([])
const error = ref(''), notice = ref(''), busy = ref(false), q = ref('')
let nextKey = 1

const dishes = computed<Item[]>(() => lib.value.flatMap(c => c.items))
function fill(m: Menu) {
  own.value = m.own_menu
  cats.value = m.categories.map(c => ({ key: 'c' + c.id, name: c.name, name_en: c.name_en }))
  const picked = new Map(m.items.map(i => [i.item_id, i]))
  for (const d of dishes.value) {
    const p = picked.get(d.id)
    rows[d.id] = { on: !!p, cat: p?.category_id ? 'c' + p.category_id : '', name: p?.name ?? '', name_en: p?.name_en ?? '',
      image_url: p?.image_url ?? '', price: p?.price != null ? toMajor(p.price) : '', published: p?.published ?? true }
  }
  // the brand's order first, then the rest of the store's dishes
  order.value = [...m.items.map(i => i.item_id).filter(i => rows[i]), ...dishes.value.map(d => d.id).filter(i => !picked.has(i))]
}
async function load() {
  try {
    lib.value = (await get<{ categories: Category[] }>(`${base}/menu`)).categories
    fill(await get<Menu>(`${base}/brands/${id}/menu`))
  } catch (e) { error.value = (e as Error).message }
}
onMounted(load)

const byId = computed(() => new Map(dishes.value.map(d => [d.id, d])))
const shown = computed(() => {
  const k = q.value.trim().toLowerCase()
  return order.value.map(i => byId.value.get(i)!).filter(d => d && (!k || [d.name, d.name_en, d.code, rows[d.id]?.name].some(s => (s ?? '').toLowerCase().includes(k))))
})
const pickedN = computed(() => order.value.filter(i => rows[i]?.on).length)
function addCat() { cats.value.push({ key: 'n' + nextKey++, name: '', name_en: '' }) }
function dropCat(k: string) {
  cats.value = cats.value.filter(c => c.key !== k)
  for (const r of Object.values(rows)) if (r.cat === k) r.cat = ''
}
function moveCat(i: number, d: number) {
  const j = i + d
  if (j < 0 || j >= cats.value.length) return
  const c = cats.value.splice(i, 1)[0]; cats.value.splice(j, 0, c)
}
function pickAll(on: boolean) { for (const d of shown.value) rows[d.id].on = on }

// copy another brand's menu, then change it
const copyFrom = ref(0)
async function copy() {
  if (!copyFrom.value) return
  try { const m = await get<Menu>(`${base}/brands/${copyFrom.value}/menu`); fill({ ...m, own_menu: true }); notice.value = t('bm.copied') }
  catch (e) { error.value = (e as Error).message }
}

const pic = ref(0)
async function save() {
  error.value = ''; notice.value = ''; busy.value = true
  const body = { own_menu: own.value, categories: cats.value.map(c => ({ key: c.key, name: c.name, name_en: c.name_en })),
    items: order.value.filter(i => rows[i]?.on).map(i => {
      const r = rows[i]
      return { item_id: i, category_key: r.cat, name: r.name, name_en: r.name_en, image_url: r.image_url,
        price: r.price === '' ? null : toMinor(r.price), published: r.published }
    }) }
  try { fill(await put<Menu>(`${base}/brands/${id}/menu`, body)); notice.value = t('saved') }
  catch (e) { error.value = (e as Error).message } finally { busy.value = false }
}
</script>

<template>
  <div class="page ox-page stack" data-testid="brand-menu">
    <div class="ox-head">
      <div><h1>{{ t('bm.title', { name: brand?.name ?? '' }) }}</h1><p class="ox-sub">{{ t('bm.sub') }}</p></div>
      <router-link class="btn" to="/merchant/brands">‹ {{ t('br.title') }}</router-link>
    </div>
    <div v-if="error" class="banner bad">{{ error }}</div>
    <div v-if="notice" class="banner ok" data-testid="bm-notice">{{ notice }}</div>

    <section class="ox-panel stack">
      <label class="check"><input v-model="own" type="checkbox" data-testid="bm-own" /><b>{{ t('bm.own') }}</b></label>
      <p class="muted small" style="margin:0">{{ own ? t('bm.own_hint') : t('bm.store_hint') }}</p>
      <div v-if="own" class="row wrap" style="gap:8px">
        <select v-model.number="copyFrom" style="max-width:220px" :aria-label="t('bm.copy')">
          <option :value="0">{{ t('bm.copy') }}…</option>
          <option v-for="b in brandsHere().filter(b => b.id !== id)" :key="b.id" :value="b.id">{{ b.name }}</option>
        </select>
        <button type="button" class="btn small" :disabled="!copyFrom" @click="copy">{{ t('bm.copy_go') }}</button>
      </div>
    </section>

    <template v-if="own">
      <section class="ox-panel stack">
        <div class="row between"><b>{{ t('bm.cats') }}</b><button type="button" class="btn small" data-testid="bm-add-cat" @click="addCat">＋ {{ t('bm.add_cat') }}</button></div>
        <div v-for="(c, i) in cats" :key="c.key" class="row wrap catrow">
          <input v-model="c.name" :placeholder="t('bm.cat_name')" maxlength="40" data-testid="bm-cat-name" />
          <input v-model="c.name_en" :placeholder="t('br.name_en')" maxlength="80" />
          <button type="button" class="btn small" :disabled="i === 0" @click="moveCat(i, -1)">↑</button>
          <button type="button" class="btn small" :disabled="i === cats.length - 1" @click="moveCat(i, 1)">↓</button>
          <button type="button" class="btn small danger" @click="dropCat(c.key)">{{ t('delete') }}</button>
        </div>
        <p v-if="!cats.length" class="muted small" style="margin:0">{{ t('bm.no_cats') }}</p>
      </section>

      <section class="ox-panel">
        <div class="ox-tools">
          <label class="ox-search"><input v-model="q" type="search" :placeholder="t('bm.search')" /></label>
          <button type="button" class="btn small" @click="pickAll(true)">{{ t('bm.pick_all') }}</button>
          <button type="button" class="btn small" @click="pickAll(false)">{{ t('bm.pick_none') }}</button>
          <span class="grow"></span>
          <span class="muted small">{{ t('bm.picked', { n: pickedN }) }}</span>
        </div>
        <div class="ox-scroll">
          <table class="data bmtable">
            <thead><tr><th></th><th>{{ t('bm.dish') }}</th><th>{{ t('bm.brand_name') }}</th><th>{{ t('bm.price') }} ({{ cur }})</th><th>{{ t('bm.cat') }}</th><th>{{ t('bm.on_sale') }}</th><th></th></tr></thead>
            <tbody>
              <tr v-for="d in shown" :key="d.id" :class="{ off: !rows[d.id].on }" :data-dish="d.id">
                <td><input v-model="rows[d.id].on" type="checkbox" :aria-label="d.name" data-testid="bm-pick" /></td>
                <td><span v-if="d.code" class="muted">{{ d.code }} </span><b>{{ d.name }}</b><div class="muted small">{{ money(d.price, cur) }}</div></td>
                <td><input v-model="rows[d.id].name" :placeholder="d.name" :disabled="!rows[d.id].on" maxlength="80" data-testid="bm-name" />
                  <input v-model="rows[d.id].name_en" :placeholder="d.name_en || t('br.name_en')" :disabled="!rows[d.id].on" maxlength="160" class="small" /></td>
                <td><input v-model="rows[d.id].price" type="number" min="0" step="0.01" :placeholder="String(toMajor(d.price))" :disabled="!rows[d.id].on" class="price" data-testid="bm-price" /></td>
                <td><select v-model="rows[d.id].cat" :disabled="!rows[d.id].on">
                  <option value="">{{ t('bm.cat_more') }}</option>
                  <option v-for="c in cats" :key="c.key" :value="c.key">{{ c.name || '—' }}</option></select></td>
                <td><input v-model="rows[d.id].published" type="checkbox" :disabled="!rows[d.id].on" :aria-label="t('bm.on_sale')" /></td>
                <td><button type="button" class="btn small" :disabled="!rows[d.id].on" @click="pic = d.id">{{ rows[d.id].image_url ? t('bm.pic_own') : t('bm.pic') }}</button></td>
              </tr>
            </tbody>
          </table>
        </div>
        <p class="muted small">{{ t('bm.hint') }}</p>
      </section>
    </template>

    <div class="row" style="justify-content:flex-end"><button class="btn primary" :disabled="busy" data-testid="bm-save" @click="save">{{ t('save') }}</button></div>

    <template v-if="pic">
      <div class="sheet-mask" @click="pic = 0"></div>
      <div class="sheet center">
        <div class="sheet-head"><h2>{{ byId.get(pic)?.name }}</h2></div>
        <div class="sheet-body">
          <ImageUpload v-model="rows[pic].image_url" :base="base" :ratio="1" :width="800" :label="t('bm.pic')" :hint="t('bm.pic_hint')" testid="bm-image" />
        </div>
        <div class="sheet-foot row">
          <button type="button" class="btn grow" @click="rows[pic].image_url = ''">{{ t('bm.pic_store') }}</button>
          <button type="button" class="btn primary grow" @click="pic = 0">{{ t('bm.done') }}</button>
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
.catrow { gap: 6px; }
.catrow input { flex: 1 1 140px; min-width: 0; }
.bmtable input:not([type=checkbox]), .bmtable select { width: 100%; min-width: 90px; }
.bmtable input.price { max-width: 110px; }
.bmtable tr.off td { color: #9a8f84; }
</style>
