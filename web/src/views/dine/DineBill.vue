<script setup lang="ts">
// A table's bill by round, what can be done to each dish (serve, one more,
// rush, void with a tapped reason), and taking payment with tapped amounts
// (docs/DINE_IN_ORDER_DESIGN.md §8, §9).
import { useNow } from '../../now'
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ApiError, get, post } from '../../api'
import { money, pick, t, toMinor } from '../../i18n'
import { can } from '../../session'
import { pctText, payText } from '../../checkout'
import type { Adjust, Bill, DinePresets, Item, Order, OrderItem, PayChoice, PayOptions, Totals } from '../../types'
import CreditSpend from '../CreditSpend.vue'
import '../../credit'
import '../../addresses'
import AdjustSheet from '../checkout/AdjustSheet.vue'
import PayPicker from '../checkout/PayPicker.vue'
import ApproveDialog from '../ApproveDialog.vue'
import type { DeskLine } from './DineOrder.vue'
import { presetList, sinceText } from './strings'
import VoidSheet from '../VoidSheet.vue'
import { session } from '../../session'
import { voidNote, voidsOf } from '../../voids'

const props = defineProps<{ bill: Bill; presets: DinePresets | null; currency: string; country: string; phone: boolean; payFirst: boolean; items?: Map<number, Item> }>()
const emit = defineEmits<{ back: []; update: [bill: Bill]; again: [line: DeskLine]; closed: []; add: [] }>()
const m = (v: number) => money(v, props.currency)
const now = useNow(30000)
const error = ref('')

// ---- rounds ----
const rounds = computed(() => {
  const by = new Map<number, OrderItem[]>()
  for (const it of props.bill.order.items) {
    if (!by.has(it.batch_seq)) by.set(it.batch_seq, [])
    by.get(it.batch_seq)!.push(it)
  }
  return [...by.entries()].sort((a, b) => a[0] - b[0]).map(([seq, items]) => ({ seq, items, first: items[0] }))
})
function source(it: OrderItem) {
  if (it.batch_source === 'staff') return it.batch_by ? t('dd.src.staff', { name: it.batch_by }) : t('dd.src.staff0')
  return t(`dd.src.${it.batch_source}`)
}
const clock = (iso?: string) => iso ? new Date(iso).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : ''
function st(it: OrderItem): [string, string] {
  if (it.prep_status === 'cancelled') return ['void', t('dd.st.void')]
  if (it.served_at) return ['served', t('dd.st.served')]
  if (it.prep_status === 'ready') return ['ready', t('dd.st.ready')]
  const n = it.batch_at ? Math.max(0, Math.floor((now.value - new Date(it.batch_at).getTime()) / 60000)) : 0
  return ['cook', it.batch_at ? t('dd.st.cook_m', { n }) : t('dd.st.cook')]
}
const detail = (it: OrderItem) => [...it.options.map(o => o.name), ...(it.note_tags ?? []).map(x => x.name), it.note].filter(Boolean).join(' · ')

const sel = ref<number | null>(null)
async function act(it: OrderItem, action: 'serve' | 'rush') {
  error.value = ''
  try { emit('update', await post<Bill>(`/merchant/bill-items/${it.id}/${action}`, {})) } catch (e) { error.value = (e as Error).message }
}
// Lines from before options kept their id are matched by name against the menu.
function again(it: OrderItem) {
  const menu = props.items?.get(it.item_id)
  const ids = it.options.map(o => o.option_id || menu?.groups.flatMap(g => g.options).find(x => x.name === o.name)?.id || 0).filter(Boolean)
  emit('again', { item_id: it.item_id, option_ids: ids, note_tags: (it.note_tags ?? []).map(x => x.id), note: it.note })
}

// ---- void (docs/VOID_ITEM_DESIGN.md §8.2): the shared sheet, with this store's reasons ----
const voiding = ref<OrderItem | null>(null)
const reasons = computed(() => props.presets?.void_reasons?.length ? presetList(props.presets, 'void_reasons') : undefined)
function startVoid(it: OrderItem) { voiding.value = it }
function voided(b: Bill) { voiding.value = null; sel.value = null; emit('update', b) }

// ---- payment ----
const paying = ref(props.payFirst || !props.phone)
const o = computed(() => props.bill.order)
const live = computed(() => o.value.items.filter(i => i.prep_status !== 'cancelled'))
const cooking = computed(() => live.value.filter(i => i.prep_status === 'pending' || i.prep_status === 'preparing').length)
// how the guests pay: cash, card or an online method the store takes (§6.1)
const payOpts = ref<PayOptions | null>(null)
onMounted(async () => {
  try { payOpts.value = await get<PayOptions>(`/merchant/branches/${o.value.branch_id}/pay-options`) } catch (e) { error.value = (e as Error).message }
})
const choice = ref<PayChoice>({ type: 'cash', channel_id: 0 })
const pay = reactive({ digits: '', manifest: 0, busy: false })
const cashLike = computed(() => choice.value.type === 'cash')
watch(() => choice.value.type, ty => { if (ty !== 'cash') pay.digits = '' })

// the discount at checkout (§5): none until someone sets it; without the
// permission a manager approves first
const adjust = ref<Adjust | null>(null)
const adjTotals = ref<Totals | null>(null)
const adjOpen = ref(false)
const approval = ref('')
const approving = ref(false)
const notice = ref('')
function openAdjust() {
  if (can('checkout.discount') || approval.value) adjOpen.value = true
  else approving.value = true
}
function approved(token: string, name: string) {
  approving.value = false; approval.value = token
  notice.value = t('ck.approved', { name }); adjOpen.value = true
}
const previewAdj = (a: Adjust) => post<Totals>(`/merchant/orders/${o.value.id}/adjust-preview`, a)
function setAdjust(a: Adjust | null, tot: Totals | null) {
  adjOpen.value = false
  adjust.value = a; adjTotals.value = a ? tot : null
}
const tot = computed(() => adjTotals.value ?? o.value.totals)
const adjLabel = computed(() => {
  const a = adjust.value, x = adjTotals.value
  if (!a || !x) return t('ck.adj_none')
  const parts: string[] = []
  if (a.percent_bp) parts.push(`${pctText(a.percent_bp)} −${m(x.staff_discount ?? 0)}`)
  if (a.mode === 'round') parts.push(`${t('ck.round_off')} −${m(x.round_off ?? 0)}`)
  if (a.mode === 'set_total') parts.push(a.amount === 0 ? t('ck.free') : `${t('ck.set_off')} −${m(x.round_off ?? 0)}`)
  return parts.join(' · ') + (a.reason ? ` · ${a.reason}` : '')
})

function resetPay() { pay.digits = ''; pay.manifest = o.value.manifest_version; adjust.value = null; adjTotals.value = null }
resetPay()
watch(() => o.value.manifest_version, v => { if (pay.manifest && v !== pay.manifest) { resetPay(); error.value = t('bill_changed') } })
// amounts are typed in major units; the bill is in minor units
// what is still collected: the bill less what the customer's store balance paid (docs/REFUND_BALANCE_DESIGN.md §5)
const total = computed(() => tot.value.total)
const due = computed(() => Math.max(0, tot.value.total - (o.value.credit_paid ?? 0)))
const spending = ref(false)
function spent(order: Order) { spending.value = false; emit('update', { ...props.bill, order }); resetPay() }
const received = computed(() => pay.digits ? toMinor(pay.digits) : due.value)
const change = computed(() => received.value - due.value)
const canClose = computed(() => !cooking.value && change.value >= 0 && !pay.busy && (choice.value.type !== 'online' || !!choice.value.channel_id))
// the quick amounts: round numbers just above the bill
const quick = computed(() => {
  const due = Math.max(0, tot.value.total - (o.value.credit_paid ?? 0)) / 100
  const steps = due < 100 ? [10, 50, 100] : due < 1000 ? [50, 100, 500] : [100, 500, 1000]
  const out: number[] = []
  for (const s of steps) { const v = Math.ceil(due / s) * s; if (v > due && !out.includes(v)) out.push(v) }
  for (const v of [due * 2, Math.ceil(due / steps[2]) * steps[2] + steps[2]]) if (out.length < 3 && !out.includes(v)) out.push(Math.ceil(v))
  return out.slice(0, 3)
})
function key(k: string) {
  if (k === '⌫') { pay.digits = pay.digits.slice(0, -1); return }
  if (pay.digits.length >= 9) return
  if ((pay.digits === '' || pay.digits === '0') && (k === '0' || k === '00')) return
  pay.digits += k
}
async function close() {
  if (!canClose.value) return
  pay.busy = true; error.value = ''
  try {
    const b = await post<Bill>(`/merchant/table-sessions/${props.bill.session_id}/close`,
      { expected_manifest_version: pay.manifest, amount_received: cashLike.value ? received.value : due.value, pay_type: choice.value.type,
        pay_channel_id: choice.value.channel_id, adjust: adjust.value, expected_total: total.value, note: '' },
      adjust.value && approval.value ? { 'X-Approval': approval.value } : undefined)
    emit('update', b)
    done.value = true
    approval.value = ''
  } catch (e) {
    if (e instanceof ApiError && e.code === 'APPROVAL_INVALID') { approval.value = ''; error.value = t('ck.approval_used') }
    else if (e instanceof ApiError && e.code === 'VERSION_CONFLICT' && e.details?.reason !== 'promotion_used_up' && adjust.value) error.value = t('ck.changed')
    else if (e instanceof ApiError && e.code === 'VERSION_CONFLICT') error.value = t('bill_changed')
    else if (e instanceof ApiError && e.code === 'ITEMS_IN_PROGRESS') error.value = t('dd.cooking_block', { n: e.details?.count ?? '' })
    else error.value = (e as Error).message
    resetPay()
  }
  pay.busy = false
}
const done = ref(false)
</script>

<template>
  <div class="dd-billwrap">
    <div class="dd-top">
      <button class="dd-back" data-testid="dd-back" @click="emit('back')"><span class="rtl-flip">‹</span> {{ t('dd.back_tables') }}</button>
      <span class="dd-chip">{{ bill.table_name }}<small>{{ t('dd.guests_n', { n: bill.guests }) }} · {{ sinceText(bill.opened_at, now) }}</small></span>
      <span v-if="!phone" class="dd-pill">{{ bill.order.public_no }}</span>
      <span class="sp"></span>
      <button v-if="bill.status === 'open'" class="dd-topbtn" data-testid="dd-bill-add" @click="emit('add')">{{ t('dd.add') }}</button>
    </div>
    <div v-if="error" class="banner bad dd-banner" @click="error = ''">{{ error }}</div>
    <div class="dd-bbody">
      <section v-if="!phone || !paying" class="dd-bill" data-testid="dd-bill-list">
        <div v-if="bill.tags.length" class="dd-chiprow pad"><span v-for="x in bill.tags" :key="x" class="dd-tag">{{ x }}</span></div>
        <template v-for="r in rounds" :key="r.seq">
          <div class="round"><b>{{ t('dd.round_head', { n: r.seq }) }}</b> {{ clock(r.first.batch_at) }} · {{ source(r.first) }}
            <template v-if="r.first.round_tags?.length"> · {{ r.first.round_tags.join(' · ') }}</template></div>
          <template v-for="it in r.items" :key="it.id">
            <button class="bi" :class="{ on: sel === it.id, void: it.prep_status === 'cancelled' }" :disabled="it.prep_status === 'cancelled' || bill.status !== 'open'"
              data-testid="dd-bill-item" @click="sel = sel === it.id ? null : it.id">
              <span class="nm">{{ it.name }}<small v-if="it.prep_status === 'cancelled' && voidsOf(bill.order, it).length" data-testid="dd-void-note">{{ voidsOf(bill.order, it).map(v => voidNote(v)).join(' / ') }}</small>
                <small v-else-if="it.prep_status === 'cancelled' && it.void_reason">{{ t('dd.voided', { r: it.void_reason }) }}</small>
                <small v-else-if="detail(it)">{{ detail(it) }}</small></span>
              <span class="q">×{{ it.qty }}</span>
              <span><span class="dd-st" :class="st(it)[0]">{{ st(it)[1] }}</span><span v-if="it.rush_at && !it.served_at && it.prep_status !== 'cancelled'" class="dd-st rush">{{ t('dd.st.rush') }}</span></span>
              <span class="p">{{ m(it.line_total) }}</span>
            </button>
            <div v-if="sel === it.id" class="acts">
              <button class="dd-ch" :disabled="it.prep_status !== 'ready' || !!it.served_at" data-testid="dd-serve" @click="act(it, 'serve')">{{ t('dd.serve') }}</button>
              <button class="dd-ch" data-testid="dd-again" @click="again(it)">{{ t('dd.again') }}</button>
              <button class="dd-ch" :disabled="!(it.prep_status === 'pending' || it.prep_status === 'preparing')" data-testid="dd-rush" @click="act(it, 'rush')">{{ it.rush_at ? t('dd.rushed') : t('dd.rush') }}</button>
              <button class="dd-ch red" data-testid="dd-void" @click="startVoid(it)">{{ t('dd.void') }}</button>
            </div>
          </template>
        </template>
        <div v-if="!rounds.length" class="dd-empty">{{ t('dd.not_ordered') }}</div>
        <div v-if="phone && bill.status === 'open'" class="dd-pfoot"><span>{{ t('dd.due') }} <b>{{ m(due) }}</b></span>
          <button class="dd-big gold" data-testid="dd-pay" @click="paying = true">{{ t('dd.pay') }}</button></div>
      </section>

      <section v-if="paying && bill.status === 'open'" class="dd-pay" data-testid="dd-pay-panel">
        <div class="ph"><div class="dd-lbl">{{ t('dd.pay_title') }}</div><button v-if="phone" class="lnk" @click="paying = false">✕</button></div>
        <div class="kv"><span>{{ t('dd.items_n', { n: live.reduce((s, i) => s + i.qty, 0) }) }}</span><span>{{ m(o.totals.subtotal) }}</span></div>
        <div v-if="o.totals.discount" class="kv promo" data-testid="dd-promo"><span>{{ t('ck.tab_dine') }} · {{ pick(o.totals.promotion_name, o.totals.promotion_name_en) }}</span><span>−{{ m(o.totals.discount) }}</span></div>
        <div v-if="o.totals.service_charge" class="kv"><span>{{ t('dd.service') }}</span><span>{{ m(o.totals.service_charge) }}</span></div>
        <div v-if="o.totals.tax_amount" class="kv"><span>{{ o.totals.tax_inclusive ? t('dd.tax_incl') : t('dd.tax') }} {{ o.totals.tax_rate_bp / 100 }}%</span><span>{{ m(o.totals.tax_amount) }}</span></div>
        <button class="kv adj" :class="{ set: !!adjust }" data-testid="dd-adj" @click="openAdjust">
          <span>{{ t('ck.adj') }}</span><span>{{ adjLabel }} <span class="rtl-flip">›</span></span></button>
        <div v-if="notice" class="muted small" data-testid="dd-approved">{{ notice }}</div>
        <div v-if="o.credit_paid" class="kv promo" data-testid="dd-credit"><span>{{ t('co.credit_line') }}</span><span>−{{ m(o.credit_paid) }}</span></div>
        <button v-else-if="can('order.handle', 'table.use', 'order.manual')" class="kv adj" data-testid="dd-use-credit" @click="spending = true">
          <span>{{ t('sc.s.btn') }}</span><span class="rtl-flip">›</span></button>
        <div class="due"><span>{{ t('dd.due') }}</span><b data-testid="dd-due"><s v-if="adjTotals && adjTotals.total !== o.totals.total" class="was">{{ m(o.totals.total) }}</s>{{ m(due) }}</b></div>
        <PayPicker v-model="choice" :options="payOpts" class="methods" />
        <template v-if="cashLike">
          <div class="recv"><span class="muted">{{ t('dd.received') }}</span><b data-testid="dd-received">{{ m(received) }}</b></div>
          <div class="quick"><button :class="{ on: !pay.digits }" @click="pay.digits = ''">{{ t('dd.exact') }}</button>
            <button v-for="v in quick" :key="v" :class="{ on: pay.digits === String(v) }" data-testid="dd-quick" @click="pay.digits = String(v)">{{ m(v * 100) }}</button></div>
          <div class="dd-keys"><button v-for="k in ['1','2','3','4','5','6','7','8','9','00','0','⌫']" :key="k" @click="key(k)">{{ k }}</button></div>
          <div class="chg"><span>{{ t('dd.change') }}</span><b :class="{ bad: change < 0 }" data-testid="dd-change">{{ change < 0 ? t('dd.short') : m(change) }}</b></div>
        </template>
        <div class="pf">
          <div v-if="cooking" class="banner warn small">{{ t('dd.cooking_block', { n: cooking }) }}</div>
          <button class="dd-big" :disabled="!canClose" data-testid="dd-close" @click="close">{{ t('dd.close') }}</button>
        </div>
      </section>
    </div>

    <CreditSpend v-if="spending" :order="o" @done="spent" @close="spending = false" />
    <AdjustSheet v-if="adjOpen" :currency="currency" :reasons="payOpts?.reasons" :initial="adjust" :bottom="phone" :preview="previewAdj"
      @close="adjOpen = false" @apply="setAdjust" />
    <ApproveDialog v-if="approving" :branch-id="o.branch_id" perm="checkout.discount" :what="t('ck.approve_what')" @approved="approved" @close="approving = false" />

    <!-- void: how many, why, and what the bill comes to -->
    <VoidSheet v-if="voiding" :item="voiding" :branch-id="session.branchId!" :currency="currency" bill :reasons="reasons" :bottom="phone" @done="voided" @close="voiding = null" />

    <!-- closed -->
    <div v-if="done" class="dd-mask">
      <div class="dd-modal" data-testid="dd-closed">
        <div class="mh"><h3>{{ t('dd.closed_title', { name: bill.table_name }) }}</h3></div>
        <div class="mb">
          <div class="kv"><span>{{ t('dd.due') }}</span><b>{{ m(o.totals.total) }}</b></div>
          <div v-if="o.totals.staff_discount" class="kv"><span>{{ t('ck.staff_disc') }}</span><b>−{{ m(o.totals.staff_discount) }}</b></div>
          <div v-if="o.totals.round_off" class="kv"><span>{{ t('ck.round_off') }}</span><b>−{{ m(o.totals.round_off) }}</b></div>
          <div class="kv"><span>{{ t('dd.received') }} · {{ payText(o) }}</span><b data-testid="dd-closed-received">{{ m(o.amount_received ?? 0) }}</b></div>
          <div v-if="o.credit_paid" class="kv"><span>{{ t('co.credit_line') }}</span><b>−{{ m(o.credit_paid) }}</b></div>
          <div v-if="(o.amount_received ?? 0) > o.totals.total - (o.credit_paid ?? 0)" class="kv"><span>{{ t('dd.change') }}</span><b>{{ m((o.amount_received ?? 0) - o.totals.total + (o.credit_paid ?? 0)) }}</b></div>
        </div>
        <div class="mf"><a class="dd-big ghost" :href="`/ticket/${o.id}`" target="_blank" rel="noopener">{{ t('dd.print') }}</a>
          <button class="dd-big" style="flex:2" data-testid="dd-done" @click="emit('closed')">{{ t('dd.done') }}</button></div>
      </div>
    </div>
  </div>
</template>
