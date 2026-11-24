<script setup lang="ts">
// A table's bill by round, what can be done to each dish (serve, one more,
// rush, void with a tapped reason), and taking payment with tapped amounts
// (docs/DINE_IN_ORDER_DESIGN.md §8, §9).
import { computed, reactive, ref, watch } from 'vue'
import { ApiError, post } from '../../api'
import { money, t, toMinor } from '../../i18n'
import type { Bill, DinePresets, Item, OrderItem } from '../../types'
import type { DeskLine } from './DineOrder.vue'
import { presetList, sinceText } from './strings'

const props = defineProps<{ bill: Bill; presets: DinePresets | null; currency: string; country: string; phone: boolean; payFirst: boolean; items?: Map<number, Item> }>()
const emit = defineEmits<{ back: []; update: [bill: Bill]; again: [line: DeskLine]; closed: []; add: [] }>()
const m = (v: number) => money(v, props.currency)
const now = ref(Date.now())
setInterval(() => (now.value = Date.now()), 30000)
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

// ---- void ----
const voiding = reactive<{ item: OrderItem | null; qty: number; reason: string; writing: boolean; text: string; busy: boolean }>(
  { item: null, qty: 1, reason: '', writing: false, text: '', busy: false })
const reasons = computed(() => presetList(props.presets, 'void_reasons'))
function startVoid(it: OrderItem) { Object.assign(voiding, { item: it, qty: it.qty, reason: '', writing: false, text: '', busy: false }) }
async function confirmVoid() {
  const it = voiding.item!
  const reason = voiding.writing ? voiding.text.trim() : voiding.reason
  if (!reason) return
  voiding.busy = true
  try {
    emit('update', await post<Bill>(`/merchant/bill-items/${it.id}/void`, { reason, qty: voiding.qty }))
    voiding.item = null; sel.value = null
  } catch (e) { error.value = (e as Error).message }
  voiding.busy = false
}

// ---- payment ----
const paying = ref(props.payFirst || !props.phone)
const o = computed(() => props.bill.order)
const live = computed(() => o.value.items.filter(i => i.prep_status !== 'cancelled'))
const cooking = computed(() => live.value.filter(i => i.prep_status === 'pending' || i.prep_status === 'preparing').length)
const methods = computed(() => presetList(props.presets, 'pay_methods', props.country))
const pay = reactive({ method: '', digits: '', manifest: 0, busy: false })
const cashLike = computed(() => pay.method === methods.value[0])
watch(methods, ms => { if (!pay.method) pay.method = ms[0] ?? '' }, { immediate: true })
function resetPay() { pay.digits = ''; pay.manifest = o.value.manifest_version }
resetPay()
watch(() => o.value.manifest_version, v => { if (pay.manifest && v !== pay.manifest) { resetPay(); error.value = t('bill_changed') } })
// amounts are typed in major units; the bill is in minor units
const received = computed(() => pay.digits ? toMinor(pay.digits) : o.value.totals.total)
const change = computed(() => received.value - o.value.totals.total)
// the quick amounts: round numbers just above the bill
const quick = computed(() => {
  const due = o.value.totals.total / 100
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
  if (cooking.value || change.value < 0) return
  pay.busy = true; error.value = ''
  try {
    const b = await post<Bill>(`/merchant/table-sessions/${props.bill.session_id}/close`,
      { expected_manifest_version: pay.manifest, amount_received: received.value, pay_method: pay.method, note: '' })
    emit('update', b)
    done.value = true
  } catch (e) {
    if (e instanceof ApiError && e.code === 'VERSION_CONFLICT') error.value = t('bill_changed')
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
              <span class="nm">{{ it.name }}<small v-if="it.prep_status === 'cancelled' && it.void_reason">{{ t('dd.voided', { r: it.void_reason }) }}</small>
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
        <div v-if="phone && bill.status === 'open'" class="dd-pfoot"><span>{{ t('dd.due') }} <b>{{ m(o.totals.total) }}</b></span>
          <button class="dd-big gold" data-testid="dd-pay" @click="paying = true">{{ t('dd.pay') }}</button></div>
      </section>

      <section v-if="paying && bill.status === 'open'" class="dd-pay" data-testid="dd-pay-panel">
        <div class="ph"><div class="dd-lbl">{{ t('dd.pay_title') }}</div><button v-if="phone" class="lnk" @click="paying = false">✕</button></div>
        <div class="kv"><span>{{ t('dd.items_n', { n: live.reduce((s, i) => s + i.qty, 0) }) }}</span><span>{{ m(o.totals.subtotal) }}</span></div>
        <div v-if="o.totals.service_charge" class="kv"><span>{{ t('dd.service') }}</span><span>{{ m(o.totals.service_charge) }}</span></div>
        <div v-if="o.totals.tax_amount" class="kv"><span>{{ o.totals.tax_inclusive ? t('dd.tax_incl') : t('dd.tax') }} {{ o.totals.tax_rate_bp / 100 }}%</span><span>{{ m(o.totals.tax_amount) }}</span></div>
        <div class="due"><span>{{ t('dd.due') }}</span><b data-testid="dd-due">{{ m(o.totals.total) }}</b></div>
        <div class="dd-chs methods"><button v-for="x in methods" :key="x" class="dd-ch" :class="{ on: pay.method === x }" data-testid="dd-method" @click="pay.method = x; if (pay.method !== methods[0]) pay.digits = ''">{{ x }}</button></div>
        <template v-if="cashLike">
          <div class="recv"><span class="muted">{{ t('dd.received') }}</span><b data-testid="dd-received">{{ m(received) }}</b></div>
          <div class="quick"><button :class="{ on: !pay.digits }" @click="pay.digits = ''">{{ t('dd.exact') }}</button>
            <button v-for="v in quick" :key="v" :class="{ on: pay.digits === String(v) }" data-testid="dd-quick" @click="pay.digits = String(v)">{{ m(v * 100) }}</button></div>
          <div class="dd-keys"><button v-for="k in ['1','2','3','4','5','6','7','8','9','00','0','⌫']" :key="k" @click="key(k)">{{ k }}</button></div>
          <div class="chg"><span>{{ t('dd.change') }}</span><b :class="{ bad: change < 0 }" data-testid="dd-change">{{ change < 0 ? t('dd.short') : m(change) }}</b></div>
        </template>
        <div class="pf">
          <div v-if="cooking" class="banner warn small">{{ t('dd.cooking_block', { n: cooking }) }}</div>
          <button class="dd-big" :disabled="!!cooking || change < 0 || pay.busy" data-testid="dd-close" @click="close">{{ t('dd.close') }}</button>
        </div>
      </section>
    </div>

    <!-- void: how many and why -->
    <div v-if="voiding.item" class="dd-mask" :class="{ bottom: phone }" @click.self="voiding.item = null">
      <div class="dd-modal" data-testid="dd-void-sheet">
        <div class="mh"><h3>{{ t('dd.void_title', { name: voiding.item.name }) }}</h3><span class="muted">{{ m(voiding.item.unit_price * voiding.qty) }}</span>
          <button class="x" @click="voiding.item = null">✕</button></div>
        <div class="mb">
          <template v-if="voiding.item.qty > 1">
            <div class="dd-lbl">{{ t('dd.void_qty') }}</div>
            <span class="dd-stp"><button @click="voiding.qty = Math.max(1, voiding.qty - 1)">−</button><b>{{ voiding.qty }}</b>
              <button @click="voiding.qty = Math.min(voiding.item!.qty, voiding.qty + 1)">＋</button></span>
          </template>
          <div class="dd-lbl" style="margin-top:14px">{{ t('dd.reason') }} <span class="req">{{ t('dd.required') }}</span></div>
          <div class="dd-chs">
            <button v-for="x in reasons" :key="x" class="dd-ch" :class="{ on: !voiding.writing && voiding.reason === x }" data-testid="dd-reason" @click="voiding.reason = x; voiding.writing = false">{{ x }}</button>
            <button v-if="!voiding.writing" class="dd-ch add" @click="voiding.writing = true">{{ t('dd.write') }}</button>
            <span v-else class="dd-write"><input v-model="voiding.text" maxlength="100" :placeholder="t('dd.reason')" /></span>
          </div>
          <div class="muted small" style="margin-top:12px">{{ t('dd.void_note') }}</div>
        </div>
        <div class="mf"><button class="dd-big ghost" @click="voiding.item = null">{{ t('dd.cancel') }}</button>
          <button class="dd-big" style="flex:2" :disabled="voiding.busy || !(voiding.writing ? voiding.text.trim() : voiding.reason)" data-testid="dd-void-ok" @click="confirmVoid">{{ t('dd.void_ok') }}</button></div>
      </div>
    </div>

    <!-- closed -->
    <div v-if="done" class="dd-mask">
      <div class="dd-modal" data-testid="dd-closed">
        <div class="mh"><h3>{{ t('dd.closed_title', { name: bill.table_name }) }}</h3></div>
        <div class="mb">
          <div class="kv"><span>{{ t('dd.due') }}</span><b>{{ m(o.totals.total) }}</b></div>
          <div class="kv"><span>{{ t('dd.received') }} · {{ o.pay_method }}</span><b>{{ m(o.amount_received ?? 0) }}</b></div>
          <div v-if="(o.amount_received ?? 0) > o.totals.total" class="kv"><span>{{ t('dd.change') }}</span><b>{{ m((o.amount_received ?? 0) - o.totals.total) }}</b></div>
        </div>
        <div class="mf"><a class="dd-big ghost" :href="`/ticket/${o.id}`" target="_blank" rel="noopener">{{ t('dd.print') }}</a>
          <button class="dd-big" style="flex:2" data-testid="dd-done" @click="emit('closed')">{{ t('dd.done') }}</button></div>
      </div>
    </div>
  </div>
</template>
