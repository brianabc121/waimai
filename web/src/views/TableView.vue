<script setup lang="ts">
// Guest ordering at a table: a phone that scanned the table code (/t/:code)
// or a tablet paired with the table (/ipad). Both add to the same bill.
import LangButton from './LangButton.vue'
import { computed, ref } from 'vue'
import { ApiError, get, post } from '../api'
import { locale, setLocale, t } from '../i18n'
import { useLive } from '../live'
import { defaultTemplate, themeVars } from '../theme'
import type { BatchLine, Bill, Storefront, TableAccess } from '../types'
import BillLines from './BillLines.vue'
import DineMenu from './DineMenu.vue'
import TotalsTable from './TotalsTable.vue'

const props = defineProps<{ code?: string }>()
const ipad = !props.code
const via = ipad ? '?via=device' : ''

type Phase = 'loading' | 'ready' | 'idle' | 'not_open' | 'closed' | 'bad_code' | 'pair' | 'offline'
const phase = ref<Phase>('loading')
const access = ref<TableAccess | null>(null)
const bill = ref<Bill | null>(null)
const store = ref<Storefront | null>(null)
const tab = ref<'menu' | 'bill'>('menu')
const busy = ref(false)
const error = ref('')
const notice = ref('')
const menu = ref<InstanceType<typeof DineMenu> | null>(null)
let storeAt = 0
let entered = false // a phone exchanges its table code once per sitting

function setPhase(e: unknown) {
  const code = e instanceof ApiError ? e.code : ''
  if (code === 'TABLE_NOT_OPEN') phase.value = 'not_open'
  else if (code === 'TABLE_SESSION_CLOSED') { phase.value = entered || bill.value ? 'closed' : 'not_open'; bill.value = null }
  else if (code === 'DEVICE_NOT_PAIRED') phase.value = 'pair'
  else if (code === 'NOT_FOUND') phase.value = 'bad_code'
  else if (phase.value === 'loading') phase.value = 'offline'
}

async function refresh() {
  try {
    if (!ipad && !entered) {
      if (phase.value === 'closed') return // the next sitting needs a deliberate re-entry
      await post('/table-sessions/exchange', { code: props.code })
      entered = true
    }
    const r = await get<{ access: TableAccess; bill: Bill | null }>('/table-sessions/current' + via)
    access.value = r.access
    if (r.bill?.session_id !== bill.value?.session_id) { tab.value = 'menu'; notice.value = ''; error.value = '' } // a new sitting
    if (!bill.value || !r.bill || r.bill.session_id !== bill.value.session_id || r.bill.order.version >= bill.value.order.version) bill.value = r.bill
    document.title = `${r.access.store_name} · ${r.access.table_name}`
    // Re-read the menu now and then: sold-out marks and the published template can change mid-meal.
    if (!store.value || phase.value !== 'ready' || Date.now() - storeAt > (ipad ? 20000 : 60000)) {
      store.value = await get<Storefront>(`/stores/${r.access.slug}`); storeAt = Date.now()
    }
    phase.value = r.bill ? 'ready' : 'idle'
    if (!live.online.value) live.reconnect() // the stream was refused before this device had access
  } catch (e) {
    if (e instanceof ApiError && e.code === 'TABLE_SESSION_CLOSED' && !ipad && entered) {
      entered = false // the cookie belongs to a finished sitting
      phase.value = 'closed'; bill.value = null
      return
    }
    setPhase(e)
    throw e
  }
}
const live = useLive(() => '/table-sessions/current/events' + via, refresh)
const { online, refreshNow } = live

function reenter() { phase.value = 'loading'; void refreshNow() }

const pairCode = ref('')
async function pair() {
  error.value = ''
  try {
    await post('/table-devices/pair', { code: pairCode.value.trim().toUpperCase() })
    pairCode.value = ''; phase.value = 'loading'
    await refreshNow()
  } catch (e) {
    error.value = e instanceof ApiError && e.code === 'PAIR_CODE_INVALID' ? t('bad_pair_code') : (e as Error).message
  }
}

async function submit(lines: BatchLine[], expectedTotal: number, key: string) {
  busy.value = true; error.value = ''; notice.value = ''
  try {
    bill.value = await post<Bill>('/table-sessions/current/batches' + via, { items: lines, expected_total: expectedTotal }, { 'Idempotency-Key': key })
    menu.value?.clear()
    notice.value = t('sent_ok'); tab.value = 'bill'
  } catch (e) {
    if (e instanceof ApiError && e.code === 'QUOTE_CHANGED') { error.value = t('amount_changed'); store.value = await get<Storefront>(`/stores/${access.value!.slug}`) }
    else if (e instanceof ApiError && e.code === 'TABLE_SESSION_CLOSED') { await refreshNow() }
    else if (e instanceof ApiError && e.status === 0) error.value = t('submit_failed')
    else { error.value = (e as Error).message; if (access.value) store.value = await get<Storefront>(`/stores/${access.value.slug}`).catch(() => store.value) }
  }
  busy.value = false
}

const channel = ipad ? 'ipad' : 'qr'
const tpl = computed(() => store.value?.templates?.[channel] ?? defaultTemplate(channel))
const cur = computed(() => store.value?.branch.currency ?? 'PHP')
const liveCount = computed(() => bill.value?.order.items.filter(i => i.prep_status !== 'cancelled').reduce((n, i) => n + i.qty, 0) ?? 0)
</script>

<template>
  <div :class="ipad ? 'tablet' : 'phone wide'" :style="{ paddingBottom: 0, ...themeVars(tpl) }">
    <div class="topbar">
      <div class="row between">
        <h1 class="row" style="gap:8px"><img v-if="tpl.logo_url" :src="tpl.logo_url" alt="" class="logo" />{{ access?.store_name ?? t('dine_in') }}<span v-if="access"> · {{ t('table') }} {{ access.table_name }}</span></h1>
        <LangButton class="lang" />
      </div>
      <div class="seg" v-if="phase === 'ready'" style="margin-top:10px">
        <button :class="{ on: tab === 'menu' }" @click="tab = 'menu'">{{ t('menu') }}</button>
        <button :class="{ on: tab === 'bill' }" data-test="tab-bill" @click="tab = 'bill'">{{ t('ordered') }} · {{ liveCount }}</button>
      </div>
    </div>

    <div class="section" v-if="!online && phase === 'ready'"><div class="banner warn">{{ t('offline') }}</div></div>

    <div v-if="phase === 'loading'" class="section muted">{{ t('loading') }}</div>
    <div v-else-if="phase === 'offline'" class="section stack"><div class="banner bad">{{ t('offline') }}</div><button class="btn" @click="reenter">{{ t('retry') }}</button></div>
    <div v-else-if="phase === 'bad_code'" class="section"><div class="banner bad">{{ t('bad_table_code') }}</div></div>
    <div v-else-if="phase === 'not_open'" class="section stack">
      <div class="banner warn" data-test="not-open">{{ t('table_not_open') }}</div><button class="btn" @click="reenter">{{ t('retry') }}</button>
    </div>
    <div v-else-if="phase === 'closed'" class="section stack">
      <div class="banner ok" data-test="closed">{{ t('table_closed') }}</div><button class="btn" @click="reenter">{{ t('rescan') }}</button>
    </div>
    <div v-else-if="phase === 'pair'" class="section">
      <form class="card stack" style="max-width:420px;margin:40px auto" @submit.prevent="pair">
        <h2>{{ t('pair_tablet') }}</h2>
        <label class="field"><span>{{ t('enter_pair_code') }}</span>
          <input v-model="pairCode" name="pair-code" maxlength="12" autocomplete="off" style="font-size:22px;letter-spacing:4px;text-transform:uppercase" /></label>
        <div v-if="error" class="banner bad">{{ error }}</div>
        <button class="btn primary" :disabled="pairCode.trim().length < 4">{{ t('pair') }}</button>
      </form>
    </div>
    <div v-else-if="phase === 'idle'" class="section"><div class="card welcome" data-test="idle">{{ t('waiting_open') }}</div></div>

    <template v-else-if="store && bill">
      <div class="section" v-if="error" style="padding-bottom:0"><div class="banner bad">{{ error }}</div></div>
      <div v-show="tab === 'menu'">
        <img v-if="tpl.cover_url" :src="tpl.cover_url" alt="" class="cover" />
        <div class="section" v-if="store.branch.paused" style="padding-bottom:0"><div class="banner warn">{{ t('paused') }}</div></div>
        <DineMenu ref="menu" :categories="store.categories" :currency="cur" :busy="busy" :layout="tpl.layout"
          :storage-key="`table-cart:${ipad ? 'ipad' : code}:${bill.session_id}`" @submit="submit">
          <template #notice><div v-if="error" class="banner bad">{{ error }}</div></template>
        </DineMenu>
      </div>
      <div v-show="tab === 'bill'" class="section stack" style="max-width:720px;margin:0 auto">
        <div v-if="notice" class="banner ok" data-test="sent">{{ notice }}</div>
        <BillLines :order="bill.order" />
        <div class="card" v-if="bill.order.items.length"><TotalsTable :totals="bill.order.totals" :currency="cur" :delivery="false" /></div>
        <div class="banner info">{{ t('pay_at_counter') }}</div>
      </div>
    </template>
  </div>
</template>

<style scoped>
.tablet { min-height: 100vh; background: var(--bg); }
.welcome { max-width: 520px; margin: 60px auto; text-align: center; font-size: 20px; padding: 40px 24px; }
</style>
