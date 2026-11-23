<script setup lang="ts">
// "My attendance" on the Me tab: the person's own shifts of one month.
// Read-only; what a manager corrected shows with the reason.
import { computed, onMounted, ref } from 'vue'
import { get } from '../../api'
import { intlTag, t } from '../../i18n'
import { session } from '../../session'
import { addMonths, clock, dayOf, hm, weekday } from '../staff/time'
import './strings'

interface Shift {
  id: number; day: string; started_at: string; ended_at: string | null; minutes: number; end_reason: string
  orig_started_at: string | null; orig_ended_at: string | null; edited: boolean; edited_by: string; edit_reason: string; voided: boolean
}
interface Mine { month: string; timezone: string; now: string; days: number; minutes: number; shifts: Shift[] }

const data = ref<Mine | null>(null)
const error = ref('')
const open = ref(false)
async function load(month = '') {
  try {
    data.value = await get<Mine>(`/work/attendance?branch_id=${session.branchId}${month ? '&month=' + month : ''}`)
    error.value = ''
  } catch (e) { error.value = (e as Error).message }
}
onMounted(() => load())
const tz = computed(() => data.value?.timezone ?? 'Asia/Manila')
const thisMonth = computed(() => dayOf(Date.now(), tz.value).slice(0, 7))
const step = (n: number) => data.value && load(addMonths(data.value.month + '-01', n).slice(0, 7))
// "09:58" on the day the shift started, "11-22 01:10" when it ended on another day
const at = (iso: string, day: string) => dayOf(iso, tz.value) === day ? clock(iso, tz.value) : `${dayOf(iso, tz.value).slice(5)} ${clock(iso, tz.value)}`
// the weekday's short name in the reader's language (2026-11-22 is a Sunday)
const wd = (n: number) => new Date(Date.UTC(2026, 10, 22 + n)).toLocaleDateString(intlTag(), { weekday: 'short', timeZone: 'UTC' })
const dayName = (d: string) => `${d.slice(5)} ${wd(weekday(d))}`
</script>

<template>
  <div class="card stack" data-testid="my-shifts">
    <div class="row between">
      <b>{{ t('wk.att.title') }}</b>
      <span v-if="data" class="row" style="gap:10px"><button class="btn small" data-act="prev-month" @click="step(-1)"><span class="rtl-flip">‹</span></button>
        <b data-testid="my-month">{{ data.month }}</b>
        <button class="btn small" data-act="next-month" :disabled="data.month >= thisMonth" @click="step(1)"><span class="rtl-flip">›</span></button></span>
    </div>
    <div v-if="error" class="banner bad small">{{ error }}</div>
    <template v-if="data">
      <div class="row between"><span class="muted">{{ t('wk.att.days') }}</span><b data-testid="my-days">{{ t('wk.att.n_days', { n: data.days }) }}</b></div>
      <div class="row between"><span class="muted">{{ t('wk.att.hours') }}</span><b data-testid="my-hours">{{ hm(data.minutes) }}</b></div>
      <button v-if="data.shifts.length" class="btn small" data-act="my-shifts-open" @click="open = !open">{{ open ? t('wk.att.hide') : t('wk.att.show', { n: data.shifts.length }) }}</button>
      <div v-else class="small muted">{{ t('wk.att.none') }}</div>
      <template v-if="open">
        <div v-for="s in data.shifts" :key="s.id" class="myshift" :class="{ voided: s.voided }" data-testid="my-shift">
          <div class="row between"><span>{{ dayName(s.day) }}</span>
            <b>{{ clock(s.started_at, tz) }} ~ {{ s.ended_at ? at(s.ended_at, s.day) : t('wk.att.now') }}</b></div>
          <div class="row between small muted"><span>
            <span v-if="s.voided" class="tag bad">{{ t('wk.att.voided') }}</span>
            <span v-else-if="s.edited" class="tag warn">{{ t('wk.att.edited') }}</span>
            <span v-else-if="s.end_reason === 'auto'" class="tag warn">{{ t('wk.att.auto') }}</span></span>
            <span v-if="!s.voided">{{ hm(s.minutes) }}</span></div>
          <div v-if="s.edited" class="small muted" data-testid="my-shift-why">{{ t('wk.att.why', { who: s.edited_by || t('wk.att.manager'), why: s.edit_reason }) }}
            <template v-if="s.orig_started_at">{{ t('wk.att.was', { from: at(s.orig_started_at, s.day), to: s.orig_ended_at ? at(s.orig_ended_at, s.day) : '—' }) }}</template></div>
        </div>
      </template>
      <div class="small muted">{{ t('wk.att.note') }}</div>
    </template>
  </div>
</template>

<style>
.wk .myshift { border-top: 1px solid var(--line); padding-top: 8px; display: grid; gap: 3px; }
.wk .myshift.voided b { text-decoration: line-through; color: var(--muted); }
</style>
