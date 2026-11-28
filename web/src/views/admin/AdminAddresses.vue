<script setup lang="ts">
// The platform's address book (docs/ADDRESS_BOOK_DESIGN.md): addresses with a
// position, per country / province / city. Staff orders add to it; the order
// desk suggests from it first. Here the platform looks through it, corrects
// it, adds to it and removes what is wrong.
import { computed, defineAsyncComponent, onMounted, reactive, ref } from 'vue'
import { ApiError, del, get, post, put } from '../../api'
import { has, t } from '../../i18n'
import { googleMapsLink, joined, parseLatLng } from '../../maps'
import AddrText from '../AddrText.vue'
import type { Picked } from '../PlacePicker.vue'

const PlacePicker = defineAsyncComponent(() => import('../PlacePicker.vue'))

interface Region { country_code: string; country: string; province: string; city: string; count: number }
interface Addr {
  id: number; country_code: string; country: string; province: string; city: string; line: string; place_name: string; place_id: string; lat: number; lng: number
  source: 'staff' | 'admin' | 'import'; branch_id: number | null; branch_name: string; uses: number
  created_at: string; updated_at: string; last_used_at: string
}
const SIZE = 20
const SORTS = ['recent', 'uses', 'new', 'line']

const regions = ref<Region[]>([])
// '*' is "any"; '' is the unknown region.
const f = reactive({ cc: '*', province: '*', city: '*', q: '', source: '', sort: 'recent', page: 1 })
const list = reactive({ items: [] as Addr[], total: 0, loaded: false })
const error = ref('')

const unknown = (s: string) => s || t('ab.unknown')
const countries = computed(() => {
  const m = new Map<string, { name: string; n: number }>()
  for (const r of regions.value) {
    const x = m.get(r.country_code) ?? { name: r.country, n: 0 }
    x.n += r.count; x.name ||= r.country
    m.set(r.country_code, x)
  }
  return [...m.entries()].map(([code, x]) => ({ code, name: x.name, n: x.n }))
})
const provinces = computed(() => {
  const m = new Map<string, number>()
  for (const r of regions.value) if (r.country_code === f.cc) m.set(r.province, (m.get(r.province) ?? 0) + r.count)
  return [...m.entries()]
})
const cities = computed(() => regions.value.filter(r => r.country_code === f.cc && r.province === f.province))

async function loadRegions() {
  regions.value = (await get<{ regions: Region[] }>('/admin/address-regions')).regions
}
async function load() {
  const u = new URLSearchParams()
  if (f.cc !== '*') u.set('country_code', f.cc)
  if (f.cc !== '*' && f.province !== '*') u.set('province', f.province)
  if (f.cc !== '*' && f.province !== '*' && f.city !== '*') u.set('city', f.city)
  if (f.q.trim()) u.set('q', f.q.trim())
  if (f.source) u.set('source', f.source)
  // with words typed, "recent" leaves the closest matches first
  if (!(f.q.trim() && f.sort === 'recent')) u.set('sort', f.sort)
  u.set('page', String(f.page)); u.set('page_size', String(SIZE))
  try {
    Object.assign(list, await get('/admin/addresses?' + u.toString()), { loaded: true })
    error.value = ''
  } catch (e) { error.value = errText(e) }
}
function search() { f.page = 1; void load() }
function pickCountry(cc: string) { f.cc = cc; f.province = '*'; f.city = '*'; search() }
function pickProvince(p: string) { f.province = p; f.city = '*'; search() }
function pickCity(c: string) { f.city = c; search() }
const pages = computed(() => Math.max(1, Math.ceil(list.total / SIZE)))
function go(p: number) { f.page = p; void load() }
onMounted(async () => { await loadRegions().catch(e => { error.value = errText(e) }); void load() })

function errText(e: unknown): string {
  if (e instanceof ApiError) {
    if (has('ab.err.' + e.code)) return t('ab.err.' + e.code)
    return e.message
  }
  return (e as Error).message
}
const two = (n: number) => String(n).padStart(2, '0')
function when(iso: string) {
  const d = new Date(iso)
  return `${d.getFullYear()}-${two(d.getMonth() + 1)}-${two(d.getDate())} ${two(d.getHours())}:${two(d.getMinutes())}`
}
const regionText = (a: { country: string; country_code: string; province: string; city: string }) =>
  [a.country || a.country_code, a.province, a.city].filter(Boolean).join(' / ') || t('ab.unknown')
const showRegion = computed(() => f.city === '*' || f.cc === '*')

// ---- adding and correcting ----
const ed = reactive({ open: false, id: 0, country_code: '', country: '', province: '', city: '', line: '', place_name: '', at: '', error: '', saving: false })
const picking = ref(false)
function startNew() {
  const r = f.cc !== '*' && f.province !== '*' && f.city !== '*'
    ? regions.value.find(x => x.country_code === f.cc && x.province === f.province && x.city === f.city) : undefined
  Object.assign(ed, { open: true, id: 0, country_code: r?.country_code ?? '', country: r?.country ?? '', province: r?.province ?? '',
    city: r?.city ?? '', line: '', place_name: '', at: '', error: '', saving: false })
}
function edit(a: Addr) {
  Object.assign(ed, { open: true, id: a.id, country_code: a.country_code, country: a.country, province: a.province, city: a.city,
    line: a.line, place_name: a.place_name, at: `${a.lat}, ${a.lng}`, error: '', saving: false })
}
const regionKey = (r: { country_code: string; province: string; city: string }) => [r.country_code, r.province, r.city].join('|')
function useRegion(k: string) {
  const r = regions.value.find(x => regionKey(x) === k)
  if (r) Object.assign(ed, { country_code: r.country_code, country: r.country, province: r.province, city: r.city })
}
const edAt = computed(() => parseLatLng(ed.at))
function picked(p: Picked) {
  ed.at = `${p.lat.toFixed(6)}, ${p.lng.toFixed(6)}`
  if (!ed.line.trim()) {
    ed.line = joined(p.name, p.address)
    if (p.address && !ed.place_name.trim()) ed.place_name = p.name
  }
  // a new address takes the region of where it was pinned, unless one was chosen
  if (p.region && !ed.id && !ed.country_code && !ed.province && !ed.city) {
    Object.assign(ed, { country_code: p.region.country_code, country: p.region.country, province: p.region.province, city: p.region.city })
  }
  picking.value = false
}
async function save() {
  const at = edAt.value
  if (!at) { ed.error = t('ab.err.PIN_REQUIRED'); return }
  ed.saving = true
  const body = { country_code: ed.country_code, country: ed.country, province: ed.province, city: ed.city, line: ed.line, place_name: ed.place_name, lat: at[0], lng: at[1] }
  try {
    if (ed.id) await put(`/admin/addresses/${ed.id}`, body)
    else await post('/admin/addresses', body)
    ed.open = false
    await loadRegions()
    await load()
  } catch (e) { ed.error = errText(e) } finally { ed.saving = false }
}
async function remove(a: Addr) {
  if (!confirm(t('ab.delete_confirm', { s: a.line }))) return
  try {
    await del(`/admin/addresses/${a.id}`)
    await loadRegions()
    await load()
  } catch (e) { error.value = errText(e) }
}
</script>

<template>
  <div class="adm-page ab" data-testid="address-book">
    <p class="muted small ab-intro">{{ t('ab.intro') }}</p>
    <div class="ab-grid">
      <aside class="card ab-regions" data-testid="ab-regions">
        <div class="ab-col">
          <div class="ab-h">{{ t('ab.country') }}</div>
          <button :class="{ on: f.cc === '*' }" @click="pickCountry('*')"><span class="grow">{{ t('ab.all') }}</span></button>
          <button v-for="c in countries" :key="c.code" :class="{ on: f.cc === c.code }" data-testid="ab-country" @click="pickCountry(c.code)">
            <span class="grow">{{ c.name || unknown(c.code) }}<small v-if="c.code" class="muted ab-cc">{{ c.code }}</small></span><b>{{ c.n }}</b></button>
        </div>
        <div v-if="f.cc !== '*'" class="ab-col">
          <div class="ab-h">{{ t('ab.province') }}</div>
          <button :class="{ on: f.province === '*' }" @click="pickProvince('*')"><span class="grow">{{ t('ab.all') }}</span></button>
          <button v-for="[p, n] in provinces" :key="p" :class="{ on: f.province === p }" data-testid="ab-province" @click="pickProvince(p)">
            <span class="grow">{{ unknown(p) }}</span><b>{{ n }}</b></button>
        </div>
        <div v-if="f.cc !== '*' && f.province !== '*'" class="ab-col">
          <div class="ab-h">{{ t('ab.city') }}</div>
          <button :class="{ on: f.city === '*' }" @click="pickCity('*')"><span class="grow">{{ t('ab.all') }}</span></button>
          <button v-for="c in cities" :key="c.city" :class="{ on: f.city === c.city }" data-testid="ab-city" @click="pickCity(c.city)">
            <span class="grow">{{ unknown(c.city) }}</span><b>{{ c.count }}</b></button>
        </div>
      </aside>

      <section class="ab-main">
        <form class="row wrap ab-filters" @submit.prevent="search">
          <input v-model="f.q" type="search" class="ab-q" :placeholder="t('ab.search_ph')" data-testid="ab-search" />
          <select v-model="f.source" @change="search">
            <option value="">{{ t('ab.any_source') }}</option>
            <option v-for="s in ['staff', 'admin', 'import']" :key="s" :value="s">{{ t('ab.src.' + s) }}</option>
          </select>
          <select v-model="f.sort" @change="search">
            <option v-for="s in SORTS" :key="s" :value="s">{{ t('ab.sort.' + s) }}</option>
          </select>
          <button class="btn">{{ t('ab.search') }}</button>
          <span class="grow"></span>
          <button type="button" class="btn primary" data-testid="ab-new" @click="startNew">{{ t('ab.new') }}</button>
        </form>
        <div class="small muted" data-testid="ab-count">{{ t('ab.count', { n: list.total }) }}</div>
        <div v-if="error" class="banner bad" data-testid="ab-error">{{ error }}</div>
        <div class="card ab-table">
          <table class="data">
            <thead><tr>
              <th>{{ t('ab.col.line') }}</th><th v-if="showRegion">{{ t('ab.col.region') }}</th><th>{{ t('ab.col.pin') }}</th>
              <th>{{ t('ab.col.source') }}</th><th class="num">{{ t('ab.col.uses') }}</th><th>{{ t('ab.col.last') }}</th><th></th>
            </tr></thead>
            <tbody>
              <tr v-for="a in list.items" :key="a.id" data-testid="ab-row">
                <td class="ab-line"><AddrText :o="{ address_line: a.line, place_name: a.place_name }" /></td>
                <td v-if="showRegion" class="small">{{ regionText(a) }}</td>
                <td class="small mono nowrap">{{ a.lat.toFixed(5) }}, {{ a.lng.toFixed(5) }}</td>
                <td class="small"><span class="tag" :class="{ info: a.source === 'admin', brand: a.source === 'staff' }">{{ t('ab.src.' + a.source) }}</span>
                  <div v-if="a.branch_name" class="muted">{{ a.branch_name }}</div></td>
                <td class="num">{{ a.uses }}</td>
                <td class="small nowrap">{{ when(a.last_used_at) }}</td>
                <td class="right nowrap"><span class="row acts">
                  <a class="btn small" :href="googleMapsLink(a.lat, a.lng, a.place_id)" target="_blank" rel="noopener" data-testid="ab-view">{{ t('ab.view') }}</a>
                  <button class="btn small" data-testid="ab-edit" @click="edit(a)">{{ t('ab.edit') }}</button>
                  <button class="btn small danger" data-testid="ab-delete" @click="remove(a)">{{ t('ab.delete') }}</button></span></td>
              </tr>
              <tr v-if="list.loaded && !list.items.length"><td :colspan="showRegion ? 7 : 6" class="muted ab-empty">{{ t('ab.empty') }}</td></tr>
            </tbody>
          </table>
        </div>
        <div v-if="pages > 1" class="row small muted">
          <span class="grow"></span><span>{{ t('ab.page', { p: f.page, t: pages }) }}</span>
          <button class="btn small" :disabled="f.page <= 1" @click="go(f.page - 1)">{{ t('ab.prev') }}</button>
          <button class="btn small" :disabled="f.page >= pages" @click="go(f.page + 1)">{{ t('ab.next') }}</button>
        </div>
      </section>
    </div>

    <div v-if="ed.open" class="sheet-mask" @click.self="ed.open = false">
      <form class="sheet center" data-testid="ab-form" @submit.prevent="save">
        <div class="sheet-head"><h2>{{ ed.id ? t('ab.edit_title') : t('ab.new') }}</h2></div>
        <div class="sheet-body">
          <div v-if="ed.error" class="banner bad" style="margin-bottom:12px" data-testid="ab-form-error">{{ ed.error }}</div>
          <label class="field"><span>{{ t('ab.region_pick') }}</span>
            <select :value="regionKey(ed)" @change="useRegion(($event.target as HTMLSelectElement).value)">
              <option value="" disabled>—</option>
              <option v-for="r in regions" :key="regionKey(r)" :value="regionKey(r)">{{ regionText(r) }}</option>
            </select></label>
          <div class="ab-two">
            <label class="field"><span>{{ t('ab.country') }}</span><input v-model="ed.country" maxlength="80" data-testid="ab-f-country" /></label>
            <label class="field"><span>{{ t('ab.country_code') }}</span><input v-model="ed.country_code" maxlength="2" data-testid="ab-f-cc" /></label>
            <label class="field"><span>{{ t('ab.province') }}</span><input v-model="ed.province" maxlength="120" data-testid="ab-f-province" /></label>
            <label class="field"><span>{{ t('ab.city') }}</span><input v-model="ed.city" maxlength="120" data-testid="ab-f-city" /></label>
          </div>
          <label class="field"><span>{{ t('ab.place_name') }}</span><input v-model="ed.place_name" maxlength="200" :placeholder="t('ab.place_name_ph')" data-testid="ab-f-place" /></label>
          <label class="field"><span>{{ t('ab.col.line') }} *</span><input v-model="ed.line" maxlength="300" data-testid="ab-f-line" /></label>
          <div class="small muted" style="margin:-6px 0 10px">{{ t('ab.place_name_hint') }}</div>
          <label class="field"><span>{{ t('ab.latlng') }} *</span>
            <div class="row"><input v-model="ed.at" class="grow" placeholder="14.554729, 121.024445" data-testid="ab-f-at" />
              <button type="button" class="btn" data-testid="ab-f-map" @click="picking = true">{{ t('ab.pick_map') }}</button>
              <a class="btn" :class="{ off: !edAt }" :href="edAt ? googleMapsLink(edAt[0], edAt[1]) : undefined" target="_blank" rel="noopener">{{ t('ab.view') }}</a></div></label>
          <p class="small muted">{{ t('ab.form_hint') }}</p>
        </div>
        <div class="sheet-foot row">
          <button type="button" class="btn grow" @click="ed.open = false">{{ t('ab.cancel') }}</button>
          <button class="btn primary grow" :disabled="ed.saving" data-testid="ab-save">{{ t('ab.save') }}</button>
        </div>
      </form>
    </div>
    <PlacePicker v-if="picking" class="ab-picker" :start="edAt ? { lat: edAt[0], lng: edAt[1] } : null" :name="ed.line" keepable region
      @done="picked" @close="picking = false" />
  </div>
</template>

<style scoped>
.ab-intro { margin: 0; }
.ab-grid { display: grid; grid-template-columns: minmax(220px, 280px) minmax(0, 1fr); gap: 14px; align-items: start; }
.ab-regions { display: grid; gap: 12px; padding: 10px; }
.ab-col { display: grid; gap: 2px; }
.ab-h { font-size: 12px; font-weight: 700; color: var(--muted); padding: 2px 8px; }
.ab-col button { display: flex; align-items: center; gap: 8px; text-align: start; background: none; border: 0; border-radius: 8px; padding: 7px 8px; font-size: 14px; cursor: pointer; color: inherit; }
.ab-col button:hover { background: var(--bg); }
.ab-col button.on { background: var(--brand-soft); color: var(--brand-dark); font-weight: 700; }
.ab-cc { margin-inline-start: 5px; }
.ab-col b { font-size: 12px; color: var(--muted); font-weight: 600; }
.ab-main { display: grid; gap: 10px; min-width: 0; }
.ab-filters { gap: 8px; }
.ab-filters .ab-q { flex: 1 1 240px; min-width: 0; max-width: 420px; }
.ab-filters select { width: auto; }
.ab-table { overflow-x: auto; padding: 0; }
.ab-line { min-width: 220px; overflow-wrap: anywhere; font-weight: 600; }
.ab-empty { text-align: center; padding: 24px !important; }
.mono { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; }
.nowrap { white-space: nowrap; }
.num { text-align: end; }
.acts { gap: 6px; flex-wrap: nowrap; }
.ab-two { display: grid; grid-template-columns: 1fr 1fr; gap: 0 12px; }
a.btn.off { opacity: .45; pointer-events: none; }
.ab-picker { z-index: 70; }
@media (max-width: 899px) {
  .ab-grid { grid-template-columns: 1fr; }
  .ab-regions { grid-template-columns: repeat(auto-fit, minmax(160px, 1fr)); }
  .ab-col { max-height: 180px; overflow-y: auto; }
}
@media (max-width: 599px) {
  .ab-two { grid-template-columns: 1fr; }
  .ab-filters .ab-q { flex-basis: 100%; max-width: none; }
}
</style>
