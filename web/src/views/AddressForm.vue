<script setup lang="ts">
// Adding or changing a delivery address. On a phone the map opens over the
// form; on a wide screen it stands beside it.
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { ApiError } from '../api'
import { deleteAddress, deletePhoto, photoUrl, saveAddress, savePhoto, termsText, type Address, type AddressInput, type DeliveryTerms } from '../addresses'
import { cart } from '../cart'
import { customer } from '../customer'
import { t } from '../i18n'
import { mapsKey } from '../maps'
import { shrink } from '../photo'
import PlacePicker, { type Picked } from './PlacePicker.vue'

const props = defineProps<{
  address?: Address | null; slug?: string; currency?: string
  shop?: { lat: number; lng: number; name: string } | null
}>()
const emit = defineEmits<{ saved: [a: Address]; deleted: [id: number]; close: [] }>()

const local = (p: string) => p.replace(/^\+63/, '')
const a = props.address
const f = reactive<AddressInput>({
  contact_name: a?.contact_name ?? '', title: a?.title ?? 'mr', phone: local(a?.phone ?? customer.phone),
  place_name: a?.place_name ?? '', place_address: a?.place_address ?? '', lat: a?.lat ?? null, lng: a?.lng ?? null,
  unit: a?.unit ?? '', note: a?.note ?? '', wechat: a?.wechat ?? '', telegram: a?.telegram ?? '',
  label: a?.label ?? '', is_default: a?.is_default ?? false,
})
const terms = ref<DeliveryTerms | null>(a?.delivery ?? null)
// What was typed by hand on this device before there were accounts is a start for the first address.
if (!a) {
  f.contact_name = cart.name; f.unit = cart.addressUnit; f.note = cart.addressNote
}
// A new address starts from the spot looked up on the store page, if any.
const start = computed(() => f.lat != null && f.lng != null ? { lat: f.lat, lng: f.lng }
  : !a && cart.lat != null && cart.lng != null ? { lat: cart.lat, lng: cart.lng } : null)

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
const phoneOk = computed(() => /^9\d{9}$/.test(f.phone.replace(/[^0-9]/g, '').replace(/^63(?=9\d{9}$)/, '').replace(/^0/, '')))
function problem(): string {
  if (!f.contact_name.trim()) return t('ad.e.name')
  if (!phoneOk.value) return t('ad.e.phone')
  if (!f.place_name.trim()) return t('ad.e.place')
  if (!f.unit.trim()) return t('ad.e.unit')
  return ''
}
async function save() {
  error.value = problem()
  if (error.value || busy.value) return
  busy.value = true
  try {
    let saved = await saveAddress(a?.id ?? 0, { ...f })
    try {
      if (photo.fresh) saved = { ...saved, ...(await savePhoto(saved.id, photo.fresh)) }
      else if (photo.removed && a?.photo_at) { await deletePhoto(saved.id); saved.photo_at = null }
    } catch { alert(t('ad.e.photo')) }
    emit('saved', { ...saved, delivery: f.lat != null ? terms.value : saved.delivery })
  } catch (e) {
    error.value = e instanceof ApiError
      ? e.code === 'ADDRESS_LIMIT' ? t('ad.full', { n: e.details?.limit ?? 20 }) : e.code === 'PHONE_INVALID' ? t('ad.e.phone') : e.code === 'NETWORK' ? t('offline') : e.message
      : String(e)
  } finally { busy.value = false }
}
async function remove() {
  if (!a || busy.value || !confirm(t('ad.delete_confirm'))) return
  busy.value = true
  try { await deleteAddress(a.id); emit('deleted', a.id) } catch (e) { error.value = (e as Error).message } finally { busy.value = false }
}
const labels = ['work', 'home', 'school'] as const
</script>

<template>
  <div class="aform" :class="{ picking, wide: !!hasMap }" data-testid="address-form">
    <div class="af-main">
      <div class="af-head"><button class="af-back" :aria-label="t('back')" data-testid="address-back" @click="emit('close')">‹</button><b>{{ t(a ? 'ad.edit' : 'ad.new') }}</b></div>
      <div class="af-body">
        <div class="af-card">
          <div class="af-row">
            <span class="l">{{ t('ad.f.name') }}</span>
            <div class="grow">
              <input v-model="f.contact_name" maxlength="80" autocomplete="name" :placeholder="t('ad.f.name_ph')" data-testid="af-name" />
              <div class="af-titles">
                <label><input v-model="f.title" type="radio" value="mr" />{{ t('ad.mr') }}</label>
                <label><input v-model="f.title" type="radio" value="ms" />{{ t('ad.ms') }}</label>
              </div>
            </div>
          </div>
          <div class="af-row">
            <span class="l">{{ t('ad.f.phone') }}</span>
            <div class="grow">
              <div class="af-phone"><b>+63</b><input v-model="f.phone" type="tel" inputmode="tel" maxlength="16" autocomplete="tel-national" :placeholder="t('ad.f.phone_ph')" data-testid="af-phone" /></div>
              <div class="hint">{{ t('ad.f.phone_hint') }}</div>
            </div>
          </div>
          <div class="af-row" v-if="hasMap">
            <span class="l">{{ t('ad.f.place') }}</span>
            <button type="button" class="grow af-place" data-testid="af-place" @click="picking = true">
              <template v-if="f.place_name">
                <b>{{ f.place_name }}</b>
                <span class="small muted" v-if="f.place_address">{{ f.place_address }}</span>
                <span v-if="terms" class="tag" :class="terms.deliverable ? 'ok' : 'bad'" data-testid="af-terms">{{ termsText(terms, currency) }}</span>
              </template>
              <span v-else class="ph">{{ t('ad.f.place_ph') }}</span>
            </button>
            <span class="chev">›</span>
          </div>
          <div class="af-row" v-else-if="hasMap === false">
            <span class="l">{{ t('ad.f.place') }}</span>
            <div class="grow"><input v-model="f.place_name" maxlength="200" autocomplete="street-address" :placeholder="t('ad.f.street_ph')" data-testid="af-street" /></div>
          </div>
          <div class="af-row"><span class="l">{{ t('ad.f.unit') }}</span>
            <div class="grow"><input v-model="f.unit" maxlength="100" :placeholder="t('ad.f.unit_ph')" data-testid="af-unit" /></div></div>
          <div class="af-row"><span class="l">{{ t('ad.f.note') }}</span>
            <div class="grow"><input v-model="f.note" maxlength="300" :placeholder="t('ad.f.note_ph')" data-testid="af-note" /></div></div>
          <div class="af-row"><span class="l">{{ t('ad.f.wechat') }}</span>
            <div class="grow"><input v-model="f.wechat" maxlength="64" autocapitalize="none" :placeholder="t('ad.f.wechat_ph')" data-testid="af-wechat" /></div></div>
          <div class="af-row"><span class="l">{{ t('ad.f.telegram') }}</span>
            <div class="grow"><input v-model="f.telegram" maxlength="64" autocapitalize="none" :placeholder="t('ad.f.optional')" /></div></div>
          <div class="af-row"><span class="l">{{ t('ad.f.photo') }}</span>
            <div class="grow af-photo" data-testid="af-photo">
              <img v-if="photo.url" :src="photo.url" alt="" data-testid="af-photo-img" />
              <div class="acts">
                <label class="btn small">{{ t(photo.url ? 'ad.f.photo_change' : 'ad.f.photo_add') }}
                  <input type="file" accept="image/*" name="door" hidden @change="choosePhoto" /></label>
                <button v-if="photo.url" type="button" class="btn small link" data-testid="af-photo-remove" @click="removePhoto">{{ t('ad.f.photo_remove') }}</button>
              </div>
              <div class="hint" v-if="!photo.url">{{ t('ad.f.photo_hint') }}</div>
            </div></div>
          <div class="af-row"><span class="l">{{ t('ad.f.label') }}</span>
            <div class="grow af-chips">
              <button v-for="l in labels" :key="l" type="button" :class="{ on: f.label === l }" @click="f.label = f.label === l ? '' : l">{{ t('ad.label_' + l) }}</button>
            </div></div>
        </div>
        <label class="af-card af-default"><span>{{ t('ad.f.default') }}</span><input v-model="f.is_default" type="checkbox" class="switch" data-testid="af-default" /></label>
        <div v-if="error" class="banner bad" data-testid="af-error">{{ error }}</div>
        <button type="button" class="btn primary block big" :disabled="busy" data-testid="af-save" @click="save">{{ busy ? t('ad.saving') : t('ad.save') }}</button>
        <button v-if="a" type="button" class="btn link danger" :disabled="busy" data-testid="af-delete" @click="remove">{{ t('ad.delete') }}</button>
      </div>
    </div>
    <div class="af-map" v-if="hasMap">
      <PlacePicker :slug="slug" :currency="currency" :start="start" :name="f.place_name" :shop="shop" keepable @done="picked" @close="picking = false" />
    </div>
  </div>
</template>

<style scoped>
.af-photo img { display: block; width: 120px; height: 90px; object-fit: cover; border-radius: 10px; margin-bottom: 8px; }
.af-photo .acts { display: flex; gap: 8px; align-items: center; }
.af-photo .hint { font-size: 12px; color: #8b919a; margin-top: 6px; }
.aform { display: flex; height: 100%; min-height: 0; position: relative; background: var(--bg); }
.af-main { flex: 1; min-width: 0; display: flex; flex-direction: column; min-height: 0; }
.af-head { display: flex; align-items: center; padding: 12px 14px; background: #fff; border-bottom: 1px solid var(--line); font-size: 17px; flex: none; }
.af-head b { flex: 1; text-align: center; margin-right: 34px; }
.af-back { border: 0; background: none; font-size: 28px; line-height: 1; width: 34px; cursor: pointer; color: var(--ink); }
.af-body { flex: 1; overflow-y: auto; padding: 12px; display: grid; gap: 12px; align-content: start; }
.af-card { background: #fff; border-radius: 14px; padding: 2px 14px; }
.af-row { display: flex; align-items: flex-start; gap: 6px; padding: 12px 0; border-bottom: 1px solid #f0f1f3; }
.af-row:last-child { border-bottom: 0; }
.af-row .l { width: 76px; flex: none; padding-top: 2px; }
.af-row .grow { flex: 1; min-width: 0; }
.af-row input:not([type=radio]) { border: 0; padding: 2px 0; width: 100%; outline: none !important; background: none; font-size: 15px; }
.af-titles { display: flex; gap: 18px; margin-top: 8px; font-size: 14px; }
.af-titles label { display: flex; align-items: center; gap: 6px; }
.af-phone { display: flex; gap: 8px; align-items: center; }
.hint { font-size: 12px; color: var(--muted); margin-top: 4px; }
.af-place { border: 0; background: none; text-align: left; padding: 2px 0; cursor: pointer; font: inherit; color: inherit; display: grid; gap: 3px; justify-items: start; }
.af-place .ph { color: #a3a9b2; }
.chev { color: #9aa1ab; font-size: 20px; }
.af-chips button { border: 1px solid #d6dae0; border-radius: 8px; padding: 3px 14px; font-size: 14px; margin-right: 8px; background: #fff; cursor: pointer; }
.af-chips button.on { border-color: var(--brand); background: var(--brand-soft); color: var(--brand-dark); font-weight: 600; }
.af-default { display: flex; align-items: center; justify-content: space-between; padding: 14px; }
.switch { appearance: none; width: 46px; height: 26px; border-radius: 13px; background: #d6dae0; position: relative; cursor: pointer; flex: none; border: 0; padding: 0; }
.switch::after { content: ""; position: absolute; top: 3px; left: 3px; width: 20px; height: 20px; border-radius: 50%; background: #fff; transition: left .15s; }
.switch:checked { background: var(--brand); } .switch:checked::after { left: 23px; }
.btn.danger { color: var(--bad); justify-self: center; }
/* the map: over the form on a phone, beside it on a wide screen */
.af-map { display: none; position: absolute; inset: 0; z-index: 3; background: #fff; }
.aform.picking .af-map { display: block; }
@media (min-width: 900px) {
  .aform.wide .af-main { flex: 0 0 400px; border-right: 1px solid var(--line); }
  .aform.wide .af-map { display: block; position: static; flex: 1; min-width: 0; }
  .aform.wide .af-map :deep(.pp-head) { display: none; }
  .aform.wide .chev { display: none; }
}
</style>
