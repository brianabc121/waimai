<script setup lang="ts">
// Staff > Role permissions: the owner decides what order takers and managers
// may do in this store (docs/STAFF_ACCESS_DESIGN.md §6).
import { computed, onMounted, reactive, ref } from 'vue'
import { ApiError, get, post, put } from '../../api'
import { t, whenText } from '../../i18n'
import { isOwner, session } from '../../session'
import StaffTabs from './StaffTabs.vue'
import '../../perms'
import '../../credit'

interface Def { code: string; group: string; cashier: boolean; manager: boolean; locked_cashier?: boolean; requires?: string[] }
interface Table { catalog: Def[]; cashier: string[]; manager: string[]; cashier_count: number; manager_count: number; updated_by?: string; updated_at?: string }
type Role = 'cashier' | 'manager'

const bid = session.branchId!
const table = ref<Table | null>(null)
const cur = reactive<Record<Role, string[]>>({ cashier: [], manager: [] })
const error = ref(''), notice = ref(''), busy = ref(false)
const readOnly = computed(() => !!session.user?.is_platform_admin)

function set(tb: Table) { table.value = tb; cur.cashier = [...tb.cashier]; cur.manager = [...tb.manager] }
onMounted(() => get<Table>(`/merchant/branches/${bid}/role-perms`).then(set).catch(e => (error.value = (e as Error).message)))

const groups = computed(() => {
  const out: { group: string; defs: Def[] }[] = []
  for (const d of table.value?.catalog ?? []) {
    const g = out.find(x => x.group === d.group)
    g ? g.defs.push(d) : out.push({ group: d.group, defs: [d] })
  }
  return out
})
const def = (code: string) => table.value?.catalog.find(d => d.code === code)
const locked = (r: Role, d: Def) => r === 'cashier' && !!d.locked_cashier

// Ticking a permission ticks what it needs; unticking one unticks what needs it.
function toggle(r: Role, code: string) {
  const list = new Set(cur[r])
  const on = !list.has(code)
  const add = (c: string) => { if (list.has(c)) return; list.add(c); def(c)?.requires?.forEach(add) }
  const drop = (c: string) => {
    if (!list.has(c) || locked(r, def(c)!)) return
    list.delete(c)
    table.value?.catalog.filter(d => d.requires?.includes(c)).forEach(d => drop(d.code))
  }
  on ? add(code) : drop(code)
  cur[r] = table.value!.catalog.map(d => d.code).filter(c => list.has(c))
}
const changed = (r: Role, code: string) => !!table.value && table.value[r].includes(code) !== cur[r].includes(code)
const changes = computed(() => (['cashier', 'manager'] as Role[]).reduce((n, r) => n + (table.value?.catalog.filter(d => changed(r, d.code)).length ?? 0), 0))

function restoreDefaults() {
  if (!table.value) return
  cur.cashier = table.value.catalog.filter(d => d.cashier).map(d => d.code)
  cur.manager = table.value.catalog.filter(d => d.manager).map(d => d.code)
}
function discard() { if (table.value) set(table.value) }
async function save() {
  error.value = ''; notice.value = ''; busy.value = true
  try { set(await put<Table>(`/merchant/branches/${bid}/role-perms`, { cashier: cur.cashier, manager: cur.manager })); notice.value = t('rp.saved') }
  catch (e) { error.value = e instanceof ApiError ? e.message : String(e) }
  busy.value = false
}

// Copy to the other stores this person owns.
const others = computed(() => (session.user?.roles ?? []).filter(r => r.role === 'owner' && r.branch_id !== bid))
const copy = reactive({ open: false, to: [] as number[] })
async function doCopy() {
  error.value = ''; notice.value = ''
  try {
    await post(`/merchant/branches/${bid}/role-perms/copy`, { to: copy.to })
    notice.value = t('rp.copied', { n: copy.to.length }); copy.open = false
  } catch (e) { error.value = (e as Error).message }
}
</script>

<template>
  <div class="ox warm"><div class="page">
    <StaffTabs />
    <div class="row between wrap" style="margin-bottom:12px">
      <h1 style="font-size:20px">{{ t('rp.nav') }}</h1>
      <button v-if="others.length && !readOnly" class="btn small" data-testid="perm-copy" @click="copy.open = true; copy.to = []">{{ t('rp.copy') }}</button>
    </div>
    <div v-if="!isOwner()" class="banner warn">{{ t('rp.no_page') }}</div>
    <template v-else>
      <div class="banner info" style="margin-bottom:12px">{{ t('rp.intro') }}
        <span v-if="table?.updated_by" class="small"> {{ t('rp.last', { who: table.updated_by, at: whenText(table.updated_at!) }) }}</span></div>
      <div v-if="error" class="banner bad" style="margin-bottom:12px">{{ error }}</div>
      <div v-if="notice" class="banner ok" style="margin-bottom:12px" data-testid="perm-notice">{{ notice }}</div>

      <div v-if="table" class="card perm-card">
        <div class="perm-scroll">
          <table class="perm" data-testid="perm-table">
            <thead><tr><th>{{ t('rp.col.perm') }}</th>
              <th class="c">{{ t('rp.col.cashier', { n: table.cashier_count }) }}</th>
              <th class="c">{{ t('rp.col.manager', { n: table.manager_count }) }}</th>
              <th class="c">{{ t('rp.col.owner') }}</th></tr></thead>
            <tbody v-for="g in groups" :key="g.group">
              <tr class="grp"><td colspan="4">{{ t('rp.g.' + g.group) }}</td></tr>
              <tr v-for="d in g.defs" :key="d.code" :class="{ chg: changed('cashier', d.code) || changed('manager', d.code) }" :data-perm="d.code">
                <td>{{ t('rp.p.' + d.code) }}<span v-if="changed('cashier', d.code) || changed('manager', d.code)" class="chgdot"></span>
                  <div class="d">{{ t('rp.d.' + d.code) }}</div></td>
                <td v-for="r in (['cashier', 'manager'] as Role[])" :key="r" class="c">
                  <input type="checkbox" :checked="cur[r].includes(d.code)" :disabled="locked(r, d) || readOnly" :data-role="r" :aria-label="t('role.' + r) + ' · ' + t('rp.p.' + d.code)" @change="toggle(r, d.code)" />
                  <div v-if="locked(r, d)" class="lock">{{ t('rp.locked') }}</div></td>
                <td class="c"><input type="checkbox" checked disabled :aria-label="t('rp.col.owner')" /></td>
              </tr>
            </tbody>
            <tbody>
              <tr class="grp"><td colspan="4">{{ t('rp.owner_only') }}</td></tr>
              <tr><td colspan="4" class="muted small">{{ t('rp.owner_only_list') }}</td></tr>
            </tbody>
          </table>
        </div>
        <div v-if="!readOnly" class="perm-foot">
          <span class="grow small muted" data-testid="perm-changes"><template v-if="changes"><span class="chgdot" style="margin:0 6px 0 0"></span>{{ t('rp.changes', { n: changes }) }}</template></span>
          <button class="btn" data-testid="perm-reset" @click="restoreDefaults">{{ t('rp.reset') }}</button>
          <button class="btn" :disabled="!changes" @click="discard">{{ t('rp.discard') }}</button>
          <button class="btn primary" :disabled="!changes || busy" data-testid="perm-save" @click="save">{{ t('rp.save') }}</button>
        </div>
      </div>
    </template>

    <template v-if="copy.open">
      <div class="drawer-mask" @click="copy.open = false"></div>
      <div class="copybox card stack" data-testid="perm-copy-box">
        <h2>{{ t('rp.copy_title') }}</h2>
        <p class="muted small">{{ t('rp.copy_hint') }}</p>
        <label v-for="r in others" :key="r.branch_id" class="check"><input v-model="copy.to" type="checkbox" :value="r.branch_id" />{{ r.branch_name }}</label>
        <div class="row" style="justify-content:flex-end">
          <button class="btn" @click="copy.open = false">{{ t('rp.cancel') }}</button>
          <button class="btn primary" :disabled="!copy.to.length" @click="doCopy">{{ t('rp.copy_go') }}</button>
        </div>
      </div>
    </template>
  </div>
  </div>
</template>

<style scoped>
.perm-card { padding: 0; overflow: hidden; }
.perm-scroll { overflow-x: auto; }
table.perm { width: 100%; border-collapse: collapse; background: #fff; min-width: 520px; }
table.perm th, table.perm td { padding: 9px 12px; border-bottom: 1px solid var(--line); text-align: start; vertical-align: middle; }
table.perm thead th { background: #fafafa; font-size: 12.5px; color: var(--muted); }
table.perm th.c, table.perm td.c { text-align: center; width: 120px; }
table.perm tr.grp td { background: #f6f6f4; font-weight: 700; font-size: 13px; color: #3f4650; padding: 7px 12px; }
table.perm td .d { font-size: 12.5px; color: var(--muted); margin-top: 1px; }
table.perm tr.chg td { background: #fff8f1; }
table.perm input:disabled { opacity: .55; }
.lock { color: #8b949e; font-size: 12px; }
.chgdot { display: inline-block; width: 6px; height: 6px; border-radius: 50%; background: var(--brand); margin-inline-start: 6px; vertical-align: middle; }
.perm-foot { position: sticky; bottom: 0; background: #fff; border-top: 1px solid var(--line); padding: 12px 16px; display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }
.drawer-mask { position: fixed; inset: 0; background: rgba(0,0,0,.35); z-index: 40; }
.copybox { position: fixed; left: 50%; top: 20vh; transform: translateX(-50%); width: min(420px, calc(100vw - 32px)); z-index: 41; }
</style>
