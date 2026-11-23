<script setup lang="ts">
// Asking to open a store on the platform. The platform reviews it and gets in
// touch; the answer also arrives as a message.
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ApiError } from '../../api'
import { t } from '../../i18n'
import { mallSpot } from '../../mall'
import { mapsKey } from '../../maps'
import PlacePicker, { type Picked } from '../PlacePicker.vue'
import MeIntro from './MeIntro.vue'
import MePage from './MePage.vue'
import { dateTime, describe, me, myApplications, placeLine, sendApplication, type Application, type ApplicationPlace } from './me'

const list = ref<Application[]>([])
const blankPlace = (): ApplicationPlace => ({ country_code: '', country: '', province: '', district: '', postal_code: '',
  place_name: '', place_address: '', unit: '', lat: null, lng: null })
const blank = () => ({ store_name: '', contact_name: '', phone: '', city: '', cuisine: '', note: '', ...blankPlace() })
const f = reactive(blank())
const error = ref(''), sent = ref(false), busy = ref(false)
const pending = computed(() => list.value.some(a => a.status === 'pending'))
watch(() => me.profile?.id, async id => { if (id) list.value = await myApplications().catch(() => []) }, { immediate: true })

// Where the store is: picked on the map like a delivery address, so that the
// platform gets the country, province, area, street and the point. Without a
// map key on the platform the old box for the city is shown instead.
const hasMap = ref<boolean | null>(null), picking = ref(false)
onMounted(async () => { hasMap.value = !!(await mapsKey().catch(() => '')) })
const start = computed(() => f.lat != null && f.lng != null ? { lat: f.lat, lng: f.lng } : mallSpot())
function picked(p: Picked) {
  const r = p.region
  Object.assign(f, { lat: p.lat, lng: p.lng, place_name: p.name, place_address: p.address,
    country_code: r?.country_code ?? '', country: r?.country ?? '', province: r?.province ?? '', city: r?.city ?? '',
    district: r?.district ?? '', postal_code: r?.postal_code ?? '' })
  picking.value = false
  if (error.value === t('ma.e.place')) error.value = ''
}

async function submit() {
  error.value = ''; sent.value = false
  if (!f.store_name.trim() || !f.contact_name.trim() || !f.phone.trim()) { error.value = t('ma.e.required'); return }
  if (hasMap.value ? f.lat == null : !f.city.trim()) { error.value = t(hasMap.value ? 'ma.e.place' : 'ma.e.city'); return }
  busy.value = true
  try {
    list.value.unshift(await sendApplication({ ...f }))
    Object.assign(f, blank())
    sent.value = true
  } catch (e) { error.value = e instanceof ApiError && e.code === 'APPLICATION_PENDING' ? t('ma.pending') : describe(e) }
  busy.value = false
}
</script>

<template>
  <MePage :title="t('ma.title')">
    <MeIntro :title="t('mi.apply')" :sub="t('mi.apply_sub')" :icon="'M5 10.5V19h14v-8.5M3.5 7 5 4h14l1.5 3v1a2.2 2.2 0 0 1-4.25.8 2.2 2.2 0 0 1-4.25 0 2.2 2.2 0 0 1-4.25 0A2.2 2.2 0 0 1 3.5 8zM9.5 19v-4.5h5V19'" />
    <div v-if="sent" class="banner ok" data-testid="apply-sent">{{ t('ma.sent') }}</div>
    <div v-if="pending" class="banner info" data-testid="apply-pending">{{ t('ma.pending') }}</div>
    <form v-else class="card" data-testid="apply-form" @submit.prevent="submit">
      <div v-if="error" class="banner bad" style="margin-bottom:12px" data-testid="apply-error">{{ error }}</div>
      <label class="field"><span>{{ t('ma.store') }} *</span><input v-model="f.store_name" maxlength="100" data-testid="apply-store" /></label>
      <label class="field"><span>{{ t('ma.contact') }} *</span><input v-model="f.contact_name" maxlength="80" data-testid="apply-contact" /></label>
      <label class="field"><span>{{ t('ma.phone') }} *</span><input v-model="f.phone" maxlength="40" inputmode="tel" data-testid="apply-phone" /></label>
      <label v-if="hasMap" class="field"><span>{{ t('ma.place') }} *</span>
        <button type="button" class="ma-place" data-testid="apply-place" @click="picking = true">
          <span class="pin"><svg viewBox="0 0 24 24" width="22" height="22" aria-hidden="true"><path d="M12 21s-6.5-6.1-6.5-11a6.5 6.5 0 0113 0c0 4.9-6.5 11-6.5 11z" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linejoin="round" /><circle cx="12" cy="10" r="2.3" fill="none" stroke="currentColor" stroke-width="1.7" /></svg></span>
          <span class="txt">
            <template v-if="f.place_name">
              <b>{{ f.place_name }}</b>
              <span class="sub" v-if="f.place_address">{{ f.place_address }}</span>
              <span class="sub" v-if="placeLine(f)" data-testid="apply-region">{{ placeLine(f) }}</span>
            </template>
            <span v-else class="sub">{{ t('ma.place_ph') }}</span>
          </span>
          <svg class="chev" viewBox="0 0 24 24" width="20" height="20" aria-hidden="true"><path d="M9 5l7 7-7 7" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" /></svg>
        </button>
      </label>
      <label v-if="hasMap" class="field"><span>{{ t('ma.unit') }}</span><input v-model="f.unit" maxlength="100" :placeholder="t('ma.unit_ph')" data-testid="apply-unit" /></label>
      <label v-else-if="hasMap === false" class="field"><span>{{ t('ma.city') }} *</span><input v-model="f.city" maxlength="100" data-testid="apply-city" /></label>
      <label class="field"><span>{{ t('ma.cuisine') }}</span><input v-model="f.cuisine" maxlength="100" :placeholder="t('ma.cuisine_ph')" /></label>
      <label class="field"><span>{{ t('ma.note') }}</span><textarea v-model="f.note" rows="3" maxlength="1000"></textarea></label>
      <button class="btn primary block big" :disabled="busy" data-testid="apply-submit">{{ t('ma.submit') }}</button>
    </form>
    <template v-if="list.length">
      <h3 class="me-sec">{{ t('ma.mine') }}</h3>
      <div v-for="a in list" :key="a.id" class="card stack" data-testid="application" :data-status="a.status">
        <div class="row between"><b>{{ a.store_name }}</b>
          <span class="tag" :class="{ ok: a.status === 'approved', bad: a.status === 'rejected', warn: a.status === 'pending' }">{{ t('ma.s_' + a.status) }}</span></div>
        <div class="small muted">{{ a.contact_name }} · {{ a.phone }}<template v-if="a.place_name || a.city"> · {{ a.place_name || a.city }}</template> · {{ dateTime(a.created_at) }}</div>
        <div v-if="a.reply" class="reply"><b>{{ t('ma.reply') }}</b>：{{ a.reply }}</div>
      </div>
    </template>
    <div v-if="picking" class="sheet-mask" @click.self="picking = false">
      <div class="sheet ma-sheet">
        <PlacePicker :start="start" :name="f.place_name" region @done="picked" @close="picking = false" />
      </div>
    </div>
  </MePage>
</template>

<style scoped>
.ma-place { display: flex; align-items: center; gap: 12px; width: 100%; padding: 12px; border: 0; border-radius: 10px; background: var(--gold-soft, #fbe7b3);
  text-align: left; cursor: pointer; font: inherit; color: inherit; }
.ma-place .pin { width: 40px; height: 40px; border-radius: 50%; flex: none; display: grid; place-items: center; color: var(--brand-dark); background: #f3d9a4; }
.ma-place .txt { flex: 1; min-width: 0; display: grid; gap: 3px; justify-items: start; }
.ma-place .txt b { font-size: 16px; overflow-wrap: anywhere; }
.ma-place .sub { font-size: 13px; color: var(--muted); overflow-wrap: anywhere; }
.ma-place .chev { color: var(--muted); flex: none; }
.ma-place:focus-visible { outline: 2px solid var(--brand); outline-offset: 2px; }
.ma-sheet { height: 94vh; max-height: 94vh; overflow: hidden; }
@media (min-width: 768px) { .ma-sheet { height: min(760px, 90vh); max-width: 640px; border-radius: 16px; margin: auto; } }
.reply { background: var(--bg); border-radius: 10px; padding: 10px 12px; font-size: 14px; white-space: pre-wrap; }
</style>
