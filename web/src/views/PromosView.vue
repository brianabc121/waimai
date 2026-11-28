<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { get, post, put } from '../api'
import { money, promoLabel, t, toMajor, toMinor } from '../i18n'
import { currentRole, session } from '../session'
import type { Category, Promotion, Totals } from '../types'
import { useRoute, useRouter } from 'vue-router'
import CouponRules from './CouponRules.vue'
import BrandSeg from './brands/BrandSeg.vue'
import BrandTag from './brands/BrandTag.vue'
import { brandById, brandsHere, multiBrand, useBrandPick } from '../brands'
import '../coupons'
import '../checkout'
import '../officeStrings'
import LangsButton from './LangsButton.vue'
import './langsStrings'

// Three tabs: delivery promotions, dine-in promotions (the two never mix,
// docs/CHECKOUT_DISCOUNT_DESIGN.md §4) and coupons. The tab is kept in the address.
// The same page is 优惠 (promotions on the whole order, and coupons) and,
// with items set, 商品营销: the deals and purchase limits on chosen items.
const props = defineProps<{ items?: boolean }>()
const route = useRoute(), router = useRouter()
const tab = computed(() => route.query.tab === 'coupons' && !props.items ? 'coupons' : route.query.tab === 'dine' ? 'dine' : 'promos')
const setTab = (k: string) => { result.value = null; trial.service_type = k === 'dine' ? 'dine_in' : 'delivery'; router.replace({ query: k === 'promos' ? {} : { tab: k } }) }
const scope = computed(() => tab.value === 'dine' ? 'dine_in' : 'delivery')
const dine = computed(() => scope.value === 'dine_in')

const bid = session.branchId!
const cur = currentRole()?.currency ?? 'PHP'
const promos = ref<Promotion[]>([])
const error = ref('')

const all = ref<Promotion[]>([])
async function load() { all.value = (await get<{ promotions: Promotion[] }>(`/merchant/branches/${bid}/promotions`)).promotions }
// Each promotion applies to the brands it lists (docs/MULTI_BRAND_DESIGN.md §10);
// the page shows one brand's, or all of them.
const { pick: brandPick } = useBrandPick('promos', brandsHere, true)
const mainId = () => brandsHere().find(b => b.is_main)?.id ?? 0
const forBrand = (p: Promotion) => !brandPick.value || (p.brand_ids ?? []).includes(brandPick.value)
watch([all, scope, brandPick, () => props.items], () => {
  promos.value = all.value.filter(p => (p.scope ?? 'delivery') === scope.value && forBrand(p) && isItemKind(p.kind) === !!props.items)
}, { immediate: true })
const brandsOf = (p: Promotion) => (p.brand_ids ?? []).map(id => brandById(id)).filter(b => b !== null)
function toggleBrand(id: number) {
  const i = form.f.brands.indexOf(id)
  if (i >= 0) { if (form.f.brands.length > 1) form.f.brands.splice(i, 1) } else form.f.brands.push(id)
}
onMounted(() => { void load(); void loadMenu() })

async function run(fn: () => Promise<unknown>) {
  error.value = ''
  try { await fn(); await load(); return true } catch (e) { error.value = (e as Error).message; return false }
}

// Amounts are typed in major units and percentages as "10" for 10% off.
// Dates use the browser's time zone, which is normally the store's.
interface Form { id: number; scope: 'delivery' | 'dine_in'; name: string; name_en: string; kind: Promotion['kind']; threshold: number; value: number
  cap: number | ''; applies_delivery: boolean; applies_pickup: boolean; starts: string; ends: string
  daily_start: string; daily_end: string; quota: number | ''; per_phone: number | ''; items: number[]; brands: number[] }
const blank = (): Form => ({ id: 0, scope: scope.value, name: '', name_en: '', kind: props.items ? 'dish_second' : 'amount_off', threshold: 0, value: 0, cap: '', applies_delivery: true,
  applies_pickup: true, starts: '', ends: '', daily_start: '00:00', daily_end: '00:00', quota: '', per_phone: '', items: [],
  ...(props.items ? { value: 50 } : {}), brands: [brandPick.value || mainId()].filter(Boolean) })
const form = reactive<{ open: boolean; f: Form }>({ open: false, f: blank() })

// Promotions on chosen dishes (docs/PRICING_REVIEW.md step 4): a percentage
// off, typed as "20" for 20% off, and the dishes it is on.
const isDish = (k: string) => k === 'dish_percent' || k === 'dish_second'
// Purchase limits on chosen items: how many on one order, or per customer per day.
const isLimit = (k: string) => k === 'dish_limit_order' || k === 'dish_limit_day'
const isItemKind = (k: string) => isDish(k) || isLimit(k)
const pctKind = (k: string) => k === 'percent_off' || isDish(k)
const menu = ref<Category[]>([])
const dishFilter = ref('')
async function loadMenu() {
  if (menu.value.length) return
  try { menu.value = (await get<{ categories: Category[] }>(`/merchant/branches/${bid}/menu`)).categories } catch (e) { error.value = (e as Error).message }
}
watch(() => form.f.kind, (k, old) => {
  if (!form.open) return
  if (isItemKind(k)) void loadMenu()
  // a percentage makes no sense as a number of pieces, and the other way round
  if (isLimit(k) !== isLimit(old)) form.f.value = isLimit(k) ? 2 : k === 'dish_second' ? 50 : 0
})
const dishGroups = computed(() => {
  const q = dishFilter.value.trim().toLowerCase()
  return menu.value.map(c => ({ ...c, items: c.items.filter(i => (!q || i.name.toLowerCase().includes(q) || (i.name_en ?? '').toLowerCase().includes(q) || (i.code ?? '').toLowerCase().includes(q))) }))
    .filter(c => c.items.length)
})
function toggleDish(id: number) {
  const i = form.f.items.indexOf(id)
  if (i >= 0) form.f.items.splice(i, 1); else form.f.items.push(id)
}
const dishNames = (p: Promotion) => {
  const names = new Map(menu.value.flatMap(c => c.items.map(i => [i.id, i.name] as const)))
  const ids = p.item_ids ?? []
  const shown = ids.slice(0, 3).map(id => names.get(id)).filter(Boolean)
  return shown.length ? shown.join('、') + (ids.length > 3 ? ' …' : '') : t('promo.dishes_n', { n: ids.length })
}

const pad = (n: number) => String(n).padStart(2, '0')
function toLocal(iso: string | null) {
  if (!iso) return ''
  const d = new Date(iso)
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}
const toISO = (v: string) => (v ? new Date(v).toISOString() : null)
const num = (v: number | '') => (v === '' || v === null ? null : Number(v))

function edit(p: Promotion | null) {
  form.f = p ? { id: p.id, scope: p.scope ?? 'delivery', name: p.name, name_en: p.name_en, kind: p.kind, threshold: toMajor(p.threshold),
    value: pctKind(p.kind) ? p.value / 100 : isLimit(p.kind) ? p.value : toMajor(p.value), cap: p.max_discount === null ? '' : toMajor(p.max_discount),
    applies_delivery: p.applies_delivery, applies_pickup: p.applies_pickup, starts: toLocal(p.starts_at), ends: toLocal(p.ends_at),
    daily_start: p.daily_start, daily_end: p.daily_end, quota: p.total_quota ?? '', per_phone: p.per_phone_limit ?? '', items: [...(p.item_ids ?? [])],
    brands: [...(p.brand_ids ?? [])] } : blank()
  error.value = ''
  form.open = true
  if (isItemKind(form.f.kind)) void loadMenu()
}

async function save() {
  const f = form.f
  const dineIn = f.scope === 'dine_in'
  const body = { scope: f.scope, name: f.name, name_en: f.name_en, kind: f.kind, threshold: isLimit(f.kind) ? 0 : toMinor(f.threshold),
    value: pctKind(f.kind) ? Math.round(Number(f.value) * 100) : isLimit(f.kind) ? Math.round(Number(f.value)) : f.kind === 'amount_off' ? toMinor(f.value) : 0,
    max_discount: f.kind === 'percent_off' && num(f.cap) ? toMinor(f.cap as number) : null,
    applies_delivery: !dineIn && f.applies_delivery, applies_pickup: !dineIn && f.kind !== 'free_delivery' && f.applies_pickup,
    starts_at: toISO(f.starts), ends_at: toISO(f.ends), daily_start: f.daily_start || '00:00', daily_end: f.daily_end || '00:00',
    total_quota: isItemKind(f.kind) ? null : num(f.quota), per_phone_limit: dineIn || isItemKind(f.kind) ? null : num(f.per_phone),
    item_ids: isItemKind(f.kind) ? f.items : [], brand_ids: f.brands }
  if (await run(() => f.id ? put(`/merchant/branches/${bid}/promotions/${f.id}`, body) : post(`/merchant/branches/${bid}/promotions`, body))) form.open = false
}

function setStatus(p: Promotion, status: string) {
  if (status === 'ended' && !confirm(t('promo_end_confirm'))) return
  void run(() => post(`/merchant/branches/${bid}/promotions/${p.id}/status`, { status }))
}

const STATES = ['active', 'draft', 'paused', 'ended'] as const
const st = ref('')
const picked = computed(() => promos.value.filter(p => !st.value || p.status === st.value))
const statusTag = (p: Promotion) => p.status === 'active' ? (p.live_now ? 'ok' : 'warn') : p.status === 'paused' ? 'warn' : p.status === 'ended' ? 'bad' : ''
const statusText = (p: Promotion) => p.status === 'active' && !p.live_now ? t('promo.not_live') : t('promo.' + p.status)
const when = (p: Promotion) => {
  const parts: string[] = []
  if (p.starts_at || p.ends_at) parts.push(`${p.starts_at ? new Date(p.starts_at).toLocaleString() : '…'} – ${p.ends_at ? new Date(p.ends_at).toLocaleString() : '…'}`)
  if (p.daily_start !== p.daily_end) parts.push(`${t('daily')} ${p.daily_start}–${p.daily_end}`)
  return parts.join(' · ') || t('always')
}

// ---- trial calculation ----
const trial = reactive({ service_type: 'delivery', subtotal: 300, delivery_fee: 49 })
const result = ref<{ rows: { id: number; name: string; status: string; discount: number; reason: string; wins: boolean }[]; totals: Totals } | null>(null)
async function runTrial() {
  error.value = ''
  try {
    result.value = await post(`/merchant/branches/${bid}/promotion-trial`, { service_type: trial.service_type,
      subtotal: toMinor(trial.subtotal), delivery_fee: toMinor(trial.delivery_fee), brand_id: brandPick.value })
  } catch (e) { error.value = (e as Error).message }
}
</script>

<template>
  <div class="ox warm">
  <div class="page ox-page">
    <div class="ox-head">
      <div><h1>{{ items ? t('ip.page') : t('cr.page') }}</h1><p class="ox-sub">{{ items ? t('ip.sub') : t('ox.pr.sub') }}</p></div>
      <div class="ox-acts"><button v-if="tab !== 'coupons'" class="btn primary" data-testid="promo-new" @click="edit(null)">＋ {{ items ? t('ip.new') : dine ? t('ck.new_dine') : t('new_promo') }}</button></div>
    </div>
    <div class="ox-tabs" role="tablist">
      <button type="button" role="tab" :class="{ on: tab === 'promos' }" data-testid="promo-tab" @click="setTab('promos')">{{ t('ck.tab_delivery') }}</button>
      <button type="button" role="tab" :class="{ on: tab === 'dine' }" data-testid="promo-dine-tab" @click="setTab('dine')">{{ t('ck.tab_dine') }}</button>
      <button v-if="!items" type="button" role="tab" :class="{ on: tab === 'coupons' }" data-testid="coupon-tab" @click="setTab('coupons')">{{ t('ck.tab_coupons') }}</button>
    </div>
    <div v-if="multiBrand()" class="pr-brands"><BrandSeg v-model="brandPick" :brands="brandsHere()" :all="tab !== 'coupons'" /></div>
    <div v-if="tab === 'coupons'" class="ox-panel"><CouponRules :brand="brandPick || mainId()" /></div>
    <template v-else>
    <div v-if="error && !form.open" class="banner bad">{{ error }}</div>

    <div class="ox-body">
      <aside class="ox-rail">
        <div class="ox-rail-h">{{ t('ox.pr.state') }}</div>
        <div class="ox-rail-list">
          <button class="ox-cat" :class="{ on: st === '' }" @click="st = ''"><span class="nm">{{ t('ox.pr.all') }}</span><span class="n">{{ promos.length }}</span></button>
          <button v-for="k in STATES" :key="k" class="ox-cat" :class="{ on: st === k }" :data-state="k" @click="st = k"><span class="nm">{{ t('promo.' + k) }}</span><span class="n">{{ promos.filter(p => p.status === k).length }}</span></button>
        </div>
        <p v-if="items" class="ox-rail-tip">{{ t('promo.dish_hint') }}</p>
        <p v-else-if="!dine" class="ox-rail-tip">{{ t('promos_hint') }} {{ t('ck.delivery_hint') }}</p>
      </aside>

      <div class="ox-stack">
        <section class="ox-panel">
          <p v-if="dine" class="ox-tip small" style="margin:0 0 12px" data-testid="promo-dine-hint">{{ t('ck.dine_hint') }}</p>
          <div class="ox-scroll">
            <table class="data">
              <thead><tr><th>{{ t('promo_name') }}</th><th>{{ t('promo_rule') }}</th><th>{{ t('promo_when') }}</th><th class="num">{{ t('promo_usage') }}</th><th>{{ t('ox.pr.state') }}</th><th class="right">{{ t('ox.ops') }}</th></tr></thead>
              <tbody>
                <tr v-for="p in picked" :key="p.id">
                  <td><b class="ox-strong">{{ p.name }}</b><div class="muted small">{{ p.name_en }}</div>
                    <div v-if="multiBrand()" class="row wrap" style="gap:4px;margin-top:2px" data-testid="promo-brands"><BrandTag v-for="b in brandsOf(p)" :key="b.id" :name="b.name" :color="b.color" /></div></td>
                  <td><span class="ox-tag brand">{{ promoLabel(p, cur) }}</span>
                    <div class="muted small" v-if="isItemKind(p.kind)" data-testid="promo-dishes">{{ dishNames(p) }}</div>
                    <div class="muted small" v-if="!dine && !isLimit(p.kind)">{{ [p.applies_delivery && t('delivery'), p.applies_pickup && t('pickup')].filter(Boolean).join(' · ') }}</div></td>
                  <td class="small">{{ when(p) }}</td>
                  <td class="num"><template v-if="!isLimit(p.kind)">{{ p.used_count }}</template><span v-else class="muted">—</span><span v-if="p.total_quota" class="muted"> / {{ p.total_quota }}</span>
                    <div class="muted small" v-if="p.per_phone_limit">{{ t('per_customer_n', { n: p.per_phone_limit }) }}</div></td>
                  <td><span class="tag ox-pill" :class="statusTag(p)">{{ statusText(p) }}</span></td>
                  <td class="right">
                    <span v-if="p.status !== 'ended'" class="ox-ops">
                      <button v-if="p.status === 'draft'" class="op red" @click="setStatus(p, 'active')">{{ t('publish') }}</button>
                      <button v-if="p.status === 'active'" class="op" @click="setStatus(p, 'paused')">{{ t('pause') }}</button>
                      <button v-if="p.status === 'paused'" class="op red" @click="setStatus(p, 'active')">{{ t('resume') }}</button>
                      <button class="op red" @click="edit(p)">{{ t('edit') }}</button>
                      <button class="op bad" @click="setStatus(p, 'ended')">{{ t('end') }}</button>
                    </span>
                  </td>
                </tr>
                <tr v-if="picked.length === 0"><td colspan="6" class="ox-empty">{{ items ? t('ip.none') : t('no_promos') }}</td></tr>
              </tbody>
            </table>
          </div>
        </section>

        <form v-if="!items" class="ox-panel stack" @submit.prevent="runTrial">
          <div class="ox-panel-h" style="margin:0"><h2>{{ t('trial') }}</h2></div>
          <p class="muted small" style="margin:0">{{ t('trial_hint') }}</p>
          <div class="ox-tools" style="margin:0">
            <select v-if="!dine" v-model="trial.service_type" style="flex:0 1 120px;min-width:0" :aria-label="t('delivery')">
              <option value="delivery">{{ t('delivery') }}</option><option value="pickup">{{ t('pickup') }}</option>
            </select>
            <label class="row" style="gap:6px">{{ t('subtotal') }}<input type="number" min="0" step="0.01" v-model.number="trial.subtotal" style="width:110px" /></label>
            <label class="row" style="gap:6px" v-if="trial.service_type === 'delivery'">{{ t('delivery_fee') }}<input type="number" min="0" step="0.01" v-model.number="trial.delivery_fee" style="width:110px" /></label>
            <button class="btn primary">{{ t('trial_run') }}</button>
          </div>
          <template v-if="result">
            <table class="data">
              <tbody><tr v-for="r in result.rows" :key="r.id">
                <td>{{ r.name }} <span class="tag" v-if="r.status !== 'active'">{{ t('promo.' + r.status) }}</span></td>
                <td class="price">{{ r.reason ? '—' : '−' + money(r.discount, cur) }}</td>
                <td><span v-if="r.wins" class="tag ok">{{ t('trial_wins') }}</span><span v-else-if="r.reason" class="muted small">{{ t('why.' + r.reason) }}</span></td>
              </tr></tbody>
            </table>
            <div class="row between bold trial-total"><span>{{ t('total') }}</span>
              <span class="ox-money">{{ money(result.totals.total, cur) }} <span class="muted small" v-if="result.totals.discount">(−{{ money(result.totals.discount, cur) }})</span></span></div>
          </template>
        </form>
      </div>
    </div>

    <div class="sheet-mask" v-if="form.open" @click.self="form.open = false">
      <form class="sheet center" @submit.prevent="save">
        <div class="sheet-head row between"><h2>{{ form.f.id ? t('edit') : items ? t('ip.new') : form.f.scope === 'dine_in' ? t('ck.new_dine') : t('new_promo') }}
          <span v-if="form.f.scope === 'dine_in'" class="tag">{{ t('dine_in') }}</span></h2>
          <LangsButton :base="`/merchant/branches/${bid}`" :scope="form.f.id ? `promotion/${form.f.id}` : null" :name="form.f.name" /></div>
        <div class="sheet-body">
          <div v-if="error" class="banner bad" style="margin-bottom:12px">{{ error }}</div>
          <div class="grid2">
            <label class="field" style="grid-column:1 / -1"><span>{{ t('lg.name') }}</span><input v-model="form.f.name" name="promo_name" required :placeholder="t('promo_name_ph')" /></label>
            <label class="field"><span>{{ t('promo_kind') }}</span><select v-model="form.f.kind" data-testid="promo-kind">
              <template v-if="items">
                <option value="dish_second">{{ t('kind.dish_second') }}</option>
                <option value="dish_limit_order">{{ t('kind.dish_limit_order') }}</option>
                <option value="dish_limit_day">{{ t('kind.dish_limit_day') }}</option>
                <option value="dish_percent">{{ t('kind.dish_percent') }}</option>
              </template>
              <template v-else>
                <option value="amount_off">{{ t('kind.amount_off') }}</option>
                <option value="percent_off">{{ t('kind.percent_off') }}</option>
                <option v-if="form.f.scope !== 'dine_in'" value="free_delivery">{{ t('kind.free_delivery') }}</option>
              </template></select></label>
            <label class="field" v-if="isLimit(form.f.kind)"><span>{{ t('promo.limit_n') }}</span><input type="number" min="1" max="99" step="1" v-model.number="form.f.value" required data-testid="limit-n" /></label>
            <label class="field" v-else><span>{{ t('promo_threshold') }} ({{ cur }})</span><input type="number" min="0" step="0.01" v-model.number="form.f.threshold" /></label>
            <label class="field" v-if="form.f.kind === 'amount_off'"><span>{{ t('promo_amount') }} ({{ cur }})</span><input type="number" min="0.01" step="0.01" v-model.number="form.f.value" required /></label>
            <label class="field" v-if="form.f.kind === 'percent_off'"><span>{{ t('promo_percent') }}</span><input type="number" min="1" max="99" step="0.5" v-model.number="form.f.value" required /></label>
            <label class="field" v-if="form.f.kind === 'dish_percent'"><span>{{ t('promo_percent') }}</span><input type="number" min="1" max="99" step="0.5" v-model.number="form.f.value" required data-testid="dish-pct" /></label>
            <label class="field" v-if="form.f.kind === 'dish_second'"><span>{{ t('promo.second_pct') }}</span><input type="number" min="1" max="100" step="1" v-model.number="form.f.value" required data-testid="dish-second" /></label>
            <label class="field" v-if="form.f.kind === 'percent_off'"><span>{{ t('promo_cap') }} ({{ cur }})</span><input type="number" min="0" step="0.01" v-model="form.f.cap" :placeholder="t('unlimited')" /></label>
          </div>
          <p v-if="form.f.kind === 'dish_second'" class="muted small" data-testid="second-note">{{ t('ip.second_note') }}</p>
          <div v-if="isItemKind(form.f.kind)" class="stack dishpick" data-testid="dish-pick">
            <div class="row between"><b>{{ t('promo.dishes') }}</b><span class="muted small">{{ t('promo.dishes_n', { n: form.f.items.length }) }}</span></div>
            <input v-model="dishFilter" :placeholder="t('promo.dish_search')" />
            <div class="dishlist">
              <template v-for="c in dishGroups" :key="c.id">
                <div class="muted small cat">{{ c.name }}</div>
                <label v-for="i in c.items" :key="i.id" class="check"><input type="checkbox" :checked="form.f.items.includes(i.id)" @change="toggleDish(i.id)" />
                  <span v-if="i.code" class="muted">{{ i.code }}</span> {{ i.name }} <span class="muted small">{{ money(i.price, cur) }}</span></label>
              </template>
            </div>
            <p class="muted small">{{ isLimit(form.f.kind) ? t('promo.limit_hint') : t('promo.dish_hint') }}</p>
          </div>
          <div v-if="multiBrand()" class="stack" style="gap:4px;margin:6px 0" data-testid="promo-f-brands">
            <b class="small">{{ t('br.promo_brands') }}</b>
            <div class="row wrap" style="gap:10px">
              <label v-for="b in brandsHere()" :key="b.id" class="check"><input type="checkbox" :checked="form.f.brands.includes(b.id)" :data-brand="b.id" @change="toggleBrand(b.id)" />{{ b.name }}</label>
            </div>
            <span class="muted small">{{ t('br.promo_hint') }}</span>
          </div>
          <div class="grid2" v-if="form.f.scope !== 'dine_in' && !isLimit(form.f.kind)">
            <label class="check"><input type="checkbox" v-model="form.f.applies_delivery" />{{ t('delivery') }}</label>
            <label class="check" v-if="form.f.kind !== 'free_delivery'"><input type="checkbox" v-model="form.f.applies_pickup" />{{ t('pickup') }}</label>
          </div>
          <div class="grid2">
            <label class="field"><span>{{ t('promo_starts') }}</span><input type="datetime-local" v-model="form.f.starts" /></label>
            <label class="field"><span>{{ t('promo_ends') }}</span><input type="datetime-local" v-model="form.f.ends" /></label>
            <label class="field"><span>{{ t('promo_daily_start') }}</span><input type="time" v-model="form.f.daily_start" /></label>
            <label class="field"><span>{{ t('promo_daily_end') }}</span><input type="time" v-model="form.f.daily_end" /></label>
            <label class="field" v-if="!isItemKind(form.f.kind)"><span>{{ t('promo_quota') }}</span><input type="number" min="1" step="1" v-model="form.f.quota" :placeholder="t('unlimited')" /></label>
            <label class="field" v-if="form.f.scope !== 'dine_in' && !isItemKind(form.f.kind)"><span>{{ t('promo_per_phone') }}</span><input type="number" min="1" step="1" v-model="form.f.per_phone" :placeholder="t('unlimited')" /></label>
          </div>
          <p class="muted small">{{ t('promo_time_hint') }}</p>
          <p v-if="form.f.scope === 'dine_in'" class="muted small">{{ t('ck.dine_time_hint') }}</p>
        </div>
        <div class="sheet-foot row">
          <button type="button" class="btn grow" @click="form.open = false">{{ t('cancel') }}</button>
          <button class="btn primary grow">{{ t('save') }}</button>
        </div>
      </form>
    </div>
    </template>
  </div>
  </div>
</template>

<style scoped>
.trial-total { font-size: 16px; padding: 4px 2px 0; }
.pr-brands { margin: 0 0 12px; }
.grid2 { display: grid; grid-template-columns: 1fr 1fr; gap: 0 12px; align-items: center; }
.dishpick { margin: 8px 0; }
.dishlist { max-height: 240px; overflow: auto; border: 1px solid var(--line, #e5e7eb); border-radius: 8px; padding: 4px 10px; }
.dishlist .cat { margin-top: 6px; }
.dishlist .check { display: flex; gap: 6px; align-items: center; padding: 3px 0; }
</style>
