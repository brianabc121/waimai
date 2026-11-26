<script setup lang="ts">
// Store settings: the store code and the daily reset time of order numbers
// (docs/ORDER_TYPES_DESIGN.md §4).
import { reactive, ref, watch } from 'vue'
import { put } from '../api'
import { t } from '../i18n'
import '../orderTypes'
import { typeColor } from '../orderTypes'
import type { Branch } from '../types'

const props = defineProps<{ branch: Branch }>()
const emit = defineEmits<{ saved: [b: Branch] }>()
const f = reactive({ store_code: '', day_reset_time: '04:00' })
watch(() => props.branch, b => { f.store_code = b.store_code ?? ''; f.day_reset_time = b.day_reset_time || '04:00' }, { immediate: true })
const error = ref('')
const ok = ref(false)
const times = ['00:00', '01:00', '02:00', '03:00', '04:00', '05:00', '06:00', '07:00', '08:00']

async function save() {
  error.value = ''; ok.value = false
  try {
    const b = await put<Branch>(`/merchant/branches/${props.branch.id}/order-numbering`, { store_code: f.store_code, day_reset_time: f.day_reset_time })
    ok.value = true; emit('saved', b)
  } catch (e) { error.value = (e as Error).message }
}
const code = () => (f.store_code || 'YPX').toUpperCase()
</script>

<template>
  <form class="ox-panel stack" data-testid="order-numbering" @submit.prevent="save">
    <div class="ox-panel-h" style="margin:0"><h2>{{ t('ot.set.title') }}</h2><span class="grow"></span><button class="btn primary" data-testid="on-save">{{ t('save') }}</button></div>
    <div v-if="error" class="banner bad">{{ error }}</div>
    <div v-else-if="ok" class="banner ok">{{ t('saved') }}</div>
    <div class="grid2">
      <label class="field"><span>{{ t('ot.set.code') }}</span>
        <input v-model="f.store_code" name="store_code" maxlength="8" pattern="[A-Za-z0-9]{2,8}" required style="text-transform:uppercase" data-testid="store-code" />
        <small class="muted">{{ t('ot.set.code_hint') }}</small></label>
      <label class="field"><span>{{ t('ot.set.reset') }}</span>
        <select v-model="f.day_reset_time" name="day_reset_time" data-testid="reset-time"><option v-for="x in times" :key="x" :value="x">{{ x }}</option></select>
        <small class="muted">{{ t('ot.set.reset_hint') }}</small></label>
    </div>
    <h3 style="margin:4px 0 0">{{ t('ot.set.preview') }}</h3>
    <div class="onprev">
      <div><span class="ottag" :style="{ background: typeColor.dine_in }">{{ t('ot.t.dine_in') }}</span><b>DineIn-1</b><small class="muted">{{ t('ot.set.p_dine') }}</small></div>
      <div><span class="ottag" :style="{ background: typeColor.staff }">{{ t('ot.t.staff') }}</span><b data-testid="staff-preview">{{ code() }}000123</b><small class="muted">{{ t('ot.set.p_staff') }}</small></div>
      <div><span class="ottag" :style="{ background: typeColor.platform }">{{ t('ot.t.platform') }}</span><b>Grab GF-123</b></div>
      <div><span class="ottag" :style="{ background: typeColor.mall }">{{ t('ot.t.mall') }}</span><b>Hippo-1</b><small class="muted">{{ t('ot.set.p_mall') }}</small></div>
      <div><span class="ottag" :style="{ background: typeColor.web }">{{ t('ot.t.web') }}</span><b>Online-1</b><small class="muted">{{ t('ot.set.p_web') }}</small></div>
    </div>
    <p class="muted small" style="margin:0">{{ t('ot.set.note') }}</p>
  </form>
</template>

<style scoped>
.grid2 { display: grid; grid-template-columns: 1fr 1fr; gap: 0 12px; }
.onprev { display: grid; gap: 8px; }
.onprev > div { display: flex; flex-wrap: wrap; gap: 4px 10px; align-items: center; }
.onprev b { font-variant-numeric: tabular-nums; min-width: 110px; }
.ottag { color: #fff; border-radius: 6px; padding: 1px 8px; font-size: 12px; font-weight: 700; white-space: nowrap; }
</style>
