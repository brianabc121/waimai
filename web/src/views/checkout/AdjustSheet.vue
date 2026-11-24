<script setup lang="ts">
// The discount at checkout (docs/CHECKOUT_DISCOUNT_DESIGN.md §5): a percentage
// off the dishes, then a round-off, or the amount the customer actually pays.
// The server prices every choice (preview), so what is shown is what is saved.
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { isZh, money, t, toMinor } from '../../i18n'
import { pctText, reasonList } from '../../checkout'
import type { Adjust, Totals } from '../../types'

const props = defineProps<{ currency: string; reasons?: string[] | null; initial?: Adjust | null; bottom?: boolean
  preview: (a: Adjust) => Promise<Totals> }>()
const emit = defineEmits<{ close: []; apply: [a: Adjust | null, totals: Totals | null] }>()
const m = (v: number) => money(v, props.currency)

const PCTS = [9500, 9000, 8800, 8500, 8000, 7000]
const s = reactive({ pct: 0, pctOther: false, pctText: '', mode: '' as '' | 'round' | 'set_total', round: 0, roundOther: false, roundText: '',
  setText: '', reason: '', writing: false, reasonText: '' })
const init = props.initial
if (init) {
  s.pct = init.percent_bp; s.pctOther = !!init.percent_bp && !PCTS.includes(init.percent_bp)
  if (s.pctOther) s.pctText = isZh() ? String(init.percent_bp / 100) : String((10000 - init.percent_bp) / 100)
  s.mode = init.mode
  if (init.mode === 'round') s.round = init.amount
  if (init.mode === 'set_total') s.setText = String(init.amount / 100)
  s.reason = init.reason
}
const reasons = computed(() => reasonList(props.reasons))
if (s.reason && !reasons.value.includes(s.reason)) { s.writing = true; s.reasonText = s.reason }

// typed percentage: 折 for Chinese readers (92 → 92 折, 9 → 9 折), else % off
const typedPct = computed(() => {
  const n = Number(s.pctText)
  if (!s.pctText || !isFinite(n) || n <= 0) return 0
  if (isZh()) return Math.round((n < 10 ? n * 1000 : n * 100))
  return Math.round(10000 - n * 100)
})
const pct = computed(() => s.mode === 'set_total' ? 0 : s.pctOther ? typedPct.value : s.pct)
const reason = computed(() => (s.writing ? s.reasonText : s.reason).trim())
const amount = computed(() => s.mode === 'round' ? (s.roundOther ? toMinor(s.roundText || '0') : s.round)
  : s.mode === 'set_total' ? (s.setText === '' ? -1 : toMinor(s.setText)) : 0)
const adj = computed<Adjust>(() => ({ percent_bp: pct.value > 0 && pct.value < 10000 ? pct.value : 0, mode: s.mode, amount: Math.max(0, amount.value), reason: reason.value }))

// before: no discount; afterPct: the percentage only (the round-offs are cut from it); now: everything
const before = ref<Totals | null>(null)
const afterPct = ref<Totals | null>(null)
const now = ref<Totals | null>(null)
const error = ref('')
let seq = 0
async function price() {
  const my = ++seq
  error.value = ''
  const a = adj.value
  try {
    const [p, n] = await Promise.all([
      props.preview({ percent_bp: a.percent_bp, mode: '', amount: 0, reason: '' }),
      a.mode && (a.mode !== 'set_total' || amount.value >= 0) && (a.mode !== 'round' || a.amount > 0)
        ? props.preview({ ...a, reason: a.reason || (a.mode === 'set_total' ? '-' : '') }) : Promise.resolve(null)])
    if (my !== seq) return
    afterPct.value = p; now.value = n ?? p
  } catch (e) { if (my === seq) error.value = (e as Error).message }
}
onMounted(async () => {
  try { before.value = await props.preview({ percent_bp: 0, mode: '', amount: 0, reason: '' }) } catch (e) { error.value = (e as Error).message }
  void price()
})
let timer = 0
watch(adj, () => { clearTimeout(timer); timer = window.setTimeout(price, 200) }, { deep: true })

// the round-offs offered: drop the cents, down to a ten, down to a hundred
const rounds = computed(() => {
  const due = afterPct.value?.total ?? 0
  const out: number[] = []
  for (const step of [100, 1000, 10000]) {
    const off = due % step
    if (off > 0 && off < due && !out.includes(off)) out.push(off)
  }
  return out.slice(0, 3)
})
function setMode(x: '' | 'round' | 'set_total') {
  s.mode = x
  if (x === 'set_total') { s.pct = 0; s.pctOther = false }
  if (x === 'round' && !s.round && !s.roundOther) s.round = rounds.value[0] ?? 0
}
watch(rounds, r => { if (s.mode === 'round' && !s.roundOther && !r.includes(s.round)) s.round = r[0] ?? 0 })

const needReason = computed(() => s.mode === 'set_total')
const off = computed(() => before.value && now.value ? before.value.total - now.value.total : 0)
const ok = computed(() => !error.value && !!now.value && (!needReason.value || !!reason.value) && (s.mode !== 'set_total' || amount.value >= 0)
  && (s.mode !== 'round' || adj.value.amount > 0) && (!s.pctOther || (typedPct.value > 0 && typedPct.value < 10000)))
const empty = computed(() => !adj.value.percent_bp && !adj.value.mode)
function apply() { if (empty.value) emit('apply', null, before.value); else emit('apply', adj.value, now.value) }
</script>

<template>
  <div class="ck-mask" :class="{ bottom }" @click.self="emit('close')">
    <div class="ck-sheet" data-testid="adj-sheet">
      <div class="ck-h"><h3>{{ t('ck.adj') }}</h3><button class="x" @click="emit('close')">✕</button></div>
      <div class="ck-sum" data-testid="adj-summary">
        <template v-if="before && now">{{ t('ck.summary', { from: m(before.total), to: m(now.total), off: m(off) }) }}</template>&nbsp;
      </div>
      <div class="ck-b">
        <div class="ck-l">{{ t('ck.adj_pct') }}</div>
        <div class="ck-chs">
          <button class="ck-ch" :class="{ on: !s.pct && !s.pctOther }" :disabled="s.mode === 'set_total'" @click="s.pct = 0; s.pctOther = false">{{ t('ck.adj_none') }}</button>
          <button v-for="p in PCTS" :key="p" class="ck-ch" :class="{ on: !s.pctOther && s.pct === p }" :disabled="s.mode === 'set_total'" data-testid="adj-pct"
            @click="s.pct = p; s.pctOther = false">{{ pctText(p) }}</button>
          <button class="ck-ch" :class="{ on: s.pctOther }" :disabled="s.mode === 'set_total'" data-testid="adj-pct-other" @click="s.pctOther = true">{{ t('ck.other') }}</button>
        </div>
        <input v-if="s.pctOther && s.mode !== 'set_total'" v-model="s.pctText" class="ck-in" inputmode="decimal" :placeholder="t('ck.pct_hint')" data-testid="adj-pct-input" />

        <div class="ck-seg">
          <button :class="{ on: s.mode === '' }" @click="setMode('')">{{ t('ck.adj_none') }}</button>
          <button :class="{ on: s.mode === 'round' }" data-testid="adj-mode-round" @click="setMode('round')">{{ t('ck.adj_round') }}</button>
          <button :class="{ on: s.mode === 'set_total' }" data-testid="adj-mode-set" @click="setMode('set_total')">{{ t('ck.adj_set') }}</button>
        </div>
        <div v-if="s.mode === 'round'" class="ck-chs">
          <button v-for="r in rounds" :key="r" class="ck-ch" :class="{ on: !s.roundOther && s.round === r }" data-testid="adj-round"
            @click="s.round = r; s.roundOther = false">{{ t('ck.round_to', { to: m((afterPct?.total ?? 0) - r), off: m(r) }) }}</button>
          <button class="ck-ch" :class="{ on: s.roundOther }" @click="s.roundOther = true">{{ t('ck.other') }}</button>
          <input v-if="s.roundOther" v-model="s.roundText" class="ck-in" inputmode="decimal" :placeholder="t('ck.round_hint')" data-testid="adj-round-input" />
        </div>
        <div v-if="s.mode === 'set_total'" class="ck-chs">
          <input v-model="s.setText" class="ck-in big" inputmode="decimal" :placeholder="t('ck.set_hint')" data-testid="adj-set-input" />
          <button class="ck-ch" :class="{ on: s.setText === '0' }" data-testid="adj-free" @click="s.setText = '0'">{{ t('ck.free') }}</button>
        </div>

        <div class="ck-l">{{ t('ck.reason') }} <span :class="needReason ? 'req' : 'muted'">{{ needReason ? t('ck.reason_req') : t('ck.reason_opt') }}</span></div>
        <div class="ck-chs">
          <button v-for="x in reasons" :key="x" class="ck-ch" :class="{ on: !s.writing && s.reason === x }" data-testid="adj-reason"
            @click="s.reason = s.reason === x && !s.writing ? '' : x; s.writing = false">{{ x }}</button>
          <input v-if="s.writing" v-model="s.reasonText" class="ck-in" maxlength="40" :placeholder="t('ck.reason')" />
          <button v-else class="ck-ch add" @click="s.writing = true">＋</button>
        </div>
        <div v-if="error" class="banner bad small" data-testid="adj-error">{{ error }}</div>
      </div>
      <div class="ck-f">
        <button class="ck-btn ghost" data-testid="adj-clear" @click="emit('apply', null, before)">{{ t('ck.clear') }}</button>
        <button class="ck-btn" :disabled="!ok" data-testid="adj-apply" @click="apply">{{ t('ck.apply') }}</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.ck-mask { position: fixed; inset: 0; background: rgba(0,0,0,.4); z-index: 60; display: flex; align-items: center; justify-content: center; padding: 16px; }
.ck-mask.bottom { align-items: flex-end; padding: 0; }
.ck-sheet { background: #fff; color: #2b1d16; border-radius: 14px; width: min(520px, 100%); max-height: 92vh; display: flex; flex-direction: column; overflow: hidden; }
.ck-mask.bottom .ck-sheet { border-radius: 14px 14px 0 0; width: 100%; }
.ck-h { display: flex; align-items: center; padding: 14px 16px 8px; }
.ck-h h3 { margin: 0; font-size: 17px; flex: 1; }
.ck-h .x { border: 0; background: none; font-size: 18px; cursor: pointer; color: #8a7a70; }
.ck-sum { margin: 0 16px; padding: 10px 12px; border-radius: 10px; background: #fbf3e2; color: #8a5a00; font-weight: 600; font-size: 14px; }
.ck-b { padding: 6px 16px 12px; overflow: auto; }
.ck-l { font-size: 13px; color: #6b5a50; margin: 14px 0 8px; font-weight: 600; }
.ck-l .req { color: #b3261e; font-weight: 400; margin-inline-start: 6px; }
.ck-l .muted { font-weight: 400; margin-inline-start: 6px; }
.ck-chs { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; }
.ck-ch { min-height: 40px; padding: 6px 14px; border-radius: 20px; border: 1px solid #e3d6c8; background: #fff; color: #2b1d16; font-size: 14px; cursor: pointer; }
.ck-ch.on { border-color: #a4161a; background: #a4161a; color: #fff; }
.ck-ch:disabled { opacity: .4; cursor: default; }
.ck-ch.add { color: #a4161a; border-style: dashed; }
.ck-in { min-height: 40px; padding: 6px 12px; border: 1px solid #e3d6c8; border-radius: 10px; font-size: 15px; flex: 1; min-width: 160px; margin-top: 8px; }
.ck-chs .ck-in { margin-top: 0; }
.ck-in.big { font-size: 18px; font-weight: 600; }
.ck-seg { display: flex; margin-top: 16px; border: 1px solid #e3d6c8; border-radius: 10px; overflow: hidden; }
.ck-seg button { flex: 1; min-height: 40px; border: 0; background: #fff; font-size: 14px; cursor: pointer; color: #2b1d16; }
.ck-seg button + button { border-inline-start: 1px solid #e3d6c8; }
.ck-seg button.on { background: #fbf3e2; color: #a4161a; font-weight: 600; }
.ck-seg + .ck-chs { margin-top: 10px; }
.ck-f { display: flex; gap: 10px; padding: 12px 16px calc(12px + env(safe-area-inset-bottom)); border-top: 1px solid #f0e6da; }
.ck-btn { flex: 2; min-height: 46px; border-radius: 10px; border: 0; background: #a4161a; color: #fff; font-size: 16px; font-weight: 600; cursor: pointer; }
.ck-btn.ghost { flex: 1; background: #fff; color: #a4161a; border: 1px solid #a4161a; }
.ck-btn:disabled { opacity: .45; cursor: default; }
.banner { margin-top: 12px; }
</style>
