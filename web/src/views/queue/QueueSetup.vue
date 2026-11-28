<script setup lang="ts">
// 排队叫号 > 设置 (docs/QUEUE_DISPLAY_DESIGN.md §6.1, §9.1): which modes are
// on, the table sizes, the rules, the screens and the QR code for the door.
import QRCode from 'qrcode'
import { computed, onMounted, ref, watch } from 'vue'
import { get, post, put } from '../../api'
import { t, tIn, loadLocale } from '../../i18n'
import { LANGS, isLocale } from '../../langs'
import { currentRole, session } from '../../session'
import LangsButton from '../LangsButton.vue'
import { announce, groupName, qErr, sayCall, unlockSound, type QueueDesk, type QueueGroup, type QueueScreen, type QueueSettings } from '../../queue'

const props = defineProps<{ desk: QueueDesk; base: string; manage: boolean }>()
const emit = defineEmits<{ reload: [] }>()
const error = ref('')
const flash = ref('')
function ok() { flash.value = t('qu.set.saved'); window.setTimeout(() => { flash.value = '' }, 3000) }

// ---- modes and rules ----
const set = ref<QueueSettings>({ ...props.desk.settings })
watch(() => props.desk.settings, s => { if (!dirty.value) set.value = { ...s } })
const dirty = computed(() => JSON.stringify(set.value) !== JSON.stringify(props.desk.settings))
const saving = ref(false)
async function saveSettings() {
  saving.value = true
  error.value = ''
  try { await put(props.base + '/settings', set.value); emit('reload'); ok() } catch (e) { error.value = qErr(e) }
  saving.value = false
}

// ---- table sizes ----
type Row = { id: number; letter: string; preset: string; label: string; label_en: string; min_people: number; max_people: number; table_ids: number[] }
const toRows = (gs: QueueGroup[]): Row[] => gs.map(g => ({ id: g.id, letter: g.letter, preset: g.preset, label: g.label, label_en: g.label_en, min_people: g.min_people, max_people: g.max_people, table_ids: [...(g.table_ids ?? [])] }))
const rows = ref<Row[]>(toRows(props.desk.groups))
const snapshot = computed(() => JSON.stringify(toRows(props.desk.groups)))
const groupsDirty = computed(() => JSON.stringify(rows.value) !== snapshot.value)
// a change from elsewhere shows unless this page has edits of its own
watch(snapshot, (_, old) => { if (JSON.stringify(rows.value) === old) rows.value = toRows(props.desk.groups) })
function addGroup() {
  const used = new Set(rows.value.map(r => r.letter))
  const letter = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ'.split('').find(l => !used.has(l)) ?? 'Z'
  const last = rows.value[rows.value.length - 1]
  const from = last ? Math.min(last.max_people + 1, 98) : 1
  rows.value.push({ id: 0, letter, preset: '', label: '', label_en: '', min_people: from, max_people: from + 1, table_ids: [] })
}
const tablesOf = (r: Row) => r.table_ids.length ? r.table_ids : props.desk.tables.filter(x => props.desk.groups.find(g => g.id === r.id)?.tables?.includes(x.id)).map(x => x.id)
function toggleTable(r: Row, id: number) {
  const l = r.table_ids
  const i = l.indexOf(id)
  if (i >= 0) l.splice(i, 1); else l.push(id)
}
const pickFor = ref<Row | null>(null)
async function saveGroups() {
  saving.value = true
  error.value = ''
  try {
    const r = await put<{ groups: QueueGroup[] }>(props.base + '/groups', { groups: rows.value })
    rows.value = toRows(r.groups)
    emit('reload')
    ok()
  } catch (e) { error.value = qErr(e) }
  saving.value = false
}
const dine = (id: number) => props.desk.groups.find(g => g.id === id)

// ---- screens ----
const screens = ref<QueueScreen[]>([])
async function loadScreens() { if (props.manage) screens.value = (await get<{ screens: QueueScreen[] }>(props.base + '/screens').catch(() => ({ screens: [] }))).screens }
onMounted(loadScreens)
const adding = ref(false)
const form = ref({ name: '', mode: 'restaurant', orientation: 'landscape', lang: 'zh', lang2: '' })
const codes = ref<Record<number, string>>({})
async function addScreen() {
  error.value = ''
  try {
    const r = await post<{ screen: QueueScreen; code: string }>(props.base + '/screens', form.value)
    codes.value = { ...codes.value, [r.screen.id]: r.code }
    adding.value = false
    await loadScreens()
  } catch (e) { error.value = qErr(e) }
}
async function updateScreen(s: QueueScreen) {
  try { await put(`${props.base}/screens/${s.id}`, { name: s.name, mode: s.mode, orientation: s.orientation, lang: s.lang, lang2: s.lang2 }); ok() }
  catch (e) { error.value = qErr(e); await loadScreens() }
}
async function revoke(s: QueueScreen) {
  if (!confirm(t('qu.set.revoke_q', { name: s.name }))) return
  try { await post(`${props.base}/screens/${s.id}/revoke`, {}); await loadScreens() } catch (e) { error.value = qErr(e) }
}
const online = (s: QueueScreen) => !!s.last_seen_at && Date.now() - new Date(s.last_seen_at).getTime() < 3 * 60000
const when = (iso: string) => new Date(iso).toLocaleString(undefined, { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
async function testVoice(s: QueueScreen) {
  unlockSound()
  const l = isLocale(s.lang) ? s.lang : 'zh'
  await loadLocale(l)
  announce(s.mode === 'tea' ? tIn(l, 'qu.sc.say_ready', { no: '036' }) : sayCall('A012', '05', (k, v) => tIn(l, k, v)), l)
}

// ---- the door's QR code ----
const slug = computed(() => currentRole()?.slug ?? '')
const doorLink = computed(() => `${location.origin}/q/${slug.value}`)
const qr = ref('')
watch(doorLink, async l => { qr.value = await QRCode.toDataURL(l, { width: 360, margin: 1 }).catch(() => '') }, { immediate: true })
const screenUrl = computed(() => `${location.host}/screen`)
</script>

<template>
  <div class="qx">
    <div v-if="error" class="banner bad" data-testid="qx-error">{{ error }}</div>
    <div v-else-if="flash" class="banner ok" data-testid="qx-saved">{{ flash }}</div>
    <div v-if="!manage" class="banner info">{{ t('rp.need') }}</div>

    <div class="ox-split wide">
      <div class="ox-stack">
        <!-- modes -->
        <section class="ox-panel">
          <div class="ox-panel-h"><h2>{{ t('qu.set.modes') }}</h2></div>
          <label class="srow"><span class="tx"><b>{{ t('qu.set.rest') }}</b><small>{{ t('qu.set.rest_d') }}</small></span>
            <input v-model="set.restaurant" type="checkbox" class="sw" :disabled="!manage" data-testid="qx-rest" /></label>
          <label class="srow"><span class="tx"><b>{{ t('qu.set.tea') }}</b><small>{{ t('qu.set.tea_d') }}</small></span>
            <input v-model="set.tea" type="checkbox" class="sw" :disabled="!manage" data-testid="qx-tea" /></label>
        </section>

        <!-- table sizes -->
        <section v-if="set.restaurant" class="ox-panel">
          <div class="ox-panel-h"><h2>{{ t('qu.set.groups') }}</h2><span class="grow"></span>
            <LangsButton v-if="rows.some(r => r.id && r.label)" small :base="`/merchant/branches/${session.branchId}`" :scope="`queue/${session.branchId}`" :name="t('qu.set.groups')" />
            <button v-if="manage" class="btn small" @click="addGroup">{{ t('qu.set.add_group') }}</button></div>
          <p class="muted small">{{ t('qu.set.groups_d') }}</p>
          <div class="ox-scroll">
            <table class="data gtbl">
              <thead><tr><th>{{ t('qu.set.letter') }}</th><th>{{ t('qu.set.name') }}</th><th>{{ t('qu.set.range') }}</th><th>{{ t('qu.set.tables') }}</th><th></th></tr></thead>
              <tbody>
                <tr v-for="(r, i) in rows" :key="i" data-testid="qx-group">
                  <td><input v-model="r.letter" class="lt" maxlength="1" :disabled="!manage" @input="r.letter = r.letter.toUpperCase()" /></td>
                  <td><input v-model="r.label" class="nm" :placeholder="groupName({ ...r, label: '', label_en: '' })" :disabled="!manage" /></td>
                  <td class="nowrap"><input v-model.number="r.min_people" type="number" min="1" max="99" class="pn" :disabled="!manage" /> –
                    <input v-model.number="r.max_people" type="number" min="1" max="99" class="pn" :disabled="!manage" :data-testid="'qx-max-' + r.letter" /></td>
                  <td>
                    <button class="tlist" :disabled="!manage" @click="pickFor = pickFor === r ? null : r">
                      <span v-if="!r.table_ids.length" class="muted">{{ t('qu.set.auto') }}<template v-if="tablesOf(r).length">: </template></span>
                      <span v-for="id in tablesOf(r)" :key="id" class="tb">{{ desk.tables.find(x => x.id === id)?.name }}</span>
                    </button>
                    <div v-if="pickFor === r" class="tpick">
                      <button v-for="tb in desk.tables" :key="tb.id" :class="{ on: r.table_ids.includes(tb.id) }" @click="toggleTable(r, tb.id)">{{ tb.name }} <small>{{ t('qu.seats', { n: tb.seats }) }}</small></button>
                    </div>
                    <small v-if="dine(r.id)" class="muted block">{{ t('qu.set.dine', { n: dine(r.id)!.dine_minutes }) }}</small>
                  </td>
                  <td><button v-if="manage" class="btn link small" @click="rows.splice(i, 1)">×</button></td>
                </tr>
              </tbody>
            </table>
          </div>
          <div v-if="manage" class="row end"><button class="btn primary" :disabled="saving || !groupsDirty" data-testid="qx-save-groups" @click="saveGroups">{{ t('qu.set.save') }}</button></div>
        </section>

        <!-- rules -->
        <section class="ox-panel">
          <template v-if="set.restaurant">
            <div class="ox-panel-h"><h2>{{ t('qu.set.rules') }}</h2></div>
            <label class="srow"><span class="tx"><b>{{ t('qu.set.upgrade') }}</b></span><input v-model.number="set.upgrade_minutes" type="number" min="0" max="240" class="pn" :disabled="!manage" data-testid="qx-upgrade" /></label>
            <label class="srow"><span class="tx"><b>{{ t('qu.set.alert') }}</b></span><input v-model.number="set.call_alert_minutes" type="number" min="1" max="60" class="pn" :disabled="!manage" /></label>
            <label class="srow"><span class="tx"><b>{{ t('qu.set.requeue') }}</b></span><input v-model.number="set.requeue_after" type="number" min="0" max="20" class="pn" :disabled="!manage" /></label>
            <label class="srow"><span class="tx"><b>{{ t('qu.set.dine_min') }}</b></span><input v-model.number="set.dine_minutes" type="number" min="10" max="300" class="pn" :disabled="!manage" /></label>
            <label class="srow"><span class="tx"><b>{{ t('qu.set.sms') }}</b></span><input v-model="set.sms" type="checkbox" class="sw" :disabled="!manage" /></label>
          </template>
          <template v-if="set.tea">
            <div class="ox-panel-h" :class="{ mt: set.restaurant }"><h2>{{ t('qu.set.tea_rules') }}</h2></div>
            <label class="srow"><span class="tx"><b>{{ t('qu.set.ready_stay') }}</b></span><input v-model.number="set.ready_stay_minutes" type="number" min="1" max="120" class="pn" :disabled="!manage" /></label>
            <label class="srow"><span class="tx"><b>{{ t('qu.set.dine_in') }}</b></span><input v-model="set.show_dine_in" type="checkbox" class="sw" :disabled="!manage" /></label>
            <label class="srow"><span class="tx"><b>{{ t('qu.set.delivery') }}</b></span><input v-model="set.show_delivery" type="checkbox" class="sw" :disabled="!manage" /></label>
          </template>
          <label class="srow"><span class="tx"><b>{{ t('qu.set.media') }}</b></span><input v-model="set.media" type="checkbox" class="sw" :disabled="!manage" /></label>
          <div v-if="manage" class="row end"><button class="btn primary" :disabled="saving || !dirty" data-testid="qx-save" @click="saveSettings">{{ t('qu.set.save') }}</button></div>
        </section>
      </div>

      <div class="ox-stack">
        <!-- screens -->
        <section v-if="manage" class="ox-panel" data-testid="qx-screens">
          <div class="ox-panel-h"><h2>{{ t('qu.set.screens') }}</h2><span class="grow"></span>
            <button class="btn small primary" data-testid="qx-add-screen" @click="adding = !adding">{{ t('qu.set.add_screen') }}</button></div>
          <p class="muted small">{{ t('qu.set.screens_d', { url: screenUrl }) }}</p>
          <div v-if="adding" class="addf">
            <input v-model="form.name" :placeholder="t('qu.set.screen_name')" maxlength="40" data-testid="qx-sname" />
            <select v-model="form.mode"><option value="restaurant">{{ t('qu.set.mode.restaurant') }}</option><option value="tea">{{ t('qu.set.mode.tea') }}</option></select>
            <select v-model="form.orientation"><option value="landscape">{{ t('qu.set.landscape') }}</option><option value="portrait">{{ t('qu.set.portrait') }}</option></select>
            <select v-model="form.lang" :aria-label="t('qu.set.lang')"><option v-for="l in LANGS" :key="l.code" :value="l.code">{{ l.name }}</option></select>
            <select v-model="form.lang2" :aria-label="t('qu.set.lang2')"><option value="">{{ t('qu.set.lang2') }}: {{ t('qu.set.lang2_none') }}</option><option v-for="l in LANGS" :key="l.code" :value="l.code">{{ l.name }}</option></select>
            <button class="btn primary" :disabled="!form.name.trim()" data-testid="qx-create-screen" @click="addScreen">{{ t('qu.set.save') }}</button>
          </div>
          <div v-for="s in screens" :key="s.id" class="scr" data-testid="qx-screen">
            <div class="ic" :class="{ v: s.orientation === 'portrait' }"></div>
            <div class="tx">
              <input v-model="s.name" class="sn" maxlength="40" @change="updateScreen(s)" />
              <div class="sel">
                <select v-model="s.mode" @change="updateScreen(s)"><option value="restaurant">{{ t('qu.set.mode.restaurant') }}</option><option value="tea">{{ t('qu.set.mode.tea') }}</option></select>
                <select v-model="s.orientation" @change="updateScreen(s)"><option value="landscape">{{ t('qu.set.landscape') }}</option><option value="portrait">{{ t('qu.set.portrait') }}</option></select>
                <select v-model="s.lang" @change="updateScreen(s)"><option v-for="l in LANGS" :key="l.code" :value="l.code">{{ l.name }}</option></select>
                <select v-model="s.lang2" @change="updateScreen(s)"><option value="">{{ t('qu.set.lang2') }}: {{ t('qu.set.lang2_none') }}</option><option v-for="l in LANGS" :key="l.code" :value="l.code">{{ l.name }}</option></select>
              </div>
              <small v-if="codes[s.id] && !s.paired" class="code" data-testid="qx-code">{{ t('qu.set.code', { code: codes[s.id] }) }}</small>
              <small v-else-if="s.paired" :class="online(s) ? 'okt' : 'muted'">{{ online(s) ? t('qu.set.online') : s.last_seen_at ? t('qu.set.seen', { t: when(s.last_seen_at) }) : '' }}</small>
              <small v-else class="muted">{{ t('qu.set.unpaired') }}</small>
            </div>
            <div class="acts">
              <button class="btn small" @click="testVoice(s)">{{ t('qu.set.voice') }}</button>
              <button class="btn small danger" @click="revoke(s)">{{ t('qu.set.revoke') }}</button>
            </div>
          </div>
        </section>

        <!-- QR for the door -->
        <section v-if="set.restaurant && slug" class="ox-panel qrp">
          <div class="ox-panel-h"><h2>{{ t('qu.set.qr') }}</h2></div>
          <p class="muted small">{{ t('qu.set.qr_d') }}</p>
          <div class="qrrow"><img v-if="qr" :src="qr" alt="" data-testid="qx-qr" /><a :href="doorLink" target="_blank" rel="noopener" class="lnk">{{ doorLink }}</a></div>
        </section>
      </div>
    </div>
  </div>
</template>

<style scoped>
.qx { display: grid; gap: 12px; }
.srow { display: flex; align-items: center; gap: 12px; padding: 10px 0; border-bottom: 1px solid #f0e6d7; cursor: pointer; }
.srow:last-of-type { border-bottom: 0; }
.srow .tx { flex: 1; min-width: 0; } .srow .tx b { display: block; font-size: 14px; } .srow .tx small { color: var(--muted); font-size: 12.5px; }
.sw { width: 20px; height: 20px; flex: none; accent-color: var(--brand); }
.pn { width: 76px; }
.lt { width: 44px; text-align: center; font-weight: 900; }
.nm { width: 130px; }
.block { display: block; margin-top: 4px; }
.tlist { border: 1px dashed var(--line); background: #fff; border-radius: 8px; padding: 4px 8px; cursor: pointer; text-align: start; font: inherit; min-width: 120px; }
.tb { display: inline-block; border: 1px solid #e5d6c0; border-radius: 6px; padding: 1px 7px; margin: 1px 2px; background: #fffaf2; font-size: 12.5px; }
.tpick { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 6px; }
.tpick button { border: 1px solid var(--line); background: #fff; border-radius: 8px; padding: 4px 10px; cursor: pointer; font: inherit; font-size: 13px; }
.tpick button.on { border-color: var(--brand); background: var(--pink); color: var(--brand); font-weight: 700; }
.tpick small { color: var(--muted); }
.row.end { justify-content: flex-end; margin-top: 10px; }
.mt { margin-top: 16px; }
.addf { display: grid; grid-template-columns: repeat(auto-fill, minmax(140px, 1fr)); gap: 8px; margin-bottom: 12px; padding: 10px; background: #fbf4ea; border-radius: 10px; }
.scr { display: flex; align-items: center; gap: 12px; padding: 10px; border: 1px solid #f0e6d7; border-radius: 10px; margin-bottom: 8px; background: #fff; flex-wrap: wrap; }
.scr .ic { width: 46px; height: 30px; border-radius: 4px; border: 3px solid #3b1f14; flex: none; } .scr .ic.v { width: 26px; height: 40px; }
.scr .tx { flex: 1; min-width: 200px; display: grid; gap: 4px; }
.scr .sn { font-weight: 700; border-color: transparent; padding: 2px 4px; }
.scr .sn:hover, .scr .sn:focus { border-color: var(--line); }
.scr .sel { display: flex; flex-wrap: wrap; gap: 6px; } .scr .sel select { width: auto; padding: 3px 6px; font-size: 13px; }
.scr .acts { display: flex; gap: 6px; }
.code { font-size: 15px; font-weight: 900; color: var(--brand); background: #fbf4ea; border-radius: 8px; padding: 4px 10px; justify-self: start; }
.okt { color: #2f7a2a; font-weight: 700; }
.qrrow { display: flex; align-items: center; gap: 14px; flex-wrap: wrap; } .qrrow img { width: 150px; height: 150px; }
.lnk { word-break: break-all; color: var(--brand); }
</style>
