<script setup lang="ts">
// The period picker of the work-summary pages: a day or a range, with
// shortcuts. Days are those of the store's time zone.
import { computed, ref, watch } from 'vue'
import { t } from '../../i18n'
import { addDays, dayOf, monthStart, weekday } from './time'
import './strings'

const props = defineProps<{ tz: string }>()
const from = defineModel<string>('from', { required: true })
const to = defineModel<string>('to', { required: true })
type Preset = 'today' | 'yesterday' | 'week' | 'month' | 'custom'
const preset = ref<Preset>('today')
const today = computed(() => dayOf(Date.now(), props.tz))

function choose(p: Preset) {
  const d = today.value
  preset.value = p
  if (p === 'today') { from.value = to.value = d }
  else if (p === 'yesterday') { from.value = to.value = addDays(d, -1) }
  else if (p === 'week') { from.value = addDays(d, -6); to.value = d }
  else if (p === 'month') { from.value = monthStart(d); to.value = d }
}
const single = computed(() => from.value === to.value)
const span = computed(() => Math.round((Date.parse(to.value) - Date.parse(from.value)) / 86400000) + 1)
function step(n: number) {
  const days = span.value * n, f = addDays(from.value, days), e = addDays(to.value, days)
  from.value = f; to.value = e
  preset.value = f === e && e === today.value ? 'today' : f === e && e === addDays(today.value, -1) ? 'yesterday' : 'custom'
}
function openDay(d: string) { from.value = to.value = d; preset.value = d === today.value ? 'today' : 'custom' }
if (!from.value) choose('today')
// The store may keep another time than first assumed: start over from its today.
watch(() => props.tz, () => choose(preset.value === 'custom' ? 'today' : preset.value))
const title = computed(() => single.value ? `${from.value} ${t('weekday.' + weekday(from.value))}` : `${from.value.slice(5)} ~ ${to.value.slice(5)}`)
/** "today", "yesterday", "11-20" or "these 7 days", for sentences */
const text = computed(() => preset.value === 'today' ? t('att.today') : preset.value === 'yesterday' ? t('kr.yesterday') : single.value ? from.value.slice(5) : t('kr.n_days', { n: span.value }))
defineExpose({ openDay, text, single, today })
</script>

<template>
  <span class="datepick"><button data-act="prev" @click="step(-1)"><span class="rtl-flip">‹</span></button><b data-testid="period-title">{{ title }}</b><button data-act="next" :disabled="to >= today" @click="step(1)"><span class="rtl-flip">›</span></button></span>
  <span class="pills">
    <button v-for="p in (['today', 'yesterday', 'week', 'month'] as Preset[])" :key="p" :class="{ on: preset === p }" :data-preset="p" @click="choose(p)">{{ t('kr.p.' + p) }}</button>
    <button :class="{ on: preset === 'custom' }" data-preset="custom" @click="preset = 'custom'">{{ t('kr.p.custom') }}</button>
  </span>
  <span v-if="preset === 'custom'" class="custom"><input v-model="from" type="date" name="from" :max="to"> ~ <input v-model="to" type="date" name="to" :min="from" :max="today"></span>
</template>
