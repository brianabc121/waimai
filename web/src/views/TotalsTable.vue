<script setup lang="ts">
import { money, t } from '../i18n'
import type { Totals } from '../types'
defineProps<{ totals: Totals; currency: string; delivery: boolean }>()
</script>

<template>
  <table class="totals">
    <tbody>
      <tr><td>{{ t('subtotal') }}</td><td>{{ money(totals.subtotal, currency) }}</td></tr>
      <tr v-if="totals.packaging_fee"><td>{{ t('packaging_fee') }}</td><td>{{ money(totals.packaging_fee, currency) }}</td></tr>
      <tr v-if="delivery"><td>{{ t('delivery_fee') }}</td><td>{{ money(totals.delivery_fee, currency) }}</td></tr>
      <tr v-if="totals.service_charge"><td>{{ t('service_charge') }}</td><td>{{ money(totals.service_charge, currency) }}</td></tr>
      <tr v-if="totals.tax_amount && !totals.tax_inclusive"><td>{{ t('tax') }} {{ totals.tax_rate_bp / 100 }}%</td><td>{{ money(totals.tax_amount, currency) }}</td></tr>
      <tr v-if="totals.discount"><td>−</td><td>−{{ money(totals.discount, currency) }}</td></tr>
      <tr class="sum"><td>{{ t('total') }}</td><td>{{ money(totals.total, currency) }}</td></tr>
      <tr v-if="totals.tax_amount && totals.tax_inclusive"><td class="muted small">{{ t('tax_included') }} {{ totals.tax_rate_bp / 100 }}%</td>
        <td class="muted small">{{ money(totals.tax_amount, currency) }}</td></tr>
    </tbody>
  </table>
</template>
