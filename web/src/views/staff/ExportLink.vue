<script setup lang="ts">
// "Export to Excel" of a list under Staff: the same request as the page
// makes, with format=xlsx, in the language the page is shown in.
import { computed } from 'vue'
import { locale, t } from '../../i18n'
import { session } from '../../session'
import './strings'

const props = defineProps<{ list: 'attendance' | 'reports/kitchen' | 'reports/riders'; from: string; to: string }>()
const href = computed(() => `/api/v1/merchant/branches/${session.branchId}/${props.list}?from=${props.from}&to=${props.to}&format=xlsx&lang=${locale.value}`)
</script>

<template>
  <a v-if="from && to && from <= to" class="btn" data-testid="export" :href="href" download>{{ t('att.export') }}</a>
</template>
