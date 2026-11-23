<script setup lang="ts">
// Adding or changing a delivery address. On a phone the map opens over the
// form; on a wide screen it stands beside it.
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { ApiError } from '../api'
import { CONTACT_FIELDS, deleteAddress, deletePhoto, photoUrl, saveAddress, savePhoto, termsText, type Address, type AddressInput, type DeliveryTerms } from '../addresses'
import { cart } from '../cart'
import { customer } from '../customer'
import { DIALS, dialOf, e164, phoneOk as validPhone, splitPhone } from '../dial'
import { t } from '../i18n'
import { mallSpot } from '../mall'
import { mapsKey } from '../maps'
import { shrink } from '../photo'
import PlacePicker, { type Picked } from './PlacePicker.vue'

const props = defineProps<{
  address?: Address | null; slug?: string; currency?: string
  shop?: { lat: number; lng: number; name: string; country?: string } | null
  // The store this is opened from: its country (ISO code) sets the telephone
  // code a new number starts with, and fields are the contact names it asks for.
  country?: string; fields?: string[]
}>()
const emit = defineEmits<{ saved: [a: Address]; deleted: [id: number]; close: [] }>()

const a = props.address
// A saved number keeps the country code it was saved with. A new address
// starts on the code of the store's country, with the login number when that
// is a number of this country.
const storeDial = dialOf(props.country)
const had = splitPhone(a?.phone ?? customer.phone)
const dial = ref(a && had ? had.dial : storeDial)
const prefilled = !a && had?.dial === storeDial
const asked = computed(() => CONTACT_FIELDS.filter(x => props.fields?.includes(x)))
const f = reactive<AddressInput>({
  contact_name: a?.contact_name ?? '', title: a?.title ?? 'mr', phone: had && (a || prefilled) ? had.local : '',
  place_name: a?.place_name ?? '', place_address: a?.place_address ?? '', lat: a?.lat ?? null, lng: a?.lng ?? null,
  unit: a?.unit ?? '', note: a?.note ?? '', wechat: a?.wechat ?? '', telegram: a?.telegram ?? '', line: a?.line ?? '', whatsapp: a?.whatsapp ?? '',
  label: a?.label ?? '', is_default: a?.is_default ?? false,
})
const terms = ref<DeliveryTerms | null>(a?.delivery ?? null)
// What was typed by hand on this device before there were accounts is a start for the first address.
if (!a) {
  f.contact_name = cart.name; f.unit = cart.addressUnit; f.note = cart.addressNote
}
// A new address starts from the spot looked up on the store page, if any,
// else from where the marketplace found the customer.
const start = computed(() => f.lat != null && f.lng != null ? { lat: f.lat, lng: f.lng }
  : !a && cart.lat != null && cart.lng != null ? { lat: cart.lat, lng: cart.lng } : !a ? mallSpot() : null)

// null until it is known whether there is a map to pick on
const hasMap = ref<boolean | null>(null)
const picking = ref(false)
onMounted(async () => { hasMap.value = !!(await mapsKey()) })

function picked(p: Picked, d: DeliveryTerms | null) {
  Object.assign(f, { lat: p.lat, lng: p.lng, place_name: p.name, place_address: p.address })
  terms.value = d
  picking.value = false
}

// A photo of the door (optional). It is sent after the address is saved,
// because a new address has no id before that.
const photo = reactive<{ fresh: Blob | null; url: string; removed: boolean }>({ fresh: null, url: a?.photo_at ? photoUrl(a) : '', removed: false })
async function choosePhoto(e: Event) {
  const input = e.target as HTMLInputElement, file = input.files?.[0]
  input.value = ''
  if (!file) return
  try {
    const small = await shrink(file)
    if (photo.fresh) URL.revokeObjectURL(photo.url)
    Object.assign(photo, { fresh: small, url: URL.createObjectURL(small), removed: false })
  } catch { error.value = t('ad.e.photo') }
}
function removePhoto() {
  if (photo.fresh) URL.revokeObjectURL(photo.url)
  Object.assign(photo, { fresh: null, url: '', removed: true })
}
onBeforeUnmount(() => { if (photo.fresh) URL.revokeObjectURL(photo.url) })

const error = ref(''), busy = ref(false)
const phoneOk = computed(() => validPhone(dial.value, f.phone))
const phoneError = () => t(dial.value === '63' ? 'ad.e.phone' : 'ad.e.phone_any')
function problem(): string {
  if (!f.place_name.trim()) return t('ad.e.place')
  if (!f.unit.trim()) return t('ad.e.unit')
  if (!f.contact_name.trim()) return t('ad.e.name')
  if (!phoneOk.value) return phoneError()
  return ''
}
async function save() {
  error.value = problem()
  if (error.value || busy.value) return
  busy.value = true
  try {
    let saved = await saveAddress(a?.id ?? 0, { ...f, phone: e164(dial.value, f.phone) })
    try {
      if (photo.fresh) saved = { ...saved, ...(await savePhoto(saved.id, photo.fresh)) }
      else if (photo.removed && a?.photo_at) { await deletePhoto(saved.id); saved.photo_at = null }
    } catch { alert(t('ad.e.photo')) }
    emit('saved', { ...saved, delivery: f.lat != null ? terms.value : saved.delivery })
  } catch (e) {
    error.value = e instanceof ApiError
      ? e.code === 'ADDRESS_LIMIT' ? t('ad.full', { n: e.details?.limit ?? 20 }) : e.code === 'PHONE_INVALID' ? phoneError() : e.code === 'NETWORK' ? t('offline') : e.message
      : String(e)
  } finally { busy.value = false }
}
async function remove() {
  if (!a || busy.value || !confirm(t('ad.delete_confirm'))) return
  busy.value = true
  try { await deleteAddress(a.id); emit('deleted', a.id) } catch (e) { error.value = (e as Error).message } finally { busy.value = false }
}
const labels = ['work', 'home', 'school'] as const
const titles = ['mr', 'ms'] as const
</script>

<template>
  <div class="aform" :class="{ picking, wide: !!hasMap }" data-testid="address-form">
    <div class="af-main">
      <div class="af-head">
        <button class="af-back" :aria-label="t('back')" data-testid="address-back" @click="emit('close')">
          <svg viewBox="0 0 24 24" width="24" height="24" aria-hidden="true"><path d="M15 5l-7 7 7 7" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" /></svg>
        </button>
        <b>{{ t(a ? 'ad.edit' : 'ad.new') }}</b>
      </div>
      <div class="af-body">
        <h3>{{ t('ad.s.place') }}</h3>
        <div class="af-card">
          <button v-if="hasMap" type="button" class="af-place" data-testid="af-place" @click="picking = true">
            <span class="pin"><svg viewBox="0 0 24 24" width="22" height="22" aria-hidden="true"><path d="M12 21s-6.5-6.1-6.5-11a6.5 6.5 0 0113 0c0 4.9-6.5 11-6.5 11z" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linejoin="round" /><circle cx="12" cy="10" r="2.3" fill="none" stroke="currentColor" stroke-width="1.7" /></svg></span>
            <span class="txt">
              <span class="cap">{{ t('ad.f.place') }}<i class="req">*</i></span>
              <template v-if="f.place_name">
                <b>{{ f.place_name }}</b>
                <span class="sub" v-if="f.place_address">{{ f.place_address }}</span>
                <!-- only a warning: a spot the store delivers to needs no label -->
                <span v-if="terms && !terms.deliverable" class="tag bad" data-testid="af-terms">{{ termsText(terms, currency) }}</span>
              </template>
              <span v-else class="sub">{{ t('ad.f.place_ph') }}</span>
            </span>
            <svg class="chev" viewBox="0 0 24 24" width="20" height="20" aria-hidden="true"><path d="M9 5l7 7-7 7" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" /></svg>
          </button>
          <label class="af-row" v-else-if="hasMap === false">
            <span class="l">{{ t('ad.f.place') }}<i class="req">*</i></span>
            <span class="v"><input v-model="f.place_name" maxlength="200" autocomplete="street-address" :placeholder="t('ad.f.street_ph')" data-testid="af-street" /></span>
          </label>
          <label class="af-row">
            <span class="l">{{ t('ad.f.unit') }}<i class="req">*</i></span>
            <span class="v"><input v-model="f.unit" maxlength="100" :placeholder="t('ad.f.unit_ph')" data-testid="af-unit" /></span>
          </label>
        </div>

        <h3>{{ t('ad.s.contact') }}</h3>
        <div class="af-card">
          <div class="af-row">
            <label class="l" for="af-name">{{ t('ad.f.name') }}<i class="req">*</i></label>
            <span class="v who">
              <input id="af-name" v-model="f.contact_name" maxlength="80" autocomplete="name" :placeholder="t('ad.f.name_ph')" data-testid="af-name" />
              <span class="af-titles">
                <label v-for="x in titles" :key="x" class="pill" :class="{ on: f.title === x }"><input v-model="f.title" type="radio" name="af-title" :value="x" /><i></i>{{ t('ad.' + x) }}</label>
              </span>
            </span>
          </div>
          <div class="af-row">
            <label class="l" for="af-phone">{{ t('ad.f.phone') }}<i class="req">*</i></label>
            <span class="v">
              <span class="af-phone">
                <select v-model="dial" :aria-label="t('ad.f.dial')" data-testid="af-dial"><option v-for="d in DIALS" :key="d" :value="d">+{{ d }}</option></select>
                <input id="af-phone" v-model="f.phone" type="tel" inputmode="tel" maxlength="18" autocomplete="tel-national" :placeholder="dial === '63' ? '9XX XXX XXXX' : t('ad.f.phone_ph')" data-testid="af-phone" />
              </span>
              <span class="hint" v-if="prefilled">{{ t('ad.f.phone_hint') }}</span>
            </span>
          </div>
        </div>

        <h3>{{ t('ad.s.extra') }}<small>{{ t('ad.s.optional') }}</small></h3>
        <div class="af-card">
          <label class="af-row">
            <span class="l">{{ t('ad.f.note') }}</span>
            <span class="v"><input v-model="f.note" maxlength="300" :placeholder="t('ad.f.note_ph')" data-testid="af-note" /></span>
          </label>
          <label v-for="x in asked" :key="x" class="af-row">
            <span class="l">{{ t('ad.f.' + x) }}</span>
            <span class="v"><input v-model="f[x]" maxlength="64" autocapitalize="none" autocorrect="off" spellcheck="false" :inputmode="x === 'whatsapp' ? 'tel' : undefined"
              :placeholder="t(x === 'wechat' ? 'ad.f.wechat_ph' : x === 'whatsapp' ? 'ad.f.whatsapp_ph' : 'ad.f.account_ph')" :data-testid="'af-' + x" /></span>
          </label>
          <div class="af-row">
            <span class="l">{{ t('ad.f.photo') }}</span>
            <div class="v af-photo" data-testid="af-photo">
              <img v-if="photo.url" :src="photo.url" alt="" data-testid="af-photo-img" />
              <div class="acts">
                <label class="shoot">
                  <svg viewBox="0 0 24 24" width="20" height="20" aria-hidden="true"><path d="M4 8h3l1.5-2h7L17 8h3v11H4z" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round" /><circle cx="12" cy="13" r="3.2" fill="none" stroke="currentColor" stroke-width="1.6" /></svg>
                  {{ t(photo.url ? 'ad.f.photo_change' : 'ad.f.photo_add') }}
                  <input type="file" accept="image/*" name="door" hidden @change="choosePhoto" /></label>
                <button v-if="photo.url" type="button" class="btn small link" data-testid="af-photo-remove" @click="removePhoto">{{ t('ad.f.photo_remove') }}</button>
              </div>
              <div class="hint" v-if="!photo.url">{{ t('ad.f.photo_hint') }}</div>
            </div>
          </div>
        </div>

        <div class="af-card">
          <div class="af-row">
            <span class="l">{{ t('ad.f.label') }}</span>
            <div class="v af-chips">
              <button v-for="l in labels" :key="l" type="button" :class="{ on: f.label === l }" :aria-pressed="f.label === l" @click="f.label = f.label === l ? '' : l">{{ t('ad.label_' + l) }}</button>
            </div>
          </div>
          <label class="af-row af-default"><span>{{ t('ad.f.default') }}</span><input v-model="f.is_default" type="checkbox" class="switch" data-testid="af-default" /></label>
        </div>
        <button v-if="a" type="button" class="btn link danger" :disabled="busy" data-testid="af-delete" @click="remove">{{ t('ad.delete') }}</button>
      </div>
      <div class="af-foot">
        <div v-if="error" class="banner bad" data-testid="af-error">{{ error }}</div>
        <button type="button" class="af-save" :disabled="busy" data-testid="af-save" @click="save">{{ busy ? t('ad.saving') : t('ad.save') }}</button>
      </div>
    </div>
    <div class="af-map" v-if="hasMap">
      <PlacePicker :slug="slug" :currency="currency" :start="start" :name="f.place_name" :shop="shop" keepable @done="picked" @close="picking = false" />
    </div>
  </div>
</template>

<style scoped>
/* warm paper tones around the store's own accent colour */
.aform { --af-bg: #f7f4ef; --af-card: #fffdfa; --af-line: #e8e1d6; --af-ink: #33241a; --af-soft: #8a7b6d;
  --af-tint: color-mix(in srgb, var(--brand) 8%, #f9ecd6);
  display: flex; height: 100%; min-height: 0; position: relative; background: var(--af-bg); color: var(--af-ink); }
.af-main { flex: 1; min-width: 0; display: flex; flex-direction: column; min-height: 0; }
.af-head { display: flex; align-items: center; padding: 14px 12px 12px; margin: 0 4px; border-bottom: 1px solid var(--af-line); flex: none; }
.af-head b { flex: 1; text-align: center; margin-inline-end: 40px; font-size: 19px; letter-spacing: .04em; }
.af-back { border: 0; background: none; width: 40px; height: 36px; display: grid; place-items: center; cursor: pointer; color: var(--af-ink); padding: 0; border-radius: 8px; }
.af-body { flex: 1; overflow-y: auto; padding: 4px 12px 16px; display: grid; gap: 10px; align-content: start; }
.af-body h3 { font-size: 16px; margin: 10px 4px 0; }
.af-body h3 small { font-size: 13px; font-weight: 400; color: var(--af-soft); margin-inline-start: 8px; }
.af-card { background: var(--af-card); border: 1px solid var(--af-line); border-radius: 14px; padding: 0 12px; }
.af-row { display: flex; align-items: flex-start; padding: 12px 2px; border-top: 1px solid var(--af-line); }
.af-card > .af-row:first-child { border-top: 0; }
.af-row .l { width: 88px; flex: none; font-weight: 600; padding: 4px 6px 4px 0; overflow-wrap: anywhere; }
.req { color: var(--bad); font-style: normal; margin-inline-start: 4px; }
.af-row .v { flex: 1; min-width: 0; border-inline-start: 1px solid var(--af-line); padding-inline-start: 14px; display: block; }
.af-row input:not([type=radio], [type=checkbox], [type=file]) { border: 0; padding: 4px 0; width: 100%; outline: none !important; background: none; font-size: 15px; border-radius: 0; }
.af-row input::placeholder { color: #a99d91; }
.af-row:focus-within .l { color: var(--brand-dark); }
.hint { display: block; font-size: 12px; color: var(--af-soft); margin-top: 4px; }

/* the place picked on the map */
.af-place { display: flex; align-items: center; gap: 12px; width: 100%; margin: 10px 0; padding: 12px; border: 0; border-radius: 10px; background: var(--af-tint);
  text-align: start; cursor: pointer; font: inherit; color: inherit; }
.af-place .pin { width: 40px; height: 40px; border-radius: 50%; flex: none; display: grid; place-items: center; color: var(--brand-dark);
  background: color-mix(in srgb, var(--brand) 14%, #f3dfc0); }
.af-place .txt { flex: 1; min-width: 0; display: grid; gap: 3px; justify-items: start; }
.af-place .cap { font-weight: 600; }
.af-place .txt b { font-size: 16px; overflow-wrap: anywhere; }
.af-place .sub { font-size: 13px; color: var(--af-soft); overflow-wrap: anywhere; }
.af-place .txt:has(b) .cap { font-size: 12px; font-weight: 400; color: var(--af-soft); }
.af-place .txt:has(b) .req { display: none; }
.chev { color: var(--af-soft); flex: none; }

/* name with Mr / Ms */
.who { display: flex !important; flex-wrap: wrap; align-items: center; gap: 6px 8px; }
.who > input { flex: 1 1 60px; min-width: 60px; }
.af-titles { display: flex; gap: 6px; flex: none; }
.pill { display: flex; align-items: center; gap: 6px; padding: 4px 10px 4px 8px; border: 1px solid var(--af-line); border-radius: 999px; font-size: 14px; cursor: pointer; background: #fff; white-space: nowrap; }
.pill input { position: absolute; opacity: 0; width: 1px; height: 1px; }
.pill i { width: 16px; height: 16px; border-radius: 50%; border: 1.500px solid #b9ad9f; display: grid; place-items: center; flex: none; }
.pill.on { background: var(--af-tint); border-color: transparent; color: var(--brand-dark); font-weight: 600; }
.pill.on i { border-color: var(--brand-dark); }
.pill.on i::after { content: ""; width: 8px; height: 8px; border-radius: 50%; background: var(--brand-dark); }
.pill:has(input:focus-visible) { outline: 2px solid var(--brand); outline-offset: 1px; }

/* telephone: the country code, then the number */
.af-phone { display: flex; align-items: center; }
.af-phone select { width: auto; flex: none; border: 0; border-inline-end: 1px solid var(--af-line); border-radius: 0; background: none; padding: 4px 22px 4px 0; margin-inline-end: 14px;
  font-weight: 600; font-size: 15px; cursor: pointer; appearance: none; -webkit-appearance: none;
  background: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='10' height='6'%3E%3Cpath d='M1 1l4 4 4-4' fill='none' stroke='%238a7b6d' stroke-width='1.5' stroke-linecap='round'/%3E%3C/svg%3E") no-repeat right 8px center; }
.af-phone select:focus { outline: none; }
.af-phone select:focus-visible { outline: 2px solid var(--brand); outline-offset: 2px; border-radius: 4px; }

/* photo of the door */
.af-photo img { display: block; width: 120px; height: 90px; object-fit: cover; border-radius: 10px; margin-bottom: 8px; }
.af-photo .acts { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
.shoot { display: inline-flex; align-items: center; gap: 8px; padding: 6px 14px; border: 1px solid #cfc4b6; border-radius: 9px; background: #fff; cursor: pointer; font-size: 14px; color: var(--af-ink); }
.shoot svg { color: var(--brand-dark); }

.af-row .af-chips { display: flex; flex-wrap: wrap; gap: 8px; }
.af-chips button { border: 1px solid #cfc4b6; border-radius: 9px; padding: 4px 16px; font-size: 14px; background: #fff; cursor: pointer; color: var(--af-ink); }
.af-chips button.on { border-color: var(--brand); background: var(--af-tint); color: var(--brand-dark); font-weight: 600; }
.af-default { align-items: center; justify-content: space-between; font-weight: 600; cursor: pointer; }
.switch { appearance: none; -webkit-appearance: none; width: 46px; height: 26px; border-radius: 13px; background: #d9d2c8; position: relative; cursor: pointer; flex: none; border: 0; padding: 0; }
.switch::after { content: ""; position: absolute; top: 3px; inset-inline-start: 3px; width: 20px; height: 20px; border-radius: 50%; background: #fff; transition: left .15s; box-shadow: 0 1px 2px rgba(0,0,0,.2); }
.switch:checked { background: var(--brand); } .switch:checked::after { inset-inline-start: 23px; }
.btn.danger { color: var(--bad); justify-self: center; }

.af-foot { flex: none; padding: 12px 12px calc(12px + env(safe-area-inset-bottom)); border-top: 1px solid var(--af-line); margin: 0 4px; display: grid; gap: 10px; }
.af-save { width: 100%; min-height: 52px; border: 0; border-radius: 12px; background: var(--brand); color: #fff; font-size: 18px; font-weight: 700; letter-spacing: .12em; cursor: pointer; }
.af-save:hover:not(:disabled) { background: var(--brand-dark); }
.af-save:disabled { opacity: .55; cursor: not-allowed; }
.af-save:focus-visible, .af-back:focus-visible, .af-place:focus-visible, .af-chips button:focus-visible { outline: 2px solid var(--brand); outline-offset: 2px; }

/* the map: over the form on a phone, beside it on a wide screen */
.af-map { display: none; position: absolute; inset: 0; z-index: 3; background: #fff; }
.aform.picking .af-map { display: block; }
@media (min-width: 900px) {
  .aform.wide .af-main { flex: 0 0 420px; border-inline-end: 1px solid var(--af-line); }
  .aform.wide .af-map { display: block; position: static; flex: 1; min-width: 0; }
  .aform.wide .af-map :deep(.pp-head) { display: none; }
  .aform.wide .chev { display: none; }
}
</style>
