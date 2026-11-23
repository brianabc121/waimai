<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ApiError, del, get, patch, post } from '../api'
import { pick, t } from '../i18n'
import { currentRole, session } from '../session'
import type { Post, StaffMember, Station } from '../types'
import StaffTabs from './staff/StaffTabs.vue'
import './work/strings'

const bid = session.branchId!
const staff = ref<StaffMember[]>([])
const stations = ref<Station[]>([])
const error = ref('')
const notice = ref('')
const q = ref('')
// Filter: 'all', 'packing', 'rider', 'office', 'off', or a station id.
const filter = ref<string | number>('all')
const myRole = computed(() => currentRole()?.role ?? '')

async function load() {
  const [a, b] = await Promise.all([get<{ staff: StaffMember[] }>(`/merchant/branches/${bid}/staff`), get<{ stations: Station[] }>(`/merchant/branches/${bid}/stations`)])
  staff.value = a.staff; stations.value = b.stations.filter(s => s.active)
}
onMounted(() => load().catch(e => (error.value = (e as Error).message)))

const errText = (e: unknown) => {
  const code = e instanceof ApiError ? e.code : ''
  const k = 'st.e.' + code
  return t(k) !== k ? t(k) : (e as Error).message
}
const has = (m: StaffMember, kind: Post['kind'], station?: number) => m.posts.some(p => p.kind === kind && (station === undefined || p.station_id === station))
const active = computed(() => staff.value.filter(m => m.active))
const counts = computed(() => ({
  all: staff.value.length, packing: active.value.filter(m => has(m, 'packing')).length, rider: active.value.filter(m => has(m, 'rider')).length,
  office: active.value.filter(m => m.role !== 'staff').length, off: staff.value.length - active.value.length,
}))
const stationCount = (id: number) => active.value.filter(m => has(m, 'station', id)).length
const unstaffed = computed(() => stations.value.filter(s => stationCount(s.id) === 0))
const shown = computed(() => {
  const k = q.value.trim().toLowerCase(), f = filter.value
  return staff.value.filter(m => {
    if (k && !(m.display_name + ' ' + m.username + ' ' + m.phone).toLowerCase().includes(k)) return false
    if (f === 'all') return true
    if (f === 'off') return !m.active
    if (!m.active) return false
    if (f === 'office') return m.role !== 'staff'
    return typeof f === 'number' ? has(m, 'station', f) : has(m, f as Post['kind'])
  })
})
const postName = (p: Post) => p.kind === 'station' ? pick(p.station_name ?? '', p.station_name_en ?? '') : p.kind === 'packing' ? t('wk.packing') : t('rider')
const nowText = (m: StaffMember) => {
  if (!m.active) return t('st.disabled')
  const parts = []
  if (m.open_tasks) parts.push(t('st.now.tasks', { n: m.open_tasks }))
  if (m.active_deliveries) parts.push(t('st.now.deliveries', { n: m.active_deliveries }))
  // Everyone clocks on now, not only riders. Someone with back-office access
  // and no post is not expected to.
  const clocks = m.posts.length > 0 || m.on_duty
  return (clocks ? t(m.on_duty ? 'st.now.on_duty' : 'st.now.off_duty') + ' · ' : '') + (parts.join(' / ') || t('st.now.idle'))
}
const colors = ['#d9480f', '#1d4ed8', '#18794e', '#7c3aed', '#b45309', '#0e7490', '#be185d']
// Owners are managed by owners only.
const editable = (m: StaffMember) => m.role !== 'owner' || myRole.value === 'owner'

// ---- drawer ----
const f = reactive({ open: false, id: 0, username: '', display_name: '', phone: '', password: '', role: 'staff', stations: [] as number[], packing: false, rider: false,
  active: true, error: '', busy: false, pw: false, newPw: '' })
function edit(m: StaffMember | null) {
  Object.assign(f, { open: true, id: m?.id ?? 0, username: m?.username ?? '', display_name: m?.display_name ?? '', phone: m?.phone ?? '', password: '',
    role: m?.role ?? 'staff', stations: m ? m.posts.filter(p => p.kind === 'station').map(p => p.station_id!) : [], packing: !!m && has(m, 'packing'), rider: !!m && has(m, 'rider'),
    active: m?.active ?? true, error: '', busy: false, pw: false, newPw: '' })
  notice.value = ''
}
const self = computed(() => f.id === session.user?.id)
// Stations offered in the form: the active ones, plus a disabled one the person still holds.
const formStations = computed(() => {
  const held = staff.value.find(m => m.id === f.id)?.posts.filter(p => p.kind === 'station' && !stations.value.some(s => s.id === p.station_id)) ?? []
  return [...stations.value.map(s => ({ id: s.id, name: pick(s.name, s.name_en) })), ...held.map(p => ({ id: p.station_id!, name: postName(p) }))]
})
const toggleStation = (id: number) => { const i = f.stations.indexOf(id); i < 0 ? f.stations.push(id) : f.stations.splice(i, 1) }
const roles = computed(() => myRole.value === 'owner' ? ['staff', 'cashier', 'manager', 'owner'] : ['staff', 'cashier', 'manager'])

async function act(fn: () => Promise<unknown>, done = t('st.saved')) {
  f.error = ''; f.busy = true
  try { await fn(); await load(); f.open = false; notice.value = done } catch (e) { f.error = errText(e) }
  f.busy = false
}
function save() {
  const posts = [...f.stations.map(id => ({ kind: 'station', station_id: id })), ...(f.packing ? [{ kind: 'packing' }] : []), ...(f.rider ? [{ kind: 'rider' }] : [])]
  const body: Record<string, unknown> = { username: f.username, display_name: f.display_name, phone: f.phone, posts }
  if (!self.value) body.role = f.role
  return act(() => f.id ? patch(`/merchant/branches/${bid}/staff/${f.id}`, body)
    : post(`/merchant/branches/${bid}/staff`, { ...body, role: f.role, password: f.password }))
}
const setActive = (id: number, on: boolean) => act(() => patch(`/merchant/branches/${bid}/staff/${id}`, { active: on }))
const restore = async (m: StaffMember) => { f.id = m.id; await setActive(m.id, true); if (f.error) { error.value = f.error } }
function remove() {
  if (confirm(t('st.delete_confirm', { name: f.display_name }))) void act(() => del(`/merchant/branches/${bid}/staff/${f.id}`))
}
const resetPw = () => act(() => post(`/merchant/branches/${bid}/staff/${f.id}/password`, { password: f.newPw }), t('st.pw_done'))
</script>

<template>
  <div class="page staffpage">
    <StaffTabs />
    <div class="row between wrap" style="margin-bottom:12px">
      <h1 style="font-size:20px">{{ t('staff') }} <span class="muted small" style="font-weight:400">{{ t('st.count', { a: active.length, d: counts.off }) }}</span></h1>
      <span class="row wrap" style="max-width:100%">
        <input v-model="q" type="search" :placeholder="t('st.search')" style="width:220px;max-width:100%" data-testid="staff-search" />
        <button class="btn primary" data-testid="staff-new" @click="edit(null)">＋ {{ t('st.new') }}</button>
      </span>
    </div>
    <div v-if="error" class="banner bad" style="margin-bottom:12px">{{ error }}</div>
    <div v-if="notice" class="banner ok" style="margin-bottom:12px" data-testid="staff-notice">{{ notice }}</div>

    <div class="chips">
      <button :class="{ on: filter === 'all' }" @click="filter = 'all'">{{ t('st.all') }} {{ counts.all }}</button>
      <button v-for="s in stations" :key="s.id" :class="{ on: filter === s.id }" @click="filter = s.id">{{ pick(s.name, s.name_en) }} {{ stationCount(s.id) }}</button>
      <button :class="{ on: filter === 'packing' }" @click="filter = 'packing'">{{ t('wk.packing') }} {{ counts.packing }}</button>
      <button :class="{ on: filter === 'rider' }" data-testid="filter-rider" @click="filter = 'rider'">{{ t('rider') }} {{ counts.rider }}</button>
      <button :class="{ on: filter === 'office' }" @click="filter = 'office'">{{ t('st.backoffice') }} {{ counts.office }}</button>
      <button v-if="counts.off" :class="{ on: filter === 'off' }" @click="filter = 'off'">{{ t('st.disabled') }} {{ counts.off }}</button>
    </div>

    <div class="card" style="padding:0;overflow-x:auto">
      <table class="data stafftable">
        <thead><tr><th>{{ t('st.col.staff') }}</th><th>{{ t('phone') }}</th><th>{{ t('st.col.posts') }}</th><th>{{ t('st.col.role') }}</th><th>{{ t('st.col.now') }}</th><th></th></tr></thead>
        <tbody>
          <tr v-for="m in shown" :key="m.id" :class="{ off: !m.active }" :data-staff="m.username">
            <td><span class="av" :style="{ background: m.active ? colors[m.id % colors.length] : '#9ca3af' }">{{ m.display_name.slice(0, 1) }}</span>
              <b>{{ m.display_name }}</b> <span class="muted small">{{ m.username }}<template v-if="m.id === session.user?.id"> · {{ t('st.you') }}</template></span></td>
            <td>{{ m.phone }}</td>
            <td data-col="posts">
              <span v-for="p in m.posts" :key="p.kind + (p.station_id ?? '')" class="post" :class="p.kind">{{ postName(p) }}</span>
              <span v-if="!m.posts.length" class="post none">{{ t('st.no_posts') }}</span></td>
            <td><span v-if="m.role !== 'staff'" class="tag" :class="{ brand: m.role === 'owner' }">{{ t('role.' + m.role) }}</span><span v-else class="muted">{{ t('st.role.none') }}</span></td>
            <td class="small"><i class="dot" :class="{ on: m.active && (m.open_tasks > 0 || m.active_deliveries > 0 || m.on_duty) }"></i>{{ nowText(m) }}</td>
            <td class="right">
              <button v-if="!m.active && editable(m)" class="btn small" @click="restore(m)">{{ t('st.restore') }}</button>
              <button v-if="editable(m)" class="btn small" style="margin-inline-start:6px" @click="edit(m)">{{ t('edit') }}</button></td>
          </tr>
          <tr v-if="!shown.length"><td colspan="6" class="muted" style="text-align:center;padding:24px">{{ t('st.empty') }}</td></tr>
        </tbody>
      </table>
    </div>
    <p class="muted small" style="margin-top:12px">{{ t('st.hint') }}
      <span v-if="unstaffed.length" style="color:var(--warn)" data-testid="unstaffed">{{ t('st.unstaffed', { names: unstaffed.map(s => pick(s.name, s.name_en)).join('、') }) }}</span></p>

    <template v-if="f.open">
      <div class="drawer-mask" @click="f.open = false"></div>
      <form class="drawer" data-testid="staff-form" @submit.prevent="save">
        <div class="row between"><h2 style="font-size:18px">{{ f.id ? t('st.edit_title', { name: f.display_name }) : t('st.new_title') }}</h2>
          <button type="button" class="btn link" @click="f.open = false">✕</button></div>
        <div v-if="f.error" class="banner bad" data-testid="staff-error">{{ f.error }}</div>
        <div class="two">
          <label class="field"><span>{{ t('display_name') }}</span><input v-model="f.display_name" name="display_name" required /></label>
          <label class="field"><span>{{ t('phone') }}</span><input v-model="f.phone" name="phone" inputmode="tel" /></label>
          <label class="field"><span>{{ t('st.username') }}</span><input v-model="f.username" name="username" required autocapitalize="none" /></label>
          <label v-if="!f.id" class="field"><span>{{ t('st.password') }}</span><input v-model="f.password" name="password" type="password" required minlength="8" autocomplete="new-password" /></label>
          <label v-else class="field"><span>{{ t('password') }}</span><button type="button" class="btn block" @click="f.pw = !f.pw">{{ t('st.reset_pw') }}</button></label>
        </div>
        <div v-if="f.pw" class="row">
          <input v-model="f.newPw" name="new_password" type="password" minlength="8" autocomplete="new-password" :placeholder="t('st.new_pw')" />
          <button type="button" class="btn primary" :disabled="f.newPw.length < 8 || f.busy" @click="resetPw">{{ t('st.confirm') }}</button>
        </div>

        <div class="grp">
          <div class="gh"><b>{{ t('st.store_posts') }}</b> <span class="muted small">{{ t('st.store_posts_hint') }}</span></div>
          <div class="muted small">{{ t('st.stations') }}</div>
          <div class="opts">
            <button v-for="s in formStations" :key="s.id" type="button" class="opt" :class="{ on: f.stations.includes(s.id) }" :data-post="'station-' + s.name" @click="toggleStation(s.id)"><i></i>{{ s.name }}</button>
            <span v-if="!formStations.length" class="muted small">{{ t('st.no_stations') }}</span>
          </div>
          <div class="muted small">{{ t('st.other') }}</div>
          <div class="opts"><button type="button" class="opt" :class="{ on: f.packing }" data-post="packing" @click="f.packing = !f.packing"><i></i>{{ t('wk.packing') }}</button></div>
        </div>
        <div class="grp">
          <div class="gh"><b>{{ t('rider') }}</b> <span class="muted small">{{ t('st.rider_hint') }}</span></div>
          <div class="opts"><button type="button" class="opt" :class="{ on: f.rider }" data-post="rider" @click="f.rider = !f.rider"><i></i>{{ t('rider') }}</button></div>
        </div>
        <div class="grp" :class="{ locked: self }">
          <div class="gh"><b>{{ t('st.col.role') }}</b> <span class="muted small">{{ t('st.role_hint') }}</span></div>
          <div class="opts">
            <button v-for="r in roles" :key="r" type="button" class="opt radio" :class="{ on: f.role === r }" :data-role="r" :disabled="self" @click="f.role = r"><i></i>{{ r === 'staff' ? t('st.role.none') : t('role.' + r) }}</button>
          </div>
        </div>

        <div class="foot">
          <template v-if="f.id && !self">
            <button type="button" class="btn danger" :disabled="f.busy" data-testid="staff-toggle" @click="setActive(f.id, !f.active)">{{ f.active ? t('disable') : t('enable') }}</button>
            <button type="button" class="btn link" style="color:var(--bad)" :disabled="f.busy" data-testid="staff-delete" @click="remove">{{ t('st.delete') }}</button>
          </template>
          <span class="grow"></span>
          <button type="button" class="btn" @click="f.open = false">{{ t('cancel') }}</button>
          <button class="btn primary" :disabled="f.busy" data-testid="staff-save">{{ t('save') }}</button>
        </div>
      </form>
    </template>
  </div>
</template>

<style scoped>
.chips { display: flex; gap: 6px; flex-wrap: wrap; margin-bottom: 12px; }
.chips button { padding: 5px 12px; border-radius: 999px; background: #fff; border: 1px solid var(--line); font-size: 13px; cursor: pointer; }
.chips button.on { background: var(--ink); color: #fff; border-color: var(--ink); }
.stafftable td { white-space: nowrap; } .stafftable td[data-col=posts] { white-space: normal; }
.stafftable tr.off td { color: var(--muted); }
.av { width: 32px; height: 32px; border-radius: 50%; display: inline-flex; align-items: center; justify-content: center; color: #fff; font-weight: 700; margin-inline-end: 10px; }
.post { display: inline-block; padding: 2px 9px; border-radius: 6px; font-size: 12.5px; font-weight: 600; margin: 1px 4px 1px 0; }
.post.station { background: var(--info-soft); color: var(--info); } .post.packing { background: #f1e9ff; color: #6d28d9; }
.post.rider { background: var(--brand-soft); color: var(--brand-dark); } .post.none { background: #eef0f2; color: var(--muted); font-weight: 500; }
.dot { display: inline-block; width: 8px; height: 8px; border-radius: 50%; background: #c4c9d0; margin-inline-end: 6px; } .dot.on { background: var(--ok); }
.drawer-mask { position: fixed; inset: 0; background: rgba(0,0,0,.35); z-index: 40; }
.drawer { position: fixed; top: 0; inset-inline-end: 0; bottom: 0; width: min(460px, 100vw); background: #fff; z-index: 41; box-shadow: -8px 0 24px rgba(0,0,0,.15);
  padding: 18px 20px; display: flex; flex-direction: column; gap: 12px; overflow-y: auto; }
.two { display: grid; grid-template-columns: 1fr 1fr; gap: 0 10px; } .two .field { margin-bottom: 8px; }
.grp { border: 1px solid var(--line); border-radius: 12px; padding: 10px 12px 12px; display: grid; gap: 8px; }
.grp .gh b { margin-inline-end: 6px; } .grp.locked { opacity: .55; }
.opts { display: flex; flex-wrap: wrap; gap: 8px; }
.opt { border: 1.5px solid var(--line); border-radius: 10px; padding: 7px 12px; display: flex; gap: 7px; align-items: center; font-size: 14px; background: #fff; cursor: pointer; }
.opt i { width: 16px; height: 16px; border: 1.5px solid #9ca3af; border-radius: 4px; flex: none; position: relative; } .opt.radio i { border-radius: 50%; }
.opt.on { border-color: var(--brand); background: var(--brand-soft); font-weight: 600; } .opt.on i { background: var(--brand); border-color: var(--brand); }
.opt.on i::after { content: ""; position: absolute; inset-inline-start: 4.5px; top: 1.5px; width: 4px; height: 8px; border: solid #fff; border-width: 0 2px 2px 0; transform: rotate(45deg); }
.foot { margin-top: auto; display: flex; gap: 8px; align-items: center; padding-top: 6px; }
</style>
