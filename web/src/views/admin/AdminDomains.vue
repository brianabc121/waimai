<script setup lang="ts">
// Domains of the stores' own pages (docs/ORDER_TYPES_DESIGN.md §5.3): the
// purchase requests the platform handles by hand (quote, buy, put to use)
// and the merchants' own domains, which the worker checks by itself.
import { onMounted, reactive, ref } from 'vue'
import { get, put } from '../../api'
import { extend, t } from '../../i18n'
import { fmtDateTime } from './util'

extend({
  'ad.dom.hint': ['商家的专属页面网址：代买申请由平台联系商家报价、购买，买好后把域名的 CNAME 指向表里的“指向”，再改成“已生效”。商家自有域名由系统每几分钟自动检查，一般不用手动改。',
    'Addresses of the stores\' own pages. For a purchase request, contact the merchant with a price, buy the domain, point its CNAME to the target shown, then set it to working. Merchants\' own domains are checked automatically every few minutes.'],
  'ad.dom.no_site': ['还没设置“店铺网址的主域名”（系统设置 → 店铺专属页面），店铺专属页面和域名绑定都还不能用。', 'The domain of store pages is not set (Settings → Store pages): store pages and domains do not work yet.'],
  'ad.dom.f_open': ['待处理', 'Open'], 'ad.dom.f_all': ['全部', 'All'],
  'ad.dom.store': ['店铺', 'Store'], 'ad.dom.kind': ['类型', 'Kind'], 'ad.dom.domain': ['域名', 'Domain'],
  'ad.dom.target': ['指向', 'Target'], 'ad.dom.status': ['状态', 'Status'], 'ad.dom.at': ['申请时间', 'Asked'],
  'ad.dom.note': ['商家备注', 'Merchant note'], 'ad.dom.reply': ['回复商家', 'Reply to merchant'],
  'ad.dom.save': ['保存', 'Save'], 'ad.dom.none': ['没有', 'None'],
  'ad.dom.k.own': ['自有域名', 'Own domain'], 'ad.dom.k.buy': ['代买申请', 'Purchase'],
  'ad.dom.s.pending': ['等待解析', 'Waiting for DNS'], 'ad.dom.s.active': ['已生效', 'Working'], 'ad.dom.s.failed': ['未通过', 'Not accepted'],
  'ad.dom.s.requested': ['待处理', 'Received'], 'ad.dom.s.quoted': ['已报价', 'Price sent'], 'ad.dom.s.bought': ['已购买', 'Bought'], 'ad.dom.s.rejected': ['已拒绝', 'Declined'],
  'ad.dom.wanted': ['想要：{d}', 'Wanted: {d}'],
})

interface Domain {
  id: number; kind: 'own' | 'buy'; domain: string; candidates: string; status: string; note: string; admin_note: string
  check_error: string; requested_at: string; store_name: string; store_slug: string; store_no: number; target: string
}
const STATUSES = { own: ['pending', 'active', 'failed'], buy: ['requested', 'quoted', 'bought', 'active', 'rejected'] }

const list = ref<Domain[]>([])
const filter = ref<'open' | ''>('open')
const error = ref('')
const saved = ref(0)
const siteSet = ref(true)
const draft = reactive<Record<number, { status: string; domain: string; admin_note: string }>>({})

async function load() {
  error.value = ''
  try {
    const [d, s] = await Promise.all([get<{ domains: Domain[] }>('/admin/domains?status=' + filter.value), get<{ store_domain: string }>('/site')])
    list.value = d.domains; siteSet.value = !!s.store_domain
    for (const x of d.domains) draft[x.id] = { status: x.status, domain: x.domain, admin_note: x.admin_note }
  } catch (e) { error.value = (e as Error).message }
}
onMounted(load)
async function save(x: Domain) {
  error.value = ''; saved.value = 0
  try { const d = await put<Domain>(`/admin/domains/${x.id}`, draft[x.id]); Object.assign(x, d); saved.value = x.id } catch (e) { error.value = (e as Error).message }
}
function show(f: 'open' | '') { filter.value = f; void load() }
</script>

<template>
  <div class="adm-page">
    <p class="muted small" style="margin-top:0">{{ t('ad.dom.hint') }}</p>
    <div v-if="!siteSet" class="banner warn" data-testid="dom-no-site">{{ t('ad.dom.no_site') }}</div>
    <div v-if="error" class="banner bad">{{ error }}</div>
    <div class="row" style="gap:6px;margin-bottom:10px">
      <button class="btn small" :class="{ primary: filter === 'open' }" @click="show('open')">{{ t('ad.dom.f_open') }}</button>
      <button class="btn small" :class="{ primary: filter === '' }" @click="show('')">{{ t('ad.dom.f_all') }}</button>
    </div>
    <div class="card flush">
      <table class="data" data-testid="domains">
        <thead><tr><th>{{ t('ad.dom.store') }}</th><th>{{ t('ad.dom.kind') }}</th><th>{{ t('ad.dom.domain') }}</th><th>{{ t('ad.dom.target') }}</th>
          <th>{{ t('ad.dom.status') }}</th><th>{{ t('ad.dom.reply') }}</th><th></th></tr></thead>
        <tbody>
          <tr v-if="!list.length"><td colspan="7" class="muted">{{ t('ad.dom.none') }}</td></tr>
          <tr v-for="x in list" :key="x.id" :data-domain="x.id">
            <td><b>{{ x.store_name }}</b><div class="small muted mono">{{ x.store_no }} · {{ x.store_slug }}</div>
              <div class="small muted">{{ t('ad.dom.at') }} {{ fmtDateTime(x.requested_at) }}</div></td>
            <td>{{ t('ad.dom.k.' + x.kind) }}</td>
            <td>
              <div v-if="x.kind === 'buy'" class="small">{{ t('ad.dom.wanted', { d: x.candidates }) }}</div>
              <div v-if="x.note" class="small muted">{{ t('ad.dom.note') }}：{{ x.note }}</div>
              <input v-if="x.kind === 'buy'" v-model.trim="draft[x.id].domain" class="mono" placeholder="example.com" />
              <b v-else class="mono">{{ x.domain }}</b>
              <div v-if="x.check_error" class="small muted">{{ x.check_error }}</div>
            </td>
            <td class="mono small">{{ x.target || '—' }}</td>
            <td><select v-model="draft[x.id].status" :data-testid="'dom-status-' + x.id">
              <option v-for="s in STATUSES[x.kind]" :key="s" :value="s">{{ t('ad.dom.s.' + s) }}</option></select></td>
            <td><input v-model.trim="draft[x.id].admin_note" maxlength="500" /></td>
            <td><button class="btn small primary" @click="save(x)">{{ t('ad.dom.save') }}</button>
              <span v-if="saved === x.id" class="small" style="color:var(--ok)"> ✓</span></td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
