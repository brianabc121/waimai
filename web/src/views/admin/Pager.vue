<script setup lang="ts">
import { computed } from 'vue'
import { t } from '../../i18n'
import { pages } from './util'

const props = defineProps<{ total: number; page: number; size: number }>()
const emit = defineEmits<{ go: [page: number] }>()
const last = computed(() => pages(props.total, props.size))
</script>

<template>
  <div class="row small muted">
    <slot />
    <span class="grow"></span>
    <span>{{ t('ad.total_n', { n: total }) }} · {{ t('ad.page_n', { p: page, t: last }) }}</span>
    <button class="btn small" :disabled="page <= 1" @click="emit('go', page - 1)">{{ t('ad.prev') }}</button>
    <button class="btn small" :disabled="page >= last" data-testid="next-page" @click="emit('go', page + 1)">{{ t('ad.next') }}</button>
  </div>
</template>
