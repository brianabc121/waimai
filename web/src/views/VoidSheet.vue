<script setup lang="ts">
// The void sheet (docs/VOID_ITEM_DESIGN.md §8.1, §8.2): how many, why, how
// far the dish has got, and what the order comes to after; a dish already
// started needs a manager's code when this person may not void it.
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { post } from '../api'
import { money, t } from '../i18n'
import type { OrderItem, VoidQuote } from '../types'
import { customerReasons, kindOf, splitList, voidErr } from '../voids'
import ApproveDialog from './ApproveDialog.vue'

const props = defineProps<{ item: OrderItem; branchId: number; currency: string
  // the table bill voids through its own route and gets the bill back
  bill?: boolean; reasons?: string[]; bottom?: boolean }>()
const emit = defineEmits<{ (e: 'done', r: any): void; (e: 'close'): void }>()
const m = (v: number) => money(v, props.currency)

const s = reactive({ qty: 1, reason: '', kind: 'store' as 'store' | 'customer', writing: false, text: '', busy: false })
const quote = ref<VoidQuote | null>(null)
const error = ref('')
const approving = ref(false)
const reasons = computed(() => props.reasons?.length ? props.reasons : [...customerReasons(), ...splitList('vd.reasons.store')])
const reason = computed(() => s.writing ? s.text.trim() : s.reason)

let seq = 0
async function requote() {
  const n = ++seq
  try {
    const q = await post<VoidQuote>(`/merchant/order-items/${props.item.id}/void-quote`, { qty: s.qty, reason_kind: s.kind })
    if (n === seq) { quote.value = q; error.value = '' }
  } catch (e) { if (n === seq) { quote.value = null; error.value = voidErr(e) } }
}
onMounted(requote)
watch(() => [s.qty, s.kind], requote)

function tap(r: string) { s.reason = r; s.writing = false; s.kind = kindOf(r) }
const started = computed(() => {
  const at = quote.value?.started_at
  return at ? Math.max(0, Math.floor((Date.now() - new Date(at).getTime()) / 60000)) : 0
})
const stage = computed(() => {
  const q = quote.value
  if (!q) return ''
  if (!q.wasted) return t('vd.st.free')
  if (q.was_status === 'preparing') return q.started_at ? t('vd.st.preparing', { n: started.value }) : t('vd.st.preparing0')
  return t(`vd.st.${q.was_status}`)
})
const promoName = (n: string, v?: number) => n && v ? `${n} −${m(v)}` : t('vd.promo_none')

async function go(headers?: Record<string, string>) {
  if (!reason.value || !quote.value) return
  if (quote.value.needs_approval && !headers) { approving.value = true; return }
  s.busy = true; error.value = ''
  try {
    const url = props.bill ? `/merchant/bill-items/${props.item.id}/void` : `/merchant/order-items/${props.item.id}/void`
    emit('done', await post(url, { qty: s.qty, reason: reason.value, reason_kind: s.kind, expected_version: quote.value.version }, headers))
  } catch (e) { error.value = voidErr(e); void requote() }
  s.busy = false
}
function approved(token: string) { approving.value = false; void go({ 'X-Approval': token }) }
</script>

<template>
  <div class="vmask" :class="{ bottom }" @click.self="emit('close')">
    <div class="vsheet" data-testid="void-sheet">
      <div class="vh"><h3>{{ t('vd.title', { name: item.name }) }}</h3>
        <button class="x" :aria-label="t('vd.cancel')" @click="emit('close')">✕</button></div>
      <div class="vb">
        <template v-if="item.qty > 1">
          <div class="lbl">{{ t('vd.qty') }}</div>
          <span class="stp"><button :disabled="s.qty <= 1" @click="s.qty--">−</button><b data-testid="void-qty">{{ s.qty }}</b>
            <button :disabled="s.qty >= item.qty" data-testid="void-more" @click="s.qty++">＋</button></span>
        </template>
        <div class="lbl">{{ t('vd.reason') }} <span class="req">{{ t('vd.required') }}</span></div>
        <div class="chs">
          <button v-for="x in reasons" :key="x" class="ch" :class="{ on: !s.writing && s.reason === x }" data-testid="void-reason" @click="tap(x)">{{ x }}
            <small :class="kindOf(x)">{{ t('vd.kind.' + kindOf(x)) }}</small></button>
          <button v-if="!s.writing" class="ch add" data-testid="void-write" @click="s.writing = true">{{ t('vd.write') }}</button>
          <input v-else v-model="s.text" class="write" maxlength="100" :placeholder="t('vd.reason')" data-testid="void-text" />
        </div>
        <div class="kinds" role="radiogroup">
          <button v-for="k in (['store', 'customer'] as const)" :key="k" class="kd" :class="{ on: s.kind === k }" :data-kind="k" @click="s.kind = k">{{ t('vd.kind.' + k) }}</button>
          <span class="small muted">{{ t('vd.kind_hint.' + s.kind) }}</span>
        </div>

        <div v-if="error" class="banner bad small" data-testid="void-error">{{ error }}</div>
        <template v-if="quote">
          <div class="stage" :class="quote.wasted ? 'warn' : 'ok'" data-testid="void-stage">{{ stage }}</div>
          <div class="money" data-testid="void-money">{{ t('vd.before') }} <s>{{ m(quote.before.total) }}</s> → <b>{{ m(quote.after.total) }}</b>
            <span class="less">{{ t('vd.less', { s: m(quote.less) }) }}</span></div>
          <div v-if="quote.promo_down" class="promo" data-testid="void-promo">{{ t('vd.promo_down', { a: promoName(quote.promo_before, quote.before.promotion_discount), b: promoName(quote.promo_after, quote.after.promotion_discount) }) }}</div>
          <div v-if="quote.rider" class="small note">{{ t('vd.rider', { name: quote.rider, s: m(quote.after.total) }) }}</div>
          <div v-if="quote.packed" class="small note warn">{{ t('vd.packed') }}</div>
          <div v-if="quote.needs_approval" class="small note warn" data-testid="void-need">🔒 {{ t('vd.need') }}</div>
        </template>
      </div>
      <div class="vf">
        <button class="btn" @click="emit('close')">{{ t('vd.cancel') }}</button>
        <button class="btn danger grow" :disabled="s.busy || !reason || !quote" data-testid="void-ok" @click="go()">
          {{ quote?.needs_approval ? '🔒 ' + t('vd.ask') : quote ? t('vd.ok_less', { s: m(quote.less) }) : t('vd.ok') }}</button>
      </div>
    </div>
    <ApproveDialog v-if="approving" :branch-id="branchId" perm="order.void" :what="t('vd.what', { name: item.name })" @approved="approved" @close="approving = false" />
  </div>
</template>

<style scoped>
.vmask { position: fixed; inset: 0; background: rgba(0,0,0,.4); z-index: 30; display: flex; align-items: center; justify-content: center; padding: 16px; }
.vmask.bottom { align-items: flex-end; padding: 0; }
.vsheet { background: var(--card, #fff); border-radius: 14px; width: min(480px, 100%); max-height: calc(100vh - 32px); display: flex; flex-direction: column; overflow: hidden; }
.vmask.bottom .vsheet { border-radius: 14px 14px 0 0; width: 100%; }
.vh { display: flex; align-items: center; gap: 8px; padding: 14px 16px 8px; }
.vh h3 { margin: 0; font-size: 17px; flex: 1; min-width: 0; overflow-wrap: anywhere; }
.x { background: none; border: 0; font-size: 18px; cursor: pointer; color: var(--muted); }
.vb { padding: 0 16px 12px; overflow-y: auto; display: flex; flex-direction: column; gap: 10px; }
.lbl { font-size: 13px; color: var(--muted); margin-top: 4px; }
.req { color: var(--bad, #c0392b); font-size: 12px; }
.stp { display: inline-flex; align-items: center; gap: 14px; }
.stp button { width: 40px; height: 40px; border-radius: 10px; border: 1px solid var(--line); background: #fff; font-size: 20px; cursor: pointer; }
.stp b { font-size: 20px; min-width: 24px; text-align: center; }
.chs { display: flex; flex-wrap: wrap; gap: 8px; }
.ch { border: 1px solid var(--line); background: #fff; border-radius: 10px; padding: 8px 12px; cursor: pointer; font-size: 14px; display: inline-flex; flex-direction: column; align-items: flex-start; gap: 2px; text-align: start; }
.ch small { font-size: 11px; color: var(--muted); }
.ch small.customer { color: #b45309; }
.ch.on { border-color: var(--brand); background: var(--brand-soft); box-shadow: 0 0 0 1px var(--brand); }
.ch.add { border-style: dashed; color: var(--muted); justify-content: center; }
.write { flex: 1 1 160px; min-width: 0; }
.kinds { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; }
.kd { border: 1px solid var(--line); background: #fff; border-radius: 999px; padding: 4px 10px; font-size: 12.5px; cursor: pointer; }
.kd.on { border-color: var(--brand); color: var(--brand); font-weight: 600; }
.stage { border-radius: 8px; padding: 8px 10px; font-size: 14px; }
.stage.ok { background: #e8f6ee; color: #18794e; }
.stage.warn { background: #fff4e0; color: #a85b00; }
.money { font-size: 15px; }
.money s { color: var(--muted); }
.money b { font-size: 18px; }
.less { margin-inline-start: 8px; color: var(--bad, #c0392b); font-weight: 600; }
.promo { background: #fff7dc; color: #8a6400; border-radius: 8px; padding: 6px 10px; font-size: 13.5px; }
.note { color: var(--muted); }
.note.warn { color: #a85b00; }
.vf { display: flex; gap: 10px; padding: 12px 16px calc(12px + env(safe-area-inset-bottom)); border-top: 1px solid var(--line); }
.vf .btn { min-height: 44px; }
.grow { flex: 1; }
</style>
