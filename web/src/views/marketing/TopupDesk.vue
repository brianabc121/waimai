<script setup lang="ts">
// 点单 → 会员充值 (docs/STORED_VALUE_DESIGN.md §6): the counter takes the
// money for a top-up, either the code the customer made on their phone, or
// a top-up the staff start by phone number, which the customer confirms with
// a texted code. Nothing goes in before the staff say the money is here.
import { computed, onMounted, reactive, ref } from 'vue'
import { ApiError, get } from '../../api'
import { money, t, toMinor, whenText } from '../../i18n'
import { session } from '../../session'
import type { PayChoice, PayOptions } from '../../types'
import '../../officeStrings'
import '../../checkout'
import PayPicker from '../checkout/PayPicker.vue'
import ApproveDialog from '../ApproveDialog.vue'
import {
  branchTopups, confirmTopup, startStaffTopup, storedErr, storedOverview, tierCoupon,
  type StoredCampaign, type StoredOverview, type Topup,
} from '../../stored'

const bid = session.branchId!
const ov = ref<StoredOverview | null>(null)
const payOpts = ref<PayOptions | null>(null)
const today = ref<Topup[]>([])
const error = ref(''), done = ref('')
const running = computed<StoredCampaign | null>(() => ov.value?.campaigns.find(c => c.state === 'running') ?? null)
const cur = computed(() => ov.value?.currency ?? 'PHP')
const m = (v: number) => money(v, cur.value)

const code = ref('')
const ph = reactive({ phone: '', tier: 0, amount: '', verifyID: 0, sentTo: '', otp: '' })
const sel = ref<Topup | null>(null)
const pay = ref<PayChoice>({ type: 'cash', channel_id: 0 })
const busy = ref(false), approving = ref(false)

async function loadToday() {
  const d = new Date().toDateString()
  today.value = (await branchTopups(bid).catch(() => [])).filter(x => new Date(x.paid_at ?? x.created_at).toDateString() === d)
}
onMounted(async () => {
  try {
    ov.value = await storedOverview(bid)
    payOpts.value = await get<PayOptions>(`/merchant/branches/${bid}/pay-options`)
  } catch (e) { error.value = storedErr(e) }
  await loadToday()
})

function reset() { sel.value = null; code.value = ''; Object.assign(ph, { phone: '', tier: 0, amount: '', verifyID: 0, sentTo: '', otp: '' }) }
async function find() {
  error.value = ''; done.value = ''; ph.verifyID = 0
  const l = await branchTopups(bid, { status: 'pending', code: code.value.trim() }).catch(e => { error.value = storedErr(e); return [] })
  sel.value = l[0] ?? null
  if (!sel.value && !error.value) error.value = t('sv.d.not_found')
}
async function send() {
  error.value = ''; done.value = ''; busy.value = true
  try {
    const r = await startStaffTopup(bid, { phone: ph.phone, tier_id: ph.tier || undefined, amount: ph.tier ? undefined : toMinor(ph.amount) })
    sel.value = r.topup; ph.verifyID = r.verify.id; ph.sentTo = r.verify.phone
    if (r.verify.dev_code) ph.otp = r.verify.dev_code
  } catch (e) { error.value = storedErr(e) } finally { busy.value = false }
}
async function confirm(headers?: Record<string, string>) {
  if (!sel.value) return
  error.value = ''; busy.value = true
  try {
    const x = await confirmTopup(bid, sel.value.id, { pay_type: pay.value.type, pay_channel_id: pay.value.channel_id,
      verify: sel.value.source === 'staff' ? { id: ph.verifyID, code: ph.otp } : undefined }, headers)
    done.value = t('sv.d.done', { p: x.phone, total: m(x.pay + x.bonus) })
    reset(); await loadToday()
  } catch (e) {
    if (e instanceof ApiError && e.code === 'STORED_NEEDS_APPROVAL') approving.value = true
    else error.value = storedErr(e)
  } finally { busy.value = false }
}
const ready = computed(() => !!sel.value && (sel.value.source !== 'staff' || ph.otp.length === 6) && (pay.value.type !== 'online' || !!pay.value.channel_id))
</script>

<template>
  <div class="ox warm">
  <div class="page ox-page" data-testid="topup-desk">
    <div class="ox-head"><div><h1>{{ t('sv.desk') }}</h1><p class="ox-sub">{{ t('sv.d.hint') }}</p></div></div>
    <div v-if="error" class="banner bad" data-testid="td-error">{{ error }}</div>
    <div v-if="done" class="banner ok" data-testid="td-done" @click="done = ''">{{ done }}</div>
    <div v-if="ov && !running" class="banner warn" data-testid="td-off">{{ t('sv.d.off') }}</div>

    <div class="ox-split">
      <section class="ox-panel stack">
        <h2>{{ t('sv.d.by_code') }}</h2>
        <form class="row" @submit.prevent="find">
          <input v-model="code" class="codein grow" inputmode="numeric" maxlength="6" :placeholder="t('sv.d.code_ph')" data-testid="td-code" />
          <button class="btn primary" :disabled="code.length !== 6" data-testid="td-find">{{ t('sv.d.find') }}</button>
        </form>
        <h2 style="margin-top:8px">{{ t('sv.d.by_phone') }}</h2>
        <label class="field" style="margin:0"><span>{{ t('sv.d.phone') }}</span><input v-model="ph.phone" inputmode="tel" data-testid="td-phone" /></label>
        <div v-if="running" class="tiers">
          <button v-for="x in running.tiers" :key="x.id" class="tb" :class="{ on: ph.tier === x.id }" data-testid="td-tier" @click="ph.tier = x.id">
            <b>{{ m(x.pay) }}</b><span>{{ t('sv.t.give', { amt: m(x.bonus) }) }}</span></button>
          <button v-if="running.custom_ok" class="tb" :class="{ on: !ph.tier }" @click="ph.tier = 0"><b>{{ t('sv.d.other') }}</b><span>≥ {{ m(running.custom_min) }}</span></button>
        </div>
        <input v-if="running?.custom_ok && !ph.tier" v-model="ph.amount" inputmode="decimal" :placeholder="t('sv.d.other')" />
        <div class="row">
          <button class="btn" :disabled="busy || !ph.phone || (!ph.tier && !ph.amount)" data-testid="td-send" @click="send">{{ t(ph.verifyID ? 'sc.r.resend' : 'sc.r.send') }}</button>
          <span v-if="ph.sentTo" class="small muted">{{ t('sc.r.sent', { p: ph.sentTo }) }}</span>
        </div>
      </section>

      <section class="ox-panel stack" data-testid="td-confirm">
        <h2>{{ t('sv.d.confirm') }}</h2>
        <div v-if="!sel" class="ox-empty">—</div>
        <template v-else>
          <div class="who"><span class="muted">{{ t('sv.c.customer') }}</span> <b>{{ sel.phone }}</b><span v-if="sel.brand_name" class="muted"> · {{ sel.brand_name }}</span></div>
          <div class="big" data-testid="td-get">{{ t('sv.d.get', { pay: m(sel.pay), total: m(sel.pay + sel.bonus) }) }}</div>
          <div v-if="sel.coupon_qty" class="small cp">{{ tierCoupon(sel, cur) }}</div>
          <label v-if="sel.source === 'staff'" class="field" style="margin:0"><span>{{ t('sc.r.code') }}</span>
            <input v-model="ph.otp" class="codein" inputmode="numeric" maxlength="6" data-testid="td-otp" /></label>
          <div class="small muted">{{ t('sv.c.way') }}</div>
          <PayPicker v-model="pay" :options="payOpts" />
          <button class="btn primary big-btn" :disabled="busy || !ready" data-testid="td-ok" @click="confirm()">{{ t('sv.d.ok', { amt: m(sel.pay) }) }}</button>
          <button class="btn link" @click="reset">{{ t('sv.e.cancel') }}</button>
        </template>
      </section>
    </div>

    <section class="ox-panel">
      <div class="ox-panel-h"><h2>{{ t('sv.d.today') }}</h2></div>
      <div v-if="!today.length" class="ox-empty">—</div>
      <table v-else class="data">
        <tbody>
          <tr v-for="x in today" :key="x.id" data-testid="td-row">
            <td class="small">{{ whenText(x.paid_at ?? x.created_at) }}</td><td>{{ x.phone }}</td>
            <td class="num"><span class="ox-money">{{ m(x.pay) }}</span> + {{ m(x.bonus) }}</td>
            <td>{{ x.pay_type ? (x.pay_type === 'online' ? x.pay_method : t('ck.pt.' + x.pay_type)) : '' }}</td>
            <td>{{ x.received_by }}</td><td><span class="tag">{{ t('sv.ts.' + x.status) }}</span></td>
          </tr>
        </tbody>
      </table>
    </section>
  </div>
  <ApproveDialog v-if="approving" :branch-id="bid" perm="stored.cashout" :what="t('sv.d.approve')"
    @approved="tok => { approving = false; confirm({ 'X-Approval': tok }) }" @close="approving = false" />
  </div>
</template>

<style scoped>
.stack { display: grid; gap: 10px; align-content: start; }
.stack h2 { margin: 0; font-size: 16px; }
.codein { font-size: 22px; letter-spacing: 6px; font-variant-numeric: tabular-nums; text-align: center; }
.tiers { display: grid; grid-template-columns: repeat(auto-fill, minmax(120px, 1fr)); gap: 8px; }
.tb { display: grid; gap: 2px; padding: 8px 10px; border: 1px solid var(--line); border-radius: 10px; background: #fff; text-align: start; cursor: pointer; font: inherit; color: inherit; }
.tb b { font-size: 16px; } .tb span { font-size: 12px; color: var(--brand, #a02c20); font-weight: 600; }
.tb.on { border: 2px solid var(--brand, #a02c20); background: #fdf6f4; }
.big { font-size: 20px; font-weight: 700; color: #2f1810; }
.cp { color: #7a3e00; }
.big-btn { min-height: 48px; font-size: 16px; }
</style>
