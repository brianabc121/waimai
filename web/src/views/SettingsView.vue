<script setup lang="ts">
import { storeURL } from '../site'
import '../addon'
import '../progress'
import { computed, onMounted, reactive, ref } from 'vue'
import '../officeStrings'
import { get, patch, post, put } from '../api'
import { both, extend, money, pick, t, toMajor, toMinor } from '../i18n'
import { can, session } from '../session'
import { CONTACT_FIELDS } from '../addresses'
import type { Branch, Station, Zone } from '../types'
import StoreTextsCard from './StoreTextsCard.vue'
import PaySettingsCard from './checkout/PaySettingsCard.vue'
import OrderNumberingCard from './OrderNumberingCard.vue'

const bid = session.branchId!
const branch = ref<Branch | null>(null)
const zones = ref<Zone[]>([])
const stations = ref<Station[]>([])
const error = ref('')
const saved = ref(false)
const texts = ref<InstanceType<typeof StoreTextsCard> | null>(null)

const s = reactive({ name: '', name_en: '', address: '', phone: '', tax: 12, tax_inclusive: true, service: 0, packaging: 0,
  min_order: 0, accept_timeout_minutes: 10, addon_confirm_minutes: 5, cook_target_minutes: 15, delivery_enabled: true, pickup_enabled: true, paused: false, open_time: '00:00', close_time: '00:00', notify_sms: true, scheduling_enabled: false, prep_minutes: 30,
  kitchen_slow_minutes: 15, detour_percent: 30, detour_km: 1, notice: '', intro: '', baskets_enabled: true, contact_fields: [] as string[] })
const st = reactive({ id: 0, name: '', name_en: '', active: true })

// ---- how the store shows in the marketplace ----
interface Listing { listed: boolean; hidden: boolean; tagline: string; category_ids: number[]
  categories: { id: number; name: string; name_en: string; icon: string }[]; has_location: boolean; has_zones: boolean; has_logo: boolean; has_cover: boolean }
const listing = ref<Listing | null>(null)
const ml = reactive({ listed: true, tagline: '', category_ids: [] as number[] })
const mallSaved = ref(false), mallError = ref('')
function setListing(l: Listing) { listing.value = l; Object.assign(ml, { listed: l.listed, tagline: l.tagline, category_ids: [...l.category_ids] }) }
function toggleCat(id: number) {
  if (ml.category_ids.includes(id)) ml.category_ids = ml.category_ids.filter(x => x !== id)
  else if (ml.category_ids.length < 2) ml.category_ids = [...ml.category_ids, id]
}
async function saveListing() {
  mallError.value = ''; mallSaved.value = false
  try { setListing(await put(`/merchant/branches/${bid}/mall`, ml)); mallSaved.value = true } catch (e) { mallError.value = (e as Error).message }
}
extend({
  'mls.title': ['商城展示', 'Marketplace'], 'mls.hint': ['顾客在商城首页按自己的位置看到能送到的店。这里决定你的店在不在里面、显示什么。', 'Customers see the stores that deliver to them on the marketplace home page. Decide here whether yours is there and what it says.'],
  'mls.listed': ['在商城里展示', 'Show in the marketplace'], 'mls.listed_hint': ['关掉以后，顾客仍然可以用店铺链接直接下单', 'When off, customers can still order through the store link'],
  'mls.cats': ['店铺分类（最多选 2 个）', 'Categories (up to 2)'], 'mls.tagline': ['一句话介绍', 'Tagline'], 'mls.tagline_ph': ['例如：现煮牛肉面，每天现熬汤头', 'For example: beef noodles, broth made daily'],
  'mls.hidden': ['平台已把这家店从商城下架。有疑问请联系平台。', 'The platform has taken this store off the marketplace. Contact the platform if you have questions.'],
  'mls.no_location': ['还没有设置店铺位置，商城里不会出现。', 'The store has no location yet, so it does not appear.'],
  'mls.no_zones': ['还没有画配送范围，商城里只会出现在“可自取”里。', 'No delivery area is drawn yet, so the store only shows under pickup.'],
  'mls.no_logo': ['还没有店铺头像，建议上传。', 'There is no store logo yet. Uploading one is recommended.'],
  'mls.no_cover': ['还没有封面图，电脑和 iPad 上的店铺卡会是空的。', 'There is no cover picture, so the card is empty on a computer or tablet.'],
  'mls.fix_map': ['去配送地图', 'Delivery map'], 'mls.fix_tpl': ['去店铺装修', 'Store design'], 'mls.preview': ['看看商城', 'Open the marketplace'],
})

async function load() {
  if (!listing.value && can('mall')) void get(`/merchant/branches/${bid}/mall`).then(setListing).catch(() => null)
  const m = await get(`/merchant/branches/${bid}/menu`)
  const b: Branch = m.branch
  branch.value = b; zones.value = m.zones; stations.value = m.stations
  Object.assign(s, { name: b.name, name_en: b.name_en, address: b.address, phone: b.phone, tax: b.tax_rate_bp / 100,
    tax_inclusive: b.tax_inclusive, service: b.service_charge_bp / 100, packaging: toMajor(b.packaging_fee), min_order: toMajor(b.min_order_amount),
    accept_timeout_minutes: b.accept_timeout_minutes, addon_confirm_minutes: b.addon_confirm_minutes ?? 5, cook_target_minutes: b.cook_target_minutes ?? 15, delivery_enabled: b.delivery_enabled, pickup_enabled: b.pickup_enabled,
    paused: b.paused, open_time: b.open_time, close_time: b.close_time, notify_sms: b.notify_sms,
    scheduling_enabled: b.scheduling_enabled, prep_minutes: b.prep_minutes,
    kitchen_slow_minutes: b.kitchen_slow_minutes, detour_percent: b.detour_percent, detour_km: b.detour_meters / 1000,
    notice: b.notice, intro: b.intro, baskets_enabled: b.baskets_enabled, contact_fields: [...(b.contact_fields ?? [])] })
}
onMounted(load)

async function run(fn: () => Promise<unknown>) {
  error.value = ''; saved.value = false
  try { await fn(); await load(); saved.value = true; return true } catch (e) { error.value = (e as Error).message; return false }
}

const saveSettings = () => run(() => put(`/merchant/branches/${bid}/settings`, {
  name: s.name, name_en: s.name_en, address: s.address, phone: s.phone, tax_rate_bp: Math.round(s.tax * 100), tax_inclusive: s.tax_inclusive,
  service_charge_bp: Math.round(s.service * 100), packaging_fee: toMinor(s.packaging), min_order_amount: toMinor(s.min_order),
  accept_timeout_minutes: s.accept_timeout_minutes, addon_confirm_minutes: s.addon_confirm_minutes, cook_target_minutes: s.cook_target_minutes, delivery_enabled: s.delivery_enabled, pickup_enabled: s.pickup_enabled,
  paused: s.paused, open_time: s.open_time, close_time: s.close_time, notify_sms: s.notify_sms,
  scheduling_enabled: s.scheduling_enabled, prep_minutes: s.prep_minutes,
  kitchen_slow_minutes: s.kitchen_slow_minutes, detour_percent: s.detour_percent, detour_meters: Math.round(s.detour_km * 1000),
  notice: s.notice, intro: s.intro, baskets_enabled: s.baskets_enabled, contact_fields: s.contact_fields })).then(ok => {
  // the other languages are filled in a few seconds after the save
  if (ok) { texts.value?.load(); setTimeout(() => texts.value?.load(), 5000) }
  return ok
})

function editStation(x: Station | null) {
  Object.assign(st, x ? { id: x.id, name: x.name, name_en: x.name_en, active: x.active } : { id: 0, name: '', name_en: '', active: true })
}
async function saveStation() {
  const body = { name: st.name, name_en: st.name_en, active: st.active, sort: st.id ? stations.value.find(x => x.id === st.id)?.sort ?? 0 : stations.value.length }
  if (await run(() => st.id ? put(`/merchant/branches/${bid}/stations/${st.id}`, body) : post(`/merchant/branches/${bid}/stations`, body))) editStation(null)
}

// the store's own page (docs/ORDER_TYPES_DESIGN.md §5)
const link = () => branch.value ? storeURL(branch.value) : ''
const copied = ref(false)
async function copyLink() { try { await navigator.clipboard.writeText(link()); copied.value = true } catch { /* the link is shown anyway */ } }
// the sections in the rail; a click scrolls to one
const sections = computed(() => [
  { id: 'set-link', label: t('share_link'), on: true },
  { id: 'set-store', label: t('store_settings'), on: can('settings') },
  { id: 'set-pay', label: t('ox.set.pay'), on: can('settings') },
  { id: 'set-numbers', label: t('ot.set.title'), on: can('settings') },
  { id: 'set-texts', label: t('ox.set.texts'), on: can('menu.translate') },
  { id: 'set-mall', label: t('mls.title'), on: !!listing.value && can('mall') },
  { id: 'set-zones', label: t('zones'), on: can('delivery.map') },
  { id: 'set-stations', label: t('stations'), on: can('settings') },
  { id: 'set-staff', label: t('staff'), on: can('staff.view') },
].filter(x => x.on))
const at = ref('set-link')
function jump(id: string) { at.value = id; document.getElementById(id)?.scrollIntoView({ behavior: 'smooth', block: 'start' }) }
</script>

<template>
  <div class="ox warm">
  <div class="page ox-page setpage" v-if="branch">
    <div class="ox-head">
      <div><h1>{{ t('settings') }}</h1><p class="ox-sub">{{ t('ox.set.sub') }}</p></div>
      <div class="ox-acts"><a class="btn" :href="link()" target="_blank" rel="noopener">{{ t('ox.set.open_store') }}</a></div>
    </div>
    <div v-if="error" class="banner bad">{{ error }}</div>
    <div v-else-if="saved" class="banner ok">{{ t('saved') }}</div>

    <div class="ox-body">
    <aside class="ox-rail">
      <div class="ox-rail-h">{{ t('ox.set.sections') }}</div>
      <div class="ox-rail-list">
        <button v-for="x in sections" :key="x.id" type="button" class="ox-cat" :class="{ on: at === x.id }" @click="jump(x.id)"><span class="nm">{{ x.label }}</span></button>
      </div>
    </aside>
    <div class="ox-stack setmain">
    <section id="set-link" class="ox-panel stack">
      <div class="ox-panel-h" style="margin:0"><h2>{{ t('share_link') }}</h2></div>
      <div class="linkrow"><a :href="link()" target="_blank">{{ link() }}</a><button type="button" class="btn small" @click="copyLink">{{ copied ? t('copied') : t('copy_link') }}</button></div>
    </section>

    <form v-if="can('settings')" id="set-store" class="ox-panel" @submit.prevent="saveSettings">
      <div class="ox-panel-h"><h2>{{ t('store_settings') }}</h2><span class="grow"></span><button class="btn primary">{{ t('save') }}</button></div>
      <h3>{{ t('ox.set.basic') }}</h3>
      <div class="grid2">
        <label class="field"><span>{{ t('name_zh') }}</span><input v-model="s.name" required /></label>
        <label class="field"><span>{{ t('name_en') }}</span><input v-model="s.name_en" /></label>
        <label class="field"><span>{{ t('address') }}</span><input v-model="s.address" /></label>
        <label class="field"><span>{{ t('phone') }}</span><input v-model="s.phone" /></label>
        <label class="field"><span>{{ t('open_time') }}</span><input type="time" v-model="s.open_time" required /></label>
        <label class="field"><span>{{ t('close_time') }}</span><input type="time" v-model="s.close_time" required /></label>
      </div>
      <h3>{{ t('ox.set.fees') }}</h3>
      <div class="grid2">
        <label class="field"><span>{{ t('tax_rate') }}</span><input type="number" min="0" max="50" step="0.01" v-model.number="s.tax" /></label>
        <label class="field"><span>{{ t('service_rate') }}</span><input type="number" min="0" max="50" step="0.01" v-model.number="s.service" /></label>
        <label class="field"><span>{{ t('packaging_fee') }} ({{ branch.currency }})</span><input type="number" min="0" step="0.01" v-model.number="s.packaging" /></label>
        <label class="field"><span>{{ t('min_order') }} ({{ branch.currency }})</span><input type="number" min="0" step="0.01" v-model.number="s.min_order" /></label>
        <label class="check"><input type="checkbox" v-model="s.tax_inclusive" />{{ t('tax_inclusive') }}</label>
      </div>
      <h3>{{ t('ox.set.timing') }}</h3>
      <div class="grid2">
        <label class="field"><span>{{ t('accept_timeout') }}</span><input type="number" min="1" max="120" v-model.number="s.accept_timeout_minutes" /></label>
        <label class="field"><span>{{ t('ao.set.minutes') }}</span><input type="number" min="1" max="60" data-testid="addon-minutes" v-model.number="s.addon_confirm_minutes" :title="t('ao.set.minutes_hint')" /></label>
        <label class="field"><span>{{ t('prep_minutes') }}</span><input type="number" min="5" max="240" v-model.number="s.prep_minutes" /></label>
        <label class="field"><span>{{ t('kp.set.target') }}</span><input type="number" min="5" max="90" name="cook_target_minutes" data-testid="cook-target" v-model.number="s.cook_target_minutes" :title="t('kp.set.target_hint')" /></label>
        <label class="field"><span>{{ t('set.slow_minutes') }}</span><input type="number" min="1" max="120" name="kitchen_slow_minutes" v-model.number="s.kitchen_slow_minutes" /></label>
        <label class="field"><span>{{ t('set.detour_percent') }}</span><input type="number" min="5" max="300" name="detour_percent" v-model.number="s.detour_percent" /></label>
        <label class="field"><span>{{ t('set.detour_km') }}</span><input type="number" min="0.1" max="20" step="0.1" name="detour_km" v-model.number="s.detour_km" /></label>
      </div>
      <h3>{{ t('ox.set.switches') }}</h3>
      <div class="grid2 checks">
        <label class="check"><input type="checkbox" v-model="s.paused" />{{ t('pause_orders') }}</label>
        <label class="check"><input type="checkbox" v-model="s.delivery_enabled" />{{ t('delivery') }}</label>
        <label class="check"><input type="checkbox" v-model="s.pickup_enabled" />{{ t('pickup') }}</label>
        <label class="check"><input type="checkbox" v-model="s.notify_sms" />{{ t('notify_sms') }}</label>
        <label class="check"><input type="checkbox" v-model="s.scheduling_enabled" />{{ t('scheduling_enabled') }}</label>
        <label class="check"><input type="checkbox" name="baskets_enabled" v-model="s.baskets_enabled" />{{ t('set.baskets') }}</label>
      </div>
      <fieldset class="contact-fields" data-testid="contact-fields">
        <legend>{{ t('set.contact_fields') }}</legend>
        <p class="muted small">{{ t('set.contact_fields_hint') }}</p>
        <div class="row wrap">
          <label v-for="f in CONTACT_FIELDS" :key="f" class="check"><input type="checkbox" name="contact_fields" :value="f" v-model="s.contact_fields" />{{ t('ad.f.' + f) }}</label>
        </div>
      </fieldset>
      <label class="field"><span>{{ t('set.notice') }}</span><textarea name="notice" v-model="s.notice" rows="2" maxlength="300"></textarea></label>
      <label class="field"><span>{{ t('set.intro') }}</span><textarea name="intro" v-model="s.intro" rows="3" maxlength="500"></textarea></label>
      <div class="setfoot"><button class="btn primary">{{ t('save') }}</button></div>
    </form>

    <div v-if="can('settings')" id="set-pay"><PaySettingsCard :branch-id="bid" /></div>

    <div v-if="can('settings')" id="set-numbers"><OrderNumberingCard :branch="branch" @saved="b => branch = b" /></div>

    <div v-if="can('menu.translate')" id="set-texts"><StoreTextsCard ref="texts" :base="`/merchant/branches/${bid}`" @en-name="v => s.name_en = v" /></div>

    <form v-if="listing && can('mall')" id="set-mall" class="ox-panel stack" data-testid="mall-listing" @submit.prevent="saveListing">
      <h2>{{ t('mls.title') }}</h2>
      <p class="muted small">{{ t('mls.hint') }}</p>
      <div v-if="listing.hidden" class="banner bad" data-testid="mall-listing-hidden">{{ t('mls.hidden') }}</div>
      <div v-if="!listing.has_location" class="banner warn small">{{ t('mls.no_location') }} <router-link to="/merchant/delivery-map">{{ t('mls.fix_map') }}</router-link></div>
      <div v-else-if="!listing.has_zones" class="banner warn small">{{ t('mls.no_zones') }} <router-link to="/merchant/delivery-map">{{ t('mls.fix_map') }}</router-link></div>
      <div v-if="!listing.has_logo || !listing.has_cover" class="banner warn small">{{ t(listing.has_logo ? 'mls.no_cover' : 'mls.no_logo') }} <router-link to="/merchant/templates">{{ t('mls.fix_tpl') }}</router-link></div>
      <label class="check"><input type="checkbox" v-model="ml.listed" data-testid="mall-listed" />{{ t('mls.listed') }} <span class="muted small">{{ t('mls.listed_hint') }}</span></label>
      <div>
        <div class="muted small" style="margin-bottom:6px">{{ t('mls.cats') }}</div>
        <div class="row wrap" style="gap:8px">
          <button v-for="c in listing.categories" :key="c.id" type="button" class="btn small" :class="{ primary: ml.category_ids.includes(c.id) }"
            :disabled="!ml.category_ids.includes(c.id) && ml.category_ids.length >= 2" data-testid="mall-cat" @click="toggleCat(c.id)">{{ c.icon }} {{ pick(c.name, c.name_en) }}</button>
        </div>
      </div>
      <label class="field" style="margin:0"><span>{{ t('mls.tagline') }}</span><input v-model="ml.tagline" maxlength="40" :placeholder="t('mls.tagline_ph')" data-testid="mall-tagline" /></label>
      <div v-if="mallError" class="banner bad small">{{ mallError }}</div>
      <div class="row" style="gap:10px;align-items:center">
        <button class="btn primary" data-testid="mall-listing-save">{{ t('save') }}</button>
        <a class="btn" href="/" target="_blank" rel="noopener">{{ t('mls.preview') }}</a>
        <span v-if="mallSaved" class="tag ok" data-testid="mall-listing-saved">{{ t('saved') }}</span>
      </div>
    </form>

    <div v-if="can('delivery.map')" id="set-zones" class="ox-panel stack">
      <h2>{{ t('zones') }}</h2>
      <p class="muted">{{ t('zones_moved') }}</p>
      <div><span v-for="zone in zones" :key="zone.id" class="tag" :class="zone.active ? 'ok' : ''" style="margin:0 6px 6px 0">{{ both(zone.name, zone.name_en) }} · {{ money(zone.fee, branch.currency) }}</span></div>
      <router-link class="btn" to="/merchant/delivery-map" data-testid="to-delivery-map">{{ t('delivery_map') }}</router-link>
    </div>

    <div v-if="can('settings')" id="set-stations" class="ox-panel stack">
      <h2>{{ t('stations') }}</h2>
      <p class="muted small">{{ t('stations_hint') }}</p>
      <table class="data" v-if="stations.length">
        <tbody><tr v-for="x in stations" :key="x.id">
          <td>{{ both(x.name, x.name_en) }}</td>
          <td class="muted small">{{ t('items_n', { n: x.item_count }) }} · {{ t('tasks_n', { n: x.open_tasks }) }}</td>
          <td><span class="tag" :class="x.active ? 'ok' : ''">{{ x.active ? t('enabled') : t('disabled') }}</span></td>
          <td class="right"><button class="btn small" @click="editStation(x)">{{ t('edit') }}</button></td>
        </tr></tbody>
      </table>
      <form class="row wrap" @submit.prevent="saveStation">
        <input v-model="st.name" :placeholder="t('name_zh')" required style="flex:1 1 140px" />
        <input v-model="st.name_en" :placeholder="t('name_en')" style="flex:1 1 140px" />
        <label class="check" style="margin:0"><input type="checkbox" v-model="st.active" />{{ t('enabled') }}</label>
        <button class="btn primary">{{ st.id ? t('save') : t('new_station') }}</button>
        <button v-if="st.id" type="button" class="btn" @click="editStation(null)">{{ t('cancel') }}</button>
      </form>
    </div>

    <div v-if="can('staff.view')" id="set-staff" class="ox-panel row between">
      <span><b>{{ t('staff') }}</b> <span class="muted small">{{ t('staff_moved') }}</span></span>
      <router-link class="btn" to="/merchant/staff">{{ t('staff') }}</router-link>
    </div>
  </div>
    </div>
    </div>
  </div>
</template>

<style scoped>
.grid2 { display: grid; grid-template-columns: 1fr 1fr; gap: 0 12px; }
.grid2.checks { gap: 4px 12px; margin-bottom: 12px; }
.setpage h3 { margin: 14px 0 8px; padding-top: 12px; border-top: 1px solid var(--line); font-size: 15px; color: #2f1810; }
.setpage .ox-panel-h + h3 { border-top: 0; padding-top: 0; margin-top: 0; }
.setpage .ox-panel h2 { font-size: 18px; color: #2f1810; }
.setmain > [id] { scroll-margin-top: 16px; }
.setmain :deep(.card) { background: var(--card); border-color: var(--line); }
.setfoot { display: flex; justify-content: flex-end; }
.linkrow { display: flex; flex-wrap: wrap; gap: 10px; align-items: center; } .linkrow a { overflow-wrap: anywhere; font-weight: 600; }
.contact-fields { border: 1px solid var(--line); border-radius: 10px; padding: 10px 12px 2px; margin: 0 0 12px; }
.contact-fields legend { font-weight: 600; padding: 0 6px; }
.contact-fields .row { gap: 4px 22px; margin-top: 8px; }
</style>
