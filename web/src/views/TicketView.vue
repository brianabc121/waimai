<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { get } from '../api'
import { both, money, t, whenText } from '../i18n'
import type { Order } from '../types'
import TotalsTable from './TotalsTable.vue'

// A browser-printable ticket (80mm-friendly). There is no printer integration
// in the first version; staff print from the browser when they need paper.
const props = defineProps<{ id: string }>()
const o = ref<Order | null>(null)
onMounted(async () => { o.value = await get<Order>(`/merchant/orders/${props.id}`) })
const print = () => window.print()
// An order split into baskets is printed basket by basket.
const multi = computed(() => !!o.value?.items.some(i => i.basket > 1))
const sorted = computed(() => [...(o.value?.items ?? [])].sort((a, b) => a.basket - b.basket || a.id - b.id))
</script>

<template>
  <div class="ticket" v-if="o">
    <button class="btn primary no-print" @click="print">{{ t('print') }}</button>
    <h1>{{ o.public_no }}</h1>
    <p v-if="o.scheduled_for"><b>{{ t('scheduled') }} {{ whenText(o.scheduled_for, o.timezone) }}</b></p>
    <p><b>{{ t(o.service_type) }}</b> · {{ new Date(o.created_at).toLocaleString() }}</p>
    <p>{{ o.customer_name }} · {{ o.customer_phone }}</p>
    <p v-if="o.service_type === 'delivery'">{{ o.zone_name }} · {{ o.address_line }} {{ o.address_unit }}<br />{{ o.address_note }}</p>
    <p v-else>{{ t('pickup_code') }}: <b>{{ o.pickup_code }}</b></p>
    <p v-if="o.note"><b>{{ t('order_note') }}: {{ o.note }}</b></p>
    <hr />
    <div v-for="(it, i) in sorted" :key="it.id" class="line">
      <p v-if="multi && (i === 0 || sorted[i - 1].basket !== it.basket)" class="bk"><b>—— {{ t('basket_n', { n: it.basket }) }} ——</b></p>
      <div><b>{{ it.qty }} × {{ both(it.name, it.name_en) }}</b><span>{{ money(it.line_total, o.currency) }}</span></div>
      <small v-if="it.options.length">{{ it.options.map(x => both(x.name, x.name_en)).join(' · ') }}</small>
      <small v-if="it.note">{{ t('note') }}: {{ it.note }}</small>
    </div>
    <hr />
    <TotalsTable :totals="o.totals" :currency="o.currency" :delivery="o.service_type === 'delivery'" />
    <p>{{ t(o.payment_method) }} · {{ t(o.payment_status) }}</p>
  </div>
</template>

<style scoped>
.ticket { width: 80mm; max-width: 100%; margin: 0 auto; padding: 8px; background: #fff; font-size: 13px; }
.ticket p { margin: 4px 0; }
.ticket .bk { text-align: center; margin: 6px 0 2px; }
.line { margin: 6px 0; } .line > div { display: flex; justify-content: space-between; gap: 8px; }
.line small { display: block; }
hr { border: 0; border-top: 1px dashed #000; margin: 8px 0; }
</style>
