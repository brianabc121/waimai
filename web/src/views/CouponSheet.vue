<script setup lang="ts">
// The checkout's coupon picker: one coupon of each kind, all used together.
import { computed, ref } from 'vue'
import { couponName, couponStub, kindName, validity, whereUsable } from '../coupons'
import { money, t } from '../i18n'
import type { CouponChoice, CouponKind } from '../types'

const props = defineProps<{ choices: CouponChoice[]; currency: string }>()
const emit = defineEmits<{ done: [ids: number[]]; close: [] }>()

const order: CouponKind[] = ['store_threshold', 'store_free', 'store_delivery', 'platform_free']
const groups = computed(() => order.map(k => ({ kind: k, list: props.choices.filter(c => c.kind === k) })).filter(g => g.list.length))
// what is ticked: kind -> coupon id
const picked = ref<Record<string, number | null>>(Object.fromEntries(order.map(k => [k, props.choices.find(c => c.kind === k && c.selected)?.id ?? null])))
function toggle(c: CouponChoice) {
  if (!c.usable) return
  picked.value[c.kind] = picked.value[c.kind] === c.id ? null : c.id
}
const ids = computed(() => Object.values(picked.value).filter((x): x is number => x != null))
// An estimate: the server works out the real amounts.
const estimate = computed(() => props.choices.filter(c => ids.value.includes(c.id)).reduce((s, c) => s + c.saves, 0))
function why(c: CouponChoice): string {
  switch (c.reason) {
    case 'below_threshold': return t('cp.short_by', { amt: money(c.short, props.currency) })
    case 'delivery_only': return t('cp.delivery_only')
    case 'no_saving': return t('cp.no_saving')
  }
  return ''
}
</script>

<template>
  <div class="sheet-mask" data-testid="coupon-sheet" @click.self="emit('close')">
    <div class="sheet cps">
      <div class="sheet-head row between"><b>{{ t('cp.pick_title') }}</b><button class="btn link" @click="emit('close')">{{ t('close') }}</button></div>
      <div class="sheet-body">
        <div class="small muted sub">{{ t('cp.pick_sub') }}</div>
        <template v-for="g in groups" :key="g.kind">
          <div class="grp"><span>{{ kindName(g.kind) }}</span><em>{{ t('cp.n_usable', { n: g.list.filter(c => c.usable).length }) }}</em></div>
          <button v-for="c in g.list" :key="c.id" type="button" class="cp" :class="[c.kind, { off: !c.usable }]" :disabled="!c.usable"
            :data-testid="'coupon-' + c.id" :aria-pressed="picked[c.kind] === c.id" @click="toggle(c)">
            <span class="stub"><b>{{ couponStub(c, currency)[0] }}</b><small>{{ couponStub(c, currency)[1] }}</small></span>
            <span class="body">
              <b>{{ couponName(c) }}</b>
              <span v-if="!c.usable" class="why">{{ why(c) }}</span>
              <span v-else :class="{ soon: validity(c).soon }">{{ c.kind === 'platform_free' ? whereUsable(c) + ' · ' : '' }}{{ validity(c).text }}</span>
            </span>
            <i class="radio" :class="{ on: picked[c.kind] === c.id }"></i>
          </button>
        </template>
      </div>
      <div class="foot"><button class="btn primary block" data-testid="coupon-done" @click="emit('done', ids)">
        {{ ids.length ? t('cp.confirm', { amt: money(estimate, currency) }) : t('cp.confirm_none') }}</button></div>
    </div>
  </div>
</template>

<style scoped>
.cps { max-height: 88vh; display: flex; flex-direction: column; background: #fffaf2; }
.cps .sheet-body { overflow-y: auto; display: grid; gap: 8px; }
.sub { text-align: center; margin-top: -4px; }
.grp { display: flex; justify-content: space-between; font-size: 13px; font-weight: 700; color: #7b6657; margin: 6px 2px -2px; }
.grp em { font-style: normal; font-weight: 400; }
.cp { display: flex; align-items: stretch; border: 1px solid #ecd9b8; border-radius: 12px; overflow: hidden; background: #fff; padding: 0; font: inherit; color: inherit; text-align: start; cursor: pointer; width: 100%; }
.cp .stub { width: 86px; flex: none; background: linear-gradient(160deg, #b8392a, #8f251b); color: #fff; display: grid; place-items: center; align-content: center; padding: 8px 2px; text-align: center; }
.cp .stub b { font-size: 19px; line-height: 1.15; } .cp .stub small { font-size: 11px; opacity: .92; }
.cp.store_delivery .stub { background: linear-gradient(160deg, #d0892c, #a8641a); }
.cp.platform_free .stub { background: linear-gradient(160deg, #7c3aed, #5b21b6); }
.cp .body { flex: 1; min-width: 0; padding: 9px 10px; display: flex; flex-direction: column; gap: 2px; }
.cp .body b { font-size: 15px; } .cp .body span { font-size: 12.5px; color: #7b6657; }
.cp .body .why { color: #a02c20; } .cp .body .soon { color: #c2410c; font-weight: 600; }
.cp .radio { align-self: center; margin: 0 12px; width: 22px; height: 22px; border-radius: 50%; border: 1.5px solid #c9b8a8; flex: none; display: grid; place-items: center; }
.cp .radio.on { border-color: #a02c20; background: #a02c20; } .cp .radio.on::after { content: "✓"; color: #fff; font-size: 13px; font-weight: 700; font-style: normal; }
.cp.off { opacity: .6; cursor: default; } .cp.off .stub { background: #b8aca2; }
.foot { padding: 10px 16px 14px; }
</style>
