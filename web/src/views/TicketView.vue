<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { get } from '../api'
import { both, money, t, whenText } from '../i18n'
import type { Order } from '../types'
import { contactApps, contactName, contactPhone } from '../addresses'
import TotalsTable from './TotalsTable.vue'
import './dine/strings'
import { useRoute } from 'vue-router'
import { addonNo } from '../addon'
import './manualStrings'

// A browser-printable ticket (80mm-friendly). There is no printer integration
// in the first version; staff print from the browser when they need paper.
const props = defineProps<{ id: string }>()
const o = ref<Order | null>(null)
onMounted(async () => { o.value = await get<Order>(`/merchant/orders/${props.id}`) })
const print = () => window.print()
// An order split into baskets is printed basket by basket.
// ?batch=N prints that batch only: an add-on, or a dine-in round (docs/ADD_ON_ORDER_DESIGN.md §7.1).
const batch = Number(useRoute().query.batch) || 0
const mine = computed(() => (o.value?.items ?? []).filter(i => !batch || i.batch_seq === batch))
const batchInfo = computed(() => o.value?.batches?.find(b => b.seq === batch) ?? null)
const batchSum = computed(() => mine.value.filter(i => i.prep_status !== 'cancelled').reduce((s, i) => s + i.line_total, 0))
const multi = computed(() => mine.value.some(i => i.basket > 1))
const sorted = computed(() => [...mine.value].sort((a, b) => a.basket - b.basket || a.id - b.id))
</script>

<template>
  <div class="ticket" v-if="o">
    <button class="btn primary no-print" @click="print">{{ t('print') }}</button>
    <template v-if="batch">
      <h1 class="bhead" data-testid="ticket-batch">{{ o.service_type === 'dine_in' ? t('dd.round_head', { n: batch }) : t('ao.t.head', { n: addonNo(batch) }) }}</h1>
      <p><b>{{ o.service_type === 'dine_in' ? o.public_no : t('ao.t.with', { no: o.public_no }) }}</b></p>
      <p v-if="batchInfo?.round_tags?.length"><b>※ {{ batchInfo.round_tags.join(' · ') }}</b></p>
    </template>
    <h1 v-else>{{ o.public_no }}</h1>
    <p v-if="o.scheduled_for"><b>{{ t('scheduled') }} {{ whenText(o.scheduled_for, o.timezone) }}</b></p>
    <p><b>{{ t(o.service_type) }}</b> · {{ new Date(o.created_at).toLocaleString() }}</p>
    <p v-if="o.service_type === 'dine_in'" data-testid="ticket-table"><b>{{ t('table') }} {{ o.table_name }}</b><template v-if="o.table_tags?.length"> · {{ o.table_tags.join(' · ') }}</template></p>
    <p v-else>{{ contactName(o) }} · {{ contactPhone(o) }}
      <template v-for="c in contactApps(o)" :key="c.field"><br />{{ c.label }} {{ c.value }}</template></p>
    <p v-if="o.service_type === 'delivery'">{{ o.zone_name }} · {{ o.address_line }} {{ o.address_unit }}<br />{{ o.address_note }}</p>
    <p v-else-if="o.service_type === 'pickup'">{{ t('pickup_code') }}: <b>{{ o.pickup_code }}</b></p>
    <p v-if="o.note"><b>{{ t('order_note') }}: {{ o.note }}</b></p>
    <hr />
    <div v-for="(it, i) in sorted" :key="it.id" class="line">
      <p v-if="multi && (i === 0 || sorted[i - 1].basket !== it.basket)" class="bk"><b>—— {{ t('basket_n', { n: it.basket }) }} ——</b></p>
      <div><b>{{ it.qty }} × {{ both(it.name, it.name_en) }}</b><span>{{ money(it.line_total, o.currency) }}</span></div>
      <small v-if="it.options.length">{{ it.options.map(x => both(x.name, x.name_en)).join(' · ') }}</small>
      <small v-if="it.note_tags?.length" class="nt" data-testid="ticket-notes">※ {{ it.note_tags.map(x => x.name).join(' ') }}</small>
      <small v-if="it.note" class="nt">{{ t('note') }}: {{ it.note }}</small>
      <small v-if="it.prep_status === 'cancelled'" class="nt">{{ t('dd.voided', { r: it.void_reason || '—' }) }}</small>
    </div>
    <hr />
    <p v-if="batch" class="line"><span>{{ t('vo.total') }}</span> <b>{{ money(batchSum, o.currency) }}</b>
      <template v-if="batchInfo?.total_after != null"> · {{ t('ao.changed', { a: money(batchInfo.total_before ?? 0, o.currency), b: money(batchInfo.total_after, o.currency) }) }}</template></p>
    <TotalsTable v-else :totals="o.totals" :currency="o.currency" :delivery="o.service_type === 'delivery'" :discounts="o.discounts" />
    <p v-if="o.service_type === 'dine_in' && o.amount_received != null" data-testid="ticket-paid">
      {{ o.pay_method || t('dd.paid') }} {{ money(o.amount_received, o.currency) }} · {{ t('dd.change') }} {{ money(o.amount_received - o.totals.total, o.currency) }}</p>
    <p v-else>{{ t(o.payment_method) }} · {{ t(o.payment_status) }}</p>
  </div>
</template>

<style scoped>
.ticket { width: 80mm; max-width: 100%; margin: 0 auto; padding: 8px; background: #fff; font-size: 13px; }
.ticket p { margin: 4px 0; }
.ticket .bk { text-align: center; margin: 6px 0 2px; }
.line { margin: 6px 0; } .line > div { display: flex; justify-content: space-between; gap: 8px; }
.line small { display: block; }
.line small.nt { font-weight: 700; padding-inline-start: 10px; }
.bhead { font-size: 26px; text-align: center; border: 2px solid #000; padding: 4px; }
hr { border: 0; border-top: 1px dashed #000; margin: 8px 0; }
</style>
