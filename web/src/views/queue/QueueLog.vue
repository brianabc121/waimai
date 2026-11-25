<script setup lang="ts">
// 排队记录: every step of every number on a day — who did it and when;
// moving a number up shows from where to where and why (§6.6).
import { onMounted, ref, watch } from 'vue'
import { get } from '../../api'
import { t } from '../../i18n'
import { qErr } from '../../queue'

interface Entry { id: number; ticket_id: number; number: string; people: number; action: string; actor: string; detail: Record<string, any>; at: string }
const props = defineProps<{ base: string }>()
const date = ref('')
const rows = ref<Entry[] | null>(null)
const error = ref('')

async function load() {
  try { rows.value = (await get<{ events: Entry[] }>(`${props.base}/log${date.value ? '?date=' + date.value : ''}`)).events; error.value = '' }
  catch (e) { error.value = qErr(e) }
}
onMounted(load)
watch(date, load)
function shift(d: number) {
  const base = date.value ? new Date(date.value + 'T12:00:00') : new Date()
  base.setDate(base.getDate() + d)
  date.value = base.toISOString().slice(0, 10)
}
const action = (a: string) => t('qu.log.a.' + a)
function detail(e: Entry): string {
  const d = e.detail ?? {}
  switch (e.action) {
    case 'take': return [t('qu.src.' + d.source), t('qu.people', { n: d.people })].concat(d.priority ? [t('qu.priority') + ' · ' + d.reason] : []).join(' · ')
    case 'call': case 'recall': case 'seat': return d.table ? t('qu.log.d.table', { t: d.table }) : ''
    case 'jump': return t('qu.log.d.jump', { a: d.from, b: d.to }) + ' · ' + (d.reason ?? '')
    case 'requeue': return t('qu.log.d.pos', { n: d.position })
    case 'people': return t('qu.log.d.people', { a: d.from, b: d.to })
    default: return ''
  }
}
const clock = (iso: string) => new Date(iso).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit' })
</script>

<template>
  <section class="ox-panel">
    <div class="ox-tools">
      <button class="btn small" @click="shift(-1)">‹</button>
      <input v-model="date" type="date" data-testid="ql-date" />
      <button class="btn small" @click="shift(1)">›</button>
    </div>
    <div v-if="error" class="banner bad">{{ error }}</div>
    <div class="ox-scroll">
      <table class="data">
        <thead><tr><th>{{ t('qu.log.time') }}</th><th>{{ t('qu.log.no') }}</th><th>{{ t('qu.log.what') }}</th><th></th><th>{{ t('qu.log.by') }}</th></tr></thead>
        <tbody>
          <tr v-if="rows && !rows.length"><td colspan="5" class="ox-empty">{{ t('qu.log.none') }}</td></tr>
          <tr v-for="e in rows ?? []" :key="e.id" data-testid="ql-log">
            <td class="nowrap">{{ clock(e.at) }}</td>
            <td><span class="ox-code">{{ e.number }}</span></td>
            <td><span class="ox-tag" :class="{ blue: e.action === 'jump', brand: e.action === 'seat' }">{{ action(e.action) }}</span></td>
            <td>{{ detail(e) }}</td>
            <td>{{ e.actor || (e.detail?.by === 'guest' || e.action === 'take' ? t('qu.log.guest') : '—') }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
</template>
