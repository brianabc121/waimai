<script setup lang="ts">
// The address book as a sheet: choose an address for this order, or (manage)
// just keep the book in order. Each address says whether this store delivers
// to it; one it does not deliver to cannot be chosen.
import { computed, onMounted, ref } from 'vue'
import { ApiError } from '../api'
import { listAddresses, sorted, termsText, usable, whoLine, type Address } from '../addresses'
import { t } from '../i18n'
import AddressForm from './AddressForm.vue'

const props = defineProps<{
  slug?: string; currency?: string; selected?: number | null; manage?: boolean
  shop?: { lat: number; lng: number; name: string; country?: string } | null
  // the store's country (for the telephone code) and the contact names it asks for
  country?: string; fields?: string[]
  // open straight on the form for a new address
  startNew?: boolean
  // the address book as a page of its own ("me" → addresses) rather than a sheet;
  // the form still opens as a sheet over it
  page?: boolean
}>()
const emit = defineEmits<{ select: [a: Address]; close: []; changed: [list: Address[]]; limit: [n: number] }>()

const list = ref<Address[] | null>(null)
const limit = ref(20)
const error = ref('')
const editing = ref<Address | null | undefined>(props.startNew ? null : undefined)   // undefined: the list; null: a new address

async function load() {
  error.value = ''
  try {
    const r = await listAddresses(props.slug)
    list.value = sorted(r.addresses); limit.value = r.limit
    emit('changed', list.value); emit('limit', r.limit)
  } catch (e) { error.value = e instanceof ApiError && e.code === 'NETWORK' ? t('offline') : (e as Error).message }
}
onMounted(load)

const full = computed(() => (list.value?.length ?? 0) >= limit.value)
function choose(a: Address) { if (!props.manage && usable(a)) emit('select', a) }
async function saved(a: Address) {
  editing.value = undefined
  await load()
  // A new or changed address that can be delivered to is what the customer wants now.
  const fresh = list.value?.find(x => x.id === a.id)
  if (fresh && !props.manage && usable(fresh)) emit('select', fresh)
}
async function deleted() { editing.value = undefined; await load() }
function closeForm() { if (props.startNew && !list.value?.length) emit('close'); else editing.value = undefined }
</script>

<template>
  <div v-if="page" class="as-page" data-testid="address-sheet">
    <div v-if="error" class="banner bad">{{ error }} <button class="btn link" @click="load">{{ t('retry') }}</button></div>
    <div v-else-if="!list" class="card empty">{{ t('loading') }}</div>
    <div v-else-if="!list.length" class="card empty" data-testid="address-empty">{{ t('ad.empty') }}</div>
    <div v-for="a in list ?? []" :key="a.id" class="card ap-row" data-testid="address-row">
      <div class="ap-main">
        <span class="ap-pin"><svg viewBox="0 0 24 24" width="24" height="24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M12 13a2.6 2.6 0 1 0 0-5.2 2.6 2.6 0 0 0 0 5.2zM12 21s6.5-5.8 6.5-10.6a6.5 6.5 0 0 0-13 0C5.5 15.2 12 21 12 21z" /></svg></span>
        <div class="grow">
          <div class="t">{{ a.place_name }}<span v-if="a.is_default" class="tg def" data-testid="address-default">{{ t('ad.default_tag') }}</span><span v-if="a.label" class="tg lab">{{ t('ad.label_' + a.label) }}</span></div>
          <div class="u">{{ t('ad.unit_is', { unit: a.unit }) }}</div>
          <div class="w">{{ whoLine(a) }}</div>
        </div>
      </div>
      <div class="ap-bar">
        <span v-if="a.photo_at" class="ap-photo" data-testid="address-has-photo">
          <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M4 8h3l2-2.5h6L17 8h3v11H4z" /><circle cx="12" cy="13" r="3.2" /></svg>{{ t('ad.f.photo') }}</span>
        <button class="ap-edit" :aria-label="t('ad.edit')" data-testid="address-edit" @click="editing = a">
          <svg viewBox="0 0 24 24" width="17" height="17" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M4 20h4L19 9l-4-4L4 16z" /><path d="m13.5 6.5 4 4" /></svg>{{ t('ad.edit_short') }}</button>
      </div>
    </div>
    <div class="ap-foot">
      <div v-if="full" class="note" style="margin-bottom:8px;text-align:center">{{ t('ad.full', { n: limit }) }}</div>
      <button class="btn primary block big ap-new" :disabled="full" data-testid="address-new" @click="editing = null">
        <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round"><path d="M12 5v14M5 12h14" /></svg>{{ t('ad.new') }}</button>
    </div>
    <div v-if="editing !== undefined" class="sheet-mask" @click.self="editing = undefined">
      <div class="sheet asheet form">
        <AddressForm :address="editing" :slug="slug" :currency="currency" :shop="shop" :country="country" :fields="fields"
          @saved="saved" @deleted="deleted" @close="closeForm" />
      </div>
    </div>
  </div>
  <div v-else class="sheet-mask" data-testid="address-sheet" @click.self="emit('close')">
    <div class="sheet asheet" :class="{ form: editing !== undefined }">
      <AddressForm v-if="editing !== undefined" :address="editing" :slug="slug" :currency="currency" :shop="shop" :country="country" :fields="fields"
        @saved="saved" @deleted="deleted" @close="closeForm" />
      <template v-else>
        <div class="as-head">{{ t(manage ? 'ad.mine' : 'ad.choose') }}<button :aria-label="t('close')" data-testid="address-close" @click="emit('close')">×</button></div>
        <div class="as-list">
          <div v-if="error" class="banner bad" style="margin:12px">{{ error }} <button class="btn link" @click="load">{{ t('retry') }}</button></div>
          <div v-else-if="!list" class="muted as-empty">{{ t('loading') }}</div>
          <div v-else-if="!list.length" class="muted as-empty" data-testid="address-empty">{{ t('ad.empty') }}</div>
          <div v-for="a in list ?? []" :key="a.id" class="as-row" :class="{ dis: !usable(a), pick: !manage }" data-testid="address-row" :data-usable="usable(a)" @click="choose(a)">
            <span v-if="!manage" class="radio" :class="{ on: selected === a.id, no: !usable(a) }"></span>
            <div class="grow">
              <div class="t"><span v-if="a.label" class="tag brand">{{ t('ad.label_' + a.label) }}</span>{{ a.place_name }}</div>
              <div class="u">{{ t('ad.unit_is', { unit: a.unit }) }}</div>
              <div class="small muted">{{ whoLine(a) }}</div>
              <div v-if="a.delivery" class="st"><span class="tag" :class="a.delivery.deliverable ? 'ok' : 'bad'" data-testid="address-terms">{{ a.lat == null ? t('ad.need_pin') : termsText(a.delivery, currency) }}</span></div>
            </div>
            <button class="edit" :aria-label="t('ad.edit')" data-testid="address-edit" @click.stop="editing = a">
              <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M4 20h4L19 9l-4-4L4 16z" /><path d="m13.5 6.5 4 4" /></svg></button>
          </div>
        </div>
        <div class="as-foot">
          <div v-if="full" class="small muted" style="margin-bottom:8px">{{ t('ad.full', { n: limit }) }}</div>
          <button class="btn primary block big" :disabled="full" data-testid="address-new" @click="editing = null">{{ t('ad.new') }}</button>
        </div>
      </template>
    </div>
  </div>
</template>

<style scoped>
.asheet { height: 78vh; max-height: 78vh; overflow: hidden; }
.asheet.form { height: 94vh; max-height: 94vh; }
.as-head { text-align: center; font-size: 17px; font-weight: 600; padding: 16px 44px 12px; position: relative; flex: none; }
.as-head button { position: absolute; inset-inline-end: 12px; top: 10px; border: 0; background: none; font-size: 26px; color: #8b919a; cursor: pointer; width: 36px; height: 36px; }
.as-list { flex: 1; overflow-y: auto; min-height: 0; }
.as-empty { padding: 40px 16px; text-align: center; }
.as-row { display: flex; gap: 12px; align-items: center; padding: 13px 16px; border-bottom: 1px solid #f0f1f3; }
.as-row.pick:not(.dis) { cursor: pointer; }
.as-row .grow { flex: 1; min-width: 0; }
.as-row .t { font-size: 16px; font-weight: 600; } .as-row .t .tag { margin-inline-end: 6px; font-weight: 400; }
.as-row .u { font-size: 14px; }
.as-row .st { margin-top: 4px; }
.as-row.dis .t, .as-row.dis .u { color: #9aa1ab; }
.radio { width: 22px; height: 22px; border-radius: 50%; border: 1.5px solid #b8bec7; flex: none; display: grid; place-items: center; }
.radio.on { border-color: var(--brand); } .radio.on::after { content: ""; width: 12px; height: 12px; border-radius: 50%; background: var(--brand); }
.radio.no { background: #f0f1f3; border-color: #dde0e4; }
.edit { border: 0; background: none; color: #8b919a; padding: 8px; cursor: pointer; flex: none; }
.as-foot { padding: 12px 14px calc(14px + env(safe-area-inset-bottom)); flex: none; }
/* the page */
.as-page { display: grid; gap: 12px; }
.as-page .ap-row { padding: 0; border: 1px solid var(--line); border-radius: 14px; box-shadow: 0 2px 8px rgba(90, 40, 15, .05); }
.ap-main { display: flex; align-items: flex-start; gap: 14px; padding: 14px 14px 12px; }
.ap-pin { flex: none; width: 44px; height: 44px; border-radius: 50%; background: var(--gold-soft); color: var(--red-deep); display: grid; place-items: center; }
.ap-row .grow { flex: 1; min-width: 0; display: grid; gap: 3px; padding-top: 1px; }
.ap-row .t { font-size: 15px; font-weight: 600; line-height: 1.4; overflow-wrap: anywhere; }
.ap-row .tg { display: inline-block; font-size: 11px; font-weight: 400; line-height: 16px; padding: 0 6px; border-radius: 5px; margin-inline-start: 6px; vertical-align: 2px; white-space: nowrap; }
.ap-row .tg.def { color: var(--red); box-shadow: inset 0 0 0 1px currentColor; }
.ap-row .tg.lab { background: var(--gold-soft); color: var(--red-deep); }
.ap-row .u, .ap-row .w { font-size: 13px; line-height: 1.5; color: var(--muted); overflow-wrap: anywhere; }
.ap-bar { display: flex; align-items: center; gap: 10px; margin: 0 14px; padding: 6px 0; border-top: 1px solid var(--line); min-height: 44px; }
.ap-photo { display: inline-flex; align-items: center; gap: 4px; font-size: 12px; color: var(--muted); min-width: 0; }
.ap-edit { margin-inline-start: auto; flex: none; display: inline-flex; align-items: center; gap: 5px; border: 0; background: none; border-radius: 8px; padding: 6px 2px 6px 8px; color: var(--red); cursor: pointer; font: inherit; font-size: 14px; font-weight: 600; }
.ap-edit:hover { background: rgba(160, 44, 32, .06); }
.ap-new { display: inline-flex; align-items: center; justify-content: center; gap: 8px; border-radius: 999px; min-height: 50px; font-size: 16px; box-shadow: 0 6px 16px rgba(143, 37, 27, .22); }
.ap-new svg { flex: none; }
.ap-foot { margin-top: 4px; }
/* a computer: the addresses two by two, the button below them */
@media (min-width: 1024px) {
  .as-page { grid-template-columns: repeat(2, minmax(0, 1fr)); align-items: stretch; }
  .as-page > :not(.ap-row) { grid-column: 1 / -1; }
  .ap-foot { display: grid; justify-items: center; }
  .ap-foot .btn { max-width: 320px; }
}
@media (min-width: 768px) { .asheet { height: min(640px, 86vh); } .asheet.form { height: min(700px, 90vh); } }
@media (min-width: 900px) { .asheet.form:has(.aform.wide) { max-width: 980px; } }
</style>
