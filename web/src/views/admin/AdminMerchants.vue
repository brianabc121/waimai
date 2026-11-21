<script setup lang="ts">
import { onMounted, reactive, ref, watch } from 'vue'
import { get, post } from '../../api'
import { t } from '../../i18n'
import MerchantEdit from './MerchantEdit.vue'
import Pager from './Pager.vue'
import { deleteMerchant, restoreMerchant, resumeMerchant, suspendMerchant } from './merchantActions'
import type { MerchantRow } from './types'
import { fmtDate, fmtDateTime, qs } from './util'

const SIZE = 20
const q = ref('')
const status = ref('')
const page = ref(1)
const list = reactive<{ items: MerchantRow[]; total: number; counts: Record<string, number> }>({ items: [], total: 0, counts: {} })
const error = ref('')
const editing = ref<MerchantRow | null>(null)

async function load() {
  try {
    Object.assign(list, await get('/admin/merchants' + qs({ q: q.value, status: status.value, page: page.value, page_size: SIZE })))
    error.value = ''
  } catch (e) { error.value = (e as Error).message }
}
onMounted(load)
let timer = 0
watch(q, () => { clearTimeout(timer); timer = window.setTimeout(() => { page.value = 1; void load() }, 250) })
function tab(s: string) { status.value = s; page.value = 1; void load() }
function go(p: number) { page.value = p; void load() }

async function act(fn: (m: MerchantRow) => Promise<boolean>, m: MerchantRow) {
  error.value = ''
  try { if (await fn(m)) await load() } catch (e) { error.value = (e as Error).message }
}

// ---- new merchant ----
const blank = () => ({ merchant_name: '', contact_name: '', contact_phone: '', note: '', branch_name: '', branch_name_en: '', slug: '',
  timezone: 'Asia/Manila', currency: 'PHP', owner: { username: '', password: '', display_name: '' } })
const create = reactive({ open: false, error: '', f: blank() })
function openCreate() { Object.assign(create, { open: true, error: '', f: blank() }) }
async function submitCreate() {
  create.error = ''
  try { await post('/admin/merchants', create.f); create.open = false; tab('') } catch (e) { create.error = (e as Error).message }
}
const origin = location.origin
</script>

<template>
  <div class="adm-page">
    <div class="row wrap">
      <input v-model="q" :placeholder="t('ad.search_merchants')" style="width:300px" data-testid="merchant-search" />
      <div class="tabs">
        <button :class="{ on: status === '' }" @click="tab('')">{{ t('ad.all') }} {{ list.counts.all ?? 0 }}</button>
        <button :class="{ on: status === 'active' }" @click="tab('active')">{{ t('ad.st.active') }} {{ list.counts.active ?? 0 }}</button>
        <button :class="{ on: status === 'suspended' }" @click="tab('suspended')">{{ t('ad.st.suspended') }} {{ list.counts.suspended ?? 0 }}</button>
        <button :class="{ on: status === 'deleted' }" data-testid="tab-deleted" @click="tab('deleted')">{{ t('ad.st.deleted') }} {{ list.counts.deleted ?? 0 }}</button>
      </div>
      <span class="grow"></span>
      <button class="btn primary" data-testid="new-merchant" @click="openCreate">＋ {{ t('ad.new_merchant') }}</button>
    </div>
    <div v-if="error" class="banner bad">{{ error }}</div>

    <div class="card flush">
      <table class="data">
        <thead><tr><th>{{ t('ad.merchant') }}</th><th>{{ t('ad.contact') }}</th><th class="right">{{ t('ad.branches') }}</th><th>{{ t('ad.status') }}</th>
          <th class="right">{{ t('ad.orders_today') }}</th><th class="right">{{ t('ad.orders_7d') }}</th><th>{{ t('ad.joined') }}</th><th></th></tr></thead>
        <tbody>
          <tr v-for="m in list.items" :key="m.id" :data-merchant="m.name">
            <td><router-link :to="`/admin/merchants/${m.id}`" class="bold">{{ m.name }}</router-link>
              <div class="small muted mono">{{ m.slugs.length === 0 ? t('ad.no_branch') : m.slugs.length === 1 ? '/s/' + m.slugs[0] : t('ad.n_links', { slug: '/s/' + m.slugs[0], n: m.slugs.length }) }}</div></td>
            <td>{{ m.contact_name || '—' }}<div class="small muted num">{{ m.contact_phone }}</div></td>
            <td class="right num">{{ m.branch_count }}</td>
            <td><span class="tag" :class="m.status === 'active' ? 'ok' : m.status === 'suspended' ? 'warn' : ''">{{ t('ad.st.' + m.status) }}</span>
              <div v-if="m.status === 'suspended'" class="small muted">{{ fmtDateTime(m.suspended_at) }} {{ m.suspend_reason }}</div>
              <div v-if="m.status === 'deleted'" class="small muted">{{ fmtDateTime(m.deleted_at) }}</div></td>
            <td class="right num">{{ m.orders_today }}</td>
            <td class="right num">{{ m.orders_7d }}</td>
            <td class="num">{{ fmtDate(m.created_at) }}</td>
            <td><div class="acts">
              <template v-if="m.status === 'deleted'"><button class="btn small" @click="act(restoreMerchant, m)">{{ t('ad.restore') }}</button></template>
              <template v-else>
                <button class="btn small" @click="editing = m">{{ t('edit') }}</button>
                <button v-if="m.status === 'active'" class="btn small" @click="act(suspendMerchant, m)">{{ t('ad.suspend') }}</button>
                <template v-else>
                  <button class="btn small" @click="act(resumeMerchant, m)">{{ t('ad.resume') }}</button>
                  <button class="btn small danger" @click="act(deleteMerchant, m)">{{ t('delete') }}</button>
                </template>
              </template>
            </div></td>
          </tr>
          <tr v-if="list.items.length === 0"><td colspan="8" class="muted">{{ t('ad.empty') }}</td></tr>
        </tbody>
      </table>
    </div>
    <Pager :total="list.total" :page="page" :size="SIZE" @go="go" />

    <MerchantEdit v-if="editing" :merchant="editing" @close="editing = null" @saved="editing = null; load()" />

    <template v-if="create.open">
      <div class="adm-mask" @click="create.open = false"></div>
      <form class="adm-drawer" data-testid="merchant-create" @submit.prevent="submitCreate">
        <div class="dh"><h2>{{ t('ad.new_merchant') }}</h2><div class="small muted">{{ t('ad.new_merchant_hint') }}</div></div>
        <div class="db">
          <div v-if="create.error" class="banner bad" style="margin-bottom:12px">{{ create.error }}</div>
          <label class="field"><span>{{ t('merchant_name') }}</span><input v-model="create.f.merchant_name" name="merchant_name" required /></label>
          <div class="f2">
            <label class="field"><span>{{ t('ad.contact') }}</span><input v-model="create.f.contact_name" name="contact_name" /></label>
            <label class="field"><span>{{ t('ad.contact_phone') }}</span><input v-model="create.f.contact_phone" name="contact_phone" /></label>
          </div>
          <label class="field"><span>{{ t('ad.note_private') }}</span><input v-model="create.f.note" name="note" /></label>
          <h2 style="margin:14px 0 8px">{{ t('ad.first_branch') }}</h2>
          <div class="f2">
            <label class="field"><span>{{ t('branch_name') }}</span><input v-model="create.f.branch_name" name="branch_name" required /></label>
            <label class="field"><span>{{ t('name_en') }}</span><input v-model="create.f.branch_name_en" name="branch_name_en" /></label>
          </div>
          <label class="field"><span>{{ t('ad.link') }} (a-z, 0-9, -)</span><input v-model="create.f.slug" name="slug" required pattern="[a-z0-9][a-z0-9\-]{1,40}" autocapitalize="none" />
            <div class="small muted" style="margin-top:3px">{{ t('ad.slug_hint', { url: origin + '/s/' + (create.f.slug || '…') }) }}</div></label>
          <div class="f2">
            <label class="field"><span>{{ t('ad.timezone') }}</span><input v-model="create.f.timezone" name="timezone" required /></label>
            <label class="field"><span>{{ t('ad.currency') }}</span><input v-model="create.f.currency" name="currency" required maxlength="3" /></label>
          </div>
          <h2 style="margin:14px 0 8px">{{ t('owner_account') }}</h2>
          <div class="f2">
            <label class="field"><span>{{ t('username') }}</span><input v-model="create.f.owner.username" name="username" required autocapitalize="none" /></label>
            <label class="field"><span>{{ t('display_name') }}</span><input v-model="create.f.owner.display_name" name="display_name" required /></label>
          </div>
          <label class="field"><span>{{ t('ad.initial_password') }}</span><input v-model="create.f.owner.password" name="password" type="password" required minlength="8" autocomplete="new-password" /></label>
        </div>
        <div class="df"><button type="button" class="btn" @click="create.open = false">{{ t('cancel') }}</button><button class="btn primary">{{ t('create') }}</button></div>
      </form>
    </template>
  </div>
</template>
