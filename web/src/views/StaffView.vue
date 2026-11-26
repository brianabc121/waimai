<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ApiError, del, get, patch, post, put } from '../api'
import { brandsHere, multiBrand } from '../brands'
import { pick, t } from '../i18n'
import { currentRole, isOwner, session } from '../session'
import '../perms'
import type { Post, StaffMember, Station } from '../types'
import '../officeStrings'
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
const colors = ['#a02c20', '#2556a8', '#2f7a2a', '#8a5a00', '#c8641e', '#0e7490', '#7b3f8c']
const busy = (m: StaffMember) => m.open_tasks > 0 || m.active_deliveries > 0 || m.on_duty
// Owners and managers are managed by owners only (docs/STAFF_ACCESS_DESIGN.md §6.3).
const editable = (m: StaffMember) => !['owner', 'manager'].includes(m.role) || myRole.value === 'owner' || m.id === session.user?.id

// ---- drawer ----
const f = reactive({ open: false, id: 0, username: '', display_name: '', phone: '', password: '', role: 'staff', stations: [] as number[], packing: false, rider: false,
  active: true, error: '', busy: false, pw: false, newPw: '', brands: [] as number[] })
function edit(m: StaffMember | null) {
  Object.assign(f, { open: true, id: m?.id ?? 0, username: m?.username ?? '', display_name: m?.display_name ?? '', phone: m?.phone ?? '', password: '',
    role: m?.role ?? 'staff', stations: m ? m.posts.filter(p => p.kind === 'station').map(p => p.station_id!) : [], packing: !!m && has(m, 'packing'), rider: !!m && has(m, 'rider'),
    active: m?.active ?? true, error: '', busy: false, pw: false, newPw: '', brands: m?.brand_ids ? [...m.brand_ids] : [] })
  notice.value = ''
  void loadPerms()
}
const self = computed(() => f.id === session.user?.id)
// Stations offered in the form: the active ones, plus a disabled one the person still holds.
const formStations = computed(() => {
  const held = staff.value.find(m => m.id === f.id)?.posts.filter(p => p.kind === 'station' && !stations.value.some(s => s.id === p.station_id)) ?? []
  return [...stations.value.map(s => ({ id: s.id, name: pick(s.name, s.name_en) })), ...held.map(p => ({ id: p.station_id!, name: postName(p) }))]
})
// Brands they work on (docs/MULTI_BRAND_DESIGN.md §10); none ticked = all.
const showBrands = computed(() => multiBrand() && f.role !== 'owner' && !self.value)
const toggleBrand = (id: number) => { const i = f.brands.indexOf(id); i < 0 ? f.brands.push(id) : f.brands.splice(i, 1) }
const brandNames = (m: StaffMember) => m.brand_ids ? brandsHere().filter(b => m.brand_ids!.includes(b.id)).map(b => b.name).join('、') : ''
const toggleStation = (id: number) => { const i = f.stations.indexOf(id); i < 0 ? f.stations.push(id) : f.stations.splice(i, 1) }
const roles = computed(() => myRole.value === 'owner' || self.value ? ['staff', 'cashier', 'manager', 'owner'] : ['staff', 'cashier'])
const roleHint = (r: string) => r === 'staff' ? t('rp.role.staff_hint') : r === 'owner' ? t('rp.role.owner_hint') : r === 'manager' ? t('rp.manager_only_owner') : t('rp.role.cashier_hint')

// What the chosen role can do in this store, for the owner (who sees the table).
interface PermTable { catalog: { code: string }[]; cashier: string[]; manager: string[] }
const permTable = ref<PermTable | null>(null)
async function loadPerms() { if (isOwner() && !permTable.value) permTable.value = await get<PermTable>(`/merchant/branches/${bid}/role-perms`).catch(() => null) }
const summary = computed(() => {
  const tb = permTable.value
  if (!tb || (f.role !== 'cashier' && f.role !== 'manager')) return null
  const mine = f.role === 'cashier' ? tb.cashier : tb.manager
  return { yes: tb.catalog.filter(c => mine.includes(c.code)).map(c => t('rp.p.' + c.code)), no: tb.catalog.filter(c => !mine.includes(c.code)).map(c => t('rp.p.' + c.code)) }
})

async function act(fn: () => Promise<unknown>, done = t('st.saved')) {
  f.error = ''; f.busy = true
  try { await fn(); await load(); f.open = false; notice.value = done } catch (e) { f.error = errText(e) }
  f.busy = false
}
function save() {
  const posts = [...f.stations.map(id => ({ kind: 'station', station_id: id })), ...(f.packing ? [{ kind: 'packing' }] : []), ...(f.rider ? [{ kind: 'rider' }] : [])]
  const body: Record<string, unknown> = { username: f.username, display_name: f.display_name, phone: f.phone, posts }
  if (!self.value) body.role = f.role
  return act(async () => {
    let id = f.id
    if (id) await patch(`/merchant/branches/${bid}/staff/${id}`, body)
    else id = (await post<{ id: number }>(`/merchant/branches/${bid}/staff`, { ...body, role: f.role, password: f.password })).id
    const was = staff.value.find(m => m.id === id)?.brand_ids ?? []
    const now = showBrands.value ? f.brands : []
    if ([...was].sort().join() !== [...now].sort().join()) await put(`/merchant/branches/${bid}/staff/${id}/brands`, { brand_ids: now })
  })
}
const setActive = (id: number, on: boolean) => act(() => patch(`/merchant/branches/${bid}/staff/${id}`, { active: on }))
const restore = async (m: StaffMember) => { f.id = m.id; await setActive(m.id, true); if (f.error) { error.value = f.error } }
function remove() {
  if (confirm(t('st.delete_confirm', { name: f.display_name }))) void act(() => del(`/merchant/branches/${bid}/staff/${f.id}`))
}
const resetPw = () => act(() => post(`/merchant/branches/${bid}/staff/${f.id}/password`, { password: f.newPw }), t('st.pw_done'))
</script>

<template>
  <div class="ox warm">
  <div class="page staffpage">
    <StaffTabs><button class="btn primary" data-testid="staff-new" @click="edit(null)">＋ {{ t('st.new') }}</button></StaffTabs>
    <div v-if="error" class="banner bad" style="margin-bottom:12px">{{ error }}</div>
    <div v-if="notice" class="banner ok" style="margin-bottom:12px" data-testid="staff-notice">{{ notice }}</div>

    <div class="ox-body">
      <aside class="ox-rail">
        <div class="ox-rail-h">{{ t('st.col.posts') }}<span class="n">{{ t('st.count', { a: active.length, d: counts.off }) }}</span></div>
        <div class="ox-rail-list chips">
          <button class="ox-cat" :class="{ on: filter === 'all' }" @click="filter = 'all'"><span class="nm">{{ t('st.all') }}</span><span class="n">{{ counts.all }}</span></button>
          <button v-for="s in stations" :key="s.id" class="ox-cat" :class="{ on: filter === s.id }" @click="filter = s.id"><span class="nm">{{ pick(s.name, s.name_en) }}</span><span class="n">{{ stationCount(s.id) }}</span></button>
          <button class="ox-cat" :class="{ on: filter === 'packing' }" @click="filter = 'packing'"><span class="nm">{{ t('wk.packing') }}</span><span class="n">{{ counts.packing }}</span></button>
          <button class="ox-cat" :class="{ on: filter === 'rider' }" data-testid="filter-rider" @click="filter = 'rider'"><span class="nm">{{ t('rider') }}</span><span class="n">{{ counts.rider }}</span></button>
          <button class="ox-cat" :class="{ on: filter === 'office' }" @click="filter = 'office'"><span class="nm">{{ t('st.backoffice') }}</span><span class="n">{{ counts.office }}</span></button>
          <button v-if="counts.off" class="ox-cat" :class="{ on: filter === 'off' }" @click="filter = 'off'"><span class="nm">{{ t('st.disabled') }}</span><span class="n">{{ counts.off }}</span></button>
        </div>
        <p class="ox-rail-tip">{{ t('st.hint') }}</p>
      </aside>

      <section class="ox-panel">
        <div class="ox-tools">
          <label class="ox-search"><input v-model="q" type="search" :placeholder="t('st.search')" data-testid="staff-search" /></label>
          <span class="grow"></span>
          <span class="muted small">{{ t('ox.st.shown', { n: shown.length }) }}</span>
        </div>
        <div v-if="unstaffed.length" class="ox-tip" style="margin-bottom:12px" data-testid="unstaffed">{{ t('st.unstaffed', { names: unstaffed.map(s => pick(s.name, s.name_en)).join('、') }) }}</div>
        <div class="ox-scroll">
          <table class="data stafftable">
            <thead><tr><th>{{ t('st.col.staff') }}</th><th>{{ t('phone') }}</th><th>{{ t('st.col.posts') }}</th><th>{{ t('st.col.role') }}</th><th>{{ t('st.col.now') }}</th><th class="right">{{ t('ox.ops') }}</th></tr></thead>
            <tbody>
              <tr v-for="m in shown" :key="m.id" :class="{ off: !m.active }" :data-staff="m.username">
                <td><span class="who"><span class="av" :style="{ background: m.active ? colors[m.id % colors.length] : '#b9aa9a' }">{{ m.display_name.slice(0, 1) }}</span>
                  <span><b class="ox-strong">{{ m.display_name }}</b><span class="muted small uname">{{ m.username }}<template v-if="m.id === session.user?.id"> · {{ t('st.you') }}</template></span></span></span></td>
                <td>{{ m.phone || '—' }}</td>
                <td data-col="posts">
                  <span v-for="p in m.posts" :key="p.kind + (p.station_id ?? '')" class="post" :class="p.kind">{{ postName(p) }}</span>
                  <span v-if="!m.posts.length" class="post none">{{ t('st.no_posts') }}</span></td>
                <td><span class="rtag" :class="m.role" :data-role="m.role">{{ m.role === 'staff' ? t('st.role.none') : t('role.' + m.role) }}</span>
                  <div v-if="multiBrand() && m.brand_ids" class="muted small" data-testid="staff-brand-names">{{ brandNames(m) }}</div></td>
                <td><span class="ox-pill" :class="!m.active ? 'bad' : busy(m) ? 'ok' : ''">{{ nowText(m) }}</span></td>
                <td class="right"><span class="ox-ops">
                  <button v-if="!m.active && editable(m)" class="op red" @click="restore(m)">{{ t('st.restore') }}</button>
                  <button v-if="editable(m)" class="op" :class="{ red: m.active }" @click="edit(m)">{{ t('edit') }}</button></span></td>
              </tr>
              <tr v-if="!shown.length"><td colspan="6" class="ox-empty">{{ t('st.empty') }}</td></tr>
            </tbody>
          </table>
        </div>
      </section>
    </div>

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
            <button v-for="r in roles" :key="r" type="button" class="opt radio" :class="{ on: f.role === r }" :data-role="r" :title="roleHint(r)" :disabled="self" @click="f.role = r"><i></i>{{ r === 'staff' ? t('st.role.none') : t('role.' + r) }}</button>
          </div>
          <div class="muted small">{{ roleHint(f.role) }}</div>
          <div v-if="summary" class="permsum" data-testid="role-summary">
            <div class="row between"><b>{{ t('rp.can', { role: t('role.' + f.role) }) }}</b>
              <router-link class="small" to="/merchant/staff/roles">{{ t('rp.edit_link') }} ›</router-link></div>
            <div class="y">✓ {{ summary.yes.join('、') }}</div>
            <div v-if="summary.no.length" class="n">✗ {{ summary.no.join('、') }}</div>
          </div>
        </div>

        <div v-if="showBrands" class="grp" data-testid="staff-brands">
          <div class="gh"><b>{{ t('br.staff') }}</b> <span class="muted small">{{ t('br.staff_hint') }}</span></div>
          <div class="opts">
            <button type="button" class="opt radio" :class="{ on: !f.brands.length }" data-brand="all" @click="f.brands = []"><i></i>{{ t('br.staff_all') }}</button>
            <button v-for="b in brandsHere()" :key="b.id" type="button" class="opt" :class="{ on: f.brands.includes(b.id) }" :data-brand="b.id" @click="toggleBrand(b.id)"><i></i>{{ b.name }}</button>
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
  </div>
</template>

<style scoped>
.who { display: inline-flex; align-items: center; gap: 10px; }
.who > span:last-child { display: grid; }
.uname { font-weight: 400; }
.stafftable td { white-space: nowrap; } .stafftable td[data-col=posts] { white-space: normal; }
.stafftable tr.off td { color: var(--muted); }
.av { width: 36px; height: 36px; border-radius: 50%; display: inline-flex; align-items: center; justify-content: center; color: #fff; font-weight: 700; flex: none; }
.post { display: inline-block; padding: 2px 9px; border-radius: 6px; font-size: 12.5px; font-weight: 600; margin: 1px 4px 1px 0; }
.post.station { background: #e7eefb; color: #2556a8; } .post.packing { background: #fbecc8; color: #8a5a00; }
.post.rider { background: var(--pink, var(--brand-soft)); color: var(--brand); } .post.none { background: #f3ebdf; color: var(--muted); font-weight: 500; }
.drawer-mask { position: fixed; inset: 0; background: rgba(0,0,0,.35); z-index: 40; }
.drawer { position: fixed; top: 0; inset-inline-end: 0; bottom: 0; width: min(460px, 100vw); background: var(--card, #fff); z-index: 41; box-shadow: -8px 0 24px rgba(0,0,0,.15);
  padding: 18px 20px; display: flex; flex-direction: column; gap: 12px; overflow-y: auto; }
.two { display: grid; grid-template-columns: 1fr 1fr; gap: 0 10px; } .two .field { margin-bottom: 8px; }
.grp { border: 1px solid var(--line); border-radius: 12px; padding: 10px 12px 12px; display: grid; gap: 8px; }
.grp .gh b { margin-inline-end: 6px; } .grp.locked { opacity: .55; }
.opts { display: flex; flex-wrap: wrap; gap: 8px; }
.opt { border: 1.5px solid var(--line); border-radius: 10px; padding: 7px 12px; display: flex; gap: 7px; align-items: center; font-size: 14px; background: #fff; cursor: pointer; }
.opt i { width: 16px; height: 16px; border: 1.5px solid #9ca3af; border-radius: 4px; flex: none; position: relative; } .opt.radio i { border-radius: 50%; }
.opt.on { border-color: var(--brand); background: var(--brand-soft); font-weight: 600; } .opt.on i { background: var(--brand); border-color: var(--brand); }
.opt.on i::after { content: ""; position: absolute; inset-inline-start: 4.5px; top: 1.5px; width: 4px; height: 8px; border: solid #fff; border-width: 0 2px 2px 0; transform: rotate(45deg); }
.permsum { background: #f6f6f4; border-radius: 10px; padding: 10px 12px; font-size: 13px; display: grid; gap: 4px; }
.permsum .y { color: var(--ok); } .permsum .n { color: #8b949e; }
.foot { margin-top: auto; display: flex; gap: 8px; align-items: center; padding-top: 6px; }
</style>
