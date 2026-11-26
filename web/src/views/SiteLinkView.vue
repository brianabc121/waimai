<script setup lang="ts">
// "Store link": the store's own page, which shows this store only and never
// the mall, its QR code, the merchant's own domain and asking the platform
// to buy one (docs/ORDER_TYPES_DESIGN.md §5.3).
import QRCode from 'qrcode'
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { del, get, post } from '../api'
import { extend, intlTag, t } from '../i18n'
import '../officeStrings'
import { currentRole, session } from '../session'
import { site, storeURL } from '../site'

extend({
  'sl.intro': ['这是本店的专属页面：顾客打开后只看到本店，进不了商城，也看不到别的店。从这里下的单是网店订单（Online- 开头）。', 'Your store\'s own page: customers see this store only, with no way into the mall or to other stores. Orders placed here are online orders (Online-…).'],
  'sl.url': ['专属网址', 'Your page'],
  'sl.copy': ['复制链接', 'Copy link'],
  'sl.copied': ['已复制', 'Copied'],
  'sl.open': ['打开看看', 'Open'],
  'sl.qr': ['下载二维码', 'Download QR code'],
  'sl.no_domain': ['平台还没设置店铺网址的主域名，暂时用下面这个链接，顾客打开后进入商城里的本店页面。', 'The platform has not set the domain of store pages yet: this link opens your store inside the mall for now.'],
  'sl.own': ['用自己的域名', 'Use your own domain'],
  'sl.own_hint': ['已经有域名？填在这里，然后到域名商那里加一条 CNAME 记录，指向 {target}。系统每几分钟检查一次，生效后自动开通 HTTPS。', 'Have a domain? Enter it here, then add a CNAME record at your domain provider pointing to {target}. It is checked every few minutes and gets HTTPS by itself once it works.'],
  'sl.own_ph': ['例如 order.yourshop.com', 'e.g. order.yourshop.com'],
  'sl.add': ['添加', 'Add'],
  'sl.buy': ['请平台代买域名', 'Ask the platform to buy a domain'],
  'sl.buy_hint': ['没有域名？写下想要的域名（可以写 2–3 个备选），平台客服会联系你报价、代买和绑定。', 'No domain yet? Write the names you would like (2–3 choices are fine); the platform will contact you with a price, buy it and set it up.'],
  'sl.buy_ph': ['例如 yourshop.com、yourshop.ph', 'e.g. yourshop.com, yourshop.ph'],
  'sl.note_ph': ['备注（可不填）', 'Note (optional)'],
  'sl.send': ['提交申请', 'Send'],
  'sl.domains': ['我的域名', 'Your domains'],
  'sl.none': ['还没有', 'None yet'],
  'sl.remove': ['移除', 'Remove'],
  'sl.remove_confirm': ['移除 {d}？移除后这个域名就打不开本店了。', 'Remove {d}? It will no longer open your store.'],
  'sl.withdraw': ['撤回', 'Withdraw'],
  'sl.k.own': ['自有域名', 'Own domain'],
  'sl.k.buy': ['代买申请', 'Purchase request'],
  'sl.s.pending': ['等待解析生效', 'Waiting for DNS'],
  'sl.s.active': ['已生效', 'Working'],
  'sl.s.failed': ['未通过', 'Not accepted'],
  'sl.s.requested': ['待处理', 'Received'],
  'sl.s.quoted': ['已报价', 'Price sent'],
  'sl.s.bought': ['已购买', 'Bought'],
  'sl.s.rejected': ['已拒绝', 'Declined'],
  'sl.wanted': ['想要：{d}', 'Wanted: {d}'],
  'sl.platform_says': ['平台回复：{n}', 'Platform: {n}'],
})

interface Domain {
  id: number; kind: 'own' | 'buy'; domain: string; candidates: string; status: string; note: string
  admin_note: string; check_error: string; requested_at: string; verified_at: string | null
}
interface Links { store_no: number; url: string; store_domain: string; target: string; domains: Domain[] }

const bid = session.branchId!
const base = `/merchant/branches/${bid}`
const links = ref<Links | null>(null)
const error = ref('')
const copied = ref(false)
const qr = ref('')
const own = reactive({ domain: '', busy: false })
const buy = reactive({ candidates: '', note: '', busy: false })

const url = computed(() => links.value ? storeURL({ store_no: links.value.store_no, slug: currentRole()?.slug ?? '' }) : '')
const live = computed(() => !!site.store_domain || url.value.includes('.localhost'))
watch(url, async u => { qr.value = u ? await QRCode.toDataURL(u, { width: 480, margin: 1 }).catch(() => '') : '' })

async function load() { links.value = await get<Links>(`${base}/site`) }
onMounted(() => load().catch(e => { error.value = (e as Error).message }))

async function run(fn: () => Promise<unknown>) {
  error.value = ''
  try { await fn(); await load(); return true } catch (e) { error.value = (e as Error).message; return false }
}
async function copy() { try { await navigator.clipboard.writeText(url.value); copied.value = true; setTimeout(() => { copied.value = false }, 2000) } catch { /* shown anyway */ } }
async function addOwn() {
  own.busy = true
  if (await run(() => post(`${base}/domains`, { kind: 'own', domain: own.domain }))) own.domain = ''
  own.busy = false
}
async function askBuy() {
  buy.busy = true
  if (await run(() => post(`${base}/domains`, { kind: 'buy', candidates: buy.candidates, note: buy.note }))) { buy.candidates = ''; buy.note = '' }
  buy.busy = false
}
function remove(d: Domain) {
  if (d.kind === 'own' && !confirm(t('sl.remove_confirm', { d: d.domain }))) return
  void run(() => del(`${base}/domains/${d.id}`))
}
const day = (s: string) => new Date(s).toLocaleDateString(intlTag())
const stClass = (s: string) => s === 'active' ? 'ok' : s === 'failed' || s === 'rejected' ? 'bad' : 'warn'
</script>

<template>
  <div class="page ox-page stack" data-testid="site-link">
    <div class="ox-panel stack">
      <div class="ox-panel-h" style="margin:0"><h2>{{ t('sl.url') }}</h2></div>
      <p class="muted small" style="margin:0">{{ t('sl.intro') }}</p>
      <div v-if="error" class="banner bad">{{ error }}</div>
      <div v-if="links && !live" class="banner warn" data-testid="sl-no-domain">{{ t('sl.no_domain') }}</div>
      <div v-if="links" class="slmain">
        <img v-if="qr" :src="qr" alt="" class="slqr" data-testid="sl-qr" />
        <div class="stack" style="min-width:0">
          <a class="slurl mono" :href="url" target="_blank" rel="noopener" data-testid="sl-url">{{ url }}</a>
          <div class="row wrap" style="gap:8px">
            <button class="btn primary" data-testid="sl-copy" @click="copy">{{ copied ? t('sl.copied') : t('sl.copy') }}</button>
            <a class="btn" :href="url" target="_blank" rel="noopener">{{ t('sl.open') }}</a>
            <a v-if="qr" class="btn" :href="qr" :download="`store-${links.store_no}.png`">{{ t('sl.qr') }}</a>
          </div>
        </div>
      </div>
    </div>

    <div class="grid2">
      <form class="ox-panel stack" data-testid="sl-own" @submit.prevent="addOwn">
        <h2 style="margin:0">{{ t('sl.own') }}</h2>
        <p class="muted small" style="margin:0">{{ t('sl.own_hint', { target: links?.target || '—' }) }}</p>
        <div class="row" style="gap:8px">
          <input v-model.trim="own.domain" class="grow" :placeholder="t('sl.own_ph')" required data-testid="sl-own-input" />
          <button class="btn primary" :disabled="own.busy || !links?.target">{{ t('sl.add') }}</button>
        </div>
      </form>
      <form class="ox-panel stack" data-testid="sl-buy" @submit.prevent="askBuy">
        <h2 style="margin:0">{{ t('sl.buy') }}</h2>
        <p class="muted small" style="margin:0">{{ t('sl.buy_hint') }}</p>
        <input v-model.trim="buy.candidates" :placeholder="t('sl.buy_ph')" required maxlength="300" data-testid="sl-buy-input" />
        <input v-model.trim="buy.note" :placeholder="t('sl.note_ph')" maxlength="300" />
        <div class="row" style="justify-content:flex-end"><button class="btn primary" :disabled="buy.busy">{{ t('sl.send') }}</button></div>
      </form>
    </div>

    <div class="ox-panel stack">
      <h2 style="margin:0">{{ t('sl.domains') }}</h2>
      <div v-if="links && !links.domains.length" class="muted">{{ t('sl.none') }}</div>
      <div v-for="d in links?.domains ?? []" :key="d.id" class="sldom" data-testid="sl-domain">
        <div class="grow" style="min-width:0">
          <div class="row wrap" style="gap:8px;align-items:center">
            <b class="mono">{{ d.domain || t('sl.wanted', { d: d.candidates }) }}</b>
            <span class="tag" :class="stClass(d.status)" data-testid="sl-status">{{ t('sl.s.' + d.status) }}</span>
            <span class="muted small">{{ t('sl.k.' + d.kind) }} · {{ day(d.requested_at) }}</span>
          </div>
          <div v-if="d.status === 'pending' && d.check_error" class="small muted">{{ d.check_error }}</div>
          <div v-if="d.admin_note" class="small">{{ t('sl.platform_says', { n: d.admin_note }) }}</div>
        </div>
        <button class="btn small" @click="remove(d)">{{ d.kind === 'buy' && d.status !== 'active' ? t('sl.withdraw') : t('sl.remove') }}</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.grid2 { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; align-items: start; }
.slmain { display: flex; gap: 18px; align-items: center; flex-wrap: wrap; }
.slqr { width: 140px; height: 140px; border: 1px solid var(--line); border-radius: 8px; }
.slurl { font-size: 18px; font-weight: 700; word-break: break-all; }
.sldom { display: flex; gap: 10px; align-items: center; padding: 10px 0; border-top: 1px solid var(--line); }
.sldom:first-of-type { border-top: 0; }
</style>
