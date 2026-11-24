<script setup lang="ts">
import { discountLabel, kindBadge } from '../coupons'
import { money, pick, t } from '../i18n'
import '../checkout'
import type { OrderDiscount, Totals } from '../types'
// discounts, when given, are shown one per line: the promotion and each coupon.
defineProps<{ totals: Totals; currency: string; delivery: boolean; discounts?: OrderDiscount[] }>()
</script>

<template>
  <table class="totals">
    <tbody>
      <tr><td>{{ t('subtotal') }}</td><td>{{ money(totals.subtotal, currency) }}</td></tr>
      <tr v-if="totals.packaging_fee"><td>{{ t('packaging_fee') }}</td><td>{{ money(totals.packaging_fee, currency) }}</td></tr>
      <tr v-if="delivery"><td>{{ t('delivery_fee') }}</td><td>{{ money(totals.delivery_fee, currency) }}</td></tr>
      <tr v-if="totals.service_charge"><td>{{ t('service_charge') }}</td><td>{{ money(totals.service_charge, currency) }}</td></tr>
      <tr v-if="totals.tax_amount && !totals.tax_inclusive"><td>{{ t('tax') }} {{ totals.tax_rate_bp / 100 }}%</td><td>{{ money(totals.tax_amount, currency) }}</td></tr>
      <template v-if="discounts?.length">
        <tr v-for="(d, i) in discounts" :key="i" class="disc" :class="{ plat: d.funded_by === 'platform' }" data-testid="discount-line">
          <td><i class="badge" :class="d.kind">{{ kindBadge(d.kind) }}</i>{{ discountLabel(d) }}</td><td>−{{ money(d.amount, currency) }}</td></tr>
      </template>
      <tr v-else-if="totals.discount" class="disc"><td>{{ pick(totals.promotion_name, totals.promotion_name_en) || t('discount') }}</td><td>−{{ money(totals.discount, currency) }}</td></tr>
      <tr v-if="totals.staff_discount" class="disc" data-testid="staff-discount-line"><td>{{ t('ck.staff_disc') }}</td><td>−{{ money(totals.staff_discount, currency) }}</td></tr>
      <tr v-if="totals.round_off" class="disc" data-testid="round-off-line"><td>{{ t(totals.adj_mode === 'round' ? 'ck.round_off' : 'ck.staff_disc') }}</td><td>−{{ money(totals.round_off, currency) }}</td></tr>
      <tr class="sum"><td>{{ t('total') }}</td><td>{{ money(totals.total, currency) }}</td></tr>
      <tr v-if="totals.tax_amount && totals.tax_inclusive"><td class="muted small">{{ t('tax_included') }} {{ totals.tax_rate_bp / 100 }}%</td>
        <td class="muted small">{{ money(totals.tax_amount, currency) }}</td></tr>
    </tbody>
  </table>
</template>

<style scoped>
.badge { display: inline-block; font-style: normal; font-size: 11px; line-height: 17px; padding: 0 4px; border-radius: 4px; background: var(--brand); color: #fff; margin-inline-end: 6px; vertical-align: 1px; }
.badge.promotion, .badge.dish_promotion { background: #d97706; } .badge.store_delivery { background: #b8792a; } .badge.platform_free { background: #6d28d9; }
.plat td:last-child { color: #6d28d9; }
</style>
