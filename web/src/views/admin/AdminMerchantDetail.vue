<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { del, get, patch, post, put } from '../../api'
import { money, t } from '../../i18n'
import { setAdminView } from '../../session'
import MerchantEdit from './MerchantEdit.vue'
import { deleteMerchant, restoreMerchant, resumeMerchant, suspendMerchant } from './merchantActions'
import type { AdminBranch, MerchantDetail, MerchantRow, MerchantUser } from './types'
import { ago, fmtDate, fmtDateTime } from './util'

const props = defineProps<{ id: string }>()
const router = useRouter()
const d = ref<MerchantDetail | null>(null)
const error = ref('')
const editing = ref(false)
const m = computed(() => d.value?.merchant)
const live = computed(() => m.value?.status !== 'deleted')

async function load() {
  try { d.value = await get<MerchantDetail>(`/admin/merchants/${props.id}`); error.value = '' } catch (e) { error.value = (e as Error).message }
}
onMounted(load)

async function run(fn: () => Promise<unknown>) {
  error.value = ''
  try { await fn(); await load(); return true } catch (e) { error.value = (e as Error).message; return false }
}
const act = (fn: (m: MerchantRow) => Promise<boolean>) => run(() => fn(m.value!))
async function remove() {
  error.value = ''
  try { if (await deleteMerchant(m.value!)) router.replace('/admin/merchants') } catch (e) { error.value = (e as Error).message }
}

const today = computed(() => { const o = d.value?.stats.orders_today ?? {}; return (o.delivery ?? 0) + (o.pickup ?? 0) + (o.dine_in ?? 0) })
const completion = computed(() => { const s = d.value?.stats; return s && s.orders_7d ? Math.round(s.completed_7d * 100 / s.orders_7d) : 0 })

// ---- stores ----
const blankBranch = () => ({ id: 0, name: '', name_en: '', slug: '', timezone: 'Asia/Manila', currency: 'PHP', address: '', phone: '', oldSlug: '' })
const br = reactive({ open: false, error: '', f: blankBranch() })
function editBranch(b: AdminBranch | null) {
  br.f = b ? { id: b.id, name: b.name, name_en: b.name_en, slug: b.slug, timezone: b.timezone, currency: b.currency, address: b.address, phone: b.phone, oldSlug: b.slug } : blankBranch()
  br.error = ''; br.open = true
}
async function saveBranch() {
  const { id, oldSlug, ...body } = br.f
  br.error = ''
  try {
    await (id ? put(`/admin/branches/${id}`, body) : post(`/admin/merchants/${props.id}/branches`, body))
    br.open = false; await load()
  } catch (e) { br.error = (e as Error).message }
}
function removeBranch(b: AdminBranch) { if (confirm(t('ad.delete_branch_confirm', { name: b.name }))) void run(() => del(`/admin/branches/${b.id}`)) }
function viewStore(b: AdminBranch) { setAdminView(b); location.href = '/merchant/orders' }
const branchState = (b: AdminBranch) => b.paused ? ['warn', 'ad.b.paused'] : b.open_now ? ['ok', 'ad.b.open'] : ['', 'ad.b.closed']

// ---- accounts ----
const acct = reactive({ open: false, error: '', f: { branch_id: 0, username: '', display_name: '', password: '', role: 'owner' } })
function newAccount() { Object.assign(acct, { open: true, error: '', f: { branch_id: d.value?.branches[0]?.id ?? 0, username: '', display_name: '', password: '', role: 'owner' } }) }
async function saveAccount() {
  const { branch_id, ...body } = acct.f
  acct.error = ''
  try { await post(`/admin/branches/${branch_id}/users`, body); acct.open = false; await load() } catch (e) { acct.error = (e as Error).message }
}
const fresh = ref<{ name: string; password: string } | null>(null)
async function resetPassword(u: MerchantUser) {
  if (!confirm(t('ad.reset_confirm', { name: u.username }))) return
  error.value = ''
  try {
    const out = await post<{ password: string }>(`/admin/users/${u.id}/reset-password`)
    fresh.value = { name: u.username, password: out.password }
  } catch (e) { error.value = (e as Error).message }
}
const setActive = (u: MerchantUser, active: boolean) => run(() => patch(`/admin/users/${u.id}`, { active }))
function storesOf(u: MerchantUser) {
  const n = d.value?.branches.length ?? 0
  return n > 1 && u.roles.length === n ? t('ad.all_branches_n', { n }) : u.roles.map(r => r.branch_name).join('、')
}
const rolesOf = (u: MerchantUser) => [...new Set(u.roles.map(r => r.role))]
</script>

<template>
  <div class="adm-page">
    <div class="row">
      <router-link to="/admin/merchants">‹ {{ t('ad.back_merchants') }}</router-link><span class="grow"></span>
      <template v-if="m">
        <button v-if="m.status === 'active'" class="btn" data-testid="suspend" @click="act(suspendMerchant)">{{ t('ad.suspend_merchant') }}</button>
        <button v-if="m.status === 'suspended'" class="btn" data-testid="resume" @click="act(resumeMerchant)">{{ t('ad.resume_merchant') }}</button>
        <button v-if="m.status === 'suspended'" class="btn danger" data-testid="delete" @click="remove">{{ t('delete') }}</button>
        <button v-if="m.status === 'deleted'" class="btn" @click="act(restoreMerchant)">{{ t('ad.restore') }}</button>
      </template>
    </div>
    <div v-if="error" class="banner bad">{{ error }}</div>
    <div v-if="fresh" class="banner ok" data-testid="new-password">
      {{ t('ad.new_password') }} · {{ fresh.name }}: <b class="mono" style="font-size:15px;user-select:all">{{ fresh.password }}</b>
      <span class="small"> · {{ t('ad.new_password_hint') }}</span>
      <button class="btn small" style="margin-left:8px" @click="fresh = null">{{ t('back') }}</button>
    </div>

    <template v-if="d && m">
      <div class="adm-grid2" style="grid-template-columns:1fr 1.4fr">
        <div class="card">
          <div class="head"><h2>{{ t('ad.basic') }}</h2><button v-if="live" class="btn small" @click="editing = true">{{ t('edit') }}</button></div>
          <dl class="kv">
            <dt>{{ t('merchant_name') }}</dt><dd class="bold" data-testid="merchant-name">{{ m.name }}</dd>
            <dt>{{ t('ad.status') }}</dt><dd><span class="tag" data-testid="merchant-status" :class="m.status === 'active' ? 'ok' : m.status === 'suspended' ? 'warn' : ''">{{ t('ad.st.' + m.status) }}</span>
              <span v-if="m.status === 'suspended'" class="small muted"> {{ fmtDateTime(m.suspended_at) }} {{ m.suspend_reason }}</span>
              <span v-if="m.status === 'deleted'" class="small muted"> {{ fmtDateTime(m.deleted_at) }}</span></dd>
            <dt>{{ t('ad.contact') }}</dt><dd>{{ m.contact_name || '—' }}</dd>
            <dt>{{ t('ad.contact_phone') }}</dt><dd class="num">{{ m.contact_phone || '—' }}</dd>
            <dt>{{ t('ad.joined') }}</dt><dd class="num">{{ fmtDate(m.created_at) }}</dd>
            <dt>{{ t('ad.note') }}</dt><dd class="muted">{{ m.note || '—' }}</dd>
          </dl>
        </div>
        <div class="adm-grid3 stat">
          <div class="card"><div class="muted small">{{ t('ad.orders_today') }}</div><div class="v">{{ today }}</div>
            <div class="small muted">{{ t('delivery') }} {{ d.stats.orders_today.delivery ?? 0 }} · {{ t('pickup') }} {{ d.stats.orders_today.pickup ?? 0 }} · {{ t('dine_in') }} {{ d.stats.orders_today.dine_in ?? 0 }}</div></div>
          <div class="card"><div class="muted small">{{ t('ad.orders_7d') }}</div><div class="v">{{ d.stats.orders_7d }}</div>
            <div class="small muted">{{ t('ad.completion', { p: completion }) }}</div></div>
          <div class="card"><div class="muted small">{{ t('ad.revenue_7d') }}</div>
            <div class="v"><template v-if="d.stats.revenue_7d.length"><div v-for="r in d.stats.revenue_7d" :key="r.currency">{{ money(r.amount, r.currency) }}</div></template><template v-else>—</template></div>
            <div class="small muted">{{ t('ad.revenue_hint') }}</div></div>
        </div>
      </div>

      <div class="card flush">
        <div class="head"><h2>{{ t('ad.branches') }}（{{ d.branches.length }}）</h2>
          <button v-if="live" class="btn small primary" data-testid="new-branch" @click="editBranch(null)">＋ {{ t('ad.new_branch') }}</button></div>
        <table class="data">
          <thead><tr><th>{{ t('ad.branch') }}</th><th>{{ t('ad.link') }}</th><th>{{ t('ad.tz_cur') }}</th><th>{{ t('ad.status') }}</th>
            <th class="right">{{ t('ad.orders_today') }}</th><th class="right">{{ t('ad.items_on_sale') }}</th><th></th></tr></thead>
          <tbody>
            <tr v-for="b in d.branches" :key="b.id" :data-branch="b.slug">
              <td class="bold">{{ b.name }}<div class="small muted" style="font-weight:400">{{ b.name_en }}</div></td>
              <td class="mono"><a :href="'/s/' + b.slug" target="_blank">/s/{{ b.slug }}</a></td>
              <td>{{ b.timezone }} · {{ b.currency }}</td>
              <td><span class="tag" :class="branchState(b)[0]">{{ t(branchState(b)[1]) }}</span></td>
              <td class="right num">{{ b.orders_today }}</td><td class="right num">{{ b.items_on_sale }}</td>
              <td><div class="acts" v-if="live">
                <button class="btn small" @click="editBranch(b)">{{ t('edit') }}</button>
                <router-link class="btn small" :to="{ path: '/admin/items', query: { merchant_id: b.merchant_id, branch_id: b.id } }">{{ t('ad.items') }}</router-link>
                <button class="btn small" data-testid="view-store" @click="viewStore(b)">{{ t('ad.view_store') }} ↗</button>
                <button class="btn small danger" @click="removeBranch(b)">{{ t('delete') }}</button>
              </div></td>
            </tr>
            <tr v-if="d.branches.length === 0"><td colspan="7" class="muted">{{ t('ad.empty') }}</td></tr>
          </tbody>
        </table>
      </div>

      <div class="card flush">
        <div class="head"><h2>{{ t('ad.accounts') }}（{{ d.users.length }}）</h2>
          <button v-if="live && d.branches.length" class="btn small primary" data-testid="new-account" @click="newAccount">＋ {{ t('ad.new_account') }}</button></div>
        <table class="data">
          <thead><tr><th>{{ t('username') }}</th><th>{{ t('display_name') }}</th><th>{{ t('ad.branch') }}</th><th>{{ t('role') }}</th><th>{{ t('ad.status') }}</th><th>{{ t('ad.last_login') }}</th><th></th></tr></thead>
          <tbody>
            <tr v-for="u in d.users" :key="u.id" :data-user="u.username">
              <td class="mono">{{ u.username }}</td><td>{{ u.display_name }}</td><td>{{ storesOf(u) }}</td>
              <td><span v-for="r in rolesOf(u)" :key="r" class="tag" :class="{ info: r === 'owner' }" style="margin-right:4px">{{ t('role.' + r) }}</span></td>
              <td><span class="tag" :class="{ ok: u.active }">{{ u.active ? t('enabled') : t('disabled') }}</span></td>
              <td class="num">{{ u.last_login_at ? ago(u.last_login_at) : t('ad.never') }}</td>
              <td><div class="acts" v-if="live">
                <template v-if="u.active">
                  <button class="btn small" data-testid="reset-password" @click="resetPassword(u)">{{ t('ad.reset_password') }}</button>
                  <button class="btn small danger" @click="setActive(u, false)">{{ t('disable') }}</button>
                </template>
                <button v-else class="btn small" @click="setActive(u, true)">{{ t('enable') }}</button>
              </div></td>
            </tr>
          </tbody>
        </table>
      </div>
      <div class="card flush" v-if="d.reports.length" data-testid="reports">
        <div class="head"><h2>{{ t('ad.reports') }}（{{ d.reports.length }}）</h2></div>
        <table class="data">
          <thead><tr><th>{{ t('ad.report_time') }}</th><th>{{ t('ad.branch') }}</th><th>{{ t('ad.report_reason') }}</th><th>{{ t('ad.report_detail') }}</th><th>{{ t('ad.report_contact') }}</th></tr></thead>
          <tbody>
            <tr v-for="r in d.reports" :key="r.id">
              <td class="num">{{ fmtDateTime(r.created_at) }}</td><td>{{ r.branch_name }}</td>
              <td><span class="tag warn">{{ t('rr.' + r.reason) }}</span></td><td style="white-space:pre-wrap">{{ r.detail || '—' }}</td><td>{{ r.contact || '—' }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <p class="small muted">{{ t('ad.detail_foot') }}</p>

      <MerchantEdit v-if="editing" :merchant="m" @close="editing = false" @saved="editing = false; load()" />

      <template v-if="br.open">
        <div class="adm-mask" @click="br.open = false"></div>
        <form class="adm-drawer" data-testid="branch-form" @submit.prevent="saveBranch">
          <div class="dh"><h2>{{ br.f.id ? t('ad.edit_branch') : t('ad.new_branch') }}</h2></div>
          <div class="db">
            <div v-if="br.error" class="banner bad" style="margin-bottom:12px">{{ br.error }}</div>
            <div class="f2">
              <label class="field"><span>{{ t('branch_name') }}</span><input v-model="br.f.name" name="name" required /></label>
              <label class="field"><span>{{ t('name_en') }}</span><input v-model="br.f.name_en" name="name_en" /></label>
            </div>
            <label class="field"><span>{{ t('ad.link') }} (a-z, 0-9, -)</span><input v-model="br.f.slug" name="slug" required pattern="[a-z0-9][a-z0-9\-]{1,40}" autocapitalize="none" /></label>
            <div v-if="br.f.id && br.f.slug !== br.f.oldSlug" class="banner warn" style="margin-bottom:12px">{{ t('ad.slug_change_warn') }}</div>
            <div class="f2">
              <label class="field"><span>{{ t('ad.timezone') }}</span><input v-model="br.f.timezone" name="timezone" required /></label>
              <label class="field"><span>{{ t('ad.currency') }}</span><input v-model="br.f.currency" name="currency" required maxlength="3" /></label>
            </div>
            <label class="field"><span>{{ t('address') }}</span><input v-model="br.f.address" name="address" /></label>
            <label class="field"><span>{{ t('phone') }}</span><input v-model="br.f.phone" name="phone" /></label>
          </div>
          <div class="df"><button type="button" class="btn" @click="br.open = false">{{ t('cancel') }}</button><button class="btn primary">{{ t('save') }}</button></div>
        </form>
      </template>

      <template v-if="acct.open">
        <div class="adm-mask" @click="acct.open = false"></div>
        <form class="adm-drawer" data-testid="account-form" @submit.prevent="saveAccount">
          <div class="dh"><h2>{{ t('ad.new_account') }}</h2></div>
          <div class="db">
            <div v-if="acct.error" class="banner bad" style="margin-bottom:12px">{{ acct.error }}</div>
            <div class="f2">
              <label class="field"><span>{{ t('ad.branch') }}</span><select v-model="acct.f.branch_id" name="branch_id">
                <option v-for="b in d.branches" :key="b.id" :value="b.id">{{ b.name }}</option></select></label>
              <label class="field"><span>{{ t('role') }}</span><select v-model="acct.f.role" name="role">
                <option v-for="r in ['owner', 'manager', 'cashier', 'kitchen', 'rider']" :key="r" :value="r">{{ t('role.' + r) }}</option></select></label>
              <label class="field"><span>{{ t('username') }}</span><input v-model="acct.f.username" name="username" required autocapitalize="none" /></label>
              <label class="field"><span>{{ t('display_name') }}</span><input v-model="acct.f.display_name" name="display_name" required /></label>
            </div>
            <label class="field"><span>{{ t('ad.initial_password') }}</span><input v-model="acct.f.password" name="password" type="password" required minlength="8" autocomplete="new-password" /></label>
          </div>
          <div class="df"><button type="button" class="btn" @click="acct.open = false">{{ t('cancel') }}</button><button class="btn primary">{{ t('create') }}</button></div>
        </form>
      </template>
    </template>
  </div>
</template>
