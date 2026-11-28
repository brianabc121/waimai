<script setup lang="ts">
// Refund by dish once the customer has the order (docs/VOID_ITEM_DESIGN.md
// §7): pick dishes, the amount follows the void rules, the money is given
// back outside the system and recorded here.
import { computed, reactive, ref, watch } from 'vue'
import { ApiError, newKey, post } from '../api'
import { creditErr, requestCreditCode } from '../credit'
import { money, t, toMinor } from '../i18n'
import { can } from '../session'
import type { Order, RefundQuote } from '../types'
import { customerReasons, kindOf, splitList, voidErr } from '../voids'
import ApproveDialog from './ApproveDialog.vue'
import '../stored'

const props = defineProps<{ order: Order; branchId: number }>()
const emit = defineEmits<{ (e: 'done', o: Order, amount: number): void; (e: 'close'): void }>()
const m = (v: number) => money(v, props.order.currency)

// what each line can still be refunded
const refunded = computed(() => {
  const n = new Map<number, number>()
  for (const r of props.order.refunds ?? []) for (const l of r.lines) n.set(l.item_id, (n.get(l.item_id) ?? 0) + l.qty)
  return n
})
const lines = computed(() => props.order.items.filter(i => i.prep_status !== 'cancelled')
  .map(i => ({ it: i, left: i.qty - (refunded.value.get(i.id) ?? 0) })).filter(l => l.left > 0))
const qty = reactive<Record<number, number>>({})
const picked = computed(() => lines.value.filter(l => (qty[l.it.id] ?? 0) > 0).map(l => ({ item_id: l.it.id, qty: qty[l.it.id] })))

const s = reactive({ reason: '', writing: false, text: '', kind: 'store' as 'store' | 'customer', method: '', amount: '', busy: false,
  // into the customer's store balance (docs/REFUND_BALANCE_DESIGN.md §6.3): how they agreed, or the code texted to them
  toCredit: false, consent: '' as '' | 'in_person' | 'phone' | 'chat', phone: '', code: '', codeID: 0, sentTo: '', needPhone: false })
// one refund, however many times it is pressed or retried
const idem = newKey()
const consents = ['in_person', 'phone', 'chat'] as const
const methods = splitList('vd.r.methods')
// the list is cash, transfer, e-wallet, other in every language
const payType = () => ['cash', 'transfer', 'online', 'other'][methods.indexOf(s.method)] ?? 'other'
s.method = methods[0] ?? ''
const reasons = [...customerReasons(), ...splitList('vd.reasons.store')]
const reason = computed(() => s.writing ? s.text.trim() : s.reason)
const quote = ref<RefundQuote | null>(null)
const error = ref('')
const approving = ref('')
const canCreditDest = computed(() => !!quote.value && props.order.order_type !== 'platform')
const needCode = computed(() => s.toCredit && !!quote.value && !quote.value.can_credit)

let seq = 0
async function dry(keepAmount: boolean) {
  const n = ++seq
  if (!picked.value.length) { quote.value = null; return }
  try {
    const typed = keepAmount && s.amount ? toMinor(s.amount) : 0
    const r = await post<{ quote: RefundQuote }>(`/merchant/orders/${props.order.id}/refunds`,
      { lines: picked.value, reason_kind: s.kind, dry: true, amount: typed > 0 ? typed : undefined })
    if (n === seq) { quote.value = r.quote; if (!keepAmount) s.amount = ''; error.value = '' }
  } catch (e) { if (n === seq) { quote.value = null; error.value = voidErr(e) } }
}
watch([picked, () => s.kind], () => dry(false), { deep: true })
// the split between the money paid and the balance follows the amount typed
let timer = 0
watch(() => s.amount, () => { clearTimeout(timer); timer = window.setTimeout(() => dry(true), 300) })

const amount = computed(() => s.amount ? toMinor(s.amount) : quote.value?.suggested ?? 0)
const tooMuch = computed(() => !!quote.value && amount.value > quote.value.suggested)
function tap(r: string) { s.reason = r; s.writing = false; s.kind = kindOf(r) }

const perm = computed(() => s.toCredit ? 'wallet.refund' : 'order.aftersale')
const ready = computed(() => !!quote.value && !!reason.value && !tooMuch.value && amount.value > 0 &&
  (!s.toCredit || (needCode.value ? s.codeID > 0 && /^\d{6}$/.test(s.code) : !!s.consent)))

async function sendCode() {
  error.value = ''
  try {
    const r = await requestCreditCode(props.order.id, 'refund', s.phone.trim())
    s.codeID = r.id; s.sentTo = r.phone; s.code = r.dev_code ?? ''
  } catch (e) {
    if (e instanceof ApiError && e.code === 'CREDIT_PHONE') s.needPhone = true
    error.value = creditErr(e)
  }
}

async function go(headers?: Record<string, string>) {
  if (!ready.value) return
  if (!headers && !can(perm.value)) { approving.value = perm.value; return }
  s.busy = true; error.value = ''
  try {
    const body: Record<string, unknown> = { lines: picked.value, amount: amount.value, reason: reason.value, reason_kind: s.kind }
    if (s.toCredit) {
      body.to_credit = true
      if (needCode.value) body.verify = { id: s.codeID, code: s.code }
      else body.consent = s.consent
    } else { body.method = s.method; body.pay_type = payType() }
    const r = await post<{ order: Order }>(`/merchant/orders/${props.order.id}/refunds`, body, { ...headers, 'Idempotency-Key': idem })
    emit('done', r.order, amount.value)
  } catch (e) {
    // above the store's limits: a manager (or the owner) approves this one
    if (e instanceof ApiError && (e.code === 'CREDIT_NEEDS_APPROVAL' || e.code === 'CREDIT_DAILY_LIMIT') && !headers) {
      error.value = creditErr(e); approving.value = 'wallet.refund'
    } else error.value = e instanceof ApiError && e.code.startsWith('CREDIT') || e instanceof ApiError && e.code.startsWith('OTP') ? creditErr(e) : voidErr(e)
  }
  s.busy = false
}
</script>

<template>
  <div class="vmask" @click.self="emit('close')">
    <div class="vsheet" data-testid="refund-sheet">
      <div class="vh"><h3>{{ t('vd.r.title', { no: order.public_no }) }}</h3><button class="x" @click="emit('close')">✕</button></div>
      <div class="vb">
        <div class="small muted">{{ t('vd.r.hint') }}</div>
        <div class="lbl">{{ t('vd.r.pick') }}</div>
        <div v-for="l in lines" :key="l.it.id" class="rl" data-testid="refund-line">
          <span class="grow">{{ l.it.name }} <span class="muted small">×{{ l.left }} · {{ m(l.it.unit_price) }}</span></span>
          <span class="stp"><button :disabled="!qty[l.it.id]" @click="qty[l.it.id] = (qty[l.it.id] ?? 0) - 1">−</button><b>{{ qty[l.it.id] ?? 0 }}</b>
            <button :disabled="(qty[l.it.id] ?? 0) >= l.left" data-testid="refund-more" @click="qty[l.it.id] = (qty[l.it.id] ?? 0) + 1">＋</button></span>
        </div>
        <div class="lbl">{{ t('vd.reason') }} <span class="req">{{ t('vd.required') }}</span></div>
        <div class="chs">
          <button v-for="x in reasons" :key="x" class="ch" :class="{ on: !s.writing && s.reason === x }" data-testid="refund-reason" @click="tap(x)">{{ x }}</button>
          <button v-if="!s.writing" class="ch add" @click="s.writing = true">{{ t('vd.write') }}</button>
          <input v-else v-model="s.text" class="write" maxlength="100" :placeholder="t('vd.reason')" />
        </div>
        <div class="kinds"><button v-for="k in (['store', 'customer'] as const)" :key="k" class="kd" :class="{ on: s.kind === k }" @click="s.kind = k">{{ t('vd.kind.' + k) }}</button>
          <span class="small muted">{{ t('vd.kind_hint.' + s.kind) }}</span></div>
        <template v-if="canCreditDest">
          <div class="lbl">{{ t('sc.r.dest') }}</div>
          <div class="chs">
            <button class="ch" :class="{ on: !s.toCredit }" data-testid="refund-to-money" @click="s.toCredit = false">{{ t('sc.r.to_money') }}</button>
            <button class="ch" :class="{ on: s.toCredit }" data-testid="refund-to-credit" @click="s.toCredit = true">{{ !can('wallet.refund') ? '🔒 ' : '' }}{{ t('sc.r.to_credit') }}</button>
          </div>
        </template>
        <template v-if="!s.toCredit">
          <div class="lbl">{{ t('vd.r.method') }}</div>
          <div class="chs"><button v-for="x in methods" :key="x" class="ch" :class="{ on: s.method === x }" @click="s.method = x">{{ x }}</button></div>
        </template>
        <template v-else>
          <div class="small muted">{{ t('sc.r.credit_hint') }}</div>
          <template v-if="!needCode">
            <div class="lbl">{{ t('sc.r.consent') }} <span class="req">{{ t('vd.required') }}</span></div>
            <div class="chs"><button v-for="c in consents" :key="c" class="ch" :class="{ on: s.consent === c }" data-testid="refund-consent" @click="s.consent = c">{{ t('sc.r.c.' + c) }}</button></div>
          </template>
          <div v-else class="stack code" data-testid="refund-code">
            <div class="small">{{ t('sc.r.code_need') }}</div>
            <label v-if="s.needPhone || !order.customer_phone" class="field" style="margin:0"><span>{{ t('sc.r.phone') }}</span>
              <input v-model="s.phone" inputmode="tel" data-testid="refund-code-phone" /></label>
            <div class="row"><button class="btn small" data-testid="refund-code-send" @click="sendCode">{{ t(s.codeID ? 'sc.r.resend' : 'sc.r.send') }}</button>
              <span v-if="s.sentTo" class="small muted">{{ t('sc.r.sent', { p: s.sentTo }) }}</span></div>
            <label v-if="s.codeID" class="field" style="margin:0"><span>{{ t('sc.r.code') }}</span>
              <input v-model="s.code" inputmode="numeric" maxlength="6" data-testid="refund-code-input" /></label>
          </div>
        </template>
        <template v-if="quote">
          <div v-if="quote.received != null && quote.received < order.totals.total" class="banner warn small" data-testid="refund-received">
            {{ t('vd.r.received', { t: m(order.totals.total), r: m(quote.received) }) }}</div>
          <label class="field" style="margin:0"><span>{{ t('vd.r.amount') }} · {{ t('vd.r.suggested', { s: m(quote.suggested) }) }}</span>
            <input v-model="s.amount" inputmode="decimal" :placeholder="(quote.suggested / 100).toFixed(2)" data-testid="refund-amount" /></label>
          <div v-if="s.toCredit" class="banner info small" data-testid="refund-split">{{ t('sc.r.split_credit', { s: m(amount) }) }}</div>
          <div v-else-if="quote.credit_paid && quote.credit_part" class="banner info small" data-testid="refund-split">
            {{ t('sc.r.split', { cash: m(quote.cash_part ?? 0), credit: m(quote.credit_part) }) }}</div>
          <div v-if="quote.stored_part" class="banner info small" data-testid="refund-stored">{{ t('sv.r.split', { s: m(quote.stored_part) }) }}</div>
        </template>
        <div v-if="error" class="banner bad small">{{ error }}</div>
      </div>
      <div class="vf">
        <button class="btn" @click="emit('close')">{{ t('vd.cancel') }}</button>
        <button class="btn primary grow" :disabled="s.busy || !ready" data-testid="refund-ok" @click="go()">
          {{ !can(perm) ? '🔒 ' : '' }}{{ t(s.toCredit ? 'sc.r.ok' : 'vd.r.ok', { s: m(amount) }) }}</button>
      </div>
    </div>
    <ApproveDialog v-if="approving" :branch-id="branchId" :perm="approving" :what="t(approving === 'wallet.refund' ? 'sc.r.approve' : 'vd.r.btn')"
      @approved="tok => { approving = ''; go({ 'X-Approval': tok }) }" @close="approving = ''" />
  </div>
</template>

<style scoped>
.vmask { position: fixed; inset: 0; background: rgba(0,0,0,.4); z-index: 30; display: flex; align-items: center; justify-content: center; padding: 16px; }
.vsheet { background: var(--card, #fff); border-radius: 14px; width: min(480px, 100%); max-height: calc(100vh - 32px); display: flex; flex-direction: column; overflow: hidden; }
.vh { display: flex; align-items: center; gap: 8px; padding: 14px 16px 8px; }
.vh h3 { margin: 0; font-size: 17px; flex: 1; min-width: 0; overflow-wrap: anywhere; }
.x { background: none; border: 0; font-size: 18px; cursor: pointer; color: var(--muted); }
.vb { padding: 0 16px 12px; overflow-y: auto; display: flex; flex-direction: column; gap: 10px; }
.lbl { font-size: 13px; color: var(--muted); margin-top: 4px; }
.req { color: var(--bad, #c0392b); font-size: 12px; }
.rl { display: flex; align-items: center; gap: 10px; }
.grow { flex: 1; min-width: 0; }
.stp { display: inline-flex; align-items: center; gap: 10px; }
.stp button { width: 34px; height: 34px; border-radius: 8px; border: 1px solid var(--line); background: #fff; font-size: 18px; cursor: pointer; }
.stp b { min-width: 18px; text-align: center; }
.chs { display: flex; flex-wrap: wrap; gap: 8px; }
.ch { border: 1px solid var(--line); background: #fff; border-radius: 10px; padding: 7px 12px; cursor: pointer; font-size: 14px; }
.ch.on { border-color: var(--brand); background: var(--brand-soft); box-shadow: 0 0 0 1px var(--brand); }
.ch.add { border-style: dashed; color: var(--muted); }
.write { flex: 1 1 160px; min-width: 0; }
.kinds { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; }
.kd { border: 1px solid var(--line); background: #fff; border-radius: 999px; padding: 4px 10px; font-size: 12.5px; cursor: pointer; }
.kd.on { border-color: var(--brand); color: var(--brand); font-weight: 600; }
.code { gap: 8px; } .code .row { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.vf { display: flex; gap: 10px; padding: 12px 16px calc(12px + env(safe-area-inset-bottom)); border-top: 1px solid var(--line); }
.vf .btn { min-height: 44px; }
</style>
